package ws

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
)

func newTestManager() *Manager {
	return &Manager{
		subscriptions: make(map[string]*ClientSubscription),
	}
}

func TestManager_Subscribe(t *testing.T) {
	mgr := newTestManager()
	var writeMu sync.Mutex

	sub := mgr.Subscribe(nil, &writeMu, "client-1", "")
	if sub == nil {
		t.Fatal("expected non-nil subscription")
	}

	mgr.mu.Lock()
	stored, ok := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()
	if !ok || stored != sub {
		t.Error("subscription not stored correctly")
	}
}

func TestManager_Subscribe_StoresLocale(t *testing.T) {
	mgr := newTestManager()
	var writeMu sync.Mutex

	sub := mgr.Subscribe(nil, &writeMu, "client-locale", "zh")
	if sub == nil {
		t.Fatal("expected non-nil subscription")
		return
	}

	sub.mu.Lock()
	locale := sub.locale
	sub.mu.Unlock()
	if locale != "zh" {
		t.Errorf("expected locale %q, got %q", "zh", locale)
	}
}

func TestManager_SubscribeReplacesExisting(t *testing.T) {
	mgr := newTestManager()
	var writeMu1, writeMu2 sync.Mutex

	sub1 := mgr.Subscribe(nil, &writeMu1, "client-1", "en")
	_ = sub1

	// Second subscribe with same clientID should replace the first and update locale
	sub2 := mgr.Subscribe(nil, &writeMu2, "client-1", "zh")

	mgr.mu.Lock()
	stored := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()
	if stored != sub2 {
		t.Error("expected subscription to be replaced")
	}
	sub2.mu.Lock()
	if sub2.locale != "zh" {
		t.Errorf("expected locale to be updated to %q, got %q", "zh", sub2.locale)
	}
	sub2.mu.Unlock()
}

func TestManager_SubscribeMultipleClients(t *testing.T) {
	mgr := newTestManager()
	var writeMu1, writeMu2 sync.Mutex

	sub1 := mgr.Subscribe(nil, &writeMu1, "client-1", "")
	sub2 := mgr.Subscribe(nil, &writeMu2, "client-2", "")

	// Both should exist independently
	mgr.mu.Lock()
	s1 := mgr.subscriptions["client-1"]
	s2 := mgr.subscriptions["client-2"]
	mgr.mu.Unlock()
	if s1 != sub1 {
		t.Error("client-1 subscription not stored correctly")
	}
	if s2 != sub2 {
		t.Error("client-2 subscription not stored correctly")
	}
	if len(mgr.subscriptions) != 2 {
		t.Errorf("expected 2 subscriptions, got %d", len(mgr.subscriptions))
	}
}

func TestManager_Unsubscribe(t *testing.T) {
	mgr := newTestManager()
	var writeMu sync.Mutex

	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.DisconnectClient("client-1")

	mgr.mu.Lock()
	sub, ok := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()

	if !ok {
		t.Fatal("subscription should still exist after unsubscribe")
	}
	sub.mu.Lock()
	conn := sub.conn
	sub.mu.Unlock()
	if conn != nil {
		t.Error("expected conn to be nil after unsubscribe")
	}
}

func TestManager_HasDisconnectedClients(t *testing.T) {
	m := NewManagerForTest()
	// No subscriptions → true
	if !m.HasDisconnectedClients() {
		t.Fatal("expected true with no subscriptions")
	}
}

func TestManager_HasConnectedClients(t *testing.T) {
	m := NewManagerForTest()

	// No subscriptions → false
	if m.HasConnectedClients() {
		t.Error("expected false with no subscriptions")
	}

	// Disconnected subscription → false
	var writeMu sync.Mutex
	m.Subscribe(nil, &writeMu, "disc-client", "")
	m.DisconnectClient("disc-client")
	if m.HasConnectedClients() {
		t.Error("expected false with only disconnected subscription")
	}

	// Add a connected client via real WS
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		var wmu sync.Mutex
		m.Subscribe(conn, &wmu, "conn-client", "")
		time.Sleep(500 * time.Millisecond)
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	time.Sleep(150 * time.Millisecond)

	// At least one connected client → true
	if !m.HasConnectedClients() {
		t.Error("expected true when at least one client is connected")
	}
}

func TestManager_BroadcastEvent_NoSubscription(_ *testing.T) {
	mgr := newTestManager()
	// Should not panic
	mgr.BroadcastEvent(ServerMessage{Type: "event", Event: "session_update"})
}

func TestManager_BroadcastEvent_Disconnected(t *testing.T) {
	mgr := newTestManager()
	var writeMu sync.Mutex

	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.DisconnectClient("client-1")

	// Broadcast while disconnected — should buffer
	msg := ServerMessage{Type: "event", ID: "evt_1", Event: "session_update", Data: &SessionUpdateData{SessionID: "s1", Status: "completed"}}
	mgr.BroadcastEvent(msg)

	mgr.mu.Lock()
	sub := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()

	buffered := sub.GetBufferedEvents()
	if len(buffered) != 1 {
		t.Fatalf("expected 1 buffered event, got %d", len(buffered))
	}
	if buffered[0].ID != "evt_1" {
		t.Errorf("expected buffered event ID 'evt_1', got %q", buffered[0].ID)
	}
}

func TestManager_BroadcastEvent_MultipleClients(t *testing.T) {
	mgr := newTestManager()
	var writeMu1, writeMu2 sync.Mutex

	// Two clients subscribed
	mgr.Subscribe(nil, &writeMu1, "client-1", "")
	mgr.Subscribe(nil, &writeMu2, "client-2", "")

	// Disconnect both
	mgr.DisconnectClient("client-1")
	mgr.DisconnectClient("client-2")

	// Broadcast — both should buffer the event
	msg := ServerMessage{Type: "event", ID: "evt_1", Event: "session_update", Data: &SessionUpdateData{SessionID: "s1", Status: "completed"}}
	mgr.BroadcastEvent(msg)

	mgr.mu.Lock()
	s1 := mgr.subscriptions["client-1"]
	s2 := mgr.subscriptions["client-2"]
	mgr.mu.Unlock()

	if len(s1.GetBufferedEvents()) != 1 {
		t.Errorf("client-1: expected 1 buffered event, got %d", len(s1.GetBufferedEvents()))
	}
	if len(s2.GetBufferedEvents()) != 1 {
		t.Errorf("client-2: expected 1 buffered event, got %d", len(s2.GetBufferedEvents()))
	}
}

func TestBufferEvent_MaxSize(t *testing.T) {
	sub := &ClientSubscription{}

	for i := range 60 {
		sub.bufferEvent(ServerMessage{ID: string(rune('a' + i%26))})
	}

	if len(sub.eventBuffer) > 50 {
		t.Errorf("expected at most 50 buffered events, got %d", len(sub.eventBuffer))
	}

	if len(sub.eventBuffer) == 50 {
		if sub.eventBuffer[0].ID != "k" {
			t.Logf("first buffered event ID: %q (eviction order may vary)", sub.eventBuffer[0].ID)
		}
	}
}

