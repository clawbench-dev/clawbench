package claude

import (
	"os/exec"
	"strings"

	"clawbench/internal/ai"
)

func init() {
	ai.RegisterBackend("claude", newClaudeBackend, true)
}

// newClaudeBackend returns a CLIBackend instance configured for Claude CLI.
func newClaudeBackend() ai.AIBackend {
	return &ai.CLIBackend{
		BackendName: "claude",
		Cmd:         "claude",
		BuildArgsFn: buildClaudeStreamArgs,
		NewParserFn: func() ai.LineParser { return &ai.StreamParser{} },
		FilterLineFn: nil, // skip empty lines only (default)
		PreStartFn: func(cmd *exec.Cmd, req ai.ChatRequest) {
			// Claude CLI in --print mode with stdout piped (non-TTY) requires prompt
			// via stdin — positional prompt argument is not recognized.
			// Both new sessions and resume sessions use stdin for prompt.
			cmd.Stdin = strings.NewReader(req.Prompt)
		},
	}
}

// buildClaudeStreamArgs constructs the CLI arguments for Claude streaming.
func buildClaudeStreamArgs(req ai.ChatRequest) []string {
	return ai.BuildBaseStreamArgs(req, func(r ai.ChatRequest) []string {
		return []string{"--verbose"}
	})
}
