package cline

import (
	"os/exec"
	"strings"

	"clawbench/internal/ai"
)

func init() {
	ai.RegisterBackend("cline", newClineBackend, true)
}

func newClineBackend() ai.AIBackend {
	return &ai.CLIBackend{
		BackendName: "cline",
		Cmd:         "cline",
		BuildArgsFn: buildClineStreamArgs,
		NewParserFn: func() ai.LineParser { return &ai.StreamParser{} },
		FilterLineFn: nil,
		PreStartFn: func(cmd *exec.Cmd, req ai.ChatRequest) {
			cmd.Stdin = strings.NewReader(req.Prompt)
		},
	}
}

func buildClineStreamArgs(req ai.ChatRequest) []string {
	args := []string{"--json", "--auto-approve", "true"}
	if req.SessionID != "" && req.Resume {
		args = append(args, "--id", req.SessionID)
	}
	if req.WorkDir != "" {
		args = append(args, "--cwd", req.WorkDir)
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.ThinkingEffort != "" {
		args = append(args, "--thinking", req.ThinkingEffort)
	}
	return args
}
