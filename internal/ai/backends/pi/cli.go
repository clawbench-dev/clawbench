package pi

import (
	"fmt"
	"log/slog"
	"os/exec"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

// PiInputRemaps maps Pi CLI input field names to canonical names.
// Injected into PiStreamParser at construction time.
var PiInputRemaps = map[string]string{
	"path": "file_path",
}

func init() {
	ai.RegisterBackend("pi", newPiBackend, true)
	backends.Register(&backends.BackendPlugin{
		ID: "pi",
		Spec: model.BackendSpec{
			ID: "pi", Backend: "pi", DefaultCmd: "pi", Name: "Pi", Icon: "🥧", Specialty: "极简编程智能体",
			ThinkingEffortLevels: []string{"off", "minimal", "low", "medium", "high", "xhigh"},
			SortOrder:            8,
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
		FilterLineFn:  nil,
		PreStartFn:    nil,
		PreExecHookFn: injectPiAPIKey,
	}
}

// injectPiAPIKey loads the encrypted API key for the Pi agent from the database
// and injects it as an environment variable on the CLI command.
// Also adds the --provider flag so Pi knows which provider config to use.
// If the agent has no stored API key, this is a no-op (Pi falls back to auth.json).
func injectPiAPIKey(cmd *exec.Cmd, req ai.ChatRequest) {
	loader := ai.GetAgentAPIKeyLoader()
	if loader == nil {
		return
	}

	agent, ok := model.Agents[req.AgentID]
	if !ok {
		return
	}

	// Only inject for Pi backend
	if agent.Backend != "pi" {
		return
	}

	// Find the provider and API key for this agent — single DB query
	provider, customURL, apiKey, found := loader(req.AgentID)
	if !found || apiKey == "" {
		return // No stored API key — Pi will fall back to auth.json
	}

	// Custom URL mode: provider is the agent ID (set by setup complete).
	// Use --provider {agentID} + --api-key so Pi reads models.json for the endpoint.
	if customURL != "" {
		cmd.Args = append(cmd.Args[:len(cmd.Args)-1], "--provider", provider, "--api-key", apiKey, cmd.Args[len(cmd.Args)-1])
		slog.Debug("injected custom URL API key for agent", "agent_id", req.AgentID, "provider", provider)
		return
	}

	// Built-in provider mode: inject env var + --provider flag
	spec := model.FindProviderSpec(provider)
	if spec != nil && spec.EnvVar != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", spec.EnvVar, apiKey))
		// Add --provider flag to Pi CLI args
		cmd.Args = append(cmd.Args[:len(cmd.Args)-1], "--provider", provider, cmd.Args[len(cmd.Args)-1])
	}

	slog.Debug("injected API key for agent", "agent_id", req.AgentID, "provider", provider)
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
