package wechat

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/push/common"

	ilink "github.com/openilink/openilink-sdk-go"
)

var db DB

// clientChecker is set once at startup via RegisterClientChecker before any
// push events occur, then read-only. Safe for concurrent reads after
// initialization.
var clientChecker common.ConnectedClientChecker

// sessionMessenger is set once at startup via RegisterSessionMessenger before
// Manager.Start(), then read-only.
var sessionMessenger common.SessionMessenger

// Manager owns the iLink client, the long-poll monitor and the connection
// status the settings panel displays.
type Manager struct {
	cfg        *model.WeChatConfig
	httpClient *http.Client

	startMu sync.Mutex
	client  *ilink.Client
	cancel  context.CancelFunc
	started bool
	// gen increments on every Start. A monitor goroutine captures the value it
	// was started with and only mutates manager state while it still matches —
	// otherwise a monitor dying from an old token could mark a freshly
	// restarted manager as stopped.
	gen uint64

	statusMu   sync.Mutex
	expired    bool
	lastErr    string
	lastPollAt time.Time
}

var (
	mgrInstance *Manager
	mgrMu       sync.RWMutex
)

// NewManager creates a new WeChat Manager.
func NewManager(cfg *model.WeChatConfig) *Manager {
	return &Manager{
		cfg: cfg,
		// No global Timeout: it would apply to every request and silently cap
		// the SDK's own per-request budgets, which are larger on purpose —
		// 35s for the long poll and 60s for a CDN download. The SDK already
		// derives a context with the right deadline for each call.
		httpClient: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:       10,
				IdleConnTimeout:    90 * time.Second,
				DisableCompression: false,
			},
		},
	}
}

// GetManager returns the global manager instance (for hot-reload).
func GetManager() *Manager {
	mgrMu.RLock()
	defer mgrMu.RUnlock()
	return mgrInstance
}

// SetManager sets the global manager instance (called from main.go).
func SetManager(m *Manager) {
	mgrMu.Lock()
	defer mgrMu.Unlock()
	mgrInstance = m
}

// SetStartedForTest sets the started flag for testing purposes.
// Production code must not use this.
func (m *Manager) SetStartedForTest(started bool) {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	m.started = started
}

// IsStarted reports whether the WeChat monitor is running.
func IsStarted() bool {
	mgrMu.RLock()
	defer mgrMu.RUnlock()
	return mgrInstance != nil && mgrInstance.isStarted()
}

func (m *Manager) isStarted() bool {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	return m.started
}

// IsStarted reports whether this manager's monitor is running.
//
// Exported for hot-reload: a manager that retired after a -14 expiry must be
// rebuilt even when the credentials are unchanged.
func (m *Manager) IsStarted() bool { return m.isStarted() }

// GetPushMode returns the current push_mode from the global ConfigInstance.
// Values: "native" (default), "dingtalk", "feishu", "wechat", "disabled".
func GetPushMode() string {
	mode := model.ConfigInstance.PushMode
	if mode == "" {
		return "native"
	}
	return mode
}

// ReconfigureResult indicates whether the manager needs to be fully restarted.
type ReconfigureResult struct {
	NeedsRestart bool
}

// Reconfigure reports whether the new config requires a full restart.
//
// Unlike DingTalk/Feishu there is nothing to update in place: the only mutable
// state is the credential set, and a new token means a new client (the SDK
// binds the token at construction). So this is a pure comparison.
func (m *Manager) Reconfigure(cfg *model.WeChatConfig) ReconfigureResult {
	m.startMu.Lock()
	defer m.startMu.Unlock()

	if m.cfg.BotToken != cfg.BotToken || m.cfg.BotID != cfg.BotID || m.cfg.BaseURL != cfg.BaseURL {
		return ReconfigureResult{NeedsRestart: true}
	}
	m.cfg.UserID = cfg.UserID
	return ReconfigureResult{NeedsRestart: false}
}

// Status is the connection state the settings panel renders.
type Status struct {
	State      string // "stopped" | "running" | "expired"
	BotID      string
	UserID     string
	BaseURL    string
	HasToken   bool
	LastError  string
	LastPollAt time.Time
}

