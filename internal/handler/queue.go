//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"encoding/json"
	"log/slog"
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

	inserted, msgID, err := service.InjectQueuedMessage(sessionID, queueID)
	if err != nil {
		// The claim succeeded but the row could not be restored to the queue, so
		// it is now invisible to the drain loop. Do NOT claim "queued" here —
		// that would tell the user their message is safe when it is stranded.
		// Tell them to resend instead.
		slog.Error("queue: inject left the message stranded",
			slog.String("session", sessionID),
			slog.String("queue_id", queueID),
			slog.String("error", err.Error()))
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "QueueInjectStranded")
		return
	}
	if !inserted {
		// Benign decline: the message is still queued (or was already drained by
		// its own turn). Nothing was lost. 409 rather than 500 — the request was
		// well-formed, the session simply was not in an injectable state.
		writeJSON(w, http.StatusConflict, map[string]any{
			"inserted": false,
			"queued":   true,
		})
		return
	}

	// Tell clients the entry is no longer waiting in the queue. This is NOT
	// queue_drain: a drain means "this message starts its OWN turn" and makes
	// clients open a new assistant placeholder. An inserted message joins the
	// turn already running, so clients must only drop it from the queue panel
	// and leave the current reply alone (the steer boundary handles the split).
	//
	// No content/files: the message's own user_message event is emitted by
	// InjectQueuedMessage (it is a real chat_history row now), so emitting them
	// here too would duplicate the bubble. No SenderClientID: the message may
	// have been queued by another device, so the acting device must update too.
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
// POST /api/ai/queue/interrupt?session_id=xxx&queueId=xxx
//
// Semantics: "stop the current reply; the queue continues in order." It does
// NOT jump the given message to the front — queue order is DB id order, and
// re-ordering would break the "insert preserves the original row id" property
// the whole design relies on. queueId is therefore a freshness check: it must
// still be a queued message, so a stale click on an already-drained bubble
// cannot silently kill an unrelated turn.
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
	queueID := r.URL.Query().Get("queueId")
	if queueID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "QueueIdRequired")
		return
	}

	if sessionProject := service.GetSessionProjectPath(sessionID); sessionProject != "" && sessionProject != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	// The acting message must still be queued. Two reasons: a stale bubble (its
	// turn already ran) should not kill the current turn, and interrupting with
	// nothing queued would stop the reply and have nothing to run — a net loss.
	queued, err := service.GetQueuedMessages(sessionID)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "QueueReadFailed")
		return
	}
	found := false
	for _, q := range queued {
		if q.QueueID == queueID {
			found = true
			break
		}
	}
	if !found {
		writeJSON(w, http.StatusConflict, map[string]any{"interrupted": false, "reason": "not_queued"})
		return
	}

	// Read the turn id BEFORE deciding, then interrupt only that exact turn. The
	// old turn may finish and the drain loop start the next queued message in
	// between; without the id check this would cut off a reply the user never
	// asked to stop (the check-then-act race).
	turnID, ok := service.CurrentTurnID(sessionID)
	if !ok {
		// No turn running: the drain loop will pick the queue up on its own.
		writeJSON(w, http.StatusOK, map[string]any{"interrupted": false, "reason": "idle"})
		return
	}
	if !service.InterruptSessionTurnIfCurrent(sessionID, turnID) {
		// The turn was replaced (or consumed) between the read and the act. Same
		// outcome as idle: nothing of the user's was stopped, and the queue runs.
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
	// EnqueueAndMaybeStart emits the announcement itself: user_message when the
	// session was idle (the message is a real chat_history row now) or
	// queue_added when a runner is live. Centralizing it there keeps this
	// handler and the /api/ai/chat busy path from drifting.
	started, _, err := service.EnqueueAndMaybeStart(service.EnqueueStartConfig{
		SessionID:      sessionID,
		ProjectPath:    info.ProjectPath,
		BackendName:    info.Backend,
		AgentID:        req.AgentID,
		Message:        req.Message,
		Files:          validatedFiles,
		QueueID:        req.QueueID,
		ModelID:        req.ModelID,
		Transport:      req.Transport,
		SenderClientID: req.ClientID,
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
		// Quote entries carry text, not a path. Their Path is a label and may
		// be empty, so resolving it would 404 the whole enqueue.
		if fEntry.IsQuote() {
			validated = append(validated, validatedQuoteEntry(fEntry))
			continue
		}
		// URL entries carry an external address, not a local path. Resolving one
		// as a path would 404 ("File not found: owner/repo#123") and drop the
		// attachment — which is what happened before the chat endpoint's URL
		// handling was mirrored here.
		if fEntry.IsURL() {
			entry, ok := validatedURLEntry(w, r, fEntry)
			if !ok {
				return nil, false
			}
			validated = append(validated, entry)
			continue
		}
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
		msgs = []model.QueuedMessage{}
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
