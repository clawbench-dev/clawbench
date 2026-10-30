package service_test

import (
	"database/sql"
	"errors"
	"testing"

	"clawbench/internal/store"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupTestDBForWeChat creates an in-memory SQLite with the wechat_subscribers
// table in its post-migration shape (last_session_id + context_token present).
func setupTestDBForWeChat(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS wechat_subscribers (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id         TEXT NOT NULL UNIQUE,
			conversation_id TEXT NOT NULL DEFAULT '',
			user_name       TEXT NOT NULL DEFAULT '',
			source          TEXT NOT NULL DEFAULT 'stream',
			context_token   TEXT NOT NULL DEFAULT '',
			created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_session_id TEXT NOT NULL DEFAULT ''
		);
	`)
	require.NoError(t, err)

	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(cleanup)
	return db
}

func TestWeChatSubscribers_CRUD(t *testing.T) {
	db := setupTestDBForWeChat(t)
	defer func() { _ = db.Close() }()

	require.NoError(t, service.UpsertWeChatSubscriber("wx_user1", "conv_1", "User 1", "stream"))

	subs, err := service.GetWeChatSubscribers()
	require.NoError(t, err)
	require.Len(t, subs, 1)
	assert.Equal(t, "wx_user1", subs[0].UserID)
	assert.Equal(t, "User 1", subs[0].UserName)
	assert.Equal(t, "stream", subs[0].Source)

	// Upsert again updates the profile fields without adding a row.
	require.NoError(t, service.UpsertWeChatSubscriber("wx_user1", "conv_2", "Renamed", "stream"))
	subs, err = service.GetWeChatSubscribers()
	require.NoError(t, err)
	require.Len(t, subs, 1)
	assert.Equal(t, "Renamed", subs[0].UserName)

	require.NoError(t, service.DeleteWeChatSubscriber("wx_user1"))
	assert.ErrorIs(t, service.DeleteWeChatSubscriber("wx_user1"), sql.ErrNoRows,
		"deleting a missing subscriber reports ErrNoRows")
}

// TestWeChatSubscribers_UpsertPreservesContextToken is the load-bearing guard on
// the ON CONFLICT clause: Upsert runs on every inbound message, so if it wrote
// context_token it would blank the token a concurrent Set wrote — and outbound
// push would stop working for that user until their next message.
func TestWeChatSubscribers_UpsertPreservesContextToken(t *testing.T) {
	db := setupTestDBForWeChat(t)
	defer func() { _ = db.Close() }()

	require.NoError(t, service.UpsertWeChatSubscriber("wx_user1", "conv_1", "User 1", "stream"))
	require.NoError(t, service.SetWeChatContextToken("wx_user1", "tok-abc"))

	// A later inbound message re-upserts the same subscriber.
	require.NoError(t, service.UpsertWeChatSubscriber("wx_user1", "conv_2", "Renamed", "stream"))

	subs, err := service.GetWeChatSubscribers()
	require.NoError(t, err)
	require.Len(t, subs, 1)
	assert.Equal(t, "tok-abc", subs[0].ContextToken,
		"Upsert must not touch context_token")
}

func TestWeChatSubscribers_ContextToken(t *testing.T) {
	db := setupTestDBForWeChat(t)
	defer func() { _ = db.Close() }()

	// A missing row is an error, not a silent insert of a partial subscriber.
	assert.ErrorIs(t, service.SetWeChatContextToken("nobody", "tok"),
		sql.ErrNoRows, "setting a token on a missing subscriber must fail loudly")

	require.NoError(t, service.UpsertWeChatSubscriber("wx_user1", "", "", "stream"))
	require.NoError(t, service.SetWeChatContextToken("wx_user1", "tok-1"))

	subs, err := service.GetWeChatSubscribers()
	require.NoError(t, err)
	require.Len(t, subs, 1)
	assert.Equal(t, "tok-1", subs[0].ContextToken)

	// Clear blanks every token but keeps the rows: last_session_id is a user
	// preference that should survive a logout/login cycle.
	require.NoError(t, service.ClearWeChatContextTokens())
	subs, err = service.GetWeChatSubscribers()
	require.NoError(t, err)
	require.Len(t, subs, 1, "clearing tokens must not delete subscriber rows")
	assert.Equal(t, "", subs[0].ContextToken)
}

func TestWeChatSubscribers_LastSessionID(t *testing.T) {
	db := setupTestDBForWeChat(t)
	defer func() { _ = db.Close() }()

	// No row yet: empty, no error — the caller falls back to the "/ls" hint.
	got, err := service.GetWeChatLastSessionID("nobody")
	require.NoError(t, err)
	assert.Equal(t, "", got)

	require.NoError(t, service.UpsertWeChatSubscriber("wx_user1", "", "", "stream"))

	got, err = service.GetWeChatLastSessionID("wx_user1")
	require.NoError(t, err)
	assert.Equal(t, "", got, "a subscriber with no recorded target must read as empty")

	require.NoError(t, service.SetWeChatLastSessionID("wx_user1", "sess-42"))
	got, err = service.GetWeChatLastSessionID("wx_user1")
	require.NoError(t, err)
	assert.Equal(t, "sess-42", got)

	assert.True(t, errors.Is(service.SetWeChatLastSessionID("nobody", "s"), sql.ErrNoRows),
		"setting a sticky target on a missing subscriber must fail loudly")
}

// TestWeChatSubscribers_NoManualSource documents the deliberate deviation from
// DingTalk/Feishu: WeChat has no static subscriber list, because a push needs a
// context_token that only inbound traffic can mint. Nothing merges config users.
func TestWeChatSubscribers_NoManualSource(t *testing.T) {
	db := setupTestDBForWeChat(t)
	defer func() { _ = db.Close() }()

	subs, err := service.GetWeChatSubscribers()
	require.NoError(t, err)
	assert.Empty(t, subs)
}

// Every subscriber helper must degrade to a no-op before the database is
// initialized: the push manager starts with the server and may receive a
// message before InitDB has run.
func TestWeChatSubscribers_NoDatabaseIsANoOp(t *testing.T) {
	cleanup := store.SetDBForTest(nil, nil)
	defer cleanup()

	subs, err := service.GetWeChatSubscribers()
	require.NoError(t, err)
	assert.Nil(t, subs)

	assert.NoError(t, service.UpsertWeChatSubscriber("u1", "c1", "name", "stream"))
	assert.NoError(t, service.DeleteWeChatSubscriber("u1"))

	sid, err := service.GetWeChatLastSessionID("u1")
	require.NoError(t, err)
	assert.Empty(t, sid)
	assert.NoError(t, service.SetWeChatLastSessionID("u1", "s1"))
	assert.NoError(t, service.SetWeChatContextToken("u1", "t1"))
	assert.NoError(t, service.ClearWeChatContextTokens())
}

// A failing database must surface the error rather than reporting "no
// subscribers", which would silently stop all pushes.
func TestWeChatSubscribers_PropagateDatabaseErrors(t *testing.T) {
	db := setupTestDBForWeChat(t)
	require.NoError(t, db.Close())

	_, err := service.GetWeChatSubscribers()
	require.Error(t, err)

	assert.Error(t, service.UpsertWeChatSubscriber("u1", "c1", "name", "stream"))
	assert.Error(t, service.DeleteWeChatSubscriber("u1"))

	_, err = service.GetWeChatLastSessionID("u1")
	require.Error(t, err)
	assert.Error(t, service.SetWeChatLastSessionID("u1", "s1"))
	assert.Error(t, service.SetWeChatContextToken("u1", "t1"))
	assert.Error(t, service.ClearWeChatContextTokens())
}