func TestGetBufferedEvents_Copy(t *testing.T) {
	sub := &ClientSubscription{}
	sub.bufferEvent(ServerMessage{ID: "evt_1"})

	events := sub.GetBufferedEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	// Modifying the copy should not affect the original
	events[0] = ServerMessage{ID: "modified"}
	original := sub.GetBufferedEvents()
	if original[0].ID == "modified" {
		t.Error("GetBufferedEvents should return a copy")
	}
}

func TestCleanupStale_Disconnected(t *testing.T) {
	mgr := newTestManager()
	var writeMu sync.Mutex

	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.DisconnectClient("client-1")

	// Set bufferStart to just past staleTimeout — should be cleaned up
	mgr.mu.Lock()
	sub := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()
	sub.mu.Lock()
	sub.bufferStart = time.Now().Add(-staleTimeout - time.Second)
	sub.mu.Unlock()

	mgr.CleanupStale()

	mgr.mu.Lock()
	_, exists := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()
	if exists {
		t.Error("expected stale subscription to be cleaned up after staleTimeout")
	}
}

func TestCleanupStale_RecentNotCleaned(t *testing.T) {
	mgr := newTestManager()
	var writeMu sync.Mutex

	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.DisconnectClient("client-1")

	// Set bufferStart to well before staleTimeout — should NOT be cleaned up
	mgr.mu.Lock()
	sub := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()
	sub.mu.Lock()
	sub.bufferStart = time.Now().Add(-60 * time.Second)
	sub.mu.Unlock()

	mgr.CleanupStale()

	mgr.mu.Lock()
	_, exists := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()
	if !exists {
		t.Error("expected subscription (before staleTimeout) to not be cleaned up")
	}
}

func TestCleanupStale_ActiveNotCleaned(t *testing.T) {
	mgr := newTestManager()
	var writeMu sync.Mutex

	mgr.Subscribe(nil, &writeMu, "client-1", "")
	// Not unsubscribing — conn is active, should not be cleaned

	mgr.CleanupStale()

	mgr.mu.Lock()
	_, exists := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()
	if !exists {
		t.Error("expected active subscription to not be cleaned up")
	}
}

func TestSetManagerForTest(t *testing.T) {
	orig := defaultManager
	defer func() { defaultManager = orig }()

	mgr := NewManagerForTest()
	SetManagerForTest(mgr)

	if GetManager() != mgr {
		t.Error("expected GetManager to return the test manager")
	}

	SetManagerForTest(nil)
	if GetManager() != nil {
		t.Error("expected GetManager to return nil after reset")
	}
}

func TestNewManagerForTest(t *testing.T) {
	mgr := NewManagerForTest()
	if mgr == nil {
		t.Fatal("expected non-nil manager")
		return
	}
	if len(mgr.subscriptions) != 0 {
		t.Errorf("expected empty subscriptions, got %d", len(mgr.subscriptions))
	}
}

