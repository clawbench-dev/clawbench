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

// --- GetDingTalkSubscribers nil db ---

func TestGetDingTalkSubscribers_NilDB(t *testing.T) {
	cleanup := service.SetDBForTest(nil, nil)
	defer cleanup()

	subs, err := service.GetDingTalkSubscribers()
	assert.NoError(t, err)
	assert.Nil(t, subs)
}

// --- UpsertDingTalkSubscriber nil db ---

func TestUpsertDingTalkSubscriber_NilDB(t *testing.T) {
	cleanup := service.SetDBForTest(nil, nil)
	defer cleanup()

	err := service.UpsertDingTalkSubscriber("user1", "conv1", "name", "stream")
	assert.NoError(t, err)
}

// --- DeleteDingTalkSubscriber nil db ---

func TestDeleteDingTalkSubscriber_NilDB(t *testing.T) {
	cleanup := service.SetDBForTest(nil, nil)
	defer cleanup()

	err := service.DeleteDingTalkSubscriber("user1")
	assert.NoError(t, err)
}

// --- DeleteDingTalkSubscriber not found ---

func TestDeleteDingTalkSubscriber_NotFound(t *testing.T) {
	db := setupTestDBForDingTalk(t)
	defer func() { _ = db.Close() }()

	err := service.DeleteDingTalkSubscriber("nonexistent_user")
	assert.True(t, errors.Is(err, sql.ErrNoRows))
}

// --- MergeDingTalkConfigSubscribers ---

func TestMergeDingTalkConfigSubscribers_NilDB(t *testing.T) {
	cleanup := service.SetDBForTest(nil, nil)
	defer cleanup()

	// Should not panic
	service.MergeDingTalkConfigSubscribers([]string{"user1"})
}

func TestMergeDingTalkConfigSubscribers_AddNewUsers(t *testing.T) {
	db := setupTestDBForDingTalk(t)
	defer func() { _ = db.Close() }()

	service.MergeDingTalkConfigSubscribers([]string{"manual_user1", "manual_user2"})

	subs, err := service.GetDingTalkSubscribers()
	require.NoError(t, err)

	found1, found2 := false, false
	for _, s := range subs {
		if s.UserID == "manual_user1" {
			found1 = true
			assert.Equal(t, "manual", s.Source)
		}
		if s.UserID == "manual_user2" {
			found2 = true
			assert.Equal(t, "manual", s.Source)
		}
	}
	assert.True(t, found1, "manual_user1 should be present")
	assert.True(t, found2, "manual_user2 should be present")
}

func TestMergeDingTalkConfigSubscribers_RemoveStaleUsers(t *testing.T) {
	db := setupTestDBForDingTalk(t)
	defer func() { _ = db.Close() }()

	// First merge adds user1 and user2 as manual
	service.MergeDingTalkConfigSubscribers([]string{"manual_user1", "manual_user2"})

	// Second merge removes user2 and keeps only user1
	service.MergeDingTalkConfigSubscribers([]string{"manual_user1"})

	subs, err := service.GetDingTalkSubscribers()
	require.NoError(t, err)

	found1, found2 := false, false
	for _, s := range subs {
		if s.UserID == "manual_user1" {
			found1 = true
		}
		if s.UserID == "manual_user2" {
			found2 = true
		}
	}
	assert.True(t, found1, "manual_user1 should still be present")
	assert.False(t, found2, "manual_user2 should be removed")
}

func TestMergeDingTalkConfigSubscribers_EmptyUsers(t *testing.T) {
	db := setupTestDBForDingTalk(t)
	defer func() { _ = db.Close() }()

	// First add a manual user
	service.MergeDingTalkConfigSubscribers([]string{"manual_user1"})

	// Then merge with empty list — should remove the manual user
	service.MergeDingTalkConfigSubscribers([]string{})

	subs, err := service.GetDingTalkSubscribers()
	require.NoError(t, err)
	assert.Empty(t, subs, "no subscribers expected after removing all manual users")
}

