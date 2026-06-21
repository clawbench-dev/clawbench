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

// --- ServeChatHistory GET ---

func TestServeChatHistory_Get_NewSessionCreated(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/history", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatHistory, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.NotEmpty(t, result["sessionId"])
}

func TestServeChatHistory_Get_ExistingSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/chat/history", nil)
	req = withProjectCookie(req, env.ProjectDir)
	req.AddCookie(&http.Cookie{
		Name:  model.ScopedCookieName("chat_session_id"),
		Value: sessionID,
	})

	w := callHandler(ServeChatHistory, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, sessionID, result["sessionId"])
}

func TestServeChatHistory_Get_SessionFromQueryParam(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/chat/history?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatHistory, req)
	assertOK(t, w)
}

func TestServeChatHistory_Get_WrongProjectAccess(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	otherDir := env.WatchDir + "/other-project"
	_ = os.MkdirAll(otherDir, 0o755)

	req := newRequest(t, http.MethodGet, "/api/ai/chat/history?session_id="+sessionID, nil)
	req.AddCookie(&http.Cookie{
		Name:  model.ScopedCookieName("clawbench_project"),
		Value: url.QueryEscape(otherDir),
	})

	w := callHandler(ServeChatHistory, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeChatHistory_Get_SessionNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/history?session_id=nonexistent", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatHistory, req)
	// Nonexistent session returns 403 because GetSessionProjectPath returns ""
	// which doesn't match the project cookie, so project mismatch fires first
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// --- ServeChatHistory POST ---

func TestServeChatHistory_Post_AddUserMessage(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/chat/history", map[string]any{
		"role":       "user",
		"content":    "Hello, AI!",
		"session_id": sessionID,
	})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatHistory, req)
	assertOK(t, w)
}

func TestServeChatHistory_Post_AddAssistantMessage(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/chat/history", map[string]any{
		"role":       "assistant",
		"content":    "Hello, human!",
		"session_id": sessionID,
	})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatHistory, req)
	assertOK(t, w)
}

func TestServeChatHistory_Post_BadRole(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/chat/history", map[string]any{
		"role":       "system",
		"content":    "invalid role",
		"session_id": sessionID,
	})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatHistory, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeChatHistory_Post_SessionFromCookie(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/chat/history", map[string]any{
		"role":    "user",
		"content": "Hello!",
	})
	req = withProjectCookie(req, env.ProjectDir)
	req.AddCookie(&http.Cookie{
		Name:  model.ScopedCookieName("chat_session_id"),
		Value: sessionID,
	})

	w := callHandler(ServeChatHistory, req)
	assertOK(t, w)
}

func TestServeChatHistory_Post_WrongProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	otherDir := env.WatchDir + "/other-project"
	_ = os.MkdirAll(otherDir, 0o755)

	req := newRequest(t, http.MethodPost, "/api/ai/chat/history", map[string]any{
		"role":       "user",
		"content":    "Hello!",
		"session_id": sessionID,
	})
	req.AddCookie(&http.Cookie{
		Name:  model.ScopedCookieName("clawbench_project"),
		Value: url.QueryEscape(otherDir),
	})

	w := callHandler(ServeChatHistory, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeChatHistory_Post_SessionNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/chat/history", map[string]any{
		"role":       "user",
		"content":    "Hello!",
		"session_id": "nonexistent",
	})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatHistory, req)
	// Nonexistent session returns 403 because GetSessionProjectPath returns ""
	// which doesn't match the project cookie, so project mismatch fires first
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// --- ServeChatHistory DELETE ---

func TestServeChatHistory_Delete_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/ai/chat/history", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatHistory, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// --- ServeChatCount ---

func TestServeChatCount_Success(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/chat/count?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatCount, req)
	assertOK(t, w)
}

func TestServeChatCount_ProjectMismatch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	otherDir := env.WatchDir + "/other-project"
	_ = os.MkdirAll(otherDir, 0o755)

	req := newRequest(t, http.MethodGet, "/api/ai/chat/count?session_id="+sessionID, nil)
	req.AddCookie(&http.Cookie{
		Name:  model.ScopedCookieName("clawbench_project"),
		Value: url.QueryEscape(otherDir),
	})

	w := callHandler(ServeChatCount, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeChatCount_MissingSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/count", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatCount, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeChatCount_WrongMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/chat/count", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatCount, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// --- ServeChatMessageUpdate ---

func TestServeChatMessageUpdate_Success(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", `{"blocks":[]}`, nil, false, "")
	require.NoError(t, err)
	require.True(t, msgID > 0)

	req := newRequest(t, http.MethodPut, "/api/ai/chat/message", map[string]any{
		"messageId": msgID,
		"content":   `{"blocks":[{"type":"text","text":"updated"}]}`,
	})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatMessageUpdate, req)
	assertOK(t, w)
}

func TestServeChatMessageUpdate_MissingMessageID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPut, "/api/ai/chat/message", map[string]any{
		"content": "some content",
	})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatMessageUpdate, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeChatMessageUpdate_ProjectMismatch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", `{"blocks":[]}`, nil, false, "")
	require.NoError(t, err)

	otherDir := env.WatchDir + "/other-project"
	_ = os.MkdirAll(otherDir, 0o755)

	req := newRequest(t, http.MethodPut, "/api/ai/chat/message", map[string]any{
		"messageId": msgID,
		"content":   "updated",
	})
	req.AddCookie(&http.Cookie{
		Name:  model.ScopedCookieName("clawbench_project"),
		Value: url.QueryEscape(otherDir),
	})

	w := callHandler(ServeChatMessageUpdate, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeChatMessageUpdate_WrongMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/chat/message", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatMessageUpdate, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeChatMessageUpdate_MessageNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPut, "/api/ai/chat/message", map[string]any{
		"messageId": 99999,
		"content":   "updated",
	})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatMessageUpdate, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- ServeToolCallDetail ---

func TestServeToolCallDetail_Found(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", `{"blocks":[]}`, nil, false, "")
	require.NoError(t, err)

	err = service.UpsertToolCall(msgID, sessionID, "toolu_td01", "Read", json.RawMessage(`{"file_path":"/tmp/test.go"}`), "contents", "success", "test.go", true)
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

	err = service.UpsertToolCall(msgID, sessionID, "toolu_td02", "Read", json.RawMessage(`{}`), "contents", "success", "", true)
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

// --- DeleteSession ---

func TestDeleteSession_OK(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "To Delete", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodDelete, "/api/ai/session/delete?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(DeleteSession, req)
	assertOK(t, w)
}

func TestDeleteSession_NoSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/ai/session/delete", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(DeleteSession, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeleteSession_BadMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/session/delete", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(DeleteSession, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// --- ServeForkSession ---

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
