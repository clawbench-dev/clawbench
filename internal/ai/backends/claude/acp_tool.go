package claude

import (
	"strings"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/ai"
)

// ParseACPToolCall extracts a ToolCall from a Claude/Qoder ACP ToolCall event.
// Claude ACP specifics:
//   - Tool name from _meta.claudeCode.toolName (preferred), fallback to ExtractToolName
//   - rawInput already uses snake_case field names (file_path, old_string, new_string)
//   - Content may include diff-type blocks
//   - rawOutput is plain text string
//   - _meta.claudeCode.toolResponse contains stdout/stderr/interrupted on completed
func ParseACPToolCall(tc acp.SessionUpdateToolCall) *ai.ToolCall {
	// Extract tool name: prefer _meta.claudeCode.toolName
	name := ai.ExtractMetaToolName(tc.Meta)
	if name == "" {
		name = ai.ExtractToolName(tc.Title, tc.Kind, "claude", string(tc.ToolCallId))
	}

	tool := &ai.ToolCall{
		Name: name,
		ID:   string(tc.ToolCallId),
		Done: false,
	}

	// Claude ACP rawInput already uses snake_case, defaultMappings sufficient
	ai.ResolveACPToolInput(tc, tool, ai.GetRemaps("claude_acp"))
	return tool
}

// ParseACPToolCallUpdate extracts a ToolCall from a Claude/Qoder ACP ToolCallUpdate.
func ParseACPToolCallUpdate(tcu acp.SessionToolCallUpdate) *ai.ToolCall {
	tool := &ai.ToolCall{
		ID: string(tcu.ToolCallId),
	}

	ai.MapToolCallStatus(tcu.Status, tool)

	// Extract tool name: prefer _meta.claudeCode.toolName
	name := ai.ExtractMetaToolName(tcu.Meta)
	if name != "" {
		tool.Name = name
	} else if !tool.Done && tcu.Title != nil && *tcu.Title != "" {
		// Don't overwrite an already-correct canonical name
		if tool.Name == "" || tool.Name == strings.ToLower(tool.Name) {
			kind := acp.ToolKindExecute
			if tcu.Kind != nil {
				kind = *tcu.Kind
			}
			tool.Name = ai.ExtractToolName(*tcu.Title, kind, "claude", string(tcu.ToolCallId))
		}
	}

	ai.MapToolCallInput(tcu, tool, "claude")
	if tool.Done {
		ai.MapToolCallOutput(tcu, tool)
	}

	return tool
}
