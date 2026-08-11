package service

import (
	"database/sql"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

// setupTestDBForChatSummary creates an in-memory DB with chat_history and summaries tables.
func setupTestDBForChatSummary(t *testing.T) (*sql.DB, func()) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	db.SetMaxOpenConns(1)
	_, _ = db.Exec("PRAGMA journal_mode=WAL")
	_, _ = db.Exec("PRAGMA busy_timeout=5000")

	// Create minimal tables needed for enrichMessagesWithSummaries
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			project_path TEXT NOT NULL,
			backend TEXT NOT NULL,
			title TEXT NOT NULL,
			archived INTEGER NOT NULL DEFAULT 0
		);
	`)
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_path TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			files TEXT,
			session_id TEXT,
			backend TEXT NOT NULL DEFAULT 'claude',
			streaming INTEGER NOT NULL DEFAULT 0,
			indexed INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	_, _ = db.Exec(`
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

	cleanup := SetDBForTest(db, db)
	teardown := func() {
		cleanup()
		db.Close()
	}
	return db, teardown
}

func TestEnrichMessagesWithSummaries_NoAssistantMessages(t *testing.T) {
	_, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	// Only user messages — no enrichment needed
	messages := []model.ChatMessage{
		{ID: 1, Role: "user", Content: "hello"},
		{ID: 2, Role: "user", Content: "world"},
	}
	enrichMessagesWithSummaries(messages, false)
	assert.Nil(t, messages[0].Summary)
	assert.Nil(t, messages[1].Summary)
}

func TestEnrichMessagesWithSummaries_WithSummary(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	// Save a summary for assistant message ID 10
	_, err := db.Exec("INSERT INTO summaries (target_type, target_id, summary) VALUES ('chat_message', 10, '这是摘要')")
	assert.NoError(t, err)

	messages := []model.ChatMessage{
		{ID: 5, Role: "user", Content: "question"},
		{ID: 10, Role: "assistant", Content: "long answer"},
	}
	enrichMessagesWithSummaries(messages, false)
	assert.Nil(t, messages[0].Summary)
	assert.NotNil(t, messages[1].Summary)
	assert.Equal(t, "这是摘要", *messages[1].Summary)
}

func TestEnrichMessagesWithSummaries_NoSummarySaved(t *testing.T) {
	_, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	// No summary in DB for message ID 20
	messages := []model.ChatMessage{
		{ID: 20, Role: "assistant", Content: "answer without summary"},
	}
	enrichMessagesWithSummaries(messages, false)
	assert.Nil(t, messages[0].Summary)
}

func TestEnrichMessagesWithSummaries_EmptySummary(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	// Empty summary means text was too short
	_, err := db.Exec("INSERT INTO summaries (target_type, target_id, summary) VALUES ('chat_message', 30, '')")
	assert.NoError(t, err)

	messages := []model.ChatMessage{
		{ID: 30, Role: "assistant", Content: "short"},
	}
	enrichMessagesWithSummaries(messages, false)
	assert.NotNil(t, messages[0].Summary)
	assert.Equal(t, "", *messages[0].Summary)
}

func TestEnrichMessagesWithSummaries_MultipleAssistantMessages(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	// Save summaries for two assistant messages
	_, err := db.Exec("INSERT INTO summaries (target_type, target_id, summary) VALUES ('chat_message', 40, '摘要一')")
	assert.NoError(t, err)
	_, err = db.Exec("INSERT INTO summaries (target_type, target_id, summary) VALUES ('chat_message', 42, '摘要二')")
	assert.NoError(t, err)

	messages := []model.ChatMessage{
		{ID: 39, Role: "user", Content: "q1"},
		{ID: 40, Role: "assistant", Content: "a1"},
		{ID: 41, Role: "user", Content: "q2"},
		{ID: 42, Role: "assistant", Content: "a2"},
	}
	enrichMessagesWithSummaries(messages, false)
	assert.Nil(t, messages[0].Summary)
	assert.NotNil(t, messages[1].Summary)
	assert.Equal(t, "摘要一", *messages[1].Summary)
	assert.Nil(t, messages[2].Summary)
	assert.NotNil(t, messages[3].Summary)
	assert.Equal(t, "摘要二", *messages[3].Summary)
}

func TestEnrichMessagesWithSummaries_DifferentTargetType(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	// Save summary with different target type — should NOT match
	_, err := db.Exec("INSERT INTO summaries (target_type, target_id, summary) VALUES ('task_execution', 50, 'task summary')")
	assert.NoError(t, err)

	messages := []model.ChatMessage{
		{ID: 50, Role: "assistant", Content: "answer"},
	}
	enrichMessagesWithSummaries(messages, false)
	assert.Nil(t, messages[0].Summary) // Different target_type, should not match
}

// --- triggerChatSummarization ---

func TestTriggerChatSummarization_ExtractsLastAnswer(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	// Insert session + messages
	sessionID := "test-simple-session"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, '/test', 'claude', 'test')", sessionID)
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (100, '/test', 'user', 'hello', ?, 0)", sessionID)
	assistantContent := `{"blocks":[{"type":"text","text":"Let me check."},{"type":"tool_use","name":"Bash","id":"t1"},{"type":"text","text":"The answer is 42."}]}`
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (101, '/test', 'assistant', ?, ?, 0)", assistantContent, sessionID)

	triggerChatSummarization(sessionID)

	// Always-extract: should have saved the last text block (conclusion) as summary
	summary, found := GetSummary("chat_message", 101)
	assert.True(t, found)
	assert.Equal(t, "The answer is 42.", summary)
}

func TestTriggerChatSummarization_NoTextAfterToolUse(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	sessionID := "test-simple-notext"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, '/test', 'claude', 'test')", sessionID)
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (110, '/test', 'user', 'hello', ?, 0)", sessionID)
	assistantContent := `{"blocks":[{"type":"tool_use","name":"Bash","id":"t1"}]}`
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (111, '/test', 'assistant', ?, ?, 0)", assistantContent, sessionID)

	triggerChatSummarization(sessionID)

	// No text block at all → no summary saved
	_, found := GetSummary("chat_message", 111)
	assert.False(t, found)
}
