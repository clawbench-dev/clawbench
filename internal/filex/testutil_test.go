package filex

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/handlertest"
)

// 缩略图测试从 handler 搬过来时带着那套夹具的名字，这里用公共夹具补上。
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

func assertOK(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	handlertest.AssertStatus(t, w, http.StatusOK)
}

func createTestFile(t *testing.T, projectDir, relPath, content string) {
	handlertest.CreateFile(t, projectDir, relPath, content)
}

func withProjectCookie(req *http.Request, projectPath string) *http.Request {
	return handlertest.WithProjectCookie(req, projectPath)
}
