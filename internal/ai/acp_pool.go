package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
)

// idleTimeout is how long an ACP connection can be idle before being killed.
const idleTimeout = 5 * time.Minute

// idleCheckInterval is how often the sweeper checks for idle connections.
const idleCheckInterval = 60 * time.Second

// ---------------------------------------------------------------------------
// ACPConnectionPool — singleton managing long-lived ACP connections
// ---------------------------------------------------------------------------

// ACPConnectionPool manages a pool of long-lived ACP stdio connections, one per agent.
// Connections are reused across multiple ExecuteStream calls and multiple sessions.
// Idle connections are automatically killed after idleTimeout.
type ACPConnectionPool struct {
	mu      sync.Mutex
	entries map[string]*ACPConnEntry // keyed by agentID
	done    chan struct{}            // closed on StopAll
}

var (
	globalPool     *ACPConnectionPool
	globalPoolOnce sync.Once
)

// GetACPConnectionPool returns the singleton connection pool.
func GetACPConnectionPool() *ACPConnectionPool {
	globalPoolOnce.Do(func() {
		globalPool = &ACPConnectionPool{
			entries: make(map[string]*ACPConnEntry),
			done:    make(chan struct{}),
		}
		go globalPool.idleSweeper()
	})
	return globalPool
}

// StopAll closes all connections in the pool. Called on server shutdown.
func (p *ACPConnectionPool) StopAll() {
	p.mu.Lock()
	for id, entry := range p.entries {
		entry.Close()
		delete(p.entries, id)
	}
	p.mu.Unlock()

	// Signal sweeper to stop
	select {
	case <-p.done:
	default:
		close(p.done)
	}
}

// GetOrCreate returns an ACPConnEntry for the given agent, creating one if needed.
// The entry is guaranteed to be alive (or an error is returned).
func (p *ACPConnectionPool) GetOrCreate(ctx context.Context, agent *model.Agent) (*ACPConnEntry, error) {
	p.mu.Lock()
	entry, ok := p.entries[agent.ID]
	if !ok {
		entry = newACPConnEntry(agent)
		p.entries[agent.ID] = entry
	}
	p.mu.Unlock()

	if err := entry.EnsureAlive(ctx); err != nil {
		return nil, err
	}
	return entry, nil
}

// CloseConnection closes and removes the connection for the given agent ID.
func (p *ACPConnectionPool) CloseConnection(agentID string) {
	p.mu.Lock()
	entry, ok := p.entries[agentID]
	if ok {
		delete(p.entries, agentID)
	}
	p.mu.Unlock()

	if ok {
		entry.Close()
	}
}

// GetClient returns the ClawBenchACPClient for the given agent ID.
// Returns nil if the agent has no active ACP connection.
func (p *ACPConnectionPool) GetClient(agentID string) *ClawBenchACPClient {
	p.mu.Lock()
	entry, ok := p.entries[agentID]
	p.mu.Unlock()

	if !ok {
		return nil
	}
	return entry.GetClient()
}

