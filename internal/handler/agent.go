//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/ai"
	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/platform"
	"clawbench/internal/service"
)

// IsChinaMainland exports the China detection result for use by other packages.
func IsChinaMainland() bool {
	return platform.IsChinaMainland()
}

const npmMirrorRegistry = "https://registry.npmmirror.com"

// agentIDRe validates agent IDs: alphanumeric, hyphens, underscores, dots only.
var agentIDRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// isValidAgentID checks that an agent ID is non-empty, within length limits,
// and contains only safe characters (no path traversal or injection vectors).
func isValidAgentID(id string) bool {
	if id == "" || utf8.RuneCountInString(id) > 128 {
		return false
	}
	// Must start with a letter or digit; only letters, digits, hyphens, underscores, dots allowed.
	// Reject pure dot sequences like ".." to prevent path traversal.
	if id == "." || id == ".." {
		return false
	}
	return agentIDRe.MatchString(id)
}

// prepareInstallCmd modifies an install command for display:
// Adds China npm mirror registry if in mainland China.
func prepareInstallCmd(installCmd string) string {
	if !strings.HasPrefix(installCmd, "npm install") {
		return installCmd
	}
	if platform.IsChinaMainland() && !strings.Contains(installCmd, "--registry") {
		return installCmd + " --registry=" + npmMirrorRegistry
	}
	return installCmd
}

// ServeAgentSubRoutes handles /api/agents/* sub-routes (e.g. /api/agents/{id}/refresh-models).
func ServeAgentSubRoutes(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if strings.HasSuffix(path, "/common-prompt") && r.Method == http.MethodGet {
		ServeAgentCommonPrompt(w, r)
		return
	}
	if strings.HasSuffix(path, "/refresh-models") && r.Method == http.MethodPost {
		ServeAgentRefreshModels(w, r)
		return
	}
	if strings.HasSuffix(path, "/acp-sessions") && r.Method == http.MethodGet {
		ServeACPSessions(w, r)
		return
	}
	if strings.HasSuffix(path, "/rescan") && r.Method == http.MethodPost {
		serveAgentsRescan(w, r)
		return
	}
	writeLocalizedErrorf(w, r, http.StatusNotFound, "NotFound")
}

// ServeAgentCommonPrompt handles GET /api/agents/common-prompt — returns the
// built-in common prompt that is prepended to all agents' system prompts.
// The frontend uses this to strip the common prefix when displaying the
// user-editable custom system prompt in the settings panel.
func ServeAgentCommonPrompt(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"commonPrompt": model.BuildCommonPrompt(),
	})
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
	if r.Method == http.MethodPost {
		serveAgentsDuplicate(w, r)
		return
	}
	if r.Method == http.MethodDelete {
		serveAgentsDelete(w, r)
		return
	}
	writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
}

func serveAgentsGet(w http.ResponseWriter, _ *http.Request) {
	configMutex.RLock()
	agents := make([]*model.Agent, len(model.AgentList))
	copy(agents, model.AgentList)
	defaultAgent := model.GetDefaultAgentID()
	configMutex.RUnlock()

	// Attach cached ACP mode/thinking/commands state to each agent.
	// This lets the frontend populate mode chips and slash commands without
	// extra API calls. State comes from the AgentCapabilityRegistry (agent-level)
	// so it persists across connection lifecycle.
	type acpState struct {
		Mode         *ai.ModeState             `json:"modeState,omitempty"`
		Effort       *ai.ThinkingEffortState   `json:"thinkingEffortState,omitempty"`
		Commands     []ai.AvailableCommandInfo `json:"commands,omitempty"`
		ModelList    *ai.ModelListState        `json:"modelListState,omitempty"`
		Plan         *ai.PlanState             `json:"planState,omitempty"`
		LoadSession  bool                      `json:"loadSession"`
		ListSessions bool                      `json:"listSessions"`
	}
	states := make(map[string]*acpState, len(agents))
	reg := ai.GetAgentCapabilityRegistry()
	for _, a := range agents {
		if !a.SupportsACP() {
			continue
		}
		// Use BackendSpec.ACPLoadSession as the authoritative source —
		// some agents (e.g. CodeBuddy) report LoadSession in ACP Initialize
		// but don't actually support it.
		spec := model.FindSpecByBackend(a.Backend)
		loadSession := spec != nil && spec.ACPLoadSession
		s := &acpState{LoadSession: loadSession, ListSessions: reg.GetListSessions(a.ID)}

		agentCap := reg.Get(a.ID)
		if agentCap != nil && agentCap.HasData() {
			s.Mode = reg.GetModeState(a.ID, "")
			s.Effort = reg.GetThinkingEffortState(a.ID, "")
			s.Commands = reg.GetCommands(a.ID)
			// Include the agent's currently-selected model from any live ACP
			// connection so the frontend can mark the correct default on the
			// merged model list. Falls back to "" when no active connection.
			s.ModelList = reg.GetModelListState(a.ID, ai.GetACPConnManager().GetCurrentModelIDByAgentID(a.ID))

			// NOTE: Do NOT overwrite a.Models with s.ModelList.Models here.
			// a.Models must always stay the pure CLI-discovered list so the
			// frontend can merge ACP models by ID on top of it (stable display
			// names/order). ACP models are delivered separately via
			// acpStates[].modelListState.
		}
		states[a.ID] = s
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"agents":       agents,
		"defaultAgent": defaultAgent,
		"acpStates":    states,
	})
}

