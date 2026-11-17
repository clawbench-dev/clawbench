package ai

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	assert.True(t, processAlive(pid), "process with living parent should NOT be killed")
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
	assert.True(t, processAlive(pid), "normal process should NOT be killed")
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