// GetCachedStateByAgentID returns the cached mode, config, thinking effort,
// and model list state for the given agent ID. Returns nil for each state that is not available.
// Used for pre-fetching mode state before the first message (no ClawBench session yet).
func (p *ACPConnectionPool) GetCachedStateByAgentID(agentID string) (mode *ModeState, config *ConfigOptionState, effort *ThinkingEffortState, modelList *ModelListState) {
	p.mu.Lock()
	entry, ok := p.entries[agentID]
	p.mu.Unlock()

	if !ok {
		return nil, nil, nil, nil
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	return entry.cachedModeState, entry.cachedConfigState, entry.cachedThinkingEffortState, entry.cachedModelListState
}

// GetClientByACPSession returns the ClawBenchACPClient for the connection
// that owns the given ACP session ID. It searches all entries' session maps.
// Returns nil if no matching session is found.
func (p *ACPConnectionPool) GetClientByACPSession(acpSessionID string) *ClawBenchACPClient {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, entry := range p.entries {
		entry.mu.Lock()
		for _, sid := range entry.sessions {
			if sid == acpSessionID {
				client := entry.client
				entry.mu.Unlock()
				return client
			}
		}
		entry.mu.Unlock()
	}
	return nil
}

// GetACPSessionID resolves a ClawBench session ID to the ACP session ID
// on the given agent's connection. Returns empty string if not found.
func (p *ACPConnectionPool) GetACPSessionID(agentID, clawbenchSID string) string {
	p.mu.Lock()
	entry, ok := p.entries[agentID]
	p.mu.Unlock()

	if !ok {
		return ""
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	return entry.sessions[clawbenchSID]
}

// GetCachedStateByClawbenchSID returns the cached mode, config, thinking effort,
// slash commands, and model list for the pool entry that owns the given ClawBench session ID.
// Returns nil/empty for each state that is not available.
// Used by the SSE handler and REST API to re-emit state on reconnect.
func (p *ACPConnectionPool) GetCachedStateByClawbenchSID(clawbenchSID string) (mode *ModeState, config *ConfigOptionState, effort *ThinkingEffortState, cmds []AvailableCommandInfo, modelList *ModelListState) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, entry := range p.entries {
		entry.mu.Lock()
		if _, ok := entry.sessions[clawbenchSID]; ok {
			mode = entry.cachedModeState
			config = entry.cachedConfigState
			effort = entry.cachedThinkingEffortState
			cmds = entry.client.GetCommandsAsInfo()
			modelList = entry.cachedModelListState
			entry.mu.Unlock()
			return
		}
		entry.mu.Unlock()
	}
	return nil, nil, nil, nil, nil
}

// GetCommandsByAgentID returns the cached slash commands for the given agent ID.
// Returns nil if no connection exists for the agent.
// Used for pre-fetching commands before the first message (no session yet).
func (p *ACPConnectionPool) GetCommandsByAgentID(agentID string) []AvailableCommandInfo {
	client := p.GetClient(agentID)
	if client == nil {
		return nil
	}
	return client.GetCommandsAsInfo()
}

// idleSweeper periodically kills idle connections.
func (p *ACPConnectionPool) idleSweeper() {
	ticker := time.NewTicker(idleCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			p.sweepIdle()
		}
	}
}

// sweepIdle kills connections that have been idle for too long.
func (p *ACPConnectionPool) sweepIdle() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	for id, entry := range p.entries {
		entry.mu.Lock()
		idle := now.Sub(entry.lastUsed)
		entry.mu.Unlock()

		if idle > idleTimeout {
			slog.Info("acp pool: killing idle connection", "agent_id", id, "idle", idle.Round(time.Second))
			entry.Close()
			delete(p.entries, id)
		}
	}
}

// ---------------------------------------------------------------------------
// ACPConnEntry — one long-lived ACP stdio connection to an agent
// ---------------------------------------------------------------------------

// ACPConnEntry represents a long-lived ACP stdio connection to one agent.
// It can serve multiple sessions and multiple prompt turns.
type ACPConnEntry struct {
	agent *model.Agent
	mu    sync.Mutex

	cmd    *exec.Cmd
	conn   *acp.ClientSideConnection
	client *ClawBenchACPClient // shared across sessions

	// session mapping: clawbench session ID → ACP session ID
	sessions map[string]string

	// lastSessionResp stores the NewSessionResponse from the most recent session/new
	// so ExecuteStream can extract mode/config state. Cleared after reading.
	lastSessionResp *acp.NewSessionResponse

	// cachedModeState and cachedConfigState are populated from NewSessionResponse
	// and re-emitted for every ExecuteStream call (not just new sessions).
	// This ensures the frontend always has up-to-date mode/command state,
	// even after page refreshes or SSE reconnections.
	cachedModeState           *ModeState
	cachedConfigState         *ConfigOptionState
	cachedThinkingEffortState *ThinkingEffortState
	cachedModelListState      *ModelListState

	// persistDebounce timer for batching ACP state DB writes
	persistTimer *time.Timer
	persistMu    sync.Mutex // separate mutex to avoid deadlock with e.mu

	// liveness
	lastUsed time.Time
	alive    bool
}

// persistAgentACPStateToDB is the global function for persisting ACP state to the database.
// Set by the application startup via SetACPStatePersister.
// Uses a function variable to avoid import cycles between internal/ai and internal/service.
var persistAgentACPStateToDB = func(agentID, modeState, commands, thinkingState, modelListState string) error {
	return nil // no-op until SetACPStatePersister is called
}

// SetACPStatePersister sets the function used to persist ACP cached state
// (modes, commands, thinking, model list) to the database. Must be called once during
// application startup, after service.InitDB(). This avoids import cycles
// between internal/ai and internal/service packages.
func SetACPStatePersister(fn func(agentID, modeState, commands, thinkingState, modelListState string) error) {
	persistAgentACPStateToDB = fn
}

