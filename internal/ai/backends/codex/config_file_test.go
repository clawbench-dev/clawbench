package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupCodexConfig points both the home directory (where codex keeps
// config.toml) and the data directory (where the sidecar lives) at temp dirs,
// so a test never touches the developer's own ~/.codex.
//
// USERPROFILE is set too: os.UserHomeDir reads $HOME on unix but %USERPROFILE%
// on Windows, and the suite runs on all three.
func setupCodexConfig(t *testing.T) (cfgPath string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	orig := model.DataDir
	model.DataDir = t.TempDir()
	t.Cleanup(func() { model.DataDir = orig })
	return filepath.Join(home, ".codex", "config.toml")
}

func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func readConfig(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func testProvider() *model.Provider {
	return &model.Provider{
		ID:       1,
		Name:     "MiniMax",
		BaseURL:  "https://api.example.com/v1",
		APIKey:   "sk-secret",
		Protocol: model.ProviderProtocolOpenAI,
		WireAPI:  model.ProviderWireChat,
	}
}

// ---------------------------------------------------------------------------
// Apply
// ---------------------------------------------------------------------------

func TestApplyProvider_WritesBlockAndRemembersOriginal(t *testing.T) {
	cfg := setupCodexConfig(t)
	writeConfig(t, cfg, "model_provider = \"openai\"\n\n[model_providers.openai]\nname = \"OpenAI\"\n")

	require.NoError(t, ApplyProvider(testProvider()))

	got := readConfig(t, cfg)
	assert.Contains(t, got, blockStart)
	assert.Contains(t, got, blockEnd)
	assert.Contains(t, got, `model_provider = "clawbench"`)
	assert.Contains(t, got, `base_url = "https://api.example.com/v1"`)
	assert.Contains(t, got, `wire_api = "chat"`)
	// The secret is referenced through env_key, never written out.
	assert.NotContains(t, got, "sk-secret")
	// Another tool's table must survive the apply.
	assert.Contains(t, got, "[model_providers.openai]")

	state, err := loadState()
	require.NoError(t, err)
	fs, ok := state[cfg]
	require.True(t, ok, "apply must record what the file looked like")
	assert.Equal(t, `"openai"`, fs.OriginalModelProvider, "the raw right-hand side is kept verbatim")
	assert.True(t, fs.HadModelProvider)
	require.NotEmpty(t, fs.Backup)

	backup, err := os.ReadFile(fs.Backup)
	require.NoError(t, err)
	assert.Contains(t, string(backup), `model_provider = "openai"`)
	assert.NotContains(t, string(backup), blockStart)
}

func TestApplyProvider_NoUserFileCreatesOneWithoutBackup(t *testing.T) {
	cfg := setupCodexConfig(t)

	require.NoError(t, ApplyProvider(testProvider()))

	got := readConfig(t, cfg)
	assert.Contains(t, got, blockStart)
	assert.Contains(t, got, `model_provider = "clawbench"`)

	state, err := loadState()
	require.NoError(t, err)
	fs, ok := state[cfg]
	require.True(t, ok)
	assert.False(t, fs.HadModelProvider, "there was no original key")
	assert.Empty(t, fs.Backup, "nothing to back up")
}

func TestApplyProvider_SecondApplyIsNoOp(t *testing.T) {
	cfg := setupCodexConfig(t)
	writeConfig(t, cfg, "model_provider = \"openai\"\n")

	require.NoError(t, ApplyProvider(testProvider()))
	first := readConfig(t, cfg)

	require.NoError(t, ApplyProvider(testProvider()))
	assert.Equal(t, first, readConfig(t, cfg), "re-applying the same provider must not rewrite the file")
}

func TestApplyProvider_RepointsAfterAnotherToolChangedTheKey(t *testing.T) {
	cfg := setupCodexConfig(t)
	writeConfig(t, cfg, "model_provider = \"openai\"\n")

	require.NoError(t, ApplyProvider(testProvider()))
	// Another tool repoints the key; that newer value is the one unbinding must
	// put back, not the stale sidecar record.
	edited := strings.Replace(readConfig(t, cfg), `model_provider = "clawbench"`, `model_provider = "other"`, 1)
	writeConfig(t, cfg, edited)

	require.NoError(t, ApplyProvider(testProvider()))

	state, err := loadState()
	require.NoError(t, err)
	assert.Equal(t, `"other"`, state[cfg].OriginalModelProvider,
		"the value found in the file wins over a stale sidecar entry")
}

func TestApplyProvider_UsesBackupWhenSidecarIsGone(t *testing.T) {
	cfg := setupCodexConfig(t)
	writeConfig(t, cfg, "model_provider = \"openai\"\n")

	require.NoError(t, ApplyProvider(testProvider()))
	require.NoError(t, os.Remove(configStatePath()))

	// Re-apply a DIFFERENT provider: the key still reads "clawbench" and there
	// is no sidecar, so the pristine backup is the only surviving record of the
	// user's value.
	other := testProvider()
	other.BaseURL = "https://api.other.example/v1"
	require.NoError(t, ApplyProvider(other))
	state, err := loadState()
	require.NoError(t, err)
	assert.Equal(t, `"openai"`, state[cfg].OriginalModelProvider)
	assert.True(t, state[cfg].HadModelProvider)
}

func TestApplyProvider_NoHomeDirectoryFails(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	err := ApplyProvider(testProvider())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "codex config path unavailable")
}

// ---------------------------------------------------------------------------
// Restore
// ---------------------------------------------------------------------------

func TestRestoreProvider_PutsOriginalBack(t *testing.T) {
	cfg := setupCodexConfig(t)
	writeConfig(t, cfg, "model_provider = \"openai\"\n\n[model_providers.openai]\nname = \"OpenAI\"\n")

	require.NoError(t, ApplyProvider(testProvider()))
	require.NoError(t, RestoreProvider())

	got := readConfig(t, cfg)
	assert.NotContains(t, got, blockStart)
	assert.NotContains(t, got, `model_provider = "clawbench"`)
	assert.Contains(t, got, `model_provider = "openai"`)
	assert.Contains(t, got, "[model_providers.openai]")

	state, err := loadState()
	require.NoError(t, err)
	assert.NotContains(t, state, cfg, "the record is dropped once restored")
}

func TestRestoreProvider_RemovesKeyTheUserNeverHad(t *testing.T) {
	cfg := setupCodexConfig(t)

	require.NoError(t, ApplyProvider(testProvider()))
	require.NoError(t, RestoreProvider())

	got := readConfig(t, cfg)
	assert.NotContains(t, got, "model_provider")
	assert.NotContains(t, got, blockStart)
}

func TestRestoreProvider_LeavesForeignValueAlone(t *testing.T) {
	cfg := setupCodexConfig(t)
	writeConfig(t, cfg, "model_provider = \"openai\"\n")

	require.NoError(t, ApplyProvider(testProvider()))
	edited := strings.Replace(readConfig(t, cfg), `model_provider = "clawbench"`, `model_provider = "other"`, 1)
	writeConfig(t, cfg, edited)

	require.NoError(t, RestoreProvider())

	got := readConfig(t, cfg)
	assert.Contains(t, got, `model_provider = "other"`,
		"restoring over another tool's choice would hijack its configuration")
	assert.NotContains(t, got, blockStart, "the managed block goes away regardless")
}

func TestRestoreProvider_NoSidecarUsesBackup(t *testing.T) {
	cfg := setupCodexConfig(t)
	writeConfig(t, cfg, "model_provider = \"openai\"\n")

	require.NoError(t, ApplyProvider(testProvider()))
	require.NoError(t, os.Remove(configStatePath()))

	require.NoError(t, RestoreProvider())

	got := readConfig(t, cfg)
	assert.Contains(t, got, `model_provider = "openai"`)
	assert.NotContains(t, got, blockStart)
}

func TestRestoreProvider_NoSidecarNoBackupDropsKey(t *testing.T) {
	cfg := setupCodexConfig(t)

	require.NoError(t, ApplyProvider(testProvider()))
	require.NoError(t, os.Remove(configStatePath()))
	matches, err := filepath.Glob(cfg + ".clawbench-bak-*")
	require.NoError(t, err)
	for _, m := range matches {
		require.NoError(t, os.Remove(m))
	}

	require.NoError(t, RestoreProvider())

	got := readConfig(t, cfg)
	assert.NotContains(t, got, "model_provider",
		"without a record the key must not be left pointing at a table that is gone")
}

func TestRestoreProvider_NoFileButRecordedDropsRecord(t *testing.T) {
	cfg := setupCodexConfig(t)
	writeConfig(t, cfg, "model_provider = \"openai\"\n")

	require.NoError(t, ApplyProvider(testProvider()))
	require.NoError(t, os.Remove(cfg))

	require.NoError(t, RestoreProvider())

	state, err := loadState()
	require.NoError(t, err)
	assert.NotContains(t, state, cfg)
}

func TestRestoreProvider_UntouchedFileIsNoOp(t *testing.T) {
	cfg := setupCodexConfig(t)
	writeConfig(t, cfg, "model_provider = \"openai\"\n")

	require.NoError(t, RestoreProvider())
	assert.Equal(t, "model_provider = \"openai\"\n", readConfig(t, cfg))
}

func TestRestoreProvider_NoHomeDirectoryIsNoOp(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	assert.NoError(t, RestoreProvider())
}

// ---------------------------------------------------------------------------
// TOML helpers
// ---------------------------------------------------------------------------

func TestProviderBlock_DefaultsWireAPIToResponses(t *testing.T) {
	p := testProvider()
	p.WireAPI = "bogus"
	block := providerBlock(p)
	assert.Contains(t, block, `wire_api = "responses"`)
	assert.Contains(t, block, `name = "MiniMax"`)
	assert.Contains(t, block, `env_key = "`+model.ProviderSecretEnvName()+`"`)
	assert.Contains(t, block, "requires_openai_auth = false")
	assert.True(t, strings.HasPrefix(block, blockStart))
	assert.True(t, strings.HasSuffix(block, blockEnd+"\n"))
}

func TestTomlString_EscapesReservedCharacters(t *testing.T) {
	assert.Equal(t, `"plain"`, tomlString("plain"))
	assert.Equal(t, `"a\\b"`, tomlString(`a\b`))
	assert.Equal(t, `"say \"hi\""`, tomlString(`say "hi"`))
}

func TestUnquoteTomlString(t *testing.T) {
	cases := map[string]string{
		`"x"`:   "x",
		`'x'`:   "x",
		`"x`:    `"x`,
		`x`:     "x",
		`""`:    "",
		`"a"b"`: `a"b`,
	}
	for in, want := range cases {
		assert.Equal(t, want, unquoteTomlString(in), "in=%q", in)
	}
}

func TestStripManagedBlock(t *testing.T) {
	block := blockStart + "\n[model_providers.clawbench]\nname = \"x\"\n" + blockEnd + "\n"

	t.Run("no block", func(t *testing.T) {
		assert.Equal(t, "a = 1\n", stripManagedBlock("a = 1\n"))
	})

	t.Run("removes block and its newline", func(t *testing.T) {
		got := stripManagedBlock("a = 1\n" + block + "b = 2\n")
		assert.Equal(t, "a = 1\nb = 2\n", got)
	})

	t.Run("removes every block", func(t *testing.T) {
		got := stripManagedBlock(block + "a = 1\n" + block)
		assert.Equal(t, "a = 1\n", got)
	})

	t.Run("half a block truncates to the start marker", func(t *testing.T) {
		// The end marker was deleted by hand. Keeping the remainder would make
		// the next strip delete everything between two start markers — i.e. the
		// user's own config.
		got := stripManagedBlock("a = 1\n" + blockStart + "\n[model_providers.clawbench]\nname = \"x\"\n")
		assert.Equal(t, "a = 1\n", got)
	})
}

func TestFindTopLevelKey(t *testing.T) {
	content := "model_provider = \"openai\" # note\n[model_providers.x]\nmodel_provider = \"nested\"\n"

	idx, value, found := findTopLevelKey(content, "model_provider")
	require.True(t, found)
	assert.Equal(t, 0, idx)
	assert.Equal(t, `"openai" # note`, value)

	_, _, found = findTopLevelKey(content, "missing")
	assert.False(t, found)

	// A key only counts when it is followed by '='.
	_, _, found = findTopLevelKey("model_providerx = 1\n", "model_provider")
	assert.False(t, found)
	_, _, found = findTopLevelKey("model_provider\n", "model_provider")
	assert.False(t, found)

	// Keys below the first table header are not top-level.
	_, _, found = findTopLevelKey("[t]\nmodel_provider = \"x\"\n", "model_provider")
	assert.False(t, found)
}

func TestUpsertTopLevelKey(t *testing.T) {
	t.Run("replaces an existing key in place", func(t *testing.T) {
		got := upsertTopLevelKey("a = 1\nmodel_provider = \"old\"\nb = 2\n", `"new"`)
		assert.Equal(t, "a = 1\nmodel_provider = \"new\"\nb = 2\n", got)
	})

	t.Run("inserts before the first table", func(t *testing.T) {
		got := upsertTopLevelKey("a = 1\n[t]\nk = 2\n", `"new"`)
		assert.Equal(t, "a = 1\nmodel_provider = \"new\"\n[t]\nk = 2\n", got)
	})

	t.Run("appends when there is no table", func(t *testing.T) {
		got := upsertTopLevelKey("a = 1\n", `"new"`)
		assert.Equal(t, "a = 1\n\nmodel_provider = \"new\"", got)
	})
}

func TestRemoveTopLevelKey(t *testing.T) {
	assert.Equal(t, "a = 1\n", removeTopLevelKey("a = 1\nmodel_provider = \"x\"\n"))
	assert.Equal(t, "a = 1\n", removeTopLevelKey("a = 1\n"))
}

func TestTopLevelLimit(t *testing.T) {
	assert.Equal(t, 2, topLevelLimit([]string{"a = 1", "b = 2", "[t]", "c = 3"}))
	assert.Equal(t, 2, topLevelLimit([]string{"a = 1", "b = 2"}))
}

// ---------------------------------------------------------------------------
// Backups and the sidecar
// ---------------------------------------------------------------------------

func TestOriginalModelProviderFromBackup(t *testing.T) {
	cfg := setupCodexConfig(t)

	t.Run("no backups", func(t *testing.T) {
		_, _, ok := originalModelProviderFromBackup(cfg)
		assert.False(t, ok)
	})

	// Newest backup is our own apply; the older one holds the user's value.
	writeConfig(t, cfg+".clawbench-bak-0000000000000000001", "model_provider = \"openai\"\n")
	writeConfig(t, cfg+".clawbench-bak-0000000000000000002", "model_provider = \"clawbench\"\n")

	value, had, ok := originalModelProviderFromBackup(cfg)
	require.True(t, ok)
	assert.True(t, had)
	assert.Equal(t, `"openai"`, value)

	// All backups are ours: nothing to recover.
	require.NoError(t, os.Remove(cfg+".clawbench-bak-0000000000000000001"))
	_, _, ok = originalModelProviderFromBackup(cfg)
	assert.False(t, ok)
}

func TestBackupFile_PrunesToOldestPlusNewest(t *testing.T) {
	cfg := setupCodexConfig(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o755))

	// Fixed-width names sort chronologically, exactly like the real nanosecond
	// suffix, so this pins the keep policy without racing a coarse clock.
	for i := range 5 {
		writeConfig(t, fmt.Sprintf("%s.clawbench-bak-%019d", cfg, i), "x")
	}

	pruneBackups(cfg, 3)

	matches, err := filepath.Glob(cfg + ".clawbench-bak-*")
	require.NoError(t, err)
	require.Len(t, matches, 3)
	assert.Equal(t, fmt.Sprintf("%s.clawbench-bak-%019d", cfg, 0), matches[0],
		"the oldest backup is the only pre-apply record and must survive")
	assert.Equal(t, fmt.Sprintf("%s.clawbench-bak-%019d", cfg, 3), matches[1])
	assert.Equal(t, fmt.Sprintf("%s.clawbench-bak-%019d", cfg, 4), matches[2])
}

