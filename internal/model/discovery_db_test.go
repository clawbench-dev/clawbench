package model

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupTestDBForDiscovery creates an in-memory SQLite with the agents table.
func setupTestDBForDiscovery(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS agents (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			icon TEXT NOT NULL DEFAULT '',
			specialty TEXT NOT NULL DEFAULT '',
			backend TEXT NOT NULL,
			command TEXT NOT NULL DEFAULT '',
			thinking_effort TEXT NOT NULL DEFAULT '',
			thinking_effort_levels TEXT NOT NULL DEFAULT '[]',
			preferred_model TEXT NOT NULL DEFAULT '',
			preferred_thinking_effort TEXT NOT NULL DEFAULT '',
			system_prompt TEXT NOT NULL DEFAULT '',
			models TEXT NOT NULL DEFAULT '[]',
			models_auto_detected INTEGER NOT NULL DEFAULT 0,
			source TEXT NOT NULL DEFAULT 'auto',
			sort_order INTEGER NOT NULL DEFAULT 0,
			transport TEXT NOT NULL DEFAULT 'cli',
			acp_command TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_agents_backend ON agents(backend);
		CREATE INDEX IF NOT EXISTS idx_agents_source ON agents(source);
		CREATE INDEX IF NOT EXISTS idx_agents_sort ON agents(sort_order);
	`)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

// --- SyncDiscoverAgentsDB tests ---

func TestSyncDiscoverAgentsDB_DetectsPresentCLIs(t *testing.T) {
	db := setupTestDBForDiscovery(t)

	// Save/restore global state
	origAgents := Agents
	origList := AgentList
	t.Cleanup(func() {
		Agents = origAgents
		AgentList = origList
	})
	Agents = make(map[string]*Agent)
	AgentList = nil

	present := SyncDiscoverAgentsDB(db)

	// present should be a valid map
	assert.NotNil(t, present)

	// Any CLI that exists on this system should be in the present map
	for backend, exists := range present {
		assert.True(t, exists, "present map should only contain true values, but %s=%v", backend, exists)
	}
}

func TestSyncDiscoverAgentsDB_InsertsNewAgentsToDB(t *testing.T) {
	db := setupTestDBForDiscovery(t)

	// Save/restore global state
	origAgents := Agents
	origList := AgentList
	t.Cleanup(func() {
		Agents = origAgents
		AgentList = origList
	})
	Agents = make(map[string]*Agent)
	AgentList = nil

	present := SyncDiscoverAgentsDB(db)

	// For each present backend, there should be a DB record
	for backend := range present {
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM agents WHERE backend = ?", backend).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count, "expected 1 DB record for backend %s", backend)
	}
}

func TestSyncDiscoverAgentsDB_DoesNotOverwriteExistingRecords(t *testing.T) {
	db := setupTestDBForDiscovery(t)

	// Pre-insert an agent with custom data
	_, err := db.Exec(`INSERT INTO agents (id, name, backend, specialty, source, preferred_model, system_prompt)
		VALUES ('codebuddy', 'My Custom Name', 'codebuddy', 'Custom specialty', 'setup', 'my-model', 'custom prompt')`)
	require.NoError(t, err)

	// Save/restore global state
	origAgents := Agents
	origList := AgentList
	t.Cleanup(func() {
		Agents = origAgents
		AgentList = origList
	})
	Agents = make(map[string]*Agent)
	AgentList = nil

	SyncDiscoverAgentsDB(db)

	// The existing record should NOT be overwritten
	var name, specialty, prefModel, sysPrompt string
	err = db.QueryRow("SELECT name, specialty, preferred_model, system_prompt FROM agents WHERE id = 'codebuddy'").Scan(&name, &specialty, &prefModel, &sysPrompt)
	require.NoError(t, err)
	assert.Equal(t, "My Custom Name", name, "existing agent name should not be overwritten")
	assert.Equal(t, "Custom specialty", specialty)
	assert.Equal(t, "my-model", prefModel)
	assert.Equal(t, "custom prompt", sysPrompt)
}

func TestSyncDiscoverAgentsDB_NewAgentHasSourceAuto(t *testing.T) {
	db := setupTestDBForDiscovery(t)

	// Save/restore global state
	origAgents := Agents
	origList := AgentList
	t.Cleanup(func() {
		Agents = origAgents
		AgentList = origList
	})
	Agents = make(map[string]*Agent)
	AgentList = nil

	present := SyncDiscoverAgentsDB(db)

	// New agents should have source='auto'
	for backend := range present {
		var source string
		err := db.QueryRow("SELECT source FROM agents WHERE backend = ?", backend).Scan(&source)
		require.NoError(t, err)
		assert.Equal(t, "auto", source, "new agent should have source='auto', backend=%s", backend)
	}
}

func TestSyncDiscoverAgentsDB_SkipsBackendsAlreadyInDB(t *testing.T) {
	db := setupTestDBForDiscovery(t)

	// Pre-insert an agent for a backend
	_, err := db.Exec(`INSERT INTO agents (id, name, backend, source) VALUES ('test-existing', 'Existing', 'mock', 'manual')`)
	require.NoError(t, err)

	// Save/restore global state
	origAgents := Agents
	origList := AgentList
	t.Cleanup(func() {
		Agents = origAgents
		AgentList = origList
	})
	Agents = make(map[string]*Agent)
	AgentList = nil

	SyncDiscoverAgentsDB(db)

	// The existing record should still have source='manual'
	var source string
	err = db.QueryRow("SELECT source FROM agents WHERE id = 'test-existing'").Scan(&source)
	require.NoError(t, err)
	assert.Equal(t, "manual", source)
}

// --- MergeDiscoveredDataDB tests ---

func TestMergeDiscoveredDataDB_SoftDeletesMissingCLIs(t *testing.T) {
	db := setupTestDBForDiscovery(t)

	// Insert an agent with source='auto' for a non-existent backend
	_, err := db.Exec(`INSERT INTO agents (id, name, backend, source) VALUES ('phantom', 'Phantom', 'nonexistent-cli', 'auto')`)
	require.NoError(t, err)

	// Save/restore global state
	origAgents := Agents
	origList := AgentList
	t.Cleanup(func() {
		Agents = origAgents
		AgentList = origList
	})
	Agents = make(map[string]*Agent)
	AgentList = nil

	// Empty present map — nothing is present
	present := map[string]bool{}
	MergeDiscoveredDataDB(db, nil, present)

	// The auto agent should be deleted
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM agents WHERE id = 'phantom'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "auto agent with missing CLI should be soft-deleted")
}

func TestMergeDiscoveredDataDB_PreservesSetupAgents(t *testing.T) {
	db := setupTestDBForDiscovery(t)

	// Insert an agent with source='setup' for a non-existent backend
	_, err := db.Exec(`INSERT INTO agents (id, name, backend, source) VALUES ('wizard-agent', 'Wizard Agent', 'nonexistent-cli', 'setup')`)
	require.NoError(t, err)

	// Save/restore global state
	origAgents := Agents
	origList := AgentList
	t.Cleanup(func() {
		Agents = origAgents
		AgentList = origList
	})
	Agents = make(map[string]*Agent)
	AgentList = nil

	// Empty present map
	present := map[string]bool{}
	MergeDiscoveredDataDB(db, nil, present)

	// The setup agent should still exist
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM agents WHERE id = 'wizard-agent'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "setup agent should be preserved even if CLI is missing")
}

func TestMergeDiscoveredDataDB_PreservesManualAgents(t *testing.T) {
	db := setupTestDBForDiscovery(t)

	// Insert an agent with source='manual' for a non-existent backend
	_, err := db.Exec(`INSERT INTO agents (id, name, backend, source) VALUES ('manual-agent', 'Manual Agent', 'nonexistent-cli', 'manual')`)
	require.NoError(t, err)

	// Save/restore global state
	origAgents := Agents
	origList := AgentList
	t.Cleanup(func() {
		Agents = origAgents
		AgentList = origList
	})
	Agents = make(map[string]*Agent)
	AgentList = nil

	present := map[string]bool{}
	MergeDiscoveredDataDB(db, nil, present)

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM agents WHERE id = 'manual-agent'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "manual agent should be preserved")
}

func TestMergeDiscoveredDataDB_FillsThinkingEffortLevels(t *testing.T) {
	db := setupTestDBForDiscovery(t)

	// Insert an agent for claude (has known ThinkingEffortLevels in BackendRegistry)
	_, err := db.Exec(`INSERT INTO agents (id, name, backend, thinking_effort_levels, source)
		VALUES ('claude', 'Claude', 'claude', '[]', 'auto')`)
	require.NoError(t, err)

	// Save/restore global state
	origAgents := Agents
	origList := AgentList
	t.Cleanup(func() {
		Agents = origAgents
		AgentList = origList
	})
	Agents = make(map[string]*Agent)
	AgentList = nil

	present := map[string]bool{"claude": true}
	MergeDiscoveredDataDB(db, nil, present)

	// The thinking_effort_levels should be updated from BackendRegistry
	var levelsJSON string
	err = db.QueryRow("SELECT thinking_effort_levels FROM agents WHERE id = 'claude'").Scan(&levelsJSON)
	require.NoError(t, err)
	assert.Contains(t, levelsJSON, "low")
	assert.Contains(t, levelsJSON, "high")
}

func TestMergeDiscoveredDataDB_FillsModelsFromDiscovery(t *testing.T) {
	db := setupTestDBForDiscovery(t)

	// Insert an agent with empty models
	_, err := db.Exec(`INSERT INTO agents (id, name, backend, models, models_auto_detected, source)
		VALUES ('test-empty', 'Test Empty', 'mock', '[]', 0, 'auto')`)
	require.NoError(t, err)

	// Provide discovered models for 'mock' backend
	discoveredModels := map[string][]AgentModel{
		"mock": {
			{ID: "model-1", Name: "Model 1", Default: true},
			{ID: "model-2", Name: "Model 2", Default: false},
		},
	}

	// Save/restore global state
	origAgents := Agents
	origList := AgentList
	t.Cleanup(func() {
		Agents = origAgents
		AgentList = origList
	})
	Agents = make(map[string]*Agent)
	AgentList = nil

	present := map[string]bool{"mock": true}
	MergeDiscoveredDataDB(db, discoveredModels, present)

	// The models should be filled from discovery results
	var modelsJSON string
	var autoDetected int
	err = db.QueryRow("SELECT models, models_auto_detected FROM agents WHERE id = 'test-empty'").Scan(&modelsJSON, &autoDetected)
	require.NoError(t, err)
	assert.Contains(t, modelsJSON, "model-1")
	assert.Contains(t, modelsJSON, "model-2")
	assert.Equal(t, 1, autoDetected, "models_auto_detected should be 1 after filling from discovery")
}

func TestMergeDiscoveredDataDB_DoesNotOverwriteUserModels(t *testing.T) {
	db := setupTestDBForDiscovery(t)

	// Insert an agent with user-defined models (models_auto_detected=0 but non-empty)
	_, err := db.Exec(`INSERT INTO agents (id, name, backend, models, models_auto_detected, source)
		VALUES ('test-user', 'Test User', 'mock', '[{"id":"my-model","name":"My Model","default":true}]', 0, 'auto')`)
	require.NoError(t, err)

	// Provide discovered models for 'mock' backend
	discoveredModels := map[string][]AgentModel{
		"mock": {
			{ID: "cached-model", Name: "Cached Model", Default: true},
		},
	}

	// Save/restore global state
	origAgents := Agents
	origList := AgentList
	t.Cleanup(func() {
		Agents = origAgents
		AgentList = origList
	})
	Agents = make(map[string]*Agent)
	AgentList = nil

	present := map[string]bool{"mock": true}
	MergeDiscoveredDataDB(db, discoveredModels, present)

	// User models should be preserved (the agent has non-empty models, so step 3 query won't match it)
	var modelsJSON string
	err = db.QueryRow("SELECT models FROM agents WHERE id = 'test-user'").Scan(&modelsJSON)
	require.NoError(t, err)
	assert.Contains(t, modelsJSON, "my-model")
	assert.NotContains(t, modelsJSON, "cached-model")
}
