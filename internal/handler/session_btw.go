package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"clawbench/internal/service"
)

// ServeBtwQuestion handles /api/ai/session/btw.
//
// "/btw" is a ClawBench-owned side question: the user asks something "by the
// way" in the current session, and the shared AI summary model answers it from
// a compressed snapshot of the conversation. The current session's agent is not
// involved.
//
//	POST { sessionId, question } -> { ok: true, record }
//	GET  ?session_id=...        -> { questions: [...] }
//
// POST persists the question and returns the stored record. A model failure is
// NOT an HTTP error: the record is stored with a non-empty `error` and returned
// with 200, because the chat list must render the marker for a question the
// user really asked (the drawer then explains the failure). Only problems that
// create no record at all — a missing/foreign session, an empty question, an
// unconfigured summary model — are reported as 4xx.
//
// GET lists a session's records so the client can place the anchors.
func ServeBtwQuestion(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		serveBtwAsk(w, r)
	case http.MethodGet:
		serveBtwList(w, r)
	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

// resolveBtwSession performs the shared session resolution and ownership check.
// Returns ok=false after writing the error response.
func resolveBtwSession(w http.ResponseWriter, r *http.Request, bodySessionID string) (string, bool) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return "", false
	}
	// Body sessionId wins; fall back to query/cookie for clients that omit it
	// (mirrors ServeForkSession).
	sessionID := bodySessionID
	if sessionID == "" {
		sessionID = getSessionID(r)
	}
	if sessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return "", false
	}
	// Ownership check, same as generate-title: the session must belong to the
	// project named by the cookie.
	if sessionProject := service.GetSessionProjectPath(sessionID); sessionProject == "" {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return "", false
	} else if projectPath != "" && projectPath != sessionProject {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return "", false
	}
	return sessionID, true
}

func serveBtwAsk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"sessionId"`
		Question  string `json:"question"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxChatBodySize)
	if !decodeJSON(w, r, &req) {
		return
	}

	sessionID, ok := resolveBtwSession(w, r, req.SessionID)
	if !ok {
		return
	}

	// The summary model is user-configured; without it there is nothing to ask
	// and no record is worth creating.
	if service.ConfigSummaryModelMissing() {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SummaryModelNotConfigured")
		return
	}

	// The LLM call is bounded server-side (the context may be large). The client
	// timeout is raised to match so a slow model is not reported as a failure.
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	rec, err := service.AnswerBtwQuestion(ctx, sessionID, req.Question)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrBtwEmptyQuestion):
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "BtwQuestionRequired")
		case errors.Is(err, service.ErrSummaryModelNotConfigured):
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "SummaryModelNotConfigured")
		case rec.ID > 0:
			// The question was persisted but the model failed. Return the record
			// so the client can still show its anchor; `record.error` carries the
			// failure for the drawer.
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "record": rec})
		default:
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "BtwFailed")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "record": rec})
}

func serveBtwList(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := resolveBtwSession(w, r, "")
	if !ok {
		return
	}
	questions, err := service.ListBtwQuestions(sessionID)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "BtwFailed")
		return
	}
	if questions == nil {
		questions = []service.BtwQuestion{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"questions": questions})
}
