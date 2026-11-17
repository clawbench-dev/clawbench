package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The file has to name THIS process: a reader uses it to decide which server to
// stop after a crash, and the wrong number would either kill nothing (leaving
// the data directory locked) or kill a stranger.
func TestWritePIDFile_NamesThisProcessAndCleansUp(t *testing.T) {
	dir := t.TempDir()

	cleanup, err := writePIDFile(dir)
	require.NoError(t, err)

	path := filepath.Join(dir, "server.pid")
	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, strconv.Itoa(os.Getpid()), string(data))

	// 0600: the PID is not a secret, but the data directory may be shared with
	// other OS users, and there is no reason to advertise it.
	info, statErr := os.Stat(path)
	require.NoError(t, statErr)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	cleanup()
	_, statErr = os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "a graceful exit must remove the PID file")
}

// Cleanup must be safe to call twice: it is deferred, and a future shutdown path
// may also call it explicitly. Removing an already-absent file must not panic.
func TestWritePIDFile_CleanupIsIdempotent(t *testing.T) {
	cleanup, err := writePIDFile(t.TempDir())
	require.NoError(t, err)
	cleanup()
	assert.NotPanics(t, cleanup)
}