// serveAgentsDuplicate handles POST /api/agents — duplicates an existing agent.
// Expects: {"source_id": "claude", "name": "My Custom Claude"}
// Returns the newly created agent.
func serveAgentsDuplicate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceID string `json:"source_id"`
		Name     string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	if req.SourceID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}
	if req.Name == "" || utf8.RuneCountInString(req.Name) > 64 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidAgentName")
		return
	}

	configMutex.Lock()
	defer configMutex.Unlock()

	clone, err := service.DuplicateAgent(req.SourceID, req.Name)
	if err != nil {
		slog.Error("failed to duplicate agent", "source", req.SourceID, "error", err)
		if strings.Contains(err.Error(), "not found") {
			writeLocalizedErrorf(w, r, http.StatusNotFound, "AgentNotFound")
			return
		}
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	// Add to in-memory maps for immediate reflection
	model.Agents[clone.ID] = clone
	model.AgentList = append(model.AgentList, clone)

	// Populate runtime-only fields
	if spec := model.FindSpecByBackend(clone.Backend); spec != nil {
		if model.CanDiscoverModels(*spec) {
			clone.CanRefreshModels = true
		}
		if len(clone.ThinkingEffortLevels) == 0 && len(spec.ThinkingEffortLevels) > 0 {
			clone.ThinkingEffortLevels = spec.ThinkingEffortLevels
		}
	}
	clone.SupportsCLI = model.BackendSupportsCLI(clone.Backend)

	writeJSON(w, http.StatusOK, clone)
}

// serveAgentsRescan handles POST /api/agents/rescan — re-runs the full agent
// discovery pipeline (detect CLIs → discover models → merge → reload memory).
// This brings back any auto-detected agents that were accidentally deleted.
func serveAgentsRescan(w http.ResponseWriter, _ *http.Request) {
	configMutex.Lock()
	defer configMutex.Unlock()

	model.SyncDiscoverAgentsDB(service.WriteDB())
	discoveredModels := model.SyncDiscoverModels()
	model.MergeDiscoveredDataDB(service.WriteDB(), discoveredModels)

	// Return the current agent list (same shape as GET /api/agents)
	agents := make([]*model.Agent, len(model.AgentList))
	copy(agents, model.AgentList)
	defaultAgent := model.GetDefaultAgentID()

	writeJSON(w, http.StatusOK, map[string]any{
		"agents":       agents,
		"defaultAgent": defaultAgent,
	})
}

func serveAgentsDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	if req.ID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}

	configMutex.Lock()
	defer configMutex.Unlock()

	// Cannot delete the default agent
	if req.ID == model.GetDefaultAgentID() {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "CannotDeleteDefaultAgent")
		return
	}

	agent, ok := model.Agents[req.ID]
	if !ok {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "AgentNotFound")
		return
	}

	// Close ACP connections for this agent before deleting
	if agent.SupportsACP() {
		mgr := ai.GetACPConnManager()
		mgr.CloseConnsByAgentID(req.ID)
		slog.Info("closed ACP connections before agent delete", "agent", req.ID)
	}

	if err := service.DeleteAgent(req.ID); err != nil {
		slog.Error("failed to delete agent", "agent", req.ID, "error", err)
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	// Remove from in-memory maps
	delete(model.Agents, req.ID)
	newAgentList := make([]*model.Agent, 0, len(model.AgentList)-1)
	for _, a := range model.AgentList {
		if a.ID != req.ID {
			newAgentList = append(newAgentList, a)
		}
	}
	model.AgentList = newAgentList

	writeJSON(w, http.StatusOK, map[string]any{"deleted": req.ID})
}

// serveAgentsPatch handles PATCH /api/agents — updates an agent's configurable fields.
// isValidThinkingEffort checks if a thinking effort level is valid for an agent.
// It checks levels based on the agent's effective transport:
//   - ACP mode: only ACP-reported levels from AgentCapabilityRegistry
//   - CLI mode: only static ThinkingEffortLevels from BackendSpec
//   - Neither has levels: allow any value (backward compatible)
func isValidThinkingEffort(agent *model.Agent, level string) bool {
	transport := agent.Transport
	if transport == "" {
		if agent.AcpCommand != "" {
			transport = "acp-stdio"
		} else {
			transport = "cli"
		}
	}

	reg := ai.GetAgentCapabilityRegistry()

	if transport == "acp-stdio" {
		// ACP mode: only check ACP-reported levels
		if es := reg.GetThinkingEffortState(agent.ID, ""); es != nil && len(es.AvailableLevels) > 0 {
			for _, l := range es.AvailableLevels {
				if l.ID == level {
					return true
				}
			}
			return false
		}
		// No ACP levels yet (pool not initialized) — allow any value
		return true
	}

	// CLI mode: check static levels from BackendSpec
	if len(agent.ThinkingEffortLevels) > 0 {
		for _, l := range agent.ThinkingEffortLevels {
			if l == level {
				return true
			}
		}
		return false
	}

	// No static levels and not ACP — allow any value
	return true
}

