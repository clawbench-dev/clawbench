package forge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"clawbench/internal/cfgwrite"
	forgecore "clawbench/internal/forge"
	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// TestServeForgeCredentials_URLHostRecordsScheme is the regression pin for the
// reported bug: pasting "https://host" into the host field produced a malformed
// URL and an opaque "network unreachable" error, and storing it as a key meant
// the token could never match the binding.
//
// A URL must now be accepted, reduced to the host key, and its scheme recorded
// as the instance hint.
func TestServeForgeCredentials_URLHostRecordsScheme(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodPost, "/api/forge/credentials",
		map[string]any{"host": "http://gitlab.internal:8080/group/repo", "token": "glpat-x"})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeCredentials, req)

	require.Equal(t, http.StatusOK, w.Code)

	// The key is the bare host, so it matches what a binding stores and what
	// ForgeToken looks up.
	assert.Equal(t, "glpat-x", model.ConfigInstance.ForgeToken("gitlab.internal:8080"),
		"the token must be keyed by the host, not the URL")
	assert.Equal(t, "http", model.ConfigInstance.ForgeScheme("gitlab.internal:8080"),
		"the scheme must be recorded so an http-only instance is reachable")

	// A port is part of the instance identity and must survive.
	assert.False(t, model.ConfigInstance.ForgeHasToken("gitlab.internal"),
		"a different port is a different instance")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "gitlab.internal:8080", resp["host"])
	assert.Equal(t, "http", resp["scheme"])
}

// TestServeForgeCredentials_BareHostKeepsScheme covers the half of the contract
// that is easy to get wrong: a bare host must NOT reset the scheme to https.
// The user may have named it earlier with a URL, and typing the host alone is
// not a statement about the scheme.
func TestServeForgeCredentials_BareHostKeepsScheme(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	// First: name the scheme with a URL.
	req := newRequest(t, http.MethodPost, "/api/forge/credentials",
		map[string]any{"host": "http://gitlab.internal", "token": "glpat-x"})
	withAuthCookie(req, model.SessionToken)
	require.Equal(t, http.StatusOK, callHandler(ServeForgeCredentials, req).Code)
	require.Equal(t, "http", model.ConfigInstance.ForgeScheme("gitlab.internal"))

	// Then: replace the token using a bare host. The hint must survive, or the
	// instance would silently switch to https.
	req = newRequest(t, http.MethodPost, "/api/forge/credentials",
		map[string]any{"host": "gitlab.internal", "token": "glpat-y"})
	withAuthCookie(req, model.SessionToken)
	require.Equal(t, http.StatusOK, callHandler(ServeForgeCredentials, req).Code)

	assert.Equal(t, "glpat-y", model.ConfigInstance.ForgeToken("gitlab.internal"))
	assert.Equal(t, "http", model.ConfigInstance.ForgeScheme("gitlab.internal"),
		"a bare host must not overwrite a scheme the user set with a URL")
}

// TestServeForgeCredentials_DeleteClearsScheme ensures clearing a token also
// drops its hint: a leftover scheme for a host with no credential is state the
// UI cannot act on and would misreport.
func TestServeForgeCredentials_DeleteClearsScheme(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodPost, "/api/forge/credentials",
		map[string]any{"host": "http://gitlab.internal", "token": "glpat-x"})
	withAuthCookie(req, model.SessionToken)
	require.Equal(t, http.StatusOK, callHandler(ServeForgeCredentials, req).Code)

	req = newRequest(t, http.MethodDelete, "/api/forge/credentials?host=gitlab.internal", nil)
	withAuthCookie(req, model.SessionToken)
	require.Equal(t, http.StatusNoContent, callHandler(ServeForgeCredentials, req).Code)

	assert.Equal(t, "", model.ConfigInstance.ForgeToken("gitlab.internal"))
	assert.Equal(t, "", model.ConfigInstance.ForgeScheme("gitlab.internal"),
		"the hint must be cleared with the token")
}

