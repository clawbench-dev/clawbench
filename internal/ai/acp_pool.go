package ai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// configKilledConnectionError — typed error for set_config_option killing the connection
// ---------------------------------------------------------------------------

// configKilledConnectionError indicates that a SetSessionConfigOption call
// caused the agent process to crash or exit, killing the ACP connection.
// This is a retryable error — the connection is already marked dead and will
// be respawned on the next prompt attempt.
type configKilledConnectionError struct {
	configID string // "model", "thinkingEffort", or "mode"
	value    string // the value that caused the crash
	diag     crashDiagnostics
}

func (e *configKilledConnectionError) Error() string {
	s := "acp: set_config_option(" + e.configID + ") killed connection"
	if e.value != "" {
		s += " (value=" + e.value + ")"
	}
	if diagStr := e.diag.String(); diagStr != "" {
		s += diagStr
	}
	return s
}

// ConfigID returns the config ID that caused the crash (e.g., "model").
func (e *configKilledConnectionError) ConfigID() string { return e.configID }

// Value returns the config value that caused the crash.
func (e *configKilledConnectionError) Value() string { return e.value }

// errConfigKilledConnection creates a configKilledConnectionError for the given config ID.
func errConfigKilledConnection(configID, value string) error {
	return &configKilledConnectionError{configID: configID, value: value}
}

// errConfigKilledConnectionWithDiag creates a configKilledConnectionError with crash diagnostics.
func errConfigKilledConnectionWithDiag(configID, value string, diag crashDiagnostics) error {
	return &configKilledConnectionError{configID: configID, value: value, diag: diag}
}

// isConfigKilledConnection reports whether the error indicates a set_config_option
// call killed the agent connection. These errors are retryable.
func isConfigKilledConnection(err error) bool {
	var e *configKilledConnectionError
	return errors.As(err, &e)
}

// ---------------------------------------------------------------------------
// ACPConnManager — singleton managing one ACP connection per ClawBench session
// ---------------------------------------------------------------------------

// ACPConnManager manages one ACP stdio connection per ClawBench session.
// Idle connections are reaped by a background sweep goroutine to prevent
// stale agent processes from consuming resources indefinitely.
type ACPConnManager struct {
	mu        sync.Mutex
	conns     map[string]*ACPConn // keyed by clawbenchSID
	stopSweep chan struct{}       // closed to stop the idle sweep goroutine

	// isSessionRunning is a callback that checks whether a session is
	// actively running. Set by the service layer to avoid circular imports.
	// If nil, idle sweep skips the running-check and closes all idle connections.
	isSessionRunning func(sessionID string) bool
}

const (
	// idleSweepInterval controls how often the background sweep runs.
	idleSweepInterval = 1 * time.Minute
	// idleConnTimeout is the maximum duration a connection can be idle
	// before it is closed and removed from the pool.
	idleConnTimeout = 5 * time.Minute
)

var (
	globalManager     *ACPConnManager
	globalManagerOnce sync.Once
)

// GetACPConnManager returns the singleton connection manager.
func GetACPConnManager() *ACPConnManager {
	globalManagerOnce.Do(func() {
		globalManager = &ACPConnManager{
			conns:     make(map[string]*ACPConn),
			stopSweep: make(chan struct{}),
		}
		go globalManager.idleSweep()
	})
	return globalManager
}

// StopAll closes all connections and stops the idle sweep goroutine.
// Called on server shutdown.
func (m *ACPConnManager) StopAll() {
	// Stop the idle sweep goroutine
	close(m.stopSweep)

	m.mu.Lock()
	for sid, conn := range m.conns {
		conn.close()
		delete(m.conns, sid)
	}
	m.mu.Unlock()
}

// idleSweep periodically closes connections that have been idle for longer
// than idleConnTimeout. This prevents stale agent processes from consuming
// resources indefinitely after sessions complete without explicit deletion.
// Connections with actively running sessions are skipped.
func (m *ACPConnManager) idleSweep() {
	ticker := time.NewTicker(idleSweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopSweep:
			return
		case <-ticker.C:
			m.sweepOnce()
		}
	}
}

