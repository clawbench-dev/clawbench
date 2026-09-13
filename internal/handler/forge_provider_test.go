package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawbench/internal/forge"
	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRecorder is a shorthand for a fresh response recorder.
func newRecorder() *httptest.ResponseRecorder { return httptest.NewRecorder() }

// httptestTLSServer starts a TLS server and registers its shutdown, returning
// the server so callers can read its URL. It exists so the forge tests do not
// each repeat the defer srv.Close() boilerplate.
func httptestTLSServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// resetForgeIdentityCache clears the process-global login cache so tests that
// use the same host do not observe each other's memoized result.
func resetForgeIdentityCache(t *testing.T) {
	t.Helper()
	forgeIdentityMu.Lock()
	forgeIdentityCache = map[string]forgeIdentityEntry{}
	forgeIdentityMu.Unlock()
	t.Cleanup(func() {
		forgeIdentityMu.Lock()
		forgeIdentityCache = map[string]forgeIdentityEntry{}
		forgeIdentityMu.Unlock()
	})
}

// TestForgeCredentialLogin_ResolvesIdentity covers the happy path: the token's
// login is fetched from the host and memoized.
func TestForgeCredentialLogin_ResolvesIdentity(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	resetForgeIdentityCache(t)
	allowLoopbackForgeHost(t)

	var calls int
	srv := httptestTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.True(t, strings.HasSuffix(r.URL.Path, "/user"))
		assert.Equal(t, "glpat-test", r.Header.Get("PRIVATE-TOKEN"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"username":"octocat"}`))
	}))
	host := strings.TrimPrefix(srv.URL, "https://")

	cfg := model.Config{Forge: model.ForgeConfig{InsecureTLS: true}}
	cfg.SetForgeToken(host, "glpat-test")
	model.ConfigInstance = cfg

	got := ForgeCredentialLogin("gitlab", host)
	assert.Equal(t, "octocat", got)

	// The second call is served from the cache, so the host is not asked again.
	assert.Equal(t, "octocat", ForgeCredentialLogin("gitlab", host))
	assert.Equal(t, 1, calls, "a successful login is memoized")
}

// TestForgeCredentialLogin_UnsupportedPlatform covers the provider-construction
// failure: an unknown platform yields "" (treated as "unknown" by callers).
func TestForgeCredentialLogin_UnsupportedPlatform(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	resetForgeIdentityCache(t)

	// No credential stored for the host, so the probe is never attempted.
	assert.Equal(t, "", ForgeCredentialLogin("bitbucket", "bitbucket.example.com"))
}

// TestForgeCredentialLogin_NoCredentialReturnsEmpty covers the "nothing to
// authenticate with" branch: without a stored token there is no identity to
// resolve, and the host must not be probed at all.
func TestForgeCredentialLogin_NoCredentialReturnsEmpty(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	resetForgeIdentityCache(t)
	model.ConfigInstance = model.Config{}

	var calls int
	srv := httptestTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
	}))
	host := strings.TrimPrefix(srv.URL, "https://")

	assert.Equal(t, "", ForgeCredentialLogin("gitlab", host))
	assert.Zero(t, calls, "no token means no request is made")
}

// TestForgeCredentialLogin_FailureIsNegativeCached verifies a failed lookup is
// cached with the shorter TTL, so a transient outage does not permanently
// disable the anti-recursion check but also is not retried on every event.
func TestForgeCredentialLogin_FailureIsNegativeCached(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	resetForgeIdentityCache(t)
	allowLoopbackForgeHost(t)

	var calls int
	srv := httptestTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"401"}`))
	}))
	host := strings.TrimPrefix(srv.URL, "https://")

	cfg := model.Config{Forge: model.ForgeConfig{InsecureTLS: true}}
	cfg.SetForgeToken(host, "bad-token")
	model.ConfigInstance = cfg

	assert.Equal(t, "", ForgeCredentialLogin("gitlab", host))
	assert.Equal(t, "", ForgeCredentialLogin("gitlab", host))
	assert.Equal(t, 1, calls, "a failed lookup is negative-cached too")

	// The cached failure carries the shorter TTL.
	key := "gitlab|" + strings.ToLower(host)
	forgeIdentityMu.Lock()
	entry, ok := forgeIdentityCache[key]
	forgeIdentityMu.Unlock()
	require.True(t, ok)
	assert.Empty(t, entry.login)
	assert.WithinDuration(t, time.Now(), entry.fetchedAt, time.Minute)
}

