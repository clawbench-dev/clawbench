package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── ServeForkSession: POST /api/ai/session/fork ────────────────────────

func TestServeForkSession_NormalFlow(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a source session with messages
	sessID, err := service.CreateSession(env.ProjectDir, "claude", "Original", "claude", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessID, "user", "Hello", nil, false, "")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessID, "assistant", "Hi!", nil, false, "")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/fork", map[string]string{"sessionId": sessID})
	req = withProjectCookie(req, env.ProjectDir)
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("chat_session_id"), Value: sessID})

	w := callHandler(ServeForkSession, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.True(t, result["ok"].(bool))
	assert.NotEmpty(t, result["sessionId"])
	assert.NotEqual(t, sessID, result["sessionId"])
	assert.NotNil(t, result["sessionCount"])

	// Verify forked session title has [Fork] prefix
	newSessID := result["sessionId"].(string)
	title, err := service.GetSessionTitle(newSessID)
	require.NoError(t, err)
	assert.Contains(t, title, "Fork")
}

func TestServeForkSession_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/session/fork", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeForkSession, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeForkSession_MissingSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/session/fork", map[string]string{})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeForkSession, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeForkSession_SessionNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/session/fork", map[string]string{"sessionId": "nonexistent"})
	req = withProjectCookie(req, env.ProjectDir)
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("chat_session_id"), Value: "nonexistent"})

	w := callHandler(ServeForkSession, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeForkSession_UsesCookieSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessID, err := service.CreateSession(env.ProjectDir, "claude", "Original", "claude", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessID, "user", "Hello", nil, false, "")
	require.NoError(t, err)

	// No sessionId in body, but cookie is set
	req := newRequest(t, http.MethodPost, "/api/ai/session/fork", map[string]string{})
	req = withProjectCookie(req, env.ProjectDir)
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("chat_session_id"), Value: sessID})

	w := callHandler(ServeForkSession, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.True(t, result["ok"].(bool))
}

func TestServeForkSession_SessionLimitReturns409(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	origMax := model.SessionMaxCount
	model.SessionMaxCount = 1
	t.Cleanup(func() { model.SessionMaxCount = origMax })

	sessID, err := service.CreateSession(env.ProjectDir, "claude", "Original", "claude", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessID, "user", "Hello", nil, false, "")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/fork", map[string]string{"sessionId": sessID})
	req = withProjectCookie(req, env.ProjectDir)
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("chat_session_id"), Value: sessID})

	w := callHandler(ServeForkSession, req)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestServeForkSession_SessionCountIncremented(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessID, err := service.CreateSession(env.ProjectDir, "claude", "Original", "claude", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessID, "user", "Hello", nil, false, "")
	require.NoError(t, err)

	countBefore, err := service.GetSessionCount(env.ProjectDir)
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/fork", map[string]string{"sessionId": sessID})
	req = withProjectCookie(req, env.ProjectDir)
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("chat_session_id"), Value: sessID})

	w := callHandler(ServeForkSession, req)
	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, float64(countBefore+1), result["sessionCount"])

	countAfter, err := service.GetSessionCount(env.ProjectDir)
	require.NoError(t, err)
	assert.Equal(t, countBefore+1, countAfter)
}

func TestServeForkSession_BodySessionIdOverridesCookie(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sess1, err := service.CreateSession(env.ProjectDir, "claude", "Session 1", "claude", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sess1, "user", "From session 1", nil, false, "")
	require.NoError(t, err)

	sess2, err := service.CreateSession(env.ProjectDir, "claude", "Session 2", "claude", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sess2, "user", "From session 2", nil, false, "")
	require.NoError(t, err)

	// Cookie points to sess1, but body specifies sess2
	req := newRequest(t, http.MethodPost, "/api/ai/session/fork", map[string]string{"sessionId": sess2})
	req = withProjectCookie(req, env.ProjectDir)
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("chat_session_id"), Value: sess1})

	w := callHandler(ServeForkSession, req)
	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	newSessID := result["sessionId"].(string)

	// Forked session should have sess2's messages, not sess1's
	msgs, err := service.GetChatHistory(env.ProjectDir, "claude", newSessID)
	require.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Contains(t, msgs[0].Content, "From session 2")
}

