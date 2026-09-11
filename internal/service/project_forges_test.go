package service_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/forge"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupTestDBForProjectForges creates an in-memory SQLite with the
// project_forges table (mirrors the production DDL in database.go).
func setupTestDBForProjectForges(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)

	_, err = db.Exec(service.ProjectForgesDDL)
	require.NoError(t, err)

	cleanup := service.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})
	return db
}

func TestProjectForge_UpsertAndGet(t *testing.T) {
	setupTestDBForProjectForges(t)

	dir := t.TempDir()
	err := service.UpsertProjectForge(service.ProjectForge{
		ProjectPath: dir,
		Platform:    string(forge.PlatformGitHub),
		Host:        "github.com",
		Owner:       "acme",
		Repo:        "widgets",
		Source:      "auto",
	})
	require.NoError(t, err)

	got, err := service.GetProjectForge(dir)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, string(forge.PlatformGitHub), got.Platform)
	assert.Equal(t, "acme", got.Owner)
	assert.Equal(t, "widgets", got.Repo)
	assert.Equal(t, "acme/widgets", got.Slug())
}

func TestProjectForge_GetMissingReturnsNil(t *testing.T) {
	setupTestDBForProjectForges(t)
	got, err := service.GetProjectForge(t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, got, "absent binding must return nil, not an error")
}

func TestProjectForge_UpsertReplacesExisting(t *testing.T) {
	setupTestDBForProjectForges(t)
	dir := t.TempDir()

	require.NoError(t, service.UpsertProjectForge(service.ProjectForge{
		ProjectPath: dir, Platform: "github", Host: "github.com", Owner: "old", Repo: "repo", Source: "auto",
	}))
	require.NoError(t, service.UpsertProjectForge(service.ProjectForge{
		ProjectPath: dir, Platform: "gitlab", Host: "gitlab.com", Owner: "new", Repo: "repo", Source: "manual",
	}))

	got, err := service.GetProjectForge(dir)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "gitlab", got.Platform, "second upsert must replace the first")
	assert.Equal(t, "new", got.Owner)
	assert.Equal(t, "manual", got.Source)

	all, err := service.ListProjectForges()
	require.NoError(t, err)
	assert.Len(t, all, 1, "project_path is unique — no duplicate rows")
}

func TestProjectForge_NormalizesPath(t *testing.T) {
	setupTestDBForProjectForges(t)
	base := t.TempDir()
	// A trailing separator and a redundant "." must normalize to the same row.
	require.NoError(t, service.UpsertProjectForge(service.ProjectForge{
		ProjectPath: base, Platform: "github", Host: "github.com", Owner: "a", Repo: "b", Source: "auto",
	}))

	got, err := service.GetProjectForge(base + string(filepath.Separator))
	require.NoError(t, err)
	require.NotNil(t, got, "trailing separator must resolve to the same binding")

	got2, err := service.GetProjectForge(filepath.Join(base, "."))
	require.NoError(t, err)
	require.NotNil(t, got2, "redundant . must resolve to the same binding")
}

func TestProjectForge_NormalizesSymlink(t *testing.T) {
	setupTestDBForProjectForges(t)
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	require.NoError(t, service.UpsertProjectForge(service.ProjectForge{
		ProjectPath: real, Platform: "github", Host: "github.com", Owner: "a", Repo: "b", Source: "auto",
	}))

	// Looking up via the symlink must find the binding stored under the real path.
	got, err := service.GetProjectForge(link)
	require.NoError(t, err)
	require.NotNil(t, got, "symlink must resolve to the canonical project path")
}

func TestProjectForge_Validation(t *testing.T) {
	setupTestDBForProjectForges(t)

	assert.Error(t, service.UpsertProjectForge(service.ProjectForge{
		ProjectPath: "", Platform: "github", Host: "github.com", Owner: "a", Repo: "b",
	}), "empty project path must be rejected")

	assert.Error(t, service.UpsertProjectForge(service.ProjectForge{
		ProjectPath: t.TempDir(), Platform: "github", Host: "github.com", Owner: "", Repo: "b",
	}), "empty owner must be rejected")

	assert.Error(t, service.UpsertProjectForge(service.ProjectForge{
		ProjectPath: t.TempDir(), Platform: "", Host: "github.com", Owner: "a", Repo: "b",
	}), "empty platform must be rejected")
}

func TestProjectForge_Delete(t *testing.T) {
	setupTestDBForProjectForges(t)
	dir := t.TempDir()
	require.NoError(t, service.UpsertProjectForge(service.ProjectForge{
		ProjectPath: dir, Platform: "github", Host: "github.com", Owner: "a", Repo: "b", Source: "auto",
	}))

	require.NoError(t, service.DeleteProjectForge(dir))
	got, err := service.GetProjectForge(dir)
	require.NoError(t, err)
	assert.Nil(t, got)

	// Deleting a non-existent binding is a no-op, not an error.
	require.NoError(t, service.DeleteProjectForge(dir))
}

func TestProjectForge_FromRemote(t *testing.T) {
	remote, err := forge.ParseRemoteURL("git@gitlab.com:group/sub/team/widgets.git")
	require.NoError(t, err)

	dir := t.TempDir()
	pf := service.ProjectForgeFromRemote(dir, remote, "auto")
	assert.Equal(t, "gitlab", pf.Platform)
	assert.Equal(t, "gitlab.com", pf.Host)
	assert.Equal(t, "group/sub/team", pf.Owner)
	assert.Equal(t, "widgets", pf.Repo)
	assert.Equal(t, "auto", pf.Source)
}

func TestUniqueForgeRepos_DeduplicatesAcrossProjects(t *testing.T) {
	setupTestDBForProjectForges(t)

	// The same repository bound to two different projects must collapse to one
	// polling unit.
	for _, dir := range []string{t.TempDir(), t.TempDir()} {
		require.NoError(t, service.UpsertProjectForge(service.ProjectForge{
			ProjectPath: dir, Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets", Source: "auto",
		}))
	}
	// A different repository stays distinct.
	require.NoError(t, service.UpsertProjectForge(service.ProjectForge{
		ProjectPath: t.TempDir(), Platform: "gitlab", Host: "gitlab.com", Owner: "acme", Repo: "gadgets", Source: "auto",
	}))

	repos, err := service.UniqueForgeRepos()
	require.NoError(t, err)
	assert.Len(t, repos, 2, "same repo in two projects counts once; distinct repo counts separately")

	keys := map[string]bool{}
	for _, r := range repos {
		keys[r.Key()] = true
	}
	assert.True(t, keys["github|github.com|acme/widgets"])
	assert.True(t, keys["gitlab|gitlab.com|acme/gadgets"])
}

func TestNormalizeProjectPath_Empty(t *testing.T) {
	assert.Equal(t, "", service.NormalizeProjectPath(""))
	assert.Equal(t, "", service.NormalizeProjectPath("   "))
}
