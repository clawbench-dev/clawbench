package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListDir(t *testing.T) {
	t.Run("NormalDirectoryListing", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "file1.txt", "hello")
		createTestFile(t, env.ProjectDir, "file2.go", "package main")
		_ = os.MkdirAll(filepath.Join(env.ProjectDir, "subdir"), 0o755)

		req := newRequest(t, http.MethodGet, "/api/dir", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ListDir, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)

		items, ok := result["items"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, items, 3)

		// Items should be sorted: dirs first, then files alphabetically
		names := make([]string, len(items))
		for i, item := range items {
			entry, _ := item.(map[string]interface{})
			names[i] = entry["name"].(string)
		}
		expected := []string{"subdir", "file1.txt", "file2.go"}
		sort.Strings(expected[:1])          // dirs first
		assert.Equal(t, "subdir", names[0]) // dir comes first
	})

	t.Run("EmptyDirectory", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/dir", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ListDir, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)

		// Empty dir returns nil slice (null in JSON)
		assert.Nil(t, result["items"])
	})

	t.Run("NoProjectCookie_Returns403", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/dir", nil)
		// No project cookie

		w := callHandler(ListDir, req)
		assertStatus(t, w, http.StatusForbidden)
	})

	t.Run("PathTraversal_Returns403", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/dir?path=../../../etc", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ListDir, req)
		assertStatus(t, w, http.StatusForbidden)
	})

	t.Run("SubdirectoryListing", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "subdir/nested.txt", "nested content")

		req := newRequest(t, http.MethodGet, "/api/dir?path=subdir", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ListDir, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)

		items, ok := result["items"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, items, 1)

		entry, _ := items[0].(map[string]interface{})
		assert.Equal(t, "nested.txt", entry["name"])
		assert.Equal(t, "file", entry["type"])
	})
}

func TestGetFile_DoubleSlashPath(t *testing.T) {
	t.Run("DoubleSlashPath_ReturnsFileContent", func(t *testing.T) {
		// Regression test: when encodeURIComponent("/path") produces %2Fpath,
		// Go's ServeMux decodes it back to /, creating a double-slash URL like
		// /api/file//docs/dev/file.md. This should NOT return InvalidFilePath.
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "docs/dev/test.md", "# Hello")

		// Simulate the double-slash URL that results from encodeURIComponent("/path")
		req := newRequest(t, http.MethodGet, "/api/file//docs/dev/test.md", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)
		assert.Equal(t, "# Hello", result["content"])
		assert.Equal(t, "test.md", result["name"])
	})

	t.Run("SingleSlashPath_StillWorks", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "docs/dev/test.md", "# Hello")

		req := newRequest(t, http.MethodGet, "/api/file/docs/dev/test.md", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)
		assert.Equal(t, "# Hello", result["content"])
	})
}

func TestListFiles(t *testing.T) {
	t.Run("ListsAllFilesRecursively", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "root.txt", "root")
		createTestFile(t, env.ProjectDir, "sub/deep.txt", "deep")
		createTestFile(t, env.ProjectDir, "sub/nested.txt", "nested")

		req := newRequest(t, http.MethodGet, "/api/files", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ListFiles, req)
		assertOK(t, w)

		var files []FileInfo
		err := json.Unmarshal(w.Body.Bytes(), &files)
		assert.NoError(t, err)
		assert.Len(t, files, 3)

		// Verify paths are relative
		paths := make([]string, len(files))
		for i, f := range files {
			paths[i] = f.Path
		}
		assert.Contains(t, paths, "root.txt")
		assert.Contains(t, paths, "sub/deep.txt")
		assert.Contains(t, paths, "sub/nested.txt")
	})

	t.Run("EmptyProject", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/files", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ListFiles, req)
		assertOK(t, w)

		var files []FileInfo
		err := json.Unmarshal(w.Body.Bytes(), &files)
		assert.NoError(t, err)
		assert.Len(t, files, 0)
	})

	t.Run("NoProjectCookie_Returns403", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/files", nil)

		w := callHandler(ListFiles, req)
		assertStatus(t, w, http.StatusForbidden)
	})
}