// Expects: {"id": "claude", "preferred_model": "claude-opus-4-5", "preferred_thinking_effort": "high", ...}
// Patchable fields: preferred_model, preferred_thinking_effort, transport,
// name, specialty, custom_system_prompt, sort_order.
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

	ap := service.AgentPatch{}

	// Validate and apply preferred_mode
	if v, exists := patch["preferred_mode"]; exists {
		modeID, _ := v.(string)
		if modeID != "" {
			// Validate against ACP available modes for this agent
			reg := ai.GetAgentCapabilityRegistry()
			if !reg.IsModeAvailable(agentID, modeID) {
				writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidModeForAgent")
				return
			}
		}
		ap.PreferredMode = &modeID
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
			// Accept models reported by the ACP runtime even if they aren't in
			// the CLI-discovered agent.Models list (runtime union of both sources).
			if !found {
				reg := ai.GetAgentCapabilityRegistry()
				if mls := reg.GetModelListState(agentID, ""); mls != nil {
					for _, m := range mls.Models {
						if m.ID == modelID {
							found = true
							break
						}
					}
				}
			}
			if !found {
				writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidModelForAgent")
				return
			}
		}
		ap.PreferredModel = &modelID
	}

	// Validate and apply preferred_thinking_effort
	if v, exists := patch["preferred_thinking_effort"]; exists {
		level, _ := v.(string)
		if level != "" {
			found := isValidThinkingEffort(agent, level)
			if !found {
				writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidThinkingEffort")
				return
			}
		}
		ap.PreferredThinkingEffort = &level
	}

	// Validate and apply transport (only for agents that support ACP)
	if v, exists := patch["transport"]; exists {
		transport, _ := v.(string)
		spec := model.FindSpecByBackend(agent.Backend)
		hasACP := spec != nil && spec.AcpCommand != ""
		hasCLI := agent.SupportsCLI
		oldTransport := agent.Transport
		switch {
		case transport == "cli" && hasCLI:
			agent.Transport = "cli"
		case transport == "acp-stdio" && hasACP:
			agent.Transport = "acp-stdio"
		default:
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidTransport")
			return
		}
		ap.Transport = &agent.Transport
		// When switching from ACP to CLI, close all ACP connections for this agent
		if oldTransport == "acp-stdio" && agent.Transport == "cli" {
			mgr := ai.GetACPConnManager()
			mgr.CloseConnsByAgentID(agentID)
			slog.Info("closed ACP connections after transport switch to CLI", "agent", agentID)
		}
	}

	// Validate and apply name
	if v, exists := patch["name"]; exists {
		name, _ := v.(string)
		if name == "" || utf8.RuneCountInString(name) > 64 {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidAgentName")
			return
		}
		ap.Name = &name
	}

	// Validate and apply specialty
	if v, exists := patch["specialty"]; exists {
		specialty, _ := v.(string)
		if utf8.RuneCountInString(specialty) > 128 {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidAgentSpecialty")
			return
		}
		ap.Specialty = &specialty
	}

	// Validate and apply custom_system_prompt
	if v, exists := patch["custom_system_prompt"]; exists {
		customPrompt, _ := v.(string)
		if len(customPrompt) > 32*1024 {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidSystemPrompt")
			return
		}
		if containsPromptOverride(customPrompt) {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "SystemPromptOverride")
			return
		}
		ap.CustomSystemPrompt = &customPrompt
	}

	// Validate and apply sort_order
	if v, exists := patch["sort_order"]; exists {
		switch n := v.(type) {
		case float64:
			order := int(n)
			if order < 0 {
				writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidSortOrder")
				return
			}
			ap.SortOrder = &order
		case int:
			if n < 0 {
				writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidSortOrder")
				return
			}
			ap.SortOrder = &n
		default:
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidSortOrder")
			return
		}
	}

	// Persist to database
	if err := service.PatchAgentFields(agentID, ap); err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	// Update in-memory agent for immediate reflection
	if ap.PreferredMode != nil {
		agent.PreferredMode = *ap.PreferredMode
	}
	if ap.PreferredModel != nil {
		agent.PreferredModel = *ap.PreferredModel
	}
	if ap.PreferredThinkingEffort != nil {
		agent.PreferredThinkingEffort = *ap.PreferredThinkingEffort
	}
	if ap.Transport != nil {
		agent.Transport = *ap.Transport
	}
	if ap.Name != nil {
		agent.Name = *ap.Name
	}
	if ap.Specialty != nil {
		agent.Specialty = *ap.Specialty
	}
	if ap.CustomSystemPrompt != nil {
		agent.CustomSystemPrompt = *ap.CustomSystemPrompt
		// Recompose SystemPrompt
		commonPrompt := model.BuildCommonPrompt()
		if commonPrompt != "" && agent.CustomSystemPrompt != "" {
			agent.SystemPrompt = commonPrompt + "\n\n" + agent.CustomSystemPrompt
		} else if commonPrompt != "" {
			agent.SystemPrompt = commonPrompt
		} else {
			agent.SystemPrompt = agent.CustomSystemPrompt
		}
	}
	if ap.SortOrder != nil {
		agent.SortOrder = *ap.SortOrder
	}

	writeJSON(w, http.StatusOK, agent)
}

// containsPromptOverride checks for common prompt injection patterns that attempt
// to override built-in safety rules. This is a best-effort heuristic, not a
// comprehensive security boundary — the actual safety boundary is enforced by
// the AI model itself at inference time.
func containsPromptOverride(prompt string) bool {
	lower := strings.ToLower(prompt)
	overridePatterns := []string{
		"ignore previous instructions",
		"ignore all previous",
		"ignore above instructions",
		"disregard all previous",
		"disregard all above",
		"forget all previous instructions",
	}
	for _, pattern := range overridePatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// ServeAgentRefreshModels handles POST /api/agents/{id}/refresh-models — triggers model re-discovery
// for the specified agent and returns the updated model list. The discovered models completely replace
// the agent's current model list (both in memory and in the cache file).
//
// Refresh strategy: CLI model discovery via BackendSpec (e.g., pi --list-models)
func ServeAgentRefreshModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}

	// Extract agent ID from path: /api/agents/{id}/refresh-models
	path := strings.TrimPrefix(r.URL.Path, "/api/agents/")
	agentID := strings.TrimSuffix(path, "/refresh-models")

	if !isValidAgentID(agentID) {
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

	// CLI model discovery via BackendSpec
	spec := model.FindSpecByBackend(agent.Backend)
	if spec != nil && model.CanDiscoverModels(*spec) {
		canDiscover = true
		models = model.DiscoverModels(*spec)
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
	if err := service.SaveAgent(service.WriteDB(), agent); err != nil {
		slog.Warn("failed to persist model refresh to DB", "agent", agentID, "error", err)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"models": models,
	})
}

