package tasks

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAgentVisibleInProject pins the visibility rule the task panel shares with
// the chat side: global agents everywhere, project agents only in their own
// project, unknown project = not visible.
func TestAgentVisibleInProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	t.Cleanup(teardown)

	assert.False(t, agentVisibleInProject(nil, env.ProjectDir), "no agent is never visible")

	assert.True(t, agentVisibleInProject(&model.Agent{}, env.ProjectDir),
		"a global agent (ProjectID == nil) is visible in every project")

	pid := store.ProjectIDForTest(t, env.ProjectDir)
	scoped := &model.Agent{ProjectID: &pid}
	assert.True(t, agentVisibleInProject(scoped, env.ProjectDir))

	other := pid + 1
	assert.False(t, agentVisibleInProject(&model.Agent{ProjectID: &other}, env.ProjectDir),
		"a project-scoped agent must not leak into another project")

	assert.False(t, agentVisibleInProject(scoped, env.ProjectDir+"/does-not-exist"),
		"an unregistered project path resolves to nothing")
}

// TestRejectIfTasksDisabled pins the 409 the handlers answer when the master
// switch is off (the prefix cannot express this rule).
func TestRejectIfTasksDisabled(t *testing.T) {
	orig := model.TasksEnabled
	t.Cleanup(func() { model.TasksEnabled = orig })

	model.TasksEnabled = true
	w := httptest.NewRecorder()
	assert.False(t, rejectIfTasksDisabled(w, httptest.NewRequest(http.MethodGet, "/api/tasks", http.NoBody)))

	model.TasksEnabled = false
	w = httptest.NewRecorder()
	assert.True(t, rejectIfTasksDisabled(w, httptest.NewRequest(http.MethodGet, "/api/tasks", http.NoBody)))
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "TasksDisabled")
}

// TestSetSessionID pins the cookie the task handlers point the browser at.
func TestSetSessionID(t *testing.T) {
	w := httptest.NewRecorder()
	setSessionID(w, httptest.NewRequest(http.MethodGet, "/", http.NoBody), "sess-1")

	res := w.Result()
	require.Len(t, res.Cookies(), 1)
	c := res.Cookies()[0]
	assert.Equal(t, model.ScopedCookieName("chat_session_id"), c.Name)
	assert.Equal(t, "sess-1", c.Value)
	assert.True(t, c.HttpOnly)
}
