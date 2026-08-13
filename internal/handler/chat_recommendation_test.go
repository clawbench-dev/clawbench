package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServeChatRecommendation_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/chat/recommendation?session_id=abc", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatRecommendation, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeChatRecommendation_MissingSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/chat/recommendation", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatRecommendation, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeChatRecommendation_SessionNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/chat/recommendation?session_id=nonexistent", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatRecommendation, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeChatRecommendation_ProjectMismatch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", "", "", "default", "chat")
	require.NoError(t, err)

	// Cookie points at a different project than the session's project.
	req := newRequest(t, http.MethodGet, "/api/chat/recommendation?session_id="+sid, nil)
	req = withProjectCookie(req, "/other/project/path")

	w := callHandler(ServeChatRecommendation, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeChatRecommendation_Success(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", "", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/chat/recommendation?session_id="+sid, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatRecommendation, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, sid, body["session_id"])
	assert.Contains(t, body, "recommendation")
}

func TestServeChatRecommendation_ReturnsOnlyForRequestedMessage(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", "", "", "default", "chat")
	require.NoError(t, err)

	// Persist a recommendation for two different assistant messages.
	service.SaveChatRecommendation(sid, env.ProjectDir, 5001, "for msg 5001")
	service.SaveChatRecommendation(sid, env.ProjectDir, 5002, "for msg 5002")

	// Asking about msg 5002 returns only its own recommendation.
	req := newRequest(t, http.MethodGet, "/api/chat/recommendation?session_id="+sid+"&message_id=5002", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeChatRecommendation, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "for msg 5002", body["recommendation"])

	// A message with no recommendation yet returns empty, never the stale one.
	req2 := newRequest(t, http.MethodGet, "/api/chat/recommendation?session_id="+sid+"&message_id=5003", nil)
	req2 = withProjectCookie(req2, env.ProjectDir)
	w2 := callHandler(ServeChatRecommendation, req2)
	assert.Equal(t, http.StatusOK, w2.Code)
	var body2 map[string]any
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &body2))
	assert.Equal(t, "", body2["recommendation"])
}
