package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

// ServeSessionRewind handles POST /api/ai/session/rewind — the "回溯/Rewind"
// action that truncates a session's history IN PLACE at an earlier assistant
// message and resets the AI-side session so the conversation restarts there.
//
// Semantics:
//   - The anchor message (beforeMessageId) and everything before it are kept;
//     all messages after it are deleted from chat_history (and child tables).
//   - external_session_id is cleared and the live ACP connection closed, so the
//     next user send creates a brand-new ACP session whose first prompt gets the
//     retained history injected as fork-context (the existing fork flow — see
//     buildChatRequest's `resume && resolvedExtID == ""` branch).
//   - The plain text of the first removed user message is returned as
//     restoredText so the frontend can pre-fill the input box for re-editing.
//     Nothing is auto-sent.
func ServeSessionRewind(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	var req struct {
		SessionID       string `json:"sessionId"`
		BeforeMessageID int64  `json:"beforeMessageId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.SessionID == "" || req.BeforeMessageID <= 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRewindPoint")
		return
	}

	// Verify the session belongs to the requesting project
	if sessionProject := service.GetSessionProjectPath(req.SessionID); sessionProject != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	// Cancel an in-flight turn before truncating. The executor's post-cancel
	// persistence writes are all serialized under the global writeMu and target
	// the streaming row by id — once truncation deletes that row they become
	// no-ops (FinalizeStreamingMessage affects 0 rows, tool/metadata writes hit a
	// deleted message_id). Proceeding immediately mirrors Archive/Destroy/Reset.
	if service.IsSessionRunning(req.SessionID) {
		slog.Info("session rewind: cancelling running session", "session_id", req.SessionID)
		service.CancelSession(req.SessionID)
	}

	res, err := service.TruncateSessionAfterMessage(req.SessionID, req.BeforeMessageID)
	if err != nil {
		slog.Error("handler: failed to rewind session", "session_id", req.SessionID, "error", err)
		switch {
		case errors.Is(err, service.ErrRewindAnchorNotFound),
			errors.Is(err, service.ErrRewindAnchorNotAssistant),
			errors.Is(err, service.ErrRewindAnchorStreaming):
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRewindPoint")
		default:
			model.WriteError(w, model.Internal(err))
		}
		return
	}
	if res.DeletedCount == 0 {
		// Nothing followed the anchor — a no-op rewind must not touch the AI-side
		// session mapping (clearing it would force a fresh session + full history
		// re-injection on the next message even though nothing was removed).
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "NothingToRewind")
		return
	}

	// Clear the AI-side session mapping so the next prompt starts a fresh session
	// with the truncated history injected as context. Without this, the pool's
	// in-memory acpSID (or the DB external_session_id) would ResumeSession the old
	// agent transcript, whose context is longer than the truncated DB history.
	service.ClearExternalSessionID(req.SessionID)

	// Remove the connection from the pool SYNCHRONOUSLY so a re-send right after
	// this response (user edits the prefilled question and hits send) can never
	// reuse the stale conn — whose in-memory acpSID still points at the old,
	// longer transcript that ResumeSession would otherwise re-attach to. The
	// blocking close() (cmd.Wait) runs in a goroutine.
	if conn := ai.GetACPConnManager().RemoveConn(req.SessionID); conn != nil {
		go conn.Close()
	}

	// Broadcast a session_update so other open clients reload the truncated
	// history. WS-only: no push notification, no pending event.
	service.EmitSessionEventWSOnly(req.SessionID, "rewound", true)

	slog.Info("session rewound",
		slog.String("session", req.SessionID),
		slog.Int64("anchor_message", req.BeforeMessageID),
		slog.Int64("deleted_messages", res.DeletedCount))

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"sessionId":    req.SessionID,
		"restoredText": res.RestoredText,
		"deletedCount": res.DeletedCount,
	})
}
