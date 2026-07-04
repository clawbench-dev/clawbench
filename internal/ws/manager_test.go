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
		readClientMessages(conn, "replace-test")
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
		readClientMessages(conn, "overflow")
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
		defer m.DisconnectClient("ws-client")
		// Keep connection alive — read loop discards incoming client messages
		readClientMessages(conn, "ws-client")
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
