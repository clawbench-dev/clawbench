package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeShell writes an executable /bin/sh script that exports the given
// assignments before handing off to a real shell.
func fakeShell(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fakeshell")
	script := "#!/bin/sh\n" + body + "\nexec /bin/sh \"$@\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake shell: %v", err)
	}
	return path
}

// isolateShell points $SHELL at path and $HOME at an empty directory, so the
// test never executes the developer's real startup files.
func isolateShell(t *testing.T, path string) {
	t.Helper()
	orig, hadShell := os.LookupEnv("SHELL")
	t.Cleanup(func() {
		if hadShell {
			_ = os.Setenv("SHELL", orig)
		} else {
			_ = os.Unsetenv("SHELL")
		}
		resetLoginShellEnvCache()
	})
	_ = os.Setenv("SHELL", path)
	t.Setenv("HOME", t.TempDir())
	resetLoginShellEnvCache()
}

func writeHomeFile(t *testing.T, name, content string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestParseShellEnv(t *testing.T) {
	t.Run("nul separated keeps values containing newlines", func(t *testing.T) {
		out := []byte(shellEnvMarker + "\nAK=abc\x00MULTI=line1\nline2\x00JUNK\x00")
		got := parseShellEnv(out)
		want := []string{"AK=abc", "MULTI=line1\nline2"}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("startup banner cannot swallow the first variable", func(t *testing.T) {
		// A banner containing '=' used to merge with the first assignment into
		// one unparsable record, silently dropping that variable.
		out := []byte("Welcome user=root, today is Monday\n" + shellEnvMarker + "\nAK=abc\x00")
		got := parseShellEnv(out)
		if len(got) != 1 || got[0] != "AK=abc" {
			t.Fatalf("got %q, want [AK=abc]", got)
		}
	})

	t.Run("missing marker yields nothing", func(t *testing.T) {
		if got := parseShellEnv([]byte("AK=abc\x00")); len(got) != 0 {
			t.Fatalf("got %q, want empty", got)
		}
	})

	t.Run("newline separated ignores shell noise", func(t *testing.T) {
		out := []byte("banner\n" + shellEnvMarker + "\nAK=abc\n  \n=novalue\n1BAD=x\nOK_2=y\r\n")
		got := parseShellEnv(out)
		want := []string{"AK=abc", "OK_2=y"}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("drops shell bookkeeping variables", func(t *testing.T) {
		out := []byte(shellEnvMarker + "\nAK=abc\x00PWD=/tmp\x00OLDPWD=/home\x00SHLVL=2\x00_=/bin/env\x00PS1=$ \x00")
		got := parseShellEnv(out)
		want := []string{"AK=abc"}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

func TestMergeLoginShellEnv(t *testing.T) {
	t.Cleanup(resetLoginShellEnvCache)

	shellEnvMu.Lock()
	shellEnvCache = []string{"AK=from-bashrc", "PATH=/from/bashrc", "NEW_VAR=1"}
	shellEnvAt = time.Now()
	shellEnvReady = true
	shellEnvMu.Unlock()

	got := MergeLoginShellEnv([]string{"PATH=/server/path", "HOME=/home/user"})

	// Existing entries win, missing ones are filled in.
	want := []string{"PATH=/server/path", "HOME=/home/user", "AK=from-bashrc", "NEW_VAR=1"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}

	// Calling twice must not duplicate entries.
	again := MergeLoginShellEnv(got)
	if strings.Join(again, "|") != strings.Join(want, "|") {
		t.Fatalf("second merge changed env: %q", again)
	}
}

// TestLoginShellEnvReadsBashrc is the regression test for issue #619: the
// credentials live in ~/.bashrc, and bash does NOT read that file when invoked
// as a login shell.
func TestLoginShellEnvReadsBashrc(t *testing.T) {
	requireRealShell(t, "bash")
	isolateShell(t, resolveBash(t))
	writeHomeFile(t, ".bashrc", "export CLAWBENCH_TEST_AK=from-bashrc\n")

	got := LoginShellEnv()
	if !containsEntry(got, "CLAWBENCH_TEST_AK=from-bashrc") {
		t.Fatalf(".bashrc exports missing from captured env (%d entries)", len(got))
	}
}

func TestLoginShellEnvReadsProfile(t *testing.T) {
	requireRealShell(t, "bash")
	isolateShell(t, resolveBash(t))
	writeHomeFile(t, ".bash_profile", "export CLAWBENCH_TEST_PROFILE=from-profile\n")

	got := LoginShellEnv()
	if !containsEntry(got, "CLAWBENCH_TEST_PROFILE=from-profile") {
		t.Fatalf(".bash_profile exports missing from captured env (%d entries)", len(got))
	}
}

func TestLoginShellEnvCachesSnapshot(t *testing.T) {
	requireRealShell(t, "bash")
	isolateShell(t, resolveBash(t))
	writeHomeFile(t, ".bashrc", "export CLAWBENCH_TEST_CACHE=first\n")

	if got := LoginShellEnv(); !containsEntry(got, "CLAWBENCH_TEST_CACHE=first") {
		t.Fatalf("first capture missing export (%d entries)", len(got))
	}

	// Changing the startup file must not change the result until the TTL
	// expires — otherwise every AI turn would pay for a shell startup.
	writeHomeFile(t, ".bashrc", "export CLAWBENCH_TEST_CACHE=second\n")
	if got := LoginShellEnv(); !containsEntry(got, "CLAWBENCH_TEST_CACHE=first") {
		t.Fatalf("snapshot was not reused")
	}
}

// TestLoginShellEnvRefreshesInBackground pins the stale-while-revalidate
// contract: an expired snapshot is returned immediately and replaced off the
// caller's goroutine, which may hold a connection lock.
func TestLoginShellEnvRefreshesInBackground(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("login shell startup files are POSIX-only")
	}
	isolateShell(t, fakeShell(t, `export CLAWBENCH_TEST_SWR=first`))
	if got := LoginShellEnv(); !containsEntry(got, "CLAWBENCH_TEST_SWR=first") {
		t.Fatalf("initial capture missing export (%d entries)", len(got))
	}

	_ = os.Setenv("SHELL", fakeShell(t, `export CLAWBENCH_TEST_SWR=second`))
	forceShellEnvStale()

	start := time.Now()
	got := LoginShellEnv()
	elapsed := time.Since(start)
	if elapsed > time.Second {
		t.Fatalf("stale snapshot refresh blocked the caller for %v", elapsed)
	}
	if !containsEntry(got, "CLAWBENCH_TEST_SWR=first") {
		t.Fatalf("stale call returned %q, want the previous snapshot", got)
	}

	waitForShellEnvRefresh(t)
	if got := LoginShellEnv(); !containsEntry(got, "CLAWBENCH_TEST_SWR=second") {
		t.Fatalf("background refresh did not land, got %q", got)
	}
}

// TestLoginShellEnvKeepsSnapshotWhenRefreshFails: a transient failure must not
// throw away a working snapshot and leave agents credential-less for a full TTL.
func TestLoginShellEnvKeepsSnapshotWhenRefreshFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("login shell startup files are POSIX-only")
	}
	isolateShell(t, fakeShell(t, `export CLAWBENCH_TEST_KEEP=good`))
	if got := LoginShellEnv(); !containsEntry(got, "CLAWBENCH_TEST_KEEP=good") {
		t.Fatalf("initial capture missing export (%d entries)", len(got))
	}

	origTimeout := shellEnvTimeout
	shellEnvTimeout = 200 * time.Millisecond
	t.Cleanup(func() { shellEnvTimeout = origTimeout })

	_ = os.Setenv("SHELL", fakeShell(t, `exec sleep 30`))
	forceShellEnvStale()

	_ = LoginShellEnv()
	waitForShellEnvRefresh(t)

	if got := LoginShellEnv(); !containsEntry(got, "CLAWBENCH_TEST_KEEP=good") {
		t.Fatalf("failed refresh discarded the working snapshot: %q", got)
	}
}

// TestLoginShellEnvInteractivePassSurvivesSlowLogin: the .bashrc pass runs
// first, so a profile chain that eats the whole budget (nvm/conda/network home)
// must not starve the credentials this mechanism exists for.
func TestLoginShellEnvInteractivePassSurvivesSlowLogin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("login shell startup files are POSIX-only")
	}
	isolateShell(t, fakeShell(t, `case "$1" in
  -ilc) exec sleep 30 ;;
esac
export CLAWBENCH_TEST_FAST=ok`))

	origTimeout := shellEnvTimeout
	shellEnvTimeout = 300 * time.Millisecond
	t.Cleanup(func() { shellEnvTimeout = origTimeout })

	start := time.Now()
	got := LoginShellEnv()
	elapsed := time.Since(start)

	if !containsEntry(got, "CLAWBENCH_TEST_FAST=ok") {
		t.Fatalf("interactive pass was starved by the login pass (%d entries)", len(got))
	}
	if elapsed > 3*time.Second {
		t.Fatalf("timeout was not honored, took %v", elapsed)
	}
}

func TestLoginShellEnvTimesOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("login shell startup files are POSIX-only")
	}
	// `exec` so the timeout kills the only process holding the stdout pipe.
	isolateShell(t, fakeShell(t, `exec sleep 30`))

	origTimeout := shellEnvTimeout
	shellEnvTimeout = 200 * time.Millisecond
	t.Cleanup(func() { shellEnvTimeout = origTimeout })

	start := time.Now()
	got := LoginShellEnv()
	elapsed := time.Since(start)

	if len(got) != 0 {
		t.Fatalf("expected empty env on timeout, got %q", got)
	}
	// Two passes share one deadline, so the second one must not start a fresh
	// timeout after the first expired.
	if elapsed > 3*time.Second {
		t.Fatalf("timeout was not honored, took %v", elapsed)
	}
}

func TestMergeLoginShellEnvNoopOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only guard")
	}
	t.Cleanup(resetLoginShellEnvCache)
	base := []string{"A=1"}
	if got := MergeLoginShellEnv(base); strings.Join(got, "|") != "A=1" {
		t.Fatalf("got %q", got)
	}
}

func forceShellEnvStale() {
	shellEnvMu.Lock()
	shellEnvAt = time.Now().Add(-shellEnvTTL - time.Second)
	shellEnvMu.Unlock()
}

func waitForShellEnvRefresh(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		shellEnvMu.Lock()
		busy := shellEnvRefreshing
		shellEnvMu.Unlock()
		if !busy {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("background shell env refresh did not finish")
}

func requireRealShell(t *testing.T, name string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shells only")
	}
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not installed", name)
	}
}

func resolveBash(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash not installed: %v", err)
	}
	return path
}

func containsEntry(env []string, want string) bool {
	for _, kv := range env {
		if kv == want {
			return true
		}
	}
	return false
}
