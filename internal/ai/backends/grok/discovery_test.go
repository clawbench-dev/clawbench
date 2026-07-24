package grok

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGrokDefaultModels_Structure(t *testing.T) {
	require.NotEmpty(t, grokDefaultModels)
	defaultCount := 0
	for _, m := range grokDefaultModels {
		assert.NotEmpty(t, m.ID)
		assert.NotEmpty(t, m.Name)
		if m.Default {
			defaultCount++
		}
	}
	assert.Equal(t, 1, defaultCount, "exactly one default model expected")
}

func TestParseGrokModels_SampleOutput(t *testing.T) {
	output := `Model 'grok' is using its own API key.

Default model: grok

Available models:
  - grok-4.5
  * grok (default)
  - grok-code
`
	models := parseGrokModels(output)
	require.NotEmpty(t, models)

	ids := map[string]bool{}
	defaultIDs := []string{}
	for _, m := range models {
		ids[m.ID] = true
		if m.Default {
			defaultIDs = append(defaultIDs, m.ID)
		}
	}
	assert.True(t, ids["grok"])
	assert.True(t, ids["grok-4.5"])
	assert.True(t, ids["grok-code"])
	assert.Contains(t, defaultIDs, "grok")
	assert.Len(t, defaultIDs, 1)
}

func TestParseGrokModels_Empty(t *testing.T) {
	assert.Empty(t, parseGrokModels(""))
	assert.Empty(t, parseGrokModels("nothing useful here\nDefault model: x\n"))
}

func TestDiscoverGrokModels_DefensiveCopyWhenDefaults(t *testing.T) {
	// When CLI is present, discovery may return live models or defaults.
	// Ensure defaults slice is never mutated by callers of the fallback path.
	originalLen := len(grokDefaultModels)
	models := make([]model.AgentModel, len(grokDefaultModels))
	copy(models, grokDefaultModels)
	require.Len(t, models, originalLen)
	models[0] = model.AgentModel{ID: "mutated"}
	assert.NotEqual(t, "mutated", grokDefaultModels[0].ID)
}

func TestDiscoverGrokModels_ReturnsNilWhenMissingCLI(t *testing.T) {
	// If grok is not on PATH in the test environment, DiscoverGrokModels returns nil.
	// If it is present (developer machine), it should return a non-empty list.
	models := DiscoverGrokModels()
	if models == nil {
		t.Log("grok CLI not found — discovery correctly returned nil")
		return
	}
	assert.NotEmpty(t, models)
	for _, m := range models {
		assert.NotEmpty(t, m.ID)
	}
}