// Status snapshots the current connection state.
func (m *Manager) Status() Status {
	// cfg fields are written by Reconfigure under startMu, so they must be read
	// under it too — a torn string header would otherwise be possible once
	// anything in production calls this.
	m.startMu.Lock()
	cfg := m.cfg
	st := Status{
		BotID:    cfg.BotID,
		UserID:   cfg.UserID,
		BaseURL:  cfg.BaseURL,
		HasToken: cfg.BotToken != "",
	}
	started := m.started
	m.startMu.Unlock()

	m.statusMu.Lock()
	st.LastError = m.lastErr
	st.LastPollAt = m.lastPollAt
	expired := m.expired
	m.statusMu.Unlock()

	switch {
	case expired:
		st.State = "expired"
	case started:
		st.State = "running"
	default:
		st.State = "stopped"
	}
	return st
}

func (m *Manager) setLastError(err error) {
	if err == nil {
		return
	}
	m.statusMu.Lock()
	m.lastErr = err.Error()
	m.lastPollAt = time.Now()
	m.statusMu.Unlock()
}

// markExpired records the -14 state, but only if the calling monitor is still
// the current generation.
func (m *Manager) markExpired(gen uint64) {
	m.startMu.Lock()
	current := m.gen == gen
	m.startMu.Unlock()
	if !current {
		return
	}
	m.statusMu.Lock()
	m.expired = true
	m.lastErr = "wechat session expired (-14); re-scan the QR code"
	m.statusMu.Unlock()
}

// retire clears the started flag for a monitor that has exited, but only if it
// is still the current generation.
func (m *Manager) retire(gen uint64) {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	if m.gen != gen {
		return
	}
	m.started = false
}

// cancelCurrent cancels the monitor context of the given generation only, so a
// stale monitor cannot tear down a newer one.
func (m *Manager) cancelCurrent(gen uint64) {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	if m.gen != gen {
		return
	}
	if m.cancel != nil {
		m.cancel()
	}
}

// Start connects the monitor.
//
// Missing credentials are not an error: the channel is simply not logged in
// yet, and the settings panel offers the QR scan. Logging a warning and
// returning nil keeps startup idempotent and lets hot-reload retry later.
func (m *Manager) Start() error {
	m.startMu.Lock()
	defer m.startMu.Unlock()

	if m.started {
		return nil
	}

	if m.cfg.BotToken == "" {
		slog.Warn("wechat: no bot token (not logged in), skipping")
		return nil
	}

	if db == nil {
		slog.Warn("wechat: DB adapter not set, skipping")
		return nil
	}

	opts := []ilink.Option{ilink.WithHTTPClient(apiErrorDoer{inner: m.httpClient})}
	if m.cfg.BaseURL != "" {
		opts = append(opts, ilink.WithBaseURL(m.cfg.BaseURL))
	}
	client := ilink.NewClient(m.cfg.BotToken, opts...)

	ctx, cancel := context.WithCancel(context.Background())
	m.client = client
	m.cancel = cancel
	m.gen++
	gen := m.gen
	m.started = true

	m.statusMu.Lock()
	m.expired = false
	m.lastErr = ""
	m.statusMu.Unlock()

	go m.runMonitor(ctx, client, gen)

	slog.Info("wechat: manager started", "bot_id", m.cfg.BotID)
	return nil
}

// Stop tears the monitor down.
//
// It does not join the monitor goroutine: the goroutine exits at its next
// ctx.Done() check, and waiting for it while holding startMu is the deadlock to
// avoid. Any in-flight inbound media download is aborted by the same cancel.
func (m *Manager) Stop() {
	m.startMu.Lock()
	defer m.startMu.Unlock()

	if !m.started {
		return
	}
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.client = nil
	m.started = false
	m.gen++ // invalidate the exiting monitor's generation

	// Clear the expiry marker: Stop is the logout / mode-switch path, where the
	// channel is deliberately down rather than broken. The -14 path does not go
	// through Stop (it only cancels the monitor context), so the expired state
	// stays visible there until a re-scan.
	m.statusMu.Lock()
	m.expired = false
	m.statusMu.Unlock()

	slog.Info("wechat: manager stopped")
}
