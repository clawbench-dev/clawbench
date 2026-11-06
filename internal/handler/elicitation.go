package handler

import (
	"log/slog"
	"net/http"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// ServeElicitationRespond handles POST /api/ai/elicitation/respond — delivers
// the user's answer to a blocking ACP form elicitation (the question card an
// agent such as OpenCode renders through `elicitation/create`).
//
// The card is an ordinary AskUserQuestion card in the chat, so this is the
// second exit of that card: instead of sending the selection as a chat message
// (which would queue behind the very turn that is blocked) it is handed back to
// the agent as the RPC result.
func ServeElicitationRespond(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	var req struct {
		SessionID     string         `json:"sessionId"`     // ClawBench session ID
		ElicitationID string         `json:"elicitationId"` // card block id ("elic_…")
		Content       map[string]any `json:"content"`       // property name → answer
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.SessionID == "" || req.ElicitationID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}

	// Verify the session belongs to the requesting project
	if sessionProject := service.GetSessionProjectPath(req.SessionID); sessionProject != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	if err := service.RespondElicitation(req.SessionID, req.ElicitationID, req.Content); err != nil {
		// A stale card (history restored after the turn ended) or a double
		// submit lands here. The frontend reverts the card so it stays
		// answerable instead of pretending the answer went through.
		slog.Warn("elicitation respond: failed",
			"error", err, "session_id", req.SessionID, "elicitation_id", req.ElicitationID)
		writeLocalizedErrorf(w, r, http.StatusNotFound, "NotFound")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
