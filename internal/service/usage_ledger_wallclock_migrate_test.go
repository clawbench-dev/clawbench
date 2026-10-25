package service

import (
	"database/sql"
	"testing"

	"clawbench/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupWallClockDB builds the minimal schema the backfill touches: the ledger
// (with wall_ms) plus the message rows it reads the two sources from.
func setupWallClockDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`
		CREATE TABLE chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			role TEXT NOT NULL DEFAULT 'assistant',
			content TEXT NOT NULL DEFAULT '{}',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			completed_at DATETIME
		);
		CREATE TABLE chat_metadata (
			message_id INTEGER PRIMARY KEY,
			wall_ms INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`)
	require.NoError(t, err)
	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})
	return db
}

// insertWallClockRow seeds one ledger row plus the message it points at. The
// message always starts at a fixed time so only the completion stamp varies.
func insertWallClockRow(t *testing.T, db *sql.DB, msgID int64, content string, completedAt sql.NullString, wallMs int64) {
	t.Helper()
	_, err := db.Exec(
		"INSERT INTO chat_history (id, content, created_at, completed_at) VALUES (?, ?, ?, ?)",
		msgID, content, "2026-01-10 10:00:00", completedAt,
	)
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO chat_metadata (message_id, wall_ms) VALUES (?, ?)", msgID, wallMs)
	require.NoError(t, err)
}

func wallMsOf(t *testing.T, db *sql.DB, msgID int64) int64 {
	t.Helper()
	var ms int64
	require.NoError(t, db.QueryRow("SELECT wall_ms FROM chat_metadata WHERE message_id = ?", msgID).Scan(&ms))
	return ms
}

// The exact source wins: the value the message itself recorded is the same
// number SaveMetadata would have written, so it must not be replaced by the
// (coarser) timestamp derivation.
func TestMigrateLedgerWallClock_PrefersEmbeddedMetadata(t *testing.T) {
	db := setupWallClockDB(t)
	insertWallClockRow(t, db,
		1, `{"metadata":{"wallMs":42000}}`,
		sql.NullString{String: "2026-01-10 10:30:00", Valid: true}, 0)

	require.True(t, MigrateLedgerWallClock())
	// 42s from metadata, NOT the 30 min the timestamps suggest.
	assert.Equal(t, int64(42000), wallMsOf(t, db, 1))
}

// Without embedded metadata the turn's own start/finish stamps are the only
// evidence, and they agree with wall_ms on the overwhelming majority of rows.
func TestMigrateLedgerWallClock_DerivesFromTimestamps(t *testing.T) {
	db := setupWallClockDB(t)
	insertWallClockRow(t, db,
		1, `{"metadata":{"transport":"acp-stdio"}}`,
		sql.NullString{String: "2026-01-10 10:02:30", Valid: true}, 0)

	require.True(t, MigrateLedgerWallClock())
	assert.Equal(t, int64(150_000), wallMsOf(t, db, 1))
}

// An existing value is never overwritten — a recorded runtime is authoritative,
// and re-running the migration must be a no-op.
func TestMigrateLedgerWallClock_KeepsRecordedValue(t *testing.T) {
	db := setupWallClockDB(t)
	insertWallClockRow(t, db,
		1, `{"metadata":{"wallMs":999}}`,
		sql.NullString{String: "2026-01-10 12:00:00", Valid: true}, 7000)

	require.True(t, MigrateLedgerWallClock())
	assert.Equal(t, int64(7000), wallMsOf(t, db, 1), "an already-recorded runtime must survive")
}

// No evidence at all stays 0: an unknown runtime must not be invented, and the
// timestamp fallback must not fire on an implausible delta (here a re-saved row
// whose completion stamp is days after the turn started).
func TestMigrateLedgerWallClock_LeavesUnknownRowsAtZero(t *testing.T) {
	db := setupWallClockDB(t)
	// No completed_at at all.
	insertWallClockRow(t, db, 1, `{}`, sql.NullString{}, 0)
	// A delta beyond the 48h plausibility ceiling.
	insertWallClockRow(t, db, 2, `{}`,
		sql.NullString{String: "2026-01-14 10:00:00", Valid: true}, 0)
	// A zero-length delta (start and finish in the same second): no evidence
	// that any model call happened, so it must not become a 0-length "runtime"
	// that is indistinguishable from a missing one.
	insertWallClockRow(t, db, 3, `{}`,
		sql.NullString{String: "2026-01-10 10:00:00", Valid: true}, 0)

	require.True(t, MigrateLedgerWallClock())
	assert.Equal(t, int64(0), wallMsOf(t, db, 1))
	assert.Equal(t, int64(0), wallMsOf(t, db, 2))
	assert.Equal(t, int64(0), wallMsOf(t, db, 3))
}

// Malformed content must not abort the migration: json_extract RAISES on
// invalid JSON, which would fail the whole UPDATE and leave every row unfixed
// (the runOnce ledger would then retry forever without progress).
func TestMigrateLedgerWallClock_SurvivesMalformedContent(t *testing.T) {
	db := setupWallClockDB(t)
	insertWallClockRow(t, db, 1, `{not json`, sql.NullString{}, 0)
	insertWallClockRow(t, db,
		2, `{"metadata":{"wallMs":1200}}`,
		sql.NullString{}, 0)

	require.True(t, MigrateLedgerWallClock())
	assert.Equal(t, int64(0), wallMsOf(t, db, 1))
	assert.Equal(t, int64(1200), wallMsOf(t, db, 2), "one corrupt row must not block the rest")
}
