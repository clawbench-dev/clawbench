//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

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
	if strings.HasSuffix(path, "/install") && r.Method == http.MethodPost {
		serveAgentsInstall(w, r)
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

//nolint:gocyclo // serveAgentsGet fan-outs across ACP/CLI transport branches
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
		Usage        *ai.UsageState            `json:"usageState,omitempty"`
		LoadSession  bool                      `json:"loadSession"`
		ListSessions bool                      `json:"listSessions"`
	}
	states := make(map[string]*acpState, len(agents))
	reg := ai.GetAgentCapabilityRegistry()
	for _, a := range agents {
		if a.SupportsACP() {
			// ACP agents: populate from AgentCapabilityRegistry
			agentCap := reg.Get(a.ID)
			if agentCap == nil || !agentCap.HasData() {
				// Agent supports ACP but pool hasn't been initialized yet.
				// Still include a minimal state so the frontend can show
				// loadSession/listSessions capabilities from DB.
				loadSession := reg.GetLoadSession(a.ID)
				listSessions := reg.GetListSessions(a.ID)
				if loadSession || listSessions {
					states[a.ID] = &acpState{LoadSession: loadSession, ListSessions: listSessions}
				}
				continue
			}

			var ms *ai.ModeState
			var es *ai.ThinkingEffortState
			var cmds []ai.AvailableCommandInfo
			var ml *ai.ModelListState
			var us *ai.UsageState

			ms = reg.GetModeState(a.ID, "")
			es = reg.GetThinkingEffortState(a.ID, "")
			cmds = reg.GetCommands(a.ID)
			ml = reg.GetModelListState(a.ID, "")
			us = reg.GetUsageState(a.ID)

			// When ACP provides a model list, override the agent's Models
			// so the frontend SessionSettingModal shows ACP models instead of CLI-discovered ones.
			if ml != nil && len(ml.Models) > 0 {
				a.Models = ml.Models
			}

			if ms != nil || es != nil || len(cmds) > 0 || ml != nil || us != nil {
				states[a.ID] = &acpState{
					Mode: ms, Effort: es, Commands: cmds, ModelList: ml, Usage: us,
					LoadSession: reg.GetLoadSession(a.ID), ListSessions: reg.GetListSessions(a.ID),
				}
			}
			// Even without mode/effort/commands/model, include LoadSession/ListSessions
			if states[a.ID] == nil && (reg.GetLoadSession(a.ID) || reg.GetListSessions(a.ID)) {
				states[a.ID] = &acpState{LoadSession: reg.GetLoadSession(a.ID), ListSessions: reg.GetListSessions(a.ID)}
			}
		}
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

	writeJSON(w, http.StatusOK, clone)
}

// serveAgentsRescan handles POST /api/agents/rescan — re-runs the full agent
// discovery pipeline (detect CLIs → discover models → merge → reload memory).
// This brings back any auto-detected agents that were accidentally deleted.
func serveAgentsRescan(w http.ResponseWriter, _ *http.Request) {
	configMutex.Lock()
	defer configMutex.Unlock()

	present := model.SyncDiscoverAgentsDB(service.WriteDB())
	discoveredModels := model.SyncDiscoverModels()
	model.MergeDiscoveredDataDB(service.WriteDB(), discoveredModels, present)

	// Return the current agent list (same shape as GET /api/agents)
	agents := make([]*model.Agent, len(model.AgentList))
	copy(agents, model.AgentList)
	defaultAgent := model.GetDefaultAgentID()

	writeJSON(w, http.StatusOK, map[string]any{
		"agents":       agents,
		"defaultAgent": defaultAgent,
	})
}

// installMu enforces one install at a time.
var installMu sync.Mutex

