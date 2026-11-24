package codex

import (
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverCodexModelsFromCache(t *testing.T) {
	codexDir := t.TempDir()
	cache := `{"models":[
		{"slug":"gpt-6-astra","display_name":"GPT-6-Astra"},
		{"slug":"gpt-6-astra","display_name":"duplicate"},
		{"slug":"gpt-5.6-luna","display_name":"GPT-5.6 Luna"}
	]}`
	require.NoError(t, os.WriteFile(filepath.Join(codexDir, "models_cache.json"), []byte(cache), 0o644))
	t.Setenv("CODEX_HOME", codexDir)

	models := discoverCodexModelsFromCache()

	require.Len(t, models, 2, "duplicates collapse")
	assert.Equal(t, "gpt-6-astra", models[0].ID)
	assert.Equal(t, "GPT-6-Astra", models[0].Name)
	assert.Equal(t, "GPT-5.6 Luna", models[1].Name)
}

// The cache used to drop every entry whose `visibility` was not "list". That
// rule had no measurement behind it and could hide models the account can
// actually run, so the field is ignored: whatever the CLI cached IS the list.
func TestDiscoverCodexModelsFromCache_IgnoresVisibility(t *testing.T) {
	codexDir := t.TempDir()
	cache := `{"models":[
		{"slug":"gpt-6-astra","display_name":"GPT-6-Astra","visibility":"hide"},
		{"slug":"gpt-5.6-luna","visibility":"internal"}
	]}`
	require.NoError(t, os.WriteFile(filepath.Join(codexDir, "models_cache.json"), []byte(cache), 0o644))
	t.Setenv("CODEX_HOME", codexDir)

	models := discoverCodexModelsFromCache()

	require.Len(t, models, 2, "an unmodelled visibility field must not filter the account's own catalog")
	assert.Equal(t, "gpt-6-astra", models[0].ID)
	assert.Equal(t, "gpt-5.6-luna", models[1].ID)
	assert.Equal(t, "gpt-5.6-luna", models[1].Name, "a missing display name falls back to the slug")
}

func TestDiscoverCodexModelsFromCache_NoFile(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	assert.Nil(t, discoverCodexModelsFromCache())
}

func TestDiscoverCodexModelsFromCache_MalformedJSON(t *testing.T) {
	codexDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(codexDir, "models_cache.json"), []byte("{not json"), 0o644))
	t.Setenv("CODEX_HOME", codexDir)

	assert.Nil(t, discoverCodexModelsFromCache())
}

// Without a cache the built-in catalog stands in, and the reason is reported
// rather than substituted silently.
func TestDiscoverCodexModels_FallsBackToCatalog(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir()) // no cache file
	mockCodexOnPath(t)

	models, detail := discoverCodexModels()

	require.Len(t, models, len(CodexCatalog))
	assert.Equal(t, "gpt-5.5", models[0].ID)
	assert.Contains(t, detail, "built-in catalog", "the fallback must be reported, not silent")
}

// The cache is preferred over the catalog whenever it parses.
func TestDiscoverCodexModels_CacheWins(t *testing.T) {
	codexDir := t.TempDir()
	cache := `{"models":[{"slug":"gpt-6-astra","display_name":"GPT-6-Astra"}]}`
	require.NoError(t, os.WriteFile(filepath.Join(codexDir, "models_cache.json"), []byte(cache), 0o644))
	t.Setenv("CODEX_HOME", codexDir)
	mockCodexOnPath(t)

	models, detail := discoverCodexModels()

	require.Len(t, models, 1)
	assert.Empty(t, detail)
	assert.Equal(t, "gpt-6-astra", models[0].ID)
}

func TestDiscoverCodexModels_CLINotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	models, detail := discoverCodexModels()

	assert.Nil(t, models, "a missing CLI must not be answered from the catalog")
	assert.Contains(t, detail, "CLI not found on PATH")
}

func TestCodexSource_MarksDefaultAtTheBoundary(t *testing.T) {
	codexDir := t.TempDir()
	cache := `{"models":[
		{"slug":"gpt-6-astra","display_name":"GPT-6-Astra"},
		{"slug":"gpt-5.6-luna","display_name":"GPT-5.6 Luna"}
	]}`
	require.NoError(t, os.WriteFile(filepath.Join(codexDir, "models_cache.json"), []byte(cache), 0o644))
	t.Setenv("CODEX_HOME", codexDir)
	mockCodexOnPath(t)

	src, ok := model.LookupModelSource("codex")
	require.True(t, ok)
	models, _ := src.Discover()

	require.Len(t, models, 2)
	assert.True(t, models[0].Default, "the source wrapper is responsible for marking a default")
	assert.False(t, models[1].Default)
}

// mockCodexOnPath puts an executable named codex on PATH without the vendor/
// tree the removed binary scan used to look for.
func mockCodexOnPath(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "codex"), []byte("#!/bin/sh\nexit 0\n"), 0o755))
	t.Setenv("PATH", binDir)
}
