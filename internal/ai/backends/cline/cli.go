package cline

import (
	"os/exec"
	"strings"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	ai.RegisterBackend("cline", newClineBackend, true)
	backends.Register(&backends.BackendPlugin{
		ID: "cline",
		Spec: model.BackendSpec{
			ID: "cline", Backend: "cline", DefaultCmd: "cline", Name: "Cline", Icon: "🔮", Specialty: "自主编码智能体",
			ThinkingEffortLevels: []string{"none", "low", "medium", "high", "xhigh"},
			AcpCommand:           "cline --acp",
			InstallCmd:           "npm install -g cline",
			SortOrder:            9,
		},
	})
}

func newClineBackend() ai.AIBackend {
	return &ai.CLIBackend{
		BackendName:  "cline",
		Cmd:          "cline",
		BuildArgsFn:  buildClineStreamArgs,
		NewParserFn:  func() ai.LineParser { return &ai.StreamParser{} },
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
