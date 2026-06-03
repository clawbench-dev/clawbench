package handler

import (
	"log/slog"
	"net/http"

	"clawbench/internal/ai"
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

	// Resolve ClawBench session ID → ACP session ID via service
	agentID := service.GetSessionAgentID(req.SessionID)
	if agentID == "" {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return
	}

	// Look up the ACP session ID for this ClawBench session
	pool := ai.GetACPConnectionPool()
	client := pool.GetClient(agentID)
	if client == nil {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotRunning")
		return
	}

	// We need the ACP session ID to construct the permission key.
	// Get it from the pool entry's session mapping.
	acpSessionID := pool.GetACPSessionID(agentID, req.SessionID)
	if acpSessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return
	}

	key := ai.PermissionKey(acpSessionID, req.ToolCallID)

	ok = client.RespondPermission(key, req.OptionID, req.Cancelled)
	if !ok {
		slog.Warn("permission respond: no pending permission found",
			"session_id", req.SessionID,
			"tool_call_id", req.ToolCallID,
		)
		writeLocalizedErrorf(w, r, http.StatusNotFound, "PermissionNotFound")
		return
	}

	slog.Info("permission respond: user responded to permission request",
		"session_id", req.SessionID,
		"tool_call_id", req.ToolCallID,
		"option_id", req.OptionID,
		"cancelled", req.Cancelled,
	)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
	})
}
