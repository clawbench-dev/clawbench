package ai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
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

// GetOrCreateConn returns the ACPConn for a ClawBench session, creating one if needed.
// If the existing connection is dead, it respawns and tries to recover the session
// via ResumeSession. If recovery fails or there's no prior session, it creates a new one.
// Returns (conn, isNew, error) where isNew indicates whether a new ACP session was created.
func (m *ACPConnManager) GetOrCreateConn(ctx context.Context, agent *model.Agent, clawbenchSID, cwd string) (*ACPConn, bool, error) {
	m.mu.Lock()
	conn, ok := m.conns[clawbenchSID]
	if !ok {
		conn = newACPConn(agent, clawbenchSID)
		m.conns[clawbenchSID] = conn
	}
	m.mu.Unlock()

	isNew, err := conn.ensureAliveWithSession(ctx, cwd)
	if err != nil {
		return nil, false, err
	}
	return conn, isNew, nil
}

// GetConn returns the ACPConn for the given ClawBench session ID.
// Returns nil if no connection exists.
func (m *ACPConnManager) GetConn(clawbenchSID string) *ACPConn {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.conns[clawbenchSID]
}

// CancelTurn sends an ACP Cancel notification for the given ClawBench session.
// This tells the ACP agent to stop its current turn gracefully, which prevents
// zombie processes when the user cancels mid-stream. Safe to call even if no
// connection exists or the connection is dead.
// Uses a 3-second timeout to avoid blocking the caller if the agent's stdin
// pipe is full (e.g. agent busy with a long tool call and not reading stdin).
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
// Used for explicit teardown (session deletion, server shutdown). Do NOT call
// this from AI goroutine defers — use MarkIdle instead to avoid racing with a
// new prompt on the same session.
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
// lastUsed to the current time. This replaces the old deferred CloseConn in
// the AI goroutine, which caused a race condition: the goroutine sets
// session-running=false, then a new request starts and reuses the connection,
// but the deferred CloseConn still fires and kills the process mid-prompt.
//
// MarkIdle is safe because idleSweep will close the connection after the
// idle timeout only if no new request has claimed it. If a new request arrives
// before the sweep, GetOrCreateConn reuses the live connection.
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
}

// GetCachedStateByClawbenchSID returns the cached state for the connection
// owned by the given ClawBench session ID. Combines agent-level capabilities
// from the registry with session-level current values from the ACPConn.
func (m *ACPConnManager) GetCachedStateByClawbenchSID(clawbenchSID string) ACPCachedState {
	m.mu.Lock()
	conn := m.conns[clawbenchSID]
	m.mu.Unlock()

	if conn == nil {
		return ACPCachedState{}
	}

	// Use TryLock to avoid blocking if ensureAliveWithSession holds conn.mu
	// (e.g., during GetOrCreateConn for a new session). In that case return
	// empty state — the next request will find the connection ready.
	if !conn.mu.TryLock() {
		return ACPCachedState{}
	}
	currentModeID := conn.currentModeID
	currentThinkingEffortID := conn.currentThinkingEffortID
	currentModelID := conn.currentModelID
	planState := conn.cachedPlanState
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
	}
}

// GetCachedStateByAgentID returns agent-level capabilities from the registry
// for the given agent ID. Used for pre-fetching state before the first
// message (no session yet). Session-level current values are empty.
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

// ---------------------------------------------------------------------------
// Backward-compatible aliases — temporary until all callers are migrated
// ---------------------------------------------------------------------------

// GetACPConnectionPool returns the singleton connection manager.
//
// Deprecated: use GetACPConnManager() instead.
func GetACPConnectionPool() *ACPConnManager {
	return GetACPConnManager()
}

// ACPConnectionPool is an alias for ACPConnManager for backward compatibility.
//
// Deprecated: use ACPConnManager instead.
type ACPConnectionPool = ACPConnManager

// ACPConnEntry is an alias for ACPConn for backward compatibility.
//
// Deprecated: use ACPConn instead.
type ACPConnEntry = ACPConn

// ---------------------------------------------------------------------------
// ACPConn — one ACP stdio connection for one ClawBench session
// ---------------------------------------------------------------------------

// ACPConn represents a dedicated ACP stdio connection for one ClawBench session.
// One session = one agent process = one ACP session. No sharing, no pooling.
type ACPConn struct {
	agent        *model.Agent
	clawbenchSID string
	mu           sync.Mutex

	cmd    *exec.Cmd
	conn   *acp.ClientSideConnection
	client *ClawBenchACPClient

	// acpSID is the ACP session ID. Populated from DB (ResumeSession) or
	// from NewSession response. Empty means no session yet.
	acpSID string

	// lastNewSessionResp stores the NewSessionResponse from the most recent
	// session/new so ExecuteStream can extract mode/config state. Cleared after reading.
	lastNewSessionResp *acp.NewSessionResponse

	// lastResumeSessionResp stores the ResumeSessionResponse from the most recent
	// session/resume so ExecuteStream can extract mode/config state. Cleared after reading.
	lastResumeSessionResp *acp.ResumeSessionResponse

	// liveness
	lastUsed  time.Time
	alive     bool
	startedAt time.Time // when the agent process was spawned

	// cmdWaitOnce ensures cmd.Wait() is called exactly once; the result is
	// cached in cmdWaitState for subsequent readers (e.g. collectCrashDiagnostics
	// and spawnLocked both need the exit state).
	cmdWaitOnce  sync.Once
	cmdWaitState *os.ProcessState

	// cached state — populated from NewSession/ResumeSession responses and
	// re-emitted for every ExecuteStream call so the frontend always has
	// up-to-date mode/command state, even after page refreshes or SSE reconnects.
	//
	// Agent-level capabilities (available modes, thinking levels, models, commands)
	// are stored in AgentCapabilityRegistry. ACPConn only holds session-level
	// current values that differ between sessions of the same agent.
	currentModeID           string
	currentThinkingEffortID string
	currentModelID          string
	cachedPlanState         *PlanState

	// lastSetConfig tracks the last values successfully sent to the agent via
	// setSessionConfigOption. Used to avoid re-sending unchanged values that
	// may trigger expensive agent-side restarts (e.g., Claude bridge setModel).
	lastSetConfigMu sync.Mutex
	lastSetModel    string
	lastSetEffort   string
	lastSetMode     string

	// autoApprove enables hands-off mode: all permission requests are
	// automatically approved with the first allow_* option.
	autoApprove bool

	// unsupportedConfigs tracks config IDs that the agent reported as unknown
	// (e.g., CodeBuddy doesn't support "thinkingEffort"). Once detected, we
	// skip sending that config to avoid flooding the agent with errors on every
	// prompt. Cleared on respawn — the new process might support it after an update.
	unsupportedConfigs map[string]bool
}

