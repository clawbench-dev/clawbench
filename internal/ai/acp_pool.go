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
	// defaultACPStallTimeout is the default no-progress watchdog window for a
	// running ACP prompt. The idle sweep skips actively-running sessions, and
	// conn.Prompt has no hard timeout, so without this a hung agent process
	// (alive but unresponsive) blocks the session forever.
	defaultACPStallTimeout = 30 * time.Minute
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
// Connections that have been idle longer than idleConnTimeout are killed
// (but acpSID is preserved so the session can be recovered via ResumeSession).
// Connections with actively running sessions are skipped.
func (m *ACPConnManager) sweepOnce() {
	var toClose []string

	m.mu.Lock()
	now := time.Now()
	for sid, conn := range m.conns {
		conn.mu.Lock()
		lastActivity := conn.lastActivityNano()
		alive := conn.alive
		conn.mu.Unlock()

		if !alive {
			continue // already dead, will be respawned on next use
		}
		if now.Sub(time.Unix(0, lastActivity)) < idleConnTimeout {
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
		lastActivity := conn.lastActivityNano()
		alive := conn.alive
		conn.mu.Unlock()

		if !alive {
			continue // already dead
		}
		if time.Since(time.Unix(0, lastActivity)) < idleConnTimeout {
			continue // recently used, no longer idle
		}
		if m.isSessionRunning != nil && m.isSessionRunning(sid) {
			continue // session started running since initial scan
		}

		// Kill the idle connection but preserve acpSID so the session
		// can be recovered via ResumeSession on the next prompt.
		// Do NOT delete from pool or call close() (which clears acpSID) —
		// that would cause amnesia if the DB external_session_id was
		// not yet persisted (e.g., session_capture missed).
		m.mu.Lock()
		conn, ok = m.conns[sid]
		m.mu.Unlock()

		if ok {
			slog.Info("acp: idle sweep killing idle connection (preserving acpSID for recovery)",
				"clawbench_sid", sid, "idle_duration", time.Since(time.Unix(0, lastActivity)))
			conn.killAndMarkDead()
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

// NewSessionFallback forces a brand-new ACP session on the given ClawBench
// session's connection, ignoring any prior (unrecoverable) session mapping.
// Used when LoadSession/ResumeSession recovery fails after the agent process
// was killed (e.g. OOM/LMK) — the conversation continues in a fresh session
// rather than hard-failing the request.
//
// It clears acpSID and loadTargetSID so ensureAliveWithSession creates a new
// session instead of retrying the dead one. Returns the conn on success, or
// nil if even a new session could not be established (the pool entry is
// cleaned up so a later request starts fresh).
func (m *ACPConnManager) NewSessionFallback(ctx context.Context, agent *model.Agent, clawbenchSID, cwd string) *ACPConn {
	m.mu.Lock()
	conn, ok := m.conns[clawbenchSID]
	if !ok {
		conn = newACPConn(agent, clawbenchSID)
		m.conns[clawbenchSID] = conn
	}
	m.mu.Unlock()

	// Drop the previous session mapping so ensureAliveWithSession goes down the
	// NewSession branch instead of retrying the unrecoverable session.
	conn.mu.Lock()
	conn.acpSID = ""
	conn.loadTargetSID = ""
	conn.mu.Unlock()

	if _, err := conn.ensureAliveWithSession(ctx, cwd); err != nil {
		slog.Warn("acp: NewSessionFallback failed, cleaning up connection",
			"clawbench_sid", clawbenchSID, "error", err)
		m.mu.Lock()
		if c, exists := m.conns[clawbenchSID]; exists && c == conn {
			delete(m.conns, clawbenchSID)
		}
		m.mu.Unlock()
		return nil
	}
	return conn
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

// DeleteSession best-effort tells the ACP agent to delete the session, then
// closes the connection. Used when a ClawBench session is permanently deleted.
// Failures are logged but not propagated — session deletion on the agent side
// is best-effort and the frontend must not depend on it.
func (m *ACPConnManager) DeleteSession(clawbenchSID string) {
	m.mu.Lock()
	conn, ok := m.conns[clawbenchSID]
	if ok {
		delete(m.conns, clawbenchSID)
	}
	m.mu.Unlock()

	if !ok {
		return
	}
	conn.deleteACPSession()
	conn.close()
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
	Mode          *ModeState
	Config        *ConfigOptionState
	Effort        *ThinkingEffortState
	Commands      []AvailableCommandInfo
	ModelList     *ModelListState
	Plan          *PlanState
	Usage         *UsageState
	ReplayPending bool // true if LoadSession replay is still in progress (async goroutine)
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
	replayPending := conn.loadSessionActive.Load()
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
		Mode:          reg.GetModeState(agentID, currentModeID),
		Effort:        reg.GetThinkingEffortState(agentID, currentThinkingEffortID),
		ModelList:     reg.GetModelListState(agentID, currentModelID),
		Commands:      reg.GetCommands(agentID),
		Config:        reg.GetConfigState(agentID),
		Plan:          planState,
		Usage:         usageState,
		ReplayPending: replayPending,
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
//
// DEADLOCK SAFETY: Methods called from the SDK's processNotifications goroutine
// (via ClawBenchACPClient.SessionUpdate → mapACPSessionUpdate, mergeAndSyncCommands,
// handleConfigOptionSelect, RequestPermission, etc.) MUST NOT acquire c.mu.
// RPC methods like NewSession/ResumeSession hold c.mu while waiting for queued
// notifications to be processed (SDK waitNotificationsUpTo), so acquiring c.mu in
// a notification callback would deadlock.
//
// Safe patterns for notification callbacks:
//   - Read immutable fields (agent, clawbenchSID) directly without locking
//   - Use atomic operations (TouchSessionUpdate, SetToolInFlight)
//   - Use dedicated locks (rawOutputMu, lastSetConfigMu) that don't interact with c.mu
//   - Use ClawBenchACPClient.mu (different lock) for client-internal state
//
// If an RPC must be made while holding c.mu (e.g. reapplyConfigOption), the only
// safe pattern is: unlock → RPC → re-lock. See reapplyConfigOption for an example.
type ACPConn struct {
	// Immutable fields — set once in newACPConn, never modified.
	// Safe to read without c.mu from any goroutine (including notification callbacks).
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

	// lastSessionUpdate is the UnixNano timestamp of the most recent
	// SessionUpdate event received from the agent, updated atomically (no
	// lock) so the SessionUpdate callback can keep an async workflow's
	// connection alive without blocking the ACP notification processing
	// chain. A blocked notification chain would deadlock RPC calls like
	// NewSession, which wait for queued notifications to be processed
	// (see waitNotificationsUpTo in the ACP SDK).
	lastSessionUpdate atomic.Int64

	// toolInFlight is true while the agent is executing a tool call (a
	// tool_use was emitted but no tool_result yet). A no-progress stall
	// watchdog treats an in-flight tool as activity, so long-running
	// legitimate tools (e.g. `sleep`, builds) are never killed.
	toolInFlight atomic.Bool

	// stallTimeout bounds how long a running prompt may go without any
	// SessionUpdate and without an in-flight tool before the connection is
	// terminated. Zero uses defaultACPStallTimeout; negative disables the
	// watchdog. See isStalled / startStallWatchdog.
	stallTimeout time.Duration

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

	// currentSelections is a generalized map for tracking the current
	// selection of any category (mode, thought_level, model, etc.).
	// The legacy fields (currentModeID, currentThinkingEffortID, currentModelID)
	// are kept for backward compatibility and are the canonical source of truth
	// for those well-known categories. This map is used for any additional
	// categories and as a unified access pattern.
	currentSelections map[string]string

	// lastSetConfig tracks the last values successfully sent to the agent via
	// setSessionConfigOption. Used to avoid re-sending unchanged values.
	lastSetConfigMu sync.Mutex
	lastSetModel    string
	lastSetEffort   string
	lastSetMode     string

	// lastSetConfigs is the generalized version of lastSetModel/Effort/Mode.
	// For well-known categories, the legacy fields remain the canonical source.
	// For other categories, this map is used.
	lastSetConfigs map[string]string

	// autoApprove enables hands-off mode: all permission requests are
	// automatically approved with the first allow_* option.
	autoApprove bool

	// promptCancel is called when the agent process dies to unblock any
	// pending conn.Prompt call that would otherwise hang indefinitely.
	promptCancel context.CancelFunc

	// unsupportedConfigs tracks config IDs that the agent reported as unknown.
	unsupportedConfigs map[string]bool

	// listSessionsFn overrides ListSessions for testing. If nil, the real
	// ACP JSON-RPC call is used.
	listSessionsFn func(ctx context.Context, cursor *string) ([]acp.SessionInfo, *string, error)

	// rawOutputBuf accumulates raw ACP JSON-RPC notification payloads for
	// debugging (written to ai_raw_responses on Finalize). This is a separate
	// buffer from the StreamEvent channel so raw_output events don't consume
	// channel buffer space and cause content events to be dropped when the
	// channel is full (previously ~27K drops/day on busy sessions).
	// Protected by rawOutputMu. Cleared at the start of each Prompt call.
	rawOutputMu  sync.Mutex
	rawOutputBuf strings.Builder
}

// AppendRawOutput appends a raw ACP notification payload to the connection's
// raw output buffer. Called from mapACPSessionUpdate (on the ACP SDK's
// notification goroutine) instead of sending a raw_output StreamEvent through
// the channel, which would consume channel buffer space and cause content
// events to be dropped when the channel is full.
func (c *ACPConn) AppendRawOutput(rawJSON string) {
	c.rawOutputMu.Lock()
	if c.rawOutputBuf.Len() > 0 {
		c.rawOutputBuf.WriteByte('\n')
	}
	c.rawOutputBuf.WriteString(rawJSON)
	c.rawOutputMu.Unlock()
}

// ResetRawOutput clears the raw output buffer and returns the accumulated
// content. Called at the start of each Prompt to reset the buffer, and after
// Prompt returns to collect the raw output for the completed turn.
func (c *ACPConn) ResetRawOutput() string {
	c.rawOutputMu.Lock()
	s := c.rawOutputBuf.String()
	c.rawOutputBuf.Reset()
	c.rawOutputMu.Unlock()
	return s
}

// TouchSessionUpdate records the current time as the connection's most recent
// SessionUpdate activity. It is lock-free (atomic store) so it can be called
// from the ACP notification processing goroutine without risking a deadlock:
// RPCs like NewSession hold c.mu while waiting for queued notifications to be
// processed, so if this callback took c.mu it would block notification
// processing and stall the RPC until timeout.
func (c *ACPConn) TouchSessionUpdate() {
	c.lastSessionUpdate.Store(time.Now().UnixNano())
}

// lastActivityNano returns the later of lastUsed and lastSessionUpdate as a
// UnixNano timestamp, representing the last time the connection did any work
// (either an explicit use or an incoming SessionUpdate from an async workflow).
// Must be called without holding c.mu.
func (c *ACPConn) lastActivityNano() int64 {
	lastUsedNano := c.lastUsed.UnixNano()
	if su := c.lastSessionUpdate.Load(); su > lastUsedNano {
		return su
	}
	return lastUsedNano
}

// stallTimeout returns the effective no-progress watchdog window.
// 0 → defaultACPStallTimeout, negative → disabled (0).
func (c *ACPConn) effectiveStallTimeout() time.Duration {
	switch {
	case c.stallTimeout == 0:
		return defaultACPStallTimeout
	case c.stallTimeout < 0:
		return 0
	default:
		return c.stallTimeout
	}
}

// SetToolInFlight records whether the agent is currently executing a tool call.
// Set true when a tool_use is emitted and false when its tool_result arrives,
// so the stall watchdog treats long-running tools as active.
func (c *ACPConn) SetToolInFlight(inFlight bool) {
	c.toolInFlight.Store(inFlight)
}

// isStalled reports whether the running prompt has made no progress for the
// given window. Progress is defined as any incoming SessionUpdate OR an
// in-flight tool call. A zero window disables the check.
func (c *ACPConn) isStalled(timeout time.Duration) bool {
	if timeout <= 0 {
		return false
	}
	if c.toolInFlight.Load() {
		return false
	}
	last := c.lastSessionUpdate.Load()
	if last == 0 {
		last = c.lastUsed.UnixNano()
	}
	if last == 0 {
		// No activity recorded yet — treat as not stalled so a freshly
		// started prompt is never killed immediately.
		return false
	}
	return time.Since(time.Unix(0, last)) > timeout
}

// startStallWatchdog starts a goroutine that calls onStall once the running
// prompt has made no progress for the stall window. It stops when ctx is done
// or when the returned stop func is called. A disabled or zero timeout starts
// no goroutine and returns a no-op stop func.
func (c *ACPConn) startStallWatchdog(ctx context.Context, onStall func()) func() {
	timeout := c.effectiveStallTimeout()
	if timeout <= 0 {
		return func() {}
	}
	interval := timeout / 10
	if interval < 100*time.Millisecond {
		interval = 100 * time.Millisecond
	}
	stop := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				if c.isStalled(timeout) {
					slog.Warn("acp: prompt stalled (no progress for "+timeout.String()+"), terminating connection",
						"clawbench_sid", c.clawbenchSID)
					once.Do(onStall)
					return
				}
			}
		}
	}()
	return func() {
		once.Do(func() { close(stop) })
	}
}

// Cwd returns the project working directory for this connection,
// set on the first ensureAliveWithSession call. Used by CreateTerminal
// as a fallback when the ACP agent omits Cwd in terminal/create requests.
func (c *ACPConn) Cwd() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cwd
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
// c.agent is set once at construction and never mutated, so no lock needed.
// This must NOT acquire c.mu — it is called from the SDK's processNotifications
// goroutine (via ClawBenchACPClient.SessionUpdate → mergeAndSyncCommands →
// connRef.AgentID()), and ensureAliveWithSession holds c.mu during ResumeSession
// and other RPCs. Acquiring c.mu here would deadlock.
func (c *ACPConn) AgentID() string {
	if c.agent != nil {
		return c.agent.ID
	}
	return ""
}

// BackendID returns the backend identifier of the agent this connection belongs to.
// Used for ACP event mapping to look up backend-specific tool name and input remap tables.
// c.agent is set once at construction and never mutated, so no lock needed.
// This must NOT acquire c.mu — same deadlock risk as AgentID (called from
// mapACPSessionUpdate during notification processing while c.mu is held).
func (c *ACPConn) BackendID() string {
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

// markDeadIfCurrent marks the connection dead (alive=false) only if conn is
// still the current active connection. This guards against a stale goroutine
// (a prompt whose connection was superseded by a respawn) clobbering the alive
// flag of a freshly respawned connection. Without this guard, the old prompt's
// error path would set alive=false after spawnLocked had already set the new
// connection's alive=true, causing the next prompt to treat a healthy process
// as dead and hang in collectCrashDiagnostics.
func (c *ACPConn) markDeadIfCurrent(conn *acp.ClientSideConnection) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == conn {
		c.alive = false
	}
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

// SyncLoadSession 强制对连接触发一次 LoadSession 回放，即使连接已存活。
// ensureAliveWithSession 对"已存活+有 acpSID"的连接会提前返回，因此同步场景
// 必须显式回放以获取外部最新历史。回放通知被收集到 load 缓冲供调用方持久化。
// 若 loadSessionActive 已为 true（GetOrCreateConnForLoad 刚在全新连接上触发过
// LoadSession），则跳过，避免重复回放。
func (c *ACPConn) SyncLoadSession(ctx context.Context, cwd, acpSID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loadSessionActive.Load() {
		return nil
	}
	c.loadSessionActive.Store(true)
	loadCtx, loadCancel := context.WithTimeout(ctx, 60*time.Second)
	defer loadCancel()
	loadResp, err := c.conn.LoadSession(loadCtx, acp.LoadSessionRequest{
		SessionId:  acp.SessionId(acpSID),
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		c.alive = false
		c.loadSessionActive.Store(false)
		return fmt.Errorf("acp: session/load: %w", err)
	}
	c.acpSID = acpSID
	c.lastLoadSessionResp = &loadResp
	c.lastUsed = time.Now()
	slog.Info("acp conn: SyncLoadSession replay completed", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID)
	return nil
}

// SetLoadSessionActiveForTest 设置 loadSessionActive，用于测试跳过真实 RPC。
func (c *ACPConn) SetLoadSessionActiveForTest(v bool) {
	c.loadSessionActive.Store(v)
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

// ---------------------------------------------------------------------------
// Generalized selection accessors — unified pattern for mode/thought_level/model
// ---------------------------------------------------------------------------

// categoryToField maps generalized categories to their legacy field accessors.
// For well-known categories, the legacy fields remain the canonical source.

// UpdateCachedCurrent updates the current selection for the given category.
// For well-known categories ("mode", "thought_level", "model"), this delegates
// to the existing legacy field for backward compatibility. For other categories,
// it uses the currentSelections map.
func (c *ACPConn) UpdateCachedCurrent(category, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch category {
	case "mode":
		c.currentModeID = value
	case "thought_level":
		c.currentThinkingEffortID = value
	case "model":
		c.currentModelID = value
	default:
		if c.currentSelections == nil {
			c.currentSelections = make(map[string]string)
		}
		c.currentSelections[category] = value
	}
}

// GetCurrentSelection returns the current selection value for the given category.
// For well-known categories, it reads from the legacy field. For other categories,
// it reads from the currentSelections map.
func (c *ACPConn) GetCurrentSelection(category string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch category {
	case "mode":
		return c.currentModeID
	case "thought_level":
		return c.currentThinkingEffortID
	case "model":
		return c.currentModelID
	default:
		if c.currentSelections == nil {
			return ""
		}
		return c.currentSelections[category]
	}
}

// HasCurrentChanged checks if the given value differs from the session's current
// selection for the specified category.
func (c *ACPConn) HasCurrentChanged(category, value string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	var current string
	switch category {
	case "mode":
		current = c.currentModeID
	case "thought_level":
		current = c.currentThinkingEffortID
	case "model":
		current = c.currentModelID
	default:
		if c.currentSelections != nil {
			current = c.currentSelections[category]
		}
	}
	return current != value
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
	default:
		if c.lastSetConfigs == nil {
			return true // No previous value recorded
		}
		return c.lastSetConfigs[configID] != value
	}
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
	default:
		if c.lastSetConfigs == nil {
			c.lastSetConfigs = make(map[string]string)
		}
		c.lastSetConfigs[configID] = value
	}
}

// resetLastSetConfig clears cached config values (called on respawn).
func (c *ACPConn) resetLastSetConfig() {
	c.lastSetConfigMu.Lock()
	defer c.lastSetConfigMu.Unlock()
	c.lastSetModel = ""
	c.lastSetEffort = ""
	c.lastSetMode = ""
	c.lastSetConfigs = nil
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
// before WS events are emitted, so the frontend sees the user's requested value
// (e.g. "plan") instead of the agent's default (e.g. "bypassPermissions").
// The actual RPC is still done inside Prompt(); this only affects WS display.
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

// HasCurrentThinkingEffortChanged checks if the given effortId differs from the session's current thinking effort.
func (c *ACPConn) HasCurrentThinkingEffortChanged(effortID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentThinkingEffortID != effortID
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

// killAndMarkDead kills the agent process and marks the connection as dead,
// but preserves acpSID so ensureAliveWithSession can recover the session via
// LoadSession/ResumeSession on the next prompt. Used by the stall watchdog
// which kills a stuck process but must not cause amnesia on recovery.
// Must NOT be called with c.mu held — use killAndMarkDeadLocked instead.
func (c *ACPConn) killAndMarkDead() {
	c.mu.Lock()
	c.killAndMarkDeadLocked()
	c.mu.Unlock()
}

// killAndMarkDeadLocked kills the agent process and marks the connection as dead,
// preserving acpSID for future ResumeSession recovery.
// Must be called with c.mu held; temporarily releases c.mu during Wait().
func (c *ACPConn) killAndMarkDeadLocked() {
	if c.cmd != nil && c.cmd.Process != nil {
		if c.stdoutFilter != nil {
			c.stdoutFilter.Close()
			c.stdoutFilter = nil
		}
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
	// Intentionally preserve c.acpSID — ensureAliveWithSession needs it
	// to recover the session via LoadSession/ResumeSession after respawn.
	c.resetLastSetConfig()
}

// close kills the agent process and marks the connection as dead.
// Unlike killAndMarkDead, this clears acpSID because callers (idle sweep,
// pool teardown, RemoveConn) permanently discard the connection.
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

// deleteACPSession best-effort tells the ACP agent to delete this session via
// session/delete (unstable capability). It only runs when the connection is
// alive, has a known ACP session ID, and the agent advertises the delete
// capability. Failures are logged, never propagated — the caller must not rely
// on the result.
func (c *ACPConn) deleteACPSession() {
	c.mu.Lock()
	conn := c.conn
	acpSID := c.acpSID
	alive := c.alive && c.isAliveLocked()
	agentID := ""
	if c.agent != nil {
		agentID = c.agent.ID
	}
	c.mu.Unlock()

	if conn == nil || acpSID == "" || !alive {
		slog.Debug("acp: skip session/delete, connection not usable",
			"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "alive", alive)
		return
	}

	if !GetAgentCapabilityRegistry().GetDeleteSession(agentID) {
		slog.Debug("acp: skip session/delete, agent does not advertise delete capability",
			"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "agent_id", agentID)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := conn.UnstableDeleteSession(ctx, acp.UnstableDeleteSessionRequest{
		SessionId: acp.SessionId(acpSID),
	})
	if err != nil {
		slog.Warn("acp: session/delete failed (best-effort, ignored)",
			"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "error", err)
		return
	}
	slog.Info("acp: session/delete succeeded",
		"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID)
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
