package ai

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"clawbench/internal/dbutil"
	"clawbench/internal/model"
)

// boolPtrVal returns the dereferenced bool value, or "<nil>" if nil.
func boolPtrVal(p *bool) string {
	if p == nil {
		return "<nil>"
	}
	if *p {
		return "true"
	}
	return "false"
}

// AgentCapability holds agent-level ACP capabilities that are shared
// across all sessions of the same agent. These are inherent to the
// agent binary (e.g., Claude's available modes: [ask, architect, code])
// and do not vary between sessions.
type AgentCapability struct {
	AvailableModes           []ModeDef
	AvailableThinkingEfforts []ThinkingEffortDef
	AvailableModels          []model.AgentModel
	AvailableCommands        []AvailableCommandInfo
	ConfigOptionState        *ConfigOptionState
	CachedUsageState         *UsageState // Last-known usage from any active connection (best-effort fallback)
	LoadSession              *bool       // AgentCapabilities.LoadSession from ACP Initialize (nil = not yet set)
	ListSessions             *bool       // SessionCapabilities.List != nil from ACP Initialize (nil = not yet set)
	UpdatedAt                time.Time

	// refreshedInProcess marks whether this capability has already been
	// refreshed from the current agent process instance. Each agent process
	// should trigger exactly one full refresh (via ForceUpdate) when it first
	// establishes a session. Subsequent ResumeSession calls on the same
	// process instance skip the refresh. The marker is cleared when the
	// process dies (connection closed) so a new process triggers a fresh refresh.
	refreshedInProcess bool
}

// HasData returns true if the capability has any non-empty data.
func (c *AgentCapability) HasData() bool {
	return len(c.AvailableModes) > 0 ||
		len(c.AvailableThinkingEfforts) > 0 ||
		len(c.AvailableModels) > 0 ||
		len(c.AvailableCommands) > 0 ||
		c.ConfigOptionState != nil ||
		c.CachedUsageState != nil ||
		c.LoadSession != nil ||
		c.ListSessions != nil
}

// AgentCapabilityRegistry stores agent-level capabilities, keyed by agent ID.
// Thread-safe via sync.RWMutex. Capabilities are persisted to the agents
// table so they survive restarts without requiring prefetch.
type AgentCapabilityRegistry struct {
	mu   sync.RWMutex
	caps map[string]*AgentCapability // agentID -> capability
}

// Global singleton
var (
	globalCapabilityRegistry     *AgentCapabilityRegistry
	globalCapabilityRegistryOnce sync.Once
)

// GetAgentCapabilityRegistry returns the global AgentCapabilityRegistry singleton.
func GetAgentCapabilityRegistry() *AgentCapabilityRegistry {
	globalCapabilityRegistryOnce.Do(func() {
		globalCapabilityRegistry = &AgentCapabilityRegistry{
			caps: make(map[string]*AgentCapability),
		}
	})
	return globalCapabilityRegistry
}

// Get returns the capability for the given agent ID.
// Returns nil if no capability has been registered yet.
func (r *AgentCapabilityRegistry) Get(agentID string) *AgentCapability {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.caps[agentID]
}

// Update updates the capability for an agent and persists to DB.
// Only non-nil/non-empty fields are overwritten; others are preserved.
func (r *AgentCapabilityRegistry) Update(agentID string, agentCap *AgentCapability) {
	r.mu.Lock()
	existing, ok := r.caps[agentID]
	if !ok || existing == nil {
		r.caps[agentID] = agentCap
		r.mu.Unlock()
	} else {
		r.mu.Unlock()
		r.merge(agentID, agentCap)
	}
	r.persistAsync(agentID)
}

// merge overlays non-empty fields from src onto the existing capability.
func (r *AgentCapabilityRegistry) merge(agentID string, src *AgentCapability) {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.caps[agentID]
	if !ok || existing == nil {
		r.caps[agentID] = src
		return
	}

	if len(src.AvailableModes) > 0 {
		existing.AvailableModes = src.AvailableModes
	}
	if len(src.AvailableThinkingEfforts) > 0 {
		existing.AvailableThinkingEfforts = src.AvailableThinkingEfforts
	}
	if len(src.AvailableModels) > 0 {
		existing.AvailableModels = src.AvailableModels
	}
	if len(src.AvailableCommands) > 0 {
		existing.AvailableCommands = src.AvailableCommands
	}
	if src.ConfigOptionState != nil {
		existing.ConfigOptionState = src.ConfigOptionState
	}
	if src.CachedUsageState != nil {
		existing.CachedUsageState = src.CachedUsageState
	}
	if src.LoadSession != nil {
		existing.LoadSession = src.LoadSession
	}
	if src.ListSessions != nil {
		existing.ListSessions = src.ListSessions
	}
	existing.UpdatedAt = time.Now()
}

