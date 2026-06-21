package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"sync"
	"time"

	"clawbench/internal/model"

	"github.com/coder/websocket"
)

// clientIDPattern validates client_id query parameter.
var clientIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// EventsHandler handles the /api/ai/events/ws WebSocket endpoint.
// Auth is handled by middleware.Auth before this function is called.
// Query parameter "client_id" identifies the client device (fallback: "default").
func EventsHandler(w http.ResponseWriter, r *http.Request) {
	mgr := GetManager()
	if mgr == nil {
		http.Error(w, "events not initialized", http.StatusServiceUnavailable)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{
			"http://" + r.Host,
			"https://" + r.Host,
			"http://localhost:*",
			"https://localhost:*",
			"http://127.0.0.1:*",
			"https://127.0.0.1:*",
		},
	})
	if err != nil {
		slog.Error("ws: accept failed", "error", err)
		return
	}

	// Extract and validate client_id from query parameter
	clientID := r.URL.Query().Get("client_id")
	if clientID == "" || !clientIDPattern.MatchString(clientID) {
		clientID = "default"
	}

	// Extract user's locale preference for push notification i18n (ISS-129)
	locale := r.Header.Get("X-Locale")
	if locale == "" {
		if c, err := r.Cookie(model.ScopedCookieName("clawbench-locale")); err == nil {
			locale = c.Value
		}
	}

	var writeMu sync.Mutex
	sub := mgr.Subscribe(conn, &writeMu, clientID, locale)
	if sub == nil {
		// Subscription rejected (e.g. limit reached) — conn already closed by Subscribe
		return
	}
	defer mgr.DisconnectClient(clientID)

	// Replay buffered events on reconnect
	buffered := sub.GetBufferedEvents()
	if len(buffered) > 0 {
		slog.Debug("ws: replaying buffered events", "count", len(buffered), "client_id", clientID)
		for _, msg := range buffered {
			data, err := json.Marshal(msg)
			if err != nil {
				slog.Warn("ws: failed to marshal buffered event for replay", "error", err, "client_id", clientID)
				continue
			}
			writeMu.Lock()
			ctx2, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = conn.Write(ctx2, websocket.MessageText, data)
			cancel()
			writeMu.Unlock()
		}
	}

	// Ping ticker
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	// Ping goroutine
	go func() { //nolint:gosec // ping goroutine uses Background intentionally, not request-scoped
		for range pingTicker.C {
			writeMu.Lock()
			pingData, _ := json.Marshal(ServerMessage{Type: "ping"})
			ctx2, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := conn.Write(ctx2, websocket.MessageText, pingData)
			cancel()
			writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}()

	// Read client messages (blocks until disconnect).
	// Use the request context so the connection is closed when the client
	// disconnects or the server shuts down. Add an idle timeout to prevent
	// dead connections from lingering indefinitely (no client messages for 5min).
	readClientMessages(conn, mgr, clientID)

	_ = conn.Close(websocket.StatusNormalClosure, "handler exiting")
}

// readClientMessages reads messages from the WebSocket connection, resetting
// an idle timeout on each message. Extracted into a helper for clarity.
func readClientMessages(conn *websocket.Conn, mgr *Manager, clientID string) {
	for {
		// Create a fresh idle-timeout context for each read attempt.
		// Each cancel is called explicitly — no deferred cancel needed since
		// the loop re-creates the context on every iteration and calls
		// the previous cancel before creating a new one.
		readCtx, readCancel := context.WithTimeout(context.Background(), 5*time.Minute)

		_, data, err := conn.Read(readCtx)
		if err != nil {
			readCancel()
			slog.Debug("ws: client disconnected", "error", err, "client_id", clientID)
			return
		}

		var msg ClientMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			readCancel()
			slog.Warn("ws: invalid client message", "error", err, "client_id", clientID)
			continue
		}

		readCancel()

		switch msg.Type {
		case "ack":
			slog.Debug("ws: ack received", "id", msg.ID, "client_id", clientID)
		case "pong":
			// Connection alive
		case "register":
			// Client registering its JPush push registration ID
			if msg.PushRegID != "" {
				mgr.RegisterPushID(msg.PushRegID, clientID)
			}
		default:
			slog.Warn("ws: unknown client message type", "type", msg.Type, "client_id", clientID)
		}
	}
}
