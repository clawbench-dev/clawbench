package service_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// setupConfigDirTest sandboxes the data dir (where per-agent config dirs live)
// and the home dir (the user's own config dirs we symlink back from).
func setupConfigDirTest(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	setHome(t, home)
	origDataDir := model.DataDir
	model.DataDir = filepath.Join(home, ".clawbench")
	t.Cleanup(func() { model.DataDir = origDataDir })
	return home
}

func configDirProvider() *model.Provider {
	return &model.Provider{
		ID: 7, Name: "Probe", Protocol: model.ProviderProtocolOpenAI,
		BaseURL: "https://api.deepseek.com/v1", APIKey: "sk-secret",
		AuthMode: model.ProviderAuthAuto,
		Models:   []string{"deepseek-chat", "deepseek-v4.1-flash"},
	}
}

// CodeBuddy's models.json must carry the endpoint PER MODEL, because that url
// wins over CODEBUDDY_BASE_URL. A file that only named the provider would look
// right and still send every turn to the old endpoint.
func TestApplyProviderConfigDir_CodebuddyWritesPerModelURL(t *testing.T) {
	setupConfigDirTest(t)

	p := configDirProvider()
	require.NoError(t, service.ApplyProviderConfigDirToAgent("codebuddy", "agent-1", p))

	dir := service.ProviderConfigDir("agent-1")
	assert.Equal(t, filepath.Join(model.DataDir, "providers", "agents", "agent-1"), dir,
		"the directory must live inside ClawBench's data dir, not the user's home")

	data, err := os.ReadFile(filepath.Join(dir, "models.json"))
	require.NoError(t, err)

	var doc struct {
		Models []struct {
			ID     string `json:"id"`
			URL    string `json:"url"`
			APIKey string `json:"apiKey"`
		} `json:"models"`
	}
	require.NoError(t, json.Unmarshal(data, &doc))
	require.Len(t, doc.Models, 2)
	for _, m := range doc.Models {
		assert.Equal(t, p.BaseURL, m.URL, "each model needs its own url")
		// The secret is referenced, never written: ${VAR} is expanded by the CLI.
		assert.Equal(t, "${"+model.ProviderSecretEnvName()+"}", m.APIKey)
	}
	assert.NotContains(t, string(data), "sk-secret")
}

// kimi's config has no ${ENV} syntax, so the key IS written — but the file must
// stay 0600 and the model ids must be quoted, because a bare TOML key with a
// dot ("deepseek-v4.1-flash") is parsed as a nested table and the model
// silently disappears.
func TestApplyProviderConfigDir_KimiQuotesDottedModelIDs(t *testing.T) {
	setupConfigDirTest(t)

	p := configDirProvider()
	require.NoError(t, service.ApplyProviderConfigDirToAgent("kimi", "agent-2", p))

	path := filepath.Join(service.ProviderConfigDir("agent-2"), "config.toml")
	if runtime.GOOS != "windows" {
		// Windows has no POSIX permission bits: os.WriteFile with 0600 still
		// reports 0666, so this assertion only holds where the bits mean it.
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "a file holding a key must not be group/world readable")
	}

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)

	// The dotted id must be quoted, not a bare key.
	assert.Contains(t, content, `[models."deepseek-v4.1-flash"]`)
	assert.NotContains(t, content, "[models.deepseek-v4.1-flash]")
	// The provider type follows the protocol.
	assert.Contains(t, content, `type = "openai_legacy"`)
	assert.Contains(t, content, `base_url = "https://api.deepseek.com/v1"`)
}

