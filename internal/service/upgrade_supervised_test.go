package service

import (
	"os"
	"path/filepath"
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
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
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
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
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
