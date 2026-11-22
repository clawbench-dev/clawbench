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

// ModelListProvider is implemented by in-process backends that can name the
// models they run WITHOUT a handshake.
//
// It exists because GetModelListState is the single funnel every model-list
// consumer reads (the chat drawer, GET /api/agents, the ACP gate, the selftest),
// and an in-process backend has no session/new to ever write the capability
// registry. Without this hook its picker was empty until the first turn.
//
// It takes the agent id because several in-process agents can share one
// backend and each has its own provider binding; an id it does not recognise
// must yield nil rather than some other agent's list.
type ModelListProvider interface {
	ModelListState(agentID string) *ModelListState
}

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

// inProcessModelListState returns the model list an in-process backend reports
// for this agent, or nil when it has none (an unbound built-in agent, a
// non-built-in agent, or a backend without the hook).
//
// The registry only holds a backend id, so the agent is resolved here — the list
// is a property of (backend, the agent's provider binding), and only the agent
// knows which provider it is bound to.
//
// Resolution (which model is current, which provider ids apply) belongs to the
// backend: it is the one place that knows an in-process turn's model, and the
// ids it accepts. Here it is only asked whether one exists, and checked that it
// names the agent we asked about.
func inProcessModelListState(agentID, currentModelID string) *ModelListState {
	agent := model.GetAgent(agentID)
	if agent == nil || !agent.BuiltinBackend {
		return nil
	}
	entry := lookupBackendFactory(agent.Backend)
	if entry == nil || entry.NewBackendFn == nil {
		return nil
	}
	provider, ok := entry.NewBackendFn().(ModelListProvider)
	if !ok {
		return nil
	}
	state := provider.ModelListState(agentID)
	if state == nil || state.AgentID != agentID || len(state.Models) == 0 {
		return nil
	}
	// Copied before the current-model override: callers hand the result to a
	// response or a price pass, and an in-place edit would leak into whatever
	// the next caller receives.
	out := &ModelListState{
		CurrentModelID: state.CurrentModelID,
		Models:         append([]model.AgentModel(nil), state.Models...),
	}
	// A session may have picked a model the agent's list has since lost. Echo
	// the pick back instead of marking someone else's row current: the turn
	// really does run it (the backend falls back only when the endpoint is
	// known not to serve it), so the chip must not claim otherwise.
	if currentModelID != "" {
		out.CurrentModelID = currentModelID
	}
	return out
}

// lookupBackendFactory returns the factory entry for the given backend ID.
func lookupBackendFactory(id string) *BackendFactoryEntry {
	backendFactoriesMu.RLock()
	defer backendFactoriesMu.RUnlock()
	return backendFactories[id]
}

// LookupInProcessBackend returns the factory entry for an in-process backend,
// or nil when that id has no in-process implementation (every backend that
// spawns an agent over ACP).
//
// The caller uses it to reach a backend's own capability hooks (such as
// ModelListProvider) without instantiating a run: the handler needs the model
// list of a provider-backed agent whose process is not running, and importing
// the plugin registry from there would close an import cycle (plugins register
// INTO this package).
func LookupInProcessBackend(id string) *BackendFactoryEntry {
	return lookupBackendFactory(id)
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
