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
	"clawbench/internal/store"
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

// ── Path resolution and the agent-driven entry point ─────────

// Without a home directory there is nowhere to write, and the caller must get
// an empty list (env backends) rather than a bogus path under the CWD.
func TestProviderConfigPaths_NoHome(t *testing.T) {
	t.Setenv("HOME", "")
	assert.Empty(t, service.ProviderConfigPaths("codex"))
	assert.Empty(t, service.ProviderConfigPaths("claude"))
}

// ApplyProviderConfig is what the agent PATCH path calls: it re-reads the agent
// (a caller may hold a stale snapshot) and does nothing for the two cases that
// need no file edit.
func TestApplyProviderConfig_AgentDriven(t *testing.T) {
	path := setupCodexHome(t)
	t.Cleanup(func() {
		model.ReplaceProviders(nil)
		model.ReplaceAgents(nil, nil)
	})

	pid := int64(1)
	model.ReplaceProviders([]*model.Provider{{
		ID: 1, Name: "GW", Protocol: model.ProviderProtocolOpenAI,
		BaseURL: "https://gw.example/v1", APIKey: "sk", AuthMode: model.ProviderAuthAuto,
	}})
	model.ReplaceAgents(
		map[string]*model.Agent{
			"codex-bound":   {ID: "codex-bound", Backend: "codex", ProviderID: &pid},
			"codex-unbound": {ID: "codex-unbound", Backend: "codex"},
			"claude-agent":  {ID: "claude-agent", Backend: "claude", ProviderID: &pid},
		},
		[]*model.Agent{
			{ID: "codex-bound", Backend: "codex", ProviderID: &pid},
			{ID: "codex-unbound", Backend: "codex"},
			{ID: "claude-agent", Backend: "claude", ProviderID: &pid},
		},
	)

	// An unknown agent is a no-op (the row may have been deleted concurrently).
	require.NoError(t, service.ApplyProviderConfig("missing"))

	// An env-driven backend needs no file.
	require.NoError(t, service.ApplyProviderConfig("claude-agent"))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "clawbench provider")

	// A config-file backend with a binding writes the managed block.
	require.NoError(t, service.ApplyProviderConfig("codex-bound"))
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "[model_providers.clawbench]")
	assert.Contains(t, string(data), `base_url = "https://gw.example/v1"`)

	// An unbound config-file backend is restored, not left pointing at us.
	require.NoError(t, service.ApplyProviderConfig("codex-unbound"))
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "clawbench provider")
	assert.Contains(t, string(data), `model_provider = "deepseek"`)
}

func TestApplyProviderConfigToBackend_NoopForNilOrEnvBackend(t *testing.T) {
	path := setupCodexHome(t)

	require.NoError(t, service.ApplyProviderConfigToBackend("claude", &model.Provider{Name: "x"}))
	require.NoError(t, service.ApplyProviderConfigToBackend("codex", nil))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, codexFixture, string(data), "a no-op must not touch the file")
}

// A codex install with no resolvable config path cannot be applied; the error
// must be reported (the caller then refuses to persist the binding).
func TestApplyCodexProvider_NoConfigPath(t *testing.T) {
	t.Setenv("HOME", "")
	origDataDir := model.DataDir
	model.DataDir = t.TempDir()
	t.Cleanup(func() { model.DataDir = origDataDir })

	err := service.ApplyProviderConfigToBackend("codex", &model.Provider{
		ID: 1, Name: "P", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://x", APIKey: "k",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config path unavailable")
}

// ── Reconcile at startup ────────────────────────────────────

// A restart must re-apply the file for a bound codex agent and restore it when
// nothing is bound: the file is the process's only copy of the endpoint.
func TestReconcileProviderConfigs(t *testing.T) {
	path := setupCodexHome(t)
	t.Cleanup(func() {
		model.ReplaceProviders(nil)
		model.ReplaceAgents(nil, nil)
	})

	pid := int64(1)
	model.ReplaceProviders([]*model.Provider{{
		ID: 1, Name: "GW", Protocol: model.ProviderProtocolOpenAI,
		BaseURL: "https://gw.example/v1", APIKey: "sk", AuthMode: model.ProviderAuthAuto,
	}})

	// Nothing bound: the file must be left in the user's original state.
	model.ReplaceAgents(nil, nil)
	require.NoError(t, service.ReconcileProviderConfigs())
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `model_provider = "deepseek"`)

	// Bound: the block is (re)written.
	model.ReplaceAgents(
		map[string]*model.Agent{"codex-bound": {ID: "codex-bound", Backend: "codex", ProviderID: &pid}},
		[]*model.Agent{{ID: "codex-bound", Backend: "codex", ProviderID: &pid}},
	)
	require.NoError(t, service.ReconcileProviderConfigs())
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "[model_providers.clawbench]")
}

