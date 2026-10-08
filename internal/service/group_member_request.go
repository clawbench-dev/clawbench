package service

import "clawbench/internal/ai"

// group_member_request.go fixes the resume signals for a group member's turn.
//
// A member session row carries NO chat_history rows (all group messages live on
// the group timeline), so every chat_history-derived signal BuildChatRequest
// computes is wrong for a member:
//   - SessionHasAssistant  -> false => Resume=false
//   - GetChatMessageCount  -> 0     => HasConversationHistory=false
//
// The member's real memory signal is its own external_session_id (written by
// captureExternalSessionID on the member row, connection semantics).
//
// CRITICAL: req.SessionID is the ACP connection-pool key. For ACP it MUST stay
// the member row id — overwriting it with the ACP session id would create a
// second, wrong pool entry, lose auto-approve, and break resume. Only CLI
// backends need SessionID switched to the external id (that value is what gets
// passed to --resume).
func applyMemberResumeOverrides(req *ai.ChatRequest, externalSessionID string, isACP bool) {
	hasMemory := externalSessionID != ""
	req.Resume = hasMemory
	req.HasConversationHistory = hasMemory
	if hasMemory {
		// >0 signals "has history" to the periodic system-prompt re-injection.
		req.AssistantMessageCount = 1
	}
	if !isACP && hasMemory {
		req.SessionID = externalSessionID
	}
	// ACP: leave req.SessionID as the member row id (pool key).
}
