package service

import (
	"database/sql"
	"path/filepath"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newGroupSchemaDB initializes a fresh DB (full schema via InitDB) for the
// group-chat schema tests and returns a cleanup function. Mirrors
// database_test.go's TestSchema_* pattern.
func newGroupSchemaDB(t *testing.T) {
	t.Helper()
	tmpDir := t.TempDir()
	origBinDir := model.BinDir
	origDataDir := model.DataDir
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	t.Cleanup(func() {
		model.BinDir = origBinDir
		model.DataDir = origDataDir
	})

	restoreDB := store.SnapshotDBForTest()
	t.Cleanup(restoreDB)

	if err := InitDB(); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { store.Close() })
}

func TestChatHistoryHasAgentIDColumn(t *testing.T) {
	newGroupSchemaDB(t)
	cols := getTableColumns(t, store.UnsafeDBForTest(), "chat_history")
	if !cols["agent_id"] {
		t.Fatalf("chat_history.agent_id column missing (cols=%v)", cols)
	}
}

func TestChatSessionsHasGroupIDColumn(t *testing.T) {
	newGroupSchemaDB(t)
	cols := getTableColumns(t, store.UnsafeDBForTest(), "chat_sessions")
	if !cols["group_id"] {
		t.Fatalf("chat_sessions.group_id column missing (cols=%v)", cols)
	}
}

func TestGroupIDMigrationIdempotent(t *testing.T) {
	// Running InitDB twice must not fail on the already-present columns.
	newGroupSchemaDB(t)
	if err := InitDB(); err != nil {
		t.Fatalf("second InitDB: %v", err)
	}
}

// chatHistoryRoleSQL reads the stored CREATE TABLE statement for chat_history.
// It is the authoritative shape of the live table (sqlite_master), unlike a
// copied DDL in a test.
func chatHistoryRoleSQL(t *testing.T) string {
	t.Helper()
	var ddl string
	if err := store.UnsafeDBForTest().QueryRow(
		"SELECT sql FROM sqlite_master WHERE type='table' AND name='chat_history'",
	).Scan(&ddl); err != nil {
		t.Fatalf("read chat_history DDL: %v", err)
	}
	return ddl
}

// TestChatHistoryRoleCheck_AllowsSystem verifies a fresh database (created by
// InitDB) accepts a role='system' row end to end: the CHECK constraint admits
// it, and the ordinary read path (GetChatHistoryPaged) returns it.
func TestChatHistoryRoleCheck_AllowsSystem(t *testing.T) {
	newGroupSchemaDB(t)

	project := "/tmp/grouprole"
	sid, err := CreateSession(project, "codebuddy", "role-test", "", "", "default", "chat")
	require.NoError(t, err)

	// The fresh-schema DDL must already carry 'system' — no migration needed.
	assert.Contains(t, chatHistoryRoleSQL(t), "'system'",
		"fresh chat_history must accept role='system'")

	id, err := AddChatMessage(project, "codebuddy", sid, "system", "成员 A 加入了讨论", nil, false, "")
	require.NoError(t, err, "role='system' insert must succeed")

	msgs, _, err := GetChatHistoryPaged(project, "codebuddy", sid, 0, 0)
	require.NoError(t, err)
	found := false
	for _, m := range msgs {
		if m.ID == id {
			found = true
			assert.Equal(t, "system", m.Role)
			assert.Equal(t, "成员 A 加入了讨论", m.Content)
		}
	}
	assert.True(t, found, "the system event must be readable via GetChatHistoryPaged")
}

