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

func TestServeBtwQuestion_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// POST (ask) and GET (list) are both supported; anything else is rejected.
	req := newRequest(t, http.MethodDelete, "/api/ai/session/btw?session_id=abc", nil)
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

// A malformed body must be rejected by decodeJSON before any session or model
// work happens (no record, no LLM call).
func TestServeBtwQuestion_InvalidJSON(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/session/btw", nil)
	req.Body = io.NopCloser(strings.NewReader("{not json"))
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeBtwQuestion, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
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

	rec, ok := body["record"].(map[string]any)
	require.True(t, ok, "response must carry the stored record")
	assert.Equal(t, "因为连接池只有 2 个连接。", rec["answer"])
	assert.Equal(t, "连接池是多大？", rec["question"])
	// The anchor is the last message that existed when the question was asked.
	assert.NotZero(t, rec["anchorMessageId"])
	assert.Empty(t, rec["error"])

	// The compressed session history is background context, the question is the
	// rolling tail.
	assert.Contains(t, capturedBody, "为什么并发一高就慢")
	assert.Contains(t, capturedBody, "连接池是多大？")
	// /btw answers with a larger cap than the recommendation pass.
	assert.Contains(t, capturedBody, `"max_tokens":8192`)
}

// /btw records must not pollute the conversation itself: the question and
// answer live in btw_questions, never in chat_history.
func TestServeBtwQuestion_DoesNotTouchChatHistory(t *testing.T) {
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
	assert.Equal(t, len(before), len(after), "the /btw exchange must not be added to chat_history")

	// But it IS stored, so the anchor can be rendered.
	stored, err := service.ListBtwQuestions(sid)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, "顺便问一句", stored[0].Question)
}

// A model failure still returns 200 with a stored record: the user asked the
// question, so its anchor must render and the drawer must explain the failure.
func TestServeBtwQuestion_ModelFailureStillRecords(t *testing.T) {
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
	require.Equal(t, http.StatusOK, w.Code, "a recorded failure is not an HTTP error")

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	rec, ok := body["record"].(map[string]any)
	require.True(t, ok)
	assert.NotEmpty(t, rec["error"], "the failure reason must be stored for the drawer")
	assert.Empty(t, rec["answer"])
}

// An empty model answer is a failure the user should see, not a silent success.
func TestServeBtwQuestion_EmptyAnswerIsRecordedAsError(t *testing.T) {
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
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	rec := body["record"].(map[string]any)
	assert.NotEmpty(t, rec["error"])
}

// GET lists the session's records so the client can place anchors.
func TestServeBtwList_Success(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", "", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sid, "user", "hello", nil, false, "")
	require.NoError(t, err)
	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", sid, "assistant", "hi", nil, false, "")
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"answer"}}]}`))
	}))
	defer srv.Close()
	withAISummaryModel(t, srv.URL)

	// Ask two questions at the same position.
	for _, q := range []string{"第一个问题", "第二个问题"} {
		ask := newRequest(t, http.MethodPost, "/api/ai/session/btw",
			map[string]any{"sessionId": sid, "question": q})
		ask = withProjectCookie(ask, env.ProjectDir)
		require.Equal(t, http.StatusOK, callHandler(ServeBtwQuestion, ask).Code)
	}

	req := newRequest(t, http.MethodGet, "/api/ai/session/btw?session_id="+sid, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeBtwQuestion, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Questions []service.BtwQuestion `json:"questions"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Questions, 2)
	assert.Equal(t, "第一个问题", body.Questions[0].Question)
	assert.Equal(t, "第二个问题", body.Questions[1].Question)
	// Both share the same anchor: the assistant message was last both times.
	assert.Equal(t, msgID, body.Questions[0].AnchorMessageID)
	assert.Equal(t, msgID, body.Questions[1].AnchorMessageID)
}

func TestServeBtwList_EmptyIsArrayNotNull(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", "", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/session/btw?session_id="+sid, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeBtwQuestion, req)
	require.Equal(t, http.StatusOK, w.Code)
	// `[]`, never `null` — the client iterates it directly.
	assert.Contains(t, w.Body.String(), `"questions":[]`)
}

func TestServeBtwList_SessionNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/session/btw?session_id=nonexistent", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeBtwQuestion, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
