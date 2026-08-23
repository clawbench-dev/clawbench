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

	// Async send queue: BroadcastEvent enqueues marshaled messages here and a
	// single writer goroutine (started by StartWriter) drains it to the socket.
	// This decouples the chat event loop from slow WS clients — a client that
	// can't keep up no longer stalls the producer (which previously held sub.mu
	// during a synchronous conn.Write of up to wsWriteTimeout).
	sendQueue     chan []byte     // bounded: maxAsyncQueue
	writerStarted bool            // writer goroutine started for the current connection
	writerConn    *websocket.Conn // connection the current writer was started for (identity check)
	writerStopped chan struct{}   // closed when the writer goroutine exits
}

// maxSubscriptions limits the number of concurrent WS subscriptions to prevent
// resource exhaustion. Matches the original SSE limit of 20.
const maxSubscriptions = 20

// pushAlertMaxRunes is the max rune count for push notification previews.
const pushAlertMaxRunes = model.PushPreviewMaxRunes

// wsWriteTimeout is the maximum time to wait for a WebSocket write to complete.
const wsWriteTimeout = 5 * time.Second

// maxAsyncQueue bounds the per-subscription asynchronous send queue. Events
// beyond this bound force a connection close so the client reconnects and
// reloads a consistent snapshot (order preservation beats dropping events).
// 256 comfortably covers bursts of stream events within a single flush cycle.
const maxAsyncQueue = 256

// disconnectedBufferWindow is the duration after disconnection during which
// events are still buffered for replay. After this window, events are dropped.
const disconnectedBufferWindow = 10 * time.Second

// maxBufferedEvents is the maximum number of events retained in the replay
// buffer for WS reconnection.
const maxBufferedEvents = 50

// staleTimeout is the duration after which a disconnected subscription
// is cleaned up.
const staleTimeout = 120 * time.Second

// writeMessage serializes a WebSocket write under writeMu with a timeout.
// It is the single write path used by both the event broadcast and the ping
// loop, so a write error is detected consistently in one place.
func writeMessage(writeMu *sync.Mutex, conn *websocket.Conn, data []byte) error {
	writeMu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), wsWriteTimeout)
	err := conn.Write(ctx, websocket.MessageText, data)
	cancel()
	writeMu.Unlock()
	return err
}

// Manager manages all client subscriptions.
type Manager struct {
	mu            sync.Mutex
	subscriptions map[string]*ClientSubscription // keyed by clientID
	hub           *StreamHub
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
	mgr := &Manager{
		subscriptions: make(map[string]*ClientSubscription),
	}
	mgr.hub = NewStreamHub(mgr)
	return mgr
}

func InitManager() {
	defaultManagerOnce.Do(func() {
		mgr := &Manager{
			subscriptions: make(map[string]*ClientSubscription),
		}
		mgr.hub = NewStreamHub(mgr)
		defaultManager = mgr
	})
}

func GetManager() *Manager {
	return defaultManager
}

// StreamHub returns the StreamHub for chat streaming event fan-out.
func (m *Manager) StreamHub() *StreamHub {
	return m.hub
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
	// Save existing connection and writer state to clean up after releasing locks
	oldConn := sub.conn
	oldWriterStarted := sub.writerStarted
	var oldQueue chan []byte
	var oldExit chan struct{}
	if oldWriterStarted {
		// Detach the old connection's writer NOW (under the lock) so the old
		// EventsHandler's deferred StopWriter becomes a no-op. Without this, the
		// old handler's StopWriter could run after the new connection started its
		// writer and would close the NEW queue / kill the NEW writer (the
		// writerStarted/writerStopped fields are shared across connections).
		oldQueue = sub.sendQueue
		oldExit = sub.writerStopped
		sub.writerStarted = false
		sub.writerStopped = nil
		sub.writerConn = nil
	}
	sub.conn = conn
	sub.writeMu = writeMu
	sub.locale = locale
	sub.lastActive = time.Now()
	sub.eventBuffer = nil
	sub.bufferStart = time.Time{}
	// Rebuild the async queue for this connection. The fresh queue isolates the
	// new connection from any stragglers of the old one.
	sub.sendQueue = make(chan []byte, maxAsyncQueue)
	sub.mu.Unlock()

	m.mu.Unlock()

	// Stop the old connection's writer outside the locks, so it cannot race with
	// the new connection's StartWriter. close(oldQueue) makes the old writer's
	// range-read return and it signals exit; we wait so no goroutine outlives
	// the connection it writes to.
	if oldWriterStarted {
		if oldQueue != nil {
			close(oldQueue)
		}
		if oldExit != nil {
			<-oldExit
		}
	}

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

// SendToClient sends a ServerMessage to a specific client by clientID.
// If the client is connected, sends via WS. If disconnected, buffers for replay.
func (m *Manager) SendToClient(clientID string, msg ServerMessage) {
	m.broadcastToSubscription(clientID, msg)
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
		// Client is connected — marshal once and enqueue for the async writer.
		// The synchronous conn.Write path is gone: a slow client could hold
		// sub.mu for up to wsWriteTimeout, stalling the entire session event
		// loop and allowing the ACP stream channel to fill and drop events.
		data, err := json.Marshal(msg)
		if err != nil {
			slog.Error("ws: marshal event", "error", err, "client_id", key)
			sub.mu.Unlock()
			return
		}
		needClose := false
		if !sub.enqueueSendLocked(data) {
			// Queue full or writer stopped. Force a reconnect so the client
			// reloads a consistent snapshot; dropping mid-stream events would
			// corrupt the rendered message far worse than a reconnect.
			slog.Warn("ws: send queue full or stopped, closing connection for reconnect",
				"client_id", key, "queue_cap", maxAsyncQueue)
			needClose = true
		}
		// Buffer event for reconnect replay (even on enqueue failure, so it isn't lost)
		sub.bufferEvent(msg)
		sub.mu.Unlock()

		// Close outside the lock: CloseNow is non-blocking today, but keeping
		// connection teardown out of sub.mu avoids reintroducing a stall if it
		// ever performs a close handshake.
		if needClose {
			_ = conn.CloseNow()
		}
		return
	}

	// Client is disconnected — check buffer window
	if sub.bufferStart.IsZero() || time.Since(sub.bufferStart) < disconnectedBufferWindow {
		sub.bufferEvent(msg)
	}

	sub.mu.Unlock()
}

