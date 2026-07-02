package frontend

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestGetFS_EmbedFallback(t *testing.T) {
	// In test environment, public/ likely doesn't exist at CWD,
	// so GetFS should return the embedded distFS.
	fsys := GetFS()

	// If the embedded dist/ directory is empty (no frontend build),
	// we can still verify it returns a valid fs.FS.
	if fsys == nil {
		t.Fatal("GetFS() returned nil")
	}

	// Verify it's the embed FS by checking that it's not os.DirFS
	// (os.DirFS("public") would fail if public/ doesn't exist)
	if _, err := os.Stat("public"); err != nil {
		// No public/ on disk — must be using embed
		_, err := fs.Stat(fsys, "index.html")
		if err != nil {
			// Empty embed (no build) — expected in test env
			t.Log("GetFS() returns embed FS with no index.html (empty embed, expected in test env)")
		} else {
			t.Log("GetFS() returns embed FS with index.html")
		}
	}
}

func TestDiskPublicExists(t *testing.T) {
	result := DiskPublicExists()
	// In test environment, public/ typically doesn't exist at CWD
	if result {
		// public/ exists — verify it's actually a directory
		fi, err := os.Stat("public")
		if err != nil {
			t.Fatalf("DiskPublicExists() = true but os.Stat failed: %v", err)
		}
		if !fi.IsDir() {
			t.Fatal("DiskPublicExists() = true but public/ is not a directory")
		}
	}
	// If false, that's expected in test environment
}

func TestServeFileFromFS_NotFound(t *testing.T) {
	// Create a simple fs.FS with no files
	dir := t.TempDir()
	fsys := os.DirFS(dir)

	w := &responseWriterMock{}
	req, _ := http.NewRequest("GET", "/nonexistent.js", nil)

	ServeFileFromFS(w, req, fsys, "nonexistent.js")

	if w.status != 404 {
		t.Errorf("expected status 404, got %d", w.status)
	}
}

func TestServeFileFromFS_ExistingFile(t *testing.T) {
	// Create a temp dir with a test file
	dir := t.TempDir()
	testContent := []byte("console.log('hello')")
	if err := os.WriteFile(filepath.Join(dir, "test.js"), testContent, 0o644); err != nil {
		t.Fatal(err)
	}

	fsys := os.DirFS(dir)
	w := &responseWriterMock{}
	req, _ := http.NewRequest("GET", "/test.js", nil)

	ServeFileFromFS(w, req, fsys, "test.js")

	if w.status != 200 {
		t.Errorf("expected status 200, got %d", w.status)
	}
}

func TestServeFileFromFS_Directory(t *testing.T) {
	// Serving a directory path should return 404
	dir := t.TempDir()
	subdir := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	fsys := os.DirFS(dir)
	w := &responseWriterMock{}
	req, _ := http.NewRequest("GET", "/subdir", nil)

	ServeFileFromFS(w, req, fsys, "subdir")

	if w.status != 404 {
		t.Errorf("expected status 404 for directory, got %d", w.status)
	}
}

// responseWriterMock is a minimal http.ResponseWriter for testing.
type responseWriterMock struct {
	status int
	header http.Header
	body   []byte
}

func (m *responseWriterMock) Header() http.Header {
	if m.header == nil {
		m.header = make(http.Header)
	}
	return m.header
}

func (m *responseWriterMock) Write(p []byte) (int, error) {
	m.body = append(m.body, p...)
	if m.status == 0 {
		m.status = 200
	}
	return len(p), nil
}

func (m *responseWriterMock) WriteHeader(statusCode int) {
	m.status = statusCode
}
