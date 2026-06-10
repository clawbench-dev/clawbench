package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"clawbench/internal/model"
)

func TestNewBackend_Claude(t *testing.T) {
	backend, err := NewBackend("claude")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "claude", backend.Name())
	// Claude is wrapped in AutoResumeBackend (ExitPlanMode auto-resume)
	_, ok := backend.(*AutoResumeBackend)
	assert.True(t, ok, "claude should be wrapped in AutoResumeBackend")
}

func TestNewBackend_Codebuddy(t *testing.T) {
	backend, err := NewBackend("codebuddy")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "codebuddy", backend.Name())
	// Codebuddy is wrapped in AutoResumeBackend (ExitPlanMode auto-resume)
	_, ok := backend.(*AutoResumeBackend)
	assert.True(t, ok, "codebuddy should be wrapped in AutoResumeBackend")
}

func TestNewBackend_OpenCode(t *testing.T) {
	backend, err := NewBackend("opencode")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "opencode", backend.Name())
	// OpenCode is NOT wrapped in AutoResumeBackend (no ExitPlanMode issue)
	_, ok := backend.(*AutoResumeBackend)
	assert.False(t, ok, "opencode should NOT be wrapped in AutoResumeBackend")
}

func TestNewBackend_Gemini(t *testing.T) {
	backend, err := NewBackend("gemini")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "gemini", backend.Name())
	// Gemini is NOT wrapped in AutoResumeBackend (no ExitPlanMode issue)
	_, ok := backend.(*AutoResumeBackend)
	assert.False(t, ok, "gemini should NOT be wrapped in AutoResumeBackend")
}

func TestNewBackend_Qoder(t *testing.T) {
	backend, err := NewBackend("qoder")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "qoder", backend.Name())
	// Verify AutoResumeBackend wrapping (Qoder has EnterPlanMode/ExitPlanMode)
	_, ok := backend.(*AutoResumeBackend)
	assert.True(t, ok, "qoder should be wrapped in AutoResumeBackend")
}

func TestNewBackend_Vecli(t *testing.T) {
	backend, err := NewBackend("vecli")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "vecli", backend.Name())
	// VeCLI is NOT wrapped in AutoResumeBackend (no ExitPlanMode detection)
	_, ok := backend.(*VeCLIBackend)
	assert.True(t, ok, "vecli should be a VeCLIBackend")
}

func TestNewBackend_Pi(t *testing.T) {
	backend, err := NewBackend("pi")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "pi", backend.Name())
	// Pi is wrapped in AutoResumeBackend (has ExitPlanMode)
	_, ok := backend.(*AutoResumeBackend)
	assert.True(t, ok, "pi should be wrapped in AutoResumeBackend")
}

func TestNewBackend_DeepSeek(t *testing.T) {
	backend, err := NewBackend("deepseek")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "deepseek", backend.Name())
	// DeepSeek is wrapped in AutoResumeBackend (supports ExitPlanMode)
	_, ok := backend.(*AutoResumeBackend)
	assert.True(t, ok, "deepseek should be wrapped in AutoResumeBackend")
}

func TestNewBackend_Cline(t *testing.T) {
	backend, err := NewBackend("cline")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "cline", backend.Name())
	// Cline is wrapped in AutoResumeBackend (supports ExitPlanMode)
	_, ok := backend.(*AutoResumeBackend)
	assert.True(t, ok, "cline should be wrapped in AutoResumeBackend")
}

func TestNewBackend_Kimi(t *testing.T) {
	backend, err := NewBackend("kimi")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "kimi", backend.Name())
	// Kimi is wrapped in AutoResumeBackend (supports plan mode)
	_, ok := backend.(*AutoResumeBackend)
	assert.True(t, ok, "kimi should be wrapped in AutoResumeBackend")
}

func TestNewBackend_Copilot(t *testing.T) {
	backend, err := NewBackend("copilot")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "copilot", backend.Name())
	// Copilot is wrapped in AutoResumeBackend (supports plan mode)
	_, ok := backend.(*AutoResumeBackend)
	assert.True(t, ok, "copilot should be wrapped in AutoResumeBackend")
}

func TestNewBackend_Codex(t *testing.T) {
	backend, err := NewBackend("codex")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "codex", backend.Name())
	// Codex is NOT wrapped in AutoResumeBackend (custom ExecuteStream, no ExitPlanMode)
	_, ok := backend.(*CodexBackend)
	assert.True(t, ok, "codex should be a CodexBackend (not wrapped in AutoResumeBackend)")
}

