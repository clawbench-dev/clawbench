package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ServeConversationIndex ---

func TestServeConversationIndex_Basic(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Insert some messages
	_, err = service.UnsafeDBForTest().Exec(`INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'user', 'Hello', ?, 'claude', 0)`, env.ProjectDir, sessionID)
	require.NoError(t, err)
	_, err = service.UnsafeDBForTest().Exec(`INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', '{"blocks":[{"type":"text","text":"Hi there"}]}', ?, 'claude', 0)`, env.ProjectDir, sessionID)
	require.NoError(t, err)
	_, err = service.UnsafeDBForTest().Exec(`INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'user', 'How are you?', ?, 'claude', 0)`, env.ProjectDir, sessionID)
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/chat/user-messages?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeConversationIndex, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	messages := result["messages"].([]any)
	// Both roles are indexed now.
	require.Equal(t, 3, len(messages))

	first := messages[0].(map[string]any)
	assert.Equal(t, "user", first["role"])
	assert.Equal(t, "Hello", first["content"])

	// The assistant row carries the reply text as its summary, and its heavy
	// content column is emptied so it does not cross the wire.
	second := messages[1].(map[string]any)
	assert.Equal(t, "assistant", second["role"])
	assert.Equal(t, "Hi there", second["summary"])
	assert.Equal(t, "", second["content"])
}

func TestServeConversationIndex_NoSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/user-messages", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeConversationIndex, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeConversationIndex_WrongProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/chat/user-messages?session_id="+sessionID, nil)
	// Use different project path
	req = withProjectCookie(req, env.ProjectDir+"/other")

	w := callHandler(ServeConversationIndex, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeConversationIndex_DeletedSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Archive the session
	_, err = service.UnsafeDBForTest().Exec(`UPDATE chat_sessions SET archived = 1 WHERE id = ?`, sessionID)
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/chat/user-messages?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeConversationIndex, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeConversationIndex_PostMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/chat/user-messages", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeConversationIndex, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeConversationIndex_EmptyResult(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	// No messages inserted — should return empty array
	req := newRequest(t, http.MethodGet, "/api/ai/chat/user-messages?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeConversationIndex, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	messages := result["messages"].([]any)
	assert.Equal(t, 0, len(messages))
}

func TestServeConversationIndex_WithFiles(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Insert a user message with files
	_, err = service.UnsafeDBForTest().Exec(`INSERT INTO chat_history (project_path, role, content, files, session_id, backend, streaming) VALUES (?, 'user', 'Check this', '[{"path":"/src/main.go","isDir":false}]', ?, 'claude', 0)`, env.ProjectDir, sessionID)
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/chat/user-messages?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeConversationIndex, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	messages := result["messages"].([]any)
	assert.Equal(t, 1, len(messages))
}

func TestServeConversationIndex_NoProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/chat/user-messages?session_id="+sessionID, nil)
	// No project cookie

	w := callHandler(ServeConversationIndex, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}
