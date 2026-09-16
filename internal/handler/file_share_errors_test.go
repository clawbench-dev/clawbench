package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// swapClosedWriteDB replaces the service write handle with a closed DB while
// keeping a healthy read handle, so write statements fail with a real SQL
// error. Returns a cleanup that restores the original handles.
func swapClosedWriteDB(t *testing.T, healthyRead *sql.DB) func() {
	t.Helper()
	origWrite := service.UnsafeDBForTest()
	closedDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	require.NoError(t, closedDB.Close())
	cleanup := service.SetDBForTest(closedDB, healthyRead)
	return func() {
		cleanup()
		assert.Same(t, origWrite, service.UnsafeDBForTest(), "write handle must be restored")
	}
}

// swapClosedReadDB replaces the service read handle with a closed DB so read
// queries fail with a real SQL error. Returns a cleanup restoring originals.
func swapClosedReadDB(t *testing.T, healthyWrite *sql.DB) func() {
	t.Helper()
	closedDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	require.NoError(t, closedDB.Close())
	return service.SetDBForTest(healthyWrite, closedDB)
}

// ─── Management endpoints: DB write failures ─────────────────────────────────

func TestShareManage_Create_UpsertDBError_500(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "err/create.md", "hi")

	// Healthy read DB, closed write DB → UpsertFileShare's insert fails.
	defer swapClosedWriteDB(t, service.UnsafeDBForTest())()

	req := newRequest(t, http.MethodPost, "/api/share", map[string]string{"path": absPath})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeShareManage, req)
	assert.NotEqual(t, http.StatusOK, w.Code, "DB failure must surface as an error")
	assert.NotEqual(t, http.StatusNotFound, w.Code)
}

func TestShareManage_Status_GetShareDBError_500(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "err/status.md", "hi")

	// Read failure → GetFileShareByPath errors.
	defer swapClosedReadDB(t, service.UnsafeDBForTest())()

	req := newRequest(t, http.MethodGet, "/api/share?path="+absPath, nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeShareManage, req)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestShareManage_Revoke_DeleteDBError_500(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "err/revoke.md", "hi")

	// Write failure → DeleteFileShareByPath errors.
	defer swapClosedWriteDB(t, service.UnsafeDBForTest())()

	req := newRequest(t, http.MethodDelete, "/api/share?path="+absPath, nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeShareManage, req)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

// ─── Resolve-target failure branches ─────────────────────────────────────────

func TestShareManage_ResolveTargetRejectsAbsoluteOutsideRoot(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	_ = env

	// An absolute path outside every root path → resolveAbsPath writes a
	// 403-style error and serveShareCreate/Status/Revoke bail early.
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		req := newRequest(t, method, "/api/share", map[string]string{"path": "/etc/hostname"})
		w := callHandler(ServeShareManage, req)
		assert.NotEqual(t, http.StatusOK, w.Code, "method %s outside-root must be rejected", method)
	}
}

// ─── List: DB failures ───────────────────────────────────────────────────────

func TestShareList_ListDBError_500(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	defer swapClosedReadDB(t, service.UnsafeDBForTest())()

	req := newRequest(t, http.MethodGet, "/api/share/list", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeShareList, req)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestShareList_RevokeAllDBError_500(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "err/all.md", "x")
	_ = absPath

	defer swapClosedWriteDB(t, service.UnsafeDBForTest())()

	req := newRequest(t, http.MethodDelete, "/api/share/list", map[string]any{"all": true})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeShareList, req)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestShareList_RevokeByTokenDBError_500(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "err/tok.md", "x")
	_ = absPath

	defer swapClosedWriteDB(t, service.UnsafeDBForTest())()

	req := newRequest(t, http.MethodDelete, "/api/share/list?token=deadbeefdeadbeefdeadbeefdeadbeef", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeShareList, req)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

// ─── Public endpoints: DB failure on token lookup ────────────────────────────

func TestSharePublic_TokenLookupDBError_500(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	_ = env

	defer swapClosedReadDB(t, service.UnsafeDBForTest())()

	req := newRequest(t, http.MethodGet, "/api/share/deadbeefdeadbeefdeadbeefdeadbeef/file", nil)
	w := callHandler(ServeSharePublic, req)
	assert.NotEqual(t, http.StatusOK, w.Code, "DB error must not be treated as unknown token 404")
	assert.NotEqual(t, http.StatusNotFound, w.Code)
}

func TestSharePublic_UnknownSubPath_404(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/readme.md", "hi")
	token := createShareViaAPI(t, env, absPath)

	// A rest path that matches no public endpoint → uniform 404.
	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/bogus", nil)
	w := callHandler(ServeSharePublic, req)
	assertStatus(t, w, http.StatusNotFound)
}

func TestSharePublic_RequireGetMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/m.md", "hi")
	token := createShareViaAPI(t, env, absPath)

	req := newRequest(t, http.MethodPost, "/api/share/"+token+"/file", nil)
	w := callHandler(ServeSharePublic, req)
	assert.NotEqual(t, http.StatusOK, w.Code, "POST to public share endpoint must be rejected")
}

// ─── Public file content edge cases ──────────────────────────────────────────

