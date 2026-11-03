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
	// A pre-record row plus one of its segments. They belong to a meeting the
	// list cannot place on a timeline; the migration must HIDE them, never
	// delete them — the transcript is not recoverable from the Markdown
	// artifacts (those are only written on success and the user may have
	// removed the folder).
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
	assert.Contains(t, cols, "hidden")

	var idx int
	require.NoError(t, store.UnsafeDBForTest().QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_live_notes_project_started'",
	).Scan(&idx))
	assert.Equal(t, 1, idx, "the index must exist once the column it covers has been added")

	// The row survives, hidden: an upgrade must not destroy the user's data.
	var rows int
	require.NoError(t, store.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM live_notes").Scan(&rows))
	assert.Equal(t, 1, rows, "pre-record rows must be kept, not dropped")
	var hidden int
	require.NoError(t, store.UnsafeDBForTest().QueryRow(
		"SELECT hidden FROM live_notes WHERE token = 'legacy-share'",
	).Scan(&hidden))
	assert.Equal(t, 1, hidden, "pre-record rows must be hidden from the list")

	var segments int
	require.NoError(t, store.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM live_notes_segments").Scan(&segments))
	assert.Equal(t, 1, segments, "the transcript of a pre-record row must be preserved")

	// End to end: the hidden row is invisible to the meeting list (the whole
	// point of hiding rather than deleting), and a direct lookup by token
	// behaves as "not found" too.
	records, err := ListMeetingRecords(tmpDir, 1, 0)
	require.NoError(t, err)
	assert.Empty(t, records, "a pre-record row must not appear in the meeting list")
	_, found, err := GetMeetingRecord(tmpDir, 1, "legacy-share")
	require.NoError(t, err)
	assert.False(t, found, "a hidden row must read as not found")

	// Idempotent: the next startup re-runs the same probes.
	assert.NoError(t, InitDB())
	var stillHidden int
	require.NoError(t, store.UnsafeDBForTest().QueryRow(
		"SELECT hidden FROM live_notes WHERE token = 'legacy-share'",
	).Scan(&stillHidden))
	assert.Equal(t, 1, stillHidden)

	// The row is still there after the second startup, just hidden.
	require.NoError(t, store.UnsafeDBForTest().QueryRow(
		"SELECT COUNT(*) FROM live_notes_segments",
	).Scan(&segments))
	assert.Equal(t, 1, segments)
}

// TestSchema_LiveNotesMigration_KeepsLiveRowsVisible is the counterweight to the
// test above: started_at = 0 is ALSO the value of a share whose publisher has
// not attached yet (EnsureLiveNote inserts the row before the room starts). The
// one-time hide must not run again on a database that already has started_at,
// or a restart during a live meeting would hide it.
func TestSchema_LiveNotesMigration_KeepsLiveRowsVisible(t *testing.T) {
	tmpDir := t.TempDir()
	origBinDir := model.BinDir
	origDataDir := model.DataDir
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	defer func() { model.BinDir = origBinDir; model.DataDir = origDataDir }()

	require.NoError(t, InitDB())
	defer store.Close()

	// A share that exists but has no start time yet: the publisher is still
	// negotiating.
	_, err := store.WriteExec(
		"INSERT INTO live_notes (token, project_id, status, started_at) VALUES ('live-share', 1, 'idle', 0)",
	)
	require.NoError(t, err)

	require.NoError(t, InitDB())

	var hidden int
	require.NoError(t, store.UnsafeDBForTest().QueryRow(
		"SELECT hidden FROM live_notes WHERE token = 'live-share'",
	).Scan(&hidden))
	assert.Zero(t, hidden, "a row created by the current model must stay visible")
}

// TestSchema_LiveNotesMigration_BackfillsArtifactStates is the guard for the
// upgrade that introduced live_notes_artifacts: the record list reads the new
// table, so an existing install's finished minutes/proofread must be visible
// there without waiting for the next generation.
func TestSchema_LiveNotesMigration_BackfillsArtifactStates(t *testing.T) {
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
	_, err = oldDB.Exec(`INSERT INTO live_notes
		(token, project_id, status, minutes, file_path, revised, revise_status, updated_at)
		VALUES ('done-share', 1, 'ready', '## 纪要', '/p/minutes.md', '校对稿', 'ready', '2026-01-02 03:04:05')`)
	require.NoError(t, err)
	_, err = oldDB.Exec(`INSERT INTO live_notes
		(token, project_id, status, updated_at)
		VALUES ('crashed-share', 1, 'generating', '2026-01-02 03:04:05')`)
	require.NoError(t, err)
	require.NoError(t, oldDB.Close())

	require.NoError(t, InitDB())
	defer store.Close()

	states, err := ArtifactStatesForToken("done-share")
	require.NoError(t, err)
	assert.Equal(t, ArtifactJobIdle, states[ArtifactMinutes].JobStatus)
	assert.NotZero(t, states[ArtifactMinutes].ContentAt, "a finished minutes row must keep its content time")
	assert.Equal(t, ArtifactJobIdle, states[ArtifactRevised].JobStatus)
	assert.NotZero(t, states[ArtifactRevised].ContentAt)

	crashed, err := ArtifactStatesForToken("crashed-share")
	require.NoError(t, err)
	assert.Equal(t, ArtifactJobFailed, crashed[ArtifactMinutes].JobStatus)
	assert.Equal(t, ArtifactErrInterrupted, crashed[ArtifactMinutes].ErrorCode,
		"a job that was running before the upgrade cannot still be alive")

	// The backfill must not resurrect an advanced state on the next startup.
	require.NoError(t, SetArtifactSucceeded("done-share", ArtifactMinutes, 12345, 12345))
	require.NoError(t, InitDB())
	states, err = ArtifactStatesForToken("done-share")
	require.NoError(t, err)
	assert.Equal(t, int64(12345), states[ArtifactMinutes].ContentAt,
		"INSERT OR IGNORE must leave the live row alone")
}