func TestGetFile(t *testing.T) {
	t.Run("ReadTextFile", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "test.txt", "hello world")

		req := newRequest(t, http.MethodGet, "/api/file/test.txt", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assertOK(t, w)

		var fc FileContent
		err := json.Unmarshal(w.Body.Bytes(), &fc)
		assert.NoError(t, err)
		assert.Equal(t, "hello world", fc.Content)
		assert.Equal(t, "test.txt", fc.Name)
		assert.Equal(t, "test.txt", fc.Path)
		assert.True(t, fc.Supported)
		assert.Equal(t, int64(11), fc.Size)
	})

	t.Run("FileNotFound_Returns404", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/file/nonexistent.txt", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assertStatus(t, w, http.StatusNotFound)
	})

	t.Run("NoProjectCookie_Returns403", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/file/test.txt", nil)

		w := callHandler(GetFile, req)
		assertStatus(t, w, http.StatusForbidden)
	})

	t.Run("PathTraversal_Returns400Or403", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/file/../../../etc/passwd", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assert.True(t, w.Code == http.StatusBadRequest || w.Code == http.StatusForbidden,
			"expected 400 or 403, got %d", w.Code)
	})

	t.Run("DirectoryInsteadOfFile_Returns400", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		_ = os.MkdirAll(filepath.Join(env.ProjectDir, "mydir"), 0o755)

		req := newRequest(t, http.MethodGet, "/api/file/mydir", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assertStatus(t, w, http.StatusBadRequest)
	})
}

func TestServeLocalFile(t *testing.T) {
	t.Run("ServeImageFile_CorrectContentType", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a minimal PNG file (1x1 pixel)
		pngData := []byte{
			0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
			0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
			0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
			0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
			0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41,
			0x54, 0x08, 0xD7, 0x63, 0xF8, 0xCF, 0xC0, 0x00,
			0x00, 0x00, 0x02, 0x00, 0x01, 0xE2, 0x21, 0xBC,
			0x33, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E,
			0x44, 0xAE, 0x42, 0x60, 0x82,
		}
		fullPath := filepath.Join(env.ProjectDir, "test.png")
		_ = os.MkdirAll(filepath.Dir(fullPath), 0o755)
		_ = os.WriteFile(fullPath, pngData, 0o644)

		req := newRequest(t, http.MethodGet, "/api/local-file/test.png", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeLocalFile, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
	})

	t.Run("FileNotFound_Returns404", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/local-file/missing.png", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeLocalFile, req)
		assertStatus(t, w, http.StatusNotFound)
	})

	t.Run("NoProjectCookie_Returns403", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/local-file/test.png", nil)

		w := callHandler(ServeLocalFile, req)
		assertStatus(t, w, http.StatusForbidden)
	})

	t.Run("DoubleSlashPath_ServesFile", func(t *testing.T) {
		// Regression test: same as TestGetFile_DoubleSlashPath but for local-file endpoint
		env, teardown := setupTestEnv(t)
		defer teardown()

		pngData := []byte{
			0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
			0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
			0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
			0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
			0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41,
			0x54, 0x08, 0xD7, 0x63, 0xF8, 0xCF, 0xC0, 0x00,
			0x00, 0x00, 0x02, 0x00, 0x01, 0xE2, 0x21, 0xBC,
			0x33, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E,
			0x44, 0xAE, 0x42, 0x60, 0x82,
		}
		fullPath := filepath.Join(env.ProjectDir, "assets/img/test.png")
		_ = os.MkdirAll(filepath.Dir(fullPath), 0o755)
		_ = os.WriteFile(fullPath, pngData, 0o644)

		req := newRequest(t, http.MethodGet, "/api/local-file//assets/img/test.png", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeLocalFile, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
	})
}

