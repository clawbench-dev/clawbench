package wechat

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withTempDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := model.DataDir
	model.DataDir = dir
	t.Cleanup(func() { model.DataDir = orig })
	return dir
}

func TestSyncBuf_RoundTrip(t *testing.T) {
	withTempDataDir(t)

	require.NoError(t, saveSyncBuf("bot-1", "cursor-1"))
	assert.Equal(t, "cursor-1", loadSyncBuf("bot-1"))

	require.NoError(t, saveSyncBuf("bot-1", "cursor-2"))
	assert.Equal(t, "cursor-2", loadSyncBuf("bot-1"), "a second save must overwrite")
}

// TestSyncBuf_MissingIsEmpty: a first run has no cursor, which must read as ""
// rather than an error — the monitor treats "" as "start from the beginning".
func TestSyncBuf_MissingIsEmpty(t *testing.T) {
	withTempDataDir(t)
	assert.Equal(t, "", loadSyncBuf("bot-1"))
}

func TestSyncBuf_Permissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no POSIX permission bits: a writable file always reports 0666")
	}
	withTempDataDir(t)
	require.NoError(t, saveSyncBuf("bot-1", "cursor"))

	info, err := os.Stat(syncBufPath("bot-1"))
	require.NoError(t, err)
	// Per-account state, same permission as config.yaml.
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// TestSyncBuf_NoTempLeftBehind: the write is atomic via a .tmp + rename, and a
// stray temp file would accumulate on every poll.
func TestSyncBuf_NoTempLeftBehind(t *testing.T) {
	dir := withTempDataDir(t)
	require.NoError(t, saveSyncBuf("bot-1", "cursor"))

	entries, err := os.ReadDir(filepath.Join(dir, "wechat"))
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotEqual(t, "sync_buf.tmp", e.Name(), "the temp file must be renamed away")
	}
}

// TestSyncBuf_ScopedPerBot is the guard on account switching: a cursor is only
// meaningful to the account that produced it, so a second bot must start from
// its own (empty) cursor rather than inheriting the first one's.
func TestSyncBuf_ScopedPerBot(t *testing.T) {
	withTempDataDir(t)

	require.NoError(t, saveSyncBuf("bot-A", "cursor-A"))
	assert.Equal(t, "", loadSyncBuf("bot-B"),
		"bot B must not inherit bot A's cursor")

	require.NoError(t, saveSyncBuf("bot-B", "cursor-B"))
	assert.Equal(t, "cursor-A", loadSyncBuf("bot-A"), "each bot keeps its own")
	assert.Equal(t, "cursor-B", loadSyncBuf("bot-B"))
}

func TestSyncBuf_Clear(t *testing.T) {
	withTempDataDir(t)

	require.NoError(t, saveSyncBuf("bot-1", "cursor"))
	require.NoError(t, ClearSyncBuf("bot-1"))
	assert.Equal(t, "", loadSyncBuf("bot-1"))

	// Clearing a missing cursor is a no-op, not an error: logout runs even when
	// nothing was ever polled.
	assert.NoError(t, ClearSyncBuf("never-existed"))
}

// TestSyncBuf_SanitizesBotID: the bot id comes from the login response, i.e.
// from the network, so it must not be able to escape the state directory.
func TestSyncBuf_SanitizesBotID(t *testing.T) {
	dir := withTempDataDir(t)

	require.NoError(t, saveSyncBuf("../../etc/passwd", "cursor"))

	path := syncBufPath("../../etc/passwd")
	rel, err := filepath.Rel(filepath.Join(dir, "wechat"), path)
	require.NoError(t, err)
	assert.NotContains(t, rel, "..", "the cursor must stay inside the state directory")
	assert.NotContains(t, path, string(filepath.Separator)+"etc"+string(filepath.Separator),
		"a traversal in the bot id must not reach outside")
}

// The cursor directory lives under DataDir; if that path is unusable the error
// must surface instead of being swallowed (a silently lost cursor replays the
// whole backlog).
func TestSaveSyncBuf_CreateDirFailure(t *testing.T) {
	dir := withTempDataDir(t)
	blocker := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))
	model.DataDir = blocker

	err := saveSyncBuf("bot-1", "cursor")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create wechat state dir")
}

// A target that cannot be replaced (here: a directory squatting on the final
// name) must not be reported as success — the monitor would then keep re-reading
// a stale cursor.
func TestSaveSyncBuf_RenameFailure(t *testing.T) {
	dir := withTempDataDir(t)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "wechat", "sync_buf.bot-1"), 0o755))

	err := saveSyncBuf("bot-1", "cursor")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rename sync buf")
}

func TestSaveSyncBuf_WriteFailure(t *testing.T) {
	dir := withTempDataDir(t)
	// The temp file's own name is occupied by a directory, so the write fails
	// before the rename is attempted.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "wechat", "sync_buf.bot-1.tmp"), 0o755))

	err := saveSyncBuf("bot-1", "cursor")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write sync buf")
}

func TestClearSyncBuf_ReportsRemovalFailure(t *testing.T) {
	dir := withTempDataDir(t)
	victim := filepath.Join(dir, "wechat", "sync_buf.bot-1")
	require.NoError(t, os.MkdirAll(victim, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(victim, "child"), []byte("x"), 0o644))

	err := ClearSyncBuf("bot-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remove sync buf")

	// A missing cursor is not an error: logout must be idempotent.
	require.NoError(t, ClearSyncBuf("never-existed"))
}
