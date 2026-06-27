package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderRegistry_ContainsAllProviders(t *testing.T) {
	expectedProviders := []string{
		"openai", "anthropic", "deepseek", "groq",
		"openrouter", "cerebras", "xai", "mistral", "fireworks",
		"minimax", "minimax-cn", "kimi-coding", "moonshotai", "moonshotai-cn",
		"xiaomi", "xiaomi-token-plan-cn", "xiaomi-token-plan-ams", "xiaomi-token-plan-sgp",
		"zai", "huggingface", "opencode", "vercel-ai-gateway",
		"amazon-bedrock", "azure-openai-responses",
		"cloudflare-ai-gateway", "cloudflare-workers-ai",
		"google-vertex",
	}

	for _, id := range expectedProviders {
		spec, ok := ProviderRegistry[id]
		require.True(t, ok, "ProviderRegistry missing provider: %s", id)
		assert.Equal(t, id, spec.ID)
		assert.NotEmpty(t, spec.Name)
		assert.True(t, spec.SupportsCLI, "provider %s should support CLI", id)
	}
}

func TestProviderRegistry_AllProvidersHaveRequiredFields(t *testing.T) {
	for id, spec := range ProviderRegistry {
		assert.Equal(t, id, spec.ID, "ProviderRegistry key %s should match spec.ID %s", id, spec.ID)
		assert.NotEmpty(t, spec.Name, "provider %s missing Name", id)
		assert.NotEmpty(t, spec.ID, "provider %s missing ID", id)

		// APIFormat must be "openai" or "anthropic" (or empty for enterprise)
		if spec.APIFormat != "" {
			assert.True(t, spec.APIFormat == "openai" || spec.APIFormat == "anthropic",
				"provider %s has invalid APIFormat: %s", id, spec.APIFormat)
		}
	}
}

func TestProviderRegistry_OpenAIFormatProvidersHaveEndpoints(t *testing.T) {
	openaiProviders := []string{
		"openai", "deepseek", "groq", "openrouter", "cerebras", "xai",
		"mistral", "moonshotai", "moonshotai-cn", "xiaomi",
		"xiaomi-token-plan-cn", "xiaomi-token-plan-ams", "xiaomi-token-plan-sgp",
		"zai", "huggingface", "opencode",
	}

	for _, id := range openaiProviders {
		spec, ok := ProviderRegistry[id]
		require.True(t, ok, "missing provider: %s", id)
		assert.Equal(t, "openai", spec.APIFormat, "provider %s should be openai format", id)
		assert.NotEmpty(t, spec.ChatEndpoint, "openai-format provider %s missing ChatEndpoint", id)
		assert.NotEmpty(t, spec.ModelsEndpoint, "openai-format provider %s missing ModelsEndpoint", id)
	}
}

func TestProviderRegistry_AnthropicFormatProviders(t *testing.T) {
	anthropicProviders := []string{
		"anthropic", "fireworks", "minimax", "minimax-cn",
		"kimi-coding", "vercel-ai-gateway",
	}

	for _, id := range anthropicProviders {
		spec, ok := ProviderRegistry[id]
		require.True(t, ok, "missing provider: %s", id)
		assert.Equal(t, "anthropic", spec.APIFormat, "provider %s should be anthropic format", id)
		assert.Empty(t, spec.ModelsEndpoint, "anthropic-format provider %s should have empty ModelsEndpoint", id)
	}
}

func TestGetSummarizeModelHint_V1Models(t *testing.T) {
	models := []ModelInfo{
		{ID: "gpt-5.5", Name: "GPT-5.5"},
		{ID: "gpt-4o-mini", Name: "GPT-4o Mini"},
		{ID: "gpt-5.4", Name: "GPT-5.4"},
	}
	hint := GetSummarizeModelHint(models)
	assert.Equal(t, "gpt-4o-mini", hint, "should pick model matching 'mini' keyword")
}

func TestGetSummarizeModelHint_V1Models_FlashKeyword(t *testing.T) {
	models := []ModelInfo{
		{ID: "deepseek-chat", Name: "DeepSeek Chat"},
		{ID: "deepseek-flash", Name: "DeepSeek Flash"},
	}
	hint := GetSummarizeModelHint(models)
	assert.Equal(t, "deepseek-flash", hint, "should pick model matching 'flash' keyword")
}