// sweepOnce performs a single idle sweep pass.
func (m *ACPConnManager) sweepOnce() {
	var toClose []string

	m.mu.Lock()
	now := time.Now()
	for sid, conn := range m.conns {
		conn.mu.Lock()
		idle := now.Sub(conn.lastUsed)
		alive := conn.alive
		conn.mu.Unlock()

		if !alive {
			continue // already dead, will be respawned on next use
		}
		if idle < idleConnTimeout {
			continue // not idle enough yet
		}
		// Skip connections with actively running sessions
		if m.isSessionRunning != nil && m.isSessionRunning(sid) {
			continue
		}
		toClose = append(toClose, sid)
	}
	m.mu.Unlock()

	for _, sid := range toClose {
		m.mu.Lock()
		conn, ok := m.conns[sid]
		m.mu.Unlock()

		if !ok {
			continue // already removed by CloseConn
		}

		// Re-check under conn.mu: the connection may have been used since
		// the initial scan (TOCTOU race). If it's no longer idle or the
		// session started running, skip it.
		conn.mu.Lock()
		idle := time.Since(conn.lastUsed)
		alive := conn.alive
		conn.mu.Unlock()

		if !alive {
			continue // already dead
		}
		if idle < idleConnTimeout {
			continue // recently used, no longer idle
		}
		if m.isSessionRunning != nil && m.isSessionRunning(sid) {
			continue // session started running since initial scan
		}

		// Re-acquire manager lock to atomically delete + close.
		m.mu.Lock()
		conn, ok = m.conns[sid]
		if ok {
			delete(m.conns, sid)
		}
		m.mu.Unlock()

		if ok {
			slog.Info("acp: idle sweep closing connection", "clawbench_sid", sid, "idle_duration", idle)
			conn.close()
		}
	}
}

// SetSessionRunningChecker sets the callback used by idle sweep to check
// whether a session is actively running. Must be called once during startup
// by the service layer (avoids circular import between ai and service packages).
func (m *ACPConnManager) SetSessionRunningChecker(fn func(sessionID string) bool) {
	m.isSessionRunning = fn
}

// GetOrCreateConnNoSession returns an alive ACPConn for the given agent without
// creating an ACP session. It spawns the agent process and runs Initialize (which
// populates capabilities in the registry), but does NOT call NewSession or
// ResumeSession. Used by ServeACPSessions which needs an alive connection for
// ListSessions but no session.
// Returns nil if the connection could not be established.
func (m *ACPConnManager) GetOrCreateConnNoSession(ctx context.Context, agent *model.Agent) *ACPConn {
	// Use a special key that won't collide with real session IDs.
	// This connection is shared across all ListSessions calls for this agent
	// until a real chat session claims it.
	connKey := "__list_sessions__:" + agent.ID

	m.mu.Lock()
	conn, ok := m.conns[connKey]
	if !ok {
		conn = newACPConn(agent, connKey)
		m.conns[connKey] = conn
	}
	m.mu.Unlock()

	if err := conn.EnsureAlive(ctx, ""); err != nil {
		slog.Warn("acp: GetOrCreateConnNoSession failed", "agent", agent.ID, "error", err)
		// Clean up the failed connection entry
		m.mu.Lock()
		if c, exists := m.conns[connKey]; exists && c == conn {
			delete(m.conns, connKey)
		}
		m.mu.Unlock()
		return nil
	}
	return conn
}

// GetOrCreateConn returns the ACPConn for a ClawBench session, creating one if needed.
// If the existing connection is dead, it respawns and tries to recover the session
// via ResumeSession. If recovery fails or there's no prior session, it creates a new one.
// Returns (conn, isNew, error) where isNew indicates whether a new ACP session was created.
func (m *ACPConnManager) GetOrCreateConn(ctx context.Context, agent *model.Agent, clawbenchSID, cwd string) (*ACPConn, bool, error) {
	m.mu.Lock()
	conn, ok := m.conns[clawbenchSID]
	if !ok {
		conn = newACPConn(agent, clawbenchSID)
		// Pre-populate acpSID from DB so ensureAliveWithSession can attempt
		// ResumeSession after a server restart.
		if extID := getExternalSessionID(clawbenchSID); extID != "" {
			conn.acpSID = extID
			slog.Info("acp conn: pre-populated acpSID from DB for ResumeSession",
				"clawbench_sid", clawbenchSID, "acp_sid", extID)
		}
		m.conns[clawbenchSID] = conn
	}
	m.mu.Unlock()

	isNew, err := conn.ensureAliveWithSession(ctx, cwd)
	if err != nil {
		return nil, false, err
	}
	return conn, isNew, nil
}

