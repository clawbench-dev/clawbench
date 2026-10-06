package service

import (
	"database/sql"
	"encoding/json"
	"testing"

	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
)

func setupTestDBForThinkingMigration(t *testing.T) func() {
	t.Helper()
	memDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory memDB: %v", err)
	}
	memDB.SetMaxOpenConns(1)
	memDB.Exec("PRAGMA journal_mode=WAL")
	memDB.Exec("PRAGMA busy_timeout=5000")
	memDB.Exec("PRAGMA foreign_keys = ON")

	_, err = memDB.Exec(`
		CREATE TABLE IF NOT EXISTS projects (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	path TEXT NOT NULL,
	forge_bind_opt_out INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(path)
);
CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL,
			backend TEXT NOT NULL,
			title TEXT NOT NULL,
			session_type TEXT NOT NULL DEFAULT 'chat',
			group_id TEXT DEFAULT '',
			archived INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(backend, id)
		);
		CREATE TABLE IF NOT EXISTS chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			role TEXT NOT NULL CHECK(role IN ('user', 'assistant', 'system')),
			content TEXT NOT NULL,
			session_id TEXT,
			backend TEXT NOT NULL DEFAULT 'claude',
			agent_id TEXT DEFAULT '',
			streaming INTEGER NOT NULL DEFAULT 0,
			queue_id TEXT DEFAULT '',
			queued INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			completed_at DATETIME
		);
		CREATE TABLE IF NOT EXISTS chat_thinking (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id INTEGER NOT NULL REFERENCES chat_history(id) ON DELETE CASCADE,
			session_id TEXT NOT NULL,
			think_id TEXT NOT NULL,
			seq INTEGER NOT NULL DEFAULT 0,
			text TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(think_id, message_id, seq)
		);
		CREATE INDEX IF NOT EXISTS idx_thinking_message ON chat_thinking(message_id);
		CREATE INDEX IF NOT EXISTS idx_thinking_session ON chat_thinking(session_id, created_at DESC);
	`)
	if err != nil {
		t.Fatalf("failed to create tables: %v", err)
	}
	cleanup := store.SetDBForTest(memDB, memDB)
	return func() { cleanup(); memDB.Close() }
}

func TestMigrateThinkingFromContent_ExtractsThinking(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()

	_, err := store.UnsafeDBForTest().Exec("INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('sess-1', 1, 'claude', 'Test')")
	assert.NoError(t, err)

	oldContent := `{
		"blocks": [
			{"type": "text", "text": "I'll check."},
			{"type": "thinking", "text": "internal reasoning", "done": true},
			{"type": "text", "text": "Result"}
		]
	}`
	res, err := store.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 0)",
		"/proj", oldContent, "sess-1",
	)
	assert.NoError(t, err)
	msgID, _ := res.LastInsertId()

	MigrateThinkingFromContent()

	var count int
	err = store.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_thinking WHERE message_id = ?", msgID).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 1, count)

	var thinkID, text string
	err = store.UnsafeDBForTest().QueryRow("SELECT think_id, text FROM chat_thinking WHERE message_id = ?", msgID).Scan(&thinkID, &text)
	assert.NoError(t, err)
	assert.NotEmpty(t, thinkID)
	assert.Equal(t, "internal reasoning", text)

	var newContent string
	err = store.UnsafeDBForTest().QueryRow("SELECT content FROM chat_history WHERE id = ?", msgID).Scan(&newContent)
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

	_, err := store.UnsafeDBForTest().Exec("INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('sess-1', 1, 'claude', 'Test')")
	assert.NoError(t, err)
	oldContent := `{"blocks":[{"type":"thinking","text":"old","done":true},{"type":"text","text":"ok"}]}`
	_, err = store.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 0)",
		"/proj", oldContent, "sess-1",
	)
	assert.NoError(t, err)

	MigrateThinkingFromContent()
	MigrateThinkingFromContent()

	var count int
	store.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_thinking").Scan(&count)
	assert.Equal(t, 1, count, "second run must be idempotent")

	// Streaming message must be skipped.
	_, err = store.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 1)",
		"/proj", oldContent, "sess-1",
	)
	assert.NoError(t, err)
	MigrateThinkingFromContent()
	store.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_thinking").Scan(&count)
	assert.Equal(t, 1, count, "streaming message must be skipped")
}

