package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"clawbench/internal/service"
)

// ServeBtwQuestion handles POST /api/ai/session/btw.
//
// "/btw" is a ClawBench-owned side question: the user asks something "by the
// way" in the current session, and the shared AI summary model answers it from
// a compressed snapshot of the conversation. The current session's agent is not
// involved and nothing is persisted — the answer is returned for the client to
// display in a drawer.
//
// Request body: { sessionId: string, question: string }
// Response:     { ok: true, answer: string }
func ServeBtwQuestion(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	var req struct {
		SessionID string `json:"sessionId"`
		Question  string `json:"question"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxChatBodySize)
	if !decodeJSON(w, r, &req) {
		return
	}

	// Body sessionId wins; fall back to query/cookie for clients that omit it
	// (mirrors ServeForkSession).
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = getSessionID(r)
	}
	if sessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}
	// Ownership check, same as generate-title: the session must belong to the
	// project named by the cookie.
	if sessionProject := service.GetSessionProjectPath(sessionID); sessionProject == "" {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return
	} else if projectPath != "" && projectPath != sessionProject {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return
	}

	// The summary model is user-configured; without it there is nothing to ask.
	if service.ConfigSummaryModelMissing() {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SummaryModelNotConfigured")
		return
	}

	// The LLM call is bounded server-side (the context may be large). The client
	// timeout is raised to match so a slow model is not reported as a failure.
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	answer, err := service.AnswerBtwQuestion(ctx, sessionID, req.Question)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrBtwEmptyQuestion):
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "BtwQuestionRequired")
		default:
			// Includes an empty model answer: the summarize layer treats that as
			// an error, so a successful call always carries non-empty text.
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "BtwFailed")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "answer": answer})
}
