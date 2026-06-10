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

	// Add an ACP agent with no registry data
	acpAgent := &model.Agent{
		ID:        "acp-test",
		Name:      "ACP Test",
		Backend:   "acp-test",
		Transport: "acp-stdio",
		Models:    []model.AgentModel{{ID: "m1", Name: "M1", Default: true}},
	}
	model.Agents["acp-test"] = acpAgent
	model.AgentList = append(model.AgentList, acpAgent)

	// Ensure no registry data for this agent
	ai.GetAgentCapabilityRegistry().Update("acp-test", &ai.AgentCapability{})

	req := newRequest(t, http.MethodGet, "/api/ai/commands?agent_id=acp-test", nil)
	w := callHandler(ServeAICommands, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	commands, ok := resp["commands"].([]any)
	require.True(t, ok, "response should contain commands array")
	assert.Empty(t, commands)
}

func TestServeAICommands_ACPAgentWithCommands(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Add an ACP agent
	acpAgent := &model.Agent{
		ID:        "acp-cmds",
		Name:      "ACP Commands",
		Backend:   "acp-test",
		Transport: "acp-stdio",
		Models:    []model.AgentModel{{ID: "m1", Name: "M1", Default: true}},
	}
	model.Agents["acp-cmds"] = acpAgent
	model.AgentList = append(model.AgentList, acpAgent)

	// Populate commands in the registry
	ai.GetAgentCapabilityRegistry().UpdateCommands("acp-cmds", []ai.AvailableCommandInfo{
		{Name: "/compact", Description: "Compact history"},
		{Name: "/ask", Description: "Ask a question", InputHint: "your question"},
	})

	req := newRequest(t, http.MethodGet, "/api/ai/commands?agent_id=acp-cmds", nil)
	w := callHandler(ServeAICommands, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	commands, ok := resp["commands"].([]any)
	require.True(t, ok, "response should contain commands array")
	require.Len(t, commands, 2)

	// Verify first command
	cmd0 := commands[0].(map[string]any)
	assert.Equal(t, "/compact", cmd0["name"])
	assert.Equal(t, "Compact history", cmd0["description"])

	// Verify second command with input hint
	cmd1 := commands[1].(map[string]any)
	assert.Equal(t, "/ask", cmd1["name"])
	assert.Equal(t, "Ask a question", cmd1["description"])
	assert.Equal(t, "your question", cmd1["inputHint"])
}

func TestServeAICommands_ACPAgentWithEmptyCommands(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Add an ACP agent
	acpAgent := &model.Agent{
		ID:        "acp-no-cmds",
		Name:      "ACP No Cmds",
		Backend:   "acp-test",
		Transport: "acp-stdio",
		Models:    []model.AgentModel{{ID: "m1", Name: "M1", Default: true}},
	}
	model.Agents["acp-no-cmds"] = acpAgent
	model.AgentList = append(model.AgentList, acpAgent)

	// No commands in registry for this agent

	req := newRequest(t, http.MethodGet, "/api/ai/commands?agent_id=acp-no-cmds", nil)
	w := callHandler(ServeAICommands, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	commands, ok := resp["commands"].([]any)
	require.True(t, ok, "response should contain commands array")
	assert.Empty(t, commands)
}
