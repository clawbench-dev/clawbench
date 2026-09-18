package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// ACPConn lifecycle — spawn, ensure alive, resume, session creation
// ---------------------------------------------------------------------------

// advertiseTerminalCapability returns whether ClawBench should advertise the
// Terminal=true client capability for the given agent in ACP Initialize.
//
// Background: CodeBuddy's run_in_background Bash tool relies on the agent's
// OWN background-task registry (TaskOutput/TaskStop/TaskList). When ClawBench
// advertises Terminal=true, CodeBuddy delegates background commands to the
// host via terminal/* RPCs — but then CodeBuddy's TaskOutput tool can't find
// the host-created "term-N" task, producing "Background task not found" on
// every query. Hiding the Terminal capability makes CodeBuddy run background
// commands internally, keeping its task registry consistent.
//
// Other agents (Claude, OpenCode, etc.) either have their own task management
// decoupled from the host terminals or no background-task feature, so the
// capability is kept enabled for them.
//
// Tests can override via SetAdvertiseTerminalForTest (the override takes
// precedence over the per-agent default).

var (
	advertiseTerminal         = true
	advertiseTerminalOverride *bool
)

// advertiseTerminalCapability returns the current Terminal capability value.
func advertiseTerminalCapability(agent *model.Agent) bool {
	if advertiseTerminalOverride != nil {
		return *advertiseTerminalOverride
	}
	if agent != nil && isCodeBuddyBackend(agent) {
		// CodeBuddy's background-task registry (TaskOutput/TaskStop) is not
		// compatible with host-managed terminals: its tools query an internal
		// registry that never sees host-created "term-N" tasks, so every
		// TaskOutput fails with "Background task not found". Hide the
		// capability so CodeBuddy manages background commands internally.
		return false
	}
	return advertiseTerminal
}

// SetAdvertiseTerminalForTest overrides the Terminal capability for tests.
// Production code must not use this.
func SetAdvertiseTerminalForTest(enabled bool) {
	advertiseTerminalOverride = &enabled
}

// ResetAdvertiseTerminalForTest clears the test override.
// Production code must not use this.
func ResetAdvertiseTerminalForTest() {
	advertiseTerminalOverride = nil
}

// EnsureAlive ensures the connection has a live agent process and initialized
// ACP connection, but does NOT create/resume a session. Used by ListSessions
// which needs an alive connection but no session.
func (c *ACPConn) EnsureAlive(ctx context.Context, cwd string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.alive && c.isAliveLocked() {
		c.lastUsed = time.Now()
		return nil
	}

	return c.spawnLocked(ctx)
}

// ListSessions calls the ACP ListSessions RPC on this connection's client.
func (c *ACPConn) ListSessions(ctx context.Context, cursor *string) ([]acp.SessionInfo, *string, error) {
	c.mu.Lock()
	if !c.alive || c.conn == nil {
		c.mu.Unlock()
		return nil, nil, fmt.Errorf("acp: connection not alive for ListSessions")
	}
	conn := c.conn
	fn := c.listSessionsFn
	c.mu.Unlock()

	// Use test override if set
	if fn != nil {
		return fn(ctx, cursor)
	}

	req := acp.ListSessionsRequest{}
	if cursor != nil {
		req.Cursor = cursor
	}
	resp, err := conn.ListSessions(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("acp: ListSessions: %w", err)
	}
	return resp.Sessions, resp.NextCursor, nil
}

