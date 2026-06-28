package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
)

// ---------- ServeAgents ----------

func TestServeAgents_Get(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Set up agent list
	model.AgentList = []*model.Agent{
		{ID: "agent-1", Name: "Agent 1", Backend: "claude"},
		{ID: "agent-2", Name: "Agent 2", Backend: "codebuddy"},
	}
	defer func() { model.AgentList = nil }()

	req := newRequest(t, http.MethodGet, "/api/agents", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAgents, req)

	assertOK(t, w)
	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	agents, _ := result["agents"].([]interface{})
	assert.Len(t, agents, 2)
}

func TestServeAgents_PostNotAllowed(t *testing.T) {
	// POST /api/agents is now used for agent duplication;
	// without a valid JSON body it returns 400 (bad request), not 405.
	req := newRequest(t, http.MethodPost, "/api/agents", nil)
	w := callHandler(ServeAgents, req)
	assertStatus(t, w, http.StatusBadRequest)
}

// ---------- ServeChatCount ----------

func TestServeChatCount(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid := createTestSession(t, env.ProjectDir)

	// Add messages
	_, _ = service.AddChatMessage(env.ProjectDir, "claude", sid, "user", "Hello", nil, false, "NewSession")
	_, _ = service.AddChatMessage(env.ProjectDir, "claude", sid, "assistant", "Hi", nil, false, "NewSession")

	req := newRequest(t, http.MethodGet, "/api/ai/chat/count?session_id="+sid, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeChatCount, req)

	assertOK(t, w)
	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	assert.Equal(t, float64(2), result["count"])
}

func TestServeChatCount_NoSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/count", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeChatCount, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeChatCount_PostNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/chat/count", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeChatCount, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

// ---------- ServeChatMessageUpdate ----------