// ServeACPSessions handles GET /api/agents/{id}/acp-sessions — lists ACP sessions
// for an agent that supports LoadSession + ListSessions.
func ServeACPSessions(w http.ResponseWriter, r *http.Request) {
	agentID, agent, ok := acpSessionsAgentCheck(w, r)
	if !ok {
		return
	}

	reg := ai.GetAgentCapabilityRegistry()
	mgr := ai.GetACPConnManager()
	conn := mgr.GetConnByAgentID(agentID)
	if conn == nil {
		conn = mgr.GetOrCreateConnNoSession(r.Context(), agent)
	}

	spec := model.FindSpecByBackend(agent.Backend)
	loadSession := spec != nil && spec.ACPLoadSession
	listSessions := reg.GetListSessions(agentID)
	diskListSessions := ai.HasListSessionsFromDisk(agent.Backend)

	if !acpSessionsCapCheck(w, r, loadSession, listSessions, diskListSessions) {
		return
	}

	cursor := r.URL.Query().Get("cursor")
	sessions, nextCursor, ok := fetchACPSessions(w, r, agent, agentID, conn, listSessions, cursor)
	if !ok {
		return
	}

	sessions = filterAndRetitleACPSessions(sessions, agent, r)

	writeJSON(w, http.StatusOK, map[string]any{
		"sessions":   sessions,
		"nextCursor": nextCursor,
	})
}

// acpSessionsAgentCheck validates the agent ID from the URL and returns the
// agent. If validation fails it writes an error response and returns ok=false.
func acpSessionsAgentCheck(w http.ResponseWriter, r *http.Request) (string, *model.Agent, bool) {
	path := strings.TrimPrefix(r.URL.Path, "/api/agents/")
	agentID := strings.TrimSuffix(path, "/acp-sessions")

	if !isValidAgentID(agentID) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return "", nil, false
	}

	configMutex.RLock()
	agent, ok := model.Agents[agentID]
	configMutex.RUnlock()

	if !ok {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "AgentNotFound")
		return "", nil, false
	}

	if !agent.SupportsACP() {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return "", nil, false
	}

	return agentID, agent, true
}

// acpSessionsCapCheck verifies that at least one session enumeration path is
// available. Returns true if the request should proceed; false if an error
// response was written.
func acpSessionsCapCheck(w http.ResponseWriter, r *http.Request, loadSession, listSessions, diskListSessions bool) bool {
	if !loadSession && !listSessions && !diskListSessions {
		writeLocalizedErrorf(w, r, http.StatusNotImplemented, "NotImplemented")
		return false
	}
	if !listSessions && !diskListSessions {
		writeLocalizedErrorf(w, r, http.StatusNotImplemented, "NotImplemented")
		return false
	}
	return true
}

// fetchACPSessions retrieves sessions via the ACP session/list RPC or the
// on-disk scanner fallback. Returns (sessions, nextCursor, ok); if ok is
// false an error response was written.
func fetchACPSessions(w http.ResponseWriter, r *http.Request, agent *model.Agent, agentID string, conn *ai.ACPConn, listSessions bool, cursor string) ([]acp.SessionInfo, *string, bool) {
	if listSessions {
		if conn != nil {
			var cursorPtr *string
			if cursor != "" {
				cursorPtr = &cursor
			}
			sessions, nextCursor, err := conn.ListSessions(r.Context(), cursorPtr)
			if err == nil {
				if cursor == "" && ai.HasListSessionsFromDisk(agent.Backend) {
					cwd := middleware.GetProjectFromCookie(r)
					diskSessions, diskErr := ai.ListSessionsFromDisk(agent, cwd)
					if diskErr != nil {
						slog.Warn("handler: on-disk ListSessions augmentation failed",
							"agent", agentID, "error", diskErr)
					} else {
						sessions = mergeACPSessions(sessions, diskSessions)
					}
				}
				return sessions, nextCursor, true
			}
			slog.Warn("handler: ACP ListSessions failed; trying on-disk fallback",
				"agent", agentID, "error", err)
		} else {
			slog.Warn("handler: failed to spawn ACP connection for ListSessions; trying on-disk fallback",
				"agent", agentID)
		}
		if !ai.HasListSessionsFromDisk(agent.Backend) || cursor != "" {
			if conn == nil {
				writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "ServiceUnavailable")
			} else {
				writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
			}
			return nil, nil, false
		}
	}

	cwd := middleware.GetProjectFromCookie(r)
	sessions, err := ai.ListSessionsFromDisk(agent, cwd)
	if err != nil {
		slog.Error("handler: on-disk ListSessions failed", "agent", agentID, "error", err)
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return nil, nil, false
	}
	return sessions, nil, true
}

func mergeACPSessions(primary, fallback []acp.SessionInfo) []acp.SessionInfo {
	seen := make(map[string]struct{}, len(primary)+len(fallback))
	merged := make([]acp.SessionInfo, 0, len(primary)+len(fallback))
	for _, group := range [][]acp.SessionInfo{primary, fallback} {
		for _, session := range group {
			id := string(session.SessionId)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			merged = append(merged, session)
		}
	}
	sort.SliceStable(merged, func(i, j int) bool {
		return acpSessionUpdatedAt(merged[i]).After(acpSessionUpdatedAt(merged[j]))
	})
	return merged
}

