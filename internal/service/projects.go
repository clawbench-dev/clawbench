package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
)

// ProjectsDDL creates the projects table: the single registry that maps a
// project directory to a stable integer identity.
//
// Why an id instead of the path: every project-scoped table used to store the
// project *path* verbatim (chat_history, chat_sessions, scheduled_tasks, tags,
// queues, forges, RAG chunks, …). Renaming or moving a project directory
// therefore meant rewriting the path in ~15 tables, and nothing did that — a
// renamed directory silently orphaned every row. With an id, a rename is one
// UPDATE on this table.
//
// path is stored CANONICAL (see NormalizeProjectPath): absolute, symlinks
// resolved, cleaned. UNIQUE(path) is what makes two raw spellings of the same
// directory collapse into one row.
//
// There is deliberately NO foreign key from the other tables. Two sentinel
// values must be representable: session_tags uses project_id = 0 for GLOBAL
// tags (visible in every project), and file_shares uses 0 for "not attributable
// to a project". A real FK would require a synthetic projects row with id = 0,
// and this repo already omits FKs where rows must outlive or be denormalized
// (chat_metadata ledger, btw_questions, session_shares).
//
// forge_bind_opt_out is folded in from the former project_meta table, which had
// exactly one live column. Its other column, next_session_number, was dead
// (written by nothing, read by nothing) and is dropped.
const ProjectsDDL = `
CREATE TABLE IF NOT EXISTS projects (
	id                 INTEGER PRIMARY KEY AUTOINCREMENT,
	path               TEXT NOT NULL,
	forge_bind_opt_out INTEGER NOT NULL DEFAULT 0,
	created_at         DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at         DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(path)
);
CREATE INDEX IF NOT EXISTS idx_projects_path ON projects(path);
`

// GlobalScopeProjectID is the reserved project id for "no project": global
// session tags, and file shares that could not be attributed to any project.
//
// It is 0 rather than NULL on purpose. SQLite treats NULLs as distinct in a
// UNIQUE constraint, so a nullable project_id would let two global tags with
// the same name coexist and silently break the (name, scope) uniqueness the
// tag feature relies on.
const GlobalScopeProjectID int64 = 0

// projectIDCache maps a canonical project path to its id. Only path→id is
// cached: an id→path cache would reintroduce exactly the staleness this
// refactor exists to remove (a rename would keep serving the old path), while
// path→id only ever grows and is invalidated explicitly by RenameProject.
var (
	projectIDMu    sync.RWMutex
	projectIDCache = map[string]int64{}
)

// ProjectIDForPath resolves a project path to its id, creating the projects row
// on first use.
//
// The path is canonicalized first, so every caller that passes a raw cookie
// value still lands on one row. An empty path (no project selected) resolves to
// GlobalScopeProjectID.
func ProjectIDForPath(path string) (int64, error) {
	canon := NormalizeProjectPath(path)
	if canon == "" {
		return GlobalScopeProjectID, nil
	}

	projectIDMu.RLock()
	if id, ok := projectIDCache[canon]; ok {
		projectIDMu.RUnlock()
		return id, nil
	}
	projectIDMu.RUnlock()

	// ON CONFLICT DO NOTHING makes this safe to race: whoever loses the insert
	// still reads the winner's id on the SELECT below.
	if _, err := WriteExec(
		"INSERT INTO projects (path) VALUES (?) ON CONFLICT(path) DO NOTHING", canon,
	); err != nil {
		return 0, fmt.Errorf("upsert project: %w", err)
	}

	// Read back through the WRITE pool: this follows the INSERT above, and on a
	// deployment where the read pool is a separate replica the row may not be
	// visible there yet. Reading the same connection family also keeps the
	// function usable when only the read handle is unavailable.
	var id int64
	if err := db.QueryRowContext(context.Background(), "SELECT id FROM projects WHERE path = ?", canon).Scan(&id); err != nil {
		return 0, fmt.Errorf("query project id: %w", err)
	}

	projectIDMu.Lock()
	projectIDCache[canon] = id
	projectIDMu.Unlock()
	return id, nil
}

