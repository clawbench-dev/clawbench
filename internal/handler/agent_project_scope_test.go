package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"
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

	// Register env.ProjectDir too, so the rejection below is provably because
	// the agent belongs to a DIFFERENT (resolvable) project — not merely because
	// the request's project is unknown. Without this, ProjectIDByPath returns
	// ok=false and the gate would fail for the wrong reason.
	_, err = store.ProjectIDForPath(env.ProjectDir)
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

// TestServeSessions_Post_DefaultFallbackRespectsScope verifies the fix for the
// default-agent fallback hole: when no agent is specified, the server must not
// pick a default that is scoped to a DIFFERENT project (model.GetDefaultAgentID
// falls back to AgentList[0], which may be scoped elsewhere).
func TestServeSessions_Post_DefaultFallbackRespectsScope(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	otherID, err := store.ProjectIDForPath("/tmp/agent-scope-default-other")
	require.NoError(t, err)
	_, err = store.ProjectIDForPath(env.ProjectDir)
	require.NoError(t, err)

	origAgents := model.Agents
	origList := model.AgentList
	origDefault := model.GetDefaultAgentID()
	// AgentList[0] (and the configured default) is scoped to ANOTHER project;
	// a second agent is global and must be chosen instead.
	scoped := &model.Agent{ID: "scoped-default", Name: "ScopedDefault", Backend: "claude", ProjectID: &otherID}
	global := &model.Agent{ID: "global-agent", Name: "Global", Backend: "claude"}
	model.Agents = map[string]*model.Agent{"scoped-default": scoped, "global-agent": global}
	model.AgentList = []*model.Agent{scoped, global}
	model.SetDefaultAgentID("scoped-default")
	defer func() {
		model.Agents = origAgents
		model.AgentList = origList
		model.SetDefaultAgentID(origDefault)
	}()

	// No agentId: the server resolves the default itself.
	req := newRequest(t, http.MethodPost, "/api/ai/sessions", map[string]any{})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "global-agent", resp["agentId"],
		"the scope-aware default fallback must skip an agent scoped to another project")
}

// TestServeAgentsRescan_FiltersByProjectScope verifies POST /api/agents/rescan
// returns the same project-filtered list as GET (it previously leaked every
// scoped agent).
func TestServeAgentsRescan_FiltersByProjectScope(t *testing.T) {
	defer setupAgentTestEnv(t)()

	projA := "/tmp/agent-rescan-a"
	projB := "/tmp/agent-rescan-b"
	idA, err := store.ProjectIDForPath(projA)
	require.NoError(t, err)
	_, err = store.ProjectIDForPath(projB)
	require.NoError(t, err)

	scoped := model.Agents["claude"]
	scoped.ProjectID = &idA
	model.Agents["claude"] = scoped
	for i, a := range model.AgentList {
		if a.ID == "claude" {
			model.AgentList[i] = scoped
		}
	}
	// The scope must ALSO be in the DB: rescan reloads the roster from disk, so
	// an in-memory-only edit is wiped before the response is built.
	_, err = store.WriteExec("UPDATE agents SET project_id = ? WHERE id = 'claude'", idA)
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/agents/rescan", nil)
	req = withAuthCookie(req, model.SessionToken)
	req = withProjectCookie(req, projB)
	w := callHandler(ServeAgentSubRoutes, req)
	require.Equal(t, http.StatusOK, w.Code)
	ids := agentIDsFromList(t, w.Body.Bytes())
	assert.False(t, ids["claude"], "rescan must not leak an agent scoped to another project")
	assert.True(t, ids["codebuddy"], "global agents must still appear in rescan")
}

// scopeAgentToOtherProject swaps the in-memory roster to a single agent scoped
// to a project other than env.ProjectDir, returning a restore func.
func scopeAgentToOtherProject(t *testing.T, env *testEnv, agentID string) func() {
	t.Helper()
	otherID, err := store.ProjectIDForPath("/tmp/agent-scope-gate-other")
	require.NoError(t, err)
	// Register the request's project too, so a rejection is provably about the
	// scope, not an unresolvable project.
	_, err = store.ProjectIDForPath(env.ProjectDir)
	require.NoError(t, err)

	origAgents := model.Agents
	origList := model.AgentList
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: agentID, Backend: "claude", ProjectID: &otherID},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}
	return func() { model.Agents = origAgents; model.AgentList = origList }
}

// TestServeForkSession_ScopedAgentFromOtherProjectRejected verifies the fork
// gate: a fork is a NEW session, so a project-only agent from another project
// must be refused.
func TestServeForkSession_ScopedAgentFromOtherProjectRejected(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	defer scopeAgentToOtherProject(t, env, "scoped-fork")()

	sessID, err := service.CreateSession(env.ProjectDir, "claude", "Original", "claude", "", "default", "chat")
	require.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessID, "user", "Hello", nil, false, "")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/fork", map[string]any{"sessionId": sessID, "agentId": "scoped-fork"})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeForkSession, req)

	assertStatus(t, w, http.StatusBadRequest)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "AgentNotInProject", resp["msgKey"])
}

// TestServeTasks_PostScopedAgentFromOtherProjectRejected verifies the task
// create gate for a project-only agent from another project.
func TestServeTasks_PostScopedAgentFromOtherProjectRejected(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	defer scopeAgentToOtherProject(t, env, "scoped-task")()

	s := service.NewScheduler()
	defer s.Stop()
	service.GlobalScheduler = s
	defer func() { service.GlobalScheduler = nil }()

	req := newRequest(t, http.MethodPost, "/api/tasks", map[string]any{
		"name":      "Scoped Task",
		"cron_expr": "0 * * * *",
		"agent_id":  "scoped-task",
		"prompt":    "Do something",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTasks, req)

	assertStatus(t, w, http.StatusBadRequest)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "AgentNotInProject", resp["msgKey"])
}