func TestGetSummarizeModelHint_V1Models_NoMatchFallsToFirst(t *testing.T) {
	models := []ModelInfo{
		{ID: "some-model-1", Name: "Some Model 1"},
		{ID: "some-model-2", Name: "Some Model 2"},
	}
	hint := GetSummarizeModelHint(models)
	assert.Equal(t, "some-model-1", hint, "should fall back to first model when no keywords match")
}

func TestGetSummarizeModelHint_V1Models_MiniDoesNotMatchPartial(t *testing.T) {
	models := []ModelInfo{
		{ID: "deepseek-chat", Name: "DeepSeek Chat"},
		{ID: "deepseek-flash", Name: "DeepSeek Flash"},
	}
	hint := GetSummarizeModelHint(models)
	assert.Equal(t, "deepseek-flash", hint, "should pick flash model, not match 'mini' inside partial word")
}

func TestGetSummarizeModelHint_V1Models_MiniMatchesHyphenated(t *testing.T) {
	models := []ModelInfo{
		{ID: "gpt-5.5", Name: "GPT-5.5"},
		{ID: "gpt-4o-mini", Name: "GPT-4o Mini"},
	}
	hint := GetSummarizeModelHint(models)
	assert.Equal(t, "gpt-4o-mini", hint, "should match '-mini' segment in 'gpt-4o-mini'")
}

func TestGetSummarizeModelHint_Empty(t *testing.T) {
	hint := GetSummarizeModelHint(nil)
	assert.Equal(t, "", hint, "should return empty when no models available")
}

func TestFindProviderSpec(t *testing.T) {
	spec := FindProviderSpec("openai")
	require.NotNil(t, spec)
	assert.Equal(t, "OpenAI", spec.Name)

	spec = FindProviderSpec("nonexistent")
	assert.Nil(t, spec)
}

// ---------- GetSummarizeModelHint extended tests ----------

func TestGetSummarizeModelHint_V1Models_HaikuKeyword(t *testing.T) {
	models := []ModelInfo{
		{ID: "claude-opus-4", Name: "Opus"},
		{ID: "claude-3.5-haiku", Name: "Haiku"},
	}
	hint := GetSummarizeModelHint(models)
	assert.Equal(t, "claude-3.5-haiku", hint, "should pick haiku keyword model")
}

func TestGetSummarizeModelHint_V1Models_LiteKeyword(t *testing.T) {
	models := []ModelInfo{
		{ID: "deepseek-chat", Name: "Chat"},
		{ID: "deepseek-lite", Name: "Lite"},
	}
	hint := GetSummarizeModelHint(models)
	assert.Equal(t, "deepseek-lite", hint, "should pick lite keyword model")
}

func TestGetSummarizeModelHint_V1Models_SmallKeyword(t *testing.T) {
	models := []ModelInfo{
		{ID: "llama-4-maverick", Name: "Maverick"},
		{ID: "llama-4-small", Name: "Small"},
	}
	hint := GetSummarizeModelHint(models)
	assert.Equal(t, "llama-4-small", hint, "should pick small keyword model")
}

func TestGetSummarizeModelHint_V1Models_DotSeparated(t *testing.T) {
	models := []ModelInfo{
		{ID: "model-pro.v2", Name: "Pro"},
		{ID: "model-mini.v1", Name: "Mini"},
	}
	hint := GetSummarizeModelHint(models)
	assert.Equal(t, "model-mini.v1", hint, "should match mini after dot separator")
}

func TestGetSummarizeModelHint_V1Models_SlashSeparated(t *testing.T) {
	models := []ModelInfo{
		{ID: "provider/pro-model", Name: "Pro"},
		{ID: "provider/flash-model", Name: "Flash"},
	}
	hint := GetSummarizeModelHint(models)
	assert.Equal(t, "provider/flash-model", hint, "should match flash after slash separator")
}

func TestGetSummarizeModelHint_V1Models_UnderscoreSeparated(t *testing.T) {
	models := []ModelInfo{
		{ID: "model_pro", Name: "Pro"},
		{ID: "model_mini", Name: "Mini"},
	}
	hint := GetSummarizeModelHint(models)
	assert.Equal(t, "model_mini", hint, "should match mini after underscore separator")
}

func TestGetSummarizeModelHint_V1Models_PrefixMatch(t *testing.T) {
	models := []ModelInfo{
		{ID: "minimax-chat", Name: "MiniMax"},
	}
	hint := GetSummarizeModelHint(models)
	assert.Equal(t, "minimax-chat", hint, "should match keyword at start of model ID")
}