// GetOrCreateConnForLoad creates an ACPConn for a LoadSession operation.
// Unlike GetOrCreateConn, this sets loadTargetSID so that ensureAliveWithSession
// calls LoadSession instead of NewSession/ResumeSession.
// Returns (conn, error).
func (m *ACPConnManager) GetOrCreateConnForLoad(ctx context.Context, agent *model.Agent, clawbenchSID, acpSessionID, cwd string) (*ACPConn, error) {
	m.mu.Lock()
	conn, ok := m.conns[clawbenchSID]
	if !ok {
		conn = newACPConn(agent, clawbenchSID)
		m.conns[clawbenchSID] = conn
	}
	m.mu.Unlock()

	conn.mu.Lock()
	conn.loadTargetSID = acpSessionID
	conn.mu.Unlock()

	_, err := conn.ensureAliveWithSession(ctx, cwd)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// GetConn returns the ACPConn for the given ClawBench session ID.
// Returns nil if no connection exists.
func (m *ACPConnManager) GetConn(clawbenchSID string) *ACPConn {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.conns[clawbenchSID]
}

// GetConnByAgentID returns an alive ACPConn for the given agent ID.
// Returns nil if no alive connection exists for this agent.
func (m *ACPConnManager) GetConnByAgentID(agentID string) *ACPConn {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, conn := range m.conns {
		conn.mu.Lock()
		matched := conn.agent != nil && conn.agent.ID == agentID && conn.alive
		conn.mu.Unlock()
		if matched {
			return conn
		}
	}
	return nil
}

// CancelTurn sends an ACP Cancel notification for the given ClawBench session.
func (m *ACPConnManager) CancelTurn(clawbenchSID string) {
	m.mu.Lock()
	conn := m.conns[clawbenchSID]
	m.mu.Unlock()

	if conn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		conn.CancelTurn(ctx)
		cancel()
	}
}

// CloseConn closes and removes the connection for the given ClawBench session ID.
func (m *ACPConnManager) CloseConn(clawbenchSID string) {
	m.mu.Lock()
	conn, ok := m.conns[clawbenchSID]
	if ok {
		delete(m.conns, clawbenchSID)
	}
	m.mu.Unlock()

	if ok {
		conn.close()
	}
}

// MarkIdle marks the connection for a ClawBench session as idle by setting
// lastUsed to the current time.
func (m *ACPConnManager) MarkIdle(clawbenchSID string) {
	m.mu.Lock()
	conn, ok := m.conns[clawbenchSID]
	m.mu.Unlock()

	if ok {
		conn.mu.Lock()
		conn.lastUsed = time.Now()
		conn.mu.Unlock()
	}
}

// ACPCachedState holds the cached ACP state for a connection.
type ACPCachedState struct {
	Mode      *ModeState
	Config    *ConfigOptionState
	Effort    *ThinkingEffortState
	Commands  []AvailableCommandInfo
	ModelList *ModelListState
	Plan      *PlanState
	Usage     *UsageState
}

// GetCachedStateByClawbenchSID returns the cached state for the connection
// owned by the given ClawBench session ID.
func (m *ACPConnManager) GetCachedStateByClawbenchSID(clawbenchSID string) ACPCachedState {
	m.mu.Lock()
	conn := m.conns[clawbenchSID]
	m.mu.Unlock()

	if conn == nil {
		return ACPCachedState{}
	}

	if !conn.mu.TryLock() {
		return ACPCachedState{}
	}
	currentModeID := conn.currentModeID
	currentThinkingEffortID := conn.currentThinkingEffortID
	currentModelID := conn.currentModelID
	planState := conn.cachedPlanState
	usageState := conn.cachedUsageState
	agentID := ""
	if conn.agent != nil {
		agentID = conn.agent.ID
	}
	conn.mu.Unlock()

	if agentID == "" {
		return ACPCachedState{}
	}

	reg := GetAgentCapabilityRegistry()
	return ACPCachedState{
		Mode:      reg.GetModeState(agentID, currentModeID),
		Effort:    reg.GetThinkingEffortState(agentID, currentThinkingEffortID),
		ModelList: reg.GetModelListState(agentID, currentModelID),
		Commands:  reg.GetCommands(agentID),
		Config:    reg.GetConfigState(agentID),
		Plan:      planState,
		Usage:     usageState,
	}
}

