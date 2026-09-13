//go:build !windows

package handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLaunchSentinel_RecordsLiveBinaryPath is a regression test for the
// restart-after-package-replacement case.
//
// The sentinel re-execs the binary after the current process exits. If it
// captured os.Executable(), a package manager that replaced the package
// mid-run (npm retires and deletes the old package directory) leaves it with a
// path to a deleted inode: all five exec retries fail and the service stays
// down permanently. The sentinel must therefore use the path recorded at
// startup, which npm never touches.
func TestLaunchSentinel_RecordsLiveBinaryPath(t *testing.T) {
	origBinDir := model.BinDir
	origDataDir := model.DataDir
	t.Cleanup(func() {
		model.BinDir = origBinDir
		model.DataDir = origDataDir
	})

	dir := t.TempDir()
	model.BinDir = dir
	model.DataDir = filepath.Join(dir, ".clawbench")

	// The live binary — what npm would have put back in place after replacing
	// the package directory.
	live := filepath.Join(dir, "live", "clawbench")
	require.NoError(t, os.MkdirAll(filepath.Dir(live), 0o755))
	require.NoError(t, os.WriteFile(live, []byte("binary"), 0o755))
	require.NoError(t, service.WriteSelfPath(live))

	cmd, err := LaunchSentinelProcess()
	if err != nil {
		t.Skipf("launchSentinel unavailable in this environment: %v", err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	require.NotNil(t, cmd.Process)

	// The sentinel script is passed to /bin/sh -c; inspect its argv to confirm
	// which path it was told to exec.
	args := strings.Join(cmd.Args, " ")
	assert.Contains(t, args, live,
		"sentinel must re-exec the recorded live path")
	assert.NotContains(t, args, "/.clawbench-",
		"sentinel must not reference a retired package directory")
}
