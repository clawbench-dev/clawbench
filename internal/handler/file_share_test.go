package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"clawbench/internal/store"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createShareTestFile writes a file and returns its absolute path.
func createShareTestFile(t *testing.T, env *testEnv, relPath, content string) string {
	t.Helper()
	createTestFile(t, env.ProjectDir, relPath, content)
	return filepath.Join(env.ProjectDir, relPath)
}

// createShareViaAPI creates a share through the auth endpoint and returns the token.
//
// The project cookie is set to env.ProjectDir — the project root, matching what
// production sends (the user's selected project). Using filepath.Dir(absPath)
// instead would understate the real boundary and hide the confinement logic
// under test.
func createShareViaAPI(t *testing.T, env *testEnv, absPath string) string {
	t.Helper()
	req := newRequest(t, http.MethodPost, "/api/share", map[string]string{"path": absPath})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeShareManage, req)
	assertOK(t, w)

	var resp shareResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.Token)
	assert.Equal(t, "/share/"+resp.Token, resp.Path)
	return resp.Token
}

// ─── Management endpoints ────────────────────────────────────────────────────

func TestShareManage_CreateStatusRevoke(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/a.md", "# Hello\n\nbody")
	token := createShareViaAPI(t, env, absPath)

	// GET status → share exists
	req := newRequest(t, http.MethodGet, "/api/share?path="+absPath, nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeShareManage, req)
	assertOK(t, w)
	var resp shareResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, token, resp.Token)

	// DELETE revoke
	req = newRequest(t, http.MethodDelete, "/api/share?path="+absPath, nil)
	withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeShareManage, req)
	assertOK(t, w)

	// GET status → empty
	req = newRequest(t, http.MethodGet, "/api/share?path="+absPath, nil)
	withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeShareManage, req)
	assertOK(t, w)
	resp = shareResponse{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.Token)
}

func TestShareManage_CreateRotatesToken(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/rotate.md", "v1")
	token1 := createShareViaAPI(t, env, absPath)

	// Re-create (regenerate) → new token, old invalid.
	token2 := createShareViaAPI(t, env, absPath)
	assert.NotEqual(t, token1, token2)

	// Old token's public endpoint 404s.
	req := newRequest(t, http.MethodGet, "/api/share/"+token1+"/file", nil)
	w := callHandler(ServeSharePublic, req)
	assertStatus(t, w, http.StatusNotFound)
}

func TestShareManage_RejectsDirectory(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	dir := filepath.Join(env.ProjectDir, "docs")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	req := newRequest(t, http.MethodPost, "/api/share", map[string]string{"path": dir})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeShareManage, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestShareManage_RejectsMissingPath(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/share", map[string]string{})
	w := callHandler(ServeShareManage, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestShareManage_RejectsNonExistentFile(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	missing := filepath.Join(env.ProjectDir, "nope.md")
	req := newRequest(t, http.MethodPost, "/api/share", map[string]string{"path": missing})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeShareManage, req)
	assertStatus(t, w, http.StatusNotFound)
}

func TestShareManage_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPut, "/api/share", map[string]string{"path": "/x"})
	w := callHandler(ServeShareManage, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

// ─── Public endpoints ────────────────────────────────────────────────────────

func TestSharePublic_FileContent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/content.md", "# Doc\n\n**bold** text")
	token := createShareViaAPI(t, env, absPath)

	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/file", nil)
	w := callHandler(ServeSharePublic, req)
	assertOK(t, w)

	var fc FileContent
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
	assert.Equal(t, "# Doc\n\n**bold** text", fc.Content)
	assert.Equal(t, "content.md", fc.Name)
	assert.Equal(t, absPath, fc.Path)
	assert.True(t, fc.Supported)
}

func TestSharePublic_InvalidToken_404(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	for _, p := range []string{
		"/api/share/ffffffffffffffffffffffffffffffff/file",
		"/api/share//file",
		"/api/share/nonexistent-token-here/file",
	} {
		req := newRequest(t, http.MethodGet, p, nil)
		w := callHandler(ServeSharePublic, req)
		assertStatus(t, w, http.StatusNotFound)
	}
}

func TestSharePublic_FileDeletedAfterShare_404(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/tmp.md", "x")
	token := createShareViaAPI(t, env, absPath)

	require.NoError(t, os.Remove(absPath))

	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/file", nil)
	w := callHandler(ServeSharePublic, req)
	assertStatus(t, w, http.StatusNotFound)
}

func TestSharePublic_Download(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/dl.md", "# download me")
	token := createShareViaAPI(t, env, absPath)

	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/download", nil)
	w := callHandler(ServeSharePublic, req)
	assertOK(t, w)
	assert.Equal(t, "# download me", w.Body.String())
	assert.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
	assert.Contains(t, w.Header().Get("Content-Disposition"), "dl.md")
}

func TestSharePublic_LocalResolvesRelativeToSharedFileDir(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Shared markdown references ./img/pic.png in the same directory.
	createTestFile(t, env.ProjectDir, "docs/img/pic.png", "PNGDATA")
	absPath := createShareTestFile(t, env, "docs/readme.md", "![p](img/pic.png)")
	token := createShareViaAPI(t, env, absPath)

	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/local/img/pic.png", nil)
	w := callHandler(ServeSharePublic, req)
	assertOK(t, w)
	assert.Equal(t, "PNGDATA", w.Body.String())
}

func TestSharePublic_LocalTraversalRejected(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "secret.txt", "SECRET")
	absPath := createShareTestFile(t, env, "docs/readme.md", "hi")
	token := createShareViaAPI(t, env, absPath)

	// ../secret.txt must NOT escape the shared file's dir.
	for _, p := range []string{
		"/api/share/" + token + "/local/../secret.txt",
		"/api/share/" + token + "/local/..%2Fsecret.txt",
		"/api/share/" + token + "/local/../../secret.txt",
	} {
		req := newRequest(t, http.MethodGet, p, nil)
		w := callHandler(ServeSharePublic, req)
		assert.NotEqual(t, http.StatusOK, w.Code, "path %s must be rejected", p)
		assert.NotContains(t, w.Body.String(), "SECRET", "path %s leaked content", p)
	}
}