// GetCachedStateByAgentID returns agent-level capabilities from the registry
// for the given agent ID.
func (m *ACPConnManager) GetCachedStateByAgentID(agentID string) ACPCachedState {
	reg := GetAgentCapabilityRegistry()
	agentCap := reg.Get(agentID)
	if agentCap == nil || !agentCap.HasData() {
		return ACPCachedState{}
	}
	return ACPCachedState{
		Mode:      reg.GetModeState(agentID, ""),
		Effort:    reg.GetThinkingEffortState(agentID, ""),
		ModelList: reg.GetModelListState(agentID, ""),
		Commands:  reg.GetCommands(agentID),
		Config:    reg.GetConfigState(agentID),
	}
}

// GetCommandsByAgentID returns the cached slash commands for an agent from the registry.
func (m *ACPConnManager) GetCommandsByAgentID(agentID string) []AvailableCommandInfo {
	return GetAgentCapabilityRegistry().GetCommands(agentID)
}

// GetClientByAgentID returns the ClawBenchACPClient for any connection
// belonging to the given agent. Returns nil if not found.
func (m *ACPConnManager) GetClientByAgentID(agentID string) *ClawBenchACPClient {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, conn := range m.conns {
		conn.mu.Lock()
		matched := (conn.agent != nil && conn.agent.ID == agentID) || key == agentID
		if matched {
			client := conn.client
			conn.mu.Unlock()
			return client
		}
		conn.mu.Unlock()
	}
	return nil
}

// SetConnForTest injects a connection for testing. Production code must not use this.
func (m *ACPConnManager) SetConnForTest(clawbenchSID string, conn *ACPConn) {
	m.mu.Lock()
	m.conns[clawbenchSID] = conn
	m.mu.Unlock()
}

// CloseConnsByAgentID closes all ACP connections for the given agent ID.
// Used when transport is switched from ACP to CLI.
func (m *ACPConnManager) CloseConnsByAgentID(agentID string) {
	m.mu.Lock()
	var toClose []*ACPConn
	for sid, conn := range m.conns {
		if conn.agent != nil && conn.agent.ID == agentID {
			delete(m.conns, sid)
			toClose = append(toClose, conn)
		}
	}
	m.mu.Unlock()
	for _, conn := range toClose {
		conn.close()
	}
}

// GetPendingApprovalSessionIDs returns the set of ClawBench session IDs that
// currently have a pending permission approval request.
func (m *ACPConnManager) GetPendingApprovalSessionIDs() map[string]bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make(map[string]bool)
	for sid, conn := range m.conns {
		if !conn.mu.TryLock() {
			continue
		}
		if conn.client != nil {
			if conn.client.mu.TryLock() {
				for _, pp := range conn.client.pendingPermission {
					if pp.SessionID == conn.acpSID {
						result[sid] = true
					}
				}
				conn.client.mu.Unlock()
			}
		}
		conn.mu.Unlock()
	}
	return result
}

// ---------------------------------------------------------------------------
// ACPConn — one ACP stdio connection for one ClawBench session
// ---------------------------------------------------------------------------

