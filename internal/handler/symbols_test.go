package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/symbol"
)

func TestServeFileSymbols_Success(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a Go file in the project directory
	goFile := filepath.Join(env.ProjectDir, "main.go")
	content := []byte(`package main

type Server struct {
	Port int
}

func main() {}
`)
	if err := os.WriteFile(goFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/file/symbols?path="+goFile, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	ServeFileSymbols(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var result symbol.SymbolResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if result.Lang != "go" {
		t.Errorf("expected lang=go, got %s", result.Lang)
	}
	if len(result.Symbols) == 0 {
		t.Error("expected at least one symbol")
	}
}

func TestServeFileSymbols_MissingPath(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodGet, "/api/file/symbols", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	ServeFileSymbols(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestServeFileSymbols_FileNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Use a path under the project directory so it passes root validation,
	// but the file doesn't exist
	missingFile := filepath.Join(env.ProjectDir, "nonexistent.go")

	req := httptest.NewRequest(http.MethodGet, "/api/file/symbols?path="+missingFile, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	ServeFileSymbols(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestServeFileSymbols_NonTextFile(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	imgFile := filepath.Join(env.ProjectDir, "image.png")
	if err := os.WriteFile(imgFile, []byte("fake png"), 0644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/file/symbols?path="+imgFile, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	ServeFileSymbols(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var result symbol.SymbolResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(result.Symbols) != 0 {
		t.Errorf("expected no symbols for non-text file, got %d", len(result.Symbols))
	}
}

func TestServeFileSymbols_LargeFile(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	largeFile := filepath.Join(env.ProjectDir, "large.go")
	largeContent := make([]byte, 1<<20+100)
	for i := range largeContent {
		largeContent[i] = 'x'
	}
	if err := os.WriteFile(largeFile, largeContent, 0644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/file/symbols?path="+largeFile, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	ServeFileSymbols(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var result symbol.SymbolResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(result.Symbols) != 0 {
		t.Errorf("expected no symbols for large file, got %d", len(result.Symbols))
	}
}

func TestServeFileSymbols_Directory(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	subDir := filepath.Join(env.ProjectDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/file/symbols?path="+subDir, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	ServeFileSymbols(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for directory, got %d", w.Code)
	}
}

func TestServeFileSymbols_WrongMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/file/symbols?path=test.go", nil)
	w := httptest.NewRecorder()

	ServeFileSymbols(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestServeFileSymbols_Python(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	pyFile := filepath.Join(env.ProjectDir, "app.py")
	content := []byte(`class MyApp:
    def run(self):
        pass

def helper():
    pass
`)
	if err := os.WriteFile(pyFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/file/symbols?path="+pyFile, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()

	ServeFileSymbols(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var result symbol.SymbolResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if result.Lang != "python" {
		t.Errorf("expected lang=python, got %s", result.Lang)
	}
}