func TestBackupFile_WritesContentAndSuffix(t *testing.T) {
	cfg := setupCodexConfig(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o755))

	backup, err := backupFile(cfg, []byte("model_provider = \"openai\"\n"))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(backup, cfg+".clawbench-bak-"))
	assert.Equal(t, "model_provider = \"openai\"\n", readConfig(t, backup))
}

func TestLoadState_MissingAndCorrupt(t *testing.T) {
	setupCodexConfig(t)

	state, err := loadState()
	require.NoError(t, err)
	assert.Empty(t, state)

	require.NoError(t, os.MkdirAll(filepath.Dir(configStatePath()), 0o755))
	require.NoError(t, os.WriteFile(configStatePath(), []byte("{not json"), 0o600))

	state, err = loadState()
	require.NoError(t, err, "a corrupt sidecar must not block applying a provider")
	assert.NotNil(t, state)
	assert.Empty(t, state)
}

func TestSaveStateRoundTrip(t *testing.T) {
	cfg := setupCodexConfig(t)

	want := map[string]fileState{
		cfg: {Backup: "b", OriginalModelProvider: `"x"`, HadModelProvider: true},
	}
	require.NoError(t, saveState(want))

	got, err := loadState()
	require.NoError(t, err)
	assert.Equal(t, want, got)
}
