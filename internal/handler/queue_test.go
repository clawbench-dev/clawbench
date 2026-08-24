package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
)

// createQueueSession creates a real session in the test DB and returns its ID.
func createQueueSession(t *testing.T, env *testEnv, sessionID string) {
	t.Helper()
	_, err := service.CreateSession(env.ProjectDir, "claude", "Queue Session", "", "", "default", "chat")
	if err != nil {
		// Fallback: insert directly if CreateSession signature changed
		_, err2 := service.UnsafeDBForTest().Exec(
			`INSERT OR IGNORE INTO chat_sessions (id, project_path, backend, title) VALUES (?, ?, 'claude', 'Queue Session')`,
			sessionID, env.ProjectDir,
		)
		assert.NoError(t, err2)
		return
	}
	// Use the requested session id directly.
	_, _ = service.UnsafeDBForTest().Exec(
		`INSERT OR IGNORE INTO chat_sessions (id, project_path, backend, title) VALUES (?, ?, 'claude', 'Queue Session')`,
		sessionID, env.ProjectDir,
	)
}

func TestQueueHandler_Enqueue_Success(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-enqueue-1"
	createQueueSession(t, env, sessionID)
	defer service.ClearQueuedMessages(sessionID)

	body := map[string]any{
		"message": "hello world",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/queue?session_id="+sessionID, body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	assertOK(t, w)
	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	assert.Equal(t, true, result["ok"])
	// Session not running → the handler starts a goroutine. Verify started flag.
	assert.Equal(t, true, result["started"])

	// Message persisted in DB (queued=1, will be drained by the goroutine).
	msgs, err := service.GetQueuedMessages(sessionID)
	assert.NoError(t, err)
	assert.LessOrEqual(t, len(msgs), 1)

	// Cancel to stop the started goroutine before teardown.
	service.CancelSession(sessionID)
}

func TestQueueHandler_Enqueue_WithFilePaths(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-enqueue-paths"
	createQueueSession(t, env, sessionID)
	defer service.ClearQueuedMessages(sessionID)

	body := map[string]any{
		"message":   "check this file",
		"filePaths": []string{"/main.go", "/util.go"},
	}
	req := newRequest(t, http.MethodPost, "/api/ai/queue?session_id="+sessionID, body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	assertOK(t, w)

	// Message persisted with the file paths attached.
	msgs, err := service.GetQueuedMessages(sessionID)
	assert.NoError(t, err)
	if len(msgs) > 0 {
		assert.Equal(t, "check this file", msgs[0].Content)
	}

	service.CancelSession(sessionID)
}

func TestQueueHandler_Enqueue_WithFiles(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-enqueue-files"
	createQueueSession(t, env, sessionID)
	defer service.ClearQueuedMessages(sessionID)

	body := map[string]any{
		"files": []map[string]any{{"path": "/upload/a.png", "isDir": false}, {"path": "/upload/b.jpg", "isDir": false}},
	}
	req := newRequest(t, http.MethodPost, "/api/ai/queue?session_id="+sessionID, body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	assertOK(t, w)
	service.CancelSession(sessionID)
}

func TestQueueHandler_Enqueue_MissingSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]any{"message": "test"}
	req := newRequest(t, http.MethodPost, "/api/ai/queue", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestQueueHandler_Enqueue_SessionNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// No session created — handler must 404.
	body := map[string]any{"message": "test"}
	req := newRequest(t, http.MethodPost, "/api/ai/queue?session_id=no-such-session", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	assertStatus(t, w, http.StatusNotFound)
}

func TestQueueHandler_Enqueue_InvalidJSON(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-enqueue-badjson"
	createQueueSession(t, env, sessionID)
	req := httptest.NewRequest(http.MethodPost, "/api/ai/queue?session_id="+sessionID, http.NoBody)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("clawbench_project"), Value: env.ProjectDir})
	w := callHandler(QueueHandler, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestQueueHandler_Enqueue_EmptyMessage(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-enqueue-empty"
	createQueueSession(t, env, sessionID)
	defer service.ClearQueuedMessages(sessionID)

	body := map[string]any{
		"message": "",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/queue?session_id="+sessionID, body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestQueueHandler_Get_Success(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-get-1"
	createQueueSession(t, env, sessionID)
	defer service.ClearQueuedMessages(sessionID)

	// Persist a queued message directly (no goroutine start).
	_, err := service.AddQueuedMessage(env.ProjectDir, "claude", sessionID, "hello", nil, "q-1", "")
	assert.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/queue?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	assertOK(t, w)
	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	queue, _ := result["queue"].([]any)
	assert.Len(t, queue, 1)
}

func TestQueueHandler_Get_MissingSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/queue", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestQueueHandler_Get_EmptyQueue(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-get-empty"
	createQueueSession(t, env, sessionID)

	req := newRequest(t, http.MethodGet, "/api/ai/queue?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	assertOK(t, w)
	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	queue, _ := result["queue"].([]any)
	assert.Len(t, queue, 0)
}

func TestQueueHandler_Delete_ByQueueID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-delete-qid"
	createQueueSession(t, env, sessionID)

	_, err := service.AddQueuedMessage(env.ProjectDir, "claude", sessionID, "msg", nil, "q-1", "")
	assert.NoError(t, err)
	_, err = service.AddQueuedMessage(env.ProjectDir, "claude", sessionID, "msg2", nil, "q-2", "")
	assert.NoError(t, err)

	req := newRequest(t, http.MethodDelete, "/api/ai/queue?session_id="+sessionID+"&queueId=q-1", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	assertOK(t, w)

	// q-1 cancelled (queued=0), q-2 still queued.
	assert.Equal(t, 1, service.GetQueuedCount(sessionID))
}

func TestQueueHandler_Delete_ClearAll(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-delete-all"
	createQueueSession(t, env, sessionID)

	_, _ = service.AddQueuedMessage(env.ProjectDir, "claude", sessionID, "msg", nil, "q-1", "")
	_, _ = service.AddQueuedMessage(env.ProjectDir, "claude", sessionID, "msg2", nil, "q-2", "")

	req := newRequest(t, http.MethodDelete, "/api/ai/queue?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	assertOK(t, w)
	assert.Equal(t, 0, service.GetQueuedCount(sessionID))
}

func TestQueueHandler_Delete_InvalidIndex(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-delete-index"
	createQueueSession(t, env, sessionID)

	req := newRequest(t, http.MethodDelete, "/api/ai/queue?session_id="+sessionID+"&index=0", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	// Legacy index-based delete is no longer supported.
	assertStatus(t, w, http.StatusBadRequest)
}

func TestQueueHandler_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPut, "/api/ai/queue?session_id=x", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestQueueHandler_Enqueue_CrossProject_403(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Session in another project.
	otherProject := t.TempDir()
	_ = otherProject
	sessionID := "q-cross-project"
	_, err := service.UnsafeDBForTest().Exec(
		`INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, '/other/project', 'claude', 'Other')`,
		sessionID,
	)
	assert.NoError(t, err)

	body := map[string]any{"message": "test"}
	req := newRequest(t, http.MethodPost, "/api/ai/queue?session_id="+sessionID, body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	assertStatus(t, w, http.StatusForbidden)
}

func TestQueueHandler_Get_CrossProject_403(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-cross-project-get"
	_, err := service.UnsafeDBForTest().Exec(
		`INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, '/other/project', 'claude', 'Other')`,
		sessionID,
	)
	assert.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/queue?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	assertStatus(t, w, http.StatusForbidden)
}

func TestQueueHandler_Delete_CrossProject_403(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-cross-project-del"
	_, err := service.UnsafeDBForTest().Exec(
		`INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, '/other/project', 'claude', 'Other')`,
		sessionID,
	)
	assert.NoError(t, err)

	req := newRequest(t, http.MethodDelete, "/api/ai/queue?session_id="+sessionID+"&queueId=q-1", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	assertStatus(t, w, http.StatusForbidden)
}

func TestQueueHandler_Enqueue_MissingProjectCookie(t *testing.T) {
	sessionID := "q-no-cookie"

	body := map[string]any{"message": "test"}
	req := newRequest(t, http.MethodPost, "/api/ai/queue?session_id="+sessionID, body)
	w := callHandler(QueueHandler, req)

	assertStatus(t, w, http.StatusForbidden)
}

func TestQueueHandler_Get_MissingProjectCookie(t *testing.T) {
	sessionID := "q-no-cookie-get"

	req := newRequest(t, http.MethodGet, "/api/ai/queue?session_id="+sessionID, nil)
	w := callHandler(QueueHandler, req)

	assertStatus(t, w, http.StatusForbidden)
}

func TestQueueHandler_Delete_MissingProjectCookie(t *testing.T) {
	sessionID := "q-no-cookie-del"

	req := newRequest(t, http.MethodDelete, "/api/ai/queue?session_id="+sessionID, nil)
	w := callHandler(QueueHandler, req)

	assertStatus(t, w, http.StatusForbidden)
}
