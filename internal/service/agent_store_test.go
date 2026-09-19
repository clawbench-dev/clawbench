package service_test

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupTestDBForAgents creates an in-memory SQLite with the agents table.
func setupTestDBForAgents(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)

	// Use the production DDL so the fixture cannot drift from the real schema.
	_, err = db.Exec(service.AgentDDL)
	require.NoError(t, err)

	// Save and replace global DB
	cleanup := service.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		db.Close()
	})

	return db
}

func TestLoadAgentsFromDB_Empty(t *testing.T) {
	_ = setupTestDBForAgents(t)

	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	assert.Empty(t, agents)
}

func TestSaveAgent_Insert(t *testing.T) {
	db := setupTestDBForAgents(t)

	agent := &model.Agent{
		ID:        "pi",
		Name:      "Pi",
		Specialty: "极简编程智能体",
		Backend:   "pi",
		Command:   "/path/to/pi",
		Models: []model.AgentModel{
			{ID: "openai/gpt-5.5", Name: "GPT-5.5", Default: true},
			{ID: "openai/gpt-5.4", Name: "GPT-5.4"},
		},
		ThinkingEffortLevels: []string{"off", "minimal", "low", "medium", "high", "xhigh"},
		PreferredModel:       "openai/gpt-5.5",
	}

	err := service.SaveAgent(db, agent)
	require.NoError(t, err)

	// Verify it was saved
	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)

	got := agents[0]
	assert.Equal(t, "pi", got.ID)
	assert.Equal(t, "Pi", got.Name)
	assert.Equal(t, "极简编程智能体", got.Specialty)
	assert.Equal(t, "pi", got.Backend)
	assert.Equal(t, "/path/to/pi", got.Command)
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
	}
	err := service.SaveAgent(db, agent)
	require.NoError(t, err)

	// Upsert with updated name
	agent.Name = "Pi Updated"
	agent.PreferredModel = "openai/gpt-5.5"
	err = service.SaveAgent(db, agent)
	require.NoError(t, err)

	// Verify only one record, with updated values
	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "Pi Updated", agents[0].Name)
	assert.Equal(t, "openai/gpt-5.5", agents[0].PreferredModel)
}

func TestSaveAgent_MultipleAgents(t *testing.T) {
	db := setupTestDBForAgents(t)

	agents := []*model.Agent{
		{ID: "claude", Name: "Claude", Backend: "claude"},
		{ID: "pi", Name: "Pi", Backend: "pi"},
		{ID: "codebuddy", Name: "Codebuddy", Backend: "codebuddy"},
	}

	for _, a := range agents {
		err := service.SaveAgent(db, a)
		require.NoError(t, err)
	}

	got, err := service.LoadAgentsFromDB()
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
	err := service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi"})
	require.NoError(t, err)
	err = service.SaveAgent(db, &model.Agent{ID: "claude", Name: "Claude", Backend: "claude"})
	require.NoError(t, err)

	// Delete one
	err = service.DeleteAgent("pi")
	require.NoError(t, err)

	// Verify only claude remains
	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "claude", agents[0].ID)
}

func TestDeleteAgent_NotFound(t *testing.T) {
	_ = setupTestDBForAgents(t)

	// Deleting non-existent agent should not error
	err := service.DeleteAgent("nonexistent")
	assert.NoError(t, err)
}

func TestPatchAgent(t *testing.T) {
	db := setupTestDBForAgents(t)

	// Insert an agent
	err := service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi"})
	require.NoError(t, err)

	// Patch preferred model and thinking
	err = service.PatchAgent("pi", "openai/gpt-5.5", "high", "cli")
	require.NoError(t, err)

	// Verify
	agents, err := service.LoadAgentsFromDB()
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
	}
	err := service.SaveAgent(db, agent)
	require.NoError(t, err)

	// Patch to clear preferences
	err = service.PatchAgent("pi", "", "", "cli")
	require.NoError(t, err)

	// Verify preferences are cleared
	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "", agents[0].PreferredModel)
	assert.Equal(t, "", agents[0].PreferredThinkingEffort)
}

