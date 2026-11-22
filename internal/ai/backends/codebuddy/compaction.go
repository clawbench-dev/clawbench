package codebuddy

import "clawbench/internal/ai"

// Compaction signals.
//
// CodeBuddy reports a context compaction through private _meta keys rather than
// a protocol event. Verified on the wire:
//
//	isCompactInternal       true on EVERY update emitted during the compaction
//	compactType             restates the purpose ("auto" / manual trigger)
//	compact-cancelled       the compaction was aborted, history restored
//	compact-limit-reached   it gave up at the token limit, history restored
//
// The two negative keys matter: an aborted compaction leaves the original
// history in place, so the session must NOT be flagged as compacted — otherwise
// the next turn re-injects the system prompt for nothing.

const (
	// metaKeyIsCompactInternal marks every session update emitted while
	// CodeBuddy is compacting the session context.
	metaKeyIsCompactInternal = "codebuddy.ai/isCompactInternal"
	// metaKeyCompactType carries the compaction purpose.
	metaKeyCompactType = "codebuddy.ai/compactType"
	// metaKeyCompactCancelled marks a compaction that was aborted.
	metaKeyCompactCancelled = "codebuddy.ai/compact-cancelled"
	// metaKeyCompactLimitReached marks a compaction that gave up at the token
	// limit.
	metaKeyCompactLimitReached = "codebuddy.ai/compact-limit-reached"
)

// IsCompactionMeta reports whether a _meta payload marks the session as being
// compacted right now.
//
// The boolean flag is authoritative; compactType is accepted as an alternative
// carrier because the broadcast service attaches both, and older/newer builds
// may omit one.
func IsCompactionMeta(m map[string]any) bool {
	if len(m) == 0 {
		return false
	}
	if v, ok := m[metaKeyIsCompactInternal]; ok && ai.MetaBool(v) {
		return true
	}
	return ai.MetaString(m[metaKeyCompactType]) != ""
}

// IsCompactionCancelledMeta reports whether a _meta payload announces a
// compaction that did NOT complete (cancelled by the user, or the max-token
// limit was reached). The agent restores the original history in both cases, so
// the session must not be flagged as compacted.
func IsCompactionCancelledMeta(m map[string]any) bool {
	if len(m) == 0 {
		return false
	}
	_, cancelled := m[metaKeyCompactCancelled]
	_, limitReached := m[metaKeyCompactLimitReached]
	return cancelled || limitReached
}

// SteerBoundaryID returns the client user-message id CodeBuddy echoes on a
// `user_message_chunk` for a mid-turn injection, or "" for any other frame.
//
// Verified on the wire (docs/dev/codebuddy_acp_extensions.md §9.6): the echo
// frame's `_meta["codebuddy.ai/messageId"]` equals the clientUserMessageId we
// sent, and it is interleaved between the reply's pre- and post-injection
// content. Whether that id is one THIS host issued is decided by the shared
// loop (conn.claimPendingSteerID); reading the key is this backend's business.
func SteerBoundaryID(meta map[string]any) string {
	return ai.MetaString(meta["codebuddy.ai/messageId"])
}
