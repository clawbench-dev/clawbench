package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func setupFeishuHandlerTestDB(t *testing.T) {
	t.Helper()
	db := setupDingTalkHandlerTestDB(t) // reuse the shared test DB setup

	// Create feishu_subscribers table
	_, err := db.Exec(`
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
}

func TestServeFeishuSubscribers_Get(t *testing.T) {
	setupFeishuHandlerTestDB(t)

	err := service.UpsertFeishuSubscriber("ou_user1", "chat1", "User One", "stream")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/feishu/subscribers", http.NoBody)
	w := httptest.NewRecorder()
	ServeFeishuSubscribers(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var subs []service.FeishuSubscriber
	if err := json.NewDecoder(w.Body).Decode(&subs); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(subs) != 1 || subs[0].UserID != "ou_user1" {
		t.Errorf("expected 1 subscriber with user_id=ou_user1, got %v", subs)
	}
}

func TestServeFeishuSubscribers_GetEmpty(t *testing.T) {
	setupFeishuHandlerTestDB(t)

	req := httptest.NewRequest(http.MethodGet, "/api/feishu/subscribers", http.NoBody)
	w := httptest.NewRecorder()
	ServeFeishuSubscribers(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var subs []service.FeishuSubscriber
	if err := json.NewDecoder(w.Body).Decode(&subs); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(subs) != 0 {
		t.Errorf("expected empty array, got %v", subs)
	}
}

func TestServeFeishuSubscribers_Post(t *testing.T) {
	setupFeishuHandlerTestDB(t)

	req := httptest.NewRequest(http.MethodPost, "/api/feishu/subscribers", strings.NewReader(`{"user_id":"ou_new_user","user_name":"New User"}`))
	w := httptest.NewRecorder()
	ServeFeishuSubscribers(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	subs, _ := service.GetFeishuSubscribers()
	found := false
	for _, s := range subs {
		if s.UserID == "ou_new_user" {
			found = true
			if s.Source != "manual" {
				t.Errorf("expected source manual, got %s", s.Source)
			}
		}
	}
	if !found {
		t.Error("ou_new_user not found")
	}
}

func TestServeFeishuSubscribers_PostEmptyUserID(t *testing.T) {
	setupFeishuHandlerTestDB(t)

	req := httptest.NewRequest(http.MethodPost, "/api/feishu/subscribers", strings.NewReader(`{"user_id":"","user_name":"No ID"}`))
	w := httptest.NewRecorder()
	ServeFeishuSubscribers(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestServeFeishuSubscribers_PostInvalidJSON(t *testing.T) {
	setupFeishuHandlerTestDB(t)

	req := httptest.NewRequest(http.MethodPost, "/api/feishu/subscribers", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	ServeFeishuSubscribers(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestServeFeishuSubscribers_Delete(t *testing.T) {
	setupFeishuHandlerTestDB(t)

	err := service.UpsertFeishuSubscriber("ou_del_user", "chat1", "Delete Me", "manual")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodDelete, "/api/feishu/subscribers/ou_del_user", http.NoBody)
	w := httptest.NewRecorder()
	ServeFeishuSubscribers(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServeFeishuSubscribers_DeleteNotFound(t *testing.T) {
	setupFeishuHandlerTestDB(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/feishu/subscribers/ou_nonexistent", http.NoBody)
	w := httptest.NewRecorder()
	ServeFeishuSubscribers(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestServeFeishuSubscribers_DeleteNoUserID(t *testing.T) {
	setupFeishuHandlerTestDB(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/feishu/subscribers/", http.NoBody)
	w := httptest.NewRecorder()
	ServeFeishuSubscribers(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestServeFeishuSubscribers_MethodNotAllowed(t *testing.T) {
	setupFeishuHandlerTestDB(t)

	req := httptest.NewRequest(http.MethodPut, "/api/feishu/subscribers", http.NoBody)
	w := httptest.NewRecorder()
	ServeFeishuSubscribers(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestServeFeishuSubscribers_DeleteWithSubscribersPath(t *testing.T) {
	setupFeishuHandlerTestDB(t)

	// URL ending with "subscribers" should be rejected (no user_id)
	req := httptest.NewRequest(http.MethodDelete, "/api/feishu/subscribers/subscribers", http.NoBody)
	w := httptest.NewRecorder()
	ServeFeishuSubscribers(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for user_id=subscribers, got %d", w.Code)
	}
}
