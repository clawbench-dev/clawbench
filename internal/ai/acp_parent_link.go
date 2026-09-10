package ai

// ---------------------------------------------------------------------------
// Sub-agent parent linkage
// ---------------------------------------------------------------------------
//
// Several ACP agents tag content produced *by a sub-agent* with a `_meta` key
// pointing at the parent tool call that spawned it. ClawBench uses this to group
// a sub-agent's thinking/text/tool calls under the parent Agent card in the UI.
//
// Known encodings (verified against real wire captures):
//   - codebuddy: flat key `codebuddy.ai/parentToolCallId` on every child
//     agent_thought_chunk / agent_message_chunk / tool_call(_update). The parent
//     agent's own content carries no such key. (session cfe979e6: 963 child
//     thinking + 3484 child text + 111 child tools all tagged; parent content
//     untagged.)
//   - claude/qoder: nested `_meta.claudeCode.parentToolUseId` (stamped by
//     claude-agent-acp's toAcpNotifications). The bridge currently filters most
//     sub-agent text/thinking out of the parent wire, so this mostly surfaces on
//     child tool calls / images, but the field is the generic analog.
//
// The extracted value is the parent tool-call id (e.g. "call_00_..."), or "" when
// the event is not sub-agent content. An empty value means "top-level".

const (
	// metaKeyCodeBuddyParentToolCallID is the flat codebuddy key stamped on
	// sub-agent content.
	metaKeyCodeBuddyParentToolCallID = "codebuddy.ai/parentToolCallId"
	// metaKeyClaudeParentToolUseID is the nested claudeCode key stamped on
	// sub-agent content.
	metaKeyClaudeParentToolUseID = "parentToolUseId"
)

// extractParentToolCallID returns the id of the parent tool call that spawned
// the sub-agent which produced this event, or "" for top-level (non-sub-agent)
// content. backendID selects the per-agent meta encoding.
func extractParentToolCallID(backendID string, meta map[string]any) string {
	if len(meta) == 0 {
		return ""
	}
	switch backendID {
	case "codebuddy":
		return metaString(meta[metaKeyCodeBuddyParentToolCallID])
	case "claude", "qoder":
		if ns, ok := meta["claudeCode"].(map[string]any); ok {
			return metaString(ns[metaKeyClaudeParentToolUseID])
		}
		return ""
	default:
		// Unknown/other backends: best-effort — accept either encoding so a new
		// agent reusing one of the conventions still groups correctly.
		if v := metaString(meta[metaKeyCodeBuddyParentToolCallID]); v != "" {
			return v
		}
		if ns, ok := meta["claudeCode"].(map[string]any); ok {
			return metaString(ns[metaKeyClaudeParentToolUseID])
		}
		return ""
	}
}
