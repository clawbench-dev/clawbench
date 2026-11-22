package ai

import (
	"fmt"
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
	NewBackendFn func() AIBackend // returns a new backend instance
}

// RegisterBackend registers an in-process backend factory function.
//
// Callers are expected to be the plugin forwarding point
// (backends.Register), not backend sub-packages: a sub-package declares its
// factory as BackendPlugin.Implementation.NewBackend and gets this call for free.
// Keeping the exported function lets tests register throwaway factories.
func RegisterBackend(id string, newBackend func() AIBackend) {
	backendFactoriesMu.Lock()
	defer backendFactoriesMu.Unlock()
	if _, exists := backendFactories[id]; exists {
		panic(fmt.Sprintf("backend factory already registered: %s", id))
	}
	backendFactories[id] = &BackendFactoryEntry{
		NewBackendFn: newBackend,
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

// NewBackend creates an in-process backend instance based on the backend type.
//
// Only the built-in agent has one. Every other backend reaches its agent over
// ACP, so an id without a factory is an error rather than a CLI fallback.
func NewBackend(backendType string) (AIBackend, error) {
	if entry := lookupBackendFactory(backendType); entry != nil {
		return entry.NewBackendFn(), nil
	}
	return nil, fmt.Errorf("unsupported backend type: %s (no in-process implementation registered)", backendType)
}

// NewBackendForAgent creates a backend instance for the given agent.
//
// An agent with an ACP command always runs over ACP (ACP uses session/cancel
// instead of process kill for stuck agents). The in-process backend (the
// built-in agent) has no ACP command, so it is the only case that resolves
// through the factory table.
//
// This is the preferred entry point when the agent ID is known (all handler paths).
func NewBackendForAgent(backendType, agentID string) (AIBackend, error) {
	return NewBackendForAgentFn(backendType, agentID)
}

// NewBackendForAgentFn is the indirection behind NewBackendForAgent.
//
// It exists so a test can observe the exact ai.ChatRequest a caller handed to
// the backend (the turn runners build it, then resolve a backend, then execute).
// Asserting on the backend's input is the only way to catch a request assembly
// bug without also depending on real streaming: a stubbed turn runner would
// assert on the caller instead of on the request the backend actually sees.
var NewBackendForAgentFn = newBackendForAgent

func newBackendForAgent(backendType, agentID string) (AIBackend, error) {
	// agentID and backendType are different identifiers (agent ID vs backend
	// name) and are not interchangeable: resolving an agent by backendType is
	// only a best-effort recovery for callers that omitted the agent ID. It is
	// deliberately NOT extended to the default agent — the default agent may
	// have nothing to do with backendType, so using it here would hand back an
	// ACPBackend for an unrelated agent (whose Name() is that agent's backend)
	// and silently run the wrong agent/command. When no agent matches the
	// backend type, fall through to the factory table, which fails loudly
	// instead of running something unintended.
	effectiveAgentID := agentID
	if effectiveAgentID == "" {
		if a := model.GetAgent(backendType); a != nil {
			effectiveAgentID = a.ID
		}
	}
	if effectiveAgentID != "" {
		if agent := model.GetAgent(effectiveAgentID); agent != nil {
			if agent.SupportsACP() {
				acpBackend, err := NewACPBackend(agent)
				if err != nil {
					return nil, fmt.Errorf("acp backend for agent %q: %w", effectiveAgentID, err)
				}
				return acpBackend, nil
			}
		}
	}

	// In-process backend (built-in agent); fails loudly for anything else.
	return NewBackend(backendType)
}
