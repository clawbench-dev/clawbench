package model

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
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

// withBackendSpec registers an extra BackendSpec for the duration of a test.
//
// The registry is built lazily by a sync.Once, so appending before the first
// GetBackendRegistry() call would be discarded when the once fires. Calling
// GetBackendRegistry() first forces initialization, making the append reliable
// regardless of which test ran before.
func withBackendSpec(t *testing.T, spec BackendSpec) {
	t.Helper()
	orig := GetBackendRegistry()
	BackendRegistry = append(append([]BackendSpec{}, orig...), spec)
	t.Cleanup(func() { BackendRegistry = orig })
}

// ---------------------------------------------------------------------------
// RefreshAgents — the single pipeline entry point
// ---------------------------------------------------------------------------

// The registry is loaded once per process (sync.Once), so a test cannot append to
// it reliably. Seed the DB directly instead and assert on the derived map, which
// is what RefreshAgents actually reports.
func TestRefreshAgents_ReportsPresentCLIs(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	isolateAgentGlobals(t)

	// A backend that only exists in the database still counts as present.
	_, err := db.Exec(`INSERT INTO agents (id, name, backend) VALUES ('known', 'Known', 'db-only-backend')`)
	require.NoError(t, err)

	result, err := RefreshAgents(db, RefreshOptions{SkipDiscovery: true})
	require.NoError(t, err)

	assert.True(t, result.PresentCLIs["db-only-backend"],
		"a backend with a DB record must be reported present even without a registry entry")
}

