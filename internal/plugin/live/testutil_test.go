package live

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/handlertest"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/require"
)

// 夹具本体在 internal/handlertest（各功能插件共用）。这里保留同名薄包装，
// 是为了让测试函数体不必改写——它们从 handler 包搬过来时就是这个形状。
type testEnv = handlertest.Env

func setupTestEnv(t *testing.T) (*testEnv, func()) {
	t.Helper()
	env, teardown := handlertest.Setup(t)
	// 会议纪要的表：接口的读写路径都依赖它们。
	for _, ddl := range []string{service.LiveNotesDDL, service.NotificationsDDL} {
		_, err := store.WriteExec(ddl)
		require.NoError(t, err)
	}
	return env, teardown
}

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

// saveFeatureSwitchState snapshots the feature-switch globals and restores them
// after the test (moved from handler/feature_switch_test.go).
func saveFeatureSwitchState(t *testing.T) {
	t.Helper()
	origCfg := model.ConfigInstance
	origTasks := model.TasksEnabled
	origShare := model.ShareEnabled
	origPolicy := model.FeaturePolicy
	origDataDir := model.DataDir
	origScheduler := service.GlobalScheduler

	t.Cleanup(func() {
		model.ConfigInstance = origCfg
		model.TasksEnabled = origTasks
		model.ShareEnabled = origShare
		model.FeaturePolicy = origPolicy
		model.DataDir = origDataDir
		service.GlobalScheduler = origScheduler
	})
}
