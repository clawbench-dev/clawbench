package handler

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
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

	assertStatus(t, w, http.StatusBadRequest)
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
	for i := 0; i < 5; i++ {
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
	if done.Total != 3 {
		t.Errorf("expected total 3 (sent count), got %d", done.Total)
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

	// Search only within internal/handler
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