// ensureAliveWithSession ensures the connection is alive and has a valid ACP session.
// If the process is dead, it respawns and tries ResumeSession recovery, falling back to NewSession.
// Returns isNew=true if a new ACP session was created, false if reusing or recovered.
func (c *ACPConn) ensureAliveWithSession(ctx context.Context, cwd string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Set cwd on first call — used by spawnLocked to set cmd.Dir so the ACP
	// process starts in the correct project directory instead of inheriting
	// the ClawBench server's cwd.
	if c.cwd == "" && cwd != "" {
		c.cwd = cwd
		slog.Info("acp conn: cwd locked on first call",
			slog.String("clawbench_sid", c.clawbenchSID),
			slog.String("cwd", cwd))
	}
	if c.cwd != cwd && cwd != "" {
		slog.Warn("acp conn: cwd mismatch — cwd is already locked, ignoring new value",
			slog.String("clawbench_sid", c.clawbenchSID),
			slog.String("locked_cwd", c.cwd),
			slog.String("requested_cwd", cwd))
	}

	// If alive and already has a session, reuse
	if c.alive && c.isAliveLocked() && c.acpSID != "" {
		slog.Debug("acp conn: reusing existing connection", "clawbench_sid", c.clawbenchSID, "acp_sid", c.acpSID)
		c.lastUsed = time.Now()
		return false, nil
	}

	// Snapshot cached config state before spawn
	prevConfig := c.snapshotCachedConfig()

	// Save acpSID before spawnLocked clears it
	preSpawnAcpSID := c.acpSID

	// Need to spawn or respawn
	spawnStart := time.Now()
	if err := c.spawnLocked(ctx); err != nil {
		return false, err
	}
	slog.Info("acp perf: ensureAliveWithSession.spawnLocked", "clawbench_sid", c.clawbenchSID, "elapsed", time.Since(spawnStart))

	// LoadSession branch — explicit load request (acp-load endpoint).
	// drainReplay=false: the caller (ServeACPLoadSession) reads the buffered
	// SessionUpdate notifications to replay the conversation into the DB, so the
	// buffer must be preserved.
	if c.loadTargetSID != "" {
		loadSID := c.loadTargetSID
		c.loadTargetSID = "" // clear to prevent reuse on next call
		return c.recoverViaLoadSession(ctx, cwd, loadSID, false)
	}

	// Recover a previous session after the process died.
	// Check both in-memory acpSID (saved before spawnLocked cleared it) and
	// the DB (external_session_id). The DB fallback handles cases where the
	// in-memory acpSID was lost — e.g., after ResumeSession failure called
	// killProcessLocked (which clears acpSID), or after idle sweep called
	// close() (which also clears acpSID and removes from pool, but the new
	// conn's GetOrCreateConn may not pre-populate if the DB write was delayed).
	acpSID := preSpawnAcpSID
	if acpSID == "" {
		if extID := getExternalSessionID(c.clawbenchSID); extID != "" {
			acpSID = extID
			slog.Info("acp conn: recovered acpSID from DB (in-memory was empty)",
				"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID)
		}
	}

	if acpSID != "" {
		// Always use ResumeSession for automatic recovery after process death.
		// LoadSession replays the entire conversation history, which is very slow
		// for long conversations and can exceed the 60s timeout, producing
		// "acp: session/load: context deadline exceeded". ResumeSession only
		// re-attaches to the existing session state without replaying, so it's
		// much faster and more reliable.
		// LoadSession is still used by the explicit acp-load endpoint
		// (loadTargetSID branch above) where the replay is intentional.
		slog.Info("acp conn: recovering previous session via ResumeSession",
			"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID)
		err := c.recoverViaResumeSession(ctx, cwd, acpSID, prevConfig)
		if err == nil {
			return false, nil // recovered successfully
		}
		// ResumeSession failed — the session is unrecoverable.
		// Do NOT silently fall back to NewSession (amnesia): the user
		// would lose all conversation context without any indication.
		// Surface the error so the user knows the session needs a fresh start.
		// Use killAndMarkDeadLocked (not killProcessLocked) so acpSID is preserved —
		// a future prompt can retry ResumeSession instead of becoming
		// permanently unrecoverable.
		slog.Error("acp conn: ResumeSession failed, session is unrecoverable",
			"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "error", err)
		c.killAndMarkDeadLocked()
		return false, fmt.Errorf("acp: session %s ResumeSession failed: %w", acpSID, err)
	}

	// No prior session — create new session.
	newSessCtx, newSessCancel := context.WithTimeout(ctx, 30*time.Second)
	defer newSessCancel()

	newSessStart := time.Now()
	slog.Info("acp conn: calling NewSession with cwd",
		slog.String("clawbench_sid", c.clawbenchSID),
		slog.String("cwd", cwd),
		slog.String("c.cwd", c.cwd))
	sessResp, err := c.conn.NewSession(newSessCtx, acp.NewSessionRequest{
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
	})
	slog.Info("acp perf: ensureAliveWithSession.NewSession", "clawbench_sid", c.clawbenchSID, "elapsed", time.Since(newSessStart), "error", err)
	if err != nil {
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
// Takes stateMu, not c.mu: the caller (ensureAliveWithSession) already holds c.mu,
// and stateMu is a leaf lock so acquiring it there is safe.
func (c *ACPConn) snapshotCachedConfig() cachedConfigSnapshot {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return cachedConfigSnapshot{
		mode:   c.currentModeID,
		model:  c.currentModelID,
		effort: c.currentThinkingEffortID,
	}
}

// recoverViaLoadSession recovers a session via LoadSession and returns
// isNew=true (the session was re-established on a fresh process).
// Used only by explicit endpoints (acp-load, acp-sync), not automatic recovery.
//
// drainReplay controls whether the LoadSession replay buffer is drained:
//   - true  (acp-sync endpoint): the replayed messages are already persisted
//     in ClawBench's DB, so they must be drained so they don't leak into the
//     live stream (which would double-display them).
//   - false (acp-load endpoint): the caller (ServeACPLoadSession) reads the
//     buffered SessionUpdate notifications to persist the replay into the DB,
//     so the buffer must be preserved.
func (c *ACPConn) recoverViaLoadSession(ctx context.Context, cwd, loadSID string, drainReplay bool) (bool, error) {
	loadCtx, loadCancel := context.WithTimeout(ctx, 60*time.Second)
	defer loadCancel()

	c.loadSessionActive.Store(true)
	loadStart := time.Now()
	loadResp, err := c.conn.LoadSession(loadCtx, acp.LoadSessionRequest{
		SessionId:  acp.SessionId(loadSID),
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
	})
	slog.Info("acp perf: ensureAliveWithSession.LoadSession", "clawbench_sid", c.clawbenchSID, "acp_sid", loadSID, "elapsed", time.Since(loadStart), "error", err)

	if err != nil {
		c.alive = false
		c.loadSessionActive.Store(false)
		return false, fmt.Errorf("acp: session/load: %w", err)
	}

	c.acpSID = loadSID
	c.lastLoadSessionResp = &loadResp
	c.lastUsed = time.Now()

	// For automatic recovery (drainReplay=true): clear the active flag and drain
	// the replay buffer. The replayed messages are already persisted in
	// ClawBench's DB, so they must not be routed to the live stream. Leaving
	// loadSessionActive set would cause all subsequent SessionUpdate
	// notifications (including the new prompt's output) to be swallowed into the
	// buffer instead of reaching the stream, hanging the conversation.
	//
	// For the explicit acp-load path (drainReplay=false): keep loadSessionActive
	// set and the buffer intact. The caller (ServeACPLoadSession) waits a short
	// delay for late-arriving replay notifications to accumulate in the buffer,
	// then clears the flag itself and persists the replay to the DB.
	if drainReplay {
		if c.client != nil {
			c.client.GetAndClearLoadSessionBuf()
		}
		c.loadSessionActive.Store(false)
	}

	slog.Info("acp conn: loaded session via LoadSession", "clawbench_sid", c.clawbenchSID, "acp_sid", loadSID)
	return true, nil
}

// recoverViaResumeSession recovers a session via ResumeSession and re-applies config.
func (c *ACPConn) recoverViaResumeSession(ctx context.Context, cwd, acpSID string, prevConfig cachedConfigSnapshot) error {
	resumeCtx, resumeCancel := context.WithTimeout(ctx, 30*time.Second)
	defer resumeCancel()

	resumeStart := time.Now()
	slog.Info("acp conn: calling ResumeSession with cwd",
		slog.String("clawbench_sid", c.clawbenchSID),
		slog.String("acp_sid", acpSID),
		slog.String("cwd", cwd),
		slog.String("c.cwd", c.cwd))
	resumeResp, err := c.conn.ResumeSession(resumeCtx, acp.ResumeSessionRequest{
		SessionId:  acp.SessionId(acpSID),
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
	})
	slog.Info("acp perf: recoverViaResumeSession.ResumeSession", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "elapsed", time.Since(resumeStart), "error", err)
	if err != nil {
		slog.Error("acp conn: ResumeSession failed",
			"clawbench_sid", c.clawbenchSID,
			"acp_sid", acpSID,
			"error", err)
		c.alive = false
		return fmt.Errorf("acp: ResumeSession failed for session %s: %w", acpSID, err)
	}
	c.acpSID = acpSID
	c.lastResumeSessionResp = &resumeResp
	c.lastUsed = time.Now()
	slog.Info("acp conn: recovered session via ResumeSession", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID)

	c.reapplyConfigAfterResume(ctx, acpSID, prevConfig)

	return nil
}

// reapplyConfigAfterResume re-applies cached mode/model/thinking config after a ResumeSession.
// Config ids are resolved to what the agent advertised (resolveWireConfigIDLocked —
// c.mu is held on this path via ensureAliveWithSession), falling back to the
// historical hardcoded ids.
func (c *ACPConn) reapplyConfigAfterResume(ctx context.Context, acpSID string, prevConfig cachedConfigSnapshot) {
	reapplyStart := time.Now()
	c.reapplyConfigOption(ctx, acpSID, c.resolveWireConfigIDLocked("mode", "mode"), prevConfig.mode)
	c.reapplyConfigOption(ctx, acpSID, c.resolveWireConfigIDLocked("model", "model"), prevConfig.model)
	c.reapplyConfigOption(ctx, acpSID, c.resolveWireConfigIDLocked("thought_level", "thinkingEffort"), prevConfig.effort)
	slog.Info("acp perf: reapplyConfigAfterResume.total", "clawbench_sid", c.clawbenchSID, "elapsed", time.Since(reapplyStart),
		"mode", prevConfig.mode, "model", prevConfig.model, "effort", prevConfig.effort)
}

// reapplyConfigOption sets a config option on the resumed session if the value is non-empty
// and the connection is still alive. Called with c.mu held; temporarily unlocks for the RPC.
//
// This unlock→RPC→re-lock pattern is the ONLY safe way to make an RPC while holding c.mu.
// The SDK's SendRequest (used by all RPC methods) calls waitNotificationsUpTo after
// receiving the response, which waits for the processNotifications goroutine to finish
// processing queued notifications. If c.mu were held during the RPC, any notification
// callback that tries to acquire c.mu would deadlock, preventing waitNotificationsUpTo
// from completing.
func (c *ACPConn) reapplyConfigOption(ctx context.Context, acpSID, configID, value string) {
	if value == "" || !c.alive || !c.isAliveLocked() {
		return
	}
	// Never re-send a config the agent already rejected as unknown
	// (e.g. thinkingEffort) — it would only produce a failing RPC on every resume.
	if c.IsConfigUnsupported(configID) {
		slog.Debug("acp conn: skipping reapply of unsupported config", "config_id", configID, "value", value, "clawbench_sid", c.clawbenchSID)
		return
	}
	reapplyStart := time.Now()
	slog.Info("acp conn: reapplyConfigOption starting", "config_id", configID, "value", value, "clawbench_sid", c.clawbenchSID)
	c.mu.Unlock()
	c.setSessionConfigOption(ctx, acpSID, configID, value)
	c.mu.Lock()
	slog.Info("acp conn: reapplyConfigOption done", "config_id", configID, "value", value, "clawbench_sid", c.clawbenchSID, "elapsed", time.Since(reapplyStart))
	if c.alive {
		// Record the value even when the agent rejected it: the cached value is
		// the one the agent is known to hold, so shouldSetConfig dedups until it
		// actually changes.
		c.markConfigSet(configID, value)
		slog.Info("acp conn: re-applied config after resume", "config_id", configID, "value", value, "clawbench_sid", c.clawbenchSID)
	}
}

// isAliveLocked checks if the connection is still alive (must hold c.mu).
//
// Two independent conditions, because either can fail on its own:
//
//   - The SDK connection is not done. This catches a closed pipe.
//   - The OS still has the process. This catches a process that exited while
//     its pipe stayed open, so the SDK never observes EOF and c.conn.Done()
//     never fires — typically because an orphaned grandchild (e.g. an MCP
//     server spawned by the agent) inherited the write end of stdout/stderr.
//     Without this check such a connection is reused indefinitely and every
//     prompt fails against a process that no longer exists.
//
// Limits worth knowing before extending this:
//
//   - A zombie (exited, not yet reaped) still answers signal 0, and a reused
//     PID would look alive. Neither is distinguishable from a live process by
//     PID alone; the check is a cheap improvement, not a guarantee.
//   - It does NOT detect a wedged-but-running agent (event loop stopped,
//     process alive). That is the shape of the incident this area was
//     investigated for, and it is handled after the fact by the empty-turn
//     detection in acp_conn_prompt.go, which respawns the agent.
//
// Deliberately NOT an idle/heartbeat timeout. An agent legitimately goes quiet
// for minutes while a long tool runs, and Prompt must never be given a timeout
// (see acp_pool.go) — a quiet-but-healthy agent must stay reusable.
func (c *ACPConn) isAliveLocked() bool {
	if c.conn == nil {
		return false
	}
	select {
	case <-c.conn.Done():
		return false
	default:
	}
	if c.cmd != nil && c.cmd.Process != nil && !agentProcessAlive(c.cmd.Process.Pid) {
		slog.Warn("acp conn: agent process no longer exists, treating connection as dead",
			"clawbench_sid", c.clawbenchSID, "pid", c.cmd.Process.Pid)
		return false
	}
	return true
}

// killProcessLocked kills the agent subprocess and waits for it to exit.
// Must be called with c.mu held; temporarily releases c.mu during Wait().
func (c *ACPConn) killProcessLocked() {
	if c.cmd == nil || c.cmd.Process == nil {
		return
	}

	oldCmd := c.cmd
	oldFilter := c.stdoutFilter
	c.stdoutFilter = nil
	c.stdin = nil
	c.rawRPC = nil
	c.mu.Unlock()
	c.reapProcess(oldCmd, oldFilter)
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
//
// On failure the connection is left in a clean, explicitly-dead state via
// markSpawnFailedLocked (see the deferred cleanup below) so a later
// ensureAliveWithSession cannot mistake it for a reusable connection.
//
//nolint:gocyclo,gocognit // complex spawn logic with multiple sequential setup steps; the failure-cleanup defer is part of that sequence
func (c *ACPConn) spawnLocked(ctx context.Context) (err error) {
	// A failed spawn must not leave the connection looking alive. The
	// assignments that would normally clear this state (c.cmd/c.conn/c.client/
	// c.alive/c.startedAt) all sit AFTER Initialize succeeds, so without this
	// defer every early return below would leave the PREVIOUS connection's
	// fields in place — while the kill-old-process step above had already
	// cleared c.cmd. Because isAliveLocked() skips its process probe when c.cmd
	// is nil, such a connection reported alive if its Done() had not fired yet,
	// and the next ensureAliveWithSession reused it instead of respawning.
	//
	// A defer (rather than cleanup at each return site) keeps this true for
	// future early returns added to this function.
	defer func() {
		if err != nil {
			c.markSpawnFailedLocked()
		}
	}()

	// Kill any existing process first
	if c.cmd != nil && c.cmd.Process != nil {
		killStart := time.Now()
		if c.conn != nil && c.acpSID != "" {
			cancelCtx, cancelCancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = c.conn.Cancel(cancelCtx, acp.CancelNotification{SessionId: acp.SessionId(c.acpSID)})
			cancelCancel()
		}
		oldCmd := c.cmd
		oldFilter := c.stdoutFilter
		c.stdoutFilter = nil
		c.stdin = nil
		c.rawRPC = nil
		c.mu.Unlock()
		c.reapProcess(oldCmd, oldFilter)
		c.mu.Lock()
		slog.Info("acp perf: spawnLocked.kill_old_process", "clawbench_sid", c.clawbenchSID, "elapsed", time.Since(killStart))
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

	// Workaround for https://github.com/clawbench-dev/clawbench/issues/270:
	// When Codebuddy starts with --acp, it sets strictDynamic=true which causes
	// McpConfigManager.shouldLoadFromFilesystem() to return false for ALL
	// filesystem scopes (user/project/local), resulting in 0 MCP servers loaded.
	// User's MCP services (websearch, tavily, chrome-devtools, etc.) configured
	// in ~/.codebuddy/.mcp.json become completely unavailable.
	//
	// We read ~/.codebuddy/.mcp.json and inject it via --mcp-config so the ACP
	// process can still load MCP tools. Only applied to the codebuddy backend
	// since other ACP agents may not recognize this flag.
	if cmdParts[0] == "codebuddy" {
		if mcpConfigJSON := readUserMcpConfig(); mcpConfigJSON != "" {
			cmdArgs = append(cmdArgs, "--mcp-config", mcpConfigJSON)
			slog.Info("acp conn: injecting user MCP config via --mcp-config (workaround for issue #270)")
		}
	}

	cmd := exec.CommandContext(context.Background(), cmdName, cmdArgs...)
	cmd.Dir = c.cwd // project working directory for this ACP session
	slog.Info("acp conn: spawnLocked setting cmd.Dir",
		slog.String("clawbench_sid", c.clawbenchSID),
		slog.String("cmd_dir", c.cwd),
		slog.String("cmd_dir_empty", func() string {
			if c.cwd == "" {
				return "YES - will inherit server CWD!"
			}
			return "no"
		}()))
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, OrphanChildEnvVar)
	// Workaround for the opencode ACP subagent permission-ask hang:
	// opencode's ACP layer (packages/opencode/src/acp/permission.ts) silently
	// drops permission requests from subagent (task-tool) sessions — the
	// subagent's session isn't in the ACP session registry, so the handler
	// hits `if (!session) return` and never replies. The subagent's tool call
	// then blocks forever waiting for the approval, hanging the whole session.
	//
	// Injecting OPENCODE_PERMISSION makes the three permissions that default to
	// "ask" resolve to "allow" client-side, so subagents never trigger an ask.
	// We deliberately DON'T use {"*":"allow"}: that would be merged last into
	// every agent's permission rules and override per-mode enforcement (e.g.
	// plan mode's edit deny, explore's read-only boundary). Only these three
	// ask-type gates are lifted, and mode protections stay intact.
	if perm := openCodePermissionEnv(cmdName); perm != "" {
		cmd.Env = append(cmd.Env, perm)
	}
	// Put the ACP process in its own process group so we can kill the
	// entire tree (npx + child claude process) when closing the connection.
	// Without this, killing npx leaves the claude child alive, which holds
	// the stdout/stderr pipes open and causes cmd.Wait() to hang.
	setProcessGroup(cmd)

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

	spawnStart := time.Now()
	slog.Info("acp conn: spawning agent process",
		slog.String("agent_id", c.agent.ID),
		slog.String("clawbench_sid", c.clawbenchSID),
		slog.String("command", cmdName),
		slog.String("args", fmt.Sprintf("%v", cmdArgs)),
		slog.String("cmd.Dir", cmd.Dir))

	if startErr := cmd.Start(); startErr != nil {
		return fmt.Errorf("acp: start: %w", startErr)
	}
	slog.Info("acp perf: spawnLocked.cmd.Start", "agent_id", c.agent.ID, "clawbench_sid", c.clawbenchSID, "pid", cmd.Process.Pid, "elapsed", time.Since(spawnStart))

	client := NewClawBenchACPClient()
	client.connRef = c // back-reference for cache updates

	// Wrap stdout to fix common ACP protocol violations:
	// - CodeWhale/codewhale returns string IDs ("1") for numeric requests (1)
	// - Some agents emit terminal escape sequences on stdout
	stdoutFilter := newACPStdoutFilter(stdoutPipe)

	// Share one write lock between the SDK and the raw JSON-RPC side channel
	// (see acp_raw_rpc.go). Passing the wrapper to the SDK is what makes that
	// possible: its own write mutex is private, so both parties must go through
	// the same io.Writer or their bytes would interleave mid-line.
	stdinWriter := &lockedWriter{dst: stdinPipe}

	conn := acp.NewClientSideConnection(client, stdinWriter, stdoutFilter)
	// Deliberately NOT calling conn.SetLogger here. The SDK's SetLogger writes
	// c.logger without synchronization (connection.go:125) while receive() —
	// started by NewClientSideConnection above — reads it via loggerOrDefault()
	// (connection.go:128). Setting it after construction is therefore a data
	// race that `go test -race ./internal/ai` reports intermittently, failing
	// whichever unrelated test happens to be running.
	//
	// The call is also redundant: loggerOrDefault() falls back to
	// slog.Default(), which is exactly what was being passed. Production sets
	// slog.Default once at startup (cmd/server/main.go), before any connection
	// exists, so SDK diagnostics still reach the same handler.

	// Raw side channel: lets backends call private methods the SDK rejects
	// (e.g. CodeBuddy's session/steer). Responses are demuxed from the filter's
	// tee by request-id prefix.
	rawRPC := newACPRawRPC(stdinWriter, conn.Done())
	stdoutFilter.SetRawSink(rawRPC)

	initCtx, initCancel := context.WithTimeout(ctx, 60*time.Second)
	defer initCancel()

	initStart := time.Now()
	initResp, err := conn.Initialize(initCtx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs: acp.FileSystemCapabilities{
				ReadTextFile:  true,
				WriteTextFile: true,
			},
			Terminal: advertiseTerminalCapability(c.agent),
		},
		ClientInfo: &acp.Implementation{
			Name:    "clawbench",
			Version: "1.0.0",
		},
	})
	if err != nil {
		stdoutFilter.Close()
		_ = cmd.Process.Kill()
		return fmt.Errorf("acp: initialize: %w", err)
	}

	slog.Info("acp perf: spawnLocked.Initialize", "agent_id", c.agent.ID, "clawbench_sid", c.clawbenchSID, "protocol_version", initResp.ProtocolVersion, "elapsed", time.Since(initStart))

	// Extract ListSessions capability from ACP Initialize.
	// LoadSession is NOT written from the ACP response — BackendSpec.ACPLoadSession
	// is the authoritative source (the Initialize report may be unreliable).
	if c.agent != nil && c.agent.ID != "" {
		reg := GetAgentCapabilityRegistry()
		listSessions := initResp.AgentCapabilities.SessionCapabilities.List != nil
		deleteSession := initResp.AgentCapabilities.SessionCapabilities.Delete != nil
		// PromptCapabilities.Image: whether the agent accepts ContentBlock::Image
		// in session/prompt requests (multimodal recognition). Defaults to false
		// when omitted, per the protocol's "omitted means unsupported" rule.
		promptImage := initResp.AgentCapabilities.PromptCapabilities.Image
		reg.UpdateListSessions(c.agent.ID, listSessions)
		reg.UpdateDeleteSession(c.agent.ID, deleteSession)
		reg.UpdatePromptImage(c.agent.ID, promptImage)
		slog.Info("acp conn: extracted capabilities from Initialize",
			"agent_id", c.agent.ID,
			"loadSession", "skipped (use BackendSpec)",
			"listSessions", listSessions,
			"deleteSession", deleteSession,
			"promptImage", promptImage)
	}

	// Pre-scan CodeBuddy plugin commands to work around the AvailableCommandsUpdate
	// race condition (issue #383). Plugin skills aren't available at NewSession time
	// because CodeBuddy's PluginManager hasn't finished loading yet. Scanning the
	// plugin cache directory pre-populates the registry so EmitCommandsUpdate can
	// include plugin commands from the start.
	if isCodeBuddyBackend(c.agent) {
		if pluginCmds := ScanCodeBuddyPluginCommands(); len(pluginCmds) > 0 {
			agentID := c.agent.ID
			existing := GetAgentCapabilityRegistry().GetCommands(agentID)
			merged := MergeCommands(existing, pluginCmds)
			GetAgentCapabilityRegistry().UpdateCommands(agentID, merged)
			client.MergeCommandsFromScan(pluginCmds)
			slog.Info("acp: pre-scanned CodeBuddy plugin commands",
				"agent", agentID, "plugin_count", len(pluginCmds), "merged_count", len(merged))
		}
	}

	// Pre-scan CodeBuddy skills so they are available in ACP mode just like TUI mode.
	// CodeBuddy's ACP process does not auto-scan ~/.codebuddy/skills/, so we
	// scan them here and inject a skills summary into the system prompt.
	// Skills also appear in the slash command menu (/) via AvailableCommandsUpdate.
	if isCodeBuddyBackend(c.agent) {
		if skills := ScanCodeBuddySkills(); len(skills) > 0 {
			c.skillsPrompt = buildSkillsSystemPrompt(skills)

			// Register skills as slash commands so they appear in the / menu
			skillCmds := SkillsToCommands(skills)
			agentID := c.agent.ID
			existing := GetAgentCapabilityRegistry().GetCommands(agentID)
			merged := MergeCommands(existing, skillCmds)
			GetAgentCapabilityRegistry().UpdateCommands(agentID, merged)
			client.MergeCommandsFromScan(skillCmds)

			slog.Info("acp: pre-scanned CodeBuddy skills",
				"agent", agentID, "skill_count", len(skills), "command_count", len(skillCmds))
		} else {
			c.skillsPrompt = ""
		}
	}

	c.cmd = cmd
	c.conn = conn
	c.client = client
	c.stdoutFilter = stdoutFilter
	c.stdin = stdinWriter
	c.rawRPC = rawRPC
	c.acpSID = "" // cleared on respawn — will be set by ensureAliveWithSession
	c.alive = true
	c.lastUsed = time.Now()
	c.startedAt = time.Now()
	c.cmdWaitOnce = sync.Once{}
	c.cmdWaitState = nil

	go c.watchProcessDeath()
	return nil
}

// readUserMcpConfig reads ~/.codebuddy/.mcp.json and returns the mcpServers
// JSON string if non-empty, suitable for --mcp-config CLI injection.
// Returns "" on any error or if no servers configured.
func readUserMcpConfig() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, ".codebuddy", ".mcp.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var cfg struct {
		McpServers map[string]any `json:"mcpServers"`
	}
	if unmarshalErr := json.Unmarshal(data, &cfg); unmarshalErr != nil {
		return ""
	}
	if len(cfg.McpServers) == 0 {
		return ""
	}
	// Re-marshal just the mcpServers object as --mcp-config expects
	serversJSON, err := json.Marshal(cfg.McpServers)
	if err != nil {
		return ""
	}
	return string(serversJSON)
}

// openCodePermissionEnvValue is injected into opencode ACP processes via the
// OPENCODE_PERMISSION env var (opencode merges it into its `permission` config
// and it is inherited by subagent sessions). See the workaround comment in
// spawnLocked for the full bug context.
const openCodePermissionEnvValue = `{"external_directory":"allow","read":{"*.env":"allow","*.env.*":"allow"},"doom_loop":"allow"}`

// openCodePermissionEnv returns the "OPENCODE_PERMISSION=<json>" env entry for
// opencode ACP processes, or "" for other backends so their behavior is
// unchanged.
func openCodePermissionEnv(cmdName string) string {
	if cmdName != "opencode" {
		return ""
	}
	return "OPENCODE_PERMISSION=" + openCodePermissionEnvValue
}

// watchProcessDeath monitors the ACP connection and marks it as dead
// when the agent process exits or the connection drops.
func (c *ACPConn) watchProcessDeath() {
	if c.conn == nil {
		return
	}
	<-c.conn.Done()

	c.mu.Lock()
	if c.alive {
		c.alive = false
		if c.agent != nil && c.agent.ID != "" {
			GetAgentCapabilityRegistry().MarkStale(c.agent.ID)
		}
	}
	// Cancel any pending prompt to unblock conn.Prompt call
	if c.promptCancel != nil {
		c.promptCancel()
		c.promptCancel = nil
	}
	agentID := ""
	if c.agent != nil {
		agentID = c.agent.ID
	}
	c.mu.Unlock()

	// Collect crash diagnostics outside the lock
	diag := c.collectCrashDiagnostics()

	if diag.ExitCode == 0 && diag.Signal == "" {
		slog.Info(
			"acp conn: agent process exited",
			"agent_id", agentID,
			"clawbench_sid", c.clawbenchSID,
			"exit_code", diag.ExitCode,
			"uptime", diag.Uptime.Round(time.Second),
		)
	} else {
		slog.Error(
			"acp conn: agent process died",
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
// Also updates cached state so re-emitted WS events reflect the new value.
//
// configID is the agent-facing (wire) config-option id as sent on the wire —
// for well-known categories it is resolved through the agent's advertised ids
// first (see resolveWireConfigID), so callers may pass either the historical
// hardcoded id (e.g. "thinkingEffort") or the internal category key
// ("thought_level") and the actual RPC still uses what the agent advertised
// (e.g. claude-agent-acp's "effort"). Dedup/unsupported tracking keys on the
// resolved wire id, so a rejection under one spelling is not bypassed by
// sending under another (issue #429).
func (c *ACPConn) SetSessionConfigOption(ctx context.Context, configID, value string) {
	category, wireID := c.resolveConfigCategory(configID)
	if !c.shouldSetConfig(wireID, value) {
		slog.Debug("acp conn: SetSessionConfigOption skipped (unchanged)", "config_id", wireID, "value", value, "clawbench_sid", c.clawbenchSID)
		return
	}

	c.mu.Lock()
	acpSID := c.acpSID
	c.mu.Unlock()

	if acpSID == "" {
		slog.Debug("acp conn: SetSessionConfigOption: no session", "clawbench_sid", c.clawbenchSID)
		return
	}

	wasUnsupported := c.IsConfigUnsupported(wireID)

	c.setSessionConfigOption(ctx, acpSID, wireID, value)

	nowUnsupported := c.IsConfigUnsupported(wireID)

	if nowUnsupported {
		return
	}

	_ = wasUnsupported

	c.markConfigSet(wireID, value)

	// Keep the session-level cached current value in sync so re-emitted WS
	// events reflect the new selection.
	switch category {
	case "mode":
		c.UpdateCachedCurrent("mode", value)
	case "thought_level":
		c.UpdateCachedCurrent("thought_level", value)
	case "model":
		c.UpdateCachedCurrent("model", value)
	}
}

// resolveConfigCategory maps a caller-supplied config id (historical hardcoded
// spelling or internal category key) to the connection's wire id for the
// option. Returns (internalCategory, wireID) where wireID is what the agent
// advertised (falling back to the historical spelling when unknown), so
// dedup/unsupported bookkeeping stays on the single id actually sent on the
// wire.
func (c *ACPConn) resolveConfigCategory(configID string) (category, wireID string) {
	switch configID {
	case "mode":
		return "mode", c.resolveWireConfigID("mode", "mode")
	case "thought_level", "thinkingEffort", "thinking_effort":
		return "thought_level", c.resolveWireConfigID("thought_level", "thinkingEffort")
	case "model":
		return "model", c.resolveWireConfigID("model", "model")
	default:
		// Unknown agent-specific category: send under the literal id. The
		// session cache key is the wire id itself (category==wireID is only
		// used by the well-known cases above).
		return configID, configID
	}
}

// setSessionConfigOption sets a config option. Errors are logged but not fatal.
func (c *ACPConn) setSessionConfigOption(ctx context.Context, acpSessionID, configID, value string) {
	c.mu.Lock()
	conn := c.conn
	alive := c.alive && c.isAliveLocked()
	hook := c.setConfigOptionFn
	c.mu.Unlock()

	if conn == nil || !alive {
		slog.Debug("acp conn: skipping set_config_option on dead connection", "config_id", configID, "value", value)
		return
	}

	// Test override: record the (resolved) wire config id without an RPC.
	if hook != nil {
		hookErr := hook(ctx, acpSessionID, configID, value)
		if hookErr != nil {
			slog.Warn("acp conn: set_config_option hook failed", "config_id", configID, "value", value, "error", hookErr)
		}
		return
	}

	slog.Info("acp conn: sending set_config_option", "config_id", configID, "value", value, "clawbench_sid", c.clawbenchSID, "acp_sid", acpSessionID)

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
		if isUnknownConfigOption(err) {
			c.lastSetConfigMu.Lock()
			if c.unsupportedConfigs == nil {
				c.unsupportedConfigs = make(map[string]bool)
			}
			c.unsupportedConfigs[configID] = true
			c.lastSetConfigMu.Unlock()
			slog.Info("acp conn: marking config as unsupported by agent", "config_id", configID, "value", value)
		}
		if isACPPeerDisconnected(err) {
			c.mu.Lock()
			c.alive = false
			c.mu.Unlock()
			slog.Info("acp conn: set_config_option detected peer disconnect, marking dead", "config_id", configID, "value", value)
		}
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
