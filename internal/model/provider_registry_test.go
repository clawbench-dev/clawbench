package model

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testProviderModelsJSON is a minimal fixture for tests that need KnownModels populated.
const testProviderModelsJSON = `{
  "_generated_at": "test",
  "_source": "test",
  "providers": {
    "anthropic": {
      "models": [
        {"id": "claude-sonnet-4-20250514", "name": "Claude Sonnet 4", "context_length": 200000, "max_output_tokens": 64000, "supports_thinking": true, "cost_tier": "expensive"},
        {"id": "claude-3-5-haiku-20241022", "name": "Claude Haiku 3.5", "context_length": 200000, "max_output_tokens": 8192, "supports_thinking": false, "cost_tier": "moderate"}
      ]
    },
    "fireworks": {
      "models": [
        {"id": "fw-model", "name": "FW Model", "context_length": 128000, "max_output_tokens": 4096, "supports_thinking": false, "cost_tier": "cheap"}
      ]
    },
    "minimax": {
      "models": [
        {"id": "mm-model", "name": "MM Model", "context_length": 128000, "max_output_tokens": 4096, "supports_thinking": false, "cost_tier": "cheap"}
      ]
    },
    "minimax-cn": {
      "models": [
        {"id": "mmc-model", "name": "MMC Model", "context_length": 128000, "max_output_tokens": 4096, "supports_thinking": false, "cost_tier": "cheap"}
      ]
    },
    "kimi-coding": {
      "models": [
        {"id": "kc-model", "name": "KC Model", "context_length": 128000, "max_output_tokens": 4096, "supports_thinking": false, "cost_tier": "cheap"}
      ]
    },
    "vercel-ai-gateway": {
      "models": [
        {"id": "va-model", "name": "VA Model", "context_length": 128000, "max_output_tokens": 4096, "supports_thinking": false, "cost_tier": "cheap"}
      ]
    }
  }
}`

// setupProviderModels creates a temp directory with provider_models.json,
// loads it into ProviderRegistry, and restores the registry on cleanup.
func setupProviderModels(t *testing.T) {
	t.Helper()
	tmpDir := t.TempDir()
	dir := filepath.Join(tmpDir, ".clawbench")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "provider_models.json"), []byte(testProviderModelsJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	// Save original ProviderRegistry entries
	origRegistry := make(map[string]ProviderSpec, len(ProviderRegistry))
	for k, v := range ProviderRegistry {
		origRegistry[k] = v
	}
	t.Cleanup(func() {
		for k, v := range origRegistry {
			ProviderRegistry[k] = v
		}
	})
	LoadProviderModelsFromFile(dir)
}

func TestProviderRegistry_ContainsAllWizardReadyProviders(t *testing.T) {
	expectedProviders := []string{
		"openai", "anthropic", "deepseek", "groq",
		"openrouter", "cerebras", "xai", "mistral", "fireworks",
		"minimax", "minimax-cn", "kimi-coding", "moonshotai", "moonshotai-cn",
		"xiaomi", "xiaomi-token-plan-cn", "xiaomi-token-plan-ams", "xiaomi-token-plan-sgp",
		"zai", "huggingface", "opencode", "vercel-ai-gateway",
	}

	for _, id := range expectedProviders {
		spec, ok := ProviderRegistry[id]
		require.True(t, ok, "ProviderRegistry missing provider: %s", id)
		assert.True(t, spec.WizardReady, "provider %s should be WizardReady", id)
		assert.Equal(t, id, spec.ID)
		assert.NotEmpty(t, spec.Name)
		assert.NotEmpty(t, spec.EnvVar, "provider %s should have EnvVar", id)
		assert.True(t, spec.SupportsCLI, "provider %s should support CLI", id)
	}
}

