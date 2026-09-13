package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ServeToolCallDetail ---

func TestServeToolCallDetail_Found(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", `{"blocks":[]}`, nil, false, "")
	require.NoError(t, err)

	err = service.UpsertToolCall(msgID, sessionID, "toolu_td01", "Read", json.RawMessage(`{"file_path":"/tmp/test.go"}`), "contents", "success", "test.go", true, 0)
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/chat/tool-call?tool_id=toolu_td01&message_id="+fmt.Sprintf("%d", msgID), nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeToolCallDetail, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, "Read", result["name"])
}

func TestServeToolCallDetail_MissingBothParams(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/tool-call", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeToolCallDetail, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeToolCallDetail_BadMessageID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/tool-call?tool_id=toolu_01&message_id=invalid", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeToolCallDetail, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeToolCallDetail_NoRecord(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/tool-call?tool_id=nonexistent&message_id=1", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeToolCallDetail, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeToolCallDetail_ProjectMismatch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", `{"blocks":[]}`, nil, false, "")
	require.NoError(t, err)

	err = service.UpsertToolCall(msgID, sessionID, "toolu_td02", "Read", json.RawMessage(`{}`), "contents", "success", "", true, 0)
	require.NoError(t, err)

	otherDir := env.WatchDir + "/other-project"
	_ = os.MkdirAll(otherDir, 0o755)

	req := newRequest(t, http.MethodGet, "/api/ai/chat/tool-call?tool_id=toolu_td02&message_id="+fmt.Sprintf("%d", msgID), nil)
	req.AddCookie(&http.Cookie{
		Name:  model.ScopedCookieName("clawbench_project"),
		Value: url.QueryEscape(otherDir),
	})

	w := callHandler(ServeToolCallDetail, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeToolCallDetail_PostRejected(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/chat/tool-call", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeToolCallDetail, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeToolCallDetail_SessionIDFallback(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Simulate multiple assistant messages in a session
	msgID1, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", `{"blocks":[{"type":"tool_use","name":"Read","id":"toolu_split01","status":"success","done":true}]}`, nil, false, "")
	require.NoError(t, err)

	msgID2, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", `{"blocks":[{"type":"tool_use","name":"Write","id":"toolu_split02","status":"success","done":true}]}`, nil, false, "")
	require.NoError(t, err)

	// Tool call stored under msgID1 (first assistant message)
	err = service.UpsertToolCall(msgID1, sessionID, "toolu_split01", "Read", json.RawMessage(`{"file_path":"/tmp/a.go"}`), "contents-a", "success", "", true, 0)
	require.NoError(t, err)

	// Tool call stored under msgID2 (second assistant message)
	err = service.UpsertToolCall(msgID2, sessionID, "toolu_split02", "Write", json.RawMessage(`{"file_path":"/tmp/b.go"}`), "contents-b", "success", "", true, 0)
	require.NoError(t, err)

	// Case 1: Wrong message_id but correct session_id → should find via fallback
	req := newRequest(t, http.MethodGet, fmt.Sprintf("/api/ai/chat/tool-call?tool_id=toolu_split01&message_id=%d&session_id=%s", msgID2, sessionID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeToolCallDetail, req)
	assertOK(t, w)
	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, "Read", result["name"])

	// Case 2: No session_id, wrong message_id → 404
	req2 := newRequest(t, http.MethodGet, fmt.Sprintf("/api/ai/chat/tool-call?tool_id=toolu_split01&message_id=%d", msgID2), nil)
	req2 = withProjectCookie(req2, env.ProjectDir)
	w2 := callHandler(ServeToolCallDetail, req2)
	assert.Equal(t, http.StatusNotFound, w2.Code)

	// Case 3: Correct message_id, no session_id → should still work (primary lookup)
	req3 := newRequest(t, http.MethodGet, fmt.Sprintf("/api/ai/chat/tool-call?tool_id=toolu_split01&message_id=%d", msgID1), nil)
	req3 = withProjectCookie(req3, env.ProjectDir)
	w3 := callHandler(ServeToolCallDetail, req3)
	assertOK(t, w3)
	var result3 map[string]any
	require.NoError(t, json.Unmarshal(w3.Body.Bytes(), &result3))
	assert.Equal(t, "Read", result3["name"])
}

// --- ServeSessions GET with pagination ---

func TestServeSessions_Get_WithLimitParam(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	for i := range 5 {
		_, err := service.CreateSession(env.ProjectDir, "claude", fmt.Sprintf("Session %d", i), "claude", "", "default", "chat")
		require.NoError(t, err)
	}

	req := newRequest(t, http.MethodGet, "/api/ai/sessions?limit=2", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	sessions := result["sessions"].([]any)
	assert.LessOrEqual(t, len(sessions), 2)
	assert.Equal(t, true, result["hasMore"])
}

func TestServeSessions_Get_AllSessionsNoLimit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	_, err := service.CreateSession(env.ProjectDir, "claude", "Session A", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/sessions", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, false, result["hasMore"])
}

// --- ServeSessions POST ---

func TestServeSessions_Post_WithTitle(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/sessions", map[string]any{
		"title":   "My Session",
		"backend": "claude",
		"agentId": "claude",
	})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
	assert.NotEmpty(t, result["sessionId"])
}

func TestServeSessions_Post_AutoTitle(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/sessions", map[string]any{
		"backend": "claude",
	})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
	assert.NotEmpty(t, result["title"])
}

