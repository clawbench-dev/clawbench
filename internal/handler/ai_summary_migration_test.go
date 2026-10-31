package handler

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"
)

func setupMigrationEnv(t *testing.T) string {
	t.Helper()
	db, err := service.InitInMemoryDB()
	require.NoError(t, err)
	cleanup := store.SetDBForTest(db, db)

	origDataDir := model.DataDir
	origCfg := model.ConfigInstance
	model.DataDir = t.TempDir()
	model.ConfigInstance = model.Config{}
	model.ReplaceProviders(nil)

	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
		model.DataDir = origDataDir
		model.ConfigInstance = origCfg
		model.ReplaceProviders(nil)
	})

	require.NoError(t, os.MkdirAll(filepath.Join(model.DataDir, "config"), 0o755))
	return filepath.Join(model.DataDir, "config", "config.yaml")
}

func writeConfigFile(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
}

func readConfigMap(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, yaml.Unmarshal(data, &raw))
	return raw
}

func TestMigrateAISummaryProvider_MovesLegacyEndpoint(t *testing.T) {
	path := setupMigrationEnv(t)
	writeConfigFile(t, path, `ai_summary:
  model: gpt-4o-mini
  format: openai
  api:
    base_url: https://legacy.example/v1
    key: sk-legacy
`)

	model.ConfigInstance.AISummary.Model = "gpt-4o-mini"
	model.ConfigInstance.AISummary.Format = "openai"
	model.ConfigInstance.AISummary.API = model.APIConfig{BaseURL: "https://legacy.example/v1", Key: "sk-legacy"}

	require.NoError(t, MigrateAISummaryProvider())

	require.NotNil(t, model.ConfigInstance.AISummary.ProviderID)
	provider := model.GetProvider(*model.ConfigInstance.AISummary.ProviderID)
	require.NotNil(t, provider)
	assert.Equal(t, "https://legacy.example/v1", provider.BaseURL)
	assert.Equal(t, "sk-legacy", provider.APIKey)
	assert.Equal(t, model.ProviderProtocolOpenAI, provider.Protocol)

	// The legacy endpoint must not survive on disk: the key now lives in the DB.
	raw := readConfigMap(t, path)
	block, ok := raw["ai_summary"].(map[string]any)
	require.True(t, ok)
	assert.EqualValues(t, provider.ID, block["provider_id"])
	assert.Equal(t, "gpt-4o-mini", block["model"])
	assert.NotContains(t, block, "api")
	assert.NotContains(t, block, "format")
	assert.NotContains(t, string(mustRead(t, path)), "sk-legacy")

	// Idempotent: a second run must not create another provider.
	require.NoError(t, MigrateAISummaryProvider())
	providers, err := service.ListProviders()
	require.NoError(t, err)
	assert.Len(t, providers, 1)
}

func TestMigrateAISummaryProvider_NoLegacyEndpointIsNoop(t *testing.T) {
	path := setupMigrationEnv(t)
	writeConfigFile(t, path, "ai_summary:\n  model: gpt-4o-mini\n")

	require.NoError(t, MigrateAISummaryProvider())
	assert.Nil(t, model.ConfigInstance.AISummary.ProviderID)

	providers, err := service.ListProviders()
	require.NoError(t, err)
	assert.Empty(t, providers)
}

func TestMigrateAISummaryProvider_AnthropicFormatKept(t *testing.T) {
	setupMigrationEnv(t)
	model.ConfigInstance.AISummary.Format = "anthropic"
	model.ConfigInstance.AISummary.API = model.APIConfig{BaseURL: "https://gw.example/anthropic", Key: "k"}

	require.NoError(t, MigrateAISummaryProvider())
	provider := model.GetProvider(*model.ConfigInstance.AISummary.ProviderID)
	require.NotNil(t, provider)
	assert.Equal(t, model.ProviderProtocolAnthropic, provider.Protocol)
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}
