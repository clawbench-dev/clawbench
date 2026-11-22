package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupClaudeHome points $HOME at a temp dir so readUserSettings never reads the
// developer's own ~/.claude/settings.json. USERPROFILE covers Windows.
func setupClaudeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func claudeProvider() *model.Provider {
	return &model.Provider{
		ID:       1,
		Name:     "MiniMax",
		BaseURL:  "https://api.example.com/anthropic",
		APIKey:   "sk-secret",
		Protocol: model.ProviderProtocolAnthropic,
	}
}

func decodeSettings(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc))
	return doc
}

func TestSettingsJSONContent_WritesEnvBlock(t *testing.T) {
	setupClaudeHome(t)

	data, err := SettingsJSONContent(claudeProvider())
	require.NoError(t, err)

	doc := decodeSettings(t, data)
	env, ok := doc["env"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "https://api.example.com/anthropic", env["ANTHROPIC_BASE_URL"])
	assert.Equal(t, "sk-secret", env["ANTHROPIC_API_KEY"])
	assert.NotContains(t, env, "ANTHROPIC_AUTH_TOKEN")
}

func TestSettingsJSONContent_BearerModeUsesAuthToken(t *testing.T) {
	setupClaudeHome(t)
	p := claudeProvider()
	p.AuthMode = model.ProviderAuthBearer

	data, err := SettingsJSONContent(p)
	require.NoError(t, err)

	env := decodeSettings(t, data)["env"].(map[string]any)
	assert.Equal(t, "sk-secret", env["ANTHROPIC_AUTH_TOKEN"])
	assert.NotContains(t, env, "ANTHROPIC_API_KEY")
}

func TestSettingsJSONContent_NoBaseURLIsAnError(t *testing.T) {
	setupClaudeHome(t)
	p := claudeProvider()
	p.BaseURL = "   "

	_, err := SettingsJSONContent(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no base URL")
}

// The isolated directory is new, so the user's own permissions and plugin list
// have to be carried over or binding a provider would silently switch them off.
func TestSettingsJSONContent_CarriesUserSettingsButOwnsTheEnvKeys(t *testing.T) {
	home := setupClaudeHome(t)
	userDir := filepath.Join(home, model.ProviderUserConfigDir("claude"))
	require.NoError(t, os.MkdirAll(userDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(userDir, "settings.json"), []byte(`{
  "permissions": {"allow": ["Bash(ls:*)"]},
  "model": "sonnet",
  "env": {
    "HTTPS_PROXY": "http://proxy:8080",
    "ANTHROPIC_BASE_URL": "https://stale.example",
    "ANTHROPIC_API_KEY": "stale-key",
    "ANTHROPIC_AUTH_TOKEN": "stale-token"
  }
}`), 0o600))

	data, err := SettingsJSONContent(claudeProvider())
	require.NoError(t, err)

	doc := decodeSettings(t, data)
	assert.Equal(t, "sonnet", doc["model"])
	perms, ok := doc["permissions"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{"Bash(ls:*)"}, perms["allow"])

	env := doc["env"].(map[string]any)
	assert.Equal(t, "http://proxy:8080", env["HTTPS_PROXY"], "variables ClawBench does not own are kept")
	assert.Equal(t, "https://api.example.com/anthropic", env["ANTHROPIC_BASE_URL"])
	assert.Equal(t, "sk-secret", env["ANTHROPIC_API_KEY"])
	assert.NotContains(t, env, "ANTHROPIC_AUTH_TOKEN", "a stale credential of the other kind must not survive")
}

func TestSettingsJSONContent_BrokenUserSettingsAreIgnored(t *testing.T) {
	home := setupClaudeHome(t)
	userDir := filepath.Join(home, model.ProviderUserConfigDir("claude"))
	require.NoError(t, os.MkdirAll(userDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(userDir, "settings.json"), []byte("{not json"), 0o600))

	data, err := SettingsJSONContent(claudeProvider())
	require.NoError(t, err, "an unreadable user file only means nothing to carry over")
	env := decodeSettings(t, data)["env"].(map[string]any)
	assert.Equal(t, "sk-secret", env["ANTHROPIC_API_KEY"])
}

func TestReadUserSettings_NoFile(t *testing.T) {
	setupClaudeHome(t)
	_, ok := readUserSettings()
	assert.False(t, ok)
}
