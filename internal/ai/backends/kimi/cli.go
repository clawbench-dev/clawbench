package kimi

import (
	"strings"

	"clawbench/internal/ai"
)

func init() {
	ai.RegisterBackend("kimi", newKimiBackend, true)
}

// newKimiBackend returns a CLIBackend instance for Kimi CLI.
// Kimi uses stream-json output format (--print --output-format stream-json).
func newKimiBackend() ai.AIBackend {
	return &ai.CLIBackend{
		BackendName: "kimi",
		Cmd:         "kimi",
		BuildArgsFn: buildKimiStreamArgs,
		NewParserFn: func() ai.LineParser { return &ai.StreamJSONParser{} },
		FilterLineFn: func(line string) (string, bool) {
			if line == "" || !strings.HasPrefix(line, "{") {
				return "", false
			}
			return line, true
		},
		PreStartFn: nil,
	}
}

// buildKimiStreamArgs constructs the CLI arguments for Kimi streaming.
// Kimi uses --print for non-interactive mode and --output-format stream-json
// for streaming output (Kimi CLI is forked from Gemini CLI and uses the same stream-json format).
func buildKimiStreamArgs(req ai.ChatRequest) []string {
	// Kimi CLI has no --system-prompt flag, so inject into the user prompt.
	prompt := ai.InjectSystemPrompt(req)

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
