package opencode

import (
	"strings"

	"clawbench/internal/ai"
)

func init() {
	ai.RegisterBackend("opencode", newOpenCodeBackend, false)
}

// newOpenCodeBackend returns a CLIBackend instance configured for OpenCode CLI.
func newOpenCodeBackend() ai.AIBackend {
	return &ai.CLIBackend{
		BackendName: "opencode",
		Cmd:         "opencode",
		BuildArgsFn: buildOpenCodeStreamArgs,
		NewParserFn: func() ai.LineParser { return &ai.OpenCodeStreamParser{} },
		FilterLineFn: func(line string) (string, bool) {
			if line == "" || strings.HasPrefix(line, "[opencode-mobile]") {
				return "", false
			}
			if !strings.HasPrefix(line, "{") {
				return "", false
			}
			return line, true
		},
		PreStartFn: nil,
	}
}

// buildOpenCodeStreamArgs constructs the CLI arguments for OpenCode streaming.
func buildOpenCodeStreamArgs(req ai.ChatRequest) []string {
	// OpenCode CLI has no --system-prompt flag — inject into user prompt.
	prompt := ai.InjectSystemPrompt(req)

	args := []string{
		"run",
		prompt,
		"--format", "json",
		"--dangerously-skip-permissions",
	}

	// Pass OpenCode session ID for continuing conversations.
	// Only pass --session when resuming an existing OpenCode session
	// (indicated by Resume=true and a ses_ prefixed session ID).
	// On first message, SessionID contains ClawBench's UUID which OpenCode
	// doesn't recognize — let OpenCode create its own session.
	if req.SessionID != "" && req.Resume {
		args = append(args, "--session", req.SessionID)
	}

	// Working directory
	if req.WorkDir != "" {
		args = append(args, "--dir", req.WorkDir)
	}

	// Model override (format: provider/model, e.g., "minimax-cn-coding-plan/MiniMax-M2.7")
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}

	// Thinking effort level (e.g., --variant high)
	if req.ThinkingEffort != "" {
		args = append(args, "--variant", req.ThinkingEffort)
	}

	return args
}
