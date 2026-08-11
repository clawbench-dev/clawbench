package service

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

// setupTestDBForTriggerSummary creates an in-memory DB with all tables needed for triggerChatSummarization.
func setupTestDBForTriggerSummary(t *testing.T) (*sql.DB, func()) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	db.SetMaxOpenConns(1)
	_, _ = db.Exec("PRAGMA journal_mode=WAL")
	_, _ = db.Exec("PRAGMA busy_timeout=5000")

	_, err = db.Exec(`
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
			transport TEXT DEFAULT '',
			auto_approve INTEGER NOT NULL DEFAULT 0,
			context_state TEXT DEFAULT '',
			archived INTEGER NOT NULL DEFAULT 0,
			last_read_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(project_path, backend, id)
		);
		CREATE TABLE IF NOT EXISTS summaries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			target_type TEXT NOT NULL,
			target_id   INTEGER NOT NULL,
			summary     TEXT NOT NULL,
			summary_cards TEXT NOT NULL DEFAULT '',
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(target_type, target_id)
		);
		CREATE INDEX IF NOT EXISTS idx_history_session ON chat_history(project_path, backend, session_id, created_at);
		CREATE INDEX IF NOT EXISTS idx_sessions_project_backend ON chat_sessions(project_path, backend);
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

func TestTriggerChatSummarization_NoMessages(t *testing.T) {
	_, teardown := setupTestDBForTriggerSummary(t)
	defer teardown()

	// Session doesn't exist in DB — should return with no error
	triggerChatSummarization("nonexistent-session")
}

func TestTriggerChatSummarization_NoAssistantMessages(t *testing.T) {
	db, teardown := setupTestDBForTriggerSummary(t)
	defer teardown()

	// Create session and user message only
	_, err := db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES ('sess-1', '/test', 'claude', 'Test')")
	assert.NoError(t, err)
	_, err = db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES ('/test', 'user', 'hello', 'sess-1', 'claude')")
	assert.NoError(t, err)

	// No assistant message — should return without saving a summary
	triggerChatSummarization("sess-1")
}

func TestTriggerChatSummarization_AlreadySummarized(t *testing.T) {
	db, teardown := setupTestDBForTriggerSummary(t)
	defer teardown()

	// Create session with assistant message
	_, err := db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES ('sess-2', '/test', 'claude', 'Test')")
	assert.NoError(t, err)

	content, _ := json.Marshal(map[string]any{
		"blocks": []any{map[string]any{"type": "text", "text": strings.Repeat("这是一段较长的AI回复内容。", 30)}},
	})
	_, err = db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES ('/test', 'assistant', ?, 'sess-2', 'claude')", string(content))
	assert.NoError(t, err)

	// Get the message ID
	var msgID int64
	db.QueryRow("SELECT id FROM chat_history WHERE session_id = 'sess-2' AND role = 'assistant'").Scan(&msgID)

	// Pre-save a summary
	err = SaveSummary("chat_message", msgID, "already summarized")
	assert.NoError(t, err)

	// Should skip summarization since already summarized
	triggerChatSummarization("sess-2")

	// Original summary preserved
	summary, found := GetSummary("chat_message", msgID)
	assert.True(t, found)
	assert.Equal(t, "already summarized", summary)
}

func TestTriggerChatSummarization_EmptyBlocks(t *testing.T) {
	db, teardown := setupTestDBForTriggerSummary(t)
	defer teardown()

	// Create session with assistant message that has no blocks
	_, err := db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES ('sess-3', '/test', 'claude', 'Test')")
	assert.NoError(t, err)

	content, _ := json.Marshal(map[string]any{"blocks": []any{}})
	_, err = db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES ('/test', 'assistant', ?, 'sess-3', 'claude')", string(content))
	assert.NoError(t, err)

	// Should return since blocks are empty
	triggerChatSummarization("sess-3")
}

func TestTriggerChatSummarization_InvalidJSON(t *testing.T) {
	db, teardown := setupTestDBForTriggerSummary(t)
	defer teardown()

	// Create session with assistant message that has invalid JSON content
	_, err := db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES ('sess-4', '/test', 'claude', 'Test')")
	assert.NoError(t, err)
	_, err = db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES ('/test', 'assistant', 'not valid json', 'sess-4', 'claude')")
	assert.NoError(t, err)

	// Should return on JSON parse error without panicking
	triggerChatSummarization("sess-4")
}

func TestTriggerChatSummarization_Success(t *testing.T) {
	db, teardown := setupTestDBForTriggerSummary(t)
	defer teardown()

	// Create session with assistant message containing long text
	_, err := db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES ('sess-5', '/test', 'claude', 'Test')")
	assert.NoError(t, err)

	longText := strings.Repeat("这是一段较长的AI回复内容。", 30)
	content, _ := json.Marshal(map[string]any{
		"blocks": []any{map[string]any{"type": "text", "text": longText}},
	})
	_, err = db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES ('/test', 'assistant', ?, 'sess-5', 'claude')", string(content))
	assert.NoError(t, err)

	// Trigger summarization
	triggerChatSummarization("sess-5")

	// Always-extract: the full answer text is saved directly as the summary
	var msgID int64
	db.QueryRow("SELECT id FROM chat_history WHERE session_id = 'sess-5' AND role = 'assistant'").Scan(&msgID)

	summary, found := GetSummary("chat_message", msgID)
	assert.True(t, found)
	assert.Equal(t, longText, summary)
}

// --- summarizeTarget (shared entry point for chat + scheduled tasks) ---

func TestSummarizeTarget_ShortTextGetsSummary(t *testing.T) {
	_, teardown := setupTestDBForTriggerSummary(t)
	defer teardown()

	// Always-extract has no threshold — even a short answer is saved.
	blocks := []model.ContentBlock{{Type: "text", Text: "Short answer"}}

	summarizeTarget("chat_message", 1001, blocks, "/test", "sess-sum")

	summary, found := GetSummary("chat_message", 1001)
	assert.True(t, found, "always-extract should save a summary even for short text")
	assert.Equal(t, "Short answer", summary)
}

func TestSummarizeTarget_EmptyTextSkips(t *testing.T) {
	_, teardown := setupTestDBForTriggerSummary(t)
	defer teardown()

	// No text blocks → no summary saved
	summarizeTarget("chat_message", 1002, []model.ContentBlock{{Type: "tool_use", Text: "read_file"}}, "/test", "sess-sum2")

	_, found := GetSummary("chat_message", 1002)
	assert.False(t, found, "no text block should produce no summary")
}

func TestSummarizeTarget_ExtractsConclusion(t *testing.T) {
	_, teardown := setupTestDBForTriggerSummary(t)
	defer teardown()

	conclusion := "这是最终结论内容，比较长。"
	blocks := []model.ContentBlock{
		{Type: "text", Text: "让我检查一下..."},
		{Type: "tool_use", Text: "read_file"},
		{Type: "text", Text: conclusion},
	}

	summarizeTarget("chat_message", 1003, blocks, "/test", "sess-sum3")

	summary, found := GetSummary("chat_message", 1003)
	assert.True(t, found, "always-extract should save the conclusion")
	assert.Equal(t, conclusion, summary, "should use the extracted last answer")
}