// ACPConn represents a dedicated ACP stdio connection for one ClawBench session.
// One session = one agent process = one ACP session. No sharing, no pooling.
type ACPConn struct {
	agent        *model.Agent
	clawbenchSID string
	cwd          string // project working directory, set on first ensureAliveWithSession
	mu           sync.Mutex

	cmd    *exec.Cmd
	conn   *acp.ClientSideConnection
	client *ClawBenchACPClient

	// stdoutFilter wraps the agent's stdout pipe to fix ACP protocol violations
	// (string-number IDs, non-JSON lines). Must be Close'd when the process dies
	// to unblock pending reads and prevent cleanup hangs.
	stdoutFilter *acpStdoutFilter

	// acpSID is the ACP session ID. Populated from DB (ResumeSession) or
	// from NewSession response. Empty means no session yet.
	acpSID string

	// lastNewSessionResp stores the NewSessionResponse from the most recent
	// session/new so ExecuteStream can extract mode/config state. Cleared after reading.
	lastNewSessionResp *acp.NewSessionResponse

	// lastResumeSessionResp stores the ResumeSessionResponse from the most recent
	// session/resume so ExecuteStream can extract mode/config state. Cleared after reading.
	lastResumeSessionResp *acp.ResumeSessionResponse

	// lastLoadSessionResp stores the LoadSessionResponse from the most recent
	// session/load so the handler can extract mode/config state. Cleared after reading.
	lastLoadSessionResp *acp.LoadSessionResponse

	// loadTargetSID is the ACP session ID to load via LoadSession.
	loadTargetSID string

	// loadSessionActive indicates that a LoadSession replay is in progress.
	loadSessionActive atomic.Bool

	// liveness
	lastUsed  time.Time
	alive     bool
	startedAt time.Time // when the agent process was spawned

	// cmdWaitOnce ensures cmd.Wait() is called exactly once; the result is
	// cached in cmdWaitState for subsequent readers.
	cmdWaitOnce  sync.Once
	cmdWaitState *os.ProcessState

	// cached state — populated from NewSession/ResumeSession responses
	currentModeID           string
	currentThinkingEffortID string
	currentModelID          string
	cachedPlanState         *PlanState
	cachedUsageState        *UsageState

	// lastSetConfig tracks the last values successfully sent to the agent via
	// setSessionConfigOption. Used to avoid re-sending unchanged values.
	lastSetConfigMu sync.Mutex
	lastSetModel    string
	lastSetEffort   string
	lastSetMode     string

	// autoApprove enables hands-off mode: all permission requests are
	// automatically approved with the first allow_* option.
	autoApprove bool

	// unsupportedConfigs tracks config IDs that the agent reported as unknown.
	unsupportedConfigs map[string]bool

	// listSessionsFn overrides ListSessions for testing. If nil, the real
	// ACP JSON-RPC call is used.
	listSessionsFn func(ctx context.Context, cursor *string) ([]acp.SessionInfo, *string, error)
}

// newACPConn creates a new (uninitialized) ACPConn.
func newACPConn(agent *model.Agent, clawbenchSID string) *ACPConn {
	return &ACPConn{
		agent:        agent,
		clawbenchSID: clawbenchSID,
		lastUsed:     time.Now(),
		alive:        false,
	}
}

// ---------------------------------------------------------------------------
// Session-level state accessors
// ---------------------------------------------------------------------------

// AcpSID returns the ACP session ID for this connection.
func (c *ACPConn) AcpSID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.acpSID
}

// AgentID returns the ID of the agent this connection belongs to.
func (c *ACPConn) AgentID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.agent != nil {
		return c.agent.ID
	}
	return ""
}

// BackendID returns the backend identifier of the agent this connection belongs to.
// Used for ACP event mapping to look up backend-specific tool name and input remap tables.
func (c *ACPConn) BackendID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.agent != nil {
		return c.agent.Backend
	}
	return ""
}

// IsAlive returns whether the connection is currently alive.
func (c *ACPConn) IsAlive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.alive && c.isAliveLocked()
}

// GetClient returns the ClawBenchACPClient for this connection.
func (c *ACPConn) GetClient() *ClawBenchACPClient {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.client
}

// GetAndClearNewSessionResp returns the last NewSessionResponse and clears it.
func (c *ACPConn) GetAndClearNewSessionResp() *acp.NewSessionResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	resp := c.lastNewSessionResp
	c.lastNewSessionResp = nil
	return resp
}

// GetAndClearResumeSessionResp returns the last ResumeSessionResponse and clears it.
func (c *ACPConn) GetAndClearResumeSessionResp() *acp.ResumeSessionResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	resp := c.lastResumeSessionResp
	c.lastResumeSessionResp = nil
	return resp
}

// GetAndClearLoadSessionResp returns the last LoadSessionResponse and clears it.
func (c *ACPConn) GetAndClearLoadSessionResp() *acp.LoadSessionResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	resp := c.lastLoadSessionResp
	c.lastLoadSessionResp = nil
	return resp
}

// ClearLoadSessionActive sets loadSessionActive to false after the handler
// has read the replay buffer.
func (c *ACPConn) ClearLoadSessionActive() {
	c.loadSessionActive.Store(false)
}

// GetCurrentModeID returns the session's current mode ID.
func (c *ACPConn) GetCurrentModeID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentModeID
}

// SetCurrentModeID sets the session's current mode ID.
func (c *ACPConn) SetCurrentModeID(modeID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentModeID = modeID
}

