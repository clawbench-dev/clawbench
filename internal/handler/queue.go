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
//
// The "insert into the running turn" action lives on its own route
// (QueueInjectHandler, registered at /api/ai/queue/inject) so it gets the same
// auth/ownership middleware as the rest of the queue API.
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

// QueueInjectHandler delivers an already-queued message into the turn that is
// running right now, instead of waiting for the next one. It backs the queued
// bubble's "insert into the current reply" action (shown only for backends that
// can inject — see model.Agent.SupportsMidTurn).
//
// POST /api/ai/queue/inject?session_id=xxx&queueId=xxx
//
// A decline is not an error: the message stays queued and the drain loop will
// still deliver it. The response distinguishes the two so the UI can say
// "couldn't insert" without implying the message was lost.
func QueueInjectHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}

	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}
	queueID := r.URL.Query().Get("queueId")
	if queueID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "QueueIdRequired")
		return
	}

	if sessionProject := service.GetSessionProjectPath(sessionID); sessionProject != "" && sessionProject != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	inserted, msgID := service.InjectQueuedMessage(sessionID, queueID)
	if !inserted {
		// The message is still queued (or already drained) — nothing was lost.
		// 409 rather than 500: the request was well-formed, the session simply
		// was not in an injectable state.
		writeJSON(w, http.StatusConflict, map[string]any{
			"inserted": false,
			"queued":   true,
		})
		return
	}

	// Announce the message as a normal user message so every subscribed device
	// (including the sender) drops its pending bubble and shows it inline. The
	// DB id is preserved from the original enqueue, so ordering is already
	// correct — no re-sort needed.
	//
	// No SenderClientID: the message was queued earlier, possibly by another
	// device, so the acting device must also update. A duplicate user_message
	// for an id it already holds is idempotent on the client.
	ws.EmitToSession(sessionID, ai.StreamEvent{
		Type: "user_message",
		UserMessage: &ai.UserMessageData{
			MessageID: msgID,
			QueueID:   queueID,
			Queued:    false,
		},
	})
	// Tell clients the bubble is no longer waiting in the queue. This is NOT
	// queue_drain: a drain means "this message starts its OWN turn" and makes
	// clients open a new assistant placeholder. An inserted message joins the
	// turn already running, so clients must only clear its pending state and
	// leave the current reply alone (the steer boundary handles the split).
	ws.EmitToSession(sessionID, ai.StreamEvent{
		Type: "queue_inject",
		QueueEvent: &ai.QueueEventData{
			SessionID: sessionID,
			QueueID:   queueID,
			MessageID: msgID,
		},
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"inserted": true,
		"msgId":    msgID,
	})
}

// QueueInterruptHandler stops the turn that is running right now so the next
// queued message can run, WITHOUT ending the session or dropping the queue.
// It backs the queued bubble's "interrupt and send" action (shown for backends
// that cannot inject into a running turn — see model.Agent.SupportsMidTurn).
//
// POST /api/ai/queue/interrupt?session_id=xxx[&queueId=xxx]
//
// The message is already queued (the user queued it by sending, then chose this
// action on its bubble), so nothing is persisted here — this only stops the
// current turn. The session's drain loop then picks the queue up in order and
// runs it, so the reply that was interrupted is replaced by the new message.
//
// Unlike POST /api/ai/chat/cancel this is NOT a user cancel: it keeps the
// queue, does not mark the session stopped, and does not stamp the interrupted
// reply as "cancelled".
func QueueInterruptHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}

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

	// A queued message must exist, otherwise "interrupt and send" would stop the
	// turn and then have nothing to run — a net loss for the user. Report it as
	// a conflict so the UI can explain instead of silently killing the reply.
	queued, err := service.GetQueuedMessages(sessionID)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "QueueReadFailed")
		return
	}
	if len(queued) == 0 {
		writeJSON(w, http.StatusConflict, map[string]any{"interrupted": false, "reason": "empty_queue"})
		return
	}

	if !service.InterruptSessionTurn(sessionID) {
		// No turn registered: it finished between the check and now, and the
		// drain loop will pick the queue up on its own. Not an error.
		writeJSON(w, http.StatusOK, map[string]any{"interrupted": false, "reason": "idle"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"interrupted": true})
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

	// Validate the agent override (ISS-246): an unknown agentId would otherwise
	// silently fall back to the CLI backend and run with the wrong agent/config.
	// Empty agentId keeps the existing default-agent behavior.
	if req.AgentID != "" {
		if _, _, _, _, agentOK := resolveAgentConfig(req.AgentID); !agentOK {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidAgentID")
			return
		}
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
	started, _, msgID, err := service.EnqueueAndMaybeStart(service.EnqueueStartConfig{
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
			// Always queued: sending never joins the running turn, that is an
			// explicit action on the queued bubble (see QueueInjectHandler).
			Queued: true,
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
		// Emit queue_cancel so other subscribed devices remove their pending
		// /_remote bubble for this message immediately (the row is deleted —
		// without the event they would keep a stale bubble until the next
		// loadHistory).
		ws.EmitToSession(sessionID, ai.StreamEvent{
			Type: "queue_cancel",
			QueueEvent: &ai.QueueEventData{
				SessionID: sessionID,
				QueueIDs:  []string{queueID},
			},
		})
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
	// Collect queue IDs before clearing so other devices can remove their
	// pending/_remote bubbles.
	queueIDs, _ := service.GetQueuedQueueIDs(sessionID)
	if err := service.ClearQueuedMessages(sessionID); err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "QueueClearFailed")
		return
	}
	// Emit queue_cancel unconditionally (even with empty queueIDs) so every
	// clear path — including archive/destroy of a session with no queued
	// messages — reaches other devices; a cross-device _remote bubble must not
	// linger until the next loadHistory. The frontend treats an empty queueIds
	// array as a no-op.
	ws.EmitToSession(sessionID, ai.StreamEvent{
		Type: "queue_cancel",
		QueueEvent: &ai.QueueEventData{
			SessionID: sessionID,
			QueueIDs:  queueIDs,
		},
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