func acpSessionUpdatedAt(session acp.SessionInfo) time.Time {
	if session.UpdatedAt == nil {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, *session.UpdatedAt)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

// filterAndRetitleACPSessions removes sessions already loaded into ClawBench
// and re-derives display titles from transcript data when available.
func filterAndRetitleACPSessions(sessions []acp.SessionInfo, agent *model.Agent, r *http.Request) []acp.SessionInfo {
	if len(sessions) > 0 {
		// Dedup by sessionId first. Agents may report the same session more
		// than once within a single response (e.g. OpenCode's updatedAt-based
		// cursor collides on equal timestamps, or the list changed between
		// page fetches). Without this, duplicates leak through to the resume
		// drawer and accumulate across infinite-scroll pages. Order of first
		// occurrence is preserved.
		seen := make(map[acp.SessionId]struct{}, len(sessions))
		deduped := make([]acp.SessionInfo, 0, len(sessions))
		for _, s := range sessions {
			// Sessions without a real id are not dedupable; keep them all so
			// distinct entries with missing ids are never collapsed together.
			if s.SessionId == "" {
				deduped = append(deduped, s)
				continue
			}
			if _, ok := seen[s.SessionId]; ok {
				continue
			}
			seen[s.SessionId] = struct{}{}
			deduped = append(deduped, s)
		}
		sessions = deduped

		acpSessionIDs := make([]string, len(sessions))
		for i, s := range sessions {
			acpSessionIDs[i] = string(s.SessionId)
		}
		existingACP := findExistingACPSessions(acpSessionIDs)
		filtered := make([]acp.SessionInfo, 0, len(sessions))
		for _, s := range sessions {
			if !existingACP[string(s.SessionId)] {
				filtered = append(filtered, s)
			}
		}
		sessions = filtered
	}

	// Display-only title cleanup: agents title sessions after the last user
	// message, which for ClawBench-origin sessions begins with the injected
	// [System Instructions: ...] block (or a continuation summary). The
	// agent truncates the reported title inside that block, so the user's
	// actual text is not present in the title at all — recovery must
	// re-read the session transcript on disk (read-only; the transcript
	// itself is never modified) and re-derive the title from the first
	// user-typed message.
	//
	// Backend support: title detection (isMachineGeneratedTitle) and this
	// display-layer hook are backend-agnostic, but transcript recovery
	// needs a per-backend resolver (sessionTranscriptResolver). Backends
	// with a registered resolver get transcript-based re-titling;
	// unregistered backends keep the agent-reported title as-is.
	// deriveSessionTitleForAgent uses the SAME resolver registry, so the
	// two lists (external + acp-load) stay consistent automatically.
	//
	// 后端支持：标题检测（isMachineGeneratedTitle）与本展示层钩子与后端无关，但
	// 转录恢复需要逐后端的解析器（sessionTranscriptResolver）。已注册解析器的
	// 后端获取基于转录的重新命名；未注册后端保留 agent 上报标题不变。
	// deriveSessionTitleForAgent 使用同一套解析器注册表，两个列表（外部+acp-load）
	// 自动保持一致。
	cwd := middleware.GetProjectFromCookie(r)
	if r := transcriptResolverFor(agent.Backend); r != nil {
		for i := range sessions {
			if sessions[i].Title == nil {
				continue
			}
			display := acpDisplayTitle(cwd, string(sessions[i].SessionId), *sessions[i].Title, r)
			sessions[i].Title = &display
		}
	}

	return sessions
}

// findExistingACPSessions returns a set of ACP session IDs that already
// exist in ClawBench's session manager (active or archived). These are the
// ACP sessions the user has already loaded/used, so they are filtered out of
// the @resume drawer's "native" list.
//
// A session can be matched to an ACP session id in two ways:
//   - source_session_id = "acp:{acpSessionId}" — set when a session is created
//     via ACP load/resume.
//   - external_session_id = "{acpSessionId}" — the raw session id reported by
//     the backend (e.g. opencode's "ses_..."), captured on every run.
//
// Matching both covers sessions created through either path.
// acpDisplayTitle returns a human-readable display title for an ACP session
// in the external session list ("外部会话"). The agent reports a per-session
// title via the session/list RPC, but for claude that reported title is an
// inconsistent user message (often the LAST or a middle one, not the first),
// so it is NOT trusted as the "first question". The transcript on disk is the
// only reliable source of the first user question. The transcript is only
// ever read.
//
// Tier order (claude), highest first:
//  1. the CLI's persisted session title ("custom-title" transcript record —
//     the auto-generated topic title);
//  2. the transcript's first real user question (machine headers stripped);
//  3. fall back to the agent-reported title (only when the transcript is
//     unreadable or yields no question).
//
// acp-load (会话搜索) reuses this SAME function (with agentTitle=""), so a
// session keeps one title while cycled between the two lists.
//
// 为"外部会话"列表中的 ACP 会话返回可读标题。agent 经 session/list RPC 上报每
// 会话标题，但 claude 上报的是不一致的某条用户消息（常为末问或中间某条，非首问），
// 故不可作为"首问"信任。磁盘转录是首问的唯一可靠来源。转录只读不改。
// 层级（claude，从高到低）：1) CLI 持久化的会话标题（custom-title 记录，自动主题名）；
// 2) 转录首问（剥离机器前缀后）；3) 回退到 agent 上报标题（仅当转录不可读或无首问时）。
// acp-load（会话搜索）复用本函数（传 agentTitle=""），使会话在两列表间循环时标题一致。
func acpDisplayTitle(cwd, sessionID, agentTitle string, r sessionTranscriptResolver) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return agentTitle
	}
	return acpDisplayTitleFromHome(home, cwd, sessionID, agentTitle, r)
}

