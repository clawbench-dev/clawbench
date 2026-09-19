package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- WS test harness ----------

// setupFileWatchWSTest starts an httptest server exposing the real route with
// the real auth middleware, so tests exercise the same path production serves.
func setupFileWatchWSTest(t *testing.T) (*testEnv, *httptest.Server, func()) {
	t.Helper()
	env, teardown := setupTestEnv(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/file/watch/ws", middleware.Auth(FileWatchWS))
	server := httptest.NewServer(mux)

	cleanup := func() {
		server.Close()
		teardown()
	}
	return env, server, cleanup
}

// dialFileWatchWS connects to the file-watch WS. query is appended verbatim
// (e.g. "?dir=.&file=test.txt"). websocket.Dial sends no Origin header by
// default, so the OriginPatterns check passes.
func dialFileWatchWS(t *testing.T, serverURL, projectPath, query string) *websocket.Conn {
	t.Helper()
	conn, _, err := dialFileWatchWSRaw(t, serverURL, projectPath, query)
	require.NoError(t, err)
	return conn
}

// dialFileWatchWSRaw returns the handshake response too, for status assertions
// on rejected upgrades.
func dialFileWatchWSRaw(t *testing.T, serverURL, projectPath, query string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	wsURL := "ws" + serverURL[len("http"):] + "/api/file/watch/ws" + query
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	opts := &websocket.DialOptions{}
	if projectPath != "" {
		opts.HTTPHeader = http.Header{
			"Cookie": []string{model.ScopedCookieName("clawbench_project") + "=" + url.QueryEscape(projectPath)},
		}
	}
	return websocket.Dial(ctx, wsURL, opts)
}

// readWatchFrame reads one text frame and returns its decoded type.
func readWatchFrame(t *testing.T, conn *websocket.Conn) (string, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	require.NoError(t, err, "failed to read WS frame")
	var envelope struct {
		Type string `json:"type"`
	}
	require.NoError(t, json.Unmarshal(data, &envelope), "frame is not valid JSON: %s", data)
	return envelope.Type, data
}

// waitForWatchEventType reads frames until one of wantType arrives, skipping
// pings and unrelated events. Fails the test on timeout.
func waitForWatchEventType(t *testing.T, conn *websocket.Conn, wantType string) []byte {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Until(deadline))
		_, data, err := conn.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("timed out waiting for %q frame: %v", wantType, err)
		}
		var envelope struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(data, &envelope) != nil {
			continue
		}
		if envelope.Type == wantType {
			return data
		}
	}
	t.Fatalf("never received %q frame", wantType)
	return nil
}

// sendWatchMessage writes a JSON control frame.
func sendWatchMessage(t *testing.T, conn *websocket.Conn, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, conn.Write(ctx, websocket.MessageText, data))
}

// ---------- FileWatchWS: pre-upgrade rejection paths ----------

