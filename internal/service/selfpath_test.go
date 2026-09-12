package service

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withTempDataDir points model.DataDir at a fresh temp dir for the duration of
// the test. self-path lives under DataDir, so tests must isolate it.
func withTempDataDir(t *testing.T) string {
	t.Helper()
	orig := model.DataDir
	dir := t.TempDir()
	model.DataDir = dir
	t.Cleanup(func() { model.DataDir = orig })
	return dir
}

// --- WriteSelfPath / ReadSelfPath ---

func TestWriteSelfPath_RoundTrip(t *testing.T) {
	dir := withTempDataDir(t)
	exe := filepath.Join(dir, "bin", "clawbench")

	require.NoError(t, WriteSelfPath(exe))
	assert.Equal(t, exe, ReadSelfPath())
}

// TestWriteSelfPath_FileMode verifies the record is not world-readable: it
// contains a filesystem path that could hint at the install layout.
func TestWriteSelfPath_FileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission semantics differ on Windows")
	}
	dir := withTempDataDir(t)
	require.NoError(t, WriteSelfPath("/opt/clawbench"))

	info, err := os.Stat(filepath.Join(dir, selfPathFileName))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// TestWriteSelfPath_CreatesDataDir covers a first run where DataDir has not
// been materialised yet (self-path is written before anything else uses it).
func TestWriteSelfPath_CreatesDataDir(t *testing.T) {
	orig := model.DataDir
	model.DataDir = filepath.Join(t.TempDir(), "not-yet-created")
	t.Cleanup(func() { model.DataDir = orig })

	require.NoError(t, WriteSelfPath("/opt/clawbench"))
	assert.Equal(t, "/opt/clawbench", ReadSelfPath())
}

// TestWriteSelfPath_EmptyDataDirIsNoop documents that an unset DataDir is not
// an error: the caller cannot know where to write, and startup must not fail.
func TestWriteSelfPath_EmptyDataDirIsNoop(t *testing.T) {
	orig := model.DataDir
	model.DataDir = ""
	t.Cleanup(func() { model.DataDir = orig })

	assert.NoError(t, WriteSelfPath("/opt/clawbench"))
	assert.Empty(t, ReadSelfPath())
}

func TestReadSelfPath_MissingFile(t *testing.T) {
	withTempDataDir(t)
	assert.Empty(t, ReadSelfPath())
}

// TestReadSelfPath_TrimsWhitespace guards against a trailing newline being
// carried into the path (which would make os.Stat fail later).
func TestReadSelfPath_TrimsWhitespace(t *testing.T) {
	dir := withTempDataDir(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, selfPathFileName),
		[]byte("/opt/clawbench\n"), 0o600))

	assert.Equal(t, "/opt/clawbench", ReadSelfPath())
}

// TestWriteSelfPath_UnwritableDataDirErrors verifies the error is surfaced
// (not swallowed) so the startup caller can log it; startup still continues.
func TestWriteSelfPath_UnwritableDataDirErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}

	orig := model.DataDir
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o755)
		model.DataDir = orig
	})
	model.DataDir = dir

	err := WriteSelfPath("/opt/clawbench")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "self-path")
}

// TestWriteSelfPath_MkdirFailureErrors covers the data directory being
// uncreatable (its path component is a regular file). Unlike a permission
// failure, this fails even for root, so the branch is exercised everywhere.
func TestWriteSelfPath_MkdirFailureErrors(t *testing.T) {
	orig := model.DataDir
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-dir")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))
	t.Cleanup(func() { model.DataDir = orig })

	// DataDir nested under a regular file → MkdirAll must fail.
	model.DataDir = filepath.Join(blocker, "sub")

	err := WriteSelfPath("/opt/clawbench")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create data dir")
}

// TestWriteSelfPath_WriteFailureErrors covers the record path being occupied by
// a directory, so the write fails. Like the MkdirAll case this fails for root
// too, keeping the branch covered in any environment.
func TestWriteSelfPath_WriteFailureErrors(t *testing.T) {
	orig := model.DataDir
	dir := t.TempDir()
	// Occupy the record path with a directory.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, selfPathFileName), 0o755))
	t.Cleanup(func() { model.DataDir = orig })
	model.DataDir = dir

	err := WriteSelfPath("/opt/clawbench")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write self-path")
}

// --- probeBinaryVersion ---

// TestProbeBinaryVersion_ReportsOutput verifies the probe runs the binary and
// returns its trimmed version string.
func TestProbeBinaryVersion_ReportsOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script helper is Unix-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-version")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho v9.9.9\n"), 0o755))

	got, err := probeBinaryVersion(script)
	require.NoError(t, err)
	assert.Equal(t, "v9.9.9", got)
}

// TestProbeBinaryVersion_TrimsWhitespace guards against a trailing newline
// reaching CompareVersions.
func TestProbeBinaryVersion_TrimsWhitespace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script helper is Unix-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-version")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nprintf 'v1.2.3\\n\\n'\n"), 0o755))

	got, err := probeBinaryVersion(script)
	require.NoError(t, err)
	assert.Equal(t, "v1.2.3", got)
}

