package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"clawbench/internal/model"
)

// setupTestBackends registers backend factories for testing.
// We can't import backend sub-packages due to import cycles,
// so we register lightweight stubs that satisfy NewBackend's lookup.
func setupTestBackends() {
	backendFactoriesMu.Lock()
	defer backendFactoriesMu.Unlock()
	backendFactories = make(map[string]*BackendFactoryEntry)

	stubs := []string{
		"claude",
		"codebuddy",
		"opencode",
		"qoder",
		"vecli",
		"pi",
		"deepseek",
		"kimi",
		"copilot",
		"codex",
		"mimo",
	}
	for _, id := range stubs {
		backendType := id // capture for closure
		switch backendType {
		case "vecli":
			backendFactories[backendType] = &BackendFactoryEntry{
				NewBackendFn: func() AIBackend { return NewVeCLIBackend() },
			}
		case "codex":
			backendFactories[backendType] = &BackendFactoryEntry{
				NewBackendFn: func() AIBackend { return &CodexBackend{} },
			}
		default:
			backendFactories[backendType] = &BackendFactoryEntry{
				NewBackendFn: func() AIBackend {
					return &CLIBackend{
						BackendName: backendType,
						Cmd:         backendType,
						BuildArgsFn: func(req ChatRequest) []string { return nil },
						NewParserFn: func() LineParser { return &StreamParser{} },
					}
				},
			}
		}
	}
}

func TestNewBackend_Claude(t *testing.T) {
	setupTestBackends()
	backend, err := NewBackend("claude")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "claude", backend.Name())
	_, ok := backend.(*CLIBackend)
	assert.True(t, ok, "claude should be a CLIBackend")
}

func TestNewBackend_Codebuddy(t *testing.T) {
	setupTestBackends()
	backend, err := NewBackend("codebuddy")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "codebuddy", backend.Name())
	_, ok := backend.(*CLIBackend)
	assert.True(t, ok, "codebuddy should be a CLIBackend")
}

func TestNewBackend_OpenCode(t *testing.T) {
	setupTestBackends()
	backend, err := NewBackend("opencode")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "opencode", backend.Name())
	_, ok := backend.(*CLIBackend)
	assert.True(t, ok, "opencode should be a CLIBackend")
}

func TestNewBackend_Qoder(t *testing.T) {
	setupTestBackends()
	backend, err := NewBackend("qoder")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "qoder", backend.Name())
	_, ok := backend.(*CLIBackend)
	assert.True(t, ok, "qoder should be a CLIBackend")
}

func TestNewBackend_Vecli(t *testing.T) {
	setupTestBackends()
	backend, err := NewBackend("vecli")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "vecli", backend.Name())
	_, ok := backend.(*VeCLIBackend)
	assert.True(t, ok, "vecli should be a VeCLIBackend")
}

func TestNewBackend_Pi(t *testing.T) {
	setupTestBackends()
	backend, err := NewBackend("pi")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "pi", backend.Name())
	_, ok := backend.(*CLIBackend)
	assert.True(t, ok, "pi should be a CLIBackend")
}

func TestNewBackend_DeepSeek(t *testing.T) {
	setupTestBackends()
	backend, err := NewBackend("deepseek")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "deepseek", backend.Name())
	_, ok := backend.(*CLIBackend)
	assert.True(t, ok, "deepseek should be a CLIBackend")
}

func TestNewBackend_Kimi(t *testing.T) {
	setupTestBackends()
	backend, err := NewBackend("kimi")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "kimi", backend.Name())
	_, ok := backend.(*CLIBackend)
	assert.True(t, ok, "kimi should be a CLIBackend")
}

func TestNewBackend_Copilot(t *testing.T) {
	setupTestBackends()
	backend, err := NewBackend("copilot")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "copilot", backend.Name())
	_, ok := backend.(*CLIBackend)
	assert.True(t, ok, "copilot should be a CLIBackend")
}

func TestNewBackend_Codex(t *testing.T) {
	setupTestBackends()
	backend, err := NewBackend("codex")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "codex", backend.Name())
	_, ok := backend.(*CodexBackend)
	assert.True(t, ok, "codex should be a CodexBackend")
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
	// Backend type is case-sensitive
	_, err := NewBackend("Claude")
	assert.Error(t, err, "backend type should be case-sensitive")

	_, err = NewBackend("PI")
	assert.Error(t, err, "backend type should be case-sensitive")
}

