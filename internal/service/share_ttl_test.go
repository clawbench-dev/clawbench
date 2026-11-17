package service

import (
	"database/sql"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func setupShareTTLDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)

	for _, ddl := range []string{store.ProjectsDDL, FileSharesDDL, SessionSharesDDL, LiveNotesDDL} {
		_, err = db.Exec(ddl)
		require.NoError(t, err)
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS chat_sessions (
		id TEXT PRIMARY KEY,
		project_id INTEGER NOT NULL,
		backend TEXT NOT NULL,
		title TEXT NOT NULL,
		agent_name TEXT NOT NULL DEFAULT '',
		archived INTEGER NOT NULL DEFAULT 0
	)`)
	require.NoError(t, err)

	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(cleanup)
	return db
}

func withShareTTL(t *testing.T, days int) {
	t.Helper()
	orig := model.ConfigInstance
	t.Cleanup(func() { model.ConfigInstance = orig })
	model.ConfigInstance.Share.LinkTTLDays = days
}

func expireRow(t *testing.T, table, keyCol, token string) {
	t.Helper()
	_, err := store.WriteExec(
		"UPDATE "+table+" SET expires_at = datetime('now', '-1 hour') WHERE "+keyCol+" = ?", token)
	require.NoError(t, err)
}

// A file link created under a TTL stops resolving once the deadline passes.
func TestFileShare_ExpiresAfterTTL(t *testing.T) {
	db := setupShareTTLDB(t)
	defer func() { _ = db.Close() }()
	withShareTTL(t, 1)

	token, _, err := UpsertFileShare("/tmp/a.md", "a.md", "/tmp", "/tmp")
	require.NoError(t, err)

	var expires sql.NullString
	require.NoError(t, db.QueryRow(`SELECT expires_at FROM file_shares WHERE token = ?`, token).Scan(&expires))
	assert.True(t, expires.Valid, "a TTL share must store an absolute expiry")

	expireRow(t, "file_shares", "token", token)
	_, _, _, ok, err := GetFileShareByToken(token)
	require.NoError(t, err)
	assert.False(t, ok, "an expired token must not resolve")

	_, _, ok, err = GetFileShareByPath("/tmp/a.md")
	require.NoError(t, err)
	assert.False(t, ok, "the status endpoint must report no active share")

	shares, err := ListFileShares("/tmp")
	require.NoError(t, err)
	assert.Empty(t, shares, "expired links must not be listed")
}

// A session link follows the same rule.
func TestSessionShare_ExpiresAfterTTL(t *testing.T) {
	db := setupShareTTLDB(t)
	defer func() { _ = db.Close() }()
	withShareTTL(t, 1)

	_, err := db.Exec(`INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('sess-ttl', 0, 'claude', 'T')`)
	require.NoError(t, err)

	token, _, err := UpsertSessionShare("sess-ttl", []int64{1, 2})
	require.NoError(t, err)

	expireRow(t, "session_shares", "token", token)
	_, ok, err := GetSessionShareByToken(token)
	require.NoError(t, err)
	assert.False(t, ok, "an expired session token must not resolve")

	_, ok, err = GetSessionShareBySession("sess-ttl")
	require.NoError(t, err)
	assert.False(t, ok, "the status endpoint must report no active share")
}

// A meeting publication follows the same rule; re-enabling mints a fresh
// deadline instead of inheriting the old one.
func TestMeetingShare_ExpiresAfterTTL(t *testing.T) {
	db := setupShareTTLDB(t)
	defer func() { _ = db.Close() }()
	withShareTTL(t, 1)

	require.NoError(t, StartLiveNote("tok-ttl", 1, "Meeting", 1_700_000_000_000))
	require.NoError(t, SetMeetingShareEnabled(1, "tok-ttl", true))

	enabled, err := MeetingShareEnabled("tok-ttl")
	require.NoError(t, err)
	assert.True(t, enabled)

	_, err = db.Exec(`UPDATE live_notes SET share_expires_at = datetime('now', '-1 hour') WHERE token = 'tok-ttl'`)
	require.NoError(t, err)
	enabled, err = MeetingShareEnabled("tok-ttl")
	require.NoError(t, err)
	assert.False(t, enabled, "an expired publication must read as not shared")

	// Re-publishing starts a new window.
	require.NoError(t, SetMeetingShareEnabled(1, "tok-ttl", true))
	enabled, err = MeetingShareEnabled("tok-ttl")
	require.NoError(t, err)
	assert.True(t, enabled, "re-enabling must mint a fresh deadline")
}

// 0 means permanent: no expiry column value is written.
func TestShare_NoTTLWritesNullExpiry(t *testing.T) {
	db := setupShareTTLDB(t)
	defer func() { _ = db.Close() }()
	withShareTTL(t, 0)

	token, _, err := UpsertFileShare("/tmp/perm.md", "perm.md", "/tmp", "/tmp")
	require.NoError(t, err)

	var expires sql.NullString
	require.NoError(t, db.QueryRow(`SELECT expires_at FROM file_shares WHERE token = ?`, token).Scan(&expires))
	assert.False(t, expires.Valid, "0 days must store NULL (never expires)")
}