func TestProviderRegistry_EnterpriseProvidersNotWizardReady(t *testing.T) {
	enterpriseProviders := []string{
		"amazon-bedrock", "azure-openai-responses",
		"cloudflare-ai-gateway", "cloudflare-workers-ai",
		"google-vertex",
	}

	for _, id := range enterpriseProviders {
		spec, ok := ProviderRegistry[id]
		require.True(t, ok, "ProviderRegistry missing enterprise provider: %s", id)
		assert.False(t, spec.WizardReady, "enterprise provider %s should NOT be WizardReady", id)
	}
}

func TestProviderRegistry_AllProvidersHaveRequiredFields(t *testing.T) {
	setupProviderModels(t)

	for id, spec := range ProviderRegistry {
		assert.Equal(t, id, spec.ID, "ProviderRegistry key %s should match spec.ID %s", id, spec.ID)
		assert.NotEmpty(t, spec.Name, "provider %s missing Name", id)
		assert.NotEmpty(t, spec.ID, "provider %s missing ID", id)

		if spec.WizardReady {
			assert.NotEmpty(t, spec.EnvVar, "WizardReady provider %s missing EnvVar", id)
		}

		// APIFormat must be "openai" or "anthropic" (or empty for enterprise)
		if spec.APIFormat != "" {
			assert.True(t, spec.APIFormat == "openai" || spec.APIFormat == "anthropic",
				"provider %s has invalid APIFormat: %s", id, spec.APIFormat)
		}

		// Providers with ModelsEndpoint may also have KnownModels (from runtime file)
		// as a fallback when the endpoint is unreachable — this is intentional.
		// Only assert KnownModels are populated for Anthropic-format providers (no ModelsEndpoint).
		if spec.ModelsEndpoint == "" && spec.WizardReady {
			assert.NotEmpty(t, spec.KnownModels,
				"WizardReady provider %s with no ModelsEndpoint should have KnownModels (from runtime file)", id)
		}
	}
}

