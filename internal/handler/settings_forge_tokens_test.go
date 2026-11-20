package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 这两个测试从 forge_credentials_test.go 搬回来：它们验证的是 GET /api/config
// 的凭据脱敏，属于配置接口的安全性，不是 forge 功能本身。forge 搬进
// internal/plugin/forge 之后，那边不再有 ServeConfig。

// TestServeConfig_ForgeTokensNeverReturned is the security regression guard:
// GET /api/config must report which hosts have a token but must never include
// the token value itself.
func TestServeConfig_ForgeTokensNeverReturned(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.SetForgeToken("github.com", "ghp_super_secret_value")
	cfg.SetForgeToken("git.acme.internal:8443", "glpat-also-secret")
	model.ConfigInstance = cfg

	req := newRequest(t, http.MethodGet, "/api/config", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	require.Equal(t, http.StatusOK, w.Code)

	// The raw body must not contain either secret anywhere.
	body := w.Body.String()
	assert.NotContains(t, body, "ghp_super_secret_value", "GitHub token must never be serialized")
	assert.NotContains(t, body, "glpat-also-secret", "GitLab token must never be serialized")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	forge, ok := resp["forge"].(map[string]any)
	require.True(t, ok, "response must include the forge section")

	hosts, ok := forge["credential_hosts"].([]any)
	require.True(t, ok, "forge must expose credential_hosts")
	got := make([]string, 0, len(hosts))
	for _, h := range hosts {
		got = append(got, h.(string))
	}
	assert.Contains(t, got, "github.com")
	assert.Contains(t, got, "git.acme.internal:8443")

	// No token-bearing field may exist.
	_, hasCreds := forge["credentials"]
	assert.False(t, hasCreds, "the forge section must not expose a credentials map")
}

func TestServeConfig_ForgeNotifyDefaultsVisible(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()

	var cfg model.Config
	model.ApplyDefaults(&cfg, nil)
	model.ConfigInstance = cfg

	req := newRequest(t, http.MethodGet, "/api/config", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	forge := resp["forge"].(map[string]any)
	notify := forge["notify"].(map[string]any)
	for _, key := range []string{"opened", "closed", "merged", "reopened", "commented", "pipeline"} {
		assert.Equal(t, true, notify[key], "notify.%s must default to true in the API response", key)
	}
}

// TestServeConfig_ForgeSchemesExposed covers the settings UI's data source: the
// scheme per host must be reported so an http instance is distinguishable from
// an https one.
func TestServeConfig_ForgeSchemesExposed(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.SetForgeToken("gitlab.internal", "glpat-x")
	cfg.SetForgeScheme("gitlab.internal", "http")
	// A hint with no credential is not actionable, so it must not be reported.
	cfg.SetForgeScheme("orphan.internal", "http")
	model.ConfigInstance = cfg

	req := newRequest(t, http.MethodGet, "/api/config", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	forgeSection, ok := resp["forge"].(map[string]any)
	require.True(t, ok)

	schemes, ok := forgeSection["credential_schemes"].(map[string]any)
	require.True(t, ok, "config must expose credential_schemes")
	assert.Equal(t, "http", schemes["gitlab.internal"])
	_, orphan := schemes["orphan.internal"]
	assert.False(t, orphan, "a hint with no credential must not be reported")

	// The token itself must still never be serialized.
	assert.NotContains(t, w.Body.String(), "glpat-x")
}

// TestServeConfig_PatchForgeNotify verifies the forge notification toggles are
// accepted by the PATCH whitelist and applied to the in-memory config.
func TestServeConfig_PatchForgeNotify(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodPatch, "/api/config", map[string]any{
		"forge": map[string]any{
			"notify": map[string]any{
				"commented": false,
				"pipeline":  true,
			},
		},
	})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.False(t, model.ConfigInstance.Forge.Notify.Commented, "commented toggle must be applied")
	assert.True(t, model.ConfigInstance.Forge.Notify.Pipeline)
}

// TestServeConfig_PatchForgeInsecureTLS verifies the TLS escape hatch is
// patchable.
func TestServeConfig_PatchForgeInsecureTLS(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodPatch, "/api/config", map[string]any{
		"forge": map[string]any{"insecure_tls": true},
	})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, model.ConfigInstance.Forge.InsecureTLS)
}

// TestServeConfig_PatchRejectsUnknownForgeField ensures the whitelist still
// blocks arbitrary forge keys (e.g. a path traversal attempt via credentials).
func TestServeConfig_PatchRejectsUnknownForgeField(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodPatch, "/api/config", map[string]any{
		"forge": map[string]any{"credentials": map[string]any{"evil.com": "tok"}},
	})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, "credentials must not be PATCHable directly")
}

// TestServeConfig_ForgePlatformsExposed covers the settings UI's data source.
// Platforms are reported RESOLVED, so a host that will be spoken to as GitLab
// says so even when nobody recorded a hint.
func TestServeConfig_ForgePlatformsExposed(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.SetForgeToken("github.com", "ghp-x")
	cfg.SetForgeToken("gitlab.internal", "glpat-x")
	cfg.SetForgeToken("git.acme.internal", "gitea-x")
	cfg.SetForgePlatform("git.acme.internal", "gitea")
	// A hint with no credential is not actionable, so it must not be reported.
	cfg.SetForgePlatform("orphan.internal", "gitea")
	model.ConfigInstance = cfg

	req := newRequest(t, http.MethodGet, "/api/config", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	forgeSection, ok := resp["forge"].(map[string]any)
	require.True(t, ok)
	platforms, ok := forgeSection["credential_platforms"].(map[string]any)
	require.True(t, ok, "config must expose credential_platforms")

	assert.Equal(t, "github", platforms["github.com"])
	assert.Equal(t, "gitlab", platforms["gitlab.internal"], "unset hint falls back to the inference")
	assert.Equal(t, "gitea", platforms["git.acme.internal"])
	_, orphan := platforms["orphan.internal"]
	assert.False(t, orphan, "a hint with no credential must not be reported")
}
