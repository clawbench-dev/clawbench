package handler

import (
	"fmt"
	"net/http"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// ServeThinkingDetail handles GET /api/ai/chat/thinking — returns the full text
// for a single thinking block from the chat_thinking table.
// Parameters: think_id (required), message_id (required), session_id (optional).
// When the think_id+message_id lookup fails, falls back to think_id+session_id
// (mirrors ServeToolCallDetail for ACP multi-message sessions).
func ServeThinkingDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	thinkID := r.URL.Query().Get("think_id")
	messageIDStr := r.URL.Query().Get("message_id")
	sessionID := r.URL.Query().Get("session_id")
	if thinkID == "" || messageIDStr == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "ThinkIdAndMessageIdRequired")
		return
	}
	var messageID int64
	if _, err := fmt.Sscanf(messageIDStr, "%d", &messageID); err != nil || messageID <= 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidMessageId")
		return
	}

	record, err := service.GetThinking(thinkID, messageID)
	if err != nil || record == nil {
		if sessionID != "" {
			record, err = service.GetThinkingBySession(thinkID, sessionID)
		}
		if err != nil || record == nil {
			writeLocalizedError(w, r, model.NotFound(fmt.Errorf("thinking not found"), "ThinkingNotFound"))
			return
		}
	}

	if service.GetSessionBackend(record.SessionID) == "" {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return
	}
	if sessionProject := service.GetSessionProjectPath(record.SessionID); sessionProject != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	writeJSON(w, http.StatusOK, record)
}
