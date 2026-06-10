package ai

import (
	"os"
	"os/exec"
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

func TestCleanupOrphans_KillsReParentedProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("orphan process cleanup uses Unix-specific process signaling")
	}
	if runtime.GOOS == "darwin" {
		t.Skip("orphan re-parenting test uses /proc which is Linux-specific")
	}
	if testing.Short() {
		t.Skip("skipping orphan cleanup test in short mode")
	}

	// Create a true orphan via double-fork:
	// The intermediate process starts sleep with CLAWBENCH_CHILD=1 in the
	// background, then exits. The sleep process is re-parented to PID 1.
	// We write the grandchild PID to a temp file so we can verify it was killed.
	tmpFile := t.TempDir() + "/orphan_pid"
	script := `env ` + OrphanChildEnvVar + ` sh -c 'echo $$ > ` + tmpFile + `; exec sleep 300' &`
	intermediate := exec.Command("sh", "-c", script)
	require.NoError(t, intermediate.Start())

	// Wait for the intermediate to exit (it backgrounds the sleep and returns)
	_ = intermediate.Wait()
	time.Sleep(300 * time.Millisecond)

	// Read the orphaned PID
	pidData, err := os.ReadFile(tmpFile)
	require.NoError(t, err, "should have written orphan PID to temp file")
	var orphanPID int
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	require.NoError(t, err, "invalid PID")
	orphanPID = pid

	// Verify it's truly orphaned (PPid should be 1)
	ppid, _, err := readProcStatus(orphanPID)
	require.NoError(t, err, "orphan process should still exist before cleanup")
	assert.Equal(t, 1, ppid, "process should be re-parented to PID 1")

	// CleanupOrphans should find and kill the orphaned process
	CleanupOrphans()

	// Give the kernel a moment to clean up the process entry
	time.Sleep(100 * time.Millisecond)

	// Verify the process was killed by checking /proc
	_, err = os.ReadFile("/proc/" + strconv.Itoa(orphanPID) + "/stat")
	assert.Error(t, err, "re-parented orphan process should have been killed (no /proc entry)")
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
