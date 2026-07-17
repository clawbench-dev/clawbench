package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- ServeProjectDialog: non-GET method ---

func TestServeProjectDialog_NonGET_Returns405(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/dialog/project", http.NoBody)
	w := httptest.NewRecorder()
	ServeProjectDialog(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeProjectDialog_DELETE_Returns405(t *testing.T) {
	req := httptest.NewRequest(http.MethodDelete, "/dialog/project", http.NoBody)
	w := httptest.NewRecorder()
	ServeProjectDialog(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeProjectDialog_GET_DoesNotCrash(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/dialog/project", http.NoBody)
	w := httptest.NewRecorder()
	ServeProjectDialog(w, req)
	// May be 200 or 404 depending on whether web/project-dialog.html exists
	assert.Contains(t, []int{http.StatusOK, http.StatusNotFound}, w.Code)
}

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
	publicDir := filepath.Join(tmpDir, "public")
	if err := os.MkdirAll(publicDir, 0o755); err != nil {
		t.Fatalf("failed to create public dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(publicDir, "index.html"), []byte("<html>ok</html>"), 0o644); err != nil {
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
	publicDir := filepath.Join(tmpDir, "public")
	if err := os.MkdirAll(publicDir, 0o755); err != nil {
		t.Fatalf("failed to create public dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(publicDir, "index.html"), []byte("<html>ok</html>"), 0o644); err != nil {
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
		{"short-x.js", false},   // hash too short
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
	publicDir := filepath.Join(tmpDir, "public")
	if err := os.MkdirAll(publicDir, 0o755); err != nil {
		t.Fatalf("failed to create public dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(publicDir, "index.html"), []byte("<html>ok</html>"), 0o644); err != nil {
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
	publicDir := filepath.Join(tmpDir, "public")
	if err := os.MkdirAll(publicDir, 0o755); err != nil {
		t.Fatalf("failed to create public dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(publicDir, "pdf-D-oSvAqu.js"), []byte("var x=1;"), 0o644); err != nil {
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
	publicDir := filepath.Join(tmpDir, "public")
	if err := os.MkdirAll(publicDir, 0o755); err != nil {
		t.Fatalf("failed to create public dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(publicDir, "robots.txt"), []byte("User-agent: *"), 0o644); err != nil {
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