func TestServeSessions_Post_AutoTitle_BasedOnMaxUnnamed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	newSessionTitle := func() string {
		req := newRequest(t, http.MethodPost, "/api/ai/sessions", map[string]any{
			"backend": "claude",
		})
		req = withProjectCookie(req, env.ProjectDir)
		w := callHandler(ServeSessions, req)
		assertOK(t, w)
		var result map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		return result["title"].(string)
	}

	// Two unnamed sessions → numbered 1 and 2.
	first := newSessionTitle()
	assert.Equal(t, "New Session 1", first)
	second := newSessionTitle()
	assert.Equal(t, "New Session 2", second)

	// Archive the highest-numbered unnamed session ("New Session 2"). With
	// max-based numbering the next title is again "New Session 2", never
	// exceeding the largest number still present among active unnamed sessions.
	sessions, err := service.GetSessions(env.ProjectDir, "")
	require.NoError(t, err)
	require.Len(t, sessions, 2)
	archivedID := ""
	for _, s := range sessions {
		if s.Title == "New Session 2" {
			archivedID = s.ID
		}
	}
	require.NotEmpty(t, archivedID)
	req := newRequest(t, http.MethodDelete, "/api/ai/session/archive?session_id="+archivedID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ArchiveSession, req)
	assertOK(t, w)

	// "New Session 1" still exists → max is 1, so the new one is 2 (no duplicate
	// among active unnamed sessions).
	third := newSessionTitle()
	assert.Equal(t, "New Session 2", third)
}

func TestServeSessions_Post_LimitExceeded(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	origMax := model.SessionMaxCount
	defer func() { model.SessionMaxCount = origMax }()
	model.SessionMaxCount = 1

	_, err := service.CreateSession(env.ProjectDir, "claude", "First", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/sessions", map[string]any{
		"backend": "claude",
	})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestServeSessions_Post_NoAgents(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	origAgents := model.Agents
	defer func() { model.Agents = origAgents }()
	model.Agents = map[string]*model.Agent{}

	req := newRequest(t, http.MethodPost, "/api/ai/sessions", map[string]any{
		"backend": "claude",
	})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// --- ArchiveSession ---

func TestArchiveSession_OK(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "To Delete", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodDelete, "/api/ai/session/archive?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ArchiveSession, req)
	assertOK(t, w)
}

func TestArchiveSession_NoSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/ai/session/archive", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ArchiveSession, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestArchiveSession_BadMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/session/archive", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ArchiveSession, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// --- ServeForkSession ---

// TestBuildForkContextHandler_PreservesSummarizedAssistant is a regression test
// for fork "amnesia": buildForkContext must keep assistant replies even when the
// messages carry a reading summary (GetMessagesBySessionID would strip them).
func TestBuildForkContextHandler_PreservesSummarizedAssistant(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Original", "claude", "", "default", "chat")
	require.NoError(t, err)

	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", "first user question", nil, false, "")
	require.NoError(t, err)
	assistantMsg, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", `{"blocks":[{"type":"text","text":"ai reply"}]}`, nil, false, "")
	require.NoError(t, err)

	// Give the assistant message a reading summary — this used to trigger
	// content stripping when building the fork context.
	_, err = service.WriteExec(
		"INSERT INTO summaries (target_type, target_id, summary) VALUES ('chat_message', ?, 'reading summary')",
		assistantMsg,
	)
	require.NoError(t, err)

	ctx := buildForkContext(sessionID)
	assert.Contains(t, ctx, "first user question")
	assert.Contains(t, ctx, "ai reply", "summarized assistant reply must survive fork context")
	assert.NotContains(t, ctx, "reading summary")
}

func TestServeForkSession_OK(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Original", "claude", "", "default", "chat")
	require.NoError(t, err)

	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", "Hello!", nil, false, "")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/fork?session_id="+sessionID, map[string]any{
		"sessionId": sessionID,
	})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeForkSession, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
	assert.NotEmpty(t, result["sessionId"])
}

