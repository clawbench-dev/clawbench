package copilot

import (
	"os/exec"
	"strings"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	ai.RegisterBackend("copilot", newCopilotBackend, true)
	backends.Register(&backends.BackendPlugin{
		ID: "copilot",
		Spec: model.BackendSpec{
			ID: "copilot", Backend: "copilot", DefaultCmd: "copilot", Name: "Copilot", Icon: "🤝", Specialty: "GitHub Copilot 编码助手",
			ThinkingEffortLevels: []string{"none", "low", "medium", "high", "xhigh", "max"},
			AcpCommand:           "copilot --acp",
			SortOrder:            11,
		},
	})
}

// newCopilotBackend returns a CLIBackend instance for GitHub Copilot CLI.
func newCopilotBackend() ai.AIBackend {
	return &ai.CLIBackend{
		BackendName: "copilot",
		Cmd:         "copilot",
		BuildArgsFn: buildCopilotStreamArgs,
		NewParserFn: func() ai.LineParser { return &ai.StreamParser{} },
		FilterLineFn: func(line string) (string, bool) {
			if line == "" || !strings.HasPrefix(line, "{") {
				return "", false
			}
			return line, true
		},
		PreStartFn: func(cmd *exec.Cmd, req ai.ChatRequest) {
			// Copilot CLI in non-interactive mode receives prompt via stdin
			cmd.Stdin = strings.NewReader(req.Prompt)
		},
	}
}

// buildCopilotStreamArgs constructs the CLI arguments for GitHub Copilot streaming.
// Copilot uses --output-format json for streaming output and -p for non-interactive mode.
func buildCopilotStreamArgs(req ai.ChatRequest) []string {
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
