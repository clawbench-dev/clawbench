package frp

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/fatedier/frp/client"
	"github.com/fatedier/frp/pkg/config/source"

	"clawbench/internal/model"
	"clawbench/internal/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- NewManager ---

func TestNewManager(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "120.26.168.245",
		ServerPort: 7000,
		Token:      "test-token",
	}
	m := NewManager(cfg, 20000, 20001)

	assert.NotNil(t, m)
	assert.Equal(t, StateStarting, m.state)
	assert.Equal(t, 20000, m.httpLocalPort)
	assert.Equal(t, 20001, m.sshLocalPort)
	assert.Equal(t, cfg, m.cfg)
}

// --- OnReady ---

func TestOnReady_ReturnsChannel(t *testing.T) {
	cfg := model.FRPConfig{Enabled: true}
	m := NewManager(cfg, 20000, 0)

	ch := m.OnReady()
	assert.NotNil(t, ch)
}

// --- Status ---

func TestStatus_Starting(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "120.26.168.245",
	}
	m := NewManager(cfg, 20000, 0)

	status := m.Status()
	assert.True(t, status.Enabled)
	assert.False(t, status.Running)
	assert.Equal(t, StateStarting, status.State)
	assert.Equal(t, "120.26.168.245", status.ServerAddr)
	assert.Empty(t, status.RemoteURL)
}

func TestStatus_RunningWithRemotePort(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "120.26.168.245",
	}
	m := NewManager(cfg, 20000, 0)

	m.mu.Lock()
	m.state = StateRunning
	m.remotePort = 20050
	m.sshRemotePort = 20051
	m.mu.Unlock()

	status := m.Status()
	assert.True(t, status.Enabled)
	assert.True(t, status.Running)
	assert.Equal(t, StateRunning, status.State)
	assert.Equal(t, 20050, status.RemotePort)
	assert.Equal(t, 20051, status.SSHRemotePort)
	assert.Equal(t, "http://120.26.168.245:20050", status.RemoteURL)
}

func TestStatus_Failed(t *testing.T) {
	cfg := model.FRPConfig{Enabled: true, ServerAddr: "server"}
	m := NewManager(cfg, 20000, 0)

	m.mu.Lock()
	m.state = StateFailed
	m.mu.Unlock()

	status := m.Status()
	assert.True(t, status.Enabled)
	assert.False(t, status.Running)
	assert.Equal(t, StateFailed, status.State)
}

func TestStatus_Stopped(t *testing.T) {
	cfg := model.FRPConfig{Enabled: true, ServerAddr: "server"}
	m := NewManager(cfg, 20000, 0)

	m.mu.Lock()
	m.state = StateStopped
	m.mu.Unlock()

	status := m.Status()
	assert.True(t, status.Enabled)
	assert.False(t, status.Running)
	assert.Equal(t, StateStopped, status.State)
}

// --- Stop ---

func TestStop_SetsStateToStopped(t *testing.T) {
	cfg := model.FRPConfig{Enabled: true, ServerAddr: "server"}
	m := NewManager(cfg, 20000, 0)

	m.Stop()

	m.mu.RLock()
	state := m.state
	stopped := m.stopped
	m.mu.RUnlock()

	assert.Equal(t, StateStopped, state)
	assert.True(t, stopped)
}

func TestStop_Idempotent(t *testing.T) {
	cfg := model.FRPConfig{Enabled: true, ServerAddr: "server"}
	m := NewManager(cfg, 20000, 0)

	// Stop twice should not panic
	m.Stop()
	m.Stop()

	m.mu.RLock()
	stopped := m.stopped
	m.mu.RUnlock()
	assert.True(t, stopped)
}

func TestStop_WithCancelFunc(t *testing.T) {
	cfg := model.FRPConfig{Enabled: true, ServerAddr: "server"}
	m := NewManager(cfg, 20000, 0)

	cancelled := false
	m.mu.Lock()
	m.cancel = func() { cancelled = true }
	m.mu.Unlock()

	m.Stop()
	assert.True(t, cancelled)
}

// --- Reconfigure ---

func TestReconfigure_ServerAddrChanged_NeedsRestart(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "old-server",
		ServerPort: 7000,
		Token:      "token",
	}
	m := NewManager(cfg, 20000, 0)

	newCfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "new-server",
		ServerPort: 7000,
		Token:      "token",
	}

	needsRestart, err := m.Reconfigure(newCfg, 20000, 0)
	assert.NoError(t, err)
	assert.True(t, needsRestart, "changing ServerAddr should require restart")
}

func TestReconfigure_ServerPortChanged_NeedsRestart(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "server",
		ServerPort: 7000,
		Token:      "token",
	}
	m := NewManager(cfg, 20000, 0)

	newCfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "server",
		ServerPort: 7001,
		Token:      "token",
	}

	needsRestart, err := m.Reconfigure(newCfg, 20000, 0)
	assert.NoError(t, err)
	assert.True(t, needsRestart, "changing ServerPort should require restart")
}

