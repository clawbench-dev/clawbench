package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedRecentDir creates <project>/.clawbench/<subDir> with the given files and
// returns the directory path.
func seedRecentDir(t *testing.T, projectDir, subDir string, names ...string) string {
	t.Helper()
	dir := filepath.Join(projectDir, ".clawbench", subDir)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for _, name := range names {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644))
	}
	return dir
}

// clearAllRequest builds the DELETE /{endpoint} request the drawer sends.
func clearAllRequest(t *testing.T, endpoint, projectDir string, body map[string]any) *http.Request {
	t.Helper()
	payload, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodDelete, endpoint, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if projectDir != "" {
		withProjectCookie(req, projectDir)
	}
	return req
}

func decodeClearResponse(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

func TestUploadRecent_ClearAll_EmptiesDirectory(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	dir := seedRecentDir(t, env.ProjectDir, "uploads", "a.png", "b.png", "c.txt")

	w := callHandler(UploadRecent, clearAllRequest(t, "/api/upload/recent", env.ProjectDir, map[string]any{"all": true}))
	resp := decodeClearResponse(t, w)

	assert.Equal(t, true, resp["ok"])
	assert.Equal(t, float64(3), resp["deleted"])
	assert.Equal(t, float64(0), resp["failed"])

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "the whole directory must be emptied")
}

func TestShareInRecent_ClearAll_EmptiesDirectory(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	dir := seedRecentDir(t, env.ProjectDir, "share-in", "s1.png", "s2.png")

	w := callHandler(ShareInRecent, clearAllRequest(t, "/api/share-in/recent", env.ProjectDir, map[string]any{"all": true}))
	resp := decodeClearResponse(t, w)

	assert.Equal(t, float64(2), resp["deleted"])
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// Each endpoint must only touch its own directory: uploads and share-in are
// separate lists in the UI, so clearing one must never empty the other.
func TestClearAll_DoesNotCrossDirectories(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	uploads := seedRecentDir(t, env.ProjectDir, "uploads", "u.png")
	shares := seedRecentDir(t, env.ProjectDir, "share-in", "s.png")

	callHandler(UploadRecent, clearAllRequest(t, "/api/upload/recent", env.ProjectDir, map[string]any{"all": true}))

	_, err := os.Stat(filepath.Join(shares, "s.png"))
	assert.NoError(t, err, "clearing uploads must leave share-in alone")

	entries, err := os.ReadDir(uploads)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// Clearing must never reach outside .clawbench/<subDir> — a project file must
// survive even when it sits right next to the uploads directory.
func TestClearAll_LeavesProjectFilesAlone(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	seedRecentDir(t, env.ProjectDir, "uploads", "u.png")
	projectFile := filepath.Join(env.ProjectDir, "main.go")
	require.NoError(t, os.WriteFile(projectFile, []byte("package main"), 0o644))
	// A sibling directory that must not be touched either.
	sibling := filepath.Join(env.ProjectDir, ".clawbench", "generated")
	require.NoError(t, os.MkdirAll(sibling, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sibling, "g.png"), []byte("x"), 0o644))

	callHandler(UploadRecent, clearAllRequest(t, "/api/upload/recent", env.ProjectDir, map[string]any{"all": true}))

	_, err := os.Stat(projectFile)
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(sibling, "g.png"))
	assert.NoError(t, err, "a sibling directory under .clawbench must survive")
}

// Subdirectories are skipped: a directory upload can hold a whole tree the user
// still expects, and nothing in this UI ever created one here.
func TestClearAll_SkipsSubdirectories(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	dir := seedRecentDir(t, env.ProjectDir, "uploads", "top.png")
	nested := filepath.Join(dir, "batch")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	inner := filepath.Join(nested, "inner.png")
	require.NoError(t, os.WriteFile(inner, []byte("x"), 0o644))

	w := callHandler(UploadRecent, clearAllRequest(t, "/api/upload/recent", env.ProjectDir, map[string]any{"all": true}))
	resp := decodeClearResponse(t, w)

	assert.Equal(t, float64(1), resp["deleted"], "only the flat file counts")
	// A skipped directory must not be reported as a FAILED deletion either:
	// the sweep never tried, so a "partial failure" toast would be a lie.
	assert.Equal(t, float64(0), resp["failed"], "a skipped subdirectory is not a failure")
	_, err := os.Stat(inner)
	assert.NoError(t, err, "a nested file must survive the sweep")
}