// A failed apply during reconcile must be returned: startup then logs it and
// the user still has the previous file, which is the only honest outcome.
func TestReconcileProviderConfigs_PropagatesApplyError(t *testing.T) {
	setupCodexHome(t)
	t.Cleanup(func() {
		model.ReplaceProviders(nil)
		model.ReplaceAgents(nil, nil)
	})

	pid := int64(1)
	model.ReplaceProviders([]*model.Provider{{
		ID: 1, Name: "GW", Protocol: model.ProviderProtocolOpenAI,
		BaseURL: "https://gw.example/v1", APIKey: "sk", AuthMode: model.ProviderAuthAuto,
	}})
	model.ReplaceAgents(
		map[string]*model.Agent{"codex-bound": {ID: "codex-bound", Backend: "codex", ProviderID: &pid}},
		[]*model.Agent{{ID: "codex-bound", Backend: "codex", ProviderID: &pid}},
	)

	// No home → no config path → the apply fails.
	t.Setenv("HOME", "")
	require.Error(t, service.ReconcileProviderConfigs())
}

func TestReconcileProviderConfigs_PropagatesRestoreError(t *testing.T) {
	setupCodexHome(t)
	t.Cleanup(func() {
		model.ReplaceProviders(nil)
		model.ReplaceAgents(nil, nil)
	})
	model.ReplaceAgents(nil, nil)

	// An unreadable sidecar makes the restore step fail.
	statePath := filepath.Join(model.DataDir, "providers", "config-state.json")
	require.NoError(t, os.MkdirAll(statePath, 0o755))

	require.Error(t, service.ReconcileProviderConfigs())
}

// ── Restore edge cases ──────────────────────────────────────

func TestRestoreProviderConfig_NoConfigPathIsNoop(t *testing.T) {
	t.Setenv("HOME", "")
	require.NoError(t, service.RestoreProviderConfig("codex"))
	// An env-driven backend never had a file to restore.
	require.NoError(t, service.RestoreProviderConfig("claude"))
}

