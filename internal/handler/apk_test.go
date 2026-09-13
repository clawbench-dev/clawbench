package handler

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"clawbench/internal/frontend"
)

// apkFS builds an in-memory FS containing the APK at the embed path, so tests
// never depend on the real go:embed payload (which is absent from dev builds
// unless build.sh --android ran).
func apkFS(content string) fs.FS {
	return fstest.MapFS{
		apkEmbedPath: &fstest.MapFile{Data: []byte(content)},
	}
}

func TestServeAPK_NotFound(t *testing.T) {
	// Empty FS → no APK at the embed path → 404.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/apk", http.NoBody)

	serveAPK(fstest.MapFS{}, w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestServeAPK_ServesFromFS(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/apk", http.NoBody)

	serveAPK(apkFS("embedded-apk-content"), w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/vnd.android.package-archive" {
		t.Errorf("expected apk content type, got %s", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); cd != `attachment; filename="clawbench-android.apk"` {
		t.Errorf("unexpected Content-Disposition: %s", cd)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "public, max-age=3600" {
		t.Errorf("unexpected Cache-Control: %s", cc)
	}
	if w.Body.String() != "embedded-apk-content" {
		t.Errorf("body mismatch: got %q", w.Body.String())
	}
}

// TestServeAPK_IgnoresDiskPublic is the regression test for the bug where a
// public/ directory in the CWD shadowed the embedded APK: ServeAPK used
// frontend.GetFS() (disk-first) so /api/apk 404'd whenever the server was
// started from a directory containing public/. ServeAPK must now always serve
// the build-time embedded copy, regardless of the CWD.
func TestServeAPK_IgnoresDiskPublic(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Reproduce the failing environment: CWD contains public/assets/ with a
	// DIFFERENT (stale) APK. The served bytes must never come from disk.
	tmpDir := t.TempDir()
	pubAssets := filepath.Join(tmpDir, "public", "assets")
	if err := os.MkdirAll(pubAssets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pubAssets, apkFilename), []byte("stale-disk-apk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/apk", http.NoBody)

	ServeAPK(w, req)

	// The stale disk bytes must never be served. Dev builds (no --android) have
	// no embedded APK, so 404 is the correct outcome there; release builds must
	// return the embedded copy.
	if w.Body.String() == "stale-disk-apk" {
		t.Fatal("served the stale APK from disk; embedded copy must take precedence")
	}
	_, embedErr := fs.ReadFile(frontend.EmbeddedFS(), apkEmbedPath)
	if embedErr != nil {
		if w.Code != http.StatusNotFound {
			t.Fatalf("no embedded APK → want 404, got %d", w.Code)
		}
		return
	}
	if w.Code != http.StatusOK {
		t.Fatalf("embedded APK present → want 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/vnd.android.package-archive" {
		t.Errorf("expected apk content type, got %s", ct)
	}
}

// TestServeAPK_InjectedFSWinsOverDisk proves the injection point itself is
// disk-agnostic: even with a public/assets/ APK in the CWD, serveAPK serves
// the FS it was given.
func TestServeAPK_InjectedFSWinsOverDisk(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	tmpDir := t.TempDir()
	pubAssets := filepath.Join(tmpDir, "public", "assets")
	if err := os.MkdirAll(pubAssets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pubAssets, apkFilename), []byte("stale-disk-apk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/apk", http.NoBody)

	serveAPK(apkFS("injected-apk-content"), w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "injected-apk-content" {
		t.Errorf("injected FS must win over disk, got %q", w.Body.String())
	}
}

func TestServeAPK_EmbedOnly(t *testing.T) {
	// Sanity check that the real embedded FS is wired up and reachable through
	// ServeAPK when it contains an APK. Skips in dev builds without --android.
	if _, err := fs.ReadFile(frontend.EmbeddedFS(), apkEmbedPath); err != nil {
		t.Skip("no APK in embedded FS (dev build), skipping")
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/apk", http.NoBody)

	ServeAPK(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/vnd.android.package-archive" {
		t.Errorf("expected apk content type, got %s", ct)
	}
}

func TestReaderSeeker(t *testing.T) {
	data := []byte("hello world")
	rs := NewReaderSeeker(data)

	buf := make([]byte, 5)
	n, err := rs.Read(buf)
	if n != 5 || string(buf) != "hello" {
		t.Errorf("read: got %d %q, err %v", n, buf, err)
	}

	off, err := rs.Seek(6, io.SeekStart)
	if off != 6 || err != nil {
		t.Errorf("seek: got %d, err %v", off, err)
	}

	buf2 := make([]byte, 5)
	n, err = rs.Read(buf2)
	if n != 5 || string(buf2) != "world" {
		t.Errorf("read after seek: got %d %q, err %v", n, buf2, err)
	}

	n, err = rs.Read(buf2)
	if n != 0 || !errors.Is(err, io.EOF) {
		t.Errorf("read past end: got %d, err %v", n, err)
	}
}

func TestReaderSeeker_SeekEnd(t *testing.T) {
	data := []byte("hello")
	rs := NewReaderSeeker(data)

	off, err := rs.Seek(-3, io.SeekEnd)
	if off != 2 || err != nil {
		t.Errorf("seek end: got %d, err %v", off, err)
	}

	buf := make([]byte, 3)
	n, err := rs.Read(buf)
	if n != 3 || string(buf) != "llo" {
		t.Errorf("read after seek end: got %d %q, err %v", n, buf, err)
	}
}

func TestReaderSeeker_SeekCurrent(t *testing.T) {
	rs := NewReaderSeeker([]byte("hello world"))

	if _, err := rs.Seek(6, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	// Relative seek from the current offset.
	off, err := rs.Seek(1, io.SeekCurrent)
	if off != 7 || err != nil {
		t.Errorf("seek current: got %d, err %v", off, err)
	}
}

func TestReaderSeeker_SeekInvalid(t *testing.T) {
	rs := NewReaderSeeker([]byte("hello"))

	// Unsupported whence → fs.ErrInvalid.
	if _, err := rs.Seek(0, 99); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("invalid whence: expected fs.ErrInvalid, got %v", err)
	}
	// Negative resulting offset → fs.ErrInvalid.
	if _, err := rs.Seek(-1, io.SeekStart); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("negative offset: expected fs.ErrInvalid, got %v", err)
	}
}
