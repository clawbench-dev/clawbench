package claude

import (
	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	backends.Register(&backends.BackendPlugin{
		ID: "claude",
		Spec: model.BackendSpec{
			ID: "claude", Backend: "claude", DefaultCmd: "claude", Name: "Claude", Specialty: "代码编写与推理",
			ThinkingEffortLevels: []string{"low", "medium", "high", "xhigh", "max"},
			AcpCommand:           "npx -y @agentclientprotocol/claude-agent-acp@latest",
			ACPLoadSession:       true,
			InstallCmd:           "npm install -g @anthropic-ai/claude-code",
			SortOrder:            1,
			// Claude Code loads .claude/skills into context itself (verified in
			// the shipped claude.exe: it references ".claude/skills" and
			// "SKILL.md"), so ClawBench must not inject them again — they would
			// appear twice. They still shadow same-named skills from other
			// sources. Claude does NOT read the shared .agents/skills.
			NativeSkillsDirs:      []string{".claude/skills"},
			AutoLoadsNativeSkills: true,
		},
		// Claude can inject a message into the running turn via the ACP
		// steering extension (_session/steering). It emits no echo boundary, so
		// the injection MERGES into the running reply (no split).
		// This is the definition of the Claude-family ACP hook set. qoder /
		// qodercn are Claude Code forks and re-export these same function
		// values rather than copying the parsers; codex shares ExtractMeta too.
		// Every hook Claude's bridge protocol needs must be listed here — a hook
		// missing from this struct silently degrades to the generic fallback for
		// claude AND every fork that re-exports it (which is exactly how
		// ExtractMeta was lost once), so keep the set complete and let
		// TestClaudeFamilyBehaviorsAreShared catch a fork that stops sharing.
		Behavior: &ai.ACPBehavior{
			ParseToolCall:       ParseACPToolCall,
			ParseToolCallUpdate: ParseACPToolCallUpdate,
			ExtractMeta:         ExtractMeta,
		},
		ModelSource: model.PluginSource("claude", discoverClaudeModels),
		MidTurn:     midTurn,
		ProviderEnv: &model.ProviderEnvSpec{
			Anthropic: model.ProtoEnv("ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"),
			// CLAUDE_CONFIG_DIR relocates the whole ~/.claude tree, and the ACP
			// adapter (claude-agent-acp) merges settings.json's `env` block into
			// the session. Verified end-to-end: with a private directory holding
			// ONLY settings.json (no login state at all), a turn against a
			// third-party Anthropic-compatible endpoint returned its reply.
			//
			// This is the ONE backend where the config file is not merely an
			// alternative to the environment variables: ANTHROPIC_BASE_URL alone
			// created the session but the turn never produced a reply (the
			// adapter reads its endpoint from the merged settings, not from the
			// raw environment). So without this the Anthropic-protocol binding
			// was effectively unusable for claude.
			ConfigDirEnv:          "CLAUDE_CONFIG_DIR",
			ConfigDirFile:         "settings.json",
			ConfigDirProtocols:    []string{model.ProviderProtocolAnthropic},
			ConfigDirProviderID:   "clawbench",
			ConfigDirSecretInFile: true,
			UserConfigDir:         ".claude",
			ConfigDirRender:       SettingsJSONContent,
		},
	})
}
