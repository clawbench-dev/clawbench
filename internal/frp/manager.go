package frp

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"log/slog"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/ws"
)

// State represents the FRP tunnel lifecycle state.
type State string

const (
	StateDisabled   State = "disabled"   // FRP not configured
	StateStarting   State = "starting"   // frpc started, waiting for port assignment
	StateRunning    State = "running"    // tunnel established, remote port known
	StateRestarting State = "restarting" // frpc crashed, retrying with backoff
	StateFailed     State = "failed"     // max retries exceeded
	StateStopped    State = "stopped"    // user stopped the tunnel
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

// Manager manages the frpc subprocess lifecycle.
type Manager struct {
	cfg           model.FRPConfig
	binaryPath    string
	httpLocalPort int
	sshLocalPort  int

	mu            sync.RWMutex
	cmd           *exec.Cmd
	state         State
	remotePort    int
	sshRemotePort int

	readyCh  chan Status // closed when port is assigned (OnReady)
	done     chan struct{} // closed when manager stops
	stopped  bool         // user called Stop()

	retryCount int
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration
}

// NewManager creates a new FRP manager. Call Start() to launch frpc.
func NewManager(cfg model.FRPConfig, binaryPath string, httpLocalPort, sshLocalPort int) *Manager {
	return &Manager{
		cfg:           cfg,
		binaryPath:    binaryPath,
		httpLocalPort: httpLocalPort,
		sshLocalPort:  sshLocalPort,
		state:         StateStarting,
		readyCh:       make(chan Status, 1),
		done:          make(chan struct{}),
		maxRetries:    10,
		baseDelay:     1 * time.Second,
		maxDelay:      30 * time.Second,
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

// Start launches the frpc subprocess. Returns error on immediate failure.
func (m *Manager) Start() error {
	if err := m.startProcess(); err != nil {
		m.mu.Lock()
		m.state = StateFailed
		m.mu.Unlock()
		return fmt.Errorf("frpc start failed: %w", err)
	}

	// Monitor goroutine: handles crash + exponential backoff restart
	go m.monitor()

	return nil
}

// Stop gracefully stops the frpc subprocess.
func (m *Manager) Stop() {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	m.stopped = true
	m.state = StateStopped
	m.mu.Unlock()

	// Close done channel FIRST to signal monitor goroutine to exit
	// before we reap the process, avoiding a double-Wait() race.
	close(m.done)

	m.killProcess()

	emitEvent("stopped")
}

// startProcess starts (or restarts) the frpc subprocess.
func (m *Manager) startProcess() error {
	// Generate frpc.toml config file
	configContent, err := GenerateConfig(m.cfg, m.httpLocalPort, m.sshLocalPort)
	if err != nil {
		return fmt.Errorf("generate frpc config: %w", err)
	}
	configDir := filepath.Join(model.DataDir, "frp")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	configPath := filepath.Join(configDir, "frpc.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		return fmt.Errorf("write frpc.toml: %w", err)
	}

	// Build command
	cmd := exec.Command(m.binaryPath, "-c", configPath)
	cmd.Env = append(os.Environ(), ai.OrphanChildEnvVar)
	setProcessGroup(cmd)

	// Capture stdout for parsing
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	// Merge stderr into stdout for unified parsing
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("frpc start: %w", err)
	}

	m.mu.Lock()
	m.cmd = cmd
	if !m.stopped {
		m.state = StateStarting
	}
	m.mu.Unlock()

	slog.Info("frp: frpc started", slog.Int("pid", cmd.Process.Pid), slog.String("config", configPath))

	// Parse stdout in background
	go m.parseStdout(stdout)

	return nil
}

// parseStdout reads frpc stdout line by line and extracts port assignments.
func (m *Manager) parseStdout(pipe io.Reader) {
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		line := scanner.Text()
		slog.Debug("frp: frpc stdout", slog.String("line", line))

		ev := ParseLine(line)
		if ev == nil {
			continue
		}

		switch ev.Type {
		case "port_assigned":
			m.mu.Lock()
			if ev.ProxyName == "clawbench-http" {
				m.remotePort = ev.RemotePort
			} else if ev.ProxyName == "clawbench-ssh" {
				m.sshRemotePort = ev.RemotePort
			}

			// Once HTTP port is assigned, we're running
			if m.remotePort > 0 && (m.state == StateStarting || m.state == StateRestarting) {
				m.state = StateRunning
				m.retryCount = 0

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
			m.mu.Unlock()

		case "proxy_start":
			slog.Info("frp: proxy started", slog.String("proxy", ev.ProxyName))
		}
	}

	if err := scanner.Err(); err != nil {
		slog.Warn("frp: stdout scanner error", slog.String("err", err.Error()))
	}
}

// monitor waits for frpc to exit and restarts with exponential backoff.
func (m *Manager) monitor() {
	for {
		m.mu.RLock()
		cmd := m.cmd
		m.mu.RUnlock()

		if cmd == nil {
			return
		}

		exitErr := cmd.Wait()
		slog.Warn("frp: frpc exited", slog.String("err", func() string {
			if exitErr != nil {
				return exitErr.Error()
			}
			return "exit code 0"
		}()))

		m.mu.Lock()
		if m.stopped {
			m.mu.Unlock()
			return
		}

		m.retryCount++
		if m.retryCount > m.maxRetries {
			m.state = StateFailed
			m.mu.Unlock()
			slog.Error("frp: max retries exceeded, giving up", slog.Int("retries", m.retryCount))
			emitEvent("failed", map[string]any{"error": "max retries exceeded"})
			return
		}

		// Exponential backoff: 1s, 2s, 4s, 8s, 16s, 30s, 30s, ...
		delay := m.baseDelay * time.Duration(1<<(m.retryCount-1))
		if delay > m.maxDelay {
			delay = m.maxDelay
		}
		m.state = StateRestarting
		m.mu.Unlock()

		slog.Info("frp: restarting frpc", slog.Int("retry", m.retryCount), slog.Duration("delay", delay))

		emitEvent("restarting", map[string]any{
			"retry":         m.retryCount,
			"max_retries":   m.maxRetries,
			"next_delay_ms": delay.Milliseconds(),
		})

		// Wait with cancellation
		select {
		case <-m.done:
			return
		case <-time.After(delay):
		}

		if err := m.startProcess(); err != nil {
			slog.Error("frp: restart failed", slog.String("err", err.Error()))
		}
	}
}

// killProcess kills the frpc process group.
func (m *Manager) killProcess() {
	m.mu.Lock()
	cmd := m.cmd
	m.cmd = nil
	m.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return
	}

	killProcessGroup(cmd.Process.Pid)
	_ = cmd.Process.Kill()
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
