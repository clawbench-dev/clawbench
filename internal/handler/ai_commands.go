package handler

import (
	"net/http"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// ServeAICommands returns the cached slash commands and ACP mode/thinking state
// for an ACP-backed agent. Only ACP agents expose commands via available_commands_update.
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
		writeJSON(w, http.StatusOK, map[string]any{"commands": []any{}})
		return
	}

	agent, found := model.Agents[agentID]
	if !found {
		writeJSON(w, http.StatusOK, map[string]any{"commands": []any{}})
		return
	}

	// Only ACP agents have commands
	if agent.Transport != "acp-stdio" {
		writeJSON(w, http.StatusOK, map[string]any{"commands": []any{}})
		return
	}

	pool := ai.GetACPConnectionPool()
	client := pool.GetClient(agent.ID)

	acpCmds := client.GetCommands()
	cmds := make([]ai.AvailableCommandInfo, 0, len(acpCmds))
	for _, c := range acpCmds {
		info := ai.AvailableCommandInfo{
			Name:        c.Name,
			Description: c.Description,
		}
		if c.Input != nil && c.Input.Unstructured != nil {
			info.InputHint = c.Input.Unstructured.Hint
		}
		cmds = append(cmds, info)
	}

	resp := map[string]any{"commands": cmds}

	// Also return cached mode/thinking state so the frontend can populate
	// mode chips before the first message is sent.
	if modeState, _, effortState := pool.GetCachedStateByAgentID(agent.ID); modeState != nil || effortState != nil {
		resp["modeState"] = modeState
		resp["thinkingEffortState"] = effortState
	}

	writeJSON(w, http.StatusOK, resp)
}