func TestMergeDingTalkConfigSubscribers_SkipsEmptyUserID(t *testing.T) {
	db := setupTestDBForDingTalk(t)
	defer func() { _ = db.Close() }()

	// Merge with a mix of empty and non-empty user IDs
	service.MergeDingTalkConfigSubscribers([]string{"", "valid_user"})

	subs, err := service.GetDingTalkSubscribers()
	require.NoError(t, err)

	// Only valid_user should be present
	found := false
	for _, s := range subs {
		if s.UserID == "" {
			t.Error("empty user_id should be skipped")
		}
		if s.UserID == "valid_user" {
			found = true
		}
	}
	assert.True(t, found, "valid_user should be present")
}

func TestMergeDingTalkConfigSubscribers_DoesNotRemoveStreamSource(t *testing.T) {
	db := setupTestDBForDingTalk(t)
	defer func() { _ = db.Close() }()

	// Add a stream-source subscriber directly
	err := service.UpsertDingTalkSubscriber("stream_user", "conv1", "Stream User", "stream")
	require.NoError(t, err)

	// Merge with empty config list — should NOT remove stream users
	service.MergeDingTalkConfigSubscribers([]string{})

	subs, err := service.GetDingTalkSubscribers()
	require.NoError(t, err)

	found := false
	for _, s := range subs {
		if s.UserID == "stream_user" {
			found = true
			assert.Equal(t, "stream", s.Source, "stream subscriber should not be removed by merge")
		}
	}
	assert.True(t, found, "stream_user should still be present")
}

func TestMergeDingTalkConfigSubscribers_NilUsers(t *testing.T) {
	db := setupTestDBForDingTalk(t)
	defer func() { _ = db.Close() }()

	// Should not panic with nil users list
	service.MergeDingTalkConfigSubscribers(nil)
}

func TestMergeDingTalkConfigSubscribers_UpdatesExistingManual(t *testing.T) {
	db := setupTestDBForDingTalk(t)
	defer func() { _ = db.Close() }()

	// Add a manual subscriber with conversation_id
	err := service.UpsertDingTalkSubscriber("manual_user", "old_conv", "Old Name", "manual")
	require.NoError(t, err)

	// Re-merge with the same user — should upsert (overwrite)
	service.MergeDingTalkConfigSubscribers([]string{"manual_user"})

	subs, err := service.GetDingTalkSubscribers()
	require.NoError(t, err)

	for _, s := range subs {
		if s.UserID == "manual_user" {
			assert.Equal(t, "manual", s.Source)
			// After merge, conversation_id and user_name are cleared (empty strings)
			assert.Equal(t, "", s.ConversationID)
			assert.Equal(t, "", s.UserName)
		}
	}
}

