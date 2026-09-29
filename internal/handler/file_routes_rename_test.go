package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The three file-read endpoints were renamed off the `/api/file?path=` shape so
// that their traffic no longer matches the Crawlab LFI fingerprint
// (`GET /api/file?path=../../etc/passwd`), which a customer's IDS flagged as an
// attack against normal ClawBench usage.
//
// Mapping:
//
//	/api/file?path=<abs>          -> /api/fs/file?target=<abs>
//	/api/file/<rel>               -> /api/fs/file/<rel>
//	/api/file/thumb?path=…&w=…    -> /api/fs/thumb?target=…&w=…
//	/api/local-file/?path=<abs>   -> /api/fs/raw/?target=<abs>
//	/api/local-file/<rel>         -> /api/fs/raw/<rel>
//
// These tests pin the rename on both sides: the new patterns must resolve, and
// the old ones must NOT — otherwise the fingerprint silently comes back.
//
// EXCEPTION — /api/local-file/ is deliberately served again as a deprecated
// backward-compatibility alias (legacyLocalFilePrefix in file.go). The rename was
// a hard cutover, but that URL is built in NATIVE client code (Android
// downloadFile(), desktop resolveLocalFileUrl) which a frontend update cannot
// reach and which cannot self-update. Those clients would 404 forever on every
// file-manager download. The alias is scoped to exactly that prefix; the Crawlab
// fingerprint itself (`/api/file?path=`) stays dead, and /api/fs/raw/ keeps the
// renamed `?target=` shape. See TestLocalFile_LegacyAlias* below.

// resolvePattern returns the http.ServeMux pattern a request path resolves to.
func resolvePattern(t *testing.T, mux *http.ServeMux, target string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, http.NoBody)
	_, pattern := mux.Handler(req)
	return pattern
}

func TestFileRoutes_RenamedEndpointsResolve(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux)

	cases := []struct {
		target      string
		wantPattern string
	}{
		// Query-param (absolute path) forms.
		{"/api/fs/file?target=/etc/hosts", "/api/fs/file/"},
		{"/api/fs/raw/?target=/etc/hosts", "/api/fs/raw/"},
		{"/api/fs/thumb?target=/tmp/a.png&w=100", "/api/fs/thumb"},
		// URL-path (project-relative) forms.
		{"/api/fs/file/docs/a.md", "/api/fs/file/"},
		{"/api/fs/raw/assets/a.png", "/api/fs/raw/"},
	}
	for _, tc := range cases {
		if got := resolvePattern(t, mux, tc.target); got != tc.wantPattern {
			t.Errorf("%s resolved to %q, want %q", tc.target, got, tc.wantPattern)
		}
	}
}

// TestFileRoutes_LegacyLocalFileAliasResolves pins the deprecated alias: the old
// /api/local-file/ prefix must route to the same handler as /api/fs/raw/.
// If this stops resolving, pre-rename Android/desktop clients 404 on download
// again (and cannot self-recover — their embedded APK predates the fix).
func TestFileRoutes_LegacyLocalFileAliasResolves(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux)

	cases := []struct {
		target      string
		wantPattern string
	}{
		// Both shapes the legacy native clients build.
		{"/api/local-file/?path=/etc/hosts", legacyLocalFilePrefix},
		{"/api/local-file/assets/a.png", legacyLocalFilePrefix},
		{"/api/local-file/?download=1&path=/etc/hosts", legacyLocalFilePrefix},
	}
	for _, tc := range cases {
		if got := resolvePattern(t, mux, tc.target); got != tc.wantPattern {
			t.Errorf("%s resolved to %q, want %q (legacy alias must stay mounted)", tc.target, got, tc.wantPattern)
		}
	}
}