// GetCurrentThinkingEffortID returns the session's current thinking effort ID.
func (c *ACPConn) GetCurrentThinkingEffortID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentThinkingEffortID
}

// SetCurrentThinkingEffortID sets the session's current thinking effort ID.
func (c *ACPConn) SetCurrentThinkingEffortID(effortID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentThinkingEffortID = effortID
}

// GetCurrentModelID returns the session's current model ID.
func (c *ACPConn) GetCurrentModelID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentModelID
}

// SetCurrentModelID sets the session's current model ID.
func (c *ACPConn) SetCurrentModelID(modelID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentModelID = modelID
}

// SetCachedPlanState caches the plan state from a plan_update event.
func (c *ACPConn) SetCachedPlanState(state *PlanState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cachedPlanState = state
}

// GetCachedPlanState returns the cached plan state.
func (c *ACPConn) GetCachedPlanState() *PlanState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cachedPlanState
}

// SetCachedUsageState caches the usage state from a usage_update event.
func (c *ACPConn) SetCachedUsageState(state *UsageState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cachedUsageState = state
}

// GetCachedUsageState returns the cached usage state.
func (c *ACPConn) GetCachedUsageState() *UsageState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cachedUsageState
}

// SetAutoApprove enables or disables hands-off mode for this connection.
func (c *ACPConn) SetAutoApprove(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.autoApprove = enabled
}

// IsAutoApprove returns whether hands-off mode is enabled.
func (c *ACPConn) IsAutoApprove() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.autoApprove
}

// IsConfigUnsupported reports whether the agent has rejected a config ID as unknown.
func (c *ACPConn) IsConfigUnsupported(configID string) bool {
	c.lastSetConfigMu.Lock()
	defer c.lastSetConfigMu.Unlock()
	return c.unsupportedConfigs != nil && c.unsupportedConfigs[configID]
}

// shouldSetConfig returns true if the config value has changed since the last
// successful set AND the config is not marked as unsupported by the agent.
func (c *ACPConn) shouldSetConfig(configID, value string) bool {
	c.lastSetConfigMu.Lock()
	defer c.lastSetConfigMu.Unlock()
	if c.unsupportedConfigs != nil && c.unsupportedConfigs[configID] {
		return false
	}
	switch configID {
	case "model":
		return c.lastSetModel != value
	case "thinkingEffort":
		return c.lastSetEffort != value
	case "mode":
		return c.lastSetMode != value
	}
	return true
}

// markConfigSet records that a config value was successfully sent.
func (c *ACPConn) markConfigSet(configID, value string) {
	c.lastSetConfigMu.Lock()
	defer c.lastSetConfigMu.Unlock()
	switch configID {
	case "model":
		c.lastSetModel = value
	case "thinkingEffort":
		c.lastSetEffort = value
	case "mode":
		c.lastSetMode = value
	}
}

// resetLastSetConfig clears cached config values (called on respawn).
func (c *ACPConn) resetLastSetConfig() {
	c.lastSetConfigMu.Lock()
	defer c.lastSetConfigMu.Unlock()
	c.lastSetModel = ""
	c.lastSetEffort = ""
	c.lastSetMode = ""
	c.unsupportedConfigs = nil
}

func (c *ACPConn) UpdateCachedCurrentModel(modelID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentModelID = modelID
}

func (c *ACPConn) UpdateCachedCurrentMode(modeID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentModeID = modeID
}

func (c *ACPConn) UpdateCachedCurrentThinkingEffort(effortID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentThinkingEffortID = effortID
}

// PreApplyConfigCurrentID optimistically updates the registry's ConfigOptionState.CurrentID
// before SSE events are emitted, so the frontend sees the user's requested value
// (e.g. "plan") instead of the agent's default (e.g. "bypassPermissions").
// The actual RPC is still done inside Prompt(); this only affects SSE display.
func (c *ACPConn) PreApplyConfigCurrentID(configID, value string) {
	agentID := c.AgentID()
	reg := GetAgentCapabilityRegistry()
	configState := reg.GetConfigState(agentID)
	if configState == nil {
		return
	}
	// Find the matching option and update CurrentID only if the value is valid
	for _, opt := range configState.Options {
		if opt.Category == configID || opt.ID == configID {
			for _, v := range opt.Values {
				if v.ID == value {
					configState.CurrentID = value
					return
				}
			}
		}
	}
}

