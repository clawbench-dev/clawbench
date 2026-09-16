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

// --- ISS-055: Path traversal in ServeIndex ---

func TestServeIndex_PathTraversal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("filepath.Clean and path traversal semantics differ on Windows")
	}
	// Create a temporary public directory with a known file
	tmpDir := t.TempDir()
	diskDir := filepath.Join(tmpDir, frontend.DiskDirName)
	if err := os.MkdirAll(diskDir, 0o755); err != nil {
		t.Fatalf("failed to create public dir: %v", err)
	}
	// Create a secret file outside public that should NOT be accessible
	secretFile := filepath.Join(tmpDir, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("secret data"), 0o644); err != nil {
		t.Fatalf("failed to write secret file: %v", err)
	}
	// Create a legitimate file inside public
	if err := os.WriteFile(filepath.Join(diskDir, "index.html"), []byte("<html>ok</html>"), 0o644); err != nil {
		t.Fatalf("failed to write index.html: %v", err)
	}

	// Save and restore working directory
	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	tests := []struct {
		name       string
		path       string
		wantStatus int // 200, 404, or 403
	}{
		{
			name:       "root path serves index",
			path:       "/",
			wantStatus: http.StatusOK,
		},
		{
			name:       "path traversal with ../ blocked",
			path:       "/../secret.txt",
			wantStatus: http.StatusNotFound, // cleaned path escapes public, rejected
		},
		{
			name:       "path traversal with /.. blocked",
			path:       "/..%2Fsecret.txt",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "legitimate asset path",
			path:       "/index.html",
			wantStatus: http.StatusOK, // http.ServeFile may return 301 redirect which is also acceptable
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, http.NoBody)
			w := httptest.NewRecorder()
			ServeIndex(w, req)
			if tt.wantStatus == http.StatusOK {
				// 200 or 301 redirect are both acceptable for legitimate paths
				assert.True(t, w.Code == http.StatusOK || w.Code == http.StatusMovedPermanently, "expected 200 or 301, got %d for path %s", w.Code, tt.path)
			} else {
				// For traversal paths, should NOT return 200
				assert.NotEqual(t, http.StatusOK, w.Code, "path traversal should not return 200 for path %s", tt.path)
			}
		})
	}
}

func TestServeIndex_PathTraversalDoesNotLeakSecret(t *testing.T) {
	tmpDir := t.TempDir()
	diskDir := filepath.Join(tmpDir, frontend.DiskDirName)
	if err := os.MkdirAll(diskDir, 0o755); err != nil {
		t.Fatalf("failed to create public dir: %v", err)
	}
	// Secret file at root level
	secretFile := filepath.Join(tmpDir, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("SECRET_CONTENT"), 0o644); err != nil {
		t.Fatalf("failed to write secret: %v", err)
	}
	// index.html in public
	if err := os.WriteFile(filepath.Join(diskDir, "index.html"), []byte("OK"), 0o644); err != nil {
		t.Fatalf("failed to write index: %v", err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	req := httptest.NewRequest(http.MethodGet, "/../secret.txt", http.NoBody)
	w := httptest.NewRecorder()
	ServeIndex(w, req)

	// The secret content should NOT be in the response
	assert.NotContains(t, w.Body.String(), "SECRET_CONTENT", "path traversal should not expose secret file")
}
