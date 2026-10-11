package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// agentIDsFromList decodes the agent list response and returns the set of ids.
func agentIDsFromList(t *testing.T, body []byte) map[string]bool {
	t.Helper()
	var resp struct {
		Agents []struct {
			ID        string `json:"id"`
			ProjectID *int64 `json:"projectId"`
		} `json:"agents"`
		DefaultAgent string `json:"defaultAgent"`
	}
	require.NoError(t, json.Unmarshal(body, &resp))
	ids := map[string]bool{}
	for _, a := range resp.Agents {
		ids[a.ID] = true
	}
	return ids
}

// TestServeAgentsGet_FiltersByProjectScope verifies that GET /api/agents hides a
// project-scoped agent from other projects while keeping global agents visible
// everywhere, and shows the scoped agent in its own project.
func TestServeAgentsGet_FiltersByProjectScope(t *testing.T) {
	defer setupAgentTestEnv(t)()

	projA := "/tmp/agent-scope-a"
	projB := "/tmp/agent-scope-b"
	idA, err := store.ProjectIDForPath(projA)
	require.NoError(t, err)
	_, err = store.ProjectIDForPath(projB)
	require.NoError(t, err)

	// Scope claude to project A.
	scoped := model.Agents["claude"]
	scoped.ProjectID = &idA
	model.Agents["claude"] = scoped
	for i, a := range model.AgentList {
		if a.ID == "claude" {
			model.AgentList[i] = scoped
		}
	}

	// In project A: both visible.
	reqA := newRequest(t, http.MethodGet, "/api/agents", nil)
	reqA = withAuthCookie(reqA, model.SessionToken)
	reqA = withProjectCookie(reqA, projA)
	wA := callHandler(ServeAgents, reqA)
	require.Equal(t, http.StatusOK, wA.Code)
	idsA := agentIDsFromList(t, wA.Body.Bytes())
	assert.True(t, idsA["claude"], "scoped agent must be visible in its own project")
	assert.True(t, idsA["codebuddy"], "global agent must be visible in every project")

	// In project B: scoped agent hidden, global still visible.
	reqB := newRequest(t, http.MethodGet, "/api/agents", nil)
	reqB = withAuthCookie(reqB, model.SessionToken)
	reqB = withProjectCookie(reqB, projB)
	wB := callHandler(ServeAgents, reqB)
	require.Equal(t, http.StatusOK, wB.Code)
	idsB := agentIDsFromList(t, wB.Body.Bytes())
	assert.False(t, idsB["claude"], "scoped agent must be hidden from another project")
	assert.True(t, idsB["codebuddy"], "global agent must still be visible")

	// No project cookie: only globals.
	reqNone := newRequest(t, http.MethodGet, "/api/agents", nil)
	reqNone = withAuthCookie(reqNone, model.SessionToken)
	wNone := callHandler(ServeAgents, reqNone)
	require.Equal(t, http.StatusOK, wNone.Code)
	idsNone := agentIDsFromList(t, wNone.Body.Bytes())
	assert.False(t, idsNone["claude"], "scoped agent must be hidden without a project")
	assert.True(t, idsNone["codebuddy"])
}

// TestAgentVisibleInProject covers the shared predicate directly.
func TestAgentVisibleInProject(t *testing.T) {
	defer setupAgentTestEnv(t)()

	proj := "/tmp/agent-visible-proj"
	pid, err := store.ProjectIDForPath(proj)
	require.NoError(t, err)

	global := &model.Agent{ID: "g"}
	assert.True(t, agentVisibleInProject(global, proj), "global agent visible anywhere")
	assert.True(t, agentVisibleInProject(global, ""), "global agent visible with no project")

	scoped := &model.Agent{ID: "s", ProjectID: &pid}
	assert.True(t, agentVisibleInProject(scoped, proj), "scoped agent visible in its project")
	assert.False(t, agentVisibleInProject(scoped, "/tmp/agent-visible-other"), "scoped agent hidden elsewhere")
	assert.False(t, agentVisibleInProject(scoped, ""), "scoped agent hidden with no project")
	assert.False(t, agentVisibleInProject(nil, proj), "nil agent is not visible")
}