func TestSharePublic_LocalAbsolutePath(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// A file in the project root, referenced absolutely by the shared markdown.
	// docs/m.md referencing ../images/abs.png is the real-world case: the share
	// SPA rewrites every local media reference to the ?path= form using the
	// document's own directory, so a `..` hop that stays inside the project must
	// keep working.
	absTarget := filepath.Join(env.ProjectDir, "images", "abs.png")
	createTestFile(t, env.ProjectDir, "images/abs.png", "ABSDATA")
	absPath := createShareTestFile(t, env, "docs/m.md", "hi")
	token := createShareViaAPI(t, env, absPath)

	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/local?path="+absTarget, nil)
	w := callHandler(ServeSharePublic, req)
	assertOK(t, w)
	assert.Equal(t, "ABSDATA", w.Body.String())
}

// TestSharePublic_LocalAbsolutePathOutsideProject_404 is the regression guard
// for the path-traversal fix: the public endpoints are unauthenticated, so a
// token holder must not be able to read anything outside the share's scope —
// regardless of how wide model.RootPaths is.
//
// The companion test in file_share_errors_test.go
// (TestSharePublic_LocalQueryPathOutsideRoot_404) covers the same shape, but it
// passes for the WRONG reason when the fixture narrows RootPaths: it then
// asserts on the fixture's boundary, not the share's. This test restores the
// production value so the share's own confinement is what is actually
// exercised.
func TestSharePublic_LocalAbsolutePathOutsideProject_404(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// The share covers only docs/m.md.
	absPath := createShareTestFile(t, env, "docs/m.md", "hi")
	token := createShareViaAPI(t, env, absPath)

	// A secret living outside the project root, but still inside the temp tree
	// so the test is hermetic.
	secretDir := filepath.Join(env.WatchDir, "outside")
	require.NoError(t, os.MkdirAll(secretDir, 0o755))
	secret := filepath.Join(secretDir, "secret.txt")
	require.NoError(t, os.WriteFile(secret, []byte("TOP_SECRET"), 0o600))

	for _, tc := range []struct {
		name  string
		roots []string
	}{
		// Production semantics on Unix: the app browses the whole machine.
		// Before the fix this made the ?path= check degenerate to "is absolute".
		{"production roots", []string{"/"}},
		{"narrowed roots", []string{env.WatchDir}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model.RootPaths = tc.roots

			req := newRequest(t, http.MethodGet,
				"/api/share/"+token+"/local?path="+secret, nil)
			w := callHandler(ServeSharePublic, req)

			assert.NotEqual(t, http.StatusOK, w.Code,
				"share token must not reach outside the shared scope")
			assert.NotContains(t, w.Body.String(), "TOP_SECRET",
				"share token leaked content outside the shared scope")
		})
	}
}

// TestSharePublic_LocalAbsolutePathEtcPasswd_404 pins the exact reported
// exploit: an unrelated share token plus ?path=/etc/passwd. Skipped when the
// file is unreadable so the test stays hermetic across environments.
func TestSharePublic_LocalAbsolutePathEtcPasswd_404(t *testing.T) {
	if _, err := os.Stat("/etc/passwd"); err != nil {
		t.Skip("/etc/passwd not available")
	}

	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/m.md", "hi")
	token := createShareViaAPI(t, env, absPath)

	model.RootPaths = []string{"/"} // production semantics

	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/local?path=/etc/passwd", nil)
	w := callHandler(ServeSharePublic, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.NotContains(t, w.Body.String(), "root:")
}

// ─── Referenced-file content endpoint ────────────────────────────────────────

// TestSharePublic_ContentServesReferencedFile covers the endpoint the share SPA
// uses to render a file the shared document links to: same FileContent JSON
// shape as /file, but resolved from ?path= within the share's scope.
func TestSharePublic_ContentServesReferencedFile(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/readme.md", "[g](guide.md)")
	target := filepath.Join(env.ProjectDir, "docs", "guide.md")
	createTestFile(t, env.ProjectDir, "docs/guide.md", "# Guide\n\ntext")
	token := createShareViaAPI(t, env, absPath)

	req := newRequest(t, http.MethodGet,
		"/api/share/"+token+"/content?path="+target, nil)
	w := callHandler(ServeSharePublic, req)
	assertOK(t, w)

	var fc FileContent
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
	assert.Equal(t, "# Guide\n\ntext", fc.Content)
	assert.Equal(t, "guide.md", fc.Name)
	assert.Equal(t, target, fc.Path)
}

// TestSharePublic_ContentOutsideScope_404 pins the boundary: ?path= is
// confined to the share's own root, exactly like /local?path=.
func TestSharePublic_ContentOutsideScope_404(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/m.md", "hi")
	token := createShareViaAPI(t, env, absPath)

	secret := filepath.Join(env.WatchDir, "outside.txt")
	require.NoError(t, os.WriteFile(secret, []byte("TOP_SECRET"), 0o600))

	req := newRequest(t, http.MethodGet,
		"/api/share/"+token+"/content?path="+secret, nil)
	w := callHandler(ServeSharePublic, req)
	assertStatus(t, w, http.StatusNotFound)
	assert.NotContains(t, w.Body.String(), "TOP_SECRET")
}

// TestSharePublic_ContentRejectsDirectory_404 — directories have no content and
// the share API deliberately exposes no listing endpoint, so a directory target
// must 404 rather than return a FileContent with empty content.
func TestSharePublic_ContentRejectsDirectory_404(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/m.md", "hi")
	token := createShareViaAPI(t, env, absPath)

	dir := filepath.Join(env.ProjectDir, "docs")
	req := newRequest(t, http.MethodGet,
		"/api/share/"+token+"/content?path="+dir, nil)
	w := callHandler(ServeSharePublic, req)
	assertStatus(t, w, http.StatusNotFound)
}

// TestSharePublic_ContentMissing_404 — a link to a file that does not exist is a
// 404, not an empty 200, so the SPA can tell "gone" from "empty".
func TestSharePublic_ContentMissing_404(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/m.md", "hi")
	token := createShareViaAPI(t, env, absPath)

	req := newRequest(t, http.MethodGet,
		"/api/share/"+token+"/content?path="+filepath.Join(env.ProjectDir, "docs", "nope.md"), nil)
	w := callHandler(ServeSharePublic, req)
	assertStatus(t, w, http.StatusNotFound)
}

// TestSharePublic_ContentWithoutPathServesSharedFile — the endpoint degrades to
// the shared file when ?path= is absent, so the SPA can use one URL shape.
func TestSharePublic_ContentWithoutPathServesSharedFile(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/self.md", "# Self")
	token := createShareViaAPI(t, env, absPath)

	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/content", nil)
	w := callHandler(ServeSharePublic, req)
	assertOK(t, w)

	var fc FileContent
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
	assert.Equal(t, "# Self", fc.Content)
}

// TestSharePublic_DownloadReferencedFile — ?path= also applies to /download so a
// referenced file can be downloaded under its own name.
func TestSharePublic_DownloadReferencedFile(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/m.md", "hi")
	target := filepath.Join(env.ProjectDir, "docs", "data.csv")
	createTestFile(t, env.ProjectDir, "docs/data.csv", "a,b\n1,2")
	token := createShareViaAPI(t, env, absPath)

	req := newRequest(t, http.MethodGet,
		"/api/share/"+token+"/download?path="+target, nil)
	w := callHandler(ServeSharePublic, req)
	assertOK(t, w)
	assert.Equal(t, "a,b\n1,2", w.Body.String())
	assert.Contains(t, w.Header().Get("Content-Disposition"), "data.csv")
}

// TestSharePublic_DownloadReferencedFileOutsideScope_404 — the download variant
// honors the same boundary as the content endpoint.
func TestSharePublic_DownloadReferencedFileOutsideScope_404(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/m.md", "hi")
	token := createShareViaAPI(t, env, absPath)

	secret := filepath.Join(env.WatchDir, "outside.txt")
	require.NoError(t, os.WriteFile(secret, []byte("TOP_SECRET"), 0o600))

	req := newRequest(t, http.MethodGet,
		"/api/share/"+token+"/download?path="+secret, nil)
	w := callHandler(ServeSharePublic, req)
	assertStatus(t, w, http.StatusNotFound)
	assert.NotContains(t, w.Body.String(), "TOP_SECRET")
}

func TestSharePublic_NoAuthNeeded(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "pub.md", "public")
	token := createShareViaAPI(t, env, absPath)

	// Bypass Auth middleware entirely — direct call without cookie must work.
	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/file", nil)
	w := callHandler(ServeSharePublic, req)
	assertOK(t, w)
}

