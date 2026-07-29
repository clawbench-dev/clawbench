package dingtalk

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/push/common"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/client"
)

var db common.PushDB

// clientChecker is set once at startup via RegisterClientChecker before any
// push events occur, then read-only. Safe for concurrent reads after initialization.
var clientChecker common.ConnectedClientChecker

// RegisterClientChecker sets the client checker (called from main.go).
func RegisterClientChecker(c common.ConnectedClientChecker) { clientChecker = c }

// sessionMessenger is set once at startup via RegisterSessionMessenger before Manager.Start(),
// then read-only. Safe for concurrent reads after initialization.
var sessionMessenger common.SessionMessenger

// RegisterSessionMessenger sets the session messenger (called from main.go).
func RegisterSessionMessenger(m common.SessionMessenger) { sessionMessenger = m }

// SetDB sets the DB adapter (called from main.go after service.InitDB).
// RegisterDBAdapter is the only way to set the DB adapter — see adapter.go.
// Must be called before Manager.Start() (guaranteed by call order in main.go).

// Manager manages the DingTalk Stream connection and token.
type Manager struct {
	cfg        *model.DingTalkConfig
	streamCli  *client.StreamClient
	cancel     context.CancelFunc
	started    bool
	startMu    sync.Mutex
	httpClient *http.Client

	// Per-instance token cache (C2 fix: avoids race during hot-reload credential change)
	tokenMu     sync.RWMutex
	cachedToken string
	cachedExp   time.Time
}

var (
	mgrInstance *Manager
	mgrMu       sync.RWMutex
)

// NewManager creates a new DingTalk Manager.
func NewManager(cfg *model.DingTalkConfig) *Manager {
	return &Manager{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
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

// IsStarted returns whether the DingTalk manager is running.
func IsStarted() bool {
	mgrMu.RLock()
	defer mgrMu.RUnlock()
	return mgrInstance != nil && mgrInstance.started
}

// GetPushMode returns the current push_mode from the global ConfigInstance.
// Values: "native" (default), "dingtalk", "disabled".
func GetPushMode() string {
	mode := model.ConfigInstance.PushMode
	if mode == "" {
		return "native"
	}
	return mode
}

// ReconfigureResult indicates whether the manager needs to be fully restarted.
type ReconfigureResult struct {
	NeedsRestart bool // true = caller must Stop this Manager + create new one
}

// Reconfigure updates in-place config fields (agent_id, users) or
// signals that a full restart is needed (enabled/credentials changed).
// Thread-safe: acquires startMu.
func (m *Manager) Reconfigure(cfg *model.DingTalkConfig) ReconfigureResult {
	m.startMu.Lock()
	defer m.startMu.Unlock()

	// Credentials or enabled changed — these require full restart
	if m.cfg.AppKey != cfg.AppKey || m.cfg.AppSecret != cfg.AppSecret || m.cfg.Enabled != cfg.Enabled {
		return ReconfigureResult{NeedsRestart: true}
	}

	// In-place update: agent_id, users
	m.cfg.AgentID = cfg.AgentID
	m.cfg.Users = cfg.Users

	// Merge updated config users into DB
	if db != nil {
		db.MergeConfigSubscribers(m.cfg.Users)
	}

	return ReconfigureResult{NeedsRestart: false}
}

// Start initializes the Stream connection.
func (m *Manager) Start() error {
	m.startMu.Lock()
	defer m.startMu.Unlock()

	if m.started {
		return nil
	}

	if m.cfg.AppKey == "" || m.cfg.AppSecret == "" {
		slog.Warn("dingtalk: missing app_key or app_secret, skipping")
		return nil
	}

	if db == nil {
		slog.Warn("dingtalk: DB adapter not set, skipping")
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	// Merge config users into DB
	db.MergeConfigSubscribers(m.cfg.Users)

	// Start Stream connection (non-blocking)
	var streamErr error
	if err := m.startStream(ctx); err != nil {
		streamErr = err
		slog.Warn("dingtalk: stream connection failed", "error", err)
		// Stream failure is not fatal
	}

	m.started = true
	slog.Info("dingtalk: manager started")
	return streamErr
}

// Stop gracefully shuts down the manager.
func (m *Manager) Stop() {
	m.startMu.Lock()
	defer m.startMu.Unlock()

	if !m.started {
		return
	}

	if m.cancel != nil {
		m.cancel()
	}
	if m.streamCli != nil {
		m.streamCli.Close()
		m.streamCli = nil
	}

	// Invalidate cached token so a new Manager with different credentials
	// won't reuse a stale token.
	m.invalidateToken()

	m.started = false
	slog.Info("dingtalk: manager stopped")
}
