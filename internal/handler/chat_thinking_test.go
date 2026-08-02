package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServeThinkingDetail_Found(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)
	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant",
		`{"blocks":[{"type":"thinking","think_id":"th_01","done":true}]}`, nil, false, "")
	require.NoError(t, err)
	require.NoError(t, service.UpsertThinking(msgID, sessionID, "th_01", "deep reasoning"))

	req := newRequest(t, http.MethodGet, "/api/ai/chat/thinking?think_id=th_01&message_id="+fmt.Sprintf("%d", msgID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeThinkingDetail, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, "th_01", result["think_id"])
	assert.Equal(t, "deep reasoning", result["text"])
}

func TestServeThinkingDetail_MissingParams(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	req := newRequest(t, http.MethodGet, "/api/ai/chat/thinking", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeThinkingDetail, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeThinkingDetail_BadMessageID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	req := newRequest(t, http.MethodGet, "/api/ai/chat/thinking?think_id=th_x&message_id=invalid", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeThinkingDetail, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeThinkingDetail_NotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	req := newRequest(t, http.MethodGet, "/api/ai/chat/thinking?think_id=th_x&message_id=1", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeThinkingDetail, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeThinkingDetail_SessionIDFallback(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)
	msgID1, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant",
		`{"blocks":[{"type":"thinking","think_id":"th_fb","done":true}]}`, nil, false, "")
	require.NoError(t, err)
	msgID2, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant",
		`{"blocks":[]}`, nil, false, "")
	require.NoError(t, err)
	require.NoError(t, service.UpsertThinking(msgID1, sessionID, "th_fb", "fallback text"))

	req := newRequest(t, http.MethodGet,
		fmt.Sprintf("/api/ai/chat/thinking?think_id=th_fb&message_id=%d&session_id=%s", msgID2, sessionID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeThinkingDetail, req)
	assertOK(t, w)
	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, "fallback text", result["text"])

	req2 := newRequest(t, http.MethodGet,
		fmt.Sprintf("/api/ai/chat/thinking?think_id=th_fb&message_id=%d", msgID2), nil)
	req2 = withProjectCookie(req2, env.ProjectDir)
	w2 := callHandler(ServeThinkingDetail, req2)
	assert.Equal(t, http.StatusNotFound, w2.Code)
}

func TestServeThinkingDetail_ProjectMismatch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)
	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant",
		`{"blocks":[{"type":"thinking","think_id":"th_sec","done":true}]}`, nil, false, "")
	require.NoError(t, err)
	require.NoError(t, service.UpsertThinking(msgID, sessionID, "th_sec", "secret"))

	otherDir := env.WatchDir + "/other-project"
	_ = os.MkdirAll(otherDir, 0o755)

	req := newRequest(t, http.MethodGet, "/api/ai/chat/thinking?think_id=th_sec&message_id="+fmt.Sprintf("%d", msgID), nil)
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("clawbench_project"), Value: url.QueryEscape(otherDir)})
	w := callHandler(ServeThinkingDetail, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeThinkingDetail_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	req := newRequest(t, http.MethodPost, "/api/ai/chat/thinking", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeThinkingDetail, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
