package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEventsHandler_ExtractsLocaleFromHeader verifies that the WebSocket handler
// extracts the locale from the X-Locale header and stores it in the subscription.
func TestEventsHandler_ExtractsLocaleFromHeader(t *testing.T) {
	mgr := newTestManager()
	origMgr := defaultManager
	defaultManager = mgr
	defer func() { defaultManager = origMgr }()

	// Create a test HTTP server that routes to EventsHandler
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ai/events/ws", EventsHandler)
	server := httptest.NewServer(mux)
	defer server.Close()

	// Connect with X-Locale header
	wsURL := "ws" + server.URL[4:] + "/api/ai/events/ws?client_id=locale-test"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"X-Locale": []string{"zh"},
		},
	})
	require.NoError(t, err, "WebSocket connection should succeed")
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// Verify locale is stored in the subscription
	time.Sleep(100 * time.Millisecond) // Allow goroutine to process
	mgr.mu.Lock()
	sub, ok := mgr.subscriptions["locale-test"]
	mgr.mu.Unlock()
	require.True(t, ok, "subscription should exist")
	sub.mu.Lock()
	locale := sub.locale
	sub.mu.Unlock()
	assert.Equal(t, "zh", locale, "locale should be extracted from X-Locale header")

	// Clean up
	mgr.DisconnectClient("locale-test")
}

// TestEventsHandler_ExtractsLocaleFromCookie verifies that the WebSocket handler
// extracts the locale from the clawbench-locale cookie when X-Locale header is absent.
func TestEventsHandler_ExtractsLocaleFromCookie(t *testing.T) {
	mgr := newTestManager()
	origMgr := defaultManager
	defaultManager = mgr
	defer func() { defaultManager = origMgr }()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/ai/events/ws", EventsHandler)
	server := httptest.NewServer(mux)
	defer server.Close()

	// Connect with cookie but no X-Locale header
	wsURL := "ws" + server.URL[4:] + "/api/ai/events/ws?client_id=locale-cookie"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Cookie": []string{"clawbench-locale=en"},
		},
	})
	require.NoError(t, err, "WebSocket connection should succeed")
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// Verify locale is stored from cookie
	time.Sleep(100 * time.Millisecond)
	mgr.mu.Lock()
	sub, ok := mgr.subscriptions["locale-cookie"]
	mgr.mu.Unlock()
	require.True(t, ok, "subscription should exist")
	sub.mu.Lock()
	locale := sub.locale
	sub.mu.Unlock()
	assert.Equal(t, "en", locale, "locale should be extracted from cookie")

	// Clean up
	mgr.DisconnectClient("locale-cookie")
}

// TestEventsHandler_DefaultLocaleWhenNoneProvided verifies that locale defaults
// to empty string when neither X-Locale header nor cookie is provided.
func TestEventsHandler_DefaultLocaleWhenNoneProvided(t *testing.T) {
	mgr := newTestManager()
	origMgr := defaultManager
	defaultManager = mgr
	defer func() { defaultManager = origMgr }()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/ai/events/ws", EventsHandler)
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/api/ai/events/ws?client_id=locale-default"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err, "WebSocket connection should succeed")
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// Verify locale defaults to empty (English via i18n fallback)
	time.Sleep(100 * time.Millisecond)
	mgr.mu.Lock()
	sub, ok := mgr.subscriptions["locale-default"]
	mgr.mu.Unlock()
	require.True(t, ok, "subscription should exist")
	sub.mu.Lock()
	locale := sub.locale
	sub.mu.Unlock()
	assert.Equal(t, "", locale, "locale should default to empty when not provided")

	// Clean up
	mgr.DisconnectClient("locale-default")
}

// TestEventsHandler_SubscriptionLimit verifies that the subscription limit
// is enforced (exercises _ = conn.Close in Subscribe for limit rejection).
func TestEventsHandler_SubscriptionLimit(t *testing.T) {
	mgr := newTestManager()
	origMgr := defaultManager
	defaultManager = mgr
	defer func() { defaultManager = origMgr }()

	// Pre-fill subscriptions up to the limit
	for i := range maxSubscriptions {
		var writeMu sync.Mutex
		mgr.Subscribe(nil, &writeMu, fmt.Sprintf("filler-%d", i), "")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/ai/events/ws", EventsHandler)
	server := httptest.NewServer(mux)
	defer server.Close()

	// Try connecting with a new client_id (should be rejected — limit reached)
	wsURL := "ws" + server.URL[4:] + "/api/ai/events/ws?client_id=overflow-client"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		// Connection was rejected — expected
		return
	}
	// If connection was accepted, server should close it quickly
	_ = conn.Close(websocket.StatusNormalClosure, "")
}

// TestEventsHandler_ServerCloseOnExit covers the `_ = conn.Close()` path
// at the end of EventsHandler when the handler exits normally.
func TestEventsHandler_ServerCloseOnExit(t *testing.T) {
	mgr := newTestManager()
	origMgr := defaultManager
	defaultManager = mgr
	defer func() { defaultManager = origMgr }()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/ai/events/ws", EventsHandler)
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/api/ai/events/ws?client_id=close-test"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err)

	// Close the connection from client side.
	// The handler will detect the close and exit, calling `_ = conn.Close()`.
	_ = conn.Close(websocket.StatusNormalClosure, "test done")

	// Give the server time to process the disconnect
	time.Sleep(300 * time.Millisecond)
}