// SetCachedModeState updates the session's current mode ID and registers
// available modes in the agent capability registry.
func (c *ACPConn) SetCachedModeState(state *ModeState) {
	if state == nil {
		return
	}
	c.mu.Lock()
	c.currentModeID = state.CurrentModeID
	agentID := ""
	if c.agent != nil {
		agentID = c.agent.ID
	}
	c.mu.Unlock()
	if agentID != "" && len(state.AvailableModes) > 0 {
		GetAgentCapabilityRegistry().UpdateModes(agentID, state.AvailableModes)
	}
}

// SetCachedConfigState registers the config option state in the agent capability registry.
func (c *ACPConn) SetCachedConfigState(state *ConfigOptionState) {
	if state == nil {
		return
	}
	c.mu.Lock()
	agentID := ""
	if c.agent != nil {
		agentID = c.agent.ID
	}
	c.mu.Unlock()
	if agentID != "" {
		GetAgentCapabilityRegistry().UpdateConfigState(agentID, state)
		if !GetAgentCapabilityRegistry().HasAvailableModes(agentID) {
			if derived := modeStateFromConfigState(state); derived != nil && len(derived.AvailableModes) > 0 {
				GetAgentCapabilityRegistry().UpdateModes(agentID, derived.AvailableModes)
				c.mu.Lock()
				if c.currentModeID == "" {
					c.currentModeID = derived.CurrentModeID
				}
				c.mu.Unlock()
			}
		}
	}
}

// SetCachedThinkingEffortState updates the session's current thinking effort ID
// and registers available levels in the agent capability registry.
func (c *ACPConn) SetCachedThinkingEffortState(state *ThinkingEffortState) {
	if state == nil {
		return
	}
	c.mu.Lock()
	c.currentThinkingEffortID = state.CurrentID
	agentID := ""
	if c.agent != nil {
		agentID = c.agent.ID
	}
	c.mu.Unlock()
	if agentID != "" && len(state.AvailableLevels) > 0 {
		GetAgentCapabilityRegistry().UpdateThinkingEfforts(agentID, state.AvailableLevels)
	}
}

// SetCachedModelListState updates the session's current model ID
// and registers available models in the agent capability registry.
func (c *ACPConn) SetCachedModelListState(state *ModelListState) {
	if state == nil {
		return
	}
	c.mu.Lock()
	c.currentModelID = state.CurrentModelID
	agentID := ""
	if c.agent != nil {
		agentID = c.agent.ID
	}
	c.mu.Unlock()
	if agentID != "" && len(state.Models) > 0 {
		GetAgentCapabilityRegistry().UpdateModels(agentID, state.Models)
	}
}

// HasNewAvailableModes delegates to AgentCapabilityRegistry.
func (c *ACPConn) HasNewAvailableModes(newModes []ModeDef) bool {
	c.mu.Lock()
	agentID := ""
	if c.agent != nil {
		agentID = c.agent.ID
	}
	c.mu.Unlock()
	return GetAgentCapabilityRegistry().HasNewAvailableModes(agentID, newModes)
}

// HasCurrentModeChanged checks if the given modeId differs from the session's current mode.
func (c *ACPConn) HasCurrentModeChanged(modeID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentModeID != modeID
}

// IsModeAvailable delegates to AgentCapabilityRegistry.
func (c *ACPConn) IsModeAvailable(modeID string) bool {
	c.mu.Lock()
	agentID := ""
	if c.agent != nil {
		agentID = c.agent.ID
	}
	c.mu.Unlock()
	return GetAgentCapabilityRegistry().IsModeAvailable(agentID, modeID)
}

// HasNewAvailableThinkingEfforts delegates to AgentCapabilityRegistry.
func (c *ACPConn) HasNewAvailableThinkingEfforts(newLevels []ThinkingEffortDef) bool {
	c.mu.Lock()
	agentID := ""
	if c.agent != nil {
		agentID = c.agent.ID
	}
	c.mu.Unlock()
	return GetAgentCapabilityRegistry().HasNewAvailableThinkingEfforts(agentID, newLevels)
}

