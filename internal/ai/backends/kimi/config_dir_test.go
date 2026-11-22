package kimi

import (
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func kimiProvider() *model.Provider {
	return &model.Provider{
		ID:           1,
		Name:         "MiniMax",
		BaseURL:      "https://api.example.com/v1",
		APIKey:       "sk-secret",
		Protocol:     model.ProviderProtocolOpenAI,
		Models:       []string{"kimi-k2", "deepseek-v4.1-flash"},
		DefaultModel: "kimi-k2",
	}
}

func TestConfigTOMLContent_OpenAIProvider(t *testing.T) {
	data, err := ConfigTOMLContent(kimiProvider())
	require.NoError(t, err)

	got := string(data)
	assert.Contains(t, got, `default_model = "kimi-k2"`)
	assert.Contains(t, got, `type = "openai_legacy"`)
	assert.Contains(t, got, `base_url = "https://api.example.com/v1"`)
	assert.Contains(t, got, `api_key = "sk-secret"`)
	// Model ids with dots must be quoted keys, or TOML reads them as nested tables.
	assert.Contains(t, got, `[models."deepseek-v4.1-flash"]`)
	assert.Contains(t, got, "[models.kimi-k2]")
	assert.Contains(t, got, `max_context_size = 128000`)
}

func TestConfigTOMLContent_AnthropicProvider(t *testing.T) {
	p := kimiProvider()
	p.Protocol = model.ProviderProtocolAnthropic

	data, err := ConfigTOMLContent(p)
	require.NoError(t, err)
	assert.Contains(t, string(data), `type = "anthropic"`)
}

func TestConfigTOMLContent_FallsBackToDefaultModel(t *testing.T) {
	p := kimiProvider()
	p.Models = nil

	data, err := ConfigTOMLContent(p)
	require.NoError(t, err)
	assert.Contains(t, string(data), `default_model = "kimi-k2"`)
}

func TestConfigTOMLContent_NoModelsIsAnError(t *testing.T) {
	p := kimiProvider()
	p.Models = nil
	p.DefaultModel = ""

	_, err := ConfigTOMLContent(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no models")
}

func TestTomlKey(t *testing.T) {
	assert.Equal(t, "kimi-k2", tomlKey("kimi-k2"))
	assert.Equal(t, "a_b9", tomlKey("a_b9"))
	assert.Equal(t, `"a.b"`, tomlKey("a.b"))
	assert.Equal(t, `"a b"`, tomlKey("a b"))
	assert.Equal(t, `""`, tomlKey(""))
}

func TestTomlString(t *testing.T) {
	assert.Equal(t, `"plain"`, tomlString("plain"))
	assert.Equal(t, `"a\\b"`, tomlString(`a\b`))
	assert.Equal(t, `"say \"hi\""`, tomlString(`say "hi"`))
	assert.Equal(t, `"line\nnext"`, tomlString("line\nnext"))
	assert.Equal(t, `"tab\there"`, tomlString("tab\there"))
	assert.Equal(t, `"cr\rhere"`, tomlString("cr\rhere"))
}

// Every model must be declared, or kimi cannot resolve the ones the picker
// offers.
func TestConfigTOMLContent_DeclaresEveryModel(t *testing.T) {
	p := kimiProvider()
	p.Models = []string{"a", "b", "c"}

	data, err := ConfigTOMLContent(p)
	require.NoError(t, err)
	assert.Equal(t, 3, strings.Count(string(data), "[models."))
}