func TestSharePage_ServesShareHtml(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	_ = env

	// In test env there is no built share.html; handler falls back to
	// web/share.html which may not exist in test CWD — both outcomes must not
	// panic, and non-GET methods are rejected with 405.
	req := newRequest(t, http.MethodPost, "/share/abc", nil)
	w := callHandler(ServeSharePage, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

// ─── Route registration (ServeMux precedence) ────────────────────────────────

func TestShareRoutes_Precedence(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "prec.md", "prec")
	token := createShareViaAPI(t, env, absPath)

	mux := http.NewServeMux()
	RegisterRoutes(mux)

	// Admin route /api/share (no trailing slash) is matched by Auth-wrapped handler.
	req := newRequest(t, http.MethodGet, "/api/share?path="+absPath, nil)
	withProjectCookie(req, env.ProjectDir)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Public data route /api/share/{token}/file works unauthenticated.
	req = newRequest(t, http.MethodGet, "/api/share/"+token+"/file", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestShareManage_RouteDoesNotShadowShareIn ensures /api/share (admin) does not
// swallow /api/share-in/recent.
func TestShareManage_RouteDoesNotShadowShareIn(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	mux := http.NewServeMux()
	RegisterRoutes(mux)

	// /api/share-in/recent has its own handler (ShareInRecent). A GET should
	// reach it (method GET → 405/400 depending on handler) rather than 404 from
	// falling through. We just assert it is NOT routed to ServeShareManage's
	// method-switch (which would 405 too) — the real guarantee is that the
	// ServeMux does not conflict; any response other than a mux 404 is fine.
	req := newRequest(t, http.MethodGet, "/api/share-in/recent", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assert.NotEqual(t, http.StatusNotFound, rec.Code, "share-in route must not 404")
}

// ─── List endpoints ──────────────────────────────────────────────────────────

func TestShareList_EmptyReturnsArray(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/share/list", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeShareList, req)
	assertOK(t, w)

	var resp struct {
		Shares []shareListItem `json:"shares"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.Shares, "empty list must encode as [] not null")
	assert.Empty(t, resp.Shares)
}

func TestShareList_ListsAllSharesWithDisplayPath(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// File inside the project → display path should be project-relative.
	inProject := createShareTestFile(t, env, "docs/inside.md", "x")
	createShareViaAPI(t, env, inProject)

	// File outside the project but under a root → display path stays absolute.
	outsideDir := filepath.Join(env.WatchDir, "other")
	require.NoError(t, os.MkdirAll(outsideDir, 0o755))
	outside := filepath.Join(outsideDir, "outside.md")
	createTestFile(t, outsideDir, "outside.md", "y")
	createShareViaAPI(t, env, outside)

	req := newRequest(t, http.MethodGet, "/api/share/list", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeShareList, req)
	assertOK(t, w)

	var resp struct {
		Shares []shareListItem `json:"shares"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Shares, 2)

	byPath := map[string]shareListItem{}
	for _, s := range resp.Shares {
		byPath[s.Path] = s
	}
	// Project file relativized.
	assert.Contains(t, byPath, "docs/inside.md")
	assert.True(t, byPath["docs/inside.md"].Exists)
	assert.Equal(t, "inside.md", byPath["docs/inside.md"].Name)
	assert.NotEmpty(t, byPath["docs/inside.md"].Token)
	assert.NotEmpty(t, byPath["docs/inside.md"].CreatedAt)
	// Out-of-project file stays absolute.
	assert.Contains(t, byPath, outside)
	assert.True(t, byPath[outside].Exists)
}

func TestShareList_MarksDeletedFileAsNotExists(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/doomed.md", "x")
	createShareViaAPI(t, env, absPath)
	require.NoError(t, os.Remove(absPath))

	req := newRequest(t, http.MethodGet, "/api/share/list", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeShareList, req)
	assertOK(t, w)

	var resp struct {
		Shares []shareListItem `json:"shares"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Shares, 1)
	assert.Equal(t, "docs/doomed.md", resp.Shares[0].Path)
	assert.False(t, resp.Shares[0].Exists)
}

func TestShareList_RevokeByToken(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Share a file then delete it — stale share must still be revocable by token.
	absPath := createShareTestFile(t, env, "docs/stale.md", "x")
	token := createShareViaAPI(t, env, absPath)
	require.NoError(t, os.Remove(absPath))

	req := newRequest(t, http.MethodDelete, "/api/share/list", map[string]string{"token": token})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeShareList, req)
	assertOK(t, w)

	_, _, _, ok, err := service.GetFileShareByToken(token)
	require.NoError(t, err)
	assert.False(t, ok, "share must be revoked")
}

func TestShareList_RevokeByTokenMissingToken_400(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/share/list", map[string]string{})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeShareList, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestShareList_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/share/list", map[string]string{})
	w := callHandler(ServeShareList, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

// TestShareList_RoutePrecedence ensures /api/share/list is NOT swallowed by the
// public /api/share/{token}/... handler (ServeSharePublic).
func TestShareList_RoutePrecedence(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "prec2.md", "x")
	createShareViaAPI(t, env, absPath)

	mux := http.NewServeMux()
	RegisterRoutes(mux)

	// A request to the literal list path must reach the authed list handler.
	req := newRequest(t, http.MethodGet, "/api/share/list", nil)
	withProjectCookie(req, env.ProjectDir)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	// ... and be a JSON list (not a public "file content" response).
	assert.Contains(t, rec.Body.String(), `"shares"`)
}

// TestShareList_DeleteAll revokes every share via DELETE with {"all":true}.
func TestShareList_DeleteAll(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	abs1 := createShareTestFile(t, env, "docs/one.md", "x")
	createShareViaAPI(t, env, abs1)
	abs2 := createShareTestFile(t, env, "docs/two.md", "y")
	createShareViaAPI(t, env, abs2)

	// Sanity: two shares exist.
	list := newRequest(t, http.MethodGet, "/api/share/list", nil)
	withProjectCookie(list, env.ProjectDir)
	w := callHandler(ServeShareList, list)
	assertOK(t, w)
	var resp struct {
		Shares []shareListItem `json:"shares"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Shares, 2)

	// One-click clear.
	clearReq := newRequest(t, http.MethodDelete, "/api/share/list", map[string]any{"all": true})
	withProjectCookie(clearReq, env.ProjectDir)
	w = callHandler(ServeShareList, clearReq)
	assertOK(t, w)

	after := newRequest(t, http.MethodGet, "/api/share/list", nil)
	withProjectCookie(after, env.ProjectDir)
	w = callHandler(ServeShareList, after)
	assertOK(t, w)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.Shares)
}

// ─── share root resolution ───────────────────────────────────────────────────

// TestShareCreate_RootConfinesToProject verifies a share of a file inside the
// active project is bounded by the PROJECT root, not the file's own directory.
// That is what keeps `../images/x.png` references working (the share SPA
// rewrites every local media ref to ?path=), while still refusing anything
// outside the project.
func TestShareCreate_RootConfinesToProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Share docs/m.md; a sibling directory one level up must stay reachable.
	absPath := createShareTestFile(t, env, "docs/m.md", "hi")
	token := createShareViaAPI(t, env, absPath)

	sibling := filepath.Join(env.ProjectDir, "images", "pic.png")
	createTestFile(t, env.ProjectDir, "images/pic.png", "PICDATA")

	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/local?path="+sibling, nil)
	w := callHandler(ServeSharePublic, req)
	assertOK(t, w)
	assert.Equal(t, "PICDATA", w.Body.String())

	// ...but a file outside the project must not be.
	outside := filepath.Join(env.WatchDir, "outside.txt")
	require.NoError(t, os.WriteFile(outside, []byte("NOPE"), 0o600))

	req = newRequest(t, http.MethodGet, "/api/share/"+token+"/local?path="+outside, nil)
	w = callHandler(ServeSharePublic, req)
	assert.NotEqual(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "NOPE")
}

// TestShareCreate_OutsideProjectFallsBackToFileDir verifies that sharing a file
// outside the active project narrows the boundary to that file's own directory
// (fail closed) instead of inheriting the project root.
func TestShareCreate_OutsideProjectFallsBackToFileDir(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	outsideDir := filepath.Join(env.WatchDir, "elsewhere")
	require.NoError(t, os.MkdirAll(outsideDir, 0o755))
	shared := filepath.Join(outsideDir, "doc.md")
	require.NoError(t, os.WriteFile(shared, []byte("hi"), 0o644))
	createTestFile(t, outsideDir, "sibling.txt", "SIBLING")

	token := createShareViaAPI(t, env, shared)

	// Sibling in the same directory is fine.
	req := newRequest(t, http.MethodGet,
		"/api/share/"+token+"/local?path="+filepath.Join(outsideDir, "sibling.txt"), nil)
	w := callHandler(ServeSharePublic, req)
	assertOK(t, w)
	assert.Equal(t, "SIBLING", w.Body.String())

	// A file inside the project must NOT become reachable through this share.
	inProject := filepath.Join(env.ProjectDir, "secret.txt")
	createTestFile(t, env.ProjectDir, "secret.txt", "PROJECTSECRET")

	req = newRequest(t, http.MethodGet, "/api/share/"+token+"/local?path="+inProject, nil)
	w = callHandler(ServeSharePublic, req)
	assert.NotEqual(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "PROJECTSECRET")
}

// TestShareLocal_LegacyRowWithoutRootFailsClosed verifies rows written before
// the root column existed fall back to the shared file's directory rather than
// reopening the traversal hole.
func TestShareLocal_LegacyRowWithoutRootFailsClosed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/m.md", "hi")
	token := createShareViaAPI(t, env, absPath)

	// Blank the root to simulate a pre-migration row.
	_, err := store.WriteExec("UPDATE file_shares SET root = '' WHERE token = ?", token)
	require.NoError(t, err)

	// Same-directory media still works.
	createTestFile(t, env.ProjectDir, "docs/pic.png", "DOCPIC")
	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/local?path="+filepath.Join(env.ProjectDir, "docs", "pic.png"), nil)
	w := callHandler(ServeSharePublic, req)
	assertOK(t, w)
	assert.Equal(t, "DOCPIC", w.Body.String())

	// Anything above that directory is refused, even under RootPaths=["/"].
	model.RootPaths = []string{"/"}
	outside := filepath.Join(env.ProjectDir, "secret.txt")
	createTestFile(t, env.ProjectDir, "secret.txt", "PROJECTSECRET")

	req = newRequest(t, http.MethodGet, "/api/share/"+token+"/local?path="+outside, nil)
	w = callHandler(ServeSharePublic, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.NotContains(t, w.Body.String(), "PROJECTSECRET")
}

// TestResolveShareRoot_NoCookieNeverWidensToHome is the regression guard for a
// boundary-widening defect: resolveShareRoot must NOT fall back to
// service.GetDefaultProject() when the project cookie is absent. That function's
// own fallback chain ends at the home directory and then at RootPaths[0]
// (== "/" on Unix), and production projects live UNDER the home directory — so
// isPathUnderBase would accept it and confine the share to $HOME, exposing
// ~/.ssh, config files, and everything else to any token holder.
func TestResolveShareRoot_NoCookieNeverWidensToHome(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	require.NotEmpty(t, home)

	// Make GetDefaultProject's first two steps unusable so that, if it were
	// still consulted, it would reach the home-directory fallback.
	_, err = store.WriteExec("DELETE FROM recent_projects")
	require.NoError(t, err)

	// A project under $HOME — the normal production layout.
	projectDir := filepath.Join(home, "clawbench-share-root-test")
	docDir := filepath.Join(projectDir, "docs")
	require.NoError(t, os.MkdirAll(docDir, 0o755))
	t.Cleanup(func() { _ = os.RemoveAll(projectDir) })

	sharedFile := filepath.Join(docDir, "m.md")
	require.NoError(t, os.WriteFile(sharedFile, []byte("hi"), 0o644))

	// No project cookie on the request.
	req := newRequest(t, http.MethodPost, "/api/share", nil)

	got := resolveShareRoot(req, sharedFile)

	assert.NotEqual(t, home, got,
		"share must not be confined to the home directory (would expose ~/.ssh)")
	assert.NotEqual(t, "/", got,
		"share must not be confined to the filesystem root")
	assert.Equal(t, docDir, got,
		"with no project cookie the boundary must narrow to the file's own directory")

	_ = env
}

// TestResolveShareRoot_ProjectCookieConfinesToProject covers the normal path:
// a cookie pointing at a project that contains the file yields the project root,
// which is what keeps `../images/x.png` references working.
func TestResolveShareRoot_ProjectCookieConfinesToProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sharedFile := createShareTestFile(t, env, "docs/m.md", "hi")

	req := newRequest(t, http.MethodPost, "/api/share", nil)
	withProjectCookie(req, env.ProjectDir)

	got := resolveShareRoot(req, sharedFile)
	assert.Equal(t, env.ProjectDir, got,
		"a file inside the active project must be bounded by the project root")
}

// TestResolveShareRoot_FileOutsideProjectNarrowsToFileDir covers the fail-closed
// path: a cookie for a project that does NOT contain the file must not grant the
// project root.
func TestResolveShareRoot_FileOutsideProjectNarrowsToFileDir(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	outsideDir := filepath.Join(env.WatchDir, "elsewhere")
	require.NoError(t, os.MkdirAll(outsideDir, 0o755))
	sharedFile := filepath.Join(outsideDir, "doc.md")
	require.NoError(t, os.WriteFile(sharedFile, []byte("hi"), 0o644))

	req := newRequest(t, http.MethodPost, "/api/share", nil)
	withProjectCookie(req, env.ProjectDir)

	got := resolveShareRoot(req, sharedFile)
	assert.Equal(t, outsideDir, got,
		"a file outside the active project must be bounded by its own directory")
}

// ─── Project scoping ─────────────────────────────────────────────────────────

// createShareInProject shares a file while presenting `projectPath` as the
// active project, so the row is attributed to that project rather than to
// env.ProjectDir.
func createShareInProject(t *testing.T, absPath, projectPath string) string {
	t.Helper()
	req := newRequest(t, http.MethodPost, "/api/share", map[string]string{"path": absPath})
	withProjectCookie(req, projectPath)
	w := callHandler(ServeShareManage, req)
	assertOK(t, w)

	var resp shareResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Token)
	return resp.Token
}

