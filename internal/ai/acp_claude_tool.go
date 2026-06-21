package ai

import (
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

// parseClaudeACPToolCall extracts a ToolCall from a Claude/Qoder ACP ToolCall event.
// Claude ACP specifics:
//   - Tool name from _meta.claudeCode.toolName (preferred), fallback to extractToolName
//   - rawInput already uses snake_case field names (file_path, old_string, new_string)
//   - Content may include diff-type blocks
//   - rawOutput is plain text string
//   - _meta.claudeCode.toolResponse contains stdout/stderr/interrupted on completed
func parseClaudeACPToolCall(tc acp.SessionUpdateToolCall) *ToolCall {
	// Extract tool name: prefer _meta.claudeCode.toolName
	name := extractMetaToolName(tc.Meta)
	if name == "" {
		name = extractToolName(tc.Title, tc.Kind, "claude", string(tc.ToolCallId))
	}

	tool := &ToolCall{
		Name: name,
		ID:   string(tc.ToolCallId),
		Done: false,
	}

	// Claude ACP rawInput already uses snake_case, defaultMappings sufficient
	resolveACPToolInput(tc, tool, getRemaps("claude_acp"))
	return tool
}

// parseClaudeACPToolCallUpdate extracts a ToolCall from a Claude/Qoder ACP ToolCallUpdate.
func parseClaudeACPToolCallUpdate(tcu acp.SessionToolCallUpdate) *ToolCall {
	tool := &ToolCall{
		ID: string(tcu.ToolCallId),
	}

	mapToolCallStatus(tcu.Status, tool)

	// Extract tool name: prefer _meta.claudeCode.toolName
	name := extractMetaToolName(tcu.Meta)
	if name != "" {
		tool.Name = name
	} else if !tool.Done && tcu.Title != nil && *tcu.Title != "" {
		// Don't overwrite an already-correct canonical name
		if tool.Name == "" || tool.Name == strings.ToLower(tool.Name) {
			kind := acp.ToolKindExecute
			if tcu.Kind != nil {
				kind = *tcu.Kind
			}
			tool.Name = extractToolName(*tcu.Title, kind, "claude", string(tcu.ToolCallId))
		}
	}

	mapToolCallInput(tcu, tool, "claude")
	if tool.Done {
		mapToolCallOutput(tcu, tool)
	}

	return tool
}