func TestFileWatchWS_MethodNotAllowed(t *testing.T) {
	req := newRequest(t, http.MethodPost, "/api/file/watch/ws", nil)
	w := callHandler(FileWatchWS, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestFileWatchWS_MissingProjectCookie(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	err := service.InitFileWatcher()
	require.NoError(t, err)
	defer service.StopFileWatcher()

	// A missing project cookie must be rejected before the upgrade.
	req := newRequest(t, http.MethodGet, "/api/file/watch/ws?dir=", nil)
	w := callHandler(FileWatchWS, req)
	assertStatus(t, w, http.StatusForbidden)
	_ = env
}

func TestFileWatchWS_WatcherNotAvailable(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	orig := service.GlobalFileWatcher
	service.GlobalFileWatcher = nil
	defer func() { service.GlobalFileWatcher = orig }()

	req := newRequest(t, http.MethodGet, "/api/file/watch/ws?dir=.", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(FileWatchWS, req)

	assertStatus(t, w, http.StatusServiceUnavailable)
}

func TestFileWatchWS_PathTraversal(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	err := service.InitFileWatcher()
	require.NoError(t, err)
	defer service.StopFileWatcher()

	// Traversal is validated before the upgrade, so it stays an HTTP 403.
	req := newRequest(t, http.MethodGet, "/api/file/watch/ws?dir=../../../etc", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(FileWatchWS, req)

	assertStatus(t, w, http.StatusForbidden)
}

func TestFileWatchWS_AcceptErrorOnPlainRecorder(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	err := service.InitFileWatcher()
	require.NoError(t, err)
	defer service.StopFileWatcher()

	// httptest.NewRecorder cannot be hijacked, so Accept fails and the handler
	// must return without panicking.
	req := newRequest(t, http.MethodGet, "/api/file/watch/ws?dir=", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(FileWatchWS, req)

	assert.NotEqual(t, http.StatusOK, w.Code)
}

// ---------- FileWatchWS: event delivery ----------

func TestFileWatchWS_ConnectedFrame(t *testing.T) {
	env, server, cleanup := setupFileWatchWSTest(t)
	defer cleanup()

	require.NoError(t, service.InitFileWatcher())
	defer service.StopFileWatcher()

	conn := dialFileWatchWS(t, server.URL, env.ProjectDir, "?dir=")
	defer conn.CloseNow()

	typ, data := readWatchFrame(t, conn)
	assert.Equal(t, watchMsgConnected, typ)

	var payload struct {
		ClientID string `json:"clientId"`
	}
	require.NoError(t, json.Unmarshal(data, &payload))
	assert.NotEmpty(t, payload.ClientID)
}

func TestFileWatchWS_EmptyDirResolvesToProjectRoot(t *testing.T) {
	env, server, cleanup := setupFileWatchWSTest(t)
	defer cleanup()

	require.NoError(t, service.InitFileWatcher())
	defer service.StopFileWatcher()

	conn := dialFileWatchWS(t, server.URL, env.ProjectDir, "?dir=")
	defer conn.CloseNow()
	readWatchFrame(t, conn) // connected

	// Creating a file in the project root must produce dir_change.
	require.NoError(t, os.WriteFile(filepath.Join(env.ProjectDir, "newfile.txt"), []byte("x"), 0o644))

	waitForWatchEventType(t, conn, "dir_change")
}

func TestFileWatchWS_DirChangeEvent(t *testing.T) {
	env, server, cleanup := setupFileWatchWSTest(t)
	defer cleanup()

	require.NoError(t, service.InitFileWatcher())
	defer service.StopFileWatcher()

	conn := dialFileWatchWS(t, server.URL, env.ProjectDir, "?dir=.")
	defer conn.CloseNow()
	readWatchFrame(t, conn) // connected

	require.NoError(t, os.WriteFile(filepath.Join(env.ProjectDir, "new_in_dir.txt"), []byte("new"), 0o644))

	data := waitForWatchEventType(t, conn, "dir_change")
	var payload struct {
		Type string `json:"type"`
		Path string `json:"path"`
	}
	require.NoError(t, json.Unmarshal(data, &payload))
	assert.Equal(t, "dir_change", payload.Type)
	assert.Contains(t, payload.Path, "new_in_dir.txt")
}

func TestFileWatchWS_FileChangeEvent(t *testing.T) {
	env, server, cleanup := setupFileWatchWSTest(t)
	defer cleanup()

	require.NoError(t, service.InitFileWatcher())
	defer service.StopFileWatcher()

	testFile := filepath.Join(env.ProjectDir, "watchme.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("initial"), 0o644))

	conn := dialFileWatchWS(t, server.URL, env.ProjectDir, "?dir=.&file=watchme.txt")
	defer conn.CloseNow()
	readWatchFrame(t, conn) // connected

	require.NoError(t, os.WriteFile(testFile, []byte("modified"), 0o644))

	data := waitForWatchEventType(t, conn, "file_change")
	var payload struct {
		Type string `json:"type"`
		Path string `json:"path"`
	}
	require.NoError(t, json.Unmarshal(data, &payload))
	assert.Equal(t, "file_change", payload.Type)
	assert.Contains(t, payload.Path, "watchme.txt")
}

func TestFileWatchWS_FileRemoveEvent(t *testing.T) {
	env, server, cleanup := setupFileWatchWSTest(t)
	defer cleanup()

	require.NoError(t, service.InitFileWatcher())
	defer service.StopFileWatcher()

	testFile := filepath.Join(env.ProjectDir, "deleteme.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("bye"), 0o644))

	conn := dialFileWatchWS(t, server.URL, env.ProjectDir, "?dir=.&file=deleteme.txt")
	defer conn.CloseNow()
	readWatchFrame(t, conn) // connected

	require.NoError(t, os.Remove(testFile))

	data := waitForWatchEventType(t, conn, "file_change")
	var payload struct {
		Path string `json:"path"`
	}
	require.NoError(t, json.Unmarshal(data, &payload))
	assert.Contains(t, payload.Path, "deleteme.txt")
}

// ---------- FileWatchWS: control messages ----------

func TestFileWatchWS_WatchMessageUpdatesPaths(t *testing.T) {
	env, server, cleanup := setupFileWatchWSTest(t)
	defer cleanup()

	require.NoError(t, service.InitFileWatcher())
	defer service.StopFileWatcher()

	testFile := filepath.Join(env.ProjectDir, "retarget.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("v1"), 0o644))

	// Start watching only the directory, then re-target onto the file.
	conn := dialFileWatchWS(t, server.URL, env.ProjectDir, "?dir=")
	defer conn.CloseNow()
	readWatchFrame(t, conn) // connected

	sendWatchMessage(t, conn, fileWatchMessage{Type: watchMsgWatch, Dir: ".", File: "retarget.txt"})

	// Give the server a moment to apply the update before touching the file.
	time.Sleep(200 * time.Millisecond)
	require.NoError(t, os.WriteFile(testFile, []byte("v2"), 0o644))

	data := waitForWatchEventType(t, conn, "file_change")
	var payload struct {
		Path string `json:"path"`
	}
	require.NoError(t, json.Unmarshal(data, &payload))
	assert.Contains(t, payload.Path, "retarget.txt")
}

func TestFileWatchWS_WatchMessageTraversalRejected(t *testing.T) {
	env, server, cleanup := setupFileWatchWSTest(t)
	defer cleanup()

	require.NoError(t, service.InitFileWatcher())
	defer service.StopFileWatcher()

	conn := dialFileWatchWS(t, server.URL, env.ProjectDir, "?dir=.")
	defer conn.CloseNow()
	readWatchFrame(t, conn) // connected

	sendWatchMessage(t, conn, fileWatchMessage{Type: watchMsgWatch, Dir: "../../../etc"})

	typ, data := readWatchFrame(t, conn)
	require.Equal(t, watchMsgError, typ)
	var payload struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(data, &payload))
	assert.Equal(t, "AccessDenied", payload.Code)

	// The socket must survive a rejected update: a later valid watch still works.
	// Send the valid watch and let it apply BEFORE touching the filesystem —
	// UpdateWatch cancels this client's pending debounce timers, so creating the
	// file first would race the 200ms debounce and drop the event.
	sendWatchMessage(t, conn, fileWatchMessage{Type: watchMsgWatch, Dir: "."})
	time.Sleep(200 * time.Millisecond)
	require.NoError(t, os.WriteFile(filepath.Join(env.ProjectDir, "after_reject.txt"), []byte("x"), 0o644))
	waitForWatchEventType(t, conn, "dir_change")
}

func TestFileWatchWS_InvalidJSONIgnored(t *testing.T) {
	env, server, cleanup := setupFileWatchWSTest(t)
	defer cleanup()

	require.NoError(t, service.InitFileWatcher())
	defer service.StopFileWatcher()

	conn := dialFileWatchWS(t, server.URL, env.ProjectDir, "?dir=.")
	defer conn.CloseNow()
	readWatchFrame(t, conn) // connected

	// A malformed frame must not tear down a healthy channel.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, conn.Write(ctx, websocket.MessageText, []byte("not json")))
	cancel()

	require.NoError(t, os.WriteFile(filepath.Join(env.ProjectDir, "still_alive.txt"), []byte("x"), 0o644))
	waitForWatchEventType(t, conn, "dir_change")
}

func TestFileWatchWS_PingAndPongKeepsConnectionUsable(t *testing.T) {
	env, server, cleanup := setupFileWatchWSTest(t)
	defer cleanup()

	origInterval := watchPingInterval
	watchPingInterval = 50 * time.Millisecond
	defer func() { watchPingInterval = origInterval }()

	require.NoError(t, service.InitFileWatcher())
	defer service.StopFileWatcher()

	conn := dialFileWatchWS(t, server.URL, env.ProjectDir, "?dir=.")
	defer conn.CloseNow()
	readWatchFrame(t, conn) // connected

	waitForWatchEventType(t, conn, watchMsgPing)

	// Answering the ping must keep the connection usable.
	sendWatchMessage(t, conn, fileWatchMessage{Type: watchMsgPong})
	require.NoError(t, os.WriteFile(filepath.Join(env.ProjectDir, "after_pong.txt"), []byte("x"), 0o644))
	waitForWatchEventType(t, conn, "dir_change")
}

func TestFileWatchWS_ConcurrentWritesNoRace(t *testing.T) {
	env, server, cleanup := setupFileWatchWSTest(t)
	defer cleanup()

	origInterval := watchPingInterval
	watchPingInterval = 20 * time.Millisecond
	defer func() { watchPingInterval = origInterval }()

	require.NoError(t, service.InitFileWatcher())
	defer service.StopFileWatcher()

	conn := dialFileWatchWS(t, server.URL, env.ProjectDir, "?dir=.")
	defer conn.CloseNow()
	readWatchFrame(t, conn) // connected

	// Burst-create files so the event fan-out and the ping ticker write
	// concurrently; every frame must remain valid JSON.
	for i := range 30 {
		name := filepath.Join(env.ProjectDir, "burst_"+string(rune('a'+i%26))+".txt")
		require.NoError(t, os.WriteFile(name, []byte("x"), 0o644))
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Until(deadline))
		_, data, err := conn.Read(ctx)
		cancel()
		if err != nil {
			break // drained
		}
		assert.True(t, json.Valid(data), "frame is not valid JSON: %s", data)
	}
}

// ---------- FileWatchWS: lifecycle ----------

func TestFileWatchWS_ClientDisconnectRunsTeardown(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	require.NoError(t, service.InitFileWatcher())
	defer service.StopFileWatcher()

	handlerReturned := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/file/watch/ws", middleware.Auth(func(w http.ResponseWriter, r *http.Request) {
		FileWatchWS(w, r)
		close(handlerReturned)
	}))
	server := httptest.NewServer(mux)
	defer server.Close()

	conn := dialFileWatchWS(t, server.URL, env.ProjectDir, "?dir=")
	readWatchFrame(t, conn) // connected

	// Closing the client must make the handler return (and thus run its
	// deferred UnregisterClient) rather than leak the goroutine.
	_ = conn.CloseNow()

	select {
	case <-handlerReturned:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not return after client disconnect")
	}
}

func TestFileWatchWS_StopWatcherClosesChannel(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	require.NoError(t, service.InitFileWatcher())

	handlerReturned := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/file/watch/ws", middleware.Auth(func(w http.ResponseWriter, r *http.Request) {
		FileWatchWS(w, r)
		close(handlerReturned)
	}))
	server := httptest.NewServer(mux)
	defer server.Close()

	conn := dialFileWatchWS(t, server.URL, env.ProjectDir, "?dir=")
	defer conn.CloseNow()
	readWatchFrame(t, conn) // connected

	// Shutting the watcher down closes the push channel; the handler must exit
	// cleanly (and its deferred UnregisterClient must not double-close).
	service.StopFileWatcher()

	select {
	case <-handlerReturned:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not return after StopFileWatcher closed the push channel")
	}
}

