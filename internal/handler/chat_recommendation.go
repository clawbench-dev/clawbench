package handler

import (
	"net/http"
	"strconv"

	"clawbench/internal/middleware"
	"clawbench/internal/service"
)

// ServeChatRecommendation handles GET /api/chat/recommendation?session_id=...
// It returns the most recent persisted conversation recommendation (推荐回复)
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

	// The client tells us which assistant message it wants a recommendation for.
	// Only a recommendation generated for that exact message is returned, so a
	// stale recommendation from an earlier reply is never surfaced.
	var messageID int64
	if v := r.URL.Query().Get("message_id"); v != "" {
		messageID, _ = strconv.ParseInt(v, 10, 64)
	}

	rec := service.LatestChatRecommendation(r.Context(), sessionID, messageID)
	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":     sessionID,
		"message_id":     messageID,
		"recommendation": rec,
	})
}
