package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ========== serveTaskExecutions — cursor normalization ==========

func TestServeTaskByID_Executions_ISO8601Cursor(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.Agents = map[string]*model.Agent{
		"coder": {ID: "coder", Name: "Coder", Backend: "claude"},
	}
	defer func() { model.Agents = nil }()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "CursorNorm",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	// Create 5 completed executions with staggered timestamps
	for i := range 5 {
		sessionID, _ := service.CreateSession(env.ProjectDir, "claude", fmt.Sprintf("Exec %d", i), "coder", "", "default", "scheduled")
		_, _ = service.AddTaskExecution(task.ID, sessionID, "auto")
		_ = service.UpdateExecutionStatus(sessionID, "completed")
	}

	// Page 1: get 2 items
	req1 := newRequest(t, http.MethodGet, fmt.Sprintf("/api/tasks/%d/executions?limit=2", task.ID), nil)
	req1 = withProjectCookie(req1, env.ProjectDir)
	w1 := callHandler(ServeTaskByID, req1)
	assertOK(t, w1)

	var result1 map[string]any
	_ = json.Unmarshal(w1.Body.Bytes(), &result1)
	executions1, _ := result1["executions"].([]interface{})
	require.Len(t, executions1, 2)
	assert.Equal(t, true, result1["hasMore"])

	// Extract cursor values and simulate ISO 8601 format from frontend
	lastExec, _ := executions1[1].(map[string]interface{})
	cursorRaw, _ := lastExec["createdAt"].(string)
	cursorID := fmt.Sprintf("%v", lastExec["id"])

	// Test cursor normalization: the handler converts ISO 8601 format
	// (T separator and Z suffix) to SQLite's "YYYY-MM-DD HH:MM:SS" format.
	// The createdAt from the driver already uses T+Z format (e.g. "2026-05-31T14:28:09Z"),
	// so appending another Z simulates a frontend-sent cursor like "2026-05-31T14:28:09ZZ"
	// which should still normalize correctly.
	// A better test: use the raw cursor as-is since it's already in T+Z format,
	// and verify the handler normalizes it to the SQLite format.
	isoCursor := cursorRaw // Already in "2026-05-31T14:28:09Z" format from the driver

	// Page 2: use ISO 8601 cursor — handler should normalize T→space and strip Z
	req2 := newRequest(t, http.MethodGet,
		fmt.Sprintf("/api/tasks/%d/executions?limit=2&cursor=%s&cursor_id=%s", task.ID, isoCursor, cursorID), nil)
	req2 = withProjectCookie(req2, env.ProjectDir)
	w2 := callHandler(ServeTaskByID, req2)
	assertOK(t, w2)

	var result2 map[string]any
	_ = json.Unmarshal(w2.Body.Bytes(), &result2)
	executions2, _ := result2["executions"].([]interface{})
	// Should return results after the cursor point
	assert.NotEmpty(t, executions2, "should return executions after cursor")
	// Verify none of the returned executions match the cursor item
	for _, e := range executions2 {
		exec, _ := e.(map[string]interface{})
		assert.NotEqual(t, cursorID, fmt.Sprintf("%v", exec["id"]),
			"should not return the cursor item itself")
	}
}

// ========== serveTaskExecutions — unread calculation ==========

