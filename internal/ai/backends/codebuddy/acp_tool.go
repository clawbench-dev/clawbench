package codebuddy

import (
	"strings"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/ai"
)

// ParseACPToolCall extracts a ToolCall from a CodeBuddy ACP ToolCall event.
// CodeBuddy ACP specifics:
//   - Tool name from _meta["codebuddy.ai/toolName"] (preferred), fallback to extractToolName
//   - rawInput is streamed incrementally (each tool_call_update contains partial content)
//   - Field names may be mixed camelCase/snake_case
func ParseACPToolCall(tc acp.SessionUpdateToolCall) *ai.ToolCall {
	// Extract tool name: prefer _meta flat key
	name := ai.ExtractMetaToolNameFlat(ai.MetaAny(tc.Meta))
	if name == "" {
		name = ai.ExtractToolName(tc.Title, tc.Kind, "codebuddy", string(tc.ToolCallId))
	}

	tool := &ai.ToolCall{
		Name: name,
		ID:   string(tc.ToolCallId),
		Done: false,
	}

	// CodeBuddy may use mixed camelCase/snake_case
	ai.ResolveACPToolInput(tc, tool, ai.GetRemaps("codebuddy_acp"))
	return tool
}

// ParseACPToolCallUpdate extracts a ToolCall from a CodeBuddy ACP ToolCallUpdate.
func ParseACPToolCallUpdate(tcu acp.SessionToolCallUpdate) *ai.ToolCall {
	tool := &ai.ToolCall{
		ID: string(tcu.ToolCallId),
	}

	ai.MapToolCallStatus(tcu.Status.Ptr(), tool)

	// Extract tool name: prefer _meta flat key
	name := ai.ExtractMetaToolNameFlat(ai.PatchMetaAny(tcu.Meta))
	if name != "" {
		tool.Name = name
	} else if title, ok := tcu.Title.Value(); ok && title != "" && !tool.Done {
		if tool.Name == "" || tool.Name == strings.ToLower(tool.Name) {
			kind := acp.ToolKindExecute
			if k, ok := tcu.Kind.Value(); ok {
				kind = k
			}
			tool.Name = ai.ExtractToolName(title, kind, "codebuddy", string(tcu.ToolCallId))
		}
	}

	ai.MapToolCallInput(tcu, tool, "codebuddy")
	if tool.Done {
		ai.MapToolCallOutput(tcu, tool)
	}

	return tool
}