// newACPConnEntry creates a new (uninitialized) ACPConnEntry.
func newACPConnEntry(agent *model.Agent) *ACPConnEntry {
	return &ACPConnEntry{
		agent:    agent,
		sessions: make(map[string]string),
		lastUsed: time.Now(),
		alive:    false,
	}
}

// EnsureAlive checks if the connection is alive and respawns if dead.
// This is called before every Prompt to ensure the connection is usable.
func (e *ACPConnEntry) EnsureAlive(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.alive && e.isAliveLocked() {
		return nil
	}

	return e.spawnLocked(ctx)
}

// isAliveLocked checks if the connection is still alive (must hold e.mu).
func (e *ACPConnEntry) isAliveLocked() bool {
	if e.conn == nil {
		return false
	}
	select {
	case <-e.conn.Done():
		return false
	default:
		return true
	}
}

// spawnLocked spawns the agent process and initializes the connection (must hold e.mu).
func (e *ACPConnEntry) spawnLocked(ctx context.Context) error {
	// Kill any existing process first
	if e.cmd != nil && e.cmd.Process != nil {
		_ = e.cmd.Process.Kill()
		_ = e.cmd.Wait()
	}

	cmdParts := strings.Fields(e.agent.AcpCommand)
	if len(cmdParts) == 0 {
		return fmt.Errorf("acp: no acp_command configured for agent %q", e.agent.ID)
	}

	cmdName := cmdParts[0]
	cmdArgs := cmdParts[1:]

	// Use context.Background() for the agent process lifecycle — the subprocess
	// should outlive any single request context. The pool manages the process
	// lifetime via Close() and idleSweeper, not via request cancellation.
	cmd := exec.CommandContext(context.Background(), cmdName, cmdArgs...)
	cmd.Dir = "" // cwd is per-session, set during NewSession
	cmd.Env = os.Environ()

	// Mark as ClawBench child process for orphan cleanup on server crash.
	cmd.Env = append(cmd.Env, OrphanChildEnvVar)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("acp: stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("acp: stdout pipe: %w", err)
	}
	cmd.Stderr = &strings.Builder{}

	slog.Info(
		"acp pool: spawning agent process",
		"agent_id", e.agent.ID,
		"command", cmdName,
		"args", cmdArgs,
	)

	if startErr := cmd.Start(); startErr != nil {
		return fmt.Errorf("acp: start: %w", startErr)
	}

	// Create shared ACP client and connection
	client := NewClawBenchACPClient()
	client.poolEntry = e // back-reference for cache updates
	conn := acp.NewClientSideConnection(client, stdinPipe, stdoutPipe)
	conn.SetLogger(slog.Default())

	// Initialize the connection
	initResp, err := conn.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs: acp.FileSystemCapabilities{
				ReadTextFile:  true,
				WriteTextFile: true,
			},
			Terminal: true,
		},
		ClientInfo: &acp.Implementation{
			Name:    "clawbench",
			Version: "1.0.0",
		},
	})
	if err != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("acp: initialize: %w", err)
	}

	slog.Info(
		"acp pool: agent initialized",
		"agent_id", e.agent.ID,
		"protocol_version", initResp.ProtocolVersion,
	)

	// Store the new connection state
	e.cmd = cmd
	e.conn = conn
	e.client = client
	e.sessions = make(map[string]string) // clear session mapping on respawn
	e.alive = true
	e.lastUsed = time.Now()

	// Monitor process death in background to update alive flag
	go e.watchProcessDeath()

	return nil
}

// watchProcessDeath monitors the ACP connection and marks the entry as dead
// when the agent process exits or the connection drops.
func (e *ACPConnEntry) watchProcessDeath() {
	if e.conn == nil {
		return
	}
	<-e.conn.Done()

	e.mu.Lock()
	if e.alive {
		slog.Info("acp pool: connection died", "agent_id", e.agent.ID)
		e.alive = false
	}
	e.mu.Unlock()
}

