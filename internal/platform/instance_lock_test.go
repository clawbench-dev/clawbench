package platform_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"clawbench/internal/platform"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two servers on one data directory corrupt each other (startup treats the
// other's in-flight turn as a crash leftover), so the second acquire must fail.
func TestAcquireInstanceLock_SecondHolderIsRejected(t *testing.T) {
	dir := t.TempDir()

	first, err := platform.AcquireInstanceLock(dir, 0)
	require.NoError(t, err)
	defer first.Release()

	_, err = platform.AcquireInstanceLock(dir, 0)
	require.Error(t, err)
	assert.ErrorIs(t, err, platform.ErrDataDirInUse)
}

// A restart starts the new process while the outgoing one is still draining.
// The wait window is what keeps that from turning into a startup failure.
func TestAcquireInstanceLock_WaitsForHolderToExit(t *testing.T) {
	dir := t.TempDir()

	first, err := platform.AcquireInstanceLock(dir, 0)
	require.NoError(t, err)
	go func() {
		time.Sleep(300 * time.Millisecond)
		first.Release()
	}()

	second, err := platform.AcquireInstanceLock(dir, 5*time.Second)
	require.NoError(t, err)
	second.Release()
}

func TestAcquireInstanceLock_ReusableAfterRelease(t *testing.T) {
	dir := t.TempDir()

	first, err := platform.AcquireInstanceLock(dir, 0)
	require.NoError(t, err)
	first.Release()

	second, err := platform.AcquireInstanceLock(dir, 0)
	require.NoError(t, err)
	second.Release()
}

// The lock lives INSIDE the data directory, so two servers on different data
// directories (the normal e2e / multi-instance-dev setup) do not block each
// other.
func TestAcquireInstanceLock_DifferentDataDirsDoNotConflict(t *testing.T) {
	a, err := platform.AcquireInstanceLock(t.TempDir(), 0)
	require.NoError(t, err)
	defer a.Release()

	b, err := platform.AcquireInstanceLock(t.TempDir(), 0)
	require.NoError(t, err)
	defer b.Release()
}

// The data directory may not exist yet on a first run (e.g. --data-dir pointing
// at a fresh path); locking must create it rather than fail.
func TestAcquireInstanceLock_CreatesMissingDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")

	lock, err := platform.AcquireInstanceLock(dir, 0)
	require.NoError(t, err)
	defer lock.Release()

	_, statErr := os.Stat(filepath.Join(dir, ".instance.lock"))
	assert.NoError(t, statErr)
}

// Release on a nil lock must be a no-op: callers defer it unconditionally.
func TestInstanceLock_ReleaseNil(t *testing.T) {
	var lock *platform.InstanceLock
	assert.NotPanics(t, lock.Release)
}

// A data directory that cannot be created must surface as a real error, not as
// "another instance is running" — the user needs the distinction to know
// whether to stop a server or fix the path.
func TestAcquireInstanceLock_ReportsUnusableDataDir(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))

	_, err := platform.AcquireInstanceLock(filepath.Join(blocker, "data"), 0)
	require.Error(t, err)
	assert.NotErrorIs(t, err, platform.ErrDataDirInUse)
	assert.Contains(t, err.Error(), "create data directory")
}

// Same distinction for a lock path that exists but cannot be opened (here: the
// lock file is a directory).
func TestAcquireInstanceLock_ReportsUnopenableLockFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".instance.lock"), 0o755))

	_, err := platform.AcquireInstanceLock(dir, 0)
	require.Error(t, err)
	assert.NotErrorIs(t, err, platform.ErrDataDirInUse)
	assert.Contains(t, err.Error(), "open instance lock")
}

// The error must name the condition, not just "permission denied" — the message
// is what tells the user to pick another --data-dir.
func TestErrDataDirInUse_IsDistinguishable(t *testing.T) {
	dir := t.TempDir()
	first, err := platform.AcquireInstanceLock(dir, 0)
	require.NoError(t, err)
	defer first.Release()

	_, err = platform.AcquireInstanceLock(dir, 0)
	require.Error(t, err)
	assert.True(t, errors.Is(err, platform.ErrDataDirInUse))
	assert.Contains(t, err.Error(), "data directory")
}
