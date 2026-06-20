package codebuddy

import (
	"os/exec"
	"strings"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	ai.RegisterBackend("codebuddy", newCodebuddyBackend, true)
	backends.Register(&backends.BackendPlugin{
		ID: "codebuddy",
		Spec: model.BackendSpec{
			ID: "codebuddy", Backend: "codebuddy", DefaultCmd: "codebuddy", Name: "Codebuddy", Icon: "🐛", Specialty: "全栈开发助手",
			ThinkingEffortLevels: []string{"low", "medium", "high", "xhigh"},
			AcpCommand:           "codebuddy --acp",
			SortOrder:            2,
		},
		ACP: &backends.ACPPlugin{
			InputRemaps: CodebuddyACPRemaps,
		},
	})
}

// newCodebuddyBackend returns a CLIBackend instance configured for Codebuddy CLI.
func newCodebuddyBackend() ai.AIBackend {
	return &ai.CLIBackend{
		BackendName: "codebuddy",
		Cmd:         "codebuddy",
		BuildArgsFn: buildCodebuddyStreamArgs,
		NewParserFn: func() ai.LineParser { return &ai.StreamParser{} },
		FilterLineFn: func(line string) (string, bool) {
			line = strings.TrimPrefix(line, "\xEF\xBB\xBF") // UTF-8 BOM
			if line == "" {
				return "", false
			}
			return line, true
		},
		PreStartFn: func(cmd *exec.Cmd, req ai.ChatRequest) {
			// Codebuddy CLI in --print mode with stdout piped (non-TTY) requires
			// prompt via stdin — positional prompt argument is not recognized.
			cmd.Stdin = strings.NewReader(req.Prompt)
		},
	}
}

// buildCodebuddyStreamArgs constructs the CLI arguments for Codebuddy streaming.
func buildCodebuddyStreamArgs(req ai.ChatRequest) []string {
	return ai.BuildBaseStreamArgs(req, nil)
}
