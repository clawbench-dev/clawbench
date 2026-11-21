package tasks

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 这些用例覆盖任务接口的参数校验分支。它们从 handler 搬过来时只有
// ServeTaskByID 的动作分支有测试，列表/创建/子资源分派一直是空的。

func TestServeTasks_RequiresProject(t *testing.T) {
	_, teardown := setupTestEnv(t)
	t.Cleanup(teardown)

	w := callHandler(ServeTasks, newRequest(t, http.MethodGet, "/api/tasks", nil))
	assertStatus(t, w, http.StatusForbidden)
}

func TestServeTasks_ListEmptyAndWithRows(t *testing.T) {
	env, teardown := setupTestEnv(t)
	t.Cleanup(teardown)
	// ServeTasks 读调度器的运行中计数；生产上它总是被装配好的。
	s := service.NewScheduler()
	t.Cleanup(s.Stop)
	service.GlobalScheduler = s
	t.Cleanup(func() { service.GlobalScheduler = nil })

	// 空列表：必须是 [] 而不是 null，否则前端要额外判空。
	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/tasks", nil), env.ProjectDir)
	w := callHandler(ServeTasks, req)
	assertOK(t, w)
	var empty struct {
		Tasks     []map[string]any `json:"tasks"`
		HasUnread bool             `json:"hasUnread"`
	}
	decodeRespJSON(t, w.Body, &empty)
	assert.Empty(t, empty.Tasks)
	assert.False(t, empty.HasUnread)

	require.NoError(t, s.AddTask(&model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "nightly",
		CronExpr:    "0 3 * * *",
		AgentID:     "a1",
		Prompt:      "go",
		RepeatMode:  "unlimited",
	}))

	req = withProjectCookie(newRequest(t, http.MethodGet, "/api/tasks", nil), env.ProjectDir)
	w = callHandler(ServeTasks, req)
	assertOK(t, w)
	var withRows struct {
		Tasks []struct {
			Name string `json:"name"`
		} `json:"tasks"`
	}
	decodeRespJSON(t, w.Body, &withRows)
	require.Len(t, withRows.Tasks, 1)
	assert.Equal(t, "nightly", withRows.Tasks[0].Name)
}

func TestServeTasks_CreateValidation(t *testing.T) {
	env, teardown := setupTestEnv(t)
	t.Cleanup(teardown)

	cases := []struct {
		name string
		body map[string]any
		key  string
	}{
		{"missing required fields", map[string]any{"name": "x"}, "TaskFieldsRequired"},
		{"cron task without expression", map[string]any{
			"name": "x", "agent_id": "a", "prompt": "p", "trigger_mode": "cron",
		}, "TaskFieldsRequired"},
		{"unknown trigger mode", map[string]any{
			"name": "x", "agent_id": "a", "prompt": "p", "cron_expr": "* * * * *", "trigger_mode": "webhook",
		}, "TaskTriggerModeInvalid"},
		{"email trigger without mailbox", map[string]any{
			"name": "x", "agent_id": "a", "prompt": "p", "trigger_mode": "email",
		}, "TaskMailAccountRequired"},
		{"event trigger with a bad subscription", map[string]any{
			"name": "x", "agent_id": "a", "prompt": "p", "trigger_mode": "event", "event_types": "nope",
		}, "TaskEventTypesInvalid"},
		{"negative script timeout", map[string]any{
			"name": "x", "agent_id": "a", "prompt": "p", "cron_expr": "* * * * *", "script_timeout": -1,
		}, "TaskScriptTimeoutInvalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := withProjectCookie(newRequest(t, http.MethodPost, "/api/tasks", tc.body), env.ProjectDir)
			w := callHandler(ServeTasks, req)
			assertStatus(t, w, http.StatusBadRequest)
			assert.Contains(t, w.Body.String(), tc.key)
		})
	}
}

func TestServeTasks_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	t.Cleanup(teardown)

	req := withProjectCookie(newRequest(t, http.MethodPut, "/api/tasks", nil), env.ProjectDir)
	w := callHandler(ServeTasks, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestServeTaskSubRoute_UnknownAndWrongMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	t.Cleanup(teardown)

	// 未知子资源 → 404
	w := httptest.NewRecorder()
	serveTaskSubRoute(w, newRequest(t, http.MethodGet, "/api/tasks/1/whatever", nil), 1, "whatever", env.ProjectDir)
	assertStatus(t, w, http.StatusNotFound)

	// executions 只接受 GET
	w = httptest.NewRecorder()
	serveTaskSubRoute(w, newRequest(t, http.MethodPost, "/api/tasks/1/executions", nil), 1, "executions", env.ProjectDir)
	assertStatus(t, w, http.StatusMethodNotAllowed)

	// 执行 id 不是数字 → 400
	w = httptest.NewRecorder()
	serveTaskSubRoute(w, newRequest(t, http.MethodGet, "/api/tasks/1/executions/x/continue", nil), 1, "executions/x/continue", env.ProjectDir)
	assertStatus(t, w, http.StatusBadRequest)

	// 只有 executions/{id}/continue 是合法子资源
	w = httptest.NewRecorder()
	serveTaskSubRoute(w, newRequest(t, http.MethodGet, "/api/tasks/1/executions/7", nil), 1, "executions/7", env.ProjectDir)
	assertStatus(t, w, http.StatusNotFound)
}