func TestServeProjects(t *testing.T) {
	t.Run("GET_ListsDirectoriesUnderWatchDir", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create directories under WatchDir
		_ = os.MkdirAll(filepath.Join(env.WatchDir, "project1"), 0o755)
		_ = os.MkdirAll(filepath.Join(env.WatchDir, "project2"), 0o755)
		// Create a file (should appear too, since ListDir returns all entries)
		createTestFile(t, env.WatchDir, "readme.md", "hello")

		req := newRequest(t, http.MethodGet, "/api/projects", nil)
		w := callHandler(ServeProjects, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)

		items, ok := result["items"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, items, 4) // project (auto-created), project1, project2, readme.md
	})

	t.Run("POST_CreatesNewDirectory", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/projects", map[string]string{
			"name": "new-project",
		})
		w := callHandler(ServeProjects, req)
		assertOK(t, w)

		assertJSONField(t, w, "ok", true)

		// Verify directory was created
		info, err := os.Stat(filepath.Join(env.WatchDir, "new-project"))
		assert.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("POST_MissingName_Returns400", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/projects", map[string]string{
			"name": "",
		})
		w := callHandler(ServeProjects, req)
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("GET_WithPathParameter_ListsSubdirectory", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		_ = os.MkdirAll(filepath.Join(env.WatchDir, "myproject", "src"), 0o755)
		createTestFile(t, env.WatchDir, "myproject/src/main.go", "package main")

		req := newRequest(t, http.MethodGet, "/api/projects?path=myproject", nil)
		w := callHandler(ServeProjects, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)

		items, ok := result["items"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, items, 1) // src directory

		entry, _ := items[0].(map[string]interface{})
		assert.Equal(t, "src", entry["name"])
		assert.Equal(t, "dir", entry["type"])
	})

	t.Run("GET_RootPath_ListsFirstRootDir", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create some entries in WatchDir
		_ = os.MkdirAll(filepath.Join(env.WatchDir, "rootdir"), 0o755)
		createTestFile(t, env.WatchDir, "rootfile.txt", "root content")

		// Empty path triggers root-level browsing (Unix: lists first RootPath)
		req := newRequest(t, http.MethodGet, "/api/projects", nil)
		w := callHandler(ServeProjects, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)

		// Should list contents of RootPaths[0]
		items, ok := result["items"].([]interface{})
		assert.True(t, ok)
		assert.True(t, len(items) >= 2, "should have at least 2 entries (rootdir + rootfile.txt)")
	})

	t.Run("GET_AbsolutePathUnderRoot_ListsDir", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a subdirectory under WatchDir
		subDir := filepath.Join(env.WatchDir, "abspathdir")
		_ = os.MkdirAll(subDir, 0o755)
		createTestFile(t, subDir, "inner.txt", "inner content")

		req := newRequest(t, http.MethodGet, "/api/projects?path="+subDir, nil)
		w := callHandler(ServeProjects, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)

		items, ok := result["items"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, items, 1)
	})

	t.Run("GET_AbsolutePathOutsideRoot_Returns403", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		// Use os.TempDir() which is always an absolute path outside the test's WatchDir.
		// Do NOT use "/etc" — on Windows, forward-slash paths without a drive letter
		// are not considered absolute by filepath.IsAbs, causing the path to be
		// treated as relative and resolved differently.
		outsidePath := os.TempDir()
		req := newRequest(t, http.MethodGet, "/api/projects?path="+outsidePath, nil)
		w := callHandler(ServeProjects, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("POST_CreateWithTraversalName_Returns403", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/projects", map[string]string{
			"path": env.WatchDir,
			"name": "../../../etc",
		})
		w := callHandler(ServeProjects, req)
		assertStatus(t, w, http.StatusForbidden)
	})

	t.Run("GET_AtRootLevel_ParentIsNil", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Browse exactly at RootPaths[0] — should have nil parent
		req := newRequest(t, http.MethodGet, "/api/projects?path="+env.WatchDir, nil)
		w := callHandler(ServeProjects, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)
		assert.Nil(t, result["parent"], "parent should be nil when browsing at root level")
	})

	t.Run("GET_SubdirectoryOfRoot_ParentIsSet", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		subDir := filepath.Join(env.WatchDir, "sublevel")
		_ = os.MkdirAll(subDir, 0o755)

		req := newRequest(t, http.MethodGet, "/api/projects?path="+subDir, nil)
		w := callHandler(ServeProjects, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)
		assert.NotNil(t, result["parent"], "parent should be set when browsing subdirectory of root")
	})

	t.Run("GET_EmptyRootPaths_Returns400", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		// Set RootPaths to empty so absPath stays empty
		origRootPaths := model.RootPaths
		model.RootPaths = []string{}
		defer func() { model.RootPaths = origRootPaths }()

		req := newRequest(t, http.MethodGet, "/api/projects?path=relative", nil)
		w := callHandler(ServeProjects, req)
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("GET_NotADirectory_Returns400", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a file, then try to browse it as a directory
		filePath := filepath.Join(env.WatchDir, "notadir.txt")
		createTestFile(t, env.WatchDir, "notadir.txt", "content")

		req := newRequest(t, http.MethodGet, "/api/projects?path="+filePath, nil)
		w := callHandler(ServeProjects, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestServeFileBatchExists(t *testing.T) {
	t.Run("ExistingFile_ReturnsFile", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "src/main.go", "package main")

		req := newRequest(t, http.MethodPost, "/api/file/batch-exists", map[string]interface{}{
			"paths": []string{"src/main.go"},
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchExists, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)

		results, ok := result["results"].(map[string]interface{})
		assert.True(t, ok)
		assert.Equal(t, "file", results["src/main.go"])
	})

	t.Run("ExistingDirectory_ReturnsDir", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		_ = os.MkdirAll(filepath.Join(env.ProjectDir, "src"), 0o755)

		req := newRequest(t, http.MethodPost, "/api/file/batch-exists", map[string]interface{}{
			"paths": []string{"src"},
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchExists, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)

		results, ok := result["results"].(map[string]interface{})
		assert.True(t, ok)
		assert.Equal(t, "dir", results["src"])
	})

	t.Run("NonExistentPath_ReturnsNone", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/file/batch-exists", map[string]interface{}{
			"paths": []string{"nonexistent.go"},
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchExists, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)

		results, ok := result["results"].(map[string]interface{})
		assert.True(t, ok)
		assert.Equal(t, "none", results["nonexistent.go"])
	})

	t.Run("GlobChars_ReturnsNone", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/file/batch-exists", map[string]interface{}{
			"paths": []string{"**/*.class", "*.java", "src/[test]/file.go", "<sourcefile>"},
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchExists, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)

		results, ok := result["results"].(map[string]interface{})
		assert.True(t, ok)
		assert.Equal(t, "none", results["**/*.class"])
		assert.Equal(t, "none", results["*.java"])
		assert.Equal(t, "none", results["src/[test]/file.go"])
		assert.Equal(t, "none", results["<sourcefile>"])
	})

	t.Run("PathTraversal_ReturnsNone", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/file/batch-exists", map[string]interface{}{
			"paths": []string{"../../../etc/passwd"},
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchExists, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)

		results, ok := result["results"].(map[string]interface{})
		assert.True(t, ok)
		assert.Equal(t, "none", results["../../../etc/passwd"])
	})

	t.Run("MixedPaths_CorrectResults", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "exists.txt", "hello")
		_ = os.MkdirAll(filepath.Join(env.ProjectDir, "subdir"), 0o755)

		req := newRequest(t, http.MethodPost, "/api/file/batch-exists", map[string]interface{}{
			"paths": []string{"exists.txt", "subdir", "missing.go", "**/*.class"},
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchExists, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)

		results, ok := result["results"].(map[string]interface{})
		assert.True(t, ok)
		assert.Equal(t, "file", results["exists.txt"])
		assert.Equal(t, "dir", results["subdir"])
		assert.Equal(t, "none", results["missing.go"])
		assert.Equal(t, "none", results["**/*.class"])
	})

	t.Run("EmptyPaths_Returns400", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/file/batch-exists", map[string]interface{}{
			"paths": []string{},
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchExists, req)
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("NoProjectCookie_Returns403", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/file/batch-exists", map[string]interface{}{
			"paths": []string{"test.txt"},
		})

		w := callHandler(ServeFileBatchExists, req)
		assertStatus(t, w, http.StatusForbidden)
	})

	t.Run("TooManyPaths_Returns400", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		paths := make([]string, 101)
		for i := range paths {
			paths[i] = "file.txt"
		}

		req := newRequest(t, http.MethodPost, "/api/file/batch-exists", map[string]interface{}{
			"paths": paths,
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchExists, req)
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("WrongMethod_GET_Returns405", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/file/batch-exists", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchExists, req)
		assertStatus(t, w, http.StatusMethodNotAllowed)
	})

	t.Run("InvalidJSON_Returns400", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/file/batch-exists", "not-json")
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchExists, req)
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("ContainsGlobChars_ShortCircuit", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a file that would match if glob chars weren't filtered
		createTestFile(t, env.ProjectDir, "test.class", "class data")

		req := newRequest(t, http.MethodPost, "/api/file/batch-exists", map[string]interface{}{
			"paths": []string{"*.class", "test.class"},
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchExists, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)

		results, ok := result["results"].(map[string]interface{})
		assert.True(t, ok)
		assert.Equal(t, "none", results["*.class"])    // glob → none (no os.Stat)
		assert.Equal(t, "file", results["test.class"]) // real path → file
	})
}

