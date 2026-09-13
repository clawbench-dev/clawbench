package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"clawbench/internal/forge"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockGitLab starts a TLS server that answers the GitLab v4 endpoints used by
// the forge provider, and binds the project to it. Non-github.com hosts always
// use the GitLab client over https, so the server must be TLS and the test must
// opt into InsecureTLS (exactly what a self-hosted user does for a self-signed
// certificate).
//
// It returns the host (host:port) to bind and a pointer to the request log so
// callers can assert which endpoint was hit.
func mockGitLab(t *testing.T, handler http.Handler) string {
	t.Helper()
	allowLoopbackForgeHost(t)
	model.ConfigInstance = model.Config{Forge: model.ForgeConfig{InsecureTLS: true}}
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "https://")
}

// bindMockGitLab binds the project to a mock GitLab host.
func bindMockGitLab(t *testing.T, env *testEnv, handler http.Handler) {
	t.Helper()
	host := mockGitLab(t, handler)
	bindProject(t, env.ProjectDir, "gitlab", host)
}

// TestServeForgeItems_AgainstMockGitLab drives the full items path: binding →
// provider → mocked GitLab API → normalized forgeItemView. It also covers the
// "mine" filter, which needs a second call to /user.
func TestServeForgeItems_AgainstMockGitLab(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	bindMockGitLab(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/user"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"username":"me","name":"Me"}`))
		case strings.HasSuffix(r.URL.Path, "/issues"):
			assert.Equal(t, "opened", r.URL.Query().Get("state"))
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Next-Page", "2")
			_, _ = w.Write([]byte(`[
				{"iid":1,"title":"first","state":"opened","description":"body",
				 "author":{"username":"alice"},"assignees":[{"username":"me"}],
				 "labels":["bug"],"user_notes_count":3,
				 "web_url":"https://gitlab.example.com/acme/widgets/-/issues/1",
				 "created_at":"2026-09-01T10:00:00Z","updated_at":"2026-09-02T10:00:00Z"}
			]`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	req := newRequest(t, http.MethodGet, "/api/forge/items?type=issue&state=open&perPage=10", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeItems, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, true, resp["hasMore"], "X-Next-Page must surface as hasMore")
	assert.Equal(t, float64(2), resp["nextPage"])

	items := resp["items"].([]any)
	require.Len(t, items, 1)
	it := items[0].(map[string]any)
	assert.Equal(t, float64(1), it["number"])
	assert.Equal(t, "first", it["title"])
	assert.Equal(t, "alice", it["author"])
	assert.Equal(t, []any{"me"}, it["assignees"])
	assert.Equal(t, []any{"bug"}, it["labels"])
	assert.Equal(t, float64(3), it["commentCount"])
	assert.Equal(t, "acme/widgets", it["slug"])
	assert.Equal(t, "gitlab", it["platform"])

	binding := resp["binding"].(map[string]any)
	assert.Equal(t, "acme", binding["owner"])
}

// TestServeForgeItems_MineFilterUsesTokenIdentity covers the server-side "mine"
// filter: the item's assignee matches the token's login, so it survives.
func TestServeForgeItems_MineFilterUsesTokenIdentity(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	bindMockGitLab(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/user") {
			_, _ = w.Write([]byte(`{"username":"me"}`))
			return
		}
		_, _ = w.Write([]byte(`[
			{"iid":1,"title":"mine","state":"opened","author":{"username":"me"},"assignees":[],"labels":[]},
			{"iid":2,"title":"theirs","state":"opened","author":{"username":"other"},"assignees":[],"labels":[]}
		]`))
	}))

	req := newRequest(t, http.MethodGet, "/api/forge/items?type=issue&mine=1&mineScope=created", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeItems, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	items := resp["items"].([]any)
	require.Len(t, items, 1, "only items authored by the token identity survive")
	assert.Equal(t, float64(1), items[0].(map[string]any)["number"])
}

// TestServeForgeItem_AgainstMockGitLab covers the single-item endpoint and the
// toForgeItemView conversion it shares with the list endpoint.
func TestServeForgeItem_AgainstMockGitLab(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	bindMockGitLab(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/issues/7")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"iid":7,"title":"detail","state":"closed","description":"d",
			"author":{"username":"bob"},"labels":[],"web_url":"u",
			"created_at":"2026-09-01T10:00:00Z","updated_at":"2026-09-02T10:00:00Z"}`))
	}))

	req := newRequest(t, http.MethodGet, "/api/forge/item?type=issue&number=7", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeItem, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	item := resp["item"].(map[string]any)
	assert.Equal(t, float64(7), item["number"])
	assert.Equal(t, "closed", item["state"])
	assert.Equal(t, "bob", item["author"])
}

// TestServeForgeItem_ChangeRequestType covers the PR branch of the type query.
func TestServeForgeItem_ChangeRequestType(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	bindMockGitLab(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/merge_requests/9")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"iid":9,"title":"mr","state":"merged","draft":true,
			"author":{"username":"bob"},"labels":[],"web_url":"u",
			"merged_at":"2026-09-03T10:00:00Z",
			"created_at":"2026-09-01T10:00:00Z","updated_at":"2026-09-02T10:00:00Z"}`))
	}))

	req := newRequest(t, http.MethodGet, "/api/forge/item?type=pr&number=9", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeItem, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	item := resp["item"].(map[string]any)
	assert.Equal(t, "merged", item["state"])
	assert.Equal(t, true, item["draft"])
}