// GetOrCreateSession returns the ACP session ID for a ClawBench session.
// If the session already exists in the mapping, it returns the existing ACP session ID.
// If not, it creates a new ACP session and stores the mapping.
// Returns (acpSessionID, isNew, error).
func (e *ACPConnEntry) GetOrCreateSession(ctx context.Context, clawbenchSID string, cwd string) (string, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check existing mapping
	if acpSID, ok := e.sessions[clawbenchSID]; ok {
		slog.Debug("acp pool: reusing existing session", "clawbench_sid", clawbenchSID, "acp_sid", acpSID)
		return acpSID, false, nil
	}

	// Create new ACP session
	sessResp, err := e.conn.NewSession(ctx, acp.NewSessionRequest{
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		return "", false, fmt.Errorf("acp: session/new: %w", err)
	}

	acpSID := string(sessResp.SessionId)
	e.lastSessionResp = &sessResp

	slog.Info("acp pool: created new session", "clawbench_sid", clawbenchSID, "acp_sid", acpSID)
	e.sessions[clawbenchSID] = acpSID
	e.lastUsed = time.Now()

	return acpSID, true, nil
}

// GetAndClearSessionResp returns the last NewSessionResponse and clears it.
// Used by ExecuteStream to emit mode_update events for new sessions.
func (e *ACPConnEntry) GetAndClearSessionResp() *acp.NewSessionResponse {
	e.mu.Lock()
	defer e.mu.Unlock()
	resp := e.lastSessionResp
	e.lastSessionResp = nil
	return resp
}

// GetCachedModeState returns the cached mode state from the last session/new.
// Returns nil if no mode state has been cached yet.
func (e *ACPConnEntry) GetCachedModeState() *ModeState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cachedModeState
}

// GetCachedConfigState returns the cached config state from the last session/new.
// Returns nil if no config state has been cached yet.
func (e *ACPConnEntry) GetCachedConfigState() *ConfigOptionState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cachedConfigState
}

// SetCachedModeState caches the mode state from a NewSessionResponse.
func (e *ACPConnEntry) SetCachedModeState(state *ModeState) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cachedModeState = state
	e.debouncePersistACPState()
}

// SetCachedConfigState caches the config state from a NewSessionResponse.
func (e *ACPConnEntry) SetCachedConfigState(state *ConfigOptionState) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cachedConfigState = state
	e.debouncePersistACPState()
}

// GetCachedThinkingEffortState returns the cached thinking effort state from the last session/new.
// Returns nil if no thinking effort state has been cached yet.
func (e *ACPConnEntry) GetCachedThinkingEffortState() *ThinkingEffortState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cachedThinkingEffortState
}

// SetCachedThinkingEffortState caches the thinking effort state from a NewSessionResponse.
func (e *ACPConnEntry) SetCachedThinkingEffortState(state *ThinkingEffortState) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cachedThinkingEffortState = state
	e.debouncePersistACPState()
}

// GetCachedModelListState returns the cached model list state from the last session/new.
// Returns nil if no model list state has been cached yet.
func (e *ACPConnEntry) GetCachedModelListState() *ModelListState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cachedModelListState
}

// SetCachedModelListState caches the model list state from a NewSessionResponse or ConfigOptionUpdate.
func (e *ACPConnEntry) SetCachedModelListState(state *ModelListState) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cachedModelListState = state
	e.debouncePersistACPState()
}

// UpdateCachedCurrentModel updates the CurrentModelID in the cached model list state.
// Called when a ConfigOptionUpdate with model category arrives from the agent.
func (e *ACPConnEntry) UpdateCachedCurrentModel(modelID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cachedModelListState != nil {
		e.cachedModelListState.CurrentModelID = modelID
	}
}

// UpdateCachedCurrentMode updates only the CurrentModeID in the cached mode state.
// Called when a CurrentModeUpdate or ConfigOptionUpdate arrives from the agent,
// so that re-emitted mode_update SSE events reflect the latest mode.
func (e *ACPConnEntry) UpdateCachedCurrentMode(modeID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cachedModeState != nil {
		e.cachedModeState.CurrentModeID = modeID
	}
	if e.cachedConfigState != nil {
		e.cachedConfigState.CurrentID = modeID
	}
}

// UpdateCachedCurrentThinkingEffort updates the CurrentID in the cached thinking effort state.
// Called when a ConfigOptionUpdate with thought_level category arrives from the agent.
func (e *ACPConnEntry) UpdateCachedCurrentThinkingEffort(effortID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cachedThinkingEffortState != nil {
		e.cachedThinkingEffortState.CurrentID = effortID
	}
}