func TestClientSubscription_GetBufferedEvents_Empty(t *testing.T) {
	sub := &ClientSubscription{}
	events := sub.GetBufferedEvents()
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestBroadcastEvent_BufferWindow(t *testing.T) {
	mgr := newTestManager()
	var writeMu sync.Mutex

	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.DisconnectClient("client-1")

	// Within buffer window (10s) — should buffer
	msg := ServerMessage{Type: "event", ID: "evt_1", Event: "session_update", Data: &SessionUpdateData{SessionID: "s1", Status: "completed"}}
	mgr.BroadcastEvent(msg)

	mgr.mu.Lock()
	sub := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()

	buffered := sub.GetBufferedEvents()
	if len(buffered) != 1 {
		t.Fatalf("expected 1 buffered event within window, got %d", len(buffered))
	}

	// Beyond buffer window — should not buffer
	sub.mu.Lock()
	sub.bufferStart = time.Now().Add(-15 * time.Second)
	sub.eventBuffer = nil
	sub.mu.Unlock()

	msg2 := ServerMessage{Type: "event", ID: "evt_2", Event: "task_update", Data: &TaskUpdateData{TaskID: "t1", Status: "completed"}}
	mgr.BroadcastEvent(msg2)

	buffered2 := sub.GetBufferedEvents()
	if len(buffered2) != 0 {
		t.Errorf("expected 0 buffered events outside window, got %d", len(buffered2))
	}
}

func TestTruncateForPush(t *testing.T) {
	short := "短文本"
	if got := truncateForPush(short); got != short {
		t.Errorf("short text should pass through, got %q", got)
	}

	// Exactly pushAlertMaxRunes — no truncation
	exact := strings.Repeat("一二", pushAlertMaxRunes/2)
	if utf8.RuneCountInString(exact) != pushAlertMaxRunes {
		t.Fatalf("test setup: expected %d runes, got %d", pushAlertMaxRunes, utf8.RuneCountInString(exact))
	}
	if got := truncateForPush(exact); got != exact {
		t.Errorf("exact-length text should not be truncated, got %q", got)
	}

	// Over pushAlertMaxRunes — truncate + "…"
	long := strings.Repeat("一二", pushAlertMaxRunes/2) + "三"
	runes := []rune(long)
	expected := string(runes[:pushAlertMaxRunes]) + "…"
	got := truncateForPush(long)
	if got != expected {
		t.Errorf("long text should be truncated, got %q (want %q)", got, expected)
	}
	if utf8.RuneCountInString(got) != pushAlertMaxRunes+1 {
		t.Errorf("truncated text should be %d+1 runes, got %d", pushAlertMaxRunes, utf8.RuneCountInString(got))
	}
}

// TestManager_Subscribe_ConnectionReplacement verifies that subscribing with
// the same clientID closes the old connection (exercises _ = oldConn.Close).
func TestManager_Subscribe_ConnectionReplacement(t *testing.T) {
	mgr := newTestManager()

	// Create a real WebSocket server for the test
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		var writeMu sync.Mutex
		sub := mgr.Subscribe(conn, &writeMu, "replace-test", "")
		if sub == nil {
			return
		}
		defer mgr.DisconnectClient("replace-test")
		readClientMessages(mgr, conn, &writeMu, "replace-test")
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	// First connection
	wsURL := "ws" + server.URL[4:]
	ctx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel1()
	conn1, _, err := websocket.Dial(ctx1, wsURL, nil)
	if err != nil {
		t.Fatalf("first connection failed: %v", err)
	}

	// Wait for subscription
	time.Sleep(100 * time.Millisecond)

	// Verify first connection is stored
	mgr.mu.Lock()
	sub := mgr.subscriptions["replace-test"]
	mgr.mu.Unlock()
	sub.mu.Lock()
	firstConn := sub.conn
	sub.mu.Unlock()
	if firstConn == nil {
		t.Fatal("expected first connection to be stored")
	}

	// Second connection with same clientID — should replace and close old conn
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	conn2, _, err := websocket.Dial(ctx2, wsURL, nil)
	if err != nil {
		t.Fatalf("second connection failed: %v", err)
	}
	defer func() { _ = conn2.Close(websocket.StatusNormalClosure, "") }()

	// Wait for replacement
	time.Sleep(100 * time.Millisecond)

	// Verify the new connection replaced the old one
	mgr.mu.Lock()
	sub = mgr.subscriptions["replace-test"]
	mgr.mu.Unlock()
	sub.mu.Lock()
	secondConn := sub.conn
	sub.mu.Unlock()
	if secondConn == nil {
		t.Fatal("expected second connection to be stored")
	}
	if secondConn == firstConn {
		t.Error("expected connection to be replaced with a new one")
	}

	_ = conn1.Close(websocket.StatusNormalClosure, "")
	mgr.DisconnectClient("replace-test")
}

// TestManager_Subscribe_LimitReached verifies that Subscribe rejects new
// subscriptions when the limit is reached (exercises _ = conn.Close in Subscribe).
func TestManager_Subscribe_LimitReached(t *testing.T) {
	mgr := newTestManager()

	// Fill up to the subscription limit
	for i := range maxSubscriptions {
		var writeMu sync.Mutex
		mgr.Subscribe(nil, &writeMu, fmt.Sprintf("filler-%d", i), "")
	}

	// Create a real WebSocket server and try to subscribe beyond the limit
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		var writeMu sync.Mutex
		sub := mgr.Subscribe(conn, &writeMu, "overflow", "")
		if sub == nil {
			// Subscription rejected — conn.Close was already called by Subscribe
			return
		}
		defer mgr.DisconnectClient("overflow")
		readClientMessages(mgr, conn, &writeMu, "overflow")
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// The connection should be closed by the server since limit is reached
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		// Connection rejected — expected
		return
	}
	// Try to read — server should close the connection
	_, _, readErr := conn.Read(ctx)
	if readErr == nil {
		t.Error("expected connection to be closed by server (limit reached)")
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")
}

func TestManager_HasDisconnectedClients_AllConnected(t *testing.T) {
	m := NewManagerForTest()

	// To simulate a connected client, we need a non-nil conn.
	// Create a real WS server to get a real conn.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		var wmu sync.Mutex
		m.Subscribe(conn, &wmu, "connected-client", "")
		// Keep conn alive briefly
		time.Sleep(500 * time.Millisecond)
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// Wait for subscribe
	time.Sleep(150 * time.Millisecond)

	// All clients connected → should return false
	if m.HasDisconnectedClients() {
		t.Error("expected false when all clients are connected")
	}
}

func TestManager_HasDisconnectedClients_SomeDisconnected(t *testing.T) {
	m := NewManagerForTest()
	var writeMu sync.Mutex

	// Two clients: one connected, one disconnected
	m.Subscribe(nil, &writeMu, "disconnected-client", "")
	m.DisconnectClient("disconnected-client")

	// Subscribe a connected client via a real WS
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		var wmu sync.Mutex
		m.Subscribe(conn, &wmu, "connected-client", "")
		time.Sleep(500 * time.Millisecond)
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	time.Sleep(150 * time.Millisecond)

	// Some clients disconnected → should return true
	if !m.HasDisconnectedClients() {
		t.Error("expected true when some clients are disconnected")
	}
}

func TestManager_DisconnectClient_NonExistent(t *testing.T) {
	m := NewManagerForTest()
	// Should not panic
	m.DisconnectClient("nonexistent")
}

func TestManager_BroadcastEvent_ConnectedClient(t *testing.T) {
	m := NewManagerForTest()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		var wmu sync.Mutex
		sub := m.Subscribe(conn, &wmu, "ws-client", "")
		if sub == nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return
		}
		m.StartWriter("ws-client", conn, &wmu)
		defer m.StopWriter("ws-client", conn)
		defer m.DisconnectClient("ws-client")
		// Keep connection alive — read loop discards incoming client messages
		readClientMessages(m, conn, &wmu, "ws-client")
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// Wait for subscription
	time.Sleep(150 * time.Millisecond)

	// Broadcast while connected — should send via WS and also buffer
	msg := ServerMessage{Type: "event", ID: "evt_ws_1", Event: "session_update", Data: &SessionUpdateData{SessionID: "s1", Status: "completed"}}
	m.BroadcastEvent(msg)

	// The client (dialed conn) should receive the message via WS
	readCtx, readCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer readCancel()
	_, data, readErr := conn.Read(readCtx)
	if readErr != nil {
		t.Fatalf("expected to read WS message, got error: %v", readErr)
	}
	if !strings.Contains(string(data), "evt_ws_1") {
		t.Errorf("expected WS message to contain event ID 'evt_ws_1', got: %s", data)
	}

	// Verify the event was also buffered for reconnect replay
	m.mu.Lock()
	sub := m.subscriptions["ws-client"]
	m.mu.Unlock()
	buffered := sub.GetBufferedEvents()
	if len(buffered) == 0 {
		t.Error("expected event to be buffered for reconnect replay")
	} else if buffered[0].ID != "evt_ws_1" {
		t.Errorf("expected buffered event ID 'evt_ws_1', got %q", buffered[0].ID)
	}
}

func TestManager_BroadcastEvent_BufferStartZero(t *testing.T) {
	m := NewManagerForTest()

	// Manually create a subscription with conn=nil but bufferStart=zero
	// This simulates a subscription that was never connected (edge case)
	sub := &ClientSubscription{clientID: "never-connected"}
	m.mu.Lock()
	m.subscriptions["never-connected"] = sub
	m.mu.Unlock()

	msg := ServerMessage{Type: "event", ID: "evt_z", Event: "session_update", Data: &SessionUpdateData{SessionID: "s1", Status: "completed"}}
	m.BroadcastEvent(msg)

	// bufferStart.IsZero() → should buffer (the IsZero check allows buffering)
	buffered := sub.GetBufferedEvents()
	if len(buffered) != 1 {
		t.Fatalf("expected 1 buffered event when bufferStart is zero, got %d", len(buffered))
	}
	if buffered[0].ID != "evt_z" {
		t.Errorf("expected buffered event ID 'evt_z', got %q", buffered[0].ID)
	}
}

func TestCleanupStale_ZeroBufferStart(t *testing.T) {
	m := NewManagerForTest()

	// Manually create a subscription with conn=nil and bufferStart=zero
	sub := &ClientSubscription{clientID: "zero-buffer"}
	m.mu.Lock()
	m.subscriptions["zero-buffer"] = sub
	m.mu.Unlock()

	// Should NOT be cleaned up (bufferStart.IsZero() → continue)
	m.CleanupStale()

	m.mu.Lock()
	_, exists := m.subscriptions["zero-buffer"]
	m.mu.Unlock()
	if !exists {
		t.Error("expected subscription with zero bufferStart to not be cleaned up")
	}
}

func TestManager_BroadcastEvent_SubscriptionRemovedBetweenSnapshotAndDelivery(t *testing.T) {
	m := NewManagerForTest()
	var writeMu sync.Mutex

	m.Subscribe(nil, &writeMu, "ephemeral", "")
	m.DisconnectClient("ephemeral")

	// Remove the subscription after BroadcastEvent snapshots keys but before delivery
	// This is hard to trigger deterministically, but we can test by:
	// 1. Having a subscription, 2. Removing it manually, then 3. calling broadcastToSubscription
	// The broadcastToSubscription handles !ok → return (line 173-175)

	m.mu.Lock()
	delete(m.subscriptions, "ephemeral")
	m.mu.Unlock()

	// broadcastToSubscription on a missing key should not panic
	m.broadcastToSubscription("ephemeral", ServerMessage{Type: "event", ID: "evt_x"})
}

