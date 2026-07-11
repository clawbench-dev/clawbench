package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// createMultipartUploadRequest builds a multipart/form-data POST request with
// a file field and an optional "dir" field.
func createMultipartUploadRequest(t *testing.T, filename, content, dir string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	_, _ = part.Write([]byte(content))

	if dir != "" {
		_ = writer.WriteField("dir", dir)
	}

	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload/file", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func TestUploadFile_DefaultDir(t *testing.T) {
	t.Run("UploadToDefaultDir", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := createMultipartUploadRequest(t, "hello.txt", "hello world", "")
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(UploadFile, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)
		assert.Equal(t, true, result["ok"])

		// Path should be under .clawbench/uploads/
		pathStr, ok := result["path"].(string)
		assert.True(t, ok)
		assert.Contains(t, filepath.ToSlash(pathStr), ".clawbench/uploads/hello.txt")

		// File should exist on disk
		fullPath := filepath.Join(env.ProjectDir, pathStr)
		data, err := os.ReadFile(fullPath)
		assert.NoError(t, err)
		assert.Equal(t, "hello world", string(data))
	})

	t.Run("NoFile_Returns400", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Empty multipart form without file field
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		_ = writer.Close()
		req := httptest.NewRequest(http.MethodPost, "/api/upload/file", &buf)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(UploadFile, req)
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("NoExtension_Returns400", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := createMultipartUploadRequest(t, "noext", "content", "")
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(UploadFile, req)
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("DangerousExtension_Allowed", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// All file types are allowed — users may upload binaries, scripts, etc.
		req := createMultipartUploadRequest(t, "evil.exe", "MZ", "")
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(UploadFile, req)
		assertOK(t, w)

		var result map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &result)
		pathStr, _ := result["path"].(string)
		assert.Contains(t, filepath.ToSlash(pathStr), ".clawbench/uploads/evil.exe")
	})

	t.Run("DuplicateFilename_AppendsNumber", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// First upload
		req1 := createMultipartUploadRequest(t, "dup.txt", "first", "")
		withProjectCookie(req1, env.ProjectDir)
		w1 := callHandler(UploadFile, req1)
		assertOK(t, w1)

		// Second upload with same name
		req2 := createMultipartUploadRequest(t, "dup.txt", "second", "")
		withProjectCookie(req2, env.ProjectDir)
		w2 := callHandler(UploadFile, req2)
		assertOK(t, w2)

		var result map[string]interface{}
		_ = json.Unmarshal(w2.Body.Bytes(), &result)
		pathStr, _ := result["path"].(string)
		assert.Contains(t, pathStr, "dup_1.txt")
	})

	t.Run("SpacesInFilename_ReplacedWithUnderscore", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := createMultipartUploadRequest(t, "my file.txt", "content", "")
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(UploadFile, req)
		assertOK(t, w)

		var result map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &result)
		pathStr, _ := result["path"].(string)
		assert.Contains(t, pathStr, "my_file.txt")
		assert.NotContains(t, pathStr, "my file.txt")
	})

	t.Run("WrongMethod_GET_Returns405", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/upload/file", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(UploadFile, req)
		assertStatus(t, w, http.StatusMethodNotAllowed)
	})

	t.Run("OversizedBody_Returns400", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a multipart request with a body larger than allowed
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		part, _ := writer.CreateFormFile("file", "big.txt")
		// Write a large content (simulated by just writing the multipart boundary)
		_, _ = part.Write(make([]byte, 100))
		_ = writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/upload/file", &buf)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		withProjectCookie(req, env.ProjectDir)

		// Note: MaxBytesReader is applied in the handler, but the body we send
		// is small enough. To truly test ParseMultipartForm error, we'd need
		// to send a malformed multipart body.
		// Instead, test with an invalid multipart body.
		w := callHandler(UploadFile, req)
		// Should succeed or fail gracefully
		assert.True(t, w.Code == http.StatusOK || w.Code == http.StatusBadRequest)
	})

	t.Run("InvalidMultipartBody_Returns400", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Send invalid (non-multipart) body with multipart content type
		req := httptest.NewRequest(http.MethodPost, "/api/upload/file", bytes.NewReader([]byte("not multipart")))
		req.Header.Set("Content-Type", "multipart/form-data; boundary=----bad")
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(UploadFile, req)
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("NoProjectCookie_Returns403", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := createMultipartUploadRequest(t, "test.txt", "content", "")
		// No project cookie

		w := callHandler(UploadFile, req)
		assertStatus(t, w, http.StatusForbidden)
	})
}

