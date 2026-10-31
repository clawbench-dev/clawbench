package service_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

const codexFixture = `model = "deepseek-v4-flash"
model_provider = "deepseek"

[model_providers.deepseek]
name = "deepseek"
base_url = "https://api.deepseek.com/v1"
env_key = "DEEPSEEK_API_KEY"

[projects."/tmp"]
trust_level = "trusted"
`

func setupCodexHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	origDataDir := model.DataDir
	model.DataDir = filepath.Join(home, ".clawbench")
	t.Cleanup(func() { model.DataDir = origDataDir })

	codexDir := filepath.Join(home, ".codex")
	require.NoError(t, os.MkdirAll(codexDir, 0o755))
	path := filepath.Join(codexDir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(codexFixture), 0o600))
	return path
}

func TestCodexProviderConfig_ApplyAndRestore(t *testing.T) {
	path := setupCodexHome(t)

	p := &model.Provider{
		ID: 1, Name: "MiniMax", Protocol: model.ProviderProtocolOpenAI,
		BaseURL: "https://api.minimaxi.com/v1", APIKey: "sk-secret", AuthMode: model.ProviderAuthAuto,
	}
	require.NoError(t, service.ApplyProviderConfigToBackend("codex", p))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)

	assert.Contains(t, content, `model_provider = "clawbench"`)
	assert.Contains(t, content, "[model_providers.clawbench]")
	assert.Contains(t, content, `base_url = "https://api.minimaxi.com/v1"`)
	assert.Contains(t, content, `env_key = "CLAWBENCH_PROVIDER_API_KEY"`)
	// An unset WireAPI keeps the historical default: the Responses API.
	assert.Contains(t, content, `wire_api = "responses"`)
	// The secret must never be written to disk.
	assert.NotContains(t, content, "sk-secret")
	// Existing user config is preserved.
	assert.Contains(t, content, "[model_providers.deepseek]")
	assert.Contains(t, content, `[projects."/tmp"]`)

	// Exactly one managed block, even after a second apply.
	require.NoError(t, service.ApplyProviderConfigToBackend("codex", p))
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(data), ">>> clawbench provider"))

	backups, err := filepath.Glob(path + ".clawbench-bak-*")
	require.NoError(t, err)
	assert.NotEmpty(t, backups, "the original file must be backed up")

	require.NoError(t, service.RestoreProviderConfig("codex"))
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	restored := string(data)
	assert.NotContains(t, restored, "clawbench provider")
	assert.Contains(t, restored, `model_provider = "deepseek"`)
	assert.Contains(t, restored, `[model_providers.deepseek]`)
}

// A gateway that only serves /chat/completions must be able to say so: codex
// otherwise sends every turn to /responses and 404s with no visible cause.
func TestCodexProviderConfig_WireAPIFollowsProvider(t *testing.T) {
	path := setupCodexHome(t)

	p := &model.Provider{
		ID: 3, Name: "ChatOnly", Protocol: model.ProviderProtocolOpenAI,
		BaseURL: "https://api.deepseek.com/v1", APIKey: "sk", AuthMode: model.ProviderAuthAuto,
		WireAPI: model.ProviderWireChat,
	}
	require.NoError(t, service.ApplyProviderConfigToBackend("codex", p))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `wire_api = "chat"`)
	assert.NotContains(t, string(data), `wire_api = "responses"`)
}

func TestCodexProviderConfig_RestoreWithoutOriginalKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	origDataDir := model.DataDir
	model.DataDir = filepath.Join(home, ".clawbench")
	t.Cleanup(func() { model.DataDir = origDataDir })

	dir := filepath.Join(home, ".codex")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("[projects.\"/tmp\"]\ntrust_level = \"trusted\"\n"), 0o600))

	p := &model.Provider{ID: 2, Name: "P", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://x", APIKey: "k", AuthMode: model.ProviderAuthAuto}
	require.NoError(t, service.ApplyProviderConfigToBackend("codex", p))
	data, _ := os.ReadFile(path)
	assert.Contains(t, string(data), `model_provider = "clawbench"`)

	require.NoError(t, service.RestoreProviderConfig("codex"))
	data, _ = os.ReadFile(path)
	assert.NotContains(t, string(data), "model_provider", "a key that did not exist must not be left behind")
	assert.Contains(t, string(data), `[projects."/tmp"]`)
}

func TestCodexProviderConfig_NoStateIsNoop(t *testing.T) {
	setupCodexHome(t)
	require.NoError(t, service.RestoreProviderConfig("codex"))
}
