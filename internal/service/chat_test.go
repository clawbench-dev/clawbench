package service_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"clawbench/internal/ai"
	"clawbench/internal/service"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/assert"
)

const schema = `
CREATE TABLE IF NOT EXISTS chat_history (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_path TEXT NOT NULL,
	role TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
	content TEXT NOT NULL,
	files TEXT,
	session_id TEXT,
	backend TEXT NOT NULL DEFAULT 'claude',
	streaming INTEGER NOT NULL DEFAULT 0,
	indexed INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS chat_sessions (
	id TEXT PRIMARY KEY,
	project_path TEXT NOT NULL,
	backend TEXT NOT NULL,
	title TEXT NOT NULL,
	agent_id TEXT DEFAULT '',
	agent_source TEXT DEFAULT 'default',
	model TEXT DEFAULT '',
	session_type TEXT NOT NULL DEFAULT 'chat',
	external_session_id TEXT DEFAULT '',
	source_session_id TEXT DEFAULT NULL,
	thinking_effort TEXT DEFAULT '',
	deleted INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	last_read_at DATETIME,
	UNIQUE(project_path, backend, id)
);
CREATE TABLE IF NOT EXISTS recent_projects (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_path TEXT UNIQUE NOT NULL,
	accessed_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS scheduled_tasks (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_path TEXT NOT NULL,
	name TEXT NOT NULL,
	cron_expr TEXT NOT NULL,
	agent_id TEXT NOT NULL,
	prompt TEXT NOT NULL,
	session_id TEXT DEFAULT '',
	status TEXT DEFAULT 'active',
	repeat_mode TEXT NOT NULL DEFAULT 'unlimited',
	max_runs INTEGER DEFAULT 0,
	last_run_at DATETIME,
	next_run_at DATETIME,
	run_count INTEGER DEFAULT 0,
	last_read_at DATETIME,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS task_executions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id INTEGER NOT NULL,
	session_id TEXT NOT NULL,
	trigger_type TEXT NOT NULL DEFAULT 'auto',
	status TEXT NOT NULL DEFAULT 'completed',
	read_at DATETIME,
	summary TEXT,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_executions_task ON task_executions(task_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_history_session ON chat_history(project_path, backend, session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_sessions_project_backend ON chat_sessions(project_path, backend);
CREATE INDEX IF NOT EXISTS idx_executions_session ON task_executions(session_id);
CREATE INDEX IF NOT EXISTS idx_tasks_project ON scheduled_tasks(project_path, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_history_session_id ON chat_history(session_id, role, streaming, created_at);
CREATE INDEX IF NOT EXISTS idx_history_unread ON chat_history(project_path, role, streaming, created_at);
CREATE INDEX IF NOT EXISTS idx_sessions_order ON chat_sessions(session_type, project_path, deleted, updated_at DESC, id DESC);
CREATE TABLE IF NOT EXISTS ai_raw_responses (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL,
	message_id INTEGER NOT NULL,
	backend TEXT NOT NULL DEFAULT '',
	raw_output TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS summaries (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	target_type TEXT NOT NULL,
	target_id   INTEGER NOT NULL,
	summary     TEXT NOT NULL,
	created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(target_type, target_id)
);
CREATE INDEX IF NOT EXISTS idx_sessions_source_session ON chat_sessions(source_session_id) WHERE source_session_id IS NOT NULL;
`

// setupDB creates an in-memory SQLite database with the required schema,
// sets service.DB, and returns a cleanup function.
func setupDB(t *testing.T) *sql.DB { //nolint:unparam // test helper: DB used implicitly via global state
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	assert.NoError(t, err)

	_, err = db.Exec(schema)
	assert.NoError(t, err)

	origDB := service.DB
	origDBRead := service.DBRead
	service.DB = db
	service.DBRead = db // Same instance for :memory: SQLite — data is shared
	t.Cleanup(func() {
		service.DB = origDB
		service.DBRead = origDBRead
		db.Close()
	})
	return db
}

// helperCreateSession creates a session and asserts success, returning the session ID.
func helperCreateSession(t *testing.T, projectPath, backend, title string) string {
	t.Helper()
	id, err := service.CreateSession(projectPath, backend, title, "", "", "default", "chat")
	assert.NoError(t, err)
	assert.NotEmpty(t, id)
	t.Cleanup(func() {
		service.SetSessionRunning(id, false)
	})
	return id
}

// helperCreateScheduledSession creates a scheduled session and asserts success.
func helperCreateScheduledSession(t *testing.T, _, backend, title string) string {
	t.Helper()
	id, err := service.CreateSession("/project", backend, title, "", "", "default", "scheduled")
	assert.NoError(t, err)
	assert.NotEmpty(t, id)
	return id
}

// ---------- GetChatHistory / AddChatMessage ----------

func TestAddChatMessageAndGetHistory(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test Session")

	_, err := service.AddChatMessage("/project", "claude", sid, "user", "Hello", nil, false, "NewSession")
	assert.NoError(t, err)

	_, err = service.AddChatMessage("/project", "claude", sid, "assistant", "Hi there", nil, false, "NewSession")
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 2)

	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "Hello", msgs[0].Content)
	assert.Equal(t, sid, msgs[0].SessionID)
	assert.Equal(t, "claude", msgs[0].Backend)
	assert.False(t, msgs[0].Streaming)

	assert.Equal(t, "assistant", msgs[1].Role)
	assert.Equal(t, "Hi there", msgs[1].Content)
}

func TestGetChatHistory_Empty(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Empty")

	msgs, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Empty(t, msgs)
}

func TestAddChatMessage_AutoTitle(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New Session")

	// First user message should auto-title the session
	_, err := service.AddChatMessage("/project", "claude", sid, "user", "This is my question about Go testing", nil, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "This is my question about Go testing", title)
}

func TestAddChatMessage_AutoTitleTruncated(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New Session")

	longContent := strings.Repeat("啊", 60) // 60 runes, each is a multi-byte character
	_, err := service.AddChatMessage("/project", "claude", sid, "user", longContent, nil, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, 53, utf8.RuneCountInString(title)) // 50 runes + "..."
	runes := []rune(title)
	assert.Equal(t, "啊", string(runes[49]))    // 50th rune is still content
	assert.Equal(t, "...", string(runes[50:])) // followed by ellipsis
}

func TestAddChatMessage_AutoTitleOnlyFirstUserMessage(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New Session")

	_, err := service.AddChatMessage("/project", "claude", sid, "user", "First message", nil, false, "NewSession")
	assert.NoError(t, err)

	_, err = service.AddChatMessage("/project", "claude", sid, "user", "Second message", nil, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "First message", title) // Title unchanged after second message
}

func TestAddChatMessage_AutoTitleEmptyContentWithFiles(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New Session")

	_, err := service.AddChatMessage("/project", "claude", sid, "user", "", []string{"file1.txt"}, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "NewSession", title)
}

func TestAddChatMessage_AutoTitleEmptyContentNoFiles(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New Session")

	_, err := service.AddChatMessage("/project", "claude", sid, "user", "", nil, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "NewSession", title)
}

func TestAddChatMessage_WithFiles(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "File Test")

	files := []string{"/path/file", "image.png", "doc.pdf"}
	_, err := service.AddChatMessage("/project", "claude", sid, "user", "Check these", files, false, "NewSession")
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, files, msgs[0].Files)
	assert.Equal(t, "/path/file", msgs[0].Files[0])
}

func TestAddChatMessage_Streaming(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Stream Test")

	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", "partial...", nil, true, "")
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.True(t, msgs[0].Streaming)
}

func TestAddChatMessage_AssistantDoesNotAutoTitle(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Original Title")

	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", "AI response", nil, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "Original Title", title) // Assistant messages don't change title
}

// ---------- CreateSession ----------

func TestCreateSession_UUIDFormat(t *testing.T) {
	setupDB(t)

	id, err := service.CreateSession("/project", "claude", "Test", "", "", "default", "chat")
	assert.NoError(t, err)
	assert.NotEmpty(t, id)

	// UUID v4 format: 8-4-4-4-12 hex digits separated by dashes
	parts := strings.Split(id, "-")
	assert.Len(t, parts, 5, "UUID should have 5 parts separated by dashes")
	assert.Equal(t, 8, len(parts[0]))
	assert.Equal(t, 4, len(parts[1]))
	assert.Equal(t, 4, len(parts[2]))
	assert.Equal(t, 4, len(parts[3]))
	assert.Equal(t, 12, len(parts[4]))
}

func TestCreateSession_UniqueIDs(t *testing.T) {
	setupDB(t)

	id1, err := service.CreateSession("/project", "claude", "Session 1", "", "", "default", "chat")
	assert.NoError(t, err)
	id2, err := service.CreateSession("/project", "claude", "Session 2", "", "", "default", "chat")
	assert.NoError(t, err)
	assert.NotEqual(t, id1, id2)
}

// ---------- DeleteSession (soft delete) ----------

func TestDeleteSession(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "To Delete")
	_, err := service.AddChatMessage("/project", "claude", sid, "user", "msg", nil, false, "NewSession")
	assert.NoError(t, err)

	err = service.DeleteSession("/project", "claude", sid)
	assert.NoError(t, err)

	// Session should be invisible via user-facing APIs
	_, err = service.GetSessionTitle(sid)
	assert.Error(t, err) // deleted sessions filtered by deleted=0

	// Messages are still physically present (no message-level soft-delete,
	// session-level soft-delete controls visibility)
	msgs, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 1) // messages still exist in DB

	// But the session is soft-deleted
	var deleted int
	err = service.DB.QueryRow("SELECT deleted FROM chat_sessions WHERE id = ?", sid).Scan(&deleted)
	assert.NoError(t, err)
	assert.Equal(t, 1, deleted)

	// updated_at should have been set to the deletion timestamp
	var updatedAt string
	err = service.DB.QueryRow("SELECT updated_at FROM chat_sessions WHERE id = ?", sid).Scan(&updatedAt)
	assert.NoError(t, err)
	assert.NotEmpty(t, updatedAt)
}

func TestDeleteSession_RejectsNewMessages(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "To Delete")

	err := service.DeleteSession("/project", "claude", sid)
	assert.NoError(t, err)

	// Adding messages to a deleted session should fail
	_, err = service.AddChatMessage("/project", "claude", sid, "user", "after delete", nil, false, "NewSession")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "deleted session")
}

func TestDeleteSession_GetSessionBackendHidden(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "codebuddy", "Backend Test")

	err := service.DeleteSession("/project", "codebuddy", sid)
	assert.NoError(t, err)

	// GetSessionBackend should return empty for deleted sessions
	backend := service.GetSessionBackend(sid)
	assert.Equal(t, "", backend)
}

func TestDeleteSession_GetSessionAgentIDHidden(t *testing.T) {
	setupDB(t)

	sid, err := service.CreateSession("/project", "claude", "Agent Test", "my-agent", "gpt-4", "user", "chat")
	assert.NoError(t, err)

	err = service.DeleteSession("/project", "claude", sid)
	assert.NoError(t, err)

	// GetSessionAgentID should return empty for deleted sessions
	assert.Equal(t, "", service.GetSessionAgentID(sid))
}