// listSharesForProject reads the shared-files drawer contents as seen from
// projectPath.
func listSharesForProject(t *testing.T, projectPath string) []shareListItem {
	t.Helper()
	req := newRequest(t, http.MethodGet, "/api/share/list", nil)
	withProjectCookie(req, projectPath)
	w := callHandler(ServeShareList, req)
	assertOK(t, w)

	var resp struct {
		Shares []shareListItem `json:"shares"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp.Shares
}

// TestShareList_IsProjectScoped pins that the shared-files drawer only shows the
// active project's shares. The list used to be global, which disclosed both the
// existence and the absolute path of every other project's shared files.
func TestShareList_IsProjectScoped(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// A second project under the same root, so both are valid cookie values.
	otherProject := filepath.Join(env.WatchDir, "project-b")
	require.NoError(t, os.MkdirAll(otherProject, 0o755))

	mine := createShareTestFile(t, env, "docs/mine.md", "x")
	createShareViaAPI(t, env, mine) // env.ProjectDir

	otherDir := filepath.Join(otherProject, "docs")
	require.NoError(t, os.MkdirAll(otherDir, 0o755))
	createTestFile(t, otherDir, "theirs.md", "y")
	otherFile := filepath.Join(otherDir, "theirs.md")
	createShareInProject(t, otherFile, otherProject)

	// Each project sees exactly its own share.
	mineList := listSharesForProject(t, env.ProjectDir)
	require.Len(t, mineList, 1, "project A must see only its own share")
	assert.Equal(t, "docs/mine.md", mineList[0].Path)

	theirsList := listSharesForProject(t, otherProject)
	require.Len(t, theirsList, 1, "project B must see only its own share")
	assert.NotEqual(t, mineList[0].Token, theirsList[0].Token)
}

// TestShareList_NoProjectCookieDoesNotLeak pins the fail-closed direction: with
// no project cookie there is no project to scope to, so the response must not
// fall back to listing everything.
func TestShareList_NoProjectCookieDoesNotLeak(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/secret.md", "x")
	createShareViaAPI(t, env, absPath)

	// No cookie at all.
	req := newRequest(t, http.MethodGet, "/api/share/list", nil)
	w := callHandler(ServeShareList, req)
	assertOK(t, w)

	var resp struct {
		Shares []shareListItem `json:"shares"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.Shares, "an unscoped request must not disclose project shares")
}

