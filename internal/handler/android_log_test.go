package handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// appendClientLog rotation: once the log file would exceed clientLogMaxBytes,
// the current file is renamed to .1 (previous .1 dropped) and a fresh file is
// started, so client.log never grows without bound.
func TestAppendClientLog_RotatesPastCap(t *testing.T) {
	origLogDir := model.ConfigInstance.LogDir
	defer func() { model.ConfigInstance.LogDir = origLogDir }()

	tmpDir := t.TempDir()
	model.ConfigInstance.LogDir = tmpDir
	path := filepath.Join(tmpDir, "client.log")

	// Fill the file to exactly the cap (a single batch that lands at the cap
	// must NOT rotate; rotation happens only when the NEXT batch would exceed it).
	big := strings.Repeat("x", int(clientLogMaxBytes))
	require.NoError(t, appendClientLog([]byte(big)))

	// The next append — even a tiny one — would exceed the cap, so it must
	// rotate the current file first.
	require.NoError(t, appendClientLog([]byte("tail")))

	// Old content now lives in .1; the live file starts fresh with just "tail".
	rotated, err := os.ReadFile(path + ".1")
	require.NoError(t, err)
	assert.Equal(t, big, string(rotated))

	live, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "tail", string(live))
}

func TestAppendClientLog_SecondRotationDropsPreviousGen(t *testing.T) {
	origLogDir := model.ConfigInstance.LogDir
	defer func() { model.ConfigInstance.LogDir = origLogDir }()

	tmpDir := t.TempDir()
	model.ConfigInstance.LogDir = tmpDir
	path := filepath.Join(tmpDir, "client.log")

	require.NoError(t, appendClientLog([]byte(strings.Repeat("a", int(clientLogMaxBytes)))))
	// Rotate again with different content.
	require.NoError(t, appendClientLog([]byte(strings.Repeat("b", int(clientLogMaxBytes)))))
	require.NoError(t, appendClientLog([]byte("c")))

	// .1 holds the "b" generation only — the "a" generation was dropped.
	rotated, err := os.ReadFile(path + ".1")
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("b", int(clientLogMaxBytes)), string(rotated))
	assert.False(t, strings.Contains(string(rotated), "a"))
}

// TestAppendClientLog_RenameFailureFallsThrough exercises the non-fatal rotate
// fall-through: when the rename to .1 fails (e.g. a non-empty directory is
// squatting on the .1 path), the append must still land in the live file
// instead of erroring out and losing the batch.
func TestAppendClientLog_RenameFailureFallsThrough(t *testing.T) {
	origLogDir := model.ConfigInstance.LogDir
	defer func() { model.ConfigInstance.LogDir = origLogDir }()

	tmpDir := t.TempDir()
	model.ConfigInstance.LogDir = tmpDir
	path := filepath.Join(tmpDir, "client.log")
	rotated := path + ".1"

	// Fill the live file so the next append triggers the rotation branch.
	big := strings.Repeat("x", int(clientLogMaxBytes))
	require.NoError(t, appendClientLog([]byte(big)))

	// Occupy the .1 path with a non-empty directory: os.Remove(rotated) fails
	// (ENOTEMPTY) and os.Rename(client.log → client.log.1) fails (target not empty),
	// exercising the slog.Warn fall-through inside the rotation branch.
	require.NoError(t, os.MkdirAll(filepath.Join(rotated, "stub"), 0o755))
	require.NoError(t, appendClientLog([]byte("tail")))

	// Rotation failed, so the live file kept the full history…
	live, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, big+"tail", string(live))

	// …and the squatting directory was left untouched.
	fi, err := os.Stat(rotated)
	require.NoError(t, err)
	assert.True(t, fi.IsDir())
}
