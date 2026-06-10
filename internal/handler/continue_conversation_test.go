package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ========== Continue Conversation Handler Tests ==========

// helperCreateScheduledTaskForHandler creates a task + session + execution + messages
// and returns the task and execution ID for handler testing.
func helperCreateScheduledTaskForHandler(t *testing.T, env *testEnv, s *service.Scheduler) (int64, int64) {
	t.Helper()
	// Create a scheduled task
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

func TestContinueConversation_GET_NotContinued(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	taskID, execID := helperCreateScheduledTaskForHandler(t, env, s)

	req := newRequest(t, http.MethodGet, fmt.Sprintf("/api/tasks/%d/executions/%d/continue", taskID, execID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertOK(t, w)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.False(t, resp["exists"].(bool))
}

func TestContinueConversation_GET_AlreadyContinued(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	taskID, execID := helperCreateScheduledTaskForHandler(t, env, s)

	// Continue first via service
	_, _, err := service.ContinueFromExecution(execID, env.ProjectDir)
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, fmt.Sprintf("/api/tasks/%d/executions/%d/continue", taskID, execID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertOK(t, w)

	var resp map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.True(t, resp["exists"].(bool))
	assert.NotEmpty(t, resp["sessionId"])
}

func TestContinueConversation_POST_NormalFlow(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	taskID, execID := helperCreateScheduledTaskForHandler(t, env, s)

	req := newRequest(t, http.MethodPost, fmt.Sprintf("/api/tasks/%d/executions/%d/continue", taskID, execID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertOK(t, w)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.True(t, resp["ok"].(bool))
	assert.NotEmpty(t, resp["sessionId"])
	assert.False(t, resp["alreadyExists"].(bool))
}

func TestContinueConversation_POST_AlreadyExists(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	taskID, execID := helperCreateScheduledTaskForHandler(t, env, s)

	// Continue first
	_, _, err := service.ContinueFromExecution(execID, env.ProjectDir)
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, fmt.Sprintf("/api/tasks/%d/executions/%d/continue", taskID, execID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertOK(t, w)

	var resp map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.True(t, resp["ok"].(bool))
	assert.True(t, resp["alreadyExists"].(bool))
}

func TestContinueConversation_POST_RunningExecution(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	// Create task + execution but keep it running
	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "Running Task",
		CronExpr:    "0 8 * * *",
		AgentID:     "claude",
		Prompt:      "Review code",
	}
	err := s.AddTask(task)
	require.NoError(t, err)

	sessID, err := service.CreateSession(env.ProjectDir, "claude", "Running Task", "claude", "", "default", "scheduled")
	require.NoError(t, err)

	execID, err := service.AddTaskExecution(task.ID, sessID, "auto")
	require.NoError(t, err)
	// Don't mark as completed — stays "running"

	req := newRequest(t, http.MethodPost, fmt.Sprintf("/api/tasks/%d/executions/%d/continue", task.ID, execID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestContinueConversation_POST_ProjectMismatch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	taskID, execID := helperCreateScheduledTaskForHandler(t, env, s)

	// Use a different project cookie
	req := newRequest(t, http.MethodPost, fmt.Sprintf("/api/tasks/%d/executions/%d/continue", taskID, execID), nil)
	req = withProjectCookie(req, t.TempDir()) // wrong project
	w := callHandler(ServeTaskByID, req)

	// Should get 403 because the task's project doesn't match
	assertStatus(t, w, http.StatusForbidden)
}

func TestContinueConversation_GET_ExecutionNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	req := newRequest(t, http.MethodGet, "/api/tasks/1/executions/99999/continue", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertStatus(t, w, http.StatusNotFound)
}

func TestContinueConversation_InvalidExecID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/tasks/1/executions/abc/continue", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertStatus(t, w, http.StatusBadRequest)
}
