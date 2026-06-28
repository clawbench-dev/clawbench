package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"clawbench/internal/model"
)

// Manager manages terminal sessions for the application.
// It is a standalone service (not integrated with session_runtime.go) because
// terminal sessions have a fundamentally different lifecycle from AI sessions.
type Manager struct {
	mu          sync.Mutex
	sessions    map[string]*Session // keyed by session ID
	cfg         TerminalConfig
	port        int
	maxSessions int
}

// GlobalManager is the package-level singleton, set from main.go.
var GlobalManager *Manager

// TerminalConfig holds the terminal configuration.
// We define a local copy to avoid circular imports with the model package.
type TerminalConfig struct {
	Enabled      bool
	IdleTimeout  string
	BufferLines  int
	MaxLineBytes int
	MaxBufferMB  int
	MaxSessions  int
}

// NewManager creates a new terminal manager.
func NewManager(cfg model.TerminalConfig, port int) *Manager {
	tc := TerminalConfig{
		Enabled:      cfg.Enabled,
		IdleTimeout:  cfg.IdleTimeout,
		BufferLines:  cfg.BufferLines,
		MaxLineBytes: cfg.MaxLineBytes,
		MaxBufferMB:  cfg.MaxBufferMB,
		MaxSessions:  cfg.MaxSessions,
	}
	return &Manager{
		sessions:    make(map[string]*Session),
		cfg:         tc,
		port:        port,
		maxSessions: cfg.MaxSessions,
	}
}

// Close shuts down the manager and all active sessions.
func (m *Manager) Close() {
	m.mu.Lock()
	sessions := m.sessions
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()

	for _, s := range sessions {
		s.Close()
	}
	slog.Info("terminal: manager closed", slog.Int("sessions", len(sessions)))
}

// HandleWebSocket handles a WebSocket connection request.
// If sessionID is provided and the session still exists, the client reconnects
// to that session. Otherwise, a new session is created.
func (m *Manager) HandleWebSocket(w http.ResponseWriter, r *http.Request, projectPath, cwd, sessionID string) error {
	m.mu.Lock()

	// Check if terminal is disabled
	if !m.cfg.Enabled {
		m.mu.Unlock()
		return fmt.Errorf("terminal disabled")
	}

	var session *Session

	// Try to reconnect to existing session
	if sessionID != "" {
		if s, ok := m.sessions[sessionID]; ok && s.IsRunning() {
			session = s
			slog.Info(
				"terminal: reconnecting to existing session",
				slog.String("session", sessionID),
			)
		} else {
			// Session expired or closed — will create a new one below
			slog.Info(
				"terminal: session not found, creating new",
				slog.String("requested_session", sessionID),
			)
		}
	}

	// Create new session if needed
	if session == nil {
		// Enforce session limit
		if m.maxSessions > 0 && len(m.sessions) >= m.maxSessions {
			m.mu.Unlock()
			// Upgrade to WebSocket just to send the error, then close
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
			if err == nil {
				sendWSError(conn, ErrCodeSessionLimit, fmt.Sprintf("max sessions (%d) reached", m.maxSessions))
				_ = conn.Close(websocket.StatusPolicyViolation, "session limit")
			}
			return nil
		}

		newSession, err := NewSession(projectPath, cwd, m.cfg)
		if err != nil {
			m.mu.Unlock()
			// Send platform_unsupported instead of shell_start_failed
			// when the OS does not support PTY (e.g. Windows).
			errCode := ErrCodeShellFailed
			if errors.As(err, new(*PlatformError)) {
				errCode = ErrCodePlatformUnsupported
			}
			conn, wsErr := websocket.Accept(w, r, &websocket.AcceptOptions{
				OriginPatterns: []string{
					"http://" + r.Host,
					"https://" + r.Host,
					"http://localhost:*",
					"https://localhost:*",
					"http://127.0.0.1:*",
					"https://127.0.0.1:*",
				},
			})
			if wsErr == nil {
				sendWSError(conn, errCode, err.Error())
				_ = conn.Close(websocket.StatusInternalError, errCode)
			}
			return nil
		}

		// Set onClose callback so the session removes itself from the map
		// when the PTY process exits
		sid := newSession.ID()
		newSession.onClose = func() {
			m.mu.Lock()
			delete(m.sessions, sid)
			m.mu.Unlock()
		}

		m.sessions[sid] = newSession
		session = newSession
		slog.Info(
			"terminal: new session created",
			slog.String("session", sid),
			slog.String("project", projectPath),
			slog.String("cwd", cwd),
		)
	}

	m.mu.Unlock()

	// Upgrade to WebSocket. Keep this same-origin by default while allowing
	// localhost development frontends that proxy to the backend.
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
		return fmt.Errorf("websocket upgrade failed: %w", err)
	}

	// Connect to the session (will kick any zombie client from reconnect race)
	if err := session.Connect(conn); err != nil {
		sendWSError(conn, ErrCodeShellFailed, err.Error())
		_ = conn.Close(websocket.StatusInternalError, "connect failed")
		return nil
	}

	// Handle WebSocket messages in a goroutine
	go m.handleClientMessages(session, conn)

	return nil
}