// TestShareList_DeleteAllIsProjectScoped pins that the one-click clear only
// revokes the active project's links. It used to run an unqualified
// "DELETE FROM file_shares", silently breaking every other project's public URLs.
func TestShareList_DeleteAllIsProjectScoped(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	otherProject := filepath.Join(env.WatchDir, "project-b")
	require.NoError(t, os.MkdirAll(otherProject, 0o755))

	mine := createShareTestFile(t, env, "docs/mine.md", "x")
	myToken := createShareViaAPI(t, env, mine)

	otherDir := filepath.Join(otherProject, "docs")
	require.NoError(t, os.MkdirAll(otherDir, 0o755))
	createTestFile(t, otherDir, "theirs.md", "y")
	otherToken := createShareInProject(t, filepath.Join(otherDir, "theirs.md"), otherProject)

	// Clear from project A.
	clearReq := newRequest(t, http.MethodDelete, "/api/share/list", map[string]any{"all": true})
	withProjectCookie(clearReq, env.ProjectDir)
	w := callHandler(ServeShareList, clearReq)
	assertOK(t, w)

	// A's link is gone; B's must still work.
	_, _, _, ok, err := service.GetFileShareByToken(myToken)
	require.NoError(t, err)
	assert.False(t, ok, "the active project's share must be revoked")

	_, _, _, ok, err = service.GetFileShareByToken(otherToken)
	require.NoError(t, err)
	assert.True(t, ok, "another project's share must survive the clear")
}