func TestServeForkSession_WithBeforeMessageID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessID, err := service.CreateSession(env.ProjectDir, "claude", "Original", "claude", "", "default", "chat")
	require.NoError(t, err)
	user1ID, err := service.AddChatMessage(env.ProjectDir, "claude", sessID, "user", "First", nil, false, "")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessID, "assistant", "Answer 1", nil, false, "")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessID, "user", "Second", nil, false, "")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessID, "assistant", "Answer 2", nil, false, "")
	require.NoError(t, err)

	// Fork with beforeMessageId = first user message
	req := newRequest(t, http.MethodPost, "/api/ai/session/fork", map[string]any{"sessionId": sessID, "beforeMessageId": user1ID})
	req = withProjectCookie(req, env.ProjectDir)
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("chat_session_id"), Value: sessID})

	w := callHandler(ServeForkSession, req)
	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.True(t, result["ok"].(bool))

	newSessID := result["sessionId"].(string)
	msgs, err := service.GetChatHistory(env.ProjectDir, "claude", newSessID)
	require.NoError(t, err)
	assert.Len(t, msgs, 2) // first user message + its assistant reply
	assert.Equal(t, "First", msgs[0].Content)
	assert.Equal(t, "Answer 1", msgs[1].Content)

	// Title should be based on the user message content
	title, err := service.GetSessionTitle(newSessID)
	require.NoError(t, err)
	assert.Contains(t, title, "First")
	assert.Equal(t, "First", msgs[0].Content)
}

func TestServeForkSession_BeforeMessageID_LongContentTruncated(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessID, err := service.CreateSession(env.ProjectDir, "claude", "Original", "claude", "", "default", "chat")
	require.NoError(t, err)
	// Create a user message with content longer than 50 runes
	longContent := strings.Repeat("你好世界", 20) // 80 runes
	userID, err := service.AddChatMessage(env.ProjectDir, "claude", sessID, "user", longContent, nil, false, "")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/fork", map[string]any{"sessionId": sessID, "beforeMessageId": userID})
	req = withProjectCookie(req, env.ProjectDir)
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("chat_session_id"), Value: sessID})

	w := callHandler(ServeForkSession, req)
	require.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.True(t, result["ok"].(bool))

	newSessID := result["sessionId"].(string)
	title, err := service.GetSessionTitle(newSessID)
	require.NoError(t, err)
	// Title should contain truncated content (50 runes + "...")
	assert.Contains(t, title, "...")
	// Title should start with Fork prefix
	assert.Contains(t, title, "Fork")
}

func TestServeForkSession_BeforeMessageID_EmptyContentFallsBackToSessionTitle(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessID, err := service.CreateSession(env.ProjectDir, "claude", "MySessionTitle", "claude", "", "default", "chat")
	require.NoError(t, err)
	userID, err := service.AddChatMessage(env.ProjectDir, "claude", sessID, "user", "Hello", nil, false, "")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessID, "assistant", "Reply", nil, false, "")
	require.NoError(t, err)

	// Delete the chat_metadata and chat_tool_calls rows that might reference the message,
	// then delete the message itself. GetMessageContent will return ("", nil) for ErrNoRows.
	// Then when ForkSession tries to find the message, it also won't find it and
	// returns "not found in session" error. This tests the InvalidForkPoint error path.
	_, _ = service.DB.Exec("DELETE FROM chat_metadata WHERE message_id = ?", userID)
	_, _ = service.DB.Exec("DELETE FROM chat_tool_calls WHERE message_id = ?", userID)
	_, _ = service.DB.Exec("DELETE FROM chat_history WHERE id = ?", userID)

	req := newRequest(t, http.MethodPost, "/api/ai/session/fork", map[string]any{"sessionId": sessID, "beforeMessageId": userID})
	req = withProjectCookie(req, env.ProjectDir)
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("chat_session_id"), Value: sessID})

	w := callHandler(ServeForkSession, req)
	// ForkSession will fail because the message was deleted → "not found in session"
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeForkSession_BeforeMessageID_NotFoundInSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessID, err := service.CreateSession(env.ProjectDir, "claude", "Original", "claude", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessID, "user", "Hello", nil, false, "")
	require.NoError(t, err)

	// Use a beforeMessageId that doesn't exist in the session
	req := newRequest(t, http.MethodPost, "/api/ai/session/fork", map[string]any{"sessionId": sessID, "beforeMessageId": 99999})
	req = withProjectCookie(req, env.ProjectDir)
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("chat_session_id"), Value: sessID})

	w := callHandler(ServeForkSession, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeForkSession_BeforeMessageIDNotUserMessage(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessID, err := service.CreateSession(env.ProjectDir, "claude", "Original", "claude", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessID, "user", "Hello", nil, false, "")
	require.NoError(t, err)
	asstID, err := service.AddChatMessage(env.ProjectDir, "claude", sessID, "assistant", "World", nil, false, "")
	require.NoError(t, err)

	// Fork from an assistant message should return 400
	req := newRequest(t, http.MethodPost, "/api/ai/session/fork", map[string]any{"sessionId": sessID, "beforeMessageId": asstID})
	req = withProjectCookie(req, env.ProjectDir)
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("chat_session_id"), Value: sessID})

	w := callHandler(ServeForkSession, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
