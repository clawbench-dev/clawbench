package qoder

import (
	"log/slog"
	"os/exec"
	"strings"

	"clawbench/internal/ai"
)

// NewCLIBackend builds the CLIBackend used by both Qoder distributions.
//
// The two builds (international `qodercli` / mainland `qoderclicn`) are the
// same code compiled with a different brand constant, so their CLI surface is
// identical — verified flag-by-flag in
// internal/ai/qoder_cn_acp_probe_integration_test.go (TestQoderCNProbe_L0_FlagParity:
// all 59 flags match). Only the binary name and the user config root differ,
// neither of which reaches this function.
//
// backendName is the BackendSpec.Backend value ("qoder" / "qodercn"); it is
// what ACP event dispatch and CLI logging key on, so it must match the spec.
func NewCLIBackend(backendName, cmd string) *ai.CLIBackend {
	return &ai.CLIBackend{
		BackendName:  backendName,
		Cmd:          cmd,
		BuildArgsFn:  BuildStreamArgs,
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

// BuildStreamArgs constructs the CLI arguments for Qoder streaming.
//
// Shared by both distributions; keep it free of brand-specific flags.
func BuildStreamArgs(req ai.ChatRequest) []string {
	args := []string{"--print", "--output-format", "stream-json"}
	if req.Resume && req.SessionID != "" {
		args = append(args, "--resume", req.SessionID)
	} else if req.Resume {
		slog.Warn("cli: resume requested but no session_id, starting fresh (qoder)")
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
