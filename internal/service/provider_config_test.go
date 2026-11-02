package service_test

import (
	"fmt"
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

// The managed block wraps only [model_providers.clawbench]; model_provider is an
// ordinary top-level key outside it. Losing the sidecar therefore used to leave
// `model_provider = "clawbench"` pointing at a table that restore had just
// deleted, and every codex session afterwards refused to start.
func TestCodexProviderConfig_RestoreWithoutSidecarLeavesNoDanglingKey(t *testing.T) {
	path := setupCodexHome(t)

	first := &model.Provider{
		ID: 1, Name: "One", Protocol: model.ProviderProtocolOpenAI,
		BaseURL: "https://one.example/v1", APIKey: "sk", AuthMode: model.ProviderAuthAuto,
	}
	second := &model.Provider{
		ID: 2, Name: "Two", Protocol: model.ProviderProtocolOpenAI,
		BaseURL: "https://two.example/v1", APIKey: "sk", AuthMode: model.ProviderAuthAuto,
	}
	require.NoError(t, service.ApplyProviderConfigToBackend("codex", first))
	// A second apply backs up a file that already carries our own value, so the
	// recovery must skip it and reach the pristine copy.
	require.NoError(t, service.ApplyProviderConfigToBackend("codex", second))

	statePath := filepath.Join(model.DataDir, "providers", "config-state.json")
	require.NoError(t, os.Remove(statePath))

	require.NoError(t, service.RestoreProviderConfig("codex"))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)
	assert.NotContains(t, content, "clawbench provider")
	assert.NotContains(t, content, `model_provider = "clawbench"`,
		"a dangling key makes codex refuse to start")
	assert.Contains(t, content, `model_provider = "deepseek"`,
		"the backups are the only surviving record of the user's value")
	assert.Contains(t, content, `[model_providers.deepseek]`)
	assert.Contains(t, content, `[projects."/tmp"]`)
}

// Without a sidecar AND without a usable backup the key is still removed when it
// holds our own value — the one case where guessing is safe.
func TestCodexProviderConfig_RestoreWithoutSidecarOrBackupDropsOnlyOurValue(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	origDataDir := model.DataDir
	model.DataDir = filepath.Join(home, ".clawbench")
	t.Cleanup(func() { model.DataDir = origDataDir })

	dir := filepath.Join(home, ".codex")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "config.toml")
	// Left behind by an apply whose sidecar and backups are both gone.
	require.NoError(t, os.WriteFile(path, []byte("model = \"gpt-5\"\nmodel_provider = \"clawbench\"\n\n[projects.\"/tmp\"]\ntrust_level = \"trusted\"\n"), 0o600))

	require.NoError(t, service.RestoreProviderConfig("codex"))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)
	assert.NotContains(t, content, `model_provider = "clawbench"`)
	assert.Contains(t, content, `model = "gpt-5"`, "unrelated keys must survive")
	assert.Contains(t, content, `[projects."/tmp"]`, "unrelated tables must survive")

	// A hand-written provider is not ours to delete.
	require.NoError(t, os.WriteFile(path, []byte("model_provider = \"mine\"\n"), 0o600))
	require.NoError(t, service.RestoreProviderConfig("codex"))
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `model_provider = "mine"`)
}

// An unreadable sidecar used to be discarded (`state, _ :=`), so the first
// `state[path] = prev` panicked on a nil map — after the config file had already
// been rewritten. Failing before the write keeps the file honest.
func TestCodexProviderConfig_UnreadableSidecarFailsBeforeWriting(t *testing.T) {
	path := setupCodexHome(t)

	// A directory where the sidecar should be: os.ReadFile fails with EISDIR for
	// every uid (chmod 000 does not stop root, which CI may run as).
	statePath := filepath.Join(model.DataDir, "providers", "config-state.json")
	require.NoError(t, os.MkdirAll(statePath, 0o755))

	p := &model.Provider{ID: 1, Name: "P", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://x", APIKey: "k", AuthMode: model.ProviderAuthAuto}
	err := service.ApplyProviderConfigToBackend("codex", p)
	require.Error(t, err, "an unreadable sidecar must be reported, not swallowed")

	data, rerr := os.ReadFile(path)
	require.NoError(t, rerr)
	assert.NotContains(t, string(data), "clawbench provider", "a failed apply must not touch the file")
	assert.Contains(t, string(data), `model_provider = "deepseek"`)
}

// A hand-edited file with the end marker deleted used to leave the half block in
// place; the next apply appended a second start marker, and the strip after that
// removed everything in between — the user's own config.
func TestCodexProviderConfig_DanglingStartMarkerIsDropped(t *testing.T) {
	path := setupCodexHome(t)

	p := &model.Provider{ID: 1, Name: "P", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://x", APIKey: "k", AuthMode: model.ProviderAuthAuto}
	require.NoError(t, service.ApplyProviderConfigToBackend("codex", p))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	// Simulate the hand edit: drop the end marker, add user config after it.
	broken := strings.Replace(string(data), "# <<< clawbench provider (managed by ClawBench) <<<\n", "", 1)
	broken += "\n[model_providers.mine]\nname = \"mine\"\n"
	require.NoError(t, os.WriteFile(path, []byte(broken), 0o600))

	require.NoError(t, service.ApplyProviderConfigToBackend("codex", p))

	data, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(data), ">>> clawbench provider"),
		"two start markers make the next strip delete everything between them")
	assert.Equal(t, 1, strings.Count(string(data), "<<< clawbench provider"))
	assert.Contains(t, string(data), `model_provider = "clawbench"`, "apply rewrites the key")
	assert.Contains(t, string(data), `[model_providers.deepseek]`, "config before the marker survives")
	assert.Contains(t, string(data), `[projects."/tmp"]`)
}

// Backups are pruned, and the ones taken after the first apply all carry our own
// model_provider. Only the OLDEST can be the pristine copy, so "keep the newest
// N" silently threw away the only record of the user's value on the 4th edit.
func TestCodexProviderConfig_RestoreAfterManyAppliesKeepsPristineBackup(t *testing.T) {
	path := setupCodexHome(t)

	// Four applies: with keep=3 and "newest wins", the pristine backup is gone
	// by now.
	for i := range 4 {
		p := &model.Provider{
			ID: int64(i + 1), Name: fmt.Sprintf("P%d", i), Protocol: model.ProviderProtocolOpenAI,
			BaseURL: fmt.Sprintf("https://p%d.example/v1", i), APIKey: "sk", AuthMode: model.ProviderAuthAuto,
		}
		require.NoError(t, service.ApplyProviderConfigToBackend("codex", p))
	}

	backups, err := filepath.Glob(path + ".clawbench-bak-*")
	require.NoError(t, err)
	assert.Len(t, backups, 3, "pruning must still bound the number of backups")

	require.NoError(t, os.Remove(filepath.Join(model.DataDir, "providers", "config-state.json")))
	require.NoError(t, service.RestoreProviderConfig("codex"))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)
	assert.NotContains(t, content, `model_provider = "clawbench"`)
	assert.Contains(t, content, `model_provider = "deepseek"`,
		"the oldest backup is the only pristine copy; without it the key is deleted instead of restored")
}