// TestWriteMessage_ReturnsErrorOnClosedConnection verifies that writeMessage
// reports a write failure when the underlying connection is closed. This is the
// error that must trigger connection cleanup so the client reconnects promptly.
func TestWriteMessage_ReturnsErrorOnClosedConnection(t *testing.T) {
	m := NewManagerForTest()

	// Server subscribes a real connection and exposes it to the test.
	var wmu sync.Mutex
	var serverConn *websocket.Conn
	subscribed := make(chan struct{}, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		m.Subscribe(conn, &wmu, "closed-write", "")
		serverConn = conn
		subscribed <- struct{}{}
		// Keep the handler alive briefly; the test drives the connection.
		time.Sleep(2 * time.Second)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer func() { _ = client.CloseNow() }()

	<-subscribed

	// Sanity: a write on an open connection succeeds.
	if err := writeMessage(&wmu, serverConn, []byte("ping")); err != nil {
		t.Fatalf("expected write to succeed on open connection, got %v", err)
	}

	// Close the server-side connection; subsequent writes must fail.
	_ = serverConn.CloseNow()
	if err := writeMessage(&wmu, serverConn, []byte("ping")); err == nil {
		t.Error("expected writeMessage to return an error on a closed connection")
	}
}

// TestManager_BroadcastEvent_WriteFailureClosesConnection verifies that a
// broadcast write failure on a subscribed connection does not panic and leaves
// the subscription preserved for replay (the connection itself is cleaned up by
// the read loop / CloseNow). This guards the previously ignored write error path.
func TestManager_BroadcastEvent_WriteFailureClosesConnection(t *testing.T) {
	m := NewManagerForTest()
	var wmu sync.Mutex
	var serverConn *websocket.Conn
	subscribed := make(chan struct{}, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		m.Subscribe(conn, &wmu, "failed-write", "")
		m.StartWriter("failed-write", conn, &wmu)
		serverConn = conn
		subscribed <- struct{}{}
		time.Sleep(2 * time.Second)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer func() { _ = client.CloseNow() }()

	<-subscribed

	// Close the server-side conn so the next broadcast write fails.
	_ = serverConn.CloseNow()

	// Broadcast must not panic even though the write fails.
	msg := ServerMessage{Type: "event", ID: "evt_dead", Event: "session_update", Data: &SessionUpdateData{SessionID: "s1", Status: "completed"}}
	m.BroadcastEvent(msg)

	// The subscription must still exist (preserved for replay on reconnect).
	m.mu.Lock()
	sub := m.subscriptions["failed-write"]
	m.mu.Unlock()
	if sub == nil {
		t.Fatal("expected subscription to still exist after a failed broadcast write")
	}
}

func TestManager_BroadcastEvent_MarshalError(t *testing.T) {
	m := NewManagerForTest()

	// Create a subscription with a real conn so we hit the marshal path
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		var wmu sync.Mutex
		m.Subscribe(conn, &wmu, "marshal-client", "")
		time.Sleep(2 * time.Second)
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	time.Sleep(150 * time.Millisecond)

	// Broadcast a message with unmarshallable Data — json.Marshal should fail
	msg := ServerMessage{Type: "event", ID: "evt_bad", Data: make(chan int)} // channels can't be marshaled
	m.BroadcastEvent(msg)                                                    // should not panic, just log error

	// Verify nothing was buffered (marshal error → return before bufferEvent)
	m.mu.Lock()
	sub := m.subscriptions["marshal-client"]
	m.mu.Unlock()
	buffered := sub.GetBufferedEvents()
	if len(buffered) != 0 {
		t.Errorf("expected 0 buffered events on marshal error, got %d", len(buffered))
	}
}

func TestInitManager(t *testing.T) {
	// Reset sync.Once so InitManager actually runs
	origManager := defaultManager
	defer func() {
		defaultManager = origManager
	}()

	// Reset the Once by assigning a fresh one (can't copy sync.Once by value)
	defaultManagerOnce = sync.Once{}
	defaultManager = nil

	InitManager()

	if defaultManager == nil {
		t.Fatal("expected defaultManager to be initialized after InitManager")
	}
	if defaultManager.subscriptions == nil {
		t.Fatal("expected subscriptions map to be initialized")
	}
	if defaultManager.hub == nil {
		t.Fatal("expected hub to be initialized")
	}

	// Calling InitManager again should not create a new manager
	first := defaultManager
	InitManager()
	if defaultManager != first {
		t.Error("InitManager should only initialize once (sync.Once)")
	}
}

func TestCleanupStale_ConnectedNotCleaned_BranchCoverage(t *testing.T) {
	// Cover the `sub.conn != nil → continue` branch in CleanupStale
	mgr := newTestManager()

	// Create a real WS server to get a connected subscription
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		var wmu sync.Mutex
		mgr.Subscribe(conn, &wmu, "connected-cleanup", "")
		time.Sleep(500 * time.Millisecond)
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	time.Sleep(150 * time.Millisecond)

	// CleanupStale should not remove the connected subscription
	mgr.CleanupStale()

	mgr.mu.Lock()
	_, exists := mgr.subscriptions["connected-cleanup"]
	mgr.mu.Unlock()
	if !exists {
		t.Error("expected connected subscription to not be cleaned up")
	}
}

// TestClientSubscription_AsyncSendQueue verifies the async writer drains the
// send queue and delivers messages in FIFO order to the client.
func TestClientSubscription_AsyncSendQueue(t *testing.T) {
	m := NewManagerForTest()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		var wmu sync.Mutex
		sub := m.Subscribe(conn, &wmu, "async-fifo", "")
		if sub == nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return
		}
		m.StartWriter("async-fifo", conn, &wmu)
		defer m.StopWriter("async-fifo", conn)
		defer m.DisconnectClient("async-fifo")
		readClientMessages(m, conn, &wmu, "async-fifo")
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer func() { _ = client.Close(websocket.StatusNormalClosure, "") }()

	time.Sleep(150 * time.Millisecond)

	// Broadcast several events in a defined order.
	const n = 5
	for i := range n {
		msg := ServerMessage{Type: "event", ID: fmt.Sprintf("fifo_%d", i), Event: "session_update", Data: &SessionUpdateData{SessionID: "s1", Status: "completed"}}
		m.BroadcastEvent(msg)
	}

	// Read them back and verify FIFO order.
	for i := range n {
		readCtx, readCancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, data, readErr := client.Read(readCtx)
		readCancel()
		if readErr != nil {
			t.Fatalf("read %d failed: %v", i, readErr)
		}
		want := fmt.Sprintf("fifo_%d", i)
		if !strings.Contains(string(data), want) {
			t.Errorf("message %d = %q, want to contain %q", i, data, want)
		}
	}
}

// TestClientSubscription_QueueFullClosesConnection verifies that when the send
// queue is full, broadcastToSubscription closes the connection so the client
// reconnects instead of silently dropping events.
func TestClientSubscription_QueueFullClosesConnection(t *testing.T) {
	m := NewManagerForTest()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		var wmu sync.Mutex
		sub := m.Subscribe(conn, &wmu, "queue-full", "")
		if sub == nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return
		}
		// No writer started — the send queue is never drained, so a broadcast
		// beyond the queue capacity must overflow.
		time.Sleep(2 * time.Second)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer func() { _ = client.CloseNow() }()

	time.Sleep(150 * time.Millisecond)

	// Shrink the queue so overflow is deterministic without waiting for 256+ events.
	m.mu.Lock()
	sub := m.subscriptions["queue-full"]
	m.mu.Unlock()
	sub.mu.Lock()
	sub.sendQueue = make(chan []byte, 1)
	sub.mu.Unlock()

	msg := ServerMessage{Type: "event", ID: "overflow", Event: "session_update", Data: &SessionUpdateData{SessionID: "s1", Status: "completed"}}

	// First broadcast fills the 1-slot queue.
	m.BroadcastEvent(msg)

	// The client should still be connected (queue not full yet).
	client.SetReadLimit(1 << 20)
	clientReadCtx, clientReadCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	_, _, readErr := client.Read(clientReadCtx)
	clientReadCancel()
	if readErr == nil {
		t.Fatal("unexpected: client read data when nothing was written")
	}

	// Second broadcast overflows — connection must be closed for reconnect.
	m.BroadcastEvent(msg)

	// The close should surface to the client as a connection error.
	readCtx, readCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer readCancel()
	_, _, readErr = client.Read(readCtx)
	if readErr == nil {
		t.Fatal("expected connection to be closed after queue overflow, got a message instead")
	}

	// enqueueSendLocked must refuse further enqueues (queue full).
	sub.mu.Lock()
	ok := sub.enqueueSendLocked([]byte("x"))
	sub.mu.Unlock()
	if ok {
		t.Error("expected enqueueSendLocked to return false when queue is full")
	}
}

