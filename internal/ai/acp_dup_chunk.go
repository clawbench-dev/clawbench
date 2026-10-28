package ai

import "time"

// ---------------------------------------------------------------------------
// Duplicate model-output chunk suppression (CodeBuddy)
// ---------------------------------------------------------------------------
//
// CodeBuddy can deliver the same model-output chunk more than once. Observed in
// production (2026-10-21, an agent-team turn on a connection whose process had
// been killed and resumed): every delta of some blocks arrived back-to-back N
// times, so the accumulated text read "已已把报告把报告发给发给 team team-le-leadad…"
// — the concatenation of [d][d] (and, in another message, [d][d][d]) for each
// delta d. It hit lead and member content alike, and only whole blocks (a
// neighboring block from the same member was clean), so it is duplicate
// DELIVERY, not the model repeating itself.
//
// Upstream cause (read from the agent bundle): the team bridge sends each member
// update through `sendSessionUpdateBounded`, which races the send against a
// timeout and THROWS on timeout; `sendSessionUpdateWithRetries` then retries up
// to 3 times. A send that completes just after the timeout fires is therefore
// delivered again by the retry — with the same update object, hence the same
// `_meta`. That matches the observed multiplicities (2 and 3).
//
// It cannot be fixed on our side of the wire, so duplicates are dropped at the
// frame level, where identity is available. The key mirrors the one CodeBuddy
// itself uses to de-duplicate volatile session updates internally
// (`chunkPublicationKey`): sessionId + sessionUpdate + messageId + timestamp +
// member + text.
//
// Both `messageId` and `timestamp` are REQUIRED. That is upstream's own rule
// (`if (timestamp && messageId)`): without a timestamp a key built from text
// alone would also match a model legitimately emitting the same token twice in a
// row, so the safe answer is to not de-duplicate at all.
//
// The identity is only unique WITHIN the retry window. `timestamp` is the
// message-level publication time, not a per-chunk sequence number: every chunk
// of one assistant message carries the SAME `messageId` and the SAME
// `timestamp`, so the text is the only component that varies. Identical text is
// ordinary in model output — every GFM table's delimiter row is the same
// `|---|---|` string — which makes an unbounded set delete real content. See
// chunkDedupWindow for the production evidence.
//
// Deliberately scoped to CodeBuddy: no other backend is known to carry this
// identity pair, and dropping another agent's content on a guess is worse than
// the duplication.

// metaKeyCodeBuddyTimestamp carries the chunk's publication timestamp — the
// other half of the de-duplication key (see the file comment).
const metaKeyCodeBuddyTimestamp = "timestamp"

// chunkDedupWindow is how long an identity counts as a duplicate delivery.
//
// It mirrors CodeBuddy's own `chunkPublicationKey` window (5e3 ms in the agent
// bundle, checked as `em - eA < eQ`). Upstream needs the window because the
// identity is not unique within a message: a retry re-sends the SAME update
// object milliseconds later, while a model legitimately emitting the same text
// later in the turn produces an indistinguishable identity. Without the window
// ClawBench deleted real content: measured 2026-10-25, 16 assistant messages
// each lost exactly one `|---|---` chunk, every one of them emitted more than 5s
// after the identical delimiter row of an earlier table in the same message.
const chunkDedupWindow = 5 * time.Second

// chunkDedupNow is the clock behind chunkDedupWindow, injectable so tests can
// advance time instead of sleeping. Production never reassigns it.
var chunkDedupNow = time.Now

// maxDupChunkKeys bounds the per-turn identity set. A long team turn produces a
// few thousand chunks; the cap is generous enough to cover one. On overflow the
// expired entries are pruned first (they can no longer suppress anything), and
// only a set that is still full is cleared wholesale — which can let an old
// duplicate through, never drop a real chunk.
const maxDupChunkKeys = 16384

// chunkDedupEnabled reports whether duplicate-chunk suppression applies to this
// backend. See the file comment for why it is CodeBuddy-only.
func chunkDedupEnabled(backendID string) bool {
	return backendID == "codebuddy"
}

// chunkIdentity builds the de-duplication key for one model-output chunk.
// Returns "" when the frame lacks the identity pair, in which case the caller
// must NOT drop it.
func chunkIdentity(sessionID string, meta map[string]any, variant, text string) string {
	if len(meta) == 0 {
		return ""
	}
	msgID := metaString(meta[metaKeyCodeBuddyMessageID])
	ts := metaString(meta[metaKeyCodeBuddyTimestamp])
	if msgID == "" || ts == "" {
		return ""
	}
	return sessionID + "\x00" + variant + "\x00" + msgID + "\x00" + ts + "\x00" +
		metaString(meta[metaKeyCodeBuddyMemberEvent]) + "\x00" + text
}

// isDuplicateChunk reports whether this chunk was already delivered inside
// chunkDedupWindow, recording it otherwise.
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
	if seenAt, dup := c.seenChunkKeys[identity]; dup {
		// Only a retry of the SAME send is a duplicate. Once the window has
		// passed, the same identity means the model legitimately emitted this
		// text again (a second table's `|---|---|`, a repeated token run) and it
		// must be delivered.
		if now.Sub(seenAt) < chunkDedupWindow {
			return true
		}
	}
	if len(c.seenChunkKeys) >= maxDupChunkKeys {
		c.pruneExpiredChunkKeys(now)
	}
	c.seenChunkKeys[identity] = now
	return false
}

// pruneExpiredChunkKeys drops identities that can no longer suppress anything.
// The caller must hold stateMu. Expired entries are dead weight, so pruning them
// keeps the cap from being reached by a long turn; a set that is still full
// afterwards is cleared wholesale, which can only let an old duplicate through
// (never drop a real chunk).
func (c *ACPConn) pruneExpiredChunkKeys(now time.Time) {
	for id, seenAt := range c.seenChunkKeys {
		if now.Sub(seenAt) >= chunkDedupWindow {
			delete(c.seenChunkKeys, id)
		}
	}
	if len(c.seenChunkKeys) >= maxDupChunkKeys {
		c.seenChunkKeys = make(map[string]time.Time, 256)
	}
}

// resetChunkDedup clears the per-turn identity set. Called when a prompt starts:
// the set describes one turn's deliveries, and carrying it across turns would
// only grow it for no benefit (identities include the session id and message id,
// so cross-turn collisions are not a correctness concern either way).
func (c *ACPConn) resetChunkDedup() {
	c.stateMu.Lock()
	c.seenChunkKeys = nil
	c.stateMu.Unlock()
}
