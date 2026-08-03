//go:build !windows

package handler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestIsRunningUnderSupervisor_ReparentedProcessIsNotSupervised is a regression
// test for the config-panel restart loop: a server launched by the sentinel
// process is re-parented to PID 1 (its launching parent exits right after
// exec). If PPid==1 is treated as "under a supervisor", the NEXT restart skips
// launching a sentinel and simply shuts down, leaving the service permanently
// down. A re-parented process with no supervisor indicators must NOT be
// considered supervised.
func TestIsRunningUnderSupervisor_ReparentedProcessIsNotSupervised(t *testing.T) {
	// Inside a real container the /.dockerenv/container indicators would make
	// IsRunningUnderSupervisor return true for legitimate reasons — cannot test here.
	if _, err := os.Stat("/.dockerenv"); err == nil {
		t.Skip("running inside a container — cannot assert re-parented non-supervised state")
	}
	if os.Getenv("container") != "" || os.Getenv("INVOCATION_ID") != "" {
		t.Skip("running with supervisor indicators set in environment")
	}

	outFile := filepath.Join(t.TempDir(), "supervisor_result")

	// Double-fork: an intermediate `sh` backgrounds the helper (test binary in
	// helper mode) with no supervisor indicators, then exits — re-parenting the
	// helper to PID 1 exactly like a sentinel-launched server.
	helper := os.Args[0] + " -test.run=^TestIsRunningUnderSupervisorHelper$"
	script := "GO_WANT_SUPERVISOR_HELPER=1 SUPERVISOR_OUT=" + outFile + " " + helper + " &"
	intermediate := exec.Command("sh", "-c", script)
	require.NoError(t, intermediate.Start())
	_ = intermediate.Wait()

	// Poll for the helper's result.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(outFile)
		if err == nil {
			assert.Equal(t, "not_supervised", strings.TrimSpace(string(data)),
				"re-parented process (PPid=1) must not be treated as under a supervisor")
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("helper did not produce a result")
}

// TestIsRunningUnderSupervisorHelper runs IsRunningUnderSupervisor from inside
// a re-parented process (PPid=1) with all supervisor indicators cleared, then
// writes the result. It is invoked by
// TestIsRunningUnderSupervisor_ReparentedProcessIsNotSupervised via re-exec.
func TestIsRunningUnderSupervisorHelper(t *testing.T) {
	if os.Getenv("GO_WANT_SUPERVISOR_HELPER") != "1" {
		return
	}
	// Give the intermediate sh time to exit so we are truly re-parented to PID 1.
	time.Sleep(500 * time.Millisecond)

	os.Unsetenv("CLAWBENCH_NO_SUPERVISOR")
	os.Unsetenv("INVOCATION_ID")
	os.Unsetenv("container")

	result := "not_supervised"
	if IsRunningUnderSupervisor() {
		result = "supervised"
	}
	if err := os.WriteFile(os.Getenv("SUPERVISOR_OUT"), []byte(result), 0o644); err != nil {
		t.Fatalf("failed to write result: %v", err)
	}
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

// ---------- maskAPIKey (removed) ----------
// maskAPIKey was removed — config API now returns full values for password fields.
// Frontend uses <input type="password"> for secure display instead of server-side masking.
