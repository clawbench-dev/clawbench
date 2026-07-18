package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseSSEEvents splits an SSE response body into individual events.
func parseSSEEvents(body string) []map[string]string {
	var events []map[string]string
	parts := strings.Split(body, "\n\n")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		entry := map[string]string{}
		lines := strings.Split(part, "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "event: ") {
				entry["event"] = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				entry["data"] = strings.TrimPrefix(line, "data: ")
			}
		}
		if _, ok := entry["event"]; ok {
			events = append(events, entry)
		}
	}
	return events
}

// threadSafeRecorder wraps httptest.ResponseRecorder with a mutex
// to allow safe concurrent reads from the body while the handler is writing.
// This prevents DATA RACE when SSE goroutines write to the recorder
// while the test goroutine reads Body.String().
type threadSafeRecorder struct {
	*httptest.ResponseRecorder
	mu sync.Mutex
}

func (r *threadSafeRecorder) Write(data []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ResponseRecorder.Write(data)
}

func (r *threadSafeRecorder) BodyString() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Body.String()
}

// ---------- FileWatchSSE ----------

func TestFileWatchSSE_MethodNotAllowed(t *testing.T) {
	req := newRequest(t, http.MethodPost, "/api/file/watch", nil)
	w := callHandler(FileWatchSSE, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestFileWatchSSE_MissingProjectCookie(t *testing.T) {
	req := newRequest(t, http.MethodGet, "/api/file/watch", nil)
	w := callHandler(FileWatchSSE, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestFileWatchSSE_WatcherNotAvailable(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	orig := service.GlobalFileWatcher
	service.GlobalFileWatcher = nil
	defer func() { service.GlobalFileWatcher = orig }()

	req := newRequest(t, http.MethodGet, "/api/file/watch?dir=.", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(FileWatchSSE, req)

	assertStatus(t, w, http.StatusServiceUnavailable)
}

func TestFileWatchSSE_ConnectedEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	err := service.InitFileWatcher()
	assert.NoError(t, err)
	defer service.StopFileWatcher()

	ctx, cancel := context.WithCancel(context.Background())

	req := newRequest(t, http.MethodGet, "/api/file/watch?dir=", nil)
	req = req.WithContext(ctx)
	req = withProjectCookie(req, env.ProjectDir)

	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		FileWatchSSE(w, req)
		close(done)
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	body := w.Body.String()
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	assert.Contains(t, body, "event: connected")

	events := parseSSEEvents(body)
	assert.Len(t, events, 1)
	assert.Equal(t, "connected", events[0]["event"])

	var data map[string]string
	_ = json.Unmarshal([]byte(events[0]["data"]), &data)
	assert.NotEmpty(t, data["clientId"])
}

func TestFileWatchSSE_EmptyDirResolvesToProjectRoot(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	err := service.InitFileWatcher()
	assert.NoError(t, err)
	defer service.StopFileWatcher()

	ctx, cancel := context.WithCancel(context.Background())

	req := newRequest(t, http.MethodGet, "/api/file/watch?dir=&file=", nil)
	req = req.WithContext(ctx)
	req = withProjectCookie(req, env.ProjectDir)

	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		FileWatchSSE(w, req)
		close(done)
	}()

	time.Sleep(200 * time.Millisecond)

	// Verify: create a file in project root — should trigger dir_change
	testFile := filepath.Join(env.ProjectDir, "newfile.txt")
	_ = os.WriteFile(testFile, []byte("test"), 0o644)

	time.Sleep(500 * time.Millisecond)

	cancel()
	<-done

	body := w.Body.String()
	events := parseSSEEvents(body)

	// Should have connected + dir_change
	foundDirChange := false
	for _, e := range events {
		if e["event"] == "dir_change" {
			foundDirChange = true
		}
	}
	assert.True(t, foundDirChange, "should receive dir_change when file is created in project root")
}

func TestFileWatchSSE_DirAndFileParams(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	err := service.InitFileWatcher()
	assert.NoError(t, err)
	defer service.StopFileWatcher()

	// Create a test file
	testFile := filepath.Join(env.ProjectDir, "test.txt")
	_ = os.WriteFile(testFile, []byte("hello"), 0o644)

	ctx, cancel := context.WithCancel(context.Background())

	req := newRequest(t, http.MethodGet, "/api/file/watch?dir=.&file=test.txt", nil)
	req = req.WithContext(ctx)
	req = withProjectCookie(req, env.ProjectDir)

	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		FileWatchSSE(w, req)
		close(done)
	}()

	time.Sleep(200 * time.Millisecond)

	// Modify the file to verify file watch is working
	_ = os.WriteFile(testFile, []byte("modified"), 0o644)

	time.Sleep(500 * time.Millisecond)

	cancel()
	<-done

	body := w.Body.String()
	events := parseSSEEvents(body)

	foundFileChange := false
	for _, e := range events {
		if e["event"] == "file_change" {
			foundFileChange = true
		}
	}
	assert.True(t, foundFileChange, "should receive file_change when watched file is modified")
}

func TestFileWatchSSE_PathTraversal(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	err := service.InitFileWatcher()
	assert.NoError(t, err)
	defer service.StopFileWatcher()

	req := newRequest(t, http.MethodGet, "/api/file/watch?dir=../../../etc", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(FileWatchSSE, req)

	assertStatus(t, w, http.StatusForbidden)
}

func TestFileWatchSSE_ClientDisconnect(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	err := service.InitFileWatcher()
	assert.NoError(t, err)
	defer service.StopFileWatcher()

	ctx, cancel := context.WithCancel(context.Background())

	req := newRequest(t, http.MethodGet, "/api/file/watch?dir=", nil)
	req = req.WithContext(ctx)
	req = withProjectCookie(req, env.ProjectDir)

	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		FileWatchSSE(w, req)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)

	// Disconnect client
	cancel()
	<-done

	// After disconnect, the SSE handler should have called UnregisterClient.
	// We verify this by checking that a new SSE connection gets a fresh client.
	// (No direct way to check internal state from handler package, but no panic = success)
}

func TestFileWatchSSE_FileChangeEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	err := service.InitFileWatcher()
	assert.NoError(t, err)
	defer service.StopFileWatcher()

	testFile := filepath.Join(env.ProjectDir, "watchme.txt")
	_ = os.WriteFile(testFile, []byte("initial"), 0o644)

	ctx, cancel := context.WithCancel(context.Background())

	req := newRequest(t, http.MethodGet, "/api/file/watch?dir=.&file=watchme.txt", nil)
	req = req.WithContext(ctx)
	req = withProjectCookie(req, env.ProjectDir)

	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		FileWatchSSE(w, req)
		close(done)
	}()

	time.Sleep(300 * time.Millisecond)

	_ = os.WriteFile(testFile, []byte("modified"), 0o644)

	time.Sleep(500 * time.Millisecond)

	cancel()
	<-done

	body := w.Body.String()
	events := parseSSEEvents(body)

	foundFileChange := false
	for _, e := range events {
		if e["event"] == "file_change" {
			foundFileChange = true
			var data map[string]string
			_ = json.Unmarshal([]byte(e["data"]), &data)
			assert.Equal(t, "file_change", data["type"])
			assert.Contains(t, data["path"], "watchme.txt")
		}
	}
	assert.True(t, foundFileChange, "should receive file_change event")
}

