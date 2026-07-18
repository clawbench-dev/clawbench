package handler

import (
	"log/slog"
	"net/http"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// ServePermissionRespond handles POST /api/ai/permission/respond — delivers
// a user's approval/rejection response to a pending ACP permission request.
// The frontend calls this when the user clicks an option on the PermissionApproval card.
func ServePermissionRespond(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	var req struct {
		SessionID  string `json:"sessionId"`  // ClawBench session ID
		ToolCallID string `json:"toolCallId"` // ACP tool call ID
		OptionID   string `json:"optionId"`   // PermissionOption.OptionId (empty = cancelled)
		Cancelled  bool   `json:"cancelled"`  // True if user cancelled the request
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.SessionID == "" || req.ToolCallID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}

	// Verify the session belongs to the requesting project
	if sessionProject := service.GetSessionProjectPath(req.SessionID); sessionProject != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	err := service.RespondPermission(req.SessionID, req.ToolCallID, req.OptionID, req.Cancelled)
	if err != nil {
		slog.Warn("permission respond: failed", "error", err, "session_id", req.SessionID, "tool_call_id", req.ToolCallID)
		writeLocalizedErrorf(w, r, http.StatusNotFound, "PermissionNotFound")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
	})
}