func TestNewBackend_Unsupported(t *testing.T) {
	_, err := NewBackend("unsupported")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported backend type")
	// Verify error message lists all supported backends
	assert.Contains(t, err.Error(), "claude")
	assert.Contains(t, err.Error(), "codex")
	assert.Contains(t, err.Error(), "pi")
}

func TestNewBackend_Empty(t *testing.T) {
	_, err := NewBackend("")
	assert.Error(t, err)
}

func TestNewBackend_CaseSensitive(t *testing.T) {
	// Backend type is case-sensitive
	_, err := NewBackend("Claude")
	assert.Error(t, err, "backend type should be case-sensitive")

	_, err = NewBackend("PI")
	assert.Error(t, err, "backend type should be case-sensitive")
}

// --- NewBackendForAgent tests ---

func TestNewBackendForAgent_NoAgentID_FallsBackToCLI(t *testing.T) {
	backend, err := NewBackendForAgent("claude", "")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "claude", backend.Name())
	// Falls back to CLI (AutoResumeBackend wrapping)
	_, ok := backend.(*AutoResumeBackend)
	assert.True(t, ok)
}

func TestNewBackendForAgent_UnknownAgentID_FallsBackToCLI(t *testing.T) {
	backend, err := NewBackendForAgent("claude", "nonexistent-agent")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "claude", backend.Name())
}

func TestNewBackendForAgent_ACPStdioTransport(t *testing.T) {
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

	// ACP backends are NOT wrapped in AutoResumeBackend (session/cancel replaces it)
	_, ok := backend.(*ACPBackend)
	assert.True(t, ok, "claude ACP should be ACPBackend directly (no AutoResume wrapping)")
}

func TestNewBackendForAgent_ACPHttpTransport_Unsupported(t *testing.T) {
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

	// Should fall back to AutoResumeBackend (CLI mode), not ACPBackend
	_, ok := backend.(*AutoResumeBackend)
	assert.True(t, ok, "acp-http should fall back to CLI AutoResumeBackend")
}

func TestNewBackendForAgent_ACPNoAutoResume(t *testing.T) {
	origAgents := model.Agents
	t.Cleanup(func() { model.Agents = origAgents })

	model.Agents = map[string]*model.Agent{
		"test-gemini": {
			ID:         "test-gemini",
			Backend:    "gemini",
			Transport:  "acp-stdio",
			AcpCommand: "gemini --acp",
		},
	}

	backend, err := NewBackendForAgent("gemini", "test-gemini")
	assert.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "gemini", backend.Name())

	// gemini ACP is also NOT wrapped in AutoResumeBackend
	_, ok := backend.(*ACPBackend)
	assert.True(t, ok, "gemini ACP should be ACPBackend directly")
}

func TestNewBackendForAgent_CLITransport_FallsBack(t *testing.T) {
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

	// Should be the standard CLI AutoResumeBackend (not ACPBackend)
	ar, ok := backend.(*AutoResumeBackend)
	assert.True(t, ok)
	_, ok = ar.inner.(*CLIBackend)
	assert.True(t, ok, "inner should be CLIBackend for cli transport")
}

func TestNewBackendForAgentWithTransport_ACPOverrideOnCLIAgent_FallsBack(t *testing.T) {
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

	// Should be AutoResumeBackend (CLI mode), NOT ACPBackend
	_, ok := backend.(*AutoResumeBackend)
	assert.True(t, ok, "acp-stdio override on CLI agent should fall back to AutoResumeBackend")

	_, ok = backend.(*ACPBackend)
	assert.False(t, ok, "should NOT be ACPBackend when agent transport is cli")
}

// --- needsAutoResume tests ---

func TestNeedsAutoResume(t *testing.T) {
	assert.True(t, needsAutoResume("claude"), "claude needs auto-resume")
	assert.True(t, needsAutoResume("codebuddy"), "codebuddy needs auto-resume")
	assert.True(t, needsAutoResume("qoder"), "qoder needs auto-resume")
	assert.True(t, needsAutoResume("deepseek"), "deepseek needs auto-resume")
	assert.True(t, needsAutoResume("pi"), "pi needs auto-resume")
	assert.True(t, needsAutoResume("cline"), "cline needs auto-resume")
	assert.True(t, needsAutoResume("kimi"), "kimi needs auto-resume")
	assert.True(t, needsAutoResume("copilot"), "copilot needs auto-resume")

	assert.False(t, needsAutoResume("opencode"), "opencode does NOT need auto-resume")
	assert.False(t, needsAutoResume("gemini"), "gemini does NOT need auto-resume")
	assert.False(t, needsAutoResume("codex"), "codex does NOT need auto-resume")
	assert.False(t, needsAutoResume("vecli"), "vecli does NOT need auto-resume")
}
