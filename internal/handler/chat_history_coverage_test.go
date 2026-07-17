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

// --- ServeChatHistory additional coverage ---

func TestServeChatHistory_Get_NoAgentsAvailable(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Remove all agents so resolveAgentConfig fails
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{}
	defer func() { model.Agents = origAgents }()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/history", nil)
	req = withProjectCookie(req, env.ProjectDir)
	// No session cookie and no sessions → tries to create, fails with NoAgentsAvailable

	w := callHandler(ServeChatHistory, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestServeChatHistory_Post_WithFiles(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Set session cookie so POST handler can find the session
	req := newRequest(t, http.MethodPost, "/api/ai/chat/history", map[string]any{
		"role":      "user",
		"content":   "check this file",
		"files":     []model.FileEntry{{Path: "/src/main.go"}, {Path: "/src/util.go"}},
		"sessionId": sessionID,
	})
	req = withProjectCookie(req, env.ProjectDir)
	req.AddCookie(&http.Cookie{
		Name:  model.ScopedCookieName("chat_session_id"),
		Value: sessionID,
	})

	w := callHandler(ServeChatHistory, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
}

func TestServeChatHistory_Get_SessionFromCookie_WrongProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session in the project
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Try to access it with a different project cookie
	req := newRequest(t, http.MethodGet, "/api/ai/chat/history", nil)
	req = withProjectCookie(req, "/different/project")
	req.AddCookie(&http.Cookie{
		Name:  model.ScopedCookieName("chat_session_id"),
		Value: sessionID,
	})

	w := callHandler(ServeChatHistory, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeChatHistory_Get_DeletedSessionFromQueryParam(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Soft-delete the session
	require.NoError(t, service.DeleteSession(sessionID, env.ProjectDir, "claude"))

	req := newRequest(t, http.MethodGet, "/api/ai/chat/history?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatHistory, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeChatHistory_Post_SessionNotFoundInDB(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/chat/history", map[string]any{
		"role":      "user",
		"content":   "hello",
		"sessionId": "nonexistent-session-id",
	})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatHistory, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeChatHistory_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPut, "/api/ai/chat/history", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatHistory, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeChatHistory_Get_SessionFromQueryParamExisting(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Use session_id query param instead of cookie
	req := newRequest(t, http.MethodGet, "/api/ai/chat/history?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatHistory, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, sessionID, result["sessionId"])
}

// --- ServeSessions additional coverage ---

func TestServeSessions_Get_WithCursor(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session so there's data
	_, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/sessions?cursor=2026-05-16T15:25:50Z", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.NotNil(t, result["sessions"])
}

func TestServeSessions_Post_WithExplicitAgentID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/sessions", map[string]any{
		"agentId": "claude",
	})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "claude", result["agentId"])
}