func TestServeForkSession_NoSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/session/fork", map[string]any{})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeForkSession, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeForkSession_BadMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/session/fork", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeForkSession, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// --- getSessionID helper ---

func TestGetSessionID_FromQuery(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test?session_id=abc", http.NoBody)
	assert.Equal(t, "abc", getSessionID(req))
}

func TestGetSessionID_FromCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("chat_session_id"), Value: "from-cookie"})
	assert.Equal(t, "from-cookie", getSessionID(req))
}

func TestGetSessionID_Empty(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	assert.Equal(t, "", getSessionID(req))
}

// --- setSessionID helper ---

func TestSetSessionID_SetsCookie(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	setSessionID(w, req, "new-session-id")

	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == model.ScopedCookieName("chat_session_id") {
			found = true
			assert.Equal(t, "new-session-id", c.Value)
			assert.True(t, c.HttpOnly)
		}
	}
	assert.True(t, found, "session cookie should be set")
}

// --- ServeAISessionUpdate PATCH ---

func TestServeAISessionUpdate_NoSID(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"modelId": "test-model",
	})

	w := callHandler(ServeAISessionUpdate, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeAISessionUpdate_BadMethod(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/session/update?session_id=abc", nil)
	w := callHandler(ServeAISessionUpdate, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeAISessionUpdate_BodySessionIDOverridesCookie(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create two sessions. The pin target is passed in the body (the session the
	// user long-pressed), while the cookie points at a different session. The
	// body id must win so pinning a non-active session hits the right one.
	_, err := service.CreateSession(env.ProjectDir, "claude", "Active", "claude", "", "default", "chat")
	require.NoError(t, err)
	targetID, err := service.CreateSession(env.ProjectDir, "claude", "Target", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"sessionId": targetID,
		"pinned":    true,
	})
	req = withProjectCookie(req, env.ProjectDir)
	req = withSessionCookie(req, targetID+"-different") // cookie points elsewhere

	w := callHandler(ServeAISessionUpdate, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// The body-target session must now be pinned.
	sessions, err := service.GetSessions(env.ProjectDir, "")
	require.NoError(t, err)
	for _, s := range sessions {
		if s.ID == targetID {
			assert.True(t, s.Pinned, "body-target session should be pinned")
		}
	}
}

func TestServeAISessionUpdate_CookieSessionIDFallback(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// No body sessionId — falls back to the chat_session_id cookie.
	_, err := service.CreateSession(env.ProjectDir, "claude", "Active", "claude", "", "default", "chat")
	require.NoError(t, err)
	sid, err := service.CreateSession(env.ProjectDir, "claude", "Cookie", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"title": "Renamed via Cookie",
	})
	req = withProjectCookie(req, env.ProjectDir)
	req = withSessionCookie(req, sid)

	w := callHandler(ServeAISessionUpdate, req)
	assert.Equal(t, http.StatusOK, w.Code)

	sessions, err := service.GetSessions(env.ProjectDir, "")
	require.NoError(t, err)
	for _, s := range sessions {
		if s.ID == sid {
			assert.Equal(t, "Renamed via Cookie", s.Title)
		}
	}
}
