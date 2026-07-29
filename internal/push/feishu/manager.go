package feishu

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/push/common"

	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

var db common.PushDB

// ConnectedClientChecker checks whether any client is currently connected.
// Injected from cmd/server to avoid import cycles with the ws package.
var clientChecker common.ConnectedClientChecker

// RegisterClientChecker sets the client checker (called from main.go).
func RegisterClientChecker(c common.ConnectedClientChecker) { clientChecker = c }

// sessionMessenger is set once at startup via RegisterSessionMessenger before Manager.Start(),
// then read-only. Safe for concurrent reads after initialization.
var sessionMessenger common.SessionMessenger

// RegisterSessionMessenger sets the session messenger (called from main.go).
func RegisterSessionMessenger(m common.SessionMessenger) { sessionMessenger = m }

// Manager manages the Feishu WebSocket connection and token.
type Manager struct {
	cfg        *model.FeishuConfig
	wsClient   *larkws.Client
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

// NewManager creates a new Feishu Manager.
func NewManager(cfg *model.FeishuConfig) *Manager {
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
func (m *Manager) SetStartedForTest(started bool) {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	m.started = started
}

// IsStarted returns whether the Feishu manager is running.
func IsStarted() bool {
	mgrMu.RLock()
	defer mgrMu.RUnlock()
	return mgrInstance != nil && mgrInstance.started
}

// GetPushMode returns the current push_mode from the global ConfigInstance.
// Values: "native" (default), "dingtalk", "feishu", "disabled".
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

// Reconfigure updates in-place config fields (users) or
// signals that a full restart is needed (enabled/credentials changed).
func (m *Manager) Reconfigure(cfg *model.FeishuConfig) ReconfigureResult {
	m.startMu.Lock()
	defer m.startMu.Unlock()

	// Credentials or enabled changed — these require full restart
	if m.cfg.AppID != cfg.AppID || m.cfg.AppSecret != cfg.AppSecret || m.cfg.Enabled != cfg.Enabled {
		return ReconfigureResult{NeedsRestart: true}
	}

	// In-place update: users
	m.cfg.Users = cfg.Users

	// Merge updated config users into DB
	if db != nil {
		db.MergeConfigSubscribers(m.cfg.Users)
	}

	return ReconfigureResult{NeedsRestart: false}
}

// Start initializes the WebSocket connection.
func (m *Manager) Start() error {
	m.startMu.Lock()
	defer m.startMu.Unlock()

	if m.started {
		return nil
	}

	if m.cfg.AppID == "" || m.cfg.AppSecret == "" {
		slog.Warn("feishu: missing app_id or app_secret, skipping")
		return nil
	}

	if db == nil {
		slog.Warn("feishu: DB adapter not set, skipping")
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	// Merge config users into DB
	db.MergeConfigSubscribers(m.cfg.Users)

	// Start WebSocket connection (non-blocking)
	m.startWebSocket(ctx)

	m.started = true
	slog.Info("feishu: manager started")
	return nil
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
	if m.wsClient != nil {
		m.wsClient.Close()
		m.wsClient = nil
	}

	// Invalidate cached token so a new Manager with different credentials
	// won't reuse a stale token.
	m.invalidateToken()

	m.started = false
	slog.Info("feishu: manager stopped")
}
