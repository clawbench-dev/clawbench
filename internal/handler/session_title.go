package handler

import (
	"context"
	"net/http"
	"time"

	"clawbench/internal/middleware"
	"clawbench/internal/service"
)

// ServeGenerateSessionTitle handles POST /api/ai/session/generate-title?session_id=...
// It summarizes the session's user messages into a candidate title using the
// shared AI summary model (ai_summary.*). The title is returned for the user to
// confirm — it is NOT persisted here; the client saves it through the normal
// rename endpoint so an auto-generated title stays an explicit user choice.
//
// This is the MANUAL entry point. It shares its whole implementation with the
// automatic rename path (service.GenerateSessionTitleFromMessages) so the two
// cannot drift; this handler only owns the HTTP concerns (method, ownership,
// error mapping).
func ServeGenerateSessionTitle(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}
	// Ownership check, same as the recommendation endpoint: the session must
	// belong to the project named by the cookie.
	projectPath := middleware.GetProjectFromCookie(r)
	if sessionProject := service.GetSessionProjectPath(sessionID); sessionProject == "" {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return
	} else if projectPath != "" && projectPath != sessionProject {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return
	}

	// The summary model is user-configured; without it there is nothing to call.
	if service.ConfigSummaryModelMissing() {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SummaryModelNotConfigured")
		return
	}

	// The LLM call is bounded server-side; the client timeout is raised to
	// match so a slow model does not surface as a false failure.
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	title, err := service.GenerateSessionTitleFromMessages(ctx, sessionID, "")
	if err != nil {
		// "No user messages" is a client-side condition (nothing to summarize),
		// not a server fault — report it as such so the UI can explain why.
		if service.IsNoUserMessagesError(err) {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "NoUserMessagesToSummarize")
			return
		}
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "GenerateTitleFailed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"title": title})
}
