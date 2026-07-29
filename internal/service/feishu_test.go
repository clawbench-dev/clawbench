package service_test

import (
	"database/sql"
	"errors"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupTestDBForFeishu creates an in-memory SQLite with feishu tables.
func setupTestDBForFeishu(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS feishu_subscribers (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id         TEXT NOT NULL UNIQUE,
			chat_id         TEXT NOT NULL DEFAULT '',
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

func TestFeishuSubscribers_CRUD(t *testing.T) {
	db := setupTestDBForFeishu(t)
	defer func() { _ = db.Close() }()

	// Test Upsert
	err := service.UpsertFeishuSubscriber("ou_test_user1", "chat_1", "Test User 1", "stream")
	require.NoError(t, err)

	// Test Get
	subs, err := service.GetFeishuSubscribers()
	require.NoError(t, err)
	found := false
	for _, s := range subs {
		if s.UserID == "ou_test_user1" {
			found = true
			if s.ChatID != "chat_1" {
				t.Errorf("expected chat_id chat_1, got %s", s.ChatID)
			}
			if s.Source != "stream" {
				t.Errorf("expected source stream, got %s", s.Source)
			}
			break
		}
	}
	if !found {
		t.Error("ou_test_user1 not found")
	}

	// Test Upsert update
	err = service.UpsertFeishuSubscriber("ou_test_user1", "chat_1_updated", "Updated", "manual")
	require.NoError(t, err)

	subs, _ = service.GetFeishuSubscribers()
	for _, s := range subs {
		if s.UserID == "ou_test_user1" {
			if s.ChatID != "chat_1_updated" {
				t.Errorf("expected updated chat_id, got %s", s.ChatID)
			}
			break
		}
	}

	// Test Delete
	err = service.DeleteFeishuSubscriber("ou_test_user1")
	require.NoError(t, err)
}

func TestGetFeishuSubscribers_NilDB(t *testing.T) {
	cleanup := service.SetDBForTest(nil, nil)
	defer cleanup()

	subs, err := service.GetFeishuSubscribers()
	assert.NoError(t, err)
	assert.Nil(t, subs)
}

func TestUpsertFeishuSubscriber_NilDB(t *testing.T) {
	cleanup := service.SetDBForTest(nil, nil)
	defer cleanup()

	err := service.UpsertFeishuSubscriber("ou_user1", "chat1", "name", "stream")
	assert.NoError(t, err)
}

func TestDeleteFeishuSubscriber_NilDB(t *testing.T) {
	cleanup := service.SetDBForTest(nil, nil)
	defer cleanup()

	err := service.DeleteFeishuSubscriber("ou_user1")
	assert.NoError(t, err)
}

func TestDeleteFeishuSubscriber_NotFound(t *testing.T) {
	db := setupTestDBForFeishu(t)
	defer func() { _ = db.Close() }()

	err := service.DeleteFeishuSubscriber("ou_nonexistent_user")
	assert.True(t, errors.Is(err, sql.ErrNoRows))
}

func TestMergeFeishuConfigSubscribers_NilDB(t *testing.T) {
	cleanup := service.SetDBForTest(nil, nil)
	defer cleanup()

	// Should not panic
	service.MergeFeishuConfigSubscribers([]string{"ou_user1"})
}

func TestMergeFeishuConfigSubscribers_AddNewUsers(t *testing.T) {
	db := setupTestDBForFeishu(t)
	defer func() { _ = db.Close() }()

	service.MergeFeishuConfigSubscribers([]string{"ou_manual_user1", "ou_manual_user2"})

	subs, err := service.GetFeishuSubscribers()
	require.NoError(t, err)

	found1, found2 := false, false
	for _, s := range subs {
		if s.UserID == "ou_manual_user1" {
			found1 = true
			assert.Equal(t, "manual", s.Source)
		}
		if s.UserID == "ou_manual_user2" {
			found2 = true
			assert.Equal(t, "manual", s.Source)
		}
	}
	assert.True(t, found1, "ou_manual_user1 should be present")
	assert.True(t, found2, "ou_manual_user2 should be present")
}

func TestMergeFeishuConfigSubscribers_RemoveStaleUsers(t *testing.T) {
	db := setupTestDBForFeishu(t)
	defer func() { _ = db.Close() }()

	// First merge adds user1 and user2 as manual
	service.MergeFeishuConfigSubscribers([]string{"ou_manual_user1", "ou_manual_user2"})

	// Second merge removes user2 and keeps only user1
	service.MergeFeishuConfigSubscribers([]string{"ou_manual_user1"})

	subs, err := service.GetFeishuSubscribers()
	require.NoError(t, err)

	found1, found2 := false, false
	for _, s := range subs {
		if s.UserID == "ou_manual_user1" {
			found1 = true
		}
		if s.UserID == "ou_manual_user2" {
			found2 = true
		}
	}
	assert.True(t, found1, "ou_manual_user1 should still be present")
	assert.False(t, found2, "ou_manual_user2 should be removed")
}

func TestMergeFeishuConfigSubscribers_EmptyUsers(t *testing.T) {
	db := setupTestDBForFeishu(t)
	defer func() { _ = db.Close() }()

	// First add a manual user
	service.MergeFeishuConfigSubscribers([]string{"ou_manual_user1"})

	// Then merge with empty list — should remove the manual user
	service.MergeFeishuConfigSubscribers([]string{})

	subs, err := service.GetFeishuSubscribers()
	require.NoError(t, err)
	assert.Empty(t, subs, "no subscribers expected after removing all manual users")
}

func TestMergeFeishuConfigSubscribers_SkipsEmptyUserID(t *testing.T) {
	db := setupTestDBForFeishu(t)
	defer func() { _ = db.Close() }()

	// Merge with a mix of empty and non-empty user IDs
	service.MergeFeishuConfigSubscribers([]string{"", "ou_valid_user"})

	subs, err := service.GetFeishuSubscribers()
	require.NoError(t, err)

	// Only valid_user should be present
	for _, s := range subs {
		if s.UserID == "" {
			t.Error("empty user_id should be skipped")
		}
	}
	found := false
	for _, s := range subs {
		if s.UserID == "ou_valid_user" {
			found = true
		}
	}
	assert.True(t, found, "ou_valid_user should be present")
}

func TestMergeFeishuConfigSubscribers_DoesNotRemoveStreamSource(t *testing.T) {
	db := setupTestDBForFeishu(t)
	defer func() { _ = db.Close() }()

	// Add a stream-source subscriber directly
	err := service.UpsertFeishuSubscriber("ou_stream_user", "chat1", "Stream User", "stream")
	require.NoError(t, err)

	// Merge with empty config list — should NOT remove stream users
	service.MergeFeishuConfigSubscribers([]string{})

	subs, err := service.GetFeishuSubscribers()
	require.NoError(t, err)

	found := false
	for _, s := range subs {
		if s.UserID == "ou_stream_user" {
			found = true
			assert.Equal(t, "stream", s.Source, "stream subscriber should not be removed by merge")
		}
	}
	assert.True(t, found, "ou_stream_user should still be present")
}

func TestMergeFeishuConfigSubscribers_NilUsers(t *testing.T) {
	db := setupTestDBForFeishu(t)
	defer func() { _ = db.Close() }()

	// Should not panic with nil users list
	service.MergeFeishuConfigSubscribers(nil)
}

func TestGetFeishuSubscribers_QueryError(t *testing.T) {
	db := setupTestDBForFeishu(t)
	defer func() { _ = db.Close() }()

	// Drop the table to cause a query error
	_, _ = db.Exec("DROP TABLE feishu_subscribers")

	subs, err := service.GetFeishuSubscribers()
	assert.Error(t, err)
	assert.Nil(t, subs)
}

func TestMergeFeishuConfigSubscribers_QueryError(t *testing.T) {
	db := setupTestDBForFeishu(t)
	defer func() { _ = db.Close() }()

	// Drop the table to cause a query error
	_, _ = db.Exec("DROP TABLE feishu_subscribers")

	// Should not panic
	service.MergeFeishuConfigSubscribers([]string{"ou_user1"})
}

func TestUpsertFeishuSubscriber_WriteError(t *testing.T) {
	db := setupTestDBForFeishu(t)
	defer func() { _ = db.Close() }()

	// Drop the table to cause a write error
	_, _ = db.Exec("DROP TABLE feishu_subscribers")

	err := service.UpsertFeishuSubscriber("ou_user1", "chat1", "name", "stream")
	assert.Error(t, err)
}

func TestDeleteFeishuSubscriber_WriteError(t *testing.T) {
	db := setupTestDBForFeishu(t)
	defer func() { _ = db.Close() }()

	// Drop the table to cause a write error
	_, _ = db.Exec("DROP TABLE feishu_subscribers")

	err := service.DeleteFeishuSubscriber("ou_user1")
	assert.Error(t, err)
}