func TestDeleteSession_DoesNotAffectOtherSessions(t *testing.T) {
	setupDB(t)

	sid1 := helperCreateSession(t, "/project", "claude", "Session 1")
	sid2 := helperCreateSession(t, "/project", "claude", "Session 2")

	_, _ = service.AddChatMessage("/project", "claude", sid1, "user", "msg1", nil, false, "NewSession")
	_, _ = service.AddChatMessage("/project", "claude", sid2, "user", "msg2", nil, false, "NewSession")

	err := service.DeleteSession("/project", "claude", sid1)
	assert.NoError(t, err)

	// sid2 should still be fully functional
	title, err := service.GetSessionTitle(sid2)
	assert.NoError(t, err)
	assert.Equal(t, "msg2", title) // auto-titled from first message

	msgs, err := service.GetChatHistory("/project", "claude", sid2)
	assert.NoError(t, err)
	assert.Len(t, msgs, 1)
}

func TestDeleteSession_SessionCountExcludesDeleted(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "To Delete")

	countBefore, err := service.GetSessionCount("/project")
	assert.NoError(t, err)
	assert.Equal(t, 1, countBefore)

	err = service.DeleteSession("/project", "claude", sid)
	assert.NoError(t, err)

	countAfter, err := service.GetSessionCount("/project")
	assert.NoError(t, err)
	assert.Equal(t, 0, countAfter)
}

func TestDeleteSession_GetMessagesBySessionIDStillReturnsData(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "RAG Test")
	_, _ = service.AddChatMessage("/project", "claude", sid, "user", "hello", nil, false, "NewSession")

	err := service.DeleteSession("/project", "claude", sid)
	assert.NoError(t, err)

	// RAG API (GetMessagesBySessionID) should still return deleted messages
	msgs, err := service.GetMessagesBySessionID(sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "hello", msgs[0].Content)
}

func TestDeleteSession_GetMessageByIDStillReturnsData(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "RAG Test")
	msgID, err := service.AddChatMessage("/project", "claude", sid, "user", "hello", nil, false, "NewSession")
	assert.NoError(t, err)

	err = service.DeleteSession("/project", "claude", sid)
	assert.NoError(t, err)

	// RAG API (GetMessageByID) should still return deleted messages
	msg, err := service.GetMessageByID(msgID)
	assert.NoError(t, err)
	assert.Equal(t, "hello", msg.Content)
}

func TestDeleteSession_DeletedSessionNotInGetSessions(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/project", "claude", "Active")
	deletedSID := helperCreateSession(t, "/project", "claude", "To Delete")

	err := service.DeleteSession("/project", "claude", deletedSID)
	assert.NoError(t, err)

	sessions, err := service.GetSessions("/project", "claude")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.NotEqual(t, deletedSID, sessions[0].ID)
}

// ---------- GetSessions ----------

func TestGetSessions_FiltersByProjectAndBackend(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/proj1", "claude", "C1")
	helperCreateSession(t, "/proj1", "codebuddy", "CB1")
	helperCreateSession(t, "/proj2", "claude", "C2")

	sessions, err := service.GetSessions("/proj1", "claude")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, "claude", sessions[0].Backend)

	sessions, err = service.GetSessions("/proj1", "codebuddy")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, "codebuddy", sessions[0].Backend)

	sessions, err = service.GetSessions("/proj2", "claude")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
}

func TestGetSessions_AllBackends(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/proj", "claude", "C1")
	helperCreateSession(t, "/proj", "codebuddy", "CB1")
	helperCreateSession(t, "/other", "claude", "C2")

	sessions, err := service.GetSessions("/proj", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 2)

	// Should NOT include /other
	for _, s := range sessions {
		// Can't directly check project_path since it's not in ChatSession,
		// but we know we created 2 sessions for /proj
		assert.Contains(t, []string{"claude", "codebuddy"}, s.Backend)
	}
}

// ---------- GetSessionBackend ----------

func TestGetSessionBackend(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "codebuddy", "Test")
	backend := service.GetSessionBackend(sid)
	assert.Equal(t, "codebuddy", backend)
}

func TestGetSessionBackend_NonExistent(t *testing.T) {
	setupDB(t)

	backend := service.GetSessionBackend("non-existent-id")
	assert.Equal(t, "", backend)
}

// ---------- Session Running ----------

func TestIsSessionRunning_DefaultFalse(t *testing.T) {
	setupDB(t)

	assert.False(t, service.IsSessionRunning("any-id"))
}

func TestSetSessionRunning(t *testing.T) {
	setupDB(t)

	service.SetSessionRunning("sess-1", true)
	assert.True(t, service.IsSessionRunning("sess-1"))

	service.SetSessionRunning("sess-1", false)
	assert.False(t, service.IsSessionRunning("sess-1"))
}

func TestTrySetSessionRunning(t *testing.T) {
	setupDB(t)

	// First try should succeed
	ok := service.TrySetSessionRunning("sess-2")
	assert.True(t, ok)
	assert.True(t, service.IsSessionRunning("sess-2"))

	// Second try should fail (already running)
	ok = service.TrySetSessionRunning("sess-2")
	assert.False(t, ok)

	// Clean up
	service.SetSessionRunning("sess-2", false)
}

// ---------- SessionHasAssistant ----------

func TestSessionHasAssistant(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Test")

	// No assistant messages yet
	assert.False(t, service.SessionHasAssistant(sid))

	// Add a streaming assistant message - should NOT count
	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", "partial", nil, true, "")
	assert.NoError(t, err)
	assert.False(t, service.SessionHasAssistant(sid))

	// Add a finalized assistant message - should count
	_, err = service.AddChatMessage("/project", "claude", sid, "assistant", "final", nil, false, "NewSession")
	assert.NoError(t, err)
	assert.True(t, service.SessionHasAssistant(sid))
}

// ---------- UpdateStreamingMessage / FinalizeStreamingMessage ----------

func TestUpdateStreamingMessage(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Stream")

	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", "initial", nil, true, "")
	assert.NoError(t, err)

	err = service.UpdateStreamingMessage("/project", "claude", sid, "updated content")
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "updated content", msgs[0].Content)
	assert.True(t, msgs[0].Streaming) // Still streaming
}

func TestFinalizeStreamingMessage(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Stream")

	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", "streaming...", nil, true, "")
	assert.NoError(t, err)

	err = service.FinalizeStreamingMessage("/project", "claude", sid, "final content")
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "final content", msgs[0].Content)
	assert.False(t, msgs[0].Streaming) // No longer streaming
}

func TestUpdateStreamingMessage_NoStreamingRow(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "NoStream")

	// No streaming message exists; update should succeed but affect 0 rows
	err := service.UpdateStreamingMessage("/project", "claude", sid, "content")
	assert.NoError(t, err)
}

// ---------- CancelSession ----------

func TestCancelSession_NoCancelFunc(t *testing.T) {
	setupDB(t)

	ok := service.CancelSession("non-existent-session")
	// Non-running session with no cancel func is considered already cancelled (idempotent)
	assert.True(t, ok)
}

func TestCancelSession_WithCancelFunc(t *testing.T) {
	setupDB(t)

	sid := "cancel-test-session"
	cancelled := false
	ctx, cancel := context.WithCancel(context.Background())

	service.RegisterSessionCancel(sid, cancel)
	service.SetSessionRunning(sid, true)

	// Cancel the session
	ok := service.CancelSession(sid)
	assert.True(t, ok)

	// Context should be cancelled
	<-ctx.Done()
	cancelled = true
	assert.True(t, cancelled)

	// Session should no longer be running
	assert.False(t, service.IsSessionRunning(sid))

	// Second cancel should return true (idempotent: session is no longer running)
	ok = service.CancelSession(sid)
	assert.True(t, ok)
}

func TestCancelSession_WithStreamChannel(t *testing.T) {
	setupDB(t)

	sid := "cancel-stream-test"
	ctx, cancel := context.WithCancel(context.Background())

	// Register both cancel and stream
	service.RegisterSessionCancel(sid, cancel)
	ch := service.RegisterSessionStream(sid)
	service.SetSessionRunning(sid, true)

	// Cancel in goroutine that also reads the stream
	done := make(chan struct{})
	go func() {
		event := <-ch
		assert.Equal(t, "cancelled", event.Type)
		close(done)
	}()

	ok := service.CancelSession(sid)
	assert.True(t, ok)

	<-done
	<-ctx.Done()

	// Clean up stream
	service.UnregisterSessionStream(sid)
}

// ---------- GetSessionTitle ----------

func TestGetSessionTitle(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "My Title")

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "My Title", title)
}

func TestGetSessionTitle_NonExistent(t *testing.T) {
	setupDB(t)

	_, err := service.GetSessionTitle("non-existent")
	assert.Error(t, err)
}

// ---------- Edge cases ----------

func TestAddChatMessage_MultipleSessionsIsolated(t *testing.T) {
	setupDB(t)

	sid1 := helperCreateSession(t, "/project", "claude", "Session 1")
	sid2 := helperCreateSession(t, "/project", "claude", "Session 2")

	_, _ = service.AddChatMessage("/project", "claude", sid1, "user", "Msg in session 1", nil, false, "NewSession")
	_, _ = service.AddChatMessage("/project", "claude", sid2, "user", "Msg in session 2", nil, false, "NewSession")

	msgs1, err := service.GetChatHistory("/project", "claude", sid1)
	assert.NoError(t, err)
	assert.Len(t, msgs1, 1)
	assert.Equal(t, "Msg in session 1", msgs1[0].Content)

	msgs2, err := service.GetChatHistory("/project", "claude", sid2)
	assert.NoError(t, err)
	assert.Len(t, msgs2, 1)
	assert.Equal(t, "Msg in session 2", msgs2[0].Content)
}

func TestAddChatMessage_AutoTitleExactly50Runes(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New")

	// Exactly 50 runes - should NOT be truncated
	content := strings.Repeat("x", 50)
	_, err := service.AddChatMessage("/project", "claude", sid, "user", content, nil, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, content, title) // No "..." appended
}

func TestAddChatMessage_AutoTitle51Runes(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New")

	// 51 runes - should be truncated to 50 + "..."
	content := strings.Repeat("x", 51)
	_, err := service.AddChatMessage("/project", "claude", sid, "user", content, nil, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, strings.Repeat("x", 50)+"...", title)
}

func TestAddChatMessage_WithFilePath(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "FP Test")

	_, err := service.AddChatMessage("/project", "claude", sid, "user", "look at this", []string{"/src/main.go"}, false, "NewSession")
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, []string{"/src/main.go"}, msgs[0].Files)
}

