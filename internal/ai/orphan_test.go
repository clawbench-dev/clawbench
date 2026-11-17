package ai

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanupOrphans_SkipsProcessWithLivingParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("orphan process cleanup uses Unix-specific process signaling")
	}
	if testing.Short() {
		t.Skip("skipping orphan cleanup test in short mode")
	}

	// Start a subprocess WITH the CLAWBENCH_CHILD=1 env marker.
	// Since the test process (its parent) is still alive, CleanupOrphans
	// should NOT kill it — the process is actively managed, not orphaned.
	cmd := exec.Command("sleep", "300")
	cmd.Env = append(os.Environ(), OrphanChildEnvVar)
	require.NoError(t, cmd.Start())
	pid := cmd.Process.Pid
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	CleanupOrphans()

	// Process should still be alive — parent is alive so it's not an orphan
	proc, _ := os.FindProcess(pid)
	err := proc.Signal(syscall.Signal(0))
	assert.NoError(t, err, "process with living parent should NOT be killed")
}

// TestCleanupOrphans_KillsReParentedProcess verifies the cleanup contract: a
// process carrying the orphan marker whose parent is gone is killed, while one
// whose parent is alive survives (TestCleanupOrphans_SkipsProcessWithLivingParent).
//
// What makes a process count as abandoned is decided by isParentAlive from the
// CURRENT PPid, so the test must produce a child whose PPid really is dead.
// Two traps sit on that road, and the previous version of this test fell into
// the first one:
//
//  1. A child is re-parented to the nearest subreaper when its parent exits —
//     `systemd --user` on a desktop session, init in a container, or anything
//     holding PR_SET_CHILD_SUBREAPER. Adopted is still "parent alive", so
//     cleanup skips it. Asserting PPid==1 therefore asserted an init-is-the-
//     subreaper environment, failed on the PID value elsewhere, and then
//     reported the correct skip as "should have been killed".
//  2. If the test process is itself the subreaper, a dead parent produces the
//     same signal as a live one. So the parent is not a child of ours: an `sh`
//     launcher backgrounds a parent, `exec`s it, records its PID and exits. The
//     kernel re-parents the parent to the same subreaper this test process has,
//     and reaping is done by that subreaper, not by us.
//
// To prove the child was already abandoned BEFORE cleanup (rather than being
// abandoned by the cleanup under test) the child is gated on a pipe: it cannot
// start until the test writes to it, and by then the parent has been observed
// gone from /proc. The child must also still own the marker, so that cleanup
// matches it for the reason under test.
//
// Only the contract is asserted — child abandoned, marker present, child killed.
// The PID it was re-parented TO is infrastructure, so it is never asserted.
func TestCleanupOrphans_KillsReParentedProcess(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("orphan re-parenting test uses /proc which is Linux-specific")
	}
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
	// ExtraFiles hands releaseR to the child as fd 3; os/exec clears CLOEXEC on
	// that descriptor for us. The write end stays solely in this process, so the
	// child sees EOF if the test dies without releasing it.

	// The child is a re-exec of this test binary (helper mode) so its lifetime is
	// bounded by this binary's -timeout budget rather than lingering as a stray.
	helper := os.Getenv("CLAWBENCH_TEST_HELPER")
	if helper == "" {
		// The test binary's own path. The full argv[0] with -test.run=... cannot
		// be used with exec.Command, which splits on whitespace and needs a path.
		self, err := os.Executable()
		require.NoError(t, err)
		helper = self
	}
	// The parent is spawned directly by this process instead of by an `sh`
	// launcher. With a launcher, the background job was exec'd into the launcher
	// itself (so `$!` was a reused number) or was adopted by init, and the
	// adopted child was killed together with the launcher — a child of ours
	// cannot be reaped by anyone else, so "the parent is gone" below is a fact
	// this process establishes rather than a race it hopes to win.
	parent := exec.Command(helper, "-test.run=^TestCleanupOrphans_OrphanHelper$")
	parent.Env = append(os.Environ(),
		OrphanChildEnvVar,
		"GO_WANT_ORPHAN_HELPER=1",
		"CLAWBENCH_HELPER_PID_FILE="+pidFile,
		"CLAWBENCH_PARENT_PID_FILE="+parentPIDFile,
	)
	parent.ExtraFiles = []*os.File{releaseR}
	require.NoError(t, parent.Start())
	parentPID := parent.Process.Pid
	require.NoError(t, os.WriteFile(parentPIDFile, []byte(strconv.Itoa(parentPID)), 0o644))

	// The orphan (the parent's own child) must exist before the parent dies, so
	// that its PPid is a real dead pid afterwards instead of a reused number.
	orphanPID := waitForPIDFile(t, pidFile)
	// It has to survive its parent: the gate is what keeps it alive before the
	// release, and a child that dies with the parent would make the rest of the
	// test meaningless.
	orphanHelperReady(t, orphanPID)

	// The verify below reads its PID from the same file the launcher used.
	// The (grand)child is not released yet, so killing the parent leaves it
	// blocked on the gate with a dead PPid — the state under test.
	require.NoError(t, parent.Process.Kill())
	_, _ = parent.Process.Wait()

	// The parent must be gone before we release the child, otherwise "abandoned"
	// would be a claim about timing instead of the state under test. The parent
	// is not our child, so it is reaped by whichever process adopted it.
	waitForNoProcEntry(t, parentPID)

	// Release the child: it now starts as a process whose PPid is a dead PID.
	_, err = releaseW.Write([]byte("go"))
	require.NoError(t, err, "should be able to release the gated child")

	// Preconditions the cleanup decision rests on. If any fails, cleanup has
	// nothing to find and a green run would prove nothing, so fail loudly.
	ppid, state, err := procPPidAndState(orphanPID)
	require.NoError(t, err, "orphan process should exist before cleanup")
	require.NotEqual(t, "Z", state, "orphan should be running, not an exited zombie")
	// The contract is "the original parent is gone", not "the orphan now points
	// at some particular PID": the kernel hands a dead parent's children to the
	// nearest subreaper, which is init in a container and `systemd --user` on a
	// desktop session. Asserting a specific re-parent target made this test fail
	// on any machine where the target happened to be 1.
	require.NotEqual(t, os.Getpid(), ppid, "orphan must not be a child of the test process")
	require.False(t, isParentAlive(orphanPID),
		"orphan should have no living parent before cleanup (PPid=%d)", ppid)
	environ, err := os.ReadFile("/proc/" + strconv.Itoa(orphanPID) + "/environ")
	require.NoError(t, err, "should be able to read the orphan's environ")
	require.True(t, hasClawBenchChildMarker(environ),
		"orphan must carry the %s marker for cleanup to be expected to match", OrphanChildEnvVar)

	CleanupOrphans()

	// Poll for the process to disappear rather than sleeping a fixed amount: the
	// /proc entry goes away asynchronously once the new parent reaps the corpse.
	gone := assert.Eventually(t, func() bool {
		return !processAlive(orphanPID)
	}, 10*time.Second, 20*time.Millisecond,
		"abandoned process carrying the orphan marker should have been killed by CleanupOrphans")
	if !gone {
		_ = syscall.Kill(orphanPID, syscall.SIGKILL) // do not leak the helper on failure
	}
}

