package ai

import (
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

// ---------------------------------------------------------------------------
// Codex ACP tool call parsing
// ---------------------------------------------------------------------------
//
// Codex (`npx @agentclientprotocol/codex-acp`) reports multi-agent collaboration
// over ACP as a set of control tool calls that carry no ordinary tool name:
//
//   - Sub-agent lifecycle: titles "Start subagent <name>" / "Interact with
//     subagent <name>" / "Interrupt subagent <name>" / "Complete subagent <name>"
//     with `kind=other` and `_meta.codex.subagent = {activity, path, threadId}`.
//     The toolCallId is `call_00_*` (start/interact/interrupt) or
//     `subagent-completed-<threadId>` (complete). These map to the canonical
//     "Agent" name so the frontend renders the Bot icon + sub-agent category;
//     `rawInput.agentThreadId` (kept as `agent_thread_id`) carries the child
//     session id that is the correlation anchor for inlining child output.
//   - Collaboration wait: title "wait" with `_meta.codex.collaboration =
//     {tool:"wait", senderThreadId, receiverThreadIds}`. The parent blocks until
//     a child's mailbox has activity; the tool carries no child reference and is
//     deliberately kept as "wait" (it is NOT a Skill).
//
// Without the `_meta` disambiguation these fall into the generic branch's
// kind-to-canonical fallback, which maps every `kind=other` tool to "Skill" —
// mislabeling every sub-agent lifecycle call (observed in production: a codex
// turn showed 9 bogus "Skill" pills for Start/Complete/Interact subagent).
// acp_tool_names.go has since removed the ToolKindOther→Skill catch-all, but
// codex still needs this branch: subagent frames must canonicalize to "Agent"
// (frontend Bot icon + sub-agent category + threadId correlation), which title
// passthrough alone would not produce, and `wait` must be preserved as-is.

// codexMetaNamespace is the per-agent _meta namespace Codex reports tool
// identity under (codex-acp bridge, createSubAgentActivityUpdate /
// createCollabAgentToolCallMeta).
const codexMetaNamespace = "codex"

// extractCodexSubagentMeta returns the activity kind from a Codex
// `_meta.codex.subagent` payload. Empty activity means the meta is absent.
func extractCodexSubagentMeta(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	ns, ok := meta[codexMetaNamespace].(map[string]any)
	if !ok {
		return ""
	}
	sub, ok := ns["subagent"].(map[string]any)
	if !ok {
		return ""
	}
	activity, _ := sub["activity"].(string)
	return activity
}

// extractCodexCollaborationTool returns the collaboration tool name from a Codex
// `_meta.codex.collaboration` payload (e.g. "wait"). Empty when absent.
func extractCodexCollaborationTool(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	ns, ok := meta[codexMetaNamespace].(map[string]any)
	if !ok {
		return ""
	}
	collab, ok := ns["collaboration"].(map[string]any)
	if !ok {
		return ""
	}
	tool, _ := collab["tool"].(string)
	return tool
}

// isCodexSubagentControlTitle reports whether a Codex tool title is a sub-agent
// lifecycle frame ("Start subagent X" etc.). The `_meta.codex.subagent` payload
// is the authoritative signal; this is a fallback for updates that omit meta.
func isCodexSubagentControlTitle(title string) bool {
	lower := strings.ToLower(title)
	for _, prefix := range []string{
		"start subagent ", "interact with subagent ", "interrupt subagent ", "complete subagent ",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return strings.HasPrefix(lower, "subagent-completed-")
}

// parseCodexACPToolCall extracts a ToolCall from a Codex ACP ToolCall event.
// Codex-specific rules:
//   - `_meta.codex.subagent` present (or sub-agent control title) → "Agent" and
//     the rawInput is passed through unchanged so agent_thread_id survives.
//   - `_meta.codex.collaboration` present (title "wait") → name stays "wait".
//   - Otherwise fall back to the generic path (kind→canonical for read/execute).
func parseCodexACPToolCall(tc acp.SessionUpdateToolCall) *ToolCall {
	if extractCodexSubagentMeta(tc.Meta) != "" || isCodexSubagentControlTitle(tc.Title) {
		tool := &ToolCall{
			Name: "Agent",
			ID:   string(tc.ToolCallId),
			Done: false,
		}
		resolveACPToolInput(tc, tool, acpRemapsForBackend("codex"))
		return tool
	}
	if tool := extractCodexCollaborationTool(tc.Meta); tool != "" {
		return &ToolCall{
			Name: tool,
			ID:   string(tc.ToolCallId),
			Done: false,
		}
	}
	return parseGenericACPToolCall(tc, "codex")
}

// parseCodexACPToolCallUpdate extracts a ToolCall from a Codex ACP ToolCallUpdate.
// Name resolution mirrors parseCodexACPToolCall; meta sub-agent control updates
// and the title fallback map to "Agent".
func parseCodexACPToolCallUpdate(tcu acp.SessionToolCallUpdate) *ToolCall {
	tool := &ToolCall{ID: string(tcu.ToolCallId)}

	mapToolCallStatus(tcu.Status, tool)

	// Prefer meta-disambiguated names. When the completed "Complete subagent"
	// update carries no _meta (some codex-acp versions only attach meta on the
	// tool_call frame), fall back to the title prefix.
	title := ""
	if tcu.Title != nil {
		title = *tcu.Title
	}
	switch {
	case extractCodexSubagentMeta(tcu.Meta) != "" || isCodexSubagentControlTitle(title):
		tool.Name = "Agent"
	case extractCodexCollaborationTool(tcu.Meta) != "":
		tool.Name = "wait"
	case !tool.Done && title != "":
		kind := acp.ToolKindExecute
		if tcu.Kind != nil {
			kind = *tcu.Kind
		}
		tool.Name = extractToolName(title, kind, "codex", string(tcu.ToolCallId))
	}

	mapToolCallInput(tcu, tool, "codex")
	if tool.Done {
		mapToolCallOutput(tcu, tool)
	}
	return tool
}
