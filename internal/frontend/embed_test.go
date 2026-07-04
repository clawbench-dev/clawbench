package frontend

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
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
	req, _ := http.NewRequest("GET", "/nonexistent.js", http.NoBody)

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
	req, _ := http.NewRequest("GET", "/test.js", http.NoBody)

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
	req, _ := http.NewRequest("GET", "/subdir", http.NoBody)

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

func TestGetFS_DiskPublic(t *testing.T) {
	// Save and restore CWD so we don't break other tests.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	tmpDir := t.TempDir()
	publicDir := filepath.Join(tmpDir, "public")
	if err := os.MkdirAll(publicDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Place a sentinel file to verify DirFS is used.
	if err := os.WriteFile(filepath.Join(publicDir, "sentinel.txt"), []byte("disk"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	fsys := GetFS()
	if fsys == nil {
		t.Fatal("GetFS() returned nil")
	}

	// Since public/ exists, GetFS should return os.DirFS("public"),
	// which can read the sentinel file we placed.
	data, err := fs.ReadFile(fsys, "sentinel.txt")
	if err != nil {
		t.Fatalf("failed to read sentinel.txt from disk FS: %v", err)
	}
	if string(data) != "disk" {
		t.Errorf("got %q, want %q", data, "disk")
	}
}

func TestServeFileFromFS_ReadSeekerPath(t *testing.T) {
	// embed.FS files implement io.ReadSeeker, so ServeFileFromFS should
	// take the fast path (ServeContent with seeker directly).
	// Use distFS which is an embed.FS subset.
	fsys := GetFS()

	// Check if index.html exists in the embed FS; skip if no build.
	f, err := fsys.Open("index.html")
	if err != nil {
		t.Skip("no index.html in embedded FS (no frontend build)")
	}
	_ = f.Close()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/index.html", http.NoBody)

	ServeFileFromFS(w, req, fsys, "index.html")

	if w.Code != 200 {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if w.Body.Len() == 0 {
		t.Error("expected non-empty body")
	}
}

func TestServeFileFromFS_FallbackBufferPath(t *testing.T) {
	// Use a custom fs.FS that returns a file NOT implementing io.ReadSeeker
	// to exercise the fallback buffer path (lines 41-49 in serve.go).
	fsys := &nonSeekerFS{data: []byte("hello from fallback")}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test.txt", http.NoBody)

	ServeFileFromFS(w, req, fsys, "test.txt")

	if w.Code != 200 {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if w.Body.String() != "hello from fallback" {
		t.Errorf("got body %q, want %q", w.Body.String(), "hello from fallback")
	}
}

func TestServeFileFromFS_StatError(t *testing.T) {
	// FS that can Open but Stat returns error.
	fsys := &statErrorFS{}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/bad.txt", http.NoBody)

	ServeFileFromFS(w, req, fsys, "bad.txt")

	if w.Code != 404 {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestServeFileFromFS_ReadFileError(t *testing.T) {
	// FS where Open succeeds and returns non-ReadSeeker, but ReadFile fails.
	fsys := &readFileErrorFS{}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/bad.txt", http.NoBody)

	ServeFileFromFS(w, req, fsys, "bad.txt")

	if w.Code != 500 {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

// --- Custom fs.FS implementations for testing edge cases ---

// nonSeekerFS is an fs.FS where opened files do NOT implement io.ReadSeeker,
// forcing ServeFileFromFS into the fallback buffer path.
type nonSeekerFS struct {
	data []byte
}

func (n *nonSeekerFS) Open(name string) (fs.File, error) {
	if name != "test.txt" {
		return nil, &os.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &nonSeekerFile{data: n.data}, nil
}

// nonSeekerFile implements fs.File but NOT io.ReadSeeker.
type nonSeekerFile struct {
	data   []byte
	offset int
}

func (f *nonSeekerFile) Stat() (fs.FileInfo, error) {
	return staticFileInfo{name: "test.txt", size: int64(len(f.data))}, nil
}

func (f *nonSeekerFile) Read(p []byte) (int, error) {
	if f.offset >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.offset:])
	f.offset += n
	return n, nil
}

func (f *nonSeekerFile) Close() error { return nil }

// staticFileInfo is a minimal fs.FileInfo for testing.
type staticFileInfo struct {
	name string
	size int64
}

func (i staticFileInfo) Name() string       { return i.name }
func (i staticFileInfo) Size() int64        { return i.size }
func (i staticFileInfo) Mode() fs.FileMode  { return 0o644 }
func (i staticFileInfo) ModTime() time.Time { return time.Time{} }
func (i staticFileInfo) IsDir() bool        { return false }
func (i staticFileInfo) Sys() any           { return nil }

// statErrorFS opens a file successfully but Stat() on the file returns an error.
type statErrorFS struct{}

func (s *statErrorFS) Open(name string) (fs.File, error) {
	return &statErrorFile{}, nil
}

type statErrorFile struct{}

func (f *statErrorFile) Stat() (fs.FileInfo, error) {
	return nil, os.ErrInvalid
}
func (f *statErrorFile) Read([]byte) (int, error) { return 0, os.ErrClosed }
func (f *statErrorFile) Close() error             { return nil }

// readFileErrorFS opens a file that does NOT implement io.ReadSeeker,
// and fs.ReadFile fails (returns error), exercising the 500 path.
type readFileErrorFS struct{}

func (r *readFileErrorFS) Open(name string) (fs.File, error) {
	return &nonSeekerFile{data: []byte("data")}, nil
}

func (r *readFileErrorFS) ReadFile(name string) ([]byte, error) {
	return nil, os.ErrPermission
}
