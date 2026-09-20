package service_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/service"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// conversationProjectsSchema is the minimum slice of the real schema the query
// touches: both chat_sessions and the chat_metadata ledger contribute paths.
const conversationProjectsSchema = `
CREATE TABLE IF NOT EXISTS chat_sessions (
	id TEXT PRIMARY KEY,
	project_path TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS chat_metadata (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_path TEXT DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

func setupConversationProjectsDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	// A :memory: database is per-connection, so the pool must be pinned or a
	// second pooled connection sees an empty database (see recent_projects_test).
	db.SetMaxOpenConns(1)

	_, err = db.Exec(conversationProjectsSchema)
	require.NoError(t, err)

	cleanup := service.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		db.Close()
	})
	return db
}

func insertSessionRow(t *testing.T, db *sql.DB, projectPath, id, createdAt string) {
	t.Helper()
	_, err := db.Exec(
		"INSERT INTO chat_sessions (id, project_path, created_at) VALUES (?, ?, ?)",
		id, projectPath, createdAt,
	)
	require.NoError(t, err)
}

func insertMetadataRow(t *testing.T, db *sql.DB, projectPath, createdAt string) {
	t.Helper()
	_, err := db.Exec(
		"INSERT INTO chat_metadata (project_path, created_at) VALUES (?, ?)",
		projectPath, createdAt,
	)
	require.NoError(t, err)
}

func TestGetConversationProjects_Empty(t *testing.T) {
	setupConversationProjectsDB(t)

	projects, err := service.GetConversationProjects()
	require.NoError(t, err)
	assert.Empty(t, projects)
}

// TestGetConversationProjects_ListsSessionsNewestFirst pins ordering and the
// exists flag for a live directory.
func TestGetConversationProjects_ListsSessionsNewestFirst(t *testing.T) {
	db := setupConversationProjectsDB(t)

	older := t.TempDir()
	newer := t.TempDir()
	insertSessionRow(t, db, older, "s1", "2024-01-01 10:00:00")
	insertSessionRow(t, db, newer, "s2", "2024-03-01 10:00:00")

	projects, err := service.GetConversationProjects()
	require.NoError(t, err)
	require.Len(t, projects, 2)
	assert.Equal(t, newer, projects[0].Path, "newest activity must come first")
	assert.Equal(t, older, projects[1].Path)
	assert.True(t, projects[0].Exists, "a live directory must be flagged as existing")
}

// TestGetConversationProjects_KeepsDeletedDirectories is the point of the
// endpoint: a project whose directory is gone must still be listed so its
// history stays discoverable — unlike /api/recent-projects, which purges it.
func TestGetConversationProjects_KeepsDeletedDirectories(t *testing.T) {
	db := setupConversationProjectsDB(t)

	dir := t.TempDir()
	insertSessionRow(t, db, dir, "s1", "2024-01-01 10:00:00")
	require.NoError(t, os.RemoveAll(dir))

	projects, err := service.GetConversationProjects()
	require.NoError(t, err)
	require.Len(t, projects, 1, "a deleted project must still be listed")
	assert.Equal(t, dir, projects[0].Path)
	assert.False(t, projects[0].Exists, "a deleted directory must be flagged as missing")
}

// TestGetConversationProjects_IncludesLedgerOnlyProjects covers a project whose
// sessions were hard-deleted but whose metadata ledger rows remain: the union
// must surface it, otherwise old usage becomes unreachable.
func TestGetConversationProjects_IncludesLedgerOnlyProjects(t *testing.T) {
	db := setupConversationProjectsDB(t)

	ledgerOnly := filepath.Join(t.TempDir(), "ledger-only")
	insertMetadataRow(t, db, ledgerOnly, "2024-05-01 10:00:00")

	projects, err := service.GetConversationProjects()
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, ledgerOnly, projects[0].Path)
}

// TestGetConversationProjects_DeduplicatesAcrossSources asserts a project
// present in both chat_sessions and chat_metadata appears exactly once.
func TestGetConversationProjects_DeduplicatesAcrossSources(t *testing.T) {
	db := setupConversationProjectsDB(t)

	dir := t.TempDir()
	insertSessionRow(t, db, dir, "s1", "2024-01-01 10:00:00")
	insertMetadataRow(t, db, dir, "2024-01-02 10:00:00")
	insertMetadataRow(t, db, dir, "2024-01-03 10:00:00")

	projects, err := service.GetConversationProjects()
	require.NoError(t, err)
	require.Len(t, projects, 1, "the same path must not be listed twice")
	assert.Equal(t, 3, projects[0].SessionCount, "every recorded row counts toward the total")
	assert.Equal(t, "2024-01-03 10:00:00", projects[0].LastActiveAt,
		"last activity is the newest row across both sources")
}

// TestGetConversationProjects_SkipsEmptyPaths guards against the blank
// project_path rows that legacy metadata can carry (the column defaults to ”).
func TestGetConversationProjects_SkipsEmptyPaths(t *testing.T) {
	db := setupConversationProjectsDB(t)

	insertMetadataRow(t, db, "", "2024-01-01 10:00:00")

	projects, err := service.GetConversationProjects()
	require.NoError(t, err)
	assert.Empty(t, projects, "an empty project_path is not a project")
}
