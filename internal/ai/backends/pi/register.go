package pi

import (
	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

// PiACPInputRemaps is the ACP remap table for Pi.
//
// It is intentionally minimal and mirrors the CLI's "path" remap: pi-acp
// forwards Pi's native tool arguments, and the tool-aware normalization
// (including the nested edits[] array) lives in this package's ACP parser
// (parsePiACPToolCall → normalizePiACPInput).
//
// Do NOT add oldText/newText here: the generic ACP normalizer is a flat
// single-pass remap that never recurses into edits[], so those entries would be
// dead. They are handled by the Pi-specific parser instead.
var PiACPInputRemaps = map[string]string{
	"path": "file_path",
}

func init() {
	backends.Register(&backends.BackendPlugin{
		ID: "pi",
		Spec: model.BackendSpec{
			ID: "pi", Backend: "pi", DefaultCmd: "pi", Name: "Pi", Specialty: "极简编程智能体",
			ThinkingEffortLevels: []string{"off", "minimal", "low", "medium", "high", "xhigh"},
			// ACP via the upstream pi-acp bridge. Pi was previously CLI-only
			// because the adapter originally used (@touchtechclub/pi-acp) stopped
			// being maintained; upstream svkozak/pi-acp is active and passes the
			// full ACP integration suite (see internal/ai/acp_integration_test.go,
			// backend "pi").
			//
			// ACPLoadSession is TRUE: pi-acp advertises loadSession and implements
			// session/load, so the explicit acp-load / acp-sync endpoints work.
			// This flag is about session/load only — pi-acp does NOT implement the
			// non-standard session/resume RPC (it answers -32601), which is a
			// separate capability that affects automatic crash recovery. That path
			// falls back to session/load on -32601, so no flag is needed for it.
			AcpCommand:     "npx -y pi-acp@latest",
			ACPLoadSession: true,
			InstallCmd:     "npm install -g @earendil-works/pi-coding-agent",
			SortOrder:      8,
			// Pi loads its own skills: its docs state it "Supports Claude Code
			// (~/.claude/skills/*/SKILL.md), Codex CLI (~/.codex/skills/), and
			// Pi-native formats (~/.pi/agent/skills/, .pi/skills/)" and it emits
			// an <available_skills> block. Only its own directory is declared
			// here; .claude/skills and .codex/skills are covered by those
			// backends' declarations, and Pi does NOT read .agents/skills.
			NativeSkillsDirs:      []string{".pi/agent/skills"},
			AutoLoadsNativeSkills: true,
			// Measured 2026-10-19: session/new accepts `mcpServers`, returns no
			// error, and the tool never appears.
			MCPUnsupported: true,
		},
		ACP: &backends.ACPPlugin{
			InputRemaps: PiACPInputRemaps,
		},
		Behavior: &ai.ACPBehavior{
			ParseToolCall:       parsePiACPToolCall,
			ParseToolCallUpdate: parsePiACPToolCallUpdate,
		},
		ProviderEnv: &model.ProviderEnvSpec{
			Anthropic: model.ProtoEnv("ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"),
			OpenAI:    model.ProtoEnv("OPENAI_BASE_URL", "OPENAI_API_KEY", ""),
			// models.json holds a providers table, and a model is addressed as
			// "<provider>/<model>" (verified: declaring provider "probe" with
			// model "deepseek-chat" makes "probe/deepseek-chat" selectable and
			// runnable). apiKey accepts "$ENV", so the secret never has to be
			// written out.
			ConfigDirEnv:            "PI_CODING_AGENT_DIR",
			ConfigDirFile:           "models.json",
			ConfigDirProtocols:      []string{model.ProviderProtocolOpenAI, model.ProviderProtocolAnthropic},
			ConfigDirProviderID:     "clawbench",
			ConfigDirAddressPrefix:  true,
			ConfigDirDeclaresModels: true,
		},
	})
}
