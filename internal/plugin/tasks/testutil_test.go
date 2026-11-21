package tasks

import (
	"encoding/json"
	"io"
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
	for _, ddl := range []string{handlertest.FullSchema, service.NotificationsDDL} {
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

func assertOK(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	handlertest.AssertStatus(t, w, http.StatusOK)
}

// helperCreateScheduledTaskForHandler 造一条任务 + 会话 + 执行记录。
// 与 handler 的同名辅助一致（那边还有跨功能用例需要它）。
func helperCreateScheduledTaskForHandler(t *testing.T, env *testEnv, s *service.Scheduler) (int64, int64) {
	t.Helper()
	// Create a task
	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "Test Task",
		CronExpr:    "0 8 * * *",
		AgentID:     "claude",
		Prompt:      "Review code",
	}
	err := s.AddTask(task)
	require.NoError(t, err)

	// Create a scheduled session
	sessID, err := service.CreateSession(env.ProjectDir, "claude", "Test Task", "claude", "claude-sonnet-4-6", "default", "scheduled")
	require.NoError(t, err)
	// Thinking effort is no longer persisted to DB; it comes from ACP runtime at session init.

	// Add messages
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessID, "user", "Review this code", nil, false, "")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessID, "assistant", "Code looks good", nil, false, "")
	require.NoError(t, err)

	// Create an execution
	execID, err := service.AddTaskExecution(task.ID, sessID, "auto")
	require.NoError(t, err)

	// Mark as completed (UpdateExecutionStatus takes sessionID, not execID)
	err = service.UpdateExecutionStatus(sessID, "completed")
	require.NoError(t, err)

	return task.ID, execID
}

func decodeRespJSON(t *testing.T, body io.Reader, target any) {
	t.Helper()
	if err := json.NewDecoder(body).Decode(target); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
}
