package ai

import (
	"encoding/json"
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

// parseKimiACPToolCall extracts a ToolCall from a Kimi ACP ToolCall event.
// Kimi ACP specifics:
//   - No rawInput / rawOutput / _meta at all
//   - Tool name inferred from toolCallId prefix (e.g., "read_file-", "glob-", "list_directory-")
//   - Input inferred from locations + title
//   - Output inferred from content blocks
func parseKimiACPToolCall(tc acp.SessionUpdateToolCall) *ToolCall {
	name := extractToolName(tc.Title, tc.Kind, "kimi", string(tc.ToolCallId))

	tool := &ToolCall{
		Name: name,
		ID:   string(tc.ToolCallId),
		Done: false,
	}

	// Kimi ACP has no rawInput — extract from locations and title
	if input := extractInputFromLocationsAndTitle(tc.Locations, tc.Title, tc.Kind, string(tc.ToolCallId)); input != nil {
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

// parseKimiACPToolCallUpdate extracts a ToolCall from a Kimi ACP ToolCallUpdate.
func parseKimiACPToolCallUpdate(tcu acp.SessionToolCallUpdate) *ToolCall {
	tool := &ToolCall{
		ID: string(tcu.ToolCallId),
	}

	mapToolCallStatus(tcu.Status, tool)

	// Name from toolCallId prefix or title
	if !tool.Done {
		title := ""
		if tcu.Title != nil {
			title = *tcu.Title
		}
		kind := acp.ToolKindOther
		if tcu.Kind != nil {
			kind = *tcu.Kind
		}
		if title != "" {
			if tool.Name == "" || tool.Name == strings.ToLower(tool.Name) {
				tool.Name = extractToolName(title, kind, "kimi", string(tcu.ToolCallId))
			}
		}
	}

	// Kimi ACP has no rawInput in updates — extract from locations
	mapToolCallInputFromLocations(tcu, tool)
	if tool.Done {
		mapToolCallOutput(tcu, tool)
	}

	return tool
}
