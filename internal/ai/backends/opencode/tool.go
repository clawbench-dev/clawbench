package opencode

import (
	"encoding/json"
	"log/slog"

	"clawbench/internal/ai"
)

// parseOpenCodeToolEvent extracts a ai.ToolCall from an OpenCode stream message.
// OpenCode combines tool_use and tool_result in a single event (via part.tool).
// Returns nil if the message doesn't contain a valid tool event.
func parseOpenCodeToolEvent(msg *OpenCodeStreamMessage, toolNameMap, remaps map[string]string) *ai.ToolCall {
	if msg.Type != "tool_use" {
		return nil
	}

	var part OpenCodeToolPart
	if err := json.Unmarshal(msg.Part, &part); err != nil {
		slog.Debug("opencode tool: skipping unparseable tool_use part", "error", err)
		return nil
	}

	inputStr := "{}"
	if part.State != nil && len(part.State.Input) > 0 && string(part.State.Input) != "null" {
		normalized, err := ai.NormalizeToolInput(part.State.Input, remaps)
		if err != nil {
			inputStr = string(part.State.Input) // fallback to raw
		} else {
			inputStr = string(normalized)
		}
	}

	done := part.State != nil && part.State.Status == "completed"
	output := ""
	status := ""
	if part.State != nil {
		output = ai.TruncateToolOutput(part.State.Output)
		if done && part.State.Output != "" {
			status = "success"
		}
	}

	toolName := part.Tool
	if toolNameMap != nil {
		if canonical, ok := toolNameMap[toolName]; ok {
			toolName = canonical
		} else {
			toolName = ai.NormalizeToolName(toolName)
		}
	} else {
		toolName = ai.NormalizeToolName(toolName)
	}

	return &ai.ToolCall{
		Name:   toolName,
		ID:     part.CallID,
		Input:  inputStr,
		Done:   done,
		Output: output,
		Status: status,
	}
}
