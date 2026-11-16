package opencode

import (
	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/ai"
)

// ParseACPToolCall extracts a ToolCall from an OpenCode ACP ToolCall event.
// OpenCode ACP specifics:
//   - No _meta
//   - rawInput uses camelCase (filePath → file_path needs normalization)
//   - rawOutput is a nested object {metadata: {...}, output: "..."}
//   - Tool name from title + kind
func ParseACPToolCall(tc acp.SessionUpdateToolCall) *ai.ToolCall {
	tool := &ai.ToolCall{
		Name: ai.ExtractToolName(tc.Title, tc.Kind, "opencode", string(tc.ToolCallId)),
		ID:   string(tc.ToolCallId),
		Done: false,
	}

	// OpenCode uses camelCase, needs remapping
	ai.ResolveACPToolInput(tc, tool, ai.GetRemaps("opencode_acp"))
	return tool
}

// ParseACPToolCallUpdate extracts a ToolCall from an OpenCode ACP ToolCallUpdate.
// OpenCode has no _meta, so tool name extraction uses mapToolCallName (title-based fallback only).
func ParseACPToolCallUpdate(tcu acp.SessionToolCallUpdate) *ai.ToolCall {
	tool := &ai.ToolCall{
		ID: string(tcu.ToolCallId),
	}

	ai.MapToolCallStatus(tcu.Status, tool)
	// OpenCode has no _meta — mapToolCallName uses title-based extraction only
	ai.MapToolCallName(tcu, tool, "opencode")
	ai.MapToolCallInput(tcu, tool, "opencode")
	if tool.Done {
		ai.MapToolCallOutput(tcu, tool)
	}

	return tool
}

// IsChildSessionUpdate reports whether a frame was projected from an OpenCode
// v2 sub-agent (child) session into the parent's stream. Such frames carry no
// parent tool-call link, so the host drops them unless one was resolved.
func IsChildSessionUpdate(meta map[string]any) bool {
	_, ok := meta["opencode/child-session"]
	return ok
}