// TestShareList_RevokeByTokenRejectsForeignProject pins the ownership check: a
// token belonging to another project must not be revocable from this one. The
// response is the same 404 an unknown token gets, so neither case discloses
// whether the other exists.
func TestShareList_RevokeByTokenRejectsForeignProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	otherProject := filepath.Join(env.WatchDir, "project-b")
	require.NoError(t, os.MkdirAll(otherProject, 0o755))

	otherDir := filepath.Join(otherProject, "docs")
	require.NoError(t, os.MkdirAll(otherDir, 0o755))
	createTestFile(t, otherDir, "theirs.md", "y")
	otherToken := createShareInProject(t, filepath.Join(otherDir, "theirs.md"), otherProject)

	// Try to revoke it from project A.
	req := newRequest(t, http.MethodDelete, "/api/share/list", map[string]string{"token": otherToken})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeShareList, req)
	assertStatus(t, w, http.StatusNotFound)

	// The share must be untouched.
	_, _, _, ok, err := service.GetFileShareByToken(otherToken)
	require.NoError(t, err)
	assert.True(t, ok, "a foreign project's share must not be revoked")
}

// TestShareList_RevokeUnknownTokenIsNotFound pins that an unknown token gets the
// same 404 as a foreign one (no existence disclosure), and — unlike the
// pre-scoping behavior — is no longer silently "revoked" with a 200.
func TestShareList_RevokeUnknownTokenIsNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/share/list",
		map[string]string{"token": "deadbeefdeadbeefdeadbeefdeadbeef"})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeShareList, req)
	assertStatus(t, w, http.StatusNotFound)
}

// TestShareList_RevokeByToken_OwnershipLookupError pins the first failure
// branch of the revoke: when the ownership lookup itself errors (read handle
// down), the handler reports 500 rather than silently revoking or 404ing.
func TestShareList_RevokeByToken_OwnershipLookupError(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// A read handle that is already closed makes GetFileShareProjectByToken fail.
	closed, err := service.InitInMemoryDB()
	require.NoError(t, err)
	require.NoError(t, closed.Close())
	cleanup := store.SetDBForTest(store.UnsafeDBForTest(), closed)
	defer cleanup()

	req := newRequest(t, http.MethodDelete, "/api/share/list",
		map[string]string{"token": "anytoken"})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeShareList, req)
	assertStatus(t, w, http.StatusInternalServerError)
}

// TestShareList_RevokeByToken_DeleteError pins the second failure branch: the
// ownership check passes but the DELETE fails (write handle down), which must
// surface as 500 rather than a false 200.
func TestShareList_RevokeByToken_DeleteError(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	token := createShareViaAPI(t, env, createShareTestFile(t, env, "docs/del.md", "x"))

	// Keep a healthy read handle (so the ownership lookup succeeds) but break
	// the write handle so DeleteFileShareByToken fails.
	closed, err := service.InitInMemoryDB()
	require.NoError(t, err)
	require.NoError(t, closed.Close())
	cleanup := store.SetDBForTest(closed, store.UnsafeDBForTest())
	defer cleanup()

	req := newRequest(t, http.MethodDelete, "/api/share/list", map[string]string{"token": token})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeShareList, req)
	assertStatus(t, w, http.StatusInternalServerError)
}

// TestShareList_DeleteAll_Error pins the one-click clear failure branch: a
// write error must surface as 500, not a silent success that leaves the links
// live.
func TestShareList_DeleteAll_Error(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createShareViaAPI(t, env, createShareTestFile(t, env, "docs/a.md", "x"))

	closed, err := service.InitInMemoryDB()
	require.NoError(t, err)
	require.NoError(t, closed.Close())
	cleanup := store.SetDBForTest(closed, store.UnsafeDBForTest())
	defer cleanup()

	clearReq := newRequest(t, http.MethodDelete, "/api/share/list", map[string]any{"all": true})
	withProjectCookie(clearReq, env.ProjectDir)
	w := callHandler(ServeShareList, clearReq)
	assertStatus(t, w, http.StatusInternalServerError)
}

