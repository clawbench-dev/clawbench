package ai

import (
	"encoding/json"
	"time"
)

// ---------------------------------------------------------------------------
// Duplicate model-output chunk suppression — CodeBuddy's own scheme, ported
// ---------------------------------------------------------------------------
//
// CodeBuddy's agent bundle de-duplicates volatile session updates with
// `chunkPublicationKey` (dist/codebuddy.js). This file is a faithful port of
// that function and of the check that consumes it, so ClawBench drops exactly
// what CodeBuddy itself would drop and nothing else.
//
// Upstream, verbatim:
//
//	function chunkPublicationKey(sessionId, update) {
//	  if (!isVolatileSessionUpdate(update)) return
//	  const meta = asRecord(update._meta)
//	  const ts = readString(meta?.timestamp)
//	  const msgId = readString(update.messageId) ?? readString(meta?.["codebuddy.ai/messageId"])
//	  if (ts && msgId)
//	    return JSON.stringify([sessionId, update.sessionUpdate, msgId, ts,
//	                           meta?.["codebuddy.ai/conversationRequestId"] ?? meta?.["codebuddy.ai/requestId"],
//	                           meta?.["codebuddy.ai/parentToolCallId"],
//	                           extractTextContent(update.content)])
//	}
//
//	function publishSessionUpdateToBus(sessionId, update) {
//	  ...
//	  const key = chunkPublicationKey(sessionId, update)
//	  if (key) {
//	    const seenAt = seen.get(key)
//	    if (seenAt !== undefined && now - seenAt < 5000) return false   // duplicate
//	    seen.set(key, now)
//	    if (seen.size > 8192) for (const [k, t] of seen) if (now - t >= 5000) seen.delete(k)
//	  }
//	  ...
//	}
//
// Three properties are load-bearing and easy to get wrong:
//
//  1. BOTH `timestamp` and `messageId` are required (`if (ts && msgId)`). Without
//     them the key is empty and the update is NOT de-duplicated — a text-only
//     key would also match a model legitimately repeating a token.
//  2. The window is 5 s and is measured from the ACCEPTED delivery: a dropped
//     duplicate does not refresh the entry. Upstream compares `now - seenAt`
//     against a timestamp it only writes on acceptance.
//  3. `messageId` prefers the TOP-LEVEL `update.messageId` and only falls back
//     to `_meta["codebuddy.ai/messageId"]`.
//
// Deliberately scoped to CodeBuddy: no other backend is known to emit this
// identity tuple, and every other backend is verified to produce no duplicated
// output (chat_history scan 2026-10-29: opencode 2559 messages, claude 54,
// pi 79, codex 14 — zero duplicated blocks). Dropping another agent's content
// on a guess is worse than the duplication.

// metaKeyCodeBuddyTimestamp carries the chunk's publication timestamp — half of
// the identity pair.
const metaKeyCodeBuddyTimestamp = "timestamp"

// chunkDedupWindow mirrors upstream's `eQ = 5e3`.
const chunkDedupWindow = 5 * time.Second

// maxChunkDedupEntries mirrors upstream's `eS.size > 8192` prune threshold.
const maxChunkDedupEntries = 8192

// chunkDedupNow is the clock behind chunkDedupWindow. Injectable so tests can
// advance time instead of sleeping. Production never reassigns it.
var chunkDedupNow = time.Now

// volatileSessionUpdate mirrors upstream's `isVolatileSessionUpdate`: the three
// streaming variants, plus any update carrying an agentPhase/progress marker.
//
// The agentPhase/progress half is not ported because ClawBench only ever calls
// this for the two content variants (see the call sites in acp_events.go);
// adding an unreachable branch would be dead code. If a future call site feeds
// another variant, port that clause too.
func volatileSessionUpdate(variant string) bool {
	switch variant {
	case "agent_message_chunk", "agent_thought_chunk", "user_message_chunk":
		return true
	}
	return false
}

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

// chunkIdentity builds the de-duplication key, mirroring `chunkPublicationKey`
// element for element.
//
// Returns "" when the frame lacks the identity pair or is not a volatile
// variant, in which case the caller must NOT drop it.
func chunkIdentity(sessionID, variant, topLevelMessageID string, meta map[string]any, text string) string {
	if !volatileSessionUpdate(variant) {
		return ""
	}
	ts := metaString(meta[metaKeyCodeBuddyTimestamp])
	msgID := topLevelMessageID
	if msgID == "" {
		msgID = metaString(meta[metaKeyCodeBuddyMessageID])
	}
	if ts == "" || msgID == "" {
		return ""
	}
	// Upstream: `conversationRequestId ?? requestId`.
	requestID := metaString(meta[metaKeyCodeBuddyConversationRequestID])
	if requestID == "" {
		requestID = metaString(meta[metaKeyCodeBuddyRequestID])
	}
	parentID := metaString(meta[metaKeyCodeBuddyParentToolCallID])

	// JSON array, like upstream's JSON.stringify([...]). Marshaling a []string
	// is the same encoding and cannot fail.
	parts, err := json.Marshal([]string{sessionID, variant, msgID, ts, requestID, parentID, text})
	if err != nil {
		// Unreachable: []string always marshals. Fail open (no key) rather than
		// dropping content on an error.
		return ""
	}
	return string(parts)
}

// isDuplicateChunk reports whether this chunk was already delivered inside
// chunkDedupWindow, recording it otherwise. Mirrors upstream's seen-map check.
//
// Called from the ACP notification goroutine, so it takes only the leaf stateMu
// lock — never c.mu (see ACPConn.stateMu).
func (c *ACPConn) isDuplicateChunk(identity string) bool {
	if c == nil || identity == "" {
		return false
	}
	now := chunkDedupNow()
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if c.seenChunkKeys == nil {
		c.seenChunkKeys = make(map[string]time.Time, 256)
	}
	if seenAt, dup := c.seenChunkKeys[identity]; dup && now.Sub(seenAt) < chunkDedupWindow {
		// A duplicate does not refresh the entry: upstream only writes the map
		// on acceptance, so the window is measured from the accepted delivery.
		return true
	}
	if len(c.seenChunkKeys) >= maxChunkDedupEntries {
		// Upstream prunes expired entries rather than clearing wholesale.
		for id, seenAt := range c.seenChunkKeys {
			if now.Sub(seenAt) >= chunkDedupWindow {
				delete(c.seenChunkKeys, id)
			}
		}
	}
	c.seenChunkKeys[identity] = now
	return false
}

// resetChunkDedup clears the per-turn identity set. Called when a prompt starts:
// the set describes one turn's deliveries, and carrying it across turns would
// only grow it for no benefit.
func (c *ACPConn) resetChunkDedup() {
	c.stateMu.Lock()
	c.seenChunkKeys = nil
	c.stateMu.Unlock()
}
