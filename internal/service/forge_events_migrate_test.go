package service

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedLegacyForgeEvents creates a database file whose forge_events table predates
// item_key, and returns its path.
//
// This reproduces a real upgrade: a live install already has event rows written
// before the column existed, so the migration must reconstruct their keys. A row
// left with an empty key is invisible to the badge AND impossible to clear by
// opening anything — a permanently stuck state.
func seedLegacyForgeEvents(t *testing.T, dataDir string, rows [][2]any) {
	t.Helper()
	path := filepath.Join(dataDir, "ClawBench.db")
	legacy, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer legacy.Close()

	_, err = legacy.Exec(`
CREATE TABLE forge_events (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	platform   TEXT NOT NULL,
	host       TEXT NOT NULL,
	owner      TEXT NOT NULL,
	repo       TEXT NOT NULL,
	item_type  TEXT NOT NULL,
	number     INTEGER NOT NULL,
	event_type TEXT NOT NULL,
	dedupe_key TEXT NOT NULL,
	payload    TEXT NOT NULL DEFAULT '',
	read_at    DATETIME,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_forge_events_dedupe ON forge_events(dedupe_key);`)
	require.NoError(t, err)

	for _, r := range rows {
		itemType, dedupe := r[0].(string), r[1].(string)
		_, err := legacy.Exec(
			`INSERT INTO forge_events (platform, host, owner, repo, item_type, number, event_type, dedupe_key)
			 VALUES ('github','github.com','acme','widgets',?,0,'pipeline_done',?)`,
			itemType, dedupe)
		require.NoError(t, err)
	}
}

// TestMigration_ForgeEventsItemKeyBackfill verifies the additive migration that
// gives pre-existing event rows an item_key.
func TestMigration_ForgeEventsItemKeyBackfill(t *testing.T) {
	dataDir := withTempDataDir(t)
	t.Cleanup(CloseDB)

	seedLegacyForgeEvents(t, dataDir, [][2]any{
		{"pipeline", "github|github.com|acme|widgets|0|pipeline|pipeline_done|run:555"},
		// A malformed pipeline row with no run id: the key cannot be recovered.
		{"pipeline", "github|github.com|acme|widgets|0|pipeline|pipeline_done|state:success"},
	})

	require.NoError(t, InitDB())
	t.Cleanup(CloseDB)

	keys := map[string]string{}
	readState := map[string]bool{}
	rows, err := db.Query(`SELECT dedupe_key, item_key, read_at FROM forge_events`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var dedupe, key string
		var readAt sql.NullTime
		require.NoError(t, rows.Scan(&dedupe, &key, &readAt))
		keys[dedupe] = key
		readState[dedupe] = readAt.Valid
	}
	require.NoError(t, rows.Err())

	assert.Equal(t, "pipeline/run:555",
		keys["github|github.com|acme|widgets|0|pipeline|pipeline_done|run:555"],
		"the run id must be recovered from the dedupe key")

	// The unparseable row is retired (marked read) rather than left unread with
	// no key, which would be a phantom item the user could never clear.
	assert.Equal(t, "", keys["github|github.com|acme|widgets|0|pipeline|pipeline_done|state:success"])
	assert.True(t, readState["github|github.com|acme|widgets|0|pipeline|pipeline_done|state:success"],
		"a keyless row must be marked read")
}

// TestMigration_ForgeEventsItemKeyIdempotent: re-running InitDB must not fail and
// must not retire rows that were written after the migration.
func TestMigration_ForgeEventsItemKeyIdempotent(t *testing.T) {
	withTempDataDir(t)
	t.Cleanup(CloseDB)

	require.NoError(t, InitDB())
	t.Cleanup(CloseDB)

	var n int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('forge_events') WHERE name='item_key'`).Scan(&n))
	require.Equal(t, 1, n)

	// A live unread row.
	_, err := WriteExec(
		`INSERT INTO forge_events (platform, host, owner, repo, item_type, number, item_key, event_type, dedupe_key)
		 VALUES ('github','github.com','acme','widgets','issue',1,'issue/1','opened','k')`)
	require.NoError(t, err)

	// Second boot.
	require.NoError(t, InitDB())

	var unread int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM forge_events WHERE read_at IS NULL AND item_key = 'issue/1'`).Scan(&unread))
	assert.Equal(t, 1, unread, "re-running the migration must not retire live unread rows")
}

// TestMigration_TaskUnreadBackfill: unread became per-execution, which REMOVED a
// suppression the task-level watermark used to provide. Without a backfill, a
// long-lived task would report every run it ever made as unread on upgrade.
//
// The migration must mark exactly the set the watermark was suppressing — not
// the executions that were already unread, and not running ones.
func TestMigration_TaskUnreadBackfill(t *testing.T) {
	withTempDataDir(t)
	t.Cleanup(CloseDB)

	// Build the pre-migration state by hand, then run InitDB over it.
	require.NoError(t, InitDB())
	CloseDB()

	path := filepath.Join(model.DataDir, "ClawBench.db")
	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)

	watermark := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	_, err = raw.Exec(
		`INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, last_read_at, created_at, updated_at)
		 VALUES ('/proj','Task','0 * * * *','agent1','p','','active','unlimited',?,?,?)`,
		watermark, watermark, watermark)
	require.NoError(t, err)
	var taskID int64
	require.NoError(t, raw.QueryRow("SELECT id FROM scheduled_tasks LIMIT 1").Scan(&taskID))

	insertExec := func(session, status string, read bool, createdAt time.Time) {
		t.Helper()
		var readAt any
		if read {
			readAt = createdAt
		}
		_, err := raw.Exec(
			`INSERT INTO task_executions (task_id, session_id, trigger_type, status, read_at, created_at)
			 VALUES (?, ?, 'auto', ?, ?, ?)`,
			taskID, session, status, readAt, createdAt)
		require.NoError(t, err)
	}
	before := watermark.Add(-2 * time.Hour)
	after := watermark.Add(2 * time.Hour)

	insertExec("suppressed", "completed", false, before) // watermark hid this
	insertExec("already-read", "completed", true, before)
	insertExec("after-watermark", "completed", false, after) // was already unread
	insertExec("still-running", "running", false, before)
	require.NoError(t, raw.Close())

	// Migrate.
	require.NoError(t, InitDB())

	read := func(session string) bool {
		t.Helper()
		var n int
		require.NoError(t, db.QueryRow(
			"SELECT COUNT(*) FROM task_executions WHERE session_id = ? AND read_at IS NOT NULL",
			session).Scan(&n))
		return n == 1
	}

	assert.True(t, read("suppressed"),
		"the watermark-suppressed execution must be backfilled as read, or the badge explodes on upgrade")
	assert.True(t, read("already-read"), "an already-read execution stays read")
	assert.False(t, read("after-watermark"),
		"an execution after the watermark was already unread and must stay unread")
	assert.False(t, read("still-running"), "a running execution is never marked read")

	// The watermark is retired, which is what makes the backfill one-time.
	var watermarkLeft int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM scheduled_tasks WHERE last_read_at IS NOT NULL").Scan(&watermarkLeft))
	assert.Equal(t, 0, watermarkLeft, "the retired watermark must be cleared")

	// And the surviving unread is exactly the after-watermark execution.
	var unread int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM task_executions WHERE read_at IS NULL AND status != 'running'").Scan(&unread))
	assert.Equal(t, 1, unread)
}
