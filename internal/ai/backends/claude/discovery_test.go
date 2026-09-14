package claude

import (
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsClaudeDateStamped(t *testing.T) {
	tests := []struct {
		modelID  string
		expected bool
	}{
		{"claude-opus-4-20250514", true},
		{"claude-sonnet-4-6", false},
		{"claude-haiku-3-5-20241022", true},
		{"claude-opus-4-5", false},
		{"claude-3-haiku-20240307", true},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			assert.Equal(t, tt.expected, isClaudeDateStamped(tt.modelID))
		})
	}
}

func TestClaudeModelRe(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"claude-sonnet-4-6", true},
		{"claude-opus-4-5", true},
		{"claude-haiku-3-5", true},
		{"claude-sonnet-4-20250514", true}, // matches, but isDateStamped filters it later
		{"claude-opus-4", false},           // single version segment
		{"claude-3-5-haiku", false},        // old naming convention
		{"gpt-4.1", false},                 // not a Claude model
		{"sonnet-4-6", false},              // missing "claude-" prefix
		{"claude-sonnet-4-6-1", false},     // three version segments
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, claudeModelRe.MatchString(tt.input))
		})
	}
}

func TestClaudeCatalog_Structure(t *testing.T) {
	assert.NotEmpty(t, model.ClaudeCatalog)
	for _, m := range model.ClaudeCatalog {
		assert.NotEmpty(t, m.ID)
		assert.NotEmpty(t, m.Name)
	}
}

func TestClaudeCatalog_ContainsKnownModels(t *testing.T) {
	ids := make(map[string]bool)
	for _, m := range model.ClaudeCatalog {
		ids[m.ID] = true
	}
	assert.True(t, ids["claude-sonnet-4-20250514"], "should contain Claude Sonnet 4")
	assert.True(t, ids["claude-opus-4-20250514"], "should contain Claude Opus 4")
}

func TestParseClaudeModels_BuildsReadableNames(t *testing.T) {
	models := parseClaudeModels([]string{
		"claude-sonnet-4-6",
		"claude-opus-4-5",
		"claude-haiku-3-5",
	})

	require.Len(t, models, 3)
	assert.Equal(t, "Claude Sonnet 4.6", models[0].Name)
	assert.Equal(t, "Claude Opus 4.5", models[1].Name)
	assert.Equal(t, "Claude Haiku 3.5", models[2].Name)
}

func TestParseClaudeModels_SkipsDateStampedAndDuplicates(t *testing.T) {
	models := parseClaudeModels([]string{
		"claude-sonnet-4-6",
		"claude-sonnet-4-6",
		"claude-opus-4-20250514",
		"not-a-model",
	})

	require.Len(t, models, 1, "date-stamped snapshots and duplicates must be dropped")
	assert.Equal(t, "claude-sonnet-4-6", models[0].ID)
}

func TestSortClaudeModels_FamilyOrderThenNewestFirst(t *testing.T) {
	models := []model.AgentModel{
		{ID: "claude-haiku-3-5"},
		{ID: "claude-opus-4-4"},
		{ID: "claude-opus-4-5"},
		{ID: "claude-sonnet-4-6"},
	}

	sortClaudeModels(models)

	ids := make([]string, len(models))
	for i, m := range models {
		ids[i] = m.ID
	}
	assert.Equal(t, []string{
		"claude-sonnet-4-6",
		"claude-opus-4-5",
		"claude-opus-4-4",
		"claude-haiku-3-5",
	}, ids, "sonnet first, then opus newest-first, then haiku")
}

func TestLoadClaudeModelOverrides_MissingConfigDir(t *testing.T) {
	orig := claudeConfigDir
	t.Cleanup(func() { claudeConfigDir = orig })

	claudeConfigDir = func() string { return "/nonexistent/path" }
	assert.Nil(t, loadClaudeModelOverrides(), "a missing config dir is normal, not an error")
}

func TestLoadClaudeModelOverrides_ReadsOverridesMap(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.json"),
		[]byte(`{"modelOverrides":{"claude-sonnet-4-6":"MiniMax-M2.7"}}`), 0o644))

	orig := claudeConfigDir
	t.Cleanup(func() { claudeConfigDir = orig })
	claudeConfigDir = func() string { return dir }

	got := loadClaudeModelOverrides()
	require.Len(t, got, 1)
	assert.Equal(t, "MiniMax-M2.7", got["claude-sonnet-4-6"])
}

func TestApplyClaudeOverrides_ReplacesNameButNotID(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.json"),
		[]byte(`{"modelOverrides":{"claude-sonnet-4-6":"MiniMax-M2.7"}}`), 0o644))

	orig := claudeConfigDir
	t.Cleanup(func() { claudeConfigDir = orig })
	claudeConfigDir = func() string { return dir }

	models := []model.AgentModel{{ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6"}}
	models = applyClaudeOverrides(models)

	require.Len(t, models, 1)
	assert.Equal(t, "MiniMax-M2.7", models[0].Name, "display name reflects the real backing model")
	assert.Equal(t, "claude-sonnet-4-6", models[0].ID, "the ID must stay the Claude ID the CLI expects")
}

func TestApplyClaudeOverrides_DedupesCollapsedNames(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.json"),
		[]byte(`{"modelOverrides":{
			"claude-sonnet-4-6":"MiniMax-M2.7",
			"claude-opus-4-5":"MiniMax-M2.7"
		}}`), 0o644))

	orig := claudeConfigDir
	t.Cleanup(func() { claudeConfigDir = orig })
	claudeConfigDir = func() string { return dir }

	models := []model.AgentModel{
		{ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6"},
		{ID: "claude-opus-4-5", Name: "Claude Opus 4.5"},
		{ID: "claude-haiku-3-5", Name: "Claude Haiku 3.5"},
	}
	models = applyClaudeOverrides(models)

	require.Len(t, models, 2, "two tiers redirected to the same real model collapse to one entry")
	assert.Equal(t, "claude-sonnet-4-6", models[0].ID, "highest-priority occurrence is kept")
	assert.Equal(t, "MiniMax-M2.7", models[0].Name)
	assert.Equal(t, "claude-haiku-3-5", models[1].ID, "unrelated model survives")
}

func TestClaudeSource_Registered(t *testing.T) {
	spec := model.BackendSpec{ID: "claude", Backend: "claude", DefaultCmd: "claude"}
	assert.True(t, model.CanDiscoverModels(spec), "claude should support model discovery")

	src, ok := model.LookupModelSource("claude")
	require.True(t, ok)
	assert.Equal(t, model.SourceKindPlugin, src.Kind())
}

func TestClaudeSource_NeverReturnsNothing(t *testing.T) {
	// The plugin always has an answer: either models scanned from the binary or
	// the catalog. Which one is environment-dependent (the CLI may or may not
	// be installed), so assert the invariant rather than a specific branch.
	models, _ := discoverClaudeModels()
	require.NotEmpty(t, models, "claude discovery must always yield a usable list")
	for _, m := range models {
		assert.NotEmpty(t, m.ID)
		assert.NotEmpty(t, m.Name)
	}
}

func TestClaudeSource_FallsBackWhenCLIAbsent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	models, detail := discoverClaudeModels()

	require.Len(t, models, len(model.ClaudeCatalog))
	assert.Contains(t, detail, "not found", "the reason must say the CLI is missing")
}
