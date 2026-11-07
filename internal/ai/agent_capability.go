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
	AvailableModes []ModeDef
	// AvailableThinkingEfforts is the last level list any model reported. It is
	// NO LONGER the display source and is not persisted (issue #637): levels
	// are per model (see ThinkingEffortsByModel), and showing this list is what
	// made two sessions on different models disagree. What remains is the
	// baseline for "did the list change?" (HasNewAvailableThinkingEfforts) and
	// the fallback used when validating an agent-level setting that has no
	// model yet.
	AvailableThinkingEfforts []ThinkingEffortDef
	// ThinkingEffortsByModel is the level list each MODEL advertised, keyed by
	// model id (issue #637). Some agents (OpenCode) report a different set per
	// model — glm-5.3: low/high/max/default, deepseek-v4-pro: high/max/default
	// — so the single AvailableThinkingEfforts list above cannot answer for a
	// session: it only remembers whichever model reported last, and every other
	// session would show that model's levels. Keyed by model, the same model
	// looks identical in every session and every entry point.
	//
	// An absent entry means this model's list has never been learned. There is
	// deliberately no fallback to AvailableThinkingEfforts (which would show
	// another model's levels); callers that must show something use the
	// backend's static list. The "" key holds the list of agents that advertise
	// levels without any model option (OpenClaw).
	ThinkingEffortsByModel map[string][]ThinkingEffortDef `json:"-"`
	AvailableModels        []model.AgentModel
	AvailableCommands      []AvailableCommandInfo
	ConfigOptionState      *ConfigOptionState
	LoadSession            *bool // AgentCapabilities.LoadSession from ACP Initialize (nil = not yet set)
	ListSessions           *bool // SessionCapabilities.List != nil from ACP Initialize (nil = not yet set)
	DeleteSession          *bool // SessionCapabilities.Delete != nil from ACP Initialize (nil = not yet set)
	// McpHTTP / McpSSE are AgentCapabilities.McpCapabilities from ACP
	// Initialize. stdio is mandatory in the spec and never advertised, so it
	// has no flag. Sending http/sse to an agent that did not advertise them is
	// a HARD failure (codex-acp throws invalidRequest and session/new dies),
	// so unknown means "do not send".
	McpHTTP   *bool `json:"-"`
	McpSSE    *bool `json:"-"`
	UpdatedAt time.Time

	// AvailableOptions stores selectable options by category for categories
	// beyond the well-known ones (mode, thought_level, model). For well-known
	// categories, the legacy fields (AvailableModes, AvailableThinkingEfforts,
	// AvailableModels) remain the canonical source of truth.
	AvailableOptions map[string][]SelectOptionDef

	// refreshedInProcess marks whether this capability has already been
	// refreshed from the current agent process instance. Each agent process
	// should trigger exactly one full refresh (via ForceUpdate) when it first
	// establishes a session. Subsequent ResumeSession calls on the same
	// process instance skip the refresh. The marker is cleared when the
	// process dies (connection closed) so a new process triggers a fresh refresh.
	refreshedInProcess bool
}

// clone returns a deep copy that can be read WITHOUT holding the registry
// lock. persistAsync marshals the snapshot on a goroutine while other
// goroutines keep updating the live entry — sharing the pointer made the
// per-model map a fatal "concurrent map read and map write" (issue #637).
func (c *AgentCapability) clone() *AgentCapability {
	if c == nil {
		return nil
	}
	out := *c
	out.AvailableModes = append([]ModeDef(nil), c.AvailableModes...)
	out.AvailableThinkingEfforts = append([]ThinkingEffortDef(nil), c.AvailableThinkingEfforts...)
	if c.ThinkingEffortsByModel != nil {
		out.ThinkingEffortsByModel = make(map[string][]ThinkingEffortDef, len(c.ThinkingEffortsByModel))
		for modelID, levels := range c.ThinkingEffortsByModel {
			out.ThinkingEffortsByModel[modelID] = append([]ThinkingEffortDef(nil), levels...)
		}
	}
	out.AvailableModels = append([]model.AgentModel(nil), c.AvailableModels...)
	out.AvailableCommands = append([]AvailableCommandInfo(nil), c.AvailableCommands...)
	if c.ConfigOptionState != nil {
		cfg := *c.ConfigOptionState
		out.ConfigOptionState = &cfg
	}
	if c.AvailableOptions != nil {
		out.AvailableOptions = make(map[string][]SelectOptionDef, len(c.AvailableOptions))
		for k, v := range c.AvailableOptions {
			out.AvailableOptions[k] = append([]SelectOptionDef(nil), v...)
		}
	}
	return &out
}

