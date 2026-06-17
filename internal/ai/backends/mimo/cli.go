package mimo

import (
	"strings"

	"clawbench/internal/ai"
)

func init() {
	ai.RegisterBackend("mimo", newMimoBackend, true)
}

// newMimoBackend returns a CLIBackend instance for MiMo-Code CLI.
// MiMo-Code is a fork of OpenCode and uses the same JSON stream format,
// so we reuse OpenCode's stream parser.
func newMimoBackend() ai.AIBackend {
	return &ai.CLIBackend{
		BackendName: "mimo",
		Cmd:         "mimo",
		BuildArgsFn: buildMimoStreamArgs,
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

// buildMimoStreamArgs constructs the CLI arguments for MiMo-Code streaming.
// MiMo-Code uses the same `run --format json` interface as OpenCode.
func buildMimoStreamArgs(req ai.ChatRequest) []string {
	prompt := ai.InjectSystemPrompt(req)
	args := []string{"run", prompt, "--format", "json", "--dangerously-skip-permissions"}
	if req.SessionID != "" && req.Resume {
		args = append(args, "--session", req.SessionID)
	}
	if req.WorkDir != "" {
		args = append(args, "--dir", req.WorkDir)
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.ThinkingEffort != "" {
		args = append(args, "--variant", req.ThinkingEffort)
	}
	return args
}
