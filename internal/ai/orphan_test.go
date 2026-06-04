package ai

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanupOrphans_KillsRunningProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("orphan process cleanup uses Unix-specific process signaling")
	}
	if testing.Short() {
		t.Skip("skipping orphan cleanup test in short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("process group signaling differs on Windows")
	}

	// Start a subprocess WITH the CLAWBENCH_CHILD=1 env marker
	cmd := exec.Command("sleep", "300")
	cmd.Env = append(os.Environ(), OrphanChildEnvVar)
	require.NoError(t, cmd.Start())
	pid := cmd.Process.Pid

	CleanupOrphans()

	// Process should have been killed; Wait reaps it
	_ = cmd.Wait()

	// Verify the process is truly gone — Signal(0) should fail
	proc, _ := os.FindProcess(pid)
	err := proc.Signal(syscall.Signal(0))
	assert.Error(t, err, "orphan process should have been killed")
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
