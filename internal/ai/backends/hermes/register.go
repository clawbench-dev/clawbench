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
			// Hermes loads its own skills from ~/.hermes/skills (the
			// "hermes-acp" toolset enables skills_list/skill_view/skill_manage,
			// and agent/skill_utils.py's home tier is get_skills_dir() =
			// HERMES_HOME/skills), so ClawBench must not inject them again.
			//
			// ~/.agents/skills is deliberately NOT declared: unlike codex/copilot/
			// opencode/qoder/dsh, Hermes does not read it by default — the shared
			// dir is only loaded when listed in skills.external_dirs, which
			// defaults to []. Declaring it here with AutoLoadsNativeSkills=true
			// would make dedupe SUPPRESS injection of every skill that lives only
			// in ~/.agents/skills, leaving Hermes unable to see them at all.
			NativeSkillsDirs:      []string{".hermes/skills"},
			AutoLoadsNativeSkills: true,
			// ThinkingEffortLevels is intentionally nil: Hermes has no static
			// effort levels and advertises no `thought_level` ACP config option,
			// so there is nothing for the UI to switch (same posture as dsh).
		},
		ACP: &backends.ACPPlugin{
			ToolTitlePrefixes: HermesACPTitlePrefixes,
			InputRemaps:       HermesACPInputRemaps,
			// Hermes accepts a "model" config option but never reads it back
			// (acp_adapter/server.py: set_config_option only stores the value in
			// state.config_options), so a config-option switch is a silent no-op.
			// The method it actually honors is the ACP-standard
			// session/set_model (server.py: set_session_model → _switch_model).
			ModelSwitchMethod: backends.ACPModelSwitchSetModel,
		},
	})
}