func TestGetSessions_OrderedByUpdatedDesc(t *testing.T) {
	setupDB(t)

	sid1 := helperCreateSession(t, "/project", "claude", "First")
	sid2 := helperCreateSession(t, "/project", "claude", "Second")

	// Set explicit timestamps to guarantee ordering (SQLite time precision is seconds,
	// AddChatMessage may land in the same second as creation making order nondeterministic)
	_, err := service.DB.Exec("UPDATE chat_sessions SET updated_at = datetime('now', '-60 seconds') WHERE id = ?", sid1)
	assert.NoError(t, err)
	_, err = service.DB.Exec("UPDATE chat_sessions SET updated_at = datetime('now') WHERE id = ?", sid2)
	assert.NoError(t, err)

	sessions, err := service.GetSessions("/project", "claude")
	assert.NoError(t, err)
	assert.Len(t, sessions, 2)

	// sid2 should be first since it was updated most recently
	assert.Equal(t, sid2, sessions[0].ID)
	assert.Equal(t, sid1, sessions[1].ID)
}

func TestDeleteSession_NonExistentDoesNotError(t *testing.T) {
	setupDB(t)

	// Deleting a non-existent session should not return an error
	// (DELETE on non-existent rows is a no-op)
	err := service.DeleteSession("/project", "claude", "non-existent-id")
	assert.NoError(t, err)
}

func TestRegisterUnregisterSessionCancel(t *testing.T) {
	setupDB(t)

	sid := "cancel-reg-test"
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	service.RegisterSessionCancel(sid, cancel)

	// Should be able to cancel
	ok := service.CancelSession(sid)
	assert.True(t, ok)

	// After cancel, register a new one
	_, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	service.RegisterSessionCancel(sid, cancel2)
	service.UnregisterSessionCancel(sid)

	// After unregister, cancel returns true (session not running → idempotent)
	ok = service.CancelSession(sid)
	assert.True(t, ok)
}

func TestFinalizeStreamingMessage_NoStreamingRow(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "NoStream")

	// No streaming message; finalize should succeed but affect 0 rows
	err := service.FinalizeStreamingMessage("/project", "claude", sid, "content")
	assert.NoError(t, err)
}

func TestAddChatMessage_AutoTitleWithFilePathAndContent(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "New")

	// When content is non-empty, title comes from content (not files)
	_, err := service.AddChatMessage("/project", "claude", sid, "user", "Hello world", []string{"/some/file.go", "file.go"}, false, "NewSession")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "Hello world", title)
}

func TestGetChatHistory_DifferentBackendsIsolated(t *testing.T) {
	setupDB(t)

	sidC := helperCreateSession(t, "/project", "claude", "Claude")
	sidCB := helperCreateSession(t, "/project", "codebuddy", "CodeBuddy")

	_, _ = service.AddChatMessage("/project", "claude", sidC, "user", "claude msg", nil, false, "NewSession")
	_, _ = service.AddChatMessage("/project", "codebuddy", sidCB, "user", "codebuddy msg", nil, false, "NewSession")

	msgsC, err := service.GetChatHistory("/project", "claude", sidC)
	assert.NoError(t, err)
	assert.Len(t, msgsC, 1)
	assert.Equal(t, "claude msg", msgsC[0].Content)

	msgsCB, err := service.GetChatHistory("/project", "codebuddy", sidCB)
	assert.NoError(t, err)
	assert.Len(t, msgsCB, 1)
	assert.Equal(t, "codebuddy msg", msgsCB[0].Content)
}

// TestGetChatHistory_SameSecondOrdering verifies that messages inserted in the
// same second (identical created_at due to CURRENT_TIMESTAMP second precision)
// are returned in insertion order (id ASC) rather than non-deterministic order.
// This is the root cause of the bug where assistant output appeared above user input.
func TestGetChatHistory_SameSecondOrdering(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Same Second")

	// Insert user then assistant within the same second — no sleep
	userID, err := service.AddChatMessage("/project", "claude", sid, "user", "我自己定 AI-Driven Engineering", nil, false, "")
	assert.NoError(t, err)

	assistantID, err := service.AddChatMessage("/project", "claude", sid, "assistant", "很好！AI-Driven Engineering — 精准", nil, false, "")
	assert.NoError(t, err)

	// assistantID > userID since AUTOINCREMENT guarantees this
	assert.Greater(t, assistantID, userID, "assistant message should have higher ID than user message")

	// Verify chronological order: user first, assistant second
	msgs, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 2)
	assert.Equal(t, "user", msgs[0].Role, "user message must come first")
	assert.Equal(t, "assistant", msgs[1].Role, "assistant message must come second")
}

func TestGetMessageIDBeforeTime(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "BeforeTime")

	// Insert messages with known timestamps
	service.DB.Exec("INSERT INTO chat_history (project_path, backend, session_id, role, content, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		"/project", "claude", sid, "user", "msg1", "2025-01-01 10:00:00")
	service.DB.Exec("INSERT INTO chat_history (project_path, backend, session_id, role, content, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		"/project", "claude", sid, "assistant", "msg2", "2025-01-01 10:00:01")
	service.DB.Exec("INSERT INTO chat_history (project_path, backend, session_id, role, content, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		"/project", "claude", sid, "user", "msg3", "2025-01-01 10:00:02")

	// Query for messages before 10:00:02 — should return max ID of messages before that time
	id, err := service.GetMessageIDBeforeTime("/project", "claude", sid, "2025-01-01 10:00:02")
	assert.NoError(t, err)
	assert.Greater(t, id, 0, "should find a message ID before the given time")

	// Query with a time before all messages — should return 0
	id, err = service.GetMessageIDBeforeTime("/project", "claude", sid, "2025-01-01 09:00:00")
	assert.NoError(t, err)
	assert.Equal(t, 0, id, "should return 0 when no messages before the given time")
}

// Ensure TestMain-like global DB save/restore works correctly
func TestGlobalDBPreservedAcrossParallelTests(t *testing.T) {
	originalDB := service.DB
	setupDB(t)
	// Within this test, service.DB is our in-memory DB
	assert.NotNil(t, service.DB)
	assert.NotEqual(t, originalDB, service.DB) // if originalDB was nil or different
}

func TestAddChatMessage_StreamingFalse(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "No Stream")

	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", "final response", nil, false, "NewSession")
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.False(t, msgs[0].Streaming)
}

func TestUpdateThenFinalizeStreaming(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Full Stream")

	// Start streaming
	_, _ = service.AddChatMessage("/project", "claude", sid, "assistant", "start", nil, true, "")

	// Update content multiple times
	service.UpdateStreamingMessage("/project", "claude", sid, "start + more")
	service.UpdateStreamingMessage("/project", "claude", sid, "start + more + final")

	// Verify still streaming
	msgs, _ := service.GetChatHistory("/project", "claude", sid)
	assert.True(t, msgs[0].Streaming)
	assert.Equal(t, "start + more + final", msgs[0].Content)

	// Finalize
	service.FinalizeStreamingMessage("/project", "claude", sid, "complete response")

	msgs, _ = service.GetChatHistory("/project", "claude", sid)
	assert.False(t, msgs[0].Streaming)
	assert.Equal(t, "complete response", msgs[0].Content)
}

func TestCancelSession_CleansUpRunningState(t *testing.T) {
	setupDB(t)

	sid := fmt.Sprintf("cleanup-test-%d", len("x"))
	_, cancel := context.WithCancel(context.Background())

	service.RegisterSessionCancel(sid, cancel)
	service.SetSessionRunning(sid, true)
	assert.True(t, service.IsSessionRunning(sid))

	service.CancelSession(sid)
	assert.False(t, service.IsSessionRunning(sid))
}

// ---------- GetChatMessageCount ----------

func TestGetChatMessageCount(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Test")
	// Initially 0
	assert.Equal(t, 0, service.GetChatMessageCount(sid))
	// Add messages
	service.AddChatMessage("/project", "claude", sid, "user", "Hello", nil, false, "NewSession")
	service.AddChatMessage("/project", "claude", sid, "assistant", "Hi", nil, false, "NewSession")
	assert.Equal(t, 2, service.GetChatMessageCount(sid))
}

func TestGetChatMessageCount_NonExistent(t *testing.T) {
	setupDB(t)
	assert.Equal(t, 0, service.GetChatMessageCount("non-existent"))
}

// ---------- UpdateLastRead ----------

func TestUpdateLastRead(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Test")
	// Should not panic and should succeed
	service.UpdateLastRead(sid)
	// Verify by checking sessions - last_read_at should be set
	var lastRead sql.NullTime
	err := service.DB.QueryRow("SELECT last_read_at FROM chat_sessions WHERE id = ?", sid).Scan(&lastRead)
	assert.NoError(t, err)
	assert.True(t, lastRead.Valid)
}

// ---------- GetSessionAgentID ----------

func TestGetSessionAgentID(t *testing.T) {
	setupDB(t)
	// Create session with agent ID
	sid, err := service.CreateSession("/project", "claude", "Test", "my-agent", "gpt-4", "user", "chat")
	assert.NoError(t, err)
	assert.Equal(t, "my-agent", service.GetSessionAgentID(sid))
}

func TestGetSessionAgentID_NonExistent(t *testing.T) {
	setupDB(t)
	assert.Equal(t, "", service.GetSessionAgentID("non-existent"))
}

func TestGetSessionAgentID_EmptyAgent(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "No Agent")
	assert.Equal(t, "", service.GetSessionAgentID(sid))
}

// ---------- GetAndClearCancelReason ----------

func TestGetAndClearCancelReason_NoReason(t *testing.T) {
	setupDB(t)
	assert.Equal(t, "", service.GetAndClearCancelReason("non-existent"))
}

func TestGetAndClearCancelReason_WithReason(t *testing.T) {
	setupDB(t)
	sid := "test-reason-session"
	// Simulate setting cancel reason (as CancelSession does)
	service.RegisterSessionCancel(sid, func() {})
	service.SetSessionRunning(sid, true)
	// Cancel sets "user" reason
	service.CancelSession(sid)
	// Should return "user"
	assert.Equal(t, "user", service.GetAndClearCancelReason(sid))
	// Second call should return "" (cleared)
	assert.Equal(t, "", service.GetAndClearCancelReason(sid))
}

// ---------- ForceCancelSession ----------

func TestForceCancelSession(t *testing.T) {
	setupDB(t)
	sid := "force-cancel-test"
	ctx, cancel := context.WithCancel(context.Background())
	service.RegisterSessionCancel(sid, cancel)
	service.SetSessionRunning(sid, true)

	service.ForceCancelSession(sid)

	// Context should be cancelled
	<-ctx.Done()
	// Reason should be "disconnect"
	assert.Equal(t, "disconnect", service.GetAndClearCancelReason(sid))
}

func TestForceCancelSession_NoCancelFunc(t *testing.T) {
	setupDB(t)
	// Should not panic when no cancel func exists
	service.ForceCancelSession("non-existent")
}

// ---------- SendSessionEvent ----------

func TestSendSessionEvent(t *testing.T) {
	setupDB(t)
	sid := "send-event-test"
	ch := service.RegisterSessionStream(sid)

	// Send event
	ok := service.SendSessionEvent(sid, ai.StreamEvent{Type: "content", Content: "hello"})
	assert.True(t, ok)

	// Receive event
	event := <-ch
	assert.Equal(t, "content", event.Type)
	assert.Equal(t, "hello", event.Content)

	service.UnregisterSessionStream(sid)
}

func TestSendSessionEvent_NoStream(t *testing.T) {
	setupDB(t)
	ok := service.SendSessionEvent("non-existent", ai.StreamEvent{Type: "content"})
	assert.False(t, ok)
}

