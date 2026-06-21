//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// QueueHandler handles pending message queue operations.
// POST   /api/ai/queue?session_id=xxx  — enqueue a message
// GET    /api/ai/queue?session_id=xxx  — get current queue
// DELETE /api/ai/queue?session_id=xxx[&index=N] — remove item or clear all
func QueueHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		handleQueueEnqueue(w, r)
	case http.MethodGet:
		handleQueueGet(w, r)
	case http.MethodDelete:
		handleQueueDelete(w, r)
	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

func handleQueueEnqueue(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}

	// Verify the session belongs to the requesting project (ISS-180)
	// Skip ownership check if session doesn't exist in DB (not-yet-persisted or in-memory only)
	if sessionProject := service.GetSessionProjectPath(sessionID); sessionProject != "" && sessionProject != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	var req struct {
		Message   string   `json:"message"`
		FilePaths []string `json:"filePaths"`
		Files     []string `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}

	if req.Message == "" && len(req.Files) == 0 && len(req.FilePaths) == 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MessageOrFilesRequired")
		return
	}

	qMsg := model.QueuedMessage{
		Text:      req.Message,
		FilePaths: req.FilePaths,
		Files:     req.Files,
		CreatedAt: time.Now().Format(time.RFC3339),
	}

	queue := service.EnqueueMessage(sessionID, qMsg)

	// Race condition fix: if the session is not running when we enqueue, no
	// goroutine will drain the queue. This happens when the user sends a
	// message right as the AI finishes — the frontend sees loading=true and
	// enqueues, but the drain loop has already exited. Dequeue the message
	// and tell the frontend to resubmit as a new chat request.
	if !service.IsSessionRunning(sessionID) {
		dequeued, ok := service.DequeueMessage(sessionID)
		if ok {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":          true,
				"needs_start": true,
				"message":     dequeued.Text,
				"filePaths":   dequeued.FilePaths,
				"files":       dequeued.Files,
				"queue":       service.GetQueue(sessionID),
			})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"queue": queue,
	})
}

func handleQueueGet(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}

	// Verify the session belongs to the requesting project (ISS-180)
	// Skip ownership check if session doesn't exist in DB (not-yet-persisted or in-memory only)
	if sessionProject := service.GetSessionProjectPath(sessionID); sessionProject != "" && sessionProject != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	queue := service.GetQueue(sessionID)
	if queue == nil {
		queue = []model.QueuedMessage{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"queue": queue,
	})
}

func handleQueueDelete(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}

	// Verify the session belongs to the requesting project (ISS-180)
	// Skip ownership check if session doesn't exist in DB (not-yet-persisted or in-memory only)
	if sessionProject := service.GetSessionProjectPath(sessionID); sessionProject != "" && sessionProject != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	indexStr := r.URL.Query().Get("index")
	if indexStr == "" {
		// Clear all
		service.ClearQueue(sessionID)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}

	// Remove specific item
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidIndex")
		return
	}

	queue := service.RemoveQueueItem(sessionID, index)
	if queue == nil {
		queue = []model.QueuedMessage{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"queue": queue,
	})
}
