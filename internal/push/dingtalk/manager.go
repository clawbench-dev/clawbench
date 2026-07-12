//nolint:noctx // external SDK context handling
package dingtalk

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"clawbench/internal/model"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/client"
)

// DingtalkDB is the DB operations interface — injected from cmd/server to avoid import cycles.
type DingtalkDB interface {
	MergeConfigSubscribers(users []string)
	GetSubscribers() ([]SubscriberInfo, error)
	UpsertSubscriber(userID, conversationID, userName, source string) error
	DeleteSubscriber(userID string) error
	EnqueueMessage(userID, msgKey, msgParam string, maxRetries int) error
	GetPendingMessages(limit int) ([]OutboxMessage, error)
	MarkMessageSent(id int64) error
	MarkMessageFailed(id int64, maxRetries int) error
	CleanupOutbox()
}

// SubscriberInfo is the subscriber data transferred across the interface boundary.
type SubscriberInfo struct {
	UserID         string
	ConversationID string
	UserName       string
	Source         string
}

// OutboxMessage is the outbox message data transferred across the interface boundary.
type OutboxMessage struct {
	ID         int64
	UserID     string
	MsgKey     string
	MsgParam   string
	Status     string
	RetryCount int
	MaxRetries int
}

var db DingtalkDB

// SetDB sets the DB adapter (called from main.go after service.InitDB).
// RegisterDBAdapter is the only way to set the DB adapter — see adapter.go.
// Must be called before Manager.Start() (guaranteed by call order in main.go).

// Manager manages the DingTalk Stream connection, token, and outbox consumer.
type Manager struct {
	cfg       *model.DingTalkConfig
	streamCli *client.StreamClient
	cancel    context.CancelFunc
	done      chan struct{}
	started   bool
	startMu   sync.Mutex
}

var (
	mgrInstance *Manager
	mgrMu       sync.RWMutex
)

// NewManager creates a new DingTalk Manager.
func NewManager(cfg *model.DingTalkConfig) *Manager {
	return &Manager{
		cfg:  cfg,
		done: make(chan struct{}),
	}
}

// SetManager sets the global manager instance (called from main.go).
func SetManager(m *Manager) {
	mgrMu.Lock()
	defer mgrMu.Unlock()
	mgrInstance = m
}

// IsStarted returns whether the DingTalk manager is running.
func IsStarted() bool {
	mgrMu.RLock()
	defer mgrMu.RUnlock()
	return mgrInstance != nil && mgrInstance.started
}

// Start initializes the Stream connection and starts the outbox consumer.
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
	if err := m.startStream(ctx); err != nil {
		cancel()
		slog.Warn("dingtalk: stream connection failed", "error", err)
		// Stream failure is not fatal — outbox consumer still works for retry
	}

	// Start outbox consumer
	go m.consumeOutbox(ctx)

	// Start periodic cleanup
	go m.periodicCleanup(ctx)

	m.started = true
	slog.Info("dingtalk: manager started")
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
	if m.streamCli != nil {
		m.streamCli.Close()
		m.streamCli = nil
	}

	// Wait for goroutines to finish (with timeout)
	select {
	case <-m.done:
	case <-time.After(5 * time.Second):
		slog.Warn("dingtalk: stop timed out")
	}

	m.started = false
	slog.Info("dingtalk: manager stopped")
}

// periodicCleanup runs outbox cleanup every hour.
func (m *Manager) periodicCleanup(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			db.CleanupOutbox()
		}
	}
}
