package handler

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/model"
)

// parseSearchSSEEvents reads SSE events from a dir search response body.
// Returns a map of event type → slice of raw JSON data.
func parseSearchSSEEvents(body string) map[string][]json.RawMessage {
	events := make(map[string][]json.RawMessage)
	scanner := bufio.NewScanner(strings.NewReader(body))
	var currentEvent string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") && currentEvent != "" {
			data := strings.TrimPrefix(line, "data: ")
			events[currentEvent] = append(events[currentEvent], json.RawMessage(data))
			currentEvent = ""
		}
	}
	return events
}

func TestDirSearch_MissingQuery(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/dir/search?path=", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(DirSearch, req)

	// Should reject the missing project
	if w.Code != http.StatusForbidden && w.Code != http.StatusBadRequest {
		t.Errorf("expected 403 or 400, got %d", w.Code)
	}
}

func TestDirSearch_NonRecursive(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create files in top-level and nested dir
	createTestFile(t, env.ProjectDir, "main.go", "package main")
	createTestFile(t, env.ProjectDir, "readme.md", "# hello")
	createTestFile(t, env.ProjectDir, "src/util.go", "package src")

	req := newRequest(t, http.MethodGet, "/api/dir/search?path=&q=main&recursive=false", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(DirSearch, req)

	assertOK(t, w)
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected SSE content type, got %s", ct)
	}

	events := parseSearchSSEEvents(w.Body.String())
	results := events["result"]
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	var r DirSearchResult
	if err := json.Unmarshal(results[0], &r); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if r.Name != "main.go" {
		t.Errorf("expected name main.go, got %s", r.Name)
	}
	if r.Type != "file" {
		t.Errorf("expected type file, got %s", r.Type)
	}

	// Verify done event
	doneEvents := events["done"]
	if len(doneEvents) != 1 {
		t.Fatalf("expected 1 done event, got %d", len(doneEvents))
	}
	var done DirSearchDone
	if err := json.Unmarshal(doneEvents[0], &done); err != nil {
		t.Fatalf("failed to unmarshal done: %v", err)
	}
	if done.Total != 1 {
		t.Errorf("expected total 1, got %d", done.Total)
	}
	if done.Truncated {
		t.Error("expected truncated=false")
	}
}