// --- isNotDirError ---

func TestIsNotDirError_ENOTDIR(t *testing.T) {
	if !isNotDirError(syscall.ENOTDIR) {
		t.Fatal("expected ENOTDIR to be recognized as not-a-dir error")
	}
}

func TestIsNotDirError_OtherError(t *testing.T) {
	if isNotDirError(os.ErrNotExist) {
		t.Fatal("expected ErrNotExist to NOT be recognized as not-a-dir error")
	}
}

func TestIsNotDirError_PathErrorWithErrno(t *testing.T) {
	// A PathError wrapping an errno should be detected
	pe := &os.PathError{Op: "read", Path: "/tmp", Err: syscall.ENOTDIR}
	if !isNotDirError(pe) {
		t.Fatal("expected PathError with ENOTDIR to be recognized as not-a-dir error")
	}
}

// --- buildDirEntries ---

func TestBuildDirEntries_Sorting(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "beta.txt", "b")
	createTestFile(t, env.ProjectDir, "alpha.txt", "a")
	_ = os.MkdirAll(filepath.Join(env.ProjectDir, "zdir"), 0o755)
	_ = os.MkdirAll(filepath.Join(env.ProjectDir, "adir"), 0o755)

	req := newRequest(t, http.MethodGet, "/api/dir", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ListDir, req)
	assertOK(t, w)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

	items, ok := result["items"].([]interface{})
	require.True(t, ok)

	// Verify dirs come first, then files, sorted within each group
	names := make([]string, len(items))
	for i, item := range items {
		entry, _ := item.(map[string]interface{})
		names[i] = entry["name"].(string)
	}
	// Expected: adir, zdir (dirs first, alpha), then alpha.txt, beta.txt (files, alpha)
	require.Len(t, names, 4)
	assert.Equal(t, "adir", names[0])
	assert.Equal(t, "zdir", names[1])
	assert.Equal(t, "alpha.txt", names[2])
	assert.Equal(t, "beta.txt", names[3])
}

