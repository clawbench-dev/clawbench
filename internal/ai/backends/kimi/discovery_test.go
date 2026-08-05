package kimi

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKimiDefaultModels_Structure(t *testing.T) {
	require.NotEmpty(t, kimiDefaultModels, "should have default models")

	defaultCount := 0
	for _, m := range kimiDefaultModels {
		assert.NotEmpty(t, m.ID, "model ID should not be empty")
		assert.NotEmpty(t, m.Name, "model Name should not be empty")
		if m.Default {
			defaultCount++
		}
	}
	assert.Equal(t, 1, defaultCount, "exactly one model should be marked as default")
}

func TestDiscoverKimiModels_DefensiveCopy(t *testing.T) {
	originalLen := len(kimiDefaultModels)
	models := DiscoverKimiModels()
	if models == nil {
		assert.Len(t, kimiDefaultModels, originalLen, "original slice should be unchanged")
		return
	}
	require.Len(t, models, originalLen)
	models[0] = model.AgentModel{ID: "mutated"}
	assert.NotEqual(t, "mutated", kimiDefaultModels[0].ID, "should not mutate original")
}

func TestKimiDefaultModels_ContainsKnownModels(t *testing.T) {
	ids := make(map[string]bool)
	for _, m := range kimiDefaultModels {
		ids[m.ID] = true
	}
	assert.True(t, ids["kimi-k3"], "should contain Kimi K3")
	assert.True(t, ids["kimi-k2-0711-chat"], "should contain Kimi K2")
	assert.True(t, ids["kimi-for-coding"], "should contain Kimi K2.7 Code")
	assert.True(t, ids["kimi-for-coding-highspeed"], "should contain Kimi K2.7 Code Highspeed")
	assert.True(t, ids["moonshot-v1-128k"], "should contain Moonshot v1 128K")
}

func TestKimiDefaultModels_K3IsDefault(t *testing.T) {
	found := false
	for _, m := range kimiDefaultModels {
		if m.ID == "kimi-k3" && m.Default {
			found = true
		}
	}
	assert.True(t, found, "kimi-k3 should be the default model")
}
