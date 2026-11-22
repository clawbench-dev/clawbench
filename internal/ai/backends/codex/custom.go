package codex

import (
	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/ai/backends/claude"
	"clawbench/internal/model"
)

func init() {
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
		// Codex can inject a message into the running turn via the ACP steering
		// extension (_session/steering). It emits no echo boundary, so the
		// injection MERGES into the running reply (no split).
		Behavior: &ai.ACPBehavior{
			ParseToolCall:       ParseACPToolCall,
			ParseToolCallUpdate: ParseACPToolCallUpdate,
			// codex-acp shares claude's _meta.quota shape (same bridge stack).
			ExtractMeta: claude.ExtractMeta,
		},
		ModelSource:  model.PluginSource("codex", discoverCodexModels),
		ListSessions: listCodexSessionsFromDisk,
		MidTurn:      &midTurnInjector{},
		ProviderEnv: &model.ProviderEnvSpec{
			// codex reads its provider from ~/.codex/config.toml. The managed
			// block references this variable for env_key, so the secret never
			// lands on disk.
			ConfigOnly:      true,
			ConfigProtocols: []string{model.ProviderProtocolOpenAI},
			KeyOnly:         "CLAWBENCH_PROVIDER_API_KEY",
		},
	})
}
