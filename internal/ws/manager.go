package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"

	"clawbench/internal/model"
)

// ClientSubscription tracks a single client's WS connection state.
type ClientSubscription struct {
	mu          sync.Mutex
	conn        *websocket.Conn
	writeMu     *sync.Mutex // shared with EventsHandler for serialized writes
	clientID    string      // identifies the client device (for logging)
	locale      string      // user's preferred locale (for i18n)
	lastActive  time.Time
	eventBuffer []ServerMessage
	bufferStart time.Time
}

// maxSubscriptions limits the number of concurrent WS subscriptions to prevent
// resource exhaustion. Matches the original SSE limit of 20.
const maxSubscriptions = 20

// pushAlertMaxRunes is an alias for model.ResponsePreviewMaxRunes for local use.
const pushAlertMaxRunes = model.ResponsePreviewMaxRunes

// wsWriteTimeout is the maximum time to wait for a WebSocket write to complete.
const wsWriteTimeout = 5 * time.Second

// disconnectedBufferWindow is the duration after disconnection during which
// events are still buffered for replay. After this window, events are dropped.
const disconnectedBufferWindow = 10 * time.Second

// maxBufferedEvents is the maximum number of events retained in the replay
// buffer for WS reconnection.
const maxBufferedEvents = 50

// staleTimeout is the duration after which a disconnected subscription
// is cleaned up.
const staleTimeout = 120 * time.Second

// Manager manages all client subscriptions.
type Manager struct {
	mu            sync.Mutex
	subscriptions map[string]*ClientSubscription // keyed by clientID
}

var (
	defaultManager     *Manager
	defaultManagerOnce sync.Once
)

// SetManagerForTest sets the global manager for testing. Do not use in production.
func SetManagerForTest(m *Manager) {
	defaultManager = m
}

// NewManagerForTest creates a new Manager for testing.
func NewManagerForTest() *Manager {
	return &Manager{
		subscriptions: make(map[string]*ClientSubscription),
	}
}

func InitManager() {
	defaultManagerOnce.Do(func() {
		defaultManager = &Manager{
			subscriptions: make(map[string]*ClientSubscription),
		}
	})
}

func GetManager() *Manager {
	return defaultManager
}

// Subscribe registers a new WS connection for a client identified by clientID.
// If a subscription with the same clientID already exists, its connection is replaced.
func (m *Manager) Subscribe(conn *websocket.Conn, writeMu *sync.Mutex, clientID, locale string) *ClientSubscription {
	m.mu.Lock()

	// Check subscription limit (existing clientID reconnect is allowed)
	if _, exists := m.subscriptions[clientID]; !exists && len(m.subscriptions) >= maxSubscriptions {
		m.mu.Unlock()
		_ = conn.Close(websocket.StatusPolicyViolation, "too many subscriptions")
		slog.Warn("ws: subscription rejected, limit reached", "limit", maxSubscriptions, "client_id", clientID)
		return nil
	}

	sub, ok := m.subscriptions[clientID]
	if !ok {
		sub = &ClientSubscription{clientID: clientID}
		m.subscriptions[clientID] = sub
	}

	sub.mu.Lock()
	// Save existing connection to close after releasing locks
	oldConn := sub.conn
	sub.conn = conn
	sub.writeMu = writeMu
	sub.locale = locale
	sub.lastActive = time.Now()
	sub.eventBuffer = nil
	sub.bufferStart = time.Time{}
	sub.mu.Unlock()

	m.mu.Unlock()

	// Close old connection outside of locks to avoid blocking on slow networks
	if oldConn != nil {
		_ = oldConn.Close(websocket.StatusNormalClosure, "replaced")
	}

	slog.Info("ws: client subscribed", "client_id", clientID)
	return sub
}

// DisconnectClient handles WS disconnection for a specific clientID.
// This only detaches the connection — the subscription entry
// is preserved so that buffered events can be replayed on reconnect.
// Stale subscriptions are eventually cleaned up by CleanupStale.
func (m *Manager) DisconnectClient(clientID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sub, ok := m.subscriptions[clientID]
	if !ok {
		return
	}

	sub.mu.Lock()
	sub.conn = nil
	sub.writeMu = nil
	sub.bufferStart = time.Now() // start buffer window
	sub.mu.Unlock()

	slog.Info("ws: client disconnected (subscription preserved)", "client_id", clientID)
}

