package service_test

import (
	"encoding/json"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSaveAgent_PersistsToolPolicy pins the write path that is easiest to get
// wrong: a column added to the DDL but omitted from the INSERT is silently
// dropped, and the user only finds out after a restart.
func TestSaveAgent_PersistsToolPolicy(t *testing.T) {
	db := setupTestDBForAgents(t)

	agent := &model.Agent{
		ID:      "builtin",
		Name:    "ClawBench Agent",
		Backend: "builtin",
		ToolPolicy: model.ToolPolicy{
			Allow:          []string{model.CapFSRead},
			MaxRounds:      9,
			MaxToolCalls:   11,
			BashTimeoutSec: 33,
		},
	}
	require.NoError(t, service.SaveAgent(db, agent))

	var raw string
	require.NoError(t, db.QueryRow("SELECT tool_policy FROM agents WHERE id = ?", "builtin").Scan(&raw))

	got := model.DecodeToolPolicy(raw)
	assert.Equal(t, []string{model.CapFSRead}, got.Allow)
	assert.Equal(t, 9, got.MaxRounds)
	assert.Equal(t, 11, got.MaxToolCalls)
	assert.Equal(t, 33, got.BashTimeoutSec)
}

// TestSaveAgent_UpdatesToolPolicyOnConflict pins the ON CONFLICT branch.
//
// This is the exact bug mcp_servers shipped with: the INSERT carried the column
// but the upsert's SET list did not, so every re-save silently reverted the
// policy to whatever the row already had.
func TestSaveAgent_UpdatesToolPolicyOnConflict(t *testing.T) {
	db := setupTestDBForAgents(t)

	agent := &model.Agent{
		ID: "builtin", Name: "ClawBench Agent", Backend: "builtin",
		ToolPolicy: model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}
	require.NoError(t, service.SaveAgent(db, agent))

	agent.ToolPolicy = model.ToolPolicy{Allow: []string{model.CapExec}}
	require.NoError(t, service.SaveAgent(db, agent))

	var raw string
	require.NoError(t, db.QueryRow("SELECT tool_policy FROM agents WHERE id = ?", "builtin").Scan(&raw))
	got := model.DecodeToolPolicy(raw)
	assert.Equal(t, []string{model.CapFSRead, model.CapFSWrite, model.CapExec}, got.Allow,
		"a re-save must replace the policy, and exec must be stored closed over its implications")
}

// TestSaveAgent_PersistsMCPPolicyOnConflict pins the pre-existing defect this
// change also fixes: SaveAgent never wrote mcp_servers at all.
func TestSaveAgent_PersistsMCPPolicyOnConflict(t *testing.T) {
	db := setupTestDBForAgents(t)

	agent := &model.Agent{
		ID: "a1", Name: "A1", Backend: "claude",
		MCPServers: model.MCPServerPolicy{Mode: model.MCPPolicySelected, Names: []string{"x"}},
	}
	require.NoError(t, service.SaveAgent(db, agent))

	agent.MCPServers = model.MCPServerPolicy{Mode: model.MCPPolicyNone}
	require.NoError(t, service.SaveAgent(db, agent))

	var raw string
	require.NoError(t, db.QueryRow("SELECT mcp_servers FROM agents WHERE id = ?", "a1").Scan(&raw))
	var policy model.MCPServerPolicy
	require.NoError(t, json.Unmarshal([]byte(raw), &policy))
	assert.Equal(t, model.MCPPolicyNone, policy.Normalized().Mode)
}

// TestLoadAgentsFromDB_ReadsToolPolicy pins the read path.
func TestLoadAgentsFromDB_ReadsToolPolicy(t *testing.T) {
	db := setupTestDBForAgents(t)

	agent := &model.Agent{
		ID: "builtin", Name: "ClawBench Agent", Backend: "builtin",
		ToolPolicy: model.ToolPolicy{Allow: []string{model.CapFSWrite}, MaxRounds: 5},
	}
	require.NoError(t, service.SaveAgent(db, agent))

	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)

	got := agents[0].ToolPolicy
	assert.Equal(t, []string{model.CapFSRead, model.CapFSWrite}, got.Allow)
	assert.Equal(t, 5, got.MaxRounds)
}

// TestLoadAgentsFromDB_UnreadableToolPolicyGrantsNothing pins the fail-closed
// read: a corrupt column must never be read as "all tools".
func TestLoadAgentsFromDB_UnreadableToolPolicyGrantsNothing(t *testing.T) {
	db := setupTestDBForAgents(t)

	_, err := db.Exec(`INSERT INTO agents (id, name, backend, tool_policy)
		VALUES ('broken', 'Broken', 'builtin', 'not json')`)
	require.NoError(t, err)

	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Empty(t, agents[0].ToolPolicy.Allow,
		"a corrupt policy must degrade to no grants, never to full access")
}

// TestPatchAgentFields_UpdatesToolPolicy pins the settings-page path.
func TestPatchAgentFields_UpdatesToolPolicy(t *testing.T) {
	db := setupTestDBForAgents(t)

	agent := &model.Agent{ID: "builtin", Name: "ClawBench Agent", Backend: "builtin"}
	require.NoError(t, service.SaveAgent(db, agent))

	policy := model.ToolPolicy{Allow: []string{model.CapFSRead}}
	require.NoError(t, service.PatchAgentFields("builtin", service.AgentPatch{ToolPolicy: &policy}))

	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, []string{model.CapFSRead}, agents[0].ToolPolicy.Allow)
}

// TestPatchAgentFields_RejectsInvalidBudget pins that a bad budget is refused
// before any column is written, so a half-applied patch cannot happen.
func TestPatchAgentFields_RejectsInvalidBudget(t *testing.T) {
	db := setupTestDBForAgents(t)

	agent := &model.Agent{ID: "builtin", Name: "ClawBench Agent", Backend: "builtin"}
	require.NoError(t, service.SaveAgent(db, agent))

	bad := model.ToolPolicy{MaxRounds: -1}
	err := service.PatchAgentFields("builtin", service.AgentPatch{ToolPolicy: &bad})
	require.Error(t, err)

	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	assert.Empty(t, agents[0].ToolPolicy.Allow)
}
