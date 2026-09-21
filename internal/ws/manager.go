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

	// System-resource push preference, declared by the client via a
	// "metrics_preference" message. Only meaningful while conn != nil — a
	// disconnected subscription contributes no demand (see MetricsDemand), so
	// these are not cleared on disconnect. Reset on Subscribe: a fresh
	// connection's intent is unknown, and a stale `true` would keep the
	// sampler running for a client that never re-declares.
	metricsEnabled    bool
	metricsIntervalMs int // requested push interval in ms; 0 when disabled
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

// System-resource push rate bounds. A client declares its preferred interval;
// the server clamps it so a client cannot request an absurdly fast sampler.
const (
	defaultMetricsIntervalMs = 1000  // foreground default
	minMetricsIntervalMs     = 1000  // fastest allowed
	maxMetricsIntervalMs     = 60000 // slowest allowed
)

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
	// A fresh connection's intent is unknown, so clear any system-resource push
	// preference left by the previous connection. Preserving it would let a
	// stale `enabled` keep the metrics sampler running for a client that never
	// re-declares. The client re-declares after every reconnect (it watches
	// `connected`), so the cost is at most one interval of no data.
	sub.metricsEnabled = false
	sub.metricsIntervalMs = 0
	// NOTE: eventBuffer is deliberately NOT cleared here. It holds the events
	// buffered while this subscription was disconnected (or the rolling tail of
	// events sent before a replace) and EventsHandler replays them via
	// GetBufferedEvents right after Subscribe returns. Clearing it here made
	// reconnect replay a no-op: buffered stream events (content/tool_use/done)
	// produced while the client was away were silently lost, and the frontend
	// could show a "finished" session that was still streaming.
	// bufferStart is also left untouched — it is reset after replay completes
	// in EventsHandler, so a fresh buffer window starts for the new connection.
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
	m.disconnectClient(clientID, nil)
}

// DisconnectClientIfCurrent disconnects the client ONLY when conn is still its
// current live connection. Returns true when the disconnect happened.
//
// This guards the connection-replace race: when a client reconnects, Subscribe
// installs the new connection and closes the old one. The OLD EventsHandler's
// deferred teardown then runs — potentially AFTER the new connection was
// installed. Without an identity check it would null the NEW connection's
// sub.conn and wipe the client's StreamHub session subscriptions, leaving the
// socket alive (heartbeat keeps flowing) but every stream event dropped on the
// server side — a state only a fresh subscribe / session switch can repair.
func (m *Manager) DisconnectClientIfCurrent(clientID string, conn *websocket.Conn) bool {
	return m.disconnectClient(clientID, conn)
}

func (m *Manager) disconnectClient(clientID string, conn *websocket.Conn) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	sub, ok := m.subscriptions[clientID]
	if !ok {
		return false
	}

	sub.mu.Lock()
	defer sub.mu.Unlock()
	// Identity guard: when conn is non-nil, only disconnect if it is still the
	// subscription's current connection (an old replaced handler must not wipe
	// a freshly installed one).
	if conn != nil && sub.conn != conn {
		return false
	}
	sub.conn = nil
	sub.writeMu = nil
	sub.bufferStart = time.Now() // start buffer window

	slog.Info("ws: client disconnected (subscription preserved)", "client_id", clientID)
	return true
}

// deliveryOptions parameterizes the shared single-subscription delivery core.
// It exists because telemetry ("stale immediately" data) must NOT behave like
// ordered, stateful events.
type deliveryOptions struct {
	// buffer controls whether the message enters the reconnect replay buffer
	// (maxBufferedEvents). Telemetry must be false: at 1Hz it would evict real
	// chat/task events from the 50-entry buffer within a minute.
	buffer bool
	// closeOnOverflow forces a reconnect when the async send queue is full or
	// stopped. Correct for ordered events (a dropped `done` corrupts the
	// rendered message), catastrophic for telemetry (it would tear down the
	// whole chat connection over one lost metric frame).
	closeOnOverflow bool
	// metricsWatcherOnly restricts delivery to connected clients that declared
	// interest via a "metrics_preference" message.
	metricsWatcherOnly bool
}