// UpdateModes updates only the available modes for an agent.
func (r *AgentCapabilityRegistry) UpdateModes(agentID string, modes []ModeDef) {
	r.Update(agentID, &AgentCapability{AvailableModes: modes})
}

// UpdateThinkingEfforts updates only the available thinking effort levels.
func (r *AgentCapabilityRegistry) UpdateThinkingEfforts(agentID string, levels []ThinkingEffortDef) {
	r.Update(agentID, &AgentCapability{AvailableThinkingEfforts: levels})
}

// UpdateModels updates only the available models.
func (r *AgentCapabilityRegistry) UpdateModels(agentID string, models []model.AgentModel) {
	r.Update(agentID, &AgentCapability{AvailableModels: models})
}

// UpdateCommands updates only the available commands.
func (r *AgentCapabilityRegistry) UpdateCommands(agentID string, cmds []AvailableCommandInfo) {
	r.Update(agentID, &AgentCapability{AvailableCommands: cmds})
}

// UpdateConfigState updates only the config option state.
func (r *AgentCapabilityRegistry) UpdateConfigState(agentID string, state *ConfigOptionState) {
	r.Update(agentID, &AgentCapability{ConfigOptionState: state})
}

// UpdateUsageState caches the usage state from a usage_update event.
// This is a best-effort agent-level cache so usage chips appear on
// session switch / reconnect without waiting for a new UsageUpdate.
func (r *AgentCapabilityRegistry) UpdateUsageState(agentID string, state *UsageState) {
	r.Update(agentID, &AgentCapability{CachedUsageState: state})
}

// UpdateLoadSession updates only the LoadSession capability flag.
func (r *AgentCapabilityRegistry) UpdateLoadSession(agentID string, val bool) {
	r.Update(agentID, &AgentCapability{LoadSession: &val})
}

// UpdateListSessions updates only the ListSessions capability flag.
func (r *AgentCapabilityRegistry) UpdateListSessions(agentID string, val bool) {
	r.Update(agentID, &AgentCapability{ListSessions: &val})
}

// ForceUpdate replaces all capability fields for an agent (full overwrite, not merge)
// and persists to DB asynchronously. Used when a new agent process establishes its
// first session — the ACP response is the authoritative source of truth.
// Returns true if the update was applied, false if skipped because the current
// process instance already refreshed this agent.
func (r *AgentCapabilityRegistry) ForceUpdate(agentID string, agentCap *AgentCapability) bool {
	r.mu.Lock()
	existing, ok := r.caps[agentID]
	if ok && existing != nil && existing.refreshedInProcess {
		r.mu.Unlock()
		slog.Debug("acp capability: skipping ForceUpdate, already refreshed in this process", "agent", agentID)
		return false
	}
	agentCap.UpdatedAt = time.Now()
	agentCap.refreshedInProcess = true
	r.caps[agentID] = agentCap
	r.mu.Unlock()
	r.persistAsync(agentID)
	slog.Info("acp capability: ForceUpdate applied", "agent", agentID,
		"modes", len(agentCap.AvailableModes),
		"efforts", len(agentCap.AvailableThinkingEfforts),
		"models", len(agentCap.AvailableModels),
		"commands", len(agentCap.AvailableCommands),
		"loadSession", boolPtrVal(agentCap.LoadSession),
		"listSessions", boolPtrVal(agentCap.ListSessions))
	return true
}

// MarkStale clears the refreshedInProcess flag for an agent, indicating that
// the next ForceUpdate should be applied (e.g., after the agent process dies
// and a new one is spawned).
func (r *AgentCapabilityRegistry) MarkStale(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.caps[agentID]
	if ok && existing != nil {
		existing.refreshedInProcess = false
	}
}

