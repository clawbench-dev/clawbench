package codebuddy

import "clawbench/internal/ai"

// isReplayedTurnMeta reports whether a notification's _meta belongs to the
// previous, already-completed turn rather than the current one.
//
// CodeBuddy re-emits the tail of the previous turn when an ACP session is
// reused across prompts: both agent_message_chunk (stale text) and
// usage_update (stale trace identity) have been observed carrying the previous
// turn's requestId. Anything derived from that _meta would otherwise be
// attributed to the current turn.
//
// The comparison is against the SET of requestIds the previous turn was seen to
// use, not against one scalar. A CodeBuddy turn is not guaranteed a single
// requestId — every model generation / message group inside the turn gets its
// own — and the replay carries the turn's LAST id while the accumulator keeps
// the FIRST (first-wins merge). Comparing against a single value therefore
// missed the replay entirely and let the stale text through; production
// incident fffc1395 showed the new reply prefixed by the previous message's
// full conclusion.
//
// Registered as ai.ACPBehavior.IsReplayedTurnMeta. It lives here rather than in
// internal/ai because "which backends can be compared across turns" is this
// backend's own fact: other backends' requestIds are not comparable, and their
// genuine chunks could share an id with the baseline.
func isReplayedTurnMeta(conn *ai.ACPConn, meta map[string]any) bool {
	if conn == nil || len(meta) == 0 {
		return false
	}
	rid, _ := meta[ai.MetaKeyCodeBuddyRequestID].(string)
	if rid == "" {
		return false
	}
	return ai.ACPConnIsCompletedTurnRequestID(conn, rid)
}