func TestSendSessionEvent_FullChannel(t *testing.T) {
	setupDB(t)
	sid := "full-channel-test"
	ch := service.RegisterSessionStream(sid)

	// Fill the channel buffer (capacity is 256)
	for i := range 256 {
		ok := service.SendSessionEvent(sid, ai.StreamEvent{Type: "content", Content: fmt.Sprintf("msg-%d", i)})
		assert.True(t, ok)
	}

	// Next send should fail (non-blocking)
	ok := service.SendSessionEvent(sid, ai.StreamEvent{Type: "content", Content: "overflow"})
	assert.False(t, ok)

	// Drain the channel to clean up
	for range 256 {
		<-ch
	}
	service.UnregisterSessionStream(sid)
}

// ---------- UpdateMessageContent ----------

func TestUpdateMessageContent(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Test")

	msgID, err := service.AddChatMessage("/project", "claude", sid, "user", "original", nil, false, "NewSession")
	assert.NoError(t, err)

	err = service.UpdateMessageContent(int(msgID), "updated content")
	assert.NoError(t, err)

	msgs, _ := service.GetChatHistory("/project", "claude", sid)
	assert.Equal(t, "updated content", msgs[0].Content)
}

func TestUpdateMessageContent_NonExistent(t *testing.T) {
	setupDB(t)
	// Updating non-existent message should not error (UPDATE affects 0 rows)
	err := service.UpdateMessageContent(99999, "content")
	assert.NoError(t, err)
}

// ---------- UpdateExternalSessionID / GetExternalSessionID ----------

func TestUpdateAndGetExternalSessionID(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "opencode", "Test")

	// Initially set to ClawBench UUID (id) by CreateSession
	assert.Equal(t, sid, service.GetExternalSessionID(sid))

	// Set external ID (overwrites the default ClawBench UUID)
	err := service.UpdateExternalSessionID(sid, "ext-session-123")
	assert.NoError(t, err)

	// Get external ID
	assert.Equal(t, "ext-session-123", service.GetExternalSessionID(sid))
}

func TestGetExternalSessionID_NonExistent(t *testing.T) {
	setupDB(t)
	assert.Equal(t, "", service.GetExternalSessionID("non-existent"))
}

// ---------- GetExpiredDeletedSessions ----------

func TestGetExpiredDeletedSessions_NoExpired(t *testing.T) {
	setupDB(t)

	// Active session — should not appear
	sid := helperCreateSession(t, "/project", "claude", "Active")
	_, _ = service.AddChatMessage("/project", "claude", sid, "user", "msg", nil, false, "NewSession")

	// Recently deleted session — within retention period
	sid2 := helperCreateSession(t, "/project", "claude", "Recently Deleted")
	_ = service.DeleteSession("/project", "claude", sid2)

	cutoff := time.Now().AddDate(0, 0, -90) // 90 days ago
	ids, err := service.GetExpiredDeletedSessions(cutoff)
	assert.NoError(t, err)
	assert.Empty(t, ids)
}

func TestGetExpiredDeletedSessions_WithExpired(t *testing.T) {
	setupDB(t)

	// Create and delete a session, then manually set its updated_at to 100 days ago
	sid := helperCreateSession(t, "/project", "claude", "Old Deleted")
	_ = service.DeleteSession("/project", "claude", sid)

	_, err := service.DB.Exec("UPDATE chat_sessions SET updated_at = datetime('now', '-100 days') WHERE id = ?", sid)
	assert.NoError(t, err)

	cutoff := time.Now().AddDate(0, 0, -90)
	ids, err := service.GetExpiredDeletedSessions(cutoff)
	assert.NoError(t, err)
	assert.Contains(t, ids, sid)
}

func TestGetExpiredDeletedSessions_ActiveSessionsNotIncluded(t *testing.T) {
	setupDB(t)

	// Create an active session with old updated_at
	sid := helperCreateSession(t, "/project", "claude", "Old Active")
	_, _ = service.DB.Exec("UPDATE chat_sessions SET updated_at = datetime('now', '-100 days') WHERE id = ?", sid)

	cutoff := time.Now().AddDate(0, 0, -90)
	ids, err := service.GetExpiredDeletedSessions(cutoff)
	assert.NoError(t, err)
	assert.NotContains(t, ids, sid)
}

func TestGetExpiredDeletedSessions_MultipleExpired(t *testing.T) {
	setupDB(t)

	// Create multiple expired sessions
	expectedIDs := make([]string, 0, 3)
	for i := range 3 {
		sid := helperCreateSession(t, "/project", "claude", fmt.Sprintf("Old %d", i))
		_ = service.DeleteSession("/project", "claude", sid)
		_, _ = service.DB.Exec("UPDATE chat_sessions SET updated_at = datetime('now', '-100 days') WHERE id = ?", sid)
		expectedIDs = append(expectedIDs, sid)
	}

	// Create a recently deleted session that should NOT appear
	recentSID := helperCreateSession(t, "/project", "claude", "Recent")
	_ = service.DeleteSession("/project", "claude", recentSID)

	cutoff := time.Now().AddDate(0, 0, -90)
	ids, err := service.GetExpiredDeletedSessions(cutoff)
	assert.NoError(t, err)
	assert.Len(t, ids, 3)
	for _, id := range expectedIDs {
		assert.Contains(t, ids, id)
	}
	assert.NotContains(t, ids, recentSID)
}

// ---------- PurgeDeletedData ----------

func TestPurgeDeletedData_EmptyList(t *testing.T) {
	setupDB(t)

	sessions, messages, err := service.PurgeDeletedData(nil)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), sessions)
	assert.Equal(t, int64(0), messages)
}

func TestPurgeDeletedData_HardDeletesSessions(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "To Purge")
	_, _ = service.AddChatMessage("/project", "claude", sid, "user", "msg1", nil, false, "NewSession")
	_, _ = service.AddChatMessage("/project", "claude", sid, "assistant", "reply1", nil, false, "NewSession")
	_ = service.DeleteSession("/project", "claude", sid)

	// Add a raw response for this session
	_, _ = service.DB.Exec("INSERT INTO ai_raw_responses (session_id, message_id, backend, raw_output) VALUES (?, 1, 'claude', 'raw')", sid)

	sessionsPurged, messagesPurged, err := service.PurgeDeletedData([]string{sid})
	assert.NoError(t, err)
	assert.Equal(t, int64(1), sessionsPurged)
	assert.Equal(t, int64(2), messagesPurged)

	// Verify session is completely gone from DB
	var count int
	err = service.DB.QueryRow("SELECT COUNT(*) FROM chat_sessions WHERE id = ?", sid).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)

	// Verify messages are completely gone
	err = service.DB.QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sid).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)

	// Verify raw responses are gone
	err = service.DB.QueryRow("SELECT COUNT(*) FROM ai_raw_responses WHERE session_id = ?", sid).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestPurgeDeletedData_DoesNotPurgeActiveSession(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Active")
	_, _ = service.AddChatMessage("/project", "claude", sid, "user", "msg", nil, false, "NewSession")

	// Try to purge an active (non-deleted) session — should not delete it
	sessionsPurged, messagesPurged, err := service.PurgeDeletedData([]string{sid})
	assert.NoError(t, err)
	assert.Equal(t, int64(0), sessionsPurged) // WHERE deleted = 1 prevents purge
	assert.Equal(t, int64(1), messagesPurged) // messages are deleted regardless of deleted flag

	// Session should still exist (wasn't soft-deleted)
	title, err := service.GetSessionTitle(sid)
	assert.NoError(t, err)
	assert.Equal(t, "msg", title)
}

func TestPurgeDeletedData_MultipleSessions(t *testing.T) {
	setupDB(t)

	sid1 := helperCreateSession(t, "/project", "claude", "Purge 1")
	sid2 := helperCreateSession(t, "/project", "claude", "Purge 2")
	_, _ = service.AddChatMessage("/project", "claude", sid1, "user", "msg1", nil, false, "NewSession")
	_, _ = service.AddChatMessage("/project", "claude", sid2, "user", "msg2", nil, false, "NewSession")
	_ = service.DeleteSession("/project", "claude", sid1)
	_ = service.DeleteSession("/project", "claude", sid2)

	sessionsPurged, messagesPurged, err := service.PurgeDeletedData([]string{sid1, sid2})
	assert.NoError(t, err)
	assert.Equal(t, int64(2), sessionsPurged)
	assert.Equal(t, int64(2), messagesPurged)
}

func TestPurgeDeletedData_NonExistentSessionID(t *testing.T) {
	setupDB(t)

	sessionsPurged, messagesPurged, err := service.PurgeDeletedData([]string{"non-existent-id"})
	assert.NoError(t, err)
	assert.Equal(t, int64(0), sessionsPurged)
	assert.Equal(t, int64(0), messagesPurged)
}

// ---------- AddChatMessage guard against deleted session ----------

func TestAddChatMessage_RejectsDeletedSession(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "To Delete")
	_ = service.DeleteSession("/project", "claude", sid)

	_, err := service.AddChatMessage("/project", "claude", sid, "user", "after delete", nil, false, "NewSession")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "deleted session")
}

func TestAddChatMessage_NonExistentSessionStillWorks(t *testing.T) {
	setupDB(t)

	// Non-existent session doesn't have a deleted=1 row, so the guard doesn't block
	// (This is the existing behavior — message gets inserted with orphaned session_id)
	_, err := service.AddChatMessage("/project", "claude", "non-existent-session", "user", "orphan msg", nil, false, "NewSession")
	assert.NoError(t, err)
}

// ---------- SessionType (Task 2/3/4) ----------

func TestCreateSession_ScheduledType(t *testing.T) {
	setupDB(t)

	sid, err := service.CreateSession("/project", "claude", "Scheduled Session", "", "", "default", "scheduled")
	assert.NoError(t, err)
	assert.NotEmpty(t, sid)

	// Verify session_type is stored correctly in DB
	var sessionType string
	err = service.DB.QueryRow("SELECT session_type FROM chat_sessions WHERE id = ?", sid).Scan(&sessionType)
	assert.NoError(t, err)
	assert.Equal(t, "scheduled", sessionType)
}

func TestCreateSession_DefaultsToChatType(t *testing.T) {
	setupDB(t)

	sid, err := service.CreateSession("/project", "claude", "Chat Session", "", "", "default", "")
	assert.NoError(t, err)
	assert.NotEmpty(t, sid)

	// Verify session_type defaults to 'chat'
	var sessionType string
	err = service.DB.QueryRow("SELECT session_type FROM chat_sessions WHERE id = ?", sid).Scan(&sessionType)
	assert.NoError(t, err)
	assert.Equal(t, "chat", sessionType)
}

func TestGetSessions_FiltersBySessionType(t *testing.T) {
	setupDB(t)

	// Create a chat session and a scheduled session
	chatSID := helperCreateSession(t, "/project", "claude", "Chat Session")
	_ = chatSID
	schedSID := helperCreateScheduledSession(t, "/project", "claude", "Scheduled Session")
	_ = schedSID

	sessions, err := service.GetSessions("/project", "claude")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, "chat", sessions[0].SessionType)
	assert.Equal(t, "Chat Session", sessions[0].Title)
}