// TestServeForgeVerifyToken_StoredHintDecidesScheme is the regression pin for
// re-verifying a saved credential on an http-only instance.
//
// The host field may be bare (a user re-checking a stored token has no reason to
// retype a URL), so the only thing that can say "this instance is http" is the
// hint recorded when the credential was saved. Without that fallback the check
// would probe https and report a misleading "network unreachable".
func TestServeForgeVerifyToken_StoredHintDecidesScheme(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"username":"octocat","name":"Mona"}`))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	// The credential was saved with a URL, which recorded the hint.
	model.ConfigInstance.SetForgeToken(host, "glpat-x")
	model.ConfigInstance.SetForgeScheme(host, "http")

	// Re-verify with a bare host: the stored hint must supply the scheme.
	req := newRequest(t, http.MethodPost, "/api/forge/verify-token",
		map[string]any{"host": host})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeVerifyToken, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["ok"], "the stored hint must make an http instance verifiable")
	assert.True(t, hit, "the probe must have reached the http server")
	assert.Equal(t, "http", resp["scheme"],
		"the response must report the scheme actually used")
}

// TestServeForgeVerifyToken_HostFieldSchemeOverridesHint covers the precedence
// on the verify path: a scheme typed in the host field is the more specific
// statement about what the user is checking right now.
func TestServeForgeVerifyToken_HostFieldSchemeOverridesHint(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"username":"octocat","name":"Mona"}`))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	model.ConfigInstance.SetForgeToken(host, "glpat-x")
	// A conflicting hint that would fail against this plain-http server.
	model.ConfigInstance.SetForgeScheme(host, "https")

	req := newRequest(t, http.MethodPost, "/api/forge/verify-token",
		map[string]any{"host": "http://" + host, "token": "glpat-x"})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeVerifyToken, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["ok"], "the host field's scheme must win over the stored hint")
	assert.True(t, hit)
	assert.Equal(t, "http", resp["scheme"])
}

