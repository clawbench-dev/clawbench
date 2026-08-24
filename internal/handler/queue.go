//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"encoding/json"
	"net/http"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// QueueHandler handles pending message queue operations.
// POST   /api/ai/queue?session_id=xxx  — enqueue a message (unified send endpoint)
// GET    /api/ai/queue?session_id=xxx  — get current queued messages
// DELETE /api/ai/queue?session_id=xxx[&queueId=xxx] — cancel a queued message or clear all
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
	if sessionProject := service.GetSessionProjectPath(sessionID); sessionProject != "" && sessionProject != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	// Verify the session exists and resolve its backend/agent.
	info := service.GetSessionFullInfo(sessionID)
	if info == nil {
		writeLocalizedError(w, r, model.NotFound(nil, "SessionNotFound"))
		return
	}

	var req struct {
		Message   string            `json:"message"`
		QueueID   string            `json:"queueId"`
		FilePaths []string          `json:"filePaths"`
		Files     []model.FileEntry `json:"files"`
		AgentID   string            `json:"agentId"`
		ModelID   string            `json:"modelId"`
		Transport string            `json:"transport"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}

	if req.Message == "" && len(req.Files) == 0 && len(req.FilePaths) == 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MessageOrFilesRequired")
		return
	}

	// Persist the message + start execution or signal the running drain loop.
	started, err := service.EnqueueAndMaybeStart(service.EnqueueStartConfig{
		SessionID:   sessionID,
		ProjectPath: info.ProjectPath,
		BackendName: info.Backend,
		AgentID:     req.AgentID,
		Message:     req.Message,
		Files:       req.Files,
		QueueID:     req.QueueID,
	})
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "EnqueueFailed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"started": started,
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

	if sessionProject := service.GetSessionProjectPath(sessionID); sessionProject != "" && sessionProject != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	msgs, err := service.GetQueuedMessages(sessionID)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "QueueReadFailed")
		return
	}
	if msgs == nil {
		msgs = []model.ChatMessage{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"queue": msgs,
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

	if sessionProject := service.GetSessionProjectPath(sessionID); sessionProject != "" && sessionProject != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	// Cancel by queueId (preferred).
	queueID := r.URL.Query().Get("queueId")
	if queueID != "" {
		if err := service.CancelQueuedMessage(sessionID, queueID); err != nil {
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "QueueDeleteFailed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}

	// Legacy index-based delete — no longer supported (queued messages are
	// identified by queueId; index is ambiguous under concurrent drains).
	indexStr := r.URL.Query().Get("index")
	if indexStr != "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidQueueDelete")
		return
	}

	// Clear all queued messages for the session.
	if err := service.ClearQueuedMessages(sessionID); err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "QueueClearFailed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
