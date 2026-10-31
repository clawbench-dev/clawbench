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

	// The migration resolves config.yaml through platform.FindConfigPath, whose
	// fallback is CWD-relative. Chdir into a scratch dir so a test that leaves
	// the data dir without a config never touches the package directory.
	t.Chdir(t.TempDir())

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
	path := setupMigrationEnv(t)
	writeConfigFile(t, path, "ai_summary:\n  model: claude\n")
	model.ConfigInstance.AISummary.Format = "anthropic"
	model.ConfigInstance.AISummary.API = model.APIConfig{BaseURL: "https://gw.example/anthropic", Key: "k"}

	require.NoError(t, MigrateAISummaryProvider())
	provider := model.GetProvider(*model.ConfigInstance.AISummary.ProviderID)
	require.NotNil(t, provider)
	assert.Equal(t, model.ProviderProtocolAnthropic, provider.Protocol)
}

// A failed config write must not leave the provider row behind: the legacy
// fields still drive the next start's retry, and every retry would otherwise
// add another "AI Summary N" row until the name suffix runs out.
func TestMigrateAISummaryProvider_WriteFailureLeavesNoOrphan(t *testing.T) {
	path := setupMigrationEnv(t)
	// Turning the target into a directory makes the atomic rename fail after
	// the provider row has been created.
	require.NoError(t, os.Mkdir(path, 0o755))

	model.ConfigInstance.AISummary.API = model.APIConfig{BaseURL: "https://legacy.example/v1", Key: "sk-legacy"}

	err := MigrateAISummaryProvider()
	require.Error(t, err)

	// The legacy fallback must survive so the configuration still works.
	assert.Nil(t, model.ConfigInstance.AISummary.ProviderID)
	assert.Equal(t, "https://legacy.example/v1", model.ConfigInstance.AISummary.API.BaseURL)

	providers, listErr := service.ListProviders()
	require.NoError(t, listErr)
	assert.Empty(t, providers, "a failed write must not leave an orphan provider")
}

// The migration must edit the config.yaml the server actually loaded. It used
// to hardcode <DataDir>/config/config.yaml, so a real config in the CWD got a
// data-dir stub containing only ai_summary next to it — and that stub won the
// next startup lookup, resetting every other setting.
func TestMigrateAISummaryProvider_WritesTheLoadedConfigFile(t *testing.T) {
	dataConfigPath := setupMigrationEnv(t)

	cwdDir := t.TempDir()
	t.Chdir(cwdDir)
	cwdConfigPath := filepath.Join(cwdDir, "config", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cwdConfigPath), 0o755))
	writeConfigFile(t, cwdConfigPath, `port: 8080
ai_summary:
  model: gpt-4o-mini
  api:
    base_url: https://legacy.example/v1
    key: sk-legacy
`)

	model.ConfigInstance.AISummary.Model = "gpt-4o-mini"
	model.ConfigInstance.AISummary.API = model.APIConfig{BaseURL: "https://legacy.example/v1", Key: "sk-legacy"}

	require.NoError(t, MigrateAISummaryProvider())

	raw := readConfigMap(t, cwdConfigPath)
	assert.EqualValues(t, 8080, raw["port"], "unrelated settings must survive")
	block, ok := raw["ai_summary"].(map[string]any)
	require.True(t, ok)
	assert.NotNil(t, block["provider_id"])
	assert.NotContains(t, block, "api")

	_, statErr := os.Stat(dataConfigPath)
	assert.True(t, os.IsNotExist(statErr), "must not create an ai_summary-only stub at %s", dataConfigPath)
}

// With no config file anywhere the migration must not create one: a fresh file
// would hold only ai_summary and shadow the real configuration.
func TestMigrateAISummaryProvider_NoConfigFileDoesNotCreateOne(t *testing.T) {
	dataConfigPath := setupMigrationEnv(t)
	cwdConfigPath := filepath.Join("config", "config.yaml")

	model.ConfigInstance.AISummary.API = model.APIConfig{BaseURL: "https://legacy.example/v1", Key: "sk-legacy"}

	require.NoError(t, MigrateAISummaryProvider())
	// The in-memory binding is kept so the running process still works.
	require.NotNil(t, model.ConfigInstance.AISummary.ProviderID)

	_, dataErr := os.Stat(dataConfigPath)
	assert.True(t, os.IsNotExist(dataErr), "must not create %s", dataConfigPath)
	_, cwdErr := os.Stat(cwdConfigPath)
	assert.True(t, os.IsNotExist(cwdErr), "must not create %s", cwdConfigPath)
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}