// TestServeGroupCreate_ScopedAgentFromOtherProjectRejected verifies the group
// create gate: a project-only agent from another project must not be seeded
// into a NEW group.
func TestServeGroupCreate_ScopedAgentFromOtherProjectRejected(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	defer scopeAgentToOtherProject(t, env, "scoped-group")()

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{
		"hostAgentId":    "scoped-group",
		"memberAgentIds": []string{"scoped-group"},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeGroupCreate, req)

	assertStatus(t, w, http.StatusBadRequest)
}

// TestGroupMemberSpecs_ScopeGate pins the group-membership predicate directly:
// an out-of-project agent yields ok=false, a global one yields ok=true. This is
// stronger than asserting only "400" on the endpoint, which other failure modes
// (e.g. missing host) also produce.
func TestGroupMemberSpecs_ScopeGate(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	otherID, err := store.ProjectIDForPath("/tmp/agent-scope-specs-other")
	require.NoError(t, err)
	_, err = store.ProjectIDForPath(env.ProjectDir)
	require.NoError(t, err)

	origAgents := model.Agents
	origList := model.AgentList
	model.Agents = map[string]*model.Agent{
		"scoped": {ID: "scoped", Name: "Scoped", Backend: "claude", ProjectID: &otherID},
		"global": {ID: "global", Name: "Global", Backend: "claude"},
	}
	model.AgentList = []*model.Agent{model.Agents["scoped"], model.Agents["global"]}
	defer func() { model.Agents = origAgents; model.AgentList = origList }()

	_, ok := groupMemberSpecs([]string{"scoped"}, env.ProjectDir)
	assert.False(t, ok, "an agent scoped to another project must not be a valid group member here")

	specs, ok := groupMemberSpecs([]string{"global"}, env.ProjectDir)
	assert.True(t, ok, "a global agent is a valid group member")
	require.Len(t, specs, 1)
	assert.Equal(t, "global", specs[0].AgentID)
}

// TestServeACPLoadSession_ScopedAgentFromOtherProjectRejected verifies acp-load
// (which creates a NEW session) enforces the scope gate.
func TestServeACPLoadSession_ScopedAgentFromOtherProjectRejected(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	otherID, err := store.ProjectIDForPath("/tmp/agent-scope-acpload-other")
	require.NoError(t, err)
	_, err = store.ProjectIDForPath(env.ProjectDir)
	require.NoError(t, err)

	origAgents := model.Agents
	origList := model.AgentList
	model.Agents = map[string]*model.Agent{
		"scoped-acp": {ID: "scoped-acp", Name: "ScopedACP", Backend: "claude", AcpCommand: "claude-acp", ProjectID: &otherID},
	}
	model.AgentList = []*model.Agent{model.Agents["scoped-acp"]}
	defer func() { model.Agents = origAgents; model.AgentList = origList }()

	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-load", map[string]any{
		"agentId":      "scoped-acp",
		"acpSessionId": "acp-123",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeACPLoadSession, req)

	assertStatus(t, w, http.StatusBadRequest)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "AgentNotInProject", resp["msgKey"])
}

// TestServeAgentsPatch_MCPServersRoundTrip verifies the MCP policy round trip:
// the payload is the policy object itself, and the change is visible through
// model.GetAgent immediately (no connection teardown involved — ACPConn re-reads
// the policy each turn).
func TestServeAgentsPatch_MCPServersRoundTrip(t *testing.T) {
	defer setupAgentTestEnv(t)()

	body := map[string]any{
		"id": "claude",
		"mcp_servers": map[string]any{
			"mode":  "selected",
			"names": []any{"websearch", "browser"},
		},
	}
	req := newRequest(t, http.MethodPatch, "/api/agents", body)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)
	require.Equal(t, http.StatusOK, w.Code)

	got := model.GetAgent("claude").MCPServers
	assert.Equal(t, model.MCPPolicySelected, got.Mode)
	assert.Equal(t, []string{"websearch", "browser"}, got.Names)
}

// TestServeAgentsPatch_MCPServersOmittedModeDefaultsToAll pins the zero-value
// semantics: an omitted mode must mean "all", never "none". Reading it as
// "none" would silently strip MCP from the agent.
func TestServeAgentsPatch_MCPServersOmittedModeDefaultsToAll(t *testing.T) {
	defer setupAgentTestEnv(t)()

	req := newRequest(t, http.MethodPatch, "/api/agents", map[string]any{
		"id":          "claude",
		"mcp_servers": map[string]any{},
	})
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)
	require.Equal(t, http.StatusOK, w.Code)

	assert.Equal(t, model.MCPPolicyAll, model.GetAgent("claude").MCPServers.Normalized().Mode)
}

// TestServeAgentsPatch_MCPServersRejectsNonObject pins that a malformed payload
// is a 400 rather than being silently ignored.
func TestServeAgentsPatch_MCPServersRejectsNonObject(t *testing.T) {
	defer setupAgentTestEnv(t)()

	req := newRequest(t, http.MethodPatch, "/api/agents", map[string]any{
		"id":          "claude",
		"mcp_servers": "all",
	})
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
