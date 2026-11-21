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

func newRequest(t *testing.T, path string) *http.Request {
	return handlertest.NewRequest(t, http.MethodGet, path, nil)
}

func callHandler(handler http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	return handlertest.Call(handler, req)
}

func createTestFile(t *testing.T, projectDir, relPath, content string) {
	handlertest.CreateFile(t, projectDir, relPath, content)
}

func withProjectCookie(req *http.Request, projectPath string) {
	handlertest.WithProjectCookie(req, projectPath)
}
