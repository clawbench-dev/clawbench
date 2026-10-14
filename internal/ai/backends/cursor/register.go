package cursor

import (
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	// Cursor (Anysphere) ships an ACP server as a first-class subcommand of its
	// own CLI: `agent acp` ("Start the Cursor Agent as an ACP (Agent Client
	// Protocol) server"). `cursor-agent` is the legacy alias for the same
	// binary. There is no CLI transport, so no ai.RegisterBackend (CLI factory)
	// is registered and chat always goes through the ACP stdio transport.
	//
	// Do NOT wire the third-party npm adapter `cursor-agent-acp`: it writes the
	// child's stdin without ever closing it, while `cursor-agent -p ""` waits
	// for EOF before issuing the request, so the child blocks forever and the
	// adapter returns a hardcoded fake `end_turn` with zero output. The official
	// subcommand has none of these problems (verified end-to-end).
	//
	// No ACPPlugin: cursor's tool-card title/id shapes have not been captured
	// yet, so tools fall back to the shared lowercase-alias table plus the
	// generic remaps (same posture as dsh).
	//
	// Credentials are NOT negotiated over ACP (cursor advertises only a
	// `cursor_login` authMethod and no config is needed): CURSOR_API_KEY must
	// already be in the launching environment, or the CLI's own `agent login`
	// store must be present. cursor does NOT read ANTHROPIC_BASE_URL /
	// ANTHROPIC_API_KEY.
	backends.Register(&backends.BackendPlugin{
		ID: "cursor",
		Spec: model.BackendSpec{
			ID: "cursor", Backend: "cursor",
			// No AltCmd: the CLI also installs a bare `agent` symlink, but `agent`
			// is NOT safe as a detection alias — the Grok CLI ships a binary of
			// the same name (~/.grok/bin/agent), and probeBackendPresence only
			// checks `--version` exit status, so a Grok-only user would get a
			// phantom Cursor agent whose `agent acp` spawns Grok. `cursor-agent`
			// is the unambiguous name and the ACP command uses it for the same
			// reason.
			DefaultCmd: "cursor-agent",
			Name:       "Cursor", Specialty: "Cursor 官方编码智能体",
			AcpCommand:     "cursor-agent acp",
			ACPLoadSession: true, // initialize advertises loadSession:true
			InstallCmd:     "curl https://cursor.com/install -fsS | bash",
			SortOrder:      9,
			// cursor loads its own skills from all of these home directories
			// (verified empirically with marker SKILL.md files: every marker in
			// .cursor/skills, .claude/skills, .codex/skills, .grok/skills and
			// .agents/skills was reported back, and skills surface as ACP slash
			// commands). Declaring them all keeps ClawBench from injecting a
			// second copy.
			NativeSkillsDirs: []string{
				".cursor/skills", ".claude/skills", ".codex/skills",
				".grok/skills", ".agents/skills",
			},
			AutoLoadsNativeSkills: true,
			// ThinkingEffortLevels intentionally nil. Cursor's session/new
			// advertises `mode` and `model` config options and encodes effort
			// inside the model id (`...[effort=high]`). A `thought_level` option
			// can be derived when the SELECTED model defines a thinking/reasoning
			// /effort parameter, but that is model-dependent — so a static level
			// list here would surface a control that is dead for the default
			// model. When cursor does advertise thought_level, the frontend's
			// getAgentThinkingEffortLevelsIncludingACP picks it up at runtime.
		},
	})
}
