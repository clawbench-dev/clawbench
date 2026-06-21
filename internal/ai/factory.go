package ai

import (
	"fmt"
	"log/slog"
	"sync"

	"clawbench/internal/model"
)

// backendFactory stores factory functions registered by backend plugins.
// Keyed by backend ID (e.g. "codebuddy").
var (
	backendFactories   = make(map[string]*BackendFactoryEntry)
	backendFactoriesMu sync.RWMutex
)

type BackendFactoryEntry struct {
	NewBackendFn    func() AIBackend // returns a new backend instance
	NeedsAutoResume bool             // wrap with AutoResumeBackend?
}

// RegisterBackend registers a backend factory function.
// Called by backend plugin sub-packages in their init().
func RegisterBackend(id string, newBackend func() AIBackend, needsAutoResume bool) {
	backendFactoriesMu.Lock()
	defer backendFactoriesMu.Unlock()
	if _, exists := backendFactories[id]; exists {
		panic(fmt.Sprintf("backend factory already registered: %s", id))
	}
	backendFactories[id] = &BackendFactoryEntry{
		NewBackendFn:    newBackend,
		NeedsAutoResume: needsAutoResume,
	}
}

// lookupBackendFactory returns the factory entry for the given backend ID.
func lookupBackendFactory(id string) *BackendFactoryEntry {
	backendFactoriesMu.RLock()
	defer backendFactoriesMu.RUnlock()
	return backendFactories[id]
}

// LookupBackendFactoryForTest returns the factory entry for testing.
// Do not use in production code.
func LookupBackendFactoryForTest(id string) *BackendFactoryEntry {
	return lookupBackendFactory(id)
}

// NewBackend creates the backend via the factory entry.
func (e *BackendFactoryEntry) NewBackend() AIBackend {
	return e.NewBackendFn()
}

// NewBackend creates a backend instance based on the backend type.
// For agents with ACP transport configured, use NewBackendForAgent instead.
func NewBackend(backendType string) (AIBackend, error) {
	// Try plugin registry first (migrated backends)
	if entry := lookupBackendFactory(backendType); entry != nil {
		backend := entry.NewBackendFn()
		if entry.NeedsAutoResume {
			backend = &AutoResumeBackend{inner: backend}
		}
		return backend, nil
	}

	// All backends have been migrated to the plugin registry.
	// If we reach here, the backend type is truly unsupported.
	return nil, fmt.Errorf("unsupported backend type: %s", backendType)
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
