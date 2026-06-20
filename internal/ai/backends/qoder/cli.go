package qoder

import (
	"os/exec"
	"strings"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	ai.RegisterBackend("qoder", newQoderBackend, true)
	backends.Register(&backends.BackendPlugin{
		ID: "qoder",
		Spec: model.BackendSpec{
			ID: "qoder", Backend: "qoder", DefaultCmd: "qodercli", Name: "Qoder", Icon: "⚡", Specialty: "AI 编码助手",
			AcpCommand: "qodercli --acp",
			SortOrder:  5,
		},
	})
}

// newQoderBackend returns a CLIBackend instance for Qoder CLI.
func newQoderBackend() ai.AIBackend {
	return &ai.CLIBackend{
		BackendName:  "qoder",
		Cmd:          "qodercli",
		BuildArgsFn:  buildQoderStreamArgs,
		NewParserFn:  func() ai.LineParser { return &ai.StreamParser{} },
		FilterLineFn: nil, // skip empty lines only (default)
		PreStartFn: func(cmd *exec.Cmd, req ai.ChatRequest) {
			// Qoder CLI in --print mode with stdout piped (non-TTY) requires
			// prompt via stdin — positional prompt argument is not recognized.
			// Both new sessions and resume sessions use stdin for prompt.
			cmd.Stdin = strings.NewReader(req.Prompt)
		},
	}
}

// buildQoderStreamArgs constructs the CLI arguments for Qoder streaming
func buildQoderStreamArgs(req ai.ChatRequest) []string {
	args := []string{"--print", "--output-format", "stream-json"}
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