// TestFileRoutes_OldEndpointsNoLongerMatch is the guard that keeps the Crawlab
// fingerprint from creeping back: if any of these resolve to a file-read
// handler again, the rename has been undone.
//
// /api/local-file/ is intentionally NOT in this list — it is a deprecated alias
// (see the file header). Everything matching the actual Crawlab shape stays dead.
func TestFileRoutes_OldEndpointsNoLongerMatch(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux)

	// The exact Crawlab LFI payload, plus the other two old shapes.
	oldTargets := []string{
		"/api/file?path=../../etc/passwd",
		"/api/file/?path=/etc/passwd",
		"/api/file/docs/a.md",
		"/api/file/thumb?path=/tmp/a.png&w=100",
	}
	// The catch-all "/" (ServeIndex) is what an unrouted path falls through to.
	// Anything else means an old file-read route is still mounted.
	for _, target := range oldTargets {
		if got := resolvePattern(t, mux, target); got != "/" {
			t.Errorf("%s resolved to %q; the old file-read route is still registered", target, got)
		}
	}
}

// TestFileRoutes_OtherFileEndpointsUnchanged guards the other direction: the
// rename must not have taken out sibling `/api/file/*` routes that keep their
// names (they are not file reads and never matched the Crawlab fingerprint).
func TestFileRoutes_OtherFileEndpointsUnchanged(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux)

	unchanged := []struct {
		target      string
		wantPattern string
	}{
		{"/api/file/list-tree?path=src", "/api/file/list-tree"},
		{"/api/file/symbols?path=a.go", "/api/file/symbols"},
		{"/api/file/batch-exists", "/api/file/batch-exists"},
		{"/api/file/batch-base64", "/api/file/batch-base64"},
		{"/api/file/write", "/api/file/write"},
		{"/api/file/rename", "/api/file/rename"},
		{"/api/file/delete", "/api/file/delete"},
		{"/api/file/create", "/api/file/create"},
		{"/api/file/copy", "/api/file/copy"},
		{"/api/file/move", "/api/file/move"},
		{"/api/file/archive", "/api/file/archive"},
		{"/api/file/theme-wallpaper", "/api/file/theme-wallpaper"},
		{"/api/file/content-search", "/api/file/content-search"},
		{"/api/file/watch/ws", "/api/file/watch/ws"},
	}
	for _, tc := range unchanged {
		if got := resolvePattern(t, mux, tc.target); got != tc.wantPattern {
			t.Errorf("%s resolved to %q, want %q (sibling route must not be renamed)", tc.target, got, tc.wantPattern)
		}
	}
}

