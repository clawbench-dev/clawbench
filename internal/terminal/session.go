package terminal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"
)

// re-export killProcessGroupSig as killProcessGroup for use in session.go
func killProcessGroup(cmd *exec.Cmd, sig syscall.Signal) {
	killProcessGroupSig(cmd, sig)
}

// Session represents a single PTY terminal session.
type Session struct {
	mu          sync.Mutex
	id          string
	projectPath string
	cwd         string
	cmd         *exec.Cmd
	ptmx        *os.File
	buffer      *RingBuffer
	wsConn      *websocket.Conn
	wsMu        sync.Mutex // protects wsConn writes
	idleTimer   *time.Timer
	idleTimeout time.Duration
	cancelRead  context.CancelFunc
	done        chan struct{}
	running     bool
	exitCode    int
	closed      bool
	onClose     func() // called by waitProcess after process exits (set by Manager)

	// suppressOutput is set after Connect() sends the replay buffer.
	// While true, readPTY() writes PTY output to the ring buffer but does
	// NOT forward it to the WebSocket client. This prevents the duplicate
	// prompt that appears when fit() after reconnect triggers SIGWINCH,
	// causing the shell to redraw its prompt on top of the replay data.
	// The flag is cleared shortly after the first HandleResize() call
	// (which is when SIGWINCH fires), once the redraw output has been
	// consumed and discarded by readPTY().
	suppressOutput      bool
	suppressSafetyTimer *time.Timer // safety timeout to prevent permanent suppression
}

// generateSessionID creates a random 8-byte hex string for session identification.
func generateSessionID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID (should never happen)
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// NewSession creates a new terminal session by starting a PTY in the given directory.
func NewSession(projectPath, cwd string, cfg TerminalConfig) (*Session, error) {
	idleTimeout, err := time.ParseDuration(cfg.IdleTimeout)
	if err != nil || idleTimeout == 0 {
		// 0 = never timeout; PTY lives until process exits or user explicitly closes.
		// Empty string or invalid values also default to never timing out.
		idleTimeout = 0
	}

	ptmx, cmd, err := startPTY(cwd)
	if err != nil {
		return nil, err
	}

	s := &Session{
		id:          generateSessionID(),
		projectPath: projectPath,
		cwd:         cwd,
		cmd:         cmd,
		ptmx:        ptmx,
		buffer:      NewRingBuffer(cfg.BufferLines, cfg.MaxLineBytes, cfg.MaxBufferMB*1024*1024),
		idleTimeout: idleTimeout,
		done:        make(chan struct{}),
		running:     true,
	}

	// Start idle timer only when timeout is configured (will be stopped when a client connects)
	if s.idleTimeout > 0 {
		s.idleTimer = time.AfterFunc(s.idleTimeout, func() {
			slog.Info(
				"terminal: session idle timeout",
				slog.String("project", s.projectPath),
				slog.String("session", s.id),
			)
			s.Close()
		})
	}

	// Start reading PTY output
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelRead = cancel
	go s.readPTY(ctx)

	// Monitor process exit
	go s.waitProcess()

	return s, nil
}

// ID returns the unique session identifier.
func (s *Session) ID() string {
	return s.id
}

// readPTY reads output from the PTY and broadcasts it to the WebSocket client
// while writing to the ring buffer.
func (s *Session) readPTY(ctx context.Context) {
	s.mu.Lock()
	ptmx := s.ptmx
	s.mu.Unlock()
	if ptmx == nil {
		return
	}

	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, err := ptmx.Read(buf)
		if n > 0 {
			data := buf[:n]

			// Always write to ring buffer (for future replays)
			s.buffer.Write(data)

			// Send to WebSocket client if connected, unless suppressed.
			// suppressOutput is set after Connect() sends the replay buffer
			// to prevent the duplicate prompt caused by SIGWINCH after fit().
			s.mu.Lock()
			suppressed := s.suppressOutput
			s.mu.Unlock()

			if suppressed {
				// Output discarded; will be sent once suppressOutput clears
			} else {
				msg := ServerMessage{
					Type: "output",
					Data: string(data),
				}
				s.sendToClient(msg)
			}
		}
		if err != nil {
			if err != io.EOF {
				slog.Debug("terminal: PTY read error", slog.String("error", err.Error()))
			}
			return
		}
	}
}

