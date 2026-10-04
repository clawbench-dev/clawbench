package store

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeProjectPath(t *testing.T) {
	// The forwarder must agree with the model implementation it wraps: an
	// absolute, cleaned path for a real directory, empty for empty input.
	dir := t.TempDir()
	// The canonical form resolves symlinks, so on macOS (where t.TempDir() sits
	// under /var, a symlink to /private/var) the expected value must be resolved
	// too — comparing against the raw path would fail there.
	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	assert.Equal(t, "", NormalizeProjectPath(""))
	assert.Equal(t, "", NormalizeProjectPath("   "))
	assert.Equal(t, resolved, NormalizeProjectPath(dir))
	// A trailing separator is cleaned away.
	assert.Equal(t, resolved, NormalizeProjectPath(dir+string(filepath.Separator)))
	// Normalization is idempotent: re-normalizing the canonical form is stable.
	assert.Equal(t, resolved, NormalizeProjectPath(resolved))
}

// An empty path is the "no project" scope and resolves to the sentinel id
// without creating a row.
func TestProjectIDForPath_EmptyIsGlobalScope(t *testing.T) {
	setupStoreTestDB(t)

	id, err := ProjectIDForPath("")
	require.NoError(t, err)
	assert.Equal(t, GlobalScopeProjectID, id)

	var count int
	require.NoError(t, UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM projects").Scan(&count))
	assert.Equal(t, 0, count, "empty path must not register a project row")
}

// Resolving the same path twice returns the same id (the cache is warm on the
// second call), and the row is created exactly once.
func TestProjectIDForPath_CreatesAndCaches(t *testing.T) {
	setupStoreTestDB(t)

	first, err := ProjectIDForPath("/project")
	require.NoError(t, err)
	assert.NotZero(t, first)

	second, err := ProjectIDForPath("/project")
	require.NoError(t, err)
	assert.Equal(t, first, second)

	var count int
	require.NoError(t, UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM projects WHERE path = ?", NormalizeProjectPath("/project")).Scan(&count))
	assert.Equal(t, 1, count)
}

// ProjectIDByPath is the read-side lookup: it never creates a row.
func TestProjectIDByPath_DoesNotCreate(t *testing.T) {
	setupStoreTestDB(t)

	_, ok, err := ProjectIDByPath("/project")
	require.NoError(t, err)
	assert.False(t, ok)

	var count int
	require.NoError(t, UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM projects").Scan(&count))
	assert.Equal(t, 0, count)

	// After the path is registered through the write path, the lookup finds it.
	created, err := ProjectIDForPath("/project")
	require.NoError(t, err)
	// Drop the cache so the read path actually queries the row.
	ResetProjectIDCacheForTest()

	id, ok, err := ProjectIDByPath("/project")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, created, id)

	// The empty path resolves to the sentinel and is "found".
	emptyID, ok, err := ProjectIDByPath("")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, GlobalScopeProjectID, emptyID)
}

func TestProjectPathForID(t *testing.T) {
	setupStoreTestDB(t)

	// The sentinel has no path.
	path, ok, err := ProjectPathForID(GlobalScopeProjectID)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, "", path)

	// An id with no row reports ok=false rather than an error.
	path, ok, err = ProjectPathForID(999999)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, "", path)

	id, err := ProjectIDForPath("/project")
	require.NoError(t, err)
	path, ok, err = ProjectPathForID(id)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, NormalizeProjectPath("/project"), path)
}

func TestProjectID2Valid(t *testing.T) {
	assert.False(t, ProjectID2Valid(GlobalScopeProjectID))
	assert.True(t, ProjectID2Valid(1))
}

