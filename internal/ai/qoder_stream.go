package ai

// buildQoderStreamArgs constructs the CLI arguments for Qoder streaming
func buildQoderStreamArgs(req ChatRequest) []string {
	args := []string{
		"--print",
		"--output-format", "stream-json",
	}

	if req.Resume {
		args = append(args, "--resume", req.SessionID)
	} else if req.SessionID != "" {
		args = append(args, "--session-id", req.SessionID)
	}

	if req.WorkDir != "" {
		args = append(args, "--cwd", req.WorkDir)
	}
	args = append(args, "--dangerously-skip-permissions")

	if req.SystemPrompt != "" {
		args = append(args, "--system-prompt", req.SystemPrompt)
	}

	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}

	return args
}