// waitProcess waits for the PTY process to exit and notifies the client.
// This is the only place that calls cmd.Wait(); Close() signals the process and
// waits on s.done to avoid racing or double-waiting on exec.Cmd.
func (s *Session) waitProcess() {
	err := s.cmd.Wait()
	exitCode := 0
	if err != nil {
		exitErr := &exec.ExitError{}
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}

	s.mu.Lock()
	alreadyClosed := s.closed
	s.running = false
	s.exitCode = exitCode
	s.closed = true
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	if s.cancelRead != nil {
		s.cancelRead()
	}
	ptmx := s.ptmx
	s.ptmx = nil
	onClose := s.onClose
	s.mu.Unlock()

	if !alreadyClosed {
		s.sendToClient(ServerMessage{
			Type: "exit",
			Code: exitCode,
		})
	}

	if ptmx != nil {
		_ = ptmx.Close()
	}
	s.buffer.Reset()
	close(s.done)

	slog.Info(
		"terminal: process exited",
		slog.String("project", s.projectPath),
		slog.String("session", s.id),
		slog.Int("exit_code", exitCode),
	)

	// Notify Manager to remove this session from the map
	if onClose != nil {
		onClose()
	}
}

// Connect attaches a WebSocket client to this session.
// If a previous client is still connected (e.g. zombie from disconnect race),
// it is kicked to allow the new client to take over.
func (s *Session) Connect(conn *websocket.Conn) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("session is closed")
	}

	// Kick any existing client — this handles the race where the old
	// WebSocket's read loop hasn't exited yet (e.g. reconnect scenario).
	// Use StatusReplaced so the old client's frontend knows not to auto-reconnect.
	if s.wsConn != nil {
		slog.Info(
			"terminal: kicking existing client for new connection",
			slog.String("session", s.id),
		)
		s.wsMu.Lock()
		_ = s.wsConn.Close(StatusReplaced, "replaced by new client")
		s.wsMu.Unlock()
		s.wsConn = nil
	}

	// Stop idle timer — we have a client now
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}

	s.wsConn = conn

	// Send replay buffer and suppress output to prevent duplicate prompts.
	// When the client reconnects, it receives the replay buffer (which
	// includes the shell prompt), then calls fit() which sends a resize
	// message. That resize triggers SIGWINCH, causing the shell to redraw
	// its prompt. By suppressing output until HandleResize() fires, the
	// redraw output is discarded by readPTY() instead of being sent as a
	// duplicate. HandleResize() will clear this flag after a short delay
	// to allow the SIGWINCH-induced output to be consumed.
	// Only suppress when there is actual replay data — on first connect
	// the buffer is empty and there's nothing to duplicate.
	replayData := s.buffer.Replay()
	if replayData != nil {
		s.suppressOutput = true
		s.sendToClientUnlocked(ServerMessage{
			Type: "replay",
			Data: string(replayData),
		})

		// Safety timeout: if HandleResize is never called (e.g. fit() fails
		// or the terminal dimensions don't change), clear suppressOutput
		// after 500ms so the client is not permanently stuck receiving no output.
		s.suppressSafetyTimer = time.AfterFunc(500*time.Millisecond, func() {
			s.mu.Lock()
			if s.suppressOutput {
				s.suppressOutput = false
				slog.Warn("terminal: suppressOutput safety timeout", slog.String("session", s.id))
			}
			s.mu.Unlock()
		})
	}

	// Send current status with session ID
	s.sendToClientUnlocked(ServerMessage{
		Type:      "status",
		SessionID: s.id,
		Cwd:       s.cwd,
		Running:   true,
	})

	return nil
}

// Disconnect removes the WebSocket client if it matches the given connection,
// and starts the idle timer.
// The conn parameter identifies which connection is disconnecting — if the
// session's active wsConn has been replaced by a newer client (reconnect race),
// we must NOT close the new connection.
func (s *Session) Disconnect(conn *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Only clear and close if the disconnecting connection is still the active one.
	// If a new client has already connected (replaced s.wsConn), this old
	// goroutine must not touch the new connection.
	if s.wsConn == conn {
		_ = s.wsConn.Close(websocket.StatusNormalClosure, "client disconnected")
		s.wsConn = nil
	}

	// Start idle timer if no clients are connected and timeout is configured
	if s.wsConn == nil && !s.closed && s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer.Reset(s.idleTimeout)
	}
}

