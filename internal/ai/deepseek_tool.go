package ai

// parseDeepSeekToolUse extracts a tool_use ToolCall from a DeepSeek stream message.
// It normalizes the tool name and input field names using the shared normalization
// functions (normalizeToolName + normalizeToolInput with getRemaps("deepseek_cli")).
func parseDeepSeekToolUse(msg *DeepSeekStreamMessage) *ToolCall {
	normalized, err := normalizeToolInput(msg.Input, getRemaps("deepseek_cli"))
	input := string(msg.Input)
	if err == nil {
		input = string(normalized)
	}

	return &ToolCall{
		Name:  normalizeToolName(msg.Name),
		ID:    msg.ID,
		Input: input,
		Done:  msg.Done,
	}
}

// parseDeepSeekToolResult extracts a tool_result ToolCall from a DeepSeek stream message.
// Returns nil if the tool call ID is empty (DeepSeek skips empty-ID results).
func parseDeepSeekToolResult(msg *DeepSeekStreamMessage) *ToolCall {
	if msg.ID == "" {
		return nil
	}

	return &ToolCall{
		ID:     msg.ID,
		Output: truncateToolOutput(msg.Output),
		Status: msg.Status,
	}
}