var (
	// replayableDelivery is the semantics every ordered event family uses.
	replayableDelivery = deliveryOptions{buffer: true, closeOnOverflow: true}
	// telemetryDelivery is for high-frequency, droppable, non-replayable data.
	telemetryDelivery = deliveryOptions{buffer: false, closeOnOverflow: false, metricsWatcherOnly: true}
)

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

// BroadcastToMetricsWatchers delivers a non-buffered, droppable telemetry event
// to every CONNECTED client that declared system-resource interest. It returns
// how many clients accepted the frame.
//
// Deliberately NOT a BroadcastEvent variant: a metric frame must never enter the
// reconnect replay buffer, and a full send queue must never close the connection
// (that would abort in-flight chat streaming to recover one stale sample).
func (m *Manager) BroadcastToMetricsWatchers(msg ServerMessage) int {
	m.mu.Lock()
	keys := make([]string, 0, len(m.subscriptions))
	for k := range m.subscriptions {
		keys = append(keys, k)
	}
	m.mu.Unlock()

	delivered := 0
	for _, key := range keys {
		if m.deliverToSubscription(key, msg, telemetryDelivery) {
			delivered++
		}
	}
	return delivered
}

// broadcastToSubscription handles event delivery for a single subscription
// using the replayable-event semantics.
func (m *Manager) broadcastToSubscription(key string, msg ServerMessage) {
	m.deliverToSubscription(key, msg, replayableDelivery)
}

// deliverToSubscription is the single delivery path. It reports whether the
// frame was handed to the connection's async writer.
//
// Lock order: m.mu is acquired and released BEFORE sub.mu is taken; never hold
// m.mu while holding sub.mu.
func (m *Manager) deliverToSubscription(key string, msg ServerMessage, opts deliveryOptions) bool {
	m.mu.Lock()
	sub, ok := m.subscriptions[key]
	m.mu.Unlock()
	if !ok {
		return false
	}

	sub.mu.Lock()
	conn := sub.conn
	writeMu := sub.writeMu

	// Telemetry only goes to clients that asked for it. A disconnected client
	// (conn == nil) has no demand, so it is skipped rather than buffered.
	if opts.metricsWatcherOnly && (conn == nil || writeMu == nil || !sub.metricsEnabled) {
		sub.mu.Unlock()
		return false
	}

	if conn == nil || writeMu == nil {
		// Client is disconnected — buffer within the replay window, if enabled.
		if opts.buffer && (sub.bufferStart.IsZero() || time.Since(sub.bufferStart) < disconnectedBufferWindow) {
			sub.bufferEvent(msg)
		}
		sub.mu.Unlock()
		return false
	}

	delivered, needClose := sub.enqueueForDelivery(key, msg, opts)
	sub.mu.Unlock()

	// Close outside the lock: CloseNow is non-blocking today, but keeping
	// connection teardown out of sub.mu avoids reintroducing a stall if it
	// ever performs a close handshake.
	if needClose {
		_ = conn.CloseNow()
	}
	return delivered
}

