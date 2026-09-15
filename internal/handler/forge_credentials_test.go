package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"clawbench/internal/forge"
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

// TestServeForgeCredentials_AcceptsPrivateHost is the regression pin for
// self-hosted instances: tokens must be storable for a private-network host.
//
// These hosts used to be rejected so a token could never be pointed at an
// internal address. That gate is gone by design — the user runs an internal
// GitLab and the UI warns before binding instead.
func TestServeForgeCredentials_AcceptsPrivateHost(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	for _, host := range []string{"127.0.0.1", "10.0.0.5", "169.254.169.254", "localhost"} {
		req := newRequest(t, http.MethodPost, "/api/forge/credentials",
			map[string]any{"host": host, "token": "secret"})
		withAuthCookie(req, model.SessionToken)
		w := callHandler(ServeForgeCredentials, req)

		assert.Equal(t, http.StatusOK, w.Code, "host %s must be accepted", host)
		assert.True(t, model.ConfigInstance.ForgeHasToken(host), "the token must be stored for %s", host)
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
	if runtime.GOOS == "windows" {
		// Windows has no POSIX mode bits: os.Stat reports synthetic permissions
		// (the file came back group/other-readable) and chmod cannot tighten
		// them, so the assertion below cannot hold there. The restriction is
		// enforced by NTFS ACLs on Windows instead.
		t.Skip("Windows does not honor POSIX file modes")
	}
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

// TestServeForgeVerifyToken_RejectsMissingHost guards basic input validation.
func TestServeForgeVerifyToken_RejectsMissingHost(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/forge/verify-token", map[string]any{"token": "ghp_x"})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeVerifyToken, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestServeForgeVerifyToken_NoStoredToken covers verifying with no explicit
// token and nothing saved: it must report the absence rather than probing.
func TestServeForgeVerifyToken_NoStoredToken(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodPost, "/api/forge/verify-token", map[string]any{"host": "github.com"})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeVerifyToken, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, false, resp["ok"])
	assert.Equal(t, "ForgeNoCredential", resp["code"])
}

// TestServeForgeVerifyToken_BadTokenIsAuthError is the core behavior: a token
// the platform rejects must come back as ok:false with an auth code, and must
// NOT be confused with a network failure.
func TestServeForgeVerifyToken_BadTokenIsAuthError(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	// Non-github.com hosts verify through the GitLab client, which always uses
	// https; the test server must therefore be TLS, with the same InsecureTLS
	// opt-in that self-hosted users enable for self-signed certs.
	model.ConfigInstance = model.Config{Forge: model.ForgeConfig{InsecureTLS: true}}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v4/user", r.URL.Path)
		assert.Equal(t, "bad-token", r.Header.Get("Private-Token"))
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"401 Unauthorized"}`))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "https://")

	req := newRequest(t, http.MethodPost, "/api/forge/verify-token", map[string]any{
		"host": host, "token": "bad-token",
	})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeVerifyToken, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, false, resp["ok"])
	assert.Equal(t, string(forge.ErrKindAuth), resp["code"])
}

// TestServeForgeVerifyToken_GoodTokenReturnsIdentity covers the success path.
func TestServeForgeVerifyToken_GoodTokenReturnsIdentity(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{Forge: model.ForgeConfig{InsecureTLS: true}}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "good-token", r.Header.Get("Private-Token"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"username":"octocat","name":"The Octocat"}`))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "https://")

	req := newRequest(t, http.MethodPost, "/api/forge/verify-token", map[string]any{
		"host": host, "token": "good-token",
	})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeVerifyToken, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["ok"])
	assert.Equal(t, "octocat", resp["identity"])
	assert.Equal(t, "The Octocat", resp["name"])
}

// TestServeForgeVerifyToken_VerifiesStoredTokenWhenOmitted covers the "check a
// saved token without retyping it" path.
func TestServeForgeVerifyToken_VerifiesStoredTokenWhenOmitted(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "stored-token", r.Header.Get("Private-Token"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"username":"saved-user"}`))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "https://")

	cfg := model.Config{Forge: model.ForgeConfig{InsecureTLS: true}}
	cfg.SetForgeToken(host, "stored-token")
	model.ConfigInstance = cfg

	req := newRequest(t, http.MethodPost, "/api/forge/verify-token", map[string]any{"host": host})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeVerifyToken, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["ok"])
	assert.Equal(t, "saved-user", resp["identity"])
}

// TestServeForgeVerifyToken_DoesNotPersist is the decoupling guard: verifying a
// token must never write it to config.
func TestServeForgeVerifyToken_DoesNotPersist(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"username":"whoever"}`))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "https://")

	model.ConfigInstance = model.Config{Forge: model.ForgeConfig{InsecureTLS: true}}
	req := newRequest(t, http.MethodPost, "/api/forge/verify-token", map[string]any{
		"host": host, "token": "never-saved",
	})
	withAuthCookie(req, model.SessionToken)
	_ = callHandler(ServeForgeVerifyToken, req)

	assert.Empty(t, model.ConfigInstance.ForgeToken(host), "verify must not store the token")
	assert.NoFileExists(t, filepath.Join(model.DataDir, "config", "config.yaml"), "verify must not write config")
}

// TestForgeVerifyToken_PlatformRouting pins which verifier each host picks.
// github.com is the only host routed to the GitHub client (the spec scopes
// GitHub to github.com), so any other host — including a self-hosted GitHub
// Enterprise address — goes through the GitLab verifier. That is a real
// limitation worth asserting explicitly.
func TestForgeVerifyToken_PlatformRouting(t *testing.T) {
	assert.Equal(t, forge.PlatformGitHub, forge.PlatformForHost(forge.GitHubHost),
		"github.com must route to the GitHub verifier")
	assert.Equal(t, forge.PlatformGitHub, forge.PlatformForHost("github.com:443"),
		"the port must be stripped before matching")
	for _, host := range []string{"gitlab.com", "git.acme.internal", "ghe.corp.example"} {
		assert.Equal(t, forge.PlatformGitLab, forge.PlatformForHost(host),
			"non-github.com hosts route to the GitLab verifier: %s", host)
	}
}

// TestServeForgeVerifyToken_NetworkErrorDistinctFromAuth guards the separation
// the user asked for: an unreachable host must report a network code, not an
// auth failure, so a transient outage is not mistaken for a bad token.
func TestServeForgeVerifyToken_NetworkErrorDistinctFromAuth(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{Forge: model.ForgeConfig{InsecureTLS: true}}

	// Start then immediately close a server so the port refuses connections.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	host := strings.TrimPrefix(srv.URL, "https://")
	srv.Close()

	req := newRequest(t, http.MethodPost, "/api/forge/verify-token", map[string]any{
		"host": host, "token": "whatever",
	})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeVerifyToken, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, false, resp["ok"])
	assert.Equal(t, string(forge.ErrKindNetwork), resp["code"],
		"an unreachable host must not be reported as an auth failure")
}
