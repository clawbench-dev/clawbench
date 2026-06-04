//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

// ServeAgentSubRoutes handles /api/agents/* sub-routes (e.g. /api/agents/{id}/refresh-models).
func ServeAgentSubRoutes(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if strings.HasSuffix(path, "/refresh-models") && r.Method == http.MethodPost {
		ServeAgentRefreshModels(w, r)
		return
	}
	writeLocalizedErrorf(w, r, http.StatusNotFound, "NotFound")
}

// ServeAgents returns the list of configured AI agents.
func ServeAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		serveAgentsGet(w, r)
		return
	}
	if r.Method == http.MethodPatch {
		serveAgentsPatch(w, r)
		return
	}
	writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
}

func serveAgentsGet(w http.ResponseWriter, _ *http.Request) { //nolint:gocognit,gocyclo // agent state assembly has many branches, each simple
	configMutex.RLock()
	agents := make([]*model.Agent, len(model.AgentList))
	copy(agents, model.AgentList)
	defaultAgent := model.GetDefaultAgentID()
	configMutex.RUnlock()

	// Attach cached ACP mode/thinking/commands state to each agent.
	// This lets the frontend populate mode chips and slash commands without
	// extra API calls. If the ACP pool cache is empty (before first message
	// or after idle timeout), fall back to DB-persisted state.
	type acpState struct {
		Mode      *ai.ModeState             `json:"modeState,omitempty"`
		Effort    *ai.ThinkingEffortState   `json:"thinkingEffortState,omitempty"`
		Commands  []ai.AvailableCommandInfo `json:"commands,omitempty"`
		ModelList *ai.ModelListState        `json:"modelListState,omitempty"`
	}
	states := make(map[string]*acpState, len(agents))
	pool := ai.GetACPConnectionPool()
	for _, a := range agents {
		if a.Transport != transportACP {
			continue
		}
		var ms *ai.ModeState
		var es *ai.ThinkingEffortState
		var cmds []ai.AvailableCommandInfo
		var ml *ai.ModelListState

		// Try pool cache first
		if pms, _, pes, pml := pool.GetCachedStateByAgentID(a.ID); pms != nil || pes != nil || pml != nil {
			ms = pms
			es = pes
			ml = pml
		}
		if pcmds := pool.GetCommandsByAgentID(a.ID); len(pcmds) > 0 {
			cmds = pcmds
		}

		// Fall back to DB-persisted state when pool is empty
		if ms == nil && es == nil && len(cmds) == 0 && ml == nil {
			if a.AcpModeState != "" {
				var dbMs ai.ModeState
				if json.Unmarshal([]byte(a.AcpModeState), &dbMs) == nil && len(dbMs.AvailableModes) > 0 {
					ms = &dbMs
				}
			}
			if a.AcpThinkingState != "" {
				var dbEs ai.ThinkingEffortState
				if json.Unmarshal([]byte(a.AcpThinkingState), &dbEs) == nil && len(dbEs.AvailableLevels) > 0 {
					es = &dbEs
				}
			}
			if a.AcpCommands != "" && a.AcpCommands != "[]" {
				if err := json.Unmarshal([]byte(a.AcpCommands), &cmds); err != nil {
					slog.Warn("failed to unmarshal ACP commands from DB", "agent", a.ID, "error", err)
				}
			}
			if a.AcpModelListState != "" {
				var dbMl ai.ModelListState
				if json.Unmarshal([]byte(a.AcpModelListState), &dbMl) == nil && len(dbMl.Models) > 0 {
					ml = &dbMl
				}
			}
		}

		// When ACP provides a model list, override the agent's Models
		// so the frontend ModelModal shows ACP models instead of CLI-discovered ones.
		if ml != nil && len(ml.Models) > 0 {
			a.Models = ml.Models
		}

		if ms != nil || es != nil || len(cmds) > 0 || ml != nil {
			states[a.ID] = &acpState{Mode: ms, Effort: es, Commands: cmds, ModelList: ml}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"agents":       agents,
		"defaultAgent": defaultAgent,
		"acpStates":    states,
	})
}

// serveAgentsPatch handles PATCH /api/agents — updates an agent's preferred_model and/or preferred_thinking_effort.
// Expects: {"id": "claude", "preferred_model": "claude-opus-4-5", "preferred_thinking_effort": "high"}
// Only preferred_model and preferred_thinking_effort are patchable (whitelist).
// The original thinking_effort (agent default) is never modified — scheduled tasks use it.
func serveAgentsPatch(w http.ResponseWriter, r *http.Request) { //nolint:gocognit,gocyclo // multi-field agent patch logic
	var patch map[string]any
	if !decodeJSON(w, r, &patch) {
		return
	}

	agentID, _ := patch["id"].(string)
	if agentID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}

	configMutex.Lock()
	defer configMutex.Unlock()

	agent, ok := model.Agents[agentID]
	if !ok {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "AgentNotFound")
		return
	}

	// Validate and apply preferred_model
	if v, exists := patch["preferred_model"]; exists {
		modelID, _ := v.(string)
		if modelID != "" {
			found := false
			for _, m := range agent.Models {
				if m.ID == modelID {
					found = true
					break
				}
			}
			if !found {
				writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidModelForAgent")
				return
			}
		}
		agent.PreferredModel = modelID
	}

	// Validate and apply preferred_thinking_effort
	if v, exists := patch["preferred_thinking_effort"]; exists {
		level, _ := v.(string)
		if level != "" && len(agent.ThinkingEffortLevels) > 0 {
			found := false
			for _, l := range agent.ThinkingEffortLevels {
				if l == level {
					found = true
					break
				}
			}
			if !found {
				writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidThinkingEffort")
				return
			}
		}
		agent.PreferredThinkingEffort = level
	}

	// Persist to database
	if err := service.PatchAgent(service.DB, agentID, agent.PreferredModel, agent.PreferredThinkingEffort); err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	writeJSON(w, http.StatusOK, agent)
}

