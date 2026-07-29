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

// FeishuSubscriberRequest is the request body for adding a subscriber.
type FeishuSubscriberRequest struct {
	UserID   string `json:"user_id"`
	UserName string `json:"user_name"`
}

// ServeFeishuSubscribers handles GET/POST/DELETE for /api/feishu/subscribers.
func ServeFeishuSubscribers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		serveFeishuSubscribersGet(w, r)
	case http.MethodPost:
		serveFeishuSubscribersPost(w, r)
	case http.MethodDelete:
		serveFeishuSubscribersDelete(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// serveFeishuSubscribersGet returns all Feishu subscribers.
func serveFeishuSubscribersGet(w http.ResponseWriter, _ *http.Request) {
	subs, err := service.GetFeishuSubscribers()
	if err != nil {
		slog.Warn("feishu: get subscribers failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if subs == nil {
		subs = []service.FeishuSubscriber{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(subs)
}

// serveFeishuSubscribersPost adds a Feishu subscriber manually.
func serveFeishuSubscribersPost(w http.ResponseWriter, r *http.Request) {
	var req FeishuSubscriberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.UserID == "" {
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return
	}
	if err := service.UpsertFeishuSubscriber(req.UserID, "", req.UserName, "manual"); err != nil {
		slog.Warn("feishu: add subscriber failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// serveFeishuSubscribersDelete removes a Feishu subscriber.
// Expects the user_id as the last path segment: /api/feishu/subscribers/{user_id}
func serveFeishuSubscribersDelete(w http.ResponseWriter, r *http.Request) {
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

	if err := service.DeleteFeishuSubscriber(userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "subscriber not found", http.StatusNotFound)
			return
		}
		slog.Warn("feishu: delete subscriber failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