func TestGetSessionCount_ExcludesScheduledSessions(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/project", "claude", "Chat Session")
	helperCreateScheduledSession(t, "/project", "claude", "Scheduled Session")

	count, err := service.GetSessionCount("/project")
	assert.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestGetSessions_SessionTypeField(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Chat Session")
	_ = sid

	sessions, err := service.GetSessions("/project", "claude")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, "chat", sessions[0].SessionType)
}

func TestGetSessions_AllBackendsFiltersBySessionType(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/project", "claude", "Chat")
	helperCreateScheduledSession(t, "/project", "claude", "Scheduled")
	helperCreateSession(t, "/project", "codebuddy", "Chat CB")

	// Only chat sessions should appear
	sessions, err := service.GetSessions("/project", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 2)
	for _, s := range sessions {
		assert.Equal(t, "chat", s.SessionType)
	}
}

// ---------- GetSessionThinkingEffort / UpdateSessionThinkingEffort ----------

func TestGetSessionThinkingEffort_DefaultEmpty(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Thinking Test")
	// New session should have empty thinking effort (auto)
	assert.Equal(t, "", service.GetSessionThinkingEffort(sid))
}

func TestGetSessionThinkingEffort_NonExistent(t *testing.T) {
	setupDB(t)
	// Non-existent session should return empty string
	assert.Equal(t, "", service.GetSessionThinkingEffort("non-existent"))
}

func TestUpdateSessionThinkingEffort_Set(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Thinking Test")

	// Set thinking effort
	err := service.UpdateSessionThinkingEffort(sid, "high")
	assert.NoError(t, err)

	// Verify it was persisted
	assert.Equal(t, "high", service.GetSessionThinkingEffort(sid))
}

func TestUpdateSessionThinkingEffort_Update(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Thinking Test")

	// Set initial value
	err := service.UpdateSessionThinkingEffort(sid, "low")
	assert.NoError(t, err)
	assert.Equal(t, "low", service.GetSessionThinkingEffort(sid))

	// Update to different value
	err = service.UpdateSessionThinkingEffort(sid, "xhigh")
	assert.NoError(t, err)
	assert.Equal(t, "xhigh", service.GetSessionThinkingEffort(sid))
}

func TestUpdateSessionThinkingEffort_ResetToAuto(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Thinking Test")

	// Set thinking effort
	err := service.UpdateSessionThinkingEffort(sid, "medium")
	assert.NoError(t, err)
	assert.Equal(t, "medium", service.GetSessionThinkingEffort(sid))

	// Reset to auto (empty string)
	err = service.UpdateSessionThinkingEffort(sid, "")
	assert.NoError(t, err)
	assert.Equal(t, "", service.GetSessionThinkingEffort(sid))
}

func TestGetSessionThinkingEffort_DeletedSession(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Thinking Delete")
	err := service.UpdateSessionThinkingEffort(sid, "high")
	assert.NoError(t, err)

	// Delete session
	_ = service.DeleteSession("/project", "claude", sid)

	// Deleted session should return empty (query filters deleted=0)
	assert.Equal(t, "", service.GetSessionThinkingEffort(sid))
}

// ---------- GetSessionsPaged ----------

func TestGetSessionsPaged_NoLimit_ReturnsAll(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/project", "claude", "S1")
	helperCreateSession(t, "/project", "claude", "S2")
	helperCreateSession(t, "/project", "claude", "S3")

	sessions, hasMore, err := service.GetSessionsPaged("/project", "", 0, "", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 3)
	assert.False(t, hasMore)
}

func TestGetSessionsPaged_LimitGreaterThanTotal(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/project", "claude", "S1")
	helperCreateSession(t, "/project", "claude", "S2")

	sessions, hasMore, err := service.GetSessionsPaged("/project", "", 10, "", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 2)
	assert.False(t, hasMore)
}

func TestGetSessionsPaged_LimitEqualsTotal(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/project", "claude", "S1")
	helperCreateSession(t, "/project", "claude", "S2")
	helperCreateSession(t, "/project", "claude", "S3")

	sessions, hasMore, err := service.GetSessionsPaged("/project", "", 3, "", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 3)
	assert.False(t, hasMore) // limit+1=4, only 3 exist, so no more
}

func TestGetSessionsPaged_LimitLessThanTotal_HasMore(t *testing.T) {
	setupDB(t)

	for i := range 5 {
		helperCreateSession(t, "/project", "claude", fmt.Sprintf("S%d", i))
	}

	sessions, hasMore, err := service.GetSessionsPaged("/project", "", 3, "", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 3)
	assert.True(t, hasMore)
}

func TestGetSessionsPaged_CursorSecondPage(t *testing.T) {
	setupDB(t)

	// Create 5 sessions with staggered updated_at times
	for i := range 5 {
		sid := helperCreateSession(t, "/project", "claude", fmt.Sprintf("S%d", i))
		// Stagger updated_at so ordering is deterministic
		_, err := service.DB.Exec("UPDATE chat_sessions SET updated_at = datetime('now', ? || ' seconds') WHERE id = ?", fmt.Sprintf("-%d", (4-i)*60), sid)
		assert.NoError(t, err)
	}

	// First page: limit=2, no cursor
	sessions, hasMore, err := service.GetSessionsPaged("/project", "", 2, "", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 2)
	assert.True(t, hasMore)

	// Use last session as cursor
	lastSession := sessions[len(sessions)-1]
	cursor := lastSession.UpdatedAt.Format("2006-01-02 15:04:05")
	cursorID := lastSession.ID

	// Second page: cursor from last session of first page
	sessions2, hasMore2, err := service.GetSessionsPaged("/project", "", 2, cursor, cursorID)
	assert.NoError(t, err)
	assert.Len(t, sessions2, 2)
	assert.True(t, hasMore2)

	// Verify no overlap between page 1 and page 2
	page1IDs := make(map[string]bool)
	for _, s := range sessions {
		page1IDs[s.ID] = true
	}
	for _, s := range sessions2 {
		assert.False(t, page1IDs[s.ID], "session %s should not appear in both pages", s.ID)
	}
}

func TestGetSessionsPaged_CursorLastPage(t *testing.T) {
	setupDB(t)

	for i := range 5 {
		sid := helperCreateSession(t, "/project", "claude", fmt.Sprintf("S%d", i))
		_, err := service.DB.Exec("UPDATE chat_sessions SET updated_at = datetime('now', ? || ' seconds') WHERE id = ?", fmt.Sprintf("-%d", (4-i)*60), sid)
		assert.NoError(t, err)
	}

	// First page: limit=3
	sessions, hasMore, err := service.GetSessionsPaged("/project", "", 3, "", "")
	assert.NoError(t, err)
	assert.True(t, hasMore)

	// Second page: cursor from last session
	lastSession := sessions[len(sessions)-1]
	cursor := lastSession.UpdatedAt.Format("2006-01-02 15:04:05")
	cursorID := lastSession.ID

	sessions2, hasMore2, err := service.GetSessionsPaged("/project", "", 3, cursor, cursorID)
	assert.NoError(t, err)
	assert.Len(t, sessions2, 2) // only 2 remaining
	assert.False(t, hasMore2)
}

func TestGetSessionsPaged_EmptyProject(t *testing.T) {
	setupDB(t)

	sessions, hasMore, err := service.GetSessionsPaged("/project", "", 10, "", "")
	assert.NoError(t, err)
	assert.Empty(t, sessions)
	assert.False(t, hasMore)
}

func TestGetSessionsPaged_FiltersByProject(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/proj1", "claude", "P1-S1")
	helperCreateSession(t, "/proj1", "claude", "P1-S2")
	helperCreateSession(t, "/proj2", "claude", "P2-S1")

	sessions, hasMore, err := service.GetSessionsPaged("/proj1", "", 10, "", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 2)
	assert.False(t, hasMore)
}

func TestGetSessionsPaged_ExcludesDeletedSessions(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/project", "claude", "Active")
	deletedSID := helperCreateSession(t, "/project", "claude", "Deleted")
	err := service.DeleteSession("/project", "claude", deletedSID)
	assert.NoError(t, err)

	sessions, hasMore, err := service.GetSessionsPaged("/project", "", 10, "", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.False(t, hasMore)
	assert.Equal(t, "Active", sessions[0].Title)
}

func TestGetSessionsPaged_ExcludesScheduledSessions(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/project", "claude", "Chat")
	helperCreateScheduledSession(t, "/project", "claude", "Scheduled")

	sessions, _, err := service.GetSessionsPaged("/project", "", 10, "", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, "Chat", sessions[0].Title)
}

func TestGetSessionsPaged_OrderedByUpdatedDesc(t *testing.T) {
	setupDB(t)

	sid1 := helperCreateSession(t, "/project", "claude", "Old")
	sid2 := helperCreateSession(t, "/project", "claude", "New")

	// Set explicit timestamps to guarantee ordering (SQLite time precision is seconds)
	_, err := service.DB.Exec("UPDATE chat_sessions SET updated_at = datetime('now', '-60 seconds') WHERE id = ?", sid1)
	assert.NoError(t, err)
	_, err = service.DB.Exec("UPDATE chat_sessions SET updated_at = datetime('now') WHERE id = ?", sid2)
	assert.NoError(t, err)

	sessions, _, err := service.GetSessionsPaged("/project", "", 10, "", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 2)
	assert.Equal(t, sid2, sessions[0].ID) // most recently updated first
	assert.Equal(t, sid1, sessions[1].ID)
}

func TestGetSessionsPaged_AllPagesCoverAllSessions(t *testing.T) {
	setupDB(t)

	// Create 7 sessions with staggered times
	allIDs := make([]string, 0, 7)
	for i := range 7 {
		sid := helperCreateSession(t, "/project", "claude", fmt.Sprintf("S%d", i))
		allIDs = append(allIDs, sid)
		_, err := service.DB.Exec("UPDATE chat_sessions SET updated_at = datetime('now', ? || ' seconds') WHERE id = ?", fmt.Sprintf("-%d", (6-i)*60), sid)
		assert.NoError(t, err)
	}

	// Paginate through all sessions: limit=3
	var collectedIDs []string
	cursor := ""
	cursorID := ""
	limit := 3
	page := 0

	for {
		sessions, hasMore, err := service.GetSessionsPaged("/project", "", limit, cursor, cursorID)
		assert.NoError(t, err)
		assert.NotEmpty(t, sessions, "page %d should not be empty", page)

		for _, s := range sessions {
			collectedIDs = append(collectedIDs, s.ID)
		}

		if !hasMore {
			break
		}

		lastSession := sessions[len(sessions)-1]
		cursor = lastSession.UpdatedAt.Format("2006-01-02 15:04:05")
		cursorID = lastSession.ID
		page++

		if page > 10 {
			t.Fatal("too many pages, infinite loop?")
		}
	}

	// All sessions should be collected without duplicates
	assert.Len(t, collectedIDs, 7)
	uniqueIDs := make(map[string]bool)
	for _, id := range collectedIDs {
		assert.False(t, uniqueIDs[id], "duplicate session ID: %s", id)
		uniqueIDs[id] = true
	}
	// All original IDs should be present
	for _, id := range allIDs {
		assert.True(t, uniqueIDs[id], "missing session ID: %s", id)
	}
}

func TestGetSessionsPaged_SameTimestampTiebreaker(t *testing.T) {
	setupDB(t)

	// Create 3 sessions: 2 with same timestamp, 1 with a later timestamp
	// to test the (updated_at = cursor AND id < cursorID) tiebreaker
	sid1 := helperCreateSession(t, "/project", "claude", "Tie1")
	sid2 := helperCreateSession(t, "/project", "claude", "Tie2")
	sid3 := helperCreateSession(t, "/project", "claude", "Newer")

	// Set sid1 and sid2 to the same timestamp, sid3 slightly newer
	baseTime := "2026-01-15 12:00:00"
	_, err := service.DB.Exec("UPDATE chat_sessions SET updated_at = ? WHERE id = ?", baseTime, sid1)
	assert.NoError(t, err)
	_, err = service.DB.Exec("UPDATE chat_sessions SET updated_at = ? WHERE id = ?", baseTime, sid2)
	assert.NoError(t, err)
	_, err = service.DB.Exec("UPDATE chat_sessions SET updated_at = '2026-01-15 12:01:00' WHERE id = ?", sid3)
	assert.NoError(t, err)

	// First page: limit=2 — should get sid3 (newest) and one of sid1/sid2
	sessions, hasMore, err := service.GetSessionsPaged("/project", "", 2, "", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 2)
	assert.True(t, hasMore)

	// Second page: cursor from last session of page 1
	lastSession := sessions[len(sessions)-1]
	cursor := lastSession.UpdatedAt.Format("2006-01-02 15:04:05")
	cursorID := lastSession.ID

	sessions2, hasMore2, err := service.GetSessionsPaged("/project", "", 2, cursor, cursorID)
	assert.NoError(t, err)
	assert.Len(t, sessions2, 1) // only 1 remaining
	assert.False(t, hasMore2)

	// Verify no overlap
	page1IDs := make(map[string]bool)
	for _, s := range sessions {
		page1IDs[s.ID] = true
	}
	for _, s := range sessions2 {
		assert.False(t, page1IDs[s.ID], "session %s should not appear in both pages", s.ID)
	}
}

// ---------- GetSessionTitlesBatch ----------

func TestGetSessionTitlesBatch_Empty(t *testing.T) {
	setupDB(t)

	titles, err := service.GetSessionTitlesBatch(nil)
	assert.NoError(t, err)
	assert.Empty(t, titles)

	titles, err = service.GetSessionTitlesBatch([]string{})
	assert.NoError(t, err)
	assert.Empty(t, titles)
}

func TestGetSessionTitlesBatch_SingleSession(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "My Title")

	titles, err := service.GetSessionTitlesBatch([]string{sid})
	assert.NoError(t, err)
	assert.Equal(t, "My Title", titles[sid])
}

