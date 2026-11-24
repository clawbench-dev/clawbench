package cline

import (
	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	backends.Register(&backends.BackendPlugin{
		ID: "cline",
		Spec: model.BackendSpec{
			ID: "cline", Backend: "cline", DefaultCmd: "cline", Name: "Cline", Specialty: "开源编码智能体",
			// `cline --help` (3.0.75): "Set reasoning effort:
			// none|low|medium|high|xhigh". "none" is a real level here, not the
			// absence of a value, so all five are listed.
			ThinkingEffortLevels: []string{"none", "low", "medium", "high", "xhigh"},
			AcpCommand:           "cline --acp",
			// Initialize reports loadSession:true and session/load answers (no
			// -32601). Whether it REPLAYS prior messages was not verified — see
			// docs/dev/cline_acp_probe_results.md.
			ACPLoadSession: true,
			InstallCmd:     "npm install -g cline",
			// session/new answers -32000 AUTH_REQUIRED until one of the ACP auth
			// methods has completed. The bare CLI is what opens the sign-in UI.
			LoginCmd: "cline",
			// Initialize advertises no sessionCapabilities and session/list
			// answers -32601 (measured), so the on-disk scanner below is what
			// fills the @resume drawer.
			//
			// Cline loads its own skills (3.0.75 bundle: bA() → Io()/C(Jt(),"skills")/
			// ~/.agents/skills; SKILL.md discovery, and a `skills` tool category
			// with `use_skill` that is registered whenever tools are enabled), so
			// ClawBench must not inject them again. Directory list measured from
			// the bundle's constants: <workspace>/.clinerules, .cline, .claude,
			// .agents skills + ~/.cline/skills + ~/.agents/skills.
			NativeSkillsDirs:      []string{".cline/skills", ".agents/skills"},
			AutoLoadsNativeSkills: true,
			SortOrder:             22,
		},
		ModelSource: model.PluginSource("cline", discoverClineModels),
		ProviderEnv: &model.ProviderEnvSpec{
			// Cline takes credentials through its own ACP `authenticate`
			// (cline / cline-pass = device-code OAuth, openai-codex = ChatGPT
			// subscription), and it can also read a provider straight from
			// settings/providers.json. That file was measured NOT to drive the
			// session: with only an openai-compatible entry present, session/new
			// still resolved the account provider (cline) and its 314-model
			// catalog, and the session's provider option stayed "cline".
			//
			// So a BYOK binding here would be accepted at bind time and then
			// silently ignored on every turn, which is exactly what an empty
			// spec exists to prevent. Account login is the supported route.
			//
			// (CLINE_API_KEY is not declared for the same reason: it only
			// satisfies the "is the account signed in" gate and is not a
			// provider credential.)
		},
		ListSessions: listClineSessionsFromDisk,
		OrphanCmdline: ai.OrphanCmdlinePattern{
			Binary: "cline",
			Arg:    "--acp",
		},
	})
}