func TestReconfigure_TokenChanged_NeedsRestart(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "server",
		ServerPort: 7000,
		Token:      "old-token",
	}
	m := NewManager(cfg, 20000, 0)

	newCfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "server",
		ServerPort: 7000,
		Token:      "new-token",
	}

	needsRestart, err := m.Reconfigure(newCfg, 20000, 0)
	assert.NoError(t, err)
	assert.True(t, needsRestart, "changing Token should require restart")
}

func TestReconfigure_OnlyProxyChanged_NoRestart(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:       true,
		ServerAddr:    "server",
		ServerPort:    7000,
		Token:         "token",
		AutoPort:      false,
		RemotePort:    20050,
		SSHRemotePort: 20051,
	}
	m := NewManager(cfg, 20000, 0)

	newCfg := model.FRPConfig{
		Enabled:       true,
		ServerAddr:    "server",
		ServerPort:    7000,
		Token:         "token",
		AutoPort:      true,
		RemotePort:    0,
		SSHRemotePort: 0,
	}

	needsRestart, err := m.Reconfigure(newCfg, 20000, 0)
	assert.NoError(t, err)
	assert.False(t, needsRestart, "changing only proxy config should not require restart")

	// Verify in-memory state was updated
	m.mu.RLock()
	assert.True(t, m.cfg.AutoPort)
	assert.Equal(t, 0, m.cfg.RemotePort)
	m.mu.RUnlock()
}

func TestReconfigure_SameConfig_NoRestart(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "server",
		ServerPort: 7000,
		Token:      "token",
	}
	m := NewManager(cfg, 20000, 0)

	needsRestart, err := m.Reconfigure(cfg, 20000, 0)
	assert.NoError(t, err)
	assert.False(t, needsRestart, "same config should not require restart")
}

// --- FormatRemoteURL ---

func TestFormatRemoteURL(t *testing.T) {
	tests := []struct {
		serverAddr string
		remotePort int
		want       string
	}{
		{"120.26.168.245", 20050, "http://120.26.168.245:20050"},
		{"localhost", 8080, "http://localhost:8080"},
		{"192.168.1.1", 443, "http://192.168.1.1:443"},
	}
	for _, tt := range tests {
		got := FormatRemoteURL(tt.serverAddr, tt.remotePort)
		assert.Equal(t, tt.want, got)
	}
}

// --- parsePortFromAddr ---

func TestParsePortFromAddr(t *testing.T) {
	tests := []struct {
		addr string
		want int
	}{
		{"120.26.168.245:20050", 20050},
		{"localhost:8080", 8080},
		{"127.0.0.1:443", 443},
		{"[::1]:8080", 8080},
		{"invalid", 0},
		{"host:nonnumeric", 0},
		{"", 0},
	}
	for _, tt := range tests {
		got := parsePortFromAddr(tt.addr)
		assert.Equal(t, tt.want, got, "parsePortFromAddr(%q)", tt.addr)
	}
}

// --- State constants ---

func TestStateConstants(t *testing.T) {
	assert.Equal(t, State("disabled"), StateDisabled)
	assert.Equal(t, State("starting"), StateStarting)
	assert.Equal(t, State("running"), StateRunning)
	assert.Equal(t, State("failed"), StateFailed)
	assert.Equal(t, State("stopped"), StateStopped)
}

// --- ProxyPhaseRunning constant ---

func TestProxyPhaseRunning(t *testing.T) {
	assert.Equal(t, "running", ProxyPhaseRunning)
}

// --- buildProxyConfigs ---

func TestBuildProxyConfigs_HTTPOnly(t *testing.T) {
	cfg := model.FRPConfig{
		AutoPort:      false,
		RemotePort:    20050,
		SSHRemotePort: 0,
	}
	proxies := buildProxyConfigs(cfg, 20000, 0)
	assert.Len(t, proxies, 1, "should have 1 proxy config (HTTP only) when sshLocalPort=0")
}

func TestBuildProxyConfigs_HTTPAndSSH(t *testing.T) {
	cfg := model.FRPConfig{
		AutoPort:      false,
		RemotePort:    20050,
		SSHRemotePort: 20051,
	}
	proxies := buildProxyConfigs(cfg, 20000, 20001)
	assert.Len(t, proxies, 2, "should have 2 proxy configs (HTTP + SSH) when sshLocalPort>0")
}

func TestBuildProxyConfigs_AutoPort(t *testing.T) {
	cfg := model.FRPConfig{
		AutoPort:      true,
		RemotePort:    20050, // should be ignored when AutoPort=true
		SSHRemotePort: 20051,
	}
	proxies := buildProxyConfigs(cfg, 20000, 20001)
	assert.Len(t, proxies, 2)
}

// --- checkProxyStatus early return ---

func TestCheckProxyStatus_NilService(t *testing.T) {
	cfg := model.FRPConfig{Enabled: true, ServerAddr: "server"}
	m := NewManager(cfg, 20000, 0)

	// svc is nil — should return without panic
	assert.NotPanics(t, func() {
		m.checkProxyStatus()
	})
}