func TestGetSessionTitlesBatch_MultipleSessions(t *testing.T) {
	setupDB(t)

	sid1 := helperCreateSession(t, "/project", "claude", "Title 1")
	sid2 := helperCreateSession(t, "/project", "codebuddy", "Title 2")

	titles, err := service.GetSessionTitlesBatch([]string{sid1, sid2})
	assert.NoError(t, err)
	assert.Equal(t, "Title 1", titles[sid1])
	assert.Equal(t, "Title 2", titles[sid2])
}

func TestGetSessionTitlesBatch_ExcludesEmptyTitles(t *testing.T) {
	setupDB(t)

	// Create session with a title, then set it to empty
	sid := helperCreateSession(t, "/project", "claude", "Has Title")
	_, err := service.DB.Exec("UPDATE chat_sessions SET title = '' WHERE id = ?", sid)
	assert.NoError(t, err)

	titles, err := service.GetSessionTitlesBatch([]string{sid})
	assert.NoError(t, err)
	_, ok := titles[sid]
	assert.False(t, ok, "empty title should not be included")
}

func TestGetSessionTitlesBatch_ExcludesDeletedSessions(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "To Delete")
	_ = service.DeleteSession("/project", "claude", sid)

	titles, err := service.GetSessionTitlesBatch([]string{sid})
	assert.NoError(t, err)
	_, ok := titles[sid]
	assert.False(t, ok, "deleted session should not appear in batch titles")
}

func TestGetSessionTitlesBatch_NonExistentID(t *testing.T) {
	setupDB(t)

	titles, err := service.GetSessionTitlesBatch([]string{"non-existent-id"})
	assert.NoError(t, err)
	_, ok := titles["non-existent-id"]
	assert.False(t, ok, "non-existent ID should not appear in titles")
}

// ---------- GetSessionTitlesBatchIncludeDeleted ----------

func TestGetSessionTitlesBatchIncludeDeleted_IncludesDeletedSessions(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Deleted Session Title")
	_ = service.DeleteSession("/project", "claude", sid)

	// Regular batch excludes deleted sessions
	titles, err := service.GetSessionTitlesBatch([]string{sid})
	assert.NoError(t, err)
	_, ok := titles[sid]
	assert.False(t, ok, "GetSessionTitlesBatch should exclude deleted sessions")

	// IncludeDeleted variant includes deleted sessions
	titlesInc, err := service.GetSessionTitlesBatchIncludeDeleted([]string{sid})
	assert.NoError(t, err)
	title, ok := titlesInc[sid]
	assert.True(t, ok, "GetSessionTitlesBatchIncludeDeleted should include deleted sessions")
	assert.Equal(t, "Deleted Session Title", title)
}

func TestGetSessionTitlesBatchIncludeDeleted_ExcludesEmptyTitles(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Has Title")
	_, err := service.DB.Exec("UPDATE chat_sessions SET title = '' WHERE id = ?", sid)
	assert.NoError(t, err)

	titles, err := service.GetSessionTitlesBatchIncludeDeleted([]string{sid})
	assert.NoError(t, err)
	_, ok := titles[sid]
	assert.False(t, ok, "empty title should not be included even in IncludeDeleted variant")
}

func TestGetSessionTitlesBatchIncludeDeleted_Empty(t *testing.T) {
	setupDB(t)

	titles, err := service.GetSessionTitlesBatchIncludeDeleted([]string{})
	assert.NoError(t, err)
	assert.Empty(t, titles)
}

// ---------- GetSessionFullInfo ----------

func TestGetSessionFullInfo(t *testing.T) {
	_ = setupDB(t)

	sid, err := service.CreateSession("/my/project", "claude", "Full Info Test", "my-agent", "gpt-4o", "user", "chat")
	assert.NoError(t, err)

	info := service.GetSessionFullInfo(sid)
	assert.NotNil(t, info)
	assert.Equal(t, "claude", info.Backend)
	assert.Equal(t, "/my/project", info.ProjectPath)
	assert.Equal(t, "Full Info Test", info.Title)
	assert.Equal(t, "my-agent", info.AgentID)
	assert.Equal(t, "gpt-4o", info.Model)
	assert.Equal(t, "", info.ThinkingEffort)
}

func TestGetSessionFullInfo_WithThinkingEffort(t *testing.T) {
	_ = setupDB(t)

	sid, err := service.CreateSession("/project", "claude", "Thinking", "claude", "", "default", "chat")
	assert.NoError(t, err)
	err = service.UpdateSessionThinkingEffort(sid, "high")
	assert.NoError(t, err)

	info := service.GetSessionFullInfo(sid)
	assert.NotNil(t, info)
	assert.Equal(t, "high", info.ThinkingEffort)
}

func TestGetSessionFullInfo_NotFound(t *testing.T) {
	_ = setupDB(t)

	info := service.GetSessionFullInfo("nonexistent")
	assert.Nil(t, info)
}

func TestGetSessionFullInfo_Deleted(t *testing.T) {
	_ = setupDB(t)

	sid, _ := service.CreateSession("/project", "claude", "Deleted", "claude", "", "default", "chat")
	service.DeleteSession("/project", "claude", sid)

	info := service.GetSessionFullInfo(sid)
	assert.Nil(t, info)
}

// ---------- GetSessionInfo ----------

func TestGetSessionInfo(t *testing.T) {
	_ = setupDB(t)

	s1, _ := service.CreateSession("/project", "claude", "My Session", "claude", "claude-sonnet-4-6", "default", "chat")

	info, err := service.GetSessionInfo(s1)
	assert.NoError(t, err)
	assert.Equal(t, "My Session", info.Title)
	assert.Equal(t, "claude", info.Backend)
	assert.Equal(t, "claude", info.AgentID)
	assert.Equal(t, "claude-sonnet-4-6", info.Model)
	assert.Equal(t, "", info.ThinkingEffort)
}

func TestGetSessionInfo_NotFound(t *testing.T) {
	_ = setupDB(t)

	_, err := service.GetSessionInfo("nonexistent")
	assert.Error(t, err)
}

func TestGetSessionInfo_Deleted(t *testing.T) {
	_ = setupDB(t)

	s1, _ := service.CreateSession("/project", "claude", "Deleted", "claude", "", "default", "chat")
	service.DeleteSession("/project", "claude", s1)

	_, err := service.GetSessionInfo(s1)
	assert.Error(t, err)
}

// ---------- GetSessions UnreadCount ----------

func TestGetSessions_UnreadCount_NoMessages(t *testing.T) {
	setupDB(t)

	helperCreateSession(t, "/project", "claude", "Empty Session")

	sessions, err := service.GetSessions("/project", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, 0, sessions[0].UnreadCount)
}

func TestGetSessions_UnreadCount_AllUnread(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Unread All")

	// Add assistant messages (no last_read_at set → all unread)
	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", "reply 1", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sid, "assistant", "reply 2", nil, false, "")
	assert.NoError(t, err)

	sessions, err := service.GetSessions("/project", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, 2, sessions[0].UnreadCount)
}

func TestGetSessions_UnreadCount_OnlyAssistantCounts(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Mixed Messages")

	// User messages should NOT be counted as unread
	_, err := service.AddChatMessage("/project", "claude", sid, "user", "hello", nil, false, "")
	assert.NoError(t, err)
	// Assistant messages should be counted
	_, err = service.AddChatMessage("/project", "claude", sid, "assistant", "hi", nil, false, "")
	assert.NoError(t, err)

	sessions, err := service.GetSessions("/project", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, 1, sessions[0].UnreadCount)
}

func TestGetSessions_UnreadCount_StreamingExcluded(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Streaming")

	// Streaming assistant message should NOT count as unread
	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", "partial", nil, true, "")
	assert.NoError(t, err)
	// Finalized assistant message should count
	_, err = service.AddChatMessage("/project", "claude", sid, "assistant", "done", nil, false, "")
	assert.NoError(t, err)

	sessions, err := service.GetSessions("/project", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, 1, sessions[0].UnreadCount)
}