// TestServeForgeComments_AgainstMockGitLab covers the comment endpoint, which
// also exercises the system-note filter in the provider.
func TestServeForgeComments_AgainstMockGitLab(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	bindMockGitLab(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/issues/3/notes")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":1,"body":"real comment","system":false,"author":{"username":"alice"},
			 "created_at":"2026-09-01T10:00:00Z","updated_at":"2026-09-01T10:00:00Z"},
			{"id":2,"body":"changed the description","system":true,"author":{"username":"alice"},
			 "created_at":"2026-09-01T11:00:00Z","updated_at":"2026-09-01T11:00:00Z"}
		]`))
	}))

	req := newRequest(t, http.MethodGet, "/api/forge/comments?type=issue&number=3", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeComments, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	comments := resp["comments"].([]any)
	require.Len(t, comments, 1, "system notes must be filtered out")
	c := comments[0].(map[string]any)
	assert.Equal(t, "real comment", c["body"])
	assert.Equal(t, "alice", c["author"])
}

// TestServeForgeTest_AgainstMockGitLab covers the connectivity probe: a healthy
// host reports ok:true with the identity, and the binding is echoed back.
func TestServeForgeTest_AgainstMockGitLab(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	bindMockGitLab(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.True(t, strings.HasSuffix(r.URL.Path, "/user"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"username":"probe-user","name":"Probe"}`))
	}))

	req := newRequest(t, http.MethodPost, "/api/forge/test", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeTest, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["ok"])
	assert.Equal(t, "probe-user", resp["identity"])
	assert.NotNil(t, resp["binding"])
}

// TestServeForgeTest_NoBinding covers the unbound case: it reports ok:false
// (HTTP 200 — the probe itself ran) with the NoForgeBinding code.
func TestServeForgeTest_NoBinding(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/forge/test", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeTest, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, false, mustJSON(t, w.Body.Bytes())["ok"])
	assert.Equal(t, "NoForgeBinding", mustJSON(t, w.Body.Bytes())["code"])
}

