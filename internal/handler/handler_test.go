package handler

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsPathUnderAnyRoot(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	t.Run("PathUnderRoot_ReturnsTrue", func(t *testing.T) {
		assert.True(t, isPathUnderAnyRoot(env.ProjectDir))
		assert.True(t, isPathUnderAnyRoot(filepath.Join(env.WatchDir, "subdir")))
	})

	t.Run("PathOutsideRoot_ReturnsFalse", func(t *testing.T) {
		assert.False(t, isPathUnderAnyRoot("/etc/passwd"))
		assert.False(t, isPathUnderAnyRoot(os.TempDir()))
	})

	t.Run("ExactRootPath_ReturnsTrue", func(t *testing.T) {
		assert.True(t, isPathUnderAnyRoot(env.WatchDir))
	})
}

func TestResolveAbsPath(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	t.Run("AbsolutePathUnderRoot_ReturnsPath", func(t *testing.T) {
		createTestFile(t, env.WatchDir, "absfile.txt", "data")
		absPath := filepath.Join(env.WatchDir, "absfile.txt")

		req := newRequest(t, http.MethodPost, "/api/test", nil)
		w := httptest.NewRecorder()
		result, ok := resolveAbsPath(w, req, absPath)
		assert.True(t, ok)
		assert.Equal(t, absPath, result)
	})

	t.Run("AbsolutePathOutsideRoot_ReturnsFalse", func(t *testing.T) {
		req := newRequest(t, http.MethodPost, "/api/test", nil)
		w := httptest.NewRecorder()
		result, ok := resolveAbsPath(w, req, "/etc/passwd")
		assert.False(t, ok)
		assert.Empty(t, result)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("RelativePath_ResolvesAgainstProjectCookie", func(t *testing.T) {
		createTestFile(t, env.ProjectDir, "relfile.txt", "data")

		req := newRequest(t, http.MethodPost, "/api/test", nil)
		withProjectCookie(req, env.ProjectDir)
		w := httptest.NewRecorder()
		result, ok := resolveAbsPath(w, req, "relfile.txt")
		assert.True(t, ok)
		assert.Contains(t, result, "relfile.txt")
	})

	t.Run("RelativePathWithoutProject_Returns403", func(t *testing.T) {
		req := newRequest(t, http.MethodPost, "/api/test", nil)
		w := httptest.NewRecorder()
		result, ok := resolveAbsPath(w, req, "relfile.txt")
		assert.False(t, ok)
		assert.Empty(t, result)
	})
}

// ============================================================================
// ServeSessions additional tests
// ============================================================================

func TestServeSessions_Get_ReturnsTotalCount(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create 3 sessions
	for range 3 {
		_, err := service.CreateSession(env.ProjectDir, "codebuddy", "session", "", "", "default", "chat")
		require.NoError(t, err)
	}

	req := newRequest(t, http.MethodGet, "/api/ai/sessions", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

	totalCount, ok := result["totalCount"].(float64)
	assert.True(t, ok, "totalCount should be a number")
	assert.Equal(t, float64(3), totalCount)

	sessions, ok := result["sessions"].([]interface{})
	assert.True(t, ok)
	assert.Len(t, sessions, 3)
}

func TestServeSessions_Get_TotalCountZero(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/sessions", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

	totalCount, ok := result["totalCount"].(float64)
	assert.True(t, ok, "totalCount should be present even when zero")
	assert.Equal(t, float64(0), totalCount)

	sessions, ok := result["sessions"].([]interface{})
	assert.True(t, ok)
	assert.Empty(t, sessions)
}

func TestServeSessions_Get_ProjectIsolation(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create sessions for the test project
	_, err := service.CreateSession(env.ProjectDir, "codebuddy", "project session 1", "", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.CreateSession(env.ProjectDir, "codebuddy", "project session 2", "", "", "default", "chat")
	require.NoError(t, err)

	// Create sessions for a different project
	otherProject := filepath.Join(env.WatchDir, "other-project")
	_ = os.MkdirAll(otherProject, 0o755)
	_, err = service.CreateSession(otherProject, "codebuddy", "other project session", "", "", "default", "chat")
	require.NoError(t, err)

	// Request sessions for the test project
	req := newRequest(t, http.MethodGet, "/api/ai/sessions", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

	sessions, ok := result["sessions"].([]interface{})
	assert.True(t, ok)
	assert.Len(t, sessions, 2, "should only return sessions for the requested project")

	totalCount, ok := result["totalCount"].(float64)
	assert.True(t, ok)
	assert.Equal(t, float64(2), totalCount, "totalCount should only count sessions for the requested project")
}

func TestServeSessions_Get_SessionFields(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "test session", "codebuddy", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/sessions", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

	sessions, ok := result["sessions"].([]interface{})
	require.True(t, ok)
	require.Len(t, sessions, 1)

	session, ok := sessions[0].(map[string]interface{})
	require.True(t, ok)

	// Verify essential fields are present
	assert.Equal(t, sessionID, session["id"])
	assert.Equal(t, "test session", session["title"])
	assert.Equal(t, "codebuddy", session["backend"])
	assert.NotNil(t, session["createdAt"])
	assert.NotNil(t, session["updatedAt"])
}

func TestServeSessions_Get_PaginationWithLimit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create 5 sessions
	for range 5 {
		_, err := service.CreateSession(env.ProjectDir, "codebuddy", "session", "", "", "default", "chat")
		require.NoError(t, err)
	}

	// Request with limit=2
	req := newRequest(t, http.MethodGet, "/api/ai/sessions?limit=2", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

	sessions, ok := result["sessions"].([]interface{})
	assert.True(t, ok)
	assert.Len(t, sessions, 2, "should return at most 'limit' sessions")

	hasMore, ok := result["hasMore"].(bool)
	assert.True(t, ok, "hasMore should be present")
	assert.True(t, hasMore, "should have more sessions when limit < total")

	totalCount, ok := result["totalCount"].(float64)
	assert.True(t, ok)
	assert.Equal(t, float64(5), totalCount, "totalCount should reflect all sessions regardless of limit")
}

func TestServeSessions_Get_PaginationNoMore(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create 2 sessions
	for range 2 {
		_, err := service.CreateSession(env.ProjectDir, "codebuddy", "session", "", "", "default", "chat")
		require.NoError(t, err)
	}

	// Request with limit=5 (more than total)
	req := newRequest(t, http.MethodGet, "/api/ai/sessions?limit=5", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

	hasMore, ok := result["hasMore"].(bool)
	assert.True(t, ok)
	assert.False(t, hasMore, "hasMore should be false when all sessions fit in the limit")
}

func TestServeSessions_Get_PaginationInvalidLimit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create sessions
	for range 3 {
		_, err := service.CreateSession(env.ProjectDir, "codebuddy", "session", "", "", "default", "chat")
		require.NoError(t, err)
	}

	// Invalid limit values should fall back to no-limit (returns all)
	for _, limitVal := range []string{"0", "-1", "abc"} {
		req := newRequest(t, http.MethodGet, "/api/ai/sessions?limit="+limitVal, nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeSessions, req)
		assertOK(t, w)

		var result map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result), "limit=%s", limitVal)

		sessions, ok := result["sessions"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, sessions, 3, "invalid limit=%s should return all sessions", limitVal)

		hasMore, _ := result["hasMore"].(bool)
		assert.False(t, hasMore, "hasMore should be false when limit is invalid/zero")
	}
}

func TestServeSessions_Get_CursorNormalization(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create sessions
	for range 3 {
		_, err := service.CreateSession(env.ProjectDir, "codebuddy", "session", "", "", "default", "chat")
		require.NoError(t, err)
	}

	// Pass an ISO 8601 cursor (with T and Z) — handler should normalize it
	req := newRequest(t, http.MethodGet, "/api/ai/sessions?limit=10&cursor=2026-05-16T15:25:50Z", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.NotNil(t, result["sessions"])
}

func TestServeSessions_Post_SessionCountInResponse(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a first session so sessionCount > 0
	_, err := service.CreateSession(env.ProjectDir, "codebuddy", "existing", "", "", "default", "chat")
	require.NoError(t, err)

	body := map[string]string{}
	req := newRequest(t, http.MethodPost, "/api/ai/sessions", body)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])

	sessionCount, ok := result["sessionCount"].(float64)
	assert.True(t, ok, "sessionCount should be present in POST response")
	assert.Equal(t, float64(2), sessionCount, "sessionCount should include the newly created session")
}

func TestServeSessions_Post_SessionLimitReached(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Set a low session limit
	origMaxCount := model.SessionMaxCount
	model.SessionMaxCount = 2
	defer func() { model.SessionMaxCount = origMaxCount }()

	// Create sessions up to the limit
	for range 2 {
		_, err := service.CreateSession(env.ProjectDir, "codebuddy", "session", "", "", "default", "chat")
		require.NoError(t, err)
	}

	// Try to create one more — should be rejected
	body := map[string]string{}
	req := newRequest(t, http.MethodPost, "/api/ai/sessions", body)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestServeSessions_Post_SessionLimitZero_Unlimited(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// SessionMaxCount=0 means unlimited
	origMaxCount := model.SessionMaxCount
	model.SessionMaxCount = 0
	defer func() { model.SessionMaxCount = origMaxCount }()

	// Create multiple sessions — all should succeed
	for range 5 {
		body := map[string]string{}
		req := newRequest(t, http.MethodPost, "/api/ai/sessions", body)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeSessions, req)
		assertOK(t, w)
	}
}

func TestServeSessions_Post_SetsSessionCookie(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]string{}
	req := newRequest(t, http.MethodPost, "/api/ai/sessions", body)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	// Check that chat_session_id cookie was set
	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == model.ScopedCookieName("chat_session_id") {
			found = true
			assert.NotEmpty(t, c.Value, "session cookie should have a value")
			assert.True(t, c.HttpOnly, "session cookie should be HttpOnly")
			assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
		}
	}
	assert.True(t, found, "chat_session_id cookie should be set on session creation")
}