func TestApplyProviderConfigDir_KimiAnthropicType(t *testing.T) {
	setupConfigDirTest(t)

	p := configDirProvider()
	p.Protocol = model.ProviderProtocolAnthropic
	p.BaseURL = "https://api.minimaxi.com/anthropic"
	require.NoError(t, service.ApplyProviderConfigDirToAgent("kimi", "agent-3", p))

	data, err := os.ReadFile(filepath.Join(service.ProviderConfigDir("agent-3"), "config.toml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), `type = "anthropic"`,
		"an Anthropic provider must not be written as openai_legacy")
}

// pi addresses a model as "<provider>/<model>", so the file must register the
// models under the provider key the picker hands out.
func TestApplyProviderConfigDir_PiRegistersProviderKey(t *testing.T) {
	setupConfigDirTest(t)

	require.NoError(t, service.ApplyProviderConfigDirToAgent("pi", "agent-4", configDirProvider()))

	data, err := os.ReadFile(filepath.Join(service.ProviderConfigDir("agent-4"), "models.json"))
	require.NoError(t, err)

	var doc struct {
		Providers map[string]struct {
			BaseURL string `json:"baseUrl"`
			APIKey  string `json:"apiKey"`
			API     string `json:"api"`
			Models  []struct {
				ID string `json:"id"`
			} `json:"models"`
		} `json:"providers"`
	}
	require.NoError(t, json.Unmarshal(data, &doc))
	entry, ok := doc.Providers[model.ProviderConfigProviderID("pi")]
	require.True(t, ok, "the provider key must match what the picker prefixes")
	assert.Equal(t, "https://api.deepseek.com/v1", entry.BaseURL)
	assert.Equal(t, "$"+model.ProviderSecretEnvName(), entry.APIKey)
	assert.Equal(t, "openai-completions", entry.API)
	require.Len(t, entry.Models, 2)
}

// claude's settings.json is only PARTLY ClawBench's: the `env` block carries the
// endpoint and credential, but permissions, plugins and the rest belong to the
// user. The isolated directory is a new directory, so a file holding only `env`
// would silently drop all of it — which the user experiences as "ClawBench
// turned my approval rules off".
func TestApplyProviderConfigDir_ClaudeKeepsUserSettings(t *testing.T) {
	home := setupConfigDirTest(t)

	userClaude := filepath.Join(home, ".claude")
	require.NoError(t, os.MkdirAll(userClaude, 0o755))
	userSettings := `{
	  "$schema": "https://json.schemastore.org/claude-code-settings.json",
	  "env": {"MY_PROXY": "http://proxy", "ANTHROPIC_BASE_URL": "https://old.example"},
	  "permissions": {"allow": ["Bash(git:*)"]},
	  "enabledPlugins": {"some-plugin": true}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(userClaude, "settings.json"), []byte(userSettings), 0o600))

	p := &model.Provider{
		ID: 9, Name: "ClaudeP", Protocol: model.ProviderProtocolAnthropic,
		BaseURL: "https://api.deepseek.com/anthropic", APIKey: "sk-claude",
		AuthMode: model.ProviderAuthAuto,
	}
	require.NoError(t, service.ApplyProviderConfigDirToAgent("claude", "agent-c", p))

	data, err := os.ReadFile(filepath.Join(service.ProviderConfigDir("agent-c"), "settings.json"))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc))

	// The user's own keys survive.
	assert.Contains(t, doc, "permissions", "approval rules must not be dropped")
	assert.Contains(t, doc, "enabledPlugins")
	assert.Contains(t, doc, "$schema")

	env, ok := doc["env"].(map[string]any)
	require.True(t, ok)
	// ClawBench owns the endpoint and credential...
	assert.Equal(t, "https://api.deepseek.com/anthropic", env["ANTHROPIC_BASE_URL"])
	assert.Equal(t, "sk-claude", env["ANTHROPIC_API_KEY"])
	// ...and leaves the user's other variables alone.
	assert.Equal(t, "http://proxy", env["MY_PROXY"])

	// The user's own file is untouched.
	original, err := os.ReadFile(filepath.Join(userClaude, "settings.json"))
	require.NoError(t, err)
	assert.Equal(t, userSettings, string(original))
}

// A gateway that only accepts x-api-key must not be sent a Bearer token, and
// vice versa: claude reads whichever variable the auth mode names.
func TestApplyProviderConfigDir_ClaudeAuthMode(t *testing.T) {
	setupConfigDirTest(t)

	apiKeyMode := &model.Provider{
		ID: 1, Name: "K", Protocol: model.ProviderProtocolAnthropic,
		BaseURL: "https://gw.example/anthropic", APIKey: "sk-1", AuthMode: model.ProviderAuthAPIKey,
	}
	require.NoError(t, service.ApplyProviderConfigDirToAgent("claude", "agent-k", apiKeyMode))
	data, err := os.ReadFile(filepath.Join(service.ProviderConfigDir("agent-k"), "settings.json"))
	require.NoError(t, err)
	assert.Contains(t, string(data), `"ANTHROPIC_API_KEY": "sk-1"`)
	assert.NotContains(t, string(data), "ANTHROPIC_AUTH_TOKEN")

	bearerMode := &model.Provider{
		ID: 2, Name: "B", Protocol: model.ProviderProtocolAnthropic,
		BaseURL: "https://gw.example/anthropic", APIKey: "tok-1", AuthMode: model.ProviderAuthBearer,
	}
	require.NoError(t, service.ApplyProviderConfigDirToAgent("claude", "agent-b", bearerMode))
	data, err = os.ReadFile(filepath.Join(service.ProviderConfigDir("agent-b"), "settings.json"))
	require.NoError(t, err)
	assert.Contains(t, string(data), `"ANTHROPIC_AUTH_TOKEN": "tok-1"`)
	assert.NotContains(t, string(data), "ANTHROPIC_API_KEY")
}

// claude's file carries no model list, so requiring one would refuse a provider
// that works. The requirement is per-backend, not implied by "has a config file".
func TestApplyProviderConfigDir_ClaudeNeedsNoModels(t *testing.T) {
	setupConfigDirTest(t)

	p := &model.Provider{
		ID: 3, Name: "NoModels", Protocol: model.ProviderProtocolAnthropic,
		BaseURL: "https://api.deepseek.com/anthropic", APIKey: "k",
	}
	require.Empty(t, p.Models)
	assert.NoError(t, model.ProviderConfigDirModelsError("claude", p),
		"claude picks its model per session; it needs no declared list")
	assert.NoError(t, service.ApplyProviderConfigDirToAgent("claude", "agent-nm", p))

	// Without a base URL there is nothing to redirect, so that IS an error.
	noURL := &model.Provider{ID: 4, Name: "NoURL", Protocol: model.ProviderProtocolAnthropic, APIKey: "k"}
	assert.Error(t, service.ApplyProviderConfigDirToAgent("claude", "agent-nu", noURL))
}

// The user's own data (login state, skills, history) must stay visible inside
// the isolated directory — otherwise kimi answers "Authentication required" and
// every agent loses its skills.
func TestEnsureProviderConfigDir_LinksUserDataExceptOwnedFile(t *testing.T) {
	home := setupConfigDirTest(t)

	userKimi := filepath.Join(home, ".kimi")
	require.NoError(t, os.MkdirAll(filepath.Join(userKimi, "sessions"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(userKimi, "credentials"), []byte("token"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(userKimi, "config.toml"), []byte("user-own"), 0o600))

	require.NoError(t, service.ApplyProviderConfigDirToAgent("kimi", "agent-5", configDirProvider()))
	dir := service.ProviderConfigDir("agent-5")

	// The login and the session dir are symlinked back.
	creds := filepath.Join(dir, "credentials")
	info, err := os.Lstat(creds)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink, "credentials must be a link, not a copy")
	target, err := os.Readlink(creds)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(userKimi, "credentials"), target)

	sessions := filepath.Join(dir, "sessions")
	info, err = os.Lstat(sessions)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink)

	// The owned file is ClawBench's, not a link to the user's.
	owned := filepath.Join(dir, "config.toml")
	info, err = os.Lstat(owned)
	require.NoError(t, err)
	assert.Zero(t, info.Mode()&os.ModeSymlink, "the owned file must be a real file")

	// And the user's own file is untouched.
	userContent, err := os.ReadFile(filepath.Join(userKimi, "config.toml"))
	require.NoError(t, err)
	assert.Equal(t, "user-own", string(userContent))
}

func TestRestoreProviderConfigDir_RemovesDirectory(t *testing.T) {
	setupConfigDirTest(t)
	require.NoError(t, service.ApplyProviderConfigDirToAgent("pi", "agent-6", configDirProvider()))

	dir := service.ProviderConfigDir("agent-6")
	_, err := os.Stat(dir)
	require.NoError(t, err)

	require.NoError(t, service.RestoreProviderConfigDir("agent-6"))
	_, err = os.Stat(dir)
	assert.True(t, os.IsNotExist(err), "the directory must be gone")

	// Restoring twice is not an error: most agents never had a binding.
	assert.NoError(t, service.RestoreProviderConfigDir("agent-6"))
}

// A provider with no curated models must be refused at write time. A provider
// block with no models is invisible to the CLI, so the binding would appear to
// succeed and then never be selectable.
func TestApplyProviderConfigDir_RefusesProviderWithNoModels(t *testing.T) {
	setupConfigDirTest(t)

	p := configDirProvider()
	p.Models = nil
	p.DefaultModel = ""
	err := service.ApplyProviderConfigDirToAgent("pi", "agent-7", p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no models")
}

// A backend without a relocatable config dir must fail loudly rather than
// writing something plausible into the user's home.
func TestApplyProviderConfigDir_RefusesBackendWithoutConfigDir(t *testing.T) {
	setupConfigDirTest(t)

	// deepseek is env-driven: it has no config directory to relocate.
	err := service.ApplyProviderConfigDirToAgent("deepseek", "agent-8", configDirProvider())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no relocatable config directory")
}

// The seam is what makes the spawn path reach the writer at all. If it were
// unset, every binding would fail at turn time instead of at bind time.
func TestBuildProviderEnvForAgent_UsesConfigDirWriter(t *testing.T) {
	home := setupConfigDirTest(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".pi", "agent"), 0o755))

	origDataDir := model.DataDir
	model.DataDir = filepath.Join(home, ".clawbench")
	t.Cleanup(func() { model.DataDir = origDataDir })

	env, err := model.BuildProviderEnvForAgent("agent-9", "pi", configDirProvider(), "")
	require.NoError(t, err)

	assert.Contains(t, env, "PI_CODING_AGENT_DIR="+service.ProviderConfigDir("agent-9"))
	// pi's config references the secret through the environment, so the spawn
	// must carry it.
	found := false
	for _, entry := range env {
		if strings.HasPrefix(entry, model.ProviderSecretEnvName()+"=") {
			found = true
			assert.Equal(t, model.ProviderSecretEnvName()+"=sk-secret", entry)
		}
	}
	assert.True(t, found, "pi's config references the key, so the spawn must set it")
}

// kimi writes the key into its file, so it must NOT also be handed over in the
// environment: the reference does not exist there, and a stray variable would
// make the spawn log claim a channel that is not in use.
func TestBuildProviderEnvForAgent_SecretInFileSkipsEnvVar(t *testing.T) {
	home := setupConfigDirTest(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".kimi"), 0o755))

	origDataDir := model.DataDir
	model.DataDir = filepath.Join(home, ".clawbench")
	t.Cleanup(func() { model.DataDir = origDataDir })

	env, err := model.BuildProviderEnvForAgent("agent-10", "kimi", configDirProvider(), "")
	require.NoError(t, err)
	for _, entry := range env {
		assert.NotContains(t, entry, model.ProviderSecretEnvName(),
			"kimi's file holds the key; the environment must not claim otherwise")
	}
}

// TestReconcileProviderConfigDirs_ContinuesAfterOneFailure pins the best-effort
// contract.
//
// The files these directories hold contain the API key in PLAINTEXT. Aborting on
// the first error skipped three things at once: the remaining agents kept stale
// endpoints, the unbound agents' files were never removed, and the orphan prune
// never ran — so an unbound or deleted agent kept its key on disk after the user
// had unbound it, and no later retry the user knows about would clean it up.
// Exactly the failure mode the startup reconcile exists to prevent.
func TestReconcileProviderConfigDirs_ContinuesAfterOneFailure(t *testing.T) {
	setupConfigDirTest(t)
	t.Cleanup(func() {
		model.ReplaceProviders(nil)
		model.ReplaceAgents(nil, nil)
	})

	p := configDirProvider()
	model.ReplaceProviders([]*model.Provider{p})
	pid := p.ID

	// Agent "aaa" is bound, but its private directory is blocked by a regular
	// file where the directory should be — MkdirAll fails for it alone.
	root := filepath.Join(model.DataDir, "providers", "agents")
	require.NoError(t, os.MkdirAll(root, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "aaa-bad"), []byte("not a dir"), 0o600))

	// Agent "mmm-good" is bound and healthy: it must still be written.
	// Agent "zzz-unbound" has no provider: its directory must still be removed.
	goodDir := filepath.Join(root, "mmm-good")
	unboundDir := filepath.Join(root, "zzz-unbound")
	for _, dir := range []string{goodDir, unboundDir} {
		require.NoError(t, os.MkdirAll(dir, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "models.json"), []byte(`{"apiKey":"sk-stale"}`), 0o600))
	}

	agents := []*model.Agent{
		{ID: "aaa-bad", Backend: "codebuddy", ProviderID: &pid},
		{ID: "mmm-good", Backend: "codebuddy", ProviderID: &pid},
		{ID: "zzz-unbound", Backend: "codebuddy"},
	}
	model.ReplaceAgents(
		map[string]*model.Agent{"aaa-bad": agents[0], "mmm-good": agents[1], "zzz-unbound": agents[2]},
		agents,
	)

	err := service.ReconcileProviderConfigDirs()
	require.Error(t, err, "the failing agent must be reported")

	// The healthy agent was still processed.
	data, readErr := os.ReadFile(filepath.Join(goodDir, "models.json"))
	require.NoError(t, readErr, "the agent after the failing one must still have its config written")
	assert.Contains(t, string(data), p.BaseURL, "the healthy agent's file must carry the current endpoint")

	// The unbound agent's plaintext key is gone.
	assert.NoDirExists(t, unboundDir,
		"an unbound agent's config dir holds a plaintext key and must be removed even when another agent failed")
}
