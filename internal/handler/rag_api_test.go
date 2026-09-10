package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/rag"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- ServeMessageSummarize ----------

func TestServeMessageSummarize_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/rag/message/summarize?id=1", nil)
	w := callHandlerWithAuth(ServeMessageSummarize, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeMessageSummarize_MissingID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/rag/message/summarize", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeMessageSummarize, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeMessageSummarize_MessageNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/rag/message/summarize?id=999", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeMessageSummarize, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeMessageSummarize_GeneratesAndReturns(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)
	content := `{"blocks":[{"type":"text","text":"先查"},{"type":"tool_use","text":"Bash"},{"type":"text","text":"最终结论：完成"}]}`
	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", content, nil, false, "")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, fmt.Sprintf("/api/rag/message/summarize?id=%d", msgID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeMessageSummarize, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Summary    string `json:"summary"`
		HasSummary bool   `json:"hasSummary"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.HasSummary)
	assert.Equal(t, "最终结论：完成", resp.Summary)
}

func TestServeMessageSummarize_WrongProjectDenied(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Test", "claude", "", "default", "chat")
	require.NoError(t, err)
	content := `{"blocks":[{"type":"text","text":"最终结论：完成"}]}`
	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", content, nil, false, "")
	require.NoError(t, err)

	// Authenticate as a different project that is still under a root path.
	otherProject := filepath.Join(filepath.Dir(env.ProjectDir), "other")
	_ = os.MkdirAll(otherProject, 0o755)

	req := newRequest(t, http.MethodPost, fmt.Sprintf("/api/rag/message/summarize?id=%d", msgID), nil)
	req = withProjectCookie(req, otherProject)
	w := callHandlerWithAuth(ServeMessageSummarize, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// ---------- ServeRAGSearch ----------

func TestServeRAGSearch_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/rag/search", nil)
	w := callHandlerWithAuth(ServeRAGSearch, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeRAGSearch_EmptyQuery(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/rag/search", map[string]any{"q": ""})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGSearch, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeRAGSearch_MissingQuery(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/rag/search", map[string]any{})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGSearch, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeRAGSearch_NilStoreReturns503(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// With nil GlobalStore/GlobalEmbedder, RAGSearch should return 503
	origStore := rag.GlobalStore
	origEmbedder := rag.GlobalEmbedder
	t.Cleanup(func() {
		rag.GlobalStore = origStore
		rag.GlobalEmbedder = origEmbedder
	})
	rag.GlobalStore = nil
	rag.GlobalEmbedder = nil

	req := newRequest(t, http.MethodPost, "/api/rag/search", map[string]any{"q": "test"})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGSearch, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestServeRAGSearch_EmptyResultsArray(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Setup a real SQLite store + mock embedder
	origStore := rag.GlobalStore
	origEmbedder := rag.GlobalEmbedder
	t.Cleanup(func() {
		rag.GlobalStore = origStore
		rag.GlobalEmbedder = origEmbedder
	})

	store := setupRAGStore(t)
	rag.GlobalStore = store
	// Use a mock server that returns valid embeddings
	embedder := setupWorkingMockEmbedder(t)
	rag.GlobalEmbedder = embedder

	req := newRequest(t, http.MethodPost, "/api/rag/search", map[string]any{"q": "test"})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGSearch, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	// Results should be an empty array, not null
	results, ok := result["results"].([]any)
	assert.True(t, ok, "results should be an array")
	assert.Empty(t, results)
}

// ---------- ServeRAGMessage ----------

func TestServeRAGMessage_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/rag/message?id=1", nil)
	w := callHandlerWithAuth(ServeRAGMessage, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeRAGMessage_MissingID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/rag/message", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGMessage, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeRAGMessage_InvalidID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/rag/message?id=notanumber", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGMessage, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeRAGMessage_NotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/rag/message?id=99999", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGMessage, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeRAGMessage_Found(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Insert a message
	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", "", "user", "hello", nil, false, "NewSession")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/rag/message?id="+fmt.Sprint(msgID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGMessage, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// ---------- ServeRAGSession ----------

func TestServeRAGSession_MissingID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/rag/session", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGSession, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeRAGSession_NotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Nonexistent session — project ownership check fails (session not found)
	req := newRequest(t, http.MethodGet, "/api/rag/session?id=nonexistent", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGSession, req)
	// Returns 403 because session doesn't belong to this project (doesn't exist)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeRAGSession_Found(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session and add messages
	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test Session", "", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sid, "user", "hello", nil, false, "NewSession")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/rag/session?id="+sid, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGSession, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, sid, result["session_id"])
	msgs, ok := result["messages"].([]any)
	assert.True(t, ok, "messages should be an array")
	assert.NotEmpty(t, msgs)
}

func TestServeRAGSession_CrossProjectDenied(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session in the test project
	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test Session", "", "", "default", "chat")
	require.NoError(t, err)

	// Try to access it with a different project cookie
	req := newRequest(t, http.MethodGet, "/api/rag/session?id="+sid, nil)
	req = withProjectCookie(req, "/other/project/path")
	w := callHandlerWithAuth(ServeRAGSession, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeRAGSession_RemoteNoProjectDenied(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/rag/session?id=some-session", nil)
	// Default RemoteAddr is 192.0.2.1 (non-localhost)
	w := callHandlerWithAuth(ServeRAGSession, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeRAGMessage_CrossProjectDenied(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Insert a message in the test project
	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", "", "user", "hello", nil, false, "NewSession")
	require.NoError(t, err)

	// Try to access it with a different project cookie
	req := newRequest(t, http.MethodGet, "/api/rag/message?id="+fmt.Sprint(msgID), nil)
	req = withProjectCookie(req, "/other/project/path")
	w := callHandlerWithAuth(ServeRAGMessage, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeRAGSearch_CrossProjectIsolation(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Setup a real SQLite store + mock embedder
	origStore := rag.GlobalStore
	origEmbedder := rag.GlobalEmbedder
	t.Cleanup(func() {
		rag.GlobalStore = origStore
		rag.GlobalEmbedder = origEmbedder
	})

	store := setupRAGStore(t)
	rag.GlobalStore = store
	embedder := setupWorkingMockEmbedder(t)
	rag.GlobalEmbedder = embedder

	// Search with client-supplied project field — should be ignored,
	// cookie-derived project path should be used instead
	req := newRequest(t, http.MethodPost, "/api/rag/search", map[string]any{
		"q":       "test",
		"project": "/some/other/project", // This should be ignored
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGSearch, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// ---------- RAG global search (localhost without project cookie) ----------

func TestServeRAGSearch_LocalhostGlobalSearch(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Setup a real SQLite store + mock embedder
	origStore := rag.GlobalStore
	origEmbedder := rag.GlobalEmbedder
	t.Cleanup(func() {
		rag.GlobalStore = origStore
		rag.GlobalEmbedder = origEmbedder
	})

	store := setupRAGStore(t)
	rag.GlobalStore = store
	embedder := setupWorkingMockEmbedder(t)
	rag.GlobalEmbedder = embedder

	// Localhost request without project cookie — should succeed (global search)
	req := newRequest(t, http.MethodPost, "/api/rag/search", map[string]any{"q": "test"})
	req.RemoteAddr = "127.0.0.1:12345"
	w := callHandlerWithAuth(ServeRAGSearch, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServeRAGSearch_RemoteNoProjectDenied(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Remote request without project cookie — should be denied
	req := newRequest(t, http.MethodPost, "/api/rag/search", map[string]any{"q": "test"})
	// Default RemoteAddr is 192.0.2.1 (non-localhost)
	w := callHandlerWithAuth(ServeRAGSearch, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeRAGMessage_LocalhostCrossProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Insert a message in the test project
	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", "", "user", "hello", nil, false, "NewSession")
	require.NoError(t, err)

	// Localhost request without project cookie — should succeed (cross-project access)
	req := newRequest(t, http.MethodGet, "/api/rag/message?id="+fmt.Sprint(msgID), nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := callHandlerWithAuth(ServeRAGMessage, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServeRAGSession_LocalhostCrossProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session and add messages
	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test Session", "", "", "default", "chat")
	require.NoError(t, err)

	// Localhost request without project cookie — should succeed (cross-project access)
	req := newRequest(t, http.MethodGet, "/api/rag/session?id="+sid, nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := callHandlerWithAuth(ServeRAGSession, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// ---------- ServeRAGMessageIndexStatus ----------

func TestServeRAGMessageIndexStatus_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/rag/message-index-status?id=1", nil)
	w := callHandlerWithAuth(ServeRAGMessageIndexStatus, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeRAGMessageIndexStatus_MissingID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/rag/message-index-status", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGMessageIndexStatus, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeRAGMessageIndexStatus_NotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/rag/message-index-status?id=99999", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGMessageIndexStatus, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeRAGMessageIndexStatus_ReturnsFields(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Insert a message
	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", "", "user", "hello", nil, false, "NewSession")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/rag/message-index-status?id="+fmt.Sprint(msgID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGMessageIndexStatus, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Contains(t, result, "fts_indexed")
	assert.Contains(t, result, "vec_indexed")
	// Message not yet indexed by RAG
	assert.Equal(t, false, result["fts_indexed"])
	assert.Equal(t, false, result["vec_indexed"])
}

func TestServeRAGMessageIndexStatus_CrossProjectDenied(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", "", "user", "hello", nil, false, "NewSession")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/rag/message-index-status?id="+fmt.Sprint(msgID), nil)
	req = withProjectCookie(req, "/other/project/path")
	w := callHandlerWithAuth(ServeRAGMessageIndexStatus, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeRAGMessageIndexStatus_InvalidID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/rag/message-index-status?id=abc", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGMessageIndexStatus, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeRAGMessageIndexStatus_RemoteNoProjectDenied(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/rag/message-index-status?id=1", nil)
	// Default RemoteAddr is 192.0.2.1 (non-localhost)
	w := callHandlerWithAuth(ServeRAGMessageIndexStatus, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeRAGMessageIndexStatus_WithRAGStore(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	origStore := rag.GlobalStore
	t.Cleanup(func() { rag.GlobalStore = origStore })
	store := setupRAGStore(t)
	rag.GlobalStore = store

	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", "", "user", "hello", nil, false, "NewSession")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/rag/message-index-status?id="+fmt.Sprint(msgID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGMessageIndexStatus, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Contains(t, result, "fts_indexed")
	assert.Contains(t, result, "vec_indexed")
}

func TestServeRAGMessageIndexStatus_LocalhostNoProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", "", "user", "hello", nil, false, "NewSession")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/rag/message-index-status?id="+fmt.Sprint(msgID), nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := callHandlerWithAuth(ServeRAGMessageIndexStatus, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// ---------- ServeRAGStatus ----------

func TestServeRAGStatus_MethodCheck(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/rag/status", nil)
	w := callHandlerWithAuth(ServeRAGStatus, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeRAGStatus_ReturnsFields(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/rag/status", nil)
	w := callHandlerWithAuth(ServeRAGStatus, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Contains(t, result, "available")
	assert.Contains(t, result, "mode")
	assert.Contains(t, result, "has_fts_data")
	assert.Contains(t, result, "has_vec_data")
	assert.Contains(t, result, "embedder_healthy")
	assert.Contains(t, result, "total_messages")
	assert.Contains(t, result, "indexed_messages")
	assert.Contains(t, result, "embedded_messages")
	assert.Contains(t, result, "fts_size_bytes")
	assert.Contains(t, result, "vec_size_bytes")
}

func TestServeRAGStatus_VectorDisabled(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// With nil GlobalStore (RAG not initialized), mode should be "none"
	req := newRequest(t, http.MethodGet, "/api/rag/status", nil)
	w := callHandlerWithAuth(ServeRAGStatus, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	// Nil store: mode="none" (config-based — RAG not initialized)
	assert.Equal(t, "none", result["mode"])
	assert.Equal(t, false, result["has_vec_data"])
	assert.Equal(t, false, result["embedder_healthy"])
}

func TestServeRAGStatus_ProgressCounts(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Insert some messages
	_, err := service.AddChatMessage(env.ProjectDir, "claude", "", "user", "hello", nil, false, "NewSession")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", "", "user", "world", nil, false, "NewSession")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/rag/status", nil)
	w := callHandlerWithAuth(ServeRAGStatus, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	// 2 messages inserted, none indexed yet
	assert.Equal(t, float64(2), result["total_messages"])
	assert.Equal(t, float64(0), result["indexed_messages"])
}

func TestServeRAGStatus_NilStore(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	origStore := rag.GlobalStore
	origEmbedder := rag.GlobalEmbedder
	t.Cleanup(func() {
		rag.GlobalStore = origStore
		rag.GlobalEmbedder = origEmbedder
	})
	rag.GlobalStore = nil
	rag.GlobalEmbedder = nil

	req := newRequest(t, http.MethodGet, "/api/rag/status", nil)
	w := callHandlerWithAuth(ServeRAGStatus, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, false, result["available"])
	assert.Equal(t, "none", result["mode"])
	assert.Equal(t, float64(0), result["embedded_messages"])
}

func TestServeRAGStatus_WithStore_HybridMode(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	origStore := rag.GlobalStore
	origEmbedder := rag.GlobalEmbedder
	origVectorEnabled := model.ConfigInstance.RAG.VectorEnabled
	t.Cleanup(func() {
		rag.GlobalStore = origStore
		rag.GlobalEmbedder = origEmbedder
		model.ConfigInstance.RAG.VectorEnabled = origVectorEnabled
	})

	store := setupRAGStore(t)
	rag.GlobalStore = store
	rag.GlobalEmbedder = setupWorkingMockEmbedder(t)
	model.ConfigInstance.RAG.VectorEnabled = true
	rag.SetEmbedderHealthy(true)

	req := newRequest(t, http.MethodGet, "/api/rag/status", nil)
	w := callHandlerWithAuth(ServeRAGStatus, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	// Config-based mode: store + VectorEnabled + embedder healthy → "hybrid"
	assert.Equal(t, "hybrid", result["mode"])
	assert.Contains(t, result, "embedded_messages")
}

func TestServeRAGStatus_WithStore_FtsOnlyMode(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	origStore := rag.GlobalStore
	origEmbedder := rag.GlobalEmbedder
	origVectorEnabled := model.ConfigInstance.RAG.VectorEnabled
	origEmbedderHealthy := rag.EmbedderHealthy()
	t.Cleanup(func() {
		rag.GlobalStore = origStore
		rag.GlobalEmbedder = origEmbedder
		model.ConfigInstance.RAG.VectorEnabled = origVectorEnabled
		rag.SetEmbedderHealthy(origEmbedderHealthy)
	})

	store := setupRAGStore(t)
	rag.GlobalStore = store
	rag.GlobalEmbedder = nil
	model.ConfigInstance.RAG.VectorEnabled = false
	rag.SetEmbedderHealthy(false)

	req := newRequest(t, http.MethodGet, "/api/rag/status", nil)
	w := callHandlerWithAuth(ServeRAGStatus, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	// Config-based mode: store + !VectorEnabled → "fts"
	assert.Equal(t, "fts", result["mode"])
	assert.Equal(t, false, result["embedder_healthy"])
}

// ---------- ServeRAGSessionSearch ----------

func TestServeRAGSessionSearch_MethodCheck(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/rag/session-search", nil)
	w := callHandlerWithAuth(ServeRAGSessionSearch, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeRAGSessionSearch_EmptyQueryBrowsesRecentSessions(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Insert sessions in non-chronological order to verify newest-first output.
	insertSession(t, env.ProjectDir, "sess-old", "Old session", "2024-01-01 10:00:00", false)
	insertSession(t, env.ProjectDir, "sess-new", "New session", "2024-03-01 10:00:00", false)
	insertSession(t, env.ProjectDir, "sess-arch", "Archived session", "2024-02-01 10:00:00", true)

	req := newRequest(t, http.MethodPost, "/api/rag/session-search", map[string]any{})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGSessionSearch, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Sessions []struct {
			SessionID  string `json:"session_id"`
			Title      string `json:"session_title"`
			Archived   bool   `json:"archived"`
			MatchCount int    `json:"match_count"`
		} `json:"sessions"`
		Total int    `json:"total"`
		Mode  string `json:"mode"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)

	assert.Equal(t, 3, result.Total)
	assert.Equal(t, "recent", result.Mode)
	require.Len(t, result.Sessions, 3)
	// Newest first (reverse chronological).
	assert.Equal(t, "sess-new", result.Sessions[0].SessionID)
	assert.Equal(t, "sess-arch", result.Sessions[1].SessionID)
	assert.Equal(t, "sess-old", result.Sessions[2].SessionID)
	// Archived sessions are included.
	assert.True(t, result.Sessions[1].Archived)
	// Browse mode has no search hits, so no match count is reported.
	assert.Zero(t, result.Sessions[0].MatchCount)
	assert.Zero(t, result.Sessions[1].MatchCount)
	assert.Zero(t, result.Sessions[2].MatchCount)
}

