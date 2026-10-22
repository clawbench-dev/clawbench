package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustTime parses an RFC3339 timestamp, failing the test on a malformed literal.
func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	require.NoError(t, err)
	return parsed
}

// uploadIntoDir posts a file through the file-manager upload path (the one that
// carries a `dir` form field) and returns the project-relative path the server
// answered with.
func uploadIntoDir(t *testing.T, env *testEnv, filename, dir string) string {
	t.Helper()
	req := createMultipartUploadRequest(t, filename, "image-bytes", dir)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(UploadFile, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		OK   bool   `json:"ok"`
		Path string `json:"path"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.True(t, resp.OK)
	require.NotEmpty(t, resp.Path)
	return resp.Path
}

// recentUploadPaths lists the GET /api/upload/recent rows as a path set.
func recentUploadPaths(t *testing.T, env *testEnv) []recentFile {
	t.Helper()
	req := newRequest(t, http.MethodGet, "/api/upload/recent", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(UploadRecent, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var files []recentFile
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &files))
	return files
}

// The reported bug (#573): a file uploaded into the PROJECT ROOT through the
// file manager is visible in the file tree but never appeared in "最近上传",
// because that list only scanned .clawbench/uploads/.
func TestUploadRecent_IncludesFileManagerUploadToProjectRoot(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	rel := uploadIntoDir(t, env, "screenshot.png", ".")
	require.Equal(t, "screenshot.png", rel, "a root upload is project-relative")

	files := recentUploadPaths(t, env)
	require.Len(t, files, 1)
	assert.Equal(t, "screenshot.png", files[0].Path)
	assert.Equal(t, "screenshot.png", files[0].Name)
	assert.Equal(t, uploadOriginProject, files[0].Origin,
		"a project file must be distinguishable: deleting it must not delete the file")
}

func TestUploadRecent_IncludesFileManagerUploadToSubdirectory(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	rel := uploadIntoDir(t, env, "diagram.png", "docs/images")
	require.Equal(t, "docs/images/diagram.png", rel)

	files := recentUploadPaths(t, env)
	require.Len(t, files, 1)
	assert.Equal(t, "docs/images/diagram.png", files[0].Path)
	assert.Equal(t, "diagram.png", files[0].Name)
}

// Chat attachments already show up by scanning the uploads directory. Recording
// them as well would list every attachment twice.
func TestUploadRecent_ChatUploadIsListedOnce(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// No `dir` field → the chat attachment path (.clawbench/uploads/).
	req := createMultipartUploadRequest(t, "chat.png", "bytes", "")
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(UploadFile, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	files := recentUploadPaths(t, env)
	require.Len(t, files, 1)
	assert.Equal(t, ".clawbench/uploads/chat.png", files[0].Path)
	assert.Equal(t, uploadOriginUploads, files[0].Origin)
}

// Share-in uploads are the "最近分享" tab's own directory; listing them here
// too would double-list the same file under two tabs.
func TestUploadRecent_ShareInUploadIsNotRecorded(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	uploadIntoDir(t, env, "shared.png", ".clawbench/share-in")

	assert.Empty(t, recentUploadPaths(t, env))
}

func TestUploadRecent_SortsBothSourcesByModificationTime(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// An uploads-directory file that is OLDER than the ledger entry.
	seedRecentDir(t, env.ProjectDir, "uploads", "old.png")
	oldPath := filepath.Join(env.ProjectDir, ".clawbench", "uploads", "old.png")
	oldTime := mustTime(t, "2020-01-01T00:00:00Z")
	require.NoError(t, os.Chtimes(oldPath, oldTime, oldTime))

	uploadIntoDir(t, env, "fresh.png", ".")

	files := recentUploadPaths(t, env)
	require.Len(t, files, 2)
	assert.Equal(t, "fresh.png", files[0].Path, "newest first across both sources")
	assert.Equal(t, ".clawbench/uploads/old.png", files[1].Path)
}

// A file that has since been deleted must not linger in the list: the ledger
// keeps the entry (a file on an unmounted volume should come back), so the read
// path has to filter by existence.
func TestUploadRecent_SkipsLedgerEntryWhoseFileIsGone(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	uploadIntoDir(t, env, "gone.png", ".")
	require.NoError(t, os.Remove(filepath.Join(env.ProjectDir, "gone.png")))

	assert.Empty(t, recentUploadPaths(t, env))
}

func TestUploadRecent_ReuploadMovesEntryToFrontWithoutDuplicating(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	uploadIntoDir(t, env, "again.png", ".")
	uploadIntoDir(t, env, "other.png", ".")
	// Re-upload under the same name: UploadFile appends a suffix, so record the
	// original path again directly to exercise the dedupe branch.
	recordUploadedFile(env.ProjectDir, "again.png")

	entries := readUploadLedger(env.ProjectDir)
	require.Len(t, entries, 2, "the same path must not be recorded twice")
	assert.Equal(t, "again.png", entries[0].Path, "a re-upload moves the entry to the front")
}

// Deleting a ledger row means "stop listing this", NOT "delete my file": the
// file is an ordinary project file that the file tree still shows.
func TestUploadRecentDelete_ProjectFileRemovesEntryButKeepsFile(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	uploadIntoDir(t, env, "keep-me.png", ".")
	abs := filepath.Join(env.ProjectDir, "keep-me.png")
	require.FileExists(t, abs)

	w := callHandler(UploadRecent, clearAllRequest(t, "/api/upload/recent", env.ProjectDir, map[string]any{
		"path": "keep-me.png",
	}))
	resp := decodeClearResponse(t, w)
	assert.Equal(t, true, resp["ok"])

	assert.FileExists(t, abs, "deleting a recent row must never delete a project file")
	assert.Empty(t, recentUploadPaths(t, env))
}

// A ledger miss for a path outside .clawbench/ must not fall through to the
// file-deletion branch below it — that would delete a project file the user
// only asked to un-list.
func TestUploadRecentDelete_UnknownProjectPathDoesNotDeleteTheFile(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "source.go", "package main")
	abs := filepath.Join(env.ProjectDir, "source.go")

	req := clearAllRequest(t, "/api/upload/recent", env.ProjectDir, map[string]any{"path": "source.go"})
	w := callHandler(UploadRecent, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.FileExists(t, abs, "an unrecorded project file must never be deleted through this endpoint")
}

// Clear-all empties the whole list, which now means both sources — while still
// leaving project files on disk.
func TestUploadRecentClearAll_EmptiesLedgerButKeepsProjectFiles(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	seedRecentDir(t, env.ProjectDir, "uploads", "u.png")
	uploadIntoDir(t, env, "p.png", ".")
	abs := filepath.Join(env.ProjectDir, "p.png")
	require.FileExists(t, abs)

	w := callHandler(UploadRecent, clearAllRequest(t, "/api/upload/recent", env.ProjectDir, map[string]any{"all": true}))
	resp := decodeClearResponse(t, w)
	assert.Equal(t, true, resp["ok"])

	assert.FileExists(t, abs, "clear-all must not delete project files")
	assert.Empty(t, recentUploadPaths(t, env))
	entries, err := os.ReadDir(filepath.Join(env.ProjectDir, ".clawbench", "uploads"))
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// The reported count is what the user watched disappear from the tab, so it has
// to include the ledger rows. Counting only the directory would answer
// "cleared 0 files" for a list that visibly emptied.
func TestUploadRecentClearAll_CountsBothSources(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	seedRecentDir(t, env.ProjectDir, "uploads", "u1.png", "u2.png")
	uploadIntoDir(t, env, "p.png", ".")

	w := callHandler(UploadRecent, clearAllRequest(t, "/api/upload/recent", env.ProjectDir, map[string]any{"all": true}))
	resp := decodeClearResponse(t, w)
	assert.Equal(t, float64(3), resp["deleted"])
	assert.Equal(t, float64(0), resp["failed"])
}

// A file uploaded to an absolute directory OUTSIDE the project cannot be
// addressed by a project-relative path, so it must not be recorded — a ledger
// entry like "../elsewhere/x.png" would resolve outside the project when the
// list is rendered.
func TestUploadFile_OutsideProjectUploadIsNotRecorded(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	outside := filepath.Join(filepath.Dir(env.ProjectDir), "outside")
	require.NoError(t, os.MkdirAll(outside, 0o755))

	uploadIntoDir(t, env, "external.png", outside)

	assert.Empty(t, recentUploadPaths(t, env))
	assert.Empty(t, readUploadLedger(env.ProjectDir))
}

// The ledger is a convenience index: a corrupt one must not take the tab down
// with it, and must not hide the uploads-directory rows that do not depend on
// it.
func TestUploadRecent_SurvivesCorruptLedger(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	seedRecentDir(t, env.ProjectDir, "uploads", "still-listed.png")
	ledgerPath := filepath.Join(env.ProjectDir, ".clawbench", "upload-log.json")
	require.NoError(t, os.WriteFile(ledgerPath, []byte("{not json"), 0o644))

	files := recentUploadPaths(t, env)
	require.Len(t, files, 1)
	assert.Equal(t, ".clawbench/uploads/still-listed.png", files[0].Path)
}

// A later successful upload must repair a corrupt ledger rather than keep
// failing to record forever.
func TestUploadFile_RepairsCorruptLedger(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	ledgerPath := filepath.Join(env.ProjectDir, ".clawbench", "upload-log.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(ledgerPath), 0o755))
	require.NoError(t, os.WriteFile(ledgerPath, []byte("{not json"), 0o644))

	uploadIntoDir(t, env, "fresh.png", ".")

	entries := readUploadLedger(env.ProjectDir)
	require.Len(t, entries, 1)
	assert.Equal(t, "fresh.png", entries[0].Path)
}

// Share-in keeps its own semantics: its rows ARE files the app owns, so delete
// still removes them from disk.
func TestShareInRecentDelete_StillDeletesTheFile(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	dir := seedRecentDir(t, env.ProjectDir, "share-in", "s.png")

	w := callHandler(ShareInRecent, clearAllRequest(t, "/api/share-in/recent", env.ProjectDir, map[string]any{
		"path": ".clawbench/share-in/s.png",
	}))
	assert.Equal(t, http.StatusOK, w.Code)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "share-in rows are app-owned; deleting one removes the file")
}

// The ledger is a per-project index, not shared state: one project's uploads
// must never appear in another's list.
//
// Project B has a file at the SAME path but never uploaded it (it was created
// by other means). That is what makes this test able to fail: with a single
// shared ledger, B would read A's entry, resolve it against B's own directory —
// where a file of that name exists — and list a row B never uploaded. Merely
// asserting that B is empty after A's upload would pass even with a global
// ledger, because the existence filter would hide the leak whenever B has no
// file at that path.
func TestUploadRecent_LedgerIsPerProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	other := canonPath(t.TempDir())
	require.NoError(t, os.MkdirAll(other, 0o755))

	uploadIntoDir(t, env, "shot.png", ".")
	// Same name in the other project, created outside the upload path.
	require.NoError(t, os.WriteFile(filepath.Join(other, "shot.png"), []byte("not an upload"), 0o644))

	files := recentUploadPaths(t, env)
	require.Len(t, files, 1)
	assert.Equal(t, "shot.png", files[0].Path)
	assert.Equal(t, int64(len("image-bytes")), files[0].Size)

	req := newRequest(t, http.MethodGet, "/api/upload/recent", nil)
	withProjectCookie(req, other)
	w := callHandler(UploadRecent, req)
	require.Equal(t, http.StatusOK, w.Code)
	var otherFiles []recentFile
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &otherFiles))
	assert.Empty(t, otherFiles, "a file that was never uploaded must not be listed as a recent upload")
}