// TestClientSubscription_WriterGoroutine_StopsOnDisconnect verifies StopWriter
// terminates the writer goroutine and that no goroutine leaks.
func TestClientSubscription_WriterGoroutine_StopsOnDisconnect(t *testing.T) {
	m := NewManagerForTest()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		var wmu sync.Mutex
		m.Subscribe(conn, &wmu, "writer-stop", "")
		m.StartWriter("writer-stop", conn, &wmu)
		// Read loop blocks until the client disconnects.
		readClientMessages(m, conn, &wmu, "writer-stop")
		m.StopWriter("writer-stop", conn)
		m.DisconnectClient("writer-stop")
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	// Client closes — server read loop exits, handler stops the writer.
	_ = client.CloseNow()

	// Give the handler time to run StopWriter.
	deadline := time.Now().Add(3 * time.Second)
	for {
		m.mu.Lock()
		sub := m.subscriptions["writer-stop"]
		m.mu.Unlock()
		if sub == nil {
			break
		}
		sub.mu.Lock()
		started := sub.writerStarted
		sub.mu.Unlock()
		if !started {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("writer goroutine did not stop after disconnect")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestClientSubscription_DefensiveBranches covers the guard branches of the
// async send machinery: enqueue on a nil queue, StartWriter/StopWriter for a
// missing subscription, and StartWriter/StopWriter on a subscription whose
// writer was never started.
func TestClientSubscription_DefensiveBranches(t *testing.T) {
	m := NewManagerForTest()

	// enqueueSendLocked on a subscription with a nil queue refuses.
	sub := &ClientSubscription{clientID: "nil-queue"}
	if sub.enqueueSendLocked([]byte("x")) {
		t.Error("expected enqueue on nil queue to be refused")
	}

	// StartWriter / StopWriter for a client that never subscribed: no-op.
	var wmu sync.Mutex
	m.StartWriter("ghost", nil, &wmu)
	m.StopWriter("ghost", nil)

	// StartWriter on a subscription whose writer was not started is a no-op
	// (writerStarted stays false), and StopWriter similarly returns early.
	m.mu.Lock()
	m.subscriptions["never-writer"] = &ClientSubscription{clientID: "never-writer"}
	m.mu.Unlock()
	// sendQueue nil → StartWriter no-op.
	m.StartWriter("never-writer", nil, &wmu)
	m.mu.Lock()
	s := m.subscriptions["never-writer"]
	m.mu.Unlock()
	s.mu.Lock()
	started := s.writerStarted
	s.mu.Unlock()
	if started {
		t.Error("expected writer not to start when sendQueue is nil")
	}
	// StopWriter with writer never started: no-op (must not block).
	m.StopWriter("never-writer", nil)
}

// TestClientSubscription_ReconnectOldStopWriterNoOp verifies the C1 fix: when a
// client reconnects (Subscribe replaces the connection), the OLD handler's
// deferred StopWriter must NOT kill the NEW connection's writer.
//
// Regression scenario:
//  1. Connection A subscribes and starts writer A.
//  2. Connection B reconnects (Subscribe replaces A). Subscribe must actively
//     stop writer A so A's deferred StopWriter becomes a no-op.
//  3. Writer B starts. A's deferred StopWriter runs afterwards.
//  4. A broadcast must still reach connection B (writer B alive).
func TestClientSubscription_ReconnectOldStopWriterNoOp(t *testing.T) {
	m := NewManagerForTest()

	subAready := make(chan struct{}, 1)
	subBready := make(chan struct{}, 1)
	releaseA := make(chan struct{}, 1)
	aTornDown := make(chan struct{}, 1)

	// Connection A handler: subscribe + start writer, then block until released,
	// then run its teardown (simulating the old connection tearing down). The
	// teardown mirrors production EventsHandler: DisconnectClientIfCurrent
	// guards against wiping a connection that Subscribe has since replaced, and
	// StopWriter's conn identity check makes it a no-op for the new writer.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		var wmu sync.Mutex
		sub := m.Subscribe(conn, &wmu, "reconnect-client", "")
		if sub == nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return
		}
		m.StartWriter("reconnect-client", conn, &wmu)
		subAready <- struct{}{}
		<-releaseA
		m.DisconnectClientIfCurrent("reconnect-client", conn)
		m.StopWriter("reconnect-client", conn)
		aTornDown <- struct{}{}
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Dial connection A.
	clientA, _, err := websocket.Dial(ctx, "ws"+server.URL[4:], nil)
	if err != nil {
		t.Fatalf("dial A failed: %v", err)
	}
	<-subAready

	// Simulate reconnection with connection B on a separate handler sharing the
	// same clientID — Subscribe must replace A and stop writer A.
	handlerB := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		var wmu sync.Mutex
		sub := m.Subscribe(conn, &wmu, "reconnect-client", "")
		if sub == nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return
		}
		m.StartWriter("reconnect-client", conn, &wmu)
		subBready <- struct{}{}
		// Keep the connection alive.
		readClientMessages(m, conn, &wmu, "reconnect-client")
		m.StopWriter("reconnect-client", conn)
		m.DisconnectClient("reconnect-client")
	})
	serverB := httptest.NewServer(handlerB)
	defer serverB.Close()

	clientB, _, err := websocket.Dial(ctx, "ws"+serverB.URL[4:], nil)
	if err != nil {
		t.Fatalf("dial B failed: %v", err)
	}
	<-subBready

	// Release connection A's handler so its teardown runs against the same
	// clientID (must be a no-op now — writer A was already stopped and the
	// connection was replaced). Wait for it to finish so the broadcast below
	// cannot race with A's teardown goroutine.
	releaseA <- struct{}{}
	select {
	case <-aTornDown:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for connection A teardown")
	}

	// Broadcast — the message must reach connection B, proving writer B is alive
	// after A's teardown ran.
	msg := ServerMessage{Type: "event", ID: "reconnect_ok", Event: "session_update", Data: &SessionUpdateData{SessionID: "s1", Status: "completed"}}
	m.BroadcastEvent(msg)

	readCtx, readCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer readCancel()
	_, data, readErr := clientB.Read(readCtx)
	if readErr != nil {
		t.Fatalf("connection B should still receive events, got error: %v", readErr)
	}
	if !strings.Contains(string(data), "reconnect_ok") {
		t.Errorf("expected connection B to receive broadcast, got: %s", data)
	}

	_ = clientA.CloseNow()
	_ = clientB.CloseNow()
}

