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
	for _, id := range []string{"claude", "codebuddy", "opencode", "codex", "kimi"} {
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

// A legacy "cli" value on an ACP-capable agent must not strand the turn: the
// factory reads the command, not the column.
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

func TestNewBackendForAgent_LegacyCLITransportStillUsesACP(t *testing.T) {
	setupTestBackends()
	agentForACP(t)
	model.Agents = map[string]*model.Agent{
		"test-pi": {
			ID:         "test-pi",
			Backend:    "pi",
			AcpCommand: "pi-acp",
		},
	}

	backend, err := NewBackendForAgent("pi", "test-pi")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	_, ok := backend.(*ACPBackend)
	assert.True(t, ok)
}

func TestNewBackendForAgent_EmptyAgentID_ResolvesBackendAgent(t *testing.T) {
	setupTestBackends()
	agentForACP(t)
	model.Agents = map[string]*model.Agent{
		"antigravity": {
			ID:         "antigravity",
			Backend:    "antigravity",
			AcpCommand: "agy acp",
		},
	}

	backend, err := NewBackendForAgent("antigravity", "")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "antigravity", backend.Name())

	_, ok := backend.(*ACPBackend)
	assert.True(t, ok, "should resolve to ACPBackend for ACP-only agent when agentID is empty")
}

// TestNewBackendForAgent_EmptyAgentID_UnknownBackend_FailsLoudly
// pins the boundary of the empty-agentID recovery: it resolves an agent only
// when the backend type matches one. It must NOT fall back to the default agent
// — the default agent may belong to a different backend, and handing back an
// ACPBackend for it would silently run the wrong agent/command.
func TestNewBackendForAgent_EmptyAgentID_UnknownBackend_FailsLoudly(t *testing.T) {
	setupTestBackends()
	agentForACP(t)
	origDefault := model.DefaultAgentID
	t.Cleanup(func() { model.DefaultAgentID = origDefault })

	model.Agents = map[string]*model.Agent{
		"default-acp": {
			ID:         "default-acp",
			Backend:    "antigravity",
			AcpCommand: "agy acp",
		},
	}
	model.DefaultAgentID = "default-acp"

	backend, err := NewBackendForAgent("unknown-backend", "")
	assert.Error(t, err, "an unknown backend with no matching agent must fail, not run the default agent")
	assert.Nil(t, backend)
	assert.Contains(t, err.Error(), "unsupported backend type")
}

func TestNewBackendForAgent_EmptyAgentID_MatchesAgentByBackendType(t *testing.T) {
	setupTestBackends()
	agentForACP(t)
	model.Agents = map[string]*model.Agent{
		"antigravity": {
			ID:         "antigravity",
			Backend:    "antigravity",
			AcpCommand: "agy acp",
		},
	}

	backend, err := NewBackendForAgent("antigravity", "")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "antigravity", backend.Name())
	_, ok := backend.(*ACPBackend)
	assert.True(t, ok)
}