// ForgetProjectPath drops the cached entry after the row is deleted, so a later
// lookup does not serve a dangling id.
func TestForgetProjectPath(t *testing.T) {
	setupStoreTestDB(t)

	id, err := ProjectIDForPath("/project")
	require.NoError(t, err)

	// Delete the row, then forget the cache entry.
	_, err = WriteExec("DELETE FROM projects WHERE id = ?", id)
	require.NoError(t, err)
	ForgetProjectPath("/project")
	// The empty path is a no-op (nothing to forget).
	ForgetProjectPath("")

	// The next resolution re-creates the row and returns a fresh id.
	newID, err := ProjectIDForPath("/project")
	require.NoError(t, err)
	assert.NotEqual(t, id, newID)

	var count int
	require.NoError(t, UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM projects WHERE id = ?", newID).Scan(&count))
	assert.Equal(t, 1, count)
}

// RenameProject repoints the registry at a new directory and evicts both the
// old and new cache keys; a hook registered beforehand is notified.
func TestRenameProject(t *testing.T) {
	setupStoreTestDB(t)

	oldDir := filepath.Join(t.TempDir(), "old")
	newDir := filepath.Join(t.TempDir(), "new")

	id, err := ProjectIDForPath(oldDir)
	require.NoError(t, err)

	var notified []string
	ResetProjectRenamedHooksForTest()
	RegisterProjectRenamedHook(func(paths ...string) { notified = append(notified, paths...) })
	t.Cleanup(ResetProjectRenamedHooksForTest)

	require.NoError(t, RenameProject(oldDir, newDir))

	// The same id now resolves to the new path.
	ResetProjectIDCacheForTest()
	newID, err := ProjectIDForPath(newDir)
	require.NoError(t, err)
	assert.Equal(t, id, newID)

	// The hook saw both paths.
	assert.Equal(t, []string{NormalizeProjectPath(oldDir), NormalizeProjectPath(newDir)}, notified)
}

func TestRenameProject_Errors(t *testing.T) {
	setupStoreTestDB(t)

	dir := t.TempDir()

	// Empty either side is rejected.
	assert.Error(t, RenameProject("", dir))
	assert.Error(t, RenameProject(dir, ""))

	// Renaming to itself is a no-op success.
	require.NoError(t, RenameProject(dir, dir))

	// Renaming a path that has no row is an error.
	assert.Error(t, RenameProject(filepath.Join(dir, "missing"), filepath.Join(dir, "also-missing")))

	// Colliding with an existing project is an error rather than a merge.
	other := t.TempDir()
	_, err := ProjectIDForPath(dir)
	require.NoError(t, err)
	_, err = ProjectIDForPath(other)
	require.NoError(t, err)
	assert.Error(t, RenameProject(dir, other))
}

// A nil hook is ignored rather than panicking on invocation.
func TestRegisterProjectRenamedHook_NilIsIgnored(t *testing.T) {
	ResetProjectRenamedHooksForTest()
	t.Cleanup(ResetProjectRenamedHooksForTest)

	RegisterProjectRenamedHook(nil)
	// notifyProjectRenamed must not panic with an empty registry.
	notifyProjectRenamed("/a", "/b")
}

// ---------- test-only helpers ----------

func TestSeedTestProjectPathsForTest(t *testing.T) {
	setupStoreTestDB(t)

	paths := SeedTestProjectPathsForTest()
	require.NotEmpty(t, paths)

	// The returned slice is a copy: mutating it must not affect the registry.
	paths[0] = "mutated"
	assert.NotEqual(t, "mutated", SeedTestProjectPathsForTest()[0])

	orig := SetSeedTestProjectPathsForTest([]string{"/only"})
	t.Cleanup(func() { SetSeedTestProjectPathsForTest(orig) })
	assert.Equal(t, []string{"/only"}, SeedTestProjectPathsForTest())
}

func TestSeedTestProjectsForTest(t *testing.T) {
	setupStoreTestDB(t)

	orig := SetSeedTestProjectPathsForTest([]string{"/seed-a", "/seed-b"})
	t.Cleanup(func() { SetSeedTestProjectPathsForTest(orig) })

	SeedTestProjectsForTest(t)

	var count int
	require.NoError(t, UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM projects").Scan(&count))
	assert.Equal(t, 2, count)

	// Idempotent: seeding again does not duplicate rows.
	SeedTestProjectsForTest(t)
	require.NoError(t, UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM projects").Scan(&count))
	assert.Equal(t, 2, count)
}

func TestProjectIDForTest(t *testing.T) {
	setupStoreTestDB(t)

	id := ProjectIDForTest(t, "/project")
	assert.NotZero(t, id)
	// Stable across calls.
	assert.Equal(t, id, ProjectIDForTest(t, "/project"))
}
