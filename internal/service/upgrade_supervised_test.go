package service

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- replaceBinaryInPlace ---

func TestReplaceBinaryInPlace_RenameSuccess(t *testing.T) {
	dir := t.TempDir()
	newPath := filepath.Join(dir, "clawbench-new")
	target := filepath.Join(dir, "clawbench")
	require.NoError(t, os.WriteFile(newPath, []byte("new-binary"), 0o600))
	require.NoError(t, os.WriteFile(target, []byte("old-binary"), 0o600))

	origRename := upgradeRename
	upgradeRename = os.Rename
	defer func() { upgradeRename = origRename }()

	err := replaceBinaryInPlace(newPath, target)
	require.NoError(t, err)

	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "new-binary", string(data))

	info, err := os.Stat(target)
	require.NoError(t, err)
	// Executable permission is a Unix concept; on Windows os.Chmod does not
	// surface a 0755 mode, so only assert it where it is meaningful.
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
	}
}

func TestReplaceBinaryInPlace_CopyFallback(t *testing.T) {
	dir := t.TempDir()
	newPath := filepath.Join(dir, "clawbench-new")
	target := filepath.Join(dir, "clawbench")
	require.NoError(t, os.WriteFile(newPath, []byte("new-binary"), 0o600))
	require.NoError(t, os.WriteFile(target, []byte("old-binary"), 0o600))

	// Force rename to fail so the copy fallback path is exercised.
	origRename := upgradeRename
	upgradeRename = func(oldpath, newpath string) error { return syscall.EXDEV }
	defer func() { upgradeRename = origRename }()

	err := replaceBinaryInPlace(newPath, target)
	require.NoError(t, err)

	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "new-binary", string(data))

	info, err := os.Stat(target)
	require.NoError(t, err)
	// Executable permission is a Unix concept; on Windows os.Chmod does not
	// surface a 0755 mode, so only assert it where it is meaningful.
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
	}
}

// TestReplaceBinaryInPlace_CopyFallback_ReadOnlyTarget verifies the fallback
// succeeds when the target file is read-only but its directory is writable.
// The staged-copy implementation must not require the target file to be
// writable (that is the case that broke for a root-owned 0755 binary in a
// user-writable directory).
func TestReplaceBinaryInPlace_CopyFallback_ReadOnlyTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only file semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}

	dir := t.TempDir()
	newPath := filepath.Join(dir, "clawbench-new")
	target := filepath.Join(dir, "clawbench")
	require.NoError(t, os.WriteFile(newPath, []byte("new-binary"), 0o600))
	require.NoError(t, os.WriteFile(target, []byte("old-binary"), 0o400))
	defer func() { _ = os.Chmod(target, 0o600) }() // allow TempDir cleanup

	// Force rename to fail so the staged-copy fallback runs.
	origRename := upgradeRename
	upgradeRename = func(oldpath, newpath string) error { return syscall.EXDEV }
	defer func() { upgradeRename = origRename }()

	err := replaceBinaryInPlace(newPath, target)
	require.NoError(t, err)

	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "new-binary", string(data))

	info, err := os.Stat(target)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())

	// No staging temp file should be left behind.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".clawbench-replace-",
			"staging temp file must be cleaned up")
	}
}

func TestReplaceBinaryInPlace_CopyFallbackFails(t *testing.T) {
	dir := t.TempDir()
	newPath := filepath.Join(dir, "clawbench-new")
	target := filepath.Join(dir, "clawbench")
	// newPath does not exist → both rename and copy fail.
	require.NoError(t, os.WriteFile(target, []byte("old-binary"), 0o600))

	origRename := upgradeRename
	upgradeRename = func(oldpath, newpath string) error { return syscall.EXDEV }
	defer func() { upgradeRename = origRename }()

	err := replaceBinaryInPlace(newPath, target)
	assert.Error(t, err)
}

// --- performSupervisedUpgrade ---

func TestPerformSupervisedUpgrade_Success(t *testing.T) {
	dir := t.TempDir()
	newPath := filepath.Join(dir, "clawbench-new")
	target := filepath.Join(dir, "clawbench")
	require.NoError(t, os.WriteFile(newPath, []byte("new-binary"), 0o600))
	require.NoError(t, os.WriteFile(target, []byte("old-binary"), 0o600))

	origRename := upgradeRename
	upgradeRename = os.Rename
	defer func() { upgradeRename = origRename }()

	shutdownCalled := false
	origShutdown := upgradeShutdownFunc
	upgradeShutdownFunc = func() { shutdownCalled = true }
	defer func() { upgradeShutdownFunc = origShutdown }()

	ResetUpgradeState()
	defer ResetUpgradeState()

	err := performSupervisedUpgrade(newPath, target)
	require.NoError(t, err)

	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "new-binary", string(data))
	assert.True(t, shutdownCalled, "shutdown func should be called to let supervisor restart")

	state := GetUpgradeState()
	assert.Equal(t, UpgradePhaseRestarting, state.Phase)
}

func TestPerformSupervisedUpgrade_ReplaceError(t *testing.T) {
	dir := t.TempDir()
	newPath := filepath.Join(dir, "clawbench-missing")
	target := filepath.Join(dir, "clawbench")
	require.NoError(t, os.WriteFile(target, []byte("old-binary"), 0o600))

	origRename := upgradeRename
	upgradeRename = os.Rename
	defer func() { upgradeRename = origRename }()

	shutdownCalled := false
	origShutdown := upgradeShutdownFunc
	upgradeShutdownFunc = func() { shutdownCalled = true }
	defer func() { upgradeShutdownFunc = origShutdown }()

	ResetUpgradeState()
	defer ResetUpgradeState()

	err := performSupervisedUpgrade(newPath, target)
	assert.Error(t, err)
	assert.False(t, shutdownCalled, "shutdown should NOT be called when replacement fails")
}

func TestPerformSupervisedUpgrade_NilShutdownFunc(t *testing.T) {
	dir := t.TempDir()
	newPath := filepath.Join(dir, "clawbench-new")
	target := filepath.Join(dir, "clawbench")
	require.NoError(t, os.WriteFile(newPath, []byte("new-binary"), 0o600))
	require.NoError(t, os.WriteFile(target, []byte("old-binary"), 0o600))

	origRename := upgradeRename
	upgradeRename = os.Rename
	defer func() { upgradeRename = origRename }()

	origShutdown := upgradeShutdownFunc
	upgradeShutdownFunc = nil
	defer func() { upgradeShutdownFunc = origShutdown }()

	ResetUpgradeState()
	defer ResetUpgradeState()

	// Should not panic when no shutdown func is wired up.
	err := performSupervisedUpgrade(newPath, target)
	require.NoError(t, err)
}
