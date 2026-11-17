package service_test

import (
	"database/sql"
	"testing"

	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRevokeAllShares_IsAtomic: the three stores are independent tables, so a
// failure on the last one must undo the first two. Otherwise "sharing is off"
// would be true for files and false for meetings — the exact state the one
// transaction exists to prevent, and one the user cannot see or repair.
func TestRevokeAllShares_IsAtomic(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	// live_notes is DELIBERATELY missing: the meeting step fails after the two
	// deletes have already run inside the transaction.
	for _, ddl := range []string{store.ProjectsDDL, service.FileSharesDDL, service.SessionSharesDDL} {
		_, err := db.Exec(ddl)
		require.NoError(t, err)
	}
	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(cleanup)

	_, err = db.Exec(`INSERT INTO file_shares (token, path, name, root, project_id) VALUES ('ftoken', '/p/a.md', 'a.md', '/p', 0)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO session_shares (token, session_id, message_ids) VALUES ('stoken', 'sess-1', '[1]')`)
	require.NoError(t, err)

	_, err = service.RevokeAllShares()
	require.ErrorContains(t, err, "revoke meeting shares")

	var files, sessions int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM file_shares`).Scan(&files))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM session_shares`).Scan(&sessions))
	require.Equal(t, 1, files, "a failed revoke must not delete file links on its own")
	require.Equal(t, 1, sessions, "a failed revoke must not delete conversation links on its own")
}

// TestRevokeAllShares_ReportsEveryKindIndependently: the PATCH response tells the
// user what was revoked, and "0" versus "everything" must not look identical.
// Meetings are FLAGGED, not deleted, so they are counted separately.
func TestRevokeAllShares_ReportsEveryKindIndependently(t *testing.T) {
	db := setupShareSwitchDB(t)
	defer func() { _ = db.Close() }()

	// Two file links, none of the others.
	for _, token := range []string{"f1", "f2"} {
		_, err := db.Exec(
			`INSERT INTO file_shares (token, path, name, root, project_id) VALUES (?, '/p/a.md', 'a.md', '/p', 0)`, token,
		)
		require.NoError(t, err)
	}
	// A meeting that was never published must not be counted as revoked.
	_, err := db.Exec(`INSERT INTO live_notes (token, project_id, title, status, share_enabled) VALUES ('unpublished', 1, 'M', 'ready', 0)`)
	require.NoError(t, err)

	res, err := service.RevokeAllShares()
	require.NoError(t, err)
	assert.EqualValues(t, 2, res.FileShares)
	assert.Zero(t, res.SessionShares)
	assert.Zero(t, res.MeetingShares, "an unpublished meeting has nothing to revoke")

	// The meeting record itself survives: only the public flag is cleared, so the
	// owner still reads it in the meeting list.
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM live_notes WHERE token = 'unpublished'`).Scan(&count))
	assert.Equal(t, 1, count)
}
