package ai

// buildCopilotStreamArgs constructs the CLI arguments for GitHub Copilot streaming.
// Copilot uses --output-format json for streaming output and -p for non-interactive mode.
func buildCopilotStreamArgs(req ChatRequest) []string {
	args := []string{
		"--output-format", "json",
		"--allow-all",
	}

	// Non-interactive prompt
	args = append(args, "-p", req.Prompt)

	// Resume previous session
	if req.SessionID != "" && req.Resume {
		args = append(args, "--resume", req.SessionID)
	}

	// Working directory
	if req.WorkDir != "" {
		args = append(args, "-C", req.WorkDir)
	}

	// Model override
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}

	// Thinking effort level
	if req.ThinkingEffort != "" {
		args = append(args, "--effort", req.ThinkingEffort)
	}

	return args
}
