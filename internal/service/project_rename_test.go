package service

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errInjectDriver delegates every query to a real *sql.DB, but returns an error
// for any query containing failSubstr. This lets a test fail a single guard in
// RenameProjectData (resolve path / target collision / running session / UPDATE)
// while the earlier guards still hit the real database and succeed — something
// a blanket-failing driver cannot do. A fresh instance is registered under a
// unique name per call so each test controls its own failure target.
type errInjectDriver struct {
	name       string
	db         *sql.DB
	failSubstr string
}

func (d errInjectDriver) Open(name string) (driver.Conn, error) {
	return errInjectConn{db: d.db, failSubstr: d.failSubstr}, nil
}

type errInjectConn struct {
	db         *sql.DB
	failSubstr string
}

func (c errInjectConn) Prepare(query string) (driver.Stmt, error) {
	if c.failSubstr != "" && strings.Contains(query, c.failSubstr) {
		return errStmt{}, nil
	}
	stmt, err := c.db.Prepare(query)
	if err != nil {
		return nil, err
	}
	return realStmt{stmt: stmt}, nil
}
func (c errInjectConn) Close() error              { return nil }
func (c errInjectConn) Begin() (driver.Tx, error) { return nil, errors.New("errInject") }

type errStmt struct{}

func (errStmt) Close() error  { return errors.New("errInject") }
func (errStmt) NumInput() int { return -1 }
func (errStmt) Exec(args []driver.Value) (driver.Result, error) {
	return nil, errors.New("errInject")
}

func (errStmt) Query(args []driver.Value) (driver.Rows, error) {
	return nil, errors.New("errInject")
}

type realStmt struct{ stmt *sql.Stmt }

func (s realStmt) Close() error  { return s.stmt.Close() }
func (s realStmt) NumInput() int { return -1 }
func (s realStmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.stmt.Exec(valuesToAny(args)...)
}

func (s realStmt) Query(args []driver.Value) (driver.Rows, error) {
	rows, err := s.stmt.Query(valuesToAny(args)...) //nolint:rowserrcheck // error surfaced in Next via rows.Err()
	if err != nil {
		return nil, err
	}
	return &rowsAdapter{rows: rows}, nil
}

// rowsAdapter bridges a real *sql.Rows to the driver.Rows interface so we can
// delegate non-failing queries to the real test database.
type rowsAdapter struct {
	rows   *sql.Rows
	cols   []string
	colErr error
}

func (r *rowsAdapter) Columns() []string {
	if r.cols != nil || r.colErr != nil {
		return r.cols
	}
	cols, err := r.rows.Columns()
	if err != nil {
		r.colErr = err
		return nil
	}
	r.cols = cols
	return r.cols
}

func (r *rowsAdapter) Close() error { return r.rows.Close() }

func (r *rowsAdapter) Next(dest []driver.Value) error {
	if !r.rows.Next() {
		if err := r.rows.Err(); err != nil {
			return err
		}
		return io.EOF
	}
	scanDest := make([]any, len(dest))
	for i := range dest {
		scanDest[i] = &scanDest[i]
	}
	if err := r.rows.Scan(scanDest...); err != nil {
		return err
	}
	for i := range dest {
		dest[i] = driver.Value(scanDest[i])
	}
	return nil
}

func valuesToAny(args []driver.Value) []any {
	out := make([]any, len(args))
	for i, a := range args {
		out[i] = a
	}
	return out
}

var (
	errDriverSeq   int
	errDriverSeqMu sync.Mutex
)

// openErrInjectDB opens a connection through a fault-injecting driver that
// delegates to the currently-installed real DB, failing queries containing
// failSubstr. Each call registers a fresh, uniquely-named driver so tests don't
// share failure state.
func openErrInjectDB(t *testing.T, failSubstr string) *sql.DB {
	t.Helper()
	realDB := store.UnsafeDBForTest()
	errDriverSeqMu.Lock()
	errDriverSeq++
	name := fmt.Sprintf("rename-err-inject-%d", errDriverSeq)
	errDriverSeqMu.Unlock()
	sql.Register(name, errInjectDriver{name: name, db: realDB, failSubstr: failSubstr})
	db, err := sql.Open(name, "://")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	return db
}

