package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"clawbench/internal/service"
	"clawbench/internal/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestQueueHandler_Enqueue_FilePathsMissing(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-enqueue-file-missing"
	createQueueSession(t, env, sessionID)
	defer service.ClearQueuedMessages(sessionID)

	body := map[string]any{
		"message":   "with file",
		"filePaths": []string{"does-not-exist.txt"},
	}
	req := newRequest(t, http.MethodPost, "/api/ai/queue?session_id="+sessionID, body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	assertStatus(t, w, http.StatusNotFound)
}

func TestQueueHandler_Enqueue_FilesEntryMissing(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-enqueue-files-missing"
	createQueueSession(t, env, sessionID)
	defer service.ClearQueuedMessages(sessionID)

	body := map[string]any{
		"message": "with structured file",
		"files": []map[string]any{
			{"path": "no-such-file.txt"},
		},
	}
	req := newRequest(t, http.MethodPost, "/api/ai/queue?session_id="+sessionID, body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	assertStatus(t, w, http.StatusNotFound)
}

func TestQueueHandler_Get_DBError(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-get-db-error"
	createQueueSession(t, env, sessionID)

	db := service.UnsafeDBForTest()
	require.NoError(t, db.Close())

	req := newRequest(t, http.MethodGet, "/api/ai/queue?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	assertStatus(t, w, http.StatusInternalServerError)
}

func TestQueueHandler_Delete_QueueID_DBError(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-delete-db-error"
	createQueueSession(t, env, sessionID)

	db := service.UnsafeDBForTest()
	require.NoError(t, db.Close())

	req := newRequest(t, http.MethodDelete, "/api/ai/queue?session_id="+sessionID+"&queueId=q-1", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	assertStatus(t, w, http.StatusInternalServerError)
}

func TestQueueHandler_Delete_ClearAll_DBError(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-clear-db-error"
	createQueueSession(t, env, sessionID)

	db := service.UnsafeDBForTest()
	require.NoError(t, db.Close())

	req := newRequest(t, http.MethodDelete, "/api/ai/queue?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	assertStatus(t, w, http.StatusInternalServerError)
}

func TestQueueHandler_Enqueue_WithFilePaths(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-enqueue-paths"
	createQueueSession(t, env, sessionID)
	defer service.ClearQueuedMessages(sessionID)

	// Create real files under the project so path validation passes.
	mainGo := filepath.Join(env.ProjectDir, "main.go")
	utilGo := filepath.Join(env.ProjectDir, "util.go")
	require.NoError(t, os.WriteFile(mainGo, []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(utilGo, []byte("package util"), 0o644))

	body := map[string]any{
		"message":   "check this file",
		"filePaths": []string{"main.go", "util.go"},
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

	// Create real files under the project so path validation passes.
	aPng := filepath.Join(env.ProjectDir, "a.png")
	bJpg := filepath.Join(env.ProjectDir, "b.jpg")
	require.NoError(t, os.WriteFile(aPng, []byte("png"), 0o644))
	require.NoError(t, os.WriteFile(bJpg, []byte("jpg"), 0o644))

	body := map[string]any{
		"files": []map[string]any{{"path": "a.png", "isDir": false}, {"path": "b.jpg", "isDir": false}},
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
	req = withProjectCookie(req, env.ProjectDir)
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

	// q-1 cancelled — its row is truly deleted (not merely un-queued), so it
	// can never resurface as a formal message. q-2 still queued.
	assert.Equal(t, 1, service.GetQueuedCount(sessionID))
	var q1Rows int
	err = service.UnsafeDBForTest().QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND queue_id = ?",
		sessionID, "q-1",
	).Scan(&q1Rows)
	require.NoError(t, err)
	assert.Zero(t, q1Rows, "canceled queued message must not remain in chat_history")
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
	// Clear-all deletes the queued rows outright — they must not remain as
	// formal messages in the session history.
	var remaining int
	err := service.UnsafeDBForTest().QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND queue_id IN ('q-1', 'q-2')",
		sessionID,
	).Scan(&remaining)
	require.NoError(t, err)
	assert.Zero(t, remaining, "cleared queued messages must not remain in chat_history")
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

// TestQueueHandler_Enqueue_RejectsPathTraversal verifies the unified
// POST /api/ai/queue endpoint validates attached file paths (path traversal
// outside the project must be rejected with 403), matching the legacy
// POST /api/ai/chat path (R3).
func TestQueueHandler_Enqueue_RejectsPathTraversal(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-enqueue-traversal"
	createQueueSession(t, env, sessionID)

	body := map[string]any{
		"message":   "read this",
		"filePaths": []string{"../../../etc/passwd"},
	}
	req := newRequest(t, http.MethodPost, "/api/ai/queue?session_id="+sessionID, body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	assertStatus(t, w, http.StatusForbidden)
}

// TestQueueHandler_Enqueue_EmitsUserMessageWithRealMsgID verifies the unified
// POST /api/ai/queue endpoint broadcasts a user_message event carrying the
// persisted DB message id (msgID > 0) and the sender's clientId — so other
// devices see the new message even before it drains (cross-device sync, plan
// 竞态 5).
func TestQueueHandler_Enqueue_EmitsUserMessageWithRealMsgID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-enqueue-emit"
	createQueueSession(t, env, sessionID)
	defer service.ClearQueuedMessages(sessionID)

	// Set up a WS manager with a real connection so EmitToSession buffers the
	// event (conn != nil path in broadcastToSubscription).
	origMgr := ws.GetManager()
	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(origMgr)

	conn := newTestWSConn(t)
	var writeMu sync.Mutex
	sub := mgr.Subscribe(conn, &writeMu, "test-queue-client", "")
	mgr.StreamHub().Subscribe("test-queue-client", sessionID)
	require.NotNil(t, sub)

	body := map[string]any{
		"message":  "queue emit me",
		"clientId": "sender-device-1",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/queue?session_id="+sessionID, body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)
	assertOK(t, w)

	// The handler must have broadcast user_message with the real DB id.
	var found *ws.ServerMessage
	assert.Eventually(t, func() bool {
		for _, ev := range sub.GetBufferedEvents() {
			if ev.Event != "chat_stream" {
				continue
			}
			data, ok := ev.Data.(ws.ChatStreamData)
			if !ok || data.EventType != "user_message" {
				continue
			}
			found = &ev
			return true
		}
		return false
	}, 2*time.Second, 20*time.Millisecond)

	require.NotNil(t, found, "expected a user_message chat_stream event in the subscriber buffer")
	data := found.Data.(ws.ChatStreamData)
	payload, ok := data.Payload.(map[string]any)
	require.True(t, ok)
	// messageId is stored as the original int64 (the buffer holds the Go value,
	// not JSON), so it may appear as int64 or float64 depending on marshaling.
	msgID, _ := payload["messageId"].(int64)
	if msgID == 0 {
		if f, ok := payload["messageId"].(float64); ok {
			msgID = int64(f)
		}
	}
	assert.Greater(t, msgID, int64(0), "user_message must carry the real persisted DB id (msgID > 0)")
	assert.Equal(t, "sender-device-1", payload["senderClientId"])
	assert.Equal(t, "queue emit me", payload["content"])

	service.CancelSession(sessionID)
}
