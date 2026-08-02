package grok

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

func TestParseGrokModels_AuthenticatedWithDefault(t *testing.T) {
	output := `Default model: grok-4.5

Available models:
  * grok-4.5 (default)
  * grok-build
  * grok-3
`
	models := parseGrokModels(output)
	require.Len(t, models, 3)

	assert.Equal(t, "grok-4.5", models[0].ID)
	assert.True(t, models[0].Default)
	assert.Equal(t, "grok-build", models[1].ID)
	assert.False(t, models[1].Default)
	assert.Equal(t, "grok-3", models[2].ID)
	assert.False(t, models[2].Default)
}

func TestParseGrokModels_NoDefaultSuffix_FirstIsDefault(t *testing.T) {
	output := `Available models:
  * grok-4.5
  * grok-build
`
	models := parseGrokModels(output)
	require.Len(t, models, 2)
	assert.Equal(t, "grok-4.5", models[0].ID)
	assert.True(t, models[0].Default, "first model should be marked default when no (default) suffix")
	assert.False(t, models[1].Default)
}

func TestParseGrokModels_Unauthenticated(t *testing.T) {
	output := `You are not authenticated.

Default model: grok-4.5

Available models:
  * grok-4.5 (default)
`
	models := parseGrokModels(output)
	require.Len(t, models, 1)
	assert.Equal(t, "grok-4.5", models[0].ID)
	assert.True(t, models[0].Default)
}

func TestParseGrokModels_EmptyOutput(t *testing.T) {
	assert.Empty(t, parseGrokModels(""))
	assert.Empty(t, parseGrokModels("No models available.\n"))
}

func TestParseGrokModels_SkipsNonListLines(t *testing.T) {
	output := `Default model: grok-4.5
Header line
  * grok-4.5 (default)
Some other text
`
	models := parseGrokModels(output)
	require.Len(t, models, 1)
	assert.Equal(t, "grok-4.5", models[0].ID)
}

func TestParseGrokModels_SkipsEmptyStars(t *testing.T) {
	output := `Available models:
  * 
  * grok-build
`
	models := parseGrokModels(output)
	require.Len(t, models, 1)
	assert.Equal(t, "grok-build", models[0].ID)
}

func TestGrokDefaults_FirstIsDefault(t *testing.T) {
	models := grokDefaults()
	require.Len(t, models, len(grokDefaultModels))
	assert.True(t, models[0].Default)
	assert.Equal(t, "grok-4.5", models[0].ID)
}

func TestDiscoverGrokModels_Registered(t *testing.T) {
	// model discovery function should be registered via init()
	registry := model.GetBackendRegistry()
	found := false
	for _, spec := range registry {
		if spec.Backend == "grok" {
			found = true
			assert.True(t, model.CanDiscoverModels(spec), "grok should support model discovery")
		}
	}
	assert.True(t, found, "grok should be in the backend registry")
}