// A config.toml that exists but cannot be read is an error: silently treating
// it as absent would report success while the managed block stays behind.
func TestCodexProviderConfig_UnreadableConfigIsReported(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	origDataDir := model.DataDir
	model.DataDir = filepath.Join(home, ".clawbench")
	t.Cleanup(func() { model.DataDir = origDataDir })

	// A directory where config.toml should be.
	path := filepath.Join(home, ".codex", "config.toml")
	require.NoError(t, os.MkdirAll(path, 0o755))

	p := &model.Provider{ID: 1, Name: "P", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://x", APIKey: "k", AuthMode: model.ProviderAuthAuto}
	err := service.ApplyProviderConfigToBackend("codex", p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read codex config")

	err = service.RestoreProviderConfig("codex")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read codex config")
}

// Restoring when the file is gone: with no sidecar there is nothing to do, but
// with one the record must be dropped so the next reconcile cannot re-apply a
// stale original.
func TestCodexProviderConfig_RestoreWhenFileIsGone(t *testing.T) {
	path := setupCodexHome(t)

	// No sidecar, no file → no-op.
	require.NoError(t, os.Remove(path))
	require.NoError(t, service.RestoreProviderConfig("codex"))

	// With a sidecar: apply, delete the file, restore → the record is dropped.
	require.NoError(t, os.WriteFile(path, []byte(codexFixture), 0o600))
	p := &model.Provider{ID: 1, Name: "P", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://x", APIKey: "k", AuthMode: model.ProviderAuthAuto}
	require.NoError(t, service.ApplyProviderConfigToBackend("codex", p))
	require.NoError(t, os.Remove(path))

	require.NoError(t, service.RestoreProviderConfig("codex"))
	_, statErr := os.Stat(filepath.Join(model.DataDir, "providers", "config-state.json"))
	require.NoError(t, statErr)
	data, err := os.ReadFile(filepath.Join(model.DataDir, "providers", "config-state.json"))
	require.NoError(t, err)
	assert.NotContains(t, string(data), "config.toml", "the stale record must be dropped")
}

// An unreadable sidecar must be reported by restore too (it cannot know what
// the user's original value was).
func TestCodexProviderConfig_RestoreUnreadableSidecarIsAnError(t *testing.T) {
	setupCodexHome(t)
	statePath := filepath.Join(model.DataDir, "providers", "config-state.json")
	require.NoError(t, os.MkdirAll(statePath, 0o755))

	require.Error(t, service.RestoreProviderConfig("codex"))
}

// The sidecar can be lost (different data dir, deleted by hand) while the file
// still points at us. Re-applying must recover the user's original value from
// the backups instead of recording "clawbench" as the thing to restore.
func TestCodexProviderConfig_SidecarLostRecoversOriginalFromBackup(t *testing.T) {
	path := setupCodexHome(t)

	first := &model.Provider{ID: 1, Name: "One", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://one.example/v1", APIKey: "k", AuthMode: model.ProviderAuthAuto}
	require.NoError(t, service.ApplyProviderConfigToBackend("codex", first))

	// The pristine backup holds the user's value; the sidecar is gone.
	require.NoError(t, os.Remove(filepath.Join(model.DataDir, "providers", "config-state.json")))

	second := &model.Provider{ID: 2, Name: "Two", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://two.example/v1", APIKey: "k", AuthMode: model.ProviderAuthAuto}
	require.NoError(t, service.ApplyProviderConfigToBackend("codex", second))

	require.NoError(t, service.RestoreProviderConfig("codex"))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `model_provider = "deepseek"`,
		"the original value must come from the backup, not from our own block")
	assert.NotContains(t, string(data), `model_provider = "clawbench"`)
}

// When every backup is our own apply, there is no original to recover; the key
// must be dropped rather than left dangling at the managed provider.
func TestCodexProviderConfig_NoUsableBackupDropsOurValue(t *testing.T) {
	path := setupCodexHome(t)

	p := &model.Provider{ID: 1, Name: "P", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://x", APIKey: "k", AuthMode: model.ProviderAuthAuto}
	// A file that already carries our value: every backup taken from here on
	// records `clawbench`, never the user's original.
	require.NoError(t, os.WriteFile(path, []byte("model = \"gpt-5\"\nmodel_provider = \"clawbench\"\n"), 0o600))
	require.NoError(t, service.ApplyProviderConfigToBackend("codex", p))
	require.NoError(t, os.Remove(filepath.Join(model.DataDir, "providers", "config-state.json")))

	require.NoError(t, service.RestoreProviderConfig("codex"))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `model_provider = "clawbench"`)
	assert.Contains(t, string(data), `model = "gpt-5"`, "unrelated keys survive")
}

// An unreadable backup (a directory with a matching name) must be skipped, not
// treated as "no backups at all".
func TestCodexProviderConfig_UnreadableBackupIsSkipped(t *testing.T) {
	path := setupCodexHome(t)

	p := &model.Provider{ID: 1, Name: "P", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://x", APIKey: "k", AuthMode: model.ProviderAuthAuto}
	require.NoError(t, service.ApplyProviderConfigToBackend("codex", p))
	require.NoError(t, os.Remove(filepath.Join(model.DataDir, "providers", "config-state.json")))

	// A "backup" that sorts last but cannot be read.
	require.NoError(t, os.Mkdir(path+".clawbench-bak-9999999999999999999", 0o755))

	require.NoError(t, service.RestoreProviderConfig("codex"))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `model_provider = "deepseek"`,
		"the readable pristine backup must still be used")
}

// A corrupt sidecar must not block an apply: losing it only means the original
// value cannot be restored automatically.
func TestCodexProviderConfig_CorruptSidecarDoesNotBlockApply(t *testing.T) {
	path := setupCodexHome(t)

	statePath := filepath.Join(model.DataDir, "providers", "config-state.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(statePath), 0o755))
	require.NoError(t, os.WriteFile(statePath, []byte("{not json"), 0o600))

	p := &model.Provider{ID: 1, Name: "P", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://x", APIKey: "k", AuthMode: model.ProviderAuthAuto}
	require.NoError(t, service.ApplyProviderConfigToBackend("codex", p))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "[model_providers.clawbench]")
}

// A hand-written, unquoted single-character value still has to be compared
// correctly (unquoteTomlString only strips a matching pair of quotes).
func TestCodexProviderConfig_ShortUnquotedValueIsRecorded(t *testing.T) {
	path := setupCodexHome(t)
	require.NoError(t, os.WriteFile(path, []byte("model_provider = x\n"), 0o600))

	p := &model.Provider{ID: 1, Name: "P", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://x", APIKey: "k", AuthMode: model.ProviderAuthAuto}
	require.NoError(t, service.ApplyProviderConfigToBackend("codex", p))

	require.NoError(t, service.RestoreProviderConfig("codex"))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "model_provider = x", "the user's own value must be put back verbatim")
}

// Restoring when the recorded state says "there was no key" but the file no
// longer has one either: nothing to remove, and the file must stay intact.
func TestCodexProviderConfig_RestoreWithoutKeyInFile(t *testing.T) {
	path := setupCodexHome(t)

	// Start from a file without model_provider so the sidecar records "absent".
	require.NoError(t, os.WriteFile(path, []byte("model = \"gpt-5\"\n"), 0o600))
	p := &model.Provider{ID: 1, Name: "P", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://x", APIKey: "k", AuthMode: model.ProviderAuthAuto}
	require.NoError(t, service.ApplyProviderConfigToBackend("codex", p))

	// The user deletes the key by hand before unbinding.
	broken := "model = \"gpt-5\"\n\n" + codexBlockOnly(t, path)
	require.NoError(t, os.WriteFile(path, []byte(broken), 0o600))

	require.NoError(t, service.RestoreProviderConfig("codex"))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	// Only the managed block is removed; the rest of the file is untouched.
	assert.NotContains(t, string(data), "clawbench provider")
	assert.Equal(t, "model = \"gpt-5\"\n\n", string(data))
}

// codexBlockOnly returns just the managed block of the file at path.
func codexBlockOnly(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)
	start := strings.Index(content, "# >>> clawbench provider")
	require.GreaterOrEqual(t, start, 0, "the managed block must be present")
	return content[start:]
}

// ── ProviderBindingConflict failure path ────────────────────

// A failed conflict query must be reported: silently answering "no conflicts"
// would let two same-backend agents end up on different providers, where the
// config file can only honor one of them.
func TestProviderBindingConflict_ReportsQueryFailure(t *testing.T) {
	db, err := service.InitInMemoryDB()
	require.NoError(t, err)
	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})

	_, err = store.WriteExec("DROP TABLE agents")
	require.NoError(t, err)

	_, err = service.ProviderBindingConflict("codex", "", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider binding conflict")
}