// ServeAgentRefreshModels handles POST /api/agents/{id}/refresh-models — triggers model re-discovery
// for the specified agent and returns the updated model list. The discovered models completely replace
// the agent's current model list (both in memory and in the cache file).
//
// Refresh strategy (in priority order):
// 1. CLI model discovery via BackendSpec (e.g., pi --list-models)
// 2. Fallback: re-read models from ProviderSpec.KnownModels (embedded provider_models.json)
//
//nolint:gocognit,gocyclo // refresh logic has multiple discovery paths, each with error handling
func ServeAgentRefreshModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}

	// Extract agent ID from path: /api/agents/{id}/refresh-models
	path := strings.TrimPrefix(r.URL.Path, "/api/agents/")
	agentID := strings.TrimSuffix(path, "/refresh-models")

	if agentID == "" || strings.Contains(agentID, "/") {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}

	configMutex.Lock()
	defer configMutex.Unlock()

	agent, ok := model.Agents[agentID]
	if !ok {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "AgentNotFound")
		return
	}

	var models []model.AgentModel
	canDiscover := false // whether any discovery method is available

	// Find provider spec early — used for filtering and fallback
	providerSpec := findProviderSpecForAgent(agentID)

	// Strategy 1: CLI model discovery via BackendSpec
	spec := model.FindSpecByBackend(agent.Backend)
	if spec != nil && model.CanDiscoverModels(*spec) {
		canDiscover = true
		discovered := model.DiscoverModels(*spec)

		// If agent has a provider (from setup wizard), filter to that provider's models.
		// Pi --list-models returns all providers' models in "provider/model" format.
		if providerSpec != nil && len(discovered) > 0 {
			prefix := providerSpec.ID + "/"
			for _, m := range discovered {
				if strings.HasPrefix(m.ID, prefix) {
					m.ID = strings.TrimPrefix(m.ID, prefix)
					m.Name = strings.TrimPrefix(m.Name, prefix)
					models = append(models, m)
				}
			}
			if len(models) == 0 {
				// No models matched the prefix — use all discovered models
				models = discovered
			}
		} else {
			models = discovered
		}
	}

	// Strategy 2: Fallback to ProviderSpec.KnownModels from agent_api_keys
	// Shows ALL models for that provider (not just the ones user configured)
	if len(models) == 0 && providerSpec != nil && len(providerSpec.KnownModels) > 0 {
		canDiscover = true
		slog.Info("model refresh: CLI discovery failed, using KnownModels from provider", "agent", agentID, "provider", providerSpec.ID)
		models = model.KnownModelsToAgentModels(providerSpec.KnownModels)
	}

	if len(models) == 0 {
		// No discovery method available at all
		if !canDiscover {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "ModelDiscoveryNotSupported")
			return
		}
		// Discovery method available but returned nothing — check for specific errors
		if spec != nil {
			if err := model.CheckCLIExistsErr(spec.DefaultCmd); err != nil {
				slog.Warn("model refresh failed: CLI not available", "agent", agentID, "backend", agent.Backend, "cmd", spec.DefaultCmd, "error", err)
				writeLocalizedErrorf(w, r, http.StatusNotFound, "CLINotFound")
				return
			}
		}
		slog.Warn("model refresh returned no models", "agent", agentID, "backend", agent.Backend)
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "ModelDiscoveryFailed")
		return
	}

	// Update in-memory agent (regardless of ModelsAutoDetected — manual refresh always overrides)
	agent.Models = models
	agent.ModelsAutoDetected = true

	// Update database
	if err := service.SaveAgent(service.DB, agent); err != nil {
		slog.Warn("failed to persist model refresh to DB", "agent", agentID, "error", err)
	}

	// Update cache file
	if err := model.WriteModelCache(model.ModelCacheDir, agent.Backend, models); err != nil {
		slog.Warn("failed to write model cache after refresh", "backend", agent.Backend, "error", err)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"models": models,
	})
}

// findProviderSpecForAgent looks up the provider for an agent from the agent_api_keys table
// and returns the corresponding ProviderSpec.
func findProviderSpecForAgent(agentID string) *model.ProviderSpec {
	return service.FindProviderSpecForAgent(agentID)
}
