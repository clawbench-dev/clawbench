package ai

import (
	"encoding/json"
	"strings"
)

// parsePiToolCallEnd extracts a tool_use ToolCall from a Pi toolcall_end event.
// Pi toolcall_end provides the complete tool arguments (Done=true).
// Tool name is normalized via normalizeToolName; input field names are
// remapped using the provided remap table. The edit tool uses normalizePiEditInput
// for its nested edits array structure.
func parsePiToolCallEnd(evt *PiAssistantMessageEvent, remaps map[string]string) *ToolCall {
	if evt.ToolCall == nil {
		return nil
	}

	tc := evt.ToolCall
	normalizedInput := normalizePiToolInput(tc.Name, tc.Arguments, remaps)

	return &ToolCall{
		Name:  normalizeToolName(tc.Name),
		ID:    tc.ID,
		Input: normalizedInput,
		Done:  true,
	}
}

// parsePiToolExecutionEnd extracts a tool_result ToolCall from a Pi tool_execution_end event.
// Returns nil if toolCallId is empty. Output text is extracted from result.content[].text
// (joined with newline for multiple items) and truncated via truncateToolOutput.
func parsePiToolExecutionEnd(msg *PiStreamMessage) *ToolCall {
	if msg.ToolCallID == "" {
		return nil
	}

	var outputText string
	if msg.Result != nil {
		var parts []string
		for _, c := range msg.Result.Content {
			if c.Type == "text" && c.Text != "" {
				parts = append(parts, c.Text)
			}
		}
		if len(parts) > 0 {
			outputText = strings.Join(parts, "\n")
		}
	}

	status := "success"
	if msg.IsError {
		status = "error"
	}

	return &ToolCall{
		ID:     msg.ToolCallID,
		Output: truncateToolOutput(outputText),
		Status: status,
	}
}

// normalizePiToolInput normalizes tool input for Pi tool calls.
// For the edit tool, it uses normalizePiEditInput for nested edits array handling.
// For read/write tools, it adds path→file_path remapping on top of the base remaps.
// For all other tools, it uses normalizeToolInput with the provided remap table.
func normalizePiToolInput(toolName string, rawInput json.RawMessage, baseRemaps map[string]string) string {
	if len(rawInput) == 0 {
		return "{}"
	}

	// Copy base remaps so we don't mutate the caller's map
	remaps := map[string]string{}
	for k, v := range baseRemaps {
		remaps[k] = v
	}

	switch toolName {
	case "read", "write":
		remaps["path"] = "file_path"
	case "edit":
		remaps["path"] = "file_path"
		return normalizePiEditInput(rawInput, remaps)
	case "bash":
		// No additional remapping needed
	}

	normalized, err := normalizeToolInput([]byte(rawInput), remaps)
	if err != nil {
		return string(rawInput)
	}
	return string(normalized)
}

// normalizePiEditInput handles the nested edits array in Pi's edit tool input,
// remapping both top-level fields and nested oldText/newText fields.
func normalizePiEditInput(rawInput json.RawMessage, topRemaps map[string]string) string {
	var input map[string]any
	if err := json.Unmarshal([]byte(rawInput), &input); err != nil {
		return string(rawInput)
	}

	// Apply top-level remaps
	for from, to := range topRemaps {
		if v, ok := input[from]; ok {
			delete(input, from)
			input[to] = v
		}
	}

	// Remap fields inside edits array: oldText→old_string, newText→new_string
	if editsRaw, ok := input["edits"]; ok {
		if edits, ok := editsRaw.([]any); ok {
			for i, editRaw := range edits {
				if edit, ok := editRaw.(map[string]any); ok {
					if v, ok := edit["oldText"]; ok {
						delete(edit, "oldText")
						edit["old_string"] = v
					}
					if v, ok := edit["newText"]; ok {
						delete(edit, "newText")
						edit["new_string"] = v
					}
					edits[i] = edit
				}
			}
			input["edits"] = edits
		}
	}

	normalized, err := json.Marshal(input)
	if err != nil {
		return string(rawInput)
	}
	return string(normalized)
}
