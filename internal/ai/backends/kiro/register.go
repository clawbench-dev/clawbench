package kiro

import (
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	// Kiro CLI is ACP-only for ClawBench: `acp` is a hidden subcommand (it does
	// not appear in `kiro-cli --help`) that serves newline-delimited JSON-RPC
	// 2.0 over stdio.
	//
	// `--agent-engine=v3` is explicit on purpose. The flag overrides a saved
	// `acp.agentEngine` setting; a saved v2 would otherwise start the old
	// harness, whose flag set and slash-command surface differ. Leaving the
	// flag off entirely lets the CLI decide (docs: v3 on capable systems, v2
	// for clients it recognizes as v2-only), which is not a decision this
	// backend should delegate. Measured on 3.5.0: v3 and v2 answer initialize
	// identically, so the difference is only visible once a turn can run.
	backends.Register(&backends.BackendPlugin{
		ID: "kiro",
		Spec: model.BackendSpec{
			ID: "kiro", Backend: "kiro", DefaultCmd: "kiro-cli", Name: "Kiro", Specialty: "AWS Kiro 编码代理",
			AcpCommand: "kiro-cli acp --agent-engine=v3",
			// Measured on 3.5.0: the authenticated handshake reports
			// loadSession=true, so the explicit acp-load endpoint works. This
			// replaces (not supplements) the report, so it must match it.
			ACPLoadSession: true,
			// Without credentials every request after initialize answers -32000
			// ("Not logged in"). Naming the login renders it as an instruction
			// instead of a raw JSON-RPC blob. KIRO_API_KEY is the alternative
			// path — see ProviderEnv below.
			LoginCmd:   "kiro-cli login",
			InstallCmd: "curl -fsSL https://cli.kiro.dev/install | bash",
			SortOrder:  25,
			// Measured on 3.5.0 (marker SKILL.md dropped in both places, ACP
			// session started): `_kiro/progressive_context/items_changed`
			// reported BOTH as type:"skill" at session start —
			// `~/.kiro/skills/<name>/SKILL.md` with scope "global"/origin
			// "user", and `<project>/.kiro/skills/<name>/SKILL.md` with scope
			// "workspace"/origin "workspace" (root = cwd). The binary also
			// carries `skill://.kiro/skills/*/SKILL.md` as a
			// DEFAULT_AGENT_RESOURCES entry and a dedicated prompt builder
			// (crates/agent/src/agent/prompts/skills.rs), and `Skills` is a
			// UserContent variant — i.e. skill text goes into the prompt, not
			// only behind a tool call. There is NO `.agents/skills` support
			// (zero occurrences in the binary), so it is not listed.
			//
			// Only the home-relative directory belongs here: the workspace one
			// is per-project and Kiro resolves it from the session cwd itself.
			NativeSkillsDirs:      []string{".kiro/skills"},
			AutoLoadsNativeSkills: true,
			// NOTE MCP is NOT declared MCPUnsupported: the authenticated
			// handshake advertises mcpCapabilities {http:true, sse:true} and
			// session/new accepted an empty mcpServers without error, so
			// injection stays enabled. What was NOT measured is whether an
			// injected server's tools actually show up mid-turn (no working
			// account during the probe).
		},
		// The model catalog is SERVER-SIDE, not a local command: measured on
		// 3.5.0, `kiro-cli chat --list-models --format json` answers
		// "Not logged in" without credentials and "error: service error" with
		// a key that the service rejects, and the ACP turn itself fails with
		// ModelRegistryAccessDeniedError. So discovery must never be a CLI
		// probe — see catalog.go.
		ModelSource: model.StaticSource("kiro", "kiro-cli", KiroCatalog),
		// KIRO_API_KEY is the authoritative credential: a non-empty value is
		// what turns the handshake from "login required" into the full
		// capability set (measured both ways on 3.5.0). It is the user's Kiro
		// ACCOUNT key, not a BYOK credential — the endpoint cannot be changed,
		// the harness always talks to Kiro's own service.
		//
		// No OwnServiceEnv on purpose: that field only matters for the
		// "private" protocol (ProviderOwnServiceMode), and Kiro has no
		// endpoint/edition concept to hide, so it would only add a second,
		// meaningless entry to the provider picker.
		//
		// Kiro's session/new returns configOptions
		// {id:"autopilot", currentValue:"on"} — on = "Agent executes tools
		// without confirmation", off = "Supervised". Left alone, Kiro runs
		// tools without asking and ClawBench's permission cards
		// (session/request_permission) never appear, which is the opposite of
		// every other backend.
		//
		// Measured on 3.5.0 (docs/dev/kiro_acp_probe_results.md §7.1):
		// session/set_config_option(configId:"autopilot", value:"off") WORKS
		// (the result restates autopilot.currentValue="off", plus a
		// config_option_update notification), but only AFTER session/new
		// (before it: -32000 "Session ... not found") and only for that
		// session — a fresh session is back to "on". There is no settings key,
		// CLI flag or env var, so InitialConfigOptions is the only lever.
		ProviderEnv: &model.ProviderEnvSpec{
			KeyOnly: "KIRO_API_KEY",
		},
		// Every new session starts Supervised. The host applies this right
		// after session/new and only when the agent advertised the option (an
		// unauthenticated Kiro advertises almost nothing, and sending it then
		// would only fail).
		ACP: &backends.ACPPlugin{
			InitialConfigOptions: map[string]string{"autopilot": "off"},
		},
	})
}