func TestPatchAgent_NotFound(t *testing.T) {
	_ = setupTestDBForAgents(t)

	// Patching non-existent agent should not error (no rows affected)
	err := service.PatchAgent("nonexistent", "model", "high", "cli")
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
	}
	err := service.SaveAgent(db, agent)
	require.NoError(t, err)

	// Verify models are correctly serialized/deserialized
	agents, err := service.LoadAgentsFromDB()
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
	}
	err := service.SaveAgent(db, agent)
	require.NoError(t, err)

	agents, err := service.LoadAgentsFromDB()
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
		// Models and ThinkingEffortLevels are nil/empty
	}
	err := service.SaveAgent(db, agent)
	require.NoError(t, err)

	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Empty(t, agents[0].Models)
	assert.Empty(t, agents[0].ThinkingEffortLevels)
}

// Verify that the DDL in setupTestDBForAgents matches the production DDL in database.go.
// This test ensures we don't drift between test and production schemas.
func TestAgentSchemaMatchesProduction(t *testing.T) {
	db := setupTestDBForAgents(t)

	// Check agents table columns
	expectedColumns := map[string]bool{
		"id": true, "name": true, "icon": true, "specialty": true, "backend": true,
		"command": true, "thinking_effort": true, "thinking_effort_levels": true,
		"preferred_mode": true, "preferred_model": true, "preferred_thinking_effort": true,
		"custom_system_prompt": true,
		"models":               true, "models_auto_detected": true, "sort_order": true,
		"transport": true, "acp_command": true,
		"acp_available_modes": true, "acp_available_thinking_efforts": true, "acp_available_commands": true,
		"acp_available_models": true,
		"acp_config_options":   true, "auto_approve": true,
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
}

// Verify indexes exist
func TestAgentIndexes(t *testing.T) {
	db := setupTestDBForAgents(t)

	expectedIndexes := map[string]bool{
		"idx_agents_backend": true,
		"idx_agents_sort":    true,
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
		Models: []model.AgentModel{
			{ID: "anthropic/claude-sonnet-4-6", Name: "Claude Sonnet 4.6", Default: true},
		},
		CustomSystemPrompt: "You are a helpful assistant.\nWith newlines and \"quotes\".",
	}
	err := service.SaveAgent(db, agent)
	require.NoError(t, err)

	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "anthropic/claude-sonnet-4-6", agents[0].Models[0].ID)
	assert.Equal(t, "Claude Sonnet 4.6", agents[0].Models[0].Name)
	assert.Contains(t, agents[0].CustomSystemPrompt, "newlines and \"quotes\"")
}

func TestSaveAgent_WithTransport(t *testing.T) {
	db := setupTestDBForAgents(t)

	agent := &model.Agent{
		ID:         "kimi",
		Name:       "Kimi",
		Backend:    "kimi",
		Transport:  "acp-stdio",
		AcpCommand: "kimi --acp",
	}

	err := service.SaveAgent(db, agent)
	require.NoError(t, err)

	// Load and verify transport fields
	agents, err := service.LoadAgentsFromDB()
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
	}

	err := service.SaveAgent(db, agent)
	require.NoError(t, err)

	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "cli", agents[0].Transport)
}

func TestSaveAgent_AutoApproveRoundTrip(t *testing.T) {
	db := setupTestDBForAgents(t)

	// Default is off when the field is not set
	agent := &model.Agent{ID: "pi", Name: "Pi", Backend: "pi"}
	require.NoError(t, service.SaveAgent(db, agent))
	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.False(t, agents[0].AutoApprove, "auto_approve should default to off")

	// Enable auto-approve and save again (upsert)
	agent.AutoApprove = true
	require.NoError(t, service.SaveAgent(db, agent))
	agents, err = service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.True(t, agents[0].AutoApprove, "auto_approve should be true after upsert")

	// Disable and persist
	agent.AutoApprove = false
	require.NoError(t, service.SaveAgent(db, agent))
	agents, err = service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.False(t, agents[0].AutoApprove)
}