func TestRefreshAgents_InsertsAgentsForPresentBackends(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	isolateAgentGlobals(t)

	// Register a backend whose "CLI" always exists, with a distinctive ID.
	const backendID = "refresh-test-present"
	restore := isolateModelSources(t)
	defer restore()
	RegisterModelSource(StaticSource(backendID, "", []AgentModel{{ID: "m1", Name: "M1"}}))

	withBackendSpec(t, BackendSpec{ID: backendID, Backend: backendID, DefaultCmd: "definitely-not-a-real-cli-xyz", Name: "Refresh Test", NoCLI: true})

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
	withBackendSpec(t, BackendSpec{ID: "dup", Backend: "dup-backend", DefaultCmd: "definitely-not-a-real-cli-xyz", Name: "Spec Name", NoCLI: true})

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
	withBackendSpec(t, BackendSpec{ID: "acp", Backend: "acp-backend", DefaultCmd: "definitely-not-a-real-cli-xyz", Name: "ACP", AcpCommand: "acp-server --stdio"})

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
	// No AcpCommand: the spec withdraws ACP for this backend.
	withBackendSpec(t, BackendSpec{ID: "gone", Backend: "gone-backend", DefaultCmd: "definitely-not-a-real-cli-xyz", Name: "Gone", NoCLI: true})

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

	assert.Contains(t, result.ModelRefreshedBackends, "disc")

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
	assert.Contains(t, agent.RuntimeSystemPrompt, "be terse", "custom prompt must be composed into the runtime prompt")
	assert.Contains(t, agent.RuntimeSystemPrompt, "User Interaction", "the shared prompt prefix must be present")
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

// ComposeSystemPrompt must always apply the *current* shared prompt, so a
// change to the built-in text takes effect on every load. The bug this guards
// against: a composed prompt persisted by an older scheme was appended after the
// fresh one and overrode it, making every built-in-prompt change a no-op on
// existing installs.
func TestComposeSystemPrompt(t *testing.T) {
	common := BuildCommonPrompt()
	require.NotEmpty(t, common)

	assert.Equal(t, common, ComposeSystemPrompt(""), "no custom text yields the shared prompt alone")
	assert.Equal(t, common+"\n\nmy instructions", ComposeSystemPrompt("my instructions"))

	// The shared prompt appears exactly once — no frozen copy can be appended.
	assert.Equal(t, 1, strings.Count(ComposeSystemPrompt("my instructions"), common))

	// The custom text is always present.
	assert.Contains(t, ComposeSystemPrompt("my instructions"), "my instructions")
}

// The shared prompt is composed on every read, so changing the built-in prompt
// takes effect immediately. The bug this guards against: a composed prompt
// persisted by an older scheme was appended after the fresh one and overrode it,
// which made every built-in prompt change a no-op on existing installs.
func TestComposeSystemPrompt_SharedPromptIsAlwaysFresh(t *testing.T) {
	commonPrompt := BuildCommonPrompt()
	require.NotEmpty(t, commonPrompt)

	// A user with no custom text gets exactly the current shared prompt.
	assert.Equal(t, commonPrompt, ComposeSystemPrompt(""))

	// A user with custom text gets the current shared prompt plus their text —
	// never a previously stored composition.
	got := ComposeSystemPrompt("my instructions")
	assert.Contains(t, got, "my instructions")
	assert.NotContains(t, got, "OLD-COMMON")
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

// ---------------------------------------------------------------------------
// Regression: a freshly inserted agent must receive discovered models
// ---------------------------------------------------------------------------

// A brand-new install inserts agents in step 1 and discovers models in step 3 of
// the same refresh. The discovery write must therefore also match rows that have
// an empty list and are not yet flagged auto-managed, or the user sees an empty
// model picker until they manually refresh.
//
// The row is seeded directly rather than through CLI detection: detection depends
// on what is installed on the machine, and the bug under test is in the
// persistence predicate, not in detection.
func TestRefreshAgents_FreshInsertReceivesDiscoveredModels(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	isolateAgentGlobals(t)

	const backendID = "fresh-install"
	// This is exactly the row shape step 1 writes for a newly detected backend:
	// empty models, flag 0 (saveAgentToDB leaves ModelsAutoDetected false).
	_, err := db.Exec(`INSERT INTO agents (id, name, backend, models, models_auto_detected)
		VALUES (?, 'Fresh Install', ?, '[]', 0)`, backendID, backendID)
	require.NoError(t, err)

	restore := isolateModelSources(t)
	defer restore()
	RegisterModelSource(StaticSource(backendID, "", []AgentModel{
		{ID: "fresh-1", Name: "Fresh 1", Default: true},
		{ID: "fresh-2", Name: "Fresh 2"},
	}))

	_, err = RefreshAgents(db, RefreshOptions{})
	require.NoError(t, err)

	var modelsJSON string
	require.NoError(t, db.QueryRow("SELECT models FROM agents WHERE id = ?", backendID).Scan(&modelsJSON))
	assert.Contains(t, modelsJSON, "fresh-1", "a newly inserted agent must get the models discovered in the same refresh")
	assert.Contains(t, modelsJSON, "fresh-2")

	require.Contains(t, Agents, backendID)
	assert.NotEmpty(t, Agents[backendID].Models, "the in-memory agent must expose the discovered models")
}

// An empty list with flag 0 carries no user intent, so discovery may fill it.
// This is the same row shape as a freshly inserted agent; kept separate because
// it documents the predicate's other arm explicitly.
func TestRefreshAgents_EmptyListWithFlagZeroIsFilled(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	isolateAgentGlobals(t)

	// An empty list with flag 0 carries no user intent — it is the state of a
	// row nobody has populated yet, so discovery may fill it.
	_, err := db.Exec(`INSERT INTO agents (id, name, backend, models, models_auto_detected)
		VALUES ('empty-flag0', 'Empty', 'fill-me', '[]', 0)`)
	require.NoError(t, err)

	restore := isolateModelSources(t)
	defer restore()
	RegisterModelSource(StaticSource("fill-me", "", []AgentModel{{ID: "found", Name: "Found"}}))

	_, err = RefreshAgents(db, RefreshOptions{})
	require.NoError(t, err)

	var modelsJSON string
	require.NoError(t, db.QueryRow("SELECT models FROM agents WHERE id = 'empty-flag0'").Scan(&modelsJSON))
	assert.Contains(t, modelsJSON, "found")
}

// Thinking effort levels are spec-derived and not user-editable, so a spec that
// gains a level must reach the database. The in-memory fallback only fills an
// EMPTY list, so without the write the new level would never appear.
func TestRefreshAgents_SyncsThinkingEffortLevels(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	isolateAgentGlobals(t)

	// An agent with a stale, non-empty level list.
	_, err := db.Exec(`INSERT INTO agents (id, name, backend, thinking_effort_levels)
		VALUES ('levels', 'Levels', 'levels-backend', '["low"]')`)
	require.NoError(t, err)

	withBackendSpec(t, BackendSpec{
		ID: "levels", Backend: "levels-backend", DefaultCmd: "definitely-not-a-real-cli-xyz",
		Name: "Levels", NoCLI: true,
		ThinkingEffortLevels: []string{"low", "medium", "high"},
	})

	_, err = RefreshAgents(db, RefreshOptions{SkipDiscovery: true})
	require.NoError(t, err)

	var levelsJSON string
	require.NoError(t, db.QueryRow("SELECT thinking_effort_levels FROM agents WHERE id = 'levels'").Scan(&levelsJSON))
	assert.Contains(t, levelsJSON, "high", "the spec's new level must reach the DB")

	require.Contains(t, Agents, "levels")
	assert.Equal(t, []string{"low", "medium", "high"}, Agents["levels"].ThinkingEffortLevels)
}

// A backend with no spec-declared levels must not have its column clobbered.
func TestRefreshAgents_DoesNotClearLevelsWithoutSpec(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	isolateAgentGlobals(t)

	_, err := db.Exec(`INSERT INTO agents (id, name, backend, thinking_effort_levels)
		VALUES ('keep', 'Keep', 'no-spec-backend', '["xhigh"]')`)
	require.NoError(t, err)

	_, err = RefreshAgents(db, RefreshOptions{SkipDiscovery: true})
	require.NoError(t, err)

	var levelsJSON string
	require.NoError(t, db.QueryRow("SELECT thinking_effort_levels FROM agents WHERE id = 'keep'").Scan(&levelsJSON))
	assert.Contains(t, levelsJSON, "xhigh", "an absent spec must not wipe the stored levels")
}
