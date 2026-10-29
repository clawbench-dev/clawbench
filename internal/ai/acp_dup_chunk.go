package ai

import (
	"encoding/json"
)

// ---------------------------------------------------------------------------
// Duplicate model-output chunk suppression — CodeBuddy only
// ---------------------------------------------------------------------------
//
// CodeBuddy can deliver the same model-output chunk more than once. Observed in
// production (2026-10-21, an agent-team turn on a connection whose process had
// been killed and resumed): every delta of some blocks arrived back-to-back N
// times, so the accumulated text read "已已把报告把报告发给发给 team team-le-leadad…"
// — the concatenation of [d][d] (and, in another message, [d][d][d]) for each
// delta d.
//
// Upstream cause (read from the agent bundle): the team bridge sends each member
// update through `sendSessionUpdateBounded`, which races the send against a
// timeout and THROWS on timeout; `sendSessionUpdateWithRetries` then retries up
// to 3 times. A send that completes just after the timeout fires is therefore
// delivered again by the retry — with the SAME update object.
//
// ---------------------------------------------------------------------------
// Why this is NOT a time window (and not upstream's 5 s rule)
// ---------------------------------------------------------------------------
//
// CodeBuddy's own `chunkPublicationKey` de-duplicates with a 5-second window.
// Porting that window is wrong for ClawBench, and the production data says so:
// over 2026-10-22..10-29 the window dropped 6436 chunks, and the gaps between
// consecutive drops had a median of 11.7 s (p75 40 s, p90 120 s) — i.e. most
// drops were of text that had legitimately repeated long before. 357 of those
// drops fell INSIDE a 5 s window, deleting real content.
//
// The reason upstream gets away with a time window and we cannot is that
// upstream applies it on its WebUI publish bus, where chunks are coalesced
// before publication. ClawBench consumes the ACP wire, where CodeBuddy streams
// model output CHARACTER BY CHARACTER: a single `|---|---|` delimiter row
// arrives as `'|' ' ---' ' |' ' ---' ' |' '\n'`, and short fragments like
// `' |'` repeat 21 times within one message. Any rule that treats "same text
// again soon" as a duplicate deletes real content.
//
// What actually distinguishes a retry from a legitimate repeat is ADJACENCY.
// A retry re-sends the SAME update object synchronously on the forwarding
// queue, so it is the very next frame. A legitimate repeat is always separated
// by other output — measured on real wire data (2026-10-29, 82 frames of a
// two-table reply): adjacent frames never had identical text (0/81), while
// non-adjacent frames did (55 pairs). So the guard drops a chunk only when it
// is identical to the IMMEDIATELY PRECEDING accepted chunk.
//
// That rule is strictly safer than a window: it can only ever drop a frame that
// is byte-identical to the one before it, which is precisely the retry shape,
// and it never drops a repeat that has any output between it and its twin.
//
// Deliberately scoped to CodeBuddy: no other backend is known to emit this
// retry pattern, and a scan of chat_history (2026-10-29) found zero duplicated
// blocks for opencode (2559 messages), claude (54), pi (79) and codex (14).
// Dropping another agent's content on a guess is worse than the duplication.

// metaKeyCodeBuddyTimestamp carries the chunk's publication timestamp. It is
// part of the identity so that a retry (which reuses the same object, hence the
// same timestamp) matches, while a genuinely new chunk does not.
const metaKeyCodeBuddyTimestamp = "timestamp"

// chunkDedupEnabled reports whether duplicate-chunk suppression applies to this
// backend. See the file comment for why it is CodeBuddy-only.
func chunkDedupEnabled(backendID string) bool {
	return backendID == "codebuddy"
}

// acpTopLevelMessageID dereferences the ACP `messageId` field, which upstream
// prefers over the `_meta` copy.
func acpTopLevelMessageID(id *string) string {
	if id == nil {
		return ""
	}
	return *id
}

// chunkIdentity builds the identity of one model-output chunk, mirroring the
// element list of CodeBuddy's `chunkPublicationKey`:
//
//	sessionId + variant + messageId + timestamp + requestId + parentToolCallId + text
//
// `messageId` prefers the top-level field and falls back to the `_meta` copy;
// `requestId` prefers conversationRequestId and falls back to requestId —
// both matching upstream's `??` chains. Returns "" when the identity pair is
// missing, in which case the caller must NOT drop the chunk.
func chunkIdentity(sessionID, variant, topLevelMessageID string, meta map[string]any, text string) string {
	ts := metaString(meta[metaKeyCodeBuddyTimestamp])
	msgID := topLevelMessageID
	if msgID == "" {
		msgID = metaString(meta[metaKeyCodeBuddyMessageID])
	}
	if ts == "" || msgID == "" {
		return ""
	}
	requestID := metaString(meta[metaKeyCodeBuddyConversationRequestID])
	if requestID == "" {
		requestID = metaString(meta[metaKeyCodeBuddyRequestID])
	}
	parentID := metaString(meta[metaKeyCodeBuddyParentToolCallID])

	// JSON array, like upstream's JSON.stringify([...]). Marshaling a []string
	// is injective (escaping makes it unambiguous) and cannot fail.
	parts, err := json.Marshal([]string{sessionID, variant, msgID, ts, requestID, parentID, text})
	if err != nil {
		// Unreachable: []string always marshals. Fail open (no key) rather than
		// dropping content on an error.
		return ""
	}
	return string(parts)
}

// isDuplicateChunk reports whether this chunk is a retry of the immediately
// preceding one, recording it as the new "previous" otherwise.
//
// Adjacency — not elapsed time — is the discriminator; see the file comment for
// the measurement that establishes it. Concretely: a chunk is a duplicate iff
// its identity equals the identity of the last ACCEPTED chunk. A dropped retry
// does not replace the remembered identity, so three copies of one frame
// collapse to one.
//
// Called from the ACP notification goroutine, so it takes only the leaf stateMu
// lock — never c.mu (see ACPConn.stateMu).
func (c *ACPConn) isDuplicateChunk(identity string) bool {
	if c == nil || identity == "" {
		return false
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if identity == c.lastChunkKey {
		return true
	}
	c.lastChunkKey = identity
	return false
}

// resetChunkDedup forgets the previous chunk identity. Called when a prompt
// starts: the identity describes the previous frame of one turn's stream, and
// carrying it across turns would let the first chunk of a new turn be mistaken
// for a retry of the last chunk of the old one.
func (c *ACPConn) resetChunkDedup() {
	c.stateMu.Lock()
	c.lastChunkKey = ""
	c.stateMu.Unlock()
}