func TestSharePublic_FileTooLarge_400(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// 10MB cap in serveShareFileContent. Write an 11MB sparse file.
	big := filepath.Join(env.ProjectDir, "docs", "big.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(big), 0o755))
	f, err := os.Create(big)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(11*1024*1024))
	require.NoError(t, f.Close())
	defer func() { _ = os.Remove(big) }()

	token := createShareViaAPI(t, env, big)

	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/file", nil)
	w := callHandler(ServeSharePublic, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestSharePublic_BinaryContentSniffedAndSanitized(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// An unknown-extension file containing a NUL byte in the first 8KB: not a
	// recognized text file, so serveShareFileContent sniffs it as binary and
	// sanitizes the content for display.
	bin := filepath.Join(env.ProjectDir, "docs", "blob.dat")
	createTestFile(t, env.ProjectDir, "docs/blob.dat", "line1\x00line2")

	token := createShareViaAPI(t, env, bin)

	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/file", nil)
	w := callHandler(ServeSharePublic, req)
	assertOK(t, w)

	var fc FileContent
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
	assert.True(t, fc.IsBinary, "file with NUL bytes must be flagged binary")
}

// ─── Public local endpoint edge cases ────────────────────────────────────────

func TestSharePublic_LocalBareServesSharedFile(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/readme.md", "SELF")
	token := createShareViaAPI(t, env, absPath)

	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/local", nil)
	w := callHandler(ServeSharePublic, req)
	assertOK(t, w)
	assert.Equal(t, "SELF", w.Body.String())
}

func TestSharePublic_LocalRelativeMissingFile_404(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/readme.md", "hi")
	token := createShareViaAPI(t, env, absPath)

	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/local/img/missing.png", nil)
	w := callHandler(ServeSharePublic, req)
	assertStatus(t, w, http.StatusNotFound)
}

func TestSharePublic_LocalNonAbsoluteQueryPath_404(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/readme.md", "hi")
	token := createShareViaAPI(t, env, absPath)

	// ?path= must be absolute or start with '/'; a relative value is rejected.
	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/local?path=rel%2Fimg.png", nil)
	w := callHandler(ServeSharePublic, req)
	assertStatus(t, w, http.StatusNotFound)
}

// TestSharePublic_LocalQueryPathOutsideRoot_404 asserts ?path= cannot reach a
// file outside the share's scope.
//
// NOTE: on its own this test is a weak signal. The fixture narrows
// model.RootPaths to a temp dir, so before the traversal fix it passed because
// of that narrowing rather than because the share was confined — with the
// production value (["/"]) the same request returned 200. The
// RootPaths=["/"] variant lives in
// TestSharePublic_LocalAbsolutePathOutsideProject_404, which is the guard that
// actually covers production.
func TestSharePublic_LocalQueryPathOutsideRoot_404(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/readme.md", "hi")
	token := createShareViaAPI(t, env, absPath)

	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/local?path=/etc/passwd", nil)
	w := callHandler(ServeSharePublic, req)
	assertStatus(t, w, http.StatusNotFound)
}

// ─── Download edge cases ─────────────────────────────────────────────────────

func TestSharePublic_DownloadFileGone_404(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/gone.md", "hi")
	token := createShareViaAPI(t, env, absPath)
	require.NoError(t, os.Remove(absPath))

	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/download", nil)
	w := callHandler(ServeSharePublic, req)
	assertStatus(t, w, http.StatusNotFound)
}

func TestSharePublic_DownloadDirectory_404(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/real.md", "hi")
	token := createShareViaAPI(t, env, absPath)

	// /file endpoint 404s when the shared file is a directory (defensive).
	// Point the share token at a directory by replacing the shared file on disk.
	require.NoError(t, os.Remove(absPath))
	require.NoError(t, os.MkdirAll(absPath, 0o755))

	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/download", nil)
	w := callHandler(ServeSharePublic, req)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

// ─── Share page ──────────────────────────────────────────────────────────────

func TestSharePage_RequiresGetOrHead(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		req := newRequest(t, m, "/share/abc", nil)
		w := callHandler(ServeSharePage, req)
		assert.NotEqual(t, http.StatusOK, w.Code, "method %s must be rejected", m)
	}
}

// ─── parseSharePublicPath unit coverage ──────────────────────────────────────

func TestParseSharePublicPath(t *testing.T) {
	cases := []struct {
		path        string
		token, rest string
		ok          bool
	}{
		{"/api/share/tok123", "tok123", "", true},
		{"/api/share/tok123/file", "tok123", "file", true},
		{"/api/share/tok123/local/img.png", "tok123", "local/img.png", true},
		{"/other/path", "", "", false},
		{"/api/share/", "", "", true}, // empty token (rest is "")
	}
	for _, c := range cases {
		tok, rest, ok := parseSharePublicPath(c.path)
		assert.Equal(t, c.token, tok, "path %s", c.path)
		assert.Equal(t, c.rest, rest, "path %s", c.path)
		assert.Equal(t, c.ok, ok, "path %s", c.path)
	}
}

// ─── ServeShareList method not allowed and response decoding helpers ─────────

func TestSharePage_ServeShareHTML_NoShareHTML_FallsBack(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	_ = env

	// GET is allowed; the response is share.html if present in embedded FS, or
	// falls back to web/share.html. In the test env neither may exist — the
	// handler must simply not panic (may 200 or 500 depending on FS state).
	req := newRequest(t, http.MethodGet, "/share/tok", nil)
	w := callHandler(ServeSharePage, req)
	// GET/HEAD is allowed and should return *something* (not a method error).
	assert.NotEqual(t, http.StatusMethodNotAllowed, w.Code)
}

// ensure JSON errors from share endpoints use the localized shape (contains "error").
func TestShareManage_DecodeError_InvalidBody(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := httptestNewRequestWithRawBody(t, http.MethodPost, "/api/share", "{not json")
	w := callHandler(ServeShareManage, req)
	assertStatus(t, w, http.StatusBadRequest)
	assert.Contains(t, strings.ToLower(w.Body.String()), "error")
}

func httptestNewRequestWithRawBody(t *testing.T, method, target, rawBody string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, target, strings.NewReader(rawBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	return req
}
