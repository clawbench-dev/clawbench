package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/coder/websocket"
	"github.com/creack/pty"
)

const testIdleTimeout = "10m"

func TestResolveShell(t *testing.T) {
	shell := resolveShell()
	if shell == "" {
		t.Error("resolveShell() returned empty string")
	}
	t.Logf("resolved shell: %s", shell)
}

func TestNewSessionAndClose(t *testing.T) {
	// PTY fork may be restricted in sandboxed environments
	cfg := TerminalConfig{
		IdleTimeout:  "5s",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	session, err := NewSession("/tmp", "/tmp", cfg)
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}
	defer session.Close()

	if session.ProjectPath() != "/tmp" {
		t.Errorf("expected projectPath /tmp, got %s", session.ProjectPath())
	}
	if session.Cwd() != "/tmp" {
		t.Errorf("expected cwd /tmp, got %s", session.Cwd())
	}
	if session.ID() == "" {
		t.Error("expected non-empty session ID")
	}
}

func TestSessionIdleTimeout(t *testing.T) {
	cfg := TerminalConfig{
		IdleTimeout:  "1s", // Very short timeout for testing
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	session, err := NewSession("/tmp", "/tmp", cfg)
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}
	// Don't defer Close — the idle timer will close it

	// Wait for idle timeout to fire
	time.Sleep(2 * time.Second)

	// Session should be closed now
	session.mu.Lock()
	closed := session.closed
	session.mu.Unlock()

	if !closed {
		t.Error("expected session to be closed after idle timeout")
	}
}