// debouncePersistACPState schedules a delayed DB persist of the current ACP state.
// Rapid successive calls reset the timer, so only one DB write happens per burst.
const acpPersistDebounce = 2 * time.Second

func (e *ACPConnEntry) debouncePersistACPState() {
	e.persistMu.Lock()
	defer e.persistMu.Unlock()

	if e.persistTimer != nil {
		e.persistTimer.Stop()
	}
	e.persistTimer = time.AfterFunc(acpPersistDebounce, func() {
		e.persistACPState()
	})
}

// persistACPState writes the current cached ACP state (modes, commands, thinking, model list)
// to the database so it can be loaded before the first message or after idle timeout.
func (e *ACPConnEntry) persistACPState() { //nolint:gocyclo // ACP state serialization has multiple optional fields
	e.mu.Lock()
	if e.agent == nil {
		e.mu.Unlock()
		return
	}
	agentID := e.agent.ID
	var modeJSON, thinkingJSON, modelListJSON string
	var cmdsJSON []byte

	if e.cachedModeState != nil {
		if b, err := json.Marshal(e.cachedModeState); err == nil {
			modeJSON = string(b)
		}
	}
	if e.cachedThinkingEffortState != nil {
		if b, err := json.Marshal(e.cachedThinkingEffortState); err == nil {
			thinkingJSON = string(b)
		}
	}
	if e.cachedModelListState != nil {
		if b, err := json.Marshal(e.cachedModelListState); err == nil {
			modelListJSON = string(b)
		}
	}
	// Commands are on the client, not the entry
	if e.client != nil {
		if cmds := e.client.GetCommandsAsInfo(); len(cmds) > 0 {
			cmdsJSON, _ = json.Marshal(cmds)
		}
	}
	e.mu.Unlock()

	if modeJSON == "" && thinkingJSON == "" && len(cmdsJSON) == 0 && modelListJSON == "" {
		return
	}

	cmdsStr := "[]"
	if len(cmdsJSON) > 0 {
		cmdsStr = string(cmdsJSON)
	}

	// Use a background import to avoid circular dependency — the actual
	// DB call lives in the service package.
	if err := persistAgentACPStateToDB(agentID, modeJSON, cmdsStr, thinkingJSON, modelListJSON); err != nil {
		slog.Debug("acp: failed to persist ACP state to DB", "agent_id", agentID, "error", err)
	}
}

// Prompt sends a prompt on the given ACP session and forwards events to streamCh.
// It registers the streamCh in the client's session routes before sending the prompt,
// and unregisters after the prompt completes.
func (e *ACPConnEntry) Prompt(ctx context.Context, acpSessionID string, prompt []acp.ContentBlock, streamCh chan<- StreamEvent, req ChatRequest) error {
	e.mu.Lock()
	client := e.client
	conn := e.conn
	e.lastUsed = time.Now()
	e.mu.Unlock()

	// Register the stream channel in the client's session routes
	// so SessionUpdate callbacks are forwarded to the right channel
	if client != nil {
		client.RegisterSession(acpSessionID, streamCh)
		defer client.UnregisterSession(acpSessionID)
	}

	// Set model if configured (non-fatal)
	if req.Model != "" {
		e.setSessionConfigOption(ctx, acpSessionID, "model", req.Model)
	}

	// Set thinking effort if configured (non-fatal)
	if req.ThinkingEffort != "" {
		e.setSessionConfigOption(ctx, acpSessionID, "thinkingEffort", req.ThinkingEffort)
	}

	// Send prompt
	_, err := conn.Prompt(ctx, acp.PromptRequest{
		SessionId: acp.SessionId(acpSessionID),
		Prompt:    prompt,
	})
	if err != nil {
		if ctx.Err() != nil {
			slog.Info("acp pool: prompt cancelled", "acp_sid", acpSessionID)
			// Cancel the current turn (not the session)
			_ = conn.Cancel(context.Background(), acp.CancelNotification{SessionId: acp.SessionId(acpSessionID)})
			return ctx.Err()
		}
		return fmt.Errorf("acp: prompt: %w", err)
	}

	return nil
}

// CancelTurn cancels the current in-progress prompt turn for the given session.
// The session remains open for subsequent prompts.
func (e *ACPConnEntry) CancelTurn(ctx context.Context, acpSessionID string) {
	e.mu.Lock()
	conn := e.conn
	e.mu.Unlock()

	if conn != nil {
		_ = conn.Cancel(ctx, acp.CancelNotification{SessionId: acp.SessionId(acpSessionID)})
	}
}

