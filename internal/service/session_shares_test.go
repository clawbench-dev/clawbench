package service_test

import (
	"database/sql"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupTestDBForSessionShares creates an in-memory SQLite with the
// session_shares table plus the chat_sessions table it is listed against.
//
// chat_sessions is needed because ListSessionShares JOINs it to scope by
// project and to report whether the session is archived.
func setupTestDBForSessionShares(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)

	_, err = db.Exec(service.SessionSharesDDL)
	require.NoError(t, err)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			project_path TEXT NOT NULL,
			backend TEXT NOT NULL,
			title TEXT NOT NULL,
			archived INTEGER NOT NULL DEFAULT 0
		);`)
	require.NoError(t, err)

	cleanup := service.SetDBForTest(db, db)
	t.Cleanup(cleanup)
	return db
}

// seedSession inserts a chat_sessions row so a share can be joined to it.
func seedListedSession(t *testing.T, db *sql.DB, id, projectPath string, archived bool) {
	t.Helper()
	arch := 0
	if archived {
		arch = 1
	}
	_, err := db.Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, archived) VALUES (?, ?, ?, ?, ?)",
		id, projectPath, "codebuddy", "t", arch,
	)
	require.NoError(t, err)
}

func TestSessionShares_UpsertCreatesNewToken(t *testing.T) {
	db := setupTestDBForSessionShares(t)
	defer func() { _ = db.Close() }()

	token, created, err := service.UpsertSessionShare("sess-1", "My chat", "codebuddy", 4, `{"version":1}`)
	require.NoError(t, err)
	assert.True(t, created)
	assert.Len(t, token, 32, "token is 32 hex chars (128 bits)")

	payload, title, count, ok, err := service.GetSessionShareByToken(token)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, `{"version":1}`, payload)
	assert.Equal(t, "My chat", title)
	assert.Equal(t, 4, count)
}

// Re-sharing a session must invalidate the previous link: a conversation keeps
// growing, so the old frozen snapshot is stale the moment a new one is taken.
func TestSessionShares_UpsertRotatesToken(t *testing.T) {
	db := setupTestDBForSessionShares(t)
	defer func() { _ = db.Close() }()

	token1, created, err := service.UpsertSessionShare("sess-1", "t", "codebuddy", 1, `{"v":1}`)
	require.NoError(t, err)
	assert.True(t, created)

	token2, created, err := service.UpsertSessionShare("sess-1", "t", "codebuddy", 2, `{"v":2}`)
	require.NoError(t, err)
	assert.False(t, created, "second upsert for the same session is a rotation")
	assert.NotEqual(t, token1, token2)

	_, _, _, ok, err := service.GetSessionShareByToken(token1)
	require.NoError(t, err)
	assert.False(t, ok, "old token must be revoked after rotation")

	payload, _, count, ok, err := service.GetSessionShareByToken(token2)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, `{"v":2}`, payload)
	assert.Equal(t, 2, count)
}

func TestSessionShares_GetBySession(t *testing.T) {
	db := setupTestDBForSessionShares(t)
	defer func() { _ = db.Close() }()

	_, ok, err := service.GetSessionShareBySession("nope")
	require.NoError(t, err)
	assert.False(t, ok)

	token, _, err := service.UpsertSessionShare("sess-1", "t", "codebuddy", 1, `{}`)
	require.NoError(t, err)

	got, ok, err := service.GetSessionShareBySession("sess-1")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, token, got)
}

// An unknown or empty token must be a clean miss, never an error: the public
// endpoint turns it into a uniform 404.
func TestSessionShares_GetByTokenUnknownIsNotAnError(t *testing.T) {
	db := setupTestDBForSessionShares(t)
	defer func() { _ = db.Close() }()

	for _, token := range []string{"", "deadbeef"} {
		payload, title, count, ok, err := service.GetSessionShareByToken(token)
		require.NoError(t, err, "token=%q", token)
		assert.False(t, ok)
		assert.Empty(t, payload)
		assert.Empty(t, title)
		assert.Zero(t, count)
	}
}

func TestSessionShares_DeleteBySession(t *testing.T) {
	db := setupTestDBForSessionShares(t)
	defer func() { _ = db.Close() }()

	token, _, err := service.UpsertSessionShare("sess-1", "t", "codebuddy", 1, `{}`)
	require.NoError(t, err)

	require.NoError(t, service.DeleteSessionShareBySession("sess-1"))

	_, _, _, ok, err := service.GetSessionShareByToken(token)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestSessionShares_DeleteBySessionIDs(t *testing.T) {
	db := setupTestDBForSessionShares(t)
	defer func() { _ = db.Close() }()

	for _, id := range []string{"s1", "s2", "s3"} {
		_, _, err := service.UpsertSessionShare(id, "t", "codebuddy", 1, `{}`)
		require.NoError(t, err)
	}

	require.NoError(t, service.DeleteSessionSharesBySessionIDs([]string{"s1", "s3"}))

	_, ok, err := service.GetSessionShareBySession("s1")
	require.NoError(t, err)
	assert.False(t, ok)

	_, ok, err = service.GetSessionShareBySession("s2")
	require.NoError(t, err)
	assert.True(t, ok, "s2 was not in the purge list and must survive")

	// An empty list is a no-op, not a "delete everything".
	require.NoError(t, service.DeleteSessionSharesBySessionIDs(nil))
	_, ok, err = service.GetSessionShareBySession("s2")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestSessionShares_ListEmptyIsNonNil(t *testing.T) {
	db := setupTestDBForSessionShares(t)
	defer func() { _ = db.Close() }()

	shares, err := service.ListSessionShares("/proj")
	require.NoError(t, err)
	assert.NotNil(t, shares, "must be non-nil so JSON encodes as []")
	assert.Empty(t, shares)
}

func TestSessionShares_ListNewestFirst(t *testing.T) {
	db := setupTestDBForSessionShares(t)
	defer func() { _ = db.Close() }()

	seedListedSession(t, db, "s1", "/proj", false)
	seedListedSession(t, db, "s2", "/proj", false)
	_, _, err := service.UpsertSessionShare("s1", "first", "codebuddy", 1, `{}`)
	require.NoError(t, err)
	_, _, err = service.UpsertSessionShare("s2", "second", "claude", 2, `{}`)
	require.NoError(t, err)

	shares, err := service.ListSessionShares("/proj")
	require.NoError(t, err)
	require.Len(t, shares, 2)
	assert.Equal(t, "s2", shares[0].SessionID, "newest first")
	assert.Equal(t, "second", shares[0].Title)
	assert.Equal(t, "claude", shares[0].Backend)
	assert.Equal(t, 2, shares[0].MessageCount)
}

// A conversation title is private content: listing another project's shares
// would disclose it. The list is therefore project-scoped, unlike the file list.
func TestSessionShares_ListIsProjectScoped(t *testing.T) {
	db := setupTestDBForSessionShares(t)
	defer func() { _ = db.Close() }()

	seedListedSession(t, db, "mine", "/proj-a", false)
	seedListedSession(t, db, "theirs", "/proj-b", false)
	_, _, err := service.UpsertSessionShare("mine", "my conversation", "codebuddy", 1, `{}`)
	require.NoError(t, err)
	_, _, err = service.UpsertSessionShare("theirs", "their conversation", "codebuddy", 1, `{}`)
	require.NoError(t, err)

	shares, err := service.ListSessionShares("/proj-a")
	require.NoError(t, err)
	require.Len(t, shares, 1, "only this project's share may be listed")
	assert.Equal(t, "mine", shares[0].SessionID)
	assert.Equal(t, "my conversation", shares[0].Title)
}

// Archived sessions keep their share (only a hard delete revokes it), so the
// list must still return them — otherwise the link would be unrevocable from
// the UI, since archived sessions are not addressable by id.
func TestSessionShares_ListIncludesArchivedWithFlag(t *testing.T) {
	db := setupTestDBForSessionShares(t)
	defer func() { _ = db.Close() }()

	seedListedSession(t, db, "live", "/proj", false)
	seedListedSession(t, db, "gone", "/proj", true)
	_, _, err := service.UpsertSessionShare("live", "live one", "codebuddy", 1, `{}`)
	require.NoError(t, err)
	_, _, err = service.UpsertSessionShare("gone", "archived one", "codebuddy", 1, `{}`)
	require.NoError(t, err)

	shares, err := service.ListSessionShares("/proj")
	require.NoError(t, err)
	require.Len(t, shares, 2, "an archived session must not lose its revocable share")

	byID := map[string]service.SessionShare{}
	for _, s := range shares {
		byID[s.SessionID] = s
	}
	assert.False(t, byID["live"].Archived)
	assert.True(t, byID["gone"].Archived, "the flag drives whether the UI offers open-conversation")
}

// A share whose session row is gone must not appear: the JOIN drops it. This is
// the invariant the deletion paths maintain, so a regression there would show up
// here rather than as a stale row in the UI.
func TestSessionShares_ListDropsSharesWithoutASessionRow(t *testing.T) {
	db := setupTestDBForSessionShares(t)
	defer func() { _ = db.Close() }()

	// Insert a share with no matching chat_sessions row (simulates a share that
	// somehow outlived its session).
	_, _, err := service.UpsertSessionShare("orphan", "orphan", "codebuddy", 1, `{}`)
	require.NoError(t, err)

	shares, err := service.ListSessionShares("/proj")
	require.NoError(t, err)
	assert.Empty(t, shares, "a share with no session row must not be listed")
}

// A session id is required: an empty one would insert a share that can never be
// looked up by session (and would rotate on every call).
func TestSessionShares_UpsertRequiresSessionID(t *testing.T) {
	db := setupTestDBForSessionShares(t)
	defer func() { _ = db.Close() }()

	_, _, err := service.UpsertSessionShare("", "t", "codebuddy", 1, `{}`)
	require.Error(t, err)
}
