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
