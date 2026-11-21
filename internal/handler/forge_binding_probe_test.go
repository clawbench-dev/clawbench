package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"clawbench/internal/forge"
	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServeForgeBinding_RefusesUnreachableRepository is the regression for
// issue #722: a binding the server accepted, after which every read failed.
// The user saw "bound" in the panel and a load error everywhere else, with
// nothing saying which half was wrong. A repository the binding cannot read
// must therefore be refused AT BIND TIME, carrying the platform's own status.
func TestServeForgeBinding_RefusesUnreachableRepository(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()
	// mockGitLab replaces the whole config (for InsecureTLS), which also resets
	// the master switch — re-enable it after the server is built.
	var probed []string
	host := mockGitLab(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probed = append(probed, r.Method+" "+r.URL.Path)
		// Exactly what a self-hosted instance answers when the request reaches
		// the wrong endpoint: something upstream replies 405 instead of the
		// expected 401/200.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write([]byte(`{"message":"Method Not Allowed"}`))
	}))
	model.ConfigInstance.Forge.Enabled = true

	req := newRequest(t, http.MethodPost, "/api/forge/binding", map[string]any{
		"platform": "gitlab", "host": host, "owner": "acme", "repo": "widgets",
	})
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeBinding, req)

	require.Equal(t, http.StatusBadGateway, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Method Not Allowed", "the platform's message must reach the user")
	require.NotEmpty(t, probed, "the repository must be read once before the binding is stored")
	assert.Contains(t, probed[0], "acme/widgets", "the probe must be the binding's own list query")

	pf, err := service.GetProjectForge(env.ProjectDir)
	require.NoError(t, err)
	assert.Nil(t, pf, "an unreachable repository must not be stored")
}

// TestServeForgeBinding_StoresReachableRepository is the other half: the probe
// must not refuse a repository the instance does serve.
func TestServeForgeBinding_StoresReachableRepository(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	host := mockGitLab(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/issues") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
			return
		}
		t.Errorf("unexpected path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	model.ConfigInstance.Forge.Enabled = true

	req := newRequest(t, http.MethodPost, "/api/forge/binding", map[string]any{
		"platform": "gitlab", "host": host, "owner": "acme", "repo": "widgets",
	})
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeBinding, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	binding, ok := resp["binding"].(map[string]any)
	require.True(t, ok, "got %v", resp)
	assert.Equal(t, "acme/widgets", binding["slug"])

	pf, err := service.GetProjectForge(env.ProjectDir)
	require.NoError(t, err)
	require.NotNil(t, pf)
	assert.Equal(t, "acme/widgets", pf.Slug())
}

// TestServeForgeBinding_StoresWhenHostIsUnreachable pins the other half of the
// refusal rule: a machine that cannot reach the instance must still be able to
// bind it. An internal name that does not resolve (or a closed port) is the
// same connectivity problem the panel reports after binding, and it says
// nothing about whether the repository exists.
func TestServeForgeBinding_StoresWhenHostIsUnreachable(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	// A port nothing listens on: connection refused, not an answer.
	req := newRequest(t, http.MethodPost, "/api/forge/binding", map[string]any{
		"platform": "gitlab", "host": "127.0.0.1:1", "owner": "acme", "repo": "widgets",
	})
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeBinding, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	pf, err := service.GetProjectForge(env.ProjectDir)
	require.NoError(t, err)
	require.NotNil(t, pf, "an unreachable host must not make the repository unbindable")
	assert.Equal(t, "127.0.0.1:1", pf.Host)
}

// TestIsRefusal pins which probe failures are the platform answering.
func TestIsRefusal(t *testing.T) {
	assert.True(t, isRefusal(&forgeError{Kind: forge.ErrKindAuth, Status: 401}))
	assert.True(t, isRefusal(&forgeError{Kind: forge.ErrKindRateLimit, Status: 429}))
	assert.True(t, isRefusal(&forgeError{Kind: forge.ErrKindServer, Status: 502}))
	assert.True(t, isRefusal(&forgeError{Kind: forge.ErrKindUnknown, Status: 405}),
		"405 is an answer from something in front of the instance")

	assert.False(t, isRefusal(&forgeError{Kind: forge.ErrKindNetwork, Status: 0}),
		"a transport failure is not the platform refusing")
	assert.False(t, isRefusal(&forgeError{Kind: forge.ErrKindNotFound, Status: 404}),
		"404 cannot be told apart from a private repository without a token")
	assert.False(t, isRefusal(errors.New("plain error")), "an unclassified error is not a refusal")
}

// forgeError aliases the forge package's error type so the table above reads as
// the values it is about.
type forgeError = forge.Error

// TestServeForgeBinding_StoresOn404 pins the 404 rule: a private repository and
// an instance queried without a token both answer 404, so refusing there would
// make such a repository unbindable rather than merely unreadable.
func TestServeForgeBinding_StoresOn404(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	host := mockGitLab(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"404 Not Found"}`))
	}))
	model.ConfigInstance.Forge.Enabled = true
	req := newRequest(t, http.MethodPost, "/api/forge/binding", map[string]any{
		"platform": "gitlab", "host": host, "owner": "acme", "repo": "widgets",
	})
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeBinding, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	pf, err := service.GetProjectForge(env.ProjectDir)
	require.NoError(t, err)
	require.NotNil(t, pf)
}
