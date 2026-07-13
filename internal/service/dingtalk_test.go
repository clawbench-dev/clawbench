package service_test

import (
	"database/sql"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupTestDBForDingTalk creates an in-memory SQLite with dingtalk tables.
func setupTestDBForDingTalk(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS dingtalk_subscribers (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id         TEXT NOT NULL UNIQUE,
			conversation_id TEXT NOT NULL DEFAULT '',
			user_name       TEXT NOT NULL DEFAULT '',
			source          TEXT NOT NULL DEFAULT 'stream',
			created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	require.NoError(t, err)

	cleanup := service.SetDBForTest(db, db)
	t.Cleanup(cleanup)
	return db
}

func TestDingTalkSubscribers_CRUD(t *testing.T) {
	db := setupTestDBForDingTalk(t)
	defer func() { _ = db.Close() }()

	// Test Upsert
	err := service.UpsertDingTalkSubscriber("test_user1", "conv_1", "Test User 1", "stream")
	require.NoError(t, err)

	// Test Get
	subs, err := service.GetDingTalkSubscribers()
	require.NoError(t, err)
	found := false
	for _, s := range subs {
		if s.UserID == "test_user1" {
			found = true
			if s.ConversationID != "conv_1" {
				t.Errorf("expected conversation_id conv_1, got %s", s.ConversationID)
			}
			if s.Source != "stream" {
				t.Errorf("expected source stream, got %s", s.Source)
			}
			break
		}
	}
	if !found {
		t.Error("test_user1 not found")
	}

	// Test Upsert update
	err = service.UpsertDingTalkSubscriber("test_user1", "conv_1_updated", "Updated", "manual")
	require.NoError(t, err)

	subs, _ = service.GetDingTalkSubscribers()
	for _, s := range subs {
		if s.UserID == "test_user1" {
			if s.ConversationID != "conv_1_updated" {
				t.Errorf("expected updated conv_id, got %s", s.ConversationID)
			}
			break
		}
	}

	// Test Delete
	err = service.DeleteDingTalkSubscriber("test_user1")
	require.NoError(t, err)
}