// TestSubscribe_PreservesEventBufferForReplay is the regression test for the
// reconnect-replay bug: Subscribe used to clear eventBuffer before
// EventsHandler called GetBufferedEvents, so events buffered while a client
// was disconnected were never replayed. Stream events (content/tool_use/done)
// produced during the gap were silently lost, and the frontend could render a
// "finished" session that was still streaming.
//
// The test simulates: connect → disconnect → broadcast while away → reconnect.
// After the reconnect, GetBufferedEvents (what EventsHandler replays) MUST
// still contain the event broadcast while the client was away.
func TestSubscribe_PreservesEventBufferForReplay(t *testing.T) {
	mgr := newTestManager()
	var writeMu sync.Mutex

	// First connection.
	sub := mgr.Subscribe(nil, &writeMu, "replay-client", "")
	if sub == nil {
		t.Fatal("expected non-nil subscription on first connect")
	}

	// Disconnect — the subscription (and its buffer) is preserved.
	mgr.DisconnectClient("replay-client")

	// Event broadcast while the client is away must be buffered.
	msg := ServerMessage{Type: "event", ID: "evt_away_1", Event: "chat_stream", Data: &ChatStreamData{SessionID: "s1", EventType: "done"}}
	mgr.BroadcastEvent(msg)

	bufferedBefore := sub.GetBufferedEvents()
	if len(bufferedBefore) != 1 {
		t.Fatalf("expected 1 buffered event while disconnected, got %d", len(bufferedBefore))
	}

	// Reconnect — Subscribe must NOT clear the buffer, so EventsHandler's
	// subsequent GetBufferedEvents() can replay it.
	sub2 := mgr.Subscribe(nil, &writeMu, "replay-client", "")
	if sub2 == nil {
		t.Fatal("expected non-nil subscription on reconnect")
	}

	bufferedAfter := sub2.GetBufferedEvents()
	if len(bufferedAfter) != 1 {
		t.Fatalf("REGRESSION: reconnect lost the buffered event — replay would be empty (got %d events)", len(bufferedAfter))
	}
	if bufferedAfter[0].ID != "evt_away_1" {
		t.Errorf("expected buffered event 'evt_away_1', got %q", bufferedAfter[0].ID)
	}

	// After the replay is consumed, the buffer window resets (EventsHandler
	// clears the buffer post-replay). Simulate that here so a subsequent
	// reconnect does not replay the same event again.
	sub2.mu.Lock()
	sub2.eventBuffer = nil
	sub2.bufferStart = time.Time{}
	sub2.mu.Unlock()
	if got := sub2.GetBufferedEvents(); len(got) != 0 {
		t.Errorf("expected empty buffer after replay consumption, got %d events", len(got))
	}
}

// TestDisconnectClientIfCurrent_ReplacedConnNoOp is the regression test for
// the connection-replace cleanup race. When a client reconnects, Subscribe
// installs the new connection. The OLD EventsHandler's deferred teardown then
// runs — potentially AFTER the new connection was installed and its session
// subscribed. Without an identity guard that teardown would:
//   - null the NEW connection's sub.conn (DisconnectClient), and
//   - wipe the client's StreamHub session subscription (UnsubscribeAll),
//
// leaving the socket alive but every stream event dropped server-side — the
// silent streaming stall. The teardown must only take effect while the conn it
// belongs to is STILL the subscription's current connection.
func TestDisconnectClientIfCurrent_ReplacedConnNoOp(t *testing.T) {
	mgr := NewManagerForTest()

	// Real server-side connections: each handler accepts one socket, registers
	// it under the shared clientID, then blocks on a channel. This mirrors the
	// EventsHandler shape where each connection holds its OWN accepted conn and
	// runs teardown on exit.
	type connInfo struct {
		conn    *websocket.Conn
		writeMu *sync.Mutex
	}
	accepted := make(chan *connInfo, 2)
	release := make(chan struct{}, 2)
	// teardownDone signals that a handler's teardown has fully completed, so
	// assertions never race the teardown goroutine.
	teardownDone := make(chan struct{}, 2)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		wmu := &sync.Mutex{}
		sub := mgr.Subscribe(conn, wmu, "race-client", "")
		if sub == nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return
		}
		accepted <- &connInfo{conn: conn, writeMu: wmu}
		<-release
		// Late teardown, exactly like the EventsHandler defer. DisconnectClient
		// is what EventsHandler calls — DisconnectClientIfCurrent is the guarded
		// replacement it now uses.
		mgr.StopWriter("race-client", conn)
		if mgr.DisconnectClientIfCurrent("race-client", conn) {
			mgr.StreamHub().UnsubscribeAll("race-client")
		}
		teardownDone <- struct{}{}
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wsURL := "ws" + server.URL[4:]

	// Connection A connects.
	clientA, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial A failed: %v", err)
	}
	defer func() { _ = clientA.CloseNow() }()
	infoA := <-accepted
	// A subscribes to the session it is viewing (s1).
	mgr.StreamHub().Subscribe("race-client", "s1")

	// Connection B (same clientID) replaces A — a reconnect. The frontend
	// re-subscribes to the SAME session it was viewing.
	clientB, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial B failed: %v", err)
	}
	defer func() { _ = clientB.CloseNow() }()
	infoB := <-accepted
	mgr.StreamHub().Subscribe("race-client", "s1")
	if !mgr.StreamHub().HasSubscribers("s1") {
		t.Fatal("s1 should have a subscriber after B connects")
	}

	// Now release connection A's handler so its (late) teardown runs against
	// the shared subscription whose current conn is B's. The guarded teardown
	// must be a no-op for A's stale conn — otherwise it would wipe the
	// subscription B just re-established and the live stream events die.
	release <- struct{}{}
	select {
	case <-teardownDone:
	case <-time.After(5 * time.Second):
		t.Fatal("connection A's teardown never completed")
	}

	// After A's FULLY-completed late teardown, B's connection and its session
	// subscription must still be intact.
	subB := mgr.subscriptions["race-client"]
	subB.mu.Lock()
	cur := subB.conn
	subB.mu.Unlock()
	if cur == nil {
		t.Fatal("REGRESSION: replaced connection A's teardown nulled connection B")
	}
	if cur != infoB.conn {
		t.Fatal("REGRESSION: replaced connection A's teardown swapped connection B's conn")
	}
	if !mgr.StreamHub().HasSubscribers("s1") {
		t.Fatal("REGRESSION: replaced connection A's teardown wiped session s1 subscription")
	}

	// Release B's handler so its own teardown (current conn) still works —
	// it must disconnect B and clear the session subscription.
	release <- struct{}{}
	select {
	case <-teardownDone:
	case <-time.After(5 * time.Second):
		t.Fatal("connection B's teardown never completed")
	}
	subB.mu.Lock()
	cur = subB.conn
	subB.mu.Unlock()
	if cur != nil {
		t.Error("expected sub.conn to be null after the current connection disconnects")
	}
	if mgr.StreamHub().HasSubscribers("s1") {
		t.Error("expected s1 subscription cleared after the current connection disconnects")
	}
	_ = infoA.conn.CloseNow()
	_ = infoB.conn.CloseNow()
}