// TestProbeBinaryVersion_NonExecutableErrors verifies a path that cannot be
// executed yields an error, so callers fall back to downloading.
func TestProbeBinaryVersion_NonExecutableErrors(t *testing.T) {
	dir := t.TempDir()
	notExec := filepath.Join(dir, "not-executable")
	require.NoError(t, os.WriteFile(notExec, []byte("not a program"), 0o644))

	_, err := probeBinaryVersion(notExec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to probe version")
}

// TestProbeBinaryVersion_MissingFileErrors covers a deleted binary (the
// npm-retire case) reaching the probe.
func TestProbeBinaryVersion_MissingFileErrors(t *testing.T) {
	_, err := probeBinaryVersion(filepath.Join(t.TempDir(), "does-not-exist"))
	require.Error(t, err)
}

// --- resolveSelfBinary ---

// TestResolveSelfBinary_PrefersRecordedPath is the regression test for the
// npm-retire bug: os.Executable() returns a path inside the deleted retire
// directory, while the recorded self-path still points at the live binary.
func TestResolveSelfBinary_PrefersRecordedPath(t *testing.T) {
	dir := withTempDataDir(t)

	live := filepath.Join(dir, "live", "clawbench")
	require.NoError(t, os.MkdirAll(filepath.Dir(live), 0o755))
	require.NoError(t, os.WriteFile(live, []byte("binary"), 0o755))
	require.NoError(t, WriteSelfPath(live))

	// Simulate os.Executable() pointing into the deleted retire directory.
	origExe := upgradeExecutable
	t.Cleanup(func() { upgradeExecutable = origExe })
	upgradeExecutable = func() (string, error) {
		return filepath.Join(dir, ".clawbench-abc12345", "bin", "clawbench"), nil
	}

	got, err := ResolveSelfBinary()
	require.NoError(t, err)
	assert.Equal(t, live, got, "must use the recorded path, not the stale executable path")
}

// TestResolveSelfBinary_FallsBackWhenRecordIsStale covers the recorded path
// having been moved away (manual relocation): fall back to os.Executable().
func TestResolveSelfBinary_FallsBackWhenRecordIsStale(t *testing.T) {
	dir := withTempDataDir(t)

	require.NoError(t, WriteSelfPath(filepath.Join(dir, "gone", "clawbench")))

	live := filepath.Join(dir, "live", "clawbench")
	require.NoError(t, os.MkdirAll(filepath.Dir(live), 0o755))
	require.NoError(t, os.WriteFile(live, []byte("binary"), 0o755))

	origExe := upgradeExecutable
	t.Cleanup(func() { upgradeExecutable = origExe })
	upgradeExecutable = func() (string, error) { return live, nil }

	got, err := ResolveSelfBinary()
	require.NoError(t, err)
	assert.Equal(t, live, got)
}

// TestResolveSelfBinary_FallsBackWhenNoRecord covers a first run (or an
// install that predates this feature): no self-path file exists yet.
func TestResolveSelfBinary_FallsBackWhenNoRecord(t *testing.T) {
	dir := withTempDataDir(t)

	live := filepath.Join(dir, "live", "clawbench")
	require.NoError(t, os.MkdirAll(filepath.Dir(live), 0o755))
	require.NoError(t, os.WriteFile(live, []byte("binary"), 0o755))

	origExe := upgradeExecutable
	t.Cleanup(func() { upgradeExecutable = origExe })
	upgradeExecutable = func() (string, error) { return live, nil }

	got, err := ResolveSelfBinary()
	require.NoError(t, err)
	assert.Equal(t, live, got)
}

// TestResolveSelfBinary_ErrorsWhenFallbackMissing covers the case where no
// record exists AND the executable path does not resolve to a real file —
// there is no usable target, so the upgrade must fail fast rather than later.
func TestResolveSelfBinary_ErrorsWhenFallbackMissing(t *testing.T) {
	dir := withTempDataDir(t)

	origExe := upgradeExecutable
	t.Cleanup(func() { upgradeExecutable = origExe })
	upgradeExecutable = func() (string, error) {
		return filepath.Join(dir, "does-not-exist"), nil
	}

	_, err := ResolveSelfBinary()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not accessible")
}

// TestResolveSelfBinary_ErrorsWhenExecutableFails covers the case where no
// record exists and the executable cannot be resolved at all.
func TestResolveSelfBinary_ErrorsWhenExecutableFails(t *testing.T) {
	withTempDataDir(t)

	origExe := upgradeExecutable
	t.Cleanup(func() { upgradeExecutable = origExe })
	upgradeExecutable = func() (string, error) { return "", fmt.Errorf("boom") }

	_, err := ResolveSelfBinary()
	require.Error(t, err)
}

// TestResolveSelfBinary_EmptyExecutableErrors covers a hook returning ("", nil),
// which must not be mistaken for a usable path.
func TestResolveSelfBinary_EmptyExecutableErrors(t *testing.T) {
	withTempDataDir(t)

	origExe := upgradeExecutable
	t.Cleanup(func() { upgradeExecutable = origExe })
	upgradeExecutable = func() (string, error) { return "", nil }

	_, err := ResolveSelfBinary()
	require.Error(t, err)
}
