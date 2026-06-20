package handler

import (
	"encoding/json"
	"net/http"
	"testing"

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
	req.AddCookie(&http.Cookie{Name: "chat_session_id", Value: sessID})

	w := callHandler(ServeForkSession, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.True(t, result["ok"].(bool))
	assert.NotEmpty(t, result["sessionId"])
	assert.NotEqual(t, sessID, result["sessionId"])
	assert.NotNil(t, result["sessionCount"])
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
	req.AddCookie(&http.Cookie{Name: "chat_session_id", Value: "nonexistent"})

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
	req.AddCookie(&http.Cookie{Name: "chat_session_id", Value: sessID})

	w := callHandler(ServeForkSession, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.True(t, result["ok"].(bool))
}
