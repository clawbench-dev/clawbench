package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
)

func TestServeProjectSet(t *testing.T) {
	t.Run("GET_ReturnsDefaultProject", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		projectPath := filepath.Join(env.WatchDir, "myproject")
		_ = os.MkdirAll(projectPath, 0o755)

		// Set project as default in DB (GET now reads from DB, not cookie)
		_, err := service.DB.Exec("INSERT INTO recent_projects (project_path, is_default) VALUES (?, 1)", projectPath)
		assert.NoError(t, err)

		req := newRequest(t, http.MethodGet, "/api/project", nil)

		w := callHandler(ServeProjectSet, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assertJSONField(t, w, "path", projectPath)
		// homeDir should be present (non-empty on any system with a home directory)
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		assert.NotEmpty(t, resp["homeDir"], "homeDir should be present in response")
	})

	t.Run("GET_NoDefault_FallsBackToHomeDir", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/project", nil)
		// No default in DB, no recent projects → should fallback to home directory

		w := callHandler(ServeProjectSet, req)

		assert.Equal(t, http.StatusOK, w.Code)
		// When no default and no recents, should fallback to home directory
		// (not RootPaths[0] which is "/" on Linux)
		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.NotEmpty(t, resp["path"], "path should not be empty")
		assert.NotEqual(t, "/", resp["path"], "path should not be root /")
	})

	t.Run("GET_NoDefault_FallsBackToRecentProject", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		recentPath := filepath.Join(env.WatchDir, "recentproject")
		_ = os.MkdirAll(recentPath, 0o755)

		// Insert a recent project directly into the DB
		_, err := service.DB.Exec(
			"INSERT INTO recent_projects (project_path) VALUES (?)", recentPath,
		)
		assert.NoError(t, err)

		req := newRequest(t, http.MethodGet, "/api/project", nil)

		w := callHandler(ServeProjectSet, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assertJSONField(t, w, "path", recentPath)
	})

	t.Run("POST_ValidPath_SetsCookieAndClearsSession", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		projectPath := filepath.Join(env.WatchDir, "myproject")
		_ = os.MkdirAll(projectPath, 0o755)

		req := newRequest(t, http.MethodPost, "/api/project", map[string]string{
			"path": projectPath,
		})
		withSessionCookie(req, "old-session-id")

		w := callHandler(ServeProjectSet, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assertJSONField(t, w, "ok", "true")

		// Verify project cookie is set
		var projectCookieFound, sessionCleared bool
		for _, c := range w.Result().Cookies() {
			if c.Name == model.ScopedCookieName("clawbench_project") {
				projectCookieFound = true
				decoded, _ := url.QueryUnescape(c.Value)
				assert.Equal(t, projectPath, decoded)
			}
			if c.Name == model.ScopedCookieName("chat_session_id") {
				sessionCleared = true
				assert.Equal(t, -1, c.MaxAge, "session cookie should be cleared (MaxAge=-1)")
			}
		}
		assert.True(t, projectCookieFound, "expected project cookie to be set")
		assert.True(t, sessionCleared, "expected chat session cookie to be cleared")
	})

	t.Run("POST_ValidPath_SetsDefaultInDB", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		projectPath := filepath.Join(env.WatchDir, "myproject2")
		_ = os.MkdirAll(projectPath, 0o755)

		req := newRequest(t, http.MethodPost, "/api/project", map[string]string{
			"path": projectPath,
		})

		w := callHandler(ServeProjectSet, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assertJSONField(t, w, "ok", "true")

		// Verify is_default=1 in DB
		var isDefault int
		err := service.DB.QueryRow("SELECT is_default FROM recent_projects WHERE project_path = ?", projectPath).Scan(&isDefault)
		assert.NoError(t, err, "project should exist in recent_projects")
		assert.Equal(t, 1, isDefault, "posted project should be marked as default")
	})

	t.Run("POST_PathOutsideWatchDir_Returns403", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		// Use a path with .. that resolves outside WatchDir
		// The handler resolves this to an absolute path and checks it's under WatchDir
		req := newRequest(t, http.MethodPost, "/api/project", map[string]string{
			"path": "../../../etc",
		})

		w := callHandler(ServeProjectSet, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("POST_AbsolutePathOutsideRoot_Returns403", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		// Use an absolute path that is outside root paths
		req := newRequest(t, http.MethodPost, "/api/project", map[string]string{
			"path": os.TempDir(),
		})

		w := callHandler(ServeProjectSet, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("POST_NonExistentDirectory_Returns400", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/project", map[string]string{
			"path": filepath.Join(env.WatchDir, "nonexistent"),
		})

		w := callHandler(ServeProjectSet, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("POST_InvalidBody_Returns400", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := httptest.NewRequest(http.MethodPost, "/api/project", http.NoBody)
		w := callHandler(ServeProjectSet, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("OtherMethod_Returns405", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodDelete, "/api/project", nil)
		w := callHandler(ServeProjectSet, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("POST_RelativePath_ResolvesFromRootPaths", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a directory under WatchDir with a relative name
		subDir := filepath.Join(env.WatchDir, "myproject")
		_ = os.MkdirAll(subDir, 0o755)

		req := newRequest(t, http.MethodPost, "/api/project", map[string]string{
			"path": "myproject",
		})

		w := callHandler(ServeProjectSet, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assertJSONField(t, w, "ok", "true")
		// Verify the path was resolved from RootPaths[0]
		assertJSONField(t, w, "path", subDir)
	})

	t.Run("POST_EmptyPath_SetsFirstRootPath", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/project", map[string]string{
			"path": "",
		})

		w := callHandler(ServeProjectSet, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assertJSONField(t, w, "ok", "true")
		assertJSONField(t, w, "path", env.WatchDir)
	})

	t.Run("POST_RootSlashPath_SetsFirstRootPath", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/project", map[string]string{
			"path": "/",
		})

		w := callHandler(ServeProjectSet, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assertJSONField(t, w, "ok", "true")
		assertJSONField(t, w, "path", env.WatchDir)
	})
}

func TestServeRecentProjects(t *testing.T) {
	t.Run("GET_ReturnsEmptyList", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/recent-projects", nil)
		w := callHandler(ServeRecentProjects, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var result []string
		decodeRespJSON(t, w.Body, &result)
		assert.Empty(t, result)
	})

	t.Run("GET_WithExistingProjects_ReturnsList", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		projectPath := filepath.Join(env.WatchDir, "proj1")
		_ = os.MkdirAll(projectPath, 0o755)
		_, err := service.DB.Exec(
			"INSERT INTO recent_projects (project_path) VALUES (?)", projectPath,
		)
		assert.NoError(t, err)

		req := newRequest(t, http.MethodGet, "/api/recent-projects", nil)
		w := callHandler(ServeRecentProjects, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var result []string
		decodeRespJSON(t, w.Body, &result)
		assert.Contains(t, result, projectPath)
	})

	t.Run("POST_AddProject_ReturnsOK", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPost, "/api/recent-projects", map[string]string{
			"path": "/some/project",
		})
		w := callHandler(ServeRecentProjects, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assertJSONField(t, w, "ok", true)
	})

	t.Run("POST_InvalidBody_Returns400", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := httptest.NewRequest(http.MethodPost, "/api/recent-projects", http.NoBody)
		w := callHandler(ServeRecentProjects, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("OtherMethod_Returns405", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPut, "/api/recent-projects", nil)
		w := callHandler(ServeRecentProjects, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}
