package service

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// oldLiveNotesSchema is the shape an install that predates the meeting-record
// model has on disk: no started_at / ended_at / dir_name, and none of the
// indexes that cover started_at.
const oldLiveNotesSchema = `
CREATE TABLE IF NOT EXISTS live_notes_segments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	token TEXT NOT NULL,
	project_id INTEGER NOT NULL DEFAULT 0,
	channel TEXT NOT NULL,
	speaker TEXT NOT NULL DEFAULT '',
	started_at INTEGER NOT NULL DEFAULT 0,
	duration_ms INTEGER NOT NULL DEFAULT 0,
	text TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS live_notes (
	token TEXT PRIMARY KEY,
	project_id INTEGER NOT NULL DEFAULT 0,
	title TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'idle',
	minutes TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '',
	file_path TEXT NOT NULL DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	dir_path TEXT NOT NULL DEFAULT '',
	revised TEXT NOT NULL DEFAULT '',
	revise_status TEXT NOT NULL DEFAULT 'idle',
	revise_error TEXT NOT NULL DEFAULT ''
);
`

// TestSchema_LiveNotesMigration_FromOldSchema is the regression guard for a
// startup-killing upgrade: the DDL used to create
// idx_live_notes_project_started(project_id, started_at) in the same batch as
// CREATE TABLE IF NOT EXISTS. On an existing database the table survives
// without the column, so the index creation failed with "no such column:
// started_at" and InitDB returned an error — the server exited and never bound
// its port.
func TestSchema_LiveNotesMigration_FromOldSchema(t *testing.T) {
	tmpDir := t.TempDir()
	origBinDir := model.BinDir
	origDataDir := model.DataDir
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	defer func() { model.BinDir = origBinDir; model.DataDir = origDataDir }()

	dbDir := filepath.Join(tmpDir, ".clawbench")
	require.NoError(t, os.MkdirAll(dbDir, 0o755))
	oldDB, err := sql.Open("sqlite", filepath.Join(dbDir, "ClawBench.db"))
	require.NoError(t, err)
	oldDB.SetMaxOpenConns(1)
	oldDB.Exec("PRAGMA journal_mode=WAL")
	oldDB.Exec("PRAGMA busy_timeout=5000")
	_, err = oldDB.Exec(oldLiveNotesSchema)
	require.NoError(t, err)
	// A pre-record row plus one of its segments: both must be dropped by the
	// one-time cleanup (they belong to meetings the list can never show).
	_, err = oldDB.Exec("INSERT INTO live_notes (token, project_id, status) VALUES ('legacy-share', 1, 'done')")
	require.NoError(t, err)
	_, err = oldDB.Exec("INSERT INTO live_notes_segments (token, project_id, channel, text) VALUES ('legacy-share', 1, 'mix', 'hello')")
	require.NoError(t, err)
	require.NoError(t, oldDB.Close())

	require.NoError(t, InitDB(), "an upgrade must not fail on the live_notes indexes")
	defer store.Close()

	cols := getTableColumns(t, store.UnsafeDBForTest(), "live_notes")
	assert.Contains(t, cols, "started_at")
	assert.Contains(t, cols, "ended_at")
	assert.Contains(t, cols, "dir_name")

	var idx int
	require.NoError(t, store.UnsafeDBForTest().QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_live_notes_project_started'",
	).Scan(&idx))
	assert.Equal(t, 1, idx, "the index must exist once the column it covers has been added")

	var rows int
	require.NoError(t, store.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM live_notes").Scan(&rows))
	assert.Zero(t, rows, "pre-record rows have no start time and must be dropped")
	var segments int
	require.NoError(t, store.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM live_notes_segments").Scan(&segments))
	assert.Zero(t, segments, "segments of dropped rows must be dropped too")

	// Idempotent: the next startup re-runs the same probes.
	assert.NoError(t, InitDB())
}