// serveAgentsInstall handles POST /api/agents/install — runs InstallCmd for a
// backend and streams stdout/stderr via SSE. Only one install at a time.
// Expects: {"backend_id": "opencode"}
func serveAgentsInstall(w http.ResponseWriter, r *http.Request) { //nolint:gocyclo // SSE install streaming has multiple sequential branches
	var req struct {
		BackendID string `json:"backend_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	// Find the BackendSpec
	var spec *model.BackendSpec
	for i := range model.GetBackendRegistry() {
		s := &model.GetBackendRegistry()[i]
		if s.ID == req.BackendID {
			spec = s
			break
		}
	}
	if spec == nil {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "BackendNotFound")
		return
	}
	if spec.InstallCmd == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "BackendNotInstallable")
		return
	}

	// One install at a time
	if !installMu.TryLock() {
		writeLocalizedErrorf(w, r, http.StatusConflict, "InstallInProgress")
		return
	}
	defer installMu.Unlock()

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		slog.Error("response writer does not support flushing for install SSE")
		return
	}

	// Emit initial state
	_, _ = fmt.Fprintf(w, "event: install_start\ndata: {\"backend_id\":%q,\"command\":%q}\n\n", spec.ID, spec.InstallCmd)
	flusher.Flush()

	// Execute install command (no sudo)
	// Note: strings.Fields works because all current InstallCmd values are simple
	// space-separated tokens (no quoting needed). If future commands need quoted
	// arguments, switch to sh.Split or similar.
	cmdParts := strings.Fields(spec.InstallCmd)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, cmdParts[0], cmdParts[1:]...)
	cmd.Env = os.Environ()

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_, _ = fmt.Fprintf(w, "event: install_error\ndata: {\"error\":%q}\n\n", err.Error())
		flusher.Flush()
		return
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		_, _ = fmt.Fprintf(w, "event: install_error\ndata: {\"error\":%q}\n\n", err.Error())
		flusher.Flush()
		return
	}
	if err := cmd.Start(); err != nil {
		_, _ = fmt.Fprintf(w, "event: install_error\ndata: {\"error\":%q,\"command\":%q}\n\n", err.Error(), spec.InstallCmd)
		flusher.Flush()
		return
	}

	// Stream stdout and stderr line-by-line as SSE events.
	// Use a channel to merge both streams.
	type logLine struct {
		line   string
		stream string
	}
	logCh := make(chan logLine, 64)

	// Reader goroutine for stdout
	var readerWg sync.WaitGroup
	readerWg.Add(2)
	go func() {
		defer readerWg.Done()
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			logCh <- logLine{line: scanner.Text(), stream: "stdout"}
		}
	}()

	// Reader goroutine for stderr
	go func() {
		defer readerWg.Done()
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			logCh <- logLine{line: scanner.Text(), stream: "stderr"}
		}
	}()

	// Close logCh after both readers finish (pipes close at process exit)
	go func() {
		readerWg.Wait()
		close(logCh)
	}()

	// Merger goroutine: send exit error when process completes
	exitErrCh := make(chan error, 1)
	go func() {
		exitErrCh <- cmd.Wait()
	}()

	// Heartbeat ticker
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	// Main loop: forward log lines as SSE events, with heartbeats
	for {
		select {
		case ll, ok := <-logCh:
			if !ok {
				// Channel closed (readers finished) — wait for exit status
				logCh = nil
				continue
			}
			_, _ = fmt.Fprintf(w, "event: install_log\ndata: {\"line\":%q,\"stream\":%q}\n\n", ll.line, ll.stream)
			flusher.Flush()
		case exitErr := <-exitErrCh:
			// Drain remaining log lines (channel will be closed by reader goroutines)
			for ll := range logCh {
				_, _ = fmt.Fprintf(w, "event: install_log\ndata: {\"line\":%q,\"stream\":%q}\n\n", ll.line, ll.stream)
				flusher.Flush()
			}
			if exitErr != nil {
				_, _ = fmt.Fprintf(w, "event: install_error\ndata: {\"error\":%q,\"command\":%q}\n\n", exitErr.Error(), spec.InstallCmd)
			} else {
				_, _ = fmt.Fprintf(w, "event: install_success\ndata: {\"backend_id\":%q}\n\n", spec.ID)
			}
			flusher.Flush()
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			// Client disconnected
			cancel()
			return
		}
	}
}

// serveAgentsDelete handles DELETE /api/agents — deletes a single agent.
// Expects: {"id": "claude"}. Cannot delete the default agent.
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
// Expects: {"id": "claude", "preferred_model": "claude-opus-4-5", "preferred_thinking_effort": "high", ...}
// Patchable fields: preferred_model, preferred_thinking_effort, transport,
// name, icon, specialty, custom_system_prompt, sort_order.
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
		ap.PreferredThinkingEffort = &level
	}

	// Validate and apply transport (only for agents that support ACP)
	if v, exists := patch["transport"]; exists {
		transport, _ := v.(string)
		spec := model.FindSpecByBackend(agent.Backend)
		hasACP := spec != nil && spec.AcpCommand != ""
		oldTransport := agent.Transport
		switch {
		case transport == "cli":
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

	// Validate and apply icon
	if v, exists := patch["icon"]; exists {
		icon, _ := v.(string)
		if utf8.RuneCountInString(icon) > 8 {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidAgentIcon")
			return
		}
		ap.Icon = &icon
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
	if ap.Icon != nil {
		agent.Icon = *ap.Icon
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
//
//nolint:gocyclo // refresh logic has multiple discovery paths, each with error handling
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

	// Find provider spec early — used for filtering
	providerSpec := findProviderSpecForAgent(r.Context(), agentID)

	// CLI model discovery via BackendSpec
	spec := model.FindSpecByBackend(agent.Backend)
	if spec != nil && model.CanDiscoverModels(*spec) {
		canDiscover = true
		discovered := model.DiscoverModels(*spec)

		// If agent has a provider (from initial setup), filter to that provider's models.
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

// findProviderSpecForAgent looks up the provider for an agent from the agent_api_keys table
// and returns the corresponding ProviderSpec. Used for provider prefix filtering during model refresh.
func findProviderSpecForAgent(ctx context.Context, agentID string) *model.ProviderSpec {
	if !service.DBReady() {
		return nil
	}
	var providerID string
	if err := service.ReadDB().QueryRowContext(ctx, "SELECT provider FROM agent_api_keys WHERE agent_id = ?", agentID).Scan(&providerID); err != nil {
		return nil
	}
	return model.FindProviderSpec(providerID)
}

// ServeACPSessions handles GET /api/agents/{id}/acp-sessions — lists ACP sessions
// for an agent that supports LoadSession + ListSessions.
//
//nolint:gocyclo // ServeACPSessions has multiple sequential checks and branches for ACP capability validation; restructuring would reduce readability
func ServeACPSessions(w http.ResponseWriter, r *http.Request) {
	// Extract agent ID from path: /api/agents/{id}/acp-sessions
	path := strings.TrimPrefix(r.URL.Path, "/api/agents/")
	agentID := strings.TrimSuffix(path, "/acp-sessions")

	if agentID == "" || strings.Contains(agentID, "/") {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}

	configMutex.RLock()
	agent, ok := model.Agents[agentID]
	configMutex.RUnlock()

	if !ok {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "AgentNotFound")
		return
	}

	if !agent.SupportsACP() {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}

	reg := ai.GetAgentCapabilityRegistry()

	// Try to get an existing alive connection first.
	mgr := ai.GetACPConnManager()
	conn := mgr.GetConnByAgentID(agentID)

	// If no alive connection exists, try to spawn one to discover capabilities.
	// This solves the chicken-and-egg problem: GetListSessions is only populated
	// after Initialize, which requires spawning a connection. We use EnsureAlive
	// which spawns without creating a session.
	if conn == nil {
		conn = mgr.GetOrCreateConnNoSession(r.Context(), agent)
	}

	// Check capabilities — they may have been populated by the EnsureAlive
	// call above (via spawnLocked → Initialize), or from DB persistence.
	loadSession := reg.GetLoadSession(agentID)
	listSessions := reg.GetListSessions(agentID)

	// If neither capability is supported, return 501
	if !loadSession && !listSessions {
		writeLocalizedErrorf(w, r, http.StatusNotImplemented, "NotImplemented")
		return
	}

	// If ListSessions is not supported, return 501 — the drawer shows
	// "not supported" message. The user can still use @resume with a
	// known session ID if LoadSession is supported.
	if !listSessions {
		writeLocalizedErrorf(w, r, http.StatusNotImplemented, "NotImplemented")
		return
	}

	// We know the agent supports ListSessions but couldn't get a connection.
	if conn == nil {
		slog.Warn("handler: failed to spawn ACP connection for ListSessions", "agent", agentID)
		writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "ServiceUnavailable")
		return
	}

	cursor := r.URL.Query().Get("cursor")
	var cursorPtr *string
	if cursor != "" {
		cursorPtr = &cursor
	}

	sessions, nextCursor, err := conn.ListSessions(r.Context(), cursorPtr)
	if err != nil {
		slog.Error("handler: ListSessions failed", "agent", agentID, "error", err)
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	// Filter out ACP sessions that already exist in ClawBench's session manager.
	// Each loaded ACP session has source_session_id = "acp:{acpSessionId}".
	// Active sessions: user already has this conversation — don't show it.
	// Soft-deleted sessions: will be hard-deleted and recreated on load,
	// so also don't show them to avoid confusion.
	if len(sessions) > 0 {
		acpSessionIDs := make([]string, len(sessions))
		for i, s := range sessions {
			acpSessionIDs[i] = string(s.SessionId)
		}
		existingACP := findExistingACPSessions(acpSessionIDs)
		filtered := make([]acp.SessionInfo, 0, len(sessions))
		for _, s := range sessions {
			if !existingACP["acp:"+string(s.SessionId)] {
				filtered = append(filtered, s)
			}
		}
		sessions = filtered
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"sessions":   sessions,
		"nextCursor": nextCursor,
	})
}

// findExistingACPSessions returns a set of source_session_id values
// (formatted as "acp:{acpSessionId}") for ACP sessions that already
// exist in ClawBench's session manager (active or soft-deleted).
// This is used to filter out already-loaded sessions from the ACP
// session list displayed in the @resume drawer.
func findExistingACPSessions(acpSessionIDs []string) map[string]bool {
	if len(acpSessionIDs) == 0 {
		return nil
	}
	// Build IN clause placeholders
	placeholders := ""
	sourceIDs := make([]any, len(acpSessionIDs))
	for i, sid := range acpSessionIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		sourceIDs[i] = "acp:" + sid
	}

	result := make(map[string]bool)
	rows, err := service.ReadDB().Query( // background DB query, no request context available in this helper
		"SELECT source_session_id FROM chat_sessions WHERE source_session_id IN ("+placeholders+")",
		sourceIDs...,
	)
	if err != nil {
		slog.Warn("handler: failed to query existing ACP sessions for filtering", "error", err)
		return result
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var sourceID string
		if err := rows.Scan(&sourceID); err == nil {
			result[sourceID] = true
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
		Icon                 string   `json:"icon"`
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
			Icon:                 spec.Icon,
			Specialty:            spec.Specialty,
			DefaultCmd:           spec.DefaultCmd,
			ThinkingEffortLevels: spec.ThinkingEffortLevels,
			InstallCmd:           spec.InstallCmd,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"backends": backends,
	})
}
