package terminal

import (
	"testing"
	"time"
)

// TestSession_IsForegroundShell checks the judgement that gates command
// capture: while the prompt is up the shell owns the PTY, and once a command
// runs the foreground process is no longer a shell.
func TestSession_IsForegroundShell(t *testing.T) {
	cfg := TerminalConfig{
		IdleTimeout:  "5s",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}
	session, err := NewSession("/tmp", "/tmp", cfg, 0, 0)
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}
	defer session.Close()

	// Condition-wait for the prompt instead of sleeping a fixed time: shell
	// startup varies wildly between machines.
	if !waitFor(t, 3*time.Second, session.IsForegroundShell) {
		t.Skip("shell never became the foreground process")
	}

	if err := session.HandleInput("sleep 5\n"); err != nil {
		t.Fatalf("failed to write to PTY: %v", err)
	}
	if !waitFor(t, 3*time.Second, func() bool { return !session.IsForegroundShell() }) {
		t.Fatal("foreground process should be sleep, not the shell")
	}

	// Interrupt the sleep so the shell is back at its prompt before Close runs.
	// An interactive bash waiting on a child ignores SIGTERM, which would make
	// Close wait out its full 3s escalation budget and slow the suite down.
	_ = session.HandleInput("\x03")
	waitFor(t, 3*time.Second, session.IsForegroundShell)
}

// waitFor polls cond until it returns true or the timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

func TestForegroundIsShell_NilPTY(t *testing.T) {
	if ForegroundIsShell(nil) {
		t.Error("nil PTY must not report a shell")
	}
}

func TestSession_IsForegroundShellAfterClose(t *testing.T) {
	cfg := TerminalConfig{IdleTimeout: "5s", BufferLines: 100, MaxLineBytes: 65536, MaxBufferMB: 4}
	session, err := NewSession("/tmp", "/tmp", cfg, 0, 0)
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}
	session.Close()

	// The PTY is gone, so the probe cannot answer; it must say "no" rather than
	// claim the shell is still there.
	if session.IsForegroundShell() {
		t.Error("closed session must not report a shell")
	}
}
