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
