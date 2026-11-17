//go:build linux

package ai

// Linux fixture for the orphan-cleanup tests in orphan_test.go.
//
// The tests themselves are skipped off Linux, but skipped tests still COMPILE:
// `syscall.Kill` and `syscall.SysProcAttr{Setsid}` do not exist on Windows, so
// anything that needs them lives behind this tag. A test file that cannot
// compile fails the whole job before a single Skip can run.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// leakGuard SIGKILLs the helper when a fixture step failed, so a red run does
// not leave a `sleep 600` behind. (The package's own killOrphan reports
// whether it worked and logs; a test just needs the signal.)
func leakGuard(pid int) {
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

// startDetachedOrphan forks the process that is supposed to be cleaned up.
//
// It runs in its OWN process group (Setsid): the test kills its parent to
// abandon it, and without a separate group the kill would propagate through the
// pgid and take the child down as a side effect — leaving the test asserting
// about a process that the fixture, not the cleanup, removed.
func startDetachedOrphan(t *testing.T) int {
	t.Helper()
	orphanCmd := exec.Command("sleep", "600")
	// A child that carries the marker is what CleanupOrphans looks for.
	orphanCmd.Env = append(os.Environ(), OrphanChildEnvVar)
	orphanCmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := orphanCmd.Start(); err != nil {
		t.Fatalf("failed to start the orphan child: %v", err)
	}
	return orphanCmd.Process.Pid
}

// waitForNoProcEntry polls until <pid> is no longer a live process, failing the
// test on timeout. It accepts a zombie instead of demanding that /proc/<pid>
// disappear: a process only loses its /proc entry when its parent reaps it, and
// in a container the adopted parent is PID 1, which never reaps anything it did
// not fork. "No longer running" is exactly the state the fixture needs.
// Zombies are also not killable (signal 0 still answers), so the leak guard
// must not fire on one. A PID at or below 1 is already the reaper: it was reaped
// and the number reused, which is not a process the test may signal.
func waitForNoProcEntry(t *testing.T, pid int) {
	t.Helper()
	if pid <= 1 {
		t.Fatalf("recorded PID %d is not a real process (the shell reported the reaper's pid; "+
			"$! was captured outside the subshell that backgrounds the parent)", pid)
	}
	gone := assert.Eventually(t, func() bool {
		_, s, err := procPPidAndState(pid)
		if err != nil {
			return true // no /proc entry at all
		}
		return s == "Z"
	}, 15*time.Second, 20*time.Millisecond,
		"process %d should no longer be running once its parent is reaped", pid)
	if !gone {
		leakGuard(pid)
	}
}

// procPPidAndState reads PPid and the single-letter process state from
// /proc/<pid>/status. State is "Z" for a zombie: it has exited but its parent
// has not reaped it yet, and it still answers signal 0.
func procPPidAndState(pid int) (ppid int, state string, err error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return 0, "", err
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "PPid:"); ok {
			_, _ = fmt.Sscanf(rest, "%d", &ppid)
		} else if rest, ok := strings.CutPrefix(line, "State:"); ok {
			if fields := strings.Fields(rest); len(fields) > 0 {
				state = fields[0]
			}
		}
	}
	return ppid, state, nil
}