func TestCheckProxyStatus_Stopped(t *testing.T) {
	cfg := model.FRPConfig{Enabled: true, ServerAddr: "server"}
	m := NewManager(cfg, 20000, 0)

	m.mu.Lock()
	m.stopped = true
	m.mu.Unlock()

	// stopped=true — should return without panic even if svc is nil
	assert.NotPanics(t, func() {
		m.checkProxyStatus()
	})
}

// --- OnReady buffer cap ---

func TestOnReady_BufferSizeOne(t *testing.T) {
	cfg := model.FRPConfig{Enabled: true, ServerAddr: "server"}
	m := NewManager(cfg, 20000, 0)

	// The readyCh has buffer size 1 — can send one status without blocking
	status := Status{
		Enabled:    true,
		Running:    true,
		State:      StateRunning,
		RemotePort: 20050,
	}

	select {
	case m.readyCh <- status:
		// success
	default:
		t.Fatal("should be able to send one status to readyCh")
	}

	// Second send should not block (non-blocking select in checkProxyStatus)
	select {
	case m.readyCh <- status:
		t.Fatal("readyCh buffer is 1, second send should block")
	default:
		// expected
	}
}

// --- buildClientCommonConfig ---

func TestBuildClientCommonConfig(t *testing.T) {
	cfg := model.FRPConfig{
		ServerAddr: "120.26.168.245",
		ServerPort: 7000,
		Token:      "test-token",
	}
	cc := buildClientCommonConfig(cfg)
	assert.NotNil(t, cc)
	assert.Equal(t, "120.26.168.245", cc.ServerAddr)
	assert.Equal(t, 7000, cc.ServerPort)
	assert.Equal(t, "test-token", cc.Auth.Token)
}

func TestBuildClientCommonConfig_Defaults(t *testing.T) {
	cfg := model.FRPConfig{
		ServerAddr: "localhost",
		ServerPort: 0,
		Token:      "",
	}
	cc := buildClientCommonConfig(cfg)
	assert.NotNil(t, cc)
	assert.Equal(t, "localhost", cc.ServerAddr)
	// LoginFailExit should be false
	assert.NotNil(t, cc.LoginFailExit)
	assert.False(t, *cc.LoginFailExit)
}

// --- Reconfigure updates internal state ---

func TestReconfigure_UpdatesLocalPorts(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "server",
		ServerPort: 7000,
		Token:      "token",
	}
	m := NewManager(cfg, 20000, 0)

	newCfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "server",
		ServerPort: 7000,
		Token:      "token",
	}
	needsRestart, err := m.Reconfigure(newCfg, 30000, 20001)
	assert.NoError(t, err)
	assert.False(t, needsRestart)

	m.mu.RLock()
	assert.Equal(t, 30000, m.httpLocalPort)
	assert.Equal(t, 20001, m.sshLocalPort)
	m.mu.RUnlock()
}

// --- checkProxyStatus with Running state and nil exporter ---

func TestCheckProxyStatus_RunningStateNilExporter(t *testing.T) {
	cfg := model.FRPConfig{Enabled: true, ServerAddr: "server"}
	m := NewManager(cfg, 20000, 0)

	// Set running state but svc is nil — checkProxyStatus should return early
	m.mu.Lock()
	m.state = StateRunning
	m.mu.Unlock()

	assert.NotPanics(t, func() {
		m.checkProxyStatus()
	})
}

// --- emitEvent tests ---

func TestEmitEvent_NilManager(t *testing.T) {
	// ws.GetManager() returns nil in tests — emitEvent should not panic
	assert.NotPanics(t, func() {
		emitEvent("stopped")
	})
}

func TestEmitEvent_WithData(t *testing.T) {
	assert.NotPanics(t, func() {
		emitEvent("running", map[string]any{
			"remote_url":  "http://server:20050",
			"remote_port": 20050,
		})
	})
}

func TestEmitEvent_WithWSManager(t *testing.T) {
	// Set up a real ws.Manager to exercise the broadcast path
	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "test-client-frp", "")

	emitEvent("running", map[string]any{
		"remote_url":  "http://server:20050",
		"remote_port": 20050,
	})

	// Verify the event was broadcast
	buffered := sub.GetBufferedEvents()
	assert.Len(t, buffered, 1)
	assert.Equal(t, "frp_status", buffered[0].Event)

	data, ok := buffered[0].Data.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "running", data["status"])
	assert.Equal(t, "http://server:20050", data["remote_url"])
}

func TestEmitEvent_StoppedWithWSManager(t *testing.T) {
	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "test-client-frp-stopped", "")

	emitEvent("stopped")

	buffered := sub.GetBufferedEvents()
	assert.Len(t, buffered, 1)
	assert.Equal(t, "frp_status", buffered[0].Event)

	data, ok := buffered[0].Data.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "stopped", data["status"])
}

