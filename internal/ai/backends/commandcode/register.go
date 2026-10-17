package commandcode

import (
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	// Command Code (Langbase) is ACP-only via its built-in `acp` subcommand:
	// no ai.RegisterBackend (CLI factory) is registered, so chat always uses the
	// ACP stdio transport.
	//
	// Verified against command-code 1.84.0 (probe:
	// internal/ai/commandcode_acp_probe_integration_test.go, 8/8 pass):
	// initialize advertises loadSession + sessionCapabilities{list,resume,close}
	// and promptCapabilities{audio:false,image:true,embeddedContext:true}.
	//
	// DefaultCmd is `cmdc`, NOT `cmd`: on Windows the bare name `cmd` always
	// resolves (the system shell), so probeBackendPresence would create a phantom
	// Command Code agent for every Windows user and then spawn `cmd.exe acp`.
	// `cmdc` is one of the four bins the package ships (cmd / cmdc / commandcode /
	// command-code, all pointing at the same entry) and works on Linux/macOS too
	// (verified: `cmdc --version` → 1.84.0, `cmdc acp --help` → ACP help).
	//
	// Credentials: the ACP gate only checks that an API key is NON-EMPTY
	// (runAcp's isAuthenticated = null !== getCommandAuthKey(...)), so either
	// `cmd login` once, or set COMMAND_CODE_API_KEY. A session without either
	// fails session/new with -32000 AUTH_REQUIRED ("Not authenticated. Run
	// `cmd login` in a terminal first.") — that code must be surfaced to the user
	// as a login instruction, not as a generic backend error.
	backends.Register(&backends.BackendPlugin{
		ID: "commandcode",
		Spec: model.BackendSpec{
			ID: "commandcode", Backend: "commandcode",
			DefaultCmd: "cmdc",
			AltCmd:     "commandcode",
			Name:       "Command Code",
			Specialty:  "持续学习编码风格的智能体",
			AcpCommand: "cmdc acp",
			// initialize advertises loadSession:true and implements session/load,
			// so the explicit acp-load / acp-sync endpoints work.
			ACPLoadSession: true,
			// "npm install" (not "npm i"): prepareInstallCmd only appends the
			// mainland-China registry mirror when the command starts with that
			// exact prefix, so the short form silently bypasses it.
			InstallCmd: "npm install -g command-code",
			// Sessions are gated on a signed-in account: without one,
			// session/new answers -32000 AUTH_REQUIRED. Naming the login command
			// here lets the AUTH_REQUIRED error render as an actionable
			// instruction instead of a raw JSON-RPC blob.
			LoginCmd:  "cmdc login",
			SortOrder: 18,
			// Command Code reads its own skills from both the user-level
			// ~/.commandcode/skills and the shared ~/.agents/skills (cli.mjs
			// resolveSkillLoadPlan adds both at the "user" tier), plus the
			// project-level counterparts. Declaring them keeps ClawBench from
			// injecting a second copy. Unlike Hermes, .agents/skills IS read by
			// default here, so it must be declared.
			NativeSkillsDirs:      []string{".commandcode/skills", ".agents/skills"},
			AutoLoadsNativeSkills: true,
			// ThinkingEffortLevels is intentionally nil: Command Code advertises
			// effort as an ACP config option (category "thought_level") whose
			// levels depend on the selected model, so a static list would surface
			// a control that is dead for the default model. The frontend picks it
			// up at runtime (same posture as cursor/hermes).
		},
		ACP: &backends.ACPPlugin{
			ToolTitlePrefixes: CommandCodeACPTitlePrefixes,
			InputRemaps:       CommandCodeACPInputRemaps,
			// ModelSwitchMethod is deliberately empty. Command Code honors BOTH
			// session/set_model and the "model" config option, and its
			// handleSetConfigOption routes configId="model" straight into
			// switchModel — the same function session/set_model calls. So the
			// default config-option path is correct and the raw RPC is redundant.
			//
			// Caveat for BYOK users: the advertised model list is always Command
			// Code's own 92-model catalog (availableModels(){return
			// catalogModels()}), so a self-hosted provider's models never appear
			// and cannot be selected here. A BYOK setup must pin the model in
			// ~/.commandcode/config.json instead (model + modelProvider).
		},
	})
}
