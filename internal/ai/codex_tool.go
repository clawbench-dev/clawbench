package ai

// parseCodexToolStart extracts a tool_use ToolCall (Done=false) from a Codex item.started event.
// Returns nil if the message does not contain a command_execution item.
func parseCodexToolStart(msg *CodexStreamMessage) *ToolCall {
	if msg.Item == nil || msg.Item.Type != "command_execution" {
		return nil
	}
	input := execCommandJSON(msg.Item.Command)
	return &ToolCall{
		Name:  "Bash",
		ID:    msg.Item.ID,
		Input: input,
		Done:  false,
	}
}

// parseCodexToolComplete extracts a tool_use ToolCall (Done=true) from a Codex item.completed event.
// Returns nil if the message does not contain a command_execution item.
func parseCodexToolComplete(msg *CodexStreamMessage) *ToolCall {
	if msg.Item == nil || msg.Item.Type != "command_execution" {
		return nil
	}
	input := execCommandJSON(msg.Item.Command)
	output := truncateToolOutput(msg.Item.AggregatedOutput)
	status := ""
	if msg.Item.ExitCode != nil && *msg.Item.ExitCode != 0 {
		status = "error"
	} else if output != "" {
		status = "success"
	}
	return &ToolCall{
		Name:   "Bash",
		ID:     msg.Item.ID,
		Input:  input,
		Done:   true,
		Output: output,
		Status: status,
	}
}

// emitBashToolCall creates a Bash ToolCall StreamEvent and sends it to the channel.
// Codex only has "command_execution"; we normalize to "Bash" with {"command":"..."} input.
// This is the canonical form used throughout the codebase for Codex tool calls.
func emitBashToolCall(ch chan<- StreamEvent, id, input, output string, done bool, exitCode *int) {
	status := ""
	if done {
		if exitCode != nil && *exitCode != 0 {
			status = "error"
		} else if output != "" {
			status = "success"
		}
	}
	ch <- StreamEvent{Type: "tool_use", Tool: &ToolCall{
		Name:   "Bash",
		ID:     id,
		Input:  input,
		Done:   done,
		Output: output,
		Status: status,
	}}
}