// getExternalSessionID is the global function for looking up the ACP session ID
// from the database. Set by the application startup via SetExternalSessionIDGetter.
// Uses a function variable to avoid import cycles between internal/ai and internal/service.
var getExternalSessionID = func(clawbenchSID string) string {
	return "" // no-op until SetExternalSessionIDGetter is called
}

// SetExternalSessionIDGetter sets the function used to look up the ACP session ID
// from the database. Must be called once during application startup, after service.InitDB().
func SetExternalSessionIDGetter(fn func(clawbenchSID string) string) {
	getExternalSessionID = fn
}

// getSessionAutoApprove is the global function for looking up auto-approve state
// from the database. Set by the application startup via SetAutoApproveGetter.
var getSessionAutoApprove = func(clawbenchSID string) bool {
	return false // no-op until SetAutoApproveGetter is called
}

// SetAutoApproveGetter sets the function used to look up auto-approve state
// from the database. Must be called once during application startup, after service.InitDB().
func SetAutoApproveGetter(fn func(clawbenchSID string) bool) {
	getSessionAutoApprove = fn
}

// onPermissionStateChange is called when a pending permission request is added or resolved.
// Set by the application startup via SetPermissionStateChangeCallback.
var onPermissionStateChange = func(clawbenchSID string, pending bool) {}