// TestCleanupOrphans_OrphanHelper is the abandoned child used by
// TestCleanupOrphans_KillsReParentedProcess. It records its own PID and its
// parent's PID, blocks until the test releases it over the inherited pipe, then
// waits to be killed by CleanupOrphans.
// It is not a test on its own — it is invoked via re-exec with GO_WANT_ORPHAN_HELPER=1.
func TestCleanupOrphans_OrphanHelper(t *testing.T) {
	if os.Getenv("GO_WANT_ORPHAN_HELPER") != "1" {
		return
	}

	// Gate on the pipe (fd 3) BEFORE forking, so the forked child inherits the
	// same gate and is held by it too. The child cannot be a re-exec of this
	// helper: this helper is its parent, and the parent has to die. A new process
	// group (`Setsid`) is what keeps the test's `require.NoError(parent.Kill())`
	// from taking the child down as a side effect via pgid propagation.
	gate := os.NewFile(3, "orphan_gate")
	orphanCmd := exec.Command("sleep", "600")
	// A child that carries the marker is what CleanupOrphans looks for.
	orphanCmd.Env = append(os.Environ(), OrphanChildEnvVar)
	orphanCmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := orphanCmd.Start(); err != nil {
		t.Fatalf("failed to start the orphan child: %v", err)
	}
	if err := os.WriteFile(os.Getenv("CLAWBENCH_HELPER_PID_FILE"), []byte(strconv.Itoa(orphanCmd.Process.Pid)), 0o644); err != nil {
		t.Fatalf("failed to write helper pid file: %v", err)
	}
	// Report the parent before anything else: the test needs it to prove the
	// parent actually died, not merely that this process was re-parented.
	if err := os.WriteFile(os.Getenv("CLAWBENCH_PARENT_PID_FILE"), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatalf("failed to write parent pid file: %v", err)
	}

	// Until the test releases us this read blocks, keeping "still parented" from
	// racing the precondition check.
	if gate != nil {
		buf := make([]byte, 1)
		_, _ = gate.Read(buf) // EOF or data both mean "released"
		_ = gate.Close()
	}

	// Wait to be killed. The deadline is this binary's own -timeout budget, so a
	// failure can never leak this process indefinitely; timing out is an honest
	// failure instead of a silent hang.
	deadline, ok := t.Deadline()
	if !ok {
		deadline = time.Now().Add(10 * time.Minute)
	}
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("orphan helper was not killed by CleanupOrphans")
}

