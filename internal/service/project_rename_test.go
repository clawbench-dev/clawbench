package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mkProjectDir creates the directory a rename test points at, under the
// per-test temp dir so no test touches a real path.
func mkProjectDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "project")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	return dir
}

func TestRenameProjectData_FollowsProjectScopeData(t *testing.T) {
	setupProjectDeleteDB(t)
	oldPath := mkProjectDir(t)
	newPath := filepath.Join(t.TempDir(), "moved")
	require.NoError(t, os.MkdirAll(newPath, 0o755))

	id := seedProjectRow(t, oldPath)
	_, err := store.WriteExec(
		"INSERT INTO chat_sessions (id, project_id, backend, title, session_type, archived) VALUES ('sess-rename', ?, 'claude', 't', 'chat', 0)",
		id,
	)
	require.NoError(t, err)
	_, err = store.WriteExec(
		"INSERT INTO chat_history (project_id, backend, session_id, role, content) VALUES (?, 'claude', 'sess-rename', 'user', 'hi')",
		id,
	)
	require.NoError(t, err)
	_, err = store.WriteExec("INSERT INTO session_tags (name, scope, project_id) VALUES ('tag', 'project', ?)", id)
	require.NoError(t, err)

	gotOld, err := RenameProjectData(id, newPath)
	require.NoError(t, err)
	assert.Equal(t, oldPath, gotOld)

	var storedPath string
	require.NoError(t, store.ReadDB().QueryRow("SELECT path FROM projects WHERE id = ?", id).Scan(&storedPath))
	assert.Equal(t, newPath, storedPath)

	// The path→id cache was invalidated on both sides: the new path resolves to
	// the same id and the old path is no longer a registered project.
	resolved, err := store.ProjectIDForPath(newPath)
	require.NoError(t, err)
	assert.Equal(t, id, resolved)
	_, exists, err := store.ProjectIDByPath(oldPath)
	require.NoError(t, err)
	assert.False(t, exists)

	// Project-scoped rows kept the same id, so they are still attached.
	assert.Equal(t, 1, countRows(t, "chat_sessions", id))
	assert.Equal(t, 1, countRows(t, "chat_history", id))
	assert.Equal(t, 1, countRows(t, "session_tags", id))
}

func TestRenameProjectData_RefusesUnknownProject(t *testing.T) {
	setupProjectDeleteDB(t)
	newPath := mkProjectDir(t)

	_, err := RenameProjectData(999999, newPath)
	assert.ErrorIs(t, err, ErrProjectNotFound)
}

func TestRenameProjectData_RefusesGlobalSentinel(t *testing.T) {
	setupProjectDeleteDB(t)

	_, err := RenameProjectData(store.GlobalScopeProjectID, mkProjectDir(t))
	assert.ErrorIs(t, err, ErrProjectRenameForbidden)
}

func TestRenameProjectData_RefusesMissingOrNonDirectoryTarget(t *testing.T) {
	setupProjectDeleteDB(t)
	oldPath := mkProjectDir(t)
	id := seedProjectRow(t, oldPath)

	_, err := RenameProjectData(id, filepath.Join(t.TempDir(), "does-not-exist"))
	assert.ErrorIs(t, err, ErrProjectPathInvalid)

	file := filepath.Join(t.TempDir(), "a-file")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
	_, err = RenameProjectData(id, file)
	assert.ErrorIs(t, err, ErrProjectPathInvalid)

	// The registry row must be untouched by the refusals.
	var storedPath string
	require.NoError(t, store.ReadDB().QueryRow("SELECT path FROM projects WHERE id = ?", id).Scan(&storedPath))
	assert.Equal(t, oldPath, storedPath)
}

func TestRenameProjectData_RefusesCollision(t *testing.T) {
	setupProjectDeleteDB(t)
	oldPath := mkProjectDir(t)
	target := mkProjectDir(t)
	id := seedProjectRow(t, oldPath)
	otherID := seedProjectRow(t, target)

	_, err := RenameProjectData(id, target)
	assert.ErrorIs(t, err, ErrProjectPathExists)

	var storedPath string
	require.NoError(t, store.ReadDB().QueryRow("SELECT path FROM projects WHERE id = ?", id).Scan(&storedPath))
	assert.Equal(t, oldPath, storedPath)
	_, exists, err := store.ProjectIDByPath(target)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.NotEqual(t, otherID, id)
}

func TestRenameProjectData_RefusesWhileSessionRuns(t *testing.T) {
	setupProjectDeleteDB(t)
	oldPath := mkProjectDir(t)
	newPath := mkProjectDir(t)
	id := seedProjectRow(t, oldPath)
	_, err := store.WriteExec(
		"INSERT INTO chat_sessions (id, project_id, backend, title, session_type, archived) VALUES ('sess-running', ?, 'claude', 't', 'chat', 0)",
		id,
	)
	require.NoError(t, err)
	SetSessionRunning("sess-running", true)
	t.Cleanup(func() { SetSessionRunning("sess-running", false, true) })

	_, err = RenameProjectData(id, newPath)
	assert.ErrorIs(t, err, ErrProjectHasRunningSessions)

	var storedPath string
	require.NoError(t, store.ReadDB().QueryRow("SELECT path FROM projects WHERE id = ?", id).Scan(&storedPath))
	assert.Equal(t, oldPath, storedPath)
}

func TestRenameProjectData_SamePathIsNoop(t *testing.T) {
	setupProjectDeleteDB(t)
	path := mkProjectDir(t)
	id := seedProjectRow(t, path)

	gotOld, err := RenameProjectData(id, path)
	require.NoError(t, err)
	assert.Equal(t, path, gotOld)
}

func TestRenameProjectData_NormalizesTargetPath(t *testing.T) {
	setupProjectDeleteDB(t)
	oldPath := mkProjectDir(t)
	target := mkProjectDir(t)
	id := seedProjectRow(t, oldPath)

	// A trailing separator must still target the same registry path.
	_, err := RenameProjectData(id, target+string(filepath.Separator))
	require.NoError(t, err)

	var storedPath string
	require.NoError(t, store.ReadDB().QueryRow("SELECT path FROM projects WHERE id = ?", id).Scan(&storedPath))
	assert.Equal(t, target, storedPath)
}

func TestRenameProjectData_InvalidTargetDoesNotSurfaceAsPathExists(t *testing.T) {
	setupProjectDeleteDB(t)
	oldPath := mkProjectDir(t)
	id := seedProjectRow(t, oldPath)

	_, err := RenameProjectData(id, "")
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrProjectPathExists))
	assert.ErrorIs(t, err, ErrProjectPathInvalid)
}