// SetPermissionStateChangeCallback sets the callback invoked when a permission
// approval state changes for a session. Must be called once during startup by
// the service layer (avoids circular import between ai and service/ws packages).
func SetPermissionStateChangeCallback(fn func(clawbenchSID string, pending bool)) {
	onPermissionStateChange = fn
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

// ensureAliveWithSession ensures the connection is alive and has a valid ACP session.
// If the process is dead, it respawns and tries ResumeSession recovery, falling back to NewSession.
// Returns isNew=true if a new ACP session was created, false if reusing or recovered.
func (c *ACPConn) ensureAliveWithSession(ctx context.Context, cwd string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If alive and already has a session, reuse
	if c.alive && c.isAliveLocked() && c.acpSID != "" {
		slog.Debug("acp conn: reusing existing connection", "clawbench_sid", c.clawbenchSID, "acp_sid", c.acpSID)
		c.lastUsed = time.Now()
		return false, nil
	}

	// Snapshot cached config state before spawn, so we can re-apply it after
	// ResumeSession. When an agent process crashes and is respawned, the
	// ResumeSession response reports the agent's DEFAULT config values (not
	// the previously-set ones), which would overwrite our cache and cause
	// "amnesia" — the user's mode/model/thinking selections would be lost.
	prevConfig := c.snapshotCachedConfig()

	// Need to spawn or respawn
	if err := c.spawnLocked(ctx); err != nil {
		return false, err
	}

	// Try to recover session via ResumeSession (no history replay — ClawBench has its own DB)
	// If ResumeSession fails (e.g., session deleted), fall back to NewSession.
	acpSID := getExternalSessionID(c.clawbenchSID)
	if acpSID != "" {
		err := c.recoverViaResumeSession(ctx, cwd, acpSID, prevConfig)
		if err == nil {
			return false, nil // recovered successfully
		}
		// ResumeSession failed — the old session is gone (deleted, expired, etc.).
		// Fall through to create a new session instead of returning the error.
		slog.Warn("acp conn: ResumeSession failed, falling back to NewSession",
			"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "error", err)
		// The process may be in a bad state after a failed resume; kill it
		// and respawn a fresh one for the new session.
		c.killProcessLocked()
		if err := c.spawnLocked(ctx); err != nil {
			return false, err
		}
	}

	// No prior session (or ResumeSession failed) — create new session.
	// Timeout prevents blocking forever if the agent binary hangs during
	// session creation (e.g., bridge adapter waiting for claude CLI init).
	// Use the parent context's deadline if available, otherwise default 60s.
	newSessCtx, newSessCancel := context.WithTimeout(ctx, 60*time.Second)
	defer newSessCancel()

	sessResp, err := c.conn.NewSession(newSessCtx, acp.NewSessionRequest{
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		// Mark connection dead so the next request triggers a fresh spawn
		// instead of reusing a connection whose agent process is in an
		// unknown state (e.g., hung during session creation).
		// Note: caller (ensureAliveWithSession) already holds c.mu, so no Lock here.
		c.alive = false
		return false, fmt.Errorf("acp: session/new: %w", err)
	}

	c.acpSID = string(sessResp.SessionId)
	c.lastNewSessionResp = &sessResp
	c.lastUsed = time.Now()
	slog.Info("acp conn: created new session", "clawbench_sid", c.clawbenchSID, "acp_sid", c.acpSID)
	return true, nil
}

// cachedConfigSnapshot holds previously-set config values to re-apply after respawn.
type cachedConfigSnapshot struct {
	mode   string
	model  string
	effort string
}

// snapshotCachedConfig captures current session-level config values before a respawn.
func (c *ACPConn) snapshotCachedConfig() cachedConfigSnapshot {
	return cachedConfigSnapshot{
		mode:   c.currentModeID,
		model:  c.currentModelID,
		effort: c.currentThinkingEffortID,
	}
}

// recoverViaResumeSession recovers a session via ResumeSession and re-applies config.
func (c *ACPConn) recoverViaResumeSession(ctx context.Context, cwd, acpSID string, prevConfig cachedConfigSnapshot) error {
	// Timeout prevents blocking forever if the agent is unresponsive during resume.
	// Use the parent context's deadline if available, otherwise default 60s.
	resumeCtx, resumeCancel := context.WithTimeout(ctx, 60*time.Second)
	defer resumeCancel()

	resumeResp, err := c.conn.ResumeSession(resumeCtx, acp.ResumeSessionRequest{
		SessionId:  acp.SessionId(acpSID),
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		slog.Error("acp conn: ResumeSession failed",
			"clawbench_sid", c.clawbenchSID,
			"acp_sid", acpSID,
			"error", err)
		// Mark connection dead so the next request triggers a fresh spawn.
		// Note: caller (ensureAliveWithSession) already holds c.mu, so no Lock here.
		c.alive = false
		return fmt.Errorf("acp: ResumeSession failed for session %s: %w", acpSID, err)
	}
	c.acpSID = acpSID
	c.lastResumeSessionResp = &resumeResp
	c.lastUsed = time.Now()
	slog.Info("acp conn: recovered session via ResumeSession", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID)

	// Re-apply previously cached config values to the respawned process.
	// ResumeSession reports the agent's defaults, not the user's selections.
	// Since spawnLocked already called resetLastSetConfig(), shouldSetConfig
	// will return true for these values — they won't be dedup-skipped.
	c.reapplyConfigAfterResume(ctx, acpSID, prevConfig)

	return nil // not new — recovered
}

// reapplyConfigAfterResume re-applies cached mode/model/thinking config after a ResumeSession.
func (c *ACPConn) reapplyConfigAfterResume(ctx context.Context, acpSID string, prevConfig cachedConfigSnapshot) {
	c.reapplyConfigOption(ctx, acpSID, "mode", prevConfig.mode)
	c.reapplyConfigOption(ctx, acpSID, "model", prevConfig.model)
	c.reapplyConfigOption(ctx, acpSID, "thinkingEffort", prevConfig.effort)
}

// reapplyConfigOption sets a config option on the resumed session if the value is non-empty
// and the connection is still alive. Called with c.mu held; temporarily unlocks for the RPC.
func (c *ACPConn) reapplyConfigOption(ctx context.Context, acpSID, configID, value string) {
	if value == "" || !c.alive || !c.isAliveLocked() {
		return
	}
	reapplyStart := time.Now()
	slog.Info("acp conn: reapplyConfigOption starting", "config_id", configID, "value", value, "clawbench_sid", c.clawbenchSID)
	c.mu.Unlock()
	c.setSessionConfigOption(ctx, acpSID, configID, value)
	c.mu.Lock()
	slog.Info("acp conn: reapplyConfigOption done", "config_id", configID, "value", value, "clawbench_sid", c.clawbenchSID, "elapsed", time.Since(reapplyStart))
	if c.alive {
		c.markConfigSet(configID, value)
		slog.Info("acp conn: re-applied config after resume", "config_id", configID, "value", value, "clawbench_sid", c.clawbenchSID)
	}
}

// isAliveLocked checks if the connection is still alive (must hold c.mu).
func (c *ACPConn) isAliveLocked() bool {
	if c.conn == nil {
		return false
	}
	select {
	case <-c.conn.Done():
		return false
	default:
		return true
	}
}

// killProcessLocked kills the agent subprocess and waits for it to exit.
// Must be called with c.mu held; temporarily releases c.mu during Wait()
// to avoid blocking if the process is unresponsive.
func (c *ACPConn) killProcessLocked() {
	if c.cmd == nil || c.cmd.Process == nil {
		return
	}
	_ = c.cmd.Process.Kill()
	oldCmd := c.cmd
	c.mu.Unlock()
	_ = oldCmd.Wait()
	c.mu.Lock()
	if c.cmd == oldCmd {
		c.cmd = nil
	}
	c.alive = false
	c.conn = nil
	c.client = nil
	c.acpSID = ""
}

// spawnLocked spawns the agent process and initializes the connection (must hold c.mu).
func (c *ACPConn) spawnLocked(ctx context.Context) error {
	// Kill any existing process first
	if c.cmd != nil && c.cmd.Process != nil {
		// Send ACP Cancel to let the agent stop gracefully before killing
		if c.conn != nil && c.acpSID != "" {
			cancelCtx, cancelCancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = c.conn.Cancel(cancelCtx, acp.CancelNotification{SessionId: acp.SessionId(c.acpSID)})
			cancelCancel()
		}
		_ = c.cmd.Process.Kill()
		// Release the mutex while waiting for the old process to exit,
		// since cmd.Wait() can block if the process is unresponsive.
		// Another goroutine calling GetOrCreateConn during this window
		// will find alive=false and attempt its own spawn — but that's
		// safe because we clear c.cmd below after re-acquiring the lock.
		oldCmd := c.cmd
		c.mu.Unlock()
		_ = oldCmd.Wait()
		c.mu.Lock()
		// Clear the old cmd reference only if it hasn't been replaced
		// by another concurrent spawn (unlikely but defensive).
		if c.cmd == oldCmd {
			c.cmd = nil
		}
	}

	// Reset cached config values — the new process doesn't know about prior settings.
	c.resetLastSetConfig()

	cmdParts := strings.Fields(c.agent.AcpCommand)
	if len(cmdParts) == 0 {
		return fmt.Errorf("acp: no acp_command configured for agent %q", c.agent.ID)
	}

	cmdName := cmdParts[0]
	cmdArgs := cmdParts[1:]

	cmd := exec.CommandContext(context.Background(), cmdName, cmdArgs...)
	cmd.Dir = "" // cwd is per-session, set during NewSession/ResumeSession
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, OrphanChildEnvVar)

	// Add Node.js diagnostic flags for crash investigation.
	// --report-on-fatalerror generates a report on V8 fatal errors;
	// --report-on-signal generates a report on SIGUSR2 (for on-demand diagnostics);
	// --report-directory specifies where reports are written.
	if nodeOpts := os.Getenv("NODE_OPTIONS"); nodeOpts != "" {
		cmd.Env = append(cmd.Env, "NODE_OPTIONS="+nodeOpts+" --report-on-fatalerror --report-on-signal --report-directory=/tmp/node-reports")
	} else {
		cmd.Env = append(cmd.Env, "NODE_OPTIONS=--report-on-fatalerror --report-on-signal --report-directory=/tmp/node-reports")
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("acp: stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("acp: stdout pipe: %w", err)
	}
	cmd.Stderr = &strings.Builder{}

	slog.Info("acp conn: spawning agent process", "agent_id", c.agent.ID, "clawbench_sid", c.clawbenchSID, "command", cmdName, "args", cmdArgs)

	if startErr := cmd.Start(); startErr != nil {
		return fmt.Errorf("acp: start: %w", startErr)
	}

	client := NewClawBenchACPClient()
	client.connRef = c // back-reference for cache updates
	conn := acp.NewClientSideConnection(client, stdinPipe, stdoutPipe)
	conn.SetLogger(slog.Default())

	// Initialize the ACP connection with a timeout so that an unresponsive
	// agent binary doesn't block the entire chat goroutine indefinitely.
	// Use the parent context's deadline if available, otherwise default 60s.
	initCtx, initCancel := context.WithTimeout(ctx, 60*time.Second)
	defer initCancel()

	initResp, err := conn.Initialize(initCtx, acp.InitializeRequest{
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

	slog.Info("acp conn: agent initialized", "agent_id", c.agent.ID, "protocol_version", initResp.ProtocolVersion)

	c.cmd = cmd
	c.conn = conn
	c.client = client
	c.acpSID = "" // cleared on respawn — will be set by ensureAliveWithSession
	c.alive = true
	c.lastUsed = time.Now()
	c.startedAt = time.Now()
	c.cmdWaitOnce = sync.Once{}
	c.cmdWaitState = nil

	go c.watchProcessDeath()
	return nil
}

// watchProcessDeath monitors the ACP connection and marks it as dead
// when the agent process exits or the connection drops.
// Collects crash diagnostics (exit code, signal, stderr, uptime) to help
// diagnose why the agent process died.
func (c *ACPConn) watchProcessDeath() {
	if c.conn == nil {
		return
	}
	<-c.conn.Done()

	c.mu.Lock()
	if c.alive {
		c.alive = false
		// Mark registry capability as stale so the next connection
		// to this agent triggers a ForceUpdate with fresh capabilities.
		if c.agent != nil && c.agent.ID != "" {
			GetAgentCapabilityRegistry().MarkStale(c.agent.ID)
		}
	}
	agentID := ""
	if c.agent != nil {
		agentID = c.agent.ID
	}
	c.mu.Unlock()

	// Collect crash diagnostics outside the lock
	diag := c.collectCrashDiagnostics()

	// Normal exit (exit_code=0, no signal) means the session completed and
	// the connection was closed by CloseConn — not an error.
	if diag.ExitCode == 0 && diag.Signal == "" {
		slog.Info("acp conn: agent process exited",
			"agent_id", agentID,
			"clawbench_sid", c.clawbenchSID,
			"exit_code", diag.ExitCode,
			"uptime", diag.Uptime.Round(time.Second),
		)
	} else {
		slog.Error("acp conn: agent process died",
			"agent_id", agentID,
			"clawbench_sid", c.clawbenchSID,
			"exit_code", diag.ExitCode,
			"signal", diag.Signal,
			"uptime", diag.Uptime.Round(time.Second),
			"ppid", diag.ParentPID,
			"rss_mb", diag.VMRSSKB/1024,
			"fds", diag.FDCount,
			"stderr_tail", diag.StderrTail,
		)
	}

	c.resetLastSetConfig()
}

// GetAndClearNewSessionResp returns the last NewSessionResponse and clears it.
// Used by ExecuteStream to emit session_capture and mode_update events for new sessions.
func (c *ACPConn) GetAndClearNewSessionResp() *acp.NewSessionResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	resp := c.lastNewSessionResp
	c.lastNewSessionResp = nil
	return resp
}

// GetAndClearResumeSessionResp returns the last ResumeSessionResponse and clears it.
// Used by ExecuteStream to update mode/config cache for recovered sessions.
func (c *ACPConn) GetAndClearResumeSessionResp() *acp.ResumeSessionResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	resp := c.lastResumeSessionResp
	c.lastResumeSessionResp = nil
	return resp
}

// GetAndClearSessionResp returns the last NewSessionResponse and clears it.
//
// Deprecated: use GetAndClearNewSessionResp.
func (c *ACPConn) GetAndClearSessionResp() *acp.NewSessionResponse {
	return c.GetAndClearNewSessionResp()
}

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

// Prompt sends a prompt on the ACP session and forwards events to streamCh.
//
//nolint:gocyclo // Prompt has a long switch over ACP response types; the inline branching is clearer than extracting a dispatch table
func (c *ACPConn) Prompt(ctx context.Context, prompt []acp.ContentBlock, streamCh chan<- StreamEvent, req ChatRequest) error {
	// Clear stale plan state from the previous turn — a new prompt starts
	// a fresh execution cycle and the old plan entries are no longer relevant.
	c.mu.Lock()
	c.cachedPlanState = nil
	c.mu.Unlock()

	c.mu.Lock()
	client := c.client
	conn := c.conn
	acpSID := c.acpSID
	c.lastUsed = time.Now()
	c.mu.Unlock()

	if conn == nil || acpSID == "" {
		return fmt.Errorf("acp: connection not initialized")
	}

	// Register the stream channel so SessionUpdate callbacks are forwarded
	if client != nil {
		slog.Info("acp conn: RegisterSession starting", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID)
		client.RegisterSession(acpSID, streamCh)
		defer client.UnregisterSession(acpSID)
	}

	// Set model if configured AND changed since last set (non-fatal).
	// Avoid re-sending unchanged values that may trigger agent-side restarts.
	// If the call kills the connection (agent crashed), abort early —
	// the retry path in ACPBackend.ExecuteStream will handle respawn.
	if req.Model != "" && c.shouldSetConfig("model", req.Model) {
		configStart := time.Now()
		slog.Info("acp conn: set_config_option(model) starting", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "model", req.Model)
		c.setSessionConfigOption(ctx, acpSID, "model", req.Model)
		slog.Info("acp conn: set_config_option(model) done", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "model", req.Model, "elapsed", time.Since(configStart))
		if !c.IsAlive() {
			diag := c.collectCrashDiagnostics()
			slog.Error("acp conn: set_config_option(model) killed connection",
				"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "model", req.Model,
				"exit_code", diag.ExitCode, "signal", diag.Signal,
				"ppid", diag.ParentPID, "rss_mb", diag.VMRSSKB/1024, "fds", diag.FDCount,
				"stderr_tail", diag.StderrTail)
			err := errConfigKilledConnectionWithDiag("model", req.Model, diag)
			return err
		}
		c.markConfigSet("model", req.Model)
	} else if req.Model != "" {
		slog.Debug("acp conn: set_config_option(model) skipped (unchanged)", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "model", req.Model)
	}

	// Set thinking effort if configured AND changed since last set (non-fatal).
	if req.ThinkingEffort != "" && c.shouldSetConfig("thinkingEffort", req.ThinkingEffort) {
		configStart := time.Now()
		slog.Info("acp conn: set_config_option(thinkingEffort) starting", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "thinking_effort", req.ThinkingEffort)
		c.setSessionConfigOption(ctx, acpSID, "thinkingEffort", req.ThinkingEffort)
		slog.Info("acp conn: set_config_option(thinkingEffort) done", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "thinking_effort", req.ThinkingEffort, "elapsed", time.Since(configStart))
		if !c.IsAlive() {
			diag := c.collectCrashDiagnostics()
			slog.Error("acp conn: set_config_option(thinkingEffort) killed connection",
				"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "thinking_effort", req.ThinkingEffort,
				"exit_code", diag.ExitCode, "signal", diag.Signal,
				"ppid", diag.ParentPID, "rss_mb", diag.VMRSSKB/1024, "fds", diag.FDCount,
				"stderr_tail", diag.StderrTail)
			err := errConfigKilledConnectionWithDiag("thinkingEffort", req.ThinkingEffort, diag)
			return err
		}
		c.markConfigSet("thinkingEffort", req.ThinkingEffort)
	} else if req.ThinkingEffort != "" {
		slog.Debug("acp conn: set_config_option(thinkingEffort) skipped (unchanged)", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "thinking_effort", req.ThinkingEffort)
	}

	// Set mode if configured AND changed since last set (non-fatal).
	if req.Mode != "" && c.shouldSetConfig("mode", req.Mode) {
		configStart := time.Now()
		slog.Info("acp conn: set_config_option(mode) starting", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "mode", req.Mode)
		c.setSessionConfigOption(ctx, acpSID, "mode", req.Mode)
		slog.Info("acp conn: set_config_option(mode) done", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "mode", req.Mode, "elapsed", time.Since(configStart))
		if !c.IsAlive() {
			diag := c.collectCrashDiagnostics()
			slog.Error("acp conn: set_config_option(mode) killed connection",
				"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "mode", req.Mode,
				"exit_code", diag.ExitCode, "signal", diag.Signal,
				"ppid", diag.ParentPID, "rss_mb", diag.VMRSSKB/1024, "fds", diag.FDCount,
				"stderr_tail", diag.StderrTail)
			err := errConfigKilledConnectionWithDiag("mode", req.Mode, diag)
			return err
		}
		c.markConfigSet("mode", req.Mode)
		// Update cache so subsequent GET /api/ai/chat returns the correct mode.
		if !c.IsConfigUnsupported("mode") {
			c.UpdateCachedCurrentMode(req.Mode)
		}
	} else if req.Mode != "" {
		slog.Debug("acp conn: set_config_option(mode) skipped (unchanged)", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "mode", req.Mode)
	}

	// Send prompt with a timeout. The Prompt RPC should return quickly (it
	// just delivers the prompt to the agent; content streams back via
	// SessionUpdate notifications). If it blocks, the agent's internal
	// subprocess is likely hung. Use the parent ctx for cancellation (user
	// cancel) but add a deadline as a safety net.
	promptCtx, promptCancel := context.WithTimeout(ctx, 120*time.Second)
	defer promptCancel()

	promptStart := time.Now()
	slog.Info("acp conn: conn.Prompt starting", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID)
	_, err := conn.Prompt(promptCtx, acp.PromptRequest{
		SessionId: acp.SessionId(acpSID),
		Prompt:    prompt,
	})
	slog.Info("acp conn: conn.Prompt done", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "elapsed", time.Since(promptStart), "error", err)
	if err != nil {
		if ctx.Err() != nil {
			slog.Info("acp conn: prompt cancelled", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID)
			// Mark connection as dead so next prompt triggers respawn + ResumeSession
			c.mu.Lock()
			c.alive = false
			c.mu.Unlock()
			return ctx.Err()
		}

		// Peer disconnected — collect crash diagnostics (stderr, exit code)
		// and mark the connection as dead for respawn on retry.
		diag := c.collectCrashDiagnostics()
		c.mu.Lock()
		c.alive = false
		c.mu.Unlock()

		slog.Error("acp conn: prompt failed (peer disconnected)",
			"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID,
			"exit_code", diag.ExitCode, "signal", diag.Signal,
			"ppid", diag.ParentPID, "rss_mb", diag.VMRSSKB/1024, "fds", diag.FDCount,
			"stderr_tail", diag.StderrTail)

		return fmt.Errorf("acp: prompt: %w%s", err, diag.String())
	}

	return nil
}

// crashDiagnostics holds crash info collected after an agent process exits unexpectedly.
type crashDiagnostics struct {
	ExitCode   int
	StderrTail string // last ~2KB of stderr
	WasAlive   bool   // was conn.Done() already closed?
	Uptime     time.Duration
	Signal     string // decoded signal name (e.g., "SIGKILL", "SIGSEGV") if killed by signal
	ParentPID  int    // PPid of the crashed process (from /proc/<pid>/status)
	VMRSSKB    int    // Resident memory at crash time (from /proc/<pid>/status)
	FDCount    int    // Open file descriptors at crash time (from /proc/<pid>/fd)
}

func (d crashDiagnostics) String() string {
	parts := make([]string, 0, 7)
	if d.ExitCode != 0 {
		exitStr := fmt.Sprintf("exit_code=%d", d.ExitCode)
		if sig := d.Signal; sig != "" {
			exitStr += " (" + sig + ")"
		} else if decoded := decodeExitCode(d.ExitCode); decoded != "" {
			exitStr += " (" + decoded + ")"
		}
		parts = append(parts, exitStr)
	}
	if d.Uptime > 0 {
		parts = append(parts, fmt.Sprintf("uptime=%s", d.Uptime.Round(time.Second)))
	}
	if d.ParentPID > 0 {
		parts = append(parts, fmt.Sprintf("ppid=%d", d.ParentPID))
	}
	if d.VMRSSKB > 0 {
		parts = append(parts, fmt.Sprintf("rss=%dMB", d.VMRSSKB/1024))
	}
	if d.FDCount > 0 {
		parts = append(parts, fmt.Sprintf("fds=%d", d.FDCount))
	}
	if d.StderrTail != "" {
		parts = append(parts, fmt.Sprintf("stderr: %s", d.StderrTail))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// decodeExitCode maps common exit codes to human-readable descriptions.
// On Unix, exit codes > 128 indicate the process was killed by signal (128 + signal number).
func decodeExitCode(code int) string {
	switch code {
	case 1:
		return "general error"
	case 126:
		return "permission denied / not executable"
	case 127:
		return "command not found"
	case 128:
		return "invalid exit argument"
	case 129:
		return "SIGHUP"
	case 130:
		return "SIGINT (Ctrl+C)"
	case 137:
		return "SIGKILL (possible OOM killer)"
	case 139:
		return "SIGSEGV (segmentation fault)"
	case 141:
		return "SIGPIPE (broken pipe)"
	case 143:
		return "SIGTERM"
	default:
		if code > 128 {
			sigNum := code - 128
			return fmt.Sprintf("signal %d", sigNum)
		}
		return ""
	}
}

// collectCrashDiagnostics gathers exit code and stderr from the crashed agent process.
// Must be called after Prompt() returns a peer-disconnect error.
func (c *ACPConn) collectCrashDiagnostics() crashDiagnostics {
	var diag crashDiagnostics

	c.mu.Lock()
	cmd := c.cmd
	conn := c.conn
	startedAt := c.startedAt
	c.mu.Unlock()

	// Uptime
	if !startedAt.IsZero() {
		diag.Uptime = time.Since(startedAt)
	}

	// Check if the connection's Done channel is closed (confirming peer disconnect)
	if conn != nil {
		select {
		case <-conn.Done():
			diag.WasAlive = false
		default:
			diag.WasAlive = true
		}
	}

	if cmd == nil || cmd.Process == nil {
		return diag
	}

	// Snapshot /proc/<pid>/status and FD count while the process still exists.
	// This data is only available between the signal and Wait() returning,
	// so we read it before calling Wait() which reaps the process.
	pid := cmd.Process.Pid
	diag.ParentPID, diag.VMRSSKB, _ = readProcStatus(pid)
	if fds, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid)); err == nil {
		diag.FDCount = len(fds)
	}

	// Use cmdWaitOnce to safely call Wait() exactly once, caching the result.
	// This avoids a race between collectCrashDiagnostics and spawnLocked both
	// calling Wait() on the same process.
	c.cmdWaitOnce.Do(func() {
		if state, err := cmd.Process.Wait(); err == nil {
			c.cmdWaitState = state
		}
	})

	if c.cmdWaitState != nil {
		diag.ExitCode = c.cmdWaitState.ExitCode()
		// Check if the process was killed by a signal (Unix-specific)
		if ws, ok := c.cmdWaitState.Sys().(syscall.WaitStatus); ok {
			if ws.Signaled() {
				diag.Signal = ws.Signal().String()
			}
		}
	}

	// Extract stderr from the strings.Builder
	c.mu.Lock()
	if c.cmd != nil {
		if sb, ok := c.cmd.Stderr.(*strings.Builder); ok {
			stderr := sb.String()
			if len(stderr) > 2048 {
				stderr = "..." + stderr[len(stderr)-2048:]
			}
			diag.StderrTail = stderr
		}
	}
	c.mu.Unlock()

	return diag
}

// readProcStatus reads PPid and VmRSS from /proc/<pid>/status.
// Returns (ppid, vmRSSKB, error). Best-effort; returns zeros on failure.
func readProcStatus(pid int) (ppid int, vmRSSKB int, err error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, 0, err
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "PPid:"); ok {
			_, _ = fmt.Sscanf(rest, "%d", &ppid)
		} else if rest, ok := strings.CutPrefix(line, "VmRSS:"); ok {
			_, _ = fmt.Sscanf(rest, "%d", &vmRSSKB)
		}
	}
	return ppid, vmRSSKB, nil
}

// CancelTurn cancels the current in-progress prompt turn.
func (c *ACPConn) CancelTurn(ctx context.Context) {
	c.mu.Lock()
	conn := c.conn
	acpSID := c.acpSID
	c.mu.Unlock()

	if conn != nil && acpSID != "" {
		_ = conn.Cancel(ctx, acp.CancelNotification{SessionId: acp.SessionId(acpSID)})
	}
}

// SetSessionConfigOption sets a config option for this session.
// Also updates cached state so re-emitted SSE events reflect the new value.
func (c *ACPConn) SetSessionConfigOption(ctx context.Context, configID, value string) {
	// Skip redundant RPCs — if the value was already set (e.g., by Prompt()),
	// don't send a duplicate set_config_option that would block and conflict.
	if !c.shouldSetConfig(configID, value) {
		slog.Debug("acp conn: SetSessionConfigOption skipped (unchanged)", "config_id", configID, "value", value, "clawbench_sid", c.clawbenchSID)
		return
	}

	c.mu.Lock()
	acpSID := c.acpSID
	c.mu.Unlock()

	if acpSID == "" {
		slog.Debug("acp conn: SetSessionConfigOption: no session", "clawbench_sid", c.clawbenchSID)
		return
	}

	wasUnsupported := c.IsConfigUnsupported(configID)

	c.setSessionConfigOption(ctx, acpSID, configID, value)

	// Only update cache if the config was not marked as unsupported.
	// If setSessionConfigOption failed with "Unknown config option",
	// the agent didn't actually apply the value — updating cache would
	// cause a mismatch between displayed and actual state.
	nowUnsupported := c.IsConfigUnsupported(configID)

	if nowUnsupported {
		return
	}

	// If the config was previously unsupported but now succeeded (e.g., agent
	// was updated), still update the cache — the value was applied this time.
	_ = wasUnsupported

	switch configID {
	case "mode":
		c.UpdateCachedCurrentMode(value)
		c.markConfigSet("mode", value)
	case "thinking_effort", "thought_level":
		c.UpdateCachedCurrentThinkingEffort(value)
		c.markConfigSet("thinkingEffort", value)
	case "model":
		c.UpdateCachedCurrentModel(value)
		c.markConfigSet("model", value)
	}
}

// setSessionConfigOption sets a config option. Errors are logged but not fatal.
func (c *ACPConn) setSessionConfigOption(ctx context.Context, acpSessionID, configID, value string) {
	c.mu.Lock()
	conn := c.conn
	alive := c.alive && c.isAliveLocked()
	c.mu.Unlock()

	if conn == nil || !alive {
		slog.Debug("acp conn: skipping set_config_option on dead connection", "config_id", configID, "value", value)
		return
	}

	slog.Info("acp conn: sending set_config_option", "config_id", configID, "value", value, "clawbench_sid", c.clawbenchSID, "acp_sid", acpSessionID)

	// Apply a timeout so set_config_option doesn't block forever if the agent
	// is unresponsive (e.g., bridge adapter's internal subprocess crashed).
	// The parent ctx has no deadline (context.WithCancel(context.Background())),
	// so without this timeout a hung agent would block the entire chat goroutine
	// indefinitely — no SSE events, no logs, just a stuck session.
	configCtx, configCancel := context.WithTimeout(ctx, 30*time.Second)
	defer configCancel()

	_, err := conn.SetSessionConfigOption(configCtx, acp.SetSessionConfigOptionRequest{
		ValueId: &acp.SetSessionConfigOptionValueId{
			SessionId: acp.SessionId(acpSessionID),
			ConfigId:  acp.SessionConfigId(configID),
			Value:     acp.SessionConfigValueId(value),
		},
	})
	if err != nil {
		slog.Warn("acp conn: set_config_option failed", "config_id", configID, "value", value, "error", err)
		// If the error indicates the agent doesn't know this config option,
		// mark it as unsupported so we don't retry on subsequent prompts.
		if isUnknownConfigOption(err) {
			c.lastSetConfigMu.Lock()
			if c.unsupportedConfigs == nil {
				c.unsupportedConfigs = make(map[string]bool)
			}
			c.unsupportedConfigs[configID] = true
			c.lastSetConfigMu.Unlock()
			slog.Info("acp conn: marking config as unsupported by agent", "config_id", configID, "value", value)
		}
		// If the error indicates the peer died, mark the connection as dead
		// so the next Prompt() triggers respawn + ResumeSession.
		if isACPPeerDisconnected(err) {
			c.mu.Lock()
			c.alive = false
			c.mu.Unlock()
			slog.Info("acp conn: set_config_option detected peer disconnect, marking dead", "config_id", configID, "value", value)
		}
		// A timeout indicates the agent is unresponsive — mark dead so the
		// retry path in ExecuteStream can respawn + ResumeSession.
		if configCtx.Err() == context.DeadlineExceeded {
			c.mu.Lock()
			c.alive = false
			c.mu.Unlock()
			slog.Warn("acp conn: set_config_option timed out, marking connection dead",
				"config_id", configID, "value", value,
				"clawbench_sid", c.clawbenchSID, "acp_sid", acpSessionID)
		}
	} else {
		slog.Info("acp conn: set_config_option completed", "config_id", configID, "value", value)
	}
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

// close kills the agent process and marks the connection as dead.
func (c *ACPConn) close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
	c.cmd = nil
	c.conn = nil
	c.client = nil
	c.alive = false
	c.acpSID = ""
}

// Close kills the agent process and marks the connection as dead.
// Public alias for close().
func (c *ACPConn) Close() {
	c.close()
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

// KillProcessForTest kills the agent subprocess for integration testing.
// Returns an error if no process is running.
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

// IsConfigUnsupported reports whether the agent has rejected a config ID
// as unknown (e.g., CodeBuddy doesn't support "thinkingEffort").
func (c *ACPConn) IsConfigUnsupported(configID string) bool {
	c.lastSetConfigMu.Lock()
	defer c.lastSetConfigMu.Unlock()
	return c.unsupportedConfigs != nil && c.unsupportedConfigs[configID]
}

// ---------------------------------------------------------------------------
// Session-level state accessors
// ---------------------------------------------------------------------------

// GetCurrentModeID returns the session's current mode ID.
func (c *ACPConn) GetCurrentModeID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentModeID
}

// SetCurrentModeID sets the session's current mode ID and updates the
// agent-level available modes in the registry.
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
// Plan state is transient — not persisted to DB, not debounced.
func (c *ACPConn) SetCachedPlanState(state *PlanState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cachedPlanState = state
}

// GetCachedPlanState returns the cached plan state.
// Returns nil if no plan state has been cached yet.
func (c *ACPConn) GetCachedPlanState() *PlanState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cachedPlanState
}

// shouldSetConfig returns true if the config value has changed since the last
// successful set AND the config is not marked as unsupported by the agent.
func (c *ACPConn) shouldSetConfig(configID, value string) bool {
	c.lastSetConfigMu.Lock()
	defer c.lastSetConfigMu.Unlock()
	// Skip if the agent previously reported this config as unknown
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
// Also clears unsupported config tracking — the new process might support
// previously-unsupported options after an update.
func (c *ACPConn) resetLastSetConfig() {
	c.lastSetConfigMu.Lock()
	defer c.lastSetConfigMu.Unlock()
	c.lastSetModel = ""
	c.lastSetEffort = ""
	c.lastSetMode = ""
	c.unsupportedConfigs = nil
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
// Also derives mode state from config if no v1 Modes were present (ACP v2 agents).
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
		// ACP v2 agents (e.g., OpenCode) expose modes via ConfigOptions instead of
		// the legacy Modes field. If no modes registered yet, derive from config.
		if !GetAgentCapabilityRegistry().HasAvailableModes(agentID) {
			if derived := modeStateFromConfigState(state); derived != nil && len(derived.AvailableModes) > 0 {
				GetAgentCapabilityRegistry().UpdateModes(agentID, derived.AvailableModes)
				// Also set current mode from derived state
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

func (c *ACPConn) UpdateCachedCurrentThinkingEffort(effortID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentThinkingEffortID = effortID
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

// SetEntryForTest injects a connection for testing. Alias for SetConnForTest on manager.
//
// Deprecated: use SetConnForTest.
func (m *ACPConnManager) SetEntryForTest(agentID string, entry *ACPConn) {
	m.SetConnForTest(agentID, entry)
}

// CloseConnection closes and removes the connection for the given key.
//
// Deprecated: use CloseConn.
func (m *ACPConnManager) CloseConnection(key string) {
	m.CloseConn(key)
}

// CloseConnsByAgentID closes all ACP connections for the given agent ID.
// Used when transport is switched from ACP to CLI to ensure stale connections
// are cleaned up immediately rather than waiting for idle timeout.
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

// GetACPSessionID resolves a ClawBench session ID to an ACP session ID.
//
// Deprecated: use conn.AcpSID() directly.
func (m *ACPConnManager) GetACPSessionID(_ string, clawbenchSID string) string {
	m.mu.Lock()
	conn := m.conns[clawbenchSID]
	m.mu.Unlock()

	if conn == nil {
		return ""
	}
	return conn.AcpSID()
}

// GetClientByACPSession returns the ClawBenchACPClient for the connection
// that owns the given ACP session ID.
//
// Deprecated: not needed in one-to-one model.
func (m *ACPConnManager) GetClientByACPSession(acpSessionID string) *ClawBenchACPClient {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, conn := range m.conns {
		conn.mu.Lock()
		if conn.acpSID == acpSessionID {
			client := conn.client
			conn.mu.Unlock()
			return client
		}
		conn.mu.Unlock()
	}
	return nil
}

// GetOrCreateSession returns the ACP session ID for a ClawBench session.
//
// Deprecated: use ensureAliveWithSession or AcpSID() instead.
func (c *ACPConn) GetOrCreateSession(ctx context.Context, clawbenchSID string, cwd string) (string, bool, error) {
	isNew, err := c.ensureAliveWithSession(ctx, cwd)
	if err != nil {
		return "", false, err
	}
	return c.AcpSID(), isNew, nil
}

// GetPendingApprovalSessionIDs returns the set of ClawBench session IDs that
// currently have a pending permission approval request (user needs to click
// allow/deny). Used by the sessions API to annotate sessions with pendingApproval.
func (m *ACPConnManager) GetPendingApprovalSessionIDs() map[string]bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make(map[string]bool)
	for sid, conn := range m.conns {
		// Use TryLock to avoid blocking if ensureAliveWithSession is
		// holding conn.mu. A busy connection won't have pending
		// approvals visible to this read path anyway (they arrive via
		// SSE events, not polled here).
		if !conn.mu.TryLock() {
			continue
		}
		if conn.client != nil {
			if conn.client.mu.TryLock() {
				for _, pp := range conn.client.pendingPermission {
					// pp.SessionID is the ACP session ID; map it to ClawBenchSID
					// Since we're iterating conns keyed by clawbenchSID, use sid directly
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

// GetClient returns the ClawBenchACPClient for the given agent ID.
//
// Deprecated: use GetClientByAgentID instead.
func (m *ACPConnManager) GetClient(agentID string) *ClawBenchACPClient {
	return m.GetClientByAgentID(agentID)
}
