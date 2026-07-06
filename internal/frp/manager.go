package frp

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"log/slog"

	"github.com/fatedier/frp/client"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/config/source"

	"clawbench/internal/model"
	"clawbench/internal/ws"
)

// State represents the FRP tunnel lifecycle state.
type State string

const (
	StateDisabled State = "disabled" // FRP not configured
	StateStarting State = "starting" // service started, waiting for proxy ready
	StateRunning  State = "running"  // tunnel established, remote port known
	StateFailed   State = "failed"   // service exited with error
	StateStopped  State = "stopped"  // user stopped the tunnel
)

// Status holds the current FRP tunnel status.
type Status struct {
	Enabled       bool   `json:"enabled"`
	Running       bool   `json:"running"`
	State         State  `json:"state"`
	ServerAddr    string `json:"server_addr,omitempty"`
	RemotePort    int    `json:"remote_port,omitempty"`
	SSHRemotePort int    `json:"ssh_remote_port,omitempty"`
	RemoteURL     string `json:"remote_url,omitempty"`
	Message       string `json:"message,omitempty"`
}

// Manager manages the in-process frp client.Service lifecycle.
// A Manager is single-use: after Stop(), create a new Manager to restart.
type Manager struct {
	cfg           model.FRPConfig
	httpLocalPort int
	sshLocalPort  int

	mu            sync.RWMutex
	svc           *client.Service
	configSource  *source.ConfigSource
	cancel        context.CancelFunc
	state         State
	remotePort    int
	sshRemotePort int

	readyCh chan Status   // receives Status once when port is assigned
	done    chan struct{} // closed when manager stops (via sync.Once)
	closeOnce sync.Once  // ensures done is closed exactly once
	stopped bool         // user called Stop()
}

// NewManager creates a new FRP manager. Call Start() to launch the in-process frp service.
func NewManager(cfg model.FRPConfig, httpLocalPort, sshLocalPort int) *Manager {
	return &Manager{
		cfg:           cfg,
		httpLocalPort: httpLocalPort,
		sshLocalPort:  sshLocalPort,
		state:         StateStarting,
		readyCh:       make(chan Status, 1),
		done:          make(chan struct{}),
	}
}

// OnReady returns a channel that receives Status once the remote port is assigned.
// The channel receives at most one value. Caller should select with a timeout.
func (m *Manager) OnReady() <-chan Status {
	return m.readyCh
}

// Status returns the current FRP tunnel status.
func (m *Manager) Status() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s := Status{
		Enabled:       true,
		State:         m.state,
		Running:       m.state == StateRunning,
		ServerAddr:    m.cfg.ServerAddr,
		RemotePort:    m.remotePort,
		SSHRemotePort: m.sshRemotePort,
	}

	if m.state == StateRunning && m.remotePort > 0 {
		s.RemoteURL = FormatRemoteURL(m.cfg.ServerAddr, m.remotePort)
	}

	return s
}

// Start launches the in-process frp client.Service. Returns error on immediate failure.
func (m *Manager) Start() error {
	commonCfg := buildClientCommonConfig(m.cfg)
	proxyCfgs := buildProxyConfigs(m.cfg, m.httpLocalPort, m.sshLocalPort)

	// Create in-memory config source
	cs := source.NewConfigSource()
	if err := cs.ReplaceAll(proxyCfgs, nil); err != nil {
		return fmt.Errorf("frp: set proxy configs: %w", err)
	}
	aggregator := source.NewAggregator(cs)

	// Create Service
	svc, err := client.NewService(client.ServiceOptions{
		Common:                 commonCfg,
		ConfigSourceAggregator: aggregator,
	})
	if err != nil {
		return fmt.Errorf("frp: create service: %w", err)
	}

	// Create context for the Run() goroutine
	ctx, cancel := context.WithCancel(context.Background())

	// Store all state under mutex to prevent race with concurrent Stop()
	m.mu.Lock()
	m.svc = svc
	m.configSource = cs
	m.cancel = cancel
	m.mu.Unlock()

	// Run service in a goroutine (blocks until context cancelled)
	go m.runService(ctx, svc)

	// Start status polling goroutine
	go m.pollStatus()

	return nil
}

