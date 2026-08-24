package handler

import (
	"log/slog"
	"net/http"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

// ServeSessionReset handles POST /api/ai/session/reset — kills the ACP agent
// process for a stuck session and lets the next prompt re-establish the same
// agent session via ResumeSession.
//
// Use case: when an ACP agent session gets stuck (e.g. a turn ended in a
// "tool approved but never executed" state), subsequent prompts return empty
// responses in milliseconds. The frontend shows a "重置会话" button on the
// error/warning banner; clicking it calls this endpoint to force the agent
// process to restart, then re-sends the last user message.
//
// Reset semantics: the external session ID mapping is deliberately PRESERVED,
// so the next prompt runs ResumeSession to re-attach to the same agent session
// — the agent's conversation context and the ClawBench chat history are both
// kept intact. Only the hung agent process is recycled.
func ServeSessionReset(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	var req struct {
		SessionID string `json:"sessionId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.SessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}

	// Verify the session belongs to the requesting project
	if sessionProject := service.GetSessionProjectPath(req.SessionID); sessionProject != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	// Cancel an in-flight turn before killing the connection
	if service.IsSessionRunning(req.SessionID) {
		slog.Info("session reset: cancelling running session", "session_id", req.SessionID)
		service.CancelSession(req.SessionID)
	}

	// Kill the agent process and drop the connection from the pool. The
	// external session ID is intentionally NOT cleared: the next prompt will
	// read it from the DB and run ResumeSession, restoring the same agent
	// session (context preserved). Run in a goroutine because CloseConn calls
	// cmd.Wait() which can block if the agent subprocess doesn't exit cleanly,
	// preventing the HTTP response from being sent.
	go ai.GetACPConnManager().CloseConn(req.SessionID)

	slog.Info("session reset: ACP connection closed, next prompt will resume the same session",
		"session_id", req.SessionID)

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
