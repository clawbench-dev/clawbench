package ai

import (
	"context"
	"fmt"
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

// ACPConnectionPool manages a pool of long-lived ACP connections, one per agent.
// Connections are reused across multiple ExecuteStream calls and multiple sessions.
// Idle connections are automatically killed after idleTimeout.
type ACPConnectionPool struct {
	mu      sync.Mutex
	entries map[string]*ACPConnEntry // keyed by agentID
	done    chan struct{}            // closed on StopAll
}

var globalPool *ACPConnectionPool
var globalPoolOnce sync.Once

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
		// Already closed
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
// ACPConnEntry — one long-lived ACP connection to an agent
// ---------------------------------------------------------------------------

// ACPConnEntry represents a long-lived ACP connection to one agent.
// It can serve multiple sessions and multiple prompt turns.
type ACPConnEntry struct {
	agent     *model.Agent
	mu        sync.Mutex
	transport string // "stdio" or "http"

	// stdio fields (nil for http transport)
	cmd    *exec.Cmd
	conn   *acp.ClientSideConnection
	client *ClawBenchACPClient // shared across sessions

	// http fields (nil for stdio transport)
	httpTransport *ACPHTTPTransport

	// session mapping: clawbench session ID → ACP session ID
	sessions map[string]string

	// lastSessionResp stores the NewSessionResponse from the most recent session/new
	// so ExecuteStream can extract mode/config state. Cleared after reading.
	lastSessionResp *acp.NewSessionResponse

	// lastModeState/lastConfigState store mode info from HTTP transport session/new.
	// Used when HTTP transport doesn't have an acp.NewSessionResponse.
	lastModeState   *ModeState
	lastConfigState *ConfigOptionState

	// liveness
	lastUsed time.Time
	alive    bool
}

// newACPConnEntry creates a new (uninitialized) ACPConnEntry.
func newACPConnEntry(agent *model.Agent) *ACPConnEntry {
	transport := "stdio"
	if agent.Transport == "acp-http" {
		transport = "http"
	}
	return &ACPConnEntry{
		agent:    agent,
		transport: transport,
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

	// Connection is dead or not yet created — respawn
	if e.transport == "stdio" {
		return e.spawnStdioLocked(ctx)
	}
	return e.connectHTTPLocked(ctx)
}

// isAliveLocked checks if the connection is still alive (must hold e.mu).
func (e *ACPConnEntry) isAliveLocked() bool {
	if e.transport == "stdio" {
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
	// HTTP: check daemon health
	if e.httpTransport == nil {
		return false
	}
	return e.httpTransport.HealthCheck(context.Background())
}

// spawnStdioLocked spawns the agent process and initializes the connection (must hold e.mu).
func (e *ACPConnEntry) spawnStdioLocked(ctx context.Context) error {
	// Kill any existing process first
	if e.cmd != nil && e.cmd.Process != nil {
		_ = e.cmd.Process.Kill()
		_ = e.cmd.Wait()
	}

	cmdParts := strings.Fields(e.agent.AcpCommand)
	if len(cmdParts) == 0 {
		return fmt.Errorf("acp stdio: no acp_command configured for agent %q", e.agent.ID)
	}

	cmdName := cmdParts[0]
	cmdArgs := cmdParts[1:]

	cmd := exec.CommandContext(ctx, cmdName, cmdArgs...)
	cmd.Dir = "" // cwd is per-session, set during NewSession
	cmd.Env = os.Environ()

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("acp stdio: stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("acp stdio: stdout pipe: %w", err)
	}
	cmd.Stderr = &strings.Builder{}

	slog.Info("acp pool: spawning agent process",
		"agent_id", e.agent.ID,
		"command", cmdName,
		"args", cmdArgs,
	)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("acp stdio: start: %w", err)
	}

	// Create shared ACP client and connection
	client := NewClawBenchACPClient()
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
		return fmt.Errorf("acp stdio: initialize: %w", err)
	}

	slog.Info("acp pool: agent initialized",
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

// connectHTTPLocked establishes the HTTP transport connection (must hold e.mu).
func (e *ACPConnEntry) connectHTTPLocked(ctx context.Context) error {
	headers := e.agent.AcpHeaders
	if headers == nil {
		headers = make(map[string]string)
	}

	// Use DaemonManager for daemon health check
	baseURL, err := GetDaemonManager().EnsureDaemon(ctx, e.agent.ID, e.agent.ServePort, headers)
	if err != nil {
		return fmt.Errorf("acp http: daemon not available: %w", err)
	}

	transport := NewACPHTTPTransport(baseURL, headers)

	// Connect (daemon-specific step)
	if err := transport.Connect(ctx); err != nil {
		return fmt.Errorf("acp http: connect: %w", err)
	}

	// Initialize
	if err := transport.Initialize(ctx); err != nil {
		_ = transport.Close(ctx)
		return fmt.Errorf("acp http: initialize: %w", err)
	}

	slog.Info("acp pool: HTTP transport connected", "agent_id", e.agent.ID, "base_url", baseURL)

	e.httpTransport = transport
	e.sessions = make(map[string]string) // clear session mapping on reconnect
	e.alive = true
	e.lastUsed = time.Now()

	return nil
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
	var acpSID string
	var err error

	if e.transport == "stdio" {
		sessResp, err2 := e.conn.NewSession(ctx, acp.NewSessionRequest{
			Cwd:        cwd,
			McpServers: []acp.McpServer{},
		})
		if err2 != nil {
			return "", false, fmt.Errorf("acp: session/new: %w", err2)
		}
		acpSID = string(sessResp.SessionId)
		// Store session response for mode/config extraction by ExecuteStream
		e.lastSessionResp = &sessResp
	} else {
		var modeState *ModeState
		var configState *ConfigOptionState
		acpSID, modeState, configState, err = e.httpTransport.NewSession(ctx, cwd)
		if err != nil {
			return "", false, fmt.Errorf("acp http: session/new: %w", err)
		}
		// Store mode/config state for later retrieval by ExecuteStream
		if modeState != nil || configState != nil {
			e.lastModeState = modeState
			e.lastConfigState = configState
		}
	}

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

// GetAndClearModeStates returns the last mode/config states and clears them.
// Used by ExecuteStream for HTTP transport sessions that don't have NewSessionResponse.
func (e *ACPConnEntry) GetAndClearModeStates() (*ModeState, *ConfigOptionState) {
	e.mu.Lock()
	defer e.mu.Unlock()
	ms := e.lastModeState
	cs := e.lastConfigState
	e.lastModeState = nil
	e.lastConfigState = nil
	return ms, cs
}

// Prompt sends a prompt on the given ACP session and forwards events to streamCh.
// It registers the streamCh in the client's session routes before sending the prompt,
// and unregisters after the prompt completes.
func (e *ACPConnEntry) Prompt(ctx context.Context, acpSessionID string, prompt []acp.ContentBlock, streamCh chan<- StreamEvent, req ChatRequest) error {
	e.mu.Lock()
	client := e.client
	conn := e.conn
	transport := e.httpTransport
	transportType := e.transport
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
	if transportType == "stdio" {
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
	} else {
		stopReason, err := transport.Prompt(ctx, acpSessionID, prompt, streamCh)
		if err != nil {
			if ctx.Err() != nil {
				slog.Info("acp pool: HTTP prompt cancelled", "acp_sid", acpSessionID)
				_ = transport.Cancel(ctx, acpSessionID)
				return ctx.Err()
			}
			return fmt.Errorf("acp http: prompt: %w", err)
		}
		// Emit metadata with stop reason for HTTP transport
		if stopReason != "" {
			forwardACPEvent(streamCh, StreamEvent{
				Type: "metadata",
				Meta: &Metadata{StopReason: stopReason},
			})
		}
	}

	return nil
}

// CancelTurn cancels the current in-progress prompt turn for the given session.
// The session remains open for subsequent prompts.
func (e *ACPConnEntry) CancelTurn(ctx context.Context, acpSessionID string) {
	e.mu.Lock()
	conn := e.conn
	transport := e.httpTransport
	transportType := e.transport
	e.mu.Unlock()

	if transportType == "stdio" && conn != nil {
		_ = conn.Cancel(ctx, acp.CancelNotification{SessionId: acp.SessionId(acpSessionID)})
	} else if transport != nil {
		_ = transport.Cancel(ctx, acpSessionID)
	}
}

// SetSessionConfigOption sets a config option (e.g., mode, model, thinkingEffort) for a session.
// This is the exported version used by the handler layer for user-initiated mode switching.
// It resolves the ClawBench session ID to the ACP session ID internally.
// Errors are logged but not returned — the agent may not support this option.
func (e *ACPConnEntry) SetSessionConfigOption(ctx context.Context, clawbenchSID, configID, value string) {
	e.mu.Lock()
	acpSID, ok := e.sessions[clawbenchSID]
	e.mu.Unlock()

	if !ok {
		slog.Debug("acp pool: SetSessionConfigOption: session not found", "clawbench_sid", clawbenchSID)
		return
	}

	e.setSessionConfigOption(ctx, acpSID, configID, value)
}

// setSessionConfigOption sets a config option (e.g., model, thinkingEffort).
// Errors are logged but not fatal — the agent may not support this option.
func (e *ACPConnEntry) setSessionConfigOption(ctx context.Context, acpSessionID, configID, value string) {
	e.mu.Lock()
	conn := e.conn
	transport := e.httpTransport
	transportType := e.transport
	e.mu.Unlock()

	var err error
	if transportType == "stdio" && conn != nil {
		_, err = conn.SetSessionConfigOption(ctx, acp.SetSessionConfigOptionRequest{
			ValueId: &acp.SetSessionConfigOptionValueId{
				SessionId: acp.SessionId(acpSessionID),
				ConfigId:  acp.SessionConfigId(configID),
				Value:     acp.SessionConfigValueId(value),
			},
		})
	} else if transport != nil {
		err = transport.SetSessionConfigOption(ctx, acpSessionID, acp.SetSessionConfigOptionRequest{
			ValueId: &acp.SetSessionConfigOptionValueId{
				SessionId: acp.SessionId(acpSessionID),
				ConfigId:  acp.SessionConfigId(configID),
				Value:     acp.SessionConfigValueId(value),
			},
		})
	}

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

// Close kills the agent process (stdio) or closes the transport (http)
// and marks the entry as dead.
func (e *ACPConnEntry) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.transport == "stdio" {
		if e.cmd != nil && e.cmd.Process != nil {
			_ = e.cmd.Process.Kill()
			_ = e.cmd.Wait()
		}
		e.cmd = nil
		e.conn = nil
		e.client = nil
	} else {
		if e.httpTransport != nil {
			_ = e.httpTransport.Close(context.Background())
		}
		e.httpTransport = nil
	}

	e.alive = false
	e.sessions = make(map[string]string)
}