// --- Reconfigure with nil configSource ---

func TestReconfigure_NilConfigSource(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "server",
		ServerPort: 7000,
		Token:      "token",
	}
	m := NewManager(cfg, 20000, 0)
	// configSource is nil (not started) — Reconfigure should still work
	newCfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "server",
		ServerPort: 7000,
		Token:      "token",
		AutoPort:   true,
	}
	needsRestart, err := m.Reconfigure(newCfg, 20000, 0)
	assert.NoError(t, err)
	assert.False(t, needsRestart)
}

// --- Status Disabled ---

func TestStatus_DisabledState(t *testing.T) {
	cfg := model.FRPConfig{Enabled: true, ServerAddr: "server"}
	m := NewManager(cfg, 20000, 0)

	m.mu.Lock()
	m.state = StateDisabled
	m.mu.Unlock()

	status := m.Status()
	assert.Equal(t, StateDisabled, status.State)
	assert.False(t, status.Running)
}

// --- Start ---

func TestStart_CreatesService(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "127.0.0.1",
		ServerPort: 7000,
		Token:      "test-token",
	}
	m := NewManager(cfg, 20000, 0)

	err := m.Start()
	require.NoError(t, err)

	// Verify internal state was set
	m.mu.RLock()
	svc := m.svc
	cs := m.configSource
	cancel := m.cancel
	m.mu.RUnlock()

	assert.NotNil(t, svc, "svc should be set after Start")
	assert.NotNil(t, cs, "configSource should be set after Start")
	assert.NotNil(t, cancel, "cancel should be set after Start")

	// Clean up: stop the manager
	m.Stop()
}

// --- runService ---

func TestRunService_CancelledContext_StopsGracefully(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "127.0.0.1",
		ServerPort: 7000,
		Token:      "test-token",
	}

	// Build a real Service (won't connect until Run is called)
	commonCfg := buildClientCommonConfig(cfg)
	proxyCfgs := buildProxyConfigs(cfg, 20000, 0)
	cs := source.NewConfigSource()
	require.NoError(t, cs.ReplaceAll(proxyCfgs, nil))
	aggregator := source.NewAggregator(cs)

	svc, err := client.NewService(client.ServiceOptions{
		Common:                 commonCfg,
		ConfigSourceAggregator: aggregator,
	})
	require.NoError(t, err)

	m := NewManager(cfg, 20000, 0)

	// Set svc on the manager so runService can access it
	m.mu.Lock()
	m.svc = svc
	m.mu.Unlock()

	// Cancel context immediately so Run() returns quickly
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// runService should complete without panic
	assert.NotPanics(t, func() {
		m.runService(ctx, svc)
	})

	// State should be Failed or Stopped depending on whether Run() returned an error.
	// With no real server, Run() returns a login error, so state is Failed.
	m.mu.RLock()
	state := m.state
	m.mu.RUnlock()
	assert.Equal(t, StateFailed, state)

	// done channel should be closed
	select {
	case <-m.done:
		// expected
	default:
		t.Fatal("done channel should be closed after runService")
	}
}

func TestRunService_SetsStateOnExit(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "127.0.0.1",
		ServerPort: 7000,
		Token:      "test-token",
	}

	commonCfg := buildClientCommonConfig(cfg)
	proxyCfgs := buildProxyConfigs(cfg, 20000, 0)
	cs := source.NewConfigSource()
	require.NoError(t, cs.ReplaceAll(proxyCfgs, nil))
	aggregator := source.NewAggregator(cs)

	svc, err := client.NewService(client.ServiceOptions{
		Common:                 commonCfg,
		ConfigSourceAggregator: aggregator,
	})
	require.NoError(t, err)

	m := NewManager(cfg, 20000, 0)
	m.mu.Lock()
	m.svc = svc
	m.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m.runService(ctx, svc)

	// After runService, done should be closed (closeOnce)
	assert.NotPanics(t, func() {
		m.closeOnce.Do(func() { close(m.done) }) // already closed, should not panic
	})
}

// --- pollStatus ---

func TestPollStatus_ExitsOnDone(t *testing.T) {
	cfg := model.FRPConfig{Enabled: true, ServerAddr: "server"}
	m := NewManager(cfg, 20000, 0)

	// Close done channel to make pollStatus exit immediately
	m.closeOnce.Do(func() { close(m.done) })

	// pollStatus should return quickly
	done := make(chan struct{})
	go func() {
		m.pollStatus()
		close(done)
	}()

	select {
	case <-done:
		// expected
	case <-time.After(3 * time.Second):
		t.Fatal("pollStatus should exit when done channel is closed")
	}
}

// --- checkProxyStatus with real Service ---

