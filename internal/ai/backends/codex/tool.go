package codex

import (
	"clawbench/internal/ai"
)

// parseCodexToolStart extracts a tool_use ai.ToolCall (Done=false) from a Codex item.started event.
// Returns nil if the message does not contain a command_execution item.
func parseCodexToolStart(msg *CodexStreamMessage) *ai.ToolCall {
	if msg.Item == nil || msg.Item.Type != "command_execution" {
		return nil
	}
	input := ai.ExecCommandJSON(msg.Item.Command)
	return &ai.ToolCall{
		Name:  "Bash",
		ID:    msg.Item.ID,
		Input: input,
		Done:  false,
	}
}

// parseCodexToolComplete extracts a tool_use ai.ToolCall (Done=true) from a Codex item.completed event.
// Returns nil if the message does not contain a command_execution item.
func parseCodexToolComplete(msg *CodexStreamMessage) *ai.ToolCall {
	if msg.Item == nil || msg.Item.Type != "command_execution" {
		return nil
	}
	input := ai.ExecCommandJSON(msg.Item.Command)
	output := ai.TruncateToolOutput(msg.Item.AggregatedOutput)
	status := ""
	if msg.Item.ExitCode != nil && *msg.Item.ExitCode != 0 {
		status = "error"
	} else if output != "" {
		status = "success"
	}
	return &ai.ToolCall{
		Name:   "Bash",
		ID:     msg.Item.ID,
		Input:  input,
		Done:   true,
		Output: output,
		Status: status,
	}
}

// emitBashToolCall creates a Bash ai.ToolCall ai.StreamEvent and sends it to the channel.
// Codex only has "command_execution"; we normalize to "Bash" with {"command":"..."} input.
// This is the canonical form used throughout the codebase for Codex tool calls.
func emitBashToolCall(ch chan<- ai.StreamEvent, id, input, output string, done bool, exitCode *int) {
	status := ""
	if done {
		if exitCode != nil && *exitCode != 0 {
			status = "error"
		} else if output != "" {
			status = "success"
		}
	}
	ai.EmitStreamEvent(ch, "codex", ai.StreamEvent{Type: "tool_use", Tool: &ai.ToolCall{
		Name:   "Bash",
		ID:     id,
		Input:  input,
		Done:   done,
		Output: output,
		Status: status,
	}})
}
