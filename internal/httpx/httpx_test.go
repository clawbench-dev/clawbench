package httpx

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusCreated, map[string]string{"ok": "1"})

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"ok":"1"}`, rec.Body.String())
}

func TestDecodeJSON(t *testing.T) {
	var v struct{ A int }

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"a":1}`))
	assert.True(t, DecodeJSON(rec, r, &v))
	assert.Equal(t, 1, v.A)

	rec2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`not json`))
	assert.False(t, DecodeJSON(rec2, r2, &v))
	assert.Equal(t, http.StatusBadRequest, rec2.Code)
}

func TestDecodeOptionalJSON(t *testing.T) {
	var v struct{ A int }

	// Empty body is "no fields supplied", not an error.
	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(""))
	require.NoError(t, DecodeOptionalJSON(r, &v))

	// Malformed body is still an error.
	r2 := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("bad"))
	assert.Error(t, DecodeOptionalJSON(r2, &v))
}

func TestRequireMethod(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)

	rec := httptest.NewRecorder()
	assert.True(t, RequireMethod(rec, r, http.MethodGet, http.MethodPost))

	rec2 := httptest.NewRecorder()
	assert.False(t, RequireMethod(rec2, r, http.MethodPost))
	assert.Equal(t, http.StatusMethodNotAllowed, rec2.Code)
}

func TestWriteLocalizedErrorf(t *testing.T) {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)

	WriteLocalizedErrorf(rec, r, http.StatusForbidden, "AccessDenied")

	assert.Equal(t, http.StatusForbidden, rec.Code)
	// The message key is echoed in the body so clients can translate it.
	assert.Contains(t, rec.Body.String(), "AccessDenied")
}

func TestWriteLocalizedError(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)

	// nil error → 500, never a panic.
	rec := httptest.NewRecorder()
	WriteLocalizedError(rec, r, nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	// AppError → its own status and message key.
	rec2 := httptest.NewRecorder()
	WriteLocalizedError(rec2, r, model.Forbidden(nil, "AccessDenied"))
	assert.Equal(t, http.StatusForbidden, rec2.Code)
	assert.Contains(t, rec2.Body.String(), "AccessDenied")

	// A plain error → 500.
	rec3 := httptest.NewRecorder()
	WriteLocalizedError(rec3, r, assert.AnError)
	assert.Equal(t, http.StatusInternalServerError, rec3.Code)
}

func TestRequireProject(t *testing.T) {
	// No cookie → 403 and false.
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	got, ok := RequireProject(rec, r)
	assert.False(t, ok)
	assert.Empty(t, got)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// With the project cookie → the path comes back.
	rec2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/x", nil)
	r2.AddCookie(&http.Cookie{Name: model.ScopedCookieName("clawbench_project"), Value: "/tmp/p"})
	got, ok = RequireProject(rec2, r2)
	assert.True(t, ok)
	assert.Equal(t, "/tmp/p", got)
}

func TestValidateAndResolvePath(t *testing.T) {
	dir := t.TempDir()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)

	rec := httptest.NewRecorder()
	got, ok := ValidateAndResolvePath(rec, r, dir, "sub/file")
	require.True(t, ok)
	assert.Equal(t, filepath.Join(dir, "sub", "file"), got)

	// Traversal must be refused.
	rec2 := httptest.NewRecorder()
	_, ok = ValidateAndResolvePath(rec2, r, dir, "../../etc/passwd")
	assert.False(t, ok)
	assert.Equal(t, http.StatusForbidden, rec2.Code)
}

func TestIsPathUnderBase(t *testing.T) {
	dir := t.TempDir()

	assert.True(t, IsPathUnderBase(filepath.Join(dir, "a"), dir))
	assert.False(t, IsPathUnderBase("/etc/passwd", dir))
}

func TestIsPathUnderAnyRoot(t *testing.T) {
	// RootPaths is package-global; set it and restore so the test does not leak.
	orig := model.RootPaths
	t.Cleanup(func() { model.RootPaths = orig })

	dir := t.TempDir()
	model.RootPaths = []string{dir}

	assert.True(t, IsPathUnderAnyRoot(filepath.Join(dir, "sub")))
	assert.False(t, IsPathUnderAnyRoot("/etc/passwd"))
}

func TestResolveAbsPath(t *testing.T) {
	orig := model.RootPaths
	t.Cleanup(func() { model.RootPaths = orig })

	dir := t.TempDir()
	model.RootPaths = []string{dir}

	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.AddCookie(&http.Cookie{Name: model.ScopedCookieName("clawbench_project"), Value: dir})

	// Absolute path under a root → accepted.
	rec := httptest.NewRecorder()
	got, ok := ResolveAbsPath(rec, r, filepath.Join(dir, "a.txt"))
	require.True(t, ok)
	assert.Equal(t, filepath.Join(dir, "a.txt"), got)

	// Absolute path outside every root → 403.
	rec2 := httptest.NewRecorder()
	_, ok = ResolveAbsPath(rec2, r, "/etc/passwd")
	assert.False(t, ok)
	assert.Equal(t, http.StatusForbidden, rec2.Code)

	// Relative path resolves against the project cookie.
	rec3 := httptest.NewRecorder()
	got, ok = ResolveAbsPath(rec3, r, "a.txt")
	require.True(t, ok)
	assert.Equal(t, filepath.Join(dir, "a.txt"), got)
}

func TestT_And_Loc(t *testing.T) {
	// Without the locale middleware GetLocalizer falls back, so this must not panic.
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	assert.NotNil(t, Loc(r))
	// An unknown key falls back to the key itself (go-i18n behaviour).
	assert.NotEmpty(t, T(r, "AccessDenied"))
}