func TestPatchAgentFields_AutoApprove(t *testing.T) {
	db := setupTestDBForAgents(t)
	require.NoError(t, service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi"}))

	enabled := true
	require.NoError(t, service.PatchAgentFields("pi", service.AgentPatch{AutoApprove: &enabled}))

	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.True(t, agents[0].AutoApprove)

	disabled := false
	require.NoError(t, service.PatchAgentFields("pi", service.AgentPatch{AutoApprove: &disabled}))
	agents, err = service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.False(t, agents[0].AutoApprove)
}

func TestPatchAgentFields_AutoApprovePartialPatch(t *testing.T) {
	db := setupTestDBForAgents(t)
	require.NoError(t, service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Specialty: "old", Backend: "pi"}))

	// Patching auto_approve alone must not disturb other fields
	enabled := true
	require.NoError(t, service.PatchAgentFields("pi", service.AgentPatch{AutoApprove: &enabled}))

	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.True(t, agents[0].AutoApprove)
	assert.Equal(t, "Pi", agents[0].Name)
	assert.Equal(t, "old", agents[0].Specialty)
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
	require.NoError(t, service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi"}))

	name := "Pi Updated"
	err := service.PatchAgentFields("pi", service.AgentPatch{Name: &name})
	require.NoError(t, err)

	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "Pi Updated", agents[0].Name)
}

func TestPatchAgentFields_Specialty(t *testing.T) {
	db := setupTestDBForAgents(t)
	require.NoError(t, service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi"}))

	specialty := "极简编程"
	err := service.PatchAgentFields("pi", service.AgentPatch{Specialty: &specialty})
	require.NoError(t, err)

	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "极简编程", agents[0].Specialty)
}

func TestPatchAgentFields_CustomSystemPrompt(t *testing.T) {
	db := setupTestDBForAgents(t)
	require.NoError(t, service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi"}))

	custom := "You are a helpful math tutor."
	err := service.PatchAgentFields("pi", service.AgentPatch{CustomSystemPrompt: &custom})
	require.NoError(t, err)

	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	// LoadAgentsFromDB is a raw read: only the user's text is stored.
	assert.Equal(t, custom, agents[0].CustomSystemPrompt)
	assert.Empty(t, agents[0].RuntimeSystemPrompt, "the composed prompt is never persisted")

	// Composition happens when agents are loaded into memory, using the current
	// shared prompt — so a change to the built-in prompt always takes effect.
	require.NoError(t, service.LoadAgentsIntoMemory())
	commonPrompt := model.BuildCommonPrompt()
	require.NotEmpty(t, commonPrompt)
	assert.Equal(t, commonPrompt+"\n\n"+custom, model.Agents["pi"].RuntimeSystemPrompt)
}

func TestPatchAgentFields_SortOrder(t *testing.T) {
	db := setupTestDBForAgents(t)
	require.NoError(t, service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi"}))

	order := 5
	err := service.PatchAgentFields("pi", service.AgentPatch{SortOrder: &order})
	require.NoError(t, err)

	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, 5, agents[0].SortOrder)
}

func TestPatchAgentFields_PartialPatch(t *testing.T) {
	db := setupTestDBForAgents(t)
	require.NoError(t, service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Specialty: "old", Backend: "pi"}))

	// Only patch name, verify other fields unchanged
	name := "Pi New"
	err := service.PatchAgentFields("pi", service.AgentPatch{Name: &name})
	require.NoError(t, err)

	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "Pi New", agents[0].Name)
	assert.Equal(t, "old", agents[0].Specialty) // unchanged
}

func TestPatchAgentFields_NilFieldsSkipped(t *testing.T) {
	db := setupTestDBForAgents(t)
	require.NoError(t, service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi"}))

	// Empty patch — should be a no-op
	err := service.PatchAgentFields("pi", service.AgentPatch{})
	require.NoError(t, err)

	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "Pi", agents[0].Name)
}

// ── Prompt storage tests ──

// The composed prompt is never persisted; only the user's own text is durable.
func TestSaveAgent_DoesNotPersistComposedPrompt(t *testing.T) {
	db := setupTestDBForAgents(t)
	require.NoError(t, service.SaveAgent(db, &model.Agent{
		ID: "pi", Name: "Pi", Backend: "pi",
		CustomSystemPrompt: "my own instructions",
	}))

	// The legacy column is gone entirely.
	var columns int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name='system_prompt'",
	).Scan(&columns))
	assert.Zero(t, columns, "the composed-prompt column must not exist")

	// Only the user's text is stored.
	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "my own instructions", agents[0].CustomSystemPrompt)
}