// HasData returns true if the capability has any non-empty data.
func (c *AgentCapability) HasData() bool {
	return len(c.AvailableModes) > 0 ||
		len(c.AvailableThinkingEfforts) > 0 ||
		len(c.ThinkingEffortsByModel) > 0 ||
		len(c.AvailableModels) > 0 ||
		len(c.AvailableCommands) > 0 ||
		c.ConfigOptionState != nil ||
		c.LoadSession != nil ||
		c.ListSessions != nil ||
		c.DeleteSession != nil ||
		c.McpHTTP != nil ||
		c.McpSSE != nil ||
		len(c.AvailableOptions) > 0
}

// AgentCapabilityRegistry stores agent-level capabilities, keyed by agent ID.
// Thread-safe via sync.RWMutex. Capabilities are persisted to the agents
// table so they survive restarts without requiring prefetch.
type AgentCapabilityRegistry struct {
	mu   sync.RWMutex
	caps map[string]*AgentCapability // agentID -> capability

	// persistMu serializes the background DB writes. Each update spawns a
	// goroutine, and without this an older snapshot could land last and drop a
	// newer entry — the per-model level map accumulates knowledge, so a lost
	// write is a lost model.
	persistMu sync.Mutex
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
	if len(src.ThinkingEffortsByModel) > 0 {
		if existing.ThinkingEffortsByModel == nil {
			existing.ThinkingEffortsByModel = make(map[string][]ThinkingEffortDef, len(src.ThinkingEffortsByModel))
		}
		for modelID, levels := range src.ThinkingEffortsByModel {
			existing.ThinkingEffortsByModel[modelID] = levels
		}
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
	overlayBool(&existing.LoadSession, src.LoadSession)
	overlayBool(&existing.ListSessions, src.ListSessions)
	overlayBool(&existing.DeleteSession, src.DeleteSession)
	overlayBool(&existing.McpHTTP, src.McpHTTP)
	overlayBool(&existing.McpSSE, src.McpSSE)
	if len(src.AvailableOptions) > 0 {
		if existing.AvailableOptions == nil {
			existing.AvailableOptions = make(map[string][]SelectOptionDef, len(src.AvailableOptions))
		}
		for k, v := range src.AvailableOptions {
			existing.AvailableOptions[k] = v
		}
	}
	existing.UpdatedAt = time.Now()
}

// overlayBool copies src over *dst when src was actually reported.
//
// The tri-state is the point: nil means "the agent did not tell us", which must
// not erase a previously known value, while an explicit false is a real answer.
func overlayBool(dst **bool, src *bool) {
	if src != nil {
		*dst = src
	}
}

// UpdateModes updates only the available modes for an agent.
func (r *AgentCapabilityRegistry) UpdateModes(agentID string, modes []ModeDef) {
	r.Update(agentID, &AgentCapability{AvailableModes: modes})
}

// UpdateThinkingEfforts updates only the available thinking effort levels.
func (r *AgentCapabilityRegistry) UpdateThinkingEfforts(agentID string, levels []ThinkingEffortDef) {
	r.Update(agentID, &AgentCapability{AvailableThinkingEfforts: levels})
}

// UpdateThinkingEffortsForModel records the level list ONE model advertised
// (issue #637). Empty lists are ignored (they carry no information).
//
// An EMPTY model id is a legitimate key: some ACP agents advertise
// `thought_level` but no model option at all (OpenClaw), so their levels are
// agent-wide and are stored under "". Readers that have no model for the
// session use the same empty key.
func (r *AgentCapabilityRegistry) UpdateThinkingEffortsForModel(agentID, modelID string, levels []ThinkingEffortDef) {
	if len(levels) == 0 {
		return
	}
	copied := make([]ThinkingEffortDef, len(levels))
	copy(copied, levels)
	r.Update(agentID, &AgentCapability{ThinkingEffortsByModel: map[string][]ThinkingEffortDef{modelID: copied}})
}

// ThinkingEffortsForModel returns the level list recorded for this model, if
// one was ever reported.
func (r *AgentCapabilityRegistry) ThinkingEffortsForModel(agentID, modelID string) ([]ThinkingEffortDef, bool) {
	r.mu.RLock()
	agentCap, ok := r.caps[agentID]
	var levels []ThinkingEffortDef
	if ok && agentCap != nil {
		levels = agentCap.ThinkingEffortsByModel[modelID]
	}
	r.mu.RUnlock()
	if len(levels) == 0 {
		return nil, false
	}
	out := make([]ThinkingEffortDef, len(levels))
	copy(out, levels)
	return out, true
}

// GetThinkingEffortStateForModel returns the thinking-level state for ONE
// model, or nil when that model's list has never been reported.
//
// There is deliberately NO fallback to the agent-level list (issue #637): that
// list only remembers whichever model reported last, so falling back would show
// a different model's levels — exactly the bug this scoping exists to remove.
// Callers that must show something anyway (the settings picker) use the
// backend's static level list instead.
func (r *AgentCapabilityRegistry) GetThinkingEffortStateForModel(agentID, modelID, currentID string) *ThinkingEffortState {
	levels, ok := r.ThinkingEffortsForModel(agentID, modelID)
	if !ok {
		return nil
	}
	state := &ThinkingEffortState{CurrentID: currentID, AvailableLevels: levels}
	if base := r.GetThinkingEffortState(agentID, currentID); base != nil {
		state.ConfigID = base.ConfigID
	}
	return state
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

// UpdateLoadSession updates only the LoadSession capability flag.
func (r *AgentCapabilityRegistry) UpdateLoadSession(agentID string, val bool) {
	r.Update(agentID, &AgentCapability{LoadSession: &val})
}

// UpdateListSessions updates only the ListSessions capability flag.
func (r *AgentCapabilityRegistry) UpdateListSessions(agentID string, val bool) {
	r.Update(agentID, &AgentCapability{ListSessions: &val})
}

// UpdateDeleteSession updates only the DeleteSession capability flag.
func (r *AgentCapabilityRegistry) UpdateDeleteSession(agentID string, val bool) {
	r.Update(agentID, &AgentCapability{DeleteSession: &val})
}

// UpdateMCPCapabilities updates only the advertised MCP transports.
//
// Called from Initialize. Unlike listSessions this is NOT carried through
// ForceUpdateIfNeeded from the registry on every first-session — the Initialize
// response is the authoritative source and it runs before that path, so the
// merge (not full replace) is what preserves it.
func (r *AgentCapabilityRegistry) UpdateMCPCapabilities(agentID string, http, sse bool) {
	r.Update(agentID, &AgentCapability{McpHTTP: &http, McpSSE: &sse})
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
	// The per-model level lists are ACCUMULATED knowledge (one entry per model
	// the agent has run), not part of the "current agent process" snapshot this
	// call replaces. Carrying them through keeps every other model's list after
	// the first session of a process — otherwise a restart would drop them all
	// and every session would fall back to the agent-level list again.
	if ok && existing != nil && len(existing.ThinkingEffortsByModel) > 0 {
		merged := make(map[string][]ThinkingEffortDef, len(existing.ThinkingEffortsByModel))
		for modelID, levels := range existing.ThinkingEffortsByModel {
			merged[modelID] = levels
		}
		for modelID, levels := range agentCap.ThinkingEffortsByModel {
			merged[modelID] = levels
		}
		agentCap.ThinkingEffortsByModel = merged
	}
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
func (r *AgentCapabilityRegistry) ForceUpdateIfNeeded(agentID string, modes []ModeDef, efforts []ThinkingEffortDef, models []model.AgentModel, cmds []AvailableCommandInfo, configState *ConfigOptionState, loadSession, listSessions, mcpHTTP, mcpSSE bool) bool {
	return r.ForceUpdate(agentID, &AgentCapability{
		AvailableModes:           modes,
		AvailableThinkingEfforts: efforts,
		AvailableModels:          models,
		AvailableCommands:        cmds,
		ConfigOptionState:        configState,
		LoadSession:              &loadSession,
		ListSessions:             &listSessions,
		McpHTTP:                  &mcpHTTP,
		McpSSE:                   &mcpSSE,
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
	if !ok || agentCap == nil {
		return nil
	}
	if len(agentCap.AvailableThinkingEfforts) > 0 {
		return &ThinkingEffortState{
			CurrentID:       currentID,
			AvailableLevels: agentCap.AvailableThinkingEfforts,
		}
	}
	// Fallback: ACP v2 agents may expose thinking effort via ConfigOptionState
	// with Category "thought_level" instead of the legacy ThinkingEfforts field.
	// Priority: session-level currentID (from user selection / PreApply) overrides
	// the ConfigOptionState's CurrentID (agent default). If currentID is empty,
	// the agent's reported default is used as-is.
	if es := thinkingEffortStateFromConfigState(agentCap.ConfigOptionState); es != nil {
		if currentID != "" {
			es.CurrentID = currentID
		}
		return es
	}
	return nil
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

// HasConfirmedACPModels reports whether the ACP runtime has ever reported a
// model list for this agent — i.e. whether the model list is CONFIRMED by the
// agent rather than guessed by the separate CLI discovery pipeline.
//
// ACP has no "automatic model selection": the agent always reports a
// currentValue in its `category: "model"` config option, and the client either
// leaves it alone or sends session/set_config_option. So before the agent has
// spoken, any model list ClawBench shows is a CLI-side guess, and sending one
// as a set_config_option can select a model the agent cannot run.
//
// The data comes from the AgentCapabilityRegistry, written on the session/new
// handshake and persisted to agents.acp_available_models, so it stays true
// across restarts.
func HasConfirmedACPModels(agentID string) bool {
	return GetAgentCapabilityRegistry().GetModelListState(agentID, "") != nil
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

// GetMCPCapabilities returns the agent's advertised MCP transports.
//
// stdio is not represented: the ACP spec makes it mandatory, so it needs no
// flag. A false value for http/sse means "not advertised", which must be read
// as "do not send" — those transports are optional and sending one the agent
// does not support fails the whole session/new.
func (r *AgentCapabilityRegistry) GetMCPCapabilities(agentID string) model.MCPCapabilities {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agentCap, ok := r.caps[agentID]
	if !ok || agentCap == nil {
		return model.MCPCapabilities{}
	}
	return model.MCPCapabilities{
		HTTP: agentCap.McpHTTP != nil && *agentCap.McpHTTP,
		SSE:  agentCap.McpSSE != nil && *agentCap.McpSSE,
	}
}

// GetDeleteSession returns whether the agent supports DeleteSession.
func (r *AgentCapabilityRegistry) GetDeleteSession(agentID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agentCap, ok := r.caps[agentID]
	return ok && agentCap != nil && agentCap.DeleteSession != nil && *agentCap.DeleteSession
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
// IsModelAvailable reports whether the agent advertised modelID in its own
// model list.
//
// Used to decide whether a stored model selection may be pushed over ACP. An
// agent's list is authoritative for what IT can run, but it is not the full set
// of models a session may legitimately be pinned to: Command Code with a BYOK
// provider pins the model in ~/.commandcode/config.json, and its ACP surface
// always advertises only the vendor's own catalog (availableModels(){return
// catalogModels()}), so the BYOK model never appears. Pushing a catalog model
// over that binding silently reroutes the request to the vendor gateway and
// fails with 401 — so an unlisted selection must be left alone, not "corrected".
//
// A registry with no entry for the agent (capabilities not yet extracted)
// returns false: callers must treat that as "unknown", not "unavailable".
func (r *AgentCapabilityRegistry) IsModelAvailable(agentID, modelID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agentCap, ok := r.caps[agentID]
	if !ok || agentCap == nil {
		return false
	}
	for _, m := range agentCap.AvailableModels {
		if m.ID == modelID {
			return true
		}
	}
	return false
}

// HasModelList reports whether the agent's model list has been observed.
//
// Distinct from IsModelAvailable's false: "the agent has not reported its list
// yet" must not be treated as "this model is unavailable", or the first turn of
// every session would skip its model selection.
func (r *AgentCapabilityRegistry) HasModelList(agentID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agentCap, ok := r.caps[agentID]
	return ok && agentCap != nil && len(agentCap.AvailableModels) > 0
}

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

// HasNewAvailableThinkingEfforts reports whether the agent's available effort
// levels differ from the registry's — in EITHER direction.
//
// A shrinking list matters as much as a growing one (issue #552): OpenCode's
// levels are per model, and a narrower model's list can be a strict SUBSET of
// the previous one (glm-5.3 offers low/high/max/default, deepseek-v4-pro only
// high/max/default). Comparing "any new id?" alone would keep advertising the
// wider list, so a level the current model cannot run would look valid and be
// pushed on every turn, only to be rejected with -32602.
func (r *AgentCapabilityRegistry) HasNewAvailableThinkingEfforts(agentID string, newLevels []ThinkingEffortDef) bool {
	r.mu.RLock()
	agentCap, ok := r.caps[agentID]
	r.mu.RUnlock()
	if !ok || agentCap == nil || len(agentCap.AvailableThinkingEfforts) == 0 {
		return len(newLevels) > 0
	}
	if len(agentCap.AvailableThinkingEfforts) != len(newLevels) {
		return true
	}
	existing := make(map[string]struct{}, len(agentCap.AvailableThinkingEfforts))
	for _, l := range agentCap.AvailableThinkingEfforts {
		existing[l.ID] = struct{}{}
	}
	for _, l := range newLevels {
		if _, found := existing[l.ID]; !found {
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
// Generalized SelectState methods
// ---------------------------------------------------------------------------

// GetSelectState returns a SelectState for the given category, combining
// agent-level available options with the session-level current ID.
// Returns nil if no options are available for the category.
func (r *AgentCapabilityRegistry) GetSelectState(agentID, category, currentID string) *SelectState {
	r.mu.RLock()
	agentCap, ok := r.caps[agentID]
	r.mu.RUnlock()
	if !ok || agentCap == nil {
		return nil
	}

	switch category {
	case "mode":
		ms := r.GetModeState(agentID, currentID)
		if ms == nil {
			return nil
		}
		sel := NewSelectStateFromMode(ms)
		return &sel
	case "thought_level":
		tes := r.GetThinkingEffortState(agentID, currentID)
		if tes == nil {
			return nil
		}
		sel := NewSelectStateFromThinkingEffort(tes)
		return &sel
	case "model":
		mls := r.GetModelListState(agentID, currentID)
		if mls == nil {
			return nil
		}
		sel := SelectState{
			CurrentID: mls.CurrentModelID,
			Category:  "model",
		}
		for _, m := range mls.Models {
			sel.Available = append(sel.Available, SelectOptionDef{ID: m.ID, Name: m.Name})
		}
		return &sel
	default:
		// Custom categories use AvailableOptions map
		opts, exists := agentCap.AvailableOptions[category]
		if !exists || len(opts) == 0 {
			return nil
		}
		sel := SelectState{
			CurrentID: currentID,
			Available: opts,
			Category:  category,
		}
		return &sel
	}
}

// GetAvailableOptions returns the available options for the given category.
// For well-known categories, it converts the legacy fields to SelectOptionDef.
// Returns nil if no options are available.
func (r *AgentCapabilityRegistry) GetAvailableOptions(agentID, category string) []SelectOptionDef {
	r.mu.RLock()
	agentCap, ok := r.caps[agentID]
	r.mu.RUnlock()
	if !ok || agentCap == nil {
		return nil
	}

	switch category {
	case "mode":
		if len(agentCap.AvailableModes) == 0 {
			return nil
		}
		opts := make([]SelectOptionDef, len(agentCap.AvailableModes))
		for i, m := range agentCap.AvailableModes {
			opts[i] = SelectOptionDef(m)
		}
		return opts
	case "thought_level":
		if len(agentCap.AvailableThinkingEfforts) == 0 {
			return nil
		}
		opts := make([]SelectOptionDef, len(agentCap.AvailableThinkingEfforts))
		for i, l := range agentCap.AvailableThinkingEfforts {
			opts[i] = SelectOptionDef(l)
		}
		return opts
	case "model":
		if len(agentCap.AvailableModels) == 0 {
			return nil
		}
		opts := make([]SelectOptionDef, len(agentCap.AvailableModels))
		for i, m := range agentCap.AvailableModels {
			opts[i] = SelectOptionDef{ID: m.ID, Name: m.Name}
		}
		return opts
	default:
		return agentCap.AvailableOptions[category]
	}
}

// UpdateAvailableOptions updates the available options for the given category.
// For well-known categories, this delegates to the existing legacy methods.
// For other categories, it uses the AvailableOptions map.
func (r *AgentCapabilityRegistry) UpdateAvailableOptions(agentID, category string, opts []SelectOptionDef) {
	switch category {
	case "mode":
		modes := make([]ModeDef, len(opts))
		for i, o := range opts {
			modes[i] = ModeDef(o)
		}
		r.UpdateModes(agentID, modes)
	case "thought_level":
		levels := make([]ThinkingEffortDef, len(opts))
		for i, o := range opts {
			levels[i] = ThinkingEffortDef(o)
		}
		r.UpdateThinkingEfforts(agentID, levels)
	case "model":
		models := make([]model.AgentModel, len(opts))
		for i, o := range opts {
			models[i] = model.AgentModel{ID: o.ID, Name: o.Name}
		}
		r.UpdateModels(agentID, models)
	default:
		r.Update(agentID, &AgentCapability{
			AvailableOptions: map[string][]SelectOptionDef{category: opts},
		})
	}
}

// HasNewAvailableOptions returns true if the given options contain IDs
// not present in the registry for the given category.
func (r *AgentCapabilityRegistry) HasNewAvailableOptions(agentID, category string, newOpts []SelectOptionDef) bool {
	switch category {
	case "mode":
		modes := make([]ModeDef, len(newOpts))
		for i, o := range newOpts {
			modes[i] = ModeDef(o)
		}
		return r.HasNewAvailableModes(agentID, modes)
	case "thought_level":
		levels := make([]ThinkingEffortDef, len(newOpts))
		for i, o := range newOpts {
			levels[i] = ThinkingEffortDef(o)
		}
		return r.HasNewAvailableThinkingEfforts(agentID, levels)
	case "model":
		models := make([]model.AgentModel, len(newOpts))
		for i, o := range newOpts {
			models[i] = model.AgentModel{ID: o.ID, Name: o.Name}
		}
		return r.HasNewAvailableModels(agentID, models)
	default:
		// Custom categories
		r.mu.RLock()
		agentCap, ok := r.caps[agentID]
		r.mu.RUnlock()
		if !ok || agentCap == nil || len(agentCap.AvailableOptions[category]) == 0 {
			return len(newOpts) > 0
		}
		existing := agentCap.AvailableOptions[category]
		seen := make(map[string]struct{}, len(existing))
		for _, o := range existing {
			seen[o.ID] = struct{}{}
		}
		for _, o := range newOpts {
			if _, found := seen[o.ID]; !found {
				return true
			}
		}
		return false
	}
}

// agentCapForRead returns the agent capability by ID under a read lock.
// Returns nil if not found.
func (r *AgentCapabilityRegistry) agentCapForRead(agentID string) *AgentCapability {
	r.mu.RLock()
	agentCap, ok := r.caps[agentID]
	r.mu.RUnlock()
	if !ok || agentCap == nil {
		return nil
	}
	return agentCap
}

// IsOptionAvailable checks whether a specific option ID exists in the
// available options for the given category.
func (r *AgentCapabilityRegistry) IsOptionAvailable(agentID, category, optionID string) bool {
	switch category {
	case "mode":
		return r.IsModeAvailable(agentID, optionID)
	case "thought_level":
		agentCap := r.agentCapForRead(agentID)
		if agentCap == nil {
			return false
		}
		for _, l := range agentCap.AvailableThinkingEfforts {
			if l.ID == optionID {
				return true
			}
		}
		return false
	case "model":
		agentCap := r.agentCapForRead(agentID)
		if agentCap == nil {
			return false
		}
		for _, m := range agentCap.AvailableModels {
			if m.ID == optionID {
				return true
			}
		}
		return false
	default:
		agentCap := r.agentCapForRead(agentID)
		if agentCap == nil {
			return false
		}
		for _, o := range agentCap.AvailableOptions[category] {
			if o.ID == optionID {
				return true
			}
		}
		return false
	}
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

	go func() {
		// Serialize writes and take the snapshot AT WRITE TIME: two rapid
		// updates spawn two goroutines, and an older snapshot landing last
		// would silently drop the newer entry. Cloning under r.mu also keeps
		// the marshal away from the live entry (a shared map here was a fatal
		// "concurrent map read and map write", issue #637).
		r.persistMu.Lock()
		defer r.persistMu.Unlock()

		r.mu.RLock()
		agentCap := r.caps[agentID]
		var snapshot *AgentCapability
		if agentCap != nil {
			snapshot = agentCap.clone()
		}
		r.mu.RUnlock()
		if snapshot == nil {
			return
		}

		if err := r.saveToDB(db, agentID, snapshot); err != nil {
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
	// Per-model level lists (issue #637). Persisted separately from the
	// agent-level list above so a restart keeps each model's own levels instead
	// of collapsing back to whichever model was seen last.
	effortsByModelJSON, _ := json.Marshal(agentCap.ThinkingEffortsByModel)
	if string(effortsByModelJSON) == "null" {
		effortsByModelJSON = []byte("{}")
	}
	cmdsJSON, _ := json.Marshal(agentCap.AvailableCommands)
	if string(cmdsJSON) == "null" {
		cmdsJSON = []byte("[]")
	}
	// The ACP-reported model list is persisted so it survives a restart. Without
	// it the list lives only in memory and an agent's selectable models change
	// across a restart until a new ACP session repopulates the registry.
	modelsJSON, _ := json.Marshal(agentCap.AvailableModels)
	if string(modelsJSON) == "null" {
		modelsJSON = []byte("[]")
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
	var mcpHTTPVal, mcpSSEVal bool
	if agentCap.McpHTTP != nil {
		mcpHTTPVal = *agentCap.McpHTTP
	}
	if agentCap.McpSSE != nil {
		mcpSSEVal = *agentCap.McpSSE
	}

	_, err := db.Exec(`
		UPDATE agents SET
			acp_available_modes = ?,
			acp_thinking_efforts_by_model = ?,
			acp_available_commands = ?,
			acp_available_models = ?,
			acp_config_options = ?,
			acp_load_session = ?,
			acp_list_sessions = ?,
			acp_mcp_http = ?,
			acp_mcp_sse = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		string(modesJSON), string(effortsByModelJSON), string(cmdsJSON), string(modelsJSON), configJSON,
		loadSessionVal, listSessionsVal, mcpHTTPVal, mcpSSEVal, agentID)
	return err
}

// LoadFromDB loads persisted capabilities from the agents table on startup.
func (r *AgentCapabilityRegistry) LoadFromDB(db dbutil.Reader) {
	rows, err := db.Query(`
		SELECT id, acp_available_modes,
		       acp_thinking_efforts_by_model,
		       acp_available_commands, acp_available_models, acp_config_options,
		       acp_load_session, acp_list_sessions, acp_mcp_http, acp_mcp_sse
		FROM agents
		WHERE transport = 'acp-stdio'
	`)
	if err != nil {
		slog.Warn("failed to load agent capabilities from DB", "error", err)
		return
	}
	defer func() { _ = rows.Close() }()

	r.mu.Lock()
	defer r.mu.Unlock()

	for rows.Next() {
		var row capabilityRow
		if err := rows.Scan(&row.agentID, &row.modes, &row.effortsByModel, &row.commands,
			&row.models, &row.config, &row.loadSession, &row.listSessions,
			&row.mcpHTTP, &row.mcpSSE); err != nil {
			slog.Warn("failed to scan agent capability row", "error", err)
			continue
		}
		capability := row.toCapability()
		if !capability.HasData() {
			continue
		}
		r.caps[row.agentID] = capability
		slog.Info("loaded agent capability from DB",
			"agent", row.agentID,
			"modes", len(capability.AvailableModes),
			"efforts", len(capability.AvailableThinkingEfforts),
			"models", len(capability.AvailableModels),
			"commands", len(capability.AvailableCommands))
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		slog.Warn("agent capability rows error", "error", rowsErr)
	}
}

// capabilityRow is one raw agents row, with the JSON columns still as strings.
type capabilityRow struct {
	agentID        string
	modes          string
	effortsByModel string
	commands       string
	models         string
	config         string
	loadSession    bool
	listSessions   bool
	mcpHTTP        bool
	mcpSSE         bool
}

// toCapability decodes the JSON columns. An unparseable column is skipped rather
// than failing the row: the other capability fields are still worth loading.
func (row *capabilityRow) toCapability() *AgentCapability {
	capability := &AgentCapability{
		AvailableModes:         decodeJSONSlice[ModeDef](row.modes),
		ThinkingEffortsByModel: decodeEffortsByModel(row.effortsByModel),
		AvailableCommands:      decodeJSONSlice[AvailableCommandInfo](row.commands),
		AvailableModels:        decodeJSONSlice[model.AgentModel](row.models),
		LoadSession:            &row.loadSession,
		ListSessions:           &row.listSessions,
		McpHTTP:                &row.mcpHTTP,
		McpSSE:                 &row.mcpSSE,
	}
	if row.config != "" {
		var config ConfigOptionState
		if err := json.Unmarshal([]byte(row.config), &config); err == nil {
			// Rows written before the current value became session-scoped may
			// carry one here. It belongs to whichever session happened to be
			// active at the time, so it must not come back as this agent's
			// current value (see ACPConn.SessionConfigState).
			config.CurrentID = ""
			capability.ConfigOptionState = &config
		}
	}
	capability.UpdatedAt = time.Now()
	return capability
}

// decodeJSONSlice unmarshals a JSON array column, returning nil for an empty or
// unparseable value.
func decodeJSONSlice[T any](raw string) []T {
	if raw == "" || raw == "[]" {
		return nil
	}
	var out []T
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// decodeEffortsByModel decodes the per-model level map. An unparseable value
// yields nil (readers then fall back to the agent-level list) rather than
// failing the whole capability row.
func decodeEffortsByModel(raw string) map[string][]ThinkingEffortDef {
	if raw == "" || raw == "{}" || raw == "null" {
		return nil
	}
	var out map[string][]ThinkingEffortDef
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}
