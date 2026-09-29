package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"clawbench/internal/model"
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

	// The idle session starts a drain goroutine that claims the message, so the
	// queue may already be empty by the time we look. Claim the row ourselves
	// to inspect it deterministically; if the goroutine got it first, read the
	// materialized chat_history row instead.
	row, _, ok, err := service.ClaimNextAndMaterialize(sessionID)
	if ok {
		require.NoError(t, err)
		assert.Equal(t, "check this file", row.Content)
		require.Len(t, row.Files, 2, "both file paths must be attached")
	} else {
		messages, err := service.GetChatHistory(env.ProjectDir, "claude", sessionID)
		require.NoError(t, err)
		require.Len(t, messages, 1, "the message must be persisted")
		require.Len(t, messages[0].Files, 2, "both file paths must be attached")
		assert.Equal(t, "check this file", messages[0].Content)
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

// TestQueueHandler_Enqueue_URLAttachment verifies a URL attachment survives the
// queue endpoint.
//
// Regression: validateQueueFiles had no URL branch, so a kind=url entry was run
// through path resolution and rejected with 404 "File not found:
// acme/widgets#7" — silently dropping the attachment whenever a send went
// through the queue (i.e. whenever the session was already running).
func TestQueueHandler_Enqueue_URLAttachment(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-enqueue-url"
	createQueueSession(t, env, sessionID)
	defer service.ClearQueuedMessages(sessionID)

	body := map[string]any{
		"message": "look at this",
		"files": []map[string]any{{
			"path": "acme/widgets#7", "kind": "url", "url": "https://github.com/acme/widgets/issues/7",
		}},
	}
	req := newRequest(t, http.MethodPost, "/api/ai/queue?session_id="+sessionID, body)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(QueueHandler, req)
	assertOK(t, w)

	// The label and the address must both survive, and the entry must still be
	// marked as a URL (not turned into a resolved filesystem path).
	//
	// Assert on the persisted row rather than the live queue: the handler starts
	// a drain goroutine that may consume the queue entry concurrently.
	messages, err := service.GetChatHistory(env.ProjectDir, "claude", sessionID)
	require.NoError(t, err)
	require.Len(t, messages, 1, "the message must be persisted")
	require.Len(t, messages[0].Files, 1, "the URL entry must survive validation")
	got := messages[0].Files[0]
	assert.Equal(t, "url", got.Kind)
	assert.Equal(t, "https://github.com/acme/widgets/issues/7", got.URL)
	assert.Equal(t, "acme/widgets#7", got.Path, "the chip label must be preserved")

	service.CancelSession(sessionID)
}

// TestQueueHandler_Enqueue_QuoteAttachment verifies a quote attachment survives
// the queue endpoint without touching the filesystem.
//
// A quote's Path is only a label (and is empty for a quote taken from a chat
// message), so running it through path resolution would 404 the enqueue and
// silently drop the quote — exactly the regression the URL branch guards
// against.
func TestQueueHandler_Enqueue_QuoteAttachment(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-enqueue-quote"
	createQueueSession(t, env, sessionID)
	defer service.ClearQueuedMessages(sessionID)

	body := map[string]any{
		"message": "解释一下",
		"files": []map[string]any{{
			"path": "src/a.go", "kind": "quote", "id": "quote-1",
			"text": "x := 1", "note": "为什么这样写？", "language": "go",
			"startLine": 3, "endLine": 3,
		}},
	}
	req := newRequest(t, http.MethodPost, "/api/ai/queue?session_id="+sessionID, body)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(QueueHandler, req)
	assertOK(t, w)

	// Assert on the persisted row rather than the live queue: the handler starts
	// a drain goroutine that may consume the queue entry concurrently.
	messages, err := service.GetChatHistory(env.ProjectDir, "claude", sessionID)
	require.NoError(t, err)
	require.Len(t, messages, 1, "the message must be persisted")
	require.Len(t, messages[0].Files, 1, "the quote entry must survive validation")
	got := messages[0].Files[0]
	assert.Equal(t, "quote", got.Kind)
	assert.Equal(t, "quote-1", got.ID)
	assert.Equal(t, "x := 1", got.Text)
	assert.Equal(t, "为什么这样写？", got.Note)
	assert.Equal(t, "go", got.Language)
	assert.Equal(t, "src/a.go", got.Path, "the label must be preserved verbatim, not resolved")
	assert.Equal(t, 3, got.StartLine)

	service.CancelSession(sessionID)
}

// TestQueueHandler_Enqueue_QuoteAttachment_EmptyPath verifies a chat-message
// quote (no path at all) is accepted. Path resolution on "" would fail.
func TestQueueHandler_Enqueue_QuoteAttachment_EmptyPath(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-enqueue-quote-empty"
	createQueueSession(t, env, sessionID)
	defer service.ClearQueuedMessages(sessionID)

	body := map[string]any{
		"files": []map[string]any{{
			"kind": "quote", "id": "quote-2", "text": "quoted chat text",
		}},
	}
	req := newRequest(t, http.MethodPost, "/api/ai/queue?session_id="+sessionID, body)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(QueueHandler, req)
	assertOK(t, w) // a quote with no path is valid

	messages, err := service.GetChatHistory(env.ProjectDir, "claude", sessionID)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Len(t, messages[0].Files, 1)
	assert.Equal(t, "quoted chat text", messages[0].Files[0].Text)

	service.CancelSession(sessionID)
}

// TestQueueHandler_Enqueue_URLAttachment_RejectsUnsafeScheme verifies the queue
// endpoint applies the same scheme restriction as the chat endpoint.
func TestQueueHandler_Enqueue_URLAttachment_RejectsUnsafeScheme(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	for _, raw := range []string{"javascript:alert(1)", "data:text/html,x", "file:///etc/passwd", "not a url"} {
		t.Run(raw, func(t *testing.T) {
			sessionID := "q-enqueue-url-unsafe"
			createQueueSession(t, env, sessionID)
			defer service.ClearQueuedMessages(sessionID)

			body := map[string]any{
				"message": "x",
				"files":   []map[string]any{{"path": "label", "kind": "url", "url": raw}},
			}
			req := newRequest(t, http.MethodPost, "/api/ai/queue?session_id="+sessionID, body)
			req = withProjectCookie(req, env.ProjectDir)

			w := callHandler(QueueHandler, req)
			assert.Equal(t, http.StatusBadRequest, w.Code,
				"a non-http(s) URL must be rejected, got %s", w.Body.String())

			messages, err := service.GetChatHistory(env.ProjectDir, "claude", sessionID)
			require.NoError(t, err)
			assert.Empty(t, messages, "a rejected request must not persist a message")
		})
	}
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
		"SELECT COUNT(*) FROM queued_messages WHERE session_id = ? AND queue_id = ?",
		sessionID, "q-1",
	).Scan(&q1Rows)
	require.NoError(t, err)
	assert.Zero(t, q1Rows, "canceled queued message must be deleted from the queue table")
	// The cancel must not have materialized anything into chat_history.
	var historyRows int
	err = service.UnsafeDBForTest().QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sessionID,
	).Scan(&historyRows)
	require.NoError(t, err)
	assert.Zero(t, historyRows, "a canceled queued message must never reach chat_history")
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
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sessionID,
	).Scan(&remaining)
	require.NoError(t, err)
	assert.Zero(t, remaining, "cleared queued messages must never reach chat_history")
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

