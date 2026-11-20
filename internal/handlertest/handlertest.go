// Package handlertest 是功能插件测试的公共夹具。
//
// 功能搬进 internal/plugin/<id> 之后，它们的测试不能再引用 handler 包里
// testutil_test.go 的符号——测试文件不导出，跨包不可见。这里按插件测试的
// 实际需要重建一份最小版本：临时项目目录、内存 SQLite（projects 表）、
// 请求构造与断言。
//
// 与 handler 的 setupTestEnv 的区别：那个还准备了 npm 探测、chat 表等与
// 插件无关的夹具。插件测试需要额外表时自己执行对应的 DDL（见各插件包的
// setupXxxEnv）。
//
// 本包只被 _test.go 引用，本身不含可测逻辑，因此列入覆盖率豁免。
package handlertest

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// Env is the state a test needs after setup.
type Env struct {
	ProjectDir string
	WatchDir   string
}

// Setup creates a temp project directory and an in-memory DB with the projects
// table, and restores every global it touches on teardown.
func Setup(t *testing.T) (*Env, func()) {
	t.Helper()

	watchDir := CanonPath(t.TempDir())
	projectDir := filepath.Join(watchDir, "project")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))

	origRootPaths := model.RootPaths
	origToken := model.SessionToken
	origCookieToken := model.CookieToken

	model.SessionToken = ""
	model.CookieToken = ""
	model.RootPaths = []string{watchDir}

	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, _ = db.Exec("PRAGMA busy_timeout=5000")
	_, err = db.Exec(store.ProjectsDDL)
	require.NoError(t, err)
	restoreDB := store.SetDBForTest(db, db)

	teardown := func() {
		restoreDB()
		_ = db.Close()
		model.RootPaths = origRootPaths
		model.SessionToken = origToken
		model.CookieToken = origCookieToken
	}
	return &Env{ProjectDir: projectDir, WatchDir: watchDir}, teardown
}

// CanonPath resolves symlinks so paths match what production compares
// (macOS /var → /private/var).
func CanonPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	abs = filepath.Clean(abs)
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs
	}
	return resolved
}

// NewRequest builds a request with an optional JSON body.
func NewRequest(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

// Call runs a handler and returns the recorder.
func Call(handler http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

// AssertStatus checks the status code and includes the body on failure.
func AssertStatus(t *testing.T, w *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if w.Code != expected {
		t.Errorf("expected status %d, got %d; body: %s", expected, w.Code, w.Body.String())
	}
}

// WithProjectCookie adds the clawbench_project cookie.
func WithProjectCookie(req *http.Request, projectPath string) *http.Request {
	req.AddCookie(&http.Cookie{
		Name:  model.ScopedCookieName("clawbench_project"),
		Value: url.QueryEscape(projectPath),
	})
	return req
}

// CreateFile writes a file under projectDir, creating parent directories.
func CreateFile(t *testing.T, projectDir, relPath, content string) {
	t.Helper()
	fullPath := filepath.Join(projectDir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
	require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
}
