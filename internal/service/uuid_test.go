package service

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	_ "modernc.org/sqlite"
)

const uuidTestSchema = `
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
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	last_read_at DATETIME
);
`

func setupUUIDTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(uuidTestSchema); err != nil {
		t.Fatalf("failed to create tables: %v", err)
	}
	cleanup := SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		db.Close()
	})
	return db
}

func TestGenerateUUID_NoPrefix(t *testing.T) {
	setupUUIDTestDB(t)

	id := generateUUID("", "chat_sessions", "id")
	assert.NotEmpty(t, id)
	assert.Len(t, id, 36) // 32 hex + 4 dashes
	assert.Equal(t, 4, strings.Count(id, "-"))
}

func TestGenerateUUID_WithPrefix(t *testing.T) {
	setupUUIDTestDB(t)

	id := generateUUID("prefix-", "chat_sessions", "id")
	assert.NotEmpty(t, id)
	assert.True(t, strings.HasPrefix(id, "prefix-"))
}

func TestGenerateUUID_UniqueIDs(t *testing.T) {
	setupUUIDTestDB(t)

	ids := make(map[string]bool)
	for range 100 {
		id := generateUUID("", "chat_sessions", "id")
		assert.NotEmpty(t, id)
		assert.False(t, ids[id], "generated duplicate ID: %s", id)
		ids[id] = true
	}
}

func TestGenerateUUID_ConflictResolution(t *testing.T) {
	db := setupUUIDTestDB(t)

	// Insert an ID into the table
	id1 := generateUUID("", "chat_sessions", "id")
	assert.NotEmpty(t, id1)
	_, err := db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, '/', 'test', 'test')", id1)
	assert.NoError(t, err)

	// Next ID should be different
	id2 := generateUUID("", "chat_sessions", "id")
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
}

func TestGenerateUUID_ValidUUIDv4Format(t *testing.T) {
	setupUUIDTestDB(t)

	id := generateUUID("", "chat_sessions", "id")
	assert.NotEmpty(t, id)

	// UUID v4: the 13th char (after removing prefix) should be '4'
	// Format: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx where y is 8, 9, a, or b
	parts := strings.Split(id, "-")
	assert.Len(t, parts, 5)
	// Version nibble: 3rd group starts with '4'
	assert.True(t, strings.HasPrefix(parts[2], "4"), "UUID v4 version nibble should be 4")
	// Variant nibble: 4th group starts with 8, 9, a, or b
	variant := parts[3][0]
	assert.Contains(t, []byte{'8', '9', 'a', 'b'}, variant, "UUID v4 variant nibble should be 8/9/a/b")
}