func TestServeTaskByID_Executions_UnreadWithoutLastRead(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.Agents = map[string]*model.Agent{
		"coder": {ID: "coder", Name: "Coder", Backend: "claude"},
	}
	defer func() { model.Agents = nil }()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "UnreadTask",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	// Create a completed execution — task has no last_read_at → isUnread=true
	sessionID, _ := service.CreateSession(env.ProjectDir, "claude", "Exec", "coder", "", "default", "scheduled")
	_, _ = service.AddTaskExecution(task.ID, sessionID, "auto")
	_ = service.UpdateExecutionStatus(sessionID, "completed")

	req := newRequest(t, http.MethodGet, fmt.Sprintf("/api/tasks/%d/executions", task.ID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertOK(t, w)

	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	executions, _ := result["executions"].([]interface{})
	require.Len(t, executions, 1)
	exec, _ := executions[0].(map[string]interface{})
	assert.Equal(t, true, exec["isUnread"], "completed execution should be unread when task has no last_read_at")
}

func TestServeTaskByID_Executions_RunningNotUnread(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.Agents = map[string]*model.Agent{
		"coder": {ID: "coder", Name: "Coder", Backend: "claude"},
	}
	defer func() { model.Agents = nil }()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "RunningTask",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	// Create a running execution (don't mark as completed)
	sessionID, _ := service.CreateSession(env.ProjectDir, "claude", "Running Exec", "coder", "", "default", "scheduled")
	_, _ = service.AddTaskExecution(task.ID, sessionID, "manual")
	// Don't call UpdateExecutionStatus — stays "running"

	req := newRequest(t, http.MethodGet, fmt.Sprintf("/api/tasks/%d/executions", task.ID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertOK(t, w)

	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	executions, _ := result["executions"].([]interface{})
	require.Len(t, executions, 1)
	exec, _ := executions[0].(map[string]interface{})
	assert.Equal(t, false, exec["isUnread"], "running execution should never be unread")
}

func TestServeTaskByID_Executions_ReadAtSuppressesUnread(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.Agents = map[string]*model.Agent{
		"coder": {ID: "coder", Name: "Coder", Backend: "claude"},
	}
	defer func() { model.Agents = nil }()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "ReadExecTask",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	// Create a completed execution and mark it as read
	sessionID, _ := service.CreateSession(env.ProjectDir, "claude", "Read Exec", "coder", "", "default", "scheduled")
	execID, _ := service.AddTaskExecution(task.ID, sessionID, "auto")
	_ = service.UpdateExecutionStatus(sessionID, "completed")
	// Mark the execution as read
	_ = service.MarkExecutionRead(fmt.Sprintf("%d", execID))

	req := newRequest(t, http.MethodGet, fmt.Sprintf("/api/tasks/%d/executions", task.ID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertOK(t, w)

	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	executions, _ := result["executions"].([]interface{})
	require.Len(t, executions, 1)
	exec, _ := executions[0].(map[string]interface{})
	assert.Equal(t, false, exec["isUnread"], "execution with read_at should not be unread")
}

func TestServeTaskByID_Executions_EmptyResult(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.Agents = map[string]*model.Agent{
		"coder": {ID: "coder", Name: "Coder", Backend: "claude"},
	}
	defer func() { model.Agents = nil }()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "EmptyTask",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	// No executions created
	req := newRequest(t, http.MethodGet, fmt.Sprintf("/api/tasks/%d/executions", task.ID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertOK(t, w)

	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	executions, _ := result["executions"].([]interface{})
	assert.Empty(t, executions, "should return empty array when no executions exist")
}

func TestServeTaskByID_Executions_WrongProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.Agents = map[string]*model.Agent{
		"coder": {ID: "coder", Name: "Coder", Backend: "claude"},
	}
	defer func() { model.Agents = nil }()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "OtherProject",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	// Request from a different project
	otherProject := t.TempDir()
	req := newRequest(t, http.MethodGet, fmt.Sprintf("/api/tasks/%d/executions", task.ID), nil)
	req = withProjectCookie(req, otherProject)
	w := callHandler(ServeTaskByID, req)
	assertStatus(t, w, http.StatusForbidden)
}

// ========== serveContinueConversationCheck ==========

func TestContinueConversation_GET_FoundWithSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	taskID, execID := helperCreateScheduledTaskForHandler(t, env, s)

	// Continue first to create a continued session
	contSessionID, _, err := service.ContinueFromExecution(execID, env.ProjectDir)
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, fmt.Sprintf("/api/tasks/%d/executions/%d/continue", taskID, execID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertOK(t, w)

	var resp map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.True(t, resp["exists"].(bool))
	assert.Equal(t, contSessionID, resp["sessionId"])
}

func TestContinueConversation_GET_NotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	// Query a non-existent execution ID
	req := newRequest(t, http.MethodGet, "/api/tasks/1/executions/99999/continue", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertStatus(t, w, http.StatusNotFound)
}

// ========== serveContinueConversationCreate ==========

func TestContinueConversation_POST_ExecutionNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	// Create a task so the task ownership check passes, but use a non-existent execution
	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "Task",
		CronExpr:    "0 8 * * *",
		AgentID:     "claude",
		Prompt:      "Test",
	}
	err := s.AddTask(task)
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, fmt.Sprintf("/api/tasks/%d/executions/99999/continue", task.ID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertStatus(t, w, http.StatusNotFound)
}

func TestContinueConversation_POST_StillRunning(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

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

func TestContinueConversation_POST_SessionLimit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	// Set a very low session limit
	origMax := model.SessionMaxCount
	model.SessionMaxCount = 0 // disable limit first to create sessions
	defer func() { model.SessionMaxCount = origMax }()

	taskID, execID := helperCreateScheduledTaskForHandler(t, env, s)

	// Create chat sessions up to the limit
	model.SessionMaxCount = 1
	for i := range 1 { // 1 session to hit the limit of 1
		_, err := service.CreateSession(env.ProjectDir, "claude", fmt.Sprintf("Chat %d", i), "claude", "", "default", "chat")
		require.NoError(t, err)
	}

	req := newRequest(t, http.MethodPost, fmt.Sprintf("/api/tasks/%d/executions/%d/continue", taskID, execID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertStatus(t, w, http.StatusConflict)
}

func TestContinueConversation_POST_AccessDenied(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	taskID, execID := helperCreateScheduledTaskForHandler(t, env, s)

	// Use wrong project cookie — task ownership check catches this first
	req := newRequest(t, http.MethodPost, fmt.Sprintf("/api/tasks/%d/executions/%d/continue", taskID, execID), nil)
	req = withProjectCookie(req, t.TempDir())
	w := callHandler(ServeTaskByID, req)

	assertStatus(t, w, http.StatusForbidden)
}

func TestContinueConversation_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	req := newRequest(t, http.MethodDelete, "/api/tasks/1/executions/1/continue", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertStatus(t, w, http.StatusMethodNotAllowed)
}

// ========== ServeTaskByID — deleteExecution (running) ==========

func TestServeTaskByID_DeleteExecution_Running(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.Agents = map[string]*model.Agent{
		"coder": {ID: "coder", Name: "Coder", Backend: "claude"},
	}
	defer func() { model.Agents = nil }()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "DelRunningExec",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	// Create a running execution
	sessionID, _ := service.CreateSession(env.ProjectDir, "claude", "Running Exec", "coder", "", "default", "scheduled")
	_, _ = service.AddTaskExecution(task.ID, sessionID, "manual")
	// Don't mark as completed — stays "running"

	var execID int64
	_ = service.UnsafeDBForTest().QueryRow("SELECT id FROM task_executions WHERE session_id = ?", sessionID).Scan(&execID)

	req := newRequest(t, http.MethodPut, fmt.Sprintf("/api/tasks/%d", task.ID), map[string]any{
		"action":      "deleteExecution",
		"executionId": fmt.Sprintf("%d", execID),
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertStatus(t, w, http.StatusConflict)
}

// ========== ServeTaskByID — deleteAllExecutions wrong project ==========

func TestServeTaskByID_DeleteAllExecutions_WrongProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.Agents = map[string]*model.Agent{
		"coder": {ID: "coder", Name: "Coder", Backend: "claude"},
	}
	defer func() { model.Agents = nil }()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "DelAllWrong",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	otherProject := t.TempDir()
	req := newRequest(t, http.MethodPut, fmt.Sprintf("/api/tasks/%d", task.ID), map[string]any{
		"action": "deleteAllExecutions",
	})
	req = withProjectCookie(req, otherProject)
	w := callHandler(ServeTaskByID, req)

	assertStatus(t, w, http.StatusForbidden)
}

// ========== ServeTaskByID — read action with executionID ==========

func TestServeTaskByID_ReadWithExecutionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.Agents = map[string]*model.Agent{
		"coder": {ID: "coder", Name: "Coder", Backend: "claude"},
	}
	defer func() { model.Agents = nil }()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "ReadExec",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	sessionID, _ := service.CreateSession(env.ProjectDir, "claude", "Exec", "coder", "", "default", "scheduled")
	execID, _ := service.AddTaskExecution(task.ID, sessionID, "auto")
	_ = service.UpdateExecutionStatus(sessionID, "completed")

	req := newRequest(t, http.MethodPut, fmt.Sprintf("/api/tasks/%d", task.ID), map[string]any{
		"action":      "read",
		"executionId": fmt.Sprintf("%d", execID),
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertOK(t, w)
}

func TestServeTaskByID_ReadWithoutExecutionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.Agents = map[string]*model.Agent{
		"coder": {ID: "coder", Name: "Coder", Backend: "claude"},
	}
	defer func() { model.Agents = nil }()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "ReadAll",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	// Task-level read: marks all executions as read
	req := newRequest(t, http.MethodPut, fmt.Sprintf("/api/tasks/%d", task.ID), map[string]any{
		"action": "read",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertOK(t, w)
}

// ========== ServeTaskByID — cancel action ==========

func TestServeTaskByID_Cancel_MissingExecutionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.Agents = map[string]*model.Agent{
		"coder": {ID: "coder", Name: "Coder", Backend: "claude"},
	}
	defer func() { model.Agents = nil }()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "CancelNoID",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	req := newRequest(t, http.MethodPut, fmt.Sprintf("/api/tasks/%d", task.ID), map[string]any{
		"action": "cancel",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeTaskByID_Cancel_ExecutionNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.Agents = map[string]*model.Agent{
		"coder": {ID: "coder", Name: "Coder", Backend: "claude"},
	}
	defer func() { model.Agents = nil }()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "CancelNotFound",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	req := newRequest(t, http.MethodPut, fmt.Sprintf("/api/tasks/%d", task.ID), map[string]any{
		"action":      "cancel",
		"executionId": "nonexistent-session-id",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertStatus(t, w, http.StatusNotFound)
}

// ========== ServeTaskByID — invalid task ID ==========

func TestServeTaskByID_InvalidTaskID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/tasks/not-a-number", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertStatus(t, w, http.StatusBadRequest)
}

// ========== ServeTaskByID — method not allowed ==========

func TestServeTaskByID_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPatch, "/api/tasks/1", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertStatus(t, w, http.StatusMethodNotAllowed)
}

// ========== ServeTaskByID — deleteExecution not found ==========

func TestServeTaskByID_DeleteExecution_NotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.Agents = map[string]*model.Agent{
		"coder": {ID: "coder", Name: "Coder", Backend: "claude"},
	}
	defer func() { model.Agents = nil }()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "DelExecNotFound",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	req := newRequest(t, http.MethodPut, fmt.Sprintf("/api/tasks/%d", task.ID), map[string]any{
		"action":      "deleteExecution",
		"executionId": "99999",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertStatus(t, w, http.StatusNotFound)
}

// ========== ServeTaskByID — update with MaxRuns ==========

func TestServeTaskByID_UpdateWithMaxRuns(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.Agents = map[string]*model.Agent{
		"coder": {ID: "coder", Name: "Coder", Backend: "claude"},
	}
	defer func() { model.Agents = nil }()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "MaxRunsTask",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "limited",
		MaxRuns:     5,
	}
	_ = s.AddTask(task)

	// Update MaxRuns to 0 (explicitly set)
	req := newRequest(t, http.MethodPut, fmt.Sprintf("/api/tasks/%d", task.ID), map[string]any{
		"action":   "update",
		"max_runs": 0,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertOK(t, w)

	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	returnedTask, _ := result["task"].(map[string]interface{})
	assert.Equal(t, float64(0), returnedTask["maxRuns"], "maxRuns should be explicitly set to 0")
}

// ========== ServeTaskByID — update reactivates completed task ==========

func TestServeTaskByID_UpdateReactivatesCompletedTask(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.Agents = map[string]*model.Agent{
		"coder": {ID: "coder", Name: "Coder", Backend: "claude"},
	}
	defer func() { model.Agents = nil }()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "CompletedTask",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "limited",
		MaxRuns:     1,
	}
	_ = s.AddTask(task)

	// Mark task as completed by simulating it
	_, _ = service.UnsafeDBForTest().Exec("UPDATE scheduled_tasks SET status = 'completed', run_count = 1 WHERE id = ?", task.ID)

	// Update should reactivate it
	req := newRequest(t, http.MethodPut, fmt.Sprintf("/api/tasks/%d", task.ID), map[string]any{
		"action": "update",
		"prompt": "Updated prompt",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertOK(t, w)

	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	returnedTask, _ := result["task"].(map[string]interface{})
	assert.Equal(t, "active", returnedTask["status"], "editing a completed task should reactivate it")
}

// ========== ServeTaskByID — sub-path dispatch isolation ==========
//
// A request carrying a sub-resource suffix must never fall through to the
// task-level CRUD switch. Before this was enforced, an unrecognized suffix
// aliased onto the parent task — most dangerously, DELETE on
// /api/tasks/{id}/executions deleted the entire task instead of being
// rejected.

// setupTaskForSubRoute creates a scheduler, a task owned by env.ProjectDir,
// and returns the task ID. Callers that need executions add them afterwards.
func setupTaskForSubRoute(t *testing.T, env *testEnv, name string) int64 {
	t.Helper()
	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        name,
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	require.NoError(t, service.GlobalScheduler.AddTask(task))
	return task.ID
}

// requireTaskExists asserts the task row is still present in the DB.
func requireTaskExists(t *testing.T, taskID int64, msg string) {
	t.Helper()
	task, err := service.GetTaskByID(taskID)
	require.NoError(t, err, msg)
	require.NotNil(t, task, msg)
}

func TestServeTaskByID_DeleteOnExecutions_RejectedNotDeletingTask(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.Agents = map[string]*model.Agent{
		"coder": {ID: "coder", Name: "Coder", Backend: "claude"},
	}
	defer func() { model.Agents = nil }()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	taskID := setupTaskForSubRoute(t, env, "DeleteOnExecutions")

	// Seed one execution so we can prove it survives too.
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Exec", "coder", "", "default", "scheduled")
	require.NoError(t, err)
	_, err = service.AddTaskExecution(taskID, sessionID, "auto")
	require.NoError(t, err)
	_ = service.UpdateExecutionStatus(sessionID, "completed")

	// DELETE on the executions collection is not part of the API surface.
	req := newRequest(t, http.MethodDelete, fmt.Sprintf("/api/tasks/%d/executions", taskID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertStatus(t, w, http.StatusMethodNotAllowed)

	// The critical regression guard: the task must NOT have been deleted.
	requireTaskExists(t, taskID, "DELETE on /executions must not delete the parent task")

	var execCount int
	_ = service.UnsafeDBForTest().
		QueryRow("SELECT COUNT(*) FROM task_executions WHERE task_id = ?", taskID).
		Scan(&execCount)
	assert.Equal(t, 1, execCount, "DELETE on /executions must not touch execution rows either")
}

func TestServeTaskByID_PutOnExecutions_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	taskID := setupTaskForSubRoute(t, env, "PutOnExecutions")

	// PUT with a task-update payload must not be applied to the parent task.
	req := newRequest(t, http.MethodPut, fmt.Sprintf("/api/tasks/%d/executions", taskID), map[string]any{
		"prompt": "hijacked",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertStatus(t, w, http.StatusMethodNotAllowed)

	task, err := service.GetTaskByID(taskID)
	require.NoError(t, err)
	assert.Equal(t, "Test", task.Prompt, "PUT on /executions must not update the parent task")
}

func TestServeTaskByID_UnknownSubPath_NotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	// Every verb on an unknown sub-path must 404 rather than alias to the task.
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			taskID := setupTaskForSubRoute(t, env, "UnknownSubPath"+method)

			req := newRequest(t, method, fmt.Sprintf("/api/tasks/%d/bogus", taskID), nil)
			req = withProjectCookie(req, env.ProjectDir)
			w := callHandler(ServeTaskByID, req)

			assertStatus(t, w, http.StatusNotFound)
			requireTaskExists(t, taskID, method+" on an unknown sub-path must not touch the task")
		})
	}
}

func TestServeTaskByID_ExecutionWithoutContinueSuffix_NotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	taskID := setupTaskForSubRoute(t, env, "ExecNoContinue")

	// `executions/{id}` alone is not a route — only `executions/{id}/continue` is.
	req := newRequest(t, http.MethodGet, fmt.Sprintf("/api/tasks/%d/executions/5", taskID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertStatus(t, w, http.StatusNotFound)
}

func TestServeTaskByID_UnknownTrailingSegmentOnContinue_NotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	taskID := setupTaskForSubRoute(t, env, "ExecBadTrailing")

	// A trailing segment that is not `continue` must not silently degrade
	// into a different sub-resource.
	req := newRequest(t, http.MethodGet, fmt.Sprintf("/api/tasks/%d/executions/5/resume", taskID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertStatus(t, w, http.StatusNotFound)
}

func TestServeTaskByID_DeleteOnUnknownSubPath_DoesNotDeleteTask(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	taskID := setupTaskForSubRoute(t, env, "DeleteUnknownSub")

	req := newRequest(t, http.MethodDelete, fmt.Sprintf("/api/tasks/%d/executions/9", taskID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertStatus(t, w, http.StatusNotFound)
	requireTaskExists(t, taskID, "DELETE on a sub-path must never delete the parent task")
}

// ========== Script fields (Phase 2) ==========

// TestServeTasks_PostWithScript asserts the create handler maps the script
// fields through to the persisted task using the exact JSON tag names.
func TestServeTasks_PostWithScript(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.Agents = map[string]*model.Agent{
		"coder": {ID: "coder", Name: "Coder", Backend: "claude"},
	}
	defer func() { model.Agents = nil }()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	req := newRequest(t, http.MethodPost, "/api/tasks", map[string]any{
		"name":           "Scripted Task",
		"cron_expr":      "0 * * * *",
		"agent_id":       "coder",
		"prompt":         "Do something",
		"script":         "echo hi",
		"script_timeout": 42,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTasks, req)
	assertOK(t, w)

	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	task, _ := result["task"].(map[string]any)
	require.NotNil(t, task)
	taskID := int64(task["id"].(float64))

	persisted, err := service.GetTaskByID(taskID)
	require.NoError(t, err)
	assert.Equal(t, "echo hi", persisted.Script)
	assert.Equal(t, 42, persisted.ScriptTimeout)
}

// TestServeTaskByID_PutWithScript asserts the update handler maps script fields.
func TestServeTaskByID_PutWithScript(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.Agents = map[string]*model.Agent{
		"coder": {ID: "coder", Name: "Coder", Backend: "claude"},
	}
	defer func() { model.Agents = nil }()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	taskID := setupTaskForSubRoute(t, env, "ScriptUpdate")

	req := newRequest(t, http.MethodPut, fmt.Sprintf("/api/tasks/%d", taskID), map[string]any{
		"action":         "update",
		"script":         "printf done",
		"script_timeout": 7,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertOK(t, w)

	persisted, err := service.GetTaskByID(taskID)
	require.NoError(t, err)
	assert.Equal(t, "printf done", persisted.Script)
	assert.Equal(t, 7, persisted.ScriptTimeout)
}

// TestServeTaskByID_DetailRunningCount_ScriptPhaseExcluded asserts the detail
// enrichment counts only the AI phase, matching the list endpoint.
func TestServeTaskByID_DetailRunningCount_ScriptPhaseExcluded(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	taskID := setupTaskForSubRoute(t, env, "PhaseCount")

	// A script-phase entry only: RunningExecutions must list it, RunningCount 0.
	s.AddRunningExecution(&service.RunningExecution{
		ID: "script-" + fmt.Sprintf("%d", taskID), TaskID: taskID,
		CancelFunc: func() {}, StartedAt: time.Now(), TriggerType: "auto",
		Phase: service.RunningPhaseScript,
	})

	req := newRequest(t, http.MethodGet, fmt.Sprintf("/api/tasks/%d", taskID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertOK(t, w)

	var task map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &task)
	// runningCount carries omitempty, so 0 is absent rather than present-and-0.
	count, present := task["runningCount"]
	if present {
		assert.Equal(t, float64(0), count, "the script phase must not be counted")
	}
	execs, _ := task["runningExecutions"].([]any)
	require.Len(t, execs, 1, "the script-phase entry must still be listed")
	first, _ := execs[0].(map[string]any)
	assert.Equal(t, "script", first["phase"])
}