// HandleInput processes an input message from the WebSocket client.
func (s *Session) HandleInput(data string) error {
	s.mu.Lock()
	ptmx := s.ptmx
	s.mu.Unlock()

	if ptmx == nil {
		return fmt.Errorf("PTY not available")
	}

	_, err := ptmx.WriteString(data)
	return err
}

// HandleResize processes a resize message from the WebSocket client.
// After a reconnect (where Connect() set suppressOutput), this is the first
// message the client sends (via fit()). Setsize() triggers SIGWINCH, which
// causes the shell to redraw its prompt. We keep suppressOutput=true briefly
// after Setsize() so readPTY() discards the redraw output, then clear it.
func (s *Session) HandleResize(cols, rows uint16) error {
	s.mu.Lock()
	ptmx := s.ptmx
	suppressing := s.suppressOutput
	s.mu.Unlock()

	if ptmx == nil {
		return fmt.Errorf("PTY not available")
	}

	err := pty.Setsize(ptmx, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})

	if suppressing {
		// Cancel the safety timer since HandleResize was called normally
		if s.suppressSafetyTimer != nil {
			s.suppressSafetyTimer.Stop()
			s.suppressSafetyTimer = nil
		}

		// Setsize() just sent SIGWINCH. The shell will redraw its prompt
		// and readPTY() will read that output within microseconds (same
		// machine, no network). 50ms is more than enough for the kernel
		// to deliver the signal, the shell to respond, and readPTY() to
		// consume and discard the output.
		time.AfterFunc(50*time.Millisecond, func() {
			s.mu.Lock()
			s.suppressOutput = false
			s.mu.Unlock()
		})
	}

	return err
}

// Close terminates the PTY process, closes the WebSocket, and cleans up resources.
func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.running = false

	slog.Info(
		"terminal: closing session",
		slog.String("project", s.projectPath),
		slog.String("session", s.id),
	)

	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	if s.suppressSafetyTimer != nil {
		s.suppressSafetyTimer.Stop()
		s.suppressSafetyTimer = nil
	}
	if s.cancelRead != nil {
		s.cancelRead()
	}
	cmd := s.cmd
	s.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		killProcessGroup(cmd, syscall.SIGTERM)

		select {
		case <-s.done:
			// Process exited cleanly; waitProcess performed process wait.
		case <-time.After(3 * time.Second):
			killProcessGroup(cmd, syscall.SIGKILL)
			select {
			case <-s.done:
			case <-time.After(1 * time.Second):
				slog.Warn(
					"terminal: process did not exit after SIGKILL",
					slog.String("project", s.projectPath),
					slog.String("session", s.id),
				)
			}
		}
	}

	s.mu.Lock()
	if s.ptmx != nil {
		_ = s.ptmx.Close()
		s.ptmx = nil
	}
	if s.wsConn != nil {
		_ = s.wsConn.Close(websocket.StatusNormalClosure, "session closed")
		s.wsConn = nil
	}
	s.mu.Unlock()

	s.buffer.Reset()
}

// sendToClient sends a message to the WebSocket client (thread-safe).
func (s *Session) sendToClient(msg ServerMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendToClientUnlocked(msg)
}

// sendToClientUnlocked sends a message without acquiring the mutex (caller must hold lock).
func (s *Session) sendToClientUnlocked(msg ServerMessage) {
	if s.wsConn == nil {
		return
	}

	data, err := json.Marshal(msg)
	if err != nil {
		slog.Error("terminal: failed to marshal message", slog.String("error", err.Error()))
		return
	}

	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.wsConn.Write(ctx, websocket.MessageText, data); err != nil {
		slog.Debug("terminal: failed to send to client", slog.String("error", err.Error()))
	}
}

// ProjectPath returns the project path this session belongs to.
func (s *Session) ProjectPath() string {
	return s.projectPath
}

// Cwd returns the current working directory of the session.
func (s *Session) Cwd() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cwd
}

// IsRunning reports whether the PTY process is still alive.
func (s *Session) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running && !s.closed
}
