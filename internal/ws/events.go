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

// Chat stream callbacks — registered by the application at startup to break
// the import cycle between ws and service packages.
var (
	// OnSubscribe is called when a client subscribes to a session's streaming events.
	// It receives the Manager, clientID, and sessionID so it can emit cached state.
	OnSubscribe func(mgr *Manager, clientID, sessionID string)

	// OnCancelSession is called when a client sends a cancel message via WS.
	OnCancelSession func(sessionID string) bool

	// OnPermissionRespond is called when a client responds to a permission request via WS.
	// Returns an error if the permission was not found.
	OnPermissionRespond func(sessionID, toolCallID, optionID string, cancelled bool) error
)

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
	defer func() {
		mgr.DisconnectClient(clientID)
		mgr.StreamHub().UnsubscribeAll(clientID)
	}()

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
	// dead connections from lingering indefinitely (no client messages for 10min).
	// 10min accommodates the client's maximum app-layer ping interval (300s)
	// with safety margin for network latency and Doze maintenance windows.
	readClientMessages(mgr, conn, &writeMu, clientID)

	_ = conn.Close(websocket.StatusNormalClosure, "handler exiting")
}

// readClientMessages reads messages from the WebSocket connection, resetting
// an idle timeout on each message. Extracted into a helper for clarity.
func readClientMessages(mgr *Manager, conn *websocket.Conn, writeMu *sync.Mutex, clientID string) {
	for {
		// Create a fresh idle-timeout context for each read attempt.
		// Each cancel is called explicitly — no deferred cancel needed since
		// the loop re-creates the context on every iteration and calls
		// the previous cancel before creating a new one.
		readCtx, readCancel := context.WithTimeout(context.Background(), 10*time.Minute)

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
		case "subscribe":
			handleSubscribe(mgr, conn, writeMu, clientID, msg.SessionID)
		case "unsubscribe":
			mgr.StreamHub().Unsubscribe(clientID, msg.SessionID)
		case "cancel":
			handleCancelViaWS(msg.SessionID, clientID)
		case "permission_respond":
			handlePermissionRespondViaWS(msg, clientID)
		default:
			slog.Warn("ws: unknown client message type", "type", msg.Type, "client_id", clientID)
		}
	}
}

// handleSubscribe processes a subscribe client message: adds the client as
// a subscriber to the session and invokes the OnSubscribe callback for ACP state re-emit.
func handleSubscribe(mgr *Manager, _ *websocket.Conn, _ *sync.Mutex, clientID, sessionID string) {
	if sessionID == "" {
		slog.Warn("ws: subscribe with empty session_id", "client_id", clientID)
		return
	}

	mgr.StreamHub().Subscribe(clientID, sessionID)

	// Invoke registered callback for ACP state re-emit + stream_start
	if OnSubscribe != nil {
		OnSubscribe(mgr, clientID, sessionID)
	}

	slog.Debug("ws: client subscribed to session", "client_id", clientID, "session_id", sessionID)
}

// handleCancelViaWS processes a cancel client message by invoking the registered callback.
func handleCancelViaWS(sessionID, clientID string) {
	if sessionID == "" {
		slog.Warn("ws: cancel with empty session_id", "client_id", clientID)
		return
	}
	if OnCancelSession != nil {
		OnCancelSession(sessionID)
	}
	slog.Info("ws: session cancelled via WS", "session_id", sessionID, "client_id", clientID)
}

// handlePermissionRespondViaWS processes a permission_respond client message.
func handlePermissionRespondViaWS(msg ClientMessage, clientID string) {
	sessionID := msg.SessionID
	toolCallID := msg.ToolCallID
	if sessionID == "" || toolCallID == "" {
		slog.Warn("ws: permission_respond with missing fields", "client_id", clientID, "session_id", sessionID, "tool_call_id", toolCallID)
		return
	}

	if OnPermissionRespond != nil {
		if err := OnPermissionRespond(sessionID, toolCallID, msg.OptionID, msg.Cancelled); err != nil {
			slog.Warn("ws: permission_respond failed", "error", err, "client_id", clientID, "session_id", sessionID, "tool_call_id", toolCallID)
			return
		}
	}

	slog.Info("ws: permission responded via WS", "session_id", sessionID, "tool_call_id", toolCallID, "client_id", clientID)
}