// TestServeForgeVerifyToken_UnknownSchemeDefaultsHTTPS guards the fallback: with
// no scheme anywhere the probe must behave exactly as it did before schemes
// existed.
func TestServeForgeVerifyToken_UnknownSchemeDefaultsHTTPS(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	srv := httptestTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"username":"octocat","name":"Mona"}`))
	}))
	host := strings.TrimPrefix(srv.URL, "https://")

	model.ConfigInstance.Forge.InsecureTLS = true // self-signed test cert

	req := newRequest(t, http.MethodPost, "/api/forge/verify-token",
		map[string]any{"host": host, "token": "glpat-x"})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeVerifyToken, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["ok"])
	assert.Equal(t, "https", resp["scheme"])
}

// TestServeForgeCredentials_UnsupportedSchemeNotStored guards against a typo
// becoming visible configuration: "ftp://host" must not be persisted and echoed
// as if it were the scheme in use. Requests would still work (the resolver falls
// back to https for anything unrecognized), so the failure would be silent —
// the settings chip would simply advertise a scheme that is not being used.
func TestServeForgeCredentials_UnsupportedSchemeNotStored(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodPost, "/api/forge/credentials",
		map[string]any{"host": "ftp://gitlab.internal", "token": "tok"})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeCredentials, req)
	require.Equal(t, http.StatusOK, w.Code)

	// The host still normalizes and the token is still usable.
	assert.Equal(t, "tok", model.ConfigInstance.ForgeToken("gitlab.internal"))
	assert.Empty(t, model.ConfigInstance.ForgeScheme("gitlab.internal"),
		"an unusable scheme must not be stored as a hint")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	// The response reports what requests will actually use, not the typo.
	assert.Equal(t, "https", resp["scheme"])
}

// TestServeForgeCredentials_SchemeCaseNormalized pins that a differently-cased
// scheme is stored canonically, so the hint and the resolver agree on the value.
func TestServeForgeCredentials_SchemeCaseNormalized(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodPost, "/api/forge/credentials",
		map[string]any{"host": "HTTP://gitlab.internal", "token": "tok"})
	withAuthCookie(req, model.SessionToken)
	require.Equal(t, http.StatusOK, callHandler(ServeForgeCredentials, req).Code)

	assert.Equal(t, "http", model.ConfigInstance.ForgeScheme("gitlab.internal"))
}

// TestServeForgeCredentials_SchemeSurvivesRestart pins that the hint is
// persisted to config.yaml, not just held in memory: without this the scheme
// would be forgotten on restart and an http-only instance would break.
func TestServeForgeCredentials_SchemeSurvivesRestart(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodPost, "/api/forge/credentials",
		map[string]any{"host": "http://gitlab.internal", "token": "glpat-x"})
	withAuthCookie(req, model.SessionToken)
	require.Equal(t, http.StatusOK, callHandler(ServeForgeCredentials, req).Code)

	// Read config.yaml back and confirm the scheme landed on disk.
	path := filepath.Join(model.DataDir, "config", "config.yaml")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "schemes", "the scheme hint must be persisted")
	assert.Contains(t, string(data), "gitlab.internal", "the host key must be persisted")
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

	require.NoError(t, cfgwrite.Patch(map[string]any{"port": 20000}))

	path := filepath.Join(model.DataDir, "config", "config.yaml")
	info, err := os.Stat(path)
	require.NoError(t, err)

	// The exact mode is masked by umask, so assert the group/other read+write
	// bits are not set (the security-relevant part).
	perm := info.Mode().Perm()
	assert.Zero(t, perm&0o004, "config.yaml must not be world-readable")
	assert.Zero(t, perm&0o040, "config.yaml must not be group-readable")
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
	assert.Equal(t, string(forgecore.ErrKindAuth), resp["code"])
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
	assert.Equal(t, forgecore.PlatformGitHub, forgecore.PlatformForHost(forgecore.GitHubHost),
		"github.com must route to the GitHub verifier")
	assert.Equal(t, forgecore.PlatformGitHub, forgecore.PlatformForHost("github.com:443"),
		"the port must be stripped before matching")
	for _, host := range []string{"gitlab.com", "git.acme.internal", "ghe.corp.example"} {
		assert.Equal(t, forgecore.PlatformGitLab, forgecore.PlatformForHost(host),
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
	assert.Equal(t, string(forgecore.ErrKindNetwork), resp["code"],
		"an unreachable host must not be reported as an auth failure")
}

// TestServeForgeCredentials_PlatformHintStored covers the only way to tell a
// self-hosted Gitea/Forgejo host from a self-hosted GitLab: the user names it.
// The hint must be canonicalized (forgejo → gitea) so one adapter speaks both.
func TestServeForgeCredentials_PlatformHintStored(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodPost, "/api/forge/credentials",
		map[string]any{"host": "git.acme.internal", "token": "tok", "platform": "forgejo"})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeCredentials, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.Equal(t, "tok", model.ConfigInstance.ForgeToken("git.acme.internal"))
	assert.Equal(t, "gitea", model.ConfigInstance.ForgePlatform("git.acme.internal"))

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "gitea", resp["platform"], "the response reports the platform in use")
}

// TestServeForgeCredentials_PlatformKeptWhenOmitted pins the same rule as the
// scheme: re-saving a token without restating the platform must not reset it,
// or a Gitea host would silently become GitLab again.
func TestServeForgeCredentials_PlatformKeptWhenOmitted(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodPost, "/api/forge/credentials",
		map[string]any{"host": "git.acme.internal", "token": "tok1", "platform": "gitea"})
	withAuthCookie(req, model.SessionToken)
	require.Equal(t, http.StatusOK, callHandler(ServeForgeCredentials, req).Code)

	req = newRequest(t, http.MethodPost, "/api/forge/credentials",
		map[string]any{"host": "git.acme.internal", "token": "tok2"})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeCredentials, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.Equal(t, "tok2", model.ConfigInstance.ForgeToken("git.acme.internal"))
	assert.Equal(t, "gitea", model.ConfigInstance.ForgePlatform("git.acme.internal"),
		"a bare re-save says nothing about the platform, so the hint must survive")
}

// TestServeForgeCredentials_InvalidPlatformRejected guards the failure mode that
// would be silent: storing a value no resolver understands looks saved and then
// does nothing.
func TestServeForgeCredentials_InvalidPlatformRejected(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodPost, "/api/forge/credentials",
		map[string]any{"host": "git.acme.internal", "token": "tok", "platform": "bitbucket"})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeCredentials, req)

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Empty(t, model.ConfigInstance.ForgeToken("git.acme.internal"), "nothing may be stored")
	assert.Empty(t, model.ConfigInstance.ForgePlatform("git.acme.internal"))

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, codeForgeInvalidPlatform, resp[jsonCode])
}

// TestServeForgeVerifyToken_UsesStoredPlatformHint is the regression guard for
// the whole point of the hint: a Gitea host must be probed at /api/v1, not with
// the GitLab client at /api/v4. The server here answers ONLY the Gitea path, so
// reaching it at all proves the hint decided the client.
func TestServeForgeVerifyToken_UsesStoredPlatformHint(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/user", r.URL.Path, "a Gitea host must not be probed as GitLab")
		assert.Equal(t, "token tok", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"login":"gitea-user","full_name":"Gitea User"}`))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "https://")

	cfg := model.Config{Forge: model.ForgeConfig{InsecureTLS: true}}
	cfg.SetForgeToken(host, "tok")
	cfg.SetForgeScheme(host, "https")
	cfg.SetForgePlatform(host, "gitea")
	model.ConfigInstance = cfg

	req := newRequest(t, http.MethodPost, "/api/forge/verify-token",
		map[string]any{"host": host})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeVerifyToken, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["ok"], w.Body.String())
	assert.Equal(t, "gitea-user", resp["identity"])
	assert.Equal(t, "gitea", resp["platform"])
}

