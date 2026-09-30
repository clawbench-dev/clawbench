package codex

import (
	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	ai.RegisterBackend("codex", newCodexBackend)
	backends.Register(&backends.BackendPlugin{
		ID: "codex",
		Spec: model.BackendSpec{
			ID: "codex", Backend: "codex", DefaultCmd: "codex", Name: "Codex", Specialty: "OpenAI 编码代理",
			ThinkingEffortLevels: []string{"low", "medium", "high"},
			AcpCommand:           "npx -y @agentclientprotocol/codex-acp@latest",
			ACPLoadSession:       true,
			InstallCmd:           "npm install -g @openai/codex",
			SortOrder:            4,
			// Codex ships a whole skills subsystem (the core-skills crate:
			// "$CODEX_HOME/skills", SKILL.md parsing, a "### Available skills"
			// section and a 2% context budget). Both transports reach it — the
			// ACP bridge spawns the real codex binary — so ClawBench must not
			// inject these again.
			NativeSkillsDirs:      []string{".codex/skills", ".agents/skills"},
			AutoLoadsNativeSkills: true,
		},
	})
}

// newCodexBackend returns a CodexBackend instance.
// Codex is a custom backend — it directly implements AIBackend,
// not using the CLIBackend skeleton.
func newCodexBackend() ai.AIBackend {
	return &ai.CodexBackend{}
}