// handleClientMessages reads messages from the WebSocket and dispatches them.
// The conn parameter is passed to Disconnect so it only closes the connection
// it owns — not a newer connection that replaced it (reconnect race).
func (m *Manager) handleClientMessages(session *Session, conn *websocket.Conn) {
	defer session.Disconnect(conn)

	ctx := context.Background()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			// Client disconnected or error
			return
		}

		var msg ClientMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			slog.Debug("terminal: invalid client message", slog.String("error", err.Error()))
			continue
		}

		switch msg.Type {
		case "input":
			if err := session.HandleInput(msg.Data); err != nil {
				slog.Debug("terminal: input error", slog.String("error", err.Error()))
			}
		case "resize":
			if err := session.HandleResize(msg.Cols, msg.Rows); err != nil {
				slog.Debug("terminal: resize error", slog.String("error", err.Error()))
			}
		case "close":
			sid := session.ID()
			session.Close()
			m.mu.Lock()
			delete(m.sessions, sid)
			m.mu.Unlock()
			return
		}
	}
}

// CloseSessionByID closes a specific terminal session by ID.
func (m *Manager) CloseSessionByID(id string) {
	m.mu.Lock()
	session, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()

	if ok {
		session.Close()
	}
}

// CloseAllSessions closes all terminal sessions.
func (m *Manager) CloseAllSessions() {
	m.mu.Lock()
	sessions := m.sessions
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()

	for _, s := range sessions {
		s.Close()
	}
}

// SessionStatus returns info about a specific session.
func (m *Manager) SessionStatus(id string) (found bool, cwd string, running bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[id]
	if !ok {
		return false, "", false
	}
	if !s.IsRunning() {
		delete(m.sessions, id)
		return false, "", false
	}
	return true, s.Cwd(), true
}

// AllSessionStatus returns info about all active sessions.
func (m *Manager) AllSessionStatus() []SessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []SessionInfo
	for id, s := range m.sessions {
		if !s.IsRunning() {
			delete(m.sessions, id)
			continue
		}
		result = append(result, SessionInfo{
			ID:      id,
			Cwd:     s.Cwd(),
			Running: true,
		})
	}
	return result
}

// SessionInfo describes a terminal session for API responses.
type SessionInfo struct {
	ID      string `json:"id"`
	Cwd     string `json:"cwd"`
	Running bool   `json:"running"`
}

// Config returns the terminal configuration for the frontend.
// Thread-safe: acquires read lock since Reconfigure writes m.cfg.
func (m *Manager) Config() TerminalConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// IsEnabled returns whether the terminal feature is enabled.
// Thread-safe: acquires read lock since Reconfigure writes m.cfg.
func (m *Manager) IsEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.Enabled
}

// Reconfigure updates the terminal configuration at runtime.
// Existing sessions keep their original config; only new sessions
// will use the updated settings.
func (m *Manager) Reconfigure(cfg model.TerminalConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = TerminalConfig{
		Enabled:      cfg.Enabled,
		IdleTimeout:  cfg.IdleTimeout,
		BufferLines:  cfg.BufferLines,
		MaxLineBytes: cfg.MaxLineBytes,
		MaxBufferMB:  cfg.MaxBufferMB,
		MaxSessions:  cfg.MaxSessions,
	}
	m.maxSessions = cfg.MaxSessions
}

// SessionCount returns the number of active terminal sessions.
func (m *Manager) SessionCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for id, s := range m.sessions {
		if !s.IsRunning() {
			delete(m.sessions, id)
			continue
		}
		count++
	}
	return count
}

// sendWSError sends an error message over a WebSocket connection.
func sendWSError(conn *websocket.Conn, code, message string) {
	msg := ServerMessage{
		Type:    "error",
		ErrCode: code,
		Message: message,
	}
	data, _ := json.Marshal(msg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = conn.Write(ctx, websocket.MessageText, data)
}
