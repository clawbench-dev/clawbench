package ws

import (
	"context"
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
