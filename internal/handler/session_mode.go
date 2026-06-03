package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

// ServeSessionMode handles POST /api/ai/session/mode — switches the session
// mode for an ACP-backed session (e.g., "ask" → "code"). Only works for
// ACP agents that support session modes.
func ServeSessionMode(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	var req struct {
		SessionID string `json:"sessionId"`
		ModeID    string `json:"modeId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.SessionID == "" || req.ModeID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}

	// Verify the session belongs to the requesting project
	if sessionProject := service.GetSessionProjectPath(req.SessionID); sessionProject != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	// Get the agent ID for this session
	agentID := service.GetSessionAgentID(req.SessionID)
	if agentID == "" {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return
	}

	// Look up the agent configuration
	agent, agentFound := model.Agents[agentID]
	if !agentFound {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "AgentNotFound")
		return
	}

	// Get the ACP connection pool and find the agent's connection
	pool := ai.GetACPConnectionPool()

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	entry, err := pool.GetOrCreate(ctx, agent)
	if err != nil {
		slog.Warn("session mode: failed to get ACP connection", "agent_id", agentID, "error", err)
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotRunning")
		return
	}

	// Set the mode via SetSessionConfigOption (v2 style, works for both v1 and v2)
	entry.SetSessionConfigOption(ctx, req.SessionID, "mode", req.ModeID)

	// Persist mode to session DB so it survives restarts
	_ = service.UpdateSessionMode(req.SessionID, req.ModeID)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"modeId": req.ModeID,
	})
}
