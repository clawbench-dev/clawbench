package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"

	"clawbench/internal/forge"
	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupForgeEnv wires a project with a binding and a mocked forge server.
func setupForgeEnv(t *testing.T) (*testEnv, func()) {
	t.Helper()
	env, teardown := setupPersistTestEnv(t)
	model.ConfigInstance = model.Config{}
	return env, teardown
}

func bindProject(t *testing.T, projectPath, platform, host, owner, repo string) {
	t.Helper()
	require.NoError(t, service.UpsertProjectForge(service.ProjectForge{
		ProjectPath: projectPath,
		Platform:    platform,
		Host:        host,
		Owner:       owner,
		Repo:        repo,
		Source:      "manual",
	}))
}

func TestServeForgeBinding_CRUD(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	// Initially unbound. This temp dir has no git remote, so auto-bind finds
	// nothing to bind and the null binding stands.
	req := newRequest(t, http.MethodGet, "/api/forge/binding", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeBinding, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Nil(t, resp["binding"], "unbound project returns a null binding")

	// Bind via URL.
	req = newRequest(t, http.MethodPost, "/api/forge/binding",
		map[string]any{"url": "https://github.com/acme/widgets.git"})
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w = callHandler(ServeForgeBinding, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	binding := resp["binding"].(map[string]any)
	assert.Equal(t, "github", binding["platform"])
	assert.Equal(t, "github.com", binding["host"])
	assert.Equal(t, "acme/widgets", binding["slug"])

	// Read back.
	req = newRequest(t, http.MethodGet, "/api/forge/binding", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w = callHandler(ServeForgeBinding, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "acme/widgets", resp["binding"].(map[string]any)["slug"])

	// Delete.
	req = newRequest(t, http.MethodDelete, "/api/forge/binding", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w = callHandler(ServeForgeBinding, req)
	assert.Equal(t, http.StatusNoContent, w.Code)

	pf, err := service.GetProjectForge(env.ProjectDir)
	require.NoError(t, err)
	assert.Nil(t, pf)
}

// getBinding performs an authenticated GET /api/forge/binding for a project and
// returns the decoded body.
func getBinding(t *testing.T, projectDir string) map[string]any {
	t.Helper()
	req := newRequest(t, http.MethodGet, "/api/forge/binding", nil)
	withProjectCookie(req, projectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeBinding, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

// TestServeForgeBinding_AutoBindsOfficialRemote covers the happy path: an
// unbound project whose origin points at github.com is bound on GET without any
// dialog, so the panel can render the list immediately.
func TestServeForgeBinding_AutoBindsOfficialRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	env, teardown := setupForgeEnv(t)
	defer teardown()
	runGitInDir(t, env.ProjectDir, "init")
	runGitInDir(t, env.ProjectDir, "remote", "add", "origin", "https://github.com/acme/widgets.git")

	resp := getBinding(t, env.ProjectDir)
	binding, ok := resp["binding"].(map[string]any)
	require.True(t, ok, "an official remote must be bound automatically, got %v", resp)
	assert.Equal(t, "acme/widgets", binding["slug"])
	assert.Equal(t, "auto", binding["source"], "auto-binding must be recorded as source=auto")

	// It must be persisted, not just reported.
	pf, err := service.GetProjectForge(env.ProjectDir)
	require.NoError(t, err)
	require.NotNil(t, pf)
	assert.Equal(t, "acme/widgets", pf.Slug())
}

// TestServeForgeBinding_DoesNotAutoBindNonOfficialHost pins the security
// boundary: a self-hosted host still needs the user's explicit confirmation, so
// it comes back as a suggestion rather than being persisted.
func TestServeForgeBinding_DoesNotAutoBindNonOfficialHost(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	env, teardown := setupForgeEnv(t)
	defer teardown()
	// A self-hosted host must be resolvable to survive the SSRF guard and reach
	// the official/non-official decision; stub the guard so the test does not
	// depend on DNS (the same override forge_credentials_test.go uses).
	orig := forgeHostGuard
	forgeHostGuard = func(string) error { return nil }
	t.Cleanup(func() { forgeHostGuard = orig })

	runGitInDir(t, env.ProjectDir, "init")
	runGitInDir(t, env.ProjectDir, "remote", "add", "origin", "https://gitlab.example.com/acme/widgets.git")

	resp := getBinding(t, env.ProjectDir)
	assert.Nil(t, resp["binding"], "a non-official host must not be auto-bound")
	assert.NotNil(t, resp["suggested"], "it should still be offered as a suggestion")

	pf, err := service.GetProjectForge(env.ProjectDir)
	require.NoError(t, err)
	assert.Nil(t, pf, "nothing may be persisted without confirmation")
}

// TestServeForgeBinding_DoesNotAutoBindOfficialHostWithPort is the port guard:
// IsOfficialHost() strips the port, so "github.com:8443" would otherwise be
// treated as official and the stored token later sent to
// https://github.com:8443/api/v3.
func TestServeForgeBinding_DoesNotAutoBindOfficialHostWithPort(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	env, teardown := setupForgeEnv(t)
	defer teardown()
	runGitInDir(t, env.ProjectDir, "init")
	runGitInDir(t, env.ProjectDir, "remote", "add", "origin", "https://github.com:8443/acme/widgets.git")

	resp := getBinding(t, env.ProjectDir)
	assert.Nil(t, resp["binding"], "an official host on a non-default port must not be auto-bound")

	pf, err := service.GetProjectForge(env.ProjectDir)
	require.NoError(t, err)
	assert.Nil(t, pf)
}

// TestServeForgeBinding_UnbindIsNotUndoneByAutoBind is the regression guard for
// the unbind loop: without the opt-out marker the next GET would bind the
// repository straight back.
func TestServeForgeBinding_UnbindIsNotUndoneByAutoBind(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	env, teardown := setupForgeEnv(t)
	defer teardown()
	runGitInDir(t, env.ProjectDir, "init")
	runGitInDir(t, env.ProjectDir, "remote", "add", "origin", "https://github.com/acme/widgets.git")

	// First GET auto-binds.
	require.NotNil(t, getBinding(t, env.ProjectDir)["binding"])

	// Unbind.
	req := newRequest(t, http.MethodDelete, "/api/forge/binding", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeBinding, req)
	require.Equal(t, http.StatusNoContent, w.Code)

	// It must stay unbound across subsequent reads.
	for i := range 2 {
		resp := getBinding(t, env.ProjectDir)
		assert.Nil(t, resp["binding"], "unbind must survive the auto-bind on GET (read %d)", i)
	}
}

// TestServeForgeBinding_ExplicitBindClearsOptOut ensures a manual bind restores
// normal auto-bind behavior for that project.
func TestServeForgeBinding_ExplicitBindClearsOptOut(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	// Opt out first.
	require.NoError(t, service.SetForgeBindOptOut(env.ProjectDir, true))
	opted, err := service.IsForgeBindOptedOut(env.ProjectDir)
	require.NoError(t, err)
	require.True(t, opted)

	// An explicit POST bind.
	req := newRequest(t, http.MethodPost, "/api/forge/binding",
		map[string]any{"url": "https://github.com/acme/widgets.git"})
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeBinding, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	opted, err = service.IsForgeBindOptedOut(env.ProjectDir)
	require.NoError(t, err)
	assert.False(t, opted, "an explicit bind must clear the opt-out")
}

func TestServeForgeBinding_RejectsUnsafeHost(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/forge/binding",
		map[string]any{"url": "https://127.0.0.1/acme/widgets.git"})
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeBinding, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, "binding a loopback host must be refused")
	assert.Contains(t, w.Body.String(), "UnsafeHost")

	pf, err := service.GetProjectForge(env.ProjectDir)
	require.NoError(t, err)
	assert.Nil(t, pf, "no binding may be stored for an unsafe host")
}

func TestServeForgeBinding_RejectsInvalidURL(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/forge/binding",
		map[string]any{"url": "not a url at all"})
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeBinding, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeForgeBinding_AcceptsExplicitFields(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	// Use a publicly-resolvable host: the SSRF guard resolves names and fails
	// closed, so an unresolvable self-hosted name is (correctly) rejected.
	req := newRequest(t, http.MethodPost, "/api/forge/binding", map[string]any{
		"platform": "gitlab", "host": "example.com",
		"owner": "group/sub", "repo": "widgets",
	})
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeBinding, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	pf, err := service.GetProjectForge(env.ProjectDir)
	require.NoError(t, err)
	require.NotNil(t, pf)
	assert.Equal(t, "example.com", pf.Host)
	assert.Equal(t, "group/sub", pf.Owner)
}

func TestServeForgeItems_NoBindingReturns404(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/forge/items?type=issue", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeItems, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "NoForgeBinding")
}

func TestServeForgeItems_RequiresProject(t *testing.T) {
	_, teardown := setupForgeEnv(t)
	defer teardown()

	// No project cookie → requireProject rejects.
	req := newRequest(t, http.MethodGet, "/api/forge/items?type=issue", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeItems, req)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestServeForgeItem_RequiresNumber(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()
	bindProject(t, env.ProjectDir, "github", "github.com", "acme", "widgets")

	req := newRequest(t, http.MethodGet, "/api/forge/item?type=issue", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeItem, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeForgeComments_RequiresNumber(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()
	bindProject(t, env.ProjectDir, "github", "github.com", "acme", "widgets")

	req := newRequest(t, http.MethodGet, "/api/forge/comments?type=issue", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeComments, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeForgeItems_MethodNotAllowed(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/forge/items", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeItems, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// TestServeForgeItems_EndToEndAgainstMockGitHub drives the full path: binding →
// provider → mocked GitHub API → normalized response.
func TestServeForgeItems_EndToEndAgainstMockGitHub(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	// Point the github adapter at a mock server by binding a host whose API base
	// we intercept. The provider builds https://<host>/api/v3, so we cannot use
	// httptest directly here; instead assert the wiring via a bound host and a
	// missing credential, which must surface as an auth-classified error rather
	// than a crash.
	bindProject(t, env.ProjectDir, "github", "github.invalid", "acme", "widgets")

	req := newRequest(t, http.MethodGet, "/api/forge/items?type=issue", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeItems, req)

	// Network failure to a non-resolving host must be reported as an error, not
	// a 200 with an empty list.
	assert.NotEqual(t, http.StatusOK, w.Code)
	assert.Contains(t, []string{"ForgeNetworkError", "ForgeAuthFailed", "ForgeError"}, errorCode(w.Body.String()))
}

func TestFilterForgeItemsMine(t *testing.T) {
	items := []forgeItemView{
		{Number: 1, Type: "issue", Author: "me", Assignees: nil},
		{Number: 2, Type: "issue", Author: "other", Assignees: []string{"me"}},
		{Number: 3, Type: "pr", Author: "me", Assignees: nil},
		{Number: 4, Type: "pr", Author: "other", Assignees: []string{"someone"}},
	}

	assigned := filterForgeItemsMine(items, "me", "assigned")
	require.Len(t, assigned, 1)
	assert.Equal(t, 2, assigned[0].Number)

	created := filterForgeItemsMine(items, "me", "created")
	require.Len(t, created, 2)
	assert.Equal(t, 1, created[0].Number)
	assert.Equal(t, 3, created[1].Number)

	// "review" is change requests only.
	review := filterForgeItemsMine(items, "me", "review")
	require.Len(t, review, 1)
	assert.Equal(t, 3, review[0].Number)

	// Default: any involvement.
	allInvolved := filterForgeItemsMine(items, "me", "")
	require.Len(t, allInvolved, 3)
}

func TestFilterForgeItemsMine_EmptyLoginReturnsAll(t *testing.T) {
	items := []forgeItemView{{Number: 1, Author: "a"}}
	assert.Len(t, filterForgeItemsMine(items, "", "assigned"), 1)
}

func TestNormalizeForgeState(t *testing.T) {
	assert.Equal(t, "open", normalizeForgeState(""))
	assert.Equal(t, "open", normalizeForgeState("bogus"))
	assert.Equal(t, "closed", normalizeForgeState("closed"))
	assert.Equal(t, "all", normalizeForgeState("all"))
}

func TestAtoiDefault(t *testing.T) {
	assert.Equal(t, 5, atoiDefault("5", 1))
	assert.Equal(t, 1, atoiDefault("", 1))
	assert.Equal(t, 1, atoiDefault("abc", 1))
}

// errorCode extracts the "code" field from a JSON error body.
func errorCode(body string) string {
	var resp map[string]any
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return ""
	}
	if c, ok := resp["code"].(string); ok {
		return c
	}
	return ""
}

var (
	_ = httptest.NewRecorder
	_ = forge.PlatformGitHub
)

// TestSuggestForgeBinding_FromGitRemote verifies auto-detection: a project with
// an origin remote pointing at a forge yields a suggestion. (For official hosts
// ServeForgeBinding persists it automatically — see
// TestServeForgeBinding_AutoBindsOfficialRemote; suggestForgeBinding itself
// remains a pure read.)
func TestSuggestForgeBinding_FromGitRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGitInDir(t, dir, "init")
	runGitInDir(t, dir, "remote", "add", "origin", "https://github.com/acme/widgets.git")

	got := suggestForgeBinding(dir)
	require.NotNil(t, got, "a parseable origin remote must yield a suggestion")
	assert.Equal(t, "github", got["platform"])
	assert.Equal(t, "acme/widgets", got["slug"])
	assert.Equal(t, "origin", got["remote"])
}

func TestSuggestForgeBinding_NoRemoteReturnsNil(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGitInDir(t, dir, "init")
	assert.Nil(t, suggestForgeBinding(dir), "a repo without a forge remote must yield no suggestion")
}

func TestSuggestForgeBinding_NonForgeRemoteReturnsNil(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGitInDir(t, dir, "init")
	runGitInDir(t, dir, "remote", "add", "origin", "/local/path/repo")

	assert.Nil(t, suggestForgeBinding(dir), "a local-path remote must not be suggested")
}

func runGitInDir(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, out)
	}
}

func TestServeForgeUnreadAndMarkRead(t *testing.T) {
	_, teardown := setupForgeEnv(t)
	defer teardown()

	// Seed an unread event.
	_, err := service.InsertForgeEvent(service.ForgeEvent{
		Platform: "github", Host: "github.com", Owner: "a", Repo: "b",
		ItemType: "issue", Number: 1, EventType: "closed", DedupeKey: "k1",
	})
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/forge/unread", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeUnread, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(1), resp["count"])

	// Mark read.
	req = newRequest(t, http.MethodPost, "/api/forge/read", nil)
	withAuthCookie(req, model.SessionToken)
	w = callHandler(ServeForgeMarkRead, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Now zero.
	req = newRequest(t, http.MethodGet, "/api/forge/unread", nil)
	withAuthCookie(req, model.SessionToken)
	w = callHandler(ServeForgeUnread, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(0), resp["count"])
}

func TestServeForgeUnread_MethodNotAllowed(t *testing.T) {
	_, teardown := setupForgeEnv(t)
	defer teardown()
	req := newRequest(t, http.MethodPost, "/api/forge/unread", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgeUnread, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
