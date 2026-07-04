package service

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/summarize"
	"clawbench/internal/ws"

	"github.com/stretchr/testify/assert"
)

// --- AsyncSummarize tests ---

// setupTestDBForAsyncSummary creates an in-memory DB with summaries table
func setupTestDBForAsyncSummary(t *testing.T) (*sql.DB, func()) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	db.SetMaxOpenConns(1)
	_, _ = db.Exec("PRAGMA journal_mode=WAL")
	_, _ = db.Exec("PRAGMA busy_timeout=5000")

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS summaries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			target_type TEXT NOT NULL,
			target_id   INTEGER NOT NULL,
			summary     TEXT NOT NULL,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(target_type, target_id)
		);
	`)
	if err != nil {
		t.Fatalf("failed to create tables: %v", err)
	}

	cleanup := SetDBForTest(db, db)
	teardown := func() {
		cleanup()
		db.Close()
	}
	return db, teardown
}

// mockAsyncSummarizerBackend is a mock AI backend for AsyncSummarize tests
type mockAsyncSummarizerBackend struct {
	streamCh   chan ai.StreamEvent
	executeErr error
}

func (m *mockAsyncSummarizerBackend) Name() string { return "mock-async" }

func (m *mockAsyncSummarizerBackend) ExecuteStream(ctx context.Context, req ai.ChatRequest) (<-chan ai.StreamEvent, error) {
	if m.executeErr != nil {
		return nil, m.executeErr
	}
	return m.streamCh, nil
}

func TestAsyncSummarize_ShortText(t *testing.T) {
	_, dbTeardown := setupTestDBForAsyncSummary(t)
	defer dbTeardown()

	origInstance := taskSummarizerInstance
	defer func() { taskSummarizerInstance = origInstance }()

	// Create a TaskSummarizer with mock backend
	ch := make(chan ai.StreamEvent, 1)
	ch <- ai.StreamEvent{Type: "done"}
	close(ch)
	mock := &mockAsyncSummarizerBackend{streamCh: ch}
	taskSummarizerInstance = &summarize.TaskSummarizer{Backend: mock}

	// Short text block — should save empty summary
	blocks := []model.ContentBlock{{Type: "text", Text: "短"}}

	var wg sync.WaitGroup
	wg.Add(1)
	origBroadcast := ws.GetManager()
	_ = origBroadcast // We don't test WS here, just DB

	AsyncSummarize("chat_message", 1, blocks, "/test", "session-1")

	// Wait for goroutine to complete
	time.Sleep(200 * time.Millisecond)

	summary, found := GetSummary("chat_message", 1)
	assert.True(t, found)
	assert.Equal(t, "", summary) // short text = empty summary
}

func TestAsyncSummarize_NormalText(t *testing.T) {
	_, dbTeardown := setupTestDBForAsyncSummary(t)
	defer dbTeardown()

	origInstance := taskSummarizerInstance
	defer func() { taskSummarizerInstance = origInstance }()

	// Create mock backend that returns a summary
	ch := make(chan ai.StreamEvent, 3)
	ch <- ai.StreamEvent{Type: "content", Content: "## 精简总结\n\n关键结论。"}
	ch <- ai.StreamEvent{Type: "done"}
	close(ch)
	mock := &mockAsyncSummarizerBackend{streamCh: ch}
	taskSummarizerInstance = &summarize.TaskSummarizer{Backend: mock}

	// Long text block
	longText := strings.Repeat("这是一段较长的AI回复内容。", 30)
	blocks := []model.ContentBlock{{Type: "text", Text: longText}}

	AsyncSummarize("chat_message", 2, blocks, "/test", "session-2")

	// Wait for goroutine to complete
	time.Sleep(200 * time.Millisecond)

	summary, found := GetSummary("chat_message", 2)
	assert.True(t, found)
	assert.Contains(t, summary, "精简总结")
}

func TestAsyncSummarize_NilSummarizer(t *testing.T) {
	_, dbTeardown := setupTestDBForAsyncSummary(t)
	defer dbTeardown()

	origInstance := taskSummarizerInstance
	defer func() { taskSummarizerInstance = origInstance }()

	// nil summarizer — should return immediately, no goroutine
	taskSummarizerInstance = nil

	blocks := []model.ContentBlock{{Type: "text", Text: "some text"}}

	// Should not panic or create goroutine
	AsyncSummarize("chat_message", 3, blocks, "/test", "session-3")

	time.Sleep(100 * time.Millisecond)

	// No summary should be saved
	_, found := GetSummary("chat_message", 3)
	assert.False(t, found)
}

// --- MigrateTaskExecutionSummaries tests ---

// setupTestDBForMigration creates an in-memory DB with the tables needed
// for MigrateTaskExecutionSummaries.
func setupTestDBForMigration(t *testing.T) func() {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	db.SetMaxOpenConns(1)
	_, _ = db.Exec("PRAGMA journal_mode=WAL")
	_, _ = db.Exec("PRAGMA busy_timeout=5000")

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS summaries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			target_type TEXT NOT NULL,
			target_id   INTEGER NOT NULL,
			summary     TEXT NOT NULL,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(target_type, target_id)
		);
		CREATE TABLE IF NOT EXISTS task_executions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id INTEGER NOT NULL,
			session_id TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'completed',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_path TEXT NOT NULL,
			role TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
			content TEXT NOT NULL,
			session_id TEXT,
			backend TEXT NOT NULL DEFAULT 'claude',
			streaming INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		t.Fatalf("failed to create tables: %v", err)
	}

	cleanup := SetDBForTest(db, db)
	teardown := func() {
		cleanup()
		db.Close()
	}
	return teardown
}

func TestMigrateTaskExecutionSummaries_NoopWhenEmpty(t *testing.T) {
	teardown := setupTestDBForMigration(t)
	defer teardown()

	// No task_execution summaries — migration should be a no-op
	MigrateTaskExecutionSummaries()

	var count int
	_ = dbRead.QueryRow("SELECT COUNT(*) FROM summaries WHERE target_type = 'task_execution'").Scan(&count)
	assert.Equal(t, 0, count)
}

func TestMigrateTaskExecutionSummaries_ConvertsToChatMessage(t *testing.T) {
	teardown := setupTestDBForMigration(t)
	defer teardown()

	// Set up: task_execution → session → assistant message
	_, _ = db.Exec("INSERT INTO task_executions (id, task_id, session_id, status) VALUES (1, 10, 'sess-1', 'completed')")
	_, _ = db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES ('/test', 'assistant', '{\"blocks\":[]}', 'sess-1', 'claude', 0)")
	// Get the assistant message ID
	var msgID int64
	_ = dbRead.QueryRow("SELECT id FROM chat_history WHERE session_id = 'sess-1' AND role = 'assistant'").Scan(&msgID)

	// Insert a task_execution summary
	_, _ = db.Exec("INSERT INTO summaries (target_type, target_id, summary, created_at) VALUES ('task_execution', 1, 'Task summary text', CURRENT_TIMESTAMP)")

	// Run migration
	MigrateTaskExecutionSummaries()

	// Verify: task_execution summary deleted
	var taskExecCount int
	_ = dbRead.QueryRow("SELECT COUNT(*) FROM summaries WHERE target_type = 'task_execution'").Scan(&taskExecCount)
	assert.Equal(t, 0, taskExecCount, "task_execution summary should be deleted after migration")

	// Verify: chat_message summary created with correct content
	summary, found := GetSummary("chat_message", msgID)
	assert.True(t, found, "chat_message summary should exist after migration")
	assert.Equal(t, "Task summary text", summary)
}

func TestMigrateTaskExecutionSummaries_NoAssistantMessage(t *testing.T) {
	teardown := setupTestDBForMigration(t)
	defer teardown()

	// Set up: task_execution with no corresponding assistant message
	_, _ = db.Exec("INSERT INTO task_executions (id, task_id, session_id, status) VALUES (2, 20, 'sess-orphan', 'completed')")
	_, _ = db.Exec("INSERT INTO summaries (target_type, target_id, summary, created_at) VALUES ('task_execution', 2, 'Orphan summary', CURRENT_TIMESTAMP)")

	// Run migration — should skip this summary (no assistant message found)
	MigrateTaskExecutionSummaries()

	// Verify: task_execution summary deleted (even though no chat_message was created)
	var taskExecCount int
	_ = dbRead.QueryRow("SELECT COUNT(*) FROM summaries WHERE target_type = 'task_execution'").Scan(&taskExecCount)
	assert.Equal(t, 0, taskExecCount, "orphaned task_execution summary should be cleaned up")

	// Verify: no chat_message summary created (no assistant message to attach to)
	var chatMsgCount int
	_ = dbRead.QueryRow("SELECT COUNT(*) FROM summaries WHERE target_type = 'chat_message'").Scan(&chatMsgCount)
	assert.Equal(t, 0, chatMsgCount)
}

func TestMigrateTaskExecutionSummaries_Idempotent(t *testing.T) {
	teardown := setupTestDBForMigration(t)
	defer teardown()

	// Set up
	_, _ = db.Exec("INSERT INTO task_executions (id, task_id, session_id, status) VALUES (3, 30, 'sess-idem', 'completed')")
	_, _ = db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES ('/test', 'assistant', '{\"blocks\":[]}', 'sess-idem', 'claude', 0)")
	_, _ = db.Exec("INSERT INTO summaries (target_type, target_id, summary, created_at) VALUES ('task_execution', 3, 'Idempotent summary', CURRENT_TIMESTAMP)")

	// Run migration twice
	MigrateTaskExecutionSummaries()
	MigrateTaskExecutionSummaries()

	// Verify: only one chat_message summary exists (INSERT OR IGNORE)
	var chatMsgCount int
	_ = dbRead.QueryRow("SELECT COUNT(*) FROM summaries WHERE target_type = 'chat_message'").Scan(&chatMsgCount)
	assert.Equal(t, 1, chatMsgCount, "should have exactly one chat_message summary after running twice")

	// Verify: no task_execution summaries remain
	var taskExecCount int
	_ = dbRead.QueryRow("SELECT COUNT(*) FROM summaries WHERE target_type = 'task_execution'").Scan(&taskExecCount)
	assert.Equal(t, 0, taskExecCount)
}

func TestAsyncSummarize_BackendError(t *testing.T) {
	_, dbTeardown := setupTestDBForAsyncSummary(t)
	defer dbTeardown()

	origInstance := taskSummarizerInstance
	defer func() { taskSummarizerInstance = origInstance }()

	// Mock backend that returns error
	mock := &mockAsyncSummarizerBackend{executeErr: context.DeadlineExceeded}
	taskSummarizerInstance = &summarize.TaskSummarizer{Backend: mock}

	longText := strings.Repeat("这是一段较长的AI回复内容。", 30)
	blocks := []model.ContentBlock{{Type: "text", Text: longText}}

	AsyncSummarize("chat_message", 4, blocks, "/test", "session-4")

	time.Sleep(200 * time.Millisecond)

	// No summary saved on error
	_, found := GetSummary("chat_message", 4)
	assert.False(t, found)
}
