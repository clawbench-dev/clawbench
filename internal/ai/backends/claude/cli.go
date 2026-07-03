package claude

import (
	"os/exec"
	"strings"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	ai.RegisterBackend("claude", newClaudeBackend, true)
	backends.Register(&backends.BackendPlugin{
		ID: "claude",
		Spec: model.BackendSpec{
			ID: "claude", Backend: "claude", DefaultCmd: "claude", Name: "Claude", Icon: "🤖", Specialty: "代码编写与推理",
			ThinkingEffortLevels: []string{"low", "medium", "high", "xhigh", "max"},
			AcpCommand:           "npx -y @agentclientprotocol/claude-agent-acp@latest",
			InstallCmd:           "npm install -g @anthropic-ai/claude-code",
			SortOrder:            1,
		},
		ACP: &backends.ACPPlugin{
			InputRemaps: ClaudeACPRemaps,
		},
	})
}

// newClaudeBackend returns a CLIBackend instance configured for Claude CLI.
func newClaudeBackend() ai.AIBackend {
	return &ai.CLIBackend{
		BackendName:  "claude",
		Cmd:          "claude",
		BuildArgsFn:  buildClaudeStreamArgs,
		NewParserFn:  func() ai.LineParser { return &ai.StreamParser{} },
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
