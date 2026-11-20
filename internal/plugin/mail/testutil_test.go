package mail

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/handlertest"
)

// 夹具本体在 internal/handlertest（各功能插件共用）。这里保留同名薄包装，
// 是为了让测试函数体不必改写——它们从 handler 包搬过来时就是这个形状。
type testEnv = handlertest.Env

func setupTestEnv(t *testing.T) (*testEnv, func()) { return handlertest.Setup(t) }

func newRequest(t *testing.T, method, path string, body any) *http.Request {
	return handlertest.NewRequest(t, method, path, body)
}

func callHandler(handler http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	return handlertest.Call(handler, req)
}

func assertStatus(t *testing.T, w *httptest.ResponseRecorder, expected int) {
	handlertest.AssertStatus(t, w, expected)
}

func withProjectCookie(req *http.Request, projectPath string) *http.Request {
	return handlertest.WithProjectCookie(req, projectPath)
}

func createTestFile(t *testing.T, projectDir, relPath, content string) {
	handlertest.CreateFile(t, projectDir, relPath, content)
}