// enqueueSendLocked enqueues a marshaled message for the async writer.
// Must be called with sub.mu held. Returns false if the queue is full or the
// writer has been stopped (sendQueue closed/nil), meaning this connection can
// no longer accept events.
func (s *ClientSubscription) enqueueSendLocked(data []byte) bool {
	if s.sendQueue == nil {
		return false
	}
	select {
	case s.sendQueue <- data:
		return true
	default:
		return false // queue full
	}
}

// StartWriter launches the single writer goroutine that drains sendQueue to the
// connection. The connection must be the subscription's current connection —
// verified under the lock so a stale handler cannot start a writer on a
// replaced (already-closed) connection. A writer is started at most once per
// connection; repeated calls are no-ops.
func (m *Manager) StartWriter(clientID string, conn *websocket.Conn, writeMu *sync.Mutex) {
	m.mu.Lock()
	sub, ok := m.subscriptions[clientID]
	m.mu.Unlock()
	if !ok {
		return
	}

	sub.mu.Lock()
	if sub.writerStarted || sub.sendQueue == nil || sub.conn != conn {
		sub.mu.Unlock()
		return
	}
	sub.writerStarted = true
	sub.writerConn = conn
	exit := make(chan struct{})
	sub.writerStopped = exit
	sendQueue := sub.sendQueue
	sub.mu.Unlock()

	go sub.writeLoop(sendQueue, exit, conn, writeMu, clientID)
}

// StopWriter gracefully stops the writer goroutine for a connection: closes the
// send queue so the writer drains what it can and exits, then waits for the
// writer to signal exit. Safe to call multiple times; the writer's exit is
// observed via writerStopped. Events enqueued after StopWriter are refused by
// enqueueSendLocked (sendQueue set to nil) and instead only enter the replay
// buffer, so nothing is lost on reconnect.
//
// conn identifies the connection this caller is responsible for. StopWriter
// only stops a writer that was started for that same connection — an old
// handler's deferred StopWriter running after a reconnect must not kill the
// new connection's writer (see Subscribe's replacement logic).
func (m *Manager) StopWriter(clientID string, conn *websocket.Conn) {
	m.mu.Lock()
	sub, ok := m.subscriptions[clientID]
	m.mu.Unlock()
	if !ok {
		return
	}

	sub.mu.Lock()
	if !sub.writerStarted || sub.writerConn != conn {
		sub.mu.Unlock()
		return
	}
	sub.writerStarted = false
	sub.writerConn = nil
	sendQueue := sub.sendQueue
	sub.sendQueue = nil
	exit := sub.writerStopped
	sub.writerStopped = nil
	sub.mu.Unlock()

	// Closing sendQueue makes the writer's range-read return ok=false and exit.
	// Guard nil defensively: a writer may have exited on its own (write failure)
	// between our lock check and here — never close a nil channel.
	if sendQueue != nil {
		close(sendQueue)
	}
	if exit != nil {
		// Wait bounded: the writer may be blocked writing to a slow/dead socket
		// for up to wsWriteTimeout. Bound the wait so connection teardown is not
		// delayed beyond that on the caller's path; the writer still exits on
		// its own once the write completes or fails.
		select {
		case <-exit:
		case <-time.After(wsWriteTimeout):
		}
	}
}

// writeLoop is the single writer goroutine for a connection. It drains sendQueue
// and writes each message via writeMessage (serialized with the shared writeMu,
// which the ping goroutine also uses). On write failure it force-closes the
// socket so the client reconnects, then exits.
func (s *ClientSubscription) writeLoop(sendQueue <-chan []byte, exit chan struct{}, conn *websocket.Conn, writeMu *sync.Mutex, clientID string) {
	defer close(exit)

	for data := range sendQueue {
		if err := writeMessage(writeMu, conn, data); err != nil {
			// Socket is dead (peer gone, buffer full, or timed out). Close it
			// so the client's onclose fires and it reconnects immediately.
			slog.Warn("ws: async write failed, closing connection", "error", err, "client_id", clientID)
			_ = conn.CloseNow()
			return
		}
	}
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

// HasConnectedClients returns true if at least one subscription has an
// active WebSocket connection. Used to suppress push notifications (e.g.
// DingTalk) when a client is already watching the UI.
func (m *Manager) HasConnectedClients() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sub := range m.subscriptions {
		sub.mu.Lock()
		connected := sub.conn != nil
		sub.mu.Unlock()
		if connected {
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
