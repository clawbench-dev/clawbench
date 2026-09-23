package terminal

import (
	"testing"
	"time"
)

// waitForCwd polls session.Cwd() until it equals want, or fails after 3s.
// Shell state changes asynchronously (the PTY echoes the command, then the
// shell runs it), so a fixed sleep would be both flaky and slow.
func waitForCwd(t *testing.T, session *Session, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		last = session.Cwd()
		if last == want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for cwd %q, last saw %q", want, last)
}

// TestSession_Cwd_TracksShellCd is the regression test for the bug this probe
// exists to fix: Cwd() used to return the directory the session was launched
// in, forever. It asserts the reported directory follows `cd`.
func TestSession_Cwd_TracksShellCd(t *testing.T) {
	if !cwdProbeSupported {
		t.Skip("live cwd probe not supported on this platform")
	}

	launchDir := t.TempDir()
	targetDir := t.TempDir()
	// The assertion is only meaningful if the two differ — otherwise a frozen
	// launch directory would pass.
	if launchDir == targetDir {
		t.Fatal("test setup invalid: launch and target directories are equal")
	}

	cfg := TerminalConfig{
		IdleTimeout:  testIdleTimeout,
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}
	session, err := NewSession(launchDir, launchDir, cfg, 0, 0)
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}
	defer session.Close()

	waitForCwd(t, session, launchDir)

	if err := session.HandleInput("cd " + targetDir + "\n"); err != nil {
		t.Fatalf("failed to send cd: %v", err)
	}
	waitForCwd(t, session, targetDir)
}

// TestSession_Cwd_FallsBackToLaunchDir covers the branches where no live probe
// is possible: no PTY at all (closed/exited session) or an unsupported
// platform. Both must return the launch directory rather than an empty string.
func TestSession_Cwd_FallsBackToLaunchDir(t *testing.T) {
	session := &Session{cwd: "/launch/dir"}

	// ptmx is nil — this is the state after waitProcess() reaps the shell.
	if got := session.Cwd(); got != "/launch/dir" {
		t.Errorf("expected fallback /launch/dir, got %q", got)
	}
}

// TestForegroundCwd_NilPTY pins the nil guard: Cwd() calls this with whatever
// ptmx snapshot it took, and a nil file must be an error (not a panic) so the
// fallback path is taken.
func TestForegroundCwd_NilPTY(t *testing.T) {
	if _, err := foregroundCwd(nil); err == nil {
		t.Error("expected an error for a nil PTY, got nil")
	}
}

// TestCwdProbeSupported_MatchesPlatform documents the platform contract the
// frontend gates on: the probe works on Linux/Android only, because those
// expose /proc/<pid>/cwd. macOS would need cgo libproc, and Windows never
// reaches a PTY session at all.
func TestCwdProbeSupported_MatchesPlatform(t *testing.T) {
	want := runtimeGOOS == "linux" || runtimeGOOS == "android"
	if CwdProbeSupported() != want {
		t.Errorf("CwdProbeSupported() = %v, want %v for GOOS=%s", CwdProbeSupported(), want, runtimeGOOS)
	}
}
