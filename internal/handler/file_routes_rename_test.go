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

// TestFileRoutes_OldEndpointsNoLongerMatch is the guard that keeps the Crawlab
// fingerprint from creeping back: if any of these resolve to a file-read
// handler again, the rename has been undone.
func TestFileRoutes_OldEndpointsNoLongerMatch(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux)

	// The exact Crawlab LFI payload, plus the other two old shapes.
	oldTargets := []string{
		"/api/file?path=../../etc/passwd",
		"/api/file/?path=/etc/passwd",
		"/api/file/docs/a.md",
		"/api/file/thumb?path=/tmp/a.png&w=100",
		"/api/local-file/?path=/etc/passwd",
		"/api/local-file/assets/a.png",
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