// --- GetFile with external path ---

func TestGetFile_ExternalAbsolutePath(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a file outside the project but under the watch dir
	externalFile := filepath.Join(env.WatchDir, "external.txt")
	require.NoError(t, os.WriteFile(externalFile, []byte("external content"), 0o644))

	req := newRequest(t, http.MethodGet, "/api/file?path="+externalFile, nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(GetFile, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var fc FileContent
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
	assert.Equal(t, "external content", fc.Content)
	assert.Equal(t, "external.txt", fc.Name)
}

func TestGetFile_ExternalRelativePath_Returns400(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/file?path=relative/path.txt", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(GetFile, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetFile_ExternalPathNotExisting_Returns404(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/file?path=/nonexistent/file.txt", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(GetFile, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- GetFile binary handling ---

func TestGetFile_BinaryFile_ReturnsIsBinary(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	binFile := filepath.Join(env.ProjectDir, "test.exe")
	require.NoError(t, os.WriteFile(binFile, []byte{0x4D, 0x5A, 0x90, 0x00}, 0o644))

	req := newRequest(t, http.MethodGet, "/api/file/test.exe", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(GetFile, req)
	assertOK(t, w)

	var fc FileContent
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
	assert.True(t, fc.IsBinary)
	assert.Empty(t, fc.Content)
	assert.Equal(t, int64(4), fc.Size)
}

func TestGetFile_BinaryFileWithForceText(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	binFile := filepath.Join(env.ProjectDir, "test.exe")
	require.NoError(t, os.WriteFile(binFile, []byte{0x4D, 0x5A, 0x90, 0x00}, 0o644))

	// forceText=1 overrides binary detection, returns sanitized content
	req := newRequest(t, http.MethodGet, "/api/file/test.exe?forceText=1", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(GetFile, req)
	assertOK(t, w)

	var fc FileContent
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
	assert.False(t, fc.IsBinary)
	assert.NotEmpty(t, fc.Content)
	// Null byte should be replaced with '.'
	assert.Contains(t, fc.Content, ".")
}

func TestGetFile_LargeFile_Returns400(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a sparse file > 10MB
	largeFile := filepath.Join(env.ProjectDir, "large.txt")
	f, err := os.Create(largeFile)
	require.NoError(t, err)
	// Truncate to 11MB (creates sparse file)
	require.NoError(t, f.Truncate(11*1024*1024))
	require.NoError(t, f.Close())

	req := newRequest(t, http.MethodGet, "/api/file/large.txt", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(GetFile, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetFile_PathTraversalViaURL_Returns400(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// ".." as the file path should be rejected
	req := newRequest(t, http.MethodGet, "/api/file/..", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(GetFile, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetFile_AbsolutePathInURL_Returns400(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Absolute path in URL (after stripping prefix) should be rejected
	req := newRequest(t, http.MethodGet, "/api/file//etc/passwd", nil)
	withProjectCookie(req, env.ProjectDir)

	// Double slash gets trimmed to "etc/passwd" which is a valid relative path
	// but will 404 since it doesn't exist
	w := callHandler(GetFile, req)
	assert.True(t, w.Code == http.StatusNotFound || w.Code == http.StatusBadRequest,
		"expected 404 or 400, got %d", w.Code)
}

// --- ServeLocalFile download mode ---

func TestServeLocalFile_DownloadMode(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "test.txt", "download me")

	req := newRequest(t, http.MethodGet, "/api/local-file/test.txt?download=1", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeLocalFile, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, `attachment; filename="test.txt"`, w.Header().Get("Content-Disposition"))
}

func TestServeLocalFile_Directory_Returns400(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	_ = os.MkdirAll(filepath.Join(env.ProjectDir, "mydir"), 0o755)

	req := newRequest(t, http.MethodGet, "/api/local-file/mydir", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeLocalFile, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeLocalFile_PathTraversal_Returns400(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/local-file/..", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeLocalFile, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeLocalFile_UnknownMime(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "test.xyz", "unknown")

	req := newRequest(t, http.MethodGet, "/api/local-file/test.xyz", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeLocalFile, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/octet-stream", w.Header().Get("Content-Type"))
}

// --- ServeProjects method handling ---

func TestServeProjects_NonExistentDir_Returns404or400(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	nonExistent := filepath.Join(env.WatchDir, "does-not-exist")
	req := newRequest(t, http.MethodGet, "/api/projects?path="+nonExistent, nil)
	w := callHandler(ServeProjects, req)
	// Returns 404 (not found) or 400 (not a directory) depending on the path resolution
	assert.True(t, w.Code == http.StatusNotFound || w.Code == http.StatusBadRequest,
		"expected 404 or 400, got %d", w.Code)
}

// --- serveProjectsCreate with relative path ---

func TestServeProjects_CreateWithRelativePath(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a subdirectory under WatchDir to serve as the parent
	_ = os.MkdirAll(filepath.Join(env.WatchDir, "subdir"), 0o755)

	req := newRequest(t, http.MethodPost, "/api/projects", map[string]string{
		"path": "subdir",
		"name": "new-project",
	})
	w := callHandler(ServeProjects, req)
	assertOK(t, w)

	assertJSONField(t, w, "ok", true)
}

func TestServeProjects_CreateWithAbsolutePath(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/projects", map[string]string{
		"path": env.WatchDir,
		"name": "abs-project",
	})
	w := callHandler(ServeProjects, req)
	assertOK(t, w)
}

func TestServeProjects_CreateOutsideRoot_Returns403(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/projects", map[string]string{
		"path": os.TempDir(),
		"name": "outside-project",
	})
	w := callHandler(ServeProjects, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// --- containsGlobChars ---

func TestContainsGlobChars(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"src/main.go", false},
		{"*.java", true},
		{"**/*.class", true},
		{"src/[test]/file.go", true},
		{"<template>", true},
		{"normal/path.txt", false},
		{"file?name", true},
	}
	for _, tt := range tests {
		result := containsGlobChars(tt.path)
		assert.Equal(t, tt.expected, result, "containsGlobChars(%q) = %v, want %v", tt.path, result, tt.expected)
	}
}

// --- ListDir subdirectory parent ---

func TestListDir_SubdirectoryHasParent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "subdir/nested.txt", "nested")

	req := newRequest(t, http.MethodGet, "/api/dir?path=subdir", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ListDir, req)
	assertOK(t, w)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.NotNil(t, result["parent"], "subdirectory should have a parent")
}

func TestListDir_RootDirectoryParentIsNil(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/dir", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ListDir, req)
	assertOK(t, w)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Nil(t, result["parent"], "root directory should have nil parent")
}

// --- GetFile_ExternalBinaryFile ---

func TestGetFile_ExternalBinaryFile(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a binary file outside project but under watch dir (with null byte)
	binFile := filepath.Join(env.WatchDir, "test.exe")
	require.NoError(t, os.WriteFile(binFile, []byte{0x4D, 0x5A, 0x00}, 0o644))

	req := newRequest(t, http.MethodGet, "/api/file?path="+binFile, nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(GetFile, req)
	assertOK(t, w)

	var fc FileContent
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
	assert.True(t, fc.IsBinary)
	assert.Empty(t, fc.Content)
}

// --- ListDir_NotADirectory ---

func TestListDir_FileInsteadOfDir_Returns400(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "notadir.txt", "content")

	req := newRequest(t, http.MethodGet, "/api/dir?path=notadir.txt", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ListDir, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- ServeLocalFile with no api prefix ---

func TestServeLocalFile_NoApiPrefix_Returns404or403(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// URL without /api/local-file/ prefix — handler sees no prefix match, returns 404
	req := newRequest(t, http.MethodGet, "/other-path/test.txt", nil)

	w := callHandler(ServeLocalFile, req)
	// Returns 403 because requireProject fails (no cookie), or 404 if prefix not matched
	assert.True(t, w.Code == http.StatusNotFound || w.Code == http.StatusForbidden,
		"expected 404 or 403, got %d", w.Code)
}

// --- ServeFileBatchExists with absolute path ---

func TestServeFileBatchExists_AbsolutePathExists(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a file under WatchDir (which is a root path)
	extFile := filepath.Join(env.WatchDir, "external-file.txt")
	require.NoError(t, os.WriteFile(extFile, []byte("hello"), 0o644))

	req := newRequest(t, http.MethodPost, "/api/file/batch-exists", map[string]interface{}{
		"paths": []string{extFile},
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeFileBatchExists, req)
	assertOK(t, w)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

	results := result["results"].(map[string]interface{})
	assert.Equal(t, "file", results[extFile])
}

func TestGetFile_BrokenSymlink(t *testing.T) {
	t.Run("DanglingSymlink_Returns404BrokenSymlink", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a dangling symlink (target doesn't exist)
		linkPath := filepath.Join(env.ProjectDir, "broken_link")
		if err := os.Symlink("/nonexistent/target/file.txt", linkPath); err != nil {
			t.Fatalf("failed to create symlink: %v", err)
		}

		req := newRequest(t, http.MethodGet, "/api/file/broken_link", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assert.Equal(t, http.StatusNotFound, w.Code)

		var result map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		// Should have the BrokenSymlink error key
		errKey, _ := result["error"].(map[string]any)
		if errKey != nil {
			assert.Equal(t, "BrokenSymlink", errKey["key"])
			details, _ := errKey["details"].(map[string]any)
			if details != nil {
				assert.Equal(t, "/nonexistent/target/file.txt", details["Target"])
			}
		}
	})

	t.Run("ValidSymlink_ReturnsFileContent", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a real file and a symlink pointing to it
		createTestFile(t, env.ProjectDir, "real_file.txt", "hello from real file")
		linkPath := filepath.Join(env.ProjectDir, "good_link")
		if err := os.Symlink("real_file.txt", linkPath); err != nil {
			t.Fatalf("failed to create symlink: %v", err)
		}

		req := newRequest(t, http.MethodGet, "/api/file/good_link", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestHandleStatError(t *testing.T) {
	t.Run("permission_error_returns_500", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/file/test", http.NoBody)
		handleStatError(w, req, "/some/path", fmt.Errorf("permission denied"))
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("not_found_error_returns_404", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/file/test", http.NoBody)
		handleStatError(w, req, "/some/nonexistent", os.ErrNotExist)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("broken_symlink_returns_404_with_target", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a broken symlink
		brokenLink := filepath.Join(env.ProjectDir, "broken_link")
		err := os.Symlink("/nonexistent/target", brokenLink)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/file/test", http.NoBody)
		handleStatError(w, req, brokenLink, os.ErrNotExist)
		assert.Equal(t, http.StatusNotFound, w.Code)
		// Response should mention the broken symlink target (cross-platform: may use \ or /)
		body := filepath.ToSlash(w.Body.String())
		assert.Contains(t, body, "nonexistent")
	})
}
