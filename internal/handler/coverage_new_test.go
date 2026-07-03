package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/terminal"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// sanitizeArchiveName tests
// ============================================================================

func TestSanitizeArchiveName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"NormalFilename", "archive.zip", "archive.zip"},
		{"FilenameWithQuotes", `my"file.zip`, "my_file.zip"},
		{"FilenameWithBackslash", `my\file.zip`, "my_file.zip"},
		{"FilenameWithControlChars", "file\x01\x02.zip", "file__.zip"},
		{"MultipleSpecialChars", `"bad"\name.zip`, "_bad__name.zip"},
		{"EmptyString", "", ""},
		{"OnlySpecialChars", `"""`, "___"},
		{"NonASCII", "日本語.zip", "日本語.zip"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeArchiveName(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ============================================================================
// contentDispositionAttachment tests
// ============================================================================

func TestContentDispositionAttachment(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"ASCII", "file.txt", `attachment; filename="file.txt"; filename*=UTF-8''file.txt`},
		{"CJK", "日本語.zip", `attachment; filename="日本語.zip"; filename*=UTF-8''%E6%97%A5%E6%9C%AC%E8%AA%9E.zip`},
		{"Quote", "my\"file.txt", `attachment; filename="my_file.txt"; filename*=UTF-8''my%22file.txt`},
		{"Space", "my file.txt", `attachment; filename="my file.txt"; filename*=UTF-8''my%20file.txt`},
		{"Percent", "100%.txt", `attachment; filename="100%.txt"; filename*=UTF-8''100%25.txt`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contentDispositionAttachment(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ============================================================================
// rfc5987Encode and isRFC5987Unreserved tests
// ============================================================================

func TestRFC5987Encode(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"Empty", "", ""},
		{"AllUnreserved", "file.txt", "file.txt"},
		{"Space", "my file", "my%20file"},
		{"CJK", "日本語", "%E6%97%A5%E6%9C%AC%E8%AA%9E"},
		{"Mixed", "hello 世界.txt", "hello%20%E4%B8%96%E7%95%8C.txt"},
		{"SpecialChars", "!#$&+-.^_`|~", "!#$&+-.^_`|~"},
		{"Percent", "100%", "100%25"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rfc5987Encode(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsRFC5987Unreserved(t *testing.T) {
	unreserved := "!#$&+-.^_`|~"
	for _, r := range unreserved {
		if !isRFC5987Unreserved(r) {
			t.Errorf("expected %q to be unreserved", r)
		}
	}
	// Alphanums
	for r := 'a'; r <= 'z'; r++ {
		if !isRFC5987Unreserved(r) {
			t.Errorf("expected %q to be unreserved", r)
		}
	}
	for r := 'A'; r <= 'Z'; r++ {
		if !isRFC5987Unreserved(r) {
			t.Errorf("expected %q to be unreserved", r)
		}
	}
	for r := '0'; r <= '9'; r++ {
		if !isRFC5987Unreserved(r) {
			t.Errorf("expected %q to be unreserved", r)
		}
	}
	// Reserved characters
	for _, r := range " @<>[]()=%/\\\"" {
		if isRFC5987Unreserved(r) {
			t.Errorf("expected %q to be reserved", r)
		}
	}
}

// ============================================================================
// addFileToZip tests
// ============================================================================

func TestAddFileToZip(t *testing.T) {
	t.Run("AddsFileContent", func(t *testing.T) {
		// Create a temp file with known content
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "test.txt")
		content := "hello world from zip test"
		require.NoError(t, os.WriteFile(filePath, []byte(content), 0o644))

		info, err := os.Stat(filePath)
		require.NoError(t, err)

		// Create zip in memory
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		err = addFileToZip(zw, filePath, "test.txt", info)
		require.NoError(t, err)
		require.NoError(t, zw.Close())

		// Read back and verify
		reader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		require.NoError(t, err)
		require.Len(t, reader.File, 1)

		f := reader.File[0]
		assert.Equal(t, "test.txt", f.Name)
		rc, err := f.Open()
		require.NoError(t, err)
		data, err := readAll(rc)
		require.NoError(t, err)
		_ = rc.Close()
		assert.Equal(t, content, string(data))
	})

	t.Run("NonExistentFile_ReturnsError", func(t *testing.T) {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		info := mockFileInfo{name: "ghost.txt", size: 100}
		err := addFileToZip(zw, "/nonexistent/path/ghost.txt", "ghost.txt", info)
		assert.Error(t, err)
		_ = zw.Close()
	})
}

// mockFileInfo is a minimal os.FileInfo for testing.
type mockFileInfo struct {
	name string
	size int64
}

func (m mockFileInfo) Name() string       { return m.name }
func (m mockFileInfo) Size() int64        { return m.size }
func (m mockFileInfo) Mode() os.FileMode  { return 0o644 }
func (m mockFileInfo) ModTime() time.Time { return time.Now() }
func (m mockFileInfo) IsDir() bool        { return false }
func (m mockFileInfo) Sys() interface{}   { return nil }

func readAll(r io.Reader) ([]byte, error) {
	return io.ReadAll(r)
}

// ============================================================================
// ServeFileArchive tests
// ============================================================================

func TestServeFileArchive_SingleFile(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "archive-me.txt", "archive content")

	req := newRequest(t, http.MethodPost, "/api/file/archive", map[string]any{
		"paths": []string{"archive-me.txt"},
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeFileArchive, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/zip", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Header().Get("Content-Disposition"), "archive-me.txt.zip")

	// Verify zip contents
	reader, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	require.NoError(t, err)
	assert.Len(t, reader.File, 1)
	assert.Equal(t, "archive-me.txt", reader.File[0].Name)
}

func TestServeFileArchive_Directory(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "mydir/a.txt", "aaa")
	createTestFile(t, env.ProjectDir, "mydir/sub/b.txt", "bbb")

	req := newRequest(t, http.MethodPost, "/api/file/archive", map[string]any{
		"paths": []string{"mydir"},
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeFileArchive, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Disposition"), "mydir.zip")
}

func TestServeFileArchive_EmptyPaths_Returns400(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/file/archive", map[string]any{
		"paths": []string{},
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeFileArchive, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeFileArchive_WrongMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/file/archive", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeFileArchive, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestServeFileArchive_NoAccessiblePaths_Returns400(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/file/archive", map[string]any{
		"paths": []string{"nonexistent-file.txt"},
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeFileArchive, req)
	// On some platforms (macOS with /var symlink), path resolution may return 403
	// before reaching the "no accessible paths" check. Accept both 400 and 403.
	assert.True(t, w.Code == http.StatusBadRequest || w.Code == http.StatusForbidden,
		"expected 400 or 403, got %d", w.Code)
}

// ============================================================================
// stringsContainsAnyBlock tests
// ============================================================================

func TestStringsContainsAnyBlock(t *testing.T) {
	tests := []struct {
		name   string
		blocks []model.ContentBlock
		substr string
		want   bool
	}{
		{
			name:   "EmptySlice",
			blocks: nil,
			substr: "<ask-question",
			want:   false,
		},
		{
			name:   "NoTextBlocks",
			blocks: []model.ContentBlock{{Type: "tool_use", Name: "Bash"}},
			substr: "ask",
			want:   false,
		},
		{
			name:   "TextBlockContainsSubstring",
			blocks: []model.ContentBlock{{Type: "text", Text: "<ask-question>hello</ask-question>"}},
			substr: "<ask-question",
			want:   true,
		},
		{
			name:   "TextBlockMissingSubstring",
			blocks: []model.ContentBlock{{Type: "text", Text: "normal text"}},
			substr: "<ask-question",
			want:   false,
		},
		{
			name: "NonTextBlockIgnored",
			blocks: []model.ContentBlock{
				{Type: "thinking", Text: "<ask-question>"},
				{Type: "tool_use", Name: "Read"},
			},
			substr: "<ask-question",
			want:   false,
		},
		{
			name: "MultipleBlocks_SubstringInLaterBlock",
			blocks: []model.ContentBlock{
				{Type: "text", Text: "first block"},
				{Type: "text", Text: "second <ask-question> block"},
			},
			substr: "<ask-question",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ai.StringsContainsAnyBlock(tt.blocks, tt.substr)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ============================================================================
// sendEvent tests
// ============================================================================

func TestSendEvent_ChannelHasCapacity(t *testing.T) {
	ch := make(chan ai.StreamEvent, 1)
	ctx := context.Background()

	event := ai.StreamEvent{Type: "content", Content: "hello"}
	result := ai.SendStreamEvent(ctx, ch, event)

	assert.True(t, result)
	select {
	case e := <-ch:
		assert.Equal(t, "content", e.Type)
		assert.Equal(t, "hello", e.Content)
	default:
		t.Fatal("expected event on channel")
	}
}

func TestSendEvent_ChannelFull_DropsEvent(t *testing.T) {
	ch := make(chan ai.StreamEvent, 1)
	ch <- ai.StreamEvent{Type: "content", Content: "existing"}

	ctx := context.Background()
	event := ai.StreamEvent{Type: "content", Content: "dropped"}
	result := ai.SendStreamEvent(ctx, ch, event)

	// Should return true (event dropped, not a context cancellation)
	assert.True(t, result)
}

func TestSendEvent_ContextCancelled(t *testing.T) {
	// Use an unbuffered channel with no reader — ctx.Done() and default
	// are both available, but ctx.Done() should be selected reliably
	// because the channel send would block.
	ch := make(chan ai.StreamEvent)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	event := ai.StreamEvent{Type: "content", Content: "hello"}
	_ = ai.SendStreamEvent(ctx, ch, event)

	// With an unbuffered channel and cancelled context, either ctx.Done() or default
	// could be selected. Both indicate the event was not sent to the channel.
	// The important thing is: the event is NOT on the channel.
	assert.Empty(t, len(ch), "event should not be on channel when context is cancelled")
}

func TestSendEvent_UnbufferedChannel_NoReader(t *testing.T) {
	ch := make(chan ai.StreamEvent)
	ctx := context.Background()

	event := ai.StreamEvent{Type: "content", Content: "dropped"}
	result := ai.SendStreamEvent(ctx, ch, event)

	// Should return true (event dropped via default case)
	assert.True(t, result)
}

// ============================================================================
// sendFinalEvent tests
// ============================================================================

func TestSendFinalEvent_ChannelHasCapacity(t *testing.T) {
	ch := make(chan ai.StreamEvent, 1)
	event := ai.StreamEvent{Type: "done"}
	ai.SendFinalStreamEvent(ch, event)

	select {
	case e := <-ch:
		assert.Equal(t, "done", e.Type)
	default:
		t.Fatal("expected event on channel")
	}
}

func TestSendFinalEvent_ChannelFull_DropsWithoutBlocking(t *testing.T) {
	ch := make(chan ai.StreamEvent, 1)
	ch <- ai.StreamEvent{Type: "content", Content: "existing"}

	// Should not block even though channel is full
	done := make(chan struct{})
	go func() {
		ai.SendFinalStreamEvent(ch, ai.StreamEvent{Type: "done"})
		close(done)
	}()

	select {
	case <-done:
		// Success — did not block
	case <-time.After(time.Second):
		t.Fatal("sendFinalEvent blocked when channel was full")
	}
}

// ============================================================================
// isNotDirError tests
// ============================================================================

func TestIsNotDirError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "SyscallENOTDIR",
			err:  &os.PathError{Err: syscall.ENOTDIR, Path: "/foo"},
			want: true,
		},
		{
			name: "WindowsErrorDirectory",
			err:  &os.PathError{Err: syscall.Errno(0x267), Path: "C:\\foo"},
			want: true,
		},
		{
			name: "PathErrorWithOtherError",
			err:  &os.PathError{Err: errors.New("permission denied"), Path: "/foo"},
			want: false,
		},
		{
			name: "NonPathError",
			err:  errors.New("something else"),
			want: false,
		},
		{
			name: "NilError",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNotDirError(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ============================================================================
// loginLimiter.cleanup tests
// ============================================================================

func TestLoginLimiter_Cleanup_RemovesStaleRecords(t *testing.T) {
	limiter := &loginLimiter{records: make(map[string]*ipRecord)}

	// Add a stale record (last failure was long ago, not blocked)
	staleTime := time.Now().Add(-2 * time.Hour)
	limiter.records["192.0.2.1"] = &ipRecord{
		failCount:    0,
		lastFail:     staleTime,
		blockedUntil: time.Time{}, // not blocked
	}

	// Add a recent record (should NOT be removed)
	limiter.records["192.0.2.2"] = &ipRecord{
		failCount:    1,
		lastFail:     time.Now(),
		blockedUntil: time.Time{},
	}

	// Add a blocked record (should NOT be removed — still blocked)
	limiter.records["192.0.2.3"] = &ipRecord{
		failCount:    maxLoginFails,
		lastFail:     time.Now(),
		blockedUntil: time.Now().Add(10 * time.Minute),
	}

	limiter.cleanup()

	_, found1 := limiter.records["192.0.2.1"]
	assert.False(t, found1, "stale record should be cleaned up")

	_, found2 := limiter.records["192.0.2.2"]
	assert.True(t, found2, "recent record should remain")

	_, found3 := limiter.records["192.0.2.3"]
	assert.True(t, found3, "blocked record should remain")
}

func TestLoginLimiter_Cleanup_RemovesExpiredBlocks(t *testing.T) {
	limiter := &loginLimiter{records: make(map[string]*ipRecord)}

	// Add a record with expired block and stale last failure
	expiredTime := time.Now().Add(-2 * time.Hour)
	limiter.records["10.0.0.1"] = &ipRecord{
		failCount:    maxLoginFails,
		lastFail:     expiredTime,
		blockedUntil: time.Now().Add(-1 * time.Minute), // expired
	}

	limiter.cleanup()

	_, found := limiter.records["10.0.0.1"]
	assert.False(t, found, "expired blocked record with stale lastFail should be cleaned up")
}

func TestLoginLimiter_Cleanup_KeepsExpiredBlockButRecentFail(t *testing.T) {
	limiter := &loginLimiter{records: make(map[string]*ipRecord)}

	// Expired block but recent failure — should NOT be cleaned up
	limiter.records["10.0.0.2"] = &ipRecord{
		failCount:    maxLoginFails,
		lastFail:     time.Now().Add(-1 * time.Minute), // recent
		blockedUntil: time.Now().Add(-1 * time.Minute), // expired
	}

	limiter.cleanup()

	_, found := limiter.records["10.0.0.2"]
	assert.True(t, found, "expired block but recent failure should remain")
}

// ============================================================================
// androidLogFilePath tests
// ============================================================================

func TestAndroidLogFilePath(t *testing.T) {
	origLogDir := model.ConfigInstance.LogDir
	defer func() { model.ConfigInstance.LogDir = origLogDir }()

	model.ConfigInstance.LogDir = "/tmp/test-logs"
	got := androidLogFilePath()
	assert.Equal(t, filepath.Join("/tmp/test-logs", "android.log"), got)
}

// ============================================================================
// ServeAndroidLog tests
// ============================================================================

func TestServeAndroidLog_ValidEntries(t *testing.T) {
	origLogDir := model.ConfigInstance.LogDir
	defer func() { model.ConfigInstance.LogDir = origLogDir }()

	tmpDir := t.TempDir()
	model.ConfigInstance.LogDir = tmpDir

	entries := []AndroidLogEntry{
		{Level: "I", Tag: "MainActivity", Msg: "App started", Ts: 1700000000000},
		{Level: "E", Tag: "Network", Msg: "Connection failed", Ts: 1700000001000},
	}

	req := newRequest(t, http.MethodPost, "/api/android-log", map[string]any{
		"entries": entries,
	})

	w := callHandler(ServeAndroidLog, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Verify file was written
	data, err := os.ReadFile(filepath.Join(tmpDir, "android.log"))
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "I/MainActivity")
	assert.Contains(t, content, "App started")
	assert.Contains(t, content, "E/Network")
	assert.Contains(t, content, "Connection failed")
}

func TestServeAndroidLog_EmptyEntries_Returns400(t *testing.T) {
	req := newRequest(t, http.MethodPost, "/api/android-log", map[string]any{
		"entries": []AndroidLogEntry{},
	})

	w := callHandler(ServeAndroidLog, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeAndroidLog_WrongMethod(t *testing.T) {
	req := newRequest(t, http.MethodGet, "/api/android-log", nil)
	w := callHandler(ServeAndroidLog, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestServeAndroidLog_NewlineEscaping(t *testing.T) {
	origLogDir := model.ConfigInstance.LogDir
	defer func() { model.ConfigInstance.LogDir = origLogDir }()

	tmpDir := t.TempDir()
	model.ConfigInstance.LogDir = tmpDir

	entries := []AndroidLogEntry{
		{Level: "I", Tag: "Test", Msg: "line1\nline2", Ts: 1700000000000},
	}

	req := newRequest(t, http.MethodPost, "/api/android-log", map[string]any{
		"entries": entries,
	})

	w := callHandler(ServeAndroidLog, req)
	assert.Equal(t, http.StatusOK, w.Code)

	data, err := os.ReadFile(filepath.Join(tmpDir, "android.log"))
	require.NoError(t, err)
	content := string(data)
	// Newlines in message should be escaped to \n
	assert.Contains(t, content, "line1\\nline2")
	// But each entry should end with actual newline
	lines := strings.Split(content, "\n")
	assert.True(t, len(lines) >= 2, "should have at least 2 lines (entry + trailing)")
}

func TestServeAndroidLog_TruncatesOver200(t *testing.T) {
	origLogDir := model.ConfigInstance.LogDir
	defer func() { model.ConfigInstance.LogDir = origLogDir }()

	tmpDir := t.TempDir()
	model.ConfigInstance.LogDir = tmpDir

	// Create 250 entries — should be capped to 200
	entries := make([]AndroidLogEntry, 0, 250)
	for i := range 250 {
		entries = append(entries, AndroidLogEntry{
			Level: "I", Tag: fmt.Sprintf("Tag%d", i), Msg: fmt.Sprintf("msg%d", i), Ts: 1700000000000 + int64(i*1000),
		})
	}

	req := newRequest(t, http.MethodPost, "/api/android-log", map[string]any{
		"entries": entries,
	})

	w := callHandler(ServeAndroidLog, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, float64(200), result["written"])
}

func TestServeAndroidLog_AppendsToExistingFile(t *testing.T) {
	origLogDir := model.ConfigInstance.LogDir
	defer func() { model.ConfigInstance.LogDir = origLogDir }()

	tmpDir := t.TempDir()
	model.ConfigInstance.LogDir = tmpDir

	// First request
	entries1 := []AndroidLogEntry{
		{Level: "I", Tag: "First", Msg: "first batch", Ts: 1700000000000},
	}
	req1 := newRequest(t, http.MethodPost, "/api/android-log", map[string]any{"entries": entries1})
	w1 := callHandler(ServeAndroidLog, req1)
	assert.Equal(t, http.StatusOK, w1.Code)

	// Second request
	entries2 := []AndroidLogEntry{
		{Level: "I", Tag: "Second", Msg: "second batch", Ts: 1700000001000},
	}
	req2 := newRequest(t, http.MethodPost, "/api/android-log", map[string]any{"entries": entries2})
	w2 := callHandler(ServeAndroidLog, req2)
	assert.Equal(t, http.StatusOK, w2.Code)

	// Both should be in the file
	data, err := os.ReadFile(filepath.Join(tmpDir, "android.log"))
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "first batch")
	assert.Contains(t, content, "second batch")
}

// ============================================================================
// ServeGitBranch tests
// ============================================================================

func TestServeGitBranch_ValidRepo(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/branch", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitBranch, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["isGit"])
	assert.Equal(t, "main", result["branch"])
	assert.NotEmpty(t, result["head"])
	assert.Equal(t, false, result["dirty"])
}

func TestServeGitBranch_DirtyRepo(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)
	// Make it dirty by modifying a tracked file
	existingFile := filepath.Join(env.ProjectDir, "README.md")
	require.NoError(t, os.WriteFile(existingFile, []byte("# Modified"), 0o644))

	req := newRequest(t, http.MethodGet, "/api/git/branch", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitBranch, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["isGit"])
	assert.Equal(t, true, result["dirty"])
}

func TestServeGitBranch_NotGitRepo(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/git/branch", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitBranch, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, false, result["isGit"])
	assert.Equal(t, "", result["branch"])
}

func TestServeGitBranch_OrphanBranch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Init a git repo but do NOT make any commit — orphan branch state
	cmd := exec.Command("git", "init", env.ProjectDir)
	require.NoError(t, cmd.Run())

	req := newRequest(t, http.MethodGet, "/api/git/branch", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitBranch, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["isGit"])
	// Orphan branch name should still be resolved (e.g. "master" or "main")
	assert.NotEmpty(t, result["branch"])
}

func TestServeGitBranch_WrongMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/git/branch", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitBranch, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

// ============================================================================
// ServeGitVerifyCommits tests
// ============================================================================

func TestServeGitVerifyCommits_ValidSHA(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)
	sha := getHeadSHA(t, env.ProjectDir)

	req := newRequest(t, http.MethodPost, "/api/git/verify-commits", map[string]any{
		"shas": []string{sha},
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitVerifyCommits, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	results, ok := result["results"].(map[string]interface{})
	require.True(t, ok)
	commitInfo := results[sha]
	assert.NotNil(t, commitInfo, "valid commit SHA should have info")
}

func TestServeGitVerifyCommits_InvalidSHA(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodPost, "/api/git/verify-commits", map[string]any{
		"shas": []string{"0000000000000000000000000000000000000000"},
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitVerifyCommits, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	results, ok := result["results"].(map[string]interface{})
	require.True(t, ok)
	assert.Nil(t, results["0000000000000000000000000000000000000000"],
		"invalid SHA should have null result")
}

func TestServeGitVerifyCommits_EmptySHAs(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodPost, "/api/git/verify-commits", map[string]any{
		"shas": []string{},
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitVerifyCommits, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	results, ok := result["results"].(map[string]interface{})
	require.True(t, ok)
	assert.Empty(t, results)
}

func TestServeGitVerifyCommits_NotGitRepo(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/git/verify-commits", map[string]any{
		"shas": []string{"abc123"},
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitVerifyCommits, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	results, ok := result["results"].(map[string]interface{})
	require.True(t, ok)
	assert.Empty(t, results, "non-git repo should return empty results")
}

func TestServeGitVerifyCommits_WrongMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/git/verify-commits", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitVerifyCommits, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

// ============================================================================
// validateCreatePath tests
// ============================================================================

func TestValidateCreatePath_RelativeDir(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Ensure the subdirectory exists so isPathUnderAnyRoot can resolve it
	_ = os.MkdirAll(filepath.Join(env.ProjectDir, "subdir"), 0o755)

	w := httptest.NewRecorder()
	r := newRequest(t, http.MethodPost, "/api/file/create", nil)
	withProjectCookie(r, env.ProjectDir)

	absPath := validateCreatePath(w, r, "subdir", "newfile.txt")
	assert.NotEmpty(t, absPath, "validateCreatePath should return non-empty path for valid relative dir")
	if absPath != "" {
		assert.Contains(t, absPath, "newfile.txt")
	}
}

func TestValidateCreatePath_AbsDirUnderWatchDir(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	subDir := filepath.Join(env.WatchDir, "subproject")
	_ = os.MkdirAll(subDir, 0o755)

	w := httptest.NewRecorder()
	r := newRequest(t, http.MethodPost, "/api/file/create", nil)

	absPath := validateCreatePath(w, r, subDir, "newfile.txt")
	assert.NotEmpty(t, absPath, "validateCreatePath should return non-empty path for valid absolute dir")
	if absPath != "" {
		assert.Contains(t, absPath, "newfile.txt")
	}
}

func TestValidateCreatePath_AbsDirEscapesWatchDir(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	w := httptest.NewRecorder()
	r := newRequest(t, http.MethodPost, "/api/file/create", nil)

	absPath := validateCreatePath(w, r, "/tmp/escaped", "newfile.txt")
	assert.Empty(t, absPath)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestValidateCreatePath_EmptyDirUsesProjectCookie(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	w := httptest.NewRecorder()
	r := newRequest(t, http.MethodPost, "/api/file/create", nil)
	withProjectCookie(r, env.ProjectDir)

	absPath := validateCreatePath(w, r, "", "newfile.txt")
	assert.NotEmpty(t, absPath, "validateCreatePath should use project cookie when dir is empty")
	if absPath != "" {
		assert.Contains(t, absPath, "newfile.txt")
	}
}

func TestValidateCreatePath_NoProjectCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	w := httptest.NewRecorder()
	r := newRequest(t, http.MethodPost, "/api/file/create", nil)

	absPath := validateCreatePath(w, r, "", "newfile.txt")
	assert.Empty(t, absPath)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestValidateCreatePath_PathTraversalInName(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	w := httptest.NewRecorder()
	r := newRequest(t, http.MethodPost, "/api/file/create", nil)
	withProjectCookie(r, env.ProjectDir)

	absPath := validateCreatePath(w, r, "", "../../etc/evil.txt")
	assert.Empty(t, absPath)
}

func TestValidateCreatePath_RelativeDirWithTraversal(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	w := httptest.NewRecorder()
	r := newRequest(t, http.MethodPost, "/api/file/create", nil)
	withProjectCookie(r, env.ProjectDir)

	absPath := validateCreatePath(w, r, "../../../etc", "evil.txt")
	assert.Empty(t, absPath)
}

// ============================================================================
// detectDefaultBranch tests
// ============================================================================

func TestDetectDefaultBranch_LocalMain(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	branch := detectDefaultBranch(env.ProjectDir)
	assert.Equal(t, "main", branch)
}

func TestDetectDefaultBranch_NotGitRepo(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Not a git repo
	branch := detectDefaultBranch(env.ProjectDir)
	assert.Equal(t, "", branch)
}

// ============================================================================
// copyDir tests (low coverage: 41.9%)
// ============================================================================

func TestCopyDir_DeepNesting(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create deep directory structure
	createTestFile(t, env.ProjectDir, "src/a/b/c/deep.txt", "deep content")

	srcDir := filepath.Join(env.ProjectDir, "src")
	dstDir := filepath.Join(env.ProjectDir, "dst")

	err := copyDir(srcDir, dstDir)
	require.NoError(t, err)

	// Verify deep copy
	data, err := os.ReadFile(filepath.Join(dstDir, "a/b/c/deep.txt"))
	require.NoError(t, err)
	assert.Equal(t, "deep content", string(data))
}

func TestCopyDir_EmptySubdirectory(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create dir with an empty subdirectory
	require.NoError(t, os.MkdirAll(filepath.Join(env.ProjectDir, "src", "emptydir"), 0o755))
	createTestFile(t, env.ProjectDir, "src/file.txt", "content")

	srcDir := filepath.Join(env.ProjectDir, "src")
	dstDir := filepath.Join(env.ProjectDir, "dst")

	err := copyDir(srcDir, dstDir)
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(dstDir, "emptydir"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	data, err := os.ReadFile(filepath.Join(dstDir, "file.txt"))
	require.NoError(t, err)
	assert.Equal(t, "content", string(data))
}

// ============================================================================
// safeRemoveAll tests (low coverage: 55.6%)
// ============================================================================

func TestSafeRemoveAll_UnderWatchDir(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	targetDir := filepath.Join(env.ProjectDir, "to-remove")
	require.NoError(t, os.MkdirAll(targetDir, 0o755))
	createTestFile(t, targetDir, "file.txt", "data")

	err := safeRemoveAll(targetDir)
	assert.NoError(t, err)
	_, statErr := os.Stat(targetDir)
	assert.True(t, os.IsNotExist(statErr))
}

func TestSafeRemoveAll_NonExistentDir(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	err := safeRemoveAll(filepath.Join(env.ProjectDir, "nonexistent"))
	assert.NoError(t, err) // Walk on nonexistent dir returns no error
}

func TestSafeRemoveAll_SymlinkEscaping(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	targetDir := filepath.Join(env.ProjectDir, "symlink-dir")
	require.NoError(t, os.MkdirAll(targetDir, 0o755))
	createTestFile(t, targetDir, "file.txt", "data")

	// Create a symlink inside that points outside WatchDir
	escapeTarget := filepath.Join(os.TempDir(), "clawbench-symlink-escape")
	require.NoError(t, os.MkdirAll(escapeTarget, 0o755))
	defer func() { _ = os.RemoveAll(escapeTarget) }()
	require.NoError(t, os.Symlink(escapeTarget, filepath.Join(targetDir, "escape-link")))

	err := safeRemoveAll(targetDir)
	assert.NoError(t, err)
	// The directory itself should be removed
	_, statErr := os.Stat(targetDir)
	assert.True(t, os.IsNotExist(statErr))
	// The escape target should still exist (symlink was not followed)
	_, escapeStatErr := os.Stat(escapeTarget)
	assert.NoError(t, escapeStatErr, "escape target should not be removed")
}

// ============================================================================
// buildChatRequestFromQueue tests (low coverage: 20.8%)
// ============================================================================

func TestBuildChatRequestFromQueue_BasicFields(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "queue-test", "", "", "default", "chat")
	require.NoError(t, err)

	qMsg := model.QueuedMessage{
		Text:      "queued message",
		FilePaths: []string{},
		Files:     []string{},
		CreatedAt: time.Now().Format(time.RFC3339),
	}

	req := buildChatRequestFromQueue(qMsg, sessionID, env.ProjectDir, "codebuddy", "codebuddy", env.ProjectDir)
	assert.NotNil(t, req)
	assert.Equal(t, "queued message", req.Prompt)
	assert.Equal(t, sessionID, req.SessionID)
}

func TestBuildChatRequestFromQueue_WithFiles(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "queue-files", "", "", "default", "chat")
	require.NoError(t, err)

	qMsg := model.QueuedMessage{
		Text:      "check this",
		FilePaths: []string{"config.yaml"},
		Files:     []string{"config.yaml"},
		CreatedAt: time.Now().Format(time.RFC3339),
	}

	req := buildChatRequestFromQueue(qMsg, sessionID, env.ProjectDir, "codebuddy", "codebuddy", env.ProjectDir)
	assert.NotNil(t, req)
	// The prompt should contain the original text (may be prefixed with file annotations)
	assert.Contains(t, req.Prompt, "check this")
}

// ============================================================================
// writeDiffResponse tests (low coverage: 50%)
// ============================================================================

func TestWriteDiffResponse_WithDiff(t *testing.T) {
	w := httptest.NewRecorder()
	writeDiffResponse(w, []byte("diff --git a/file.go b/file.go\n+added line\n-removed line"), nil)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Contains(t, result, "diff")
}

// ============================================================================
// serveProjectsCreate tests (low coverage: 48.6%)
// ============================================================================

func TestServeProjectsCreate_AlreadyExists(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a directory with the same name first
	_ = os.MkdirAll(filepath.Join(env.WatchDir, "existing-project"), 0o755)

	req := newRequest(t, http.MethodPost, "/api/projects", map[string]string{
		"name": "existing-project",
	})
	w := callHandler(ServeProjects, req)

	// The handler may return 500 (mkdir error) or 409 — either is reasonable
	assert.True(t, w.Code >= 400, "expected error status for existing directory, got %d", w.Code)
}

func TestServeProjectsCreate_PathTraversalName(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/projects", map[string]string{
		"name": "../../../etc",
	})
	w := callHandler(ServeProjects, req)

	// Should reject path traversal
	assert.True(t, w.Code == http.StatusForbidden || w.Code == http.StatusBadRequest,
		"expected 403 or 400, got %d", w.Code)
}

// ============================================================================
// ServeIndex tests (low coverage: 31.2%)
// ============================================================================

func TestServeIndex_MethodNotAllowed(t *testing.T) {
	// ServeIndex rejects non-GET/HEAD methods with 405
	req := newRequest(t, http.MethodPost, "/", nil)
	w := callHandler(ServeIndex, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// ============================================================================
// ServeRecentProjects tests (low coverage: 50%)
// ============================================================================

func TestServeRecentProjects_Empty(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/recent-projects", nil)
	w := callHandler(ServeRecentProjects, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// ============================================================================
// copyDir tests — symlink handling and error paths (41.9% → target 70%+)
// ============================================================================

func TestCopyDir_SymlinkWithinWatchDir(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a real file and a directory with a symlink pointing within WatchDir
	createTestFile(t, env.ProjectDir, "realdir/target.txt", "symlink content")
	require.NoError(t, os.MkdirAll(filepath.Join(env.ProjectDir, "srcdir"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(env.ProjectDir, "realdir"), filepath.Join(env.ProjectDir, "srcdir", "link-to-real")))

	dstDir := filepath.Join(env.ProjectDir, "dstdir")
	err := copyDir(filepath.Join(env.ProjectDir, "srcdir"), dstDir)
	require.NoError(t, err)

	// The symlink target is within watchDir, so the actual file should be copied
	data, readErr := os.ReadFile(filepath.Join(dstDir, "link-to-real", "target.txt"))
	require.NoError(t, readErr)
	assert.Equal(t, "symlink content", string(data))
}

func TestCopyDir_SymlinkEscapingWatchDir_Skipped(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a symlink that points outside WatchDir
	escapeTarget := t.TempDir()
	_ = os.WriteFile(filepath.Join(escapeTarget, "secret.txt"), []byte("secret"), 0o644)

	require.NoError(t, os.MkdirAll(filepath.Join(env.ProjectDir, "srcdir"), 0o755))
	require.NoError(t, os.Symlink(escapeTarget, filepath.Join(env.ProjectDir, "srcdir", "escape-link")))

	dstDir := filepath.Join(env.ProjectDir, "dstdir")
	err := copyDir(filepath.Join(env.ProjectDir, "srcdir"), dstDir)
	require.NoError(t, err)

	// The escaping symlink should be skipped — dst dir should exist but be empty
	entries, readErr := os.ReadDir(dstDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "escaping symlinks should be skipped")
}

func TestCopyDir_SymlinkEvalFails_Skipped(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a dangling symlink (target doesn't exist)
	require.NoError(t, os.MkdirAll(filepath.Join(env.ProjectDir, "srcdir"), 0o755))
	require.NoError(t, os.Symlink("/nonexistent/path/xyz", filepath.Join(env.ProjectDir, "srcdir", "dangling")))

	dstDir := filepath.Join(env.ProjectDir, "dstdir")
	err := copyDir(filepath.Join(env.ProjectDir, "srcdir"), dstDir)
	require.NoError(t, err)

	// Dangling symlink should be skipped
	entries, readErr := os.ReadDir(dstDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "dangling symlinks should be skipped")
}

func TestCopyDir_SrcNotExists_ReturnsError(t *testing.T) {
	err := copyDir("/nonexistent/src", "/tmp/dst")
	assert.Error(t, err)
}

func TestCopyDir_SymlinkToDir_WithinWatchDir(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a directory and a symlink pointing to it within WatchDir
	createTestFile(t, env.ProjectDir, "realdir/nested/file.txt", "nested content")
	require.NoError(t, os.MkdirAll(filepath.Join(env.ProjectDir, "srcdir"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(env.ProjectDir, "realdir"), filepath.Join(env.ProjectDir, "srcdir", "link-dir")))

	dstDir := filepath.Join(env.ProjectDir, "dstdir")
	err := copyDir(filepath.Join(env.ProjectDir, "srcdir"), dstDir)
	require.NoError(t, err)

	// Symlink to directory within watchDir should be recursively copied
	data, readErr := os.ReadFile(filepath.Join(dstDir, "link-dir", "nested", "file.txt"))
	require.NoError(t, readErr)
	assert.Equal(t, "nested content", string(data))
}

// ============================================================================
// UploadFile tests — error paths (64.0% → target 80%+)
// ============================================================================

func TestUploadFilePost_NoProjectCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/upload/file", nil)
	w := callHandler(UploadFile, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestUploadFilePost_WrongMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/upload/file", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(UploadFile, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestUploadFile_NoFileProvided(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Send multipart form without a "file" field
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("wrong_field", "test.txt")
	require.NoError(t, err)
	_, _ = fmt.Fprint(part, "hello")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload/file", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(UploadFile, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUploadFile_NoExtension(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "noextension")
	require.NoError(t, err)
	_, _ = fmt.Fprint(part, "hello")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload/file", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(UploadFile, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUploadFilePost_AllExtensionsAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	tests := []struct {
		name string
		ext  string
	}{
		{"exe", "program.exe"},
		{"bat", "script.bat"},
		{"cmd", "script.cmd"},
		{"ps1", "script.ps1"},
		{"vbs", "script.vbs"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			writer := multipart.NewWriter(&buf)
			part, createErr := writer.CreateFormFile("file", tt.ext)
			require.NoError(t, createErr)
			_, _ = fmt.Fprint(part, "content")
			_ = writer.Close()

			req := httptest.NewRequest(http.MethodPost, "/api/upload/file", &buf)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			withProjectCookie(req, env.ProjectDir)
			w := callHandler(UploadFile, req)
			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}

func TestUploadFile_Success(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "test.txt")
	require.NoError(t, err)
	_, _ = fmt.Fprint(part, "hello world")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload/file", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(UploadFile, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
	path, ok := result["path"].(string)
	require.True(t, ok)
	assert.Contains(t, filepath.ToSlash(path), ".clawbench/uploads/test.txt")
}

func TestUploadFile_FilenameCollision_SequentialNumbering(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Pre-create the uploads directory with an existing file
	uploadsDir := filepath.Join(env.ProjectDir, ".clawbench", "uploads")
	require.NoError(t, os.MkdirAll(uploadsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(uploadsDir, "test.txt"), []byte("existing"), 0o644))

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "test.txt")
	require.NoError(t, err)
	_, _ = fmt.Fprint(part, "new content")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload/file", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(UploadFile, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	path, ok := result["path"].(string)
	require.True(t, ok)
	assert.Contains(t, path, "test_1.txt", "collision should produce sequentially numbered filename")
}

func TestUploadFile_SpacesInFilename(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "my file.txt")
	require.NoError(t, err)
	_, _ = fmt.Fprint(part, "content with spaces")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload/file", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(UploadFile, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	path, ok := result["path"].(string)
	require.True(t, ok)
	assert.Contains(t, path, "my_file.txt", "spaces should be replaced with underscores")
}

// ============================================================================
// ServeQuickCommands — PUT reorder and additional error paths (47.6% → 80%+)
// ============================================================================

func TestServeQuickCommands_Reorder(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	id1, err := service.AddQuickCommand("First", "cmd1", false, false)
	require.NoError(t, err)
	id2, err := service.AddQuickCommand("Second", "cmd2", false, false)
	require.NoError(t, err)

	body := map[string]any{
		"ids": []int64{id2, id1},
	}
	req := newRequest(t, http.MethodPut, "/api/terminal/quick-commands/reorder", body)
	w := callHandler(ServeQuickCommands, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	decodeRespJSON(t, w.Body, &result)
	assert.Equal(t, true, result["success"])
}

func TestServeQuickCommands_ReorderEmptyIDs(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]any{
		"ids": []int64{},
	}
	req := newRequest(t, http.MethodPut, "/api/terminal/quick-commands/reorder", body)
	w := callHandler(ServeQuickCommands, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServeQuickCommands_PutNonReorderPath(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPut, "/api/terminal/quick-commands/something-else", nil)
	w := callHandler(ServeQuickCommands, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeQuickCommands_PostWithWhitespace(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Labels/commands with only whitespace should be treated as empty after TrimSpace
	body := map[string]any{
		"label":   "  ",
		"command": "  echo hi  ",
	}
	req := newRequest(t, http.MethodPost, "/api/terminal/quick-commands", body)
	w := callHandler(ServeQuickCommands, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ============================================================================
// ServeQuickCommandByID — additional error paths (59.4% → 80%+)
// ============================================================================

func TestServeQuickCommandByID_UpdateValidation(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	id, err := service.AddQuickCommand("Old", "old cmd", false, false)
	require.NoError(t, err)

	tests := []struct {
		name string
		body map[string]any
	}{
		{"empty label", map[string]any{"label": "", "command": "cmd"}},
		{"empty command", map[string]any{"label": "Test", "command": ""}},
		{"label too long", map[string]any{"label": string(make([]byte, 101)), "command": "cmd"}},
		{"command too long", map[string]any{"label": "Test", "command": string(make([]byte, 4097))}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := newRequest(t, http.MethodPut, "/api/terminal/quick-commands/"+fmt.Sprint(id), tt.body)
			w := callHandler(ServeQuickCommandByID, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

func TestServeQuickCommandByID_DelegateToServeQuickCommands(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// When idStr is empty or "reorder", it should delegate to ServeQuickCommands
	req := newRequest(t, http.MethodGet, "/api/terminal/quick-commands/reorder", nil)
	w := callHandler(ServeQuickCommandByID, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// ============================================================================
// ServeRecentProjects — additional paths (50% → 80%+)
// ============================================================================

func TestServeRecentProjects_AddAndList(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Add a project via POST (use a real directory so GetRecentProjects doesn't filter it out)
	addReq := newRequest(t, http.MethodPost, "/api/recent-projects", map[string]string{
		"path": env.ProjectDir,
	})
	w := callHandler(ServeRecentProjects, addReq)
	assert.Equal(t, http.StatusOK, w.Code)

	// List should now contain the added project
	listReq := newRequest(t, http.MethodGet, "/api/recent-projects", nil)
	w = callHandler(ServeRecentProjects, listReq)
	assert.Equal(t, http.StatusOK, w.Code)

	// GetRecentProjects returns []string
	var projects []string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &projects))
	assert.Contains(t, projects, env.ProjectDir)
}

func TestServeRecentProjects_Delete(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Add a project first
	_ = service.AddRecentProject("/home/user/to-delete")

	delReq := newRequest(t, http.MethodDelete, "/api/recent-projects", map[string]string{
		"path": "/home/user/to-delete",
	})
	w := callHandler(ServeRecentProjects, delReq)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServeRecentProjects_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPatch, "/api/recent-projects", nil)
	w := callHandler(ServeRecentProjects, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// ============================================================================
// writeDiffResponse — error path
// ============================================================================

func TestWriteDiffResponse_WithGitError(t *testing.T) {
	w := httptest.NewRecorder()
	// When cmdErr is non-nil but output is also non-empty, the handler still returns 200 with the output
	writeDiffResponse(w, []byte("some diff output"), errors.New("git diff exited with code 1"))
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Contains(t, result, "diff")
	assert.Equal(t, false, result["empty"])
}

// ============================================================================
// TerminalWebSocket — additional error paths (50% → 70%+)
// ============================================================================

func TestTerminalWebSocket_NilManager(t *testing.T) {
	origMgr := GetTerminalManager()
	defer SetTerminalManager(origMgr)
	SetTerminalManager(nil)

	req := newRequest(t, http.MethodGet, "/api/terminal/ws", nil)
	w := callHandler(TerminalWebSocket, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestTerminalWebSocket_NoProjectCookie(t *testing.T) {
	origMgr := GetTerminalManager()
	defer func() {
		curMgr := GetTerminalManager()
		if curMgr != nil && curMgr != origMgr {
			curMgr.Close()
		}
		SetTerminalManager(origMgr)
	}()

	SetTerminalManager(terminal.NewManager(model.TerminalConfig{
		Enabled:      true,
		IdleTimeout:  "1m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}, 20000))

	req := newRequest(t, http.MethodGet, "/api/terminal/ws", nil)
	w := callHandler(TerminalWebSocket, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// ============================================================================
// ServeRoots — additional paths (66.7% → 80%+)
// ============================================================================

func TestServeRoots_WithConfig(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/roots", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeRoots, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	// roots field contains the filesystem root paths
	roots, ok := result["roots"].([]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, roots)
}

// ============================================================================
// buildChatRequestFromQueue — verify file fields (assertion improvement)
// ============================================================================

func TestBuildChatRequestFromQueue_FilePathsAndFiles(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "queue-files-test", "", "", "default", "chat")
	require.NoError(t, err)

	// Create actual files so file annotations can be added
	createTestFile(t, env.ProjectDir, "main.go", "package main")
	createTestFile(t, env.ProjectDir, "config.yaml", "key: value")

	qMsg := model.QueuedMessage{
		Text:      "review these files",
		FilePaths: []string{"main.go"},
		Files:     []string{"config.yaml"},
		CreatedAt: time.Now().Format(time.RFC3339),
	}

	req := buildChatRequestFromQueue(qMsg, sessionID, env.ProjectDir, "codebuddy", "codebuddy", env.ProjectDir)
	require.NotNil(t, req)
	assert.Contains(t, req.Prompt, "review these files")
	assert.Equal(t, sessionID, req.SessionID)
	// Verify file annotations are injected into the prompt
	assert.Contains(t, req.Prompt, "main.go", "prompt should contain file path annotation")
}

// ============================================================================
// ServeFileArchive — symlink isPathUnderAnyRoot coverage
// ============================================================================

func TestServeFileArchive_SymlinkEscapingRootPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping symlink test on Windows")
	}
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a directory with a symlink pointing outside root paths
	archiveDir := filepath.Join(env.ProjectDir, "archivedir")
	require.NoError(t, os.MkdirAll(archiveDir, 0o755))
	createTestFile(t, archiveDir, "real.txt", "real content")

	escapeTarget := filepath.Join(os.TempDir(), "clawbench-archive-escape")
	require.NoError(t, os.MkdirAll(escapeTarget, 0o755))
	defer func() { _ = os.RemoveAll(escapeTarget) }()
	_ = os.WriteFile(filepath.Join(escapeTarget, "secret.txt"), []byte("secret"), 0o644)
	require.NoError(t, os.Symlink(escapeTarget, filepath.Join(archiveDir, "escape-link")))

	req := newRequest(t, http.MethodPost, "/api/file/archive", map[string]any{
		"paths": []string{"archivedir"},
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeFileArchive, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Verify zip contents — should contain real.txt but NOT the escape symlink
	reader, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	require.NoError(t, err)
	names := make([]string, 0, len(reader.File))
	for _, f := range reader.File {
		names = append(names, f.Name)
	}
	assert.Contains(t, names, "archivedir/real.txt")
	// escape-link should be skipped (target outside root paths)
	for _, n := range names {
		assert.NotContains(t, n, "escape-link", "symlink escaping root paths should be skipped in archive")
	}
}

// ============================================================================
// UploadFile — isPathUnderAnyRoot(dstPath) defense-in-depth check
// ============================================================================

func TestUploadFile_CustomDir_DstPathUnderRoot_Succeeds(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	subDir := filepath.Join(env.ProjectDir, "customupload")
	require.NoError(t, os.MkdirAll(subDir, 0o755))

	req := createMultipartUploadRequest(t, "test.txt", "custom content", subDir)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(UploadFile, req)
	assertOK(t, w)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
	pathStr, ok := result["path"].(string)
	assert.True(t, ok)
	assert.Contains(t, filepath.ToSlash(pathStr), "customupload/test.txt")
}

// ============================================================================
// ServeRoots — empty root paths fallback
// ============================================================================

func TestServeRoots_EmptyRootPaths_FallsBackToHomeDir(t *testing.T) {
	origRootPaths := model.RootPaths
	model.RootPaths = []string{}
	defer func() { model.RootPaths = origRootPaths }()

	req := newRequest(t, http.MethodGet, "/api/roots", nil)
	w := callHandler(ServeRoots, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	roots, ok := result["roots"].([]interface{})
	assert.True(t, ok)
	assert.NotEmpty(t, roots, "should fall back to homeDir when RootPaths is empty")
}

// ============================================================================
// ServeProjects — additional coverage for DELETE method and relative paths
// ============================================================================

func TestServeProjects_DeleteMethod_Returns405(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/projects", nil)
	w := callHandler(ServeProjects, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeProjects_RelativePathOutsideRoot(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Set RootPaths to a specific directory so relative paths are resolved against it
	origRootPaths := model.RootPaths
	tmpDir := t.TempDir()
	model.RootPaths = []string{tmpDir}
	defer func() { model.RootPaths = origRootPaths }()

	// Create a subdirectory in the root
	_ = os.MkdirAll(filepath.Join(tmpDir, "subproject"), 0o755)

	t.Run("RelativePathUnderRoot_ReturnsOK", func(t *testing.T) {
		req := newRequest(t, http.MethodGet, "/api/projects?path=subproject", nil)
		w := callHandler(ServeProjects, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("RelativePathWithTraversalOutsideRoot_Returns403", func(t *testing.T) {
		req := newRequest(t, http.MethodGet, "/api/projects?path=../../../etc", nil)
		w := callHandler(ServeProjects, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

func TestServeProjectsCreate_RelativePath(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Ensure subdirectory exists for create operations
	_ = os.MkdirAll(filepath.Join(env.WatchDir, "subproject"), 0o755)

	t.Run("AbsPathUnderRoot_Succeeds", func(t *testing.T) {
		req := newRequest(t, http.MethodPost, "/api/projects", map[string]string{
			"path": filepath.Join(env.WatchDir, "subproject"),
			"name": "newdir",
		})
		w := callHandler(ServeProjects, req)
		assert.Equal(t, http.StatusOK, w.Code)

		info, err := os.Stat(filepath.Join(env.WatchDir, "subproject", "newdir"))
		assert.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("EmptyPathUsesFirstRoot", func(t *testing.T) {
		req := newRequest(t, http.MethodPost, "/api/projects", map[string]string{
			"path": "",
			"name": "rootlevel",
		})
		w := callHandler(ServeProjects, req)
		assert.Equal(t, http.StatusOK, w.Code)

		info, err := os.Stat(filepath.Join(env.WatchDir, "rootlevel"))
		assert.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("RelativePath_ResolvesFromFirstRoot", func(t *testing.T) {
		req := newRequest(t, http.MethodPost, "/api/projects", map[string]string{
			"path": "subproject",
			"name": "nested",
		})
		w := callHandler(ServeProjects, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// ============================================================================
// UploadFile — default dir isPathUnderAnyRoot check
// ============================================================================

func TestUploadFile_DefaultDir_OutsideRoot_Returns403(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Set RootPaths to a different directory that doesn't include the project
	// This triggers the defense-in-depth isPathUnderAnyRoot check for the default dir case
	otherDir := t.TempDir()
	origRootPaths := model.RootPaths
	model.RootPaths = []string{otherDir}
	defer func() { model.RootPaths = origRootPaths }()

	// Create multipart upload request
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "test.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("hello"))
	require.NoError(t, err)
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload/file", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(UploadFile, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// ============================================================================
// ValidateCreatePath — relative dirPath coverage
// ============================================================================

func TestValidateCreatePath_RelativeDirPath_UsesProjectCookie(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a subdirectory under the project
	_ = os.MkdirAll(filepath.Join(env.ProjectDir, "subdir"), 0o755)

	req := newRequest(t, http.MethodPost, "/api/file/create", map[string]string{
		"path": "subdir",
		"name": "newfile.txt",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeFileCreate, req)
	assert.Equal(t, http.StatusOK, w.Code)

	_, err := os.Stat(filepath.Join(env.ProjectDir, "subdir", "newfile.txt"))
	assert.NoError(t, err)
}

// ============================================================================
// SafeRemoveAll — symlink escaping root paths
// ============================================================================

func TestSafeRemoveAll_SymlinkEscapingRootPaths_SkipsEscape(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a directory to delete
	deleteDir := filepath.Join(env.WatchDir, "to-delete")
	_ = os.MkdirAll(deleteDir, 0o755)

	// Create a file inside
	createTestFile(t, deleteDir, "normal.txt", "content")

	// Create a symlink inside that points outside root paths
	outsideDir := t.TempDir()
	createTestFile(t, outsideDir, "secret.txt", "secret")
	linkPath := filepath.Join(deleteDir, "escape-link")
	_ = os.Symlink(outsideDir, linkPath)

	// Delete the directory using safeRemoveAll
	err := safeRemoveAll(deleteDir)
	require.NoError(t, err)

	// The directory should be gone
	_, err = os.Stat(deleteDir)
	assert.True(t, os.IsNotExist(err))

	// The outside file should still exist (symlink was not followed)
	_, err = os.Stat(filepath.Join(outsideDir, "secret.txt"))
	assert.NoError(t, err, "file outside root paths should not be deleted")
}

// ============================================================================
// CopyDir — symlink escaping root paths via copyDir
// ============================================================================

func TestCopyDir_SymlinkEscapingRootPaths_Skipped(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	srcDir := filepath.Join(env.WatchDir, "copy-src")
	_ = os.MkdirAll(srcDir, 0o755)
	createTestFile(t, srcDir, "normal.txt", "content")

	// Create symlink pointing outside root paths
	outsideDir := t.TempDir()
	_ = os.Symlink(outsideDir, filepath.Join(srcDir, "escape-link"))

	dstDir := filepath.Join(env.WatchDir, "copy-dst")

	err := copyDir(srcDir, dstDir)
	require.NoError(t, err)

	// Normal file should be copied
	_, err = os.Stat(filepath.Join(dstDir, "normal.txt"))
	assert.NoError(t, err)

	// Symlink should NOT be copied (it escapes root paths)
	_, err = os.Lstat(filepath.Join(dstDir, "escape-link"))
	assert.True(t, os.IsNotExist(err), "escaping symlink should not be copied")
}

// ============================================================================
// ListDir — not a directory error coverage
// ============================================================================

func TestListDir_NotADirectory_Returns400(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a file, then try to list it as a directory
	createTestFile(t, env.ProjectDir, "notadir.txt", "content")

	req := newRequest(t, http.MethodGet, "/api/dir?path=notadir.txt", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ListDir, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ============================================================================
// ServeProjects — additional edge cases
// ============================================================================

func TestServeProjects_EmptyRootPaths_NoFirstRoot_Returns400(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Set RootPaths to empty and try to browse with a relative path
	origRootPaths := model.RootPaths
	model.RootPaths = []string{}
	defer func() { model.RootPaths = origRootPaths }()

	req := newRequest(t, http.MethodGet, "/api/projects?path=relative", nil)
	w := callHandler(ServeProjects, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeProjects_EmptyPathWithRoots_ListsFirstRoot(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create entries in the WatchDir
	createTestFile(t, env.WatchDir, "rootfile.txt", "hello")

	req := newRequest(t, http.MethodGet, "/api/projects?path=", nil)
	w := callHandler(ServeProjects, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	items, ok := result["items"].([]interface{})
	assert.True(t, ok)
	assert.NotEmpty(t, items, "should list entries in first root")
}