func TestServeTasks_CreateRejectsDisabledOrForeignAgent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	t.Cleanup(teardown)

	origAgents := model.Agents
	t.Cleanup(func() { model.Agents = origAgents })
	model.Agents = map[string]*model.Agent{
		"off":   {ID: "off", Name: "Off", Disabled: true},
		"other": {ID: "other", Name: "Other"},
	}

	// 停用的智能体不能用来新建任务（已存在的任务继续跑，便于清理）。
	req := withProjectCookie(newRequest(t, http.MethodPost, "/api/tasks", map[string]any{
		"name": "x", "agent_id": "off", "prompt": "p", "cron_expr": "* * * * *",
	}), env.ProjectDir)
	w := callHandler(ServeTasks, req)
	assertStatus(t, w, http.StatusBadRequest)
	assert.Contains(t, w.Body.String(), "AgentDisabled")

	// 别的项目的专属智能体同样不行。
	otherPid := int64(999999)
	model.Agents["other"] = &model.Agent{ID: "other", Name: "Other", ProjectID: &otherPid}
	req = withProjectCookie(newRequest(t, http.MethodPost, "/api/tasks", map[string]any{
		"name": "x", "agent_id": "other", "prompt": "p", "cron_expr": "* * * * *",
	}), env.ProjectDir)
	w = callHandler(ServeTasks, req)
	assertStatus(t, w, http.StatusBadRequest)
	assert.Contains(t, w.Body.String(), "AgentNotInProject")
}

func TestServeTasks_CreateRejectsMalformedBody(t *testing.T) {
	env, teardown := setupTestEnv(t)
	t.Cleanup(teardown)

	req := withProjectCookie(newRequest(t, http.MethodPost, "/api/tasks", nil), env.ProjectDir)
	req.Body = io.NopCloser(strings.NewReader("{"))
	w := callHandler(ServeTasks, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeTaskByID_MissingAndUnknown(t *testing.T) {
	env, teardown := setupTestEnv(t)
	t.Cleanup(teardown)

	// 没有 id
	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/tasks/", nil), env.ProjectDir)
	w := callHandler(ServeTaskByID, req)
	assertStatus(t, w, http.StatusBadRequest)

	// 不存在的 id
	req = withProjectCookie(newRequest(t, http.MethodGet, "/api/tasks/424242", nil), env.ProjectDir)
	w = callHandler(ServeTaskByID, req)
	assertStatus(t, w, http.StatusNotFound)
}

func TestServeTaskByID_ForeignProjectIsForbidden(t *testing.T) {
	env, teardown := setupTestEnv(t)
	t.Cleanup(teardown)

	s := service.NewScheduler()
	t.Cleanup(s.Stop)
	service.GlobalScheduler = s
	t.Cleanup(func() { service.GlobalScheduler = nil })

	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "owned",
		CronExpr:    "0 3 * * *",
		AgentID:     "a1",
		Prompt:      "p",
		RepeatMode:  "unlimited",
	}
	require.NoError(t, s.AddTask(task))

	// 另一个项目读别人的任务必须 403。
	other := filepath.Join(env.WatchDir, "other")
	require.NoError(t, os.MkdirAll(other, 0o755))
	store.ProjectIDForTest(t, other)

	req := withProjectCookie(newRequest(t, http.MethodGet, fmt.Sprintf("/api/tasks/%d", task.ID), nil), other)
	w := callHandler(ServeTaskByID, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestServeTaskByID_UpdateActions(t *testing.T) {
	env, teardown := setupTestEnv(t)
	t.Cleanup(teardown)

	s := service.NewScheduler()
	t.Cleanup(s.Stop)
	service.GlobalScheduler = s
	t.Cleanup(func() { service.GlobalScheduler = nil })

	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "pausable",
		CronExpr:    "0 3 * * *",
		AgentID:     "a1",
		Prompt:      "p",
		RepeatMode:  "unlimited",
	}
	require.NoError(t, s.AddTask(task))
	path := fmt.Sprintf("/api/tasks/%d", task.ID)

	// 坏 body
	req := withProjectCookie(newRequest(t, http.MethodPut, path, nil), env.ProjectDir)
	req.Body = io.NopCloser(strings.NewReader("{"))
	w := callHandler(ServeTaskByID, req)
	assertStatus(t, w, http.StatusBadRequest)

	// 不存在的任务
	req = withProjectCookie(newRequest(t, http.MethodPut, "/api/tasks/424242", map[string]any{"action": "pause"}), env.ProjectDir)
	w = callHandler(ServeTaskByID, req)
	assertStatus(t, w, http.StatusNotFound)

	// pause
	req = withProjectCookie(newRequest(t, http.MethodPut, path, map[string]any{"action": "pause"}), env.ProjectDir)
	w = callHandler(ServeTaskByID, req)
	assertOK(t, w)

	// resume（任务开关是开的）
	model.TasksEnabled = true
	req = withProjectCookie(newRequest(t, http.MethodPut, path, map[string]any{"action": "resume"}), env.ProjectDir)
	w = callHandler(ServeTaskByID, req)
	assertOK(t, w)
}