// BroadcastEvent sends an event to all connected clients, or buffers for replay.
// Events are fanned out to every subscription independently:
// - WS connected → send via WS (and buffer for replay)
// - WS disconnected → buffer within 10s window only
func (m *Manager) BroadcastEvent(msg ServerMessage) {
	m.mu.Lock()
	// Snapshot subscription keys to avoid holding lock during sends
	keys := make([]string, 0, len(m.subscriptions))
	for k := range m.subscriptions {
		keys = append(keys, k)
	}
	m.mu.Unlock()

	for _, key := range keys {
		m.broadcastToSubscription(key, msg)
	}
}

// broadcastToSubscription handles event delivery for a single subscription.
func (m *Manager) broadcastToSubscription(key string, msg ServerMessage) {
	m.mu.Lock()
	sub, ok := m.subscriptions[key]
	m.mu.Unlock()
	if !ok {
		return
	}

	sub.mu.Lock()
	conn := sub.conn
	writeMu := sub.writeMu

	if conn != nil && writeMu != nil {
		// Client is connected — send via WS (serialized with writeMu)
		data, err := json.Marshal(msg)
		if err != nil {
			slog.Error("ws: marshal event", "error", err, "client_id", key)
			sub.mu.Unlock()
			return
		}
		writeMu.Lock()
		ctx, cancel := context.WithTimeout(context.Background(), wsWriteTimeout)
		writeErr := conn.Write(ctx, websocket.MessageText, data)
		cancel()
		writeMu.Unlock()
		// Buffer event for reconnect replay
		sub.bufferEvent(msg)
		_ = writeErr
		sub.mu.Unlock()
		return
	}

	// Client is disconnected — check buffer window
	if sub.bufferStart.IsZero() || time.Since(sub.bufferStart) < disconnectedBufferWindow {
		sub.bufferEvent(msg)
	}

	sub.mu.Unlock()
}

// GetBufferedEvents returns buffered events for replay on reconnect.
func (s *ClientSubscription) GetBufferedEvents() []ServerMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]ServerMessage, len(s.eventBuffer))
	copy(result, s.eventBuffer)
	return result
}

// bufferEvent appends an event to the replay buffer, keeping at most maxBufferedEvents events.
func (s *ClientSubscription) bufferEvent(msg ServerMessage) {
	s.eventBuffer = append(s.eventBuffer, msg)
	if len(s.eventBuffer) > maxBufferedEvents {
		s.eventBuffer = s.eventBuffer[len(s.eventBuffer)-maxBufferedEvents:]
	}
}

// HasDisconnectedClients returns true if any subscription is disconnected
// or if there are no subscriptions at all. Used to conditionally persist
// events only when clients might miss them.
func (m *Manager) HasDisconnectedClients() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.subscriptions) == 0 {
		return true
	}
	for _, sub := range m.subscriptions {
		sub.mu.Lock()
		disconnected := sub.conn == nil
		sub.mu.Unlock()
		if disconnected {
			return true
		}
	}
	return false
}

// CleanupStale removes stale subscriptions:
//   - Disconnected for > staleTimeout → remove
//   - Connected subscriptions are never cleaned up.
func (m *Manager) CleanupStale() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, sub := range m.subscriptions {
		sub.mu.Lock()
		// Never clean up active connections
		if sub.conn != nil {
			sub.mu.Unlock()
			continue
		}
		// Must have been disconnected (bufferStart is set)
		if sub.bufferStart.IsZero() {
			sub.mu.Unlock()
			continue
		}
		// Clean up after staleTimeout
		if time.Since(sub.bufferStart) > staleTimeout {
			delete(m.subscriptions, key)
			slog.Info("ws: cleaned up stale subscription", "client_id", key, "disconnected_for", time.Since(sub.bufferStart))
		}
		sub.mu.Unlock()
	}
}

// eventSeq is an atomic counter to ensure unique event IDs within a server instance.
var eventSeq atomic.Int64

// serverInstanceID is set once at init time to ensure event IDs are unique
// across server restarts.
var serverInstanceID int64

func init() {
	serverInstanceID = time.Now().UnixMilli()
}

// truncateForPush truncates s to pushAlertMaxRunes, appending "…" if truncated.
func truncateForPush(s string) string {
	if utf8.RuneCountInString(s) <= pushAlertMaxRunes {
		return s
	}
	return string([]rune(s)[:pushAlertMaxRunes]) + "…"
}

// GenerateEventID creates a unique event ID.
// Includes the server instance ID (unix millis at startup) so IDs are unique
// across server restarts, plus an atomic counter for within-instance uniqueness.
func GenerateEventID() string {
	return fmt.Sprintf("evt_%d_%d", serverInstanceID, eventSeq.Add(1))
}