func TestServeRAGSessionSearch_BrowseCursorPagination(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Four sessions; RAG.SearchLimit is 100 by default, so drive pagination by
	// passing an explicit small limit via config is not possible here. Instead
	// verify has_more is false when the whole set fits, and that a cursor
	// narrows the result set correctly.
	insertSession(t, env.ProjectDir, "s1", "S1", "2024-01-01 10:00:00", false)
	insertSession(t, env.ProjectDir, "s2", "S2", "2024-02-01 10:00:00", false)
	insertSession(t, env.ProjectDir, "s3", "S3", "2024-03-01 10:00:00", false)

	type resp struct {
		Sessions []struct {
			SessionID string `json:"session_id"`
		} `json:"sessions"`
		HasMore bool `json:"has_more"`
	}

	// First page (all three fit) → has_more false.
	req := newRequest(t, http.MethodPost, "/api/rag/session-search", map[string]any{})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGSessionSearch, req)
	require.Equal(t, http.StatusOK, w.Code)
	var page1 resp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page1))
	require.Len(t, page1.Sessions, 3)
	assert.False(t, page1.HasMore)

	// Cursor after s3 (newest) → only older rows remain.
	req = newRequest(t, http.MethodPost, "/api/rag/session-search", map[string]any{
		"cursor":    "2024-03-01T10:00:00Z",
		"cursor_id": "s3",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeRAGSessionSearch, req)
	require.Equal(t, http.StatusOK, w.Code)
	var page2 resp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page2))
	require.Len(t, page2.Sessions, 2)
	assert.Equal(t, "s2", page2.Sessions[0].SessionID)
	assert.Equal(t, "s1", page2.Sessions[1].SessionID)
}

