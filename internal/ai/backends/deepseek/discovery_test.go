package deepseek

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDeepSeekModels_RealOutput(t *testing.T) {
	output := `Available models (default: deepseek-v4-pro)
  deepseek-v4-flash (deepseek)
* deepseek-v4-pro (deepseek)
  deepseek-ai/deepseek-v4-pro (nvidia-nim)
  deepseek-ai/deepseek-v4-flash (nvidia-nim)
  gpt-4.1 (openai)
  gpt-4.1-mini (openai)
  deepseek/deepseek-v4-pro (openrouter)
  deepseek/deepseek-v4-flash (openrouter)
  deepseek-coder:1.3b (ollama)
`

	models := parseDeepSeekModels(output)
	require.Len(t, models, 2, "should only include deepseek provider models, not third-party")

	assert.Equal(t, "deepseek/deepseek-v4-flash", models[0].ID)
	assert.Equal(t, "deepseek/deepseek-v4-flash", models[0].Name)
	assert.False(t, models[0].Default, "flash is not the default")
	assert.Equal(t, "deepseek/deepseek-v4-pro", models[1].ID)
	assert.Equal(t, "deepseek/deepseek-v4-pro", models[1].Name)
	assert.True(t, models[1].Default, "pro is the default (marked with *)")
}

func TestParseDeepSeekModels_EmptyOutput(t *testing.T) {
	models := parseDeepSeekModels("no models here")
	assert.Nil(t, models)
}

func TestParseDeepSeekModels_NoDefaultMarker(t *testing.T) {
	output := `  deepseek-v4-flash (deepseek)
  deepseek-v4-pro (deepseek)
`
	models := parseDeepSeekModels(output)
	require.Len(t, models, 2)
	assert.True(t, models[0].Default, "first model should be default as fallback")
	assert.False(t, models[1].Default)
}

func TestParseDeepSeekModels_DefaultFromHeader(t *testing.T) {
	output := `Available models (default: deepseek-v4-pro)
  deepseek-v4-flash (deepseek)
  deepseek-v4-pro (deepseek)
`
	models := parseDeepSeekModels(output)
	require.Len(t, models, 2)
	assert.False(t, models[0].Default)
	assert.True(t, models[1].Default, "should match default from header")
}

func TestParseDeepSeekModels_ProviderPrefixInIDAndName(t *testing.T) {
	output := `Available models (default: deepseek-v4-pro)
* deepseek-v4-pro (deepseek)
  deepseek-v4-flash (deepseek)
`
	models := parseDeepSeekModels(output)
	require.Len(t, models, 2)

	assert.Equal(t, "deepseek/deepseek-v4-pro", models[0].ID)
	assert.Equal(t, "deepseek/deepseek-v4-pro", models[0].Name)
	assert.True(t, models[0].Default)

	assert.Equal(t, "deepseek/deepseek-v4-flash", models[1].ID)
	assert.Equal(t, "deepseek/deepseek-v4-flash", models[1].Name)
}

func TestParseDeepSeekModels_ThirdPartyProviderFiltered(t *testing.T) {
	output := `Available models (default: deepseek-v4-pro)
  deepseek-v4-pro (deepseek)
  deepseek-v4-pro (nvidia-nim)
  gpt-4.1 (openai)
`
	models := parseDeepSeekModels(output)
	require.Len(t, models, 1)
	assert.Equal(t, "deepseek/deepseek-v4-pro", models[0].ID)
}

func TestDiscoverDeepSeekModels_RegistersWithModel(t *testing.T) {
	// Verify that init() registered the discovery function
	spec := model.BackendSpec{ID: "deepseek", Backend: "deepseek", DefaultCmd: "codewhale"}
	assert.True(t, model.CanDiscoverModels(spec), "deepseek should support model discovery via registry")
}
