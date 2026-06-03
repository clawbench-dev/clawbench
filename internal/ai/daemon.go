package ai

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// DaemonManager manages HTTP daemon processes for ACP-over-HTTP agents.
// It handles health checks, auto-start, and port allocation.
type DaemonManager struct {
	mu     sync.Mutex
	daemons map[string]*daemonState // keyed by agentID
}

// daemonState tracks a running daemon process.
type daemonState struct {
	port    int       // discovered or allocated port
	cmd     *exec.Cmd // running daemon process (nil if externally managed)
	healthy bool      // last known health status
	baseURL string    // e.g. "http://localhost:9191"
	headers map[string]string
}

// globalDaemonManager is the singleton daemon manager.
var globalDaemonManager = &DaemonManager{
	daemons: make(map[string]*daemonState),
}

// GetDaemonManager returns the global DaemonManager singleton.
func GetDaemonManager() *DaemonManager {
	return globalDaemonManager
}

// EnsureDaemon ensures that the HTTP daemon for the given agent is running and healthy.
// If the daemon is already running and healthy, it returns immediately.
// If the daemon is not running, it attempts to auto-start it.
// Returns the base URL of the daemon, or an error if the daemon cannot be started.
func (dm *DaemonManager) EnsureDaemon(ctx context.Context, agentID string, servePort int, headers map[string]string) (string, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	// Check if we already have a running daemon for this agent
	if state, ok := dm.daemons[agentID]; ok {
		if state.healthy {
			return state.baseURL, nil
		}
		// Check health again
		if checkDaemonHealth(ctx, state.baseURL, headers) {
			state.healthy = true
			return state.baseURL, nil
		}
		// Unhealthy — try to restart
		slog.Warn("acp daemon: unhealthy, attempting restart", "agent_id", agentID)
		if state.cmd != nil && state.cmd.Process != nil {
			_ = state.cmd.Process.Kill()
			_ = state.cmd.Wait()
		}
		delete(dm.daemons, agentID)
	}

	// Try connecting to an already-running daemon on the known port
	baseURL := fmt.Sprintf("http://localhost:%d", servePort)
	if checkDaemonHealth(ctx, baseURL, headers) {
		dm.daemons[agentID] = &daemonState{
			port:    servePort,
			baseURL: baseURL,
			headers: headers,
			healthy: true,
		}
		slog.Info("acp daemon: found running daemon", "agent_id", agentID, "port", servePort)
		return baseURL, nil
	}

	// No running daemon found — try auto-start
	return "", fmt.Errorf("acp daemon: no healthy daemon found for agent %s on port %d; auto-start is not yet implemented — please start the daemon manually", agentID, servePort)
}

// RegisterDaemon registers a known daemon URL for an agent (without starting it).
// Used when the daemon is managed externally (e.g., by the user or a systemd service).
func (dm *DaemonManager) RegisterDaemon(agentID, baseURL string, headers map[string]string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dm.daemons[agentID] = &daemonState{
		baseURL: strings.TrimRight(baseURL, "/"),
		headers: headers,
		healthy: true, // assume healthy until proven otherwise
	}
	slog.Info("acp daemon: registered", "agent_id", agentID, "url", baseURL)
}

// StopDaemon stops a daemon process managed by DaemonManager.
// If the daemon was not started by DaemonManager, this is a no-op.
func (dm *DaemonManager) StopDaemon(agentID string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	state, ok := dm.daemons[agentID]
	if !ok || state.cmd == nil {
		return
	}

	if state.cmd.Process != nil {
		slog.Info("acp daemon: stopping", "agent_id", agentID)
		_ = state.cmd.Process.Kill()
		_ = state.cmd.Wait()
	}
	delete(dm.daemons, agentID)
}

// StopAll stops all daemon processes managed by DaemonManager.
func (dm *DaemonManager) StopAll() {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	for agentID, state := range dm.daemons {
		if state.cmd != nil && state.cmd.Process != nil {
			slog.Info("acp daemon: stopping", "agent_id", agentID)
			_ = state.cmd.Process.Kill()
			_ = state.cmd.Wait()
		}
		delete(dm.daemons, agentID)
	}
}

// IsDaemonHealthy checks whether the daemon for the given agent is healthy.
func (dm *DaemonManager) IsDaemonHealthy(ctx context.Context, agentID string) bool {
	dm.mu.Lock()
	state, ok := dm.daemons[agentID]
	dm.mu.Unlock()

	if !ok {
		return false
	}

	healthy := checkDaemonHealth(ctx, state.baseURL, state.headers)

	// Update health status under lock to avoid data race
	dm.mu.Lock()
	state.healthy = healthy
	dm.mu.Unlock()

	return healthy
}

// checkDaemonHealth performs a health check on the given daemon URL.
func checkDaemonHealth(ctx context.Context, baseURL string, headers map[string]string) bool {
	healthURL := strings.TrimRight(baseURL, "/") + "/api/v1/health"

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return false
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode == http.StatusOK
}

// findAvailablePort finds an available TCP port on localhost.
func findAvailablePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}