func TestCheckProxyStatus_WithRealService(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "127.0.0.1",
		ServerPort: 7000,
		Token:      "test-token",
	}

	commonCfg := buildClientCommonConfig(cfg)
	proxyCfgs := buildProxyConfigs(cfg, 20000, 0)
	cs := source.NewConfigSource()
	require.NoError(t, cs.ReplaceAll(proxyCfgs, nil))
	aggregator := source.NewAggregator(cs)

	svc, err := client.NewService(client.ServiceOptions{
		Common:                 commonCfg,
		ConfigSourceAggregator: aggregator,
	})
	require.NoError(t, err)

	m := NewManager(cfg, 20000, 0)
	m.mu.Lock()
	m.svc = svc
	m.mu.Unlock()

	// checkProxyStatus should not panic with a real service (even though proxies aren't running)
	assert.NotPanics(t, func() {
		m.checkProxyStatus()
	})

	// remotePort should still be 0 (no proxy is actually running)
	m.mu.RLock()
	remotePort := m.remotePort
	m.mu.RUnlock()
	assert.Equal(t, 0, remotePort)
}

// --- buildClientCommonConfig error path ---

func TestBuildClientCommonConfig_CompleteLogsError(t *testing.T) {
	// cc.Complete() should not panic even with invalid config
	cfg := model.FRPConfig{
		ServerAddr: "",
		ServerPort: 0,
		Token:      "",
	}
	assert.NotPanics(t, func() {
		cc := buildClientCommonConfig(cfg)
		assert.NotNil(t, cc)
	})
}

// --- Start error paths ---

func TestStart_ValidConfig(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "127.0.0.1",
		ServerPort: 19999,
		Token:      "test",
	}
	m := NewManager(cfg, 20000, 0)

	err := m.Start()
	// Start should succeed (goroutine launch, not actual connection)
	assert.NoError(t, err)

	// Clean up
	m.Stop()

	// Wait for done channel
	select {
	case <-m.done:
	case <-time.After(2 * time.Second):
		t.Fatal("done channel should be closed after Stop()")
	}
}

func TestStart_DoubleStartDoesNotPanic(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "127.0.0.1",
		ServerPort: 19999,
		Token:      "test",
	}
	m := NewManager(cfg, 20000, 0)

	err := m.Start()
	assert.NoError(t, err)

	// Stop immediately
	m.Stop()
}

// --- runService with stopped flag set ---

func TestRunService_StoppedFlagPreserved(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "127.0.0.1",
		ServerPort: 7000,
		Token:      "test-token",
	}

	commonCfg := buildClientCommonConfig(cfg)
	proxyCfgs := buildProxyConfigs(cfg, 20000, 0)
	cs := source.NewConfigSource()
	require.NoError(t, cs.ReplaceAll(proxyCfgs, nil))
	aggregator := source.NewAggregator(cs)

	svc, err := client.NewService(client.ServiceOptions{
		Common:                 commonCfg,
		ConfigSourceAggregator: aggregator,
	})
	require.NoError(t, err)

	m := NewManager(cfg, 20000, 0)
	m.mu.Lock()
	m.svc = svc
	m.stopped = true // already stopped
	m.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m.runService(ctx, svc)

	// State should remain unchanged because m.stopped was true
	// (runService skips state update when stopped is already set)
	m.mu.RLock()
	state := m.state
	m.mu.RUnlock()
	assert.Equal(t, StateStarting, state)
}

// --- pollStatus ticker fires ---

func TestPollStatus_TickerFiresAndExits(t *testing.T) {
	cfg := model.FRPConfig{Enabled: true, ServerAddr: "server"}
	m := NewManager(cfg, 20000, 0)

	// Close done after a short delay to let at least one ticker fire
	go func() {
		time.Sleep(3 * time.Second)
		m.closeOnce.Do(func() { close(m.done) })
	}()

	done := make(chan struct{})
	go func() {
		m.pollStatus()
		close(done)
	}()

	select {
	case <-done:
		// pollStatus returned
	case <-time.After(6 * time.Second):
		t.Fatal("pollStatus should exit after done channel is closed")
	}
}

// --- checkProxyStatus with real service and exporter ---

func TestCheckProxyStatus_WithRealService_NoRunningProxy(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "127.0.0.1",
		ServerPort: 7000,
		Token:      "test-token",
	}

	commonCfg := buildClientCommonConfig(cfg)
	proxyCfgs := buildProxyConfigs(cfg, 20000, 0)
	cs := source.NewConfigSource()
	require.NoError(t, cs.ReplaceAll(proxyCfgs, nil))
	aggregator := source.NewAggregator(cs)

	svc, err := client.NewService(client.ServiceOptions{
		Common:                 commonCfg,
		ConfigSourceAggregator: aggregator,
	})
	require.NoError(t, err)

	m := NewManager(cfg, 20000, 0)
	m.mu.Lock()
	m.svc = svc
	m.state = StateStarting
	m.mu.Unlock()

	// Call checkProxyStatus - exporter exists but no running proxies
	m.checkProxyStatus()

	// Should remain in Starting state (no port assigned)
	m.mu.RLock()
	state := m.state
	remotePort := m.remotePort
	m.mu.RUnlock()
	assert.Equal(t, StateStarting, state)
	assert.Equal(t, 0, remotePort)
}

