package pi

import (
	"log/slog"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

// PiInputRemaps maps Pi CLI input field names to canonical names.
// Injected into PiStreamParser at construction time.
var PiInputRemaps = map[string]string{
	"path": "file_path",
}

// PiACPInputRemaps is the ACP remap table for Pi.
//
// It is intentionally minimal and mirrors PiInputRemaps: pi-acp forwards Pi's
// native tool arguments, and the tool-aware normalization (including the nested
// edits[] array) lives in internal/ai's Pi-specific ACP parser
// (parsePiACPToolCall → normalizePiACPInput), which reuses the same
// normalizePiToolInput the CLI path uses. Registering the table here keeps the
// backend's declared remaps inspectable, and LookupACPRemaps falls back to the
// generic table when a backend has no ACP plugin.
//
// Do NOT add oldText/newText here: the generic ACP normalizer is a flat
// single-pass remap that never recurses into edits[], so those entries would be
// dead. They are handled by the Pi-specific parser instead.
var PiACPInputRemaps = map[string]string{
	"path": "file_path",
}

func init() {
	ai.RegisterBackend("pi", newPiBackend)
	backends.Register(&backends.BackendPlugin{
		ID: "pi",
		Spec: model.BackendSpec{
			ID: "pi", Backend: "pi", DefaultCmd: "pi", Name: "Pi", Specialty: "极简编程智能体",
			ThinkingEffortLevels: []string{"off", "minimal", "low", "medium", "high", "xhigh"},
			// ACP via the upstream pi-acp bridge. Pi was previously CLI-only
			// because the adapter originally used (@touchtechclub/pi-acp) stopped
			// being maintained; upstream svkozak/pi-acp is active and passes the
			// full ACP integration suite (see internal/ai/acp_integration_test.go,
			// backend "pi").
			//
			// ACPLoadSession is TRUE: pi-acp advertises loadSession and implements
			// session/load, so the explicit acp-load / acp-sync endpoints work.
			// This flag is about session/load only — pi-acp does NOT implement the
			// non-standard session/resume RPC (it answers -32601), which is a
			// separate capability that affects automatic crash recovery. That path
			// falls back to session/load on -32601, so no flag is needed for it.
			AcpCommand:     "npx -y pi-acp@latest",
			ACPLoadSession: true,
			InstallCmd:     "npm install -g @earendil-works/pi-coding-agent",
			SortOrder:      8,
			// Pi loads its own skills: its docs state it "Supports Claude Code
			// (~/.claude/skills/*/SKILL.md), Codex CLI (~/.codex/skills/), and
			// Pi-native formats (~/.pi/agent/skills/, .pi/skills/)" and it emits
			// an <available_skills> block. Only its own directory is declared
			// here; .claude/skills and .codex/skills are covered by those
			// backends' declarations, and Pi does NOT read .agents/skills.
			NativeSkillsDirs:      []string{".pi/agent/skills"},
			AutoLoadsNativeSkills: true,
		},
		ACP: &backends.ACPPlugin{
			InputRemaps: PiACPInputRemaps,
		},
	})
}

// newPiBackend returns a CLIBackend instance configured for Pi CLI.
func newPiBackend() ai.AIBackend {
	return &ai.CLIBackend{
		BackendName: "pi",
		Cmd:         "pi",
		BuildArgsFn: buildPiStreamArgs,
		NewParserFn: func() ai.LineParser {
			return &ai.PiStreamParser{InputRemaps: PiInputRemaps}
		},
		FilterLineFn: nil,
		PreStartFn:   nil,
	}
}

// buildPiStreamArgs constructs the CLI arguments for Pi streaming.
//
// Command: pi -p --mode json [flags] "prompt"
//
// Supported flags:
//
//	--session <id>              Resume a specific session
//	--continue                  Continue the most recent session
//	--no-session                Start a new session (no persistence)
//	--no-context-files          Skip AGENTS.md / CLAUDE.md discovery
//	--append-system-prompt <text> Append to Pi's built-in system prompt
//	--model <model>             Override model
//
// Working directory is set via cmd.Dir (CLIBackend sets cmd.Dir = req.WorkDir),
// not via a CLI flag — Pi does not have a --add-dir option.
func buildPiStreamArgs(req ai.ChatRequest) []string {
	args := []string{"-p", "--mode", "json"}

	// Session management
	switch {
	case req.Resume && req.SessionID != "":
		// Resume a specific session by its Pi-assigned ID (captured via
		// external_session_id). This allows conversation continuity.
		args = append(args, "--session", req.SessionID)
		slog.Info("cli: --session resume (pi)",
			slog.String("session_id", req.SessionID))
	case req.Resume:
		// Resume without a known session ID — continue the most recent session.
		args = append(args, "--continue")
		slog.Warn("cli: --continue fallback (pi, session_id missing)",
			slog.String("backend", "pi"))
	case req.ScheduledExecution:
		// Tasks are independent executions — no need to persist sessions.
		args = append(args, "--no-session")
	}
	// Default: new interactive session without --no-session so Pi creates
	// a persistent session whose ID can be captured for future resumption.

	// Skip AGENTS.md / CLAUDE.md discovery — ClawBench injects its own rules
	args = append(args, "--no-context-files")

	// System prompt — use --append-system-prompt to preserve Pi's built-in prompt
	if req.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", req.SystemPrompt)
	}

	// Model override
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}

	// Thinking effort level (e.g., --thinking high)
	if req.ThinkingEffort != "" {
		args = append(args, "--thinking", req.ThinkingEffort)
	}

	// Prompt is the last positional argument
	args = append(args, req.Prompt)

	return args
}