// TestForgeCredentialLogin_ExpiredEntryRefetches verifies the TTL actually
// expires: an entry older than the failure TTL is refetched.
func TestForgeCredentialLogin_ExpiredEntryRefetches(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	resetForgeIdentityCache(t)
	allowLoopbackForgeHost(t)

	var calls int
	srv := httptestTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"username":"fresh"}`))
	}))
	host := strings.TrimPrefix(srv.URL, "https://")

	cfg := model.Config{Forge: model.ForgeConfig{InsecureTLS: true}}
	cfg.SetForgeToken(host, "glpat-test")
	model.ConfigInstance = cfg

	// Seed a stale entry so the TTL check must miss.
	key := "gitlab|" + strings.ToLower(host)
	forgeIdentityMu.Lock()
	forgeIdentityCache[key] = forgeIdentityEntry{
		login:     "stale",
		fetchedAt: time.Now().Add(-2 * forgeIdentityTTL),
	}
	forgeIdentityMu.Unlock()

	assert.Equal(t, "fresh", ForgeCredentialLogin("gitlab", host))
	assert.Equal(t, 1, calls, "an expired entry must be refetched")
}

// TestWriteForgeError_MapsKinds covers the error classifier: each forge error
// kind maps to a distinct HTTP status and stable code so the frontend can react.
func TestWriteForgeError_MapsKinds(t *testing.T) {
	cases := []struct {
		kind   forge.ErrorKind
		status int
		code   string
	}{
		{forge.ErrKindAuth, http.StatusUnauthorized, "ForgeAuthFailed"},
		{forge.ErrKindRateLimit, http.StatusTooManyRequests, "ForgeRateLimited"},
		{forge.ErrKindNotFound, http.StatusNotFound, "ForgeNotFound"},
		{forge.ErrKindNetwork, http.StatusBadGateway, "ForgeNetworkError"},
		{forge.ErrKindServer, http.StatusBadGateway, "ForgeServerError"},
		{forge.ErrorKind("something-else"), http.StatusBadGateway, "ForgeError"},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			w := newRecorder()
			writeForgeError(w, &forge.Error{Kind: tc.kind, Message: "boom"})
			assert.Equal(t, tc.status, w.Code)
			resp := mustJSON(t, w.Body.Bytes())
			assert.Equal(t, tc.code, resp["code"])
			assert.Equal(t, "boom", resp["error"])
		})
	}
}

// TestWriteForgeError_RateLimitCarriesRetryAfter verifies the Retry-After header
// and body field are only emitted when the forge supplied a positive delay.
func TestWriteForgeError_RateLimitCarriesRetryAfter(t *testing.T) {
	w := newRecorder()
	writeForgeError(w, &forge.Error{
		Kind:              forge.ErrKindRateLimit,
		Message:           "slow down",
		RetryAfterSeconds: 42,
	})
	assert.Equal(t, "42", w.Header().Get("Retry-After"))
	resp := mustJSON(t, w.Body.Bytes())
	assert.Equal(t, float64(42), resp["retryAfterSeconds"])

	// A non-forge error keeps the generic shape.
	w2 := newRecorder()
	writeForgeError(w2, assert.AnError)
	assert.Equal(t, http.StatusBadGateway, w2.Code)
	assert.Equal(t, "ForgeError", mustJSON(t, w2.Body.Bytes())["code"])
	assert.Empty(t, w2.Header().Get("Retry-After"))
}

// TestNewForgeProvider_RejectsBadInput covers the factory's validation branches:
// a nil binding and an unknown platform both fail rather than returning a
// half-built provider.
func TestNewForgeProvider_RejectsBadInput(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	_, err := newForgeProvider(nil)
	require.Error(t, err)

	_, err = newForgeProvider(&service.ProjectForge{Platform: "bitbucket", Host: "x.example.com"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported platform")
}

// TestNewForgeProvider_BuildsPerPlatform covers both supported platforms,
// including the GitHub Enterprise base-URL branch for a non-github.com host.
func TestNewForgeProvider_BuildsPerPlatform(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	gh, err := newForgeProvider(&service.ProjectForge{
		Platform: "github", Host: forge.GitHubHost, Owner: "acme", Repo: "widgets",
	})
	require.NoError(t, err)
	require.NotNil(t, gh)

	// A self-hosted GitHub host switches to the /api/v3 base URL.
	ghe, err := newForgeProvider(&service.ProjectForge{
		Platform: "github", Host: "ghe.corp.example", Owner: "acme", Repo: "widgets",
	})
	require.NoError(t, err)
	require.NotNil(t, ghe)

	gl, err := newForgeProvider(&service.ProjectForge{
		Platform: "gitlab", Host: "gitlab.example.com", Owner: "group", Repo: "widgets",
	})
	require.NoError(t, err)
	require.NotNil(t, gl)
}

// TestForgeContext_NilContextFallsBack covers the defensive helper: net/http
// always supplies a context, but the fallback must not panic if one is missing.
func TestForgeContext_NilContextFallsBack(t *testing.T) {
	req := &http.Request{}
	assert.NotNil(t, forgeContext(req), "a missing context must fall back to Background")
}

// TestNewForgeProvider_ExportedWrapper covers the exported bridge used by the
// service-layer poller.
func TestNewForgeProvider_ExportedWrapper(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()
	model.ConfigInstance = model.Config{}

	p, err := NewForgeProvider(service.ProjectForge{
		Platform: "gitlab", Host: "gitlab.example.com", Owner: "a", Repo: "b",
	})
	require.NoError(t, err)
	assert.NotNil(t, p)

	_, err = NewForgeProvider(service.ProjectForge{Platform: "nope", Host: "h"})
	assert.Error(t, err)
}
