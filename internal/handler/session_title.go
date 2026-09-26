package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/summarize"
)

// ServeGenerateSessionTitle handles POST /api/ai/session/generate-title?session_id=...
// It summarizes the session's user messages into a candidate title using the
// shared AI summary model (ai_summary.*). The title is returned for the user to
// confirm — it is NOT persisted here; the client saves it through the normal
// rename endpoint so an auto-generated title stays an explicit user choice.
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
	if model.ConfigInstance.AISummary.API.BaseURL == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SummaryModelNotConfigured")
		return
	}
	summarizer := summarize.NewAISummarizer(model.ConfigInstance.AISummary)
	if summarizer == nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SummaryModelNotConfigured")
		return
	}

	messages, err := service.GetMessagesBySessionID(sessionID)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	userMessages := make([]string, 0, len(messages))
	for _, m := range messages {
		if m.Role != "user" {
			continue
		}
		if text := strings.TrimSpace(service.ExtractPlainText(m.Content)); text != "" {
			userMessages = append(userMessages, text)
		}
	}
	if len(userMessages) == 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "NoUserMessagesToSummarize")
		return
	}

	language := model.ConfigInstance.Language
	if language == "" {
		language = model.DefaultLanguage
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	title, err := summarize.GenerateSessionTitle(ctx, summarizer, userMessages, language)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "GenerateTitleFailed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"title": title})
}
