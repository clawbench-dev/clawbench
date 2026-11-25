package minimax

import (
	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	// MiniMax Code is ACP-only: `mcode acp` runs a standard Agent Client
	// Protocol server over stdio (probe: docs/dev/minimax_acp_probe_results.md,
	// measured on mcode 0.7.9 / 2026-11-24). There is no CLI transport, so no
	// CLI factory is set and chat always goes through the ACP stdio transport.
	//
	// DefaultCmd is `mcode`: that is the npm bin name (package @minimax-ai/code
	// installs `mcode` and `mcode-tools`). The product's own data directory is
	// `~/.minimax`, hence the backend id.
	//
	// Credentials are NOT negotiated over ACP (initialize advertises no
	// authMethods). The runtime accepts either a managed login or a BYOK
	// provider declared in its config.yaml through `mcode provider add
	// --api-key-env`, which stores a REFERENCE to an environment variable and
	// never the key. Without one of those, session/new answers -32000
	// "Authentication required: Run `mcode login` and try again.", so the login
	// command is named here to render that error as an instruction.
	backends.Register(&backends.BackendPlugin{
		ID: "minimax",
		Spec: model.BackendSpec{
			ID: "minimax", Backend: "minimax", DefaultCmd: "mcode", Name: "MiniMax Code",
			Specialty: "MiniMax 官方编码智能体",
			// Measured: initialize advertises loadSession:true plus
			// sessionCapabilities {list, fork, resume, close}, and
			// promptCapabilities {image:true, audio:false, embeddedContext:false}.
			AcpCommand:     "mcode acp",
			ACPLoadSession: true,
			InstallCmd:     "npm install -g @minimax-ai/code",
			LoginCmd:       "mcode login",
			SortOrder:      24,
			// MCP is supported and attaches synchronously enough for the first
			// turn: with an injected stdio server the model called
			// `mcp__probe__clawbench_mcp_probe_ping` in the very first turn and
			// returned its marker, and without it the tool did not exist (same
			// two-group probe as docs/dev/acp_mcp_probe_results.md).
			//
			// ThinkingEffortLevels is intentionally nil: effort is an ACP config
			// option (category "thought_level") whose levels depend on the
			// selected provider/model (only some models advertise effortOptions),
			// so a static list would surface a control that is dead for most
			// models. The frontend picks it up at runtime, like cursor/hermes.
			//
			// The runtime loads its own skills from <home>/.minimax/skills
			// (the bundle resolves that path next to the data dir) and also
			// registers them as slash commands — measured: the user's own
			// skills appeared in availableCommands. Declaring the directory is
			// what keeps ClawBench from injecting a second copy of the same
			// table.
			//
			// Same relocation caveat as pi (.pi/agent/skills with
			// PI_CODING_AGENT_DIR): MINIMAX_DATA_DIR moves the tree, and a
			// home-relative declaration then points at the default location.
			// The default is what almost every install uses, so it is declared
			// rather than leaving the common case duplicated.
			NativeSkillsDirs:      []string{".minimax/skills"},
			AutoLoadsNativeSkills: true,
		},
		// No ACP mapping data: every tool frame observed names the tool in both
		// `name` and `title` as a lowercase word ("read"/"write"/"bash"), which
		// the shared lowercase alias table already maps, and rawInput uses
		// path/content/command — either canonical or covered by the shared
		// command/exec remap.
		//
		// The private `_meta` extensions (mcode/session/steer, queue/*, goal/*,
		// delegation/*) are deliberately NOT wired: steering is the only one
		// that overlaps existing host machinery (MidTurn), and its echo
		// semantics have not been measured, so mapping it now could cut a reply
		// in the wrong place.
		//
		// The runtime has no standalone "list models" command that works without
		// a session: `mcode provider list --json` lists providers (models only
		// for custom ones) and the default-model catalog ships inside the
		// package, so there is nothing worth shelling out for. The ACP
		// session/new `model` config option is authoritative at runtime and
		// ResolveModels lets it win; the catalog below only fills the picker
		// before the first handshake.
		ModelSource: model.StaticSource("minimax", "mcode", MiniMaxCatalog),
		ProviderEnv: &model.ProviderEnvSpec{
			// mcode takes a provider only through its own config file (`mcode
			// provider add --base-url … --api-key-env …` writes config.yaml,
			// storing a reference to the env var, not the key). It does NOT read
			// a base-url/API-key environment pair: setting MINIMAX_API_KEY alone
			// was measured to have no effect (`mcode exec` still answers "Sign in
			// to MiniMax"), and the one appearance of that name in the package is
			// a mapping table for OTHER vendors, not mcode's own credential
			// entry. Declaring an env entry here would let the UI offer a binding
			// that silently keeps talking to MiniMax's own endpoint, so the honest
			// answer is "cannot take a provider".
			//
			// TODO(provider): if MINIMAX_DATA_DIR reliably relocates the whole
			// config tree (it did here: a fresh directory got its own
			// config.yaml, auth/ and v2/), this can become ConfigDirEnv +
			// ConfigDirFile with a ConfigFileApply hook writing custom_provider.*
			// and defaultModel. Not done in this pass: no restore hook and no
			// end-to-end turn have been verified.
		},
		// The cmdline fallback for orphan cleanup. Both substrings are required
		// and both must describe `mcode acp` specifically: "mcode" alone would
		// also match `mcode exec`, and a bare "acp" would match other tools.
		// (The primary signal is still the CLAWBENCH_CHILD marker; this only
		// covers a child spawned by an older build or one whose environ we
		// cannot read.)
		OrphanCmdline: ai.OrphanCmdlinePattern{Binary: "mcode", Arg: "acp"},
	})
}
