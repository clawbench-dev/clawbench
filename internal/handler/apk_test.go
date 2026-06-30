package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestServeAPK_FileNotFound(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/apk", http.NoBody)
	w := httptest.NewRecorder()

	ServeAPK(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestServeAPK_Success(t *testing.T) {
	dir := filepath.Dir(apkPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(apkPath, []byte("fake-apk-content"), 0o644); err != nil {
		t.Fatalf("write apk: %v", err)
	}
	defer os.Remove(apkPath)

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
	if w.Body.String() != "fake-apk-content" {
		t.Errorf("body mismatch: got %q", w.Body.String())
	}
}

func TestServeAPK_IsDir(t *testing.T) {
	dir := filepath.Dir(apkPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	os.MkdirAll(apkPath, 0o755)
	defer os.RemoveAll(apkPath)

	req := httptest.NewRequest(http.MethodGet, "/api/apk", http.NoBody)
	w := httptest.NewRecorder()

	ServeAPK(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for directory, got %d", w.Code)
	}
}

func TestServeAPK_OpenPermissionDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	dir := filepath.Dir(apkPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(apkPath, []byte("fake"), 0o000); err != nil {
		t.Fatalf("write apk: %v", err)
	}
	defer os.Remove(apkPath)
	defer os.Chmod(apkPath, 0o644)

	req := httptest.NewRequest(http.MethodGet, "/api/apk", http.NoBody)
	w := httptest.NewRecorder()

	ServeAPK(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for permission denied, got %d", w.Code)
	}
}