// TestServeForgeTest_AuthFailureClassified verifies a rejected credential is
// reported as an auth kind rather than a generic error, so the UI can point at
// the settings page.
func TestServeForgeTest_AuthFailureClassified(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	bindMockGitLab(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"401 Unauthorized"}`))
	}))

	req := newRequest(t, http.MethodPost, "/api/forge/test", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeTest, req)

	require.Equal(t, http.StatusOK, w.Code)
	resp := mustJSON(t, w.Body.Bytes())
	assert.Equal(t, false, resp["ok"])
	assert.Equal(t, "auth", resp["code"])
}

// TestServeForgeTest_MethodNotAllowed covers the verb guard.
func TestServeForgeTest_MethodNotAllowed(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()
	req := newRequest(t, http.MethodGet, "/api/forge/test", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeTest, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// TestServeForgeItem_NoBinding covers the 404 branch.
func TestServeForgeItem_NoBinding(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()
	req := newRequest(t, http.MethodGet, "/api/forge/item?type=issue&number=1", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeItem, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "NoForgeBinding")
}

// TestServeForgeComments_NoBinding covers the 404 branch.
func TestServeForgeComments_NoBinding(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()
	req := newRequest(t, http.MethodGet, "/api/forge/comments?type=issue&number=1", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeComments, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestServeForgeBinding_MethodNotAllowed covers the default switch arm, which
// must advertise the allowed verbs.
func TestServeForgeBinding_MethodNotAllowed(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()
	req := newRequest(t, http.MethodPatch, "/api/forge/binding", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeBinding, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	assert.Equal(t, "GET, POST, DELETE", w.Header().Get("Allow"))
}

// TestServeForgeBinding_ExplicitFieldsMissing covers the validation branch when
// neither a URL nor a complete field set is supplied.
func TestServeForgeBinding_ExplicitFieldsMissing(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()
	req := newRequest(t, http.MethodPost, "/api/forge/binding", map[string]any{"platform": "gitlab"})
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeBinding, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestServeForgeRemotes_ListsParsedRemotes covers the remotes endpoint: a forge
// remote is parsed into its parts, a non-forge remote is returned unparsed.
func TestServeForgeRemotes_ListsParsedRemotes(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	env, teardown := setupForgeEnv(t)
	defer teardown()
	runGitInDir(t, env.ProjectDir, "init")
	runGitInDir(t, env.ProjectDir, "remote", "add", "origin", "https://github.com/acme/widgets.git")
	runGitInDir(t, env.ProjectDir, "remote", "add", "local", "/tmp/some/repo")

	req := newRequest(t, http.MethodGet, "/api/forge/remotes", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeRemotes, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	remotes := resp["remotes"].([]any)
	require.Len(t, remotes, 2)

	byName := map[string]map[string]any{}
	for _, raw := range remotes {
		m := raw.(map[string]any)
		byName[m["name"].(string)] = m
	}
	origin := byName["origin"]
	require.NotNil(t, origin)
	assert.Equal(t, "github", origin["platform"])
	assert.Equal(t, "github.com", origin["host"])
	assert.Equal(t, "acme/widgets", origin["slug"])

	local := byName["local"]
	require.NotNil(t, local)
	_, parsed := local["platform"]
	assert.False(t, parsed, "a non-forge remote must not be parsed")
}

// TestServeForgeRemotes_NotARepoReturnsEmpty covers the error branch: git fails,
// so the endpoint returns an empty list rather than a 500.
func TestServeForgeRemotes_NotARepoReturnsEmpty(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/forge/remotes", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeRemotes, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp["remotes"])
}

// TestServeForgeRemotes_MethodNotAllowed covers the verb guard.
func TestServeForgeRemotes_MethodNotAllowed(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()
	req := newRequest(t, http.MethodPost, "/api/forge/remotes", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeRemotes, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// TestToForgeItemView_NilBindingDoesNotPanic pins the defensive nil check: the
// view is also built for callers that have no binding in hand.
func TestToForgeItemView_NilBindingDoesNotPanic(t *testing.T) {
	v := toForgeItemView(nil, forge.Item{
		Type:   forge.ItemTypeIssue,
		Number: 1,
		Title:  "t",
		State:  forge.StateOpen,
		Author: forge.Author{Login: "a"},
	})
	assert.Equal(t, "", v.Slug)
	assert.Equal(t, "issue", v.Type)
	assert.Equal(t, "a", v.Author)
}

// TestPickForgeRemote_SkipsUnsafeHost verifies the SSRF guard is applied when
// choosing a candidate binding: a loopback remote is never suggested.
func TestPickForgeRemote_SkipsUnsafeHost(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGitInDir(t, dir, "init")
	runGitInDir(t, dir, "remote", "add", "origin", "https://127.0.0.1/acme/widgets.git")

	_, _, ok := pickForgeRemote(dir)
	assert.False(t, ok, "a loopback remote must never be picked as a binding")
}

// TestPickForgeRemote_PrefersOrigin verifies priority: origin wins even when a
// forge remote was added first.
func TestPickForgeRemote_PrefersOrigin(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGitInDir(t, dir, "init")
	runGitInDir(t, dir, "remote", "add", "upstream", "https://github.com/up/stream.git")
	runGitInDir(t, dir, "remote", "add", "origin", "https://github.com/acme/widgets.git")

	remote, name, ok := pickForgeRemote(dir)
	require.True(t, ok)
	assert.Equal(t, "origin", name)
	assert.Equal(t, "acme/widgets", remote.Slug())
}

// mustJSON decodes a JSON object body, failing the test on malformed input.
func mustJSON(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(b, &out))
	return out
}