// TestServeAgentsPatch_ProjectScopeRefusedForDefault verifies the default agent
// cannot be scoped to a single project.
func TestServeAgentsPatch_ProjectScopeRefusedForDefault(t *testing.T) {
	defer setupAgentTestEnv(t)()

	origDefault := model.GetDefaultAgentID()
	model.SetDefaultAgentID("codebuddy")
	defer model.SetDefaultAgentID(origDefault)

	body := map[string]any{"id": "codebuddy", "project_scope": true}
	req := newRequest(t, http.MethodPatch, "/api/agents", body)
	req = withAuthCookie(req, model.SessionToken)
	req = withProjectCookie(req, "/tmp/agent-scope-default")
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "CannotScopeDefaultAgent", resp["msgKey"])
}

// TestServeAgentsPatch_ProjectScopeRequiresProject verifies that scoping an
// agent without a project cookie is rejected (there is no project to bind to).
func TestServeAgentsPatch_ProjectScopeRequiresProject(t *testing.T) {
	defer setupAgentTestEnv(t)()

	body := map[string]any{"id": "claude", "project_scope": true}
	req := newRequest(t, http.MethodPatch, "/api/agents", body)
	req = withAuthCookie(req, model.SessionToken)
	// Deliberately no project cookie.
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "NoProjectSelected", resp["msgKey"])
}

// TestServeAgentsPatch_ProjectScopeSetsAndClears verifies the round trip: a
// scoped agent gets a project id, and clearing it removes the scope.
func TestServeAgentsPatch_ProjectScopeSetsAndClears(t *testing.T) {
	defer setupAgentTestEnv(t)()

	proj := "/tmp/agent-scope-roundtrip"
	pid, err := store.ProjectIDForPath(proj)
	require.NoError(t, err)

	// Scope it.
	req := newRequest(t, http.MethodPatch, "/api/agents", map[string]any{"id": "claude", "project_scope": true})
	req = withAuthCookie(req, model.SessionToken)
	req = withProjectCookie(req, proj)
	w := callHandler(ServeAgents, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, model.GetAgent("claude").ProjectID)
	assert.Equal(t, pid, *model.GetAgent("claude").ProjectID)

	// Clear it.
	req2 := newRequest(t, http.MethodPatch, "/api/agents", map[string]any{"id": "claude", "project_scope": false})
	req2 = withAuthCookie(req2, model.SessionToken)
	req2 = withProjectCookie(req2, proj)
	w2 := callHandler(ServeAgents, req2)
	require.Equal(t, http.StatusOK, w2.Code)
	assert.Nil(t, model.GetAgent("claude").ProjectID, "clearing project_scope must reset to global")
}

// TestServeSessions_Post_ScopedAgentFromOtherProjectRejected verifies the
// server-side scope gate on session creation: a project-only agent from another
// project must not be usable for a NEW session (mirrors the disabled-agent
// fallback).
func TestServeSessions_Post_ScopedAgentFromOtherProjectRejected(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	otherProj := "/tmp/agent-scope-other"
	otherID, err := store.ProjectIDForPath(otherProj)
	require.NoError(t, err)

	origAgents := model.Agents
	origList := model.AgentList
	model.Agents = map[string]*model.Agent{
		"scoped": {ID: "scoped", Name: "Scoped", Backend: "claude", ProjectID: &otherID},
	}
	model.AgentList = []*model.Agent{model.Agents["scoped"]}
	defer func() { model.Agents = origAgents; model.AgentList = origList }()

	// Create in env.ProjectDir (a DIFFERENT project than the agent's scope).
	req := newRequest(t, http.MethodPost, "/api/ai/sessions", map[string]any{"agentId": "scoped"})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertStatus(t, w, http.StatusBadRequest)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "AgentNotInProject", resp["msgKey"])
}

// TestServeSessions_Post_ScopedAgentInOwnProjectAllowed verifies the positive
// case: the same agent is accepted in the project it is scoped to.
func TestServeSessions_Post_ScopedAgentInOwnProjectAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	ownID, err := store.ProjectIDForPath(env.ProjectDir)
	require.NoError(t, err)

	origAgents := model.Agents
	origList := model.AgentList
	model.Agents = map[string]*model.Agent{
		"scoped": {ID: "scoped", Name: "Scoped", Backend: "claude", ProjectID: &ownID},
	}
	model.AgentList = []*model.Agent{model.Agents["scoped"]}
	defer func() { model.Agents = origAgents; model.AgentList = origList }()

	req := newRequest(t, http.MethodPost, "/api/ai/sessions", map[string]any{"agentId": "scoped"})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)
}
