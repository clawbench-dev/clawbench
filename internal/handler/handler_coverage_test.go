package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- writeLocalizedError: nil error ---

func TestWriteLocalizedError_NilError_Returns500(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	writeLocalizedError(w, r, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// --- writeLocalizedError: non-AppError ---

func TestWriteLocalizedError_NonAppError_Returns500(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	writeLocalizedError(w, r, errors.New("some plain error"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// --- writeLocalizedError: AppError ---

func TestWriteLocalizedError_AppError_ReturnsCustomCode(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestWriteLocalizedError_AppError_UnauthorizedCode(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	writeLocalizedError(w, r, model.Unauthorized(nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// --- writeLocalizedErrorf: with template data ---

func TestWriteLocalizedErrorf_WithTemplateData(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	writeLocalizedErrorf(w, r, http.StatusBadRequest, "MethodNotAllowed", map[string]any{"method": "DELETE"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
}

// --- isPathUnderBase: basic cases ---

func TestIsPathUnderBase_PathUnderBase(t *testing.T) {
	base := t.TempDir()
	sub := filepath.Join(base, "subdir")
	_ = os.MkdirAll(sub, 0o755)
	assert.True(t, isPathUnderBase(sub, base))
}

func TestIsPathUnderBase_ExactBasePath(t *testing.T) {
	base := t.TempDir()
	assert.True(t, isPathUnderBase(base, base))
}

func TestIsPathUnderBase_PathOutsideBase(t *testing.T) {
	base := t.TempDir()
	assert.False(t, isPathUnderBase("/etc/passwd", base))
}

func TestIsPathUnderBase_NonExistentBase(t *testing.T) {
	assert.False(t, isPathUnderBase("/some/path", "/nonexistent/base"))
}

func TestIsPathUnderBase_NonExistentTarget(t *testing.T) {
	base := t.TempDir()
	// Target doesn't exist but parent does — should resolve parent
	target := filepath.Join(base, "nonexistent-file")
	result := isPathUnderBase(target, base)
	assert.True(t, result)
}

func TestIsPathUnderBase_NonExistentTargetOutsideBase(t *testing.T) {
	base := t.TempDir()
	// Target doesn't exist and is outside base
	target := filepath.Join("/tmp", "some-deep-nested-nonexistent", "file")
	result := isPathUnderBase(target, base)
	assert.False(t, result)
}

// --- resolveAbsPath: absolute path errors ---

func TestResolveAbsPath_AbsolutePathUnderRoot_Success(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.WatchDir, "testfile.txt", "data")

	req := newRequest(t, http.MethodPost, "/api/test", nil)
	w := httptest.NewRecorder()
	absPath := filepath.Join(env.WatchDir, "testfile.txt")
	result, ok := resolveAbsPath(w, req, absPath)
	assert.True(t, ok)
	assert.Equal(t, absPath, result)
}

func TestResolveAbsPath_AbsolutePathOutsideRoot_Forbidden(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/test", nil)
	w := httptest.NewRecorder()
	result, ok := resolveAbsPath(w, req, "/etc/passwd")
	assert.False(t, ok)
	assert.Empty(t, result)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestResolveAbsPath_RelativePathWithProject_Success(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "relfile.txt", "data")

	req := newRequest(t, http.MethodPost, "/api/test", nil)
	withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	result, ok := resolveAbsPath(w, req, "relfile.txt")
	assert.True(t, ok)
	assert.Contains(t, result, "relfile.txt")
}

func TestResolveAbsPath_RelativePathWithoutProject_Fails(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/test", nil)
	w := httptest.NewRecorder()
	result, ok := resolveAbsPath(w, req, "somefile.txt")
	assert.False(t, ok)
	assert.Empty(t, result)
}

// --- requireProject: with invalid cookie ---

func TestRequireProject_EmptyCookieValue_Fails(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	r.AddCookie(&http.Cookie{Name: model.ScopedCookieName("clawbench_project"), Value: ""})
	projectPath, ok := requireProject(w, r)
	assert.False(t, ok)
	assert.Empty(t, projectPath)
}

// --- writeJSON: various status codes ---

func TestWriteJSON_BadRequest(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
}

// --- validateAndResolvePath: with request context ---

func TestValidateAndResolvePath_ValidWithRequest(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	absPath, ok := validateAndResolvePath(w, r, env.ProjectDir, "test.txt")
	assert.True(t, ok)
	require.True(t, filepath.IsAbs(absPath))
}
