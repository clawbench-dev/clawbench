package openclaw

import (
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	// OpenClaw (openclaw) is ACP-only via its built-in `acp` subcommand: no
	// ai.RegisterBackend (CLI factory) is registered, so chat always uses the
	// ACP stdio transport.
	//
	// Verified against openclaw 2026.10.6 (probe:
	// internal/ai/openclaw_acp_probe_integration_test.go, 7/7 pass):
	// initialize advertises loadSession + sessionCapabilities{list,resume,close}
	// and promptCapabilities{image:true, audio:false, embeddedContext:true}.
	//
	// **`openclaw acp` is a BRIDGE, not an agent.** It forwards every prompt to a
	// running OpenClaw Gateway over WebSocket (default ws://127.0.0.1:18789).
	// Consequences that shaped this spec:
	//
	//  1. The Gateway must already be running. With it down the bridge prints
	//     "ACP bridge failed: connect ECONNREFUSED ..." to stderr and exits, so
	//     the client only sees an initialize timeout — there is no protocol-level
	//     error to translate. `openclaw gateway start` is the fix, which is what
	//     LoginCmd names below.
	//  2. Per-session mcpServers are REFUSED (session/new fails), so openclaw is
	//     in model.mcpUnsupportedBackends. Its MCP must be configured on the
	//     Gateway/agent instead.
	//  3. session/resume only resolves ids created by the CURRENT bridge process;
	//     after a restart the same id must be recovered with session/load, which
	//     DOES restore history from the Gateway. Hence ACPResumeIsProcessLocal.
	//  4. session/new advertises `modes` that mirror the thought_level config
	//     option value-for-value, and writing a mode is rejected. Hence
	//     ACP.ModesMirrorThoughtLevel so only the effort picker is shown.
	backends.Register(&backends.BackendPlugin{
		ID: "openclaw",
		Spec: model.BackendSpec{
			ID:         "openclaw",
			Backend:    "openclaw",
			DefaultCmd: "openclaw",
			Name:       "OpenClaw",
			Specialty:  "网关式通用智能体",
			// `openclaw acp` starts the ACP stdio bridge (stdout is JSON-RPC).
			AcpCommand: "openclaw acp",
			// The bridge advertises loadSession and implements session/load
			// (verified across process restarts: the replayed history restored a
			// seeded secret), so the explicit acp-load / acp-sync endpoints work.
			ACPLoadSession: true,
			// session/resume is process-scoped: a fresh bridge answers the old id
			// with -32603 "Session ... not found", which the generic recovery path
			// treats as unrecoverable (it must not silently start a fresh session).
			// Without this flag every Gateway/bridge restart would strand the
			// conversation.
			ACPResumeIsProcessLocal: true,
			// Node >= 24.22.1 (or >= 26.12.1) is required by the package.
			InstallCmd: "npm install -g openclaw",
			// The Gateway is a separate long-running service; without it the
			// bridge cannot answer initialize at all. Naming the start command here
			// is what turns the opaque initialize timeout into an instruction.
			LoginCmd:  "openclaw gateway start",
			SortOrder: 19,
			// OpenClaw loads skills from several roots (verified with marker
			// SKILL.md files): <workspace>/skills, <workspace>/.agents/skills,
			// ~/.agents/skills (default state dir only), <state-dir>/skills
			// (= ~/.openclaw/skills) and the per-agent workshop-skills dir.
			// Declaring the two user-level ones keeps ClawBench from injecting a
			// second copy of the same table.
			//
			// The workspace-relative roots are NOT declarable here (they are
			// relative to the Gateway workspace, not the home dir), and a
			// project-level .agents/skills is always injected by ClawBench by
			// design — so a project skill may appear twice for openclaw. That is
			// the documented project-skill behavior, not a bug in this list.
			NativeSkillsDirs:      []string{".openclaw/skills", ".agents/skills"},
			AutoLoadsNativeSkills: true,
			// ThinkingEffortLevels is intentionally nil: the bridge advertises a
			// thought_level config option whose accepted values are MODEL-dependent
			// (measured: minimax accepts off/minimal/low/medium/high but refuses
			// "adaptive" and "ultra" even though both are advertised). A static
			// list would surface levels the current model rejects. The frontend
			// picks the option up at runtime, same posture as cursor/hermes.
		},
		ACP: &backends.ACPPlugin{
			ToolTitlePrefixes: OpenClawACPTitlePrefixes,
			InputRemaps:       OpenClawACPInputRemaps,
			// Every plugin/MCP tool is preceded by an envelope frame whose title
			// is "tool_call: id: <tool>, args: {...}". It carries a different
			// toolCallId than the real frame, so only the title can identify it.
			ToolCallWrapperTitlePrefixes: []string{"tool_call: id: "},
			// modes == thought_level value-for-value, and set_config_option(mode)
			// is refused with "ACP bridge mode does not support session config
			// option \"mode\"". Suppress the mode surface; keep the effort picker.
			ModesMirrorThoughtLevel: true,
			// ModelSwitchMethod is empty on purpose: the bridge exposes NO model
			// selection over ACP at all (no "model" config option, and
			// session/set_model answers -32601 Method not found). Models are
			// configured on the Gateway. Do not add a raw-RPC switch here.
		},
	})
}
