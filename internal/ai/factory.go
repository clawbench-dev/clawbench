package ai

import (
	"fmt"

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
	default:
		return nil, fmt.Errorf("unsupported backend type: %s (supported: claude, codebuddy, opencode, gemini, codex, qoder, vecli, deepseek, pi, cline, kimi, copilot)", backendType)
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
	// Check if the agent has ACP transport configured
	if agentID != "" {
		if agent, ok := model.Agents[agentID]; ok {
			if agent.Transport == "acp-stdio" {
				acpBackend, err := NewACPBackend(agent)
				if err != nil {
					return nil, fmt.Errorf("acp backend for agent %q: %w", agentID, err)
				}
				// ACP does NOT need AutoResumeBackend:
				// - session/cancel can cancel a stuck turn without killing the process
				// - RequestPermission auto-approves (no ExitPlanMode hang)
				// - session/set_mode can switch plan/code mode without restart
				return acpBackend, nil
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
	case "claude", "codebuddy", "qoder", "deepseek", "pi", "cline", "kimi", "copilot":
		return true
	default:
		return false
	}
}
