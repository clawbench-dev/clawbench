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

// --- path selection: container always wins ---

// A container can never use the self-restart subprocess path: the runtime tears
// the namespace down when PID 1 exits, killing the upgrade-replace child before
// it finishes. Even when the supervisor probe says "not supervised" (k8s,
// runit, supervisord), a container must still be treated as supervised.
func TestResolveReplaceInPlace_ContainerForcesInPlaceEvenWhenProbeSaysNo(t *testing.T) {
	assert.True(t, resolveReplaceInPlace(true, false),
		"container + probe says not supervised => still in-place")
}

func TestResolveReplaceInPlace_ContainerAndProbeBothTrue(t *testing.T) {
	assert.True(t, resolveReplaceInPlace(true, true))
}

// Outside a container the probe is authoritative: a genuinely unsupervised
// deployment must use the self-restart subprocess path (nothing else restarts
// it), while a supervised one replaces in place.
func TestResolveReplaceInPlace_NonContainerFollowsProbe(t *testing.T) {
	assert.False(t, resolveReplaceInPlace(false, false),
		"not a container + not supervised => subprocess path")
	assert.True(t, resolveReplaceInPlace(false, true),
		"not a container + supervised (systemd) => in-place")
}

// The container override must survive a probe that reports false — this is the
// exact combination an unrecognized runtime (k8s/runit/supervisord) produces.
func TestResolveReplaceInPlace_ProbeOverrideCannotDefeatContainer(t *testing.T) {
	origContainer := upgradeIsContainer
	origProbe := upgradeIsSupervised
	defer func() {
		upgradeIsContainer = origContainer
		upgradeIsSupervised = origProbe
	}()

	upgradeIsContainer = func() bool { return true }
	upgradeIsSupervised = func() bool { return false } // unrecognized runtime

	got := resolveReplaceInPlace(upgradeIsContainer(), upgradeIsSupervised())
	assert.True(t, got, "container must force in-place even when the probe says false")
}