func TestBackendSupportsCLI(t *testing.T) {
	setupTestBackends()
	// Registered CLI backends report true
	assert.True(t, BackendSupportsCLI("claude"))
	assert.True(t, BackendSupportsCLI("kimi"))
	assert.True(t, BackendSupportsCLI("codex")) // custom backend also counts as CLI
	assert.True(t, BackendSupportsCLI("vecli"))

	// Unregistered backends report false
	assert.False(t, BackendSupportsCLI("grok"), "ACP-only backend without CLI factory should report false")
	assert.False(t, BackendSupportsCLI("unsupported"))
	assert.False(t, BackendSupportsCLI(""))
}

// --- NewBackendForAgent tests ---

func TestNewBackendForAgent_NoAgentID_FallsBackToCLI(t *testing.T) {
	setupTestBackends()
	backend, err := NewBackendForAgent("claude", "")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "claude", backend.Name())
	_, ok := backend.(*CLIBackend)
	assert.True(t, ok)
}

func TestNewBackendForAgent_UnknownAgentID_FallsBackToCLI(t *testing.T) {
	setupTestBackends()
	backend, err := NewBackendForAgent("claude", "nonexistent-agent")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "claude", backend.Name())
}

func TestNewBackendForAgent_ACPStdioTransport(t *testing.T) {
	setupTestBackends()
	// Set up a test agent with ACP acp-stdio transport
	origAgents := model.Agents
	t.Cleanup(func() { model.Agents = origAgents })

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

	// ACP backends are ACPBackend directly
	_, ok := backend.(*ACPBackend)
	assert.True(t, ok, "claude ACP should be ACPBackend directly")
}

func TestNewBackendForAgent_ACPHttpTransport_Unsupported(t *testing.T) {
	setupTestBackends()
	origAgents := model.Agents
	t.Cleanup(func() { model.Agents = origAgents })

	model.Agents = map[string]*model.Agent{
		"test-http": {
			ID:        "test-http",
			Backend:   "codebuddy",
			Transport: "acp-http",
		},
	}

	// acp-http is no longer supported; should fall back to CLI backend
	backend, err := NewBackendForAgent("codebuddy", "test-http")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "codebuddy", backend.Name())

	// Should fall back to CLIBackend (CLI mode), not ACPBackend
	_, ok := backend.(*CLIBackend)
	assert.True(t, ok, "acp-http should fall back to CLIBackend")
}

func TestNewBackendForAgent_CLITransport_FallsBack(t *testing.T) {
	setupTestBackends()
	origAgents := model.Agents
	t.Cleanup(func() { model.Agents = origAgents })

	model.Agents = map[string]*model.Agent{
		"test-cli": {
			ID:        "test-cli",
			Backend:   "claude",
			Transport: "cli",
		},
	}

	backend, err := NewBackendForAgent("claude", "test-cli")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "claude", backend.Name())

	// Should be the standard CLIBackend (not ACPBackend)
	_, ok := backend.(*CLIBackend)
	assert.True(t, ok)
}

func TestNewBackendForAgentWithTransport_ACPOverrideOnCLIAgent_FallsBack(t *testing.T) {
	setupTestBackends()
	origAgents := model.Agents
	t.Cleanup(func() { model.Agents = origAgents })

	model.Agents = map[string]*model.Agent{
		"test-pi": {
			ID:        "test-pi",
			Backend:   "pi",
			Transport: "cli",
		},
	}

	// Session had acp-stdio persisted but agent (pi) only supports CLI.
	// Should fall back gracefully to CLI backend instead of erroring out.
	backend, err := NewBackendForAgentWithTransport("pi", "test-pi", "acp-stdio")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "pi", backend.Name())

	// Should be CLIBackend (CLI mode), NOT ACPBackend
	_, ok := backend.(*CLIBackend)
	assert.True(t, ok, "acp-stdio override on CLI agent should fall back to CLIBackend")

	_, ok = backend.(*ACPBackend)
	assert.False(t, ok, "should NOT be ACPBackend when agent transport is cli")
}
