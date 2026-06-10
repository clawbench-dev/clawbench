package ai

import "log/slog"

// piBackend is the CLIBackend instance for Pi CLI.
var piBackend = &CLIBackend{
	name:           "pi",
	defaultCommand: "pi",
	buildArgs:      buildPiStreamArgs,
	newParser:      func() LineParser { return &PiStreamParser{} },
	filterLine:     nil,
	preStart:       nil,
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
func buildPiStreamArgs(req ChatRequest) []string {
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
		// Scheduled tasks are independent executions — no need to persist sessions.
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
