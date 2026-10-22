package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ledgerPaths lists the recorded paths in ledger order.
func ledgerPaths(t *testing.T, projectDir string) []string {
	t.Helper()
	entries := readUploadLedger(projectDir)
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Path)
	}
	return out
}

// fileOpRequest builds a file-operation request carrying the project cookie.
func fileOpRequest(t *testing.T, path, projectDir string, body any) *http.Request {
	t.Helper()
	req := newRequest(t, http.MethodPost, path, body)
	if projectDir != "" {
		withProjectCookie(req, projectDir)
	}
	return req
}

// Renaming an uploaded file must keep it in "最近上传": the entry stores a path,
// and without a rewrite the row would silently vanish from the list.
func TestServeFileRename_KeepsTheUploadRow(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	uploadIntoDir(t, env, "before.png", ".")
	require.Equal(t, []string{"before.png"}, ledgerPaths(t, env.ProjectDir))

	w := callHandler(ServeFileRename, fileOpRequest(t, "/api/file/rename", env.ProjectDir, map[string]any{
		"path": "before.png",
		"name": "after.png",
	}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.Equal(t, []string{"after.png"}, ledgerPaths(t, env.ProjectDir))
	files := recentUploadPaths(t, env)
	require.Len(t, files, 1)
	assert.Equal(t, "after.png", files[0].Path, "the row must follow the file")
}

// Moving a file to a subdirectory must also follow it.
func TestServeFileMove_KeepsTheUploadRow(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	uploadIntoDir(t, env, "shot.png", ".")
	require.NoError(t, os.MkdirAll(filepath.Join(env.ProjectDir, "archive"), 0o755))

	w := callHandler(ServeFileMove, fileOpRequest(t, "/api/file/move", env.ProjectDir, map[string]any{
		"path": "shot.png",
		"dest": "archive/shot.png",
	}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.Equal(t, []string{"archive/shot.png"}, ledgerPaths(t, env.ProjectDir))
}

// Renaming a directory carries every recorded upload beneath it.
func TestServeFileRename_DirectoryRewritesNestedEntries(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	uploadIntoDir(t, env, "a.png", "docs")
	uploadIntoDir(t, env, "b.png", "docs/images")
	require.Len(t, ledgerPaths(t, env.ProjectDir), 2)

	w := callHandler(ServeFileRename, fileOpRequest(t, "/api/file/rename", env.ProjectDir, map[string]any{
		"path": "docs",
		"name": "documentation",
	}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.ElementsMatch(t,
		[]string{"documentation/a.png", "documentation/images/b.png"},
		ledgerPaths(t, env.ProjectDir))
}

// A rename must not touch entries that live elsewhere.
func TestServeFileRename_LeavesUnrelatedEntriesAlone(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	uploadIntoDir(t, env, "target.png", ".")
	uploadIntoDir(t, env, "keep.png", "other")

	w := callHandler(ServeFileRename, fileOpRequest(t, "/api/file/rename", env.ProjectDir, map[string]any{
		"path": "target.png",
		"name": "renamed.png",
	}))
	require.Equal(t, http.StatusOK, w.Code)

	assert.ElementsMatch(t, []string{"renamed.png", "other/keep.png"}, ledgerPaths(t, env.ProjectDir))
}

// Deleting the file must drop its row: the record would point at nothing.
func TestServeFileDelete_DropsTheUploadRow(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	uploadIntoDir(t, env, "gone.png", ".")
	uploadIntoDir(t, env, "kept.png", ".")

	w := callHandler(ServeFileDelete, fileOpRequest(t, "/api/file/delete", env.ProjectDir, map[string]any{
		"path": "gone.png",
	}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.Equal(t, []string{"kept.png"}, ledgerPaths(t, env.ProjectDir))
}

// Deleting a directory drops the rows of the uploads inside it — and only
// those. A sibling whose name merely starts with the same characters
// (`docs2/` vs `docs/`) is a different directory and must survive.
func TestServeFileDelete_DirectoryDropsNestedRows(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	uploadIntoDir(t, env, "a.png", "shots")
	uploadIntoDir(t, env, "b.png", "shots/nested")
	uploadIntoDir(t, env, "c.png", "elsewhere")
	// Segment boundary: `shots2` is not under `shots`.
	uploadIntoDir(t, env, "d.png", "shots2")

	w := callHandler(ServeFileDelete, fileOpRequest(t, "/api/file/delete", env.ProjectDir, map[string]any{
		"path": "shots",
	}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.ElementsMatch(t, []string{"elsewhere/c.png", "shots2/d.png"}, ledgerPaths(t, env.ProjectDir))
}

// Moving a directory carries every recorded upload beneath it, exactly like a
// rename does — a move is a rename across directories.
func TestServeFileMove_DirectoryRewritesNestedEntries(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	uploadIntoDir(t, env, "a.png", "docs")
	uploadIntoDir(t, env, "b.png", "docs/images")
	require.NoError(t, os.MkdirAll(filepath.Join(env.ProjectDir, "archive"), 0o755))

	w := callHandler(ServeFileMove, fileOpRequest(t, "/api/file/move", env.ProjectDir, map[string]any{
		"path": "docs",
		"dest": "archive/docs",
	}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.ElementsMatch(t,
		[]string{"archive/docs/a.png", "archive/docs/images/b.png"},
		ledgerPaths(t, env.ProjectDir))
}

// Rename has no destination-exists guard, so renaming onto an existing name
// silently replaces it (Unix). The ledger must not end up with the same path
// twice — that would render two identical rows and a duplicate Vue key.
func TestServeFileRename_OntoExistingNameKeepsOneRow(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	uploadIntoDir(t, env, "source.png", ".")
	uploadIntoDir(t, env, "target.png", ".")
	require.Len(t, ledgerPaths(t, env.ProjectDir), 2)

	w := callHandler(ServeFileRename, fileOpRequest(t, "/api/file/rename", env.ProjectDir, map[string]any{
		"path": "source.png",
		"name": "target.png",
	}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	paths := ledgerPaths(t, env.ProjectDir)
	assert.Equal(t, []string{"target.png"}, paths, "the collapsed path must appear exactly once")

	files := recentUploadPaths(t, env)
	require.Len(t, files, 1, "the tab must not show the same path twice")
	assert.Equal(t, "target.png", files[0].Path)
}

// A stat that fails for a reason other than "not there" (permissions, I/O, an
// unmounted volume) must NOT be read as "the file is gone": dropping those
// records would empty the whole list the first time the user uploads while the
// project's volume is offline.
func TestPruneUploadLedger_KeepsEntriesItCannotStat(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod does not make a directory unreadable on Windows (mode bits are not enforced)")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	env, teardown := setupTestEnv(t)
	defer teardown()

	require.NoError(t, os.MkdirAll(filepath.Join(env.ProjectDir, "locked"), 0o755))
	require.NoError(t, writeUploadLedger(env.ProjectDir, []uploadLedgerEntry{
		{Path: "locked/inner.png", UploadedAt: time.Now().UTC().Format(time.RFC3339)},
	}))
	createTestFile(t, env.ProjectDir, "locked/inner.png", "x")

	// Unreadable directory: Stat on the child now fails with EACCES.
	require.NoError(t, os.Chmod(filepath.Join(env.ProjectDir, "locked"), 0o000))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(env.ProjectDir, "locked"), 0o755) })

	recordUploadedFile(env.ProjectDir, "new.png")
	createTestFile(t, env.ProjectDir, "new.png", "x")

	assert.Equal(t, []string{"new.png", "locked/inner.png"}, ledgerPaths(t, env.ProjectDir),
		"an entry that merely cannot be stat'ed must survive")
}

// A file operation with no project selected is normal (the file manager browses
// the whole machine); it simply has no ledger to update and must not fail.
func TestServeFileRename_WithoutProjectCookieStillWorks(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "plain.txt", "x")

	w := callHandler(ServeFileRename, newRequest(t, http.MethodPost, "/api/file/rename", map[string]any{
		"path": filepath.Join(env.ProjectDir, "plain.txt"),
		"name": "renamed.txt",
	}))
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// The retention is measured from the UPLOAD time, so an old entry is dropped
// even while its file is still there.
func TestPruneUploadLedger_DropsEntriesPastRetention(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	now := time.Now().UTC()
	old := now.Add(-uploadLedgerRetention - time.Hour).Format(time.RFC3339)
	fresh := now.Add(-time.Hour).Format(time.RFC3339)
	require.NoError(t, writeUploadLedger(env.ProjectDir, []uploadLedgerEntry{
		{Path: "old.png", UploadedAt: old},
		{Path: "fresh.png", UploadedAt: fresh},
	}))
	createTestFile(t, env.ProjectDir, "old.png", "x")
	createTestFile(t, env.ProjectDir, "fresh.png", "x")

	recordUploadedFile(env.ProjectDir, "newest.png")
	createTestFile(t, env.ProjectDir, "newest.png", "x")

	assert.Equal(t, []string{"newest.png", "fresh.png"}, ledgerPaths(t, env.ProjectDir),
		"the expired entry is dropped on the next write; the file itself is untouched")
	assert.FileExists(t, filepath.Join(env.ProjectDir, "old.png"),
		"retention prunes the RECORD, never the user's file")
}

// Editing a file must not extend its life: the clock is the upload time, not the
// file's mtime.
func TestPruneUploadLedger_UsesUploadTimeNotFileMtime(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	now := time.Now().UTC()
	expired := now.Add(-uploadLedgerRetention - time.Hour).Format(time.RFC3339)
	require.NoError(t, writeUploadLedger(env.ProjectDir, []uploadLedgerEntry{
		{Path: "touched.png", UploadedAt: expired},
	}))
	createTestFile(t, env.ProjectDir, "touched.png", "x")
	// The file was modified just now — a naive mtime-based retention would keep it.
	recent := now.Add(-time.Minute)
	require.NoError(t, os.Chtimes(filepath.Join(env.ProjectDir, "touched.png"), recent, recent))

	recordUploadedFile(env.ProjectDir, "other.png")
	createTestFile(t, env.ProjectDir, "other.png", "x")

	assert.Equal(t, []string{"other.png"}, ledgerPaths(t, env.ProjectDir))
}

// Entries written before the timestamp existed must survive: dropping them would
// empty the tab for every existing user on upgrade.
func TestPruneUploadLedger_KeepsEntriesWithoutTimestamp(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	require.NoError(t, writeUploadLedger(env.ProjectDir, []uploadLedgerEntry{
		{Path: "legacy.png"},
	}))
	createTestFile(t, env.ProjectDir, "legacy.png", "x")

	recordUploadedFile(env.ProjectDir, "new.png")
	createTestFile(t, env.ProjectDir, "new.png", "x")

	assert.Equal(t, []string{"new.png", "legacy.png"}, ledgerPaths(t, env.ProjectDir))
}

// Renaming a file that is not recorded must not create an entry.
func TestServeFileRename_UnrecordedFileAddsNothing(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "never-uploaded.txt", "x")

	w := callHandler(ServeFileRename, fileOpRequest(t, "/api/file/rename", env.ProjectDir, map[string]any{
		"path": "never-uploaded.txt",
		"name": "still-not-recorded.txt",
	}))
	require.Equal(t, http.StatusOK, w.Code)

	assert.Empty(t, ledgerPaths(t, env.ProjectDir))
}

// moveLedgerEntry must refuse paths outside the project rather than recording
// them.
func TestMoveLedgerEntry_IgnoresPathsOutsideProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	recordUploadedFile(env.ProjectDir, "inside.png")
	createTestFile(t, env.ProjectDir, "inside.png", "x")

	outside := filepath.Join(filepath.Dir(env.ProjectDir), "outside")
	require.NoError(t, os.MkdirAll(outside, 0o755))
	moveLedgerEntry(env.ProjectDir, filepath.Join(env.ProjectDir, "inside.png"),
		filepath.Join(outside, "inside.png"), false)

	assert.Equal(t, []string{"inside.png"}, ledgerPaths(t, env.ProjectDir),
		"a move out of the project leaves the record as it was; pruning drops it later")
}

// The ledger's JSON must stay readable by older code paths that ignore
// uploadedAt — a missing timestamp is the documented "keep it" state.
func TestUploadLedgerEntry_TimestampIsOptionalOnRead(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	ledgerPath := uploadLedgerFile(env.ProjectDir)
	require.NoError(t, os.MkdirAll(filepath.Dir(ledgerPath), 0o755))
	require.NoError(t, os.WriteFile(ledgerPath, []byte(`[{"path":"legacy.png"}]`), 0o644))

	entries := readUploadLedger(env.ProjectDir)
	require.Len(t, entries, 1)
	assert.Equal(t, "legacy.png", entries[0].Path)

	// And the write path emits the timestamp for new entries.
	createTestFile(t, env.ProjectDir, "legacy.png", "x")
	recordUploadedFile(env.ProjectDir, "fresh.png")
	raw, err := os.ReadFile(ledgerPath)
	require.NoError(t, err)
	var decoded []map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.NotEmpty(t, decoded)
	assert.Contains(t, decoded[0], "uploadedAt")
}
