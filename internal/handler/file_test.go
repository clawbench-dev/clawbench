package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

func TestGetFile_OpenAPIYAML_ReturnsSubtype(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	yamlContent := "openapi: '3.0.0'\ninfo:\n  title: Test API\n  version: '1.0'\npaths: {}"
	createTestFile(t, env.ProjectDir, "openapi.yaml", yamlContent)

	req := newRequest(t, http.MethodGet, "/api/fs/file/openapi.yaml", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(GetFile, req)
	assertOK(t, w)

	var fc FileContent
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
	assert.Equal(t, model.SubtypeOpenAPI, fc.Subtype)
	assert.NotEmpty(t, fc.SpecJSON, "specJson should be populated for YAML OpenAPI files")
}

func TestGetFile_OpenAPIJSON_ReturnsSubtype(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	jsonContent := `{"openapi":"3.0.0","info":{"title":"Test API","version":"1.0"},"paths":{}}`
	createTestFile(t, env.ProjectDir, "openapi.json", jsonContent)

	req := newRequest(t, http.MethodGet, "/api/fs/file/openapi.json", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(GetFile, req)
	assertOK(t, w)

	var fc FileContent
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
	assert.Equal(t, model.SubtypeOpenAPI, fc.Subtype)
	assert.Empty(t, fc.SpecJSON, "specJson should be empty for JSON OpenAPI files (content is already JSON)")
}

func TestGetFile_RegularYAML_ReturnsNoSubtype(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "config.yaml", "foo: bar\nbaz: 123")

	req := newRequest(t, http.MethodGet, "/api/fs/file/config.yaml", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(GetFile, req)
	assertOK(t, w)

	var fc FileContent
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
	assert.Empty(t, fc.Subtype)
	assert.Empty(t, fc.SpecJSON)
}

// --- ServeLocalFile with external ?path= query param ---

func TestServeLocalFile_ExternalPath_ServesFile(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a file outside the project directory but under WatchDir (a root path)
	extDir := filepath.Join(env.WatchDir, "external")
	require.NoError(t, os.MkdirAll(extDir, 0o755))
	extFile := filepath.Join(extDir, "photo.png")
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
	require.NoError(t, os.WriteFile(extFile, pngData, 0o644))

	req := newRequest(t, http.MethodGet, "/api/fs/raw/?target="+url.QueryEscape(extFile), nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeLocalFile, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
}

func TestServeLocalFile_ExternalPath_DownloadMode(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	extDir := filepath.Join(env.WatchDir, "external")
	require.NoError(t, os.MkdirAll(extDir, 0o755))
	extFile := filepath.Join(extDir, "data.csv")
	require.NoError(t, os.WriteFile(extFile, []byte("a,b,c\n1,2,3\n"), 0o644))

	req := newRequest(t, http.MethodGet, "/api/fs/raw/?download=1&target="+url.QueryEscape(extFile), nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeLocalFile, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
	assert.Contains(t, w.Header().Get("Content-Disposition"), "data.csv")
}

func TestServeLocalFile_ExternalPath_NotFound_Returns404(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/fs/raw/?target="+url.QueryEscape(filepath.Join(env.WatchDir, "nonexistent.txt")), nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeLocalFile, req)
	assertStatus(t, w, http.StatusNotFound)
}

func TestServeLocalFile_ExternalPath_RelativePath_Returns400(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// ?path= with a relative path should be rejected
	req := newRequest(t, http.MethodGet, "/api/fs/raw/?target=relative/path.txt", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeLocalFile, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeLocalFile_ExternalPath_OutsideRoot_Returns403(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Try to access a path outside the configured root paths
	// On most systems /etc/hostname exists, but it's outside env.WatchDir
	// We construct a path that is definitely outside WatchDir
	outsidePath := "/proc/version" // unlikely to be under WatchDir
	req := newRequest(t, http.MethodGet, "/api/fs/raw/?target="+url.QueryEscape(outsidePath), nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeLocalFile, req)
	// Should be 403 (AccessDenied) since /proc is not under WatchDir
	assertStatus(t, w, http.StatusForbidden)
}

// --- GetFile with ?path= root path validation ---

func TestGetFile_ExternalPath_OutsideRoot_Returns403(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Try to access a path outside the configured root paths
	outsidePath := "/proc/version"
	req := newRequest(t, http.MethodGet, "/api/fs/file/?target="+url.QueryEscape(outsidePath), nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(GetFile, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestGetFile_ExternalPath_UnderRoot_ServesFile(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a file under WatchDir (which is a root path)
	extFile := filepath.Join(env.WatchDir, "external-readme.md")
	require.NoError(t, os.WriteFile(extFile, []byte("# Hello"), 0o644))

	req := newRequest(t, http.MethodGet, "/api/fs/file/?target="+url.QueryEscape(extFile), nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(GetFile, req)
	assertOK(t, w)

	var result FileContent
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, "# Hello", result.Content)
	assert.Equal(t, extFile, result.Path) // external files return absolute path
}

func TestGetFile_DoubleSlashPath(t *testing.T) {
	t.Run("DoubleSlashPath_ReturnsFileContent", func(t *testing.T) {
		// Regression test: when encodeURIComponent("/path") produces %2Fpath,
		// Go's ServeMux decodes it back to /, creating a double-slash URL like
		// /api/fs/file//docs/dev/file.md. This should NOT return InvalidFilePath.
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "docs/dev/test.md", "# Hello")

		// Simulate the double-slash URL that results from encodeURIComponent("/path")
		req := newRequest(t, http.MethodGet, "/api/fs/file//docs/dev/test.md", nil)
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

		req := newRequest(t, http.MethodGet, "/api/fs/file/docs/dev/test.md", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)
		assert.Equal(t, "# Hello", result["content"])
	})
}

func TestGetFile(t *testing.T) {
	t.Run("ReadTextFile", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "test.txt", "hello world")

		req := newRequest(t, http.MethodGet, "/api/fs/file/test.txt", nil)
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

		req := newRequest(t, http.MethodGet, "/api/fs/file/nonexistent.txt", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assertStatus(t, w, http.StatusNotFound)
	})

	t.Run("NoProjectCookie_Returns403", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/fs/file/test.txt", nil)

		w := callHandler(GetFile, req)
		assertStatus(t, w, http.StatusForbidden)
	})

	t.Run("PathTraversal_Returns400Or403", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/fs/file/../../../etc/passwd", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assert.True(t, w.Code == http.StatusBadRequest || w.Code == http.StatusForbidden,
			"expected 400 or 403, got %d", w.Code)
	})

	t.Run("DirectoryInsteadOfFile_Returns400", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		_ = os.MkdirAll(filepath.Join(env.ProjectDir, "mydir"), 0o755)

		req := newRequest(t, http.MethodGet, "/api/fs/file/mydir", nil)
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

		req := newRequest(t, http.MethodGet, "/api/fs/raw/test.png", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeLocalFile, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
	})

	t.Run("FileNotFound_Returns404", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/fs/raw/missing.png", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeLocalFile, req)
		assertStatus(t, w, http.StatusNotFound)
	})

	t.Run("NoProjectCookie_Returns403", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/fs/raw/test.png", nil)

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

		req := newRequest(t, http.MethodGet, "/api/fs/raw//assets/img/test.png", nil)
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

// --- buildDirEntries with image files ---

func TestBuildDirEntries_ImageFiles(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create image files
	createTestFile(t, env.ProjectDir, "photo.png", "png-data")
	createTestFile(t, env.ProjectDir, "doc.txt", "text-data")
	_ = os.MkdirAll(filepath.Join(env.ProjectDir, "images"), 0o755)

	req := newRequest(t, http.MethodGet, "/api/dir", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ListDir, req)
	assertOK(t, w)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

	items, ok := result["items"].([]interface{})
	require.True(t, ok)

	// Find photo.png entry and verify it has type="image"
	for _, item := range items {
		entry, _ := item.(map[string]interface{})
		name, _ := entry["name"].(string)
		if name == "photo.png" {
			assert.Equal(t, "image", entry["type"], "png file should have type=image")
		}
		if name == "doc.txt" {
			assert.Equal(t, "file", entry["type"], "txt file should have type=file")
		}
		if name == "images" {
			assert.Equal(t, "dir", entry["type"], "directory should have type=dir")
		}
	}
}

// --- buildDirEntries with symlinks ---

// listDirItems calls ListDir for the given project and returns the parsed items
// keyed by name.
func listDirItems(t *testing.T, projectDir string) map[string]map[string]interface{} {
	t.Helper()
	req := newRequest(t, http.MethodGet, "/api/dir", nil)
	withProjectCookie(req, projectDir)
	w := callHandler(ListDir, req)
	assertOK(t, w)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	items, ok := result["items"].([]interface{})
	require.True(t, ok)

	byName := make(map[string]map[string]interface{}, len(items))
	for _, item := range items {
		entry, _ := item.(map[string]interface{})
		name, _ := entry["name"].(string)
		byName[name] = entry
	}
	return byName
}

func TestBuildDirEntries_SymlinkToDir(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	_ = os.MkdirAll(filepath.Join(env.ProjectDir, "realtarget"), 0o755)
	require.NoError(t, os.Symlink("realtarget", filepath.Join(env.ProjectDir, "linked")))

	byName := listDirItems(t, env.ProjectDir)

	linked, ok := byName["linked"]
	require.True(t, ok, "symlink entry should be listed")
	assert.Equal(t, "dir", linked["type"], "symlink-to-directory should be typed as dir")
	assert.Equal(t, true, linked["symlink"], "symlink entry should be marked as symlink")
	assert.NotEqual(t, true, linked["broken"], "valid symlink should not be broken")
}

func TestBuildDirEntries_SymlinkToDir_Navigable(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	_ = os.MkdirAll(filepath.Join(env.ProjectDir, "realtarget"), 0o755)
	require.NoError(t, os.Symlink("realtarget", filepath.Join(env.ProjectDir, "linked")))
	createTestFile(t, filepath.Join(env.ProjectDir, "realtarget"), "inner.txt", "hi")

	// Navigate into the symlinked directory.
	req := newRequest(t, http.MethodGet, "/api/dir?path=linked", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ListDir, req)
	assertOK(t, w)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	items, ok := result["items"].([]interface{})
	require.True(t, ok)
	require.Len(t, items, 1, "linked dir should contain its target's file")
	entry, _ := items[0].(map[string]interface{})
	assert.Equal(t, "inner.txt", entry["name"])
}

func TestBuildDirEntries_SymlinkToFile(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "real.txt", "content")
	require.NoError(t, os.Symlink("real.txt", filepath.Join(env.ProjectDir, "link.txt")))

	byName := listDirItems(t, env.ProjectDir)

	link, ok := byName["link.txt"]
	require.True(t, ok, "symlink entry should be listed")
	assert.Equal(t, "file", link["type"], "symlink-to-file should stay typed as file")
	assert.Equal(t, true, link["symlink"], "symlink entry should be marked as symlink")
	assert.NotEqual(t, true, link["broken"], "valid file symlink should not be broken")
}

func TestBuildDirEntries_DanglingSymlink(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	require.NoError(t, os.Symlink("missing-target", filepath.Join(env.ProjectDir, "dangling")))

	byName := listDirItems(t, env.ProjectDir)

	dangling, ok := byName["dangling"]
	require.True(t, ok, "dangling symlink should stay listed")
	assert.Equal(t, "file", dangling["type"], "dangling symlink should be non-navigable")
	assert.Equal(t, true, dangling["symlink"])
	assert.Equal(t, true, dangling["broken"], "dangling symlink should be marked broken")
}

func TestBuildDirEntries_SymlinkEscapingRoot_NotNavigable(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Target lives outside the project (but still within the watch dir/root).
	_ = os.MkdirAll(filepath.Join(env.WatchDir, "outside"), 0o755)
	createTestFile(t, env.WatchDir, "secret.txt", "secret")
	require.NoError(t, os.Symlink(filepath.Join(env.WatchDir, "outside"), filepath.Join(env.ProjectDir, "escape")))

	byName := listDirItems(t, env.ProjectDir)

	escape, ok := byName["escape"]
	require.True(t, ok)
	assert.Equal(t, "file", escape["type"], "symlink escaping project root should be non-navigable")
	assert.Equal(t, true, escape["symlink"])
}

func TestBuildDirEntries_RegularEntriesUnaffected(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "a.txt", "a")
	_ = os.MkdirAll(filepath.Join(env.ProjectDir, "adir"), 0o755)

	byName := listDirItems(t, env.ProjectDir)

	dir, ok := byName["adir"]
	require.True(t, ok)
	assert.Equal(t, "dir", dir["type"])
	assert.NotEqual(t, true, dir["symlink"], "regular dir should not be marked as symlink")

	file, ok := byName["a.txt"]
	require.True(t, ok)
	assert.Equal(t, "file", file["type"])
	assert.NotEqual(t, true, file["symlink"], "regular file should not be marked as symlink")
}

// --- GetFile with external path ---

func TestGetFile_ExternalAbsolutePath(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a file outside the project but under the watch dir
	externalFile := filepath.Join(env.WatchDir, "external.txt")
	require.NoError(t, os.WriteFile(externalFile, []byte("external content"), 0o644))

	req := newRequest(t, http.MethodGet, "/api/fs/file?target="+externalFile, nil)
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

	req := newRequest(t, http.MethodGet, "/api/fs/file?target=relative/path.txt", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(GetFile, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetFile_ExternalPathNotExisting_Returns404(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Use a path under WatchDir (root path) that doesn't exist
	missingPath := filepath.Join(env.WatchDir, "nonexistent", "file.txt")
	req := newRequest(t, http.MethodGet, "/api/fs/file?target="+url.QueryEscape(missingPath), nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(GetFile, req)
	// macOS sandbox may return 403 instead of 404 for paths outside allowed directory
	assert.Contains(t, []int{http.StatusNotFound, http.StatusForbidden}, w.Code, "expected 404 or 403 for missing/inaccessible file")
}

// --- GetFile binary handling ---

func TestGetFile_BinaryFile_ReturnsIsBinary(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	binFile := filepath.Join(env.ProjectDir, "test.exe")
	require.NoError(t, os.WriteFile(binFile, []byte{0x4D, 0x5A, 0x90, 0x00}, 0o644))

	req := newRequest(t, http.MethodGet, "/api/fs/file/test.exe", nil)
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
	req := newRequest(t, http.MethodGet, "/api/fs/file/test.exe?forceText=1", nil)
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

	req := newRequest(t, http.MethodGet, "/api/fs/file/large.txt", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(GetFile, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// A line-window request streams the file line by line, so the whole-file size
// ceiling must not reject it. This is what lets the quick-preview pane open a
// file larger than maxGetFileBytes at all.
func TestGetFile_LineWindowIgnoresWholeFileSizeLimit(t *testing.T) {
	t.Run("WindowSucceedsPastSizeLimit", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// 11 MiB of real lines, so the window read has content to return (a
		// sparse file would be all NULs and read back as one giant line).
		largeFile := filepath.Join(env.ProjectDir, "large.log")
		f, err := os.Create(largeFile)
		require.NoError(t, err)
		line := strings.Repeat("x", 1023) + "\n" // 1 KiB per line
		for range 11 * 1024 {
			_, err := f.WriteString(line)
			require.NoError(t, err)
		}
		require.NoError(t, f.Close())

		req := newRequest(t, http.MethodGet, "/api/fs/file/large.log?lineStart=2&lineEnd=3", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assertOK(t, w)

		var fc FileContent
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
		assert.Equal(t, strings.Repeat("x", 1023)+"\n"+strings.Repeat("x", 1023), fc.Content)
		// The file ends on a separator, so the trailing empty line counts too.
		assert.Equal(t, 11*1024+1, fc.TotalLines)
		assert.Equal(t, 2, fc.WindowStart)
		assert.Equal(t, 3, fc.WindowEnd)
	})

	t.Run("WholeFileRequestStillRejected", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		largeFile := filepath.Join(env.ProjectDir, "large.log")
		f, err := os.Create(largeFile)
		require.NoError(t, err)
		require.NoError(t, f.Truncate(11*1024*1024))
		require.NoError(t, f.Close())

		req := newRequest(t, http.MethodGet, "/api/fs/file/large.log", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assertStatus(t, w, http.StatusBadRequest)
		assert.Contains(t, w.Body.String(), "FileTooLarge")
	})

	t.Run("WindowOnMissingFileStill404", func(t *testing.T) {
		// Skipping the size ceiling must not skip path validation.
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/fs/file/nope.txt?lineStart=1&lineEnd=2", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assertStatus(t, w, http.StatusNotFound)
	})
}

// A non-text extension that sniffs as text (LICENSE, an extensionless script)
// is line-windowable: the window path does not need the whole-file sanitize.
func TestGetFile_LineWindowOnNonTextExtensionThatIsText(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "LICENSE", "one\ntwo\nthree\nfour")

	req := newRequest(t, http.MethodGet, "/api/fs/file/LICENSE?lineStart=2&lineEnd=3", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(GetFile, req)
	assertOK(t, w)

	var fc FileContent
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
	assert.Equal(t, "two\nthree", fc.Content)
	assert.Equal(t, 4, fc.TotalLines)
	assert.Equal(t, 2, fc.WindowStart)
	assert.Equal(t, 3, fc.WindowEnd)
}

func TestGetFile_PathTraversalViaURL_Returns400(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// ".." as the file path should be rejected
	req := newRequest(t, http.MethodGet, "/api/fs/file/..", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(GetFile, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetFile_AbsolutePathInURL_Returns400(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Absolute path in URL (after stripping prefix) should be rejected
	req := newRequest(t, http.MethodGet, "/api/fs/file//etc/passwd", nil)
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

	req := newRequest(t, http.MethodGet, "/api/fs/raw/test.txt?download=1", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeLocalFile, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, `attachment; filename="test.txt"; filename*=UTF-8''test.txt`, w.Header().Get("Content-Disposition"))
}

func TestServeLocalFile_Directory_Returns400(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	_ = os.MkdirAll(filepath.Join(env.ProjectDir, "mydir"), 0o755)

	req := newRequest(t, http.MethodGet, "/api/fs/raw/mydir", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeLocalFile, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeLocalFile_PathTraversal_Returns400(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/fs/raw/..", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeLocalFile, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeLocalFile_UnknownMime(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "test.xyz", "unknown")

	req := newRequest(t, http.MethodGet, "/api/fs/raw/test.xyz", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeLocalFile, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/octet-stream", w.Header().Get("Content-Type"))
}

// The HTML file preview loads a document through /api/fs/raw/ and lets the
// browser fetch its own subresources relative to it. Every asset type the
// browser will accept must therefore be served with its real MIME type: the
// octet-stream fallback silently drops stylesheets, refuses ES modules, and
// makes the browser download the document instead of rendering it.
func TestServeLocalFile_WebAssets_CorrectMime(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	cases := []struct{ name, wantMime string }{
		{"index.html", "text/html"},
		{"page.htm", "text/html"},
		{"style.css", "text/css"},
		{"app.js", "text/javascript"},
		{"mod.mjs", "text/javascript"},
		{"data.json", "application/json"},
		{"font.woff2", "font/woff2"},
		{"font.woff", "font/woff"},
		{"font.ttf", "font/ttf"},
	}
	for _, tc := range cases {
		createTestFile(t, env.ProjectDir, tc.name, "x")

		req := newRequest(t, http.MethodGet, "/api/fs/raw/"+tc.name, nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeLocalFile, req)
		assert.Equal(t, http.StatusOK, w.Code, tc.name)
		assert.Equal(t, tc.wantMime, w.Header().Get("Content-Type"), tc.name)
	}
}

// http.ServeFile treats a file named "index.html" as a directory index and
// answers 301 to "./". That breaks the HTML preview, whose iframe points
// straight at .../index.html: the redirect rewrites the URL to the parent
// directory and the preview renders a directory error instead of the document.
// ServeContent has no such special case.
func TestServeLocalFile_IndexHtml_NoRedirect(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "index.html", "<h1>hi</h1>")

	req := newRequest(t, http.MethodGet, "/api/fs/raw/index.html", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeLocalFile, req)
	assert.Equal(t, http.StatusOK, w.Code, "index.html must be served, not redirected")
	assert.Empty(t, w.Header().Get("Location"), "must not redirect")
	assert.Equal(t, "text/html", w.Header().Get("Content-Type"))
	assert.Equal(t, "<h1>hi</h1>", w.Body.String())
}

// Same fix on the absolute ?target= form, which the preview also uses for files
// outside the project directory.
func TestServeLocalFile_IndexHtml_TargetForm_NoRedirect(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "index.html", "<h1>hi</h1>")
	abs := filepath.Join(env.ProjectDir, "index.html")

	req := newRequest(t, http.MethodGet, "/api/fs/raw/?target="+url.QueryEscape(abs), nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeLocalFile, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Header().Get("Location"))
	assert.Equal(t, "text/html", w.Header().Get("Content-Type"))
}

// The web-asset MIME additions must NOT leak into the base table, because that
// table is shared with the PUBLIC token-scoped share endpoint. Serving text/html
// from an unauthenticated endpoint turns any shared .html file into same-origin
// script execution for a logged-in visitor (it would run with their session
// cookie). Verified in a browser: with text/html the shared page reached
// authenticated APIs; with the octet-stream fallback the browser downloads it.
func TestWebAssetMimeTypes_NotSharedWithPublicShareEndpoint(t *testing.T) {
	for _, ext := range []string{".html", ".htm", ".xhtml", ".js", ".mjs", ".css"} {
		_, inBase := mimeTypes[ext]
		assert.False(t, inBase,
			"%s must not be in mimeTypes: that table is served by the unauthenticated share endpoint", ext)
	}

	// The authenticated resolver still yields them.
	assert.Equal(t, "text/html", mimeForLocalFile(".html"))
	assert.Equal(t, "text/css", mimeForLocalFile(".css"))
	assert.Equal(t, "text/javascript", mimeForLocalFile(".js"))
	// ...and the base table's own entries still win / fall through correctly.
	assert.Equal(t, "image/png", mimeForLocalFile(".png"))
	assert.Equal(t, "application/octet-stream", mimeForLocalFile(".xyz"))
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

	req := newRequest(t, http.MethodGet, "/api/fs/file?target="+binFile, nil)
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

	// URL without /api/fs/raw/ prefix — handler sees no prefix match, returns 404
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

func TestServeFileBatchBase64(t *testing.T) {
	t.Run("ImageFile_ReturnsBase64", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a small PNG (1x1 red pixel)
		pngData := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00\x90wS\xde\x00\x00\x00\x0cIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")
		createTestFile(t, env.ProjectDir, "img/logo.png", string(pngData))

		req := newRequest(t, http.MethodPost, "/api/file/batch-base64", map[string]interface{}{
			"paths": []string{"img/logo.png"},
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchBase64, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)

		results, ok := result["results"].(map[string]interface{})
		assert.True(t, ok)
		item, ok := results["img/logo.png"].(map[string]interface{})
		assert.True(t, ok)
		assert.Equal(t, "image/png", item["mime"])
		assert.NotEmpty(t, item["data"])
	})

	t.Run("NonImageFile_Skipped", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "src/main.go", "package main")

		req := newRequest(t, http.MethodPost, "/api/file/batch-base64", map[string]interface{}{
			"paths": []string{"src/main.go"},
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchBase64, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)

		results, ok := result["results"].(map[string]interface{})
		assert.True(t, ok)
		assert.Empty(t, results) // no image results
		skipped, ok := result["skipped"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, skipped, 1)
	})

	t.Run("EmptyPaths_Returns400", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/file/batch-base64", map[string]interface{}{
			"paths": []string{},
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchBase64, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("TooManyPaths_Returns400", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		paths := make([]string, 51)
		for i := range paths {
			paths[i] = "img/a.png"
		}
		req := newRequest(t, http.MethodPost, "/api/file/batch-base64", map[string]interface{}{
			"paths": paths,
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchBase64, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("NonExistentImage_Skipped", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/file/batch-base64", map[string]interface{}{
			"paths": []string{"img/missing.png"},
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchBase64, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)

		results, ok := result["results"].(map[string]interface{})
		assert.True(t, ok)
		assert.Empty(t, results)
		skipped, ok := result["skipped"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, skipped, 1)
	})

	t.Run("WrongMethod_Returns405", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/file/batch-base64", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchBase64, req)
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("NoProjectCookie_Returns403", func(t *testing.T) {
		req := newRequest(t, http.MethodPost, "/api/file/batch-base64", map[string]interface{}{
			"paths": []string{"img/a.png"},
		})

		w := callHandler(ServeFileBatchBase64, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("MixedImageAndNonImage", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		pngData := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00\x90wS\xde\x00\x00\x00\x0cIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")
		createTestFile(t, env.ProjectDir, "img/logo.png", string(pngData))
		createTestFile(t, env.ProjectDir, "src/main.go", "package main")

		req := newRequest(t, http.MethodPost, "/api/file/batch-base64", map[string]interface{}{
			"paths": []string{"img/logo.png", "src/main.go"},
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchBase64, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)

		results := result["results"].(map[string]interface{})
		assert.Len(t, results, 1) // only the PNG
		skipped := result["skipped"].([]interface{})
		assert.Len(t, skipped, 1) // the .go file
	})

	t.Run("OversizedFile_Skipped", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a PNG larger than 2MB
		bigData := make([]byte, 2*1024*1024+100)
		copy(bigData, "\x89PNG\r\n\x1a\n")
		createTestFile(t, env.ProjectDir, "img/big.png", string(bigData))

		req := newRequest(t, http.MethodPost, "/api/file/batch-base64", map[string]interface{}{
			"paths": []string{"img/big.png"},
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchBase64, req)
		assertOK(t, w)

		var result map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &result)
		assert.NoError(t, err)

		results := result["results"].(map[string]interface{})
		assert.Empty(t, results)
		skipped := result["skipped"].([]interface{})
		assert.Len(t, skipped, 1)
	})
}

func TestBatchBase64ResolvePath(t *testing.T) {
	t.Run("RelativePath", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()
		abs, ok := batchBase64ResolvePath("img/logo.png", env.ProjectDir)
		assert.True(t, ok)
		assert.Equal(t, filepath.Join(env.ProjectDir, "img/logo.png"), abs)
	})

	t.Run("AbsolutePathOutsideRoot", func(t *testing.T) {
		abs, ok := batchBase64ResolvePath("/nonexistent/path/that/should/fail", "/unused")
		assert.False(t, ok)
		assert.Empty(t, abs)
	})

	t.Run("AbsolutePathUnderProject", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()
		// Create the file so the path exists for symlink resolution
		createTestFile(t, env.ProjectDir, "img/logo.png", "test")
		testPath := filepath.Join(env.ProjectDir, "img/logo.png")
		abs, ok := batchBase64ResolvePath(testPath, "/unused")
		assert.True(t, ok)
		assert.Equal(t, testPath, abs)
	})
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

		req := newRequest(t, http.MethodGet, "/api/fs/file/broken_link", nil)
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

		req := newRequest(t, http.MethodGet, "/api/fs/file/good_link", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var fc FileContent
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
		assert.Equal(t, "hello from real file", fc.Content)
		assert.True(t, fc.IsSymlink, "symlink file should be marked isSymlink")
		assert.Equal(t, "real_file.txt", fc.LinkTarget, "linkTarget should resolve to the target file")
	})

	t.Run("RegularFile_NotMarkedSymlink", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "plain.txt", "hello")
		req := newRequest(t, http.MethodGet, "/api/fs/file/plain.txt", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var fc FileContent
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
		assert.False(t, fc.IsSymlink, "regular file should not be marked isSymlink")
		assert.Empty(t, fc.LinkTarget, "regular file should have no linkTarget")
	})
}

// TestGetFileLineWindow covers the ?lineStart/?lineEnd path used by the quick
// preview pane to fetch only the lines it can render.
func TestGetFileLineWindow(t *testing.T) {
	t.Run("ReturnsOnlyRequestedLinesPlusTotal", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		var lines []string
		for i := 1; i <= 1000; i++ {
			lines = append(lines, fmt.Sprintf("line %d", i))
		}
		createTestFile(t, env.ProjectDir, "big.txt", strings.Join(lines, "\n"))

		req := newRequest(t, http.MethodGet, "/api/fs/file/big.txt?lineStart=101&lineEnd=105", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assertOK(t, w)

		var fc FileContent
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
		assert.Equal(t, "line 101\nline 102\nline 103\nline 104\nline 105", fc.Content)
		assert.Equal(t, 1000, fc.TotalLines, "total line count must reflect the whole file")
		assert.Equal(t, 101, fc.WindowStart)
		assert.Equal(t, 105, fc.WindowEnd)
		assert.False(t, fc.WindowTruncated)
	})

	t.Run("LoneLineStartMeansSingleLine", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "a.txt", "one\ntwo\nthree")

		req := newRequest(t, http.MethodGet, "/api/fs/file/a.txt?lineStart=2", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assertOK(t, w)

		var fc FileContent
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
		assert.Equal(t, "two", fc.Content)
		assert.Equal(t, 3, fc.TotalLines)
		assert.Equal(t, 2, fc.WindowStart)
		assert.Equal(t, 2, fc.WindowEnd)
	})

	t.Run("CRLFCountsAsOneSeparator", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "crlf.txt", "a\r\nb\r\nc")

		req := newRequest(t, http.MethodGet, "/api/fs/file/crlf.txt?lineStart=2&lineEnd=3", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assertOK(t, w)

		var fc FileContent
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
		assert.Equal(t, "b\nc", fc.Content)
		assert.Equal(t, 3, fc.TotalLines, "CRLF must not be counted as two lines")
	})

	t.Run("TrailingNewlineYieldsTrailingEmptyLine", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// "a\n" splits to ["a", ""] in JS — 2 lines, matching the frontend.
		createTestFile(t, env.ProjectDir, "trail.txt", "a\n")

		req := newRequest(t, http.MethodGet, "/api/fs/file/trail.txt?lineStart=2&lineEnd=2", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assertOK(t, w)

		var fc FileContent
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
		assert.Equal(t, 2, fc.TotalLines)
		assert.Equal(t, "", fc.Content)
		assert.Equal(t, 2, fc.WindowStart)
		assert.Equal(t, 2, fc.WindowEnd)
	})

	t.Run("WindowPastEOFReportsNoLines", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "short.txt", "a\nb\nc")

		req := newRequest(t, http.MethodGet, "/api/fs/file/short.txt?lineStart=500&lineEnd=510", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assertOK(t, w)

		var fc FileContent
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
		assert.Equal(t, 3, fc.TotalLines)
		assert.Equal(t, "", fc.Content)
		// windowEnd < windowStart signals "no lines captured".
		assert.Less(t, fc.WindowEnd, fc.WindowStart)
	})

	t.Run("EmptyFileHasZeroLines", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "empty.txt", "")

		req := newRequest(t, http.MethodGet, "/api/fs/file/empty.txt?lineStart=1&lineEnd=10", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assertOK(t, w)

		var fc FileContent
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
		assert.Equal(t, 0, fc.TotalLines)
		assert.Equal(t, "", fc.Content)
	})

	t.Run("WindowIsCappedAtMaxWindowLines", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		var lines []string
		for i := 1; i <= 5000; i++ {
			lines = append(lines, fmt.Sprintf("line %d", i))
		}
		createTestFile(t, env.ProjectDir, "huge.txt", strings.Join(lines, "\n"))

		req := newRequest(t, http.MethodGet, "/api/fs/file/huge.txt?lineStart=1&lineEnd=5000", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assertOK(t, w)

		var fc FileContent
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
		assert.Equal(t, maxWindowLines, strings.Count(fc.Content, "\n")+1)
		assert.Equal(t, 5000, fc.TotalLines)
	})

	t.Run("ByteCapTripsOnOversizedFirstLine_StillReportsEmptyWindow", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Line 1 alone exceeds maxWindowBytes; the rest are short. The window
		// starts at line 1, so "no lines captured" is windowEnd == 0 — which must
		// survive JSON encoding (windowEnd is deliberately not omitempty) or the
		// frontend would mistake an empty window for a successful one.
		giant := strings.Repeat("x", maxWindowBytes+1)
		createTestFile(t, env.ProjectDir, "giant.txt", giant+"\nshort\nlines")

		req := newRequest(t, http.MethodGet, "/api/fs/file/giant.txt?lineStart=1&lineEnd=3", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assertOK(t, w)

		// Assert on the raw JSON: the whole point is that the key is present.
		assert.Contains(t, w.Body.String(), `"windowEnd":0`)

		var fc FileContent
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
		assert.Equal(t, "", fc.Content)
		assert.True(t, fc.WindowTruncated)
		assert.Equal(t, 1, fc.WindowStart)
		assert.Less(t, fc.WindowEnd, fc.WindowStart, "empty window must be signaled")
	})

	t.Run("BareCarriageReturnIsASeparator", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Old-Mac line endings: JS split(/\r\n|\r|\n/) treats each lone \r as one
		// separator, and the Go reader must agree.
		createTestFile(t, env.ProjectDir, "cr.txt", "a\rb\rc")

		req := newRequest(t, http.MethodGet, "/api/fs/file/cr.txt?lineStart=2&lineEnd=3", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assertOK(t, w)

		var fc FileContent
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
		assert.Equal(t, "b\nc", fc.Content)
		assert.Equal(t, 3, fc.TotalLines)
	})

	t.Run("OnlyNewlineIsTwoEmptyLines", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// "\n".split(/\r\n|\r|\n/) === ["", ""] — two empty lines.
		createTestFile(t, env.ProjectDir, "nl.txt", "\n")

		req := newRequest(t, http.MethodGet, "/api/fs/file/nl.txt?lineStart=1&lineEnd=2", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assertOK(t, w)

		var fc FileContent
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
		assert.Equal(t, 2, fc.TotalLines)
		assert.Equal(t, "\n", fc.Content)
	})

	t.Run("InvalidRangesReturn400", func(t *testing.T) {
		cases := []string{
			"lineStart=0",
			"lineStart=-1",
			"lineStart=abc",
			"lineStart=10&lineEnd=5",
			"lineEnd=10",
			"lineStart=1&lineEnd=xyz",
		}
		for _, q := range cases {
			t.Run(q, func(t *testing.T) {
				env, teardown := setupTestEnv(t)
				defer teardown()

				createTestFile(t, env.ProjectDir, "a.txt", "one\ntwo\nthree")

				req := newRequest(t, http.MethodGet, "/api/fs/file/a.txt?"+q, nil)
				withProjectCookie(req, env.ProjectDir)

				w := callHandler(GetFile, req)
				assertStatus(t, w, http.StatusBadRequest)
			})
		}
	})

	t.Run("InvalidRangeIs400EvenForBinaryFiles", func(t *testing.T) {
		// The window is ignored for binary content, but an invalid range must
		// still be rejected: parsing it after the binary early-return made
		// `?lineStart=0` answer 200 isBinary:true, contradicting the documented
		// 400 contract. A NUL byte makes the sniffer classify it as binary.
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "blob.bin", "abc\x00def")

		for _, q := range []string{"lineStart=0", "lineStart=10&lineEnd=5", "lineEnd=10"} {
			t.Run(q, func(t *testing.T) {
				req := newRequest(t, http.MethodGet, "/api/fs/file/blob.bin?"+q, nil)
				withProjectCookie(req, env.ProjectDir)

				w := callHandler(GetFile, req)
				assertStatus(t, w, http.StatusBadRequest)
				assert.Contains(t, w.Body.String(), "InvalidLineRange")
			})
		}

		// And a VALID range on the same binary file keeps its 200 isBinary body.
		req := newRequest(t, http.MethodGet, "/api/fs/file/blob.bin?lineStart=1&lineEnd=2", nil)
		withProjectCookie(req, env.ProjectDir)
		w := callHandler(GetFile, req)
		assertOK(t, w)

		var fc FileContent
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
		assert.True(t, fc.IsBinary)
	})

	t.Run("NoParamsStillReturnsWholeFile", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "a.txt", "one\ntwo\nthree")

		req := newRequest(t, http.MethodGet, "/api/fs/file/a.txt", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assertOK(t, w)

		var fc FileContent
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
		assert.Equal(t, "one\ntwo\nthree", fc.Content)
		assert.Equal(t, 0, fc.TotalLines, "whole-file responses omit window metadata")
		assert.Equal(t, 0, fc.WindowStart)
	})

	t.Run("ForceTextIgnoresWindow", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Non-text extension: forceText takes the sanitize path, which must not
		// be line-windowed (sanitization rewrites bytes, not lines).
		createTestFile(t, env.ProjectDir, "data.bin", "one\ntwo\nthree")

		req := newRequest(t, http.MethodGet, "/api/fs/file/data.bin?forceText=1&lineStart=2&lineEnd=2", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(GetFile, req)
		assertOK(t, w)

		var fc FileContent
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fc))
		assert.Equal(t, "one\ntwo\nthree", fc.Content)
		assert.Equal(t, 0, fc.TotalLines)
	})
}

func TestHandleStatError(t *testing.T) {
	t.Run("permission_error_returns_500", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/fs/file/test", http.NoBody)
		handleStatError(w, req, "/some/path", fmt.Errorf("permission denied"))
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("not_found_error_returns_404", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/fs/file/test", http.NoBody)
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
		req := httptest.NewRequest(http.MethodGet, "/api/fs/file/test", http.NoBody)
		handleStatError(w, req, brokenLink, os.ErrNotExist)
		assert.Equal(t, http.StatusNotFound, w.Code)
		// Response should mention the broken symlink target (cross-platform: may use \ or /)
		body := filepath.ToSlash(w.Body.String())
		assert.Contains(t, body, "nonexistent")
	})
}

func TestServeListTree(t *testing.T) {
	t.Run("recursively_lists_nested_files_with_rel_paths", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		dir := filepath.Join(env.ProjectDir, "src")
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "utils"), 0o755))
		createTestFile(t, dir, "main.go", "package main")
		createTestFile(t, filepath.Join(dir, "utils"), "helper.ts", "export{}")

		req := newRequest(t, http.MethodGet, "/api/file/list-tree?path=src", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeListTree, req)
		assertOK(t, w)

		var result struct {
			Files []struct {
				Rel  string `json:"rel"`
				Size int64  `json:"size"`
			} `json:"files"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.Len(t, result.Files, 2)

		rels := []string{result.Files[0].Rel, result.Files[1].Rel}
		sort.Strings(rels)
		assert.Equal(t, []string{"main.go", "utils/helper.ts"}, rels)
		assert.Greater(t, result.Files[0].Size, int64(0))
	})

	t.Run("empty_directory_returns_no_files", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		empty := filepath.Join(env.ProjectDir, "empty")
		require.NoError(t, os.MkdirAll(empty, 0o755))

		req := newRequest(t, http.MethodGet, "/api/file/list-tree?path=empty", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeListTree, req)
		assertOK(t, w)

		var result struct {
			Files []struct {
				Rel string `json:"rel"`
			} `json:"files"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		assert.Empty(t, result.Files)
	})

	t.Run("single_file_path_returns_itself_at_root", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "a.txt", "data")

		req := newRequest(t, http.MethodGet, "/api/file/list-tree?path=a.txt", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeListTree, req)
		assertOK(t, w)

		var result struct {
			Files []struct {
				Rel string `json:"rel"`
			} `json:"files"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.Len(t, result.Files, 1)
		assert.Equal(t, "a.txt", result.Files[0].Rel)
	})

	t.Run("missing_path_lists_project_root", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "root.txt", "data")

		req := newRequest(t, http.MethodGet, "/api/file/list-tree", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeListTree, req)
		assertOK(t, w)

		var result struct {
			Files []struct {
				Rel string `json:"rel"`
			} `json:"files"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.Len(t, result.Files, 1)
		assert.Equal(t, "root.txt", result.Files[0].Rel)
	})

	t.Run("no_project_cookie_returns_forbidden", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "root.txt", "data")

		req := newRequest(t, http.MethodGet, "/api/file/list-tree", nil)

		w := callHandler(ServeListTree, req)
		assertStatus(t, w, http.StatusForbidden)
	})

	t.Run("path_traversal_returns_forbidden", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/file/list-tree?path=../../etc/passwd", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeListTree, req)
		assertStatus(t, w, http.StatusForbidden)
	})

	t.Run("nonexistent_path_returns_internal_error", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/file/list-tree?path=does-not-exist", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeListTree, req)
		assertStatus(t, w, http.StatusInternalServerError)
	})
}
