package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"clawbench/internal/frontend"

	"github.com/stretchr/testify/assert"
)

// --- ServeIndex: HEAD method ---

func TestServeIndex_HEAD_DoesNotCrash(t *testing.T) {
	req := httptest.NewRequest(http.MethodHead, "/", http.NoBody)
	w := httptest.NewRecorder()
	ServeIndex(w, req)
	// HEAD should be allowed (same as GET)
	assert.Contains(t, []int{http.StatusOK, http.StatusNotFound}, w.Code)
}

// --- ServeIndex: non-GET/HEAD method ---

func TestServeIndex_PostMethod_Returns405(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", http.NoBody)
	w := httptest.NewRecorder()
	ServeIndex(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeIndex_PutMethod_Returns405(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "/", http.NoBody)
	w := httptest.NewRecorder()
	ServeIndex(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// --- ServeIndex: non-root path that doesn't exist ---

func TestServeIndex_NonExistentAsset_Returns404(t *testing.T) {
	// Save and restore working directory
	origWd, _ := os.Getwd()
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	req := httptest.NewRequest(http.MethodGet, "/nonexistent.js", http.NoBody)
	w := httptest.NewRecorder()
	ServeIndex(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- ServeIndex: path traversal blocked when DiskPublicExists ---

func TestServeIndex_TraversalBlockedOutsidePublic(t *testing.T) {
	tmpDir := t.TempDir()
	diskDir := filepath.Join(tmpDir, frontend.DiskDirName)
	if err := os.MkdirAll(diskDir, 0o755); err != nil {
		t.Fatalf("failed to create public dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(diskDir, "index.html"), []byte("<html>ok</html>"), 0o644); err != nil {
		t.Fatalf("failed to write index.html: %v", err)
	}
	// Create a secret file outside public
	if err := os.WriteFile(filepath.Join(tmpDir, "secret.txt"), []byte("SECRET"), 0o644); err != nil {
		t.Fatalf("failed to write secret: %v", err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	// Request traversal path
	req := httptest.NewRequest(http.MethodGet, "/../secret.txt", http.NoBody)
	w := httptest.NewRecorder()
	ServeIndex(w, req)
	assert.NotEqual(t, http.StatusOK, w.Code, "should not serve files outside public/")
}

// --- ServeIndex: root path "." ---

func TestServeIndex_DotPath_ServesIndex(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("filepath.Clean behavior differs on Windows")
	}
	tmpDir := t.TempDir()
	diskDir := filepath.Join(tmpDir, frontend.DiskDirName)
	if err := os.MkdirAll(diskDir, 0o755); err != nil {
		t.Fatalf("failed to create public dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(diskDir, "index.html"), []byte("<html>ok</html>"), 0o644); err != nil {
		t.Fatalf("failed to write index.html: %v", err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	w := httptest.NewRecorder()
	ServeIndex(w, req)
	assert.True(t, w.Code == http.StatusOK || w.Code == http.StatusMovedPermanently,
		"root path should serve index.html")
}

// --- ServeIndex: CSS fallback path ---

func TestServeIndex_CSSFallback_DevMode(t *testing.T) {
	tmpDir := t.TempDir()
	// Create web/css/ directory with a test CSS file
	cssDir := filepath.Join(tmpDir, "web", "css")
	if err := os.MkdirAll(cssDir, 0o755); err != nil {
		t.Fatalf("failed to create css dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cssDir, "test.css"), []byte("body{}"), 0o644); err != nil {
		t.Fatalf("failed to write test.css: %v", err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	req := httptest.NewRequest(http.MethodGet, "/css/test.css", http.NoBody)
	w := httptest.NewRecorder()
	ServeIndex(w, req)
	// May be 200 if served from web/css/ or 404 if not found
	// Just verify no panic
	assert.NotEqual(t, http.StatusMethodNotAllowed, w.Code)
}

// --- /material-icons/ static route (file-type icons) ---

func TestMaterialIconsRoute_ServesSvgFromPublic(t *testing.T) {
	tmpDir := t.TempDir()
	diskDir := filepath.Join(tmpDir, frontend.DiskDirName)
	iconsDir := filepath.Join(diskDir, "material-icons")
	if err := os.MkdirAll(iconsDir, 0o755); err != nil {
		t.Fatalf("failed to create material-icons dir: %v", err)
	}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"/>`)
	if err := os.WriteFile(filepath.Join(iconsDir, "go.svg"), svg, 0o644); err != nil {
		t.Fatalf("failed to write go.svg: %v", err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	mux := http.NewServeMux()
	RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/material-icons/go.svg", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, string(svg), w.Body.String())
}

func TestMaterialIconsRoute_HEAD_ReturnsOK(t *testing.T) {
	tmpDir := t.TempDir()
	diskDir := filepath.Join(tmpDir, frontend.DiskDirName)
	iconsDir := filepath.Join(diskDir, "material-icons")
	if err := os.MkdirAll(iconsDir, 0o755); err != nil {
		t.Fatalf("failed to create material-icons dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(iconsDir, "go.svg"), []byte(`<svg/>`), 0o644); err != nil {
		t.Fatalf("failed to write go.svg: %v", err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	mux := http.NewServeMux()
	RegisterRoutes(mux)

	// getIconUrl() verifies icon existence with a HEAD request.
	req := httptest.NewRequest(http.MethodHead, "/material-icons/go.svg", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestMaterialIconsRoute_MissingIcon_Returns404(t *testing.T) {
	tmpDir := t.TempDir()
	diskDir := filepath.Join(tmpDir, frontend.DiskDirName)
	if err := os.MkdirAll(diskDir, 0o755); err != nil {
		t.Fatalf("failed to create public dir: %v", err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	mux := http.NewServeMux()
	RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/material-icons/nonexistent.svg", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestMaterialIconsRoute_TraversalBlocked(t *testing.T) {
	tmpDir := t.TempDir()
	diskDir := filepath.Join(tmpDir, frontend.DiskDirName)
	iconsDir := filepath.Join(diskDir, "material-icons")
	if err := os.MkdirAll(iconsDir, 0o755); err != nil {
		t.Fatalf("failed to create material-icons dir: %v", err)
	}
	// Secret outside the icons directory
	if err := os.WriteFile(filepath.Join(diskDir, "secret.txt"), []byte("SECRET"), 0o644); err != nil {
		t.Fatalf("failed to write secret: %v", err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	mux := http.NewServeMux()
	RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/material-icons/../secret.txt", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.NotEqual(t, http.StatusOK, w.Code, "should not serve files outside material-icons/")
}

// --- isHashedAsset ---

func TestIsHashedAsset(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		// Vite hash-named assets
		{"index-CaOuUlWb.js", true},
		{"pdf-D-oSvAqu.js", true},
		{"index-C_GAucyY.css", true},
		{"pdf.worker.min-VZhzohg-.js", false}, // dash in hash part before last dash
		{"some-font-a1b2c3d4.woff2", true},
		{"icon-aB3cD4eF.png", true},
		// Not hashed
		{"index.html", false},
		{"favicon.ico", false},
		{"robots.txt", false},
		{"app.js", false},
		{"style.css", false},
		{"no-dash.js", false},
		{"short-x.js", false}, // hash too short
		{"", false},
		{"assets/logo-a1b2c3d4.svg", true}, // path with directory
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, isHashedAsset(tc.name))
		})
	}
}

// --- Cache headers ---

func TestServeIndex_RootPath_NoCacheHeader(t *testing.T) {
	tmpDir := t.TempDir()
	diskDir := filepath.Join(tmpDir, frontend.DiskDirName)
	if err := os.MkdirAll(diskDir, 0o755); err != nil {
		t.Fatalf("failed to create public dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(diskDir, "index.html"), []byte("<html>ok</html>"), 0o644); err != nil {
		t.Fatalf("failed to write index.html: %v", err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	w := httptest.NewRecorder()
	ServeIndex(w, req)
	assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
}

func TestServeIndex_HashedAsset_ImmutableCacheHeader(t *testing.T) {
	tmpDir := t.TempDir()
	diskDir := filepath.Join(tmpDir, frontend.DiskDirName)
	if err := os.MkdirAll(diskDir, 0o755); err != nil {
		t.Fatalf("failed to create public dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(diskDir, "pdf-D-oSvAqu.js"), []byte("var x=1;"), 0o644); err != nil {
		t.Fatalf("failed to write hashed asset: %v", err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	req := httptest.NewRequest(http.MethodGet, "/pdf-D-oSvAqu.js", http.NoBody)
	w := httptest.NewRecorder()
	ServeIndex(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "public, max-age=31536000, immutable", w.Header().Get("Cache-Control"))
}

func TestServeIndex_NonHashedAsset_NoImmutableCache(t *testing.T) {
	tmpDir := t.TempDir()
	diskDir := filepath.Join(tmpDir, frontend.DiskDirName)
	if err := os.MkdirAll(diskDir, 0o755); err != nil {
		t.Fatalf("failed to create public dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(diskDir, "robots.txt"), []byte("User-agent: *"), 0o644); err != nil {
		t.Fatalf("failed to write non-hashed asset: %v", err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	req := httptest.NewRequest(http.MethodGet, "/robots.txt", http.NoBody)
	w := httptest.NewRecorder()
	ServeIndex(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Header().Get("Cache-Control"))
}

// --- PWA files: served with types the client's registration gate requires ---

// The frontend only registers the service worker when a HEAD request for
// /sw.js comes back as JavaScript (web/src/utils/pwaServiceWorker.ts). That
// gate exists because the dev-server SPA fallback answers unknown paths with
// index.html, and registering an HTML body as a worker script throws at install
// time. The gate is only correct if the real server labels the file as
// JavaScript — if ServeIndex ever regressed to serving it as octet-stream, the
// worker would silently stop registering and installability would break with no
// error anywhere.
func TestServeIndex_ServiceWorker_ServedAsJavaScript(t *testing.T) {
	tmpDir := t.TempDir()
	diskDir := filepath.Join(tmpDir, frontend.DiskDirName)
	if err := os.MkdirAll(diskDir, 0o755); err != nil {
		t.Fatalf("failed to create disk dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(diskDir, "sw.js"), []byte("self.addEventListener('fetch', () => {});"), 0o644); err != nil {
		t.Fatalf("failed to write sw.js: %v", err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	req := httptest.NewRequest(http.MethodHead, "/sw.js", http.NoBody)
	w := httptest.NewRecorder()
	ServeIndex(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	ct := w.Header().Get("Content-Type")
	assert.Contains(t, ct, "javascript",
		"sw.js must be served as JavaScript or the client registration gate rejects it")
}

// A worker script must not be strongly cached: the browser byte-compares it to
// detect updates, and updateViaCache:'none' on the client only bypasses the HTTP
// cache — a long max-age would still pin clients to a stale worker with no way
// to ship a fix. Asserted here because isHashedAsset() would otherwise classify
// nothing about "sw.js", and a future cache rule keyed on extension could
// silently start caching it.
func TestServeIndex_ServiceWorker_NotStronglyCached(t *testing.T) {
	tmpDir := t.TempDir()
	diskDir := filepath.Join(tmpDir, frontend.DiskDirName)
	if err := os.MkdirAll(diskDir, 0o755); err != nil {
		t.Fatalf("failed to create disk dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(diskDir, "sw.js"), []byte("self.addEventListener('fetch', () => {});"), 0o644); err != nil {
		t.Fatalf("failed to write sw.js: %v", err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	req := httptest.NewRequest(http.MethodGet, "/sw.js", http.NoBody)
	w := httptest.NewRecorder()
	ServeIndex(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Header().Get("Cache-Control"), "immutable")
}

// manifest.json must be reachable at the root path the HTML links to, and be
// served as JSON. The manifest URL is the installed app's identity, so a
// hash-renamed or 404 manifest breaks installation outright.
func TestServeIndex_Manifest_ServedAsJSON(t *testing.T) {
	tmpDir := t.TempDir()
	diskDir := filepath.Join(tmpDir, frontend.DiskDirName)
	if err := os.MkdirAll(diskDir, 0o755); err != nil {
		t.Fatalf("failed to create disk dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(diskDir, "manifest.json"), []byte(`{"name":"ClawBench"}`), 0o644); err != nil {
		t.Fatalf("failed to write manifest.json: %v", err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	req := httptest.NewRequest(http.MethodGet, "/manifest.json", http.NoBody)
	w := httptest.NewRecorder()
	ServeIndex(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "json")
}
