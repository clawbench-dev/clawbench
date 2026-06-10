package ai

import (
	"os/exec"
	"strings"
)

// copilotBackend returns a CLIBackend instance for GitHub Copilot CLI.
// Copilot uses Claude-family stream-json output format (--output-format json).
func copilotBackend() *CLIBackend {
	return &CLIBackend{
		name:           "copilot",
		defaultCommand: "copilot",
		buildArgs:      buildCopilotStreamArgs,
		newParser:      func() LineParser { return &StreamParser{} },
		filterLine:     filterSkipNonJSON(),
		preStart: func(cmd *exec.Cmd, req ChatRequest) {
			// Copilot CLI in non-interactive mode receives prompt via stdin
			cmd.Stdin = strings.NewReader(req.Prompt)
		},
	}
}
