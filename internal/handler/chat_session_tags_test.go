package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeTags unmarshals the {"tags": [...]} envelope returned by
// ServeSessionTags GET.
func decodeTags(t *testing.T, body []byte) []model.SessionTag {
	t.Helper()
	var result struct {
		Tags []model.SessionTag `json:"tags"`
	}
	require.NoError(t, json.Unmarshal(body, &result))
	return result.Tags
}

func tagNames(tags []model.SessionTag) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		out = append(out, tag.Name)
	}
	return out
}

// ── GET /api/ai/session/tags ───────────────────────────────────────────────

func TestServeSessionTags_Get_ReturnsGlobalAndProjectTags(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	other := env.ProjectDir + "-other"
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "s", "claude", "", "default", "chat")
	require.NoError(t, err)
	require.NoError(t, service.SetSessionTags(sessionID, env.ProjectDir, []service.SessionTagRef{
		{Name: "localTag", Scope: service.SessionTagScopeProject},
		{Name: "globalTag", Scope: service.SessionTagScopeGlobal},
	}))
	// A label owned by a different project must not leak into this response.
	otherSession, err := service.CreateSession(other, "claude", "o", "claude", "", "default", "chat")
	require.NoError(t, err)
	require.NoError(t, service.SetSessionTags(otherSession, other, []service.SessionTagRef{
		{Name: "otherTag", Scope: service.SessionTagScopeProject},
	}))

	req := newRequest(t, http.MethodGet, "/api/ai/session/tags", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionTags, req)
	assertOK(t, w)

	assert.Equal(t, []string{"globaltag", "localtag"}, tagNames(decodeTags(t, w.Body.Bytes())))
}

func TestServeSessionTags_Get_EmptyWhenNoTags(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/session/tags", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionTags, req)
	assertOK(t, w)
	assert.Empty(t, decodeTags(t, w.Body.Bytes()))
}

func TestServeSessionTags_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/session/tags", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionTags, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

// ── DELETE /api/ai/session/tags ────────────────────────────────────────────

func TestServeSessionTags_Delete_RemovesDefinitionAndLinks(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "s", "claude", "", "default", "chat")
	require.NoError(t, err)
	require.NoError(t, service.SetSessionTags(sessionID, env.ProjectDir, []service.SessionTagRef{
		{Name: "doomed"}, {Name: "kept"},
	}))

	req := newRequest(t, http.MethodDelete, "/api/ai/session/tags?name=doomed", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionTags, req)
	assertOK(t, w)

	// Gone from the candidate list...
	req = newRequest(t, http.MethodGet, "/api/ai/session/tags", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeSessionTags, req)
	assert.Equal(t, []string{"kept"}, tagNames(decodeTags(t, w.Body.Bytes())))

	// ...and unlinked from the session that used it.
	tags, err := service.GetSessionTags(sessionID)
	require.NoError(t, err)
	require.Len(t, tags, 1)
	assert.Equal(t, "kept", tags[0].Name)
}

func TestServeSessionTags_Delete_RequiresName(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/ai/session/tags?name=", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionTags, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeSessionTags_Delete_UnknownNameIsBadRequest(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/ai/session/tags?name=ghost", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionTags, req)
	assertStatus(t, w, http.StatusBadRequest)
}

// ── PATCH /api/ai/session/update { tags } ──────────────────────────────────

