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

	// Look up the ACP connection for this ClawBench session
	mgr := ai.GetACPConnManager()
	conn := mgr.GetConn(req.SessionID)
	if conn == nil {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotRunning")
		return
	}

	client := conn.GetClient()
	if client == nil {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotRunning")
		return
	}

	// We need the ACP session ID to construct the permission key.
	acpSessionID := conn.AcpSID()
	if acpSessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return
	}

	// The frontend sends the permissionBlockID (prefixed with "perm_") as toolCallId.
	// Strip the prefix to recover the original ACP tool call ID used in PermissionKey.
	toolCallID := req.ToolCallID
	if len(toolCallID) > 5 && toolCallID[:5] == "perm_" {
		toolCallID = toolCallID[5:]
	}

	key := ai.PermissionKey(acpSessionID, toolCallID)

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
