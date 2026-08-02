package service

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func setupTestDBForThinkingMigration(t *testing.T) func() {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")
	db.Exec("PRAGMA foreign_keys = ON")

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			project_path TEXT NOT NULL,
			backend TEXT NOT NULL,
			title TEXT NOT NULL,
			session_type TEXT NOT NULL DEFAULT 'chat',
			archived INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(project_path, backend, id)
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
		CREATE TABLE IF NOT EXISTS chat_thinking (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id INTEGER NOT NULL REFERENCES chat_history(id) ON DELETE CASCADE,
			session_id TEXT NOT NULL,
			think_id TEXT NOT NULL,
			text TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(think_id, message_id)
		);
		CREATE INDEX IF NOT EXISTS idx_thinking_message ON chat_thinking(message_id);
		CREATE INDEX IF NOT EXISTS idx_thinking_session ON chat_thinking(session_id, created_at DESC);
	`)
	if err != nil {
		t.Fatalf("failed to create tables: %v", err)
	}
	cleanup := SetDBForTest(db, db)
	return func() { cleanup(); db.Close() }
}

func TestMigrateThinkingFromContent_ExtractsThinking(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()

	_, err := db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES ('sess-1', '/proj', 'claude', 'Test')")
	assert.NoError(t, err)

	oldContent := `{
		"blocks": [
			{"type": "text", "text": "I'll check."},
			{"type": "thinking", "text": "internal reasoning", "done": true},
			{"type": "text", "text": "Result"}
		]
	}`
	res, err := db.Exec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 0)",
		"/proj", oldContent, "sess-1",
	)
	assert.NoError(t, err)
	msgID, _ := res.LastInsertId()

	MigrateThinkingFromContent()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM chat_thinking WHERE message_id = ?", msgID).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 1, count)

	var thinkID, text string
	err = db.QueryRow("SELECT think_id, text FROM chat_thinking WHERE message_id = ?", msgID).Scan(&thinkID, &text)
	assert.NoError(t, err)
	assert.NotEmpty(t, thinkID)
	assert.Equal(t, "internal reasoning", text)

	var newContent string
	err = db.QueryRow("SELECT content FROM chat_history WHERE id = ?", msgID).Scan(&newContent)
	assert.NoError(t, err)
	var parsed struct {
		Blocks []json.RawMessage `json:"blocks"`
	}
	json.Unmarshal([]byte(newContent), &parsed)
	assert.Len(t, parsed.Blocks, 3)
	var thinkBlock map[string]any
	json.Unmarshal(parsed.Blocks[1], &thinkBlock)
	assert.Equal(t, "thinking", thinkBlock["type"])
	assert.Equal(t, thinkID, thinkBlock["think_id"])
	_, hasText := thinkBlock["text"]
	assert.False(t, hasText)
}

func TestMigrateThinkingFromContent_IdempotentAndSkipsSlim(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()

	_, err := db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES ('sess-1', '/proj', 'claude', 'Test')")
	assert.NoError(t, err)
	oldContent := `{"blocks":[{"type":"thinking","text":"old","done":true},{"type":"text","text":"ok"}]}`
	_, err = db.Exec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 0)",
		"/proj", oldContent, "sess-1",
	)
	assert.NoError(t, err)

	MigrateThinkingFromContent()
	MigrateThinkingFromContent()

	var count int
	db.QueryRow("SELECT COUNT(*) FROM chat_thinking").Scan(&count)
	assert.Equal(t, 1, count, "second run must be idempotent")

	// Streaming message must be skipped.
	_, err = db.Exec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 1)",
		"/proj", oldContent, "sess-1",
	)
	assert.NoError(t, err)
	MigrateThinkingFromContent()
	db.QueryRow("SELECT COUNT(*) FROM chat_thinking").Scan(&count)
	assert.Equal(t, 1, count, "streaming message must be skipped")
}
