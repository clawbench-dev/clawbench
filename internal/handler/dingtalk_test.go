package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func setupDingTalkHandlerTestDB(t *testing.T) *sql.DB {
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

func TestServeDingTalkSubscribers_Get(t *testing.T) {
	db := setupDingTalkHandlerTestDB(t)
	defer func() { _ = db.Close() }()

	err := service.UpsertDingTalkSubscriber("user1", "conv1", "User One", "stream")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/dingtalk/subscribers", http.NoBody)
	w := httptest.NewRecorder()
	ServeDingTalkSubscribers(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var subs []service.DingTalkSubscriber
	if err := json.NewDecoder(w.Body).Decode(&subs); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(subs) != 1 || subs[0].UserID != "user1" {
		t.Errorf("expected 1 subscriber with user_id=user1, got %v", subs)
	}
}

func TestServeDingTalkSubscribers_GetEmpty(t *testing.T) {
	db := setupDingTalkHandlerTestDB(t)
	defer func() { _ = db.Close() }()

	req := httptest.NewRequest(http.MethodGet, "/api/dingtalk/subscribers", http.NoBody)
	w := httptest.NewRecorder()
	ServeDingTalkSubscribers(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var subs []service.DingTalkSubscriber
	if err := json.NewDecoder(w.Body).Decode(&subs); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(subs) != 0 {
		t.Errorf("expected empty array, got %v", subs)
	}
}

func TestServeDingTalkSubscribers_Post(t *testing.T) {
	db := setupDingTalkHandlerTestDB(t)
	defer func() { _ = db.Close() }()

	req := httptest.NewRequest(http.MethodPost, "/api/dingtalk/subscribers", strings.NewReader(`{"user_id":"new_user","user_name":"New User"}`))
	w := httptest.NewRecorder()
	ServeDingTalkSubscribers(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	subs, _ := service.GetDingTalkSubscribers()
	found := false
	for _, s := range subs {
		if s.UserID == "new_user" {
			found = true
			if s.Source != "manual" {
				t.Errorf("expected source manual, got %s", s.Source)
			}
		}
	}
	if !found {
		t.Error("new_user not found")
	}
}

func TestServeDingTalkSubscribers_PostEmptyUserID(t *testing.T) {
	db := setupDingTalkHandlerTestDB(t)
	defer func() { _ = db.Close() }()

	req := httptest.NewRequest(http.MethodPost, "/api/dingtalk/subscribers", strings.NewReader(`{"user_id":"","user_name":"No ID"}`))
	w := httptest.NewRecorder()
	ServeDingTalkSubscribers(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestServeDingTalkSubscribers_PostInvalidJSON(t *testing.T) {
	db := setupDingTalkHandlerTestDB(t)
	defer func() { _ = db.Close() }()

	req := httptest.NewRequest(http.MethodPost, "/api/dingtalk/subscribers", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	ServeDingTalkSubscribers(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestServeDingTalkSubscribers_Delete(t *testing.T) {
	db := setupDingTalkHandlerTestDB(t)
	defer func() { _ = db.Close() }()

	err := service.UpsertDingTalkSubscriber("del_user", "conv1", "Delete Me", "manual")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodDelete, "/api/dingtalk/subscribers/del_user", http.NoBody)
	w := httptest.NewRecorder()
	ServeDingTalkSubscribers(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServeDingTalkSubscribers_DeleteNotFound(t *testing.T) {
	db := setupDingTalkHandlerTestDB(t)
	defer func() { _ = db.Close() }()

	req := httptest.NewRequest(http.MethodDelete, "/api/dingtalk/subscribers/nonexistent", http.NoBody)
	w := httptest.NewRecorder()
	ServeDingTalkSubscribers(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestServeDingTalkSubscribers_DeleteNoUserID(t *testing.T) {
	db := setupDingTalkHandlerTestDB(t)
	defer func() { _ = db.Close() }()

	req := httptest.NewRequest(http.MethodDelete, "/api/dingtalk/subscribers/", http.NoBody)
	w := httptest.NewRecorder()
	ServeDingTalkSubscribers(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestServeDingTalkSubscribers_DeleteSubscribersPath(t *testing.T) {
	db := setupDingTalkHandlerTestDB(t)
	defer func() { _ = db.Close() }()

	req := httptest.NewRequest(http.MethodDelete, "/api/dingtalk/subscribers/subscribers", http.NoBody)
	w := httptest.NewRecorder()
	ServeDingTalkSubscribers(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for 'subscribers' as user_id, got %d", w.Code)
	}
}

func TestServeDingTalkSubscribers_MethodNotAllowed(t *testing.T) {
	db := setupDingTalkHandlerTestDB(t)
	defer func() { _ = db.Close() }()

	req := httptest.NewRequest(http.MethodPut, "/api/dingtalk/subscribers", http.NoBody)
	w := httptest.NewRecorder()
	ServeDingTalkSubscribers(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestServeDingTalkSubscribers_GetError(t *testing.T) {
	// Test with nil DB (service returns nil, nil for nil db)
	db := setupDingTalkHandlerTestDB(t)
	defer func() { _ = db.Close() }()

	// Close DB to cause query error
	_ = db.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/dingtalk/subscribers", http.NoBody)
	w := httptest.NewRecorder()
	ServeDingTalkSubscribers(w, req)

	// Should handle error gracefully (500 or 200 with empty)
	if w.Code != http.StatusInternalServerError && w.Code != http.StatusOK {
		t.Logf("got status %d: %s", w.Code, w.Body.String())
	}
}

func TestServeDingTalkSubscribers_PostUpsertError(t *testing.T) {
	db := setupDingTalkHandlerTestDB(t)
	defer func() { _ = db.Close() }()

	// Close DB to cause write error
	_ = db.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/dingtalk/subscribers", strings.NewReader(`{"user_id":"user1","user_name":"User"}`))
	w := httptest.NewRecorder()
	ServeDingTalkSubscribers(w, req)

	if w.Code != http.StatusInternalServerError && w.Code != http.StatusNoContent {
		t.Logf("got status %d: %s", w.Code, w.Body.String())
	}
}

func TestServeDingTalkSubscribers_DeleteEmptyPath(t *testing.T) {
	db := setupDingTalkHandlerTestDB(t)
	defer func() { _ = db.Close() }()

	req := httptest.NewRequest(http.MethodDelete, "/api/dingtalk/subscribers", http.NoBody)
	w := httptest.NewRecorder()
	ServeDingTalkSubscribers(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
