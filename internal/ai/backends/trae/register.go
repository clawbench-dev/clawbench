package trae

import (
	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

// TraeACPCommand is the ACP entry point, verified against traecli 0.216.0:
// `traecli acp` lists exactly one subcommand, `serve` ("Start the ACP server
// over stdio").
const TraeACPCommand = "traecli acp serve"

// TraeInstallCmd is the official installer from
// https://docs.trae.cn/cli_get-started-with-trae-cli (ran it: installs 0.216.0
// into ~/.local/bin and adds it to PATH). Windows uses
// `irm https://trae.cn/trae-cli/install_v2.ps1 | iex`; the field holds ONE
// command, so it carries the macOS/Linux form — the same choice kiro makes.
const TraeInstallCmd = `sh -c "$(curl -fsSL https://trae.cn/trae-cli/install_v2.sh)"`

func init() {
	// TraeCode CLI is ACP-only here: `traecli acp serve` speaks Agent Client
	// Protocol over stdio.
	//
	// Measured on traecli 0.216.0 (public installer; see
	// docs/dev/trae_acp_probe_results.md): initialize answers
	// loadSession=true, sessionCapabilities {list, fork, resume, close,
	// additionalDirectories, systemPrompt}, promptCapabilities {image,
	// embeddedContext}, mcpCapabilities {http:true, sse:false}, and
	// authMethods {trae-tob-browser, trae-tob-pat}. session/new without a
	// credential answers -32000 "Authentication required" — the login is an
	// ENTERPRISE account (ToB), which is why the CLI is only useful to
	// enterprise customers even though the binary itself is public.
	backends.Register(&backends.BackendPlugin{
		ID: "trae",
		Spec: model.BackendSpec{
			ID: "trae", Backend: "trae", DefaultCmd: "traecli", Name: "TRAE",
			Specialty:  "字节 TRAE 企业版编码智能体",
			AcpCommand: TraeACPCommand,
			// initialize advertises loadSession:true (measured), so the
			// explicit acp-load endpoint works.
			ACPLoadSession: true,
			InstallCmd:     TraeInstallCmd,
			// `traecli login` is a real subcommand (with `login status`), and
			// -32000 AUTH_REQUIRED reads as an actionable instruction when the
			// command is named.
			LoginCmd:  "traecli login",
			SortOrder: 26,
			// The binary resolves its skill directory as
			// "${TRAE_HOME:-$HOME/.trae}/skills" (string in the shipped binary)
			// and exposes a `skills` session option. Declaring it keeps
			// ClawBench from injecting a second copy.
			NativeSkillsDirs:      []string{".trae/skills"},
			AutoLoadsNativeSkills: true,
		},
		// No ACPPlugin: no evidence of any tool-name/title/field divergence
		// from the shared generic mapping. If a real run shows non-canonical
		// tool ids, add ToolCallIDPrefixes here.
		//
		// No Behavior override: the adapter claims the standard ACP surface
		// (agent_message_chunk / agent_thought_chunk / request_permission /
		// cancel), which the shared parsers already cover.
		ModelSource: model.PluginSource("trae", discoverTraeModels),
		// KeyOnly, not BYOK: the endpoint cannot be changed. The value is the
		// enterprise login token, which the ACP auth method names explicitly
		// ("Requires TRAECLI_PERSONAL_ACCESS_TOKEN to be set before starting
		// TRAE CLI") — storing it here saves the user from exporting it by hand.
		ProviderEnv: &model.ProviderEnvSpec{
			KeyOnly: "TRAECLI_PERSONAL_ACCESS_TOKEN",
		},
		// Fallback orphan recognition for crashed `traecli acp serve` processes.
		// Derived from the assumed launch shape, not verified.
		OrphanCmdline: ai.OrphanCmdlinePattern{Binary: "traecli", Arg: "acp serve"},
	})
}