func TestServeChatMessageUpdate(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid := createTestSession(t, env.ProjectDir)
	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", sid, "user", "original", nil, false, "NewSession")
	assert.NoError(t, err)

	req := newRequest(t, http.MethodPut, "/api/ai/chat/message", map[string]any{
		"messageId": msgID,
		"content":   "updated content",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeChatMessageUpdate, req)

	assertOK(t, w)
	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	assert.Equal(t, true, result["ok"])
}

func TestServeChatMessageUpdate_NoMessageID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPut, "/api/ai/chat/message", map[string]any{
		"content": "no id",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeChatMessageUpdate, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeChatMessageUpdate_InvalidBody(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodPut, "/api/ai/chat/message", http.NoBody)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	ServeChatMessageUpdate(w, req)
	assertStatus(t, w, http.StatusForbidden) // now requires project cookie
}

func TestServeChatMessageUpdate_GetNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/message", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeChatMessageUpdate, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

// ---------- ServeTasks ----------

func TestServeTasks_GetEmpty(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	req := newRequest(t, http.MethodGet, "/api/tasks", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTasks, req)

	assertOK(t, w)
	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	tasks, _ := result["tasks"].([]interface{})
	assert.Empty(t, tasks)
}

func TestServeTasks_Post(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Set up agents so the scheduler can resolve them
	model.Agents = map[string]*model.Agent{
		"coder": {ID: "coder", Name: "Coder", Backend: "claude"},
	}
	defer func() { model.Agents = nil }()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	req := newRequest(t, http.MethodPost, "/api/tasks", map[string]any{
		"name":      "Test Task",
		"cron_expr": "0 * * * *",
		"agent_id":  "coder",
		"prompt":    "Do something",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTasks, req)

	assertOK(t, w)
	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	assert.Equal(t, true, result["ok"])
}

func TestServeTasks_PostMissingFields(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	req := newRequest(t, http.MethodPost, "/api/tasks", map[string]any{
		"name": "Test Task",
		// Missing cron_expr, agent_id, prompt
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTasks, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeTasks_PostAssistantAgent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// All agents are allowed for scheduled tasks
	model.Agents = map[string]*model.Agent{
		"assistant": {ID: "assistant", Name: "Assistant", Backend: "codebuddy"},
	}
	defer func() { model.Agents = nil }()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	req := newRequest(t, http.MethodPost, "/api/tasks", map[string]any{
		"name":      "Test Task",
		"cron_expr": "0 * * * *",
		"agent_id":  "assistant",
		"prompt":    "Do something",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTasks, req)

	assertOK(t, w)
	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	assert.Equal(t, true, result["ok"])
}

func TestServeTasks_NoProject(t *testing.T) {
	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	req := newRequest(t, http.MethodGet, "/api/tasks", nil)
	w := callHandler(ServeTasks, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestServeTasks_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	req := newRequest(t, http.MethodDelete, "/api/tasks", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTasks, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

// ---------- ServeTaskByID ----------

func TestServeTaskByID_Get(t *testing.T) {
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

	// Create a task first
	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "Test Task",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test prompt",
		RepeatMode:  "unlimited",
	}
	err := s.AddTask(task)
	assert.NoError(t, err)

	req := newRequest(t, http.MethodGet, fmt.Sprintf("/api/tasks/%d", task.ID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertOK(t, w)
}

func TestServeTaskByID_Delete(t *testing.T) {
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
		Name:        "Delete Me",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	req := newRequest(t, http.MethodDelete, fmt.Sprintf("/api/tasks/%d", task.ID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertOK(t, w)
}

func TestServeTaskByID_Pause(t *testing.T) {
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
		Name:        "Pause Me",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	req := newRequest(t, http.MethodPut, fmt.Sprintf("/api/tasks/%d", task.ID), map[string]any{
		"action": "pause",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertOK(t, w)
}

func TestServeTaskByID_Resume(t *testing.T) {
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
		Name:        "Resume Me",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)
	s.PauseTask(task.ID)

	req := newRequest(t, http.MethodPut, fmt.Sprintf("/api/tasks/%d", task.ID), map[string]any{
		"action": "resume",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertOK(t, w)
}

// ---------- ServeTaskByID Trigger (ISS-187) ----------

func TestServeTaskByID_Trigger_AlreadyRunning(t *testing.T) {
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
		Name:        "Running Task",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	// Simulate a running task using the public MarkTaskRunning helper (ISS-187)
	s.MarkTaskRunning(task.ID)
	defer s.UnmarkTaskRunning(task.ID)

	// Trigger should return 409 Conflict since task is already running
	req := newRequest(t, http.MethodPut, fmt.Sprintf("/api/tasks/%d", task.ID), map[string]any{
		"action": "trigger",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertStatus(t, w, http.StatusConflict)
}

func TestServeTaskByID_Trigger_TaskNotFound(t *testing.T) {
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

	// Trigger a non-existent task — returns NotFound error from TriggerTask
	req := newRequest(t, http.MethodPut, "/api/tasks/99999", map[string]any{
		"action": "trigger",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertStatus(t, w, http.StatusNotFound)
}

func TestServeTaskByID_NotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	req := newRequest(t, http.MethodGet, "/api/tasks/99999", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertStatus(t, w, http.StatusNotFound)
}

func TestServeTaskByID_NoTaskID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/tasks/", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertStatus(t, w, http.StatusBadRequest)
}

// ---------- ServeProjectDialog ----------

func TestServeProjectDialog_PostNotAllowed(t *testing.T) {
	req := newRequest(t, http.MethodPost, "/dialog/project", nil)
	w := callHandler(ServeProjectDialog, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

// ---------- ServeIndex ----------

func TestServeIndex_NotFound(t *testing.T) {
	// In a test environment, public/ and web/ don't exist, so we get 404
	req := newRequest(t, http.MethodGet, "/nonexistent-path.js", nil)
	w := callHandler(ServeIndex, req)
	assertStatus(t, w, http.StatusNotFound)
}

// ---------- serveTaskExecutions ----------

func TestServeTaskByID_Executions(t *testing.T) {
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

	// Create a task
	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "Exec Task",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	// Create a scheduled session + messages + task_execution
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Exec Task", "coder", "", "default", "scheduled")
	assert.NoError(t, err)
	_, _ = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", "test prompt", nil, false, "Exec Task")
	_, _ = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", "test response", nil, false, "Exec Task")
	_, _ = service.AddTaskExecution(task.ID, sessionID, "manual")

	// Get executions
	req := newRequest(t, http.MethodGet, fmt.Sprintf("/api/tasks/%d", task.ID)+"/executions", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertOK(t, w)

	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	executions, _ := result["executions"].([]interface{})
	assert.Len(t, executions, 1)

	exec, _ := executions[0].(map[string]interface{})
	assert.Equal(t, sessionID, exec["sessionId"])
	assert.Equal(t, "manual", exec["triggerType"])
	assert.Equal(t, "running", exec["status"])
}

func TestServeTaskByID_ExecutionsTaskNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	req := newRequest(t, http.MethodGet, "/api/tasks/99999/executions", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertStatus(t, w, http.StatusNotFound)
}

// ---------- ServeTaskByID Update ----------

func TestServeTaskByID_Update(t *testing.T) {
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
		Name:        "Update Me",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	req := newRequest(t, http.MethodPut, fmt.Sprintf("/api/tasks/%d", task.ID), map[string]any{
		"action":    "update",
		"name":      "Updated Name",
		"cron_expr": "0 */2 * * *",
		"prompt":    "Updated prompt",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertOK(t, w)
}

func TestServeTaskByID_UpdateAssistantAgent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// All agents are allowed for scheduled tasks
	model.Agents = map[string]*model.Agent{
		"coder":     {ID: "coder", Name: "Coder", Backend: "claude"},
		"assistant": {ID: "assistant", Name: "Assistant", Backend: "codebuddy"},
	}
	defer func() { model.Agents = nil }()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "Update Agent",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	req := newRequest(t, http.MethodPut, fmt.Sprintf("/api/tasks/%d", task.ID), map[string]any{
		"agent_id": "assistant",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertOK(t, w)
}

func TestServeTaskByID_UpdateTaskNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	req := newRequest(t, http.MethodPut, "/api/tasks/99999", map[string]any{
		"action": "update",
		"name":   "Updated",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertStatus(t, w, http.StatusNotFound)
}

// ---------- ISS-006: Cross-project ownership tests ----------

func TestServeTaskByID_WrongProject_Get(t *testing.T) {
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

	// Create a task under the real project
	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "My Task",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	// Try to access with a different project cookie
	otherProject := t.TempDir()
	req := newRequest(t, http.MethodGet, fmt.Sprintf("/api/tasks/%d", task.ID), nil)
	req = withProjectCookie(req, otherProject)
	w := callHandler(ServeTaskByID, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestServeTaskByID_WrongProject_Delete(t *testing.T) {
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
		Name:        "My Task",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	otherProject := t.TempDir()
	req := newRequest(t, http.MethodDelete, fmt.Sprintf("/api/tasks/%d", task.ID), nil)
	req = withProjectCookie(req, otherProject)
	w := callHandler(ServeTaskByID, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestServeTaskByID_WrongProject_Pause(t *testing.T) {
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
		Name:        "My Task",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	otherProject := t.TempDir()
	req := newRequest(t, http.MethodPut, fmt.Sprintf("/api/tasks/%d", task.ID), map[string]any{
		"action": "pause",
	})
	req = withProjectCookie(req, otherProject)
	w := callHandler(ServeTaskByID, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestServeTaskByID_WrongProject_Executions(t *testing.T) {
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
		Name:        "My Task",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)
	_, _ = service.AddTaskExecution(task.ID, `{"blocks":[{"type":"text","text":"result"}]}`, "manual")

	otherProject := t.TempDir()
	req := newRequest(t, http.MethodGet, fmt.Sprintf("/api/tasks/%d", task.ID)+"/executions", nil)
	req = withProjectCookie(req, otherProject)
	w := callHandler(ServeTaskByID, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestServeTaskByID_NoProject(t *testing.T) {
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
		Name:        "My Task",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	// No project cookie at all → 403
	req := newRequest(t, http.MethodGet, fmt.Sprintf("/api/tasks/%d", task.ID), nil)
	w := callHandler(ServeTaskByID, req)
	assertStatus(t, w, http.StatusForbidden)
}

// ---------- ISS-002: Cross-project chat ownership tests ----------

func TestServeChatCount_WrongProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid := createTestSession(t, env.ProjectDir)
	_, _ = service.AddChatMessage(env.ProjectDir, "claude", sid, "user", "Hello", nil, false, "NewSession")

	// Try to count messages from another project's session
	otherProject := t.TempDir()
	req := newRequest(t, http.MethodGet, "/api/ai/chat/count?session_id="+sid, nil)
	req = withProjectCookie(req, otherProject)
	w := callHandler(ServeChatCount, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestServeChatMessageUpdate_WrongProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid := createTestSession(t, env.ProjectDir)
	msgID, err := service.AddChatMessage(env.ProjectDir, "claude", sid, "user", "original", nil, false, "NewSession")
	assert.NoError(t, err)

	// Try to update message from another project
	otherProject := t.TempDir()
	req := newRequest(t, http.MethodPut, "/api/ai/chat/message", map[string]any{
		"messageId": msgID,
		"content":   "hacked content",
	})
	req = withProjectCookie(req, otherProject)
	w := callHandler(ServeChatMessageUpdate, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestCancelChat_WrongProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid := createTestSession(t, env.ProjectDir)

	// Try to cancel session from another project
	otherProject := t.TempDir()
	req := newRequest(t, http.MethodPost, "/api/ai/chat/cancel?session_id="+sid, nil)
	req = withProjectCookie(req, otherProject)
	w := callHandler(CancelChat, req)
	assertStatus(t, w, http.StatusForbidden)
}

// ---------- deleteExecution / deleteAllExecutions ----------

func TestServeTaskByID_DeleteExecution(t *testing.T) {
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

	// Create task
	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "DelExec",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	// Create a scheduled session and execution
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Exec 1", "coder", "", "default", "scheduled")
	assert.NoError(t, err)
	_, err = service.AddTaskExecution(task.ID, sessionID, "auto")
	assert.NoError(t, err)

	// Get execution ID and mark it as completed (simulates finished execution)
	var execID int64
	err = service.DB.QueryRow("SELECT id FROM task_executions WHERE session_id = ?", sessionID).Scan(&execID)
	assert.NoError(t, err)
	_ = service.UpdateExecutionStatus(sessionID, "completed")

	// Delete the execution via API
	req := newRequest(t, http.MethodPut, fmt.Sprintf("/api/tasks/%d", task.ID), map[string]any{
		"action":      "deleteExecution",
		"executionId": fmt.Sprintf("%d", execID),
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertOK(t, w)

	// Verify execution is deleted
	var count int
	_ = service.DB.QueryRow("SELECT COUNT(*) FROM task_executions WHERE id = ?", execID).Scan(&count)
	assert.Equal(t, 0, count)
}

func TestServeTaskByID_DeleteExecution_MissingExecutionID(t *testing.T) {
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
		Name:        "DelExec",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	req := newRequest(t, http.MethodPut, fmt.Sprintf("/api/tasks/%d", task.ID), map[string]any{
		"action": "deleteExecution",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeTaskByID_DeleteExecution_InvalidExecutionID(t *testing.T) {
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
		Name:        "DelExec",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	req := newRequest(t, http.MethodPut, fmt.Sprintf("/api/tasks/%d", task.ID), map[string]any{
		"action":      "deleteExecution",
		"executionId": "not-a-number",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeTaskByID_DeleteExecution_WrongProject(t *testing.T) {
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
		Name:        "DelExec",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	sessionID, _ := service.CreateSession(env.ProjectDir, "claude", "Exec", "coder", "", "default", "scheduled")
	_, _ = service.AddTaskExecution(task.ID, sessionID, "auto")
	_ = service.UpdateExecutionStatus(sessionID, "completed")
	var execID int64
	_ = service.DB.QueryRow("SELECT id FROM task_executions WHERE session_id = ?", sessionID).Scan(&execID)

	// Request from a different project should be forbidden
	otherProject := t.TempDir()
	req := newRequest(t, http.MethodPut, fmt.Sprintf("/api/tasks/%d", task.ID), map[string]any{
		"action":      "deleteExecution",
		"executionId": fmt.Sprintf("%d", execID),
	})
	req = withProjectCookie(req, otherProject)
	w := callHandler(ServeTaskByID, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestServeTaskByID_DeleteAllExecutions(t *testing.T) {
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
		Name:        "DelAllExec",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	// Create 2 executions (mark as completed to simulate finished executions)
	for i := range 2 {
		sessionID, _ := service.CreateSession(env.ProjectDir, "claude", fmt.Sprintf("Exec %d", i), "coder", "", "default", "scheduled")
		_, _ = service.AddTaskExecution(task.ID, sessionID, "auto")
		_ = service.UpdateExecutionStatus(sessionID, "completed")
	}

	// Verify 2 executions exist
	var count int
	_ = service.DB.QueryRow("SELECT COUNT(*) FROM task_executions WHERE task_id = ?", task.ID).Scan(&count)
	assert.Equal(t, 2, count)

	// Delete all via API
	req := newRequest(t, http.MethodPut, fmt.Sprintf("/api/tasks/%d", task.ID), map[string]any{
		"action": "deleteAllExecutions",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertOK(t, w)

	// Verify all executions deleted
	_ = service.DB.QueryRow("SELECT COUNT(*) FROM task_executions WHERE task_id = ?", task.ID).Scan(&count)
	assert.Equal(t, 0, count)
}

// ---------- serveTaskExecutions — cursor-based pagination ----------

func TestServeTaskByID_Executions_WithLimit(t *testing.T) {
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
		Name:        "Paged Task",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	// Create 3 completed executions
	for i := range 3 {
		sessionID, err := service.CreateSession(env.ProjectDir, "claude", fmt.Sprintf("Exec %d", i), "coder", "", "default", "scheduled")
		assert.NoError(t, err)
		_, _ = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", fmt.Sprintf("prompt %d", i), nil, false, "Exec")
		_, _ = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", fmt.Sprintf("response %d", i), nil, false, "Exec")
		_, _ = service.AddTaskExecution(task.ID, sessionID, "auto")
		_ = service.UpdateExecutionStatus(sessionID, "completed")
	}

	// Request with limit=2
	req := newRequest(t, http.MethodGet, fmt.Sprintf("/api/tasks/%d/executions?limit=2", task.ID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertOK(t, w)

	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	executions, _ := result["executions"].([]interface{})
	assert.Len(t, executions, 2)
	assert.Equal(t, true, result["hasMore"])
}

func TestServeTaskByID_Executions_LimitNoMore(t *testing.T) {
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
		Name:        "Paged Task",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	// Create only 1 execution
	sessionID, _ := service.CreateSession(env.ProjectDir, "claude", "Exec 0", "coder", "", "default", "scheduled")
	_, _ = service.AddTaskExecution(task.ID, sessionID, "auto")
	_ = service.UpdateExecutionStatus(sessionID, "completed")

	// Request with limit=5 (more than available)
	req := newRequest(t, http.MethodGet, fmt.Sprintf("/api/tasks/%d/executions?limit=5", task.ID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertOK(t, w)

	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	executions, _ := result["executions"].([]interface{})
	assert.Len(t, executions, 1)
	assert.Equal(t, false, result["hasMore"])
}

func TestServeTaskByID_Executions_CursorPagination(t *testing.T) {
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
		Name:        "Paged Task",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	// Create 5 completed executions
	for i := range 5 {
		sessionID, _ := service.CreateSession(env.ProjectDir, "claude", fmt.Sprintf("Exec %d", i), "coder", "", "default", "scheduled")
		_, _ = service.AddTaskExecution(task.ID, sessionID, "auto")
		_ = service.UpdateExecutionStatus(sessionID, "completed")
	}

	// Page 1: limit=2
	req1 := newRequest(t, http.MethodGet, fmt.Sprintf("/api/tasks/%d/executions?limit=2", task.ID), nil)
	req1 = withProjectCookie(req1, env.ProjectDir)
	w1 := callHandler(ServeTaskByID, req1)
	assertOK(t, w1)

	var result1 map[string]any
	_ = json.Unmarshal(w1.Body.Bytes(), &result1)
	executions1, _ := result1["executions"].([]interface{})
	assert.Len(t, executions1, 2)
	assert.Equal(t, true, result1["hasMore"])

	// Extract cursor from last item of page 1
	lastExec1, _ := executions1[1].(map[string]interface{})
	cursor, _ := lastExec1["createdAt"].(string)
	cursorID := fmt.Sprintf("%v", lastExec1["id"])

	// Page 2: use cursor from last item of page 1
	req2 := newRequest(t, http.MethodGet,
		fmt.Sprintf("/api/tasks/%d/executions?limit=2&cursor=%s&cursor_id=%s", task.ID, cursor, cursorID), nil)
	req2 = withProjectCookie(req2, env.ProjectDir)
	w2 := callHandler(ServeTaskByID, req2)
	assertOK(t, w2)

	var result2 map[string]any
	_ = json.Unmarshal(w2.Body.Bytes(), &result2)
	executions2, _ := result2["executions"].([]interface{})
	assert.Len(t, executions2, 2)
	assert.Equal(t, true, result2["hasMore"])

	// Verify page 2 IDs are different from page 1
	firstExec2ID := fmt.Sprintf("%v", executions2[0].(map[string]interface{})["id"])
	assert.NotEqual(t, cursorID, firstExec2ID)

	// Page 3: remaining item
	lastExec2, _ := executions2[1].(map[string]interface{})
	cursor2, _ := lastExec2["createdAt"].(string)
	cursorID2 := fmt.Sprintf("%v", lastExec2["id"])

	req3 := newRequest(t, http.MethodGet,
		fmt.Sprintf("/api/tasks/%d/executions?limit=2&cursor=%s&cursor_id=%s", task.ID, cursor2, cursorID2), nil)
	req3 = withProjectCookie(req3, env.ProjectDir)
	w3 := callHandler(ServeTaskByID, req3)
	assertOK(t, w3)

	var result3 map[string]any
	_ = json.Unmarshal(w3.Body.Bytes(), &result3)
	executions3, _ := result3["executions"].([]interface{})
	assert.Len(t, executions3, 1)
	assert.Equal(t, false, result3["hasMore"])
}

func TestServeTaskByID_Executions_NoLimitBackwardCompat(t *testing.T) {
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
		Name:        "NoLimit Task",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	_ = s.AddTask(task)

	// Create 3 executions
	for i := range 3 {
		sessionID, _ := service.CreateSession(env.ProjectDir, "claude", fmt.Sprintf("Exec %d", i), "coder", "", "default", "scheduled")
		_, _ = service.AddTaskExecution(task.ID, sessionID, "auto")
		_ = service.UpdateExecutionStatus(sessionID, "completed")
	}

	// Request without limit — should return all and no hasMore field
	req := newRequest(t, http.MethodGet, fmt.Sprintf("/api/tasks/%d/executions", task.ID), nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertOK(t, w)

	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	executions, _ := result["executions"].([]interface{})
	assert.Len(t, executions, 3)
	// hasMore should NOT be present (backward compat: no limit = no pagination)
	_, hasHasMore := result["hasMore"]
	assert.False(t, hasHasMore)
}

// ---------- helper ----------

func createTestSession(t *testing.T, projectPath string) string {
	t.Helper()
	id, err := service.CreateSession(projectPath, "claude", "Test Session", "", "", "default", "chat")
	if err != nil {
		t.Fatalf("failed to create test session: %v", err)
	}
	t.Cleanup(func() {
		service.SetSessionRunning(id, false)
	})
	return id
}

// ---------- hasUnread logic (derived from tasks.UnreadCount) ----------

// TestServeTasks_Get_HasUnreadTrue verifies that hasUnread is true when at
// least one task has UnreadCount > 0.
func TestServeTasks_Get_HasUnreadTrue(t *testing.T) {
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

	// Create a task
	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "Unread Task",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	err := s.AddTask(task)
	assert.NoError(t, err)

	// Create a completed execution (not read) → makes UnreadCount = 1
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Exec", "coder", "", "default", "scheduled")
	assert.NoError(t, err)
	_, err = service.AddTaskExecution(task.ID, sessionID, "auto")
	assert.NoError(t, err)
	_ = service.UpdateExecutionStatus(sessionID, "completed")

	req := newRequest(t, http.MethodGet, "/api/tasks", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTasks, req)

	assertOK(t, w)
	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	assert.Equal(t, true, result["hasUnread"], "hasUnread should be true when a task has unread executions")
}

// TestServeTasks_Get_HasUnreadFalse verifies that hasUnread is false when
// all tasks have UnreadCount == 0.
func TestServeTasks_Get_HasUnreadFalse(t *testing.T) {
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

	// Create a task with no executions → UnreadCount = 0
	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "Read Task",
		CronExpr:    "0 * * * *",
		AgentID:     "coder",
		Prompt:      "Test",
		RepeatMode:  "unlimited",
	}
	err := s.AddTask(task)
	assert.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/tasks", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTasks, req)

	assertOK(t, w)
	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	assert.Equal(t, false, result["hasUnread"], "hasUnread should be false when no tasks have unread executions")
}
