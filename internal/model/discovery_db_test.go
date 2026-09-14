package model

import (
	"database/sql"
	"os"
	"path/filepath"
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
	// A single connection: an in-memory SQLite database is per-connection, so
	// allowing a second one would silently give queries an empty schema.
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
			preferred_mode TEXT NOT NULL DEFAULT '',
			preferred_model TEXT NOT NULL DEFAULT '',
			preferred_thinking_effort TEXT NOT NULL DEFAULT '',
			system_prompt TEXT NOT NULL DEFAULT '',
			custom_system_prompt TEXT NOT NULL DEFAULT '',
			models TEXT NOT NULL DEFAULT '[]',
			models_auto_detected INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			transport TEXT NOT NULL DEFAULT 'cli',
			acp_command TEXT NOT NULL DEFAULT '',
			acp_available_modes TEXT NOT NULL DEFAULT '[]',
			acp_available_thinking_efforts TEXT NOT NULL DEFAULT '[]',
			acp_available_commands TEXT NOT NULL DEFAULT '[]',
			acp_available_models TEXT NOT NULL DEFAULT '[]',
			acp_config_options TEXT NOT NULL DEFAULT '',
			auto_approve INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_agents_backend ON agents(backend);
		CREATE INDEX IF NOT EXISTS idx_agents_sort ON agents(sort_order);
	`)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

// isolateAgentGlobals saves and clears the package-level agent state, restoring
// it after the test.
func isolateAgentGlobals(t *testing.T) {
	t.Helper()
	origAgents := Agents
	origList := AgentList
	t.Cleanup(func() {
		Agents = origAgents
		AgentList = origList
	})
	Agents = make(map[string]*Agent)
	AgentList = nil
}

// ---------------------------------------------------------------------------
// RefreshAgents — the single pipeline entry point
// ---------------------------------------------------------------------------

func TestRefreshAgents_ReportsPresentCLIs(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	isolateAgentGlobals(t)

	result, err := RefreshAgents(db, RefreshOptions{SkipDiscovery: true})
	require.NoError(t, err)

	require.NotNil(t, result.PresentCLIs)
	// Any CLI installed on this machine must be reported present. `ls` is not a
	// backend, so assert the shape rather than specific backends.
	for backend := range result.PresentCLIs {
		assert.NotEmpty(t, backend)
	}
}

func TestRefreshAgents_InsertsAgentsForPresentBackends(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	isolateAgentGlobals(t)

	// Register a backend whose "CLI" always exists, with a distinctive ID.
	const backendID = "refresh-test-present"
	restore := isolateModelSources(t)
	defer restore()
	RegisterModelSource(StaticSource(backendID, "", []AgentModel{{ID: "m1", Name: "M1"}}))

	origRegistry := BackendRegistry
	BackendRegistry = append(append([]BackendSpec{}, origRegistry...), BackendSpec{
		ID: backendID, Backend: backendID, DefaultCmd: "definitely-not-a-real-cli-xyz",
		Name: "Refresh Test", NoCLI: true,
	})
	t.Cleanup(func() { BackendRegistry = origRegistry })

	result, err := RefreshAgents(db, RefreshOptions{SkipDiscovery: true})
	require.NoError(t, err)

	assert.Contains(t, result.InsertedAgents, backendID)

	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM agents WHERE id = ?", backendID).Scan(&count))
	assert.Equal(t, 1, count)
}

func TestRefreshAgents_DoesNotDuplicateExistingAgents(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	isolateAgentGlobals(t)

	_, err := db.Exec(`INSERT INTO agents (id, name, backend, command) VALUES ('dup', 'Custom Name', 'dup-backend', '/my/own/path')`)
	require.NoError(t, err)

	restore := isolateModelSources(t)
	defer restore()
	origRegistry := BackendRegistry
	BackendRegistry = append(append([]BackendSpec{}, origRegistry...), BackendSpec{
		ID: "dup", Backend: "dup-backend", DefaultCmd: "definitely-not-a-real-cli-xyz",
		Name: "Spec Name", NoCLI: true,
	})
	t.Cleanup(func() { BackendRegistry = origRegistry })

	result, err := RefreshAgents(db, RefreshOptions{SkipDiscovery: true})
	require.NoError(t, err)

	assert.NotContains(t, result.InsertedAgents, "dup")

	var name, command string
	require.NoError(t, db.QueryRow("SELECT name, command FROM agents WHERE id = 'dup'").Scan(&name, &command))
	assert.Equal(t, "Custom Name", name, "a user-renamed agent must not be renamed back")
	assert.Equal(t, "/my/own/path", command, "a user-set command must not be overwritten")
}

func TestRefreshAgents_SyncsACPCommandFromSpec(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	isolateAgentGlobals(t)

	_, err := db.Exec(`INSERT INTO agents (id, name, backend, transport, acp_command) VALUES ('acp', 'ACP', 'acp-backend', 'cli', '')`)
	require.NoError(t, err)

	restore := isolateModelSources(t)
	defer restore()
	origRegistry := BackendRegistry
	BackendRegistry = append(append([]BackendSpec{}, origRegistry...), BackendSpec{
		ID: "acp", Backend: "acp-backend", DefaultCmd: "definitely-not-a-real-cli-xyz",
		Name: "ACP", AcpCommand: "acp-server --stdio",
	})
	t.Cleanup(func() { BackendRegistry = origRegistry })

	_, err = RefreshAgents(db, RefreshOptions{SkipDiscovery: true})
	require.NoError(t, err)

	var acpCommand, transport string
	require.NoError(t, db.QueryRow("SELECT acp_command, transport FROM agents WHERE id = 'acp'").Scan(&acpCommand, &transport))
	assert.Equal(t, "acp-server --stdio", acpCommand, "a newly available ACP command must be recorded")
	assert.Equal(t, "acp-stdio", transport, "transport must follow the ACP command")
}

func TestRefreshAgents_ClearsRemovedACPCommand(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	isolateAgentGlobals(t)

	_, err := db.Exec(`INSERT INTO agents (id, name, backend, transport, acp_command) VALUES ('gone', 'Gone', 'gone-backend', 'acp-stdio', 'old-acp')`)
	require.NoError(t, err)

	restore := isolateModelSources(t)
	defer restore()
	origRegistry := BackendRegistry
	BackendRegistry = append(append([]BackendSpec{}, origRegistry...), BackendSpec{
		ID: "gone", Backend: "gone-backend", DefaultCmd: "definitely-not-a-real-cli-xyz",
		Name: "Gone", NoCLI: true, // no AcpCommand
	})
	t.Cleanup(func() { BackendRegistry = origRegistry })

	_, err = RefreshAgents(db, RefreshOptions{SkipDiscovery: true})
	require.NoError(t, err)

	var acpCommand, transport string
	require.NoError(t, db.QueryRow("SELECT acp_command, transport FROM agents WHERE id = 'gone'").Scan(&acpCommand, &transport))
	assert.Empty(t, acpCommand, "a removed ACP command must be cleared, not left stale")
	assert.Equal(t, "cli", transport, "transport must fall back when ACP is withdrawn")
}

func TestRefreshAgents_PersistsDiscoveredModelsForAutoManagedAgents(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	isolateAgentGlobals(t)

	// models_auto_detected = 1 means "discovery owns this list".
	_, err := db.Exec(`INSERT INTO agents (id, name, backend, models, models_auto_detected) VALUES ('auto', 'Auto', 'disc', '[]', 1)`)
	require.NoError(t, err)

	restore := isolateModelSources(t)
	defer restore()
	RegisterModelSource(StaticSource("disc", "", []AgentModel{
		{ID: "model-1", Name: "Model 1", Default: true},
		{ID: "model-2", Name: "Model 2"},
	}))

	result, err := RefreshAgents(db, RefreshOptions{})
	require.NoError(t, err)

	assert.Contains(t, result.UpdatedAgents, "disc")

	var modelsJSON string
	require.NoError(t, db.QueryRow("SELECT models FROM agents WHERE id = 'auto'").Scan(&modelsJSON))
	assert.Contains(t, modelsJSON, "model-1")
	assert.Contains(t, modelsJSON, "model-2")
}

func TestRefreshAgents_DoesNotOverwriteUserModels(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	isolateAgentGlobals(t)

	// models_auto_detected = 0 with a non-empty list means the user chose these.
	_, err := db.Exec(`INSERT INTO agents (id, name, backend, models, models_auto_detected)
		VALUES ('user', 'User', 'disc2', '[{"id":"my-model","name":"My Model","default":true}]', 0)`)
	require.NoError(t, err)

	restore := isolateModelSources(t)
	defer restore()
	RegisterModelSource(StaticSource("disc2", "", []AgentModel{{ID: "discovered", Name: "Discovered"}}))

	_, err = RefreshAgents(db, RefreshOptions{})
	require.NoError(t, err)

	var modelsJSON string
	require.NoError(t, db.QueryRow("SELECT models FROM agents WHERE id = 'user'").Scan(&modelsJSON))
	assert.Contains(t, modelsJSON, "my-model")
	assert.NotContains(t, modelsJSON, "discovered", "a user-defined model list must never be replaced by discovery")
}

func TestRefreshAgents_SkipDiscoveryLeavesModelsAlone(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	isolateAgentGlobals(t)

	_, err := db.Exec(`INSERT INTO agents (id, name, backend, models, models_auto_detected) VALUES ('auto', 'Auto', 'disc3', '[]', 1)`)
	require.NoError(t, err)

	restore := isolateModelSources(t)
	defer restore()
	RegisterModelSource(StaticSource("disc3", "", []AgentModel{{ID: "model-x", Name: "Model X"}}))

	_, err = RefreshAgents(db, RefreshOptions{SkipDiscovery: true})
	require.NoError(t, err)

	var modelsJSON string
	require.NoError(t, db.QueryRow("SELECT models FROM agents WHERE id = 'auto'").Scan(&modelsJSON))
	assert.Equal(t, "[]", modelsJSON, "SkipDiscovery must not probe or persist models")
}

func TestRefreshAgents_LoadsAgentsIntoMemory(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	isolateAgentGlobals(t)

	_, err := db.Exec(`INSERT INTO agents (id, name, backend, custom_system_prompt, thinking_effort_levels)
		VALUES ('mem', 'Memory', 'mem-backend', 'be terse', '["low","high"]')`)
	require.NoError(t, err)

	result, err := RefreshAgents(db, RefreshOptions{SkipDiscovery: true})
	require.NoError(t, err)

	assert.Equal(t, len(AgentList), result.LoadedAgents)
	require.Contains(t, Agents, "mem")

	agent := Agents["mem"]
	assert.Contains(t, agent.SystemPrompt, "be terse", "custom prompt must be composed into the runtime prompt")
	assert.Contains(t, agent.SystemPrompt, "User Interaction", "the shared prompt prefix must be present")
}

func TestRefreshAgents_LoadsYamlAgents(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	isolateAgentGlobals(t)

	configDir := t.TempDir()
	agentsDir := filepath.Join(configDir, "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "custom.yaml"), []byte(`
id: yaml-agent
name: YAML Agent
backend: yaml-backend
acp_command: npx -y yaml-acp
models:
  - id: yaml-model
    name: YAML Model
    default: true
`), 0o644))

	result, err := RefreshAgents(db, RefreshOptions{ConfigDir: configDir, SkipDiscovery: true})
	require.NoError(t, err)

	assert.Contains(t, result.InsertedAgents, "yaml-agent")
	require.Contains(t, Agents, "yaml-agent")
	require.Len(t, Agents["yaml-agent"].Models, 1)
	assert.Equal(t, "yaml-model", Agents["yaml-agent"].Models[0].ID)
	assert.Equal(t, "YAML Model", Agents["yaml-agent"].Models[0].Name)
}

func TestRefreshAgents_YamlAgentWithoutModelsOptsIntoDiscovery(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	isolateAgentGlobals(t)

	configDir := t.TempDir()
	agentsDir := filepath.Join(configDir, "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "nomodels.yaml"), []byte(`
id: yaml-nomodels
name: No Models
backend: yaml-nm-backend
`), 0o644))

	_, err := RefreshAgents(db, RefreshOptions{ConfigDir: configDir, SkipDiscovery: true})
	require.NoError(t, err)

	var autoDetected int
	require.NoError(t, db.QueryRow("SELECT models_auto_detected FROM agents WHERE id = 'yaml-nomodels'").Scan(&autoDetected))
	assert.Equal(t, 1, autoDetected, "a YAML agent that declares no models should be discoverable")
}

// ---------------------------------------------------------------------------
// YAML loading details
// ---------------------------------------------------------------------------

func TestLoadYamlAgents_SkipsInvalidFiles(t *testing.T) {
	db := setupTestDBForDiscovery(t)

	agentsDir := filepath.Join(t.TempDir(), "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "bad.yaml"), []byte("not: [valid"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "noid.yaml"), []byte("name: No ID\nbackend: x\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "ignored.txt"), []byte("id: x\nbackend: y\n"), 0o644))

	inserted := LoadYamlAgents(db, filepath.Dir(agentsDir))

	assert.Empty(t, inserted, "malformed, id-less and non-YAML files must all be skipped")
}

func TestLoadYamlAgents_NeverOverwritesExisting(t *testing.T) {
	db := setupTestDBForDiscovery(t)

	_, err := db.Exec(`INSERT INTO agents (id, name, backend) VALUES ('yaml-existing', 'User Name', 'b')`)
	require.NoError(t, err)

	agentsDir := filepath.Join(t.TempDir(), "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "existing.yaml"), []byte("id: yaml-existing\nname: From YAML\nbackend: b\n"), 0o644))

	inserted := LoadYamlAgents(db, filepath.Dir(agentsDir))

	assert.Empty(t, inserted)
	var name string
	require.NoError(t, db.QueryRow("SELECT name FROM agents WHERE id = 'yaml-existing'").Scan(&name))
	assert.Equal(t, "User Name", name, "YAML is a bootstrap, not a sync source")
}

func TestLoadYamlAgents_MissingDirectoryIsNotAnError(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	assert.Empty(t, LoadYamlAgents(db, t.TempDir()))
}

// ---------------------------------------------------------------------------
// System prompt composition
// ---------------------------------------------------------------------------

func TestComposeSystemPrompt(t *testing.T) {
	tests := []struct {
		name   string
		common string
		custom string
		stored string
		want   string
	}{
		{"common and custom", "COMMON", "custom", "", "COMMON\n\ncustom"},
		{"common only", "COMMON", "", "", "COMMON"},
		{"custom only", "", "custom", "", "custom"},
		{"neither", "", "", "", ""},
		{
			name:   "legacy stored prompt survives an empty custom field",
			common: "COMMON", custom: "", stored: "legacy text",
			want: "legacy text",
		},
		{
			name:   "custom field wins over the legacy stored prompt",
			common: "COMMON", custom: "new", stored: "legacy text",
			want: "COMMON\n\nnew",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, composeSystemPrompt(tt.common, tt.custom, tt.stored))
		})
	}
}

// ---------------------------------------------------------------------------
// DB round-trip
// ---------------------------------------------------------------------------

func TestLoadAgentsFromDBRows_PreferredModeRoundTrip(t *testing.T) {
	db := setupTestDBForDiscovery(t)

	_, err := db.Exec(`INSERT INTO agents (id, name, backend, preferred_mode, preferred_thinking_effort, custom_system_prompt)
		VALUES ('claude', 'Claude', 'claude', 'code', 'high', 'my custom instructions')`)
	require.NoError(t, err)

	agents, err := loadAgentsFromDBRows(db)
	require.NoError(t, err)

	var found *Agent
	for _, a := range agents {
		if a.ID == "claude" {
			found = a
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, "code", found.PreferredMode)
	assert.Equal(t, "high", found.PreferredThinkingEffort)
	assert.Equal(t, "my custom instructions", found.CustomSystemPrompt)
}

func TestLoadAgentsFromDBRows_ToleratesEmptyJSONColumns(t *testing.T) {
	db := setupTestDBForDiscovery(t)

	_, err := db.Exec(`INSERT INTO agents (id, name, backend, models, thinking_effort_levels)
		VALUES ('empty', 'Empty', 'b', '', '')`)
	require.NoError(t, err)

	agents, err := loadAgentsFromDBRows(db)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Nil(t, agents[0].Models)
	assert.Nil(t, agents[0].ThinkingEffortLevels)
}

// ---------------------------------------------------------------------------
// Infrastructure sync helper
// ---------------------------------------------------------------------------

func TestSyncAgentInfrastructure_NoChangeReturnsFalse(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	_, err := db.Exec(`INSERT INTO agents (id, name, backend, transport, acp_command) VALUES ('same', 'Same', 'b', 'acp-stdio', 'cmd')`)
	require.NoError(t, err)

	updated := syncAgentInfrastructure(db, BackendSpec{Backend: "b", AcpCommand: "cmd"}, "cmd", "acp-stdio")

	assert.False(t, updated, "no drift means no write")
}

// ---------------------------------------------------------------------------
// RefreshAgents error and edge paths
// ---------------------------------------------------------------------------

func TestRefreshAgents_ReportsErrorOnBrokenDB(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	isolateAgentGlobals(t)

	// Drop the table so the final reload fails.
	_, err := db.Exec("DROP TABLE agents")
	require.NoError(t, err)

	_, err = RefreshAgents(db, RefreshOptions{SkipDiscovery: true})
	assert.Error(t, err, "a failed reload must surface as an error, not a silent empty agent set")
}

func TestRefreshAgents_MalformedYamlIsSkipped(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	isolateAgentGlobals(t)

	configDir := t.TempDir()
	agentsDir := filepath.Join(configDir, "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "bad.yaml"), []byte("not: [valid"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "good.yaml"), []byte(`
id: good-yaml
name: Good
backend: good-backend
`), 0o644))

	result, err := RefreshAgents(db, RefreshOptions{ConfigDir: configDir, SkipDiscovery: true})
	require.NoError(t, err)

	assert.Contains(t, result.InsertedAgents, "good-yaml", "a malformed sibling must not block valid YAML agents")
	assert.NotContains(t, result.InsertedAgents, "bad")
}

func TestDiscoverAndPersistModels_SkipsBackendWithNoModels(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	isolateAgentGlobals(t)

	_, err := db.Exec(`INSERT INTO agents (id, name, backend, models, models_auto_detected) VALUES ('a', 'A', 'empty-disc', '[]', 1)`)
	require.NoError(t, err)

	restore := isolateModelSources(t)
	defer restore()
	RegisterModelSource(StaticSource("empty-disc", "", nil)) // yields nothing

	_, updated := discoverAndPersistModels(db)

	assert.NotContains(t, updated, "empty-disc", "a backend that discovered nothing must not be reported as updated")
	var modelsJSON string
	require.NoError(t, db.QueryRow("SELECT models FROM agents WHERE id = 'a'").Scan(&modelsJSON))
	assert.Equal(t, "[]", modelsJSON, "the existing list must be left alone")
}

func TestDiscoverAndPersistModels_SkipsUserManagedAgents(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	isolateAgentGlobals(t)

	// models_auto_detected = 0: the user owns this list.
	_, err := db.Exec(`INSERT INTO agents (id, name, backend, models, models_auto_detected)
		VALUES ('u', 'U', 'disc-user', '[{"id":"mine","name":"Mine"}]', 0)`)
	require.NoError(t, err)

	restore := isolateModelSources(t)
	defer restore()
	RegisterModelSource(StaticSource("disc-user", "", []AgentModel{{ID: "theirs", Name: "Theirs"}}))

	_, updated := discoverAndPersistModels(db)

	assert.Empty(t, updated, "a user-managed agent must not be updated by discovery")
	var modelsJSON string
	require.NoError(t, db.QueryRow("SELECT models FROM agents WHERE id = 'u'").Scan(&modelsJSON))
	assert.Contains(t, modelsJSON, "mine")
	assert.NotContains(t, modelsJSON, "theirs")
}