func TestFileWatchSSE_DirChangeEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	err := service.InitFileWatcher()
	assert.NoError(t, err)
	defer service.StopFileWatcher()

	ctx, cancel := context.WithCancel(context.Background())

	req := newRequest(t, http.MethodGet, "/api/file/watch?dir=.", nil)
	req = req.WithContext(ctx)
	req = withProjectCookie(req, env.ProjectDir)

	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		FileWatchSSE(w, req)
		close(done)
	}()

	time.Sleep(300 * time.Millisecond)

	// Create a new file in the directory
	newFile := filepath.Join(env.ProjectDir, "new_in_dir.txt")
	_ = os.WriteFile(newFile, []byte("new"), 0o644)

	time.Sleep(500 * time.Millisecond)

	cancel()
	<-done

	body := w.Body.String()
	events := parseSSEEvents(body)

	foundDirChange := false
	for _, e := range events {
		if e["event"] == "dir_change" {
			foundDirChange = true
		}
	}
	assert.True(t, foundDirChange, "should receive dir_change when file is created in watched directory")
}

// ---------- FileWatchUpdate ----------

func TestFileWatchUpdate_MethodNotAllowed(t *testing.T) {
	req := newRequest(t, http.MethodGet, "/api/file/watch/update", nil)
	w := callHandler(FileWatchUpdate, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestFileWatchUpdate_MissingProjectCookie(t *testing.T) {
	req := newRequest(t, http.MethodPut, "/api/file/watch/update", nil)
	w := callHandler(FileWatchUpdate, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestFileWatchUpdate_WatcherNotAvailable(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	orig := service.GlobalFileWatcher
	service.GlobalFileWatcher = nil
	defer func() { service.GlobalFileWatcher = orig }()

	body := fileWatchUpdateRequest{
		ClientID: "test-id",
		DirPath:  ".",
		FilePath: "test.txt",
	}
	req := newRequest(t, http.MethodPut, "/api/file/watch/update", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(FileWatchUpdate, req)

	assertStatus(t, w, http.StatusServiceUnavailable)
}

func TestFileWatchUpdate_MissingClientID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	err := service.InitFileWatcher()
	assert.NoError(t, err)
	defer service.StopFileWatcher()

	body := fileWatchUpdateRequest{
		ClientID: "",
		DirPath:  ".",
		FilePath: "test.txt",
	}
	req := newRequest(t, http.MethodPut, "/api/file/watch/update", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(FileWatchUpdate, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestFileWatchUpdate_InvalidJSON(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	err := service.InitFileWatcher()
	assert.NoError(t, err)
	defer service.StopFileWatcher()

	req := httptest.NewRequest(http.MethodPut, "/api/file/watch/update", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	FileWatchUpdate(w, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestFileWatchUpdate_PathTraversal(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	err := service.InitFileWatcher()
	assert.NoError(t, err)
	defer service.StopFileWatcher()

	body := fileWatchUpdateRequest{
		ClientID: "test-traversal",
		DirPath:  "../../../etc",
		FilePath: "",
	}
	req := newRequest(t, http.MethodPut, "/api/file/watch/update", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(FileWatchUpdate, req)

	assertStatus(t, w, http.StatusForbidden)
}

func TestFileWatchUpdate_Success(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	err := service.InitFileWatcher()
	assert.NoError(t, err)
	defer service.StopFileWatcher()

	// Register a client via SSE connection first
	ctx, cancel := context.WithCancel(context.Background())
	sseReq := newRequest(t, http.MethodGet, "/api/file/watch?dir=", nil)
	sseReq = sseReq.WithContext(ctx)
	sseReq = withProjectCookie(sseReq, env.ProjectDir)
	sseW := &threadSafeRecorder{ResponseRecorder: httptest.NewRecorder()}

	sseDone := make(chan struct{})
	go func() {
		FileWatchSSE(sseW, sseReq)
		close(sseDone)
	}()

	time.Sleep(200 * time.Millisecond)

	// Safely read the connected event to get the clientId
	sseEvents := parseSSEEvents(sseW.BodyString())
	assert.Len(t, sseEvents, 1)
	var connectedData map[string]string
	require.NoError(t, json.Unmarshal([]byte(sseEvents[0]["data"]), &connectedData))
	clientID := connectedData["clientId"]
	assert.NotEmpty(t, clientID)

	// Create test file
	testFile := filepath.Join(env.ProjectDir, "update.txt")
	_ = os.WriteFile(testFile, []byte("hello"), 0o644)

	// Update the watch paths
	body := fileWatchUpdateRequest{
		ClientID: clientID,
		DirPath:  ".",
		FilePath: "update.txt",
	}
	req := newRequest(t, http.MethodPut, "/api/file/watch/update", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(FileWatchUpdate, req)

	assertOK(t, w)
	assertJSONField(t, w, "ok", true)

	// Modify the file and verify file_change comes through
	_ = os.WriteFile(testFile, []byte("modified"), 0o644)

	time.Sleep(500 * time.Millisecond)

	cancel()
	<-sseDone

	// Check SSE events
	sseEvents2 := parseSSEEvents(sseW.BodyString())

	foundFileChange := false
	for _, e := range sseEvents2 {
		if e["event"] == "file_change" {
			foundFileChange = true
		}
	}
	assert.True(t, foundFileChange, "should receive file_change after UpdateWatch set file path")
}

func TestFileWatchUpdate_EmptyDirResolvesToProjectRoot(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	err := service.InitFileWatcher()
	assert.NoError(t, err)
	defer service.StopFileWatcher()

	// Register a client via SSE connection
	ctx, cancel := context.WithCancel(context.Background())
	sseReq := newRequest(t, http.MethodGet, "/api/file/watch?dir=", nil)
	sseReq = sseReq.WithContext(ctx)
	sseReq = withProjectCookie(sseReq, env.ProjectDir)
	sseW := &threadSafeRecorder{ResponseRecorder: httptest.NewRecorder()}

	sseDone := make(chan struct{})
	go func() {
		FileWatchSSE(sseW, sseReq)
		close(sseDone)
	}()

	time.Sleep(200 * time.Millisecond)

	sseEvents := parseSSEEvents(sseW.BodyString())
	var connectedData map[string]string
	require.NoError(t, json.Unmarshal([]byte(sseEvents[0]["data"]), &connectedData))
	clientID := connectedData["clientId"]

	// Update with empty dir — should resolve to project root
	body := fileWatchUpdateRequest{
		ClientID: clientID,
		DirPath:  "",
		FilePath: "",
	}
	req := newRequest(t, http.MethodPut, "/api/file/watch/update", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(FileWatchUpdate, req)

	assertOK(t, w)

	// Verify: create a file in project root — should trigger dir_change
	testFile := filepath.Join(env.ProjectDir, "root_file.txt")
	_ = os.WriteFile(testFile, []byte("root"), 0o644)

	time.Sleep(500 * time.Millisecond)

	cancel()
	<-sseDone

	sseEvents2 := parseSSEEvents(sseW.BodyString())
	foundDirChange := false
	for _, e := range sseEvents2 {
		if e["event"] == "dir_change" {
			foundDirChange = true
		}
	}
	assert.True(t, foundDirChange, "empty dir should watch project root, so dir_change should fire")
}

func TestFileWatchUpdate_UnknownClientId(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	err := service.InitFileWatcher()
	assert.NoError(t, err)
	defer service.StopFileWatcher()

	body := fileWatchUpdateRequest{
		ClientID: "nonexistent-client",
		DirPath:  ".",
		FilePath: "",
	}
	req := newRequest(t, http.MethodPut, "/api/file/watch/update", body)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(FileWatchUpdate, req)

	// Should return OK (UpdateWatch silently ignores unknown clientID)
	assertOK(t, w)
}

// ---------- newWatchClientID ----------

func TestNewWatchClientID_Uniqueness(t *testing.T) {
	ids := make(map[string]bool)
	for range 100 {
		id := newWatchClientID()
		assert.NotEmpty(t, id)
		assert.False(t, ids[id], "client ID should be unique, got duplicate: %s", id)
		ids[id] = true
	}
}

func TestNewWatchClientID_Format(t *testing.T) {
	id := newWatchClientID()
	// Should be UUID-like format: xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	parts := strings.Split(id, "-")
	assert.Len(t, parts, 5, "client ID should have 5 hyphen-separated parts")
}