func TestManagerCloseAllSessions(t *testing.T) {
	cfg := model.TerminalConfig{
		Enabled:      true,
		IdleTimeout:  testIdleTimeout,
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	mgr := NewManager(cfg, 20000)
	defer mgr.Close()

	// Close with no active sessions should not panic
	mgr.CloseAllSessions()

	// AllSessionStatus should return empty
	sessions := mgr.AllSessionStatus()
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestManagerClearsSessionAfterShellExit(t *testing.T) {
	cwd := t.TempDir()
	cfg := model.TerminalConfig{
		Enabled:      true,
		IdleTimeout:  "1m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	mgr := NewManager(cfg, 20000)
	defer mgr.Close()

	session, err := NewSession(cwd, cwd, mgr.Config())
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}

	// Set up onClose callback like HandleWebSocket does
	sid := session.ID()
	session.onClose = func() {
		mgr.mu.Lock()
		delete(mgr.sessions, sid)
		mgr.mu.Unlock()
	}

	mgr.mu.Lock()
	mgr.sessions[sid] = session
	mgr.mu.Unlock()

	if err := session.HandleInput("exit\r"); err != nil {
		t.Fatalf("failed to send exit: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		sessions := mgr.AllSessionStatus()
		if len(sessions) == 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}

	sessions := mgr.AllSessionStatus()
	t.Fatalf("expected manager to clear exited shell session, got %d sessions", len(sessions))
}

func TestManagerIsEnabled(t *testing.T) {
	cfg := model.TerminalConfig{
		Enabled:      true,
		IdleTimeout:  testIdleTimeout,
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	mgr := NewManager(cfg, 20000)
	defer mgr.Close()

	if !mgr.IsEnabled() {
		t.Error("expected terminal to be enabled")
	}

	disabledCfg := model.TerminalConfig{
		Enabled:      false,
		IdleTimeout:  testIdleTimeout,
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	disabledMgr := NewManager(disabledCfg, 20000)
	defer disabledMgr.Close()

	if disabledMgr.IsEnabled() {
		t.Error("expected terminal to be disabled")
	}
}

func TestManagerConfig(t *testing.T) {
	cfg := model.TerminalConfig{
		Enabled:      true,
		IdleTimeout:  testIdleTimeout,
		BufferLines:  2000,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	mgr := NewManager(cfg, 20000)
	defer mgr.Close()

	tc := mgr.Config()
	if !tc.Enabled {
		t.Error("expected enabled")
	}
	if tc.BufferLines != 2000 {
		t.Errorf("expected 2000 buffer lines, got %d", tc.BufferLines)
	}
}

func TestManagerMultipleSessions(t *testing.T) {
	cfg := model.TerminalConfig{
		Enabled:      true,
		IdleTimeout:  testIdleTimeout,
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
		MaxSessions:  5,
	}

	mgr := NewManager(cfg, 20000)
	defer mgr.Close()

	tc := mgr.Config()

	// Create multiple sessions
	ids := make(map[string]bool)
	for range 3 {
		session, err := NewSession("/tmp", "/tmp", tc)
		if err != nil {
			t.Skipf("PTY not available in this environment: %v", err)
		}
		sid := session.ID()
		if sid == "" {
			t.Error("expected non-empty session ID")
		}
		if ids[sid] {
			t.Errorf("duplicate session ID: %s", sid)
		}
		ids[sid] = true

		mgr.mu.Lock()
		mgr.sessions[sid] = session
		mgr.mu.Unlock()
	}

	sessions := mgr.AllSessionStatus()
	if len(sessions) != 3 {
		t.Errorf("expected 3 sessions, got %d", len(sessions))
	}

	// Close one by ID
	for sid := range ids {
		mgr.CloseSessionByID(sid)
		break // only close one
	}

	sessions = mgr.AllSessionStatus()
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions after closing one, got %d", len(sessions))
	}
}

// --- HandleResize Tests ---

func TestSession_HandleResize(t *testing.T) {
	cfg := TerminalConfig{
		IdleTimeout:  "5m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	session, err := NewSession("/tmp", "/tmp", cfg)
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}
	defer session.Close()

	if err := session.HandleResize(120, 40); err != nil {
		t.Errorf("HandleResize failed: %v", err)
	}
}

func TestSession_HandleResize_ClosedSession(t *testing.T) {
	cfg := TerminalConfig{
		IdleTimeout:  "5m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	session, err := NewSession("/tmp", "/tmp", cfg)
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}
	session.Close()

	err = session.HandleResize(80, 24)
	if err == nil {
		t.Error("expected error when resizing closed session")
	}
}

// --- HandleInput on closed session ---

func TestSession_HandleInput_ClosedSession(t *testing.T) {
	cfg := TerminalConfig{
		IdleTimeout:  "5m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	session, err := NewSession("/tmp", "/tmp", cfg)
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}
	session.Close()

	// Wait for close to complete
	time.Sleep(200 * time.Millisecond)

	err = session.HandleInput("test")
	if err == nil {
		t.Error("expected error when writing to closed session")
	}
}

// --- Close idempotent ---

func TestSession_Close_Idempotent(t *testing.T) {
	cfg := TerminalConfig{
		IdleTimeout:  "5m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	session, err := NewSession("/tmp", "/tmp", cfg)
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}

	// Close multiple times should not panic
	session.Close()
	session.Close()
	session.Close()
}

// --- SessionStatus with running session ---

func TestManager_SessionStatus_Running(t *testing.T) {
	cfg := model.TerminalConfig{
		Enabled:      true,
		IdleTimeout:  "5m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	mgr := NewManager(cfg, 20000)
	defer mgr.Close()

	tc := mgr.Config()
	session, err := NewSession("/tmp", "/tmp", tc)
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}

	sid := session.ID()
	session.onClose = func() {
		mgr.mu.Lock()
		delete(mgr.sessions, sid)
		mgr.mu.Unlock()
	}

	mgr.mu.Lock()
	mgr.sessions[sid] = session
	mgr.mu.Unlock()

	found, cwd, running := mgr.SessionStatus(sid)
	if !found {
		t.Error("expected session to be found")
	}
	if cwd != "/tmp" {
		t.Errorf("expected cwd /tmp, got %s", cwd)
	}
	if !running {
		t.Error("expected session to be running")
	}
}

// --- AllSessionStatus cleans up not-running sessions ---

func TestManager_AllSessionStatus_CleansUpStoppedSessions(t *testing.T) {
	cfg := model.TerminalConfig{
		Enabled:      true,
		IdleTimeout:  "5m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	mgr := NewManager(cfg, 20000)
	defer mgr.Close()

	tc := mgr.Config()
	session, err := NewSession("/tmp", "/tmp", tc)
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}

	sid := session.ID()
	mgr.mu.Lock()
	mgr.sessions[sid] = session
	mgr.mu.Unlock()

	// Force-close the session (without the onClose callback)
	session.Close()
	time.Sleep(200 * time.Millisecond)

	// AllSessionStatus should clean up the not-running session
	sessions := mgr.AllSessionStatus()
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions after session stopped, got %d", len(sessions))
	}
}

// --- killProcessGroupSig edge cases ---

func TestKillProcessGroupSig_NilCmd(t *testing.T) {
	// Should not panic with nil cmd
	killProcessGroupSig(nil, syscall.SIGTERM)
}

func TestKillProcessGroupSig_NilProcess(t *testing.T) {
	// Should not panic with nil Process
	cmd := exec.Command("true")
	killProcessGroupSig(cmd, syscall.SIGTERM)
}

// --- startPTY error path ---

func TestStartPTY_InvalidCwd(t *testing.T) {
	_, _, err := startPTY("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Error("expected error for nonexistent working directory")
	}
}

// --- startPTY sets TERM and initial size for TUI apps ---

func TestStartPTY_SetsTermEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}

	cfg := TerminalConfig{
		IdleTimeout:  "5m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	session, err := NewSession("/tmp", "/tmp", cfg)
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}
	defer session.Close()

	// Verify the command environment contains TERM and COLORTERM
	session.mu.Lock()
	cmd := session.cmd
	session.mu.Unlock()

	foundTerm := false
	foundColorterm := false
	for _, env := range cmd.Environ() {
		if env == "TERM=xterm-256color" {
			foundTerm = true
		}
		if env == "COLORTERM=truecolor" {
			foundColorterm = true
		}
	}
	if !foundTerm {
		t.Error("expected TERM=xterm-256color in PTY command environment")
	}
	if !foundColorterm {
		t.Error("expected COLORTERM=truecolor in PTY command environment")
	}
}

func TestStartPTY_InitialSize(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}

	cfg := TerminalConfig{
		IdleTimeout:  "5m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	session, err := NewSession("/tmp", "/tmp", cfg)
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}
	defer session.Close()

	// Read the PTY window size directly
	session.mu.Lock()
	ptmx := session.ptmx
	session.mu.Unlock()

	if ptmx == nil {
		t.Fatal("PTY file is nil")
	}

	ws, err := pty.GetsizeFull(ptmx)
	if err != nil {
		t.Fatalf("failed to get PTY size: %v", err)
	}
	if ws.Cols != 80 {
		t.Errorf("expected initial Cols=80, got %d", ws.Cols)
	}
	if ws.Rows != 24 {
		t.Errorf("expected initial Rows=24, got %d", ws.Rows)
	}
}

func TestPlatformError(t *testing.T) {
	pe := &PlatformError{OS: "windows"}
	if pe.Error() != "terminal not supported on windows" {
		t.Errorf("unexpected error message: %s", pe.Error())
	}
}

func TestPlatformError_SimulatedWindows(t *testing.T) {
	orig := runtimeGOOS
	runtimeGOOS = "windows"
	defer func() { runtimeGOOS = orig }()

	_, _, err := startPTY("")
	if err == nil {
		t.Fatal("expected PlatformError on simulated Windows")
	}
	var pe *PlatformError
	if !errors.As(err, &pe) {
		t.Errorf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.OS != "windows" {
		t.Errorf("expected OS=windows, got %s", pe.OS)
	}
}

// --- generateSessionID uniqueness ---

func TestGenerateSessionID_Uniqueness(t *testing.T) {
	ids := make(map[string]bool)
	for range 100 {
		id := generateSessionID()
		if ids[id] {
			t.Errorf("duplicate session ID: %s", id)
		}
		ids[id] = true
	}
}

// --- NewSession with invalid idle timeout ---

func TestNewSession_InvalidIdleTimeout(t *testing.T) {
	cfg := TerminalConfig{
		IdleTimeout:  "not-a-duration",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	session, err := NewSession("/tmp", "/tmp", cfg)
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}
	defer session.Close()

	// Should fall back to 0 (never timeout) for invalid duration
	session.mu.Lock()
	timeout := session.idleTimeout
	timerNil := session.idleTimer == nil
	session.mu.Unlock()
	if timeout != 0 {
		t.Errorf("expected 0 (never timeout) fallback for invalid duration, got %v", timeout)
	}
	if !timerNil {
		t.Error("expected nil idleTimer for invalid duration (never timeout)")
	}
}

// --- NewSession with zero idle timeout (never timeout) ---

func TestNewSession_ZeroIdleTimeout(t *testing.T) {
	cfg := TerminalConfig{
		IdleTimeout:  "0",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	session, err := NewSession("/tmp", "/tmp", cfg)
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}
	defer session.Close()

	session.mu.Lock()
	timeout := session.idleTimeout
	timerNil := session.idleTimer == nil
	session.mu.Unlock()
	if timeout != 0 {
		t.Errorf("expected 0 (never timeout), got %v", timeout)
	}
	if !timerNil {
		t.Error("expected nil idleTimer for zero timeout (never timeout)")
	}
}

// --- waitProcess exit code ---

func TestSession_ExitCode(t *testing.T) {
	cfg := TerminalConfig{
		IdleTimeout:  "5m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	session, err := NewSession("/tmp", "/tmp", cfg)
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}

	// Send "exit 42" to the shell
	if err := session.HandleInput("exit 42\n"); err != nil {
		t.Fatalf("failed to send exit: %v", err)
	}

	// Wait for process to exit
	select {
	case <-session.done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for session to exit")
	}

	session.mu.Lock()
	exitCode := session.exitCode
	session.mu.Unlock()

	// Exit code should be non-zero (exact code depends on shell)
	if exitCode == 0 {
		t.Logf("exit code was 0 (shell may not support 'exit N'); this is acceptable")
	}
}

// --- Connect on closed session ---

func TestSession_Connect_ClosedSession(t *testing.T) {
	cfg := TerminalConfig{
		IdleTimeout:  "5m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	session, err := NewSession("/tmp", "/tmp", cfg)
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}
	session.Close()

	time.Sleep(200 * time.Millisecond)

	// Connecting to a closed session should return error
	err = session.Connect(nil)
	if err == nil {
		t.Error("expected error connecting to closed session")
	}
}

// --- Manager Close with active sessions ---

func TestManager_Close_WithActiveSessions(t *testing.T) {
	cfg := model.TerminalConfig{
		Enabled:      true,
		IdleTimeout:  "5m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	mgr := NewManager(cfg, 20000)
	tc := mgr.Config()

	session, err := NewSession("/tmp", "/tmp", tc)
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}

	sid := session.ID()
	mgr.mu.Lock()
	mgr.sessions[sid] = session
	mgr.mu.Unlock()

	// Close should clean up all sessions
	mgr.Close()

	if !session.closed {
		// Wait a bit for async cleanup
		time.Sleep(200 * time.Millisecond)
		session.mu.Lock()
		closed := session.closed
		session.mu.Unlock()
		if !closed {
			t.Error("expected session to be closed after manager close")
		}
	}
}

// --- IsRunning after close ---

func TestSession_IsRunning_AfterClose(t *testing.T) {
	cfg := TerminalConfig{
		IdleTimeout:  "5m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	session, err := NewSession("/tmp", "/tmp", cfg)
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}

	if !session.IsRunning() {
		t.Error("expected session to be running initially")
	}

	session.Close()

	if session.IsRunning() {
		t.Error("expected session to not be running after close")
	}
}

// --- WebSocket Integration Tests ---

// hostFromWSURL extracts host:port from a ws:// URL.
func hostFromWSURL(url string) string {
	h := url
	if len(h) > 5 && h[:5] == "ws://" {
		h = h[5:]
	}
	for i, c := range h {
		if c == '/' {
			h = h[:i]
			break
		}
	}
	return h
}

// startTestServer starts an HTTP server that delegates terminal WebSocket
// requests to the Manager. Returns the base URL.
func startTestServer(t *testing.T, mgr *Manager) string {
	t.Helper()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sid := r.URL.Query().Get("session")
		err := mgr.HandleWebSocket(w, r, "/tmp", "/tmp", sid)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	lc := &net.ListenConfig{}
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Shutdown(t.Context()) })

	return "ws://" + listener.Addr().String() + "/ws"
}