// SetSessionConfigOption sets a config option (e.g., mode, model, thinkingEffort) for a session.
// This is the exported version used by the handler layer for user-initiated mode switching.
// It resolves the ClawBench session ID to the ACP session ID internally.
// Also updates the cached mode/config/thinking state so that re-emitted SSE events
// reflect the new value immediately, rather than reverting the frontend's optimistic update.
func (e *ACPConnEntry) SetSessionConfigOption(ctx context.Context, clawbenchSID, configID, value string) {
	e.mu.Lock()
	acpSID, ok := e.sessions[clawbenchSID]
	e.mu.Unlock()

	if !ok {
		slog.Debug("acp pool: SetSessionConfigOption: session not found", "clawbench_sid", clawbenchSID)
		return
	}

	e.setSessionConfigOption(ctx, acpSID, configID, value)

	// Update cached state to match the new value so re-emitted SSE events
	// don't revert the frontend's optimistic UI update.
	switch configID {
	case "mode":
		e.UpdateCachedCurrentMode(value)
	case "thinking_effort", "thought_level":
		e.UpdateCachedCurrentThinkingEffort(value)
	case "model":
		e.UpdateCachedCurrentModel(value)
	}
}

// setSessionConfigOption sets a config option (e.g., model, thinkingEffort).
// Errors are logged but not fatal — the agent may not support this option.
func (e *ACPConnEntry) setSessionConfigOption(ctx context.Context, acpSessionID, configID, value string) {
	e.mu.Lock()
	conn := e.conn
	e.mu.Unlock()

	if conn == nil {
		return
	}

	_, err := conn.SetSessionConfigOption(ctx, acp.SetSessionConfigOptionRequest{
		ValueId: &acp.SetSessionConfigOptionValueId{
			SessionId: acp.SessionId(acpSessionID),
			ConfigId:  acp.SessionConfigId(configID),
			Value:     acp.SessionConfigValueId(value),
		},
	})
	if err != nil {
		slog.Debug("acp pool: failed to set config option (non-fatal)", "config_id", configID, "value", value, "error", err)
	}
}

// IsAlive returns whether the connection is currently alive.
func (e *ACPConnEntry) IsAlive() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.alive && e.isAliveLocked()
}

// GetClient returns the ClawBenchACPClient for this connection.
// Returns nil if the connection is not alive.
func (e *ACPConnEntry) GetClient() *ClawBenchACPClient {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.client
}

// SetEntryForTest injects a pool entry for testing. Production code must not use this.
func (p *ACPConnectionPool) SetEntryForTest(agentID string, entry *ACPConnEntry) {
	p.mu.Lock()
	p.entries[agentID] = entry
	p.mu.Unlock()
}

// SetClientForTest injects a client into the entry for testing. Production code must not use this.
func (e *ACPConnEntry) SetClientForTest(client *ClawBenchACPClient) {
	e.mu.Lock()
	e.client = client
	e.mu.Unlock()
}

// SetSessionMappingForTest injects a ClawBench→ACP session mapping for testing.
// Production code must not use this.
func (e *ACPConnEntry) SetSessionMappingForTest(clawbenchSID, acpSID string) {
	e.mu.Lock()
	if e.sessions == nil {
		e.sessions = make(map[string]string)
	}
	e.sessions[clawbenchSID] = acpSID
	e.mu.Unlock()
}

// SetAliveForTest marks the entry as alive without spawning a real process.
// It creates a minimal ClientSideConnection backed by io.Pipe so that
// isAliveLocked() returns true (conn != nil && conn.Done() not closed).
// This allows handler-level tests to bypass EnsureAlive's process spawn.
// Production code must not use this.
func (e *ACPConnEntry) SetAliveForTest() {
	pr, pw := io.Pipe()
	conn := acp.NewClientSideConnection(e.client, pw, pr)
	e.mu.Lock()
	e.alive = true
	e.conn = conn
	e.mu.Unlock()
}

// Close kills the agent process and marks the entry as dead.
func (e *ACPConnEntry) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.cmd != nil && e.cmd.Process != nil {
		_ = e.cmd.Process.Kill()
		_ = e.cmd.Wait()
	}
	e.cmd = nil
	e.conn = nil
	e.client = nil
	e.alive = false
	e.sessions = make(map[string]string)
}
