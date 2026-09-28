package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServeBtwQuestion_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/session/btw?session_id=abc", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeBtwQuestion, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeBtwQuestion_MissingSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/session/btw", map[string]any{"question": "why?"})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeBtwQuestion, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "SessionIdRequired")
}

func TestServeBtwQuestion_SessionNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/session/btw",
		map[string]any{"sessionId": "nonexistent", "question": "why?"})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeBtwQuestion, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeBtwQuestion_ProjectMismatch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", "", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/btw",
		map[string]any{"sessionId": sid, "question": "why?"})
	req = withProjectCookie(req, "/other/project/path")

	w := callHandler(ServeBtwQuestion, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// Without a configured summary model the endpoint must refuse up front rather
// than attempting an HTTP call to an empty URL.
func TestServeBtwQuestion_ModelNotConfigured(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	prev := model.ConfigInstance.AISummary
	model.ConfigInstance.AISummary = model.AISummaryConfig{}
	t.Cleanup(func() { model.ConfigInstance.AISummary = prev })

	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", "", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/btw",
		map[string]any{"sessionId": sid, "question": "why?"})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeBtwQuestion, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "SummaryModelNotConfigured")
}

func TestServeBtwQuestion_EmptyQuestion(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	withAISummaryModel(t, "http://127.0.0.1:0")

	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", "", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/btw",
		map[string]any{"sessionId": sid, "question": "   "})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeBtwQuestion, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "BtwQuestionRequired")
}

// The happy path: the compressed history and the question reach the summary
// model, and the answer comes back to the client.
func TestServeBtwQuestion_Success(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"因为连接池只有 2 个连接。"}}]}`))
	}))
	defer srv.Close()
	withAISummaryModel(t, srv.URL)

	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", "", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sid, "user", "为什么并发一高就慢", nil, false, "")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sid, "assistant", "让我查一下连接池", nil, false, "")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/btw",
		map[string]any{"sessionId": sid, "question": "连接池是多大？"})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeBtwQuestion, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, true, body["ok"])
	assert.Equal(t, "因为连接池只有 2 个连接。", body["answer"])

	// The compressed session history is background context, the question is the
	// rolling tail.
	assert.Contains(t, capturedBody, "为什么并发一高就慢")
	assert.Contains(t, capturedBody, "连接池是多大？")
	// /btw answers with a larger cap than the recommendation pass.
	assert.Contains(t, capturedBody, `"max_tokens":8192`)
}

// /btw is explicitly transient: it must not add rows to the session history.
func TestServeBtwQuestion_DoesNotPersist(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()
	withAISummaryModel(t, srv.URL)

	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", "", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sid, "user", "hello", nil, false, "")
	require.NoError(t, err)

	before, err := service.GetMessagesBySessionID(sid)
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/btw",
		map[string]any{"sessionId": sid, "question": "顺便问一句"})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeBtwQuestion, req)
	require.Equal(t, http.StatusOK, w.Code)

	after, err := service.GetMessagesBySessionID(sid)
	require.NoError(t, err)
	assert.Equal(t, len(before), len(after), "the /btw question and answer must not be persisted")
}

// A failing model call is reported as a server error.
func TestServeBtwQuestion_ModelFailure(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	withAISummaryModel(t, srv.URL)

	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", "", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/btw",
		map[string]any{"sessionId": sid, "question": "why?"})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeBtwQuestion, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "BtwFailed")
}

// An empty model answer is an error, not a successful empty drawer. The
// summarize layer rejects empty output, so the handler reports it as a failure.
func TestServeBtwQuestion_EmptyAnswer(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"   "}}]}`))
	}))
	defer srv.Close()
	withAISummaryModel(t, srv.URL)

	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", "", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/btw",
		map[string]any{"sessionId": sid, "question": "why?"})
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeBtwQuestion, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "BtwFailed")
}
