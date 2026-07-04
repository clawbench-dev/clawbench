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

	"clawbench/internal/frontend"
)

func TestServeAPK_NotFound(t *testing.T) {
	// In dev environment, embed likely has no APK → 404
	req := httptest.NewRequest(http.MethodGet, "/api/apk", http.NoBody)
	w := httptest.NewRecorder()

	ServeAPK(w, req)

	// May be 200 (if embed has APK) or 404 (dev build without APK)
	if w.Code != http.StatusNotFound && w.Code != http.StatusOK {
		t.Errorf("expected 404 or 200, got %d", w.Code)
	}
}

func TestServeAPK_FromEmbed(t *testing.T) {
	// Provide APK via public/assets/ so GetFS() (DirFS) serves it
	pubAssets := filepath.Join("public", "assets")
	if err := os.MkdirAll(pubAssets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pubAssets, apkFilename), []byte("embedded-apk-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll("public/assets")

	req := httptest.NewRequest(http.MethodGet, "/api/apk", http.NoBody)
	w := httptest.NewRecorder()

	ServeAPK(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/vnd.android.package-archive" {
		t.Errorf("expected apk content type, got %s", ct)
	}
	cd := w.Header().Get("Content-Disposition")
	if cd != `attachment; filename="clawbench-android.apk"` {
		t.Errorf("unexpected Content-Disposition: %s", cd)
	}
	cc := w.Header().Get("Cache-Control")
	if cc != "public, max-age=3600" {
		t.Errorf("unexpected Cache-Control: %s", cc)
	}
	if w.Body.String() != "embedded-apk-content" {
		t.Errorf("body mismatch: got %q", w.Body.String())
	}
}

func TestServeAPK_EmbedOnly(t *testing.T) {
	// Remove public/ dir to test pure embed path
	if fi, err := os.Stat("public"); err == nil && fi.IsDir() {
		os.Rename("public", "public_test_bak")
		defer os.Rename("public_test_bak", "public")
	}

	fsys := frontend.GetFS()
	_, embedErr := fs.ReadFile(fsys, apkEmbedPath)
	if embedErr != nil {
		t.Skip("no APK in embedded FS, skipping embed-only test")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/apk", http.NoBody)
	w := httptest.NewRecorder()

	ServeAPK(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/vnd.android.package-archive" {
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