func TestProviderRegistry_AnthropicFormatProvidersHaveKnownModels(t *testing.T) {
	setupProviderModels(t)

	anthropicProviders := []string{
		"anthropic", "fireworks", "minimax", "minimax-cn",
		"kimi-coding", "vercel-ai-gateway",
	}

	for _, id := range anthropicProviders {
		spec, ok := ProviderRegistry[id]
		require.True(t, ok, "missing provider: %s", id)
		assert.Equal(t, "anthropic", spec.APIFormat, "provider %s should be anthropic format", id)
		assert.Empty(t, spec.ModelsEndpoint, "anthropic-format provider %s should have empty ModelsEndpoint", id)
		assert.NotEmpty(t, spec.KnownModels, "anthropic-format provider %s should have KnownModels", id)

		for _, m := range spec.KnownModels {
			assert.NotEmpty(t, m.ID, "KnownModel in provider %s missing ID", id)
			assert.NotEmpty(t, m.Name, "KnownModel in provider %s missing Name", id)
			assert.NotEmpty(t, m.CostTier, "KnownModel %s in provider %s missing CostTier", m.ID, id)
			assert.True(t, m.CostTier == "cheap" || m.CostTier == "moderate" || m.CostTier == "expensive",
				"KnownModel %s has invalid CostTier: %s", m.ID, m.CostTier)
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

func TestProviderRegistry_AnthropicProviderModels(t *testing.T) {
	setupProviderModels(t)

	spec, ok := ProviderRegistry["anthropic"]
	require.True(t, ok)
	require.NotEmpty(t, spec.KnownModels)

	// Check for key models
	modelIDs := make(map[string]bool)
	for _, m := range spec.KnownModels {
		modelIDs[m.ID] = true
	}
	assert.True(t, modelIDs["claude-sonnet-4-20250514"], "anthropic should include Claude Sonnet 4")
	assert.True(t, modelIDs["claude-3-5-haiku-20241022"], "anthropic should include Claude 3.5 Haiku")

	// Check all KnownModels have valid cost tiers
	for _, m := range spec.KnownModels {
		assert.True(t, m.CostTier == "cheap" || m.CostTier == "moderate" || m.CostTier == "expensive",
			"KnownModel %s has invalid CostTier: %s", m.ID, m.CostTier)
	}
}

func TestGetWizardProviders_ReturnsOnlyWizardReady(t *testing.T) {
	providers := GetWizardProviders()

	assert.NotEmpty(t, providers)
	for _, p := range providers {
		assert.True(t, p.WizardReady, "GetWizardProviders should only return WizardReady providers, got: %s", p.ID)
	}

	// Verify enterprise providers are NOT included
	providerIDs := make(map[string]bool)
	for _, p := range providers {
		providerIDs[p.ID] = true
	}
	assert.False(t, providerIDs["amazon-bedrock"], "enterprise providers should not be in wizard list")
	assert.False(t, providerIDs["azure-openai-responses"], "enterprise providers should not be in wizard list")
	assert.False(t, providerIDs["google-vertex"], "enterprise providers should not be in wizard list")
}

func TestGetWizardProviders_SortedByID(t *testing.T) {
	providers := GetWizardProviders()
	for i := 1; i < len(providers); i++ {
		assert.LessOrEqual(t, providers[i-1].ID, providers[i].ID,
			"GetWizardProviders should be sorted by ID")
	}
}

func TestGetSummarizeModelHint_KnownModels(t *testing.T) {
	setupProviderModels(t)

	spec := ProviderRegistry["anthropic"]
	hint := GetSummarizeModelHint(spec.KnownModels, nil)
	// Anthropic has no "cheap" model in test fixture (expensive + moderate)
	// so the hint should fall back to the first known model
	assert.NotEmpty(t, hint, "should return a model hint for anthropic")
}

func TestGetSummarizeModelHint_V1Models(t *testing.T) {
	models := []ModelInfo{
		{ID: "gpt-5.5", Name: "GPT-5.5"},
		{ID: "gpt-4o-mini", Name: "GPT-4o Mini"},
		{ID: "gpt-5.4", Name: "GPT-5.4"},
	}
	hint := GetSummarizeModelHint(nil, models)
	assert.Equal(t, "gpt-4o-mini", hint, "should pick model matching 'mini' keyword")
}

func TestGetSummarizeModelHint_V1Models_FlashKeyword(t *testing.T) {
	models := []ModelInfo{
		{ID: "deepseek-chat", Name: "DeepSeek Chat"},
		{ID: "deepseek-flash", Name: "DeepSeek Flash"},
	}
	hint := GetSummarizeModelHint(nil, models)
	assert.Equal(t, "deepseek-flash", hint, "should pick model matching 'flash' keyword")
}

func TestGetSummarizeModelHint_V1Models_NoMatchFallsToFirst(t *testing.T) {
	models := []ModelInfo{
		{ID: "some-model-1", Name: "Some Model 1"},
		{ID: "some-model-2", Name: "Some Model 2"},
	}
	hint := GetSummarizeModelHint(nil, models)
	assert.Equal(t, "some-model-1", hint, "should fall back to first model when no keywords match")
}

func TestGetSummarizeModelHint_V1Models_MiniDoesNotMatchPartial(t *testing.T) {
	models := []ModelInfo{
		{ID: "deepseek-chat", Name: "DeepSeek Chat"},
		{ID: "deepseek-flash", Name: "DeepSeek Flash"},
	}
	hint := GetSummarizeModelHint(nil, models)
	assert.Equal(t, "deepseek-flash", hint, "should pick flash model, not match 'mini' inside partial word")
}

func TestGetSummarizeModelHint_V1Models_MiniMatchesHyphenated(t *testing.T) {
	models := []ModelInfo{
		{ID: "gpt-5.5", Name: "GPT-5.5"},
		{ID: "gpt-4o-mini", Name: "GPT-4o Mini"},
	}
	hint := GetSummarizeModelHint(nil, models)
	assert.Equal(t, "gpt-4o-mini", hint, "should match '-mini' segment in 'gpt-4o-mini'")
}

func TestGetSummarizeModelHint_EmptyBoth(t *testing.T) {
	hint := GetSummarizeModelHint(nil, nil)
	assert.Equal(t, "", hint, "should return empty when no models available")
}

func TestFindProviderSpec(t *testing.T) {
	spec := FindProviderSpec("openai")
	require.NotNil(t, spec)
	assert.Equal(t, "OpenAI", spec.Name)

	spec = FindProviderSpec("nonexistent")
	assert.Nil(t, spec)
}

// ---------- KnownModelsToAgentModels tests ----------

func TestKnownModelsToAgentModels_Empty(t *testing.T) {
	result := KnownModelsToAgentModels(nil)
	assert.Nil(t, result)
}

func TestKnownModelsToAgentModels_SetsDefaults(t *testing.T) {
	models := []KnownModel{
		{ID: "model-a", Name: "Model A"},
		{ID: "model-b", Name: "Model B"},
		{ID: "model-c", Name: "Model C"},
	}
	result := KnownModelsToAgentModels(models)
	require.Len(t, result, 3)

	assert.True(t, result[0].Default, "first model should be default")
	assert.Equal(t, "model-a", result[0].ID)

	assert.False(t, result[1].Default, "second model should not be default")
	assert.Equal(t, "model-b", result[1].ID)
	assert.False(t, result[2].Default, "third model should not be default")
	assert.Equal(t, "model-c", result[2].ID)
}

func TestKnownModelsToAgentModels_SingleModel(t *testing.T) {
	models := []KnownModel{
		{ID: "only-model", Name: "Only Model"},
	}
	result := KnownModelsToAgentModels(models)
	require.Len(t, result, 1)
	assert.True(t, result[0].Default)
	assert.Equal(t, "only-model", result[0].ID)
	assert.Equal(t, "Only Model", result[0].Name)
}

// ---------- GetSummarizeModelHint extended tests ----------

func TestGetSummarizeModelHint_KnownModelsCheapFirst(t *testing.T) {
	models := []KnownModel{
		{ID: "expensive-model", Name: "Expensive", CostTier: "expensive"},
		{ID: "cheap-model", Name: "Cheap", CostTier: "cheap"},
		{ID: "moderate-model", Name: "Moderate", CostTier: "moderate"},
	}
	hint := GetSummarizeModelHint(models, nil)
	assert.Equal(t, "cheap-model", hint, "should pick first cheap model")
}

func TestGetSummarizeModelHint_KnownModelsNoCheap(t *testing.T) {
	models := []KnownModel{
		{ID: "moderate-model", Name: "Moderate", CostTier: "moderate"},
		{ID: "expensive-model", Name: "Expensive", CostTier: "expensive"},
	}
	hint := GetSummarizeModelHint(models, nil)
	assert.Equal(t, "moderate-model", hint, "should fall back to first model when no cheap")
}

func TestGetSummarizeModelHint_V1Models_HaikuKeyword(t *testing.T) {
	models := []ModelInfo{
		{ID: "claude-opus-4", Name: "Opus"},
		{ID: "claude-3.5-haiku", Name: "Haiku"},
	}
	hint := GetSummarizeModelHint(nil, models)
	assert.Equal(t, "claude-3.5-haiku", hint, "should pick haiku keyword model")
}

func TestGetSummarizeModelHint_V1Models_LiteKeyword(t *testing.T) {
	models := []ModelInfo{
		{ID: "deepseek-chat", Name: "Chat"},
		{ID: "deepseek-lite", Name: "Lite"},
	}
	hint := GetSummarizeModelHint(nil, models)
	assert.Equal(t, "deepseek-lite", hint, "should pick lite keyword model")
}

func TestGetSummarizeModelHint_V1Models_SmallKeyword(t *testing.T) {
	models := []ModelInfo{
		{ID: "llama-4-maverick", Name: "Maverick"},
		{ID: "llama-4-small", Name: "Small"},
	}
	hint := GetSummarizeModelHint(nil, models)
	assert.Equal(t, "llama-4-small", hint, "should pick small keyword model")
}

func TestGetSummarizeModelHint_V1Models_DotSeparated(t *testing.T) {
	models := []ModelInfo{
		{ID: "model-pro.v2", Name: "Pro"},
		{ID: "model-mini.v1", Name: "Mini"},
	}
	hint := GetSummarizeModelHint(nil, models)
	assert.Equal(t, "model-mini.v1", hint, "should match mini after dot separator")
}

func TestGetSummarizeModelHint_V1Models_SlashSeparated(t *testing.T) {
	models := []ModelInfo{
		{ID: "provider/pro-model", Name: "Pro"},
		{ID: "provider/flash-model", Name: "Flash"},
	}
	hint := GetSummarizeModelHint(nil, models)
	assert.Equal(t, "provider/flash-model", hint, "should match flash after slash separator")
}

func TestGetSummarizeModelHint_V1Models_UnderscoreSeparated(t *testing.T) {
	models := []ModelInfo{
		{ID: "model_pro", Name: "Pro"},
		{ID: "model_mini", Name: "Mini"},
	}
	hint := GetSummarizeModelHint(nil, models)
	assert.Equal(t, "model_mini", hint, "should match mini after underscore separator")
}

func TestGetSummarizeModelHint_KnownModelsTakesPrecedence(t *testing.T) {
	knownModels := []KnownModel{
		{ID: "cheap-known", Name: "Cheap Known", CostTier: "cheap"},
	}
	v1Models := []ModelInfo{
		{ID: "mini-v1", Name: "Mini V1"},
	}
	hint := GetSummarizeModelHint(knownModels, v1Models)
	assert.Equal(t, "cheap-known", hint, "KnownModels cheap should take precedence over v1Models")
}

func TestGetSummarizeModelHint_V1Models_PrefixMatch(t *testing.T) {
	models := []ModelInfo{
		{ID: "minimax-chat", Name: "MiniMax"},
	}
	hint := GetSummarizeModelHint(nil, models)
	assert.Equal(t, "minimax-chat", hint, "should match keyword at start of model ID")
}

// ---------- LoadProviderModelsFromFile tests ----------

func TestLoadProviderModelsFromFile_MissingFile(t *testing.T) {
	LoadProviderModelsFromFile(t.TempDir())
}

func TestLoadProviderModelsFromFile_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	dir := filepath.Join(tmpDir, ".clawbench")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "provider_models.json"), []byte("invalid json"), 0o644))

	LoadProviderModelsFromFile(dir)
}

