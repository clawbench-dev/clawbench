package copilot

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopilotDefaultModels_Structure(t *testing.T) {
	require.NotEmpty(t, copilotDefaultModels, "should have default models")

	defaultCount := 0
	for _, m := range copilotDefaultModels {
		assert.NotEmpty(t, m.ID, "model ID should not be empty")
		assert.NotEmpty(t, m.Name, "model Name should not be empty")
		if m.Default {
			defaultCount++
		}
	}
}

func TestDiscoverCopilotModels_DefensiveCopy(t *testing.T) {
	originalLen := len(copilotDefaultModels)
	models := DiscoverCopilotModels()
	if models == nil {
		assert.Len(t, copilotDefaultModels, originalLen, "original slice should be unchanged")
		return
	}
	require.Len(t, models, originalLen)
	models[0] = model.AgentModel{ID: "mutated"}
	assert.NotEqual(t, "mutated", copilotDefaultModels[0].ID, "should not mutate original")
}

func TestCopilotDefaultModels_ContainsKnownModels(t *testing.T) {
	ids := make(map[string]bool)
	for _, m := range copilotDefaultModels {
		ids[m.ID] = true
	}
	assert.True(t, ids["gpt-4.1"], "should contain GPT-4.1")
	assert.True(t, ids["claude-sonnet-4-20250514"], "should contain Claude Sonnet 4")
}