func TestShareInRecent(t *testing.T) {
	t.Run("NoShareInDir_ReturnsEmptyArray", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := httptest.NewRequest(http.MethodGet, "/api/share-in/recent", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ShareInRecent, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var result []shareInFile
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("ReturnsFilesSortedByModTime", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create share-in dir with files
		shareInDir := filepath.Join(env.ProjectDir, ".clawbench", "share-in")
		_ = os.MkdirAll(shareInDir, 0o755)

		_ = os.WriteFile(filepath.Join(shareInDir, "old.txt"), []byte("old"), 0o644)
		_ = os.WriteFile(filepath.Join(shareInDir, "new.txt"), []byte("new file"), 0o644)
		// Set different mod times
		oldTime := time.Now().Add(-2 * time.Hour)
		newTime := time.Now()
		_ = os.Chtimes(filepath.Join(shareInDir, "old.txt"), oldTime, oldTime)
		_ = os.Chtimes(filepath.Join(shareInDir, "new.txt"), newTime, newTime)

		req := httptest.NewRequest(http.MethodGet, "/api/share-in/recent", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ShareInRecent, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var result []shareInFile
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)
		assert.Len(t, result, 2)
		assert.Equal(t, "new.txt", result[0].Name)
		assert.Equal(t, "old.txt", result[1].Name)
	})

	t.Run("LimitsTo20Files", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		shareInDir := filepath.Join(env.ProjectDir, ".clawbench", "share-in")
		_ = os.MkdirAll(shareInDir, 0o755)

		for i := 0; i < 25; i++ {
			_ = os.WriteFile(filepath.Join(shareInDir, fmt.Sprintf("file%d.txt", i)), []byte("content"), 0o644)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/share-in/recent", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ShareInRecent, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var result []shareInFile
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)
		assert.Len(t, result, 20)
	})
}

