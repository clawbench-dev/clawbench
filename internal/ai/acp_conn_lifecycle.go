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

	// LoadSession branch
	if c.loadTargetSID != "" {
		loadSID := c.loadTargetSID
		c.loadTargetSID = "" // clear to prevent reuse on next call

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
			return false, fmt.Errorf("acp: session/load: %w", err)
		}

		c.acpSID = loadSID
		c.lastLoadSessionResp = &loadResp
		c.lastUsed = time.Now()
		slog.Info("acp conn: loaded session via LoadSession", "clawbench_sid", c.clawbenchSID, "acp_sid", loadSID)
		return true, nil
	}

	// Try ResumeSession if we had a previous session
	if preSpawnAcpSID != "" {
		acpSID := preSpawnAcpSID
		err := c.recoverViaResumeSession(ctx, cwd, acpSID, prevConfig)
		if err == nil {
			return false, nil // recovered successfully
		}
		// ResumeSession failed — the session is unrecoverable.
		// Do NOT silently fall back to NewSession (amnesia): the user
		// would lose all conversation context without any indication.
		// Surface the error so the user knows the session needs a fresh start.
		slog.Error("acp conn: ResumeSession failed, session is unrecoverable",
			"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "error", err)
		c.killProcessLocked()
		return false, fmt.Errorf("acp: session %s ResumeSession failed: %w", acpSID, err)
	}

	// No prior session — create new session.
	newSessCtx, newSessCancel := context.WithTimeout(ctx, 15*time.Second)
	defer newSessCancel()

	newSessStart := time.Now()
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
func (c *ACPConn) snapshotCachedConfig() cachedConfigSnapshot {
	return cachedConfigSnapshot{
		mode:   c.currentModeID,
		model:  c.currentModelID,
		effort: c.currentThinkingEffortID,
	}
}

// recoverViaResumeSession recovers a session via ResumeSession and re-applies config.
func (c *ACPConn) recoverViaResumeSession(ctx context.Context, cwd, acpSID string, prevConfig cachedConfigSnapshot) error {
	resumeCtx, resumeCancel := context.WithTimeout(ctx, 60*time.Second)
	defer resumeCancel()

	resumeStart := time.Now()
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
func (c *ACPConn) reapplyConfigAfterResume(ctx context.Context, acpSID string, prevConfig cachedConfigSnapshot) {
	reapplyStart := time.Now()
	c.reapplyConfigOption(ctx, acpSID, "mode", prevConfig.mode)
	c.reapplyConfigOption(ctx, acpSID, "model", prevConfig.model)
	c.reapplyConfigOption(ctx, acpSID, "thinkingEffort", prevConfig.effort)
	slog.Info("acp perf: reapplyConfigAfterResume.total", "clawbench_sid", c.clawbenchSID, "elapsed", time.Since(reapplyStart),
		"mode", prevConfig.mode, "model", prevConfig.model, "effort", prevConfig.effort)
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
// Must be called with c.mu held; temporarily releases c.mu during Wait().
func (c *ACPConn) killProcessLocked() {
	if c.cmd == nil || c.cmd.Process == nil {
		return
	}

	// Close the stdout filter first to unblock pending reads on the pipe.
	// This prevents cmd.Wait() from hanging when the process is killed but
	// stdout hasn't been closed yet.
	if c.stdoutFilter != nil {
		c.stdoutFilter.Close()
		c.stdoutFilter = nil
	}

	// Kill the entire process group (see killProcessGroup for rationale).
	killProcessGroup(c.cmd.Process)
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
//
//nolint:gocyclo // complex spawn logic with multiple sequential setup steps
func (c *ACPConn) spawnLocked(ctx context.Context) error {
	// Kill any existing process first
	if c.cmd != nil && c.cmd.Process != nil {
		killStart := time.Now()
		if c.conn != nil && c.acpSID != "" {
			cancelCtx, cancelCancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = c.conn.Cancel(cancelCtx, acp.CancelNotification{SessionId: acp.SessionId(c.acpSID)})
			cancelCancel()
		}
		// Close the old stdout filter to unblock pending reads before killing
		if c.stdoutFilter != nil {
			c.stdoutFilter.Close()
			c.stdoutFilter = nil
		}
		// Kill the entire process group (npx + child processes).
		killProcessGroup(c.cmd.Process)
		oldCmd := c.cmd
		c.mu.Unlock()
		_ = oldCmd.Wait()
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

	// Resolve embedded binary path for bare command names (e.g. "opencode acp" → use embedded opencode binary).
	if !strings.Contains(cmdName, "/") {
		if spec := model.FindBackendSpecByDefaultCmd(cmdName); spec != nil && spec.EmbeddedSubDir != "" {
			if p := model.EmbeddedBinaryPath(spec.EmbeddedSubDir); p != "" {
				cmdName = p
			}
		}
	}

	cmd := exec.CommandContext(context.Background(), cmdName, cmdArgs...)
	cmd.Dir = c.cwd // project working directory for this ACP session
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, OrphanChildEnvVar)
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
	slog.Info("acp conn: spawning agent process", "agent_id", c.agent.ID, "clawbench_sid", c.clawbenchSID, "command", cmdName, "args", cmdArgs)

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

	conn := acp.NewClientSideConnection(client, stdinPipe, stdoutFilter)
	conn.SetLogger(slog.Default())

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
			Terminal: true,
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

	// Extract LoadSession and ListSessions capabilities
	if c.agent != nil && c.agent.ID != "" {
		reg := GetAgentCapabilityRegistry()
		reg.UpdateLoadSession(c.agent.ID, initResp.AgentCapabilities.LoadSession)
		listSessions := initResp.AgentCapabilities.SessionCapabilities.List != nil
		reg.UpdateListSessions(c.agent.ID, listSessions)
		slog.Info("acp conn: extracted capabilities from Initialize",
			"agent_id", c.agent.ID,
			"loadSession", initResp.AgentCapabilities.LoadSession,
			"listSessions", listSessions)
	}

	c.cmd = cmd
	c.conn = conn
	c.client = client
	c.stdoutFilter = stdoutFilter
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
// Also updates cached state so re-emitted SSE events reflect the new value.
func (c *ACPConn) SetSessionConfigOption(ctx context.Context, configID, value string) {
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

	nowUnsupported := c.IsConfigUnsupported(configID)

	if nowUnsupported {
		return
	}

	_ = wasUnsupported

	switch configID {
	case "mode":
		c.UpdateCachedCurrentMode(value)
		c.markConfigSet("mode", value)
	case "thinking_effort", "thought_level", "thinkingEffort":
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
