package handler

import (
	"net/http"

	"clawbench/internal/middleware"
	"clawbench/internal/service"
)

// ServeChatRecommendation handles GET /api/chat/recommendation?session_id=...
// It returns the most recent persisted conversation recommendation (对话推荐)
// for a session. This lets a client that was offline when the session completed
// fetch and show the recommendation after opening the session.
func ServeChatRecommendation(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
		return
	}
	// Verify the session belongs to the current project scope.
	projectPath := middleware.GetProjectFromCookie(r)
	if sessionProject := service.GetSessionProjectPath(sessionID); sessionProject == "" {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return
	} else if projectPath != "" && projectPath != sessionProject {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return
	}

	rec := service.LatestChatRecommendation(sessionID)
	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":     sessionID,
		"recommendation": rec,
	})
}