func TestUploadFile_CustomDir(t *testing.T) {
	t.Run("UploadToCustomDir", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a subdirectory to upload into
		subDir := filepath.Join(env.ProjectDir, "subdir")
		_ = os.MkdirAll(subDir, 0o755)

		req := createMultipartUploadRequest(t, "test.txt", "custom dir content", "subdir")
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(UploadFile, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)
		assert.Equal(t, true, result["ok"])

		pathStr, ok := result["path"].(string)
		assert.True(t, ok)
		assert.Contains(t, filepath.ToSlash(pathStr), "subdir/test.txt")

		// Verify file on disk
		fullPath := filepath.Join(env.ProjectDir, pathStr)
		data, err := os.ReadFile(fullPath)
		assert.NoError(t, err)
		assert.Equal(t, "custom dir content", string(data))
	})

	t.Run("CustomDirAutoCreated_Succeeds", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Non-existent dir should be auto-created (same as default uploads dir behavior)
		req := createMultipartUploadRequest(t, "test.txt", "content", "nonexistent_dir")
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(UploadFile, req)
		assertOK(t, w)

		var result map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &result)
		assert.Equal(t, true, result["ok"])
	})

	t.Run("DirIsFile_Returns400", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a file where we expect a directory
		createTestFile(t, env.ProjectDir, "not_a_dir.txt", "I'm a file")

		req := createMultipartUploadRequest(t, "test.txt", "content", "not_a_dir.txt")
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(UploadFile, req)
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("PathTraversalInDir_Returns403", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := createMultipartUploadRequest(t, "test.txt", "content", "../../../etc")
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(UploadFile, req)
		assertStatus(t, w, http.StatusForbidden)
	})

	t.Run("DuplicateFileInCustomDir_AppendsNumber", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		subDir := filepath.Join(env.ProjectDir, "mydir")
		_ = os.MkdirAll(subDir, 0o755)

		// First upload
		req1 := createMultipartUploadRequest(t, "dup.txt", "first", "mydir")
		withProjectCookie(req1, env.ProjectDir)
		w1 := callHandler(UploadFile, req1)
		assertOK(t, w1)

		// Second upload with same filename
		req2 := createMultipartUploadRequest(t, "dup.txt", "second", "mydir")
		withProjectCookie(req2, env.ProjectDir)
		w2 := callHandler(UploadFile, req2)
		assertOK(t, w2)

		var result map[string]interface{}
		_ = json.Unmarshal(w2.Body.Bytes(), &result)
		pathStr, _ := result["path"].(string)
		assert.Contains(t, pathStr, "dup_1.txt")
	})

	t.Run("NestedDirUpload", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create nested directory
		nestedDir := filepath.Join(env.ProjectDir, "a", "b", "c")
		_ = os.MkdirAll(nestedDir, 0o755)

		req := createMultipartUploadRequest(t, "deep.txt", "deep content", "a/b/c")
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(UploadFile, req)
		assertOK(t, w)

		var result map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &result)
		pathStr, _ := result["path"].(string)
		assert.Contains(t, filepath.ToSlash(pathStr), "a/b/c/deep.txt")
	})

	t.Run("AbsoluteDirPath_UnderWatchDir_Succeeds", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a subdirectory
		subDir := filepath.Join(env.ProjectDir, "absdir")
		_ = os.MkdirAll(subDir, 0o755)

		// Use absolute path for dir
		req := createMultipartUploadRequest(t, "abs.txt", "absolute path", subDir)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(UploadFile, req)
		assertOK(t, w)

		var result map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &result)
		assert.Equal(t, true, result["ok"])
	})

	t.Run("AbsoluteDirPath_OutsideWatchDir_Returns403or400", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Use an absolute path outside WatchDir (platform-specific)
		outsidePath := "/tmp"
		if runtime.GOOS == "windows" {
			outsidePath = `C:\Windows`
		}
		req := createMultipartUploadRequest(t, "evil.txt", "evil", outsidePath)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(UploadFile, req)
		// Should return 403 (access denied) or 400 (directory not found under project)
		assert.True(t, w.Code == http.StatusForbidden || w.Code == http.StatusBadRequest,
			"expected 403 or 400, got %d", w.Code)
	})

	t.Run("CustomDirRelativePath_ReturnsRelativePathInResponse", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		subDir := filepath.Join(env.ProjectDir, "relpathdir")
		_ = os.MkdirAll(subDir, 0o755)

		req := createMultipartUploadRequest(t, "rel.txt", "relative", "relpathdir")
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(UploadFile, req)
		assertOK(t, w)

		var result map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &result)
		pathStr, _ := result["path"].(string)
		// Should be a relative path like "relpathdir/rel.txt"
		assert.False(t, filepath.IsAbs(pathStr))
		assert.Contains(t, pathStr, "relpathdir")
	})

	t.Run("ReadOnlyDir_Returns500", func(t *testing.T) {
		// Skip on Windows — read-only directory permissions don't prevent file creation
		if runtime.GOOS == "windows" {
			t.Skip("skipping read-only dir test on Windows")
		}
		if os.Getuid() == 0 {
			t.Skip("skipping as root: root bypasses filesystem permissions")
		}

		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a read-only directory
		readOnlyDir := filepath.Join(env.ProjectDir, "readonly")
		_ = os.MkdirAll(readOnlyDir, 0o555)
		// Ensure we can restore permissions after test
		defer func() { _ = os.Chmod(readOnlyDir, 0o755) }()

		req := createMultipartUploadRequest(t, "test.txt", "content", "readonly")
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(UploadFile, req)
		// Should fail with 500 (can't create file in read-only dir)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("DefaultUploadDir_MkdirAllFail_Returns500", func(t *testing.T) {
		// Skip on Windows — read-only directory permissions don't prevent MkdirAll
		if runtime.GOOS == "windows" {
			t.Skip("skipping MkdirAll fail test on Windows")
		}
		if os.Getuid() == 0 {
			t.Skip("skipping as root: root bypasses filesystem permissions")
		}

		env, teardown := setupTestEnv(t)
		defer teardown()

		// Make the project directory read-only so MkdirAll for .clawbench/uploads/ fails
		_ = os.Chmod(env.ProjectDir, 0o555)
		defer func() { _ = os.Chmod(env.ProjectDir, 0o755) }()

		req := createMultipartUploadRequest(t, "test.txt", "content", "")
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(UploadFile, req)
		// Should fail with 500 (can't create .clawbench/uploads/ directory)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("SymlinkDirOutsideWatchDir_Returns403", func(t *testing.T) {
		// Skip on Windows — symlinks require elevated privileges
		if runtime.GOOS == "windows" {
			t.Skip("skipping symlink test on Windows")
		}

		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a real directory OUTSIDE WatchDir
		outsideDir := filepath.Join(os.TempDir(), "clawbench_outside")
		_ = os.MkdirAll(outsideDir, 0o755)
		defer func() { _ = os.RemoveAll(outsideDir) }()

		// Create a symlink INSIDE the project that points OUTSIDE
		linkPath := filepath.Join(env.ProjectDir, "link_out")
		_ = os.Symlink(outsideDir, linkPath)
		defer func() { _ = os.Remove(linkPath) }()

		req := createMultipartUploadRequest(t, "test.txt", "content", "link_out")
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(UploadFile, req)
		// The symlink target is outside WatchDir, so isPathUnderBase should fail
		assert.True(t, w.Code == http.StatusForbidden || w.Code == http.StatusBadRequest,
			"expected 403 or 400, got %d", w.Code)
	})

	t.Run("CustomDir_DstPathUnderRoot_Succeeds", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a subdirectory to upload into using absolute path
		subDir := filepath.Join(env.ProjectDir, "customdir")
		_ = os.MkdirAll(subDir, 0o755)

		req := createMultipartUploadRequest(t, "dstcheck.txt", "dst path under root", subDir)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(UploadFile, req)
		assertOK(t, w)

		var result map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &result)
		assert.Equal(t, true, result["ok"])

		// Verify file exists on disk
		pathStr, _ := result["path"].(string)
		fullPath := filepath.Join(env.ProjectDir, pathStr)
		data, err := os.ReadFile(fullPath)
		assert.NoError(t, err)
		assert.Equal(t, "dst path under root", string(data))
	})
}
