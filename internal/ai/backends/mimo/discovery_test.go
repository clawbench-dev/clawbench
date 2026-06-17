package mimo

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMimoDefaultModels_Structure(t *testing.T) {
	require.NotEmpty(t, mimoDefaultModels, "should have default models")

	defaultCount := 0
	for _, m := range mimoDefaultModels {
		assert.NotEmpty(t, m.ID, "model ID should not be empty")
		assert.NotEmpty(t, m.Name, "model Name should not be empty")
		if m.Default {
			defaultCount++
		}
	}
	assert.Equal(t, 1, defaultCount, "exactly one model should be marked as default")
}

func TestMimoDefaultModels_FirstIsDefault(t *testing.T) {
	require.NotEmpty(t, mimoDefaultModels)
	assert.True(t, mimoDefaultModels[0].Default, "first model should be default")
	assert.Equal(t, "mimo/mimo-auto", mimoDefaultModels[0].ID)
}

func TestDiscoverMimoModels_DefensiveCopy(t *testing.T) {
	originalLen := len(mimoDefaultModels)
	models := DiscoverMimoModels()
	if models == nil {
		assert.Len(t, mimoDefaultModels, originalLen, "original slice should be unchanged")
		return
	}
	require.Len(t, models, originalLen)
	models[0] = model.AgentModel{ID: "mutated"}
	assert.NotEqual(t, "mutated", mimoDefaultModels[0].ID, "should not mutate original")
}

func TestMimoDefaultModels_ContainsKnownModels(t *testing.T) {
	ids := make(map[string]bool)
	for _, m := range mimoDefaultModels {
		ids[m.ID] = true
	}
	assert.True(t, ids["mimo/mimo-auto"], "should contain MiMo Auto")
	assert.True(t, ids["xiaomi/mimo-v2.5-pro"], "should contain MiMo V2.5 Pro")
}
