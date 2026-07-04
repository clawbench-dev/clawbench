//go:build !windows

package handler

import (
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

// ---------- IsRunningUnderSupervisor ----------

func TestIsRunningUnderSupervisor_CLAWBENCH_NO_SUPERVISOR(t *testing.T) {
	t.Setenv("CLAWBENCH_NO_SUPERVISOR", "1")

	assert.False(t, IsRunningUnderSupervisor(), "CLAWBENCH_NO_SUPERVISOR=1 should return false")
}

func TestIsRunningUnderSupervisor_INVOCATION_ID(t *testing.T) {
	t.Setenv("CLAWBENCH_NO_SUPERVISOR", "")
	t.Setenv("INVOCATION_ID", "test-id")

	assert.True(t, IsRunningUnderSupervisor(), "INVOCATION_ID set should return true")
}

func TestIsRunningUnderSupervisor_ContainerEnv(t *testing.T) {
	t.Setenv("CLAWBENCH_NO_SUPERVISOR", "")
	t.Setenv("container", "docker")

	assert.True(t, IsRunningUnderSupervisor(), "container env set should return true")
}

func TestIsRunningUnderSupervisor_NoIndicators(t *testing.T) {
	t.Setenv("CLAWBENCH_NO_SUPERVISOR", "")
	t.Setenv("INVOCATION_ID", "")
	t.Setenv("container", "")

	// Under normal test execution (not PID 1, no dockerenv), should be false
	// unless running in CI with these indicators set
	result := IsRunningUnderSupervisor()
	// Can't assert exact value since PID 1 check depends on environment,
	// but it should not panic
	_ = result
}

func TestIsRunningUnderSupervisor_DockerenvFile(t *testing.T) {
	t.Setenv("CLAWBENCH_NO_SUPERVISOR", "")
	t.Setenv("INVOCATION_ID", "")
	t.Setenv("container", "")

	// Create /.dockerenv temporarily
	if os.Getuid() == 0 {
		_ = os.WriteFile("/.dockerenv", []byte{}, 0o644)
		defer func() { _ = os.Remove("/.dockerenv") }()
		assert.True(t, IsRunningUnderSupervisor(), "/.dockerenv exists should return true")
	}
	// If not root, we can't create /.dockerenv, so just verify it doesn't panic
	IsRunningUnderSupervisor()
}

// ---------- shellQuote ----------

func TestShellQuote(t *testing.T) {
	assert.Equal(t, "'hello'", shellQuote("hello"))
	assert.Equal(t, "''", shellQuote(""))
	assert.Equal(t, `'it'\''s'`, shellQuote("it's"))
	assert.Equal(t, `'a'\''b'\''c'`, shellQuote("a'b'c"))
}

// ---------- joinArgs ----------

func TestJoinArgs(t *testing.T) {
	assert.Equal(t, "", joinArgs(nil))
	assert.Equal(t, "'hello'", joinArgs([]string{"hello"}))
	assert.Equal(t, "'hello' 'world'", joinArgs([]string{"hello", "world"}))
	assert.Equal(t, `'it'\''s' 'nice'`, joinArgs([]string{"it's", "nice"}))
}

// ---------- LaunchSentinelProcess ----------

func TestLaunchSentinelProcess_StartsAndExits(t *testing.T) {
	// Set up a minimal BinDir for the sentinel to reference
	origBinDir := model.BinDir
	origDataDir := model.DataDir
	tmpDir := t.TempDir()
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	defer func() { model.BinDir = origBinDir; model.DataDir = origDataDir }()

	cmd, err := LaunchSentinelProcess()
	if err != nil {
		// In some environments (e.g., containers without /bin/sh), this may fail
		t.Skipf("launchSentinel failed (expected in some environments): %v", err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	// Verify the sentinel process was started
	if cmd.Process == nil {
		t.Fatal("expected process to be non-nil")
	}
	if cmd.Process.Pid <= 0 {
		t.Fatalf("expected valid PID, got %d", cmd.Process.Pid)
	}

	// Kill the sentinel immediately — we just needed to verify it starts
	if err := cmd.Process.Kill(); err != nil {
		t.Logf("warning: failed to kill sentinel process: %v", err)
	}
}

// ---------- maskAPIKey ----------

func TestMaskAPIKey_Empty(t *testing.T) {
	assert.Equal(t, "", maskAPIKey(""))
}

func TestMaskAPIKey_Short(t *testing.T) {
	assert.Equal(t, "****", maskAPIKey("abc"))
	assert.Equal(t, "****", maskAPIKey("1234567"))
}

func TestMaskAPIKey_LongEnough(t *testing.T) {
	result := maskAPIKey("abcdefgh")
	assert.Equal(t, "abcd***fgh", result)
}

func TestMaskAPIKey_16Chars(t *testing.T) {
	result := maskAPIKey("abcdefghijklmnop")
	assert.Equal(t, "abcd***nop", result)
}
