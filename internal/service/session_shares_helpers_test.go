package service

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file is in package service so it can drive the session-share storage
// helpers against a closed DB (forcing real driver errors) and assert the
// project-ownership lookup that keeps an archived share revocable.

// ─── GetSessionShareProjectByToken ───────────────────────────────────────────

func TestGetSessionShareProjectByToken_EmptyTokenIsAMiss(t *testing.T) {
	path, ok, err := GetSessionShareProjectByToken("")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, path)
}

// The lookup must still resolve an ARCHIVED session's share: a share outlives
// archiving, and an ownership check that skipped archived rows would report a
// live share as unowned and make it unrevocable.
func TestGetSessionShareProjectByToken_ResolvesArchivedSession(t *testing.T) {
	db := setupShareHelperDB(t)
	defer func() { _ = db.Close() }()

	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_path, backend, title, archived) VALUES ('s1', '/proj', 'codebuddy', 't', 1)`,
	)
	require.NoError(t, err)
	token, _, err := UpsertSessionShare("s1", "t", "codebuddy", 1, `{}`)
	require.NoError(t, err)

	path, ok, err := GetSessionShareProjectByToken(token)
	require.NoError(t, err)
	assert.True(t, ok, "an archived session's share must stay owned")
	assert.Equal(t, "/proj", path)
}

func TestGetSessionShareProjectByToken_UnknownTokenIsAMiss(t *testing.T) {
	db := setupShareHelperDB(t)
	defer func() { _ = db.Close() }()

	_, ok, err := GetSessionShareProjectByToken("deadbeef")
	require.NoError(t, err)
	assert.False(t, ok)
}

// A share whose session row is gone resolves to no owner (the JOIN drops it).
func TestGetSessionShareProjectByToken_OrphanShareIsAMiss(t *testing.T) {
	db := setupShareHelperDB(t)
	defer func() { _ = db.Close() }()

	token, _, err := UpsertSessionShare("orphan", "t", "codebuddy", 1, `{}`)
	require.NoError(t, err)

	_, ok, err := GetSessionShareProjectByToken(token)
	require.NoError(t, err)
	assert.False(t, ok)
}

// ─── Delete paths ────────────────────────────────────────────────────────────

func TestDeleteSessionShareByToken_EmptyTokenIsANoop(t *testing.T) {
	db := setupShareHelperDB(t)
	defer func() { _ = db.Close() }()

	require.NoError(t, DeleteSessionShareByToken(""))
}

func TestDeleteSessionShareBySession_EmptyIDIsANoop(t *testing.T) {
	db := setupShareHelperDB(t)
	defer func() { _ = db.Close() }()

	require.NoError(t, DeleteSessionShareBySession(""))
}

// DeleteAllSessionShares is global by design; the handler deliberately does NOT
// use it for clear-all (it would revoke other projects' links), so this test
// pins the documented global behavior.
func TestDeleteAllSessionShares_RemovesEveryProject(t *testing.T) {
	db := setupShareHelperDB(t)
	defer func() { _ = db.Close() }()

	for _, id := range []string{"a", "b"} {
		_, _, err := UpsertSessionShare(id, "t", "codebuddy", 1, `{}`)
		require.NoError(t, err)
	}

	require.NoError(t, DeleteAllSessionShares())

	for _, id := range []string{"a", "b"} {
		_, ok, err := GetSessionShareBySession(id)
		require.NoError(t, err)
		assert.False(t, ok)
	}
}

// ─── Error propagation on a broken DB ────────────────────────────────────────

// Every storage helper must surface a driver error rather than reporting a
// clean miss — a revoked/unknown share and a broken DB must not look alike.
func TestSessionShares_ClosedDBSurfacesErrors(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	cleanup := SetDBForTest(db, db)
	t.Cleanup(cleanup)

	_, _, _, _, err = GetSessionShareByToken("t")
	require.Error(t, err)

	_, _, err = GetSessionShareBySession("s1")
	require.Error(t, err)

	_, _, err = GetSessionShareProjectByToken("t")
	require.Error(t, err)

	_, _, err = UpsertSessionShare("s1", "t", "codebuddy", 1, `{}`)
	require.Error(t, err)

	err = DeleteSessionShareByToken("t")
	require.Error(t, err)

	err = DeleteSessionShareBySession("s1")
	require.Error(t, err)

	err = DeleteSessionSharesBySessionIDs([]string{"s1"})
	require.Error(t, err)

	err = DeleteAllSessionShares()
	require.Error(t, err)

	_, err = ListSessionShares("/proj")
	require.Error(t, err)
}

// ─── ListSessionShares ───────────────────────────────────────────────────────

// A row whose created_at is unreadable by the driver must fail the scan rather
// than being silently skipped.
func TestListSessionShares_ScanErrorSurfaces(t *testing.T) {
	db := setupShareHelperDB(t)
	defer func() { _ = db.Close() }()

	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_path, backend, title, archived) VALUES ('s1', '/proj', 'codebuddy', 't', 0)`,
	)
	require.NoError(t, err)
	_, _, err = UpsertSessionShare("s1", "t", "codebuddy", 1, `{}`)
	require.NoError(t, err)

	// Recreate the table with created_at typed so SQLite cannot coerce it.
	_, err = db.Exec("DROP TABLE session_shares")
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE session_shares (
		token TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		title TEXT NOT NULL DEFAULT '',
		backend TEXT NOT NULL DEFAULT '',
		message_count TEXT NOT NULL DEFAULT 'x',
		payload TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO session_shares (token, session_id, title, backend, message_count, payload)
		VALUES ('t', 's1', 'a', 'b', 'not-a-number', '{}')`)
	require.NoError(t, err)

	_, err = ListSessionShares("/proj")
	require.Error(t, err, "an unscannable row must surface, not vanish from the list")
}
