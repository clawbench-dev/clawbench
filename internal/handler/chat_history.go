//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"fmt"
	"net/http"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// ServeUserMessageIndex returns lightweight {id, content, files, createdAt} for all user messages
// in a session. Used for the user message index navigation feature.
func ServeUserMessageIndex(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	sessionID, ok := requireSessionID(w, r)
	if !ok {
		return
	}
	// Verify session is not archived (GetSessionBackend filters archived=0)
	if service.GetSessionBackend(sessionID) == "" {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return
	}
	// Verify the session belongs to the requesting project
	if sessionProject := service.GetSessionProjectPath(sessionID); sessionProject != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}
	messages, err := service.GetUserMessageIndex(sessionID)
	if err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("failed to load user message index")))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": messages})
}

// ServeToolCallDetail handles GET /api/ai/chat/tool-call — returns the full
// input/output for a single tool call from the chat_tool_calls table.
// Parameters: tool_id (required), message_id (required), session_id (optional).
// When the tool_id+message_id lookup fails, falls back to tool_id+session_id
// lookup if session_id is provided. This handles ACP sessions that can create
// multiple assistant messages, where the tool call may be stored under a
// different message_id.
func ServeToolCallDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	toolID := r.URL.Query().Get("tool_id")
	messageIDStr := r.URL.Query().Get("message_id")
	sessionID := r.URL.Query().Get("session_id")
	if toolID == "" || messageIDStr == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "ToolIdAndMessageIdRequired")
		return
	}
	var messageID int64
	if _, err := fmt.Sscanf(messageIDStr, "%d", &messageID); err != nil || messageID <= 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidMessageId")
		return
	}

	record, err := service.GetToolCall(toolID, messageID)
	if err != nil || record == nil {
		// Fallback: look up by tool_id + session_id when message_id lookup fails.
		// This handles task executions with resume splits where the tool call
		// is stored under a different assistant message than the last one.
		if sessionID != "" {
			record, err = service.GetToolCallBySession(toolID, sessionID)
		}
		if err != nil || record == nil {
			writeLocalizedError(w, r, model.NotFound(fmt.Errorf("tool call not found"), "ToolCallNotFound"))
			return
		}
	}

	// Verify session is not archived, then check project ownership
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
