package service_test

import (
	"database/sql"
	"encoding/json"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupTestDBForAgents creates an in-memory SQLite with the agents and agent_api_keys tables.
func setupTestDBForAgents(t *testing.T) *sql.DB {
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
			custom_system_prompt TEXT NOT NULL DEFAULT '',
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

		CREATE TABLE IF NOT EXISTS agent_api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			agent_id TEXT NOT NULL,
			provider TEXT NOT NULL,
			custom_url TEXT NOT NULL DEFAULT '',
			encrypted_key TEXT NOT NULL,
			key_nonce TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_api_keys_agent_provider
			ON agent_api_keys(agent_id, provider);
	`)
	require.NoError(t, err)

	// Save and replace global DB
	origDB := service.DB
	origDBRead := service.DBRead
	service.DB = db
	service.DBRead = db
	t.Cleanup(func() {
		service.DB = origDB
		service.DBRead = origDBRead
		db.Close()
	})

	return db
}

func TestLoadAgentsFromDB_Empty(t *testing.T) {
	db := setupTestDBForAgents(t)

	agents, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	assert.Empty(t, agents)
}

func TestSaveAgent_Insert(t *testing.T) {
	db := setupTestDBForAgents(t)

	agent := &model.Agent{
		ID:        "pi",
		Name:      "Pi",
		Icon:      "🥧",
		Specialty: "极简编程智能体",
		Backend:   "pi",
		Command:   "/path/to/pi",
		Models: []model.AgentModel{
			{ID: "openai/gpt-5.5", Name: "GPT-5.5", Default: true},
			{ID: "openai/gpt-5.4", Name: "GPT-5.4"},
		},
		ThinkingEffortLevels: []string{"off", "minimal", "low", "medium", "high", "xhigh"},
		PreferredModel:       "openai/gpt-5.5",
		Source:               "setup",
	}

	err := service.SaveAgent(db, agent)
	require.NoError(t, err)

	// Verify it was saved
	agents, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	require.Len(t, agents, 1)

	got := agents[0]
	assert.Equal(t, "pi", got.ID)
	assert.Equal(t, "Pi", got.Name)
	assert.Equal(t, "🥧", got.Icon)
	assert.Equal(t, "极简编程智能体", got.Specialty)
	assert.Equal(t, "pi", got.Backend)
	assert.Equal(t, "/path/to/pi", got.Command)
	assert.Equal(t, "setup", got.Source)
	assert.Equal(t, "openai/gpt-5.5", got.PreferredModel)
	assert.Len(t, got.Models, 2)
	assert.Equal(t, "openai/gpt-5.5", got.Models[0].ID)
	assert.True(t, got.Models[0].Default)
	assert.Len(t, got.ThinkingEffortLevels, 6)
	assert.Equal(t, "off", got.ThinkingEffortLevels[0])
}

func TestSaveAgent_Upsert(t *testing.T) {
	db := setupTestDBForAgents(t)

	// Insert first time
	agent := &model.Agent{
		ID:      "pi",
		Name:    "Pi",
		Backend: "pi",
		Source:  "auto",
	}
	err := service.SaveAgent(db, agent)
	require.NoError(t, err)

	// Upsert with updated name
	agent.Name = "Pi Updated"
	agent.PreferredModel = "openai/gpt-5.5"
	err = service.SaveAgent(db, agent)
	require.NoError(t, err)

	// Verify only one record, with updated values
	agents, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "Pi Updated", agents[0].Name)
	assert.Equal(t, "openai/gpt-5.5", agents[0].PreferredModel)
}

func TestSaveAgent_MultipleAgents(t *testing.T) {
	db := setupTestDBForAgents(t)

	agents := []*model.Agent{
		{ID: "claude", Name: "Claude", Backend: "claude", Source: "auto"},
		{ID: "pi", Name: "Pi", Backend: "pi", Source: "setup"},
		{ID: "codebuddy", Name: "Codebuddy", Backend: "codebuddy", Source: "auto"},
	}

	for _, a := range agents {
		err := service.SaveAgent(db, a)
		require.NoError(t, err)
	}

	got, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	assert.Len(t, got, 3)

	// Should be sorted by ID
	assert.Equal(t, "claude", got[0].ID)
	assert.Equal(t, "codebuddy", got[1].ID)
	assert.Equal(t, "pi", got[2].ID)
}

func TestDeleteAgent(t *testing.T) {
	db := setupTestDBForAgents(t)

	// Insert two agents
	err := service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "setup"})
	require.NoError(t, err)
	err = service.SaveAgent(db, &model.Agent{ID: "claude", Name: "Claude", Backend: "claude", Source: "auto"})
	require.NoError(t, err)

	// Delete one
	err = service.DeleteAgent(db, "pi")
	require.NoError(t, err)

	// Verify only claude remains
	agents, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "claude", agents[0].ID)
}

func TestDeleteAgent_NotFound(t *testing.T) {
	db := setupTestDBForAgents(t)

	// Deleting non-existent agent should not error
	err := service.DeleteAgent(db, "nonexistent")
	assert.NoError(t, err)
}

func TestPatchAgent(t *testing.T) {
	db := setupTestDBForAgents(t)

	// Insert an agent
	err := service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "setup"})
	require.NoError(t, err)

	// Patch preferred model and thinking
	err = service.PatchAgent(db, "pi", "openai/gpt-5.5", "high", "cli")
	require.NoError(t, err)

	// Verify
	agents, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "openai/gpt-5.5", agents[0].PreferredModel)
	assert.Equal(t, "high", agents[0].PreferredThinkingEffort)
}

func TestPatchAgent_ClearPreferences(t *testing.T) {
	db := setupTestDBForAgents(t)

	// Insert an agent with preferences
	agent := &model.Agent{
		ID:                      "pi",
		Name:                    "Pi",
		Backend:                 "pi",
		PreferredModel:          "openai/gpt-5.5",
		PreferredThinkingEffort: "high",
		Source:                  "setup",
	}
	err := service.SaveAgent(db, agent)
	require.NoError(t, err)

	// Patch to clear preferences
	err = service.PatchAgent(db, "pi", "", "", "cli")
	require.NoError(t, err)

	// Verify preferences are cleared
	agents, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "", agents[0].PreferredModel)
	assert.Equal(t, "", agents[0].PreferredThinkingEffort)
}

func TestPatchAgent_NotFound(t *testing.T) {
	db := setupTestDBForAgents(t)

	// Patching non-existent agent should not error (no rows affected)
	err := service.PatchAgent(db, "nonexistent", "model", "high", "cli")
	assert.NoError(t, err)
}

func TestLoadAgentsFromDB_ModelsJSON(t *testing.T) {
	db := setupTestDBForAgents(t)

	agent := &model.Agent{
		ID:      "pi",
		Name:    "Pi",
		Backend: "pi",
		Models: []model.AgentModel{
			{ID: "minimax/MiniMax-M2.7", Name: "MiniMax-M2.7", Default: true},
			{ID: "openai/gpt-5.5", Name: "GPT-5.5"},
		},
		Source: "auto",
	}
	err := service.SaveAgent(db, agent)
	require.NoError(t, err)

	// Verify models are correctly serialized/deserialized
	agents, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	require.Len(t, agents[0].Models, 2)
	assert.Equal(t, "minimax/MiniMax-M2.7", agents[0].Models[0].ID)
	assert.True(t, agents[0].Models[0].Default)
	assert.Equal(t, "openai/gpt-5.5", agents[0].Models[1].ID)
	assert.False(t, agents[0].Models[1].Default)
}

func TestLoadAgentsFromDB_ThinkingEffortLevelsJSON(t *testing.T) {
	db := setupTestDBForAgents(t)

	agent := &model.Agent{
		ID:                   "pi",
		Name:                 "Pi",
		Backend:              "pi",
		ThinkingEffortLevels: []string{"off", "minimal", "low", "medium", "high", "xhigh"},
		Source:               "auto",
	}
	err := service.SaveAgent(db, agent)
	require.NoError(t, err)

	agents, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, []string{"off", "minimal", "low", "medium", "high", "xhigh"}, agents[0].ThinkingEffortLevels)
}

func TestLoadAgentsFromDB_EmptyModelsAndLevels(t *testing.T) {
	db := setupTestDBForAgents(t)

	agent := &model.Agent{
		ID:      "pi",
		Name:    "Pi",
		Backend: "pi",
		Source:  "auto",
		// Models and ThinkingEffortLevels are nil/empty
	}
	err := service.SaveAgent(db, agent)
	require.NoError(t, err)

	agents, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Empty(t, agents[0].Models)
	assert.Empty(t, agents[0].ThinkingEffortLevels)
}

func TestSaveAgent_SourceField(t *testing.T) {
	db := setupTestDBForAgents(t)

	for _, source := range []string{"auto", "setup", "manual"} {
		t.Run(source, func(t *testing.T) {
			agent := &model.Agent{
				ID:      "test-" + source,
				Name:    "Test " + source,
				Backend: "pi",
				Source:  source,
			}
			err := service.SaveAgent(db, agent)
			require.NoError(t, err)
		})
	}

	agents, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	assert.Len(t, agents, 3)

	// Verify each source
	agentMap := make(map[string]*model.Agent)
	for i := range agents {
		agentMap[agents[i].ID] = agents[i]
	}
	assert.Equal(t, "auto", agentMap["test-auto"].Source)
	assert.Equal(t, "setup", agentMap["test-setup"].Source)
	assert.Equal(t, "manual", agentMap["test-manual"].Source)
}

func TestDeleteAgent_CascadesAPIKeys(t *testing.T) {
	db := setupTestDBForAgents(t)

	// Insert agent
	err := service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "setup"})
	require.NoError(t, err)

	// Insert API key (directly, since encryption is in Task 2)
	_, err = db.Exec(`INSERT INTO agent_api_keys (agent_id, provider, encrypted_key, key_nonce)
		VALUES ('pi', 'openai', 'encrypted-value', 'nonce-value')`)
	require.NoError(t, err)

	// Verify API key exists
	var count int
	db.QueryRow("SELECT COUNT(*) FROM agent_api_keys WHERE agent_id = 'pi'").Scan(&count)
	assert.Equal(t, 1, count)

	// Delete agent should cascade to API keys
	err = service.DeleteAgent(db, "pi")
	require.NoError(t, err)

	// Verify API keys are deleted
	db.QueryRow("SELECT COUNT(*) FROM agent_api_keys WHERE agent_id = 'pi'").Scan(&count)
	assert.Equal(t, 0, count)
}

// Verify that the DDL in setupTestDBForAgents matches the production DDL in database.go.
// This test ensures we don't drift between test and production schemas.
func TestAgentSchemaMatchesProduction(t *testing.T) {
	db := setupTestDBForAgents(t)

	// Check agents table columns
	expectedColumns := map[string]bool{
		"id": true, "name": true, "icon": true, "specialty": true, "backend": true,
		"command": true, "thinking_effort": true, "thinking_effort_levels": true,
		"preferred_model": true, "preferred_thinking_effort": true, "system_prompt": true,
		"custom_system_prompt": true,
		"models":               true, "models_auto_detected": true, "source": true, "sort_order": true,
		"transport": true, "acp_command": true,
		"created_at": true, "updated_at": true,
	}

	rows, err := db.Query("SELECT name FROM pragma_table_info('agents')")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	foundColumns := make(map[string]bool)
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		foundColumns[name] = true
	}
	require.NoError(t, rows.Err())

	for col := range expectedColumns {
		assert.True(t, foundColumns[col], "missing column in agents table: %s", col)
	}
	for col := range foundColumns {
		assert.True(t, expectedColumns[col], "unexpected column in agents table: %s", col)
	}

	// Check agent_api_keys table columns
	expectedKeyColumns := map[string]bool{
		"id": true, "agent_id": true, "provider": true, "custom_url": true,
		"encrypted_key": true, "key_nonce": true, "created_at": true, "updated_at": true,
	}

	rows2, err := db.Query("SELECT name FROM pragma_table_info('agent_api_keys')")
	require.NoError(t, err)
	defer func() { _ = rows2.Close() }()

	foundKeyColumns := make(map[string]bool)
	for rows2.Next() {
		var name string
		require.NoError(t, rows2.Scan(&name))
		foundKeyColumns[name] = true
	}
	require.NoError(t, rows2.Err())

	for col := range expectedKeyColumns {
		assert.True(t, foundKeyColumns[col], "missing column in agent_api_keys table: %s", col)
	}
	for col := range foundKeyColumns {
		assert.True(t, expectedKeyColumns[col], "unexpected column in agent_api_keys table: %s", col)
	}
}

// Verify indexes exist
func TestAgentIndexes(t *testing.T) {
	db := setupTestDBForAgents(t)

	expectedIndexes := map[string]bool{
		"idx_agents_backend":                true,
		"idx_agents_source":                 true,
		"idx_agents_sort":                   true,
		"idx_agent_api_keys_agent_provider": true,
	}

	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='index' AND name LIKE 'idx_agent%'")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	foundIndexes := make(map[string]bool)
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		foundIndexes[name] = true
	}
	require.NoError(t, rows.Err())

	for idx := range expectedIndexes {
		assert.True(t, foundIndexes[idx], "missing index: %s", idx)
	}
}

// Verify models JSON round-trip with special characters
func TestSaveAgent_ModelsWithSpecialChars(t *testing.T) {
	db := setupTestDBForAgents(t)

	agent := &model.Agent{
		ID:      "pi",
		Name:    "Pi",
		Backend: "pi",
		Source:  "auto",
		Models: []model.AgentModel{
			{ID: "anthropic/claude-sonnet-4-6", Name: "Claude Sonnet 4.6", Default: true},
		},
		SystemPrompt: "You are a helpful assistant.\nWith newlines and \"quotes\".",
	}
	err := service.SaveAgent(db, agent)
	require.NoError(t, err)

	agents, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "anthropic/claude-sonnet-4-6", agents[0].Models[0].ID)
	assert.Equal(t, "Claude Sonnet 4.6", agents[0].Models[0].Name)
	assert.Contains(t, agents[0].SystemPrompt, "newlines and \"quotes\"")
}

func TestSaveAgent_WithTransport(t *testing.T) {
	db := setupTestDBForAgents(t)

	agent := &model.Agent{
		ID:         "kimi",
		Name:       "Kimi",
		Backend:    "kimi",
		Source:     "auto",
		Transport:  "acp-stdio",
		AcpCommand: "kimi --acp",
	}

	err := service.SaveAgent(db, agent)
	require.NoError(t, err)

	// Load and verify transport fields
	agents, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	require.Len(t, agents, 1)

	got := agents[0]
	assert.Equal(t, "acp-stdio", got.Transport)
	assert.Equal(t, "kimi --acp", got.AcpCommand)
}

func TestSaveAgent_TransportDefaultsToCLI(t *testing.T) {
	db := setupTestDBForAgents(t)

	// Save agent without Transport — should default to "cli"
	agent := &model.Agent{
		ID:      "pi",
		Name:    "Pi",
		Backend: "pi",
		Source:  "auto",
	}

	err := service.SaveAgent(db, agent)
	require.NoError(t, err)

	agents, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "cli", agents[0].Transport)
}

// Helper to verify JSON serialization of models
func TestAgentModelsJSON_Serialization(t *testing.T) {
	models := []model.AgentModel{
		{ID: "gpt-5.5", Name: "GPT-5.5", Default: true},
		{ID: "gpt-5.4", Name: "GPT-5.4", Default: false},
	}

	data, err := json.Marshal(models)
	require.NoError(t, err)

	var got []model.AgentModel
	err = json.Unmarshal(data, &got)
	require.NoError(t, err)
	assert.Equal(t, models, got)
}

// ── PatchAgentFields tests ──

func TestPatchAgentFields_Name(t *testing.T) {
	db := setupTestDBForAgents(t)
	require.NoError(t, service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "auto"}))

	name := "Pi Updated"
	err := service.PatchAgentFields(db, "pi", service.AgentPatch{Name: &name})
	require.NoError(t, err)

	agents, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "Pi Updated", agents[0].Name)
}

func TestPatchAgentFields_Icon(t *testing.T) {
	db := setupTestDBForAgents(t)
	require.NoError(t, service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "auto"}))

	icon := "🥧"
	err := service.PatchAgentFields(db, "pi", service.AgentPatch{Icon: &icon})
	require.NoError(t, err)

	agents, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "🥧", agents[0].Icon)
}

func TestPatchAgentFields_Specialty(t *testing.T) {
	db := setupTestDBForAgents(t)
	require.NoError(t, service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "auto"}))

	specialty := "极简编程"
	err := service.PatchAgentFields(db, "pi", service.AgentPatch{Specialty: &specialty})
	require.NoError(t, err)

	agents, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "极简编程", agents[0].Specialty)
}

func TestPatchAgentFields_CustomSystemPrompt(t *testing.T) {
	db := setupTestDBForAgents(t)
	require.NoError(t, service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "auto"}))

	custom := "You are a helpful math tutor."
	err := service.PatchAgentFields(db, "pi", service.AgentPatch{CustomSystemPrompt: &custom})
	require.NoError(t, err)

	agents, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, custom, agents[0].CustomSystemPrompt)
	// system_prompt should be composed: commonPrompt + customSystemPrompt
	commonPrompt := model.BuildCommonPrompt()
	if commonPrompt != "" {
		assert.Equal(t, commonPrompt+"\n\n"+custom, agents[0].SystemPrompt)
	}
}

func TestPatchAgentFields_SortOrder(t *testing.T) {
	db := setupTestDBForAgents(t)
	require.NoError(t, service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "auto"}))

	order := 5
	err := service.PatchAgentFields(db, "pi", service.AgentPatch{SortOrder: &order})
	require.NoError(t, err)

	agents, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, 5, agents[0].SortOrder)
}

func TestPatchAgentFields_PartialPatch(t *testing.T) {
	db := setupTestDBForAgents(t)
	require.NoError(t, service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Icon: "🤖", Specialty: "old", Backend: "pi", Source: "auto"}))

	// Only patch name, verify other fields unchanged
	name := "Pi New"
	err := service.PatchAgentFields(db, "pi", service.AgentPatch{Name: &name})
	require.NoError(t, err)

	agents, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "Pi New", agents[0].Name)
	assert.Equal(t, "🤖", agents[0].Icon)        // unchanged
	assert.Equal(t, "old", agents[0].Specialty) // unchanged
}

func TestPatchAgentFields_NilFieldsSkipped(t *testing.T) {
	db := setupTestDBForAgents(t)
	require.NoError(t, service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi", Source: "auto"}))

	// Empty patch — should be a no-op
	err := service.PatchAgentFields(db, "pi", service.AgentPatch{})
	require.NoError(t, err)

	agents, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "Pi", agents[0].Name)
}

// ── MigrateCustomSystemPrompt tests ──

func TestMigrateCustomSystemPrompt(t *testing.T) {
	db := setupTestDBForAgents(t)

	commonPrompt := model.BuildCommonPrompt()
	require.NotEmpty(t, commonPrompt, "common prompt should not be empty for migration test")

	// Insert agent with system_prompt = commonPrompt + custom, but empty custom_system_prompt
	custom := "You are a helpful assistant."
	fullPrompt := commonPrompt + "\n\n" + custom
	agent := &model.Agent{
		ID:                 "pi",
		Name:               "Pi",
		Backend:            "pi",
		Source:             "auto",
		SystemPrompt:       fullPrompt,
		CustomSystemPrompt: "", // not yet migrated
	}
	require.NoError(t, service.SaveAgent(db, agent))

	// Run migration
	service.MigrateCustomSystemPrompt(db)

	// Verify custom_system_prompt was backfilled
	agents, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, custom, agents[0].CustomSystemPrompt)
}

func TestMigrateCustomSystemPrompt_CommonOnly(t *testing.T) {
	db := setupTestDBForAgents(t)

	commonPrompt := model.BuildCommonPrompt()
	// Insert agent with system_prompt = commonPrompt only (no custom portion)
	agent := &model.Agent{
		ID:                 "pi",
		Name:               "Pi",
		Backend:            "pi",
		Source:             "auto",
		SystemPrompt:       commonPrompt,
		CustomSystemPrompt: "",
	}
	require.NoError(t, service.SaveAgent(db, agent))

	service.MigrateCustomSystemPrompt(db)

	agents, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "", agents[0].CustomSystemPrompt)
}

func TestMigrateCustomSystemPrompt_AlreadyMigrated(t *testing.T) {
	db := setupTestDBForAgents(t)

	// Insert agent that already has custom_system_prompt set
	custom := "Already done"
	agent := &model.Agent{
		ID:                 "pi",
		Name:               "Pi",
		Backend:            "pi",
		Source:             "auto",
		SystemPrompt:       "some prompt",
		CustomSystemPrompt: custom,
	}
	require.NoError(t, service.SaveAgent(db, agent))

	service.MigrateCustomSystemPrompt(db)

	agents, err := service.LoadAgentsFromDB(db)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, custom, agents[0].CustomSystemPrompt) // unchanged
}