// ProjectIDByPath resolves a project path to its id WITHOUT creating a row.
// Returns ok=false when the path has never been seen.
//
// Use this on read paths: a read must not have the side effect of registering a
// project, and an unknown project is equivalent to "no data" rather than an
// error.
func ProjectIDByPath(path string) (int64, bool, error) {
	canon := NormalizeProjectPath(path)
	if canon == "" {
		return GlobalScopeProjectID, true, nil
	}

	projectIDMu.RLock()
	if id, ok := projectIDCache[canon]; ok {
		projectIDMu.RUnlock()
		return id, true, nil
	}
	projectIDMu.RUnlock()

	var id int64
	err := ReadDB().QueryRow("SELECT id FROM projects WHERE path = ?", canon).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("query project id by path: %w", err)
	}

	projectIDMu.Lock()
	projectIDCache[canon] = id
	projectIDMu.Unlock()
	return id, true, nil
}

// ProjectPathForID returns the canonical path for a project id. Returns ok=false
// when no project has that id (including the 0 sentinel, which has no path).
//
// Deliberately uncached: see the note on projectIDCache.
func ProjectPathForID(id int64) (string, bool, error) {
	if id == GlobalScopeProjectID {
		return "", false, nil
	}
	var path string
	err := ReadDB().QueryRow("SELECT path FROM projects WHERE id = ?", id).Scan(&path)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("query project path: %w", err)
	}
	return path, true, nil
}

// projectRenamedHooks are notified after a project's path changes, so caches
// elsewhere can drop the old path→id entry.
//
// A hook rather than a direct call because the RAG store (internal/rag) owns its
// own path→id cache and imports THIS package, so the dependency cannot be
// reversed. internal/rag registers its invalidator from rag.Init, which runs
// after InitDB.
var (
	projectRenamedMu    sync.Mutex
	projectRenamedHooks []func(paths ...string)
)

// RegisterProjectRenamedHook adds a callback invoked with the old and new paths
// after a successful rename. Called once per process by internal/rag.
func RegisterProjectRenamedHook(fn func(paths ...string)) {
	if fn == nil {
		return
	}
	projectRenamedMu.Lock()
	projectRenamedHooks = append(projectRenamedHooks, fn)
	projectRenamedMu.Unlock()
}

// notifyProjectRenamed runs every registered hook. Hooks must not fail the
// rename: the registry is already updated, and a stale cache only costs a
// redundant re-read.
func notifyProjectRenamed(oldPath, newPath string) {
	projectRenamedMu.Lock()
	hooks := projectRenamedHooks
	projectRenamedMu.Unlock()
	for _, fn := range hooks {
		fn(oldPath, newPath)
	}
}

// RenameProject points a project at a new directory.
//
// This is what the id refactor buys: the directory on disk is NOT touched and
// no other table is rewritten — every project-scoped row keeps referring to the
// same id, so all of it follows the project to its new path.
//
// NOTE: there is currently NO production caller — no endpoint exposes a
// rename, so the capability is unreachable today and is exercised only by
// tests. It is kept because it is the operation the refactor exists to make
// cheap, and because the caches it invalidates (the service's path→id map and
// the RAG store's, via RegisterProjectRenamedHook) are live. Wire it to an
// endpoint before claiming the refactor's payoff in user-facing terms.
//
// Colliding with an existing project is an error rather than a merge. Merging
// would have to repoint ~15 tables and decide what to do with conflicting
// unique rows (e.g. two projects that each defined a "bug" tag); that is a
// separate operation with its own semantics, not something to do implicitly.
func RenameProject(oldPath, newPath string) error {
	oldCanon := NormalizeProjectPath(oldPath)
	newCanon := NormalizeProjectPath(newPath)
	if oldCanon == "" || newCanon == "" {
		return fmt.Errorf("project path is required")
	}
	if oldCanon == newCanon {
		return nil
	}

	if _, exists, err := ProjectIDByPath(newCanon); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("project already exists at %s", newCanon)
	}

	res, err := WriteExec(
		"UPDATE projects SET path = ?, updated_at = CURRENT_TIMESTAMP WHERE path = ?",
		newCanon, oldCanon,
	)
	if err != nil {
		return fmt.Errorf("rename project: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("project not found: %s", oldCanon)
	}

	// Drop the stale keys so the next lookup re-reads the row under its new path.
	// Both the old and the new path are evicted: a project later created at the
	// freed-up old path must not be resolved to this one's id.
	projectIDMu.Lock()
	delete(projectIDCache, oldCanon)
	delete(projectIDCache, newCanon)
	projectIDMu.Unlock()

	// Other packages hold their own path→id caches (the RAG store); let them drop
	// the same keys.
	notifyProjectRenamed(oldCanon, newCanon)
	return nil
}