// mkProjectDir creates the directory a rename test points at, under the
// per-test temp dir so no test touches a real path. The returned path is
// canonicalized the same way RenameProjectData canonicalizes it (symlink
// resolution, e.g. /var -> /private/var on macOS) so expectations match across
// platforms.
func mkProjectDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "project")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	return store.NormalizeProjectPath(dir)
}

// resolveCanon fully resolves a path on disk so platform quirks like Windows 8.3
// short names (RUNNER~1 vs runneradmin) or macOS /var vs /private/var symlinks do
// not cause false mismatches when comparing a test-constructed path against the
// value the DB stored after RenameProjectData's own (non-idempotent on some
// platforms) normalization. Both sides resolve to the same real directory.
func resolveCanon(t *testing.T, p string) string {
	t.Helper()
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func TestRenameProjectData_FollowsProjectScopeData(t *testing.T) {
	setupProjectDeleteDB(t)
	oldPath := mkProjectDir(t)
	newPath := store.NormalizeProjectPath(filepath.Join(t.TempDir(), "moved"))
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
	assert.Equal(t, resolveCanon(t, newPath), resolveCanon(t, storedPath))

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
	assert.Equal(t, resolveCanon(t, target), resolveCanon(t, storedPath))
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

// The four error paths below install a driver that delegates to the real test
// DB but fails the query each guard issues, so that step surfaces its wrapped
// error instead of succeeding. This exercises the branches that only fire on a
// DB fault.

func TestRenameProjectData_ResolvePathError(t *testing.T) {
	setupProjectDeleteDB(t)
	// The resolve-path guard reads `SELECT path FROM projects WHERE id = ?`.
	db := openErrInjectDB(t, "SELECT path FROM projects")
	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(func() {
		_ = db.Close()
		cleanup()
		store.ResetProjectIDCacheForTest()
	})

	_, err := RenameProjectData(1, mkProjectDir(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve project path")
}

func TestRenameProjectData_TargetCollisionCheckError(t *testing.T) {
	setupProjectDeleteDB(t)
	oldPath := mkProjectDir(t)
	id := seedProjectRow(t, oldPath)

	// Old path resolves fine, but the target-existence probe queries projects by
	// path; fail that query.
	db := openErrInjectDB(t, "SELECT id FROM projects WHERE path")
	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(func() {
		_ = db.Close()
		cleanup()
		store.ResetProjectIDCacheForTest()
	})

	_, err := RenameProjectData(id, mkProjectDir(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "check target project path")
}

func TestRenameProjectData_RunningSessionCheckError(t *testing.T) {
	setupProjectDeleteDB(t)
	oldPath := mkProjectDir(t)
	newPath := mkProjectDir(t)
	id := seedProjectRow(t, oldPath)
	_, err := store.WriteExec(
		"INSERT INTO chat_sessions (id, project_id, backend, title, session_type, archived) VALUES ('sess-running', ?, 'claude', 't', 'chat', 0)",
		id,
	)
	require.NoError(t, err)

	// Both path lookups succeed, but the running-session probe queries
	// chat_sessions; fail that query.
	db := openErrInjectDB(t, "FROM chat_sessions WHERE project_id")
	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(func() {
		_ = db.Close()
		cleanup()
		store.ResetProjectIDCacheForTest()
	})

	SetSessionRunning("sess-running", true)
	t.Cleanup(func() { SetSessionRunning("sess-running", false, true) })

	_, err = RenameProjectData(id, newPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "check running sessions")
}

func TestRenameProjectData_UpdateError(t *testing.T) {
	setupProjectDeleteDB(t)
	oldPath := mkProjectDir(t)
	newPath := mkProjectDir(t)
	id := seedProjectRow(t, oldPath)
	target := mkProjectDir(t)
	seedProjectRow(t, target)

	// All guards pass, but the final UPDATE on projects must fail.
	db := openErrInjectDB(t, "UPDATE projects SET path")
	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(func() {
		_ = db.Close()
		cleanup()
		store.ResetProjectIDCacheForTest()
	})

	_, err := RenameProjectData(id, newPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rename project")
}
