package ai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"clawbench/internal/model"
)

// stubBackend is the in-process backend a test factory returns. It exists so
// the factory tests never depend on the built-in agent's real implementation.
type stubBackend struct{ id string }

func (b *stubBackend) Name() string { return b.id }

func (b *stubBackend) ExecuteStream(context.Context, ChatRequest) (<-chan StreamEvent, error) {
	return nil, nil
}

// setupTestBackends registers one in-process factory for the built-in agent.
// Every other backend is reached over ACP and therefore needs no factory at
// all, which is exactly the property these tests pin.
func setupTestBackends() {
	backendFactoriesMu.Lock()
	defer backendFactoriesMu.Unlock()
	backendFactories = make(map[string]*BackendFactoryEntry)
	backendFactories["builtin"] = &BackendFactoryEntry{
		NewBackendFn: func() AIBackend { return &stubBackend{id: "builtin"} },
	}
}

func TestNewBackend_InProcessBackend(t *testing.T) {
	setupTestBackends()
	backend, err := NewBackend("builtin")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "builtin", backend.Name())
}

// A backend that only speaks ACP has no factory, so the CLI entry point must
// fail loudly instead of returning a half-working backend.
func TestNewBackend_ACPOnlyBackendFailsLoudly(t *testing.T) {
	setupTestBackends()
	for _, id := range []string{"claude", "codebuddy", "opencode", "codex", "kimi", "vecli", "copilot"} {
		backend, err := NewBackend(id)
		assert.Error(t, err, "%s must not resolve through a CLI factory", id)
		assert.Nil(t, backend)
		assert.Contains(t, err.Error(), "unsupported backend type")
	}
}

func TestNewBackend_Unsupported(t *testing.T) {
	setupTestBackends()
	_, err := NewBackend("unsupported")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported backend type")
}

func TestNewBackend_Empty(t *testing.T) {
	setupTestBackends()
	_, err := NewBackend("")
	assert.Error(t, err)
}

func TestNewBackend_CaseSensitive(t *testing.T) {
	setupTestBackends()
	_, err := NewBackend("Builtin")
	assert.Error(t, err, "backend type should be case-sensitive")
}

// --- NewBackendForAgent tests ---

func agentForACP(t *testing.T) {
	t.Helper()
	origAgents := model.Agents
	t.Cleanup(func() { model.Agents = origAgents })
}

func TestNewBackendForAgent_ACPStdioTransport(t *testing.T) {
	setupTestBackends()
	agentForACP(t)
	model.Agents = map[string]*model.Agent{
		"test-acp": {
			ID:         "test-acp",
			Backend:    "claude",
			Transport:  "acp-stdio",
			AcpCommand: "claude acp",
		},
	}

	backend, err := NewBackendForAgent("claude", "test-acp")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "claude", backend.Name())

	_, ok := backend.(*ACPBackend)
	assert.True(t, ok, "claude ACP should be ACPBackend directly")
}

// A "cli" transport on an ACP-capable agent has no CLI implementation behind
// it any more. The factory adopts the agent's ACP command rather than failing
// the turn: the transport column is legacy data, and refusing to run would
// strand every conversation created before CLI was removed.
func TestNewBackendForAgent_CLITransport_FallsBackToACP(t *testing.T) {
	setupTestBackends()
	agentForACP(t)
	model.Agents = map[string]*model.Agent{
		"test-cli": {
			ID:         "test-cli",
			Backend:    "claude",
			Transport:  "cli",
			AcpCommand: "claude acp",
		},
	}

	backend, err := NewBackendForAgent("claude", "test-cli")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	_, ok := backend.(*ACPBackend)
	assert.True(t, ok, "a legacy cli transport must run over ACP")
}

func TestNewBackendForAgentWithTransport_ACPOverrideOnCLITransportAgent(t *testing.T) {
	setupTestBackends()
	agentForACP(t)
	model.Agents = map[string]*model.Agent{
		"test-pi": {
			ID:         "test-pi",
			Backend:    "pi",
			Transport:  "cli",
			AcpCommand: "pi-acp",
		},
	}

	backend, err := NewBackendForAgentWithTransport("pi", "test-pi", "acp-stdio")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	_, ok := backend.(*ACPBackend)
	assert.True(t, ok)
}

func TestNewBackendForAgentWithTransport_EmptyAgentID_ResolvesBackendAgent(t *testing.T) {
	setupTestBackends()
	agentForACP(t)
	model.Agents = map[string]*model.Agent{
		"antigravity": {
			ID:         "antigravity",
			Backend:    "antigravity",
			Transport:  "acp-stdio",
			AcpCommand: "agy acp",
		},
	}

	backend, err := NewBackendForAgentWithTransport("antigravity", "", "acp-stdio")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "antigravity", backend.Name())

	_, ok := backend.(*ACPBackend)
	assert.True(t, ok, "should resolve to ACPBackend for ACP-only agent when agentID is empty")
}

// TestNewBackendForAgentWithTransport_EmptyAgentID_UnknownBackend_FailsLoudly
// pins the boundary of the empty-agentID recovery: it resolves an agent only
// when the backend type matches one. It must NOT fall back to the default agent
// — the default agent may belong to a different backend, and handing back an
// ACPBackend for it would silently run the wrong agent/command.
func TestNewBackendForAgentWithTransport_EmptyAgentID_UnknownBackend_FailsLoudly(t *testing.T) {
	setupTestBackends()
	agentForACP(t)
	origDefault := model.DefaultAgentID
	t.Cleanup(func() { model.DefaultAgentID = origDefault })

	model.Agents = map[string]*model.Agent{
		"default-acp": {
			ID:         "default-acp",
			Backend:    "antigravity",
			Transport:  "acp-stdio",
			AcpCommand: "agy acp",
		},
	}
	model.DefaultAgentID = "default-acp"

	backend, err := NewBackendForAgentWithTransport("unknown-backend", "", "acp-stdio")
	assert.Error(t, err, "an unknown backend with no matching agent must fail, not run the default agent")
	assert.Nil(t, backend)
	assert.Contains(t, err.Error(), "unsupported backend type")
}

func TestNewBackendForAgentWithTransport_EmptyAgentID_MatchesAgentByBackendType(t *testing.T) {
	setupTestBackends()
	agentForACP(t)
	model.Agents = map[string]*model.Agent{
		"antigravity": {
			ID:         "antigravity",
			Backend:    "antigravity",
			Transport:  "acp-stdio",
			AcpCommand: "agy acp",
		},
	}

	backend, err := NewBackendForAgentWithTransport("antigravity", "", "acp-stdio")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "antigravity", backend.Name())
	_, ok := backend.(*ACPBackend)
	assert.True(t, ok)
}
