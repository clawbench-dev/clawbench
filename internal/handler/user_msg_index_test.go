package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ServeUserMessageIndex ---

func TestServeUserMessageIndex_Basic(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Insert some user messages
	_, err = service.DB.Exec(`INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'user', 'Hello', ?, 'claude', 0)`, env.ProjectDir, sessionID)
	require.NoError(t, err)
	_, err = service.DB.Exec(`INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', 'Hi', ?, 'claude', 0)`, env.ProjectDir, sessionID)
	require.NoError(t, err)
	_, err = service.DB.Exec(`INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'user', 'How are you?', ?, 'claude', 0)`, env.ProjectDir, sessionID)
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/chat/user-messages?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeUserMessageIndex, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	messages := result["messages"].([]any)
	assert.Equal(t, 2, len(messages))
}

func TestServeUserMessageIndex_NoSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/user-messages", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeUserMessageIndex, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeUserMessageIndex_WrongProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/chat/user-messages?session_id="+sessionID, nil)
	// Use different project path
	req = withProjectCookie(req, env.ProjectDir+"/other")

	w := callHandler(ServeUserMessageIndex, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeUserMessageIndex_DeletedSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Soft-delete the session
	_, err = service.DB.Exec(`UPDATE chat_sessions SET deleted = 1 WHERE id = ?`, sessionID)
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/chat/user-messages?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeUserMessageIndex, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeUserMessageIndex_PostMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/chat/user-messages", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeUserMessageIndex, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeUserMessageIndex_EmptyResult(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	// No user messages inserted — should return empty array
	req := newRequest(t, http.MethodGet, "/api/ai/chat/user-messages?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeUserMessageIndex, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	messages := result["messages"].([]any)
	assert.Equal(t, 0, len(messages))
}

func TestServeUserMessageIndex_WithFiles(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Insert a user message with files
	_, err = service.DB.Exec(`INSERT INTO chat_history (project_path, role, content, files, session_id, backend, streaming) VALUES (?, 'user', 'Check this', '["/src/main.go"]', ?, 'claude', 0)`, env.ProjectDir, sessionID)
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/chat/user-messages?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeUserMessageIndex, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	messages := result["messages"].([]any)
	assert.Equal(t, 1, len(messages))
}

func TestServeUserMessageIndex_NoProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/chat/user-messages?session_id="+sessionID, nil)
	// No project cookie

	w := callHandler(ServeUserMessageIndex, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// --- ServeChatCount deleted session (GetSessionBackend guard) ---

func TestServeChatCount_DeletedSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Soft-delete the session
	_, err = service.DB.Exec(`UPDATE chat_sessions SET deleted = 1 WHERE id = ?`, sessionID)
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/chat/count?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatCount, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
