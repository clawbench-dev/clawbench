package ai

import (
	acp "github.com/coder/acp-go-sdk"
)

// parseOpenCodeACPToolCall extracts a ToolCall from an OpenCode ACP ToolCall event.
// OpenCode ACP specifics:
//   - No _meta
//   - rawInput uses camelCase (filePath → file_path needs normalization)
//   - rawOutput is a nested object {metadata: {...}, output: "..."}
//   - Tool name from title + kind
func parseOpenCodeACPToolCall(tc acp.SessionUpdateToolCall) *ToolCall {
	tool := &ToolCall{
		Name: extractToolName(tc.Title, tc.Kind, "opencode", string(tc.ToolCallId)),
		ID:   string(tc.ToolCallId),
		Done: false,
	}

	// OpenCode uses camelCase, needs remapping
	resolveACPToolInput(tc, tool, getRemaps("opencode_acp"))
	return tool
}

// parseOpenCodeACPToolCallUpdate extracts a ToolCall from an OpenCode ACP ToolCallUpdate.
// OpenCode has no _meta, so tool name extraction uses mapToolCallName (title-based fallback only).
func parseOpenCodeACPToolCallUpdate(tcu acp.SessionToolCallUpdate) *ToolCall {
	tool := &ToolCall{
		ID: string(tcu.ToolCallId),
	}

	mapToolCallStatus(tcu.Status, tool)
	// OpenCode has no _meta — mapToolCallName uses title-based extraction only
	mapToolCallName(tcu, tool, "opencode")
	mapToolCallInput(tcu, tool, "opencode")
	if tool.Done {
		mapToolCallOutput(tcu, tool)
	}

	return tool
}