func TestMigrateThinkingFromContent_EmptyTextThinkingSlimmed(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()

	_, err := store.UnsafeDBForTest().Exec("INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('sess-1', 1, 'claude', 'Test')")
	assert.NoError(t, err)
	oldContent := `{"blocks":[{"type":"thinking","done":true}]}`
	res, err := store.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 0)",
		"/proj", oldContent, "sess-1",
	)
	assert.NoError(t, err)
	msgID, _ := res.LastInsertId()

	MigrateThinkingFromContent()

	var count int
	store.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_thinking WHERE message_id = ?", msgID).Scan(&count)
	assert.Equal(t, 0, count, "empty-text thinking should not create a row")

	var newContent string
	err = store.UnsafeDBForTest().QueryRow("SELECT content FROM chat_history WHERE id = ?", msgID).Scan(&newContent)
	assert.NoError(t, err)
	assert.Contains(t, newContent, "think_id")
	assert.NotContains(t, newContent, "\"text\"")

	// Second run must be a no-op (row now has think_id → excluded).
	MigrateThinkingFromContent()
	store.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_thinking").Scan(&count)
	assert.Equal(t, 0, count)
}

func TestMigrateThinkingFromContent_UpsertFailureKeepsContent(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()

	_, err := store.UnsafeDBForTest().Exec("INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('sess-1', 1, 'claude', 'Test')")
	assert.NoError(t, err)
	oldContent := `{"blocks":[{"type":"thinking","text":"doomed","done":true}]}`
	_, err = store.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 0)",
		"/proj", oldContent, "sess-1",
	)
	assert.NoError(t, err)
	// Keep chat_thinking readable (NOT EXISTS count still works) but make every
	// INSERT fail, forcing the migration's tx.Exec path to error out.
	_, err = store.UnsafeDBForTest().Exec(`
		CREATE TRIGGER trg_thinking_fail BEFORE INSERT ON chat_thinking
		BEGIN SELECT RAISE(FAIL, 'forced failure'); END;
	`)
	assert.NoError(t, err)

	MigrateThinkingFromContent()

	var content string
	err = store.UnsafeDBForTest().QueryRow("SELECT content FROM chat_history WHERE session_id = 'sess-1'").Scan(&content)
	assert.NoError(t, err)
	assert.Contains(t, content, "\"text\":\"doomed\"", "content must stay full when upsert fails")
}

// TestMigrateThinkingFromContent_MoreThanOneBatch migrates more rows than one
// batch (batchSize=200) and asserts EVERY old-format row is migrated. Guards
// against OFFSET pagination over a shrinking result set skipping rows: as rows
// are slimmed they leave the query result set, so a fixed OFFSET drifts ahead
// and permanently skips a batch's worth of rows.
func TestMigrateThinkingFromContent_MoreThanOneBatch(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()

	_, err := store.UnsafeDBForTest().Exec("INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('sess-1', 1, 'claude', 'Test')")
	assert.NoError(t, err)
	oldContent := `{"blocks":[{"type":"thinking","text":"thought","done":true}]}`

	const total = 450
	for range total {
		_, err = store.UnsafeDBForTest().Exec(
			"INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 0)",
			"/proj", oldContent, "sess-1",
		)
		assert.NoError(t, err)
	}

	MigrateThinkingFromContent()

	var count int
	err = store.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_thinking").Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, total, count, "every old-format row must be migrated, none skipped by OFFSET pagination")

	var unmigrated int
	err = store.UnsafeDBForTest().QueryRow(`
		SELECT COUNT(*) FROM chat_history
		WHERE role = 'assistant' AND content LIKE '%"type":"thinking"%' AND content NOT LIKE '%think_id%'
	`).Scan(&unmigrated)
	assert.NoError(t, err)
	assert.Equal(t, 0, unmigrated, "no old-format thinking rows may remain after migration")
}
