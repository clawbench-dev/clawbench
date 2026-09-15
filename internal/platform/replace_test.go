package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forceCrossDevice makes renameFile fail with a cross-device error, which is
// what os.Rename reports on Windows when src and dst are on different volumes
// (MoveFileEx without MOVEFILE_COPY_ALLOWED) and on Unix across mounts.
func forceCrossDevice(t *testing.T) {
	t.Helper()
	orig := renameFile
	renameFile = func(oldpath, newpath string) error { return syscall.EXDEV }
	t.Cleanup(func() { renameFile = orig })
}

func TestReplaceBinary_RenameSuccess(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "clawbench-new")
	dst := filepath.Join(dir, "clawbench")
	require.NoError(t, os.WriteFile(src, []byte("new-binary"), 0o600))
	require.NoError(t, os.WriteFile(dst, []byte("old-binary"), 0o600))

	require.NoError(t, ReplaceBinary(src, dst))

	data, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "new-binary", string(data))

	info, err := os.Stat(dst)
	require.NoError(t, err)
	// Executable permission is a Unix concept; on Windows os.Chmod does not
	// surface a 0755 mode, so only assert it where it is meaningful.
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
	}
}

// TestReplaceBinary_CrossDeviceFallback is the case that broke the Windows
// upgrade: the new binary is extracted under %TEMP% (typically C:) while the
// install directory may be on another drive, so the rename fails. The staged
// copy must still land the binary.
func TestReplaceBinary_CrossDeviceFallback(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "clawbench-new")
	dst := filepath.Join(dir, "clawbench")
	require.NoError(t, os.WriteFile(src, []byte("new-binary"), 0o600))
	require.NoError(t, os.WriteFile(dst, []byte("old-binary"), 0o600))

	forceCrossDevice(t)

	require.NoError(t, ReplaceBinary(src, dst))

	data, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "new-binary", string(data))

	info, err := os.Stat(dst)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
	}
}

// TestReplaceBinary_CrossDeviceFallback_ReadOnlyTarget verifies the fallback
// succeeds when the target file is read-only but its directory is writable. The
// staged-copy implementation must not require the target file to be writable
// (that is the case that broke for a root-owned 0755 binary in a user-writable
// directory).
func TestReplaceBinary_CrossDeviceFallback_ReadOnlyTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only file semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "clawbench-new")
	dst := filepath.Join(dir, "clawbench")
	require.NoError(t, os.WriteFile(src, []byte("new-binary"), 0o600))
	require.NoError(t, os.WriteFile(dst, []byte("old-binary"), 0o400))
	defer func() { _ = os.Chmod(dst, 0o600) }() // allow TempDir cleanup

	forceCrossDevice(t)

	require.NoError(t, ReplaceBinary(src, dst))

	data, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "new-binary", string(data))

	info, err := os.Stat(dst)
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

// TestReplaceBinary_MissingSource_LeavesTargetIntact pins the invariant the
// upgrade helper depends on: when the replacement cannot happen, the existing
// binary is still there and still runnable. The bug this guards against moved
// the target aside first, leaving the install directory with no binary at all.
func TestReplaceBinary_MissingSource_LeavesTargetIntact(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "clawbench")
	require.NoError(t, os.WriteFile(dst, []byte("old-binary"), 0o600))

	err := ReplaceBinary(filepath.Join(dir, "does-not-exist"), dst)
	require.Error(t, err)

	data, readErr := os.ReadFile(dst)
	require.NoError(t, readErr, "target must survive a failed replacement")
	assert.Equal(t, "old-binary", string(data))
}

// TestReplaceBinary_UnwritableDir_LeavesTargetIntact covers the other failure
// path: the staged copy cannot be created because the directory is not
// writable. The target must still be intact afterwards.
func TestReplaceBinary_UnwritableDir_LeavesTargetIntact(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission-based test not reliable on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}

	dir := t.TempDir()
	src := filepath.Join(t.TempDir(), "clawbench-new")
	dst := filepath.Join(dir, "clawbench")
	require.NoError(t, os.WriteFile(src, []byte("new-binary"), 0o600))
	require.NoError(t, os.WriteFile(dst, []byte("old-binary"), 0o600))

	require.NoError(t, os.Chmod(dir, 0o555))
	defer func() { _ = os.Chmod(dir, 0o755) }() // allow TempDir cleanup

	forceCrossDevice(t)

	err := ReplaceBinary(src, dst)
	require.Error(t, err)

	data, readErr := os.ReadFile(dst)
	require.NoError(t, readErr, "target must survive a failed replacement")
	assert.Equal(t, "old-binary", string(data))
}

// TestReplaceBinary_StagingFileCleanedUpOnCopyFailure verifies a partial copy
// does not leave a staging file behind in the install directory.
func TestReplaceBinary_StagingFileCleanedUpOnCopyFailure(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "clawbench")
	// A directory as the source: os.Open succeeds, io.Copy fails.
	src := filepath.Join(dir, "src-is-a-dir")
	require.NoError(t, os.Mkdir(src, 0o755))
	require.NoError(t, os.WriteFile(dst, []byte("old-binary"), 0o600))

	forceCrossDevice(t)

	require.Error(t, ReplaceBinary(src, dst))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".clawbench-replace-",
			"staging temp file must be cleaned up on failure")
	}
}