// ForceUpdateIfNeeded extracts agent-level capabilities from a NewSession/ResumeSession
// response and calls ForceUpdate. This is the single entry point for full capability
// refresh — called once when an ACP connection first establishes a session.
// The update is synchronous on the registry but DB persistence is async.
func (r *AgentCapabilityRegistry) ForceUpdateIfNeeded(agentID string, modes []ModeDef, efforts []ThinkingEffortDef, models []model.AgentModel, cmds []AvailableCommandInfo, configState *ConfigOptionState, loadSession, listSessions bool) bool {
	return r.ForceUpdate(agentID, &AgentCapability{
		AvailableModes:           modes,
		AvailableThinkingEfforts: efforts,
		AvailableModels:          models,
		AvailableCommands:        cmds,
		ConfigOptionState:        configState,
		LoadSession:              &loadSession,
		ListSessions:             &listSessions,
	})
}

// GetModeState returns a ModeState combining agent-level available modes
// with the session-level current mode ID.
func (r *AgentCapabilityRegistry) GetModeState(agentID, currentModeID string) *ModeState {
	r.mu.RLock()
	agentCap, ok := r.caps[agentID]
	r.mu.RUnlock()
	if !ok || agentCap == nil {
		return nil
	}
	if len(agentCap.AvailableModes) > 0 {
		return &ModeState{
			CurrentModeID:  currentModeID,
			AvailableModes: agentCap.AvailableModes,
		}
	}
	// ACP v2 agents (like OpenCode) report modes via ConfigOptionState
	// with Category "mode" instead of the legacy Modes field.
	if ms := modeStateFromConfigState(agentCap.ConfigOptionState); ms != nil {
		if currentModeID != "" {
			ms.CurrentModeID = currentModeID
		}
		return ms
	}
	return nil
}

// GetThinkingEffortState returns a ThinkingEffortState combining agent-level
// available levels with the session-level current ID.
func (r *AgentCapabilityRegistry) GetThinkingEffortState(agentID, currentID string) *ThinkingEffortState {
	r.mu.RLock()
	agentCap, ok := r.caps[agentID]
	r.mu.RUnlock()
	if !ok || agentCap == nil || len(agentCap.AvailableThinkingEfforts) == 0 {
		return nil
	}
	return &ThinkingEffortState{
		CurrentID:       currentID,
		AvailableLevels: agentCap.AvailableThinkingEfforts,
	}
}

// GetModelListState returns a ModelListState combining agent-level available
// models with the session-level current model ID.
func (r *AgentCapabilityRegistry) GetModelListState(agentID, currentModelID string) *ModelListState {
	r.mu.RLock()
	agentCap, ok := r.caps[agentID]
	r.mu.RUnlock()
	if !ok || agentCap == nil || len(agentCap.AvailableModels) == 0 {
		return nil
	}
	return &ModelListState{
		CurrentModelID: currentModelID,
		Models:         agentCap.AvailableModels,
	}
}

// GetCommands returns the available commands for an agent.
func (r *AgentCapabilityRegistry) GetCommands(agentID string) []AvailableCommandInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agentCap, ok := r.caps[agentID]
	if !ok || agentCap == nil {
		return nil
	}
	return agentCap.AvailableCommands
}

// GetConfigState returns the config option state for an agent.
func (r *AgentCapabilityRegistry) GetConfigState(agentID string) *ConfigOptionState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agentCap, ok := r.caps[agentID]
	if !ok || agentCap == nil {
		return nil
	}
	return agentCap.ConfigOptionState
}

// GetUsageState returns the cached usage state for an agent.
// This is a best-effort fallback from the last-known usage of any
// active connection for this agent.
func (r *AgentCapabilityRegistry) GetUsageState(agentID string) *UsageState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agentCap, ok := r.caps[agentID]
	if !ok || agentCap == nil {
		return nil
	}
	return agentCap.CachedUsageState
}

// GetLoadSession returns whether the agent supports LoadSession.
func (r *AgentCapabilityRegistry) GetLoadSession(agentID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agentCap, ok := r.caps[agentID]
	return ok && agentCap != nil && agentCap.LoadSession != nil && *agentCap.LoadSession
}

// GetListSessions returns whether the agent supports ListSessions.
func (r *AgentCapabilityRegistry) GetListSessions(agentID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agentCap, ok := r.caps[agentID]
	return ok && agentCap != nil && agentCap.ListSessions != nil && *agentCap.ListSessions
}

// HasAvailableModes checks whether an agent has available modes in the registry.
func (r *AgentCapabilityRegistry) HasAvailableModes(agentID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agentCap, ok := r.caps[agentID]
	return ok && agentCap != nil && len(agentCap.AvailableModes) > 0
}