func TestGetSessions_UnreadCount_AfterLastRead(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Read Some")

	// Add 3 assistant messages
	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", "old 1", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sid, "assistant", "old 2", nil, false, "")
	assert.NoError(t, err)

	// Mark as read
	service.UpdateLastRead(sid)

	// Small sleep to ensure created_at is after last_read_at (SQLite second precision)
	time.Sleep(1100 * time.Millisecond)

	// Add 1 more assistant message after reading
	_, err = service.AddChatMessage("/project", "claude", sid, "assistant", "new 1", nil, false, "")
	assert.NoError(t, err)

	sessions, err := service.GetSessions("/project", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, 1, sessions[0].UnreadCount, "only messages after last_read_at should be unread")
}

func TestGetSessions_UnreadCount_MultipleSessions(t *testing.T) {
	setupDB(t)

	sid1 := helperCreateSession(t, "/project", "claude", "Session 1")
	sid2 := helperCreateSession(t, "/project", "claude", "Session 2")

	// sid1: 3 unread
	_, _ = service.AddChatMessage("/project", "claude", sid1, "assistant", "a1", nil, false, "")
	_, _ = service.AddChatMessage("/project", "claude", sid1, "assistant", "a2", nil, false, "")
	_, _ = service.AddChatMessage("/project", "claude", sid1, "assistant", "a3", nil, false, "")

	// sid2: 1 unread
	_, _ = service.AddChatMessage("/project", "claude", sid2, "assistant", "b1", nil, false, "")

	sessions, err := service.GetSessions("/project", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 2)

	sessionMap := make(map[string]int)
	for _, s := range sessions {
		sessionMap[s.ID] = s.UnreadCount
	}
	assert.Equal(t, 3, sessionMap[sid1])
	assert.Equal(t, 1, sessionMap[sid2])
}

func TestGetSessions_UnreadCountScopedToProject(t *testing.T) {
	db := setupDB(t)
	_ = db

	// Create sessions in two different projects
	s1, _ := service.CreateSession("/project-a", "claude", "S1", "claude", "", "default", "chat")
	s2, _ := service.CreateSession("/project-b", "claude", "S2", "claude", "", "default", "chat")

	// Add assistant messages to both sessions
	service.AddChatMessage("/project-a", "claude", s1, "assistant", `{"blocks":[{"type":"text","text":"hello"}]}`, nil, false, "")
	service.AddChatMessage("/project-b", "claude", s2, "assistant", `{"blocks":[{"type":"text","text":"world"}]}`, nil, false, "")

	// Query project-a only — should have 1 unread
	sessions, err := service.GetSessions("/project-a", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, 1, sessions[0].UnreadCount, "unread count should only count messages in project-a")

	// Verify project-b also has 1 unread independently
	sessionsB, err := service.GetSessions("/project-b", "")
	assert.NoError(t, err)
	assert.Len(t, sessionsB, 1)
	assert.Equal(t, 1, sessionsB[0].UnreadCount, "unread count should only count messages in project-b")
}

// ---------- GetSessionsPaged UnreadCount ----------

func TestGetSessionsPaged_UnreadCount(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Paged Unread")

	_, _ = service.AddChatMessage("/project", "claude", sid, "assistant", "msg", nil, false, "")

	sessions, hasMore, err := service.GetSessionsPaged("/project", "", 10, "", "")
	assert.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, 1, sessions[0].UnreadCount)
	assert.False(t, hasMore)
}

// ---------- DBRead initialization ----------

func TestDBRead_Initialized_ChatDB(t *testing.T) {
	_ = setupDB(t)
	assert.NotNil(t, service.DBRead, "DBRead should be initialized in test setup")
}

// ---------- GetRunningSessionIDs ----------

func TestGetRunningSessionIDs_Empty(t *testing.T) {
	setupDB(t)

	// Clear any leftover state from prior tests
	for _, id := range service.GetRunningSessionIDs() {
		service.SetSessionRunning(id, false)
	}

	ids := service.GetRunningSessionIDs()
	assert.Empty(t, ids)
}

func TestGetRunningSessionIDs_MultipleRunning(t *testing.T) {
	setupDB(t)

	// Clear any leftover state from prior tests
	for _, id := range service.GetRunningSessionIDs() {
		service.SetSessionRunning(id, false)
	}

	service.SetSessionRunning("sess-1", true)
	service.SetSessionRunning("sess-2", true)
	service.SetSessionRunning("sess-3", true)

	ids := service.GetRunningSessionIDs()
	assert.Len(t, ids, 3)

	idSet := make(map[string]bool)
	for _, id := range ids {
		idSet[id] = true
	}
	assert.True(t, idSet["sess-1"])
	assert.True(t, idSet["sess-2"])
	assert.True(t, idSet["sess-3"])

	// Clean up
	service.SetSessionRunning("sess-1", false)
	service.SetSessionRunning("sess-2", false)
	service.SetSessionRunning("sess-3", false)
}

func TestGetRunningSessionIDs_SomeRunning(t *testing.T) {
	setupDB(t)

	// Clear any leftover state from prior tests
	for _, id := range service.GetRunningSessionIDs() {
		service.SetSessionRunning(id, false)
	}

	service.SetSessionRunning("sess-1", true)
	service.SetSessionRunning("sess-2", true)

	ids := service.GetRunningSessionIDs()
	assert.Len(t, ids, 2)

	// After stopping one
	service.SetSessionRunning("sess-1", false)
	ids = service.GetRunningSessionIDs()
	assert.Len(t, ids, 1)
	assert.Equal(t, "sess-2", ids[0])

	// Clean up
	service.SetSessionRunning("sess-2", false)
}

// ---------- GetLatestSessionID ----------

func TestGetLatestSessionID(t *testing.T) {
	_ = setupDB(t)

	// No sessions yet
	_, _, err := service.GetLatestSessionID("/project")
	assert.Error(t, err)

	// Create a session
	s1, _ := service.CreateSession("/project", "claude", "First", "claude", "", "default", "chat")

	// Should return it
	id, backend, err := service.GetLatestSessionID("/project")
	assert.NoError(t, err)
	assert.Equal(t, s1, id)
	assert.Equal(t, "claude", backend)

	// Create another and force its updated_at ahead (SQLite timestamps have second precision)
	s2, _ := service.CreateSession("/project", "codebuddy", "Second", "codebuddy", "", "default", "chat")
	service.DB.Exec("UPDATE chat_sessions SET updated_at = datetime('now', '+1 second') WHERE id = ?", s2)

	// Should return the newer one
	id, backend, err = service.GetLatestSessionID("/project")
	assert.NoError(t, err)
	assert.Equal(t, s2, id)
	assert.Equal(t, "codebuddy", backend)

	// Different project should return error
	_, _, err = service.GetLatestSessionID("/other-project")
	assert.Error(t, err)
}

func TestGetLatestSessionID_ExcludesDeleted(t *testing.T) {
	_ = setupDB(t)

	s1, _ := service.CreateSession("/project", "claude", "First", "claude", "", "default", "chat")
	s2, _ := service.CreateSession("/project", "codebuddy", "Second", "codebuddy", "", "default", "chat")

	// Delete the most recent session
	service.DeleteSession("/project", "codebuddy", s2)

	// Should return the remaining session
	id, backend, err := service.GetLatestSessionID("/project")
	assert.NoError(t, err)
	assert.Equal(t, s1, id)
	assert.Equal(t, "claude", backend)
}

func TestGetRunningSessionIDs_AfterClearAll(t *testing.T) {
	setupDB(t)

	// Clear any leftover state from prior tests
	for _, id := range service.GetRunningSessionIDs() {
		service.SetSessionRunning(id, false)
	}

	service.SetSessionRunning("sess-1", true)
	service.SetSessionRunning("sess-2", true)

	service.SetSessionRunning("sess-1", false)
	service.SetSessionRunning("sess-2", false)

	ids := service.GetRunningSessionIDs()
	assert.Empty(t, ids)
}

// ========== CreateSession: external_session_id initialization ==========

// TestCreateSession_ExternalSessionIDInitialized verifies that CreateSession
// sets external_session_id = sessionID at creation time for all backends.
// This is critical for --resume to work with codebuddy/claude/qoder backends
// where the ClawBench UUID IS the CLI session ID.
func TestCreateSession_ExternalSessionIDInitialized(t *testing.T) {
	setupDB(t)

	// Test with multiple backends
	for _, backend := range []string{"codebuddy", "claude", "opencode", "pi"} {
		t.Run(backend, func(t *testing.T) {
			sid, err := service.CreateSession("/project", backend, "Test "+backend, "", "", "default", "chat")
			assert.NoError(t, err)
			assert.NotEmpty(t, sid)

			// external_session_id should be initialized to the session ID
			extID := service.GetExternalSessionID(sid)
			assert.Equal(t, sid, extID, "external_session_id should be initialized to sessionID for backend %s", backend)
		})
	}
}

// TestCreateSession_ExternalSessionIDPersistedInDB explicitly verifies
// that the INSERT statement in CreateSession writes external_session_id = sessionID.
func TestCreateSession_ExternalSessionIDPersistedInDB(t *testing.T) {
	setupDB(t)

	sid, err := service.CreateSession("/project", "claude", "DB Check", "", "", "default", "chat")
	assert.NoError(t, err)

	var extID string
	err = service.DB.QueryRow("SELECT external_session_id FROM chat_sessions WHERE id = ?", sid).Scan(&extID)
	assert.NoError(t, err)
	assert.Equal(t, sid, extID, "external_session_id column should equal session ID in DB")
}

// ========== DeleteSession: does NOT modify chat_history ==========

// TestDeleteSession_DoesNotModifyChatHistory verifies that DeleteSession
// only soft-deletes the session record and does NOT touch chat_history
// at all. This is critical — the old code set chat_history.deleted=1,
// which caused data loss when restoring sessions.
func TestDeleteSession_DoesNotModifyChatHistory(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "To Delete")

	// Add messages
	msgID1, err := service.AddChatMessage("/project", "claude", sid, "user", "message 1", nil, false, "NewSession")
	assert.NoError(t, err)
	msgID2, err := service.AddChatMessage("/project", "claude", sid, "assistant", "response 1", nil, false, "")
	assert.NoError(t, err)

	// Verify messages exist before deletion
	var countBefore int
	err = service.DB.QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sid).Scan(&countBefore)
	assert.NoError(t, err)
	assert.Equal(t, 2, countBefore)

	// Delete the session
	err = service.DeleteSession("/project", "claude", sid)
	assert.NoError(t, err)

	// Verify messages are still present with identical content
	var countAfter int
	err = service.DB.QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sid).Scan(&countAfter)
	assert.NoError(t, err)
	assert.Equal(t, 2, countAfter, "chat_history rows should NOT be modified by DeleteSession")

	// Verify individual message content is unchanged
	msg1, err := service.GetMessageByID(msgID1)
	assert.NoError(t, err)
	assert.Equal(t, "message 1", msg1.Content)

	msg2, err := service.GetMessageByID(msgID2)
	assert.NoError(t, err)
	assert.Equal(t, "response 1", msg2.Content)
}

// ========== Restoring a deleted session preserves all chat history ==========

