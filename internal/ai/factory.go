package ai

import (
	"fmt"
	"log/slog"

	"clawbench/internal/model"
)

// NewBackend creates a backend instance based on the backend type.
// For agents with ACP transport configured, use NewBackendForAgent instead.
func NewBackend(backendType string) (AIBackend, error) {
	switch backendType {
	case "claude":
		return &AutoResumeBackend{inner: claudeBackend}, nil
	case "codebuddy":
		return &AutoResumeBackend{inner: codebuddyBackend}, nil
	case "opencode":
		return opencodeBackend, nil
	case "gemini":
		return geminiBackend, nil
	case "codex":
		return &CodexBackend{}, nil
	case "qoder":
		return &AutoResumeBackend{inner: qoderBackend}, nil
	case "vecli":
		return NewVeCLIBackend(), nil
	case "deepseek":
		return &AutoResumeBackend{inner: deepseekBackend}, nil
	case "pi":
		return &AutoResumeBackend{inner: piBackend}, nil
	case "cline":
		return &AutoResumeBackend{inner: clineBackend()}, nil
	case "kimi":
		return &AutoResumeBackend{inner: kimiBackend()}, nil
	case "copilot":
		return &AutoResumeBackend{inner: copilotBackend()}, nil
	case "mimo":
		return &AutoResumeBackend{inner: mimoBackend}, nil
	default:
		return nil, fmt.Errorf("unsupported backend type: %s (supported: claude, codebuddy, opencode, gemini, codex, qoder, vecli, deepseek, pi, cline, kimi, copilot, mimo)", backendType)
	}
}

// NewBackendForAgent creates a backend instance for the given agent.
// If the agent has ACP transport configured (acp-stdio), it creates
// an ACPBackend directly (no AutoResumeBackend wrapping — ACP uses session/cancel
// instead of process kill for stuck agents). Otherwise, it falls back to the
// CLI-based NewBackend.
//
// This is the preferred entry point when the agent ID is known (all handler paths).
func NewBackendForAgent(backendType, agentID string) (AIBackend, error) {
	return NewBackendForAgentWithTransport(backendType, agentID, "")
}

// NewBackendForAgentWithTransport creates a backend with an optional per-session
// transport override. If transportOverride is non-empty, it takes precedence over
// the agent's configured transport. Otherwise, falls back to the agent's Transport.
// If the override requests acp-stdio but the agent doesn't support it, falls back
// to CLI backend gracefully instead of erroring out.
func NewBackendForAgentWithTransport(backendType, agentID, transportOverride string) (AIBackend, error) {
	if agentID != "" {
		if agent, ok := model.Agents[agentID]; ok {
			effectiveTransport := transportOverride
			if effectiveTransport == "" {
				effectiveTransport = agent.Transport
			}
			if effectiveTransport == "acp-stdio" {
				if agent.SupportsACP() {
					acpBackend, err := NewACPBackend(agent)
					if err != nil {
						return nil, fmt.Errorf("acp backend for agent %q: %w", agentID, err)
					}
					return acpBackend, nil
				}
				// transport override says acp-stdio but agent doesn't support it;
				// fall through to CLI backend instead of erroring out.
				slog.Warn("agent does not support acp-stdio transport, falling back to CLI", "agentID", agentID)
			}
		}
	}

	// Fall back to CLI backend (with AutoResumeBackend for ExitPlanMode agents)
	return NewBackend(backendType)
}

// needsAutoResume returns true if the backend type should be wrapped in
// AutoResumeBackend for ExitPlanMode detection (CLI mode only).
func needsAutoResume(backendType string) bool {
	switch backendType {
	case "claude", "codebuddy", "qoder", "deepseek", "pi", "cline", "kimi", "copilot", "mimo":
		return true
	default:
		return false
	}
}
