package ai

import (
	"encoding/json"
	"strings"
)

// parsePiToolCallEnd extracts a tool_use ToolCall from a Pi toolcall_end event.
// Pi toolcall_end provides the complete tool arguments (Done=true).
// Tool name is normalized via normalizeToolName; input field names are
// remapped using the pi_cli remap table. The edit tool uses normalizePiEditInput
// for its nested edits array structure.
func parsePiToolCallEnd(evt *PiAssistantMessageEvent) *ToolCall {
	if evt.ToolCall == nil {
		return nil
	}

	tc := evt.ToolCall
	normalizedInput := normalizePiToolInput(tc.Name, tc.Arguments)

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

// normalizePiEditInput is declared in pi_stream.go.
// It will be moved to this file when pi_stream.go is refactored in a later step.

// normalizePiToolInput normalizes tool input for Pi tool calls.
// For the edit tool, it uses normalizePiEditInput for nested edits array handling.
// For all other tools, it uses normalizeToolInput with the pi_cli remap table.
func normalizePiToolInput(toolName string, rawInput json.RawMessage) string {
	if len(rawInput) == 0 {
		return "{}"
	}

	if toolName == "edit" {
		return normalizePiEditInput(rawInput, getRemaps("pi_cli"))
	}

	normalized, err := normalizeToolInput([]byte(rawInput), getRemaps("pi_cli"))
	if err != nil {
		return string(rawInput)
	}
	return string(normalized)
}
