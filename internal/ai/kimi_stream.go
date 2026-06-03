package ai

// buildKimiStreamArgs constructs the CLI arguments for Kimi streaming.
// Kimi uses --print for non-interactive mode and --output-format stream-json
// for streaming output (same format as Gemini CLI since Kimi is forked from it).
func buildKimiStreamArgs(req ChatRequest) []string {
	// Kimi CLI has no --system-prompt flag, so inject into the user prompt.
	prompt := injectSystemPrompt(req)

	args := []string{
		"--print",
		"--prompt", prompt,
		"--output-format", "stream-json",
		"--yes",
	}

	// Resume previous session
	if req.SessionID != "" && req.Resume {
		args = append(args, "--session", req.SessionID)
	}

	// Working directory
	if req.WorkDir != "" {
		args = append(args, "--work-dir", req.WorkDir)
	}

	// Model override
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}

	// Thinking mode
	if req.ThinkingEffort != "" {
		if req.ThinkingEffort == "off" {
			args = append(args, "--no-thinking")
		} else {
			args = append(args, "--thinking")
		}
	}

	return args
}