// TestServeForgeVerifyToken_InvalidPlatformRejected keeps a typo from silently
// verifying against the wrong API.
func TestServeForgeVerifyToken_InvalidPlatformRejected(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodPost, "/api/forge/verify-token",
		map[string]any{"host": "git.acme.internal", "token": "tok", "platform": "bitbucket"})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeVerifyToken, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, false, resp["ok"])
	assert.Equal(t, codeForgeInvalidPlatform, resp[jsonCode])
}

// TestServeForgeVerifyToken_UsesBindingPlatform is the fix for the long-standing
// disagreement: a binding recorded platform=github on a self-hosted host (GitHub
// Enterprise) used to be verified with the GitLab client, because verification
// classified the host from its NAME while browsing classified it from the
// binding. The host here answers only on /api/v3/user, so reaching it at all
// proves the binding decided the platform.
func TestServeForgeVerifyToken_UsesBindingPlatform(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()
	// Self-signed test certificate, the same opt-in a self-hosted user enables.
	model.ConfigInstance.Forge.InsecureTLS = true

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/user", r.URL.Path,
			"an Enterprise host must be probed as GitHub, not as GitLab")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"login":"octocat","name":"The Octocat"}`))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "https://")

	// Bound the way an existing install would be: platform recorded on the
	// binding, no config hint anywhere.
	require.NoError(t, service.UpsertProjectForge(service.ProjectForge{
		ProjectPath: env.ProjectDir,
		Platform:    "github",
		Host:        host,
		Owner:       "acme",
		Repo:        "widgets",
		Source:      "manual",
	}))
	model.ConfigInstance.Forge.Credentials = map[string]string{host: "tok"}
	model.ConfigInstance.Forge.Schemes = map[string]string{host: "https"}

	req := newRequest(t, http.MethodPost, "/api/forge/verify-token", map[string]any{"host": host})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeVerifyToken, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["ok"], w.Body.String())
	assert.Equal(t, "octocat", resp["identity"])
	assert.Equal(t, "github", resp["platform"])
}
