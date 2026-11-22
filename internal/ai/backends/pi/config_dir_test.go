package pi

import (
	"encoding/json"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func piProvider() *model.Provider {
	return &model.Provider{
		ID:           1,
		Name:         "MiniMax",
		BaseURL:      "https://api.example.com/v1",
		APIKey:       "sk-secret",
		Protocol:     model.ProviderProtocolOpenAI,
		Models:       []string{"gpt-5", "claude-x"},
		DefaultModel: "gpt-5",
	}
}

func TestModelsJSONContent_DeclaresProviderAndModels(t *testing.T) {
	data, err := ModelsJSONContent(piProvider())
	require.NoError(t, err)

	var doc struct {
		Providers map[string]struct {
			Name    string           `json:"name"`
			BaseURL string           `json:"baseUrl"`
			APIKey  string           `json:"apiKey"`
			API     string           `json:"api"`
			Models  []map[string]any `json:"models"`
		} `json:"providers"`
	}
	require.NoError(t, json.Unmarshal(data, &doc))

	id := model.ProviderConfigProviderID("pi")
	entry, ok := doc.Providers[id]
	require.True(t, ok, "the provider is registered under ClawBench's own key")
	assert.Equal(t, "ClawBench MiniMax", entry.Name)
	assert.Equal(t, "https://api.example.com/v1", entry.BaseURL)
	assert.Equal(t, "$"+model.ProviderSecretEnvName(), entry.APIKey, "the secret stays out of the file")
	assert.Equal(t, "openai-completions", entry.API)
	require.Len(t, entry.Models, 2)
	assert.Equal(t, "gpt-5", entry.Models[0]["id"])
	assert.Equal(t, "gpt-5", entry.Models[0][model.ProviderConfigJSONNameKey])
}

func TestModelsJSONContent_AnthropicProtocol(t *testing.T) {
	p := piProvider()
	p.Protocol = model.ProviderProtocolAnthropic

	data, err := ModelsJSONContent(p)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"api": "anthropic-messages"`)
}

func TestModelsJSONContent_NoModelsIsAnError(t *testing.T) {
	p := piProvider()
	p.Models = nil
	p.DefaultModel = ""

	_, err := ModelsJSONContent(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no models")
}