func TestDirSearch_Recursive(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create nested files
	createTestFile(t, env.ProjectDir, "main.go", "package main")
	createTestFile(t, env.ProjectDir, "internal/handler/file.go", "package handler")
	createTestFile(t, env.ProjectDir, "internal/handler/util.go", "package handler")

	req := newRequest(t, http.MethodGet, "/api/dir/search?path=&q=file&recursive=true", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(DirSearch, req)

	assertOK(t, w)
	events := parseSearchSSEEvents(w.Body.String())
	results := events["result"]

	if len(results) != 1 {
		t.Fatalf("expected 1 result (file.go), got %d", len(results))
	}

	var r DirSearchResult
	if err := json.Unmarshal(results[0], &r); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if r.Name != "file.go" {
		t.Errorf("expected name file.go, got %s", r.Name)
	}
	if r.Path != "internal/handler/file.go" {
		t.Errorf("expected path internal/handler/file.go, got %s", r.Path)
	}
}

func TestDirSearch_FuzzyMatching(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "main.go", "package main")
	createTestFile(t, env.ProjectDir, "minimal.txt", "text")

	// "mnl" should fuzzy-match "main.go" (m-a-i-n-.go → m at 0, n at 3, l not in main.go)
	// Actually, let's use "mng" which should match "main.go" (m=0, n=3, g=5)
	req := newRequest(t, http.MethodGet, "/api/dir/search?path=&q=mng&recursive=false", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(DirSearch, req)

	assertOK(t, w)
	events := parseSearchSSEEvents(w.Body.String())
	results := events["result"]

	if len(results) < 1 {
		t.Fatalf("expected at least 1 result, got %d", len(results))
	}

	var r DirSearchResult
	if err := json.Unmarshal(results[0], &r); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if r.Name != "main.go" {
		t.Errorf("expected name main.go, got %s", r.Name)
	}
	if len(r.MatchedIndices) == 0 {
		t.Error("expected matchedIndices to be non-empty")
	}
}

func TestDirSearch_ExactMatching(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "main.go", "package main")
	createTestFile(t, env.ProjectDir, "main_helper.go", "package helper")
	createTestFile(t, env.ProjectDir, "mymain.go", "package main")

	// exact=true should only match files whose name equals the query,
	// excluding fuzzy submatches like main_helper.go and mymain.go
	req := newRequest(t, http.MethodGet, "/api/dir/search?path=&q=main.go&recursive=false&exact=true", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(DirSearch, req)

	assertOK(t, w)
	events := parseSearchSSEEvents(w.Body.String())
	results := events["result"]

	if len(results) != 1 {
		t.Fatalf("expected exactly 1 exact match, got %d", len(results))
	}

	var r DirSearchResult
	if err := json.Unmarshal(results[0], &r); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if r.Name != "main.go" {
		t.Errorf("expected name main.go, got %s", r.Name)
	}
	// Exact match highlights the entire filename
	if len(r.MatchedIndices) != len(r.Name) {
		t.Errorf("expected %d matched indices for %s, got %d", len(r.Name), r.Name, len(r.MatchedIndices))
	}
}

func TestDirSearch_ExactMatchingCaseInsensitive(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "Main.Go", "package main")

	req := newRequest(t, http.MethodGet, "/api/dir/search?path=&q=main.go&recursive=false&exact=true", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(DirSearch, req)

	assertOK(t, w)
	events := parseSearchSSEEvents(w.Body.String())
	results := events["result"]
	if len(results) != 1 {
		t.Fatalf("expected 1 case-insensitive exact match, got %d", len(results))
	}
}

func TestShouldSkipSearchDir(t *testing.T) {
	tests := []struct {
		relPath string
		name    string
		want    bool
	}{
		// Standard ignored dirs are always skipped.
		{"node_modules/pkg", "node_modules", true},
		{".git", ".git", true},
		{"vendor", "vendor", true},
		// A `build` dir itself is walked so the Android outputs subtree stays reachable.
		{"app/build", "build", false},
		{"build", "build", false},
		// Android build/outputs subtree (e.g. .apk) is searchable.
		{"app/build/outputs", "outputs", false},
		{"app/build/outputs/apk/release", "release", false},
		// Non-outputs children of a build tree are skipped.
		{"app/build/intermediates", "intermediates", true},
		{"app/build/generated", "generated", true},
		{"app/build/intermediates/debug", "debug", true},
		// Native build subdirs are skipped.
		{"build/CMakeFiles", "CMakeFiles", true},
		{"build/_deps", "_deps", true},
		// Unrelated directories are not skipped.
		{"src/main", "main", false},
		{"internal/handler", "handler", false},
		// A dir literally named "build" not in a build tree is still walked.
		{"src/build-tool", "build-tool", false},
	}

	for _, tt := range tests {
		t.Run(tt.relPath, func(t *testing.T) {
			if got := shouldSkipSearchDir(tt.relPath, tt.name); got != tt.want {
				t.Errorf("shouldSkipSearchDir(%q, %q) = %v, want %v", tt.relPath, tt.name, got, tt.want)
			}
		})
	}
}

func TestDirSearch_AndroidBuildOutputs(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// APK under build/outputs should be searchable...
	createTestFile(t, env.ProjectDir, "android/app/build/outputs/apk/release/clawbench-android.apk", "apk")
	// ...while the rest of the build tree stays skipped.
	createTestFile(t, env.ProjectDir, "android/app/build/intermediates/debug/symbols.txt", "symbols")
	createTestFile(t, env.ProjectDir, "android/app/build/generated/foo.txt", "generated")

	req := newRequest(t, http.MethodGet, "/api/dir/search?path=&q=clawbench&recursive=true", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(DirSearch, req)

	assertOK(t, w)
	events := parseSearchSSEEvents(w.Body.String())
	results := events["result"]
	if len(results) != 1 {
		t.Fatalf("expected 1 result (the .apk), got %d", len(results))
	}
	var r DirSearchResult
	if err := json.Unmarshal(results[0], &r); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if r.Name != "clawbench-android.apk" {
		t.Errorf("expected name clawbench-android.apk, got %s", r.Name)
	}
	if r.Path != "android/app/build/outputs/apk/release/clawbench-android.apk" {
		t.Errorf("expected apk path, got %s", r.Path)
	}
}

func TestDirSearch_SkipsBuildIntermediates(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "android/app/build/intermediates/debug/symbols.txt", "symbols")

	req := newRequest(t, http.MethodGet, "/api/dir/search?path=&q=symbols&recursive=true", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(DirSearch, req)

	assertOK(t, w)
	events := parseSearchSSEEvents(w.Body.String())
	if len(events["result"]) != 0 {
		t.Fatalf("expected 0 results from build/intermediates, got %d", len(events["result"]))
	}
}

func TestDirSearch_IgnoresDirs(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create files in ignored directories
	createTestFile(t, env.ProjectDir, ".git/HEAD", "ref: refs/heads/main")
	createTestFile(t, env.ProjectDir, "node_modules/pkg/index.js", "export {}")
	createTestFile(t, env.ProjectDir, "vendor/lib/util.go", "package lib")
	createTestFile(t, env.ProjectDir, "src/main.go", "package main")

	req := newRequest(t, http.MethodGet, "/api/dir/search?path=&q=main&recursive=true", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(DirSearch, req)

	assertOK(t, w)
	events := parseSearchSSEEvents(w.Body.String())
	results := events["result"]

	for _, raw := range results {
		var r DirSearchResult
		if err := json.Unmarshal(raw, &r); err != nil {
			t.Fatalf("failed to unmarshal result: %v", err)
		}
		if strings.HasPrefix(r.Path, ".git/") || strings.HasPrefix(r.Path, "node_modules/") || strings.HasPrefix(r.Path, "vendor/") {
			t.Errorf("found result in ignored directory: %s", r.Path)
		}
	}
}

func TestDirSearch_Limit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create 5 files, limit to 3
	for i := range 5 {
		createTestFile(t, env.ProjectDir, "file_"+string(rune('0'+i))+".go", "package main")
	}

	req := newRequest(t, http.MethodGet, "/api/dir/search?path=&q=file&recursive=false&limit=3", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(DirSearch, req)

	assertOK(t, w)
	events := parseSearchSSEEvents(w.Body.String())
	results := events["result"]

	if len(results) != 3 {
		t.Fatalf("expected 3 results (limit), got %d", len(results))
	}

	var done DirSearchDone
	if err := json.Unmarshal(events["done"][0], &done); err != nil {
		t.Fatalf("failed to unmarshal done: %v", err)
	}
	if done.Total != 5 {
		t.Errorf("expected total 5 (total match count), got %d", done.Total)
	}
	if !done.Truncated {
		t.Error("expected truncated=true")
	}
}

func TestDirSearch_ImageType(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "photo.png", "fake-png-data")

	req := newRequest(t, http.MethodGet, "/api/dir/search?path=&q=photo&recursive=false", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(DirSearch, req)

	assertOK(t, w)
	events := parseSearchSSEEvents(w.Body.String())
	results := events["result"]

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	var r DirSearchResult
	if err := json.Unmarshal(results[0], &r); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if r.Type != "image" {
		t.Errorf("expected type image, got %s", r.Type)
	}
}

func TestDirSearch_SubdirectorySearch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "root.go", "package main")
	createTestFile(t, env.ProjectDir, "internal/handler/file.go", "package handler")
	createTestFile(t, env.ProjectDir, "internal/service/svc.go", "package service")

	// Search only within internal/handler — paths should be project-root-relative
	req := newRequest(t, http.MethodGet, "/api/dir/search?path=internal/handler&q=file&recursive=false", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(DirSearch, req)

	assertOK(t, w)
	events := parseSearchSSEEvents(w.Body.String())
	results := events["result"]

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	var r DirSearchResult
	if err := json.Unmarshal(results[0], &r); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if r.Name != "file.go" {
		t.Errorf("expected name file.go, got %s", r.Name)
	}
	// Path should be project-root-relative, not relative to the search directory
	if r.Path != "internal/handler/file.go" {
		t.Errorf("expected path internal/handler/file.go, got %s", r.Path)
	}
}

func TestDirSearch_LimitClamp(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "a.go", "package main")

	// Request limit above maxSearchLimit (500)
	req := newRequest(t, http.MethodGet, "/api/dir/search?path=&q=a&recursive=false&limit=9999", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(DirSearch, req)

	assertOK(t, w)
	events := parseSearchSSEEvents(w.Body.String())

	// Should still get results — limit was clamped, not rejected
	if len(events["result"]) != 1 {
		t.Fatalf("expected 1 result, got %d", len(events["result"]))
	}
}

func TestDirSearch_QueryTooLong(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Query exceeds maxSearchQueryLen (256)
	longQuery := strings.Repeat("a", 300)
	req := newRequest(t, http.MethodGet, "/api/dir/search?path=&q="+longQuery, nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(DirSearch, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestDirSearch_RecursiveBoolParsing(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "main.go", "package main")
	createTestFile(t, env.ProjectDir, "src/util.go", "package src")

	// "1" should be parsed as true via strconv.ParseBool
	req := newRequest(t, http.MethodGet, "/api/dir/search?path=&q=util&recursive=1", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(DirSearch, req)

	assertOK(t, w)
	events := parseSearchSSEEvents(w.Body.String())
	results := events["result"]
	if len(results) != 1 {
		t.Fatalf("expected 1 result (recursive=true via '1'), got %d", len(results))
	}

	var r DirSearchResult
	if err := json.Unmarshal(results[0], &r); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if r.Path != "src/util.go" {
		t.Errorf("expected path src/util.go, got %s", r.Path)
	}
}

func TestParseSearchParams_EmptyQuery(t *testing.T) {
	req := newRequest(t, http.MethodGet, "/api/dir/search?path=&q=", nil)
	w := httptest.NewRecorder()
	_, ok := parseSearchParams(w, req)
	if ok {
		t.Error("expected ok=false for empty query")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestParseSearchParams_QueryTooLong(t *testing.T) {
	req := newRequest(t, http.MethodGet, "/api/dir/search?q="+strings.Repeat("a", 300), nil)
	w := httptest.NewRecorder()
	_, ok := parseSearchParams(w, req)
	if ok {
		t.Error("expected ok=false for too-long query")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestParseSearchParams_Defaults(t *testing.T) {
	req := newRequest(t, http.MethodGet, "/api/dir/search?path=sub&q=test", nil)
	w := httptest.NewRecorder()
	params, ok := parseSearchParams(w, req)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if params.query != "test" {
		t.Errorf("expected query=test, got %s", params.query)
	}
	if params.relPath != "sub" {
		t.Errorf("expected relPath=sub, got %s", params.relPath)
	}
	if !params.recursive {
		t.Error("expected recursive=true by default")
	}
	expectedDefaultLimit := model.ConfigInstance.FileSearch.DisplayLimit + 1
	if params.limit != expectedDefaultLimit {
		t.Errorf("expected limit=%d, got %d", expectedDefaultLimit, params.limit)
	}
}

func TestParseSearchParams_RecursiveFalse(t *testing.T) {
	req := newRequest(t, http.MethodGet, "/api/dir/search?q=test&recursive=false", nil)
	w := httptest.NewRecorder()
	params, ok := parseSearchParams(w, req)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if params.recursive {
		t.Error("expected recursive=false")
	}
}

func TestParseSearchParams_InvalidRecursive(t *testing.T) {
	req := newRequest(t, http.MethodGet, "/api/dir/search?q=test&recursive=maybe", nil)
	w := httptest.NewRecorder()
	params, ok := parseSearchParams(w, req)
	if !ok {
		t.Fatal("expected ok=true")
	}
	// Invalid recursive value falls back to default true
	if !params.recursive {
		t.Error("expected recursive=true for invalid value fallback")
	}
}

func TestParseSearchParams_LimitClamp(t *testing.T) {
	req := newRequest(t, http.MethodGet, "/api/dir/search?q=test&limit=9999", nil)
	w := httptest.NewRecorder()
	params, ok := parseSearchParams(w, req)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if params.limit != maxSearchLimit {
		t.Errorf("expected limit clamped to %d, got %d", maxSearchLimit, params.limit)
	}
}

func TestParseSearchParams_InvalidLimit(t *testing.T) {
	req := newRequest(t, http.MethodGet, "/api/dir/search?q=test&limit=abc", nil)
	w := httptest.NewRecorder()
	params, ok := parseSearchParams(w, req)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if params.limit != model.ConfigInstance.FileSearch.DisplayLimit+1 {
		t.Errorf("expected default limit=%d for invalid value, got %d", model.ConfigInstance.FileSearch.DisplayLimit+1, params.limit)
	}
}

func TestParseSearchParams_NegativeLimit(t *testing.T) {
	req := newRequest(t, http.MethodGet, "/api/dir/search?q=test&limit=-5", nil)
	w := httptest.NewRecorder()
	params, ok := parseSearchParams(w, req)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if params.limit != model.ConfigInstance.FileSearch.DisplayLimit+1 {
		t.Errorf("expected default limit=%d for negative value, got %d", model.ConfigInstance.FileSearch.DisplayLimit+1, params.limit)
	}
}

func TestParseSearchParams_PathLeadingSlash(t *testing.T) {
	req := newRequest(t, http.MethodGet, "/api/dir/search?q=test&path=/sub/dir", nil)
	w := httptest.NewRecorder()
	params, ok := parseSearchParams(w, req)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if params.relPath != "sub/dir" {
		t.Errorf("expected relPath=sub/dir (leading slash stripped), got %s", params.relPath)
	}
}

func TestClassifyEntry(t *testing.T) {
	tests := []struct {
		name     string
		isDir    bool
		fileName string
		want     string
	}{
		{"directory", true, "src", entryTypeDir},
		{"regular file", false, "main.go", entryTypeFile},
		{"image png", false, "photo.png", entryTypeImage},
		{"image jpg", false, "pic.jpg", entryTypeImage},
		{"image gif", false, "anim.gif", entryTypeImage},
		{"image webp", false, "icon.webp", entryTypeImage},
		{"image svg", false, "logo.svg", entryTypeImage},
		{"text file", false, "readme.txt", entryTypeFile},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := fakeDirEntry{isDir: tt.isDir, name: tt.fileName}
			got := classifyEntry(d, tt.fileName)
			if got != tt.want {
				t.Errorf("classifyEntry(%v, %q) = %q, want %q", tt.isDir, tt.fileName, got, tt.want)
			}
		})
	}
}

// fakeDirEntry implements fs.DirEntry for testing classifyEntry.
type fakeDirEntry struct {
	isDir bool
	name  string
}

func (f fakeDirEntry) Name() string               { return f.name }
func (f fakeDirEntry) IsDir() bool                { return f.isDir }
func (f fakeDirEntry) Type() fs.FileMode          { return 0 }
func (f fakeDirEntry) Info() (fs.FileInfo, error) { return nil, nil }

func TestDirSearch_WrongMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/dir/search?path=&q=test", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(DirSearch, req)

	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestDirSearch_MissingProject(t *testing.T) {
	req := newRequest(t, http.MethodGet, "/api/dir/search?path=&q=test", nil)
	// No project cookie
	w := callHandler(DirSearch, req)

	// requireProject returns 403 when no project is set
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestDirSearch_InvalidSubPath(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Request a path outside the project (traversal)
	req := newRequest(t, http.MethodGet, "/api/dir/search?path=../../../etc&q=test", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(DirSearch, req)

	// Should reject the traversal path
	if w.Code == http.StatusOK {
		t.Error("expected non-200 for path traversal")
	}
}

func TestDirSearch_DirType(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a directory
	if err := os.MkdirAll(filepath.Join(env.ProjectDir, "srcdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	req := newRequest(t, http.MethodGet, "/api/dir/search?path=&q=srcdir&recursive=false", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(DirSearch, req)

	assertOK(t, w)
	events := parseSearchSSEEvents(w.Body.String())
	results := events["result"]

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	var r DirSearchResult
	if err := json.Unmarshal(results[0], &r); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if r.Type != entryTypeDir {
		t.Errorf("expected type dir, got %s", r.Type)
	}
}

func TestDirSearch_NoMatch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "main.go", "package main")

	req := newRequest(t, http.MethodGet, "/api/dir/search?path=&q=zzznonexistent&recursive=false", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(DirSearch, req)

	assertOK(t, w)
	events := parseSearchSSEEvents(w.Body.String())

	if len(events["result"]) != 0 {
		t.Errorf("expected 0 results, got %d", len(events["result"]))
	}

	doneEvents := events["done"]
	if len(doneEvents) != 1 {
		t.Fatalf("expected 1 done event, got %d", len(doneEvents))
	}
	var done DirSearchDone
	if err := json.Unmarshal(doneEvents[0], &done); err != nil {
		t.Fatalf("failed to unmarshal done: %v", err)
	}
	if done.Total != 0 {
		t.Errorf("expected total 0, got %d", done.Total)
	}
}