func TestServeRAGSessionSearch_EmptyQueryRemoteNoProjectDenied(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/rag/session-search", map[string]any{})
	// Default RemoteAddr is 192.0.2.1 (non-localhost)
	w := callHandlerWithAuth(ServeRAGSessionSearch, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeRAGSessionSearch_BrowseArchiveFilter(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	insertSession(t, env.ProjectDir, "active-1", "Active", "2024-01-01 10:00:00", false)
	insertSession(t, env.ProjectDir, "arch-1", "Archived", "2024-02-01 10:00:00", true)

	type resp struct {
		Sessions []struct {
			SessionID string `json:"session_id"`
			Archived  bool   `json:"archived"`
		} `json:"sessions"`
		Total int `json:"total"`
	}

	// Active only.
	req := newRequest(t, http.MethodPost, "/api/rag/session-search", map[string]any{"archived": "active"})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGSessionSearch, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var active resp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &active))
	require.Len(t, active.Sessions, 1)
	assert.Equal(t, "active-1", active.Sessions[0].SessionID)
	assert.False(t, active.Sessions[0].Archived)

	// Archived only.
	req = newRequest(t, http.MethodPost, "/api/rag/session-search", map[string]any{"archived": "archived"})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeRAGSessionSearch, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var arch resp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &arch))
	require.Len(t, arch.Sessions, 1)
	assert.Equal(t, "arch-1", arch.Sessions[0].SessionID)
	assert.True(t, arch.Sessions[0].Archived)

	// All (default) includes both.
	req = newRequest(t, http.MethodPost, "/api/rag/session-search", map[string]any{})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeRAGSessionSearch, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var all resp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &all))
	assert.Equal(t, 2, all.Total)
}

