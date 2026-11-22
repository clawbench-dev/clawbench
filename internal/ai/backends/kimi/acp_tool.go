package kimi

import (
	"encoding/json"
	"strings"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/ai"
)

// parseACPToolCall extracts a ToolCall from a Kimi ACP ToolCall event.
// Kimi ACP specifics:
//   - No rawInput / rawOutput / _meta at all
//   - Tool name inferred from toolCallId prefix (e.g., "read_file-", "glob-", "list_directory-")
//   - Input inferred from locations + title
//   - Output inferred from content blocks
func parseACPToolCall(tc acp.SessionUpdateToolCall) *ai.ToolCall {
	name := ai.ExtractToolName(tc.Title, tc.Kind, "kimi", string(tc.ToolCallId))

	tool := &ai.ToolCall{
		Name: name,
		ID:   string(tc.ToolCallId),
		Done: false,
	}

	// Kimi ACP has no rawInput — extract from locations and title
	if input := ai.ExtractInputFromLocationsAndTitle(tc.Locations, tc.Title, tc.Kind, string(tc.ToolCallId)); input != nil {
		if inputBytes, err := json.Marshal(input); err == nil {
			tool.Input = string(inputBytes)
		}
	}

	// Fallback: for execute-kind tools, use title as command
	if tool.Input == "" && tc.Kind == acp.ToolKindExecute && tc.Title != "" {
		input := map[string]any{"command": tc.Title}
		if inputBytes, err := json.Marshal(input); err == nil {
			tool.Input = string(inputBytes)
		}
	}

	return tool
}

// parseACPToolCallUpdate extracts a ToolCall from a Kimi ACP ToolCallUpdate.
func parseACPToolCallUpdate(tcu acp.SessionToolCallUpdate) *ai.ToolCall {
	tool := &ai.ToolCall{
		ID: string(tcu.ToolCallId),
	}

	ai.MapToolCallStatus(tcu.Status.Ptr(), tool)

	// Name from toolCallId prefix or title
	if !tool.Done {
		title, _ := tcu.Title.Value()
		kind, ok := tcu.Kind.Value()
		if !ok {
			kind = acp.ToolKindOther
		}
		if title != "" {
			if tool.Name == "" || tool.Name == strings.ToLower(tool.Name) {
				tool.Name = ai.ExtractToolName(title, kind, "kimi", string(tcu.ToolCallId))
			}
		}
	}

	// Kimi ACP has no rawInput in updates — extract from locations
	ai.MapToolCallInputFromLocations(tcu, tool)
	if tool.Done {
		ai.MapToolCallOutput(tcu, tool)
	}

	return tool
}
