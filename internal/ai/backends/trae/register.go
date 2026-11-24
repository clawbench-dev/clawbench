package trae

import (
	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

// TraeACPCommand is the ACP entry point. UNVERIFIED: taken from the third-party
// adapter's README (@tunnelbox/trae, which drives `traecli acp serve`), not from
// a frame captured against a real binary — see
// docs/dev/trae_acp_probe_results.md.
const TraeACPCommand = "traecli acp serve"

func init() {
	// TraeCode CLI is ACP-only here: `traecli acp serve` speaks Agent Client
	// Protocol over stdio.
	//
	// NOTHING in this registration has been run against a real traecli. The CLI
	// is gated to TRAE Enterprise Flagship customers, is not published on npm,
	// and the official docs URL named by the adapter 404s, so no binary could
	// be obtained or installed during the probe. Every field below is either
	// the naming convention fixed by the project or a fact repeated from the
	// third-party adapter; anything the adapter did not state is left at zero
	// (unset) rather than guessed.
	backends.Register(&backends.BackendPlugin{
		ID: "trae",
		Spec: model.BackendSpec{
			ID: "trae", Backend: "trae", DefaultCmd: "traecli", Name: "TRAE",
			Specialty:  "字节 TRAE 企业版编码智能体",
			AcpCommand: TraeACPCommand,
			// ACPLoadSession deliberately left false (unset): the agent's
			// `agentCapabilities.loadSession` is unknown. Unset means the
			// explicit acp-load endpoint answers 501 instead of sending a
			// method the agent may not implement.
			//
			// InstallCmd deliberately unset: there is no public install path
			// (npm has no `traecli`, the docs page is gone, and the binary is
			// enterprise-only). Inventing a command would show a broken button.
			//
			// LoginCmd: the adapter states sessions expect a prior
			// `traecli login` and its ACP `authenticate` is optional. Naming it
			// renders -32000 AUTH_REQUIRED as an actionable instruction.
			LoginCmd:  "traecli login",
			SortOrder: 26,
		},
		// No ACPPlugin: no evidence of any tool-name/title/field divergence
		// from the shared generic mapping. If a real run shows non-canonical
		// tool ids, add ToolCallIDPrefixes here.
		//
		// No Behavior override: the adapter claims the standard ACP surface
		// (agent_message_chunk / agent_thought_chunk / request_permission /
		// cancel), which the shared parsers already cover.
		ModelSource: model.PluginSource("trae", discoverTraeModels),
		// Deliberately empty: enterprise subscription, no BYOK path is known.
		ProviderEnv: &model.ProviderEnvSpec{},
		// Fallback orphan recognition for crashed `traecli acp serve` processes.
		// Derived from the assumed launch shape, not verified.
		OrphanCmdline: ai.OrphanCmdlinePattern{Binary: "traecli", Arg: "acp serve"},
	})
}
