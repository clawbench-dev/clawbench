package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withAISummaryModel points the shared AI summary model at a test server and
// restores the previous config when the test ends.
func withAISummaryModel(t *testing.T, baseURL string) {
	t.Helper()
	prev := model.ConfigInstance.AISummary
	model.ConfigInstance.AISummary = model.AISummaryConfig{
		Format: "openai",
		API:    model.APIConfig{BaseURL: baseURL, Key: "test-key"},
	}
	t.Cleanup(func() { model.ConfigInstance.AISummary = prev })
}

func TestServeGenerateSessionTitle_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/session/generate-title?session_id=abc", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGenerateSessionTitle, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeGenerateSessionTitle_MissingSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/session/generate-title", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGenerateSessionTitle, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeGenerateSessionTitle_SessionNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/session/generate-title?session_id=nonexistent", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGenerateSessionTitle, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeGenerateSessionTitle_ProjectMismatch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", "", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/generate-title?session_id="+sid, nil)
	req = withProjectCookie(req, "/other/project/path")

	w := callHandler(ServeGenerateSessionTitle, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// Without a configured summary model the endpoint must refuse up front rather
// than attempting an HTTP call to an empty URL.
func TestServeGenerateSessionTitle_ModelNotConfigured(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	prev := model.ConfigInstance.AISummary
	model.ConfigInstance.AISummary = model.AISummaryConfig{}
	t.Cleanup(func() { model.ConfigInstance.AISummary = prev })

	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", "", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/generate-title?session_id="+sid, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGenerateSessionTitle, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "SummaryModelNotConfigured")
}

// A session whose only messages are assistant replies has nothing to summarize.
func TestServeGenerateSessionTitle_NoUserMessages(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	withAISummaryModel(t, "http://127.0.0.1:0")

	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", "", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sid, "assistant", "hello there", nil, false, "")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/generate-title?session_id="+sid, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGenerateSessionTitle, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "NoUserMessagesToSummarize")
}

func TestServeGenerateSessionTitle_Success(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"修复登录超时"}}]}`))
	}))
	defer srv.Close()
	withAISummaryModel(t, srv.URL)

	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", "", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sid, "user", "登录一直超时", nil, false, "")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sid, "assistant", "让我看看", nil, false, "")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sid, "user", "顺便加个重试", nil, false, "")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/generate-title?session_id="+sid, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGenerateSessionTitle, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "修复登录超时", body["title"])

	// All user messages (and only user messages) reach the model.
	assert.Contains(t, capturedBody, "登录一直超时")
	assert.Contains(t, capturedBody, "顺便加个重试")
	assert.NotContains(t, capturedBody, "让我看看")
}

// A failing model call is reported as a server error, not an empty title.
func TestServeGenerateSessionTitle_ModelFailure(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	withAISummaryModel(t, srv.URL)

	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", "", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sid, "user", "hello", nil, false, "")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/generate-title?session_id="+sid, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGenerateSessionTitle, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "GenerateTitleFailed")
	// The upstream error text must not leak to the client.
	assert.NotContains(t, strings.ToLower(w.Body.String()), "unauthorized")
}
