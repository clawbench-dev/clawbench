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
// project_forges table (mirrors the production DDL in database.go). The
// project_meta table is created too, because the auto-bind path reads
// forge_bind_opt_out from it.
func setupTestDBForProjectForges(t *testing.T) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)

	_, err = db.Exec(service.ProjectForgesDDL)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS project_meta (
		project_path TEXT PRIMARY KEY,
		next_session_number INTEGER NOT NULL DEFAULT 0,
		forge_bind_opt_out INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	require.NoError(t, err)

	cleanup := service.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})
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
	realDir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	require.NoError(t, service.UpsertProjectForge(service.ProjectForge{
		ProjectPath: realDir, Platform: "github", Host: "github.com", Owner: "a", Repo: "b", Source: "auto",
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

func TestAutoBindProjectForge_CreatesWhenUnbound(t *testing.T) {
	setupTestDBForProjectForges(t)
	dir := t.TempDir()
	remote, err := forge.ParseRemoteURL("git@github.com:acme/widgets.git")
	require.NoError(t, err)

	created, err := service.AutoBindProjectForge(dir, remote)
	require.NoError(t, err)
	assert.True(t, created)

	got, err := service.GetProjectForge(dir)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "acme/widgets", got.Slug())
	assert.Equal(t, "auto", got.Source, "auto-bind must be recorded as source=auto")
}

func TestAutoBindProjectForge_DoesNotOverwriteExistingBinding(t *testing.T) {
	setupTestDBForProjectForges(t)
	dir := t.TempDir()
	// The user picked a different repository manually.
	require.NoError(t, service.UpsertProjectForge(service.ProjectForge{
		ProjectPath: dir, Platform: "github", Host: "github.com", Owner: "chosen", Repo: "repo", Source: "manual",
	}))

	remote, err := forge.ParseRemoteURL("git@github.com:acme/widgets.git")
	require.NoError(t, err)
	created, err := service.AutoBindProjectForge(dir, remote)
	require.NoError(t, err)
	assert.False(t, created, "an existing binding must not be replaced by auto-bind")

	got, err := service.GetProjectForge(dir)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "chosen/repo", got.Slug(), "the user's manual choice must win")
}

func TestAutoBindProjectForge_RespectsOptOut(t *testing.T) {
	setupTestDBForProjectForges(t)
	dir := t.TempDir()
	require.NoError(t, service.SetForgeBindOptOut(dir, true))

	remote, err := forge.ParseRemoteURL("git@github.com:acme/widgets.git")
	require.NoError(t, err)
	created, err := service.AutoBindProjectForge(dir, remote)
	require.NoError(t, err)
	assert.False(t, created, "an opted-out project must stay unbound")

	got, err := service.GetProjectForge(dir)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestAutoBindProjectForge_ClearingOptOutReenables(t *testing.T) {
	setupTestDBForProjectForges(t)
	dir := t.TempDir()
	remote, err := forge.ParseRemoteURL("git@github.com:acme/widgets.git")
	require.NoError(t, err)

	require.NoError(t, service.SetForgeBindOptOut(dir, true))
	_, err = service.AutoBindProjectForge(dir, remote)
	require.NoError(t, err)

	// An explicit bind clears the opt-out, so auto-bind works again afterwards.
	require.NoError(t, service.SetForgeBindOptOut(dir, false))
	created, err := service.AutoBindProjectForge(dir, remote)
	require.NoError(t, err)
	assert.True(t, created, "clearing the opt-out must re-enable auto-bind")
}

func TestForgeBindOptOut_RoundTrip(t *testing.T) {
	setupTestDBForProjectForges(t)
	dir := t.TempDir()

	// A project with no row at all is not opted out.
	opted, err := service.IsForgeBindOptedOut(dir)
	require.NoError(t, err)
	assert.False(t, opted, "a missing project_meta row means not opted out")

	require.NoError(t, service.SetForgeBindOptOut(dir, true))
	opted, err = service.IsForgeBindOptedOut(dir)
	require.NoError(t, err)
	assert.True(t, opted)

	// Setting it twice must not error (upsert).
	require.NoError(t, service.SetForgeBindOptOut(dir, true))
	opted, err = service.IsForgeBindOptedOut(dir)
	require.NoError(t, err)
	assert.True(t, opted)

	require.NoError(t, service.SetForgeBindOptOut(dir, false))
	opted, err = service.IsForgeBindOptedOut(dir)
	require.NoError(t, err)
	assert.False(t, opted)
}

func TestAutoBindProjectForge_RejectsIncompleteRemote(t *testing.T) {
	setupTestDBForProjectForges(t)
	created, err := service.AutoBindProjectForge(t.TempDir(), forge.Remote{Platform: "github", Host: "github.com"})
	assert.Error(t, err)
	assert.False(t, created)
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

// TestProjectForge_NilDBGuards covers the "no database" branches of every
// accessor. They must be silent no-ops rather than panics, because the forge
// integration is optional: a build without a DB must still serve the rest of
// the app.
func TestProjectForge_NilDBGuards(t *testing.T) {
	cleanup := service.SetDBForTest(nil, nil)
	t.Cleanup(cleanup)

	assert.Nil(t, mustGet(t))
	assert.NoError(t, service.UpsertProjectForge(service.ProjectForge{
		ProjectPath: "/tmp/x", Platform: "github", Host: "github.com", Owner: "a", Repo: "b",
	}))
	assert.NoError(t, service.DeleteProjectForge("/tmp/x"))
	assert.NoError(t, service.SetForgeBindOptOut("/tmp/x", true))

	opted, err := service.IsForgeBindOptedOut("/tmp/x")
	require.NoError(t, err)
	assert.False(t, opted)

	created, err := service.AutoBindProjectForge("/tmp/x", forge.Remote{
		Platform: forge.PlatformGitHub, Host: "github.com", Owner: "a", Repo: "b",
	})
	require.NoError(t, err)
	assert.False(t, created)

	repos, err := service.ListProjectForges()
	require.NoError(t, err)
	assert.Nil(t, repos)

	uniq, err := service.UniqueForgeRepos()
	require.NoError(t, err)
	assert.Nil(t, uniq)
}

// mustGet is a helper so the nil-DB test reads as a single assertion.
func mustGet(t *testing.T) *service.ProjectForge {
	t.Helper()
	pf, err := service.GetProjectForge("/tmp/x")
	require.NoError(t, err)
	return pf
}

// TestProjectForge_EmptyPathGuards covers the empty-path branches: a project
// path that normalizes to nothing is a no-op, never a lookup on "".
func TestProjectForge_EmptyPathGuards(t *testing.T) {
	setupTestDBForProjectForges(t)

	pf, err := service.GetProjectForge("")
	require.NoError(t, err)
	assert.Nil(t, pf)

	assert.NoError(t, service.DeleteProjectForge(""))

	opted, err := service.IsForgeBindOptedOut("")
	require.NoError(t, err)
	assert.False(t, opted)

	assert.NoError(t, service.SetForgeBindOptOut("", true))

	created, err := service.AutoBindProjectForge("", forge.Remote{
		Platform: forge.PlatformGitHub, Host: "github.com", Owner: "a", Repo: "b",
	})
	require.NoError(t, err)
	assert.False(t, created)

	// Upsert with an empty path is a hard error: it is a caller bug, not an
	// optional integration being absent.
	err = service.UpsertProjectForge(service.ProjectForge{
		Platform: "github", Host: "github.com", Owner: "a", Repo: "b",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project path is required")
}

// TestAutoBindProjectForge_EmptyPathIsNoop covers AutoBind's own empty-path
// guard (distinct from the validation error for an incomplete remote).
func TestAutoBindProjectForge_EmptyPathIsNoop(t *testing.T) {
	setupTestDBForProjectForges(t)
	created, err := service.AutoBindProjectForge("   ", forge.Remote{
		Platform: forge.PlatformGitHub, Host: "github.com", Owner: "a", Repo: "b",
	})
	require.NoError(t, err)
	assert.False(t, created)
}

// TestIsForgeBindOptedOut_ErrorPath covers a query failure (the table is
// missing), which must surface as an error rather than a silent "not opted out".
func TestIsForgeBindOptedOut_ErrorPath(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	cleanup := service.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})

	_, err = service.IsForgeBindOptedOut(t.TempDir())
	require.Error(t, err, "a missing project_meta table must be reported")
}

// TestGetProjectForge_ErrorPath covers a query failure on the bindings table.
func TestGetProjectForge_ErrorPath(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	cleanup := service.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})

	_, err = service.GetProjectForge(t.TempDir())
	require.Error(t, err, "a missing project_forges table must be reported")
}

// TestListProjectForges_ErrorPath covers a query failure on the list accessor.
func TestListProjectForges_ErrorPath(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	cleanup := service.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})

	_, err = service.ListProjectForges()
	require.Error(t, err)
}

// TestUniqueForgeRepos_ErrorPath covers a query failure on the distinct-repo
// accessor.
func TestUniqueForgeRepos_ErrorPath(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	cleanup := service.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})

	_, err = service.UniqueForgeRepos()
	require.Error(t, err)
}