// ---------- System-resource telemetry delivery ----------

// telemetryMsg builds a representative non-critical telemetry frame.
func telemetryMsg() ServerMessage {
	return ServerMessage{Type: MessageTypeEvent, Event: "system_resources", Data: map[string]any{"cpu": 12.5}}
}

// connectedMetricsSub registers a subscription whose conn is non-nil (the
// "connected" signal MetricsDemand and telemetry delivery both check) and that
// has declared metrics interest. Returns the subscription for inspection.
//
// A zero-value &websocket.Conn{} cannot be used here: Subscribe's replace path
// calls Close on the previous conn, which panics on an uninitialized Conn. A
// real accepted connection is required.
func connectedMetricsSub(t *testing.T, mgr *Manager, clientID string) *ClientSubscription {
	t.Helper()
	conn := acceptRealConn(t)
	var writeMu sync.Mutex
	sub := mgr.Subscribe(conn, &writeMu, clientID, "")
	if sub == nil {
		t.Fatal("expected a subscription")
	}
	if !mgr.SetClientMetricsPreference(clientID, conn, true, defaultMetricsIntervalMs) {
		t.Fatal("expected the preference to be recorded")
	}
	return sub
}

// metricsSubConn returns the connection a subscription is currently bound to,
// for tests that need to pass the identity-guard argument.
func metricsSubConn(t *testing.T, mgr *Manager, clientID string) *websocket.Conn {
	t.Helper()
	mgr.mu.Lock()
	sub, ok := mgr.subscriptions[clientID]
	mgr.mu.Unlock()
	if !ok {
		t.Fatalf("no subscription for %q", clientID)
	}
	sub.mu.Lock()
	defer sub.mu.Unlock()
	return sub.conn
}

