package handler

import (
	"net/http"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

const (
	keyCommands  = "commands"
	transportACP = "acp-stdio"
)

// ServeAICommands returns the cached slash commands for an ACP-backed agent.
// Only ACP agents expose commands via available_commands_update.
// CLI agents return an empty list.
//
// GET /api/ai/commands?agent_id=codebuddy
func ServeAICommands(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	agentID := r.URL.Query().Get("agent_id")
	if agentID == "" {
		agentID = model.GetDefaultAgentID()
	}
	if agentID == "" {
		writeJSON(w, http.StatusOK, map[string]any{keyCommands: []any{}})
		return
	}

	agent, found := model.Agents[agentID]
	if !found {
		writeJSON(w, http.StatusOK, map[string]any{keyCommands: []any{}})
		return
	}

	// Only ACP agents have commands
	if agent.Transport != transportACP {
		writeJSON(w, http.StatusOK, map[string]any{keyCommands: []any{}})
		return
	}

	cmds := ai.GetAgentCapabilityRegistry().GetCommands(agent.ID)
	if cmds == nil {
		cmds = []ai.AvailableCommandInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{keyCommands: cmds})
}
