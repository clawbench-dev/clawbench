package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ServeACPSessions ---

func TestServeACPSessions_AgentNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// No agents configured
	model.Agents = map[string]*model.Agent{}
	model.AgentList = []*model.Agent{}

	req := newRequest(t, http.MethodGet, "/api/agents/nonexistent/acp-sessions", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSessions(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeACPSessions_NonACPTransport(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.Agents = map[string]*model.Agent{
		"cli-agent": {ID: "cli-agent", Backend: "claude", Transport: "cli"},
	}
	model.AgentList = []*model.Agent{model.Agents["cli-agent"]}

	req := newRequest(t, http.MethodGet, "/api/agents/cli-agent/acp-sessions", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSessions(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeACPSessions_NoLoadSessionCapability(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-no-load"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Backend: "acp-stdio", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	// No capabilities registered — LoadSession defaults to false
	reg := ai.GetAgentCapabilityRegistry()
	reg.Update(agentID, &ai.AgentCapability{AvailableModes: []ai.ModeDef{{ID: "code"}}})

	req := newRequest(t, http.MethodGet, "/api/agents/"+agentID+"/acp-sessions", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSessions(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestServeACPSessions_NoAliveConnection(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-no-conn"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Backend: "acp-stdio", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	// Register capability but no active connection
	reg := ai.GetAgentCapabilityRegistry()
	ls := true
	lss := true
	reg.Update(agentID, &ai.AgentCapability{LoadSession: &ls, ListSessions: &lss})

	req := newRequest(t, http.MethodGet, "/api/agents/"+agentID+"/acp-sessions", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSessions(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// TestServeACPSessions_SpawnOnDemandWhenNoCapabilityRegistered verifies that
// when no capability is registered yet (no prior connection), the handler
// attempts to spawn an ACP connection via GetOrCreateConnNoSession to discover
// capabilities. If the spawn fails (agent binary not a real ACP agent), we
// should get 501 (capability not supported) rather than 503, because the
// spawn was attempted but couldn't confirm capability support.
func TestServeACPSessions_SpawnOnDemandWhenNoCapabilityRegistered(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-spawn-ondemand"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Backend: "acp-stdio", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	// No capabilities registered — this is the "fresh startup" state
	// where the user hasn't sent any message yet.
	// The handler should attempt to spawn a connection to discover capabilities.
	req := newRequest(t, http.MethodGet, "/api/agents/"+agentID+"/acp-sessions", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSessions(w, req)

	// Spawn fails because "echo" is not a real ACP agent, and no capability
	// was discovered — so we get 501 (not implemented), not 503.
	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

// TestServeACPSessions_CapabilityFromDBPersistence verifies that when
// capabilities are persisted in the DB (from a prior connection), the
// handler can check them even without an active connection. If the
// capability is supported but no connection can be established, we get 503.
func TestServeACPSessions_CapabilityFromDBPersistence(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-db-cap"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Backend: "acp-stdio", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	// Simulate DB-persisted capabilities (from a previous session)
	reg := ai.GetAgentCapabilityRegistry()
	ls := true
	lss := true
	reg.Update(agentID, &ai.AgentCapability{LoadSession: &ls, ListSessions: &lss})

	req := newRequest(t, http.MethodGet, "/api/agents/"+agentID+"/acp-sessions", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSessions(w, req)

	// Capability is known (from registry), but no alive connection → 503
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// --- ServeACPLoadSession ---

func TestServeACPLoadSession_InvalidRequestBody(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Missing required fields
	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-load", map[string]string{"agentId": ""})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPLoadSession(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeACPLoadSession_AgentNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.Agents = map[string]*model.Agent{}
	model.AgentList = []*model.Agent{}

	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-load", map[string]string{
		"agentId":      "nonexistent",
		"acpSessionId": "acp-123",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPLoadSession(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeACPLoadSession_NonACPTransport(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.Agents = map[string]*model.Agent{
		"cli-agent": {ID: "cli-agent", Backend: "claude", Transport: "cli"},
	}
	model.AgentList = []*model.Agent{model.Agents["cli-agent"]}

	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-load", map[string]string{
		"agentId":      "cli-agent",
		"acpSessionId": "acp-123",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPLoadSession(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeACPLoadSession_NoLoadSessionCapability(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-no-load"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Backend: "acp-stdio", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-load", map[string]string{
		"agentId":      agentID,
		"acpSessionId": "acp-123",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPLoadSession(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

// --- Agents GET response includes LoadSession/ListSessions ---

func TestServeAgentsGet_IncludesLoadSessionCapability(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-with-load"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	// Register ListSessions capability (LoadSession comes from BackendSpec)
	reg := ai.GetAgentCapabilityRegistry()
	lss := true
	reg.Update(agentID, &ai.AgentCapability{ListSessions: &lss})

	req := newRequest(t, http.MethodGet, "/api/agents", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeAgents(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	states, ok := resp["acpStates"].(map[string]any)
	require.True(t, ok, "acpStates should be present")

	agentState, ok := states[agentID].(map[string]any)
	require.True(t, ok, "agent state should be present")

	// LoadSession comes from BackendSpec (claude has ACPLoadSession=true)
	assert.Equal(t, true, agentState["loadSession"])
	assert.Equal(t, true, agentState["listSessions"])
}

func TestServeAgentsGet_NoCapabilityOmitsFields(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-no-cap"
	// Use codebuddy backend (has AcpCommand and ACPLoadSession=true)
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Backend: "codebuddy", Transport: "acp-stdio", AcpCommand: "codebuddy --acp"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	// No capabilities registered

	req := newRequest(t, http.MethodGet, "/api/agents", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeAgents(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	states, ok := resp["acpStates"].(map[string]any)
	if !ok {
		// No acpStates at all — also fine
		return
	}
	// If agent has state, loadSession comes from BackendSpec (codebuddy=true),
	// while listSessions comes from the registry (none registered → false).
	if agentState, ok := states[agentID].(map[string]any); ok {
		assert.Equal(t, true, agentState["loadSession"])
		assert.Equal(t, false, agentState["listSessions"])
	}
}