// Loading composes the runtime prompt from the *current* shared prompt, so a
// change to the built-in prompt always takes effect. This is the regression the
// dropped column used to cause.
func TestLoadAgentsIntoMemory_ComposesFromCurrentSharedPrompt(t *testing.T) {
	db := setupTestDBForAgents(t)
	require.NoError(t, service.SaveAgent(db, &model.Agent{
		ID: "pi", Name: "Pi", Backend: "pi",
		CustomSystemPrompt: "my own instructions",
	}))
	require.NoError(t, service.LoadAgentsIntoMemory())

	commonPrompt := model.BuildCommonPrompt()
	require.NotEmpty(t, commonPrompt)
	assert.Equal(t, commonPrompt+"\n\nmy own instructions", model.Agents["pi"].RuntimeSystemPrompt)

	// The shared prompt appears exactly once — no frozen copy can be appended.
	assert.Equal(t, 1, strings.Count(model.Agents["pi"].RuntimeSystemPrompt, commonPrompt))
}

// With no custom text the runtime prompt is exactly the shared prompt.
func TestLoadAgentsIntoMemory_NoCustomPrompt(t *testing.T) {
	db := setupTestDBForAgents(t)
	require.NoError(t, service.SaveAgent(db, &model.Agent{ID: "pi", Name: "Pi", Backend: "pi"}))
	require.NoError(t, service.LoadAgentsIntoMemory())

	assert.Equal(t, model.BuildCommonPrompt(), model.Agents["pi"].RuntimeSystemPrompt)
}

func TestDuplicateAgent_NotFound(t *testing.T) {
	_ = setupTestDBForAgents(t)

	_, err := service.DuplicateAgent("nonexistent", "Copy")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestDuplicateAgent_Success(t *testing.T) {
	db := setupTestDBForAgents(t)

	// Insert source agent with all fields
	agent := &model.Agent{
		ID:                      "pi",
		Name:                    "Pi",
		Specialty:               "极简编程智能体",
		Backend:                 "pi",
		Command:                 "/path/to/pi",
		ThinkingEffort:          "medium",
		ThinkingEffortLevels:    []string{"off", "low", "medium", "high"},
		PreferredMode:           "code",
		PreferredModel:          "openai/gpt-5.5",
		PreferredThinkingEffort: "high",
		CustomSystemPrompt:      "You are helpful.",
		Transport:               "acp-stdio",
		AcpCommand:              "pi --acp",
		AutoApprove:             true,
		Models: []model.AgentModel{
			{ID: "openai/gpt-5.5", Name: "GPT-5.5", Default: true},
		},
	}
	require.NoError(t, service.SaveAgent(db, agent))

	// Load into memory so DuplicateAgent can find the source
	service.LoadAgentsIntoMemory()

	// Duplicate
	clone, err := service.DuplicateAgent("pi", "Pi Copy")
	require.NoError(t, err)
	assert.Contains(t, clone.ID, "pi-copy-")
	assert.Equal(t, "Pi Copy", clone.Name)
	assert.Equal(t, "pi", clone.Backend)
	assert.True(t, clone.AutoApprove, "auto_approve should be copied to the clone")

	// Verify both agents in DB
	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	assert.Len(t, agents, 2)
}

func TestLoadAgentsIntoMemory(t *testing.T) {
	db := setupTestDBForAgents(t)

	// Insert agents
	require.NoError(t, service.SaveAgent(db, &model.Agent{ID: "test-1", Name: "Test 1", Backend: "pi"}))
	require.NoError(t, service.SaveAgent(db, &model.Agent{ID: "test-2", Name: "Test 2", Backend: "pi"}))

	// Load into memory
	service.LoadAgentsIntoMemory()

	// Verify they're in model.Agents
	assert.Contains(t, model.Agents, "test-1")
	assert.Contains(t, model.Agents, "test-2")
}

// End-to-end: a database created by the old scheme (composed prompt stored in
// system_prompt, and copied into custom_system_prompt) is migrated so that the
// shared prompt is injected exactly once and always from the current template.
// This is the regression the whole change exists to prevent.
func TestLegacyPromptMigration_EndToEnd(t *testing.T) {
	db := setupTestDBForAgents(t)

	// Recreate the legacy shape: the column exists and holds the composed
	// prompt, duplicated into custom_system_prompt by the old migration.
	_, err := db.Exec("ALTER TABLE agents ADD COLUMN system_prompt TEXT NOT NULL DEFAULT ''")
	require.NoError(t, err)
	oldCommon := "## User Interaction (Highest Priority)\n\nOLD XML FORMAT\n<multi-select>false</multi-select>"
	_, err = db.Exec(
		`INSERT INTO agents (id, name, backend, system_prompt, custom_system_prompt) VALUES (?, ?, ?, ?, ?)`,
		"pi", "Pi", "pi", oldCommon, oldCommon,
	)
	require.NoError(t, err)

	require.NoError(t, service.MigrateLegacyAgentPrompts())
	require.NoError(t, service.LoadAgentsIntoMemory())

	// The legacy column is gone.
	var columns int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name='system_prompt'",
	).Scan(&columns))
	assert.Zero(t, columns)

	// The stale copy is gone and the current shared prompt is used exactly once.
	got := model.Agents["pi"].RuntimeSystemPrompt
	assert.Equal(t, 1, strings.Count(got, "## User Interaction (Highest Priority)"),
		"the shared prompt must appear exactly once")
	assert.Contains(t, got, "ONE question per tag", "the current shared prompt must be used")
	assert.NotContains(t, got, "OLD XML FORMAT", "the stale copy must be gone")

	// Idempotent: running again changes nothing.
	require.NoError(t, service.MigrateLegacyAgentPrompts())
	var columnsAfter int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name='system_prompt'",
	).Scan(&columnsAfter))
	assert.Zero(t, columnsAfter)
}

