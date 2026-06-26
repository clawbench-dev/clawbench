package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServeFileRename(t *testing.T) {
	t.Run("RenameFile_Succeeds", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "old.txt", "hello")

		req := newRequest(t, http.MethodPost, "/api/file/rename", map[string]string{
			"path": "old.txt",
			"name": "new.txt",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileRename, req)
		assertOK(t, w)
		assertJSONField(t, w, "ok", true)

		_, err := os.Stat(filepath.Join(env.ProjectDir, "new.txt"))
		assert.NoError(t, err)
		_, err = os.Stat(filepath.Join(env.ProjectDir, "old.txt"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("MissingPathOrName_Returns400", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Missing name
		req := newRequest(t, http.MethodPost, "/api/file/rename", map[string]string{
			"path": "old.txt",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileRename, req)
		assertStatus(t, w, http.StatusBadRequest)

		// Missing path
		req2 := newRequest(t, http.MethodPost, "/api/file/rename", map[string]string{
			"name": "new.txt",
		})
		withProjectCookie(req2, env.ProjectDir)

		w2 := callHandler(ServeFileRename, req2)
		assertStatus(t, w2, http.StatusBadRequest)
	})

	t.Run("NoProjectCookie_Returns403", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/file/rename", map[string]string{
			"path": "old.txt",
			"name": "new.txt",
		})
		// No project cookie

		w := callHandler(ServeFileRename, req)
		assertStatus(t, w, http.StatusForbidden)
	})

	t.Run("PathTraversalInPath_Returns403", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/file/rename", map[string]string{
			"path": "../../../etc/passwd",
			"name": "hacked",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileRename, req)
		assertStatus(t, w, http.StatusForbidden)
	})

	t.Run("AbsolutePath_UnderWatchDir_Succeeds", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a subdirectory under WatchDir to delete from
		subDir := filepath.Join(env.WatchDir, "subproject")
		_ = os.MkdirAll(subDir, 0o755)
		createTestFile(t, subDir, "file.txt", "data")

		req := newRequest(t, http.MethodPost, "/api/file/rename", map[string]string{
			"path": filepath.Join(subDir, "file.txt"),
			"name": "renamed.txt",
		})
		// No project cookie needed for absolute paths

		w := callHandler(ServeFileRename, req)
		assertOK(t, w)

		_, err := os.Stat(filepath.Join(subDir, "renamed.txt"))
		assert.NoError(t, err)
	})

	t.Run("AbsolutePath_EscapesWatchDir_Returns403", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "file.txt", "data")

		// Use os.TempDir() which is guaranteed to be outside the test's WatchDir
		escapePath := filepath.Join(os.TempDir(), "clawbench-escape-test.txt")
		req := newRequest(t, http.MethodPost, "/api/file/rename", map[string]string{
			"path": escapePath,
			"name": "renamed.txt",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileRename, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("RelativePath_UsesProjectCookie", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "file.txt", "data")

		req := newRequest(t, http.MethodPost, "/api/file/rename", map[string]string{
			"path": "file.txt",
			"name": "renamed.txt",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileRename, req)
		assertOK(t, w)

		_, err := os.Stat(filepath.Join(env.ProjectDir, "renamed.txt"))
		assert.NoError(t, err)
	})

	t.Run("NewNameEscapesRoot_Returns403", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "file.txt", "data")

		// Try to rename to a path that resolves outside root paths
		// Using ../../ to escape from project dir up past WatchDir
		req := newRequest(t, http.MethodPost, "/api/file/rename", map[string]string{
			"path": "file.txt",
			"name": "../../escaped.txt",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileRename, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

func TestServeFileWrite(t *testing.T) {
	t.Run("WriteFullContent_Succeeds", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "write.txt", "old content")

		req := newRequest(t, http.MethodPost, "/api/file/write", map[string]interface{}{
			"path":    "write.txt",
			"content": "new content",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileWrite, req)
		assertOK(t, w)

		data, err := os.ReadFile(filepath.Join(env.ProjectDir, "write.txt"))
		assert.NoError(t, err)
		assert.Equal(t, "new content", string(data))
	})

	t.Run("AtomicWrite_NoTempFileLeft", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "atomic.txt", "before")

		req := newRequest(t, http.MethodPost, "/api/file/write", map[string]interface{}{
			"path":    "atomic.txt",
			"content": "after",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileWrite, req)
		assertOK(t, w)

		// No temp files left behind
		entries, err := os.ReadDir(env.ProjectDir)
		assert.NoError(t, err)
		for _, e := range entries {
			assert.False(t, strings.HasPrefix(e.Name(), ".clawbench-write-"), "temp file left behind: %s", e.Name())
		}
	})

	t.Run("MissingPath_Returns400", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/file/write", map[string]interface{}{
			"content": "data",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileWrite, req)
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("PathTraversal_Returns403", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/file/write", map[string]interface{}{
			"path":    "../../etc/passwd",
			"content": "hacked",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileWrite, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("NoProjectCookie_Returns403", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/file/write", map[string]interface{}{
			"path":    "file.txt",
			"content": "data",
		})

		w := callHandler(ServeFileWrite, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

func TestServeFileDelete(t *testing.T) {
	t.Run("DeleteFile_Succeeds", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "todelete.txt", "bye")

		req := newRequest(t, http.MethodPost, "/api/file/delete", map[string]string{
			"path": "todelete.txt",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileDelete, req)
		assertOK(t, w)
		assertJSONField(t, w, "ok", true)

		_, err := os.Stat(filepath.Join(env.ProjectDir, "todelete.txt"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("DeleteDirectoryRecursive_Succeeds", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "mydir/file1.txt", "a")
		createTestFile(t, env.ProjectDir, "mydir/file2.txt", "b")

		req := newRequest(t, http.MethodPost, "/api/file/delete", map[string]string{
			"path": "mydir",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileDelete, req)
		assertOK(t, w)

		_, err := os.Stat(filepath.Join(env.ProjectDir, "mydir"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("NonExistentFile_Returns404", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/file/delete", map[string]string{
			"path": "nonexistent.txt",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileDelete, req)
		assertStatus(t, w, http.StatusNotFound)
	})

	t.Run("AbsolutePath_UnderWatchDir_Succeeds", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a subdirectory under WatchDir to delete from
		subDir := filepath.Join(env.WatchDir, "subproject")
		_ = os.MkdirAll(subDir, 0o755)
		createTestFile(t, subDir, "del.txt", "gone")

		req := newRequest(t, http.MethodPost, "/api/file/delete", map[string]string{
			"path": filepath.Join(subDir, "del.txt"),
		})
		// No project cookie needed for absolute paths

		w := callHandler(ServeFileDelete, req)
		assertOK(t, w)

		_, err := os.Stat(filepath.Join(subDir, "del.txt"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("AbsolutePath_EscapesWatchDir_Returns403", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Use os.TempDir() which is guaranteed to be outside the test's WatchDir
		escapePath := filepath.Join(os.TempDir(), "clawbench-escape-test.txt")
		req := newRequest(t, http.MethodPost, "/api/file/delete", map[string]string{
			"path": escapePath,
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileDelete, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("RelativePath_UsesProjectCookie", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "del.txt", "gone")

		req := newRequest(t, http.MethodPost, "/api/file/delete", map[string]string{
			"path": "del.txt",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileDelete, req)
		assertOK(t, w)

		_, err := os.Stat(filepath.Join(env.ProjectDir, "del.txt"))
		assert.True(t, os.IsNotExist(err))
	})
}

func TestServeFileBatchDelete(t *testing.T) {
	t.Run("DeleteMultipleFiles_Succeeds", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "a.txt", "aaa")
		createTestFile(t, env.ProjectDir, "b.txt", "bbb")
		createTestFile(t, env.ProjectDir, "c.txt", "ccc")

		req := newRequest(t, http.MethodPost, "/api/file/batch-delete", map[string]interface{}{
			"paths": []string{"a.txt", "b.txt", "c.txt"},
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchDelete, req)
		assertOK(t, w)
		assertJSONField(t, w, "ok", true)
		assertJSONField(t, w, "deleted", float64(3))

		_, err := os.Stat(filepath.Join(env.ProjectDir, "a.txt"))
		assert.True(t, os.IsNotExist(err))
		_, err = os.Stat(filepath.Join(env.ProjectDir, "b.txt"))
		assert.True(t, os.IsNotExist(err))
		_, err = os.Stat(filepath.Join(env.ProjectDir, "c.txt"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("DeleteMixOfFilesAndDirs_Succeeds", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "file.txt", "data")
		createTestFile(t, env.ProjectDir, "mydir/inner.txt", "inner")

		req := newRequest(t, http.MethodPost, "/api/file/batch-delete", map[string]interface{}{
			"paths": []string{"file.txt", "mydir"},
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchDelete, req)
		assertOK(t, w)
		assertJSONField(t, w, "deleted", float64(2))

		_, err := os.Stat(filepath.Join(env.ProjectDir, "file.txt"))
		assert.True(t, os.IsNotExist(err))
		_, err = os.Stat(filepath.Join(env.ProjectDir, "mydir"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("EmptyPaths_Returns400", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/file/batch-delete", map[string]interface{}{
			"paths": []string{},
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchDelete, req)
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("NoProjectCookieWithRelativePaths_ReportsErrors", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/file/batch-delete", map[string]interface{}{
			"paths": []string{"a.txt"},
		})

		w := callHandler(ServeFileBatchDelete, req)
		// Batch delete reports per-path errors instead of failing the whole request
		assertOK(t, w)
		assertJSONField(t, w, "deleted", float64(0))
	})

	t.Run("PathTraversalInOnePath_SkipsThatPathAndDeletesOthers", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "safe.txt", "ok")

		req := newRequest(t, http.MethodPost, "/api/file/batch-delete", map[string]interface{}{
			"paths": []string{"../../../etc/passwd", "safe.txt"},
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchDelete, req)
		assertOK(t, w)
		assertJSONField(t, w, "deleted", float64(1))

		// safe.txt should be deleted
		_, err := os.Stat(filepath.Join(env.ProjectDir, "safe.txt"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("NonExistentPath_SkipsAndReportsError", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "exists.txt", "data")

		req := newRequest(t, http.MethodPost, "/api/file/batch-delete", map[string]interface{}{
			"paths": []string{"exists.txt", "nope.txt"},
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchDelete, req)
		assertOK(t, w)
		assertJSONField(t, w, "deleted", float64(1))

		// exists.txt deleted, nope.txt reported in errors
		_, err := os.Stat(filepath.Join(env.ProjectDir, "exists.txt"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("RelativePaths_UsesProjectCookie", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "x.txt", "x")
		createTestFile(t, env.ProjectDir, "y.txt", "y")

		req := newRequest(t, http.MethodPost, "/api/file/batch-delete", map[string]interface{}{
			"paths": []string{"x.txt", "y.txt"},
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchDelete, req)
		assertOK(t, w)
		assertJSONField(t, w, "deleted", float64(2))

		_, err := os.Stat(filepath.Join(env.ProjectDir, "x.txt"))
		assert.True(t, os.IsNotExist(err))
		_, err = os.Stat(filepath.Join(env.ProjectDir, "y.txt"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("AbsolutePath_UnderRoot_Succeeds", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create files in a subdirectory under WatchDir (not under ProjectDir)
		subDir := filepath.Join(env.WatchDir, "batchdel")
		_ = os.MkdirAll(subDir, 0o755)
		createTestFile(t, subDir, "absdel.txt", "abs delete me")

		req := newRequest(t, http.MethodPost, "/api/file/batch-delete", map[string]interface{}{
			"paths": []string{filepath.Join(subDir, "absdel.txt")},
		})

		w := callHandler(ServeFileBatchDelete, req)
		assertOK(t, w)
		assertJSONField(t, w, "deleted", float64(1))

		_, err := os.Stat(filepath.Join(subDir, "absdel.txt"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("AbsolutePath_OutsideRoot_SkipsPath", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "safe.txt", "safe")

		req := newRequest(t, http.MethodPost, "/api/file/batch-delete", map[string]interface{}{
			"paths": []string{"/etc/passwd", "safe.txt"},
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileBatchDelete, req)
		assertOK(t, w)
		assertJSONField(t, w, "deleted", float64(1))

		// safe.txt should be deleted, /etc/passwd should be skipped
		_, err := os.Stat(filepath.Join(env.ProjectDir, "safe.txt"))
		assert.True(t, os.IsNotExist(err))
	})
}

func TestServeFileCreate(t *testing.T) {
	t.Run("CreateNewFile_Succeeds", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/file/create", map[string]string{
			"name": "newfile.txt",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileCreate, req)
		assertOK(t, w)
		assertJSONField(t, w, "ok", true)

		info, err := os.Stat(filepath.Join(env.ProjectDir, "newfile.txt"))
		assert.NoError(t, err)
		assert.Equal(t, int64(0), info.Size())
	})

	t.Run("FileAlreadyExists_Returns409", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "existing.txt", "already here")

		req := newRequest(t, http.MethodPost, "/api/file/create", map[string]string{
			"name": "existing.txt",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileCreate, req)
		assertStatus(t, w, http.StatusConflict)
	})

	t.Run("MissingName_Returns400", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/file/create", map[string]string{
			"name": "",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileCreate, req)
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("AbsoluteDirPath_Succeeds", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a subdirectory under WatchDir
		subDir := filepath.Join(env.WatchDir, "abscreatedir")
		_ = os.MkdirAll(subDir, 0o755)

		req := newRequest(t, http.MethodPost, "/api/file/create", map[string]string{
			"path": subDir,
			"name": "absfile.txt",
		})

		w := callHandler(ServeFileCreate, req)
		assertOK(t, w)
		assertJSONField(t, w, "ok", true)

		info, err := os.Stat(filepath.Join(subDir, "absfile.txt"))
		assert.NoError(t, err)
		assert.Equal(t, int64(0), info.Size())
	})

	t.Run("AbsoluteDirPathOutsideRoot_Returns403", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/file/create", map[string]string{
			"path": "/tmp/escaped",
			"name": "evil.txt",
		})

		w := callHandler(ServeFileCreate, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

func TestServeDirCreate(t *testing.T) {
	t.Run("CreateNewDirectory_Succeeds", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/dir/create", map[string]string{
			"name": "newdir",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeDirCreate, req)
		assertOK(t, w)
		assertJSONField(t, w, "ok", true)

		info, err := os.Stat(filepath.Join(env.ProjectDir, "newdir"))
		assert.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("MissingName_Returns400", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/dir/create", map[string]string{
			"name": "",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeDirCreate, req)
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("AbsoluteDirPath_Succeeds", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a subdirectory under WatchDir
		subDir := filepath.Join(env.WatchDir, "absdircreate")
		_ = os.MkdirAll(subDir, 0o755)

		req := newRequest(t, http.MethodPost, "/api/dir/create", map[string]string{
			"path": subDir,
			"name": "newsubdir",
		})

		w := callHandler(ServeDirCreate, req)
		assertOK(t, w)
		assertJSONField(t, w, "ok", true)

		info, err := os.Stat(filepath.Join(subDir, "newsubdir"))
		assert.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("RelativePath_ResolvesFromProjectCookie", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a directory under ProjectDir to resolve relative path against
		_ = os.MkdirAll(filepath.Join(env.ProjectDir, "relativedir"), 0o755)

		req := newRequest(t, http.MethodPost, "/api/dir/create", map[string]string{
			"path": "relativedir",
			"name": "newsubdir",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeDirCreate, req)
		assertOK(t, w)
		assertJSONField(t, w, "ok", true)

		info, err := os.Stat(filepath.Join(env.ProjectDir, "relativedir", "newsubdir"))
		assert.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("AbsolutePathOutsideRoot_Returns403", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/dir/create", map[string]string{
			"path": os.TempDir(),
			"name": "newsubdir",
		})

		w := callHandler(ServeDirCreate, req)
		assertStatus(t, w, http.StatusForbidden)
	})
}

func TestServeFileMove(t *testing.T) {
	t.Run("MoveFileToNewLocation_Succeeds", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "src.txt", "move me")
		_ = os.MkdirAll(filepath.Join(env.ProjectDir, "dest"), 0o755)

		req := newRequest(t, http.MethodPost, "/api/file/move", map[string]string{
			"path": "src.txt",
			"dest": "dest/src.txt",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileMove, req)
		assertOK(t, w)
		assertJSONField(t, w, "ok", true)

		_, err := os.Stat(filepath.Join(env.ProjectDir, "dest", "src.txt"))
		assert.NoError(t, err)
		_, err = os.Stat(filepath.Join(env.ProjectDir, "src.txt"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("MissingPathOrDest_Returns400", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Missing dest
		req := newRequest(t, http.MethodPost, "/api/file/move", map[string]string{
			"path": "src.txt",
		})
		withProjectCookie(req, env.ProjectDir)
		w := callHandler(ServeFileMove, req)
		assertStatus(t, w, http.StatusBadRequest)

		// Missing path
		req2 := newRequest(t, http.MethodPost, "/api/file/move", map[string]string{
			"dest": "dest.txt",
		})
		withProjectCookie(req2, env.ProjectDir)
		w2 := callHandler(ServeFileMove, req2)
		assertStatus(t, w2, http.StatusBadRequest)
	})

	t.Run("NoProjectCookie_Returns403", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/file/move", map[string]string{
			"path": "src.txt",
			"dest": "dest.txt",
		})

		w := callHandler(ServeFileMove, req)
		assertStatus(t, w, http.StatusForbidden)
	})

	t.Run("SameSourceAndDest_ReturnsOK_NoOp", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "same.txt", "don't lose me")

		req := newRequest(t, http.MethodPost, "/api/file/move", map[string]string{
			"path": "same.txt",
			"dest": "same.txt",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileMove, req)
		assertOK(t, w)
		assertJSONField(t, w, "ok", true)

		// File must still exist with original content
		data, err := os.ReadFile(filepath.Join(env.ProjectDir, "same.txt"))
		assert.NoError(t, err)
		assert.Equal(t, "don't lose me", string(data))
	})
}

func TestServeFileCopy(t *testing.T) {
	t.Run("CopyFile_Succeeds_ContentIdentical", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "original.txt", "copy this content")

		req := newRequest(t, http.MethodPost, "/api/file/copy", map[string]string{
			"path": "original.txt",
			"dest": "copy.txt",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileCopy, req)
		assertOK(t, w)
		assertJSONField(t, w, "ok", true)

		data, err := os.ReadFile(filepath.Join(env.ProjectDir, "copy.txt"))
		assert.NoError(t, err)
		assert.Equal(t, "copy this content", string(data))

		// Original should still exist
		origData, err := os.ReadFile(filepath.Join(env.ProjectDir, "original.txt"))
		assert.NoError(t, err)
		assert.Equal(t, "copy this content", string(origData))
	})

	t.Run("CopyDirectoryRecursive_Succeeds", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "srcdir/a.txt", "aaa")
		createTestFile(t, env.ProjectDir, "srcdir/sub/b.txt", "bbb")

		req := newRequest(t, http.MethodPost, "/api/file/copy", map[string]string{
			"path": "srcdir",
			"dest": "destdir",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileCopy, req)
		assertOK(t, w)

		data, err := os.ReadFile(filepath.Join(env.ProjectDir, "destdir", "a.txt"))
		assert.NoError(t, err)
		assert.Equal(t, "aaa", string(data))

		data2, err := os.ReadFile(filepath.Join(env.ProjectDir, "destdir", "sub", "b.txt"))
		assert.NoError(t, err)
		assert.Equal(t, "bbb", string(data2))
	})

	t.Run("SourceNotFound_Returns500", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/file/copy", map[string]string{
			"path": "nonexistent.txt",
			"dest": "copy.txt",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileCopy, req)
		assertStatus(t, w, http.StatusInternalServerError)
	})

	t.Run("DestAlreadyExists_Returns409", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "original.txt", "content")
		createTestFile(t, env.ProjectDir, "original (1).txt", "existing copy")

		req := newRequest(t, http.MethodPost, "/api/file/copy", map[string]string{
			"path": "original.txt",
			"dest": "original (1).txt",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileCopy, req)
		assertStatus(t, w, http.StatusConflict)

		// Original file should remain unchanged
		data, err := os.ReadFile(filepath.Join(env.ProjectDir, "original.txt"))
		assert.NoError(t, err)
		assert.Equal(t, "content", string(data))
	})

	t.Run("DestDirAlreadyExists_Returns409", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "mydir/a.txt", "aaa")
		createTestFile(t, env.ProjectDir, "mydir-copy/b.txt", "bbb")

		req := newRequest(t, http.MethodPost, "/api/file/copy", map[string]string{
			"path": "mydir",
			"dest": "mydir-copy",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileCopy, req)
		assertStatus(t, w, http.StatusConflict)
	})

	t.Run("SameSourceAndDest_ReturnsOK_NoOp", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "same.txt", "don't lose me")

		req := newRequest(t, http.MethodPost, "/api/file/copy", map[string]string{
			"path": "same.txt",
			"dest": "same.txt",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileCopy, req)
		assertOK(t, w)
		assertJSONField(t, w, "ok", true)

		// File must still exist with original content
		data, err := os.ReadFile(filepath.Join(env.ProjectDir, "same.txt"))
		assert.NoError(t, err)
		assert.Equal(t, "don't lose me", string(data))
	})

	t.Run("CopyToDifferentDir_NoConflict_Succeeds", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "src.txt", "content")
		_ = os.MkdirAll(filepath.Join(env.ProjectDir, "subdir"), 0o755)

		req := newRequest(t, http.MethodPost, "/api/file/copy", map[string]string{
			"path": "src.txt",
			"dest": "subdir/src.txt",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(ServeFileCopy, req)
		assertOK(t, w)

		data, err := os.ReadFile(filepath.Join(env.ProjectDir, "subdir", "src.txt"))
		assert.NoError(t, err)
		assert.Equal(t, "content", string(data))
	})
}

// splitLines splits a string by newline, matching the handler's behavior.
func splitLines(s string) []string { //nolint:unused // test utility kept for future use
	if s == "" {
		return nil
	}
	result := []string{}
	start := 0
	for i := range len(s) {
		if s[i] == '\n' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}