func TestFileWatchWS_ConnectionLimit(t *testing.T) {
	env, server, cleanup := setupFileWatchWSTest(t)
	defer cleanup()

	require.NoError(t, service.InitFileWatcher())
	defer service.StopFileWatcher()

	// Occupy the full allowance.
	conns := make([]*websocket.Conn, 0, maxWatchClients)
	defer func() {
		for _, c := range conns {
			_ = c.CloseNow()
		}
	}()
	for range maxWatchClients {
		conn := dialFileWatchWS(t, server.URL, env.ProjectDir, "?dir=")
		readWatchFrame(t, conn) // connected
		conns = append(conns, conn)
	}

	// The next connection must be rejected before the upgrade.
	_, resp, err := dialFileWatchWSRaw(t, server.URL, env.ProjectDir, "?dir=")
	require.Error(t, err, "expected the over-limit connection to be rejected")
	if resp != nil {
		defer resp.Body.Close()
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	}
}

// ---------- resolveWatchMediaPaths ----------

func TestResolveWatchMediaPaths(t *testing.T) {
	project := t.TempDir()

	t.Run("empty input yields nil", func(t *testing.T) {
		assert.Nil(t, resolveWatchMediaPaths(project, nil))
		assert.Nil(t, resolveWatchMediaPaths(project, []string{}))
	})

	t.Run("relative paths resolve under the project", func(t *testing.T) {
		got := resolveWatchMediaPaths(project, []string{"assets/a.png", "docs/b.png"})
		assert.Equal(t, []string{
			filepath.Join(project, "assets", "a.png"),
			filepath.Join(project, "docs", "b.png"),
		}, got)
	})

	t.Run("escaping paths are dropped individually", func(t *testing.T) {
		got := resolveWatchMediaPaths(project, []string{"../../../etc/passwd", "assets/ok.png"})
		assert.Equal(t, []string{filepath.Join(project, "assets", "ok.png")}, got,
			"one bad entry must not discard the valid ones")
	})

	t.Run("empty entries are skipped", func(t *testing.T) {
		got := resolveWatchMediaPaths(project, []string{"", "assets/a.png", ""})
		assert.Equal(t, []string{filepath.Join(project, "assets", "a.png")}, got)
	})

	t.Run("duplicates collapse", func(t *testing.T) {
		got := resolveWatchMediaPaths(project, []string{"assets/a.png", "assets/a.png"})
		assert.Equal(t, []string{filepath.Join(project, "assets", "a.png")}, got)
	})

	t.Run("list is capped", func(t *testing.T) {
		rels := make([]string, service.MaxMediaWatchPaths+25)
		for i := range rels {
			rels[i] = fmt.Sprintf("assets/img-%d.png", i)
		}
		got := resolveWatchMediaPaths(project, rels)
		assert.Len(t, got, service.MaxMediaWatchPaths)
	})
}