func TestServeRAGSessionSearch_BrowseSortOldest(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	insertSession(t, env.ProjectDir, "new", "New", "2024-03-01 10:00:00", false)
	insertSession(t, env.ProjectDir, "old", "Old", "2024-01-01 10:00:00", false)
	insertSession(t, env.ProjectDir, "mid", "Mid", "2024-02-01 10:00:00", false)

	req := newRequest(t, http.MethodPost, "/api/rag/session-search", map[string]any{"sort": "oldest"})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGSessionSearch, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Sessions []struct {
			SessionID string `json:"session_id"`
		} `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Len(t, result.Sessions, 3)
	assert.Equal(t, "old", result.Sessions[0].SessionID)
	assert.Equal(t, "mid", result.Sessions[1].SessionID)
	assert.Equal(t, "new", result.Sessions[2].SessionID)
}

func TestServeRAGSessionSearch_BrowseOmitsMessageContent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	insertSession(t, env.ProjectDir, "sess-c", "Session", "2024-01-01 10:00:00", false)
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES (?, 'user', 'First message here', ?, 'claude')",
		env.ProjectDir, "sess-c",
	)
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/rag/session-search", map[string]any{})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGSessionSearch, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Sessions []struct {
			SessionID  string `json:"session_id"`
			MatchCount int    `json:"match_count"`
			Chunks     []struct {
				ChunkText string `json:"chunk_text"`
				Role      string `json:"role"`
			} `json:"chunks"`
		} `json:"sessions"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	require.Len(t, result.Sessions, 1)
	assert.Zero(t, result.Sessions[0].MatchCount)
	// Browse mode must not attach message content: it is fetched lazily by the
	// detail view to keep the list query cheap.
	assert.Empty(t, result.Sessions[0].Chunks)
}