func TestServeSessions_Post_NoAgentsAvailable(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Remove all agents
	origAgents := model.Agents
	origAgentList := model.AgentList
	origDefault := model.DefaultAgentID
	model.Agents = map[string]*model.Agent{}
	model.AgentList = nil
	model.DefaultAgentID = ""
	defer func() {
		model.Agents = origAgents
		model.AgentList = origAgentList
		model.DefaultAgentID = origDefault
	}()

	body := map[string]string{}
	req := newRequest(t, http.MethodPost, "/api/ai/sessions", body)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestServeSessions_Post_DefaultBackendWhenEmpty(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// POST with no backend specified — should default to "codebuddy"
	body := map[string]string{
		"title": "No Backend",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/sessions", body)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, "codebuddy", result["backend"])
}

func TestServeSessions_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPut, "/api/ai/sessions", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

// ============================================================================
// DeleteSession additional tests
// ============================================================================

func TestDeleteSession_SoftDelete(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "to soft-delete", "", "", "default", "chat")
	require.NoError(t, err)

	// Verify session appears in list
	sessions, err := service.GetSessions(env.ProjectDir, "")
	require.NoError(t, err)
	assert.Len(t, sessions, 1)

	// Delete the session
	req := newRequest(t, http.MethodDelete, "/api/ai/session/delete?session_id="+sessionID, nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(DeleteSession, req)
	assertOK(t, w)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])

	// Verify session no longer appears in list (soft-deleted)
	sessions, err = service.GetSessions(env.ProjectDir, "")
	require.NoError(t, err)
	assert.Empty(t, sessions, "soft-deleted session should not appear in session list")

	// Verify totalCount decreased
	count, err := service.GetSessionCount(env.ProjectDir)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestDeleteSession_SessionCountInResponse(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create 3 sessions
	for range 3 {
		_, err := service.CreateSession(env.ProjectDir, "codebuddy", "session", "", "", "default", "chat")
		require.NoError(t, err)
	}

	// Get the first session ID
	sessions, err := service.GetSessions(env.ProjectDir, "")
	require.NoError(t, err)
	sessionID := sessions[0].ID

	// Delete one session
	req := newRequest(t, http.MethodDelete, "/api/ai/session/delete?session_id="+sessionID, nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(DeleteSession, req)
	assertOK(t, w)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

	sessionCount, ok := result["sessionCount"].(float64)
	assert.True(t, ok, "sessionCount should be present in delete response")
	assert.Equal(t, float64(2), sessionCount, "sessionCount should decrease after deletion")
}

func TestDeleteSession_DefaultBackendParam(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "default backend test", "", "", "default", "chat")
	require.NoError(t, err)

	// Delete without backend query param — should default to "codebuddy"
	req := newRequest(t, http.MethodDelete, "/api/ai/session/delete?session_id="+sessionID, nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(DeleteSession, req)
	assertOK(t, w)
}

func TestDeleteSession_NonExistentSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Deleting a session that doesn't exist should not crash
	req := newRequest(t, http.MethodDelete, "/api/ai/session/delete?session_id=nonexistent-session-id", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(DeleteSession, req)
	// The handler calls service.DeleteSession which does a SQL UPDATE that affects 0 rows,
	// but still returns nil error, so it returns 200 with ok=true
	assertOK(t, w)
}

func TestDeleteSession_SessionFromCookie(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "cookie session", "", "", "default", "chat")
	require.NoError(t, err)

	// Delete using session_id from cookie instead of query param
	req := newRequest(t, http.MethodDelete, "/api/ai/session/delete", nil)
	withProjectCookie(req, env.ProjectDir)
	withSessionCookie(req, sessionID)

	w := callHandler(DeleteSession, req)
	assertOK(t, w)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
}

// ============================================================================
// getSessionID / setSessionID additional tests
// ============================================================================

func TestGetSessionID_QueryParamTakesPrecedence(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/test?session_id=from-query", http.NoBody)
	r.AddCookie(&http.Cookie{Name: model.ScopedCookieName("chat_session_id"), Value: "from-cookie"})

	sessionID := getSessionID(r)
	assert.Equal(t, "from-query", sessionID, "query param should take precedence over cookie")
}

func TestGetSessionID_NoQueryParamNoCookie(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	sessionID := getSessionID(r)
	assert.Empty(t, sessionID)
}

func TestSetSessionID_SetsHttpOnlyCookie(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	setSessionID(w, r, "test-session-123")

	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1, "should set exactly one cookie")
	c := cookies[0]
	assert.Equal(t, model.ScopedCookieName("chat_session_id"), c.Name)
	assert.Equal(t, "test-session-123", c.Value)
	assert.Equal(t, "/", c.Path)
	assert.True(t, c.HttpOnly, "cookie should be HttpOnly to mitigate XSS")
	assert.False(t, c.Secure, "cookie should not be Secure over plain HTTP")
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
	assert.Equal(t, 86400*30, c.MaxAge)
}

