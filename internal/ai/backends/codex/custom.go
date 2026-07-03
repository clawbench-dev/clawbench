package codex

import (
	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	ai.RegisterBackend("codex", newCodexBackend, false)
	backends.Register(&backends.BackendPlugin{
		ID: "codex",
		Spec: model.BackendSpec{
			ID: "codex", Backend: "codex", DefaultCmd: "codex", Name: "Codex", Icon: "🐙", Specialty: "OpenAI 编码代理",
			ThinkingEffortLevels: []string{"low", "medium", "high"},
			AcpCommand:           "npx -y @agentclientprotocol/codex-acp@latest",
			InstallCmd:           "npm install -g @openai/codex",
			SortOrder:            4,
		},
	})
}

// newCodexBackend returns a CodexBackend instance.
// Codex is a custom backend — it directly implements AIBackend,
// not using the CLIBackend skeleton. AutoResume is not needed.
func newCodexBackend() ai.AIBackend {
	return &ai.CodexBackend{}
}