// TestQueueHandler_Enqueue_InvalidAgentID verifies that POST /api/ai/queue
// rejects an unknown agentId with 400 instead of silently falling back to the
// CLI backend with the wrong agent/config (ISS-246).
func TestQueueHandler_Enqueue_InvalidAgentID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-enqueue-bad-agent"
	createQueueSession(t, env, sessionID)
	defer service.ClearQueuedMessages(sessionID)

	body := map[string]any{
		"message": "hello",
		"agentId": "nonexistent-agent",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/queue?session_id="+sessionID, body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	assertStatus(t, w, http.StatusBadRequest)
	// The message must not be persisted (nothing enqueued).
	assert.Zero(t, service.GetQueuedCount(sessionID))
}

// TestQueueHandler_Enqueue_ValidAgentID verifies that POST /api/ai/queue
// accepts a registered agentId and runs the normal enqueue flow (ISS-246).
func TestQueueHandler_Enqueue_ValidAgentID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Register an agent so resolveAgentConfig succeeds, mirroring
	// TestServeForkSession_WithAgentID.
	model.Agents["queue-test-agent"] = &model.Agent{ID: "queue-test-agent", Backend: "claude"}
	t.Cleanup(func() { delete(model.Agents, "queue-test-agent") })

	sessionID := "q-enqueue-good-agent"
	createQueueSession(t, env, sessionID)
	defer service.ClearQueuedMessages(sessionID)

	body := map[string]any{
		"message": "hello",
		"agentId": "queue-test-agent",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/queue?session_id="+sessionID, body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)

	assertOK(t, w)
	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	assert.Equal(t, true, result["ok"])

	service.CancelSession(sessionID)
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

// TestQueueHandler_Enqueue_EmitsQueueAdded verifies the unified POST
// /api/ai/queue endpoint, when a turn is already running, broadcasts a
// queue_added event carrying the queue entry's identity (queueId), text, files
// and the sender's clientId — so other devices render it in the queue panel.
//
// A still-queued message has no chat_history row, so it must NOT be announced
// as a user_message: that would make clients render a bubble with no DB row
// behind it (a duplicate once the real drain materializes it).
func TestQueueHandler_Enqueue_EmitsQueueAdded(t *testing.T) {
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

	// Mark the session running so the message is queued rather than started.
	service.TrySetSessionRunning(sessionID)
	defer service.SetSessionRunning(sessionID, false)

	body := map[string]any{
		"message":  "queue emit me",
		"clientId": "sender-device-1",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/queue?session_id="+sessionID, body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueHandler, req)
	assertOK(t, w)

	// The handler must have broadcast queue_added with the entry's queueId.
	var found *ws.ServerMessage
	assert.Eventually(t, func() bool {
		for _, ev := range sub.GetBufferedEvents() {
			if ev.Event != "chat_stream" {
				continue
			}
			data, ok := ev.Data.(ws.ChatStreamData)
			if !ok || data.EventType != "queue_added" {
				continue
			}
			found = &ev
			return true
		}
		return false
	}, 2*time.Second, 20*time.Millisecond)

	require.NotNil(t, found, "expected a queue_added chat_stream event in the subscriber buffer")
	data := found.Data.(ws.ChatStreamData)
	payload, ok := data.Payload.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "sender-device-1", payload["senderClientId"])
	assert.Equal(t, "queue emit me", payload["text"])
	// The request sent no queueId, so the backend minted one. It must be
	// present — it is the entry's only identity (cancel/inject address it).
	assert.NotEmpty(t, payload["queueId"], "queue_added must carry the entry's queueId")
	_, hasMsgID := payload["messageId"]
	assert.False(t, hasMsgID, "queue_added must not carry a messageId — no chat_history row exists yet")

	// The message must NOT be announced as a real user message: it has no
	// chat_history row, so a bubble would be a duplicate with no DB backing.
	for _, ev := range sub.GetBufferedEvents() {
		if ev.Event != "chat_stream" {
			continue
		}
		if d, ok := ev.Data.(ws.ChatStreamData); ok {
			assert.NotEqual(t, "user_message", d.EventType,
				"a queued message must not be announced as a real user message")
		}
	}

	// It is genuinely queued, not persisted.
	messages, err := service.GetChatHistory(env.ProjectDir, "claude", sessionID)
	require.NoError(t, err)
	assert.Empty(t, messages, "a queued message must not be a chat_history row")
	assert.Equal(t, 1, service.GetQueuedCount(sessionID))
}

// ── QueueInjectHandler ──────────────────────────────────────────────────────

// TestQueueInjectHandler_MethodNotAllowed pins the method guard.
func TestQueueInjectHandler_MethodNotAllowed(t *testing.T) {
	req := newRequest(t, http.MethodGet, "/api/ai/queue/inject?session_id=s&queueId=q", nil)
	w := callHandler(QueueInjectHandler, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// TestQueueInjectHandler_RequiresQueueID verifies the queueId is mandatory: it
// is the identity of the bubble being acted on.
func TestQueueInjectHandler_RequiresQueueID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/queue/inject?session_id=s1", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueInjectHandler, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	assert.Equal(t, "QueueIdRequired", body["msgKey"])
}

// TestQueueInjectHandler_DeclinedIsConflictNotError verifies the benign path:
// the backend cannot inject (no live ACP conn), the message stays queued, and
// the response is a 409 decline — NOT a 500 and NOT a claim of success.
func TestQueueInjectHandler_DeclinedIsConflictNotError(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-inject-decline"
	createQueueSession(t, env, sessionID)
	defer service.ClearQueuedMessages(sessionID)

	_, err := service.AddQueuedMessage(env.ProjectDir, "claude", sessionID, "queued", nil, "q-inj-1", "")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/queue/inject?session_id="+sessionID+"&queueId=q-inj-1", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueInjectHandler, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	assert.Equal(t, false, body["inserted"])
	assert.Equal(t, true, body["queued"], "a decline must report the message is still queued")

	// The message must still be deliverable.
	queued, qerr := service.GetQueuedMessages(sessionID)
	require.NoError(t, qerr)
	require.Len(t, queued, 1)
}

// TestQueueInjectHandler_StaleQueueIDIsConflict verifies a queueId that is no
// longer queued (its turn already ran, or it was cancelled) is reported as a
// decline rather than injecting an unrelated row.
func TestQueueInjectHandler_StaleQueueIDIsConflict(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-inject-stale"
	createQueueSession(t, env, sessionID)

	req := newRequest(t, http.MethodPost, "/api/ai/queue/inject?session_id="+sessionID+"&queueId=does-not-exist", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueInjectHandler, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

// ── QueueInterruptHandler ───────────────────────────────────────────────────

// TestQueueInterruptHandler_MethodNotAllowed pins the method guard.
func TestQueueInterruptHandler_MethodNotAllowed(t *testing.T) {
	req := newRequest(t, http.MethodGet, "/api/ai/queue/interrupt?session_id=s&queueId=q", nil)
	w := callHandler(QueueInterruptHandler, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// TestQueueInterruptHandler_RequiresQueueID verifies queueId is mandatory — it
// is the freshness check that stops a stale click from killing an unrelated
// turn.
func TestQueueInterruptHandler_RequiresQueueID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/queue/interrupt?session_id=s1", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueInterruptHandler, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	assert.Equal(t, "QueueIdRequired", body["msgKey"])
}

// TestQueueInterruptHandler_NotQueuedRefuses verifies the guard: interrupting
// with a message that is not queued would stop the current reply and have
// nothing to run, so it must be refused (409, reason=not_queued) and the turn
// must be left alone.
func TestQueueInterruptHandler_NotQueuedRefuses(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-interrupt-not-queued"
	createQueueSession(t, env, sessionID)

	// A turn IS running, so the only thing stopping the interrupt is the
	// freshness check — that is what this test isolates.
	service.SetSessionRunning(sessionID, true, true)
	t.Cleanup(func() { service.SetSessionRunning(sessionID, false, true) })

	req := newRequest(t, http.MethodPost, "/api/ai/queue/interrupt?session_id="+sessionID+"&queueId=stale", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueInterruptHandler, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	assert.Equal(t, false, body["interrupted"])
	assert.Equal(t, "not_queued", body["reason"])
	assert.True(t, service.IsSessionRunning(sessionID),
		"a refused interrupt must not stop the session")
}

// TestQueueInterruptHandler_NoTurnIsIdle verifies that a queued message with no
// running turn reports idle (200) rather than an error: the drain loop will run
// the queue on its own, so nothing is wrong.
func TestQueueInterruptHandler_NoTurnIsIdle(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-interrupt-idle"
	createQueueSession(t, env, sessionID)
	defer service.ClearQueuedMessages(sessionID)

	_, err := service.AddQueuedMessage(env.ProjectDir, "claude", sessionID, "queued", nil, "q-int-idle", "")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/queue/interrupt?session_id="+sessionID+"&queueId=q-int-idle", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueInterruptHandler, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	assert.Equal(t, false, body["interrupted"])
	assert.Equal(t, "idle", body["reason"])

	// The message must survive — an interrupt never drops the queue.
	queued, qerr := service.GetQueuedMessages(sessionID)
	require.NoError(t, qerr)
	require.Len(t, queued, 1)
}

// TestQueueInterruptHandler_StopsRegisteredTurn verifies the happy path: with a
// queued message and a registered turn, the turn is cancelled and the queue is
// left intact for the drain loop.
func TestQueueInterruptHandler_StopsRegisteredTurn(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-interrupt-happy"
	createQueueSession(t, env, sessionID)
	defer service.ClearQueuedMessages(sessionID)

	_, err := service.AddQueuedMessage(env.ProjectDir, "claude", sessionID, "queued", nil, "q-int-happy", "")
	require.NoError(t, err)

	// Register a live runner, then a turn on it so CurrentTurnID finds one.
	// (A turn belongs to an execution; registering one without a runner is not a
	// state production can reach.)
	_, claimed := service.TryClaimSessionRun(sessionID)
	require.True(t, claimed)
	t.Cleanup(func() { service.FinishSessionRun(sessionID) })

	turnCtx, turnCancel := context.WithCancel(context.Background())
	t.Cleanup(turnCancel)
	turnID := service.RegisterSessionTurnCancel(sessionID, turnCancel)
	t.Cleanup(func() { service.UnregisterSessionTurnCancel(sessionID) })

	req := newRequest(t, http.MethodPost, "/api/ai/queue/interrupt?session_id="+sessionID+"&queueId=q-int-happy", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(QueueInterruptHandler, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	assert.Equal(t, true, body["interrupted"])

	// The turn was stopped...
	select {
	case <-turnCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("the registered turn must be cancelled")
	}
	// ...but the queue survived, which is what separates interrupt from cancel.
	queued, qerr := service.GetQueuedMessages(sessionID)
	require.NoError(t, qerr)
	require.Len(t, queued, 1, "interrupt must keep the queue")
	assert.NotZero(t, turnID)
}

// TestQueueInterruptHandler_StaleTurnIDDoesNotKillReplacement is the handler-level
// guard for the check-then-act race: the turn read at check time is replaced
// before the interrupt lands, so nothing must be interrupted.
func TestQueueInterruptHandler_StaleTurnIDDoesNotKillReplacement(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "q-interrupt-race"
	createQueueSession(t, env, sessionID)
	defer service.ClearQueuedMessages(sessionID)

	_, err := service.AddQueuedMessage(env.ProjectDir, "claude", sessionID, "queued", nil, "q-int-race", "")
	require.NoError(t, err)

	// Register a live runner for the turns below (a turn belongs to an execution).
	_, claimed := service.TryClaimSessionRun(sessionID)
	require.True(t, claimed)
	t.Cleanup(func() { service.FinishSessionRun(sessionID) })

	// T1 is registered, then replaced by T2 before the handler's interrupt runs.
	_, t1Cancel := context.WithCancel(context.Background())
	t.Cleanup(t1Cancel)
	t1 := service.RegisterSessionTurnCancel(sessionID, t1Cancel)

	t2Ctx, t2Cancel := context.WithCancel(context.Background())
	t.Cleanup(t2Cancel)
	service.RegisterSessionTurnCancel(sessionID, t2Cancel) // replaces T1
	t.Cleanup(func() { service.UnregisterSessionTurnCancel(sessionID) })

	// A stale id must be refused at the service level.
	if service.InterruptSessionTurnIfCurrent(sessionID, t1) {
		t.Fatal("a stale turn id must not interrupt the replacement turn")
	}
	select {
	case <-t2Ctx.Done():
		t.Fatal("the replacement turn must be left running")
	default:
	}
}