func TestServeAISessionUpdate_TagsPersistAndEchoOnList(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "tagged", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"tags": []map[string]any{
			{"name": "bug", "scope": "project"},
			{"name": "shared", "scope": "global"},
		},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)

	// The list endpoint must hydrate tags so the row can render the label line.
	listReq := newRequest(t, http.MethodGet, "/api/ai/sessions", nil)
	listReq = withProjectCookie(listReq, env.ProjectDir)
	listW := callHandler(ServeSessions, listReq)
	assertOK(t, listW)

	var result struct {
		Sessions []model.ChatSession `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(listW.Body.Bytes(), &result))
	require.Len(t, result.Sessions, 1)
	require.Len(t, result.Sessions[0].Tags, 2)
	assert.Equal(t, "bug", result.Sessions[0].Tags[0].Name)
	assert.Equal(t, "shared", result.Sessions[0].Tags[1].Name)
}

func TestServeAISessionUpdate_TagsEmptyArrayClearsAll(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "clear", "claude", "", "default", "chat")
	require.NoError(t, err)
	require.NoError(t, service.SetSessionTags(sessionID, env.ProjectDir, []service.SessionTagRef{{Name: "bug"}}))

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"tags": []map[string]any{},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)

	tags, err := service.GetSessionTags(sessionID)
	require.NoError(t, err)
	assert.Empty(t, tags)
}

func TestServeAISessionUpdate_TagsAbsentLeavesTagsUntouched(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "untouched", "claude", "", "default", "chat")
	require.NoError(t, err)
	require.NoError(t, service.SetSessionTags(sessionID, env.ProjectDir, []service.SessionTagRef{{Name: "bug"}}))

	// A title-only patch must not wipe tags — this is why the handler field is
	// a pointer: absent ≠ empty array.
	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"title": "renamed",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)

	tags, err := service.GetSessionTags(sessionID)
	require.NoError(t, err)
	require.Len(t, tags, 1)
	assert.Equal(t, "bug", tags[0].Name)
}

func TestServeAISessionUpdate_TagsFiledUnderSessionsOwnProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "own-project", "claude", "", "default", "chat")
	require.NoError(t, err)

	// No project cookie: the session's own project_path must own the new label,
	// otherwise the tag would be invisible in its own project's candidate list.
	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"tags": []map[string]any{{"name": "orphanCandidate"}},
	})
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)

	tags, err := service.ListSessionTags(env.ProjectDir)
	require.NoError(t, err)
	assert.Equal(t, []string{"orphancandidate"}, tagNames2(tags))
}

func tagNames2(tags []service.SessionTag) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		out = append(out, tag.Name)
	}
	return out
}

func TestServeSessions_Get_NoTagsOmitsField(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	_, err := service.CreateSession(env.ProjectDir, "claude", "plain", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/sessions", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	// Raw JSON check: an untagged session must not gain a "tags": null key, so
	// the payload for existing clients stays byte-identical.
	assert.NotContains(t, w.Body.String(), `"tags"`)
}

// ── Project ownership on the query-param path ──────────────────────────────

// TestServeAISessionUpdate_TagsForeignProjectViaQueryParam is the regression
// test for a guard bypass: resolveUpdateTargetSession only enforced project
// ownership when the body carried a sessionId, so `?session_id=<other
// project's session>` slipped through (requireSessionID checks presence only)
// and let a caller rewrite — or wipe — another project's tags.
func TestServeAISessionUpdate_TagsForeignProjectViaQueryParam(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	foreign := env.ProjectDir + "-foreign"
	sessionID, err := service.CreateSession(foreign, "claude", "Foreign", "claude", "", "default", "chat")
	require.NoError(t, err)
	require.NoError(t, service.SetSessionTags(sessionID, foreign, []service.SessionTagRef{{Name: "victim"}}))

	req := newRequest(t, http.MethodPatch,
		"/api/ai/session/update?session_id="+sessionID, map[string]any{
			"tags": []map[string]any{{"name": "attacker", "scope": "global"}},
		})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	assertStatus(t, w, http.StatusForbidden)

	// The victim's tags must be untouched.
	tags, err := service.GetSessionTags(sessionID)
	require.NoError(t, err)
	require.Len(t, tags, 1)
	assert.Equal(t, "victim", tags[0].Name)
}

// TestServeAISessionUpdate_TagsForeignProjectClearViaQueryParam covers the
// destructive variant: `"tags": []` must not be able to wipe another project's
// tags either.
func TestServeAISessionUpdate_TagsForeignProjectClearViaQueryParam(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	foreign := env.ProjectDir + "-foreign2"
	sessionID, err := service.CreateSession(foreign, "claude", "Foreign", "claude", "", "default", "chat")
	require.NoError(t, err)
	require.NoError(t, service.SetSessionTags(sessionID, foreign, []service.SessionTagRef{{Name: "victim"}}))

	req := newRequest(t, http.MethodPatch,
		"/api/ai/session/update?session_id="+sessionID, map[string]any{"tags": []map[string]any{}})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	assertStatus(t, w, http.StatusForbidden)

	tags, err := service.GetSessionTags(sessionID)
	require.NoError(t, err)
	assert.Len(t, tags, 1, "tags must not be cleared across projects")
}

// TestServeAISessionUpdate_UnknownSessionIDIs404 verifies an unknown id no
// longer returns 200 while writing orphan link rows for a nonexistent session.
func TestServeAISessionUpdate_UnknownSessionIDIs404(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPatch,
		"/api/ai/session/update?session_id=does-not-exist", map[string]any{
			"tags": []map[string]any{{"name": "orphan"}},
		})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	assertStatus(t, w, http.StatusNotFound)
}