// HasNewAvailableModels delegates to AgentCapabilityRegistry.
func (c *ACPConn) HasNewAvailableModels(newModels []model.AgentModel) bool {
	c.mu.Lock()
	agentID := ""
	if c.agent != nil {
		agentID = c.agent.ID
	}
	c.mu.Unlock()
	return GetAgentCapabilityRegistry().HasNewAvailableModels(agentID, newModels)
}

// ProcessPID returns the PID of the agent subprocess, or 0 if none.
func (c *ACPConn) ProcessPID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd != nil && c.cmd.Process != nil {
		return c.cmd.Process.Pid
	}
	return 0
}

// close kills the agent process and marks the connection as dead.
func (c *ACPConn) close() {
	c.mu.Lock()

	if c.cmd != nil && c.cmd.Process != nil {
		// Close the stdout filter first to unblock pending reads on the pipe.
		// Without this, cmd.Wait() hangs when the process is killed but
		// stdout hasn't been closed yet (same pattern as killProcessLocked).
		if c.stdoutFilter != nil {
			c.stdoutFilter.Close()
			c.stdoutFilter = nil
		}

		// Kill the entire process group (not just the parent process).
		// ACP agents like Claude are spawned via npx, which creates a child
		// process (claude). Killing only npx leaves the child alive, which
		// holds the stderr pipe open and causes cmd.Wait() to hang.
		killProcessGroup(c.cmd.Process)

		oldCmd := c.cmd
		c.mu.Unlock()
		_ = oldCmd.Wait()
		c.mu.Lock()
		if c.cmd == oldCmd {
			c.cmd = nil
		}
	}

	c.cmd = nil
	c.conn = nil
	c.client = nil
	c.alive = false
	c.acpSID = ""
	c.mu.Unlock()
}

// Close kills the agent process and marks the connection as dead.
// Public alias for close().
func (c *ACPConn) Close() {
	c.close()
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// SetClientForTest injects a client for testing.
func (c *ACPConn) SetClientForTest(client *ClawBenchACPClient) {
	c.mu.Lock()
	c.client = client
	c.mu.Unlock()
}

// SetSessionMappingForTest injects an ACP session ID for testing.
func (c *ACPConn) SetSessionMappingForTest(_, acpSID string) {
	c.mu.Lock()
	c.acpSID = acpSID
	c.mu.Unlock()
}

// SetAliveForTest marks the connection as alive without spawning a real process.
func (c *ACPConn) SetAliveForTest() {
	pr, pw := io.Pipe()
	conn := acp.NewClientSideConnection(c.client, pw, pr)
	c.mu.Lock()
	c.alive = true
	c.conn = conn
	c.mu.Unlock()
}

// KillProcessForTest kills the agent subprocess for integration testing.
func (c *ACPConn) KillProcessForTest() error {
	c.mu.Lock()
	if c.cmd == nil || c.cmd.Process == nil {
		c.mu.Unlock()
		return fmt.Errorf("acp: no process to kill")
	}
	p := c.cmd.Process
	c.mu.Unlock()
	return p.Kill()
}

// SetListSessionsFnForTest overrides the ListSessions implementation for testing.
// If fn is non-nil, it is called instead of the real ACP JSON-RPC call.
func (c *ACPConn) SetListSessionsFnForTest(fn func(ctx context.Context, cursor *string) ([]acp.SessionInfo, *string, error)) {
	c.mu.Lock()
	c.listSessionsFn = fn
	c.mu.Unlock()
}

// InjectAliveConnForTest creates and registers an alive ACPConn for testing.
// The connection is marked as alive with a session mapping and optional client,
// so that GetOrCreateConnForLoad will find and reuse it (ensureAliveWithSession
// returns early when alive + acpSID is set). Returns the conn and a cleanup function.
// Production code must not use this.
func (m *ACPConnManager) InjectAliveConnForTest(clawbenchSID string, agent *model.Agent, acpSID string, client *ClawBenchACPClient) *ACPConn {
	conn := newACPConn(agent, clawbenchSID)
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest(clawbenchSID, acpSID)
	if client != nil {
		conn.SetClientForTest(client)
	}
	m.SetConnForTest(clawbenchSID, conn)
	return conn
}

// NewACPConnForTest creates a new (uninitialized) ACPConn for testing.
// Production code must not use this.
func NewACPConnForTest(agent *model.Agent, clawbenchSID string) *ACPConn {
	return newACPConn(agent, clawbenchSID)
}
