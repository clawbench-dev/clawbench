package ai

import (
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

// parseCodeBuddyACPToolCall extracts a ToolCall from a CodeBuddy ACP ToolCall event.
// CodeBuddy ACP specifics:
//   - Tool name from _meta["codebuddy.ai/toolName"] (preferred), fallback to extractToolName
//   - rawInput is streamed incrementally (each tool_call_update contains partial content)
//   - Field names may be mixed camelCase/snake_case
func parseCodeBuddyACPToolCall(tc acp.SessionUpdateToolCall) *ToolCall {
	// Extract tool name: prefer _meta flat key
	name := extractMetaToolNameFlat(tc.Meta)
	if name == "" {
		name = extractToolName(tc.Title, tc.Kind, "codebuddy", string(tc.ToolCallId))
	}

	tool := &ToolCall{
		Name: name,
		ID:   string(tc.ToolCallId),
		Done: false,
	}

	// CodeBuddy may use mixed camelCase/snake_case
	resolveACPToolInput(tc, tool, getRemaps("codebuddy_acp"))
	return tool
}

// parseCodeBuddyACPToolCallUpdate extracts a ToolCall from a CodeBuddy ACP ToolCallUpdate.
func parseCodeBuddyACPToolCallUpdate(tcu acp.SessionToolCallUpdate) *ToolCall {
	tool := &ToolCall{
		ID: string(tcu.ToolCallId),
	}

	mapToolCallStatus(tcu.Status, tool)

	// Extract tool name: prefer _meta flat key
	name := extractMetaToolNameFlat(tcu.Meta)
	if name != "" {
		tool.Name = name
	} else if !tool.Done && tcu.Title != nil && *tcu.Title != "" {
		if tool.Name == "" || tool.Name == strings.ToLower(tool.Name) {
			kind := acp.ToolKindExecute
			if tcu.Kind != nil {
				kind = *tcu.Kind
			}
			tool.Name = extractToolName(*tcu.Title, kind, "codebuddy", string(tcu.ToolCallId))
		}
	}

	mapToolCallInput(tcu, tool, "codebuddy")
	if tool.Done {
		mapToolCallOutput(tcu, tool)
	}

	return tool
}