func TestSetSessionID_SetsSecureCookieOverTLS(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.TLS = &tls.ConnectionState{} // simulate TLS
	setSessionID(w, r, "test-session-456")

	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1, "should set exactly one cookie")
	c := cookies[0]
	assert.True(t, c.Secure, "cookie should be Secure over TLS")
}

// ============================================================================
// ServeSessions cursor-based pagination edge cases
// ============================================================================

func TestServeSessions_Get_CursorWithTZSuffix(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	for range 3 {
		_, err := service.CreateSession(env.ProjectDir, "codebuddy", "session", "", "", "default", "chat")
		require.NoError(t, err)
	}

	// Cursor with +00:00 suffix should be stripped
	req := newRequest(t, http.MethodGet, "/api/ai/sessions?limit=10&cursor=2026-05-16T15:25:50+00:00", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.NotNil(t, result["sessions"])
}

func TestServeSessions_Get_CursorAndCursorID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create sessions
	for range 5 {
		_, err := service.CreateSession(env.ProjectDir, "codebuddy", "session", "", "", "default", "chat")
		require.NoError(t, err)
	}

	// Get first page with limit=2
	req := newRequest(t, http.MethodGet, "/api/ai/sessions?limit=2", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var firstPage map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &firstPage))
	firstSessions := firstPage["sessions"].([]interface{})
	require.Len(t, firstSessions, 2)

	// Extract cursor from the last session
	lastSession := firstSessions[1].(map[string]interface{})
	cursorUpdatedAt := lastSession["updatedAt"].(string)
	cursorID := lastSession["id"].(string)

	// Normalize cursor format (replace T/space, strip Z)
	cursor := strings.ReplaceAll(cursorUpdatedAt, "T", " ")
	cursor = strings.TrimSuffix(cursor, "Z")

	// Request second page using cursor (properly encode query params)
	params := url.Values{}
	params.Set("limit", "2")
	params.Set("cursor", cursor)
	params.Set("cursor_id", cursorID)
	req2 := newRequest(t, http.MethodGet, "/api/ai/sessions?"+params.Encode(), nil)
	withProjectCookie(req2, env.ProjectDir)
	w2 := callHandler(ServeSessions, req2)
	assertOK(t, w2)

	var secondPage map[string]interface{}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &secondPage))

	secondSessions := secondPage["sessions"].([]interface{})
	assert.Len(t, secondSessions, 2, "second page should have remaining sessions")

	// Verify no overlap between pages
	firstIDs := map[string]bool{}
	for _, s := range firstSessions {
		session := s.(map[string]interface{})
		firstIDs[session["id"].(string)] = true
	}
	for _, s := range secondSessions {
		session := s.(map[string]interface{})
		assert.False(t, firstIDs[session["id"].(string)], "second page should not contain sessions from first page")
	}
}
