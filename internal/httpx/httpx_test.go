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

// templateData 会原样落进响应体的 detail 字段（前端靠它指出是哪个参数被拒）。
func TestWriteLocalizedErrorf_CarriesTemplateData(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteLocalizedErrorf(rec, httptest.NewRequest(http.MethodGet, "/x", nil),
		http.StatusBadRequest, "InvalidRequest", map[string]any{"reason": "start"})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"detail":{"reason":"start"}`)
}

// 没有 Body 的请求（例如 httptest 构造的 GET）在 DecodeOptionalJSON 里是
// "没有可选字段"，不是错误。
func TestDecodeOptionalJSON_NilBody(t *testing.T) {
	var v struct{ A int }
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Body = nil
	assert.NoError(t, DecodeOptionalJSON(r, &v))
}

// 绝对路径不在任何允许的根下 → 403；相对路径没有项目 cookie → 403。
func TestResolveAbsPath_Rejections(t *testing.T) {
	origRoots := model.RootPaths
	t.Cleanup(func() { model.RootPaths = origRoots })
	model.RootPaths = []string{t.TempDir()}

	rec := httptest.NewRecorder()
	_, ok := ResolveAbsPath(rec, httptest.NewRequest(http.MethodGet, "/x", nil), "/etc/passwd")
	assert.False(t, ok)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	rec = httptest.NewRecorder()
	_, ok = ResolveAbsPath(rec, httptest.NewRequest(http.MethodGet, "/x", nil), "relative.txt")
	assert.False(t, ok)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// IsPathUnderBase：基目录不存在、目标不存在且父目录也解析不出来，都必须
// 返回 false（宁可拒绝，不要误判为"在根下"）。
func TestIsPathUnderBase_MissingPaths(t *testing.T) {
	dir := t.TempDir()
	assert.False(t, IsPathUnderBase(filepath.Join(dir, "x"), filepath.Join(dir, "no-such-base")))
	// 目标不存在时按"最近的已存在祖先"判断：祖先在根外 → false。
	assert.False(t, IsPathUnderBase(filepath.Join(t.TempDir(), "no-such-dir", "f.txt"), dir))
}