// acceptRealConn starts a throwaway WS server and returns the server side of an
// accepted connection. The connection is never written to; it only serves as a
// non-nil conn. A client-side reader goroutine drains the socket so a graceful
// Close (e.g. Subscribe's "replaced" handshake) completes immediately instead
// of stalling until wsWriteTimeout.
func acceptRealConn(t *testing.T) *websocket.Conn {
	t.Helper()
	connCh := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		connCh <- c
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, _, err := websocket.Dial(ctx, "ws"+srv.URL[4:], nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	t.Cleanup(func() { _ = client.CloseNow() })

	// Drain incoming frames (and the peer's close frame) so the server-side
	// Close handshake can finish.
	go func() {
		for {
			if _, _, err := client.Read(context.Background()); err != nil {
				return
			}
		}
	}()

	select {
	case c := <-connCh:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("server never accepted the connection")
		return nil
	}
}

// TestManager_Telemetry_DoesNotBufferReplay is the regression test for the core
// hazard: at 1Hz, a buffered telemetry stream would evict real chat/task events
// from the 50-entry reconnect replay buffer within a minute.
func TestManager_Telemetry_DoesNotBufferReplay(t *testing.T) {
	mgr := NewManagerForTest()
	sub := connectedMetricsSub(t, mgr, "telemetry-1")

	for range 60 {
		mgr.BroadcastToMetricsWatchers(telemetryMsg())
	}

	if buffered := sub.GetBufferedEvents(); len(buffered) != 0 {
		t.Fatalf("telemetry must not enter the replay buffer, got %d entries", len(buffered))
	}

	// Sanity: a real event on the same subscription IS buffered, proving the
	// assertion above is about telemetry specifically and not a broken buffer.
	mgr.BroadcastEvent(ServerMessage{Type: MessageTypeEvent, ID: "real-1", Event: "session_update"})
	if buffered := sub.GetBufferedEvents(); len(buffered) != 1 {
		t.Fatalf("expected the ordinary event to be buffered, got %d entries", len(buffered))
	}
}

// TestManager_Telemetry_QueueFullDoesNotCloseConnection pins the second half of
// the hazard: losing one metric frame must never tear down the chat connection.
//
// The assertion must observe the CLIENT side. Checking `sub.conn != nil` here
// would be vacuous — only disconnectClient nulls that field, while the overflow
// path calls conn.CloseNow(), which leaves sub.conn intact. A mutation flipping
// telemetryDelivery.closeOnOverflow to true would still pass such a test.
func TestManager_Telemetry_QueueFullDoesNotCloseConnection(t *testing.T) {
	mgr := NewManagerForTest()

	var wmu sync.Mutex
	connCh := make(chan *websocket.Conn, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		if mgr.Subscribe(conn, &wmu, "telemetry-full", "") == nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return
		}
		connCh <- conn
		// No writer started — the send queue is never drained, so a push beyond
		// its capacity must overflow.
		<-r.Context().Done()
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, _, err := websocket.Dial(ctx, "ws"+server.URL[4:], nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer func() { _ = client.CloseNow() }()

	var serverConn *websocket.Conn
	select {
	case serverConn = <-connCh:
	case <-time.After(5 * time.Second):
		t.Fatal("server never accepted the connection")
	}

	if !mgr.SetClientMetricsPreference("telemetry-full", serverConn, true, defaultMetricsIntervalMs) {
		t.Fatal("expected the preference to be recorded")
	}

	// Shrink the queue so overflow is deterministic without waiting for 256 pushes.
	// Deliberately no writer goroutine: nothing drains the queue, which is what
	// makes the second push overflow.
	mgr.mu.Lock()
	sub := mgr.subscriptions["telemetry-full"]
	mgr.mu.Unlock()
	sub.mu.Lock()
	sub.sendQueue = make(chan []byte, 1)
	sub.mu.Unlock()

	if n := mgr.BroadcastToMetricsWatchers(telemetryMsg()); n != 1 {
		t.Fatalf("expected the first frame to be accepted, got %d", n)
	}
	// Queue is full now. Telemetry must DROP this frame, not close the socket.
	if n := mgr.BroadcastToMetricsWatchers(telemetryMsg()); n != 0 {
		t.Fatalf("expected the overflowing frame to be dropped, got %d", n)
	}

	// Prove the socket is still alive by writing on it directly. If the overflow
	// had called CloseNow, this write would fail — which is exactly what a
	// mutation flipping closeOnOverflow to true produces.
	writeCtx, writeCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer writeCancel()
	if err := serverConn.Write(writeCtx, websocket.MessageText, []byte("still-alive")); err != nil {
		t.Fatalf("telemetry overflow closed the connection: write failed: %v", err)
	}

	readCtx, readCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer readCancel()
	_, data, readErr := client.Read(readCtx)
	if readErr != nil {
		t.Fatalf("telemetry overflow closed the connection: %v", readErr)
	}
	if string(data) != "still-alive" {
		t.Fatalf("expected the liveness probe, got %s", data)
	}
}

// TestManager_BroadcastToMetricsWatchers_SkipsNonWatchers verifies delivery is
// gated on an explicit declaration, not just on being connected.
func TestManager_BroadcastToMetricsWatchers_SkipsNonWatchers(t *testing.T) {
	mgr := NewManagerForTest()

	// Connected but never declared.
	var wmu1 sync.Mutex
	if mgr.Subscribe(acceptRealConn(t), &wmu1, "silent", "") == nil {
		t.Fatal("expected a subscription")
	}
	// Connected and declared.
	connectedMetricsSub(t, mgr, "watcher")

	if n := mgr.BroadcastToMetricsWatchers(telemetryMsg()); n != 1 {
		t.Fatalf("expected exactly the declaring client to receive the frame, got %d", n)
	}
}

func TestManager_MetricsDemand_Gating(t *testing.T) {
	t.Run("no subscriptions", func(t *testing.T) {
		mgr := NewManagerForTest()
		count, interval := mgr.MetricsDemand()
		if count != 0 || interval != 0 {
			t.Fatalf("expected (0,0), got (%d,%d)", count, interval)
		}
	})

	t.Run("declared but disconnected contributes no demand", func(t *testing.T) {
		mgr := NewManagerForTest()
		connectedMetricsSub(t, mgr, "gone")
		mgr.DisconnectClient("gone")

		count, _ := mgr.MetricsDemand()
		if count != 0 {
			t.Fatalf("a disconnected client must not keep the sampler running, got count=%d", count)
		}
	})

	t.Run("disabled declaration", func(t *testing.T) {
		mgr := NewManagerForTest()
		connectedMetricsSub(t, mgr, "off")
		mgr.SetClientMetricsPreference("off", metricsSubConn(t, mgr, "off"), false, 0)

		if count, _ := mgr.MetricsDemand(); count != 0 {
			t.Fatalf("expected no demand after disabling, got %d", count)
		}
	})

	t.Run("fastest interval wins across clients", func(t *testing.T) {
		mgr := NewManagerForTest()
		connectedMetricsSub(t, mgr, "slow")
		mgr.SetClientMetricsPreference("slow", metricsSubConn(t, mgr, "slow"), true, 5000)
		connectedMetricsSub(t, mgr, "fast")
		mgr.SetClientMetricsPreference("fast", metricsSubConn(t, mgr, "fast"), true, 1000)

		count, interval := mgr.MetricsDemand()
		if count != 2 {
			t.Fatalf("expected 2 watchers, got %d", count)
		}
		if interval != 1000 {
			t.Fatalf("expected the fastest interval to win, got %d", interval)
		}
	})
}

func TestManager_SetClientMetricsPreference_UnknownClient(t *testing.T) {
	mgr := NewManagerForTest()
	if mgr.SetClientMetricsPreference("nobody", nil, true, 1000) {
		t.Fatal("expected false for an unknown client")
	}
}

// TestManager_Subscribe_ResetsMetricsPreference pins that a reconnecting client
// must re-declare: preserving a stale flag would keep the sampler running for a
// client that never comes back to declare.
func TestManager_Subscribe_ResetsMetricsPreference(t *testing.T) {
	mgr := NewManagerForTest()
	connectedMetricsSub(t, mgr, "reconnect")
	if count, _ := mgr.MetricsDemand(); count != 1 {
		t.Fatalf("precondition failed: expected 1 watcher, got %d", count)
	}

	// Reconnect (same clientID, new connection).
	var wmu sync.Mutex
	if mgr.Subscribe(acceptRealConn(t), &wmu, "reconnect", "") == nil {
		t.Fatal("expected the reconnect to succeed")
	}

	if count, _ := mgr.MetricsDemand(); count != 0 {
		t.Fatalf("a fresh connection must start with no metrics preference, got count=%d", count)
	}
}

func TestNormalizeMetricsInterval(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		requested int
		want      int
	}{
		{"disabled clears the interval", false, 1000, 0},
		{"too fast is clamped to the default", true, 1, defaultMetricsIntervalMs},
		{"zero falls back to the default", true, 0, defaultMetricsIntervalMs},
		{"within range is kept", true, 5000, 5000},
		{"too slow is clamped to the maximum", true, 999999, maxMetricsIntervalMs},
		{"exactly the minimum is kept", true, minMetricsIntervalMs, minMetricsIntervalMs},
		{"exactly the maximum is kept", true, maxMetricsIntervalMs, maxMetricsIntervalMs},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeMetricsInterval(tc.enabled, tc.requested); got != tc.want {
				t.Fatalf("normalizeMetricsInterval(%v, %d) = %d, want %d", tc.enabled, tc.requested, got, tc.want)
			}
		})
	}
}

// TestManager_SetClientMetricsPreference_RejectsStaleConnection pins the
// identity guard: a replaced connection's read loop must not write its
// declaration onto the newer connection's subscription. Without the guard, a
// tab that had the panel open would keep the sampler running after its socket
// was replaced.
func TestManager_SetClientMetricsPreference_RejectsStaleConnection(t *testing.T) {
	mgr := NewManagerForTest()

	// First connection declares interest.
	oldConn := acceptRealConn(t)
	var wmu1 sync.Mutex
	if mgr.Subscribe(oldConn, &wmu1, "same-id", "") == nil {
		t.Fatal("expected the first subscription")
	}
	if !mgr.SetClientMetricsPreference("same-id", oldConn, true, defaultMetricsIntervalMs) {
		t.Fatal("expected the first declaration to be recorded")
	}
	if count, _ := mgr.MetricsDemand(); count != 1 {
		t.Fatalf("precondition failed: expected 1 watcher, got %d", count)
	}

	// A second connection replaces the first (same clientID).
	newConn := acceptRealConn(t)
	var wmu2 sync.Mutex
	if mgr.Subscribe(newConn, &wmu2, "same-id", "") == nil {
		t.Fatal("expected the replacement to succeed")
	}
	// Subscribe reset the preference, so demand is now zero.
	if count, _ := mgr.MetricsDemand(); count != 0 {
		t.Fatalf("expected the replacement to clear demand, got %d", count)
	}

	// The OLD connection's late declaration must be rejected.
	if mgr.SetClientMetricsPreference("same-id", oldConn, true, defaultMetricsIntervalMs) {
		t.Fatal("expected a stale-connection declaration to be rejected")
	}
	if count, _ := mgr.MetricsDemand(); count != 0 {
		t.Fatalf("a stale declaration must not create demand, got %d", count)
	}

	// The CURRENT connection's declaration must still be accepted.
	if !mgr.SetClientMetricsPreference("same-id", newConn, true, defaultMetricsIntervalMs) {
		t.Fatal("expected the current connection's declaration to be recorded")
	}
	if count, _ := mgr.MetricsDemand(); count != 1 {
		t.Fatalf("expected the current declaration to take effect, got %d", count)
	}
}