// waitForPIDFile polls path until it contains a PID, failing the test on timeout.
func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
			require.NoError(t, convErr, "invalid PID in %s", path)
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatalf("PID was never written to %s: %v", path, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// orphanHelperReady waits until the helper has finished its one-time setup: it
// writes its own pid first and then the test needs the environment to be
// readable. Reading /proc/<pid>/environ of a just-forked child races its exec;
// retrying is the honest wait for "the helper is set up".
func orphanHelperReady(t *testing.T, pid int) {
	t.Helper()
	ok := assert.Eventually(t, func() bool {
		env, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
		return err == nil && hasClawBenchChildMarker(env)
	}, 10*time.Second, 20*time.Millisecond,
		"helper %d never reached the gated state", pid)
	if !ok {
		leakGuard(pid)
	}
}

// TestCleanupOrphans_KillsReParentedProcess drives the cleanup over a process
// whose original parent is gone.
//
// Four details are load-bearing, each of them a bug this test used to have:
//
//  1. The orphan's PPid is NOT asserted to be 1. A dead parent's children go to
//     the nearest subreaper: init in a container, `systemd --user` on a desktop
//     session, or anything holding PR_SET_CHILD_SUBREAPER. The cleanup's
//     contract is "the original parent is gone", so that is what is checked.
//  2. The parent is spawned by this test, not by an `sh` launcher. With a
//     launcher the recorded `$!` was read after the launcher had been waited
//     for, i.e. a PID that could already be reused — it read 1, and the test
//     then waited for init to exit.
//  3. The child is forked by the parent in its OWN process group, so killing
//     the parent does not take the child down through pgid propagation.
//  4. The child is gated on a pipe it shares with this test, so it cannot
//     reach the "released" state before the parent has been observed dead —
//     otherwise "abandoned" would be a claim about timing.
//
// Only the contract is asserted: child abandoned, marker present, child killed.
func TestCleanupOrphans_KillsReParentedProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping orphan cleanup test in short mode")
	}

	dir := t.TempDir()
	pidFile := filepath.Join(dir, "orphan_pid")
	parentPIDFile := filepath.Join(dir, "parent_pid")

	releaseR, releaseW, err := os.Pipe()
	require.NoError(t, err)
	defer releaseR.Close()
	defer releaseW.Close()
	// ExtraFiles hands releaseR to the helper as fd 3; os/exec clears CLOEXEC on
	// that descriptor for us, and the helper's own child inherits it. The write
	// end stays solely in this process, so the child sees EOF if the test dies
	// without releasing it.

	// The helper is a re-exec of this test binary (helper mode) so its lifetime
	// is bounded by this binary's -timeout budget rather than lingering as a
	// stray.
	self, err := os.Executable()
	require.NoError(t, err)
	parent := exec.Command(self, "-test.run=^TestCleanupOrphans_OrphanHelper$")
	parent.Env = append(
		os.Environ(),
		OrphanChildEnvVar,
		"GO_WANT_ORPHAN_HELPER=1",
		"CLAWBENCH_HELPER_PID_FILE="+pidFile,
		"CLAWBENCH_PARENT_PID_FILE="+parentPIDFile,
	)
	parent.ExtraFiles = []*os.File{releaseR}
	require.NoError(t, parent.Start())
	parentPID := parent.Process.Pid
	require.NoError(t, os.WriteFile(parentPIDFile, []byte(strconv.Itoa(parentPID)), 0o644))

	// The orphan must exist before the parent dies, so that its PPid is a real
	// dead pid afterwards rather than a reused number.
	orphanPID := waitForPIDFile(t, pidFile)
	// And it must survive its parent: the gate is what keeps it alive.
	orphanHelperReady(t, orphanPID)

	// The child is still gated, so killing the parent leaves it blocked with a
	// dead PPid — the state under test.
	require.NoError(t, parent.Process.Kill())
	_, _ = parent.Process.Wait()
	waitForNoProcEntry(t, parentPID)

	// Release the child: it now runs with a PPid that no longer exists.
	_, err = releaseW.WriteString("go")
	require.NoError(t, err, "should be able to release the gated child")

	// Preconditions the cleanup decision rests on. If any fails, cleanup has
	// nothing to find and a green run would prove nothing, so fail loudly.
	ppid, state, err := procPPidAndState(orphanPID)
	require.NoError(t, err, "orphan process should exist before cleanup")
	require.NotEqual(t, "Z", state, "orphan should be running, not an exited zombie")
	require.NotEqual(t, os.Getpid(), ppid, "orphan must not be a child of the test process")
	require.False(t, isParentAlive(orphanPID),
		"orphan should have no living parent before cleanup (PPid=%d)", ppid)
	environ, err := os.ReadFile("/proc/" + strconv.Itoa(orphanPID) + "/environ")
	require.NoError(t, err, "should be able to read the orphan's environ")
	require.True(t, hasClawBenchChildMarker(environ),
		"orphan must carry the %s marker for cleanup to be expected to match", OrphanChildEnvVar)

	CleanupOrphans()

	// Poll rather than sleep: the /proc entry disappears once the adopting
	// parent reaps the corpse.
	gone := assert.Eventually(t, func() bool {
		return !processAlive(orphanPID)
	}, 10*time.Second, 20*time.Millisecond,
		"abandoned process carrying the orphan marker should have been killed by CleanupOrphans")
	if !gone {
		leakGuard(orphanPID)
	}
}

// TestCleanupOrphans_OrphanHelper is the parent used by
// TestCleanupOrphans_KillsReParentedProcess. It forks the process that must be
// cleaned up, publishes both PIDs, and blocks until the test releases it.
//
// It is not a test on its own — it is re-exec'd with GO_WANT_ORPHAN_HELPER=1.
func TestCleanupOrphans_OrphanHelper(t *testing.T) {
	if os.Getenv("GO_WANT_ORPHAN_HELPER") != "1" {
		return
	}

	// Fork BEFORE waiting on the gate, so the child inherits the same gate and
	// is held by it too.
	orphanPID := startDetachedOrphan(t)
	if err := os.WriteFile(os.Getenv("CLAWBENCH_HELPER_PID_FILE"), []byte(strconv.Itoa(orphanPID)), 0o644); err != nil {
		t.Fatalf("failed to write helper pid file: %v", err)
	}
	if err := os.WriteFile(os.Getenv("CLAWBENCH_PARENT_PID_FILE"), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatalf("failed to write parent pid file: %v", err)
	}

	if gate := os.NewFile(3, "orphan_gate"); gate != nil {
		buf := make([]byte, 1)
		_, _ = gate.Read(buf) // EOF or data both mean "released"
		_ = gate.Close()
	}

	// Wait to be killed. Timing out is an honest failure instead of a silent
	// hang, and the deadline is this binary's own -timeout budget so a failure
	// can never leak this process indefinitely.
	deadline, ok := t.Deadline()
	if !ok {
		deadline = time.Now().Add(10 * time.Minute)
	}
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("orphan helper was not killed by CleanupOrphans")
}
