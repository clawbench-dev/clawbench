package service

import (
	"database/sql"
	"testing"

	"clawbench/internal/store"

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

	// The row points at project id 1; register that project so the assertion
	// below can read its path back out.
	_, err := db.Exec(
		`INSERT INTO projects (id, path) VALUES (1, '/proj') ON CONFLICT(id) DO NOTHING`,
	)
	require.NoError(t, err)
	_, err = db.Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, archived) VALUES ('s1', 1, 'codebuddy', 't', 1)`,
	)
	require.NoError(t, err)
	token, _, err := UpsertSessionShare("s1", []int64{1})
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

	token, _, err := UpsertSessionShare("orphan", []int64{1})
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

// ─── Error propagation on a broken DB ────────────────────────────────────────

// Every storage helper must surface a driver error rather than reporting a
// clean miss — a revoked/unknown share and a broken DB must not look alike.
func TestSessionShares_ClosedDBSurfacesErrors(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(cleanup)

	_, _, err = GetSessionShareByToken("t")
	require.Error(t, err)

	_, _, err = GetSessionShareBySession("s1")
	require.Error(t, err)

	_, _, err = GetSessionShareProjectByToken("t")
	require.Error(t, err)

	_, _, err = UpsertSessionShare("s1", []int64{1})
	require.Error(t, err)

	err = DeleteSessionShareByToken("t")
	require.Error(t, err)

	err = DeleteSessionShareBySession("s1")
	require.Error(t, err)

	_, err = ListSessionShares("/proj")
	require.Error(t, err)
}

// ─── ListSessionShares ───────────────────────────────────────────────────────

// A row whose message_ids cannot be decoded must fail the list rather than being
// silently skipped or, worse, treated as an empty selection.
func TestListSessionShares_BadMessageIDsSurfaces(t *testing.T) {
	db := setupShareHelperDB(t)
	defer func() { _ = db.Close() }()

	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, archived) VALUES ('s1', 1, 'codebuddy', 't', 0)`,
	)
	require.NoError(t, err)

	_, err = db.Exec(
		`INSERT INTO session_shares (token, session_id, message_ids) VALUES ('t', 's1', '{not json')`,
	)
	require.NoError(t, err)

	_, err = ListSessionShares("/proj")
	require.Error(t, err, "an undecodable row must surface, not vanish from the list")
}