// TestRebuildChatHistoryRoleCheck_MigratesLegacyAndPreservesData is the core
// migration test: a database whose chat_history still has the old two-value
// CHECK is upgraded in place.
//
// It drives the REAL production path — build the current schema, replace the
// table with the legacy CHECK to simulate an old database, run InitDB again —
// rather than hand-writing the rebuild here, so deleting the production
// migration makes this test fail.
//
// It also pins the trap that makes a naive rebuild wrong: DROP TABLE
// chat_history fires the ON DELETE CASCADE of chat_thinking / chat_tool_calls
// while PRAGMA foreign_keys=ON, silently destroying every thought and tool
// call. The child rows must survive.
func TestRebuildChatHistoryRoleCheck_MigratesLegacyAndPreservesData(t *testing.T) {
	tmpDir := t.TempDir()
	origBinDir, origDataDir := model.BinDir, model.DataDir
	model.BinDir, model.DataDir = tmpDir, filepath.Join(tmpDir, ".clawbench")
	defer func() { model.BinDir, model.DataDir = origBinDir, origDataDir }()

	restoreDB := store.SnapshotDBForTest()
	defer restoreDB()

	require.NoError(t, InitDB())
	store.Close()

	// Phase 1: rebuild chat_history with the LEGACY CHECK (no 'system') while
	// keeping the exact production column set, and seed a parent row with both
	// kinds of child rows.
	raw, err := sql.Open("sqlite", filepath.Join(model.DataDir, "ClawBench.db"))
	require.NoError(t, err)
	_, err = raw.Exec("PRAGMA foreign_keys=OFF")
	require.NoError(t, err)
	_, err = raw.Exec("DROP TABLE chat_history")
	require.NoError(t, err)
	_, err = raw.Exec(`
		CREATE TABLE chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			role TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
			content TEXT NOT NULL,
			files TEXT,
			session_id TEXT,
			backend TEXT NOT NULL DEFAULT 'claude',
			agent_id TEXT DEFAULT '',
			streaming INTEGER NOT NULL DEFAULT 0,
			indexed INTEGER NOT NULL DEFAULT 0,
			external_message_id TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			completed_at DATETIME
		)`)
	require.NoError(t, err)
	_, err = raw.Exec("PRAGMA foreign_keys=ON")
	require.NoError(t, err)

	_, err = raw.Exec("INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('legacy-s', 1, 'claude', 'Legacy')")
	require.NoError(t, err)
	res, err := raw.Exec("INSERT INTO chat_history (project_id, role, content, session_id, streaming) VALUES (1, 'assistant', 'old reply', 'legacy-s', 0)")
	require.NoError(t, err)
	msgID, _ := res.LastInsertId()
	_, err = raw.Exec("INSERT INTO chat_thinking (message_id, session_id, think_id, seq, text) VALUES (?, 'legacy-s', 'tk1', 0, 'reasoning')", msgID)
	require.NoError(t, err)
	_, err = raw.Exec("INSERT INTO chat_tool_calls (message_id, session_id, tool_id, name, input, output, status, done) VALUES (?, 'legacy-s', 'tc1', 'Read', '{}', 'out', 'completed', 1)", msgID)
	require.NoError(t, err)
	// The legacy CHECK must reject 'system' before the migration.
	_, err = raw.Exec("INSERT INTO chat_history (project_id, role, content, session_id, streaming) VALUES (1, 'system', 'x', 'legacy-s', 0)")
	require.Error(t, err, "legacy CHECK must reject role='system' (otherwise the test proves nothing)")
	raw.Close()

	// Phase 2: InitDB upgrades the table in place.
	require.NoError(t, InitDB())
	defer store.Close()

	assert.Contains(t, chatHistoryRoleSQL(t), "'system'",
		"InitDB must rebuild chat_history so role='system' is accepted")

	// The migrated row is intact.
	var content string
	require.NoError(t, store.UnsafeDBForTest().QueryRow(
		"SELECT content FROM chat_history WHERE id = ?", msgID).Scan(&content))
	assert.Equal(t, "old reply", content)

	// The child rows must survive: a naive DROP TABLE with FK ON cascades.
	var thinkCount, toolCount int
	require.NoError(t, store.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_thinking").Scan(&thinkCount))
	require.NoError(t, store.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_tool_calls").Scan(&toolCount))
	assert.Equal(t, 1, thinkCount, "chat_thinking rows must survive the rebuild")
	assert.Equal(t, 1, toolCount, "chat_tool_calls rows must survive the rebuild")

	// All chat_history indexes are recreated on the renamed table.
	for _, idx := range []string{
		"idx_history_session", "idx_history_session_id", "idx_history_unread",
		"idx_history_sess_unread", "idx_history_indexing",
	} {
		var n int
		require.NoError(t, store.UnsafeDBForTest().QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?", idx).Scan(&n))
		assert.Equal(t, 1, n, "index %s must be recreated after the rebuild", idx)
	}

	// role='system' now works.
	_, err = AddChatMessage("/p", "claude", "legacy-s", "system", "成员 A 加入了讨论", nil, false, "")
	require.NoError(t, err, "role='system' must be accepted after the migration")

	// The foreign key cascade must be restored (FK was turned off only for the
	// rebuild): deleting the parent still removes its children.
	_, err = store.UnsafeDBForTest().Exec("DELETE FROM chat_history WHERE id = ?", msgID)
	require.NoError(t, err)
	require.NoError(t, store.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_thinking").Scan(&thinkCount))
	assert.Equal(t, 0, thinkCount, "FK cascade must be re-enabled after the rebuild")
}

// TestRebuildChatHistoryRoleCheck_Idempotent verifies a second InitDB on an
// already-upgraded database is a no-op (the guard sees 'system' in the DDL and
// skips the rebuild), and that rows written between the two runs are untouched.
func TestRebuildChatHistoryRoleCheck_Idempotent(t *testing.T) {
	newGroupSchemaDB(t)

	project := "/tmp/grouproleidem"
	sid, err := CreateSession(project, "codebuddy", "role-idem", "", "", "default", "chat")
	require.NoError(t, err)
	id, err := AddChatMessage(project, "codebuddy", sid, "system", "event", nil, false, "")
	require.NoError(t, err)

	require.NoError(t, InitDB())

	var content string
	require.NoError(t, store.UnsafeDBForTest().QueryRow(
		"SELECT content FROM chat_history WHERE id = ?", id).Scan(&content))
	assert.Equal(t, "event", content, "idempotent rerun must not touch rows")
	assert.Contains(t, chatHistoryRoleSQL(t), "'system'")
}

// TestRebuildChatHistoryRoleCheck_QueuedMigrationOrdering guards the ordering
// trap: the rebuild copies an explicit column list. If it ran BEFORE
// migrateQueuedMessagesToOwnTable, the rebuilt table would no longer carry the
// legacy queue_id/queued columns and that migration's SELECT would fail —
// permanently losing every in-flight queued message on the upgrade.
func TestRebuildChatHistoryRoleCheck_QueuedMigrationOrdering(t *testing.T) {
	tmpDir := t.TempDir()
	origBinDir, origDataDir := model.BinDir, model.DataDir
	model.BinDir, model.DataDir = tmpDir, filepath.Join(tmpDir, ".clawbench")
	defer func() { model.BinDir, model.DataDir = origBinDir, origDataDir }()

	restoreDB := store.SnapshotDBForTest()
	defer restoreDB()

	require.NoError(t, InitDB())
	store.Close()

	// Legacy shape: old CHECK plus the queue_id/queued columns the queued
	// migration consumes.
	raw, err := sql.Open("sqlite", filepath.Join(model.DataDir, "ClawBench.db"))
	require.NoError(t, err)
	_, err = raw.Exec("PRAGMA foreign_keys=OFF")
	require.NoError(t, err)
	_, err = raw.Exec("DROP TABLE chat_history")
	require.NoError(t, err)
	_, err = raw.Exec(`
		CREATE TABLE chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			role TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
			content TEXT NOT NULL,
			files TEXT,
			session_id TEXT,
			backend TEXT NOT NULL DEFAULT 'claude',
			agent_id TEXT DEFAULT '',
			streaming INTEGER NOT NULL DEFAULT 0,
			queue_id TEXT DEFAULT '',
			queued INTEGER NOT NULL DEFAULT 0,
			indexed INTEGER NOT NULL DEFAULT 0,
			external_message_id TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			completed_at DATETIME
		)`)
	require.NoError(t, err)
	_, err = raw.Exec("PRAGMA foreign_keys=ON")
	require.NoError(t, err)
	_, err = raw.Exec("INSERT INTO chat_history (project_id, role, content, session_id, backend, queue_id, queued, streaming) VALUES (1, 'user', 'queued body', 'qs', 'claude', 'q-1', 1, 0)")
	require.NoError(t, err)
	raw.Close()

	require.NoError(t, InitDB())
	defer store.Close()

	// The queued message must have been moved, not lost.
	var n int
	require.NoError(t, store.UnsafeDBForTest().QueryRow(
		"SELECT COUNT(*) FROM queued_messages WHERE queue_id = 'q-1' AND content = 'queued body'").Scan(&n))
	assert.Equal(t, 1, n, "queued rows must survive the role-check rebuild ordering")

	// The dead columns are gone, and the CHECK is relaxed.
	cols := getTableColumns(t, store.UnsafeDBForTest(), "chat_history")
	assert.False(t, cols["queued"], "queued column must be dropped by the queued migration")
	assert.Contains(t, chatHistoryRoleSQL(t), "'system'")
}
