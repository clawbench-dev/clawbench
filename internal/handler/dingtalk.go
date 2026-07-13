package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"clawbench/internal/service"
)

// DingTalkSubscriberRequest is the request body for adding a subscriber.
type DingTalkSubscriberRequest struct {
	UserID   string `json:"user_id"`
	UserName string `json:"user_name"`
}

// ServeDingTalkSubscribers handles GET/POST/DELETE for /api/dingtalk/subscribers.
func ServeDingTalkSubscribers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		serveDingTalkSubscribersGet(w, r)
	case http.MethodPost:
		serveDingTalkSubscribersPost(w, r)
	case http.MethodDelete:
		serveDingTalkSubscribersDelete(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// serveDingTalkSubscribersGet returns all DingTalk subscribers.
func serveDingTalkSubscribersGet(w http.ResponseWriter, _ *http.Request) {
	subs, err := service.GetDingTalkSubscribers()
	if err != nil {
		slog.Warn("dingtalk: get subscribers failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if subs == nil {
		subs = []service.DingTalkSubscriber{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(subs)
}

// serveDingTalkSubscribersPost adds a DingTalk subscriber manually.
func serveDingTalkSubscribersPost(w http.ResponseWriter, r *http.Request) {
	var req DingTalkSubscriberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.UserID == "" {
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return
	}
	if err := service.UpsertDingTalkSubscriber(req.UserID, "", req.UserName, "manual"); err != nil {
		slog.Warn("dingtalk: add subscriber failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// serveDingTalkSubscribersDelete removes a DingTalk subscriber.
// Expects the user_id as the last path segment: /api/dingtalk/subscribers/{user_id}
func serveDingTalkSubscribersDelete(w http.ResponseWriter, r *http.Request) {
	// Extract user_id from URL path
	parts := strings.Split(strings.TrimRight(r.URL.Path, "/"), "/")
	if len(parts) < 2 {
		http.Error(w, "user_id required in path", http.StatusBadRequest)
		return
	}
	userID := parts[len(parts)-1]
	if userID == "" || userID == "subscribers" {
		http.Error(w, "user_id required in path", http.StatusBadRequest)
		return
	}

	if err := service.DeleteDingTalkSubscriber(userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "subscriber not found", http.StatusNotFound)
			return
		}
		slog.Warn("dingtalk: delete subscriber failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