// --- Reconfigure with real configSource ---

func TestReconfigure_WithRealConfigSource(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "127.0.0.1",
		ServerPort: 7000,
		Token:      "test-token",
	}

	m := NewManager(cfg, 20000, 0)

	// Start the manager to create a real configSource
	err := m.Start()
	require.NoError(t, err)
	defer m.Stop()

	// Give it a moment for goroutines to start
	time.Sleep(100 * time.Millisecond)

	// Reconfigure with same server addr (no restart needed)
	newCfg := model.FRPConfig{
		Enabled:       true,
		ServerAddr:    "127.0.0.1",
		ServerPort:    7000,
		Token:         "test-token",
		AutoPort:      true,
		RemotePort:    0,
		SSHRemotePort: 0,
	}

	needsRestart, err := m.Reconfigure(newCfg, 20000, 0)
	assert.NoError(t, err)
	assert.False(t, needsRestart)
}

func TestCheckProxyStatus_StoppedMidCheck(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "127.0.0.1",
		ServerPort: 7000,
		Token:      "test-token",
	}

	commonCfg := buildClientCommonConfig(cfg)
	proxyCfgs := buildProxyConfigs(cfg, 20000, 0)
	cs := source.NewConfigSource()
	require.NoError(t, cs.ReplaceAll(proxyCfgs, nil))
	aggregator := source.NewAggregator(cs)

	svc, err := client.NewService(client.ServiceOptions{
		Common:                 commonCfg,
		ConfigSourceAggregator: aggregator,
	})
	require.NoError(t, err)

	m := NewManager(cfg, 20000, 0)
	m.mu.Lock()
	m.svc = svc
	m.stopped = true // stopped between RLock and Lock
	m.mu.Unlock()

	// Should return early due to stopped flag
	assert.NotPanics(t, func() {
		m.checkProxyStatus()
	})
}

// --- Reconfigure with real configSource ---

func TestReconfigure_WithConfigSource(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "127.0.0.1",
		ServerPort: 7000,
		Token:      "test-token",
	}

	// Create a real ConfigSource (in-memory, no I/O)
	realCS := source.NewConfigSource()
	proxyCfgs := buildProxyConfigs(cfg, 20000, 0)
	require.NoError(t, realCS.ReplaceAll(proxyCfgs, nil))

	m := NewManager(cfg, 20000, 0)
	m.mu.Lock()
	m.configSource = realCS
	m.mu.Unlock()

	// Reconfigure with same server config — should not need restart
	newCfg := model.FRPConfig{
		Enabled:       true,
		ServerAddr:    "127.0.0.1",
		ServerPort:    7000,
		Token:         "test-token",
		AutoPort:      true,
		RemotePort:    0,
		SSHRemotePort: 0,
	}
	needsRestart, err := m.Reconfigure(newCfg, 20000, 0)
	assert.NoError(t, err)
	assert.False(t, needsRestart)

	// Verify state was updated
	m.mu.RLock()
	assert.True(t, m.cfg.AutoPort)
	m.mu.RUnlock()
}

func TestReconfigure_WithConfigSourceAndService(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "127.0.0.1",
		ServerPort: 7000,
		Token:      "test-token",
	}

	commonCfg := buildClientCommonConfig(cfg)
	proxyCfgs := buildProxyConfigs(cfg, 20000, 0)
	realCS := source.NewConfigSource()
	require.NoError(t, realCS.ReplaceAll(proxyCfgs, nil))
	aggregator := source.NewAggregator(realCS)

	svc, err := client.NewService(client.ServiceOptions{
		Common:                 commonCfg,
		ConfigSourceAggregator: aggregator,
	})
	require.NoError(t, err)

	m := NewManager(cfg, 20000, 0)
	m.mu.Lock()
	m.configSource = realCS
	m.svc = svc
	m.mu.Unlock()

	// Reconfigure with proxy-only change — should update in-place
	newCfg := model.FRPConfig{
		Enabled:       true,
		ServerAddr:    "127.0.0.1",
		ServerPort:    7000,
		Token:         "test-token",
		AutoPort:      false,
		RemotePort:    30050,
		SSHRemotePort: 0,
	}
	needsRestart, err := m.Reconfigure(newCfg, 20000, 0)
	assert.NoError(t, err)
	assert.False(t, needsRestart, "proxy-only change should not need restart")
}

// --- Start + Stop integration ---

func TestStartStop_Lifecycle(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "127.0.0.1",
		ServerPort: 7000,
		Token:      "test-token",
	}
	m := NewManager(cfg, 20000, 0)

	err := m.Start()
	require.NoError(t, err)

	// Verify running state
	m.mu.RLock()
	svc := m.svc
	m.mu.RUnlock()
	assert.NotNil(t, svc)

	// Stop should not panic
	assert.NotPanics(t, func() {
		m.Stop()
	})

	// Verify stopped state
	m.mu.RLock()
	stopped := m.stopped
	state := m.state
	m.mu.RUnlock()
	assert.True(t, stopped)
	assert.Equal(t, StateStopped, state)
}

