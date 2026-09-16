package frontend

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGetFS_EmbedFallback(t *testing.T) {
	// In test environment, the disk build dir likely doesn't exist at CWD,
	// so GetFS should return the embedded distFS.
	fsys := GetFS()

	// If the embedded dist/ directory is empty (no frontend build),
	// we can still verify it returns a valid fs.FS.
	if fsys == nil {
		t.Fatal("GetFS() returned nil")
	}

	// Verify it's the embed FS by checking that it's not os.DirFS
	// (os.DirFS(DiskDirName) would fail if the dir doesn't exist)
	if _, err := os.Stat(DiskDirName); err != nil {
		// No disk build dir — must be using embed
		_, err := fs.Stat(fsys, "index.html")
		if err != nil {
			// Empty embed (no build) — expected in test env
			t.Log("GetFS() returns embed FS with no index.html (empty embed, expected in test env)")
		} else {
			t.Log("GetFS() returns embed FS with index.html")
		}
	}
}

func TestDiskDirExists(t *testing.T) {
	result := DiskDirExists()
	// In test environment, the disk build dir typically doesn't exist at CWD
	if result {
		// dir exists — verify it's actually a directory
		fi, err := os.Stat(DiskDirName)
		if err != nil {
			t.Fatalf("DiskDirExists() = true but os.Stat failed: %v", err)
		}
		if !fi.IsDir() {
			t.Fatal("DiskDirExists() = true but the disk dir is not a directory")
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

func TestServeFileFromFS_HTMLIsNoCache(t *testing.T) {
	dir := t.TempDir()
	content := []byte("<html><body>app</body></html>")
	if err := os.WriteFile(filepath.Join(dir, "index.html"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	fsys := os.DirFS(dir)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/index.html", http.NoBody)

	ServeFileFromFS(w, req, fsys, "index.html")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// The entry HTML must not be cached so a rebuild always reaches the client.
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("expected Cache-Control: no-cache, got %q", got)
	}
}

func TestServeFileFromFS_NonHTMLDoesNotForceNoCache(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	fsys := os.DirFS(dir)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test.js", http.NoBody)

	ServeFileFromFS(w, req, fsys, "test.js")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "" {
		t.Errorf("expected no Cache-Control header for non-HTML, got %q", got)
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

func TestGetFS_DiskDir(t *testing.T) {
	// Save and restore CWD so we don't break other tests.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	tmpDir := t.TempDir()
	diskDir := filepath.Join(tmpDir, DiskDirName)
	if err := os.MkdirAll(diskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Place a sentinel file to verify DirFS is used.
	if err := os.WriteFile(filepath.Join(diskDir, "sentinel.txt"), []byte("disk"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	fsys := GetFS()
	if fsys == nil {
		t.Fatal("GetFS() returned nil")
	}

	// Since the disk build dir exists, GetFS should return os.DirFS(DiskDirName),
	// which can read the sentinel file we placed.
	data, err := fs.ReadFile(fsys, "sentinel.txt")
	if err != nil {
		t.Fatalf("failed to read sentinel.txt from disk FS: %v", err)
	}
	if string(data) != "disk" {
		t.Errorf("got %q, want %q", data, "disk")
	}
}

// TestVendorExcalidrawServed verifies that the isolated Excalidraw host is
// reachable through the same frontend FS the backend serves at
// /vendor/excalidraw/index.html. This is the contract that makes the iframe
// editor work: the vendor build output (DiskDirName/vendor/excalidraw/) must be
// embedded alongside the Vue app and served by ServeIndex.
func TestVendorExcalidrawServed(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	tmpDir := t.TempDir()
	diskDir := filepath.Join(tmpDir, DiskDirName, "vendor", "excalidraw")
	if err := os.MkdirAll(diskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(diskDir, "index.html"), []byte(`<div id="root"></div>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	fsys := GetFS()
	data, err := fs.ReadFile(fsys, "vendor/excalidraw/index.html")
	if err != nil {
		t.Fatalf("vendor/excalidraw/index.html not reachable via frontend FS: %v", err)
	}
	if string(data) != `<div id="root"></div>` {
		t.Errorf("unexpected content: %q", data)
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

// --- ModeLabel tests ---

func TestModeLabel_Embedded(t *testing.T) {
	// Save and restore CWD
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Chdir to a temp dir without the disk build dir → ModeLabel should say "embedded"
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	label := ModeLabel()
	assert.Equal(t, "embedded", label)
}

func TestModeLabel_Disk(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	tmpDir := t.TempDir()
	diskDir := filepath.Join(tmpDir, DiskDirName)
	if err := os.MkdirAll(diskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	label := ModeLabel()
	assert.Equal(t, "disk ("+DiskDirName+"/)", label)
}

func TestDiskDirExists_NonexistentDir(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	assert.False(t, DiskDirExists())
}

func TestDiskDirExists_ExistingDir(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	tmpDir := t.TempDir()
	diskDir := filepath.Join(tmpDir, DiskDirName)
	if err := os.MkdirAll(diskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	assert.True(t, DiskDirExists())
}

func TestGetFS_ConsistencyWithModeLabel(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	tmpDir := t.TempDir()
	diskDir := filepath.Join(tmpDir, DiskDirName)
	if err := os.MkdirAll(diskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write a file to verify DirFS is returned
	if err := os.WriteFile(filepath.Join(diskDir, "test.txt"), []byte("disk-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// When the disk build dir exists, ModeLabel says disk and GetFS returns DirFS
	assert.Equal(t, "disk ("+DiskDirName+"/)", ModeLabel())

	fsys := GetFS()
	data, err := fs.ReadFile(fsys, "test.txt")
	if err != nil {
		t.Fatalf("failed to read test.txt from disk FS: %v", err)
	}
	assert.Equal(t, "disk-content", string(data))
}

// TestEmbeddedFS_IgnoresDiskPublic locks the contract that EmbeddedFS() always
// returns the build-time embedded filesystem and never consults the working
// directory. This is what makes build-time artifacts such as the Android APK
// immune to a stray disk build dir in the CWD (which previously shadowed the
// embedded copy and produced a 404 from /api/apk).
func TestEmbeddedFS_IgnoresDiskDir(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// CWD has a disk build dir holding a sentinel that must NOT be reachable.
	tmpDir := t.TempDir()
	diskDir := filepath.Join(tmpDir, DiskDirName)
	if err := os.MkdirAll(diskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(diskDir, "sentinel.txt"), []byte("disk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Sanity check: GetFS() does consult the disk here, so the sentinel is
	// visible through it. This proves the CWD shadowing scenario is real.
	diskData, err := fs.ReadFile(GetFS(), "sentinel.txt")
	if err != nil {
		t.Fatalf("GetFS() should read the disk sentinel: %v", err)
	}
	assert.Equal(t, "disk", string(diskData))

	// EmbeddedFS() must ignore the disk dir entirely — the sentinel is invisible.
	if _, err := fs.ReadFile(EmbeddedFS(), "sentinel.txt"); err == nil {
		t.Fatal("EmbeddedFS() read sentinel.txt from disk; it must never consult the CWD")
	}
}

// TestDiskDirName_DoesNotCollideWithUnrelatedDirs is the regression guard for
// issue #461: the disk build dir must be a name that cannot be produced by
// anything other than this project's build.
//
// The original name "public" was resolved against the CWD, and macOS ships a
// system directory ~/Public. Because the default APFS volume is
// case-insensitive, os.Stat("public") from a home-directory CWD matched it, so
// the server served that empty directory instead of the embedded frontend and
// every page 404'd (the Android app then hung on its splash screen). The
// reproduction the reporter hit is macOS-only; the invariant it violates is not
// — the name must be distinctive enough that a collision is impossible
// anywhere, which is what this test pins on every platform.
func TestDiskDirName_DoesNotCollideWithUnrelatedDirs(t *testing.T) {
	// Names that exist by default or are otherwise plausible in a user's CWD.
	// A collision with any of these reintroduces the shadowing bug.
	forbidden := map[string]string{
		"public":       "macOS ships ~/Public; APFS is case-insensitive, so os.Stat matches it",
		"Public":       "the system directory itself",
		"static":       "common web-server convention",
		"dist":         "generic build-output name",
		"build":        "generic build-output name",
		"web":          "this repo has a web/ source dir",
		"www":          "common web-root name",
		"site":         "common web-root name",
		"html":         "generic",
		"assets":       "this repo has a top-level assets/ dir",
		"node_modules": "would be a catastrophic mixup",
	}

	lower := strings.ToLower(DiskDirName)
	if reason, bad := forbidden[lower]; bad {
		t.Fatalf("DiskDirName = %q collides with an unrelated directory (%s)", DiskDirName, reason)
	}

	// A leading dot keeps it out of casual listings and out of the way of any
	// conventional web root, and makes an accidental name match even less likely.
	if !strings.HasPrefix(DiskDirName, ".") {
		t.Errorf("DiskDirName = %q should start with '.' so it is hidden and clearly project-internal", DiskDirName)
	}

	// It must be a single path element — a nested path would change the
	// CWD-relative resolution this whole mechanism depends on.
	if strings.ContainsAny(DiskDirName, `/\`) {
		t.Errorf("DiskDirName = %q must be a single path element, not a path", DiskDirName)
	}
}