// The share page renders author-supplied content, which routinely references
// third-party assets (an agent writing `![](https://cdn.example/a.png)` is
// enough). Under the browser default, the Referer of those subresource requests
// carries the full share URL — so merely OPENING a shared conversation would hand
// the capability token to every third-party host it embeds. The page must opt out
// explicitly, and `no-referrer` is the only value that also covers same-site
// navigations (the default `strict-origin-when-cross-origin` still sends the full
// URL there).
func TestSharePage_SetsNoReferrerPolicy(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/share/0123456789abcdef0123456789abcdef", nil)
	w := callHandler(ServeSharePage, req)

	assert.Equal(t, "no-referrer", w.Header().Get("Referrer-Policy"))
}

// HEAD must carry the same header: a HEAD response is the first thing a client
// (or an intermediary) sees, and it is served by the same handler.
func TestSharePage_SetsNoReferrerPolicyOnHead(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodHead, "/share/0123456789abcdef0123456789abcdef", nil)
	w := callHandler(ServeSharePage, req)

	assert.Equal(t, "no-referrer", w.Header().Get("Referrer-Policy"))
}

// ─── /doc/ (rendered HTML document + its assets) ─────────────────────────────

// shareDocTestURL builds the /doc/ URL a share SPA would request.
func shareDocTestURL(token, dir, rel string) string {
	u := "/api/share/" + token + "/doc/" + encodeShareDocDir(dir)
	if rel != "" {
		u += "/" + rel
	}
	return u
}

func TestSharePublic_DocServesDocumentWithRealBasePath(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "web/landing/index.html",
		`<!doctype html><html><head><link rel="stylesheet" href="css/landing.css"></head><body>hi</body></html>`)
	absPath := createShareTestFile(t, env, "web/landing/index.html", "")
	token := createShareViaAPI(t, env, absPath)

	dir := filepath.Join(env.ProjectDir, "web", "landing")
	req := newRequest(t, http.MethodGet, shareDocTestURL(token, dir, "index.html"), nil)
	w := callHandler(ServeSharePublic, req)
	assertOK(t, w)

	// The document must be served AS HTML — this is the endpoint's whole point.
	// Without it the browser downloads the file instead of rendering it.
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	// ...and the sandbox CSP is what makes that safe (see serveShareDocFile).
	assert.Contains(t, w.Header().Get("Content-Security-Policy"), "sandbox")
	assert.NotContains(t, w.Header().Get("Content-Security-Policy"), "allow-same-origin")
	assert.Equal(t, "no-referrer", w.Header().Get("Referrer-Policy"))
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
}

func TestSharePublic_DocServesSiblingAssetsWithWebMimeTypes(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "web/landing/css/landing.css", "body{color:red}")
	createTestFile(t, env.ProjectDir, "web/landing/js/landing.js", "console.log(1)")
	createTestFile(t, env.ProjectDir, "web/landing/img/logo.webp", "WEBP")
	createTestFile(t, env.ProjectDir, "web/landing/fonts/x.woff2", "FONT")
	absPath := createShareTestFile(t, env, "web/landing/index.html", "<html></html>")
	token := createShareViaAPI(t, env, absPath)
	dir := filepath.Join(env.ProjectDir, "web", "landing")

	cases := map[string]string{
		"css/landing.css": "text/css",
		"js/landing.js":   "text/javascript",
		"img/logo.webp":   "image/webp",
		"fonts/x.woff2":   "font/woff2",
	}
	for rel, wantMime := range cases {
		req := newRequest(t, http.MethodGet, shareDocTestURL(token, dir, rel), nil)
		w := callHandler(ServeSharePublic, req)
		assertOK(t, w)
		// A stylesheet served as octet-stream is silently dropped by the
		// browser — that is the "unstyled HTML" defect this guards.
		assert.Equal(t, wantMime, w.Header().Get("Content-Type"), "mime for %s", rel)
		// Every response carries the sandbox, not just the document: an SVG or
		// a script reached through the same prefix must be inert too.
		assert.Contains(t, w.Header().Get("Content-Security-Policy"), "sandbox", "csp for %s", rel)
	}
}

func TestSharePublic_DocRejectsOutOfScopeDirectoryKey(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "docs/a.md", "hi")
	absPath := createShareTestFile(t, env, "docs/a.md", "hi")
	token := createShareViaAPI(t, env, absPath)

	// The directory key is caller-supplied and opaque, so it must be treated as
	// a hint only: encoding "/" must not turn the endpoint into a filesystem
	// reader for anyone holding the token.
	for _, dir := range []string{"/", "/etc", filepath.Dir(env.ProjectDir)} {
		req := newRequest(t, http.MethodGet, shareDocTestURL(token, dir, "passwd"), nil)
		w := callHandler(ServeSharePublic, req)
		assert.Equal(t, http.StatusNotFound, w.Code, "dir %s must be out of scope", dir)
	}
}

func TestSharePublic_DocRejectsTraversalAndSymlinkEscape(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "secret.txt", "SECRET")
	absPath := createShareTestFile(t, env, "docs/a.md", "hi")
	token := createShareViaAPI(t, env, absPath)
	dir := filepath.Join(env.ProjectDir, "docs")

	// Lexical traversal out of the document's own directory.
	for _, rel := range []string{"../secret.txt", "../../secret.txt", "..%2Fsecret.txt"} {
		req := newRequest(t, http.MethodGet, shareDocTestURL(token, dir, rel), nil)
		w := callHandler(ServeSharePublic, req)
		assert.NotContains(t, w.Body.String(), "SECRET", "rel %s leaked content", rel)
	}

	// A symlink inside the project pointing OUTSIDE it must not be followed.
	outside := filepath.Join(t.TempDir(), "outside.txt")
	require.NoError(t, os.WriteFile(outside, []byte("OUTSIDE"), 0o600))
	link := filepath.Join(env.ProjectDir, "docs", "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	req := newRequest(t, http.MethodGet, shareDocTestURL(token, dir, "link.txt"), nil)
	w := callHandler(ServeSharePublic, req)
	assert.NotContains(t, w.Body.String(), "OUTSIDE")
}