// ---------- ServeRAGSessionFirstMessage ----------

func TestServeRAGSessionFirstMessage_MethodCheck(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/rag/session-first-message?session_id=x", nil)
	w := callHandlerWithAuth(ServeRAGSessionFirstMessage, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeRAGSessionFirstMessage_MissingSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/rag/session-first-message", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGSessionFirstMessage, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeRAGSessionFirstMessage_ReturnsEarliestMessage(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	insertSession(t, env.ProjectDir, "sess-fm", "Session", "2024-01-01 10:00:00", false)
	_, err := service.UnsafeDBForTest().Exec(
		`INSERT INTO chat_history (project_path, role, content, session_id, backend, created_at) VALUES
		 (?, 'assistant', 'second', 'sess-fm', 'claude', '2024-01-02 10:00:00'),
		 (?, 'user', 'first', 'sess-fm', 'claude', '2024-01-01 10:00:00')`,
		env.ProjectDir, env.ProjectDir,
	)
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/rag/session-first-message?session_id=sess-fm", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGSessionFirstMessage, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, "user", result.Role)
	assert.Equal(t, "first", result.Content)
}

func TestServeRAGSessionFirstMessage_ArchivedSessionAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	insertSession(t, env.ProjectDir, "sess-arch", "Archived", "2024-01-01 10:00:00", true)
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES (?, 'user', 'hello', 'sess-arch', 'claude')",
		env.ProjectDir,
	)
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/rag/session-first-message?session_id=sess-arch", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGSessionFirstMessage, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Content string `json:"content"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, "hello", result.Content)
}

