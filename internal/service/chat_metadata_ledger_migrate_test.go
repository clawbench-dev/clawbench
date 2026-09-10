package service

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestDBForLedgerMigration creates an in-memory DB with the LEGACY
// chat_metadata schema (foreign key → chat_history ON DELETE CASCADE) plus the
// denormalized attribution columns already present, mirroring the state right
// before migrateChatMetadataLedger runs in InitDB.
func setupTestDBForLedgerMigration(t *testing.T) func() {
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
			agent_id TEXT DEFAULT '',
			archived INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
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
		CREATE TABLE IF NOT EXISTS chat_metadata (
			message_id INTEGER PRIMARY KEY,
			mode TEXT DEFAULT '',
			thinking_effort TEXT DEFAULT '',
			transport TEXT DEFAULT '',
			model TEXT DEFAULT '',
			input_tokens INTEGER DEFAULT 0,
			output_tokens INTEGER DEFAULT 0,
			duration_ms INTEGER DEFAULT 0,
			wall_ms INTEGER DEFAULT 0,
			cost_usd REAL DEFAULT 0,
			stop_reason TEXT DEFAULT '',
			is_error INTEGER DEFAULT 0,
			error_message TEXT DEFAULT '',
			cached_read_tokens INTEGER DEFAULT 0,
			cached_write_tokens INTEGER DEFAULT 0,
			thought_tokens INTEGER DEFAULT 0,
			total_tokens INTEGER DEFAULT 0,
			cache_creation_tokens INTEGER DEFAULT 0,
			cache_hit_tokens INTEGER DEFAULT 0,
			cache_miss_tokens INTEGER DEFAULT 0,
			credit REAL DEFAULT 0,
			usage_by_category TEXT DEFAULT '',
			session_id TEXT DEFAULT '',
			request_id TEXT DEFAULT '',
			trace_id TEXT DEFAULT '',
			agent_message_id TEXT DEFAULT '',
			message_request_id TEXT DEFAULT '',
			request_model_name TEXT DEFAULT '',
			response_model_id TEXT DEFAULT '',
			finish_reason TEXT DEFAULT '',
			outcome TEXT DEFAULT '',
			agent_phase TEXT DEFAULT '',
			project_path TEXT DEFAULT '',
			backend TEXT DEFAULT '',
			agent_id TEXT DEFAULT '',
			clawbench_session_id TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (message_id) REFERENCES chat_history(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_chat_metadata_model ON chat_metadata(model);
		CREATE INDEX IF NOT EXISTS idx_chat_metadata_created ON chat_metadata(created_at);
	`)
	if err != nil {
		t.Fatalf("failed to create tables: %v", err)
	}
	cleanup := SetDBForTest(db, db)
	return func() { cleanup(); db.Close() }
}

// TestMigrateChatMetadataLedger_RemovesFKAndBackfills covers the core migration:
// the cascade FK is dropped (so usage survives message deletion) and the
// denormalized attribution is backfilled from the still-present rows.
func TestMigrateChatMetadataLedger_RemovesFKAndBackfills(t *testing.T) {
	teardown := setupTestDBForLedgerMigration(t)
	defer teardown()

	_, err := db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title, agent_id) VALUES ('s1', '/proj', 'codebuddy', 'T', 'codebuddy')")
	require.NoError(t, err)
	res, err := db.Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming) VALUES ('/proj', 'codebuddy', 's1', 'assistant', '{}', 0)",
	)
	require.NoError(t, err)
	msgID, _ := res.LastInsertId()

	// Legacy row: attribution columns still empty.
	_, err = db.Exec("INSERT INTO chat_metadata (message_id, model, total_tokens) VALUES (?, 'glm', 42)", msgID)
	require.NoError(t, err)

	var fkBefore int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM pragma_foreign_key_list('chat_metadata')").Scan(&fkBefore))
	require.Equal(t, 1, fkBefore, "legacy schema must start with the cascade FK")

	require.NoError(t, migrateChatMetadataLedger())

	var fkAfter int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM pragma_foreign_key_list('chat_metadata')").Scan(&fkAfter))
	assert.Equal(t, 0, fkAfter, "FK must be removed so the ledger is standalone")

	var project, backend, agentID, clawSID string
	require.NoError(t, db.QueryRow(
		"SELECT project_path, backend, agent_id, clawbench_session_id FROM chat_metadata WHERE message_id = ?", msgID,
	).Scan(&project, &backend, &agentID, &clawSID))
	assert.Equal(t, "/proj", project)
	assert.Equal(t, "codebuddy", backend)
	assert.Equal(t, "codebuddy", agentID)
	assert.Equal(t, "s1", clawSID)

	// Row data preserved through the table rebuild.
	var total int64
	require.NoError(t, db.QueryRow("SELECT total_tokens FROM chat_metadata WHERE message_id = ?", msgID).Scan(&total))
	assert.Equal(t, int64(42), total)

	// The ledger row now survives deletion of its message.
	_, err = db.Exec("DELETE FROM chat_history WHERE id = ?", msgID)
	require.NoError(t, err)
	var remaining int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM chat_metadata WHERE message_id = ?", msgID).Scan(&remaining))
	assert.Equal(t, 1, remaining, "ledger row must survive message deletion after migration")

	// Indexes recreated on the rebuilt table.
	var idx int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_chat_metadata_project_created'").Scan(&idx))
	assert.Equal(t, 1, idx)
}

// TestMigrateChatMetadataLedger_Idempotent asserts a second run is a no-op and
// does not disturb already-backfilled rows.
func TestMigrateChatMetadataLedger_Idempotent(t *testing.T) {
	teardown := setupTestDBForLedgerMigration(t)
	defer teardown()

	_, err := db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title, agent_id) VALUES ('s1', '/proj', 'claude', 'T', 'agent-x')")
	require.NoError(t, err)
	res, err := db.Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming) VALUES ('/proj', 'claude', 's1', 'assistant', '{}', 0)",
	)
	require.NoError(t, err)
	msgID, _ := res.LastInsertId()
	_, err = db.Exec("INSERT INTO chat_metadata (message_id, total_tokens) VALUES (?, 7)", msgID)
	require.NoError(t, err)

	require.NoError(t, migrateChatMetadataLedger())
	require.NoError(t, migrateChatMetadataLedger())

	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM chat_metadata").Scan(&count))
	assert.Equal(t, 1, count, "second run must not duplicate rows")

	var agentID string
	require.NoError(t, db.QueryRow("SELECT agent_id FROM chat_metadata WHERE message_id = ?", msgID).Scan(&agentID))
	assert.Equal(t, "agent-x", agentID)
}

// TestMigrateChatMetadataLedger_NoTableIsNoop covers the early return when
// chat_metadata does not exist (fresh DB before createTables).
func TestMigrateChatMetadataLedger_NoTableIsNoop(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	cleanup := SetDBForTest(db, db)
	defer func() { cleanup(); db.Close() }()

	assert.NoError(t, migrateChatMetadataLedger())
}

// TestMigrateChatMetadataLedger_SkipsBackfillWhenAttributionPresent covers the
// early return when the table is already standalone AND every row already
// carries attribution (a plain second startup).
func TestMigrateChatMetadataLedger_SkipsBackfillWhenAttributionPresent(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`
		CREATE TABLE chat_metadata (
			message_id INTEGER PRIMARY KEY,
			model TEXT DEFAULT '',
			total_tokens INTEGER DEFAULT 0,
			project_path TEXT DEFAULT '',
			backend TEXT DEFAULT '',
			agent_id TEXT DEFAULT '',
			clawbench_session_id TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_path TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			session_id TEXT,
			backend TEXT NOT NULL DEFAULT 'claude',
			streaming INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	require.NoError(t, err)
	// Already attributed — nothing to backfill.
	_, err = db.Exec("INSERT INTO chat_metadata (message_id, project_path, backend, agent_id, clawbench_session_id) VALUES (1, '/proj', 'claude', 'a', 's1')")
	require.NoError(t, err)

	cleanup := SetDBForTest(db, db)
	defer func() { cleanup(); db.Close() }()

	assert.NoError(t, migrateChatMetadataLedger())

	var project string
	require.NoError(t, db.QueryRow("SELECT project_path FROM chat_metadata WHERE message_id = 1").Scan(&project))
	assert.Equal(t, "/proj", project, "already-attributed row must be untouched")
}

// TestMigrateChatMetadataLedger_OrphanRowsStayEmpty covers rows whose
// chat_history parent is already gone: they cannot be backfilled and must not
// be dropped by the rebuild.
func TestMigrateChatMetadataLedger_OrphanRowsStayEmpty(t *testing.T) {
	teardown := setupTestDBForLedgerMigration(t)
	defer teardown()

	// Insert a metadata row with no matching chat_history row. The legacy
	// schema enforces the FK, so temporarily disable enforcement to simulate a
	// row orphaned before enforcement existed.
	_, err := db.Exec("PRAGMA foreign_keys = OFF")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO chat_metadata (message_id, total_tokens) VALUES (9999, 11)")
	require.NoError(t, err)
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	require.NoError(t, migrateChatMetadataLedger())

	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM chat_metadata WHERE message_id = 9999").Scan(&count))
	assert.Equal(t, 1, count, "orphan ledger row must survive the rebuild")

	var project string
	require.NoError(t, db.QueryRow("SELECT project_path FROM chat_metadata WHERE message_id = 9999").Scan(&project))
	assert.Empty(t, project, "orphan cannot be attributed")
}

// TestMigrateChatMetadataLedger_EmptyHistoryProjectNotRescanned guards the
// idempotency of the backfill: a row whose chat_history.project_path is itself
// empty cannot be attributed, so the predicate must exclude it rather than match
// it again on every startup.
func TestMigrateChatMetadataLedger_EmptyHistoryProjectNotRescanned(t *testing.T) {
	teardown := setupTestDBForLedgerMigration(t)
	defer teardown()

	res, err := db.Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming) VALUES ('', 'claude', 's1', 'assistant', '{}', 0)",
	)
	require.NoError(t, err)
	msgID, _ := res.LastInsertId()
	_, err = db.Exec("INSERT INTO chat_metadata (message_id, total_tokens) VALUES (?, 3)", msgID)
	require.NoError(t, err)

	require.NoError(t, migrateChatMetadataLedger())

	var project string
	require.NoError(t, db.QueryRow("SELECT project_path FROM chat_metadata WHERE message_id = ?", msgID).Scan(&project))
	assert.Empty(t, project, "unattributable row stays empty")

	// The backfill predicate must now exclude this row (h.project_path == '').
	var stillNeeds int
	require.NoError(t, db.QueryRow(`
		SELECT COUNT(*) FROM chat_metadata m
		WHERE m.project_path = ''
		  AND EXISTS (SELECT 1 FROM chat_history h WHERE h.id = m.message_id AND h.project_path != '')
	`).Scan(&stillNeeds))
	assert.Equal(t, 0, stillNeeds, "empty-project row must not be rescanned on later startups")
}
