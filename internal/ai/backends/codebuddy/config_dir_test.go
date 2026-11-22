package codebuddy

import (
	"encoding/json"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func codebuddyProvider() *model.Provider {
	return &model.Provider{
		ID:           1,
		Name:         "MiniMax",
		BaseURL:      "https://api.example.com/v1",
		APIKey:       "sk-secret",
		Protocol:     model.ProviderProtocolOpenAI,
		Models:       []string{"glm-5", "kimi-k2"},
		DefaultModel: "glm-5",
	}
}

func TestModelsJSONContent_DeclaresEveryModel(t *testing.T) {
	data, err := ModelsJSONContent(codebuddyProvider())
	require.NoError(t, err)

	var doc struct {
		Models []map[string]any `json:"models"`
	}
	require.NoError(t, json.Unmarshal(data, &doc))
	require.Len(t, doc.Models, 2)

	first := doc.Models[0]
	assert.Equal(t, "glm-5", first["id"])
	assert.Equal(t, "glm-5", first[model.ProviderConfigJSONNameKey])
	assert.Equal(t, "openai", first["provider"])
	// The per-model url wins over CODEBUDDY_BASE_URL, so it has to be here.
	assert.Equal(t, "https://api.example.com/v1", first["url"])
	assert.Equal(t, "${"+model.ProviderSecretEnvName()+"}", first["apiKey"], "the secret is an env reference")
	assert.Equal(t, true, first["supportsToolCall"])
}

func TestModelsJSONContent_NoModelsIsAnError(t *testing.T) {
	p := codebuddyProvider()
	p.Models = nil
	p.DefaultModel = ""

	_, err := ModelsJSONContent(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no models")
}