func TestDingTalkSubscribers_DeleteNotFound(t *testing.T) {
	db := setupTestDBForDingTalk(t)
	defer func() { _ = db.Close() }()

	err := service.DeleteDingTalkSubscriber("nonexistent_user")
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestDingTalkSubscribers_MergeConfigSubscribers(t *testing.T) {
	db := setupTestDBForDingTalk(t)
	defer func() { _ = db.Close() }()

	// Merge config users
	service.MergeDingTalkConfigSubscribers([]string{"config_user1", "config_user2"})

	// Verify they exist
	subs, err := service.GetDingTalkSubscribers()
	require.NoError(t, err)
	userIDs := make(map[string]bool)
	for _, s := range subs {
		userIDs[s.UserID] = true
		if s.UserID == "config_user1" || s.UserID == "config_user2" {
			if s.Source != "manual" {
				t.Errorf("expected source manual, got %s", s.Source)
			}
		}
	}
	if !userIDs["config_user1"] {
		t.Error("config_user1 not found")
	}
	if !userIDs["config_user2"] {
		t.Error("config_user2 not found")
	}

	// Remove one user from config and merge again
	service.MergeDingTalkConfigSubscribers([]string{"config_user1"})

	// config_user2 should be removed (was manual, no longer in config)
	subs, _ = service.GetDingTalkSubscribers()
	userIDs = make(map[string]bool)
	for _, s := range subs {
		userIDs[s.UserID] = true
	}
	if userIDs["config_user2"] {
		t.Error("config_user2 should have been removed by merge")
	}
	if !userIDs["config_user1"] {
		t.Error("config_user1 should still exist")
	}
}

func TestDingTalkSubscribers_MergeEmptyUsers(t *testing.T) {
	db := setupTestDBForDingTalk(t)
	defer func() { _ = db.Close() }()

	// Add a manual user first
	err := service.UpsertDingTalkSubscriber("manual_user", "conv1", "Manual", "manual")
	require.NoError(t, err)

	// Merge empty list — manual user should be removed
	service.MergeDingTalkConfigSubscribers([]string{})

	subs, err := service.GetDingTalkSubscribers()
	require.NoError(t, err)
	for _, s := range subs {
		if s.UserID == "manual_user" {
			t.Error("manual_user should have been removed when merging empty config")
		}
	}
}

func TestDingTalkSubscribers_MergeSkipsEmpty(t *testing.T) {
	db := setupTestDBForDingTalk(t)
	defer func() { _ = db.Close() }()

	// Merge with empty string in the list
	service.MergeDingTalkConfigSubscribers([]string{"valid_user", ""})

	subs, err := service.GetDingTalkSubscribers()
	require.NoError(t, err)
	for _, s := range subs {
		if s.UserID == "" {
			t.Error("empty user_id should have been skipped")
		}
	}
}

func TestDingTalkSubscribers_StreamSourceNotRemoved(t *testing.T) {
	db := setupTestDBForDingTalk(t)
	defer func() { _ = db.Close() }()

	// Add a stream-source subscriber
	err := service.UpsertDingTalkSubscriber("stream_user", "conv1", "Stream", "stream")
	require.NoError(t, err)

	// Merge empty config — stream users should NOT be removed
	service.MergeDingTalkConfigSubscribers([]string{})

	subs, err := service.GetDingTalkSubscribers()
	require.NoError(t, err)
	found := false
	for _, s := range subs {
		if s.UserID == "stream_user" {
			found = true
		}
	}
	if !found {
		t.Error("stream_user should NOT be removed by merge (only manual users are)")
	}
}

// --- Error path tests ---

func TestGetDingTalkSubscribers_QueryError(t *testing.T) {
	db := setupTestDBForDingTalk(t)
	defer func() { _ = db.Close() }()

	// Drop the table to cause a query error
	_, _ = db.Exec("DROP TABLE dingtalk_subscribers")

	subs, err := service.GetDingTalkSubscribers()
	assert.Error(t, err)
	assert.Nil(t, subs)
}

func TestMergeDingTalkConfigSubscribers_QueryError(t *testing.T) {
	db := setupTestDBForDingTalk(t)
	defer func() { _ = db.Close() }()

	// Drop the table to cause a query error in MergeDingTalkConfigSubscribers
	_, _ = db.Exec("DROP TABLE dingtalk_subscribers")

	// Should not panic
	service.MergeDingTalkConfigSubscribers([]string{"user1"})
}

func TestUpsertDingTalkSubscriber_WriteError(t *testing.T) {
	db := setupTestDBForDingTalk(t)
	defer func() { _ = db.Close() }()

	// Drop the table to cause a write error
	_, _ = db.Exec("DROP TABLE dingtalk_subscribers")

	err := service.UpsertDingTalkSubscriber("user1", "conv1", "name", "stream")
	assert.Error(t, err)
}

func TestDeleteDingTalkSubscriber_WriteError(t *testing.T) {
	db := setupTestDBForDingTalk(t)
	defer func() { _ = db.Close() }()

	// Drop the table to cause a write error
	_, _ = db.Exec("DROP TABLE dingtalk_subscribers")

	err := service.DeleteDingTalkSubscriber("user1")
	assert.Error(t, err)
}