func TestSharePublic_DocUnknownToken_404(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	_ = env

	req := newRequest(t, http.MethodGet, shareDocTestURL("nope", "/tmp", "x.html"), nil)
	w := callHandler(ServeSharePublic, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestShareDocDirKey_RoundTrips(t *testing.T) {
	// The share SPA encodes this key in the browser; the two implementations
	// must produce identical bytes or every relative asset 404s.
	dirs := []string{"/", "/tmp", "/a b/c", "/项目/子目录"}
	if runtime.GOOS == "windows" {
		// A Windows document's directory is what the browser will encode; the
		// key must round-trip there too (IsAbs is platform-dependent, so this
		// case cannot be asserted on Unix).
		dirs = append(dirs, `C:\Users\x`)
	}
	for _, dir := range dirs {
		key := encodeShareDocDir(dir)
		got, ok := decodeShareDocDir(key)
		require.True(t, ok, "dir %q", dir)
		assert.Equal(t, filepath.Clean(dir), got)
		// The key is a URL path segment: no padding, no '+' or '/'.
		assert.NotContains(t, key, "=")
		assert.NotContains(t, key, "+")
		assert.NotContains(t, key, "/")
	}
}

// ─── /doc/ edge cases: malformed keys and unknown asset types ───────────────

func TestSharePublic_DocBareAndMalformedPaths_404(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/a.md", "hi")
	token := createShareViaAPI(t, env, absPath)

	// Each of these reaches a distinct guard in serveShareDoc. They are all
	// hand-typed URLs (the SPA always supplies a complete key + relative path),
	// so the only correct answer is 404 — never a directory listing, never the
	// shared file itself by accident.
	cases := map[string]string{
		"bare /doc (no directory key)":       "/api/share/" + token + "/doc",
		"trailing slash only":                "/api/share/" + token + "/doc/",
		"key without a relative path":        "/api/share/" + token + "/doc/" + encodeShareDocDir(env.ProjectDir),
		"key without a relative path, slash": "/api/share/" + token + "/doc/" + encodeShareDocDir(env.ProjectDir) + "/",
		"undecodable key":                    "/api/share/" + token + "/doc/!!!notbase64!!!/x.html",
		"key decoding to a relative path":    "/api/share/" + token + "/doc/" + encodeShareDocDir("relative/dir") + "/x.html",
		"key decoding to empty":              "/api/share/" + token + "/doc//x.html",
	}
	for name, url := range cases {
		req := newRequest(t, http.MethodGet, url, nil)
		w := callHandler(ServeSharePublic, req)
		assert.Equal(t, http.StatusNotFound, w.Code, "%s", name)
	}
}

func TestSharePublic_DocRejectsDotAndAbsoluteRelativePaths_404(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "docs/index.html", "<html></html>")
	absPath := createShareTestFile(t, env, "docs/a.md", "hi")
	token := createShareViaAPI(t, env, absPath)
	dir := filepath.Join(env.ProjectDir, "docs")
	key := encodeShareDocDir(dir)

	// "." and ".." name the directory itself; an absolute path would let the
	// relative segment escape the key's directory entirely. All are refused
	// before ValidatePath sees them.
	for _, rel := range []string{".", "..", "/etc/passwd"} {
		req := newRequest(t, http.MethodGet, "/api/share/"+token+"/doc/"+key+"/"+rel, nil)
		w := callHandler(ServeSharePublic, req)
		assert.Equal(t, http.StatusNotFound, w.Code, "rel %q must be refused", rel)
	}
}

func TestSharePublic_DocMissingFile_404(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/a.md", "hi")
	token := createShareViaAPI(t, env, absPath)
	dir := filepath.Join(env.ProjectDir, "docs")

	// Referenced asset that does not exist (or a directory named like one).
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "assets"), 0o755))
	for _, rel := range []string{"missing.css", "assets"} {
		req := newRequest(t, http.MethodGet, shareDocTestURL(token, dir, rel), nil)
		w := callHandler(ServeSharePublic, req)
		assert.Equal(t, http.StatusNotFound, w.Code, "rel %q", rel)
	}
}

func TestSharePublic_DocUnknownExtensionFallsBackToSniffedType(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// `.wasm` is not in the curated web-asset table but the platform knows it;
	// the response must say so rather than octet-stream, otherwise a shared
	// WebAssembly module is served as a download.
	createTestFile(t, env.ProjectDir, "web/landing/mod.wasm", "asm")
	absPath := createShareTestFile(t, env, "web/landing/index.html", "<html></html>")
	token := createShareViaAPI(t, env, absPath)
	dir := filepath.Join(env.ProjectDir, "web", "landing")

	req := newRequest(t, http.MethodGet, shareDocTestURL(token, dir, "mod.wasm"), nil)
	w := callHandler(ServeSharePublic, req)
	assertOK(t, w)
	assert.Equal(t, "application/wasm", w.Header().Get("Content-Type"))
	// The sandbox must still be applied: an unusual extension is not a reason
	// to drop the opaque-origin guarantee.
	assert.Contains(t, w.Header().Get("Content-Security-Policy"), "sandbox")
}

func TestSharePublic_DocUnknownExtensionWithoutPlatformType_IsOctetStream(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "web/landing/blob.zzzunknownzzz", "opaque")
	absPath := createShareTestFile(t, env, "web/landing/index.html", "<html></html>")
	token := createShareViaAPI(t, env, absPath)
	dir := filepath.Join(env.ProjectDir, "web", "landing")

	req := newRequest(t, http.MethodGet, shareDocTestURL(token, dir, "blob.zzzunknownzzz"), nil)
	w := callHandler(ServeSharePublic, req)
	assertOK(t, w)
	assert.Equal(t, "application/octet-stream", w.Header().Get("Content-Type"))
}

func TestShareScopeRoot_EmptyRootFallsBackToFileDir(t *testing.T) {
	// Rows predating the share_root column have an empty root. The scope must
	// then be the shared file's own directory (fail closed) — never the
	// filesystem, and never "whatever the caller's key says".
	assert.Equal(t, "/a/b", shareScopeRoot("/a/b/c.txt", ""))
	assert.Equal(t, "/proj", shareScopeRoot("/a/b/c.txt", "/proj"))
}