// acpDisplayTitleFromHome decides the display title for ONE session in the
// external session list ("外部会话"). It runs per session on every list
// request. Walkthrough:
//
// INPUT  cwd        the project dir from the cookie, e.g. /Users/luo/Desktop/palminput
//
//	sessionID  the CLI session id, e.g. fec08cbd-9b04-...
//	agentTitle what the CLI reported via session/list, e.g. "给出完整ID"
//	           (claude reports SOME user message — often the last or a
//	           middle one — NOT reliably the first question)
//
// STEP 1  locate the transcript file on disk:
//
//	  ~/.claude/projects/-Users-luo-Desktop-palminput/<sessionID>.jsonl
//	(slashes in cwd become '-'; if missing, search all project dirs by
//	 sessionID — the session may have been created under another cwd)
//	no file found → path = "", skip to STEP 4.
//
// STEP 2  scan the file for the newest "custom-title" line:
//
//	  {"type":"custom-title","customTitle":"palminput-demo-pcb-design"}
//	found → return it (capped at 50 runes). DONE.
//	(newer claude-code auto-writes this topic title; it is the CLI's
//	 own authoritative name for the session)
//
// STEP 3  scan the file top-down for the first REAL user question:
//   - skip assistant lines
//   - user line → extract its text (content is either a plain string
//     or a list of text blocks; transcriptContentText handles both)
//   - text starting with a machine prefix (see
//     machineGeneratedUserPrefixes) → strip the prefix and keep going;
//     e.g. "[System Instructions: rules]\n\n如何配置自动备份"
//     → "如何配置自动备份"
//     a turn that is 100% machine text (e.g. a compaction header
//     "This session is being continued from a previous ...") → skip
//     to the next user line
//     found → return it (capped at 50 runes). DONE.
//
// STEP 4  nothing readable on disk — last resort, the agent-reported title:
//
//	human text → return it; machine text → return "" (better no
//	title than "[System Instructions:..." noise).
//
// acp-load (会话搜索) calls this SAME function with agentTitle="" so both
// lists share one code path and a session keeps one title while cycled.
//
// acpDisplayTitleFromHome 决定"外部会话"列表中单个会话显示什么标题,每次拉取
// 列表时对每个会话执行一次。操作步骤:
//
// 输入    cwd        cookie 里的项目目录,如 /Users/luo/Desktop/palminput
//
//	sessionID  CLI 会话 ID,如 fec08cbd-9b04-...
//	agentTitle CLI 经 session/list 上报的标题,如 "给出完整ID"
//	           (claude 上报的是"某条"用户消息——常是末问或中间某条,
//	            不保证是首问,所以不能直接信)
//
// 第 1 步  定位磁盘上的转录文件:
//
//	  ~/.claude/projects/-Users-luo-Desktop-palminput/<sessionID>.jsonl
//	(cwd 的斜杠转横杠;找不到再按 sessionID 在所有项目目录里全局搜,
//	 会话可能是在别的目录下创建的)
//	仍无 → path="",直接跳到第 4 步。
//
// 第 2 步  扫描文件里最新的 "custom-title" 行:
//
//	  {"type":"custom-title","customTitle":"palminput-demo-pcb-design"}
//	有 → 返回它(截到 50 字),结束。
//	(新版 claude-code 自动写入的会话主题名,是 CLI 自己的权威命名)
//
// 第 3 步  从文件顶部向下找第一条"真问题":
//   - 跳过 assistant 行
//   - user 行取出文本(content 可能是纯字符串或文本块列表,
//     由 transcriptContentText 统一处理)
//   - 文本以机器前缀开头(见 machineGeneratedUserPrefixes)→ 剥掉前缀
//     继续用剩余部分;例:
//     "[System Instructions: 规则]\n\n如何配置自动备份"
//     → "如何配置自动备份"
//     整轮都是机器文本(如压缩头 "This session is being continued
//     from a previous ...")→ 跳过,看下一条 user 行
//     找到 → 返回它(截到 50 字),结束。
//
// 第 4 步  磁盘上读不到任何可用信息——最后兜底用 CLI 上报标题:
//
//	是人话 → 返回;是机器文本 → 返回空(宁缺勿错,绝不显示噪声)。
//
// acp-load(会话搜索)以 agentTitle="" 调用本函数,两列表共用一条代码路径,
// 会话在两列表间循环时标题保持不变。
func acpDisplayTitleFromHome(home, cwd, sessionID, agentTitle string, r sessionTranscriptResolver) string {
	path := r.TranscriptPath(home, cwd, sessionID)
	// Tier 1: the CLI's own persisted session title ("custom-title" records —
	// auto-generated topic title or manual rename) outranks every derived
	// candidate.
	// Tier 2: the transcript's first real user question (machine headers
	// stripped). The agent-reported title is deliberately NOT used here:
	// claude reports an inconsistent user message (often the last or a middle
	// one), not a reliable first question.
	// Both tiers are extracted in a single file scan (scanTranscriptForTitles).
	//
	// 第 1 层：CLI 自持久化的会话标题（custom-title 记录，自动主题名或手动改名），
	// 优先于一切派生候选。
	// 第 2 层：转录首问（剥离机器前缀后）。此处刻意不用 agent 上报标题：claude
	// 上报的是不一致的某条用户消息（常为末问或中间某条），并非可靠的首问。
	// 两层在单次文件扫描中提取（scanTranscriptForTitles）。
	if path != "" {
		custom, first := r.TitleCandidates(path)
		if custom != "" {
			return capTitle(custom)
		}
		if first != "" {
			return capTitle(first)
		}
	}
	// Tier 3: fall back to the agent-reported title only when the transcript
	// is unreadable or yields no question — but never display machine noise
	// (isMachineGeneratedTitleFor); return empty so the caller falls back further
	// (acp-load: to the replay; external list: empty field).
	// 第 3 层：仅当转录不可读或无首问时回退到 agent 上报标题，但绝不显示机器文本
	// （isMachineGeneratedTitleFor）；返回空，让调用方继续回退（acp-load：到重放；
	// 外部列表：空字段）。
	if agentTitle != "" && !isMachineGeneratedTitleFor(agentTitle, r) {
		return agentTitle
	}
	return ""
}

