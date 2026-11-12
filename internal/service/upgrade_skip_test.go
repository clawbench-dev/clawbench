package service

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ReadSkippedUpgradeVersion / WriteSkippedUpgradeVersion ---

func TestSkippedUpgradeVersion_RoundTrip(t *testing.T) {
	withTempDataDir(t)

	stored, err := WriteSkippedUpgradeVersion("1.2.3")
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", stored)
	assert.Equal(t, "1.2.3", ReadSkippedUpgradeVersion())
}

// The registry answers without a leading "v" while some callers spell it with
// one; both spellings must land on the same record, otherwise a version the
// user skipped keeps looking unskipped.
func TestSkippedUpgradeVersion_NormalizesSpelling(t *testing.T) {
	withTempDataDir(t)

	stored, err := WriteSkippedUpgradeVersion("  v1.2.3 \n")
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", stored)
	assert.Equal(t, "1.2.3", ReadSkippedUpgradeVersion())
}

func TestSkippedUpgradeVersion_EmptyMeansNothingSkipped(t *testing.T) {
	withTempDataDir(t)

	_, err := WriteSkippedUpgradeVersion("1.2.3")
	require.NoError(t, err)

	stored, err := WriteSkippedUpgradeVersion("")
	require.NoError(t, err)
	assert.Equal(t, "", stored)
	assert.Equal(t, "", ReadSkippedUpgradeVersion())
}

func TestSkippedUpgradeVersion_ClearWhenNothingRecorded(t *testing.T) {
	withTempDataDir(t)

	_, err := WriteSkippedUpgradeVersion("")
	require.NoError(t, err, "clearing an absent record is the requested state, not a failure")
}

func TestSkippedUpgradeVersion_ReadWithoutRecord(t *testing.T) {
	withTempDataDir(t)
	assert.Equal(t, "", ReadSkippedUpgradeVersion())
}

func TestSkippedUpgradeVersion_RejectsInvalidInput(t *testing.T) {
	withTempDataDir(t)

	cases := map[string]string{
		"too long":          strings.Repeat("9", maxSkippedVersionLen+1),
		"newline injection": "1.2.3\n1.2.4",
		"embedded CR":       "1.2\r.3",
		"embedded NUL":      "1.2.3\x00",
		"embedded vtab":     "1.2\x0b3",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := WriteSkippedUpgradeVersion(input)
			assert.ErrorIs(t, err, ErrSkippedVersionInvalid)
			assert.Equal(t, "", ReadSkippedUpgradeVersion(), "a rejected write must not leave a record")
		})
	}
}

func TestSkippedUpgradeVersion_AcceptsMaxLength(t *testing.T) {
	withTempDataDir(t)

	longest := strings.Repeat("9", maxSkippedVersionLen)
	_, err := WriteSkippedUpgradeVersion(longest)
	require.NoError(t, err)
	assert.Equal(t, longest, ReadSkippedUpgradeVersion())
}

// The record lives in the data directory next to the binary path and the cookie
// token, so it must not be world-readable.
func TestSkippedUpgradeVersion_FileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission semantics differ on Windows")
	}
	dir := withTempDataDir(t)

	_, err := WriteSkippedUpgradeVersion("1.2.3")
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(dir, upgradeSkipFileName))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// Without a data directory there is nowhere durable to put the choice, so the
// write reports failure instead of pretending it was remembered. Reads report
// "nothing skipped" — one extra prompt beats failing the check.
func TestSkippedUpgradeVersion_NoDataDir(t *testing.T) {
	orig := model.DataDir
	model.DataDir = ""
	t.Cleanup(func() { model.DataDir = orig })

	_, err := WriteSkippedUpgradeVersion("1.2.3")
	assert.Error(t, err)
	assert.Equal(t, "", ReadSkippedUpgradeVersion())
}

// A data directory that cannot be created must fail the write: the endpoint's
// whole contract is durability, so silently succeeding would leave the client
// believing a choice was remembered when the next launch prompts again.
func TestSkippedUpgradeVersion_FailsWhenTheDataDirCannotBeCreated(t *testing.T) {
	orig := model.DataDir
	t.Cleanup(func() { model.DataDir = orig })

	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
	// A regular file in the way: MkdirAll cannot create a directory here.
	model.DataDir = filepath.Join(blocker, "data")

	_, err := WriteSkippedUpgradeVersion("1.2.3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create data dir")
}

// A data directory that exists but is not writable must fail the write rather
// than report success. Skipped on Windows (ACLs do not map onto 0500) and when
// running as root (root ignores the mode bits).
func TestSkippedUpgradeVersion_FailsWhenTheFileCannotBeWritten(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not apply on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	orig := model.DataDir
	t.Cleanup(func() { model.DataDir = orig })

	dir := t.TempDir()
	model.DataDir = dir
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	_, err := WriteSkippedUpgradeVersion("1.2.3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write upgrade skip")
}

// A clear that fails for a reason other than "not there" must be reported: the
// user asked to stop skipping a version, and a silent failure means the next
// launch skips it again.
func TestSkippedUpgradeVersion_ClearReportsRealFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not apply on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	orig := model.DataDir
	t.Cleanup(func() { model.DataDir = orig })

	dir := t.TempDir()
	model.DataDir = dir
	require.NoError(t, os.WriteFile(filepath.Join(dir, upgradeSkipFileName), []byte("1.2.3"), 0o600))
	// Read-only directory: the file exists, so Remove fails with EACCES rather
	// than ENOENT (which is the "nothing skipped" case and must stay silent).
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	err := clearSkippedUpgradeVersion()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to clear upgrade skip")
}

// Without a data directory, clearing is a no-op that reports success: there is
// no record to remove, which is exactly the state the caller asked for.
func TestSkippedUpgradeVersion_ClearWithoutDataDirIsANoop(t *testing.T) {
	orig := model.DataDir
	model.DataDir = ""
	t.Cleanup(func() { model.DataDir = orig })

	assert.NoError(t, clearSkippedUpgradeVersion())
}
