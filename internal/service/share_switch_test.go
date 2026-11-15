package service_test

import (
	"database/sql"
	"testing"

	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupShareSwitchDB creates the three share stores the master switch must
// clear. They are independent tables, so a partial revoke is the failure this
// fixture is designed to expose.
func setupShareSwitchDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)

	for _, ddl := range []string{store.ProjectsDDL, service.FileSharesDDL, service.SessionSharesDDL, service.LiveNotesDDL} {
		_, err = db.Exec(ddl)
		require.NoError(t, err)
	}

	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(cleanup)
	return db
}

func TestRevokeAllShares_RevokesAllThreeKinds(t *testing.T) {
	db := setupShareSwitchDB(t)
	defer func() { _ = db.Close() }()

	_, err := db.Exec(
		`INSERT INTO file_shares (token, path, name, root, project_id) VALUES ('ftoken', '/p/a.md', 'a.md', '/p', 0)`)
	require.NoError(t, err)
	_, err = db.Exec(
		`INSERT INTO session_shares (token, session_id, message_ids) VALUES ('stoken', 'sess-1', '[1,2]')`)
	require.NoError(t, err)
	_, err = db.Exec(
		`INSERT INTO live_notes (token, project_id, title, status, share_enabled) VALUES ('mtoken', 1, 'Meeting', 'ready', 1)`)
	require.NoError(t, err)

	res, err := service.RevokeAllShares()
	require.NoError(t, err)
	assert.EqualValues(t, 1, res.FileShares)
	assert.EqualValues(t, 1, res.SessionShares)
	assert.EqualValues(t, 1, res.MeetingShares)

	var fileCount, sessionCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM file_shares`).Scan(&fileCount))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM session_shares`).Scan(&sessionCount))
	assert.Zero(t, fileCount, "file share rows must be deleted")
	assert.Zero(t, sessionCount, "session share rows must be deleted")

	var shareEnabled int
	require.NoError(t, db.QueryRow(`SELECT share_enabled FROM live_notes WHERE token = 'mtoken'`).Scan(&shareEnabled))
	assert.Zero(t, shareEnabled, "meeting publication flag must be cleared")
}

// Revoking is idempotent and reports zero when there is nothing to revoke, so a
// repeated "off" PATCH (the settings panel sends the whole panel on every save)
// is harmless.
func TestRevokeAllShares_NoopWhenEmpty(t *testing.T) {
	db := setupShareSwitchDB(t)
	defer func() { _ = db.Close() }()

	res, err := service.RevokeAllShares()
	require.NoError(t, err)
	assert.Zero(t, res.FileShares)
	assert.Zero(t, res.SessionShares)
	assert.Zero(t, res.MeetingShares)
}