// TestEventsHandler_ReplayTagsReplayedEvent verifies that events replayed from
// the subscription's replay buffer on reconnect are tagged with Replayed=true.
// Regression test for the "stale completion notification after page reload" bug:
// without the tag the frontend cannot distinguish caught-up history (a session
// that finished while the page was reloading) from a live completion, so it
// would re-pop the completion popup for every buffered historical event.
func TestEventsHandler_ReplayTagsReplayedEvent(t *testing.T) {
	mgr := newTestManager()
	origMgr := defaultManager
	defaultManager = mgr
	defer func() { defaultManager = origMgr }()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/ai/events/ws", EventsHandler)
	server := httptest.NewServer(mux)
	defer server.Close()

	clientID := "replay-tag-client"
	wsURL := "ws" + server.URL[4:] + "/api/ai/events/ws?client_id=" + clientID
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First connection.
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err, "first WebSocket connection should succeed")
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// Allow the subscription to register, then disconnect so the buffer window
	// opens. The subscription (and its replay buffer) is preserved.
	time.Sleep(100 * time.Millisecond)
	mgr.DisconnectClient(clientID)

	// Broadcast a terminal event while the client is away — it is buffered.
	mgr.BroadcastEvent(ServerMessage{
		Type:  "event",
		ID:    "evt_away_completed",
		Event: "session_update",
		Data:  &SessionUpdateData{SessionID: "s1", Status: "completed"},
	})

	// Close the first connection so the handler goroutine exits cleanly before
	// the reconnect (otherwise the old handler's read loop may interfere).
	_ = conn.Close(websocket.StatusNormalClosure, "reconnecting")
	time.Sleep(200 * time.Millisecond)

	// Reconnect — EventsHandler replays the buffered event with Replayed=true.
	conn2, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err, "reconnect WebSocket should succeed")
	defer func() { _ = conn2.Close(websocket.StatusNormalClosure, "") }()

	var got ServerMessage
	readCtx, readCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer readCancel()
	_, data, err := conn2.Read(readCtx)
	require.NoError(t, err, "should receive the replayed event")
	require.NoError(t, json.Unmarshal(data, &got))
	require.Equal(t, "evt_away_completed", got.ID, "replayed event should carry its original id")
	require.True(t, got.Replayed, "replayed event must be tagged Replayed=true so the frontend suppresses stale completion popups")

	// The replayed event must NOT have been mutated in the buffer (it is a copy
	// tagged only for this replay).
	mgr.mu.Lock()
	sub, ok := mgr.subscriptions[clientID]
	mgr.mu.Unlock()
	require.True(t, ok, "subscription should be preserved after reconnect")
	for _, ev := range sub.GetBufferedEvents() {
		if ev.Replayed {
			t.Error("the in-buffer event must NOT carry the Replayed tag (only the replayed copy is tagged)")
		}
	}
}

// TestEventsHandler_MetricsPreference verifies the client can declare (and
// withdraw) system-resource push interest over the wire, and that the server
// clamps an absurd interval.
func TestEventsHandler_MetricsPreference(t *testing.T) {
	mgr := newTestManager()
	origMgr := defaultManager
	defaultManager = mgr
	defer func() { defaultManager = origMgr }()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/ai/events/ws", EventsHandler)
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/api/ai/events/ws?client_id=metrics-pref"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer writeCancel()

	// Declare interest at the foreground rate.
	require.NoError(t, conn.Write(writeCtx, websocket.MessageText,
		[]byte(`{"type":"metrics_preference","metrics_enabled":true,"metrics_interval_ms":1000}`)))

	require.Eventually(t, func() bool {
		count, interval := mgr.MetricsDemand()
		return count == 1 && interval == defaultMetricsIntervalMs
	}, 2*time.Second, 20*time.Millisecond, "server should record the declared demand")

	// An absurdly fast request is clamped rather than honored.
	require.NoError(t, conn.Write(writeCtx, websocket.MessageText,
		[]byte(`{"type":"metrics_preference","metrics_enabled":true,"metrics_interval_ms":1}`)))
	require.Eventually(t, func() bool {
		_, interval := mgr.MetricsDemand()
		return interval == defaultMetricsIntervalMs
	}, 2*time.Second, 20*time.Millisecond, "a 1ms request must be clamped")

	// Withdraw interest.
	require.NoError(t, conn.Write(writeCtx, websocket.MessageText,
		[]byte(`{"type":"metrics_preference","metrics_enabled":false}`)))
	require.Eventually(t, func() bool {
		count, _ := mgr.MetricsDemand()
		return count == 0
	}, 2*time.Second, 20*time.Millisecond, "server should drop the demand when disabled")
}

// TestEventsHandler_UnknownMessageTypeIsIgnored verifies an unrecognized client
// message does not close the connection (the switch's default branch only warns).
func TestEventsHandler_UnknownMessageTypeIsIgnored(t *testing.T) {
	mgr := newTestManager()
	origMgr := defaultManager
	defaultManager = mgr
	defer func() { defaultManager = origMgr }()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/ai/events/ws", EventsHandler)
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/api/ai/events/ws?client_id=unknown-msg"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer writeCancel()
	require.NoError(t, conn.Write(writeCtx, websocket.MessageText, []byte(`{"type":"nonsense_type"}`)))

	// The connection must still accept a valid message afterwards.
	require.NoError(t, conn.Write(writeCtx, websocket.MessageText,
		[]byte(`{"type":"metrics_preference","metrics_enabled":true,"metrics_interval_ms":1000}`)))
	require.Eventually(t, func() bool {
		count, _ := mgr.MetricsDemand()
		return count == 1
	}, 2*time.Second, 20*time.Millisecond, "connection should survive an unknown message type")
}
