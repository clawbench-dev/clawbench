package model_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"clawbench/internal/model"
)

// The config-directory switch is what makes "one provider per agent" real for
// codebuddy / kimi / pi. If a backend silently lost its entry, its bindings
// would fall back to the shared env vars and two agents would overwrite each
// other again.
func TestProviderUsesConfigDir(t *testing.T) {
	for _, backend := range []string{"codebuddy", "kimi", "pi"} {
		assert.True(t, model.ProviderUsesConfigDir(backend), backend)
		assert.True(t, model.ProviderNeedsConfigWrite(backend), backend)
		assert.NotEmpty(t, model.ProviderConfigDirEnv(backend), backend)
		assert.NotEmpty(t, model.ProviderConfigDirFile(backend), backend)
	}
	// codex edits a shared file, not a directory.
	assert.False(t, model.ProviderUsesConfigDir("codex"))
	assert.True(t, model.ProviderUsesConfigFile("codex"))
	// Env-driven backends need no file at all.
	for _, backend := range []string{"claude", "opencode", "deepseek", "cursor", "copilot"} {
		assert.False(t, model.ProviderNeedsConfigWrite(backend), backend)
	}
}

// The picker's protocol list must include what the config FILE can express, not
// only what an environment variable exists for. kimi's anthropic path is
// reachable solely through its config file, so a list built from env slots hid
// the one combination the user can actually configure.
func TestSupportedProtocols_IncludesConfigDirProtocols(t *testing.T) {
	kimi := model.SupportedProtocols("kimi")
	assert.Contains(t, kimi, model.ProviderProtocolAnthropic,
		"kimi reaches anthropic through its config file, not through ANTHROPIC_*")
	assert.Contains(t, kimi, model.ProviderProtocolOpenAI)

	// codebuddy hardcodes /chat/completions regardless of the entry's provider
	// field, so it must advertise OpenAI only — offering anthropic would accept a
	// binding that can never answer.
	assert.Equal(t, []string{model.ProviderProtocolOpenAI}, model.SupportedProtocols("codebuddy"))

	pi := model.SupportedProtocols("pi")
	assert.Contains(t, pi, model.ProviderProtocolAnthropic)
	assert.Contains(t, pi, model.ProviderProtocolOpenAI)
}

// kimi's config file has no ${ENV} syntax, so the key lands on disk. Every other
// config-dir backend references the variable instead, which is why they must not
// be treated as "secret in file".
func TestProviderConfigSecretInFile(t *testing.T) {
	assert.True(t, model.ProviderConfigSecretInFile("kimi"))
	assert.False(t, model.ProviderConfigSecretInFile("pi"))
	assert.False(t, model.ProviderConfigSecretInFile("codebuddy"))
	assert.False(t, model.ProviderConfigSecretInFile("claude"))
}

// pi resolves a model as "<provider>/<model>". The picker and the declared file
// must agree on that prefix, otherwise the user picks an id the CLI cannot
// resolve (issue #651's failure mode, one backend over).
func TestProviderModelIDFor_PiUsesProviderPrefix(t *testing.T) {
	got := model.ProviderModelIDFor("pi", model.ProviderProtocolOpenAI, "deepseek-chat")
	assert.Equal(t, "clawbench/deepseek-chat", got)

	// Idempotent: an already-prefixed id must not be prefixed twice.
	assert.Equal(t, "clawbench/deepseek-chat",
		model.ProviderModelIDFor("pi", model.ProviderProtocolOpenAI, got))

	assert.Equal(t, "deepseek-chat",
		model.ProviderBareModelID("pi", model.ProviderProtocolOpenAI, got))

	// A backend that takes bare ids is unaffected.
	assert.Equal(t, "deepseek-chat",
		model.ProviderModelIDFor("kimi", model.ProviderProtocolOpenAI, "deepseek-chat"))
}

// The display path is what the consent dialog shows. It must name the per-agent
// file (so the user can find it), not the backend name.
func TestProviderConfigDisplayPath(t *testing.T) {
	origDataDir := model.DataDir
	model.DataDir = "/data/clawbench"
	t.Cleanup(func() { model.DataDir = origDataDir })

	assert.Equal(t,
		"/data/clawbench/providers/agents/agent-1/config.toml",
		model.ProviderConfigDisplayPath("kimi", "agent-1"))
	assert.Equal(t,
		"/data/clawbench/providers/agents/agent-1/models.json",
		model.ProviderConfigDisplayPath("codebuddy", "agent-1"))

	// An env-driven backend writes nothing, so there is nothing to show.
	assert.Empty(t, model.ProviderConfigDisplayPath("claude", "agent-1"))
}

// A protocol reachable only through the config file must pass the bind-time auth
// mode check: the file carries the credential, so no auth mode can be wrong.
// Before this, binding an Anthropic provider to kimi was rejected with "does not
// support the anthropic protocol" even though the file expresses it.
func TestProviderAuthModeError_ConfigDirProtocol(t *testing.T) {
	assert.NoError(t, model.ProviderAuthModeError("kimi", model.ProviderProtocolAnthropic, model.ProviderAuthAPIKey))
	assert.NoError(t, model.ProviderAuthModeError("kimi", model.ProviderProtocolAnthropic, model.ProviderAuthBearer))
	// A protocol the file cannot express is still refused.
	assert.Error(t, model.ProviderAuthModeError("codebuddy", model.ProviderProtocolAnthropic, model.ProviderAuthAuto))
}