// IsModeAvailable checks whether a specific mode ID exists in the agent's
// available modes. Used to validate agent-reported mode changes.
func (r *AgentCapabilityRegistry) IsModeAvailable(agentID, modeID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agentCap, ok := r.caps[agentID]
	if !ok || agentCap == nil {
		return false
	}
	for _, m := range agentCap.AvailableModes {
		if m.ID == modeID {
			return true
		}
	}
	return false
}

// HasNewAvailableModes returns true if the given modes list contains IDs
// not present in the registry's available modes for this agent.
func (r *AgentCapabilityRegistry) HasNewAvailableModes(agentID string, newModes []ModeDef) bool {
	r.mu.RLock()
	agentCap, ok := r.caps[agentID]
	r.mu.RUnlock()
	if !ok || agentCap == nil || len(agentCap.AvailableModes) == 0 {
		return len(newModes) > 0
	}
	seen := make(map[string]struct{}, len(agentCap.AvailableModes))
	for _, m := range agentCap.AvailableModes {
		seen[m.ID] = struct{}{}
	}
	for _, m := range newModes {
		if _, found := seen[m.ID]; !found {
			return true
		}
	}
	return false
}

// HasNewAvailableThinkingEfforts returns true if the given levels contain
// IDs not present in the registry.
func (r *AgentCapabilityRegistry) HasNewAvailableThinkingEfforts(agentID string, newLevels []ThinkingEffortDef) bool {
	r.mu.RLock()
	agentCap, ok := r.caps[agentID]
	r.mu.RUnlock()
	if !ok || agentCap == nil || len(agentCap.AvailableThinkingEfforts) == 0 {
		return len(newLevels) > 0
	}
	seen := make(map[string]struct{}, len(agentCap.AvailableThinkingEfforts))
	for _, l := range agentCap.AvailableThinkingEfforts {
		seen[l.ID] = struct{}{}
	}
	for _, l := range newLevels {
		if _, found := seen[l.ID]; !found {
			return true
		}
	}
	return false
}