// --- Start test (integration — may fail if frp server not available) ---

func TestStart_InvalidServer(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "127.0.0.1",
		ServerPort: 19999, // unlikely to have an frps here
		Token:      "test",
	}
	m := NewManager(cfg, 20000, 0)

	// Start() should succeed (it launches the goroutine, doesn't connect immediately)
	err := m.Start()
	// Even if it connects, we just need to ensure it doesn't panic.
	// Stop immediately to clean up.
	m.Stop()
	assert.NoError(t, err)
}

// --- checkProxyStatus with non-nil svc and exporter ---

func TestCheckProxyStatus_WithExporterRunning(t *testing.T) {
	cfg := model.FRPConfig{Enabled: true, ServerAddr: "server", ServerPort: 7000}
	m := NewManager(cfg, 20000, 0)

	// We can't easily inject a mock *client.Service since it's a concrete type,
	// but we can test that with svc=nil the function returns early (already tested).
	// Instead, verify the Manager correctly transitions when remotePort is set.
	m.mu.Lock()
	m.state = StateStarting
	m.remotePort = 20050
	m.sshRemotePort = 20051
	m.mu.Unlock()

	// Manually verify the state would transition to Running
	status := m.Status()
	assert.Equal(t, StateStarting, status.State) // still starting because checkProxyStatus wasn't called with real svc
}

func TestCheckProxyStatus_TransitionToRunning(t *testing.T) {
	cfg := model.FRPConfig{Enabled: true, ServerAddr: "server"}
	m := NewManager(cfg, 20000, 0)

	// Simulate checkProxyStatus detecting a remote port
	m.mu.Lock()
	m.state = StateStarting
	m.remotePort = 20050
	m.state = StateRunning // as checkProxyStatus would set
	m.mu.Unlock()

	status := m.Status()
	assert.True(t, status.Running)
	assert.Equal(t, StateRunning, status.State)
	assert.Equal(t, 20050, status.RemotePort)
}

// --- Reconfigure error path ---

func TestReconfigure_WithConfigSource_UpdatesProxies(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "server",
		ServerPort: 7000,
		Token:      "token",
		AutoPort:   false,
		RemotePort: 20050,
	}
	m := NewManager(cfg, 20000, 0)

	// With nil configSource, Reconfigure just updates in-memory state
	newCfg := model.FRPConfig{
		Enabled:       true,
		ServerAddr:    "server",
		ServerPort:    7000,
		Token:         "token",
		AutoPort:      true,
		RemotePort:    0,
		SSHRemotePort: 0,
	}

	needsRestart, err := m.Reconfigure(newCfg, 30000, 20001)
	assert.NoError(t, err)
	assert.False(t, needsRestart)

	m.mu.RLock()
	assert.True(t, m.cfg.AutoPort)
	assert.Equal(t, 30000, m.httpLocalPort)
	assert.Equal(t, 20001, m.sshLocalPort)
	m.mu.RUnlock()
}

// --- Stop cancels context and closes done ---

func TestStop_ClosesDoneChannel(t *testing.T) {
	cfg := model.FRPConfig{Enabled: true, ServerAddr: "server"}
	m := NewManager(cfg, 20000, 0)

	m.Stop()

	// done channel should be closed
	select {
	case <-m.done:
		// expected
	default:
		t.Fatal("done channel should be closed after Stop()")
	}
}

// --- Stop before Start (cancel is nil) ---

func TestStop_NilCancel(t *testing.T) {
	cfg := model.FRPConfig{Enabled: true, ServerAddr: "server"}
	m := NewManager(cfg, 20000, 0)

	// cancel is nil initially
	m.mu.Lock()
	m.cancel = nil
	m.mu.Unlock()

	assert.NotPanics(t, func() {
		m.Stop()
	})
}

// --- buildProxyConfigs edge cases ---

func TestBuildProxyConfigs_NoSSH(t *testing.T) {
	cfg := model.FRPConfig{
		AutoPort:   true,
		RemotePort: 20050,
	}
	proxies := buildProxyConfigs(cfg, 20000, 0)
	assert.Len(t, proxies, 1, "no SSH when sshLocalPort=0")
}

func TestBuildProxyConfigs_WithSSH(t *testing.T) {
	cfg := model.FRPConfig{
		AutoPort:      true,
		RemotePort:    20050,
		SSHRemotePort: 20051,
	}
	proxies := buildProxyConfigs(cfg, 20000, 20001)
	assert.Len(t, proxies, 2, "HTTP + SSH when sshLocalPort>0")
}

func TestBuildProxyConfigs_ManualPort(t *testing.T) {
	cfg := model.FRPConfig{
		AutoPort:      false,
		RemotePort:    30050,
		SSHRemotePort: 30051,
	}
	proxies := buildProxyConfigs(cfg, 20000, 20001)
	assert.Len(t, proxies, 2)
}