// TestRestoreDeletedSession_PreservesChatHistory verifies that when a
// soft-deleted session is restored (deleted=0), all chat history messages
// are still accessible. This was a critical bug where restored sessions
// had no chat history because chat_history.deleted was set to 1 on delete.
func TestRestoreDeletedSession_PreservesChatHistory(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "To Delete & Restore")

	// Add messages before deletion
	_, err := service.AddChatMessage("/project", "claude", sid, "user", "before delete", nil, false, "NewSession")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sid, "assistant", "before delete reply", nil, false, "")
	assert.NoError(t, err)

	// Delete the session
	err = service.DeleteSession("/project", "claude", sid)
	assert.NoError(t, err)

	// Verify GetChatHistory still returns messages (no deleted column in chat_history)
	msgsAfterDelete, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgsAfterDelete, 2, "messages should still exist after session soft-delete")

	// Restore the session by setting deleted=0
	_, err = service.DB.Exec("UPDATE chat_sessions SET deleted = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?", sid)
	assert.NoError(t, err)

	// Verify restored session can be found by GetSessionBackend
	backend := service.GetSessionBackend(sid)
	assert.Equal(t, "claude", backend, "restored session should be visible via GetSessionBackend")

	// Verify chat history is fully intact
	msgsAfterRestore, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgsAfterRestore, 2, "all messages should be preserved after restore")
	assert.Equal(t, "before delete", msgsAfterRestore[0].Content)
	assert.Equal(t, "before delete reply", msgsAfterRestore[1].Content)

	// Verify the session can accept new messages after restore
	_, err = service.AddChatMessage("/project", "claude", sid, "user", "after restore", nil, false, "")
	assert.NoError(t, err)

	msgsFinal, err := service.GetChatHistory("/project", "claude", sid)
	assert.NoError(t, err)
	assert.Len(t, msgsFinal, 3)
	assert.Equal(t, "after restore", msgsFinal[2].Content)
}

// ---------- GetSessionModel / UpdateSessionModel ----------

func TestGetSessionModel(t *testing.T) {
	setupDB(t)

	sid, err := service.CreateSession("/project", "claude", "Test", "claude", "claude-sonnet-4-6", "default", "chat")
	assert.NoError(t, err)

	modelID := service.GetSessionModel(sid)
	assert.Equal(t, "claude-sonnet-4-6", modelID)
}

func TestGetSessionModel_Empty(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "No Model")
	modelID := service.GetSessionModel(sid)
	assert.Equal(t, "", modelID)
}

func TestGetSessionModel_NonExistent(t *testing.T) {
	setupDB(t)
	assert.Equal(t, "", service.GetSessionModel("non-existent"))
}

func TestGetSessionModel_DeletedSession(t *testing.T) {
	setupDB(t)

	sid, err := service.CreateSession("/project", "claude", "Deleted", "claude", "gpt-4", "default", "chat")
	assert.NoError(t, err)

	err = service.DeleteSession("/project", "claude", sid)
	assert.NoError(t, err)

	assert.Equal(t, "", service.GetSessionModel(sid))
}

func TestUpdateSessionModel(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Model Test")

	err := service.UpdateSessionModel(sid, "gpt-4o")
	assert.NoError(t, err)

	modelID := service.GetSessionModel(sid)
	assert.Equal(t, "gpt-4o", modelID)
}

func TestUpdateSessionModel_NonExistent(t *testing.T) {
	setupDB(t)

	err := service.UpdateSessionModel("non-existent", "gpt-4o")
	assert.NoError(t, err) // UPDATE on non-existent row is a no-op
}

// ---------- GetStreamingMessageID ----------

func TestGetStreamingMessageID_Found(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Stream Test")

	// Add a streaming message then finalize it
	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", "streaming...", nil, true, "")
	assert.NoError(t, err)

	err = service.FinalizeStreamingMessage("/project", "claude", sid, "final content")
	assert.NoError(t, err)

	id := service.GetStreamingMessageID(sid)
	assert.Greater(t, id, int64(0))
}

func TestGetStreamingMessageID_NoAssistantMessage(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Empty")
	id := service.GetStreamingMessageID(sid)
	assert.Equal(t, int64(0), id)
}

// ---------- SaveRawResponse ----------

func TestSaveRawResponse(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Raw Test")

	_, err := service.AddChatMessage("/project", "claude", sid, "assistant", "test", nil, false, "")
	assert.NoError(t, err)

	msgID := service.GetStreamingMessageID(sid)

	err = service.SaveRawResponse(sid, "claude", msgID, "raw output data")
	assert.NoError(t, err)

	// Verify it was saved
	var rawOutput string
	err = service.DB.QueryRow("SELECT raw_output FROM ai_raw_responses WHERE session_id = ?", sid).Scan(&rawOutput)
	assert.NoError(t, err)
	assert.Equal(t, "raw output data", rawOutput)
}

// ---------- GetUnindexedMessages / MarkMessageIndexed / UnindexedCount ----------

func TestGetUnindexedMessages_Empty(t *testing.T) {
	setupDB(t)

	msgs, err := service.GetUnindexedMessages(10)
	assert.NoError(t, err)
	assert.Empty(t, msgs)
}

func TestGetUnindexedMessages_WithMessages(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Index Test")

	_, err := service.AddChatMessage("/project", "claude", sid, "user", "hello", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sid, "assistant", "world", nil, false, "")
	assert.NoError(t, err)

	msgs, err := service.GetUnindexedMessages(10)
	assert.NoError(t, err)
	assert.NotEmpty(t, msgs)
}

func TestMarkMessageIndexed(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Index Test")
	msgID, err := service.AddChatMessage("/project", "claude", sid, "user", "hello", nil, false, "")
	assert.NoError(t, err)

	err = service.MarkMessageIndexed(msgID)
	assert.NoError(t, err)

	// Verify the message is now indexed
	var indexed int
	err = service.DB.QueryRow("SELECT indexed FROM chat_history WHERE id = ?", msgID).Scan(&indexed)
	assert.NoError(t, err)
	assert.Equal(t, 1, indexed)
}

func TestUnindexedCount(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Count Test")

	// Initially 0
	count, err := service.UnindexedCount()
	assert.NoError(t, err)
	assert.Equal(t, 0, count)

	// Add messages (new messages default to indexed=0)
	_, err = service.AddChatMessage("/project", "claude", sid, "user", "hello", nil, false, "")
	assert.NoError(t, err)

	count, err = service.UnindexedCount()
	assert.NoError(t, err)
	assert.Greater(t, count, 0)
}

// ---------- GetChatHistoryPaged (cursor-based) ----------

func TestGetChatHistoryPaged_LimitAndBeforeID(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Paged History")

	// Add 5 messages
	msgIDs := make([]int64, 0, 5)
	for i := range 5 {
		id, err := service.AddChatMessage("/project", "claude", sid, "user", fmt.Sprintf("msg %d", i), nil, false, "")
		assert.NoError(t, err)
		msgIDs = append(msgIDs, id)
	}

	// Get last 2 messages with limit only (no cursor)
	msgs, err := service.GetChatHistoryPaged("/project", "claude", sid, 2, 0)
	assert.NoError(t, err)
	assert.Len(t, msgs, 2)
	assert.Equal(t, "msg 3", msgs[0].Content)
	assert.Equal(t, "msg 4", msgs[1].Content)

	// Get 2 messages before the last message (cursor-based)
	msgs, err = service.GetChatHistoryPaged("/project", "claude", sid, 2, int(msgIDs[4]))
	assert.NoError(t, err)
	assert.Len(t, msgs, 2)
	assert.Equal(t, "msg 2", msgs[0].Content)
	assert.Equal(t, "msg 3", msgs[1].Content)
}

// ---------- GetSessionProjectPath ----------

func TestGetSessionProjectPath(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/my/project", "claude", "Test")

	path := service.GetSessionProjectPath(sid)
	assert.Equal(t, "/my/project", path)
}

func TestGetSessionProjectPath_NonExistent(t *testing.T) {
	setupDB(t)

	path := service.GetSessionProjectPath("non-existent")
	assert.Equal(t, "", path)
}

// ---------- GetLatestUserModel ----------

func TestGetLatestUserModel_Found(t *testing.T) {
	setupDB(t)

	_, err := service.CreateSession("/project", "claude", "Test", "claude", "gpt-4o", "user", "chat")
	assert.NoError(t, err)

	modelID, thinkingEffort := service.GetLatestUserModel("claude", "/project")
	assert.Equal(t, "gpt-4o", modelID)
	assert.Equal(t, "", thinkingEffort)
}

func TestGetLatestUserModel_WithThinkingEffort(t *testing.T) {
	setupDB(t)

	sid, err := service.CreateSession("/project", "claude", "Test", "claude", "gpt-4o", "user", "chat")
	assert.NoError(t, err)

	err = service.UpdateSessionThinkingEffort(sid, "high")
	assert.NoError(t, err)

	modelID, thinkingEffort := service.GetLatestUserModel("claude", "/project")
	assert.Equal(t, "gpt-4o", modelID)
	assert.Equal(t, "high", thinkingEffort)
}

func TestGetLatestUserModel_NotFound(t *testing.T) {
	setupDB(t)

	modelID, thinkingEffort := service.GetLatestUserModel("claude", "/project")
	assert.Equal(t, "", modelID)
	assert.Equal(t, "", thinkingEffort)
}

// ---------- GetChatHistoryPaged: all branches ----------

func TestGetChatHistoryPaged_NoLimit(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Paged All")

	_, err := service.AddChatMessage("/project", "claude", sid, "user", "msg1", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sid, "user", "msg2", nil, false, "")
	assert.NoError(t, err)

	// limit=0 returns all messages
	msgs, err := service.GetChatHistoryPaged("/project", "claude", sid, 0, 0)
	assert.NoError(t, err)
	assert.Len(t, msgs, 2)
}

func TestGetChatHistoryPaged_LimitOnly(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Paged Limit")

	for i := range 5 {
		_, err := service.AddChatMessage("/project", "claude", sid, "user", fmt.Sprintf("msg %d", i), nil, false, "")
		assert.NoError(t, err)
	}

	// limit=3, no cursor — should return the 3 most recent
	msgs, err := service.GetChatHistoryPaged("/project", "claude", sid, 3, 0)
	assert.NoError(t, err)
	assert.Len(t, msgs, 3)
	assert.Equal(t, "msg 2", msgs[0].Content) // oldest of the 3
	assert.Equal(t, "msg 4", msgs[2].Content) // newest
}

func TestGetChatHistoryPaged_Empty(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Empty Paged")

	msgs, err := service.GetChatHistoryPaged("/project", "claude", sid, 10, 0)
	assert.NoError(t, err)
	assert.Empty(t, msgs)
}

// ---------- CreateSession: session_type default ----------

func TestCreateSession_EmptySessionTypeDefaultsToChat(t *testing.T) {
	setupDB(t)

	sid, err := service.CreateSession("/project", "claude", "Default Type", "", "", "default", "")
	assert.NoError(t, err)

	var sessionType string
	err = service.DB.QueryRow("SELECT session_type FROM chat_sessions WHERE id = ?", sid).Scan(&sessionType)
	assert.NoError(t, err)
	assert.Equal(t, "chat", sessionType)
}