// HasNewAvailableModels returns true if the given models contain IDs
// not present in the registry.
func (r *AgentCapabilityRegistry) HasNewAvailableModels(agentID string, newModels []model.AgentModel) bool {
	r.mu.RLock()
	agentCap, ok := r.caps[agentID]
	r.mu.RUnlock()
	if !ok || agentCap == nil || len(agentCap.AvailableModels) == 0 {
		return len(newModels) > 0
	}
	seen := make(map[string]struct{}, len(agentCap.AvailableModels))
	for _, m := range agentCap.AvailableModels {
		seen[m.ID] = struct{}{}
	}
	for _, m := range newModels {
		if _, found := seen[m.ID]; !found {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// DB Persistence
// ---------------------------------------------------------------------------

// dbHolder stores the DB reference for async persistence.
var dbHolder struct {
	mu sync.RWMutex
	db dbutil.Writer
}

// SetRegistryDB sets the database reference used for persistence.
// Called once during startup after InitDB.
func SetRegistryDB(db dbutil.Writer) {
	dbHolder.mu.Lock()
	defer dbHolder.mu.Unlock()
	dbHolder.db = db
}

func getRegistryDB() dbutil.Writer {
	dbHolder.mu.RLock()
	defer dbHolder.mu.RUnlock()
	return dbHolder.db
}

// persistAsync saves capabilities for a single agent to DB in a background goroutine.
func (r *AgentCapabilityRegistry) persistAsync(agentID string) {
	db := getRegistryDB()
	if db == nil {
		return
	}

	r.mu.RLock()
	agentCap, ok := r.caps[agentID]
	r.mu.RUnlock()
	if !ok || agentCap == nil {
		return
	}

	go func() {
		if err := r.saveToDB(db, agentID, agentCap); err != nil {
			slog.Warn("failed to persist agent capability to DB", "agent", agentID, "error", err)
		}
	}()
}

// saveToDB persists capabilities for a single agent to the agents table.
func (r *AgentCapabilityRegistry) saveToDB(db dbutil.Writer, agentID string, agentCap *AgentCapability) error {
	modesJSON, _ := json.Marshal(agentCap.AvailableModes)
	if string(modesJSON) == "null" {
		modesJSON = []byte("[]")
	}
	effortsJSON, _ := json.Marshal(agentCap.AvailableThinkingEfforts)
	if string(effortsJSON) == "null" {
		effortsJSON = []byte("[]")
	}
	cmdsJSON, _ := json.Marshal(agentCap.AvailableCommands)
	if string(cmdsJSON) == "null" {
		cmdsJSON = []byte("[]")
	}
	var configJSON string
	if agentCap.ConfigOptionState != nil {
		b, _ := json.Marshal(agentCap.ConfigOptionState)
		configJSON = string(b)
	}

	var loadSessionVal, listSessionsVal bool
	if agentCap.LoadSession != nil {
		loadSessionVal = *agentCap.LoadSession
	}
	if agentCap.ListSessions != nil {
		listSessionsVal = *agentCap.ListSessions
	}
	var usageJSON string
	if agentCap.CachedUsageState != nil {
		b, _ := json.Marshal(agentCap.CachedUsageState)
		usageJSON = string(b)
	}

	_, err := db.Exec(`
		UPDATE agents SET
			acp_available_modes = ?,
			acp_available_thinking_efforts = ?,
			acp_available_commands = ?,
			acp_config_options = ?,
			acp_load_session = ?,
			acp_list_sessions = ?,
			acp_cached_usage_state = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		string(modesJSON), string(effortsJSON), string(cmdsJSON), configJSON,
		loadSessionVal, listSessionsVal, usageJSON, agentID)
	return err
}

// LoadFromDB loads persisted capabilities from the agents table on startup.
//
//nolint:gocyclo,gocognit // LoadFromDB branches on each capability field; a switch adds boilerplate without clarity
func (r *AgentCapabilityRegistry) LoadFromDB(db dbutil.Reader) {
	rows, err := db.Query(`
		SELECT id, acp_available_modes, acp_available_thinking_efforts,
		       acp_available_commands, acp_config_options,
		       acp_load_session, acp_list_sessions, acp_cached_usage_state
		FROM agents
		WHERE transport = 'acp-stdio'
	`)
	if err != nil {
		slog.Warn("failed to load agent capabilities from DB", "error", err)
		return
	}
	defer func() { _ = rows.Close() }()
	if rowsErr := rows.Err(); rowsErr != nil {
		slog.Warn("agent capability rows error", "error", rowsErr)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for rows.Next() {
		var agentID, modesJSON, effortsJSON, cmdsJSON, configJSON, usageJSON string
		var loadSession, listSessions bool
		if err := rows.Scan(&agentID, &modesJSON, &effortsJSON, &cmdsJSON, &configJSON, &loadSession, &listSessions, &usageJSON); err != nil {
			slog.Warn("failed to scan agent capability row", "error", err)
			continue
		}

		agentCap := &AgentCapability{}

		if modesJSON != "" && modesJSON != "[]" {
			var modes []ModeDef
			if err := json.Unmarshal([]byte(modesJSON), &modes); err == nil {
				agentCap.AvailableModes = modes
			}
		}
		if effortsJSON != "" && effortsJSON != "[]" {
			var efforts []ThinkingEffortDef
			if err := json.Unmarshal([]byte(effortsJSON), &efforts); err == nil {
				agentCap.AvailableThinkingEfforts = efforts
			}
		}
		if cmdsJSON != "" && cmdsJSON != "[]" {
			var cmds []AvailableCommandInfo
			if err := json.Unmarshal([]byte(cmdsJSON), &cmds); err == nil {
				agentCap.AvailableCommands = cmds
			}
		}
		if configJSON != "" {
			var config ConfigOptionState
			if err := json.Unmarshal([]byte(configJSON), &config); err == nil {
				agentCap.ConfigOptionState = &config
			}
		}
		if usageJSON != "" {
			var usage UsageState
			if err := json.Unmarshal([]byte(usageJSON), &usage); err == nil && usage.Size > 0 {
				agentCap.CachedUsageState = &usage
			}
		}

		agentCap.LoadSession = &loadSession
		agentCap.ListSessions = &listSessions

		if agentCap.HasData() {
			agentCap.UpdatedAt = time.Now()
			r.caps[agentID] = agentCap
			slog.Info("loaded agent capability from DB",
				"agent", agentID,
				"modes", len(agentCap.AvailableModes),
				"efforts", len(agentCap.AvailableThinkingEfforts),
				"commands", len(agentCap.AvailableCommands))
		}
	}
}
