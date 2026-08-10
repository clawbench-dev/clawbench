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
	assert.Equal(t, "Grok 4.5", models[0].Name, "known models should use pretty names")
	assert.True(t, models[0].Default)
	assert.Equal(t, "grok-build", models[1].ID)
	assert.Equal(t, "Grok Build", models[1].Name)
	assert.False(t, models[1].Default)
	assert.Equal(t, "grok-3", models[2].ID)
	assert.Equal(t, "Grok 3", models[2].Name)
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

func TestParseGrokModels_DuplicateDefault_OnlyFirstWins(t *testing.T) {
	output := `Available models:
  * grok-4.5 (default)
  * grok-build (default)
`
	models := parseGrokModels(output)
	require.Len(t, models, 2)
	assert.True(t, models[0].Default, "first (default) marker wins")
	assert.False(t, models[1].Default, "duplicate (default) marker should be ignored")
}

func TestParseGrokModels_UnknownModel_UsesRawID(t *testing.T) {
	output := `Available models:
  * some-future-model
`
	models := parseGrokModels(output)
	require.Len(t, models, 1)
	assert.Equal(t, "some-future-model", models[0].ID)
	assert.Equal(t, "some-future-model", models[0].Name, "unknown models fall back to raw ID")
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

func TestParseGrokModels_DashBulletsAndDefaultLine(t *testing.T) {
	output := `Model 'grok' is using its own API key.

Default model: grok

Available models:
  - grok-4.5
  * grok (default)
  - grok-code
`
	models := parseGrokModels(output)
	require.Len(t, models, 3)

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
	assert.Equal(t, []string{"grok"}, defaultIDs, "only the (default)-marked model is default")
	assert.Equal(t, "Grok Code", models[2].Name, "known dash-list models should use pretty names")
}

func TestParseGrokModels_DedupesIDs(t *testing.T) {
	output := `Available models:
  * grok-4.5 (default)
  - grok-4.5
  * grok-build
`
	models := parseGrokModels(output)
	require.Len(t, models, 2, "duplicate model IDs should be collapsed")
	assert.Equal(t, "grok-4.5", models[0].ID)
	assert.True(t, models[0].Default)
	assert.Equal(t, "grok-build", models[1].ID)
	assert.False(t, models[1].Default)
}

func TestParseGrokModels_DefaultLineFallsBackWhenNoSuffix(t *testing.T) {
	output := `Default model: grok-3

Available models:
  - grok-4.5
  - grok-3
  - grok-build
`
	models := parseGrokModels(output)
	require.Len(t, models, 3)
	assert.Equal(t, "grok-3", models[1].ID)
	assert.True(t, models[1].Default, "the model named by 'Default model:' should be default")
	assert.False(t, models[0].Default)
	assert.False(t, models[2].Default)
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