// waitForNoProcEntry polls until <pid> is no longer a live process, failing the
// test on timeout. It deliberately accepts a zombie instead of demanding that
// /proc/<pid> disappear: a process only loses its /proc entry when its parent
// reaps it, and in a container the adopted parent is PID 1, which never reaps
// anything it did not fork. Waiting for "no /proc entry" therefore waits for
// something that will never happen off a real init, while "no longer running"
// is exactly the state the test needs. Zombies are also not killable (signal 0
// still answers), so the leak guard below must not fire on one. A PID at or
// below 1 is already the reaper itself: it was reaped and the number reused,
// which is not a process the test may signal.
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
		_ = syscall.Kill(pid, syscall.SIGKILL)
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

func TestCleanupOrphans_SkipsNormalProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("orphan process cleanup uses Unix-specific process signaling")
	}
	// Start a subprocess WITHOUT the marker
	cmd := exec.Command("sleep", "300")
	require.NoError(t, cmd.Start())
	pid := cmd.Process.Pid
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	CleanupOrphans()

	// Process should still be alive — Signal(0) on a live process
	// returns nil on Linux
	proc, _ := os.FindProcess(pid)
	err := proc.Signal(syscall.Signal(0))
	assert.NoError(t, err, "normal process should NOT be killed")
	cmd.Process.Kill()
	cmd.Wait()
}

// TestCleanupOrphans_DoesNotKillSelf is a regression test for a false-orphan
// self-kill: a ClawBench server restarted via build.sh --restart from inside
// a ClawBench-spawned subprocess inherits CLAWBENCH_CHILD=1 in its own
// environ. Once the detached restart parent exits, the new server is
// re-parented to PID 1 and its startup CleanupOrphans would kill itself.
//
// The test re-executes the test binary as a re-parented helper that carries
// the orphan marker, runs CleanupOrphans, and verifies it survives.
func TestCleanupOrphans_DoesNotKillSelf(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("orphan re-parenting test uses /proc which is Linux-specific")
	}
	if testing.Short() {
		t.Skip("skipping orphan cleanup test in short mode")
	}

	outFile := filepath.Join(t.TempDir(), "helper_alive")

	// Double-fork: an intermediate `sh` backgrounds the helper (test binary in
	// helper mode) carrying the orphan marker, then exits — re-parenting the
	// helper to PID 1 so it looks like a true orphan.
	helper := os.Args[0] + " -test.run=^TestCleanupOrphansHelper$"
	script := "env " + OrphanChildEnvVar +
		" GO_WANT_HELPER_PROCESS=1" +
		" CLAWBENCH_HELPER_OUT=" + outFile +
		" " + helper + " &"
	intermediate := exec.Command("sh", "-c", script)
	require.NoError(t, intermediate.Start())
	_ = intermediate.Wait()

	// Poll for the helper's success marker. It is written only if the helper
	// survived CleanupOrphans.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(outFile); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("CleanupOrphans killed the process running it (false-orphan self-kill)")
}

// TestCleanupOrphansHelper runs CleanupOrphans from inside a re-parented
// process that carries the CLAWBENCH_CHILD marker, then signals survival by
// writing CLAWBENCH_HELPER_OUT. It is not a test on its own — it is invoked
// by TestCleanupOrphans_DoesNotKillSelf via re-exec.
func TestCleanupOrphansHelper(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	// Give the intermediate sh time to exit so we are truly re-parented to PID 1.
	time.Sleep(500 * time.Millisecond)

	CleanupOrphans()

	// Reaching here means the process was not killed by its own cleanup.
	if err := os.WriteFile(os.Getenv("CLAWBENCH_HELPER_OUT"), []byte("alive"), 0o644); err != nil {
		t.Fatalf("failed to write helper output: %v", err)
	}
}