// TestFileThumb_TargetParamContract pins the query-param rename itself. The
// OpenAPI drift guard only checks paths and auth, not parameter names, so
// nothing else would catch a revert to `?path=`.
func TestFileThumb_TargetParamContract(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	createTestFile(t, env.ProjectDir, "img.png", "not really a png")

	// `?target=` reaches the handler (it fails later on decode, not on a
	// missing param — the missing-param path returns a distinct message).
	req := newRequest(t, http.MethodGet, "/api/fs/thumb?target=img.png&w=50", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(FileThumb, req)
	if body := w.Body.String(); strings.Contains(body, "path required") {
		t.Errorf("?target= was not read as the path param: %s", body)
	}

	// `?path=` is no longer the parameter name: the handler sees an empty
	// target and reports "path required".
	oldReq := newRequest(t, http.MethodGet, "/api/fs/thumb?path=img.png&w=50", nil)
	withProjectCookie(oldReq, env.ProjectDir)
	oldW := callHandler(FileThumb, oldReq)
	if body := oldW.Body.String(); !strings.Contains(body, "path required") {
		t.Errorf("?path= should no longer be accepted, got: %s", body)
	}
}

// TestLocalFile_LegacyAliasEndToEnd drives the legacy URL through the REAL mux
// (route registration + auth middleware), not the handler directly. The tests
// above call ServeLocalFile in isolation, which cannot catch a missing or
// mis-prefixed registration — the exact failure that broke the old clients.
func TestLocalFile_LegacyAliasEndToEnd(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	createTestFile(t, env.ProjectDir, "assets/a.png", "png-bytes")

	mux := http.NewServeMux()
	RegisterRoutes(mux)

	// The project-relative shape the old native clients build.
	req := httptest.NewRequest(http.MethodGet, legacyLocalFilePrefix+"assets/a.png", http.NoBody)
	withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("legacy URL through the real mux should serve the file, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "png-bytes") {
		t.Errorf("expected file content, got: %s", w.Body.String())
	}
}

// TestGetFile_TargetParamContract is the same pin for the content endpoint.
func TestGetFile_TargetParamContract(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	createTestFile(t, env.ProjectDir, "a.txt", "hello")

	// New param name serves the file.
	req := newRequest(t, http.MethodGet, "/api/fs/file?target="+env.ProjectDir+"/a.txt", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(GetFile, req)
	if w.Code != http.StatusOK {
		t.Errorf("?target= should serve the file, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "hello") {
		t.Errorf("expected file content, got: %s", w.Body.String())
	}
}

// TestLocalFile_LegacyAliasServesFiles proves the deprecated alias actually
// serves bytes — not merely that a route matches. Routing alone would still 404
// if resolveLocalFilePath refused the legacy prefix or `?path=`.
//
// These are the two exact URL shapes the pre-rename native clients build
// (Android downloadFile(), desktop resolveLocalFileUrl).
func TestLocalFile_LegacyAliasServesFiles(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	createTestFile(t, env.ProjectDir, "assets/a.png", "png-bytes")

	t.Run("URL path form", func(t *testing.T) {
		req := newRequest(t, http.MethodGet, legacyLocalFilePrefix+"assets/a.png", nil)
		withProjectCookie(req, env.ProjectDir)
		w := callHandler(ServeLocalFile, req)
		if w.Code != http.StatusOK {
			t.Fatalf("legacy URL-path form should serve the file, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "png-bytes") {
			t.Errorf("expected file content, got: %s", w.Body.String())
		}
	})

	t.Run("legacy ?path= absolute form", func(t *testing.T) {
		// The pre-rename clients spell the absolute-path param `?path=`, not
		// `?target=`, and append `download=1`.
		req := newRequest(t, http.MethodGet,
			legacyLocalFilePrefix+"?download=1&path="+env.ProjectDir+"/assets/a.png", nil)
		withProjectCookie(req, env.ProjectDir)
		w := callHandler(ServeLocalFile, req)
		if w.Code != http.StatusOK {
			t.Fatalf("legacy ?path= form should serve the file, got %d: %s", w.Code, w.Body.String())
		}
		if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
			t.Errorf("download=1 should force an attachment, got Content-Disposition=%q", cd)
		}
	})
}

// TestLocalFile_CurrentEndpointRejectsLegacyParam is the counterweight: the
// alias must not leak the old `?path=` name back onto the CURRENT endpoint.
// If it did, /api/fs/raw/ would accept the pre-rename parameter shape again and
// the rename's fingerprint-removal would be partially undone.
func TestLocalFile_CurrentEndpointRejectsLegacyParam(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	createTestFile(t, env.ProjectDir, "a.png", "png-bytes")

	// `?path=` on the current endpoint must NOT resolve the file. With no
	// recognized query param the handler falls through to the URL-path form,
	// where the remaining path is "/api/fs/raw/" -> empty -> validation error.
	req := newRequest(t, http.MethodGet, "/api/fs/raw/?path="+env.ProjectDir+"/a.png", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeLocalFile, req)
	if w.Code == http.StatusOK {
		t.Errorf("?path= must not be accepted on /api/fs/raw/ (rename must stay intact), got 200: %s", w.Body.String())
	}

	// The current name still works, so the rejection above is about the param
	// name and not about the endpoint being broken.
	okReq := newRequest(t, http.MethodGet, "/api/fs/raw/?target="+env.ProjectDir+"/a.png", nil)
	withProjectCookie(okReq, env.ProjectDir)
	okW := callHandler(ServeLocalFile, okReq)
	if okW.Code != http.StatusOK {
		t.Errorf("?target= should still serve the file, got %d: %s", okW.Code, okW.Body.String())
	}
}
