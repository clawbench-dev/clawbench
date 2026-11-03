package handler

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createMultipartUploadRequestFields builds a multipart upload with arbitrary
// extra form fields (dir / relpath / onconflict), mirroring what the browser
// sends.
func createMultipartUploadRequestFields(t *testing.T, filename, content string, fields map[string]string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, _ = part.Write([]byte(content))
	for k, v := range fields {
		_ = writer.WriteField(k, v)
	}
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload/file", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func TestUploadFile_OverwriteConflict(t *testing.T) {
	t.Run("OnConflictOverwrite_ReplacesExistingFile", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "target/dup.txt", "first")

		req := createMultipartUploadRequestFields(t, "dup.txt", "second", map[string]string{
			"dir":        "target",
			"onconflict": "overwrite",
		})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(UploadFile, req)
		assertOK(t, w)

		var result map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		// The overwrite lands on the original name — no numbered sibling.
		assert.Equal(t, "target/dup.txt", result["path"])

		data, err := os.ReadFile(filepath.Join(env.ProjectDir, "target", "dup.txt"))
		require.NoError(t, err)
		assert.Equal(t, "second", string(data))

		_, err = os.Stat(filepath.Join(env.ProjectDir, "target", "dup_1.txt"))
		assert.True(t, os.IsNotExist(err), "overwrite must not leave a renamed copy")
	})

	t.Run("Default_StillRenames", func(t *testing.T) {
		// Regression guard: the overwrite switch must stay opt-in, otherwise a
		// dropped file silently destroys its namesake.
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "target/dup.txt", "first")

		req := createMultipartUploadRequestFields(t, "dup.txt", "second", map[string]string{"dir": "target"})
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(UploadFile, req)
		assertOK(t, w)

		var result map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		assert.Equal(t, "target/dup_1.txt", result["path"])

		data, err := os.ReadFile(filepath.Join(env.ProjectDir, "target", "dup.txt"))
		require.NoError(t, err)
		assert.Equal(t, "first", string(data))
	})
}

func TestUploadCheck(t *testing.T) {
	type checkFile struct {
		Name    string `json:"name"`
		RelPath string `json:"relpath"`
	}

	check := func(t *testing.T, env *testEnv, dir string, files []checkFile) *httptest.ResponseRecorder {
		t.Helper()
		req := newRequest(t, http.MethodPost, "/api/upload/check", map[string]interface{}{
			"dir":   dir,
			"files": files,
		})
		withProjectCookie(req, env.ProjectDir)
		return callHandler(UploadCheck, req)
	}

	decodeConflicts := func(t *testing.T, w *httptest.ResponseRecorder) []map[string]string {
		t.Helper()
		var result struct {
			OK        bool                `json:"ok"`
			Conflicts []map[string]string `json:"conflicts"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		assert.True(t, result.OK)
		return result.Conflicts
	}

	t.Run("ReportsOnlyExistingNames", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "target/a.txt", "x")

		w := check(t, env, "target", []checkFile{
			{Name: "a.txt"},
			{Name: "b.txt"},
		})
		assertOK(t, w)

		conflicts := decodeConflicts(t, w)
		require.Len(t, conflicts, 1)
		assert.Equal(t, "a.txt", conflicts[0]["name"])
		assert.Equal(t, "a.txt", conflicts[0]["path"])
	})

	t.Run("AppliesTheUploadNameNormalization", func(t *testing.T) {
		// "my file.txt" is written as "my_file.txt" by the uploader, so that is
		// the name the preflight must compare against.
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "target/my_file.txt", "x")

		w := check(t, env, "target", []checkFile{{Name: "my file.txt"}})
		assertOK(t, w)

		conflicts := decodeConflicts(t, w)
		require.Len(t, conflicts, 1)
		assert.Equal(t, "my_file.txt", conflicts[0]["path"])
	})

	t.Run("ResolvesRelativeSubPaths", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "target/src/deep/a.ts", "x")

		w := check(t, env, "target", []checkFile{{Name: "a.ts", RelPath: "src/deep"}})
		assertOK(t, w)

		conflicts := decodeConflicts(t, w)
		require.Len(t, conflicts, 1)
		assert.Equal(t, "src/deep/a.ts", conflicts[0]["path"])
	})

	t.Run("DoesNotCreateMissingDirectories", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		require.NoError(t, os.MkdirAll(filepath.Join(env.ProjectDir, "target"), 0o755))

		w := check(t, env, "target", []checkFile{{Name: "a.ts", RelPath: "not-there"}})
		assertOK(t, w)
		assert.Empty(t, decodeConflicts(t, w))

		_, err := os.Stat(filepath.Join(env.ProjectDir, "target", "not-there"))
		assert.True(t, os.IsNotExist(err), "a preflight question must not touch the tree")
	})

	t.Run("RejectsTraversal", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		require.NoError(t, os.MkdirAll(filepath.Join(env.ProjectDir, "target"), 0o755))

		w := check(t, env, "target", []checkFile{{Name: "a.txt", RelPath: "../escape"}})
		assertStatus(t, w, http.StatusForbidden)
	})

	t.Run("RequiresDirectory", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		w := check(t, env, "", []checkFile{{Name: "a.txt"}})
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("RejectsOtherMethods", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := httptest.NewRequest(http.MethodGet, "/api/upload/check", http.NoBody)
		withProjectCookie(req, env.ProjectDir)
		w := callHandler(UploadCheck, req)
		assertStatus(t, w, http.StatusMethodNotAllowed)
	})
}