// ---------- FileWatchWS: media watch frames ----------

// Registering media files over the control channel must arm the watcher for
// them: rewriting one then produces a file_change even though it is not the
// open file.
func TestFileWatchWS_MediaFilesReportChanges(t *testing.T) {
	env, server, cleanup := setupFileWatchWSTest(t)
	defer cleanup()

	require.NoError(t, service.InitFileWatcher())
	defer service.StopFileWatcher()

	assetsDir := filepath.Join(env.ProjectDir, "assets")
	require.NoError(t, os.MkdirAll(assetsDir, 0o755))
	img := filepath.Join(assetsDir, "diagram.png")
	require.NoError(t, os.WriteFile(img, []byte("v1"), 0o644))

	// Watch only the root dir; the image lives in a subdirectory that is not the
	// browsed directory, so nothing but the media registration can catch it.
	conn := dialFileWatchWS(t, server.URL, env.ProjectDir, "?dir=")
	defer conn.CloseNow()
	readWatchFrame(t, conn) // connected

	sendWatchMessage(t, conn, fileWatchMessage{
		Type:  watchMsgWatch,
		Dir:   ".",
		Files: []string{"assets/diagram.png"},
	})
	// Let the server apply the watch before touching the file.
	time.Sleep(300 * time.Millisecond)

	require.NoError(t, os.WriteFile(img, []byte("v2"), 0o644))

	data := waitForWatchEventType(t, conn, "file_change")
	var payload struct {
		Type string `json:"type"`
		Path string `json:"path"`
	}
	require.NoError(t, json.Unmarshal(data, &payload))
	assert.Equal(t, "file_change", payload.Type)
	assert.Contains(t, payload.Path, "diagram.png")
}

