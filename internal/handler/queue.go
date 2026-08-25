//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/ws"
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
		ClientID  string            `json:"clientId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}

	if req.Message == "" && len(req.Files) == 0 && len(req.FilePaths) == 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MessageOrFilesRequired")
		return
	}

	// Validate file paths: reject traversal outside the project and resolve to
	// absolute paths with isDir from os.Stat, matching the POST /api/ai/chat
	// path. Without this the unified endpoint would persist raw (possibly
	// malicious) paths (R3).
	validatedFiles, ok := validateQueueFiles(w, r, projectPath, req.FilePaths, req.Files)
	if !ok {
		return
	}

	// Persist the message + start execution or signal the running drain loop.
	// msgID is the persisted DB id of the message, used to broadcast a
	// user_message event so other devices see it before it drains
	// (cross-device sync).
	started, msgID, err := service.EnqueueAndMaybeStart(service.EnqueueStartConfig{
		SessionID:   sessionID,
		ProjectPath: info.ProjectPath,
		BackendName: info.Backend,
		AgentID:     req.AgentID,
		Message:     req.Message,
		Files:       validatedFiles,
		QueueID:     req.QueueID,
		ModelID:     req.ModelID,
		Transport:   req.Transport,
	})
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "EnqueueFailed")
		return
	}

	// Emit user_message to other session subscribers for cross-device sync.
	// SenderClientID lets the sending device skip its own echo. MessageID is
	// the real persisted DB id (> 0), matching the POST /api/ai/chat path.
	ws.EmitToSession(sessionID, ai.StreamEvent{
		Type: "user_message",
		UserMessage: &ai.UserMessageData{
			MessageID:      msgID,
			Content:        req.Message,
			Files:          validatedFiles,
			SenderClientID: req.ClientID,
			QueueID:        req.QueueID,
		},
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"started": started,
	})
}

// validateQueueFiles validates and resolves attached file paths for the queue
// endpoint, mirroring the POST /api/ai/chat handler's checks: every path must
// stay within the project (path traversal → 403) and exist (missing → 404).
// Returns the validated file entries (absolute paths, isDir from os.Stat) and
// ok=false when an error response has been written.
func validateQueueFiles(w http.ResponseWriter, r *http.Request, projectPath string, filePaths []string, fileEntries []model.FileEntry) ([]model.FileEntry, bool) {
	basePath, _ := filepath.Abs(projectPath)

	// filePaths (legacy raw paths) → validated entries.
	validated := make([]model.FileEntry, 0, len(filePaths)+len(fileEntries))
	for _, fp := range filePaths {
		fAbsPath, ok := validateAndResolvePath(w, r, basePath, fp)
		if !ok {
			return nil, false
		}
		info, err := os.Stat(fAbsPath)
		if err != nil {
			writeLocalizedErrorf(w, r, http.StatusNotFound, "FileNotFound", map[string]any{"Path": fp})
			return nil, false
		}
		validated = append(validated, model.FileEntry{Path: fAbsPath, IsDir: info.IsDir()})
	}

	// files (structured entries with optional line ranges) → validated entries.
	for _, fEntry := range fileEntries {
		fAbsPath, ok := validateAndResolvePath(w, r, basePath, fEntry.Path)
		if !ok {
			return nil, false
		}
		info, err := os.Stat(fAbsPath)
		if err != nil {
			writeLocalizedErrorf(w, r, http.StatusNotFound, "FileNotFound", map[string]any{"Path": fEntry.Path})
			return nil, false
		}
		validated = append(validated, model.FileEntry{
			Path:      fAbsPath,
			IsDir:     info.IsDir(),
			StartLine: fEntry.StartLine,
			EndLine:   fEntry.EndLine,
		})
	}
	return validated, true
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
