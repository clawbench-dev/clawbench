package service_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/service"
	"clawbench/internal/store"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// projectRegistrySchema is the minimum slice of the real schema ListAllProjects
// and GetProjectDetail touch: the registry plus chat_sessions.
const projectRegistrySchema = `
CREATE TABLE IF NOT EXISTS projects (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	path TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(path)
);
CREATE TABLE IF NOT EXISTS chat_sessions (
	id TEXT PRIMARY KEY,
	project_id INTEGER NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

func setupProjectRegistryDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	// A :memory: database is per-connection, so the pool must be pinned or a
	// second pooled connection sees an empty database.
	db.SetMaxOpenConns(1)

	_, err = db.Exec(projectRegistrySchema)
	require.NoError(t, err)

	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		db.Close()
	})
	return db
}

// registerProject inserts a projects row directly with an explicit created_at,
// so ordering-by-registration can be pinned without relying on CURRENT_TIMESTAMP.
func registerProject(t *testing.T, db *sql.DB, path, createdAt string) int64 {
	t.Helper()
	res, err := db.Exec(
		"INSERT INTO projects (path, created_at) VALUES (?, ?)",
		store.NormalizeProjectPath(path), createdAt,
	)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	store.ResetProjectIDCacheForTest()
	return id
}

func insertRegistrySession(t *testing.T, db *sql.DB, projectID int64, id, createdAt string) {
	t.Helper()
	_, err := db.Exec(
		"INSERT INTO chat_sessions (id, project_id, created_at) VALUES (?, ?, ?)",
		id, projectID, createdAt,
	)
	require.NoError(t, err)
}

func TestListAllProjects_IncludesProjectsWithoutSessions(t *testing.T) {
	db := setupProjectRegistryDB(t)
	registerProject(t, db, "/proj/a", "2026-01-01 00:00:00")
	registerProject(t, db, "/proj/b", "2026-01-02 00:00:00")

	items, err := service.ListAllProjects()
	require.NoError(t, err)
	require.Len(t, items, 2, "a project with no sessions must still be listed")

	for _, it := range items {
		assert.Equal(t, 0, it.SessionCount, "no sessions means count 0")
		assert.Empty(t, it.LastActiveAt, "no sessions means no last-active time")
	}
}

func TestListAllProjects_OrdersByLastActiveThenCreatedAt(t *testing.T) {
	db := setupProjectRegistryDB(t)

	// old-but-active: registered first, has the most recent session.
	oldActive := registerProject(t, db, "/proj/old-active", "2026-01-01 00:00:00")
	insertRegistrySession(t, db, oldActive, "s1", "2026-03-01 00:00:00")

	// new-but-idle: registered later, no sessions → falls back to created_at.
	registerProject(t, db, "/proj/new-idle", "2026-02-01 00:00:00")

	// recent-active: its session time beats everything.
	recentActive := registerProject(t, db, "/proj/recent-active", "2026-01-15 00:00:00")
	insertRegistrySession(t, db, recentActive, "s2", "2026-04-01 00:00:00")

	items, err := service.ListAllProjects()
	require.NoError(t, err)
	require.Len(t, items, 3)
	assert.Equal(t, "/proj/recent-active", items[0].Path, "most recent session first")
	assert.Equal(t, "/proj/old-active", items[1].Path, "second most recent session next")
	assert.Equal(t, "/proj/new-idle", items[2].Path,
		"idle project falls back to its created_at, which predates the other sessions' times here")
}

func TestListAllProjects_CountsOnlyCurrentSessions(t *testing.T) {
	db := setupProjectRegistryDB(t)
	id := registerProject(t, db, "/proj/count", "2026-01-01 00:00:00")
	insertRegistrySession(t, db, id, "s1", "2026-02-01 00:00:00")
	insertRegistrySession(t, db, id, "s2", "2026-03-01 00:00:00")
	insertRegistrySession(t, db, id, "s3", "2026-04-01 00:00:00")

	items, err := service.ListAllProjects()
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, 3, items[0].SessionCount)
	assert.Equal(t, "2026-04-01 00:00:00", items[0].LastActiveAt, "last active is the newest session")
}

func TestListAllProjects_ExistsReflectsDisk(t *testing.T) {
	db := setupProjectRegistryDB(t)
	dir := t.TempDir()
	registerProject(t, db, dir, "2026-01-01 00:00:00")
	registerProject(t, db, filepath.Join(dir, "gone"), "2026-01-02 00:00:00")

	items, err := service.ListAllProjects()
	require.NoError(t, err)
	require.Len(t, items, 2)

	byPath := map[string]bool{}
	for _, it := range items {
		byPath[it.Path] = it.Exists
	}
	assert.True(t, byPath[dir], "existing directory must be flagged exists")
	assert.False(t, byPath[filepath.Join(dir, "gone")], "missing directory must be flagged not-exists")
}

func TestGetProjectDetail_UnknownID(t *testing.T) {
	setupProjectRegistryDB(t)
	detail, err := service.GetProjectDetail(999)
	require.NoError(t, err)
	assert.Nil(t, detail, "an unknown id is (nil, nil), not an error")
}

func TestGetProjectDetail_GlobalSentinel(t *testing.T) {
	setupProjectRegistryDB(t)
	detail, err := service.GetProjectDetail(store.GlobalScopeProjectID)
	require.NoError(t, err)
	assert.Nil(t, detail)
}

func TestGetProjectDetail_DeletedDirReportsUnknownRepoKind(t *testing.T) {
	db := setupProjectRegistryDB(t)
	dir := t.TempDir()
	gone := filepath.Join(dir, "gone")
	id := registerProject(t, db, gone, "2026-01-01 00:00:00")
	require.NoError(t, os.RemoveAll(gone))

	detail, err := service.GetProjectDetail(id)
	require.NoError(t, err)
	require.NotNil(t, detail)
	assert.False(t, detail.Exists)
	assert.Equal(t, service.RepoKindUnknown, detail.RepoKind,
		"a deleted directory cannot be classified, and must not read as plain")
}

func TestGetProjectDetail_PlainDirectory(t *testing.T) {
	db := setupProjectRegistryDB(t)
	dir := t.TempDir()
	id := registerProject(t, db, dir, "2026-01-01 00:00:00")

	detail, err := service.GetProjectDetail(id)
	require.NoError(t, err)
	require.NotNil(t, detail)
	assert.True(t, detail.Exists)
	assert.Equal(t, string(service.RepoKindPlain), detail.RepoKind, "temp dir is not in a git repo")
}