// capTitle truncates a title candidate to maxReplayTitleRunes.
// 将标题候选截断到 maxReplayTitleRunes（50 字符），超出部分以 "..." 结尾。
func capTitle(t string) string {
	if runes := []rune(t); len(runes) > maxReplayTitleRunes {
		return string(runes[:maxReplayTitleRunes]) + "..."
	}
	return t
}

// deriveSessionTitleForAgent picks the title for an acp-load (import) session,
// i.e. what gets written into chat_sessions.title when the user imports a
// session from the external list into "会话搜索". Operationally it just
// re-runs the external list's derivation on the same transcript:
//
//	user clicks "下载/导入"
//	  → acp-load replays the session via ACP (replay = CLI's CURRENT context)
//	  → replay finishes → this function decides the title:
//
//	      claude backend?
//	        YES → call acpDisplayTitleFromHome(home, projectPath, sid, "")
//	              (the EXACT function the external list uses; agentTitle=""
//	              because acp-load has no session/list RPC to ask the CLI)
//	              → STEP 1-3 of that function run on the transcript
//	                (custom-title → first real question)
//	              → non-empty → write it to DB. DONE.
//	        transcript unreadable / claude not the backend?
//	          → deriveSessionTitleFromReplay(replay): first human message of
//	            the replay, machine prefixes stripped, capped at 50 runes.
//
// Why the transcript instead of the replay: a session compacted N times has
// its original first question already summarized away in the CLI's context,
// so the replay starts with a compaction header and a LATER message — the
// title would drift and disagree with the external list (which reads the
// transcript from the top). Reading the same transcript in both paths keeps
// the two lists byte-identical while the session is cycled between them.
//
// Adding a backend: implement its transcript path resolution + first-question
// extraction (mirror acpTranscriptPath / scanTranscriptForTitles /
// transcriptContentText) as a sessionTranscriptResolver, register it in
// sessionTranscriptResolvers; acpDisplayTitleFromHome callers stay as-is.
//
// deriveSessionTitleForAgent 决定 acp-load(导入)会话的标题——即用户把外部会话
// "下载"进"会话搜索"时写进 chat_sessions.title 的值。操作上就是把外部列表的
// 派生函数在同一份转录上重跑一遍:
//
//	用户点"下载/导入"
//	  → acp-load 经 ACP 重放会话(重放 = CLI 当前上下文)
//	  → 重放完成 → 本函数决定标题:
//
//	      后端已注册 resolver?
//	        是 → 调 resolver.TranscriptPath/CustomTitle/FirstQuestion
//	            (与外部列表完全同一代码路径;agentTitle="" 是因为 acp-load 手头
//	             没有 session/list RPC 可向 CLI 要上报标题)
//	            → 非空 → 写入 DB,结束。
//	        resolver 为 nil / 转录读不到?
//	          → deriveSessionTitleFromReplay(重放):取重放里第一条人类消息,
//	            剥机器前缀,截 50 字。
//
// 为何用转录而非重放:压缩过 N 次的会话,其原始首问早被 CLI 摘要替换,重放以
// 压缩头开头、后面是较晚的消息——标题会漂移,与从顶部读转录的外部列表不一致。
// 两条路径读同一份转录,会话在两列表间循环时标题逐字节一致。
//
// 新增后端:实现 sessionTranscriptResolver 接口并在 sessionTranscriptResolvers
// 注册;acpDisplayTitleFromHome 的调用方无须改动。
func deriveSessionTitleForAgent(agent *model.Agent, projectPath, acpSessionID string, replay []replayMessage) string {
	var resolver sessionTranscriptResolver
	if agent != nil {
		resolver = transcriptResolverFor(agent.Backend)
		if resolver != nil {
			if home, err := os.UserHomeDir(); err == nil {
				path := resolver.TranscriptPath(home, projectPath, acpSessionID)
				if path != "" {
					custom, first := resolver.TitleCandidates(path)
					if custom != "" {
						return capTitle(custom)
					}
					if first != "" {
						return capTitle(first)
					}
				}
			}
		}
	}
	return deriveSessionTitleFromReplay(replay, resolver)
}

