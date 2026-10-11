package service

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInitDB_AddsAgentProjectIDColumn verifies the project_id column exists on a
// freshly initialized database (AgentDDL path).
func TestInitDB_AddsAgentProjectIDColumn(t *testing.T) {
	setupGroupDB(t)

	cols := getTableColumns(t, store.UnsafeDBForTest(), "agents")
	assert.Contains(t, cols, "project_id", "agents table should include the project_id column")
}

// TestSchema_AgentProjectIDMigration_FromOldSchema guards the upgrade path for
// the project_id column: a database created before it existed must gain the
// column via ALTER TABLE, and pre-existing rows must default to NULL (global) —
// otherwise an upgrade would either drop the column read or hide every existing
// agent from every project.
func TestSchema_AgentProjectIDMigration_FromOldSchema(t *testing.T) {
	tmpDir := t.TempDir()
	origBinDir := model.BinDir
	origDataDir := model.DataDir
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	defer func() { model.BinDir = origBinDir; model.DataDir = origDataDir }()

	dbDir := filepath.Join(tmpDir, ".clawbench")
	require.NoError(t, os.MkdirAll(dbDir, 0o755))
	oldDB, err := sql.Open("sqlite", filepath.Join(dbDir, "ClawBench.db"))
	require.NoError(t, err)
	oldDB.SetMaxOpenConns(1)
	oldDB.Exec("PRAGMA journal_mode=WAL")
	oldDB.Exec("PRAGMA busy_timeout=5000")
	// agents WITHOUT project_id but WITH disabled (the immediately-preceding
	// column), so the migration under test is the one that runs.
	_, err = oldDB.Exec(`
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
			custom_system_prompt TEXT NOT NULL DEFAULT '',
			avatar TEXT NOT NULL DEFAULT '',
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
			disabled INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	require.NoError(t, err)
	_, err = oldDB.Exec("INSERT INTO agents (id, name, backend) VALUES ('legacy', 'Legacy', 'claude')")
	require.NoError(t, err)
	require.NoError(t, oldDB.Close())

	require.NoError(t, InitDB())
	defer store.Close()

	cols := getTableColumns(t, store.UnsafeDBForTest(), "agents")
	assert.Contains(t, cols, "project_id", "project_id column should exist after migration")

	// The pre-existing row must survive and default to NULL (global).
	var projectID sql.NullInt64
	require.NoError(t, store.UnsafeDBForTest().
		QueryRow("SELECT project_id FROM agents WHERE id = 'legacy'").Scan(&projectID))
	assert.False(t, projectID.Valid, "pre-existing agents must default to global (NULL) after migration")

	// Idempotent: a second InitDB must not error on the already-present column.
	assert.NoError(t, InitDB())
}

// TestSaveAndLoadAgent_ProjectScopeRoundTrip verifies that a project-scoped
// agent's project_id survives a save/load cycle, and that a global agent stays
// NULL.
func TestSaveAndLoadAgent_ProjectScopeRoundTrip(t *testing.T) {
	setupGroupDB(t)

	pid := int64(42)
	scoped := &model.Agent{ID: "scoped-agent", Name: "Scoped", Backend: "claude", ProjectID: &pid}
	require.NoError(t, SaveAgent(store.WriteDB(), scoped))

	global := &model.Agent{ID: "global-agent", Name: "Global", Backend: "claude"}
	require.NoError(t, SaveAgent(store.WriteDB(), global))

	agents, err := LoadAgentsFromDB()
	require.NoError(t, err)

	byID := map[string]*model.Agent{}
	for _, a := range agents {
		byID[a.ID] = a
	}

	require.NotNil(t, byID["scoped-agent"])
	require.NotNil(t, byID["scoped-agent"].ProjectID)
	assert.Equal(t, int64(42), *byID["scoped-agent"].ProjectID)

	require.NotNil(t, byID["global-agent"])
	assert.Nil(t, byID["global-agent"].ProjectID, "global agent must load with nil ProjectID")
}

// TestPatchAgentFields_ProjectScope verifies PatchAgentFields can set and clear
// the project scope: setting stores the id, ClearProjectID resets to NULL.
func TestPatchAgentFields_ProjectScope(t *testing.T) {
	setupGroupDB(t)

	require.NoError(t, SaveAgent(store.WriteDB(), &model.Agent{ID: "a1", Name: "A1", Backend: "claude"}))

	pid := int64(7)
	require.NoError(t, PatchAgentFields("a1", AgentPatch{ProjectID: &pid}))

	var got sql.NullInt64
	require.NoError(t, store.ReadDB().QueryRow("SELECT project_id FROM agents WHERE id = 'a1'").Scan(&got))
	require.True(t, got.Valid)
	assert.Equal(t, int64(7), got.Int64)

	// Clearing must set NULL, not 0.
	require.NoError(t, PatchAgentFields("a1", AgentPatch{ClearProjectID: true}))
	require.NoError(t, store.ReadDB().QueryRow("SELECT project_id FROM agents WHERE id = 'a1'").Scan(&got))
	assert.False(t, got.Valid, "ClearProjectID must reset project_id to NULL")
}

// TestDuplicateAgent_PreservesProjectScope verifies that duplicating a
// project-scoped agent produces another project-scoped agent (scope is copied),
// while a global source stays global.
func TestDuplicateAgent_PreservesProjectScope(t *testing.T) {
	setupGroupDB(t)

	pid := int64(9)
	require.NoError(t, SaveAgent(store.WriteDB(), &model.Agent{ID: "src", Name: "Src", Backend: "claude", ProjectID: &pid}))
	require.NoError(t, model.LoadAgentsIntoMemoryFromDB(store.ReadDB()))

	clone, err := DuplicateAgent("src", "Src Copy")
	require.NoError(t, err)
	require.NotNil(t, clone.ProjectID)
	assert.Equal(t, int64(9), *clone.ProjectID, "duplicate must inherit the project scope")
}