func TestServeRAGSessionFirstMessage_EmptySessionReturnsEmpty(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	insertSession(t, env.ProjectDir, "sess-empty", "Empty", "2024-01-01 10:00:00", false)

	req := newRequest(t, http.MethodGet, "/api/rag/session-first-message?session_id=sess-empty", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGSessionFirstMessage, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result struct {
		MessageID int64  `json:"message_id"`
		Content   string `json:"content"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Zero(t, result.MessageID)
	assert.Empty(t, result.Content)
}

func TestServeRAGSessionFirstMessage_WrongProjectDenied(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	insertSession(t, env.ProjectDir, "sess-p", "Session", "2024-01-01 10:00:00", false)

	otherProject := filepath.Join(filepath.Dir(env.ProjectDir), "other-first-msg")
	_ = os.MkdirAll(otherProject, 0o755)

	req := newRequest(t, http.MethodGet, "/api/rag/session-first-message?session_id=sess-p", nil)
	req = withProjectCookie(req, otherProject)
	w := callHandlerWithAuth(ServeRAGSessionFirstMessage, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeRAGSessionFirstMessage_RemoteNoProjectDenied(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/rag/session-first-message?session_id=x", nil)
	// Default RemoteAddr is 192.0.2.1 (non-localhost)
	w := callHandlerWithAuth(ServeRAGSessionFirstMessage, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeRAGSessionSearch_BrowseModeDBError(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Drop chat_sessions so GetRecentSessions fails → RecentSessions returns an
	// error → the handler responds 503.
	_, err := service.UnsafeDBForTest().Exec("DROP TABLE chat_sessions")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/rag/session-search", map[string]any{})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGSessionSearch, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestServeRAGSessionSearch_RemoteNoProjectDenied(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/rag/session-search", map[string]any{"q": "test"})
	// Default RemoteAddr is 192.0.2.1 (non-localhost)
	w := callHandlerWithAuth(ServeRAGSessionSearch, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeRAGSessionSearch_NilStoreReturns503(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	origStore := rag.GlobalStore
	origEmbedder := rag.GlobalEmbedder
	t.Cleanup(func() {
		rag.GlobalStore = origStore
		rag.GlobalEmbedder = origEmbedder
	})
	rag.GlobalStore = nil
	rag.GlobalEmbedder = nil

	req := newRequest(t, http.MethodPost, "/api/rag/session-search", map[string]any{"q": "test"})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGSessionSearch, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestServeRAGSessionSearch_EmptyResultsArray(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	origStore := rag.GlobalStore
	origEmbedder := rag.GlobalEmbedder
	t.Cleanup(func() {
		rag.GlobalStore = origStore
		rag.GlobalEmbedder = origEmbedder
	})

	store := setupRAGStore(t)
	rag.GlobalStore = store
	embedder := setupWorkingMockEmbedder(t)
	rag.GlobalEmbedder = embedder

	req := newRequest(t, http.MethodPost, "/api/rag/session-search", map[string]any{"q": "test"})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGSessionSearch, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	sessions, ok := result["sessions"].([]any)
	assert.True(t, ok, "sessions should be an array")
	assert.Empty(t, sessions)
}

func TestServeRAGSessionSearch_LocalhostGlobalSearch(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	origStore := rag.GlobalStore
	origEmbedder := rag.GlobalEmbedder
	t.Cleanup(func() {
		rag.GlobalStore = origStore
		rag.GlobalEmbedder = origEmbedder
	})

	store := setupRAGStore(t)
	rag.GlobalStore = store
	embedder := setupWorkingMockEmbedder(t)
	rag.GlobalEmbedder = embedder

	req := newRequest(t, http.MethodPost, "/api/rag/session-search", map[string]any{"q": "test"})
	req.RemoteAddr = "127.0.0.1:12345"
	w := callHandlerWithAuth(ServeRAGSessionSearch, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// ---------- Time-range filtering ----------

func TestNormalizeTimeBound(t *testing.T) {
	// Date-only from: expands to the start of that local day.
	assert.Equal(t, "2024-03-01 00:00:00", normalizeTimeBound("2024-03-01", false))
	// Date-only to: expands to the last second of that local day, so a session
	// created later the same day is still included.
	assert.Equal(t, "2024-03-01 23:59:59", normalizeTimeBound("2024-03-01", true))
	// Whitespace is trimmed; empty stays empty.
	assert.Equal(t, "2024-03-01 00:00:00", normalizeTimeBound("  2024-03-01  ", false))
	assert.Equal(t, "", normalizeTimeBound("", false))
	assert.Equal(t, "", normalizeTimeBound("   ", true))
	// RFC3339 converts to the local wall-clock text SQLite stores.
	local := time.Date(2024, 3, 1, 10, 30, 0, 0, time.Local)
	assert.Equal(t, local.Format("2006-01-02 15:04:05"), normalizeTimeBound(local.Format(time.RFC3339), false))
	// Unparseable input passes through so the caller binds something predictable.
	assert.Equal(t, "not-a-date", normalizeTimeBound("not-a-date", false))
}

func TestServeRAGSessionSearch_BrowseTimeRangeFilter(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	insertSession(t, env.ProjectDir, "jan", "Jan", "2024-01-15 10:00:00", false)
	insertSession(t, env.ProjectDir, "feb", "Feb", "2024-02-15 10:00:00", false)
	insertSession(t, env.ProjectDir, "mar", "Mar", "2024-03-15 10:00:00", false)

	type resp struct {
		Sessions []struct {
			SessionID string `json:"session_id"`
		} `json:"sessions"`
		Total int `json:"total"`
	}

	// Date-only bounds select the whole February window.
	req := newRequest(t, http.MethodPost, "/api/rag/session-search", map[string]any{
		"from": "2024-02-01",
		"to":   "2024-02-29",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeRAGSessionSearch, req)
	require.Equal(t, http.StatusOK, w.Code)
	var feb resp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &feb))
	require.Len(t, feb.Sessions, 1)
	assert.Equal(t, "feb", feb.Sessions[0].SessionID)

	// A single-day bound must include sessions created later that day (the
	// endOfDay expansion is what makes this work with the stored timestamp).
	req = newRequest(t, http.MethodPost, "/api/rag/session-search", map[string]any{
		"from": "2024-03-15",
		"to":   "2024-03-15",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeRAGSessionSearch, req)
	require.Equal(t, http.StatusOK, w.Code)
	var day resp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &day))
	require.Len(t, day.Sessions, 1)
	assert.Equal(t, "mar", day.Sessions[0].SessionID)

	// Lower bound only → everything from February onward.
	req = newRequest(t, http.MethodPost, "/api/rag/session-search", map[string]any{"from": "2024-02-01"})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeRAGSessionSearch, req)
	require.Equal(t, http.StatusOK, w.Code)
	var fromFeb resp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fromFeb))
	assert.Equal(t, 2, fromFeb.Total)

	// Window with no sessions → empty list, not an error.
	req = newRequest(t, http.MethodPost, "/api/rag/session-search", map[string]any{
		"from": "2025-01-01",
		"to":   "2025-12-31",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeRAGSessionSearch, req)
	require.Equal(t, http.StatusOK, w.Code)
	var empty resp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &empty))
	assert.Equal(t, 0, empty.Total)
}

// ---------- ServeRAGReset ----------

func TestServeRAGReset_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/rag/reset", nil)
	w := callHandlerWithAuth(ServeRAGReset, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeRAGReset_NilStoreReturns503(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	origStore := rag.GlobalStore
	t.Cleanup(func() { rag.GlobalStore = origStore })
	rag.GlobalStore = nil

	req := newRequest(t, http.MethodPost, "/api/rag/reset", nil)
	w := callHandlerWithAuth(ServeRAGReset, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestServeRAGReset_Success(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	origStore := rag.GlobalStore
	origEmbedder := rag.GlobalEmbedder
	t.Cleanup(func() {
		rag.GlobalStore = origStore
		rag.GlobalEmbedder = origEmbedder
	})

	store := setupRAGStore(t)
	rag.GlobalStore = store
	embedder := setupWorkingMockEmbedder(t)
	rag.GlobalEmbedder = embedder

	// Insert a message and mark it as indexed
	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", "", "user", "hello", nil, false, "NewSession")
	require.NoError(t, err)
	err = service.MarkMessageIndexed(msgID)
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/rag/reset", nil)
	w := callHandlerWithAuth(ServeRAGReset, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "ok", result["status"])
	assert.Equal(t, float64(1), result["messages_reset"])

	// Verify the message's indexed flag was reset
	unindexed, err := service.UnindexedCount()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, unindexed, 1)
}

func TestServeRAGReset_ConcurrencyConflict(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	origStore := rag.GlobalStore
	origEmbedder := rag.GlobalEmbedder
	t.Cleanup(func() {
		rag.GlobalStore = origStore
		rag.GlobalEmbedder = origEmbedder
		ragResetting.Store(false)
	})

	store := setupRAGStore(t)
	rag.GlobalStore = store
	rag.GlobalEmbedder = setupWorkingMockEmbedder(t)

	// Simulate an in-progress reset by setting the flag
	ragResetting.Store(true)

	req := newRequest(t, http.MethodPost, "/api/rag/reset", nil)
	w := callHandlerWithAuth(ServeRAGReset, req)
	assert.Equal(t, http.StatusConflict, w.Code)
}

// ---------- ServeRAGResetVector ----------

func TestServeRAGResetVector_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/rag/reset-vector", nil)
	w := callHandlerWithAuth(ServeRAGResetVector, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeRAGResetVector_NilStoreReturns503(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	origStore := rag.GlobalStore
	t.Cleanup(func() { rag.GlobalStore = origStore })
	rag.GlobalStore = nil

	req := newRequest(t, http.MethodPost, "/api/rag/reset-vector", nil)
	w := callHandlerWithAuth(ServeRAGResetVector, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestServeRAGResetVector_Success(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	origStore := rag.GlobalStore
	origEmbedder := rag.GlobalEmbedder
	t.Cleanup(func() {
		rag.GlobalStore = origStore
		rag.GlobalEmbedder = origEmbedder
	})

	store := setupRAGStore(t)
	rag.GlobalStore = store
	embedder := setupWorkingMockEmbedder(t)
	rag.GlobalEmbedder = embedder

	// Insert a message, index it, and embed it
	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", "", "user", "hello", nil, false, "NewSession")
	require.NoError(t, err)
	err = service.MarkMessageIndexed(msgID)
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/rag/reset-vector", nil)
	w := callHandlerWithAuth(ServeRAGResetVector, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "ok", result["status"])
	assert.Contains(t, result, "chunks_reset")

	// Verify the message's indexed flag was NOT reset (vector-only reset keeps FTS)
	var indexed int
	err = service.UnsafeDBForTest().QueryRow("SELECT indexed FROM chat_history WHERE id = ?", msgID).Scan(&indexed)
	require.NoError(t, err)
	assert.Equal(t, 1, indexed)
}

func TestServeRAGResetVector_ConcurrencyConflict(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	origStore := rag.GlobalStore
	origEmbedder := rag.GlobalEmbedder
	t.Cleanup(func() {
		rag.GlobalStore = origStore
		rag.GlobalEmbedder = origEmbedder
		ragResetting.Store(false)
	})

	store := setupRAGStore(t)
	rag.GlobalStore = store
	rag.GlobalEmbedder = setupWorkingMockEmbedder(t)

	// Simulate an in-progress reset by setting the flag
	ragResetting.Store(true)

	req := newRequest(t, http.MethodPost, "/api/rag/reset-vector", nil)
	w := callHandlerWithAuth(ServeRAGResetVector, req)
	assert.Equal(t, http.StatusConflict, w.Code)
}

// ---------- ServeRAGRebuildFTS ----------

func TestServeRAGRebuildFTS_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/rag/rebuild-fts", nil)
	w := callHandlerWithAuth(ServeRAGRebuildFTS, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeRAGRebuildFTS_NilStoreReturns503(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	origStore := rag.GlobalStore
	t.Cleanup(func() { rag.GlobalStore = origStore })
	rag.GlobalStore = nil

	req := newRequest(t, http.MethodPost, "/api/rag/rebuild-fts", nil)
	w := callHandlerWithAuth(ServeRAGRebuildFTS, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestServeRAGRebuildFTS_Success(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	origStore := rag.GlobalStore
	origEmbedder := rag.GlobalEmbedder
	t.Cleanup(func() {
		rag.GlobalStore = origStore
		rag.GlobalEmbedder = origEmbedder
	})

	store := setupRAGStore(t)
	rag.GlobalStore = store
	rag.GlobalEmbedder = setupWorkingMockEmbedder(t)
	store.SetEmbeddingDim(1024)

	// Insert a message, index it, and embed it
	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", "", "user", "hello world", nil, false, "NewSession")
	require.NoError(t, err)
	err = service.MarkMessageIndexed(msgID)
	require.NoError(t, err)

	chunks := []rag.Chunk{{
		SessionID: "sess-1", MessageID: msgID, ChunkText: "hello world",
		ChunkTextSegmented: "hello world", ChunkIndex: 0, TokenCount: 2,
		Embedding: make([]float64, 1024), HasEmbedding: true,
		ProjectPath: env.ProjectDir, Backend: "claude", Role: "user",
	}}
	require.NoError(t, store.InsertChunks(chunks))

	req := newRequest(t, http.MethodPost, "/api/rag/rebuild-fts", nil)
	w := callHandlerWithAuth(ServeRAGRebuildFTS, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "ok", result["status"])
	assert.Equal(t, float64(1), result["chunks_rebuilt"])

	// Independent rebuild must not reset message indexed flags (FTS layer only)
	var indexed int
	err = service.UnsafeDBForTest().QueryRow("SELECT indexed FROM chat_history WHERE id = ?", msgID).Scan(&indexed)
	require.NoError(t, err)
	assert.Equal(t, 1, indexed)

	// Vectors must survive
	assert.True(t, store.HasVecData())
}

func TestServeRAGRebuildFTS_ConcurrencyConflict(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	origStore := rag.GlobalStore
	t.Cleanup(func() {
		rag.GlobalStore = origStore
		ragResetting.Store(false)
	})

	rag.GlobalStore = setupRAGStore(t)

	ragResetting.Store(true)

	req := newRequest(t, http.MethodPost, "/api/rag/rebuild-fts", nil)
	w := callHandlerWithAuth(ServeRAGRebuildFTS, req)
	assert.Equal(t, http.StatusConflict, w.Code)
}

// setupRAGStore creates a temporary SQLite store for handler tests.
func setupRAGStore(t *testing.T) *rag.Store {
	t.Helper()
	store, err := rag.NewSQLiteStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// insertSession inserts a chat session row for session-search browse tests.
func insertSession(t *testing.T, projectPath, id, title, createdAt string, archived bool) {
	t.Helper()
	archivedInt := 0
	if archived {
		archivedInt = 1
	}
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, archived, created_at, updated_at) VALUES (?, ?, 'claude', ?, ?, ?, ?)",
		id, projectPath, title, archivedInt, createdAt, createdAt,
	)
	require.NoError(t, err)
}

// setupWorkingMockEmbedder creates a mock EmbeddingClient backed by a test server
// that returns valid 1024-dim embeddings using OpenAI /v1/embeddings format.
func setupWorkingMockEmbedder(t *testing.T) *rag.EmbeddingClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/embeddings":
			// Return a 1024-dim embedding in OpenAI format
			emb := make([]float64, 1024)
			for i := range emb {
				emb[i] = 0.01
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"embedding": emb, "index": 0},
				},
			})
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{"id": "bge-m3"}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	client := rag.NewEmbeddingClient(server.URL, "bge-m3", "")
	client.HTTPClient = server.Client()
	return client
}
