package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServeAICommands_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/commands", nil)
	w := callHandler(ServeAICommands, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeAICommands_CLIAgentReturnsEmpty(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// codebuddy is a CLI agent (Transport != "acp-stdio")
	req := newRequest(t, http.MethodGet, "/api/ai/commands?agent_id=codebuddy", nil)
	w := callHandler(ServeAICommands, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	commands, ok := resp["commands"].([]any)
	require.True(t, ok, "response should contain commands array")
	assert.Empty(t, commands)
}

func TestServeAICommands_UnknownAgentReturnsEmpty(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/commands?agent_id=nonexistent", nil)
	w := callHandler(ServeAICommands, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	commands, ok := resp["commands"].([]any)
	require.True(t, ok, "response should contain commands array")
	assert.Empty(t, commands)
}

func TestServeAICommands_NoAgentIDUsesDefault(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Default agent is codebuddy (first in AgentList), which is a CLI agent
	req := newRequest(t, http.MethodGet, "/api/ai/commands", nil)
	w := callHandler(ServeAICommands, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	commands, ok := resp["commands"].([]any)
	require.True(t, ok, "response should contain commands array")
	assert.Empty(t, commands)
}

func TestServeAICommands_NoAgentIDNoDefaultReturnsEmpty(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Clear all agents so GetDefaultAgentID returns ""
	origAgents := model.Agents
	origAgentList := model.AgentList
	model.Agents = map[string]*model.Agent{}
	model.AgentList = nil
	defer func() {
		model.Agents = origAgents
		model.AgentList = origAgentList
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/commands", nil)
	w := callHandler(ServeAICommands, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	commands, ok := resp["commands"].([]any)
	require.True(t, ok, "response should contain commands array")
	assert.Empty(t, commands)
}

func TestServeAICommands_ACPAgentNoPoolClientReturnsEmpty(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Add an ACP agent with no active pool connection
	acpAgent := &model.Agent{
		ID:        "acp-test",
		Name:      "ACP Test",
		Backend:   "acp-test",
		Transport: "acp-stdio",
		Models:    []model.AgentModel{{ID: "m1", Name: "M1", Default: true}},
	}
	model.Agents["acp-test"] = acpAgent
	model.AgentList = append(model.AgentList, acpAgent)

	// Ensure no pool entry exists for this agent
	pool := ai.GetACPConnectionPool()
	pool.CloseConnection("acp-test")

	req := newRequest(t, http.MethodGet, "/api/ai/commands?agent_id=acp-test", nil)
	w := callHandler(ServeAICommands, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	commands, ok := resp["commands"].([]any)
	require.True(t, ok, "response should contain commands array")
	assert.Empty(t, commands)
}
