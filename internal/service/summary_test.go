package service

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/summarize"

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
			summary_cards TEXT NOT NULL DEFAULT '',
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

func TestAsyncSummarize_ShortText(t *testing.T) {
	_, dbTeardown := setupTestDBForAsyncSummary(t)
	defer dbTeardown()

	origInstance := taskSummarizerInstance
	defer func() { taskSummarizerInstance = origInstance }()

	// Create a TaskSummarizer with a mock pipeline (should not be called for short text)
	passFn := func(ctx context.Context, text, systemPrompt string, pass int) (string, error) {
		return "should not be called", nil
	}
	pipeline := summarize.NewPipelineWithOpts(passFn, summarize.TaskSummarizePrompt(), summarize.SummarizeOption{PreserveMarkdown: true})
	taskSummarizerInstance = summarize.NewTaskSummarizerFromPipeline(pipeline)

	// Short text block — should save empty summary
	blocks := []model.ContentBlock{{Type: "text", Text: "短"}}

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

	// Create mock pipeline that returns a summary
	passFn := func(ctx context.Context, text, systemPrompt string, pass int) (string, error) {
		return "## 精简总结\n\n关键结论。", nil
	}
	pipeline := summarize.NewPipelineWithOpts(passFn, summarize.TaskSummarizePrompt(), summarize.SummarizeOption{PreserveMarkdown: true})
	taskSummarizerInstance = summarize.NewTaskSummarizerFromPipeline(pipeline)

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
			summary_cards TEXT NOT NULL DEFAULT '',
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

	// Mock pipeline that returns error
	passFn := func(ctx context.Context, text, systemPrompt string, pass int) (string, error) {
		return "", context.DeadlineExceeded
	}
	pipeline := summarize.NewPipelineWithOpts(passFn, summarize.TaskSummarizePrompt(), summarize.SummarizeOption{PreserveMarkdown: true})
	taskSummarizerInstance = summarize.NewTaskSummarizerFromPipeline(pipeline)

	longText := strings.Repeat("这是一段较长的AI回复内容。", 30)
	blocks := []model.ContentBlock{{Type: "text", Text: longText}}

	AsyncSummarize("chat_message", 4, blocks, "/test", "session-4")

	time.Sleep(200 * time.Millisecond)

	// Fallback: SimpleSummarizer should be used as fallback on backend error
	summary, found := GetSummary("chat_message", 4)
	assert.True(t, found, "summary should exist as fallback on backend error")
	assert.NotEmpty(t, summary, "fallback summary should not be empty")
	// SimpleSummarizer strips markdown (no-op for plain text) and truncates
	assert.Equal(t, longText, summary, "plain text below 1000 runes should pass through SimpleSummarizer unchanged")
}

func TestAsyncSummarize_BackendError_ExtractsConclusion(t *testing.T) {
	_, dbTeardown := setupTestDBForAsyncSummary(t)
	defer dbTeardown()

	origInstance := taskSummarizerInstance
	defer func() { taskSummarizerInstance = origInstance }()

	// Mock pipeline that returns error
	passFn := func(ctx context.Context, text, systemPrompt string, pass int) (string, error) {
		return "", context.DeadlineExceeded
	}
	pipeline := summarize.NewPipelineWithOpts(passFn, summarize.TaskSummarizePrompt(), summarize.SummarizeOption{PreserveMarkdown: true})
	taskSummarizerInstance = summarize.NewTaskSummarizerFromPipeline(pipeline)

	// Multi-block with tool_use — fallback should extract the conclusion
	// (text after last tool_use), not the intro text
	conclusionText := strings.Repeat("这是最终结论内容，比较长。", 30)
	blocks := []model.ContentBlock{
		{Type: "text", Text: "让我检查一下..."},
		{Type: "tool_use", Text: "read_file"},
		{Type: "text", Text: conclusionText},
	}

	AsyncSummarize("chat_message", 5, blocks, "/test", "session-5")

	time.Sleep(200 * time.Millisecond)

	summary, found := GetSummary("chat_message", 5)
	assert.True(t, found, "summary should exist as fallback on backend error")
	assert.NotEmpty(t, summary, "fallback summary should not be empty")
	// SimpleSummarizer processes the conclusion text (stripped markdown + truncated),
	// but for plain Chinese text below 1000 runes it should be unchanged.
	assert.Equal(t, conclusionText, summary, "fallback should use ExtractLastAnswerFromBlocks + SimpleSummarizer")
}