// enqueueForDelivery marshals and enqueues one frame on a connected
// subscription, applying the buffering and overflow policies from opts. It
// returns whether the frame was accepted and whether the connection must be
// closed for the client to reconnect. Must be called with sub.mu held.
func (s *ClientSubscription) enqueueForDelivery(key string, msg ServerMessage, opts deliveryOptions) (delivered, needClose bool) {
	// Marshal once and enqueue for the async writer. The synchronous conn.Write
	// path is gone: a slow client could hold sub.mu for up to wsWriteTimeout,
	// stalling the entire session event loop and allowing the ACP stream channel
	// to fill and drop events.
	data, err := json.Marshal(msg)
	if err != nil {
		slog.Error("ws: marshal event", "error", err, "client_id", key)
		return false, false
	}

	delivered = s.enqueueSendLocked(data)
	if !delivered && opts.closeOnOverflow {
		// Queue full or writer stopped. Force a reconnect so the client reloads
		// a consistent snapshot; dropping mid-stream events would corrupt the
		// rendered message far worse than a reconnect.
		slog.Warn("ws: send queue full or stopped, closing connection for reconnect",
			"client_id", key, "queue_cap", maxAsyncQueue)
		needClose = true
	}

	// Buffer event for reconnect replay (even on enqueue failure, so it isn't lost)
	if opts.buffer {
		s.bufferEvent(msg)
	}
	return delivered, needClose
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

// AllConnectedClientsSubscribe reports whether every connected client is
// subscribed to sessionID — i.e. nobody who is currently online could have
// missed an event for that session.
//
// Returns false when there are no connected clients at all, so callers that
// persist "events a client might have missed" still store in that case.
//
// This is deliberately per-session rather than HasDisconnectedClients: a
// browser can be connected yet not subscribed to the session an event belongs
// to (a reconnect replaced the connection before its re-subscribe landed). Such
// an event is dropped live, so asking "is any client disconnected?" wrongly
// concludes it was seen.
func (m *Manager) AllConnectedClientsSubscribe(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	connected := 0
	m.mu.Lock()
	for clientID, sub := range m.subscriptions {
		sub.mu.Lock()
		live := sub.conn != nil
		sub.mu.Unlock()
		if !live {
			continue
		}
		connected++
		if !m.hub.IsSubscribed(clientID, sessionID) {
			m.mu.Unlock()
			return false
		}
	}
	m.mu.Unlock()
	return connected > 0
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

// SetClientMetricsPreference records whether clientID wants system-resource
// pushes and at what interval. enabled=false clears the preference. Returns
// false when clientID has no subscription.
//
// conn is the caller's own connection. It is used as an identity guard: when a
// client reconnects, Subscribe installs the new connection and resets the
// preference, but the OLD connection's read loop may still be mid-dispatch and
// would otherwise write its stale declaration onto the NEW connection's
// subscription — keeping the sampler running for a declaration that came from a
// dead socket. Mirrors the identity checks in DisconnectClientIfCurrent and
// StopWriter.
//
// Lock order: m.mu → sub.mu (the only allowed direction).
func (m *Manager) SetClientMetricsPreference(clientID string, conn *websocket.Conn, enabled bool, intervalMs int) bool {
	intervalMs = normalizeMetricsInterval(enabled, intervalMs)

	m.mu.Lock()
	defer m.mu.Unlock()
	sub, ok := m.subscriptions[clientID]
	if !ok {
		return false
	}
	sub.mu.Lock()
	defer sub.mu.Unlock()
	if sub.conn != conn {
		// A newer connection replaced this one — the declaration is stale.
		return false
	}
	sub.metricsEnabled = enabled
	sub.metricsIntervalMs = intervalMs
	return true
}

// MetricsDemand reports whether any CONNECTED client wants system-resource
// pushes, and the fastest interval requested. count == 0 means "stop sampling".
// minIntervalMs is meaningful only when count > 0.
//
// A disconnected subscription is skipped even if it still carries an enabled
// preference: disconnectClient nulls conn but preserves the entry for reconnect
// replay, and a departed client must not keep the sampler running.
func (m *Manager) MetricsDemand() (count int, minIntervalMs int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sub := range m.subscriptions {
		sub.mu.Lock()
		if sub.conn != nil && sub.metricsEnabled && sub.metricsIntervalMs > 0 {
			count++
			if minIntervalMs == 0 || sub.metricsIntervalMs < minIntervalMs {
				minIntervalMs = sub.metricsIntervalMs
			}
		}
		sub.mu.Unlock()
	}
	return count, minIntervalMs
}

// normalizeMetricsInterval clamps a client-requested push interval so a client
// cannot ask the server for an absurdly fast sampler.
func normalizeMetricsInterval(enabled bool, intervalMs int) int {
	if !enabled {
		return 0
	}
	if intervalMs < minMetricsIntervalMs {
		return defaultMetricsIntervalMs
	}
	if intervalMs > maxMetricsIntervalMs {
		return maxMetricsIntervalMs
	}
	return intervalMs
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
