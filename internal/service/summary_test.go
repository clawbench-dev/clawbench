package service

import (
	"database/sql"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

// --- GenerateMessageSummaryOnDemand tests ---

func TestGenerateMessageSummaryOnDemand_GeneratesAndSaves(t *testing.T) {
	db, teardown := setupTestDBForTriggerSummary(t)
	defer teardown()

	_, err := db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES ('/test', 'assistant', ?, 'sess-1', 'claude')",
		`{"blocks":[{"type":"text","text":"让我看看"},{"type":"tool_use","text":"Bash"},{"type":"text","text":"最终结论：搞定"}]}`)
	assert.NoError(t, err)

	summary, _, ok, err := GenerateMessageSummaryOnDemand(1)
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "最终结论：搞定", summary)

	// Persisted so a reload of history shows it.
	persisted, found := GetSummary("chat_message", 1)
	assert.True(t, found)
	assert.Equal(t, "最终结论：搞定", persisted)
}

func TestGenerateMessageSummaryOnDemand_ReturnsExisting(t *testing.T) {
	db, teardown := setupTestDBForTriggerSummary(t)
	defer teardown()

	_, err := db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES ('/test', 'assistant', ?, 'sess-1', 'claude')",
		`{"blocks":[{"type":"text","text":"已有摘要的消息"}]}`)
	assert.NoError(t, err)
	_, err = db.Exec("INSERT INTO summaries (target_type, target_id, summary) VALUES ('chat_message', 1, '已经存在')")
	assert.NoError(t, err)

	summary, _, ok, err := GenerateMessageSummaryOnDemand(1)
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "已经存在", summary)
}

func TestGenerateMessageSummaryOnDemand_NoTextReturnsNotOK(t *testing.T) {
	db, teardown := setupTestDBForTriggerSummary(t)
	defer teardown()

	// Assistant message whose blocks contain no extractable answer text.
	_, err := db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES ('/test', 'assistant', ?, 'sess-1', 'claude')",
		`{"blocks":[{"type":"tool_use","text":"Bash"}]}`)
	assert.NoError(t, err)

	summary, _, ok, err := GenerateMessageSummaryOnDemand(1)
	assert.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, "", summary)

	_, found := GetSummary("chat_message", 1)
	assert.False(t, found)
}

func TestGenerateMessageSummaryOnDemand_MessageNotFound(t *testing.T) {
	_, teardown := setupTestDBForTriggerSummary(t)
	defer teardown()

	_, _, ok, err := GenerateMessageSummaryOnDemand(999)
	assert.Error(t, err)
	assert.False(t, ok)
}

func TestGenerateMessageSummaryOnDemand_UnparseableContent(t *testing.T) {
	db, teardown := setupTestDBForTriggerSummary(t)
	defer teardown()

	// Content is not valid blocks JSON — treated as no summary available.
	_, err := db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES ('/test', 'assistant', 'not json at all', 'sess-1', 'claude')")
	assert.NoError(t, err)

	summary, _, ok, err := GenerateMessageSummaryOnDemand(1)
	assert.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, "", summary)
}

func TestGenerateMessageSummaryOnDemand_SkipsUserAndStreaming(t *testing.T) {
	db, teardown := setupTestDBForTriggerSummary(t)
	defer teardown()

	// A user message should never be summarized.
	_, err := db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES ('/test', 'user', '我的问题', 'sess-1', 'claude')")
	assert.NoError(t, err)
	summary, _, ok, err := GenerateMessageSummaryOnDemand(1)
	assert.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, "", summary)

	// A streaming assistant message should not get an incomplete summary.
	_, err = db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES ('/test', 'assistant', ?, 'sess-1', 'claude', 1)",
		`{"blocks":[{"type":"text","text":"还在生成中"}]}`)
	assert.NoError(t, err)
	summary, _, ok, err = GenerateMessageSummaryOnDemand(2)
	assert.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, "", summary)

	_, found := GetSummary("chat_message", 1)
	assert.False(t, found)
	_, found = GetSummary("chat_message", 2)
	assert.False(t, found)
}

// --- summarizeMessage tests ---

// setupTestDBForAsyncSummary creates an in-memory DB with summaries table
func setupTestDBForAsyncSummary(t *testing.T) func() {
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
	return teardown
}

func TestSummarizeMessage_MultiBlockExtractsConclusion(t *testing.T) {
	dbTeardown := setupTestDBForAsyncSummary(t)
	defer dbTeardown()

	// Multi-block: intro text, tool_use, then conclusion text.
	// Always-extract must save only the conclusion (text after last tool_use).
	conclusion := "最终结论：所有改动已完成并测试通过。"
	blocks := []model.ContentBlock{
		{Type: "text", Text: "让我检查一下..."},
		{Type: "tool_use", Text: "read_file"},
		{Type: "text", Text: conclusion},
	}

	err := summarizeMessage(1, blocks, "/test", "session-1")
	assert.NoError(t, err)

	summary, found := GetSummary("chat_message", 1)
	assert.True(t, found)
	assert.Equal(t, conclusion, summary)
}

func TestSummarizeMessage_NoTextSavesNothing(t *testing.T) {
	dbTeardown := setupTestDBForAsyncSummary(t)
	defer dbTeardown()

	// No text block (only tool_use) → nothing to extract, no summary saved.
	blocks := []model.ContentBlock{{Type: "tool_use", Text: "read_file"}}

	err := summarizeMessage(2, blocks, "/test", "session-2")
	assert.NoError(t, err)

	_, found := GetSummary("chat_message", 2)
	assert.False(t, found)
}

func TestSummarizeMessage_AlwaysExtractsConclusion(t *testing.T) {
	dbTeardown := setupTestDBForAsyncSummary(t)
	defer dbTeardown()

	conclusion := "答案就是 42。"
	blocks := []model.ContentBlock{
		{Type: "text", Text: "先看看..."},
		{Type: "tool_use", Text: "Bash"},
		{Type: "text", Text: conclusion},
	}

	_ = summarizeMessage(3, blocks, "/test", "session-3")

	summary, found := GetSummary("chat_message", 3)
	assert.True(t, found)
	assert.Equal(t, conclusion, summary)
}

func TestSummarizeMessage_SingleShortTextStillExtracted(t *testing.T) {
	dbTeardown := setupTestDBForAsyncSummary(t)
	defer dbTeardown()

	// Short text: no threshold — always-extract saves the conclusion directly.
	blocks := []model.ContentBlock{{Type: "text", Text: "Short answer"}}

	_ = summarizeMessage(4, blocks, "/test", "session-4")

	summary, found := GetSummary("chat_message", 4)
	assert.True(t, found)
	assert.Equal(t, "Short answer", summary)
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