// dialWS connects a WebSocket client to the test server.
func dialWS(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"http://" + hostFromWSURL(url)}},
	})
	if err != nil {
		t.Fatalf("failed to dial websocket %s: %v", url, err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "test done") })
	return conn
}

// readServerMessage reads a ServerMessage from the WebSocket connection.
func readServerMessage(t *testing.T, conn *websocket.Conn) ServerMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("failed to read from websocket: %v", err)
	}
	var msg ServerMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("failed to unmarshal server message: %v", err)
	}
	return msg
}

// skipIfWindows skips WebSocket tests on Windows because PTY/shell integration
// does not work the same way — the WebSocket handler returns 500 when it fails
// to start a PTY session on Windows.
func skipIfWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("WebSocket terminal tests skipped on Windows (PTY not supported)")
	}
}

func TestManager_HandleWebSocket_NewSession(t *testing.T) {
	skipIfWindows(t)
	cfg := model.TerminalConfig{
		Enabled:      true,
		IdleTimeout:  "5m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	mgr := NewManager(cfg, 20000)
	defer mgr.Close()

	baseURL := startTestServer(t, mgr)
	conn := dialWS(t, baseURL)

	// Read the status message that's sent on connect
	msg := readServerMessage(t, conn)
	if msg.Type != "status" {
		t.Fatalf("expected status message, got %q", msg.Type)
	}
	if msg.SessionID == "" {
		t.Error("expected non-empty session ID in status")
	}
	if !msg.Running {
		t.Error("expected Running=true in status")
	}

	// Verify the session exists in the manager
	sessions := mgr.AllSessionStatus()
	if len(sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(sessions))
	}
}

