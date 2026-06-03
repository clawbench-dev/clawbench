package ai

// buildClineStreamArgs constructs the CLI arguments for Cline streaming.
// Cline uses --json for streaming JSON output (Claude-family format),
// --auto-approve true for non-interactive mode, and --thinking for effort level.
func buildClineStreamArgs(req ChatRequest) []string {
	args := []string{
		"--json",
		"--auto-approve", "true",
	}

	// Resume previous session
	if req.SessionID != "" && req.Resume {
		args = append(args, "--id", req.SessionID)
	}

	// Working directory
	if req.WorkDir != "" {
		args = append(args, "--cwd", req.WorkDir)
	}

	// Model override
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}

	// Thinking effort level
	if req.ThinkingEffort != "" {
		args = append(args, "--thinking", req.ThinkingEffort)
	}

	return args
}