// Stop gracefully stops the in-process frp service.
func (m *Manager) Stop() {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	m.stopped = true
	m.state = StateStopped
	cancel := m.cancel
	m.mu.Unlock()

	// Signal the Run() goroutine to stop
	if cancel != nil {
		cancel()
	}

	// Close done channel exactly once to signal pollStatus to exit
	m.closeOnce.Do(func() { close(m.done) })

	emitEvent("stopped")
}

// Reconfigure updates the frp service configuration.
// If common config changed (ServerAddr/ServerPort/Token), returns needsRestart=true
// because frp's Service cannot hot-swap the control connection.
// Proxy-only changes (RemotePort/SSHRemotePort) are applied in-place.
// The caller should create a new Manager if needsRestart is true.
func (m *Manager) Reconfigure(cfg model.FRPConfig, httpLocalPort, sshLocalPort int) (needsRestart bool, err error) {
	// Check if common config changed (requires restart) — read under RLock
	m.mu.RLock()
	oldCfg := m.cfg
	m.mu.RUnlock()

	needsRestart = cfg.ServerAddr != oldCfg.ServerAddr ||
		cfg.ServerPort != oldCfg.ServerPort ||
		cfg.Token != oldCfg.Token

	if needsRestart {
		return needsRestart, nil
	}

	// Build new proxy configs outside the lock
	proxyCfgs := buildProxyConfigs(cfg, httpLocalPort, sshLocalPort)
	commonCfg := buildClientCommonConfig(cfg)

	// Perform frp I/O outside the lock
	m.mu.RLock()
	cs := m.configSource
	svc := m.svc
	m.mu.RUnlock()

	if cs != nil {
		if err := cs.ReplaceAll(proxyCfgs, nil); err != nil {
			return false, fmt.Errorf("frp: update proxy configs: %w", err)
		}
		if svc != nil {
			if err := svc.UpdateConfigSource(commonCfg, proxyCfgs, nil); err != nil {
				return false, fmt.Errorf("frp: update config source: %w", err)
			}
		}
	}

	// Update in-memory state under the lock
	m.mu.Lock()
	m.cfg = cfg
	m.httpLocalPort = httpLocalPort
	m.sshLocalPort = sshLocalPort
	m.mu.Unlock()

	return false, nil
}

// --- internal helpers ---

// runService runs the frp Service.Run() and handles exit.
// When Run() returns (whether by context cancellation or error),
// it ensures the done channel is closed so pollStatus exits.
func (m *Manager) runService(ctx context.Context, svc *client.Service) {
	err := svc.Run(ctx)

	m.mu.Lock()
	if !m.stopped {
		if err != nil {
			m.state = StateFailed
		} else {
			m.state = StateStopped
		}
	}
	m.mu.Unlock()

	if err != nil {
		slog.Error("frp: service exited with error", slog.String("err", err.Error()))
		emitEvent("failed", map[string]any{"error": err.Error()})
	} else {
		slog.Info("frp: service stopped")
	}

	// Ensure pollStatus exits even if Stop() wasn't called
	m.closeOnce.Do(func() { close(m.done) })
}

// pollStatus periodically checks proxy status via StatusExporter and updates state.
func (m *Manager) pollStatus() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.done:
			return
		case <-ticker.C:
			m.checkProxyStatus()
		}
	}
}

// checkProxyStatus queries the frp StatusExporter and updates remote port info.
func (m *Manager) checkProxyStatus() {
	m.mu.RLock()
	svc := m.svc
	stopped := m.stopped
	m.mu.RUnlock()

	if svc == nil || stopped {
		return
	}

	exporter := svc.StatusExporter()
	if exporter == nil {
		return
	}

	httpStatus, httpOK := exporter.GetProxyStatus("clawbench-http")
	sshStatus, sshOK := exporter.GetProxyStatus("clawbench-ssh")

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stopped {
		return
	}

	// Extract remote port from RemoteAddr (format: "host:port")
	if httpOK && httpStatus.Phase == ProxyPhaseRunning && httpStatus.RemoteAddr != "" {
		m.remotePort = parsePortFromAddr(httpStatus.RemoteAddr)
	}

	if sshOK && sshStatus.Phase == ProxyPhaseRunning && sshStatus.RemoteAddr != "" {
		m.sshRemotePort = parsePortFromAddr(sshStatus.RemoteAddr)
	}

	// Transition to running once HTTP port is assigned
	if m.remotePort > 0 && (m.state == StateStarting || m.state == StateFailed) {
		m.state = StateRunning

		status := Status{
			Enabled:       true,
			Running:       true,
			State:         StateRunning,
			ServerAddr:    m.cfg.ServerAddr,
			RemotePort:    m.remotePort,
			SSHRemotePort: m.sshRemotePort,
			RemoteURL:     FormatRemoteURL(m.cfg.ServerAddr, m.remotePort),
		}

		// Send to readyCh (non-blocking, channel has buffer 1)
		select {
		case m.readyCh <- status:
		default:
		}

		emitEvent("running", map[string]any{
			"remote_url":      status.RemoteURL,
			"remote_port":     status.RemotePort,
			"ssh_remote_port": status.SSHRemotePort,
		})
	}
}