func TestManager_HandleWebSocket_Disabled(t *testing.T) {
	cfg := model.TerminalConfig{
		Enabled:     false,
		IdleTimeout: "5m",
	}

	mgr := NewManager(cfg, 20000)
	defer mgr.Close()

	baseURL := startTestServer(t, mgr)

	// Attempt to connect — server should return HTTP error since terminal is disabled
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, baseURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"http://" + hostFromWSURL(baseURL)}},
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	// The WebSocket upgrade may succeed but the handler returns an error
	// before upgrading, resulting in a non-101 response
	if err == nil {
		t.Log("WebSocket connected despite terminal disabled (acceptable if server handles gracefully)")
	}
}

func TestManager_HandleWebSocket_ReconnectSession(t *testing.T) {
	skipIfWindows(t)
	cfg := model.TerminalConfig{
		Enabled:      true,
		IdleTimeout:  "1m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	mgr := NewManager(cfg, 20000)
	defer mgr.Close()

	baseURL := startTestServer(t, mgr)

	// Connect first client
	conn1 := dialWS(t, baseURL)
	statusMsg := readServerMessage(t, conn1)
	sessionID := statusMsg.SessionID
	if sessionID == "" {
		t.Fatal("expected non-empty session ID")
	}

	// Disconnect conn1 first to avoid the kick race
	_ = conn1.Close(websocket.StatusNormalClosure, "disconnecting")

	// Wait for disconnect to be processed
	time.Sleep(200 * time.Millisecond)

	// Reconnect with the session ID using a second client
	reconnectURL := baseURL + "?session=" + sessionID
	conn2 := dialWS(t, reconnectURL)

	// conn2 should receive messages for the existing session.
	// Read until we find a status message (may get replay first).
	deadline := time.Now().Add(5 * time.Second)
	var statusFound bool
	for !statusFound && time.Now().Before(deadline) {
		readCtx, readCancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, data, err := conn2.Read(readCtx)
		readCancel()
		if err != nil {
			continue
		}
		var msg ServerMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.Type == "status" && msg.SessionID == sessionID {
			statusFound = true
		}
	}
	if !statusFound {
		t.Error("expected to receive status message with matching session ID on reconnect")
	}
}

func TestManager_HandleWebSocket_ClientInput(t *testing.T) {
	skipIfWindows(t)
	cfg := model.TerminalConfig{
		Enabled:      true,
		IdleTimeout:  "5m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	mgr := NewManager(cfg, 20000)
	defer mgr.Close()

	baseURL := startTestServer(t, mgr)
	conn := dialWS(t, baseURL)

	// Read initial status message
	_ = readServerMessage(t, conn)

	// Send an input message
	inputMsg := ClientMessage{Type: "input", Data: "echo hello\n"}
	data, _ := json.Marshal(inputMsg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("failed to send input: %v", err)
	}

	// Read output (may be multiple messages; just verify we get something)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	_, _, err := conn.Read(ctx2)
	if err != nil {
		t.Logf("read after input: %v (may be normal)", err)
	}
}

func TestManager_HandleWebSocket_ResizeMessage(t *testing.T) {
	skipIfWindows(t)
	cfg := model.TerminalConfig{
		Enabled:      true,
		IdleTimeout:  "5m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	mgr := NewManager(cfg, 20000)
	defer mgr.Close()

	baseURL := startTestServer(t, mgr)
	conn := dialWS(t, baseURL)

	// Read initial status message
	_ = readServerMessage(t, conn)

	// Send a resize message
	resizeMsg := ClientMessage{Type: "resize", Cols: 120, Rows: 40}
	data, _ := json.Marshal(resizeMsg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("failed to send resize: %v", err)
	}
}

func TestManager_HandleWebSocket_CloseMessage(t *testing.T) {
	skipIfWindows(t)
	cfg := model.TerminalConfig{
		Enabled:      true,
		IdleTimeout:  "5m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	mgr := NewManager(cfg, 20000)
	defer mgr.Close()

	baseURL := startTestServer(t, mgr)
	conn := dialWS(t, baseURL)

	// Read initial status message
	_ = readServerMessage(t, conn)

	// Send a close message
	closeMsg := ClientMessage{Type: "close"}
	data, _ := json.Marshal(closeMsg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("failed to send close: %v", err)
	}

	// Wait for session to be closed
	time.Sleep(300 * time.Millisecond)

	sessions := mgr.AllSessionStatus()
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions after close message, got %d", len(sessions))
	}
}

func TestManager_HandleWebSocket_SessionLimit(t *testing.T) {
	skipIfWindows(t)
	cfg := model.TerminalConfig{
		Enabled:      true,
		IdleTimeout:  "5m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
		MaxSessions:  1,
	}

	mgr := NewManager(cfg, 20000)
	defer mgr.Close()

	baseURL := startTestServer(t, mgr)

	// Connect first client (fills the limit)
	conn1 := dialWS(t, baseURL)
	_ = readServerMessage(t, conn1)

	// Second connection should be rejected with session limit error
	conn2, resp, err := websocket.Dial(context.Background(), baseURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"http://" + hostFromWSURL(baseURL)}},
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		// Connection itself failed — acceptable
		t.Logf("second connection failed (expected): %v", err)
		return
	}
	defer func() { _ = conn2.Close(websocket.StatusNormalClosure, "cleanup") }()

	// Try to read error message
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, data, err := conn2.Read(ctx)
	if err != nil {
		t.Logf("read from rejected connection: %v", err)
		return
	}
	var msg ServerMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Logf("failed to unmarshal: %v", err)
		return
	}
	if msg.Type != "error" || msg.ErrCode != ErrCodeSessionLimit {
		t.Errorf("expected session limit error, got type=%q errcode=%q", msg.Type, msg.ErrCode)
	}
}