// isMachineGeneratedTitleFor reports whether an agent-reported session title
// was derived from a machine-generated user turn for the given backend, using
// the prefix list derived from stripRulesFor (universal + that backend's
// native rules). A nil resolver uses universal rules only. Agents truncate the
// reported title (~200 chars), so the check is truncation-tolerant: a title
// that is a prefix of a known marker also counts.
//
// 判断 agent 上报的会话标题是否来自机器生成的用户轮次（按给定后端的规则集）。
// 前缀列表由 stripRulesFor 推导（通用 + 该后端原生规则）。resolver 为 nil 时
// 只用通用规则。agent 会把标题截断（约 200 字符），故匹配容忍截断。
func isMachineGeneratedTitleFor(title string, r sessionTranscriptResolver) bool {
	if title == "" {
		return false
	}
	for _, marker := range machinePrefixesFor(r) {
		if strings.HasPrefix(title, marker) ||
			(len(title) < len(marker) && strings.HasPrefix(marker, title)) {
			return true
		}
	}
	return false
}

// isMachineGeneratedTitle is the backend-agnostic convenience wrapper that
// checks against both the universal (client-injected) prefix set and the
// claude-native prefix set. Prefer isMachineGeneratedTitleFor when the
// backend is known.
//
// 后端无关的便捷包装，检查通用（客户端注入）+ claude 原生前缀集。后端已知
// 时优先使用 isMachineGeneratedTitleFor。
func isMachineGeneratedTitle(title string) bool {
	// The claude rule set is universal + claude-native, which already
	// subsumes the nil (universal-only) set — one call suffices.
	// claude 规则集 = 通用 + claude 原生，已涵盖 nil（仅通用）规则集——单次调用足够。
	return isMachineGeneratedTitleFor(title, claudeTranscriptResolver{})
}

// acpTranscriptPath returns the expected CLI transcript path for a session,
// e.g. ~/.claude/projects/-Users-luo/<sessionId>.jsonl, or "" when the
// inputs are insufficient or unsafe. sessionID comes from the ACP agent's
// session/list response, so anything carrying path separators, traversal
// segments or glob metacharacters is rejected outright.
//
// 返回会话 CLI 转录的预期路径，如 ~/.claude/projects/-Users-luo/<sessionId>.jsonl；
// 输入不足或不安全时返回 ""。sessionID 来自 ACP agent 的 session/list 响应，
// 故凡携带路径分隔符、穿越段（..）或 glob 元字符的一律直接拒绝（防路径穿越）。
func acpTranscriptPath(home, cwd, sessionID string) string {
	if home == "" || cwd == "" || sessionID == "" {
		return ""
	}
	if strings.ContainsAny(sessionID, `/\`) || strings.Contains(sessionID, "..") ||
		strings.ContainsAny(sessionID, "*?[]") {
		return ""
	}
	munged := strings.ReplaceAll(cwd, "/", "-")
	// TODO: on Windows, cwd uses backslashes (e.g. C:\Users\luo\Desktop\foo)
	// which are not munged here. The Claude CLI itself replaces both / and \
	// with '-', so Windows users would get a mismatch. Add
	// strings.ReplaceAll(munged, `\`, "-") when Windows support is needed.
	return filepath.Join(home, ".claude", "projects", munged, sessionID+".jsonl")
}

func findExistingACPSessions(acpSessionIDs []string) map[string]bool {
	if len(acpSessionIDs) == 0 {
		return nil
	}
	// Build IN clause placeholders (each id appears twice: prefixed and raw).
	ph := ""
	vals := make([]any, 0, len(acpSessionIDs)*2)
	for _, sid := range acpSessionIDs {
		if ph != "" {
			ph += ","
		}
		ph += "?,?"
		vals = append(vals, "acp:"+sid, sid)
	}

	result := make(map[string]bool)

	// Four placeholders per id total (2 per IN clause). Build the arg slice
	// with explicit capacity to avoid overlapping-append aliasing.
	args := make([]any, 0, len(vals)*2)
	args = append(args, vals...)
	args = append(args, vals...)

	rows, err := service.ReadDB().Query( // background DB query, no request context available in this helper
		"SELECT source_session_id, external_session_id FROM chat_sessions WHERE source_session_id IN ("+ph+") OR external_session_id IN ("+ph+")",
		args...,
	)
	if err != nil {
		slog.Warn("handler: failed to query existing ACP sessions for filtering", "error", err)
		return result
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var sourceID, extID sql.NullString
		if err := rows.Scan(&sourceID, &extID); err == nil {
			// Mark each returned ACP id as existing if its "acp:"-prefixed
			// source id or its raw external id matches.
			for _, sid := range acpSessionIDs {
				if sourceID.Valid && sourceID.String == "acp:"+sid {
					result[sid] = true
				}
				if extID.Valid && extID.String == sid {
					result[sid] = true
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		slog.Warn("handler: error iterating ACP session rows", "error", err)
	}
	return result
}

// ServeBackends returns the list of AI backends supported by ClawBench.
// Used by the welcome overlay to show users what CLI agents can be auto-detected.
func ServeBackends(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}

	type backendInfo struct {
		ID                   string   `json:"id"`
		Name                 string   `json:"name"`
		Specialty            string   `json:"specialty"`
		DefaultCmd           string   `json:"default_cmd"`
		ThinkingEffortLevels []string `json:"thinking_effort_levels,omitempty"`
		InstallCmd           string   `json:"install_cmd,omitempty"`
	}

	backends := make([]backendInfo, 0, len(model.GetBackendRegistry()))
	for _, spec := range model.GetBackendRegistry() {
		if spec.NoCLI {
			continue // skip non-CLI backends (e.g. mock)
		}
		backends = append(backends, backendInfo{
			ID:                   spec.ID,
			Name:                 spec.Name,
			Specialty:            spec.Specialty,
			DefaultCmd:           spec.DefaultCmd,
			ThinkingEffortLevels: spec.ThinkingEffortLevels,
			InstallCmd:           prepareInstallCmd(spec.InstallCmd),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"backends": backends,
	})
}
