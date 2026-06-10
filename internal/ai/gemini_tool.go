package ai

// parseGeminiToolUse extracts a tool_use ToolCall from a Gemini/Kimi stream message.
// Returns nil if the message doesn't contain a valid tool_use.
func parseGeminiToolUse(msg *GeminiStreamMessage) *ToolCall {
	inputStr := "{}"
	if len(msg.Parameters) > 0 {
		normalized, err := normalizeToolInput(msg.Parameters, getRemaps("gemini_cli"))
		if err != nil {
			inputStr = string(msg.Parameters)
		} else {
			inputStr = string(normalized)
		}
	}
	return &ToolCall{
		Name:  normalizeToolName(msg.ToolName),
		ID:    msg.ToolID,
		Input: inputStr,
		Done:  true, // Gemini sends full tool input in one event
	}
}

// parseGeminiToolResult extracts a tool_result ToolCall from a Gemini/Kimi stream message.
// Returns nil if the message doesn't contain a valid tool_result.
func parseGeminiToolResult(msg *GeminiStreamMessage) *ToolCall {
	if msg.ToolID == "" {
		return nil
	}
	return &ToolCall{
		ID:     msg.ToolID,
		Output: truncateToolOutput(msg.ToolOutput),
		Status: msg.Status,
	}
}