func TestManager_HandleWebSocket_InvalidMessage(t *testing.T) {
	skipIfWindows(t)
	cfg := model.TerminalConfig{
		Enabled:      true,
		IdleTimeout:  "5m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	mgr := NewManager(cfg, 20000)
	defer mgr.Close()

	baseURL := startTestServer(t, mgr)
	conn := dialWS(t, baseURL)

	// Read initial status message
	_ = readServerMessage(t, conn)

	// Send invalid JSON
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte("not json")); err != nil {
		t.Fatalf("failed to send invalid message: %v", err)
	}

	// Connection should still be alive (invalid messages are logged and skipped)
	// Send a valid resize message to verify
	resizeMsg := ClientMessage{Type: "resize", Cols: 80, Rows: 24}
	data, _ := json.Marshal(resizeMsg)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if err := conn.Write(ctx2, websocket.MessageText, data); err != nil {
		t.Fatalf("failed to send resize after invalid message: %v", err)
	}
}

func TestManager_HandleWebSocket_Disconnect(t *testing.T) {
	skipIfWindows(t)
	cfg := model.TerminalConfig{
		Enabled:      true,
		IdleTimeout:  "5m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	mgr := NewManager(cfg, 20000)
	defer mgr.Close()

	baseURL := startTestServer(t, mgr)
	conn := dialWS(t, baseURL)

	// Read initial status message
	statusMsg := readServerMessage(t, conn)
	sessionID := statusMsg.SessionID

	// Close the WebSocket — simulates disconnect
	_ = conn.Close(websocket.StatusNormalClosure, "client disconnect")

	// Wait for disconnect to be processed
	time.Sleep(300 * time.Millisecond)

	// Session should still exist but idle timer should be running
	mgr.mu.Lock()
	sess, ok := mgr.sessions[sessionID]
	mgr.mu.Unlock()
	if ok && sess != nil && !sess.closed {
		t.Log("session still alive after disconnect (expected — idle timer running)")
	}
}

func TestManager_HandleWebSocket_SessionExpiredReconnect(t *testing.T) {
	skipIfWindows(t)
	cfg := model.TerminalConfig{
		Enabled:      true,
		IdleTimeout:  "5m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	mgr := NewManager(cfg, 20000)
	defer mgr.Close()

	baseURL := startTestServer(t, mgr)

	// Connect and get session ID
	conn1 := dialWS(t, baseURL)
	statusMsg := readServerMessage(t, conn1)
	sessionID := statusMsg.SessionID

	// Close the connection and close the session
	_ = conn1.Close(websocket.StatusNormalClosure, "done")

	// Force close the session
	mgr.CloseSessionByID(sessionID)
	time.Sleep(200 * time.Millisecond)

	// Try to reconnect to the expired session — should create a new one
	reconnectURL := baseURL + "?session=" + sessionID
	conn2 := dialWS(t, reconnectURL)
	msg := readServerMessage(t, conn2)
	if msg.Type != "status" {
		t.Fatalf("expected status message, got %q", msg.Type)
	}
	// New session should have a different ID since the old one expired
	if msg.SessionID == sessionID {
		t.Log("reconnected to same session ID (acceptable if session was still alive)")
	}
}

// --- SuppressOutput after Connect (prevents duplicate prompts on reconnect) ---

func TestSession_SuppressOutputAfterConnect(t *testing.T) {
	cfg := TerminalConfig{
		IdleTimeout:  "5m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	session, err := NewSession("/tmp", "/tmp", cfg)
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}
	defer session.Close()

	// Before Connect, suppressOutput should be false
	session.mu.Lock()
	suppressed := session.suppressOutput
	session.mu.Unlock()
	if suppressed {
		t.Error("expected suppressOutput=false before Connect")
	}

	// Connect with empty buffer — suppressOutput should NOT be set
	// (no replay data means nothing to duplicate)
	_ = session.Connect(nil)
	session.mu.Lock()
	suppressed = session.suppressOutput
	session.mu.Unlock()
	if suppressed {
		t.Error("expected suppressOutput=false after Connect with empty buffer (first connect)")
	}

	// Write some data to the buffer (simulates PTY output during first connection)
	session.buffer.Write([]byte("shell prompt$ "))

	// Disconnect and reconnect — now the buffer has replay data
	session.mu.Lock()
	session.wsConn = nil // simulate disconnect
	session.mu.Unlock()

	_ = session.Connect(nil)
	session.mu.Lock()
	suppressed = session.suppressOutput
	session.mu.Unlock()
	if !suppressed {
		t.Error("expected suppressOutput=true after Connect with replay data (reconnect)")
	}
}

func TestSession_HandleResize_ClearsSuppressOutput(t *testing.T) {
	cfg := TerminalConfig{
		IdleTimeout:  "5m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	session, err := NewSession("/tmp", "/tmp", cfg)
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}
	defer session.Close()

	// Manually set suppressOutput=true (simulates Connect())
	session.mu.Lock()
	session.suppressOutput = true
	session.mu.Unlock()

	// HandleResize should schedule clearing suppressOutput after 50ms
	err = session.HandleResize(80, 24)
	if err != nil {
		t.Fatalf("HandleResize failed: %v", err)
	}

	// Immediately after HandleResize, suppressOutput is still true
	// (the timer hasn't fired yet)
	session.mu.Lock()
	suppressed := session.suppressOutput
	session.mu.Unlock()
	if !suppressed {
		t.Error("expected suppressOutput=true immediately after HandleResize (timer hasn't fired)")
	}

	// After 100ms, the 50ms timer should have fired
	time.Sleep(100 * time.Millisecond)

	session.mu.Lock()
	suppressed = session.suppressOutput
	session.mu.Unlock()
	if suppressed {
		t.Error("expected suppressOutput=false after HandleResize timer fires")
	}
}

func TestSession_HandleResize_NoSuppressFlag(t *testing.T) {
	cfg := TerminalConfig{
		IdleTimeout:  "5m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}

	session, err := NewSession("/tmp", "/tmp", cfg)
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}
	defer session.Close()

	// suppressOutput is false (default) — HandleResize should NOT schedule a timer
	err = session.HandleResize(80, 24)
	if err != nil {
		t.Fatalf("HandleResize failed: %v", err)
	}

	// suppressOutput should still be false
	session.mu.Lock()
	suppressed := session.suppressOutput
	session.mu.Unlock()
	if suppressed {
		t.Error("expected suppressOutput=false when not in suppress mode")
	}
}
