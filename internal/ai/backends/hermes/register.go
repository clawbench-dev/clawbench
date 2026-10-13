package hermes

import (
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	// Hermes Agent (NousResearch) is ACP-only via its built-in `hermes acp`
	// subcommand: no ai.RegisterBackend (CLI factory) is registered, so chat
	// always uses the ACP stdio transport.
	//
	// Verified against hermes v0.21.7 (acp_adapter/): initialize advertises
	// loadSession, promptCapabilities.image, and session fork/list/resume; the
	// CLI has no stream-json mode, so there is no CLI backend to register.
	backends.Register(&backends.BackendPlugin{
		ID: "hermes",
		Spec: model.BackendSpec{
			ID:         "hermes",
			Backend:    "hermes",
			DefaultCmd: "hermes",
			Name:       "Hermes Agent",
			Specialty:  "NousResearch 通用智能体",
			// `hermes acp` starts the ACP stdio server (stdout reserved for
			// JSON-RPC; logs go to stderr).
			AcpCommand: "hermes acp",
			// The adapter advertises loadSession and implements session/load, so
			// the explicit acp-load / acp-sync endpoints work. Automatic crash
			// recovery uses session/resume and is unaffected.
			ACPLoadSession: true,
			InstallCmd:     "curl -fsSL https://hermes-agent.nousresearch.com/install.sh | bash",
			SortOrder:      17,
			// Hermes loads its own skills from ~/.hermes/skills and the shared
			// ~/.agents/skills (toolsets.py + agent/skill_utils.py: the
			// "hermes-acp" toolset enables skills_list/skill_view/skill_manage),
			// so ClawBench must not inject them again.
			NativeSkillsDirs:      []string{".hermes/skills", ".agents/skills"},
			AutoLoadsNativeSkills: true,
			// ThinkingEffortLevels is intentionally nil: Hermes has no static
			// effort levels and advertises no `thought_level` ACP config option,
			// so there is nothing for the UI to switch (same posture as dsh).
		},
		ACP: &backends.ACPPlugin{
			ToolTitlePrefixes: HermesACPTitlePrefixes,
			InputRemaps:       HermesACPInputRemaps,
		},
	})
}