// projectID2Valid reports whether a project id from ProjectIDByPath denotes a
// real project (as opposed to the 0 "no project" sentinel). Named awkwardly
// because "projectID" is already taken by a local in most call sites.
func projectID2Valid(id int64) bool { return id != GlobalScopeProjectID }

// ProjectIDForTest resolves a path to its project id, failing the test on error.
// Exported so external test packages (service_test) can seed project-scoped rows
// without duplicating the normalization rules.
func ProjectIDForTest(t interface {
	Helper()
	Fatalf(string, ...any)
}, path string,
) int64 {
	t.Helper()
	id, err := ProjectIDForPath(path)
	if err != nil {
		t.Fatalf("resolve project id for %q: %v", path, err)
	}
	return id
}

// seedTestProjectPaths is the fixed set of project paths the test fixtures
// reference by literal (inside SQL subqueries, or through ProjectIDForTest).
//
// These are canonicalized before they are inserted: the registry stores the
// canonical form, so seeding the raw literal would create a row the fixtures'
// own lookups cannot find on macOS (where /tmp is a symlink to /private/tmp).
var seedTestProjectPaths = []string{
	"/test", "/proj", "/proj1", "/proj2", "/proj/info", "/tmp", "/project", "/p",
}

// SeedTestProjectsForTest registers the shared fixture project paths through the
// package-level DB handle. Use it only AFTER SetDBForTest has installed that
// handle; when a helper owns a local *sql.DB that is not (yet) installed, call
// SeedTestProjectsOnDB instead.
//
// Test-only, and idempotent.
func SeedTestProjectsForTest(t interface {
	Helper()
	Fatalf(string, ...any)
},
) {
	t.Helper()
	for _, p := range seedTestProjectPaths {
		if _, err := WriteExec(
			"INSERT INTO projects (path) VALUES (?) ON CONFLICT(path) DO NOTHING", NormalizeProjectPath(p),
		); err != nil {
			t.Fatalf("seed project %q: %v", p, err)
		}
	}
}

// SeedTestProjectsOnDB is SeedTestProjectsForTest against an explicit handle, for
// fixture helpers that create their own *sql.DB before anything installs it
// globally. Test-only, and idempotent.
func SeedTestProjectsOnDB(t interface {
	Helper()
	Fatalf(string, ...any)
}, db *sql.DB,
) {
	t.Helper()
	for _, p := range seedTestProjectPaths {
		if _, err := db.ExecContext(
			context.Background(),
			"INSERT INTO projects (path) VALUES (?) ON CONFLICT(path) DO NOTHING", NormalizeProjectPath(p),
		); err != nil {
			t.Fatalf("seed project %q: %v", p, err)
		}
	}
}

// ResetProjectIDCacheForTest clears the process-level cache.
//
// Test-only, and necessary: the cache is keyed by canonical path, so a test
// that installs a fresh in-memory database would otherwise keep resolving paths
// to ids from a previous test's database.
func ResetProjectIDCacheForTest() {
	projectIDMu.Lock()
	projectIDCache = map[string]int64{}
	projectIDMu.Unlock()
}
