package ai

import (
	"os/exec"
	"strings"
)

// clineBackend returns a CLIBackend instance for Cline CLI.
// Cline uses Claude-family stream-json output format (--json flag).
func clineBackend() *CLIBackend {
	return &CLIBackend{
		name:           "cline",
		defaultCommand: "cline",
		buildArgs:      buildClineStreamArgs,
		newParser:      func() LineParser { return &StreamParser{} },
		filterLine:     nil, // skip empty lines only (default)
		preStart: func(cmd *exec.Cmd, req ChatRequest) {
			// Cline CLI with --json and stdout piped (non-TTY) receives
			// prompt via stdin — positional prompt argument is not recognized.
			cmd.Stdin = strings.NewReader(req.Prompt)
		},
	}
}
