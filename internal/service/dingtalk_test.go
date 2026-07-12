package service_test

import (
	"database/sql"
	"testing"

	"clawbench/internal/model"
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
		CREATE TABLE IF NOT EXISTS dingtalk_outbox (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id     TEXT NOT NULL,
			msg_key     TEXT NOT NULL DEFAULT '',
			msg_param   TEXT NOT NULL,
			status      TEXT NOT NULL DEFAULT 'pending',
			retry_count INTEGER NOT NULL DEFAULT 0,
			max_retries INTEGER NOT NULL DEFAULT 3,
			next_retry  DATETIME,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	require.NoError(t, err)

	model.DataDir = t.TempDir()
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

func TestDingTalkOutbox_EnqueueAndProcess(t *testing.T) {
	db := setupTestDBForDingTalk(t)
	defer func() { _ = db.Close() }()

	err := service.EnqueueDingTalkMessage("test_user", "sampleMarkdown", `{"title":"Test","text":"Hello"}`, 3)
	require.NoError(t, err)

	msgs, err := service.GetPendingDingTalkMessages(10)
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	if msgs[0].UserID != "test_user" {
		t.Errorf("expected user_id test_user, got %s", msgs[0].UserID)
	}
	if msgs[0].Status != "pending" {
		t.Errorf("expected status pending, got %s", msgs[0].Status)
	}

	err = service.MarkDingTalkMessageSent(msgs[0].ID)
	require.NoError(t, err)

	// Should no longer be pending
	msgs, _ = service.GetPendingDingTalkMessages(10)
	for _, m := range msgs {
		if m.UserID == "test_user" {
			t.Error("sent message should not be pending")
		}
	}
}

func TestDingTalkOutbox_RetryLogic(t *testing.T) {
	db := setupTestDBForDingTalk(t)
	defer func() { _ = db.Close() }()

	err := service.EnqueueDingTalkMessage("test_retry", "key", `{}`, 2)
	require.NoError(t, err)

	msgs, _ := service.GetPendingDingTalkMessages(10)
	require.Len(t, msgs, 1)
	msgID := msgs[0].ID

	// First failure → retry_count=1, status=retry
	err = service.MarkDingTalkMessageFailed(msgID, 2)
	require.NoError(t, err)

	// Force next_retry to now
	_, _ = db.Exec("UPDATE dingtalk_outbox SET next_retry = datetime('now') WHERE id = ?", msgID)

	msgs, _ = service.GetPendingDingTalkMessages(10)
	for _, m := range msgs {
		if m.ID == msgID {
			if m.RetryCount != 1 {
				t.Errorf("expected retry_count 1, got %d", m.RetryCount)
			}
			if m.Status != "retry" {
				t.Errorf("expected status retry, got %s", m.Status)
			}
		}
	}

	// Second failure → retry_count=2 >= max_retries=2 → permanently failed
	err = service.MarkDingTalkMessageFailed(msgID, 2)
	require.NoError(t, err)

	var status string
	_ = db.QueryRow("SELECT status FROM dingtalk_outbox WHERE id = ?", msgID).Scan(&status)
	if status != "failed" {
		t.Errorf("expected status failed, got %s", status)
	}
}