func TestIsParentAlive(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("isParentAlive uses Linux /proc")
	}

	// Current process's parent should be alive (the test runner)
	t.Run("living parent", func(t *testing.T) {
		assert.True(t, isParentAlive(os.Getpid()), "test process parent should be alive")
	})

	// PID 1's parent is 0 (kernel) — not a valid process, so isParentAlive returns false
	t.Run("init process has no parent", func(t *testing.T) {
		assert.False(t, isParentAlive(1), "init (PID 1) should have no living parent")
	})

	// Non-existent PID
	t.Run("nonexistent process", func(t *testing.T) {
		assert.False(t, isParentAlive(999999999), "nonexistent PID should return false")
	})
}

func TestHasClawBenchChildMarker(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{
			name: "exact match",
			data: append([]byte("PATH=/usr/bin\x00"), append([]byte(OrphanChildEnvVar), 0x00)...),
			want: true,
		},
		{
			name: "no marker",
			data: []byte("PATH=/usr/bin\x00HOME=/root\x00"),
			want: false,
		},
		{
			name: "marker at start",
			data: append([]byte(OrphanChildEnvVar), 0x00),
			want: true,
		},
		{
			name: "marker at end without trailing null",
			data: append([]byte("PATH=/usr/bin\x00"), []byte(OrphanChildEnvVar)...),
			want: true,
		},
		{
			name: "prefix false positive",
			// "FOO_CLAWBENCH_CHILD=1" should NOT match
			data: []byte("FOO_CLAWBENCH_CHILD=1\x00"),
			want: false,
		},
		{
			name: "empty data",
			data: []byte{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasClawBenchChildMarker(tt.data)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBytesContainsSep(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		target []byte
		sep    byte
		want   bool
	}{
		{
			name:   "single segment match",
			data:   []byte("abc\x00"),
			target: []byte("abc"),
			sep:    0,
			want:   true,
		},
		{
			name:   "middle segment match",
			data:   []byte("foo\x00bar\x00baz\x00"),
			target: []byte("bar"),
			sep:    0,
			want:   true,
		},
		{
			name:   "no match",
			data:   []byte("foo\x00bar\x00"),
			target: []byte("baz"),
			sep:    0,
			want:   false,
		},
		{
			name:   "prefix should not match",
			data:   []byte("foobar\x00"),
			target: []byte("bar"),
			sep:    0,
			want:   false,
		},
		{
			name:   "comma separated",
			data:   []byte("foo,bar,baz,"),
			target: []byte("bar"),
			sep:    ',',
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bytesContainsSep(tt.data, tt.target, tt.sep)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsClawBenchServerProcess(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{
			name: "clawbench server without --acp",
			// /proc/<pid>/cmdline: binary="clawbench", args="--port 20000"
			data: append([]byte("clawbench\x00"), []byte("--port\x0020000\x00")...),
			want: true,
		},
		{
			name: "clawbench with --acp is an agent, not server",
			data: append([]byte("clawbench\x00"), []byte("--acp\x00")...),
			want: false,
		},
		{
			name: "full path clawbench server",
			data: append([]byte("/opt/clawbench-green/clawbench\x00"), []byte("--port\x0020300\x00")...),
			want: true,
		},
		{
			name: "codebuddy with --acp is not a server",
			data: append([]byte("codebuddy\x00"), []byte("--acp\x00")...),
			want: false,
		},
		{
			name: "unrelated process",
			data: []byte("sleep\x00300\x00"),
			want: false,
		},
		{
			name: "empty data",
			data: []byte{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isClawBenchServerProcess(tt.data)
			assert.Equal(t, tt.want, got)
		})
	}
}

// orphanHelperReady waits until the helper has finished its one-time setup: it
// writes its own pid first and then the test needs the environment to be
// readable. Reading /proc/<pid>/environ or env of a just-forked child races its
// exec; retrying is the honest wait for "the helper is set up".
func orphanHelperReady(t *testing.T, pid int) {
	t.Helper()
	ok := assert.Eventually(t, func() bool {
		env, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
		return err == nil && hasClawBenchChildMarker(env)
	}, 10*time.Second, 20*time.Millisecond,
		"helper %d never reached the gated state", pid)
	if !ok {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}
