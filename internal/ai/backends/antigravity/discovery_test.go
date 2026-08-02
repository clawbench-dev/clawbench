package antigravity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

func TestParseAgyModels_SimpleIDs(t *testing.T) {
	output := `Fetching available models...
gemini-3-pro
gemini-3-flash
gemini-2.5-pro
`
	models := parseAgyModels(output)
	require.Len(t, models, 3)

	assert.Equal(t, "gemini-3-pro", models[0].ID)
	assert.Equal(t, "Gemini 3 Pro", models[0].Name, "known models should use pretty names")
	assert.True(t, models[0].Default)
	assert.Equal(t, "gemini-3-flash", models[1].ID)
	assert.Equal(t, "Gemini 3 Flash", models[1].Name)
	assert.False(t, models[1].Default)
	assert.Equal(t, "gemini-2.5-pro", models[2].ID)
	assert.False(t, models[2].Default)
}

func TestParseAgyModels_DuplicateLines_Collapse(t *testing.T) {
	output := "gemini-3-pro\ngemini-3-pro\ngemini-3-flash\n"
	models := parseAgyModels(output)
	require.Len(t, models, 2)
	assert.Equal(t, "gemini-3-pro", models[0].ID)
	assert.Equal(t, "gemini-3-flash", models[1].ID)
}

func TestParseAgyModels_UnknownModel_UsesRawID(t *testing.T) {
	models := parseAgyModels("some-future-model\n")
	require.Len(t, models, 1)
	assert.Equal(t, "some-future-model", models[0].ID)
	assert.Equal(t, "some-future-model", models[0].Name, "unknown models fall back to raw ID")
}

func TestParseAgyModels_FiltersStatusLines(t *testing.T) {
	output := `You are not logged into Antigravity
Fetching available models...
I0428 10:00:00.000000   1234 loading config
error something went wrong
Failed to fetch models

gemini-3-pro
`
	models := parseAgyModels(output)
	require.Len(t, models, 1)
	assert.Equal(t, "gemini-3-pro", models[0].ID)
}

func TestParseAgyModels_EmptyOutput(t *testing.T) {
	assert.Empty(t, parseAgyModels(""))
	assert.Empty(t, parseAgyModels("\n\n"))
	assert.Empty(t, parseAgyModels("Fetching available models...\nYou are not logged into Antigravity\n"))
}

func TestParseAgyModels_CRLF(t *testing.T) {
	models := parseAgyModels("gemini-3-pro\r\ngemini-3-flash\r\n")
	require.Len(t, models, 2)
	assert.Equal(t, "gemini-3-pro", models[0].ID)
	assert.Equal(t, "gemini-3-flash", models[1].ID)
}

func TestAntigravityDefaults_FirstIsDefault(t *testing.T) {
	models := antigravityDefaults()
	require.Len(t, models, len(antigravityDefaultModels))
	assert.True(t, models[0].Default)
	assert.Equal(t, "gemini-3-pro", models[0].ID)
	assert.False(t, models[1].Default)
}

func TestDiscoverAntigravityModels_Registered(t *testing.T) {
	registry := model.GetBackendRegistry()
	found := false
	for _, spec := range registry {
		if spec.Backend == "antigravity" {
			found = true
			assert.True(t, model.CanDiscoverModels(spec), "antigravity should support model discovery")
		}
	}
	assert.True(t, found, "antigravity should be in the backend registry")
}
