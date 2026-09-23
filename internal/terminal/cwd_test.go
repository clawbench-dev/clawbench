package terminal

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
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

// TestForegroundCwd_NonTTY covers the TIOCGPGRP failure branch: a file that is
// not a terminal cannot report a foreground process group. This is the guard
// that keeps a bogus fd from being read as pid 0 and probing /proc/0/cwd.
func TestForegroundCwd_NonTTY(t *testing.T) {
	if !cwdProbeSupported {
		t.Skip("live cwd probe not supported on this platform")
	}

	f, err := os.CreateTemp(t.TempDir(), "not-a-tty")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer f.Close()

	if _, err := foregroundCwd(f); err == nil {
		t.Error("expected an error for a non-TTY file, got nil")
	}
}

// TestForegroundCwd_NoForegroundProcessGroup covers the pgrp <= 0 branch: a PTY
// that has been opened but has no session attached yet reports process group 0.
// Returning that would make the caller readlink /proc/0/cwd, so it must be an
// error instead.
func TestForegroundCwd_NoForegroundProcessGroup(t *testing.T) {
	if !cwdProbeSupported {
		t.Skip("live cwd probe not supported on this platform")
	}

	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}
	defer ptmx.Close()
	defer tty.Close()

	if _, err := foregroundCwd(ptmx); err == nil {
		t.Error("expected an error for a PTY with no foreground process group, got nil")
	}
}

// TestForegroundCwd_StripsDeletedSuffix is the regression test for the kernel's
// "(deleted)" marker: when the directory is removed after the shell entered it,
// the /proc symlink target ends in " (deleted)". Returning that verbatim would
// hand callers a path that does not exist, so the probe must strip it.
func TestForegroundCwd_StripsDeletedSuffix(t *testing.T) {
	if !cwdProbeSupported {
		t.Skip("live cwd probe not supported on this platform")
	}

	dir := t.TempDir()
	cmd := exec.Command("bash", "-i")
	cmd.Env = append(os.Environ(), "PS1=PROBE$ ", "TERM=dumb")
	cmd.Dir = dir
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Skipf("PTY not available in this environment: %v", err)
	}
	defer ptmx.Close()
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()

	// Wait for the shell to settle so TIOCGPGRP reports it, not an intermediate
	// state, and the readlink sees the directory we are about to remove.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if got, err := foregroundCwd(ptmx); err == nil && got == dir {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for the shell to enter %q", dir)
		}
		time.Sleep(25 * time.Millisecond)
	}

	// t.TempDir cleanup tolerates an already-removed directory, so removing it
	// here is safe. The shell's cwd is now a deleted directory.
	if err := os.Remove(dir); err != nil {
		t.Fatalf("failed to remove the shell's cwd: %v", err)
	}

	got, err := foregroundCwd(ptmx)
	if err != nil {
		t.Fatalf("foregroundCwd after removal: %v", err)
	}
	// The literal kernel marker, not the package's own constant: asserting
	// against our constant would still pass if someone changed it to the wrong
	// string. (It is also the only form that compiles on non-Linux, where the
	// constant does not exist — this test skips there at runtime.)
	if strings.HasSuffix(got, " (deleted)") {
		t.Errorf("foregroundCwd returned %q, still carrying the deleted marker", got)
	}
	if got != dir {
		t.Errorf("foregroundCwd = %q, want %q", got, dir)
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