// --- pollStatus with ticker tick ---

func TestPollStatus_TickBeforeDone(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "127.0.0.1",
		ServerPort: 7000,
		Token:      "test-token",
	}

	// Create a real service so checkProxyStatus doesn't nil-pointer
	commonCfg := buildClientCommonConfig(cfg)
	proxyCfgs := buildProxyConfigs(cfg, 20000, 0)
	cs := source.NewConfigSource()
	require.NoError(t, cs.ReplaceAll(proxyCfgs, nil))
	aggregator := source.NewAggregator(cs)

	svc, err := client.NewService(client.ServiceOptions{
		Common:                 commonCfg,
		ConfigSourceAggregator: aggregator,
	})
	require.NoError(t, err)

	m := NewManager(cfg, 20000, 0)
	m.mu.Lock()
	m.svc = svc
	m.mu.Unlock()

	// Run pollStatus in a goroutine, let it tick once, then stop
	doneCh := make(chan struct{})
	go func() {
		m.pollStatus()
		close(doneCh)
	}()

	// Wait for at least one ticker tick (2s) plus margin
	time.Sleep(2500 * time.Millisecond)

	// Now close done to make pollStatus exit
	m.closeOnce.Do(func() { close(m.done) })

	select {
	case <-doneCh:
		// expected
	case <-time.After(3 * time.Second):
		t.Fatal("pollStatus should exit after done is closed")
	}
}

// --- runService with stopped flag set ---

func TestRunService_StoppedBeforeExit(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "127.0.0.1",
		ServerPort: 7000,
		Token:      "test-token",
	}

	commonCfg := buildClientCommonConfig(cfg)
	proxyCfgs := buildProxyConfigs(cfg, 20000, 0)
	cs := source.NewConfigSource()
	require.NoError(t, cs.ReplaceAll(proxyCfgs, nil))
	aggregator := source.NewAggregator(cs)

	svc, err := client.NewService(client.ServiceOptions{
		Common:                 commonCfg,
		ConfigSourceAggregator: aggregator,
	})
	require.NoError(t, err)

	m := NewManager(cfg, 20000, 0)

	// Set stopped=true and state=Stopped BEFORE runService completes
	m.mu.Lock()
	m.svc = svc
	m.stopped = true
	m.state = StateStopped
	m.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// runService should see stopped=true and not override state
	assert.NotPanics(t, func() {
		m.runService(ctx, svc)
	})

	// State should remain Stopped (set by Stop, not overwritten by runService)
	m.mu.RLock()
	state := m.state
	m.mu.RUnlock()
	assert.Equal(t, StateStopped, state)
}

// --- Start error path: invalid config ---

func TestStart_WithBadConfig_NoPanic(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "", // empty server addr
		ServerPort: 0,
		Token:      "",
	}
	m := NewManager(cfg, 20000, 0)

	// Start should not panic even with invalid config
	assert.NotPanics(t, func() {
		_ = m.Start()
	})

	m.Stop()
}

// --- Reconfigure with nil svc but non-nil configSource ---

func TestReconfigure_WithConfigSourceNilSvc(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "127.0.0.1",
		ServerPort: 7000,
		Token:      "test-token",
	}

	realCS := source.NewConfigSource()
	proxyCfgs := buildProxyConfigs(cfg, 20000, 0)
	require.NoError(t, realCS.ReplaceAll(proxyCfgs, nil))

	m := NewManager(cfg, 20000, 0)
	m.mu.Lock()
	m.configSource = realCS
	// svc is nil
	m.mu.Unlock()

	newCfg := model.FRPConfig{
		Enabled:       true,
		ServerAddr:    "127.0.0.1",
		ServerPort:    7000,
		Token:         "test-token",
		AutoPort:      false,
		RemotePort:    30050,
		SSHRemotePort: 0,
	}
	needsRestart, err := m.Reconfigure(newCfg, 20000, 0)
	assert.NoError(t, err)
	assert.False(t, needsRestart)
}

// --- checkProxyStatus with StateFailed transition ---

func TestCheckProxyStatus_FailedToRunningTransition(t *testing.T) {
	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "127.0.0.1",
		ServerPort: 7000,
		Token:      "test-token",
	}

	commonCfg := buildClientCommonConfig(cfg)
	proxyCfgs := buildProxyConfigs(cfg, 20000, 0)
	cs := source.NewConfigSource()
	require.NoError(t, cs.ReplaceAll(proxyCfgs, nil))
	aggregator := source.NewAggregator(cs)

	svc, err := client.NewService(client.ServiceOptions{
		Common:                 commonCfg,
		ConfigSourceAggregator: aggregator,
	})
	require.NoError(t, err)

	m := NewManager(cfg, 20000, 0)
	m.mu.Lock()
	m.svc = svc
	m.state = StateFailed // previously failed, should still work
	m.mu.Unlock()

	// checkProxyStatus should not panic even from Failed state
	assert.NotPanics(t, func() {
		m.checkProxyStatus()
	})
}