// --- config builders (replace config.go) ---

// buildClientCommonConfig constructs the v1.ClientCommonConfig from ClawBench's FRPConfig.
func buildClientCommonConfig(cfg model.FRPConfig) *v1.ClientCommonConfig {
	loginFailExit := false // don't exit on login failure — frp will retry
	cc := &v1.ClientCommonConfig{
		ServerAddr: cfg.ServerAddr,
		ServerPort: cfg.ServerPort,
		Auth: v1.AuthClientConfig{
			Method: "token",
			Token:  cfg.Token,
		},
		LoginFailExit: &loginFailExit,
	}
	cc.Complete() // populate defaults
	return cc
}

// buildProxyConfigs constructs proxy configs for HTTP and optional SSH.
func buildProxyConfigs(cfg model.FRPConfig, httpLocalPort, sshLocalPort int) []v1.ProxyConfigurer {
	proxies := make([]v1.ProxyConfigurer, 0, 2)

	// HTTP proxy — main ClawBench web interface
	httpCfg := v1.NewProxyConfigurerByType(v1.ProxyTypeTCP)
	tcpCfg := httpCfg.(*v1.TCPProxyConfig)
	tcpCfg.ProxyBaseConfig.Name = "clawbench-http"
	tcpCfg.ProxyBaseConfig.LocalIP = "127.0.0.1"
	tcpCfg.ProxyBaseConfig.LocalPort = httpLocalPort
	tcpCfg.RemotePort = cfg.RemotePort
	tcpCfg.ProxyBaseConfig.Transport.UseCompression = true
	tcpCfg.ProxyBaseConfig.Complete()
	proxies = append(proxies, httpCfg)

	// SSH proxy — optional, only if SSH tunnel is running
	if sshLocalPort > 0 {
		sshCfg := v1.NewProxyConfigurerByType(v1.ProxyTypeTCP)
		sshTcpCfg := sshCfg.(*v1.TCPProxyConfig)
		sshTcpCfg.ProxyBaseConfig.Name = "clawbench-ssh"
		sshTcpCfg.ProxyBaseConfig.LocalIP = "127.0.0.1"
		sshTcpCfg.ProxyBaseConfig.LocalPort = sshLocalPort
		sshTcpCfg.RemotePort = cfg.SSHRemotePort
		sshTcpCfg.ProxyBaseConfig.Transport.UseCompression = true
		sshTcpCfg.ProxyBaseConfig.Complete()
		proxies = append(proxies, sshCfg)
	}

	return proxies
}

// --- utilities (from parser.go) ---

// ProxyPhaseRunning is the frp proxy phase indicating the proxy is active.
const ProxyPhaseRunning = "running"

// FormatRemoteURL builds the public URL from server address and remote port.
func FormatRemoteURL(serverAddr string, remotePort int) string {
	return fmt.Sprintf("http://%s:%d", serverAddr, remotePort)
}

// parsePortFromAddr extracts the port number from a "host:port" address string.
func parsePortFromAddr(addr string) int {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0
	}
	return port
}

// emitEvent broadcasts an frp_status event to connected WS clients.
func emitEvent(status string, data ...map[string]any) {
	mgr := ws.GetManager()
	if mgr == nil {
		return
	}
	payload := map[string]any{"status": status}
	if len(data) > 0 {
		for k, v := range data[0] {
			payload[k] = v
		}
	}
	mgr.BroadcastEvent(ws.ServerMessage{
		Type:  ws.MessageTypeEvent,
		Event: "frp_status",
		Data:  payload,
	})
}