// A duplicated agent goes straight into the live Agents map, so its runtime
// prompt must be composed immediately — nothing recomposes it until the next
// reload. Left empty, the clone would run with no system prompt at all.
func TestDuplicateAgent_ComposesRuntimePrompt(t *testing.T) {
	db := setupTestDBForAgents(t)
	require.NoError(t, service.SaveAgent(db, &model.Agent{
		ID: "pi", Name: "Pi", Backend: "pi", CustomSystemPrompt: "be terse",
	}))
	require.NoError(t, service.LoadAgentsIntoMemory())

	clone, err := service.DuplicateAgent("pi", "Pi Copy")
	require.NoError(t, err)

	commonPrompt := model.BuildCommonPrompt()
	require.NotEmpty(t, commonPrompt)
	assert.Equal(t, commonPrompt+"\n\nbe terse", clone.RuntimeSystemPrompt)
}

// Prompts configured under the old scheme are discarded, not guessed at: the
// stored text cannot be reliably split back into "shared prefix" and "what the
// user wrote" (recognizing old prefix versions is exactly what the previous
// migration got wrong). This test pins that decision so it stays deliberate.
func TestLegacyPromptMigration_DiscardsOldSchemePrompts(t *testing.T) {
	db := setupTestDBForAgents(t)

	// A row whose legacy column holds text that is NOT our shared prompt — the
	// shape a YAML-authored prompt produced. It is still discarded, because the
	// migration cannot tell it apart from a stale composed prompt.
	_, err := db.Exec("ALTER TABLE agents ADD COLUMN system_prompt TEXT NOT NULL DEFAULT ''")
	require.NoError(t, err)
	raw := "Always answer in French and never use bullet points."
	_, err = db.Exec(
		`INSERT INTO agents (id, name, backend, system_prompt, custom_system_prompt) VALUES (?, ?, ?, ?, ?)`,
		"pi", "Pi", "pi", raw, raw,
	)
	require.NoError(t, err)

	require.NoError(t, service.MigrateLegacyAgentPrompts())

	var custom string
	require.NoError(t, db.QueryRow(
		"SELECT custom_system_prompt FROM agents WHERE id='pi'",
	).Scan(&custom))
	assert.Empty(t, custom, "old-scheme prompts are discarded rather than guessed at")

	// A prompt set after the migration is untouched.
	require.NoError(t, service.SaveAgent(db, &model.Agent{
		ID: "pi2", Name: "Pi2", Backend: "pi", CustomSystemPrompt: "keep me",
	}))
	require.NoError(t, service.MigrateLegacyAgentPrompts())
	require.NoError(t, db.QueryRow(
		"SELECT custom_system_prompt FROM agents WHERE id='pi2'",
	).Scan(&custom))
	assert.Equal(t, "keep me", custom)
}
