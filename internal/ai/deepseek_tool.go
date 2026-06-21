package ai

// parseDeepSeekToolUse extracts a tool_use ToolCall from a DeepSeek stream message.
// It normalizes the tool name and input field names using the shared normalization
// functions. DeepSeek uses concise snake_case names (path, search, replace) that
// differ from the canonical Claude-style names (file_path, old_string, new_string).
// Per-tool remap overrides are applied based on the raw tool name.
func parseDeepSeekToolUse(msg *DeepSeekStreamMessage, baseRemaps map[string]string) *ToolCall {
	remaps := map[string]string{
		"filePaths": "file_paths",
		"oldString": "old_string",
		"newString": "new_string",
		"dirPath":   "path",
	}
	for k, v := range baseRemaps {
		remaps[k] = v
	}

	switch msg.Name {
	case "edit_file":
		remaps["path"] = "file_path"
		remaps["search"] = "old_string"
		remaps["replace"] = "new_string"
	case "read_file", "write_file", "list_dir":
		remaps["path"] = "file_path"
	case "grep_files", "file_search":
		// 'path' is already canonical for Grep/Glob
	}

	normalized, err := normalizeToolInput(msg.Input, remaps)
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