// A media path escaping the project root must be dropped without rejecting the
// frame — the rest of the watch (including the open file) must stay armed.
func TestFileWatchWS_MediaPathTraversalDroppedNotFatal(t *testing.T) {
	env, server, cleanup := setupFileWatchWSTest(t)
	defer cleanup()

	require.NoError(t, service.InitFileWatcher())
	defer service.StopFileWatcher()

	watched := filepath.Join(env.ProjectDir, "watched.txt")
	require.NoError(t, os.WriteFile(watched, []byte("v1"), 0o644))

	conn := dialFileWatchWS(t, server.URL, env.ProjectDir, "?dir=")
	defer conn.CloseNow()
	readWatchFrame(t, conn) // connected

	sendWatchMessage(t, conn, fileWatchMessage{
		Type:  watchMsgWatch,
		Dir:   ".",
		File:  "watched.txt",
		Files: []string{"../../../etc/passwd"},
	})
	time.Sleep(300 * time.Millisecond)

	// The frame was accepted (no error frame) and the open file is still watched.
	require.NoError(t, os.WriteFile(watched, []byte("v2"), 0o644))

	data := waitForWatchEventType(t, conn, "file_change")
	var payload struct {
		Path string `json:"path"`
	}
	require.NoError(t, json.Unmarshal(data, &payload))
	assert.Contains(t, payload.Path, "watched.txt")
}

// ---------- resolveWatchPaths ----------

func TestResolveWatchPaths(t *testing.T) {
	project := t.TempDir()

	t.Run("empty dir resolves to project root", func(t *testing.T) {
		dirAbs, fileAbs, ok := resolveWatchPaths(project, "", "")
		assert.True(t, ok)
		assert.Equal(t, project, dirAbs)
		assert.Empty(t, fileAbs)
	})

	t.Run("relative dir and file resolve under project", func(t *testing.T) {
		dirAbs, fileAbs, ok := resolveWatchPaths(project, "sub", "sub/f.txt")
		assert.True(t, ok)
		assert.Equal(t, filepath.Join(project, "sub"), dirAbs)
		assert.Equal(t, filepath.Join(project, "sub", "f.txt"), fileAbs)
	})

	t.Run("traversal is rejected", func(t *testing.T) {
		_, _, ok := resolveWatchPaths(project, "../../../etc", "")
		assert.False(t, ok)
	})

	t.Run("file traversal is rejected", func(t *testing.T) {
		_, _, ok := resolveWatchPaths(project, "", "../../../etc/passwd")
		assert.False(t, ok)
	})
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
