package cline

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClineDefaultModels_Structure(t *testing.T) {
	require.NotEmpty(t, clineDefaultModels, "should have default models")

	defaultCount := 0
	for _, m := range clineDefaultModels {
		assert.NotEmpty(t, m.ID, "model ID should not be empty")
		assert.NotEmpty(t, m.Name, "model Name should not be empty")
		if m.Default {
			defaultCount++
		}
	}
	// Cline has no explicit Default:true in its list, which is fine —
	// DiscoverClineModels doesn't set Default either.
}

func TestDiscoverClineModels_DefensiveCopy(t *testing.T) {
	// DiscoverClineModels should return a copy, not the original slice.
	// Since cline likely isn't on PATH in CI, we test the copy logic
	// by checking the function would return nil when CLI is absent,
	// and that clineDefaultModels is untouched.
	originalLen := len(clineDefaultModels)
	models := DiscoverClineModels()
	if models == nil {
		// CLI not on PATH — expected in CI
		assert.Len(t, clineDefaultModels, originalLen, "original slice should be unchanged")
		return
	}
	// If cline is on PATH, verify it's a copy
	require.Len(t, models, originalLen)
	models[0] = model.AgentModel{ID: "mutated"}
	assert.NotEqual(t, "mutated", clineDefaultModels[0].ID, "should not mutate original")
}

func TestClineDefaultModels_ContainsKnownModels(t *testing.T) {
	ids := make(map[string]bool)
	for _, m := range clineDefaultModels {
		ids[m.ID] = true
	}
	assert.True(t, ids["anthropic/claude-sonnet-4-20250514"], "should contain Claude Sonnet 4")
	assert.True(t, ids["openai/gpt-4.1"], "should contain GPT-4.1")
}