// A missing directory means there is nothing to clear, which is success — the
// UI may hold a list from before the directory was removed.
func TestClearAll_MissingDirectoryIsSuccess(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	w := callHandler(UploadRecent, clearAllRequest(t, "/api/upload/recent", env.ProjectDir, map[string]any{"all": true}))
	resp := decodeClearResponse(t, w)

	assert.Equal(t, true, resp["ok"])
	assert.Equal(t, float64(0), resp["deleted"])
}

// The single-file path must keep working: `all` is additive, not a replacement.
func TestDeleteRecent_SinglePathStillWorks(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	dir := seedRecentDir(t, env.ProjectDir, "uploads", "keep.png", "drop.png")

	w := callHandler(UploadRecent, clearAllRequest(t, "/api/upload/recent", env.ProjectDir,
		map[string]any{"path": ".clawbench/uploads/drop.png"}))
	resp := decodeClearResponse(t, w)
	assert.Equal(t, true, resp["ok"])

	_, err := os.Stat(filepath.Join(dir, "drop.png"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(dir, "keep.png"))
	assert.NoError(t, err, "a single-file delete must not touch its neighbors")
}

// An empty body without `all` must stay a 400. Treating "no path" as "clear
// everything" would let any client wipe the directory by forgetting a field.
func TestDeleteRecent_EmptyBodyIsNotClearAll(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	dir := seedRecentDir(t, env.ProjectDir, "uploads", "a.png")

	req := clearAllRequest(t, "/api/upload/recent", env.ProjectDir, map[string]any{})
	w := callHandler(UploadRecent, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	_, err := os.Stat(filepath.Join(dir, "a.png"))
	assert.NoError(t, err, "a malformed request must not delete anything")
}

func TestClearAll_NoProjectCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	w := callHandler(UploadRecent, clearAllRequest(t, "/api/upload/recent", "", map[string]any{"all": true}))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// A directory that cannot be read must be an error, not "nothing to clear":
// silently answering ok would tell the user the list is empty when files are
// still there.
func TestClearAll_UnreadableDirectoryIsAnError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	env, teardown := setupTestEnv(t)
	defer teardown()

	dir := seedRecentDir(t, env.ProjectDir, "uploads", "a.png")
	require.NoError(t, os.Chmod(dir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	w := callHandler(UploadRecent, clearAllRequest(t, "/api/upload/recent", env.ProjectDir, map[string]any{"all": true}))

	assert.NotEqual(t, http.StatusOK, w.Code, "an unreadable directory must not report success")
}

// One undeletable file must not abort the sweep; it is counted instead so the
// UI can say "cleared N, M failed" rather than looking half-cleared.
func TestClearAll_CountsUndeletableFilesAndContinues(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	env, teardown := setupTestEnv(t)
	defer teardown()

	dir := seedRecentDir(t, env.ProjectDir, "uploads", "ok1.png", "locked.png", "ok2.png")
	locked := filepath.Join(dir, "locked.png")
	require.NoError(t, os.Chmod(dir, 0o755))
	// Removing a file needs write permission on the DIRECTORY, not the file, so
	// make the directory read-only after seeding: every remove fails.
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	w := callHandler(UploadRecent, clearAllRequest(t, "/api/upload/recent", env.ProjectDir, map[string]any{"all": true}))
	resp := decodeClearResponse(t, w)

	assert.Equal(t, float64(0), resp["deleted"])
	assert.Equal(t, float64(3), resp["failed"], "every remove failure is counted, and the sweep does not abort")
	_, err := os.Stat(locked)
	assert.NoError(t, err)
}
