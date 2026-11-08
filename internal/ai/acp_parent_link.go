package ai

import acp "github.com/coder/acp-go-sdk"

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
	// metaKeyOpenCodeChildSession marks content projected from an OpenCode v2
	// sub-agent (child) session into the PARENT session's update stream. Unlike
	// the keys above it carries no parent tool-call id, so it cannot be used for
	// grouping — it only identifies the frame as child content.
	metaKeyOpenCodeChildSession = "opencode/child-session"
	// metaKeyClawBenchParentToolCallID is ClawBench's own attribution key. The
	// client stamps it on OpenCode child frames after resolving the spawning
	// `subagent` tool call (by child title or session id), so every downstream
	// mapper picks the parent up through extractParentToolCallID.
	metaKeyClawBenchParentToolCallID = "clawbench/parentToolCallId"
)

// stampParentToolCallID writes ClawBench's attribution key into the update's
// _meta. No-op when the update carries no meta (child frames always do).
func stampParentToolCallID(update *acp.SessionUpdate, parentToolCallID string) {
	if update == nil || parentToolCallID == "" {
		return
	}
	meta := acpUpdateMeta(*update)
	if meta == nil {
		return
	}
	meta[metaKeyClawBenchParentToolCallID] = parentToolCallID
}

// isOpenCodeChildSessionUpdate reports whether an ACP session update was
// projected from an OpenCode v2 sub-agent (child) session into the parent
// session's stream.
//
// OpenCode v2 emits child-session content as ordinary session/update frames on
// the parent session unless the client opts into its
// `opencode/child-session-updates` extension channel. Every projected frame is
// tagged with `_meta["opencode/child-session"]` and carries no parent tool-call
// link, so without this check the sub-agent's text/thinking/tool calls render
// as top-level content in the main timeline, after the spawning tool already
// reported its result (issue #639).
func isOpenCodeChildSessionUpdate(update acp.SessionUpdate) bool {
	meta := acpUpdateMeta(update)
	if len(meta) == 0 {
		return false
	}
	_, ok := meta[metaKeyOpenCodeChildSession]
	return ok
}

// extractParentToolCallID returns the id of the parent tool call that spawned
// the sub-agent which produced this event, or "" for top-level (non-sub-agent)
// content. backendID selects the per-agent meta encoding.
func extractParentToolCallID(backendID string, meta map[string]any) string {
	if len(meta) == 0 {
		return ""
	}
	// ClawBench's own attribution wins: the client resolved it from the
	// spawning `subagent` tool call (see acp_opencode_children.go).
	if v := metaString(meta[metaKeyClawBenchParentToolCallID]); v != "" {
		return v
	}
	switch backendID {
	case "codebuddy":
		return metaString(meta[metaKeyCodeBuddyParentToolCallID])
	case "claude", "qoder", "qodercn":
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