func TestLoadProviderModelsFromFile_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	dir := filepath.Join(tmpDir, ".clawbench")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "provider_models.json"), []byte(testProviderModelsJSON), 0o644))

	origRegistry := make(map[string]ProviderSpec, len(ProviderRegistry))
	for k, v := range ProviderRegistry {
		origRegistry[k] = v
	}
	t.Cleanup(func() {
		for k, v := range origRegistry {
			ProviderRegistry[k] = v
		}
	})

	LoadProviderModelsFromFile(dir)

	spec := ProviderRegistry["anthropic"]
	assert.NotEmpty(t, spec.KnownModels, "anthropic should have KnownModels after loading file")
	assert.Equal(t, "claude-sonnet-4-20250514", spec.KnownModels[0].ID)
}

func TestLoadProviderModelsFromFile_UnreadableDir(t *testing.T) {
	// Create a directory with an unreadable file (permission denied, not IsNotExist)
	tmpDir := t.TempDir()
	dir := filepath.Join(tmpDir, ".clawbench")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "provider_models.json")
	require.NoError(t, os.WriteFile(path, []byte(testProviderModelsJSON), 0o644))
	// Remove read permission
	require.NoError(t, os.Chmod(path, 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	// Should not panic, just log warning and return
	LoadProviderModelsFromFile(dir)
}