// TestServeAISessionUpdate_TagsOnArchivedSessionStayReachable pins the fix for
// a permanently unreachable tag: GetSessionFullInfo filters archived=0, so an
// archived session resolved to project_path="" and its tags were filed under no
// project — invisible in every candidate list and undeletable via the API.
func TestServeAISessionUpdate_TagsOnArchivedSessionStayReachable(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Archived", "claude", "", "default", "chat")
	require.NoError(t, err)
	require.NoError(t, service.ArchiveSession(env.ProjectDir, "claude", sessionID))

	// Tag the archived session (models the dialog being open when it is archived).
	req := newRequest(t, http.MethodPatch,
		"/api/ai/session/update?session_id="+sessionID, map[string]any{
			"tags": []map[string]any{{"name": "ghost"}},
		})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)

	// The label must be visible in its own project's candidate list...
	listReq := newRequest(t, http.MethodGet, "/api/ai/session/tags", nil)
	listReq = withProjectCookie(listReq, env.ProjectDir)
	listW := callHandler(ServeSessionTags, listReq)
	assertOK(t, listW)
	assert.Equal(t, []string{"ghost"}, tagNames(decodeTags(t, listW.Body.Bytes())))

	// ...and deletable.
	delReq := newRequest(t, http.MethodDelete, "/api/ai/session/tags?name=ghost&scope=project", nil)
	delReq = withProjectCookie(delReq, env.ProjectDir)
	delW := callHandler(ServeSessionTags, delReq)
	assertOK(t, delW)
}

// ── Scoped delete ──────────────────────────────────────────────────────────

// TestServeSessionTags_DeleteUsesScope verifies the scope query param reaches
// the service: deleting the project-scoped definition must not remove a global
// one of the same name.
func TestServeSessionTags_DeleteUsesScope(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	other := env.ProjectDir + "-other-scope"
	sidA, err := service.CreateSession(env.ProjectDir, "claude", "A", "claude", "", "default", "chat")
	require.NoError(t, err)
	sidB, err := service.CreateSession(other, "claude", "B", "claude", "", "default", "chat")
	require.NoError(t, err)
	require.NoError(t, service.SetSessionTags(sidA, env.ProjectDir, []service.SessionTagRef{
		{Name: "bug", Scope: service.SessionTagScopeProject},
	}))
	require.NoError(t, service.SetSessionTags(sidB, other, []service.SessionTagRef{
		{Name: "bug", Scope: service.SessionTagScopeGlobal},
	}))

	req := newRequest(t, http.MethodDelete, "/api/ai/session/tags?name=bug&scope=project", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionTags, req)
	assertOK(t, w)

	// The global label is untouched, so the other project still shows it.
	globalTags, err := service.ListSessionTags(other)
	require.NoError(t, err)
	require.Len(t, globalTags, 1)
	assert.Equal(t, service.SessionTagScopeGlobal, globalTags[0].Scope)
}
