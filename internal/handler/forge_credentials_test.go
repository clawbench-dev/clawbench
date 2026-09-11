package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// --- ServeForgeCredentials ---

func TestServeForgeCredentials_SetAndClear(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	// Set a token.
	req := newRequest(t, http.MethodPost, "/api/forge/credentials",
		map[string]any{"host": "github.com", "token": "ghp_secret"})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeCredentials, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "ghp_secret", "the response must not echo the token")
	assert.Equal(t, "ghp_secret", model.ConfigInstance.ForgeToken("github.com"))

	// Clear it.
	req = newRequest(t, http.MethodDelete, "/api/forge/credentials?host=github.com", nil)
	withAuthCookie(req, model.SessionToken)
	w = callHandler(ServeForgeCredentials, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "", model.ConfigInstance.ForgeToken("github.com"))
}

func TestServeForgeCredentials_RejectsUnsafeHost(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	// Loopback / private / metadata hosts must be rejected so a token can never
	// be pointed at an internal address.
	for _, host := range []string{"127.0.0.1", "10.0.0.5", "169.254.169.254", "localhost"} {
		req := newRequest(t, http.MethodPost, "/api/forge/credentials",
			map[string]any{"host": host, "token": "secret"})
		withAuthCookie(req, model.SessionToken)
		w := callHandler(ServeForgeCredentials, req)

		assert.Equal(t, http.StatusBadRequest, w.Code, "host %s must be rejected", host)
		assert.False(t, model.ConfigInstance.ForgeHasToken(host), "no token may be stored for %s", host)
	}
}

func TestServeForgeCredentials_EmptyHostRejected(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodPost, "/api/forge/credentials",
		map[string]any{"host": "", "token": "secret"})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeCredentials, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeForgeCredentials_MethodNotAllowed(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/forge/credentials", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeCredentials, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	assert.Contains(t, w.Header().Get("Allow"), "POST")
}

func TestServeForgeCredentials_HostNormalizedToLowercase(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodPost, "/api/forge/credentials",
		map[string]any{"host": "GitHub.COM", "token": "tok"})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeCredentials, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "tok", model.ConfigInstance.ForgeToken("github.com"),
		"host must be stored lowercased so lookup is stable")
}

// TestWriteConfigYAML_FilePermissions ensures secrets on disk are not
// world-readable.
func TestWriteConfigYAML_FilePermissions(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()

	require.NoError(t, writeConfigYAML(map[string]any{"port": 20000}))

	path := filepath.Join(model.DataDir, "config", "config.yaml")
	info, err := os.Stat(path)
	require.NoError(t, err)

	// The exact mode is masked by umask, so assert the group/other read+write
	// bits are not set (the security-relevant part).
	perm := info.Mode().Perm()
	assert.Zero(t, perm&0o004, "config.yaml must not be world-readable")
	assert.Zero(t, perm&0o040, "config.yaml must not be group-readable")
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
