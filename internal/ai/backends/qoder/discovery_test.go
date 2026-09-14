package qoder

import (
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQoderModelKeyRe(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"modelSelector.item.gpt-4.1", true},
		{"modelSelector.item.claude-sonnet-4-6", true},
		{"modelSelector.item.o3", true},
		{"models.gpt-4.1", false},        // missing "modelSelector.item." prefix
		{"modelSelector.item", false},    // no model ID after prefix
		{"modelSelector.gpt-4.1", false}, // missing "item."
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, qoderModelKeyRe.MatchString(tt.input))
		})
	}
}

func TestQoderModelKeyRe_ExtractsID(t *testing.T) {
	tests := []struct {
		input      string
		expectedID string
	}{
		{"modelSelector.item.gpt-4.1", "gpt-4.1"},
		{"modelSelector.item.claude-opus-4-20250514", "claude-opus-4-20250514"},
		{"modelSelector.item.o4-mini", "o4-mini"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			m := qoderModelKeyRe.FindStringSubmatch(tt.input)
			assert.Len(t, m, 2, "should capture one submatch")
			assert.Equal(t, tt.expectedID, m[1])
		})
	}
}

func TestQoderSkipModels(t *testing.T) {
	assert.True(t, qoderSkipModels["auto"], "auto should be skipped")
	assert.True(t, qoderSkipModels["ultimate"], "ultimate should be skipped")
	assert.True(t, qoderSkipModels["performance"], "performance should be skipped")
	assert.True(t, qoderSkipModels["efficient"], "efficient should be skipped")
	assert.True(t, qoderSkipModels["lite"], "lite should be skipped")
	assert.False(t, qoderSkipModels["gpt-4.1"], "real model should not be skipped")
}

func TestQoderSource_Registered(t *testing.T) {
	spec := model.BackendSpec{ID: "qoder", Backend: "qoder", DefaultCmd: "qoder"}
	assert.True(t, model.CanDiscoverModels(spec), "qoder should support model discovery")

	src, ok := model.LookupModelSource("qoder")
	require.True(t, ok)
	assert.Equal(t, model.SourceKindPlugin, src.Kind())
}

func TestQoderSource_MissingFileReportsDetail(t *testing.T) {
	orig := qoderTextsPath
	t.Cleanup(func() { qoderTextsPath = orig })
	qoderTextsPath = func() (string, error) { return filepath.Join(t.TempDir(), "absent.json"), nil }

	models, detail := discoverQoderModels()
	assert.Nil(t, models)
	assert.Contains(t, detail, "not found", "the reason must name the missing file")
}

func TestQoderSource_ParsesCatalog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dynamic-texts.json")
	payload := `{"texts":{
		"modelSelector.item.gpt-4.1":"GPT-4.1",
		"modelSelector.item.claude-sonnet-4-6":"Claude Sonnet 4.6",
		"modelSelector.item.auto":"Auto",
		"modelSelector.item.lite":"Lite",
		"modelSelector.item.experts-x":"Expert",
		"modelSelector.item.quest-y":"Quest",
		"modelSelector.item.z_preview":"Preview",
		"modelSelector.item.gpt-4.1.description":"a description"
	}}`
	require.NoError(t, os.WriteFile(path, []byte(payload), 0o644))

	orig := qoderTextsPath
	t.Cleanup(func() { qoderTextsPath = orig })
	qoderTextsPath = func() (string, error) { return path, nil }

	models, detail := discoverQoderModels()
	assert.Empty(t, detail)
	require.Len(t, models, 2, "tier aliases, experts/quest entries, previews and descriptions must be filtered out")

	ids := map[string]string{}
	for _, m := range models {
		ids[m.ID] = m.Name
	}
	assert.Equal(t, "GPT-4.1", ids["gpt-4.1"])
	assert.Equal(t, "Claude Sonnet 4.6", ids["claude-sonnet-4-6"])
}
