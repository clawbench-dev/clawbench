package service_test

import (
	"database/sql"
	"testing"

	"clawbench/internal/store"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupTestDBForSessionShares creates an in-memory SQLite with the
// session_shares table plus the chat_sessions table it is listed against.
//
// chat_sessions is needed because every read JOINs it: title / backend /
// archived are read live (nothing about the conversation is frozen), and the
// inner JOIN is also what makes an orphan share indistinguishable from a
// revoked link.
func setupTestDBForSessionShares(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)

	_, err = db.Exec(service.SessionSharesDDL)
	require.NoError(t, err)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS projects (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	path TEXT NOT NULL,
	forge_bind_opt_out INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(path)
);
CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL,
			backend TEXT NOT NULL,
			title TEXT NOT NULL,
	agent_name TEXT NOT NULL DEFAULT '',
			archived INTEGER NOT NULL DEFAULT 0
		);`)
	require.NoError(t, err)

	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(cleanup)
	return db
}

// seedListedSession inserts a chat_sessions row so a share can be joined to it.
func seedListedSession(t *testing.T, db *sql.DB, id, projectPath string, archived bool) { //nolint:unparam // projectPath is kept for the project-scoping tests
	t.Helper()
	arch := 0
	if archived {
		arch = 1
	}
	_, err := db.Exec(
		"INSERT INTO chat_sessions (id, project_id, backend, title, archived) VALUES (?, ?, ?, ?, ?)",
		id, store.ProjectIDForTest(t, projectPath), "codebuddy", "t", arch,
	)
	require.NoError(t, err)
}

// seedListedSessionTitled is seedListedSession with an explicit title/backend,
// so a test can prove the list reads them live from the session row.
func seedListedSessionTitled(t *testing.T, db *sql.DB, id, projectPath, title, backend string) {
	t.Helper()
	_, err := db.Exec(
		"INSERT INTO chat_sessions (id, project_id, backend, title, archived) VALUES (?, ?, ?, ?, 0)",
		id, store.ProjectIDForTest(t, projectPath), backend, title,
	)
	require.NoError(t, err)
}

func TestSessionShares_UpsertCreatesNewToken(t *testing.T) {
	db := setupTestDBForSessionShares(t)
	defer func() { _ = db.Close() }()

	seedListedSessionTitled(t, db, "sess-1", "/proj", "My chat", "codebuddy")

	token, created, err := service.UpsertSessionShare("sess-1", []int64{7, 9, 11, 13})
	require.NoError(t, err)
	assert.True(t, created)
	assert.Len(t, token, 32, "token is 32 hex chars (128 bits)")

	share, ok, err := service.GetSessionShareByToken(token)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, []int64{7, 9, 11, 13}, share.MessageIDs)
	assert.Equal(t, 4, share.MessageCount)
	// Title and backend come from the session row, not from the share.
	assert.Equal(t, "My chat", share.Title)
	assert.Equal(t, "codebuddy", share.Backend)
	assert.NotEmpty(t, share.CreatedAt)
}

// An empty selection would store a link that 404s the instant it is created, so
// the storage layer refuses it rather than letting the handler ship a dead link.
func TestSessionShares_UpsertRejectsEmptySelection(t *testing.T) {
	db := setupTestDBForSessionShares(t)
	defer func() { _ = db.Close() }()

	_, _, err := service.UpsertSessionShare("sess-1", nil)
	require.Error(t, err)
}

// Re-sharing a session must invalidate the previous link and leave exactly one
// row: session_id is UNIQUE, so a leftover row would be a token that no
// session-scoped revoke can ever reach.
func TestSessionShares_UpsertRotatesToken(t *testing.T) {
	db := setupTestDBForSessionShares(t)
	defer func() { _ = db.Close() }()

	seedListedSession(t, db, "sess-1", "/proj", false)

	token1, created, err := service.UpsertSessionShare("sess-1", []int64{1})
	require.NoError(t, err)
	assert.True(t, created)

	token2, created, err := service.UpsertSessionShare("sess-1", []int64{1, 2})
	require.NoError(t, err)
	assert.False(t, created, "second upsert for the same session is a rotation")
	assert.NotEqual(t, token1, token2)

	_, ok, err := service.GetSessionShareByToken(token1)
	require.NoError(t, err)
	assert.False(t, ok, "old token must be revoked after rotation")

	share, ok, err := service.GetSessionShareByToken(token2)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, []int64{1, 2}, share.MessageIDs)
	assert.Equal(t, 2, share.MessageCount)

	var rows int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM session_shares WHERE session_id = 'sess-1'").Scan(&rows))
	assert.Equal(t, 1, rows, "rotation must not leave a second unreachable token")
}

func TestSessionShares_GetBySession(t *testing.T) {
	db := setupTestDBForSessionShares(t)
	defer func() { _ = db.Close() }()

	_, ok, err := service.GetSessionShareBySession("nope")
	require.NoError(t, err)
	assert.False(t, ok)

	seedListedSession(t, db, "sess-1", "/proj", false)
	token, _, err := service.UpsertSessionShare("sess-1", []int64{1})
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
		share, ok, err := service.GetSessionShareByToken(token)
		require.NoError(t, err, "token=%q", token)
		assert.False(t, ok)
		assert.Empty(t, share.Token)
		assert.Empty(t, share.Title)
		assert.Zero(t, share.MessageCount)
	}
}

// A share whose session row is gone resolves to no share at all (the inner JOIN
// drops it) — the same shape as a revoked link, so the reader sees one 404.
func TestSessionShares_GetByTokenOrphanIsAMiss(t *testing.T) {
	db := setupTestDBForSessionShares(t)
	defer func() { _ = db.Close() }()

	token, _, err := service.UpsertSessionShare("orphan", []int64{1})
	require.NoError(t, err)

	_, ok, err := service.GetSessionShareByToken(token)
	require.NoError(t, err)
	assert.False(t, ok)
}

// A corrupt message_ids value must surface as an error, never as an empty list:
// "empty" means "everything was deleted" and would silently 404 a live link.
func TestSessionShares_GetByTokenBadMessageIDsSurfaces(t *testing.T) {
	db := setupTestDBForSessionShares(t)
	defer func() { _ = db.Close() }()

	seedListedSession(t, db, "sess-1", "/proj", false)
	_, err := db.Exec(`INSERT INTO session_shares (token, session_id, message_ids) VALUES ('t', 'sess-1', '{not json')`)
	require.NoError(t, err)

	_, _, err = service.GetSessionShareByToken("t")
	require.Error(t, err)
}

func TestSessionShares_DeleteBySession(t *testing.T) {
	db := setupTestDBForSessionShares(t)
	defer func() { _ = db.Close() }()

	seedListedSession(t, db, "sess-1", "/proj", false)
	token, _, err := service.UpsertSessionShare("sess-1", []int64{1})
	require.NoError(t, err)

	require.NoError(t, service.DeleteSessionShareBySession("sess-1"))

	_, ok, err := service.GetSessionShareByToken(token)
	require.NoError(t, err)
	assert.False(t, ok)
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

	seedListedSessionTitled(t, db, "s1", "/proj", "first", "codebuddy")
	seedListedSessionTitled(t, db, "s2", "/proj", "second", "claude")
	_, _, err := service.UpsertSessionShare("s1", []int64{1})
	require.NoError(t, err)
	_, _, err = service.UpsertSessionShare("s2", []int64{1, 2})
	require.NoError(t, err)

	shares, err := service.ListSessionShares("/proj")
	require.NoError(t, err)
	require.Len(t, shares, 2)
	assert.Equal(t, "s2", shares[0].SessionID, "newest first")
	// Title / backend / count are read live from the session row and the stored
	// selection, not from a frozen copy on the share row.
	assert.Equal(t, "second", shares[0].Title)
	assert.Equal(t, "claude", shares[0].Backend)
	assert.Equal(t, 2, shares[0].MessageCount)
	assert.Equal(t, []int64{1, 2}, shares[0].MessageIDs)
}

// A conversation title is private content: listing another project's shares
// would disclose it. The list is therefore project-scoped, unlike the file list.
func TestSessionShares_ListIsProjectScoped(t *testing.T) {
	db := setupTestDBForSessionShares(t)
	defer func() { _ = db.Close() }()

	seedListedSessionTitled(t, db, "mine", "/proj-a", "my conversation", "codebuddy")
	seedListedSessionTitled(t, db, "theirs", "/proj-b", "their conversation", "codebuddy")
	_, _, err := service.UpsertSessionShare("mine", []int64{1})
	require.NoError(t, err)
	_, _, err = service.UpsertSessionShare("theirs", []int64{1})
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
	_, _, err := service.UpsertSessionShare("live", []int64{1})
	require.NoError(t, err)
	_, _, err = service.UpsertSessionShare("gone", []int64{1})
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
	_, _, err := service.UpsertSessionShare("orphan", []int64{1})
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

	_, _, err := service.UpsertSessionShare("", []int64{1})
	require.Error(t, err)
}
