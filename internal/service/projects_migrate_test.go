package service

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"

	_ "modernc.org/sqlite"
)

// requireNoError is a local t.Fatalf-on-error helper (this package's tests use
// testify, but these migration tests stay dependency-free so they read as a
// straight sequence of statements).
func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// symlinkOrSkip creates link -> target, returning an error when the platform
// forbids it (Windows without developer mode) so the caller can skip.
func symlinkOrSkip(t *testing.T, target, link string) error {
	t.Helper()
	return os.Symlink(target, link)
}

// legacySchema is a minimal but representative pre-projects database: the tables
// that carry a project_path column, with the constraints the migration has to
// deal with (UNIQUE containing project_path on chat_sessions / recent_projects /
// session_tags, a PRIMARY KEY on project_meta, and an inbound foreign key from
// session_tag_links to session_tags).
//
// It is deliberately NOT the full historical schema — tables this migration does
// not touch are created afterwards by createTables, which is exactly what an
// upgrade of an older database looks like.
const legacySchema = `
CREATE TABLE chat_history (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_path TEXT NOT NULL,
	role TEXT NOT NULL,
	content TEXT NOT NULL,
	files TEXT,
	session_id TEXT,
	backend TEXT NOT NULL DEFAULT 'claude',
	streaming INTEGER NOT NULL DEFAULT 0,
	indexed INTEGER NOT NULL DEFAULT 0,
	external_message_id TEXT DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	completed_at DATETIME
);
CREATE TABLE chat_sessions (
	id TEXT PRIMARY KEY,
	project_path TEXT NOT NULL,
	backend TEXT NOT NULL,
	title TEXT NOT NULL,
	agent_id TEXT DEFAULT '',
	agent_source TEXT DEFAULT 'default',
	model TEXT DEFAULT '',
	external_session_id TEXT DEFAULT '',
	session_type TEXT NOT NULL DEFAULT 'chat',
	archived INTEGER NOT NULL DEFAULT 0,
	last_read_at DATETIME,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	source_session_id TEXT DEFAULT NULL,
	transport TEXT DEFAULT '',
	auto_approve INTEGER NOT NULL DEFAULT 0,
	context_state TEXT DEFAULT '',
	compacted INTEGER NOT NULL DEFAULT 0,
	title_renamed INTEGER NOT NULL DEFAULT 0,
	title_source TEXT NOT NULL DEFAULT '',
	pinned INTEGER NOT NULL DEFAULT 0,
	sort_order INTEGER NOT NULL DEFAULT 0,
	UNIQUE(project_path, backend, id)
);
CREATE TABLE recent_projects (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_path TEXT UNIQUE NOT NULL,
	accessed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	is_default INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE project_meta (
	project_path TEXT PRIMARY KEY,
	next_session_number INTEGER NOT NULL DEFAULT 0,
	forge_bind_opt_out INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE scheduled_tasks (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_path TEXT NOT NULL,
	name TEXT NOT NULL,
	cron_expr TEXT NOT NULL,
	agent_id TEXT NOT NULL,
	prompt TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE queued_messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL,
	project_path TEXT NOT NULL,
	backend TEXT NOT NULL DEFAULT '',
	queue_id TEXT NOT NULL,
	content TEXT NOT NULL,
	files TEXT,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE chat_recommendations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL,
	project_path TEXT NOT NULL DEFAULT '',
	message_id INTEGER NOT NULL DEFAULT 0,
	recommendation TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE btw_questions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL,
	project_path TEXT NOT NULL DEFAULT '',
	anchor_message_id INTEGER NOT NULL DEFAULT 0,
	question TEXT NOT NULL,
	answer TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE project_forges (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_path TEXT NOT NULL,
	platform TEXT NOT NULL,
	host TEXT NOT NULL,
	scheme TEXT NOT NULL DEFAULT '',
	owner TEXT NOT NULL,
	repo TEXT NOT NULL,
	source TEXT NOT NULL DEFAULT 'auto',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE terminal_quick_commands (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	label TEXT NOT NULL,
	command TEXT NOT NULL,
	hidden INTEGER NOT NULL DEFAULT 0,
	auto_execute INTEGER NOT NULL DEFAULT 0,
	sort_order INTEGER NOT NULL DEFAULT 0,
	project_path TEXT DEFAULT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE chat_quick_send (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	label TEXT NOT NULL,
	command TEXT NOT NULL,
	sort_order INTEGER NOT NULL DEFAULT 0,
	project_path TEXT DEFAULT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE session_tags (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	scope TEXT NOT NULL DEFAULT 'project',
	project_path TEXT NOT NULL DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(name, project_path)
);
CREATE TABLE session_tag_links (
	session_id TEXT NOT NULL,
	tag_id INTEGER NOT NULL REFERENCES session_tags(id) ON DELETE CASCADE,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(session_id, tag_id)
);
CREATE TABLE chat_metadata (
	message_id INTEGER PRIMARY KEY,
	mode TEXT DEFAULT '',
	thinking_effort TEXT DEFAULT '',
	transport TEXT DEFAULT '',
	model TEXT DEFAULT '',
	input_tokens INTEGER DEFAULT 0,
	output_tokens INTEGER DEFAULT 0,
	duration_ms INTEGER DEFAULT 0,
	wall_ms INTEGER DEFAULT 0,
	cost_usd REAL DEFAULT 0,
	stop_reason TEXT DEFAULT '',
	is_error INTEGER DEFAULT 0,
	error_message TEXT DEFAULT '',
	cached_read_tokens INTEGER DEFAULT 0,
	cached_write_tokens INTEGER DEFAULT 0,
	thought_tokens INTEGER DEFAULT 0,
	total_tokens INTEGER DEFAULT 0,
	cache_creation_tokens INTEGER DEFAULT 0,
	cache_hit_tokens INTEGER DEFAULT 0,
	cache_miss_tokens INTEGER DEFAULT 0,
	credit REAL DEFAULT 0,
	usage_by_category TEXT DEFAULT '',
	session_id TEXT DEFAULT '',
	request_id TEXT DEFAULT '',
	trace_id TEXT DEFAULT '',
	agent_message_id TEXT DEFAULT '',
	message_request_id TEXT DEFAULT '',
	request_model_name TEXT DEFAULT '',
	response_model_id TEXT DEFAULT '',
	finish_reason TEXT DEFAULT '',
	outcome TEXT DEFAULT '',
	agent_phase TEXT DEFAULT '',
	project_id INTEGER DEFAULT 0,
	backend TEXT DEFAULT '',
	agent_id TEXT DEFAULT '',
	clawbench_session_id TEXT DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE file_shares (
	token TEXT PRIMARY KEY,
	path TEXT NOT NULL,
	name TEXT NOT NULL,
	root TEXT NOT NULL DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE rag_chunks (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL,
	message_id INTEGER NOT NULL,
	chunk_text TEXT NOT NULL,
	project_path TEXT NOT NULL,
	backend TEXT NOT NULL,
	role TEXT NOT NULL,
	created_at DATETIME NOT NULL
);
`

// openLegacyDB creates a database file at the path InitDB expects and applies
// the legacy schema to it, so the next InitDB exercises the upgrade path.
func openLegacyDB(t *testing.T) *sql.DB {
	t.Helper()
	model.DataDir = t.TempDir()
	raw, err := sql.Open("sqlite", filepath.Join(model.DataDir, "ClawBench.db"))
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	if _, err := raw.Exec(legacySchema); err != nil {
		raw.Close()
		t.Fatalf("apply legacy schema: %v", err)
	}
	return raw
}

// TestMigrateProjectsToIDs_MergesEquivalentPaths is the core promise of the
// refactor: two different spellings of the SAME directory must collapse onto one
// projects row, and every referencing row must end up on that row's id.
//
// The spellings differ by a redundant "/./" segment and by a symlink, neither of
// which SQL alone can normalize — this is why the map is built in Go.
func TestMigrateProjectsToIDs_MergesEquivalentPaths(t *testing.T) {
	raw := openLegacyDB(t)
	defer func() { _ = raw.Close() }()

	dir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := symlinkOrSkip(t, dir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	aliased := dir + string(filepath.Separator) + "."
	if _, err := raw.Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title) VALUES ('s1', ?, 'claude', 'a')",
		dir,
	); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := raw.Exec(
		"INSERT INTO chat_history (project_path, role, content, session_id) VALUES (?, 'user', 'hi', 's1')",
		aliased,
	); err != nil {
		t.Fatalf("seed history (aliased path): %v", err)
	}
	if _, err := raw.Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt) VALUES (?, 't', '* * * * *', 'a', 'p')",
		link,
	); err != nil {
		t.Fatalf("seed task (symlinked path): %v", err)
	}
	requireNoError(t, raw.Close())

	requireNoError(t, InitDB(true))
	defer CloseDB()

	var projectCount int
	requireNoError(t, db.QueryRow("SELECT COUNT(*) FROM projects").Scan(&projectCount))
	if projectCount != 1 {
		t.Fatalf("expected the three spellings to merge into 1 project, got %d", projectCount)
	}

	var projectID int64
	requireNoError(t, db.QueryRow("SELECT id FROM projects").Scan(&projectID))

	for _, q := range []struct {
		label string
		sql   string
	}{
		{"chat_sessions", "SELECT project_id FROM chat_sessions WHERE id = 's1'"},
		{"chat_history", "SELECT project_id FROM chat_history WHERE session_id = 's1'"},
		{"scheduled_tasks", "SELECT project_id FROM scheduled_tasks"},
	} {
		var got int64
		if err := db.QueryRow(q.sql).Scan(&got); err != nil {
			t.Fatalf("%s: %v", q.label, err)
		}
		if got != projectID {
			t.Errorf("%s.project_id = %d, want %d", q.label, got, projectID)
		}
	}
}

// TestMigrateProjectsToIDs_DropsEveryProjectPathColumn guards the completion of
// the conversion: a surviving project_path column means some query still reads
// or writes the old shape, and would fail at runtime rather than at build time.
func TestMigrateProjectsToIDs_DropsEveryProjectPathColumn(t *testing.T) {
	raw := openLegacyDB(t)
	defer func() { _ = raw.Close() }()
	if _, err := raw.Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title) VALUES ('s1', '/tmp/proj', 'claude', 'a')",
	); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	requireNoError(t, raw.Close())

	requireNoError(t, InitDB(true))
	defer CloseDB()

	for _, table := range []string{
		"chat_history", "chat_sessions", "recent_projects", "scheduled_tasks",
		"queued_messages", "chat_metadata", "chat_recommendations", "session_tags",
		"btw_questions", "project_forges", "terminal_quick_commands", "chat_quick_send",
		"rag_chunks", "file_shares",
	} {
		var n int
		if err := db.QueryRow(
			"SELECT COUNT(*) FROM pragma_table_info(?) WHERE name='project_path'", table,
		).Scan(&n); err != nil {
			t.Fatalf("inspect %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s still has a project_path column", table)
		}
		var hasID int
		if err := db.QueryRow(
			"SELECT COUNT(*) FROM pragma_table_info(?) WHERE name='project_id'", table,
		).Scan(&hasID); err != nil {
			t.Fatalf("inspect %s: %v", table, err)
		}
		if hasID != 1 {
			t.Errorf("%s is missing project_id", table)
		}
	}
}

// TestMigrateProjectsToIDs_PreservesRowsAndLateColumns checks that the rebuilt
// tables keep every row and every column that a LATER migration added.
//
// chat_sessions is the trap: its createTables definition omits nine columns
// (source_session_id, transport, auto_approve, …) that arrive via separate ALTER
// statements. A rebuild driven by a hard-coded column list would drop them
// silently; reading the stored DDL is what keeps them.
func TestMigrateProjectsToIDs_PreservesRowsAndLateColumns(t *testing.T) {
	raw := openLegacyDB(t)
	defer func() { _ = raw.Close() }()

	if _, err := raw.Exec(`
		INSERT INTO chat_sessions (id, project_path, backend, title, source_session_id, transport, auto_approve, pinned, sort_order)
		VALUES ('s1', '/tmp/proj', 'claude', 'kept', 'parent-1', 'acp', 1, 1, 7)`); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	requireNoError(t, raw.Close())

	requireNoError(t, InitDB(true))
	defer CloseDB()

	var (
		title, sourceSessionID, transport string
		autoApprove, pinned, sortOrder    int
	)
	requireNoError(t, db.QueryRow(`
		SELECT title, source_session_id, transport, auto_approve, pinned, sort_order
		  FROM chat_sessions WHERE id = 's1'`).Scan(
		&title, &sourceSessionID, &transport, &autoApprove, &pinned, &sortOrder))
	if title != "kept" {
		t.Errorf("title = %q, want kept", title)
	}
	if sourceSessionID != "parent-1" || transport != "acp" {
		t.Errorf("late columns lost: source_session_id=%q transport=%q", sourceSessionID, transport)
	}
	if autoApprove != 1 || pinned != 1 || sortOrder != 7 {
		t.Errorf("late flags lost: auto_approve=%d pinned=%d sort_order=%d", autoApprove, pinned, sortOrder)
	}
}

// TestMigrateProjectsToIDs_KeepsGlobalSessionTagsDistinct guards the 0 sentinel.
//
// Global tags live at project_id = 0. If the column were nullable, SQLite's
// "NULLs are distinct" rule would let a second global tag with the same name in,
// silently breaking the (name, scope) uniqueness the tag feature depends on.
func TestMigrateProjectsToIDs_KeepsGlobalSessionTagsDistinct(t *testing.T) {
	raw := openLegacyDB(t)
	defer func() { _ = raw.Close() }()
	// Two GLOBAL tags with the same name predate the refactor (allowed by
	// uniqueness on (name, project_path='') only if they differ in path — so
	// seed one global and one project tag, then assert the merged project keeps
	// them apart).
	if _, err := raw.Exec(
		"INSERT INTO session_tags (name, scope, project_path) VALUES ('bug', 'global', '')",
	); err != nil {
		t.Fatalf("seed global tag: %v", err)
	}
	if _, err := raw.Exec(
		"INSERT INTO session_tags (name, scope, project_path) VALUES ('bug', 'project', '/tmp/proj')",
	); err != nil {
		t.Fatalf("seed project tag: %v", err)
	}
	requireNoError(t, raw.Close())

	requireNoError(t, InitDB(true))
	defer CloseDB()

	var globalID, projectID int64
	requireNoError(t, db.QueryRow(
		"SELECT project_id FROM session_tags WHERE scope = 'global'").Scan(&globalID))
	requireNoError(t, db.QueryRow(
		"SELECT project_id FROM session_tags WHERE scope = 'project'").Scan(&projectID))
	if globalID != GlobalScopeProjectID {
		t.Errorf("global tag project_id = %d, want the 0 sentinel", globalID)
	}
	if projectID == GlobalScopeProjectID {
		t.Error("project tag must not share the global sentinel")
	}

	// The sentinel must still reject a duplicate global name.
	if _, err := db.Exec(
		"INSERT INTO session_tags (name, scope, project_id) VALUES ('bug', 'global', 0)",
	); err == nil {
		t.Error("a second global 'bug' tag must be rejected")
	}
}

// TestMigrateProjectsToIDs_DeduplicatesMergedTags covers the collision the merge
// creates: two projects that each defined "bug" become one project id, so the
// tags collide. The duplicate must be folded into the survivor with its
// assignments repointed — not dropped along with the assignments.
func TestMigrateProjectsToIDs_DeduplicatesMergedTags(t *testing.T) {
	raw := openLegacyDB(t)
	defer func() { _ = raw.Close() }()

	dir := t.TempDir()
	aliased := dir + string(filepath.Separator) + "."
	for _, p := range []string{dir, aliased} {
		if _, err := raw.Exec(
			"INSERT INTO session_tags (name, scope, project_path) VALUES ('bug', 'project', ?)", p,
		); err != nil {
			t.Fatalf("seed tag for %q: %v", p, err)
		}
	}
	// One session tagged with each of the two (soon to be merged) tag ids.
	if _, err := raw.Exec(
		"INSERT INTO session_tag_links (session_id, tag_id) VALUES ('s1', 1), ('s2', 2)",
	); err != nil {
		t.Fatalf("seed links: %v", err)
	}
	requireNoError(t, raw.Close())

	requireNoError(t, InitDB(true))
	defer CloseDB()

	var tagCount int
	requireNoError(t, db.QueryRow("SELECT COUNT(*) FROM session_tags").Scan(&tagCount))
	if tagCount != 1 {
		t.Fatalf("expected the duplicate tag to be merged, got %d rows", tagCount)
	}
	// Both assignments must survive, now pointing at the one surviving tag.
	var linkCount int
	requireNoError(t, db.QueryRow("SELECT COUNT(*) FROM session_tag_links").Scan(&linkCount))
	if linkCount != 2 {
		t.Errorf("tag assignments lost in the merge: got %d, want 2", linkCount)
	}
}

// TestMigrateProjectsToIDs_AttributesFileShares checks the file-share backfill:
// an in-project file lands on its project, and a file outside every project
// falls back to the default project rather than becoming invisible.
func TestMigrateProjectsToIDs_AttributesFileShares(t *testing.T) {
	raw := openLegacyDB(t)
	defer func() { _ = raw.Close() }()

	projectDir := t.TempDir()
	outsideDir := t.TempDir()

	if _, err := raw.Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title) VALUES ('s1', ?, 'claude', 'a')",
		projectDir,
	); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	// The default project (used as the fallback for unattributable shares).
	if _, err := raw.Exec(
		"INSERT INTO recent_projects (project_path, is_default) VALUES (?, 1)", projectDir,
	); err != nil {
		t.Fatalf("seed recent project: %v", err)
	}
	inProject := filepath.Join(projectDir, "notes.md")
	outside := filepath.Join(outsideDir, "other.md")
	for token, p := range map[string]string{"in": inProject, "out": outside} {
		if _, err := raw.Exec(
			"INSERT INTO file_shares (token, path, name, root) VALUES (?, ?, 'n.md', ?)", token, p, projectDir,
		); err != nil {
			t.Fatalf("seed file share %s: %v", token, err)
		}
	}
	requireNoError(t, raw.Close())

	requireNoError(t, InitDB(true))
	defer CloseDB()

	var projectID int64
	requireNoError(t, db.QueryRow("SELECT id FROM projects").Scan(&projectID))

	for _, tc := range []struct {
		token string
		want  int64
		label string
	}{
		{"in", projectID, "in-project share"},
		{"out", projectID, "out-of-project share falls back to the default project"},
	} {
		var got int64
		if err := db.QueryRow("SELECT project_id FROM file_shares WHERE token = ?", tc.token).Scan(&got); err != nil {
			t.Fatalf("%s: %v", tc.label, err)
		}
		if got != tc.want {
			t.Errorf("%s: project_id = %d, want %d", tc.label, got, tc.want)
		}
	}
}

// TestMigrateProjectsToIDs_FoldsForgeOptOut checks that project_meta's one live
// column survives the fold into projects and that the old table is gone.
func TestMigrateProjectsToIDs_FoldsForgeOptOut(t *testing.T) {
	raw := openLegacyDB(t)
	defer func() { _ = raw.Close() }()
	if _, err := raw.Exec(
		"INSERT INTO project_meta (project_path, forge_bind_opt_out) VALUES ('/tmp/proj', 1)",
	); err != nil {
		t.Fatalf("seed project_meta: %v", err)
	}
	requireNoError(t, raw.Close())

	requireNoError(t, InitDB(true))
	defer CloseDB()

	var optOut int
	requireNoError(t, db.QueryRow(
		"SELECT forge_bind_opt_out FROM projects WHERE path = '/tmp/proj'").Scan(&optOut))
	if optOut != 1 {
		t.Errorf("forge_bind_opt_out = %d, want 1", optOut)
	}

	var metaExists int
	requireNoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='project_meta'").Scan(&metaExists))
	if metaExists != 0 {
		t.Error("project_meta must be dropped once folded into projects")
	}
}

// TestMigrateProjectsToIDs_DropsRagVec confirms the vec0 table is dropped so the
// RAG store can recreate it with a project_id metadata column. It is a plain
// table here (vec0 needs the extension), which is enough to prove the DROP runs.
func TestMigrateProjectsToIDs_DropsRagVec(t *testing.T) {
	raw := openLegacyDB(t)
	defer func() { _ = raw.Close() }()
	if _, err := raw.Exec("CREATE TABLE rag_vec (rowid INTEGER PRIMARY KEY, project_path TEXT)"); err != nil {
		t.Fatalf("seed rag_vec: %v", err)
	}
	if _, err := raw.Exec(
		"INSERT INTO rag_chunks (session_id, message_id, chunk_text, project_path, backend, role, created_at) VALUES ('s1', 1, 'x', '/tmp/proj', 'claude', 'user', '2026-01-01')",
	); err != nil {
		t.Fatalf("seed rag_chunks: %v", err)
	}
	requireNoError(t, raw.Close())

	requireNoError(t, InitDB(true))
	defer CloseDB()

	var vecExists int
	requireNoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='rag_vec'").Scan(&vecExists))
	if vecExists != 0 {
		t.Error("rag_vec must be dropped so the RAG store rebuilds it with project_id")
	}

	// The chunk row survives with its project resolved.
	var projectID int64
	requireNoError(t, db.QueryRow("SELECT project_id FROM rag_chunks").Scan(&projectID))
	if projectID == 0 {
		t.Error("rag_chunks row lost its project attribution")
	}
}

// TestMigrateProjectsToIDs_IsIdempotent proves a second startup is a no-op: the
// guard is the absence of chat_history.project_path.
func TestMigrateProjectsToIDs_IsIdempotent(t *testing.T) {
	raw := openLegacyDB(t)
	defer func() { _ = raw.Close() }()
	if _, err := raw.Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title) VALUES ('s1', '/tmp/proj', 'claude', 'a')",
	); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	requireNoError(t, raw.Close())

	requireNoError(t, InitDB(true))
	var firstID int64
	requireNoError(t, db.QueryRow("SELECT id FROM projects").Scan(&firstID))
	CloseDB()

	// Second startup must not re-run the migration nor duplicate the project.
	requireNoError(t, InitDB(true))
	defer CloseDB()
	var count int
	requireNoError(t, db.QueryRow("SELECT COUNT(*) FROM projects").Scan(&count))
	if count != 1 {
		t.Errorf("projects rows = %d after a second InitDB, want 1", count)
	}
	var secondID int64
	requireNoError(t, db.QueryRow("SELECT id FROM projects").Scan(&secondID))
	if secondID != firstID {
		t.Errorf("project id changed across restarts: %d → %d", firstID, secondID)
	}
}

// TestRenameProject_FollowsEveryTable is the payoff: renaming a project updates
// one row and every project-scoped table follows, because they all reference the
// id rather than the path.
func TestRenameProject_FollowsEveryTable(t *testing.T) {
	raw := openLegacyDB(t)
	defer func() { _ = raw.Close() }()
	oldDir := t.TempDir()
	newDir := t.TempDir()
	if _, err := raw.Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title) VALUES ('s1', ?, 'claude', 'a')", oldDir,
	); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := raw.Exec(
		"INSERT INTO chat_history (project_path, role, content, session_id) VALUES (?, 'user', 'hi', 's1')", oldDir,
	); err != nil {
		t.Fatalf("seed history: %v", err)
	}
	requireNoError(t, raw.Close())

	requireNoError(t, InitDB(true))
	defer CloseDB()

	requireNoError(t, RenameProject(oldDir, newDir))

	// The session is still addressable under the NEW path — the whole point.
	gotID, ok, err := ProjectIDByPath(newDir)
	requireNoError(t, err)
	if !ok {
		t.Fatal("project not found under its new path")
	}
	var sessionProjectID int64
	requireNoError(t, db.QueryRow("SELECT project_id FROM chat_sessions WHERE id = 's1'").Scan(&sessionProjectID))
	if sessionProjectID != gotID {
		t.Errorf("session project_id = %d, want %d", sessionProjectID, gotID)
	}
	// And the old path no longer resolves.
	if _, ok, _ := ProjectIDByPath(oldDir); ok {
		t.Error("the old path must stop resolving after a rename")
	}
	// No extra project row was created.
	var count int
	requireNoError(t, db.QueryRow("SELECT COUNT(*) FROM projects").Scan(&count))
	if count != 1 {
		t.Errorf("projects rows = %d, want 1", count)
	}
}

// TestRenameProject_RejectsCollision documents that renaming onto an existing
// project is an error rather than a silent merge.
func TestRenameProject_RejectsCollision(t *testing.T) {
	raw := openLegacyDB(t)
	defer func() { _ = raw.Close() }()
	dirA := t.TempDir()
	dirB := t.TempDir()
	if _, err := raw.Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title) VALUES ('a', ?, 'claude', 'a'), ('b', ?, 'claude', 'b')",
		dirA, dirB,
	); err != nil {
		t.Fatalf("seed sessions: %v", err)
	}
	requireNoError(t, raw.Close())

	requireNoError(t, InitDB(true))
	defer CloseDB()

	if err := RenameProject(dirA, dirB); err == nil {
		t.Error("renaming onto an existing project must fail")
	}
}

// TestMigrateProjectsToIDs_DeduplicatesMergedProjectScopes is the regression for
// an upgrade that could not complete at all: canonicalization merges two
// spellings of one directory into a single project, which puts two rows into the
// same project scope. The rebuilt tables enforce one row per scope, so without
// deduplication the copy (or the index createTables recreates right after) fails
// with a UNIQUE violation — and InitDB's error makes the server exit, so the
// user cannot start the app at all.
//
// Each table needs its own strategy, so all three are covered:
//   - recent_projects: one row per project; the most recent access survives.
//   - project_forges:  one binding per project; a manual binding beats an auto
//     one, so merging must not silently downgrade the user's explicit choice.
//   - terminal_quick_commands: many commands per project, but only one may be
//     auto-executed — the extras are demoted, never deleted.
func TestMigrateProjectsToIDs_DeduplicatesMergedProjectScopes(t *testing.T) {
	raw := openLegacyDB(t)
	defer func() { _ = raw.Close() }()

	dir := t.TempDir()
	// A trailing "/." is a second legal spelling of the same directory under the
	// legacy constraints, so the migration canonicalizes both onto one project.
	aliased := dir + string(filepath.Separator) + "."

	seeds := []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO recent_projects (project_path, is_default, accessed_at) VALUES (?, 0, '2024-01-01 00:00:00')", []any{dir}},
		{"INSERT INTO recent_projects (project_path, is_default, accessed_at) VALUES (?, 1, '2024-06-01 00:00:00')", []any{aliased}},
		{"INSERT INTO project_forges (project_path, platform, host, owner, repo, source) VALUES (?, 'github', 'github.com', 'acme', 'w', 'auto')", []any{dir}},
		{"INSERT INTO project_forges (project_path, platform, host, owner, repo, source) VALUES (?, 'github', 'github.com', 'acme', 'w', 'manual')", []any{aliased}},
		{"INSERT INTO terminal_quick_commands (label, command, auto_execute, project_path) VALUES ('A', 'a', 1, ?)", []any{dir}},
		{"INSERT INTO terminal_quick_commands (label, command, auto_execute, project_path) VALUES ('B', 'b', 1, ?)", []any{aliased}},
	}
	for _, s := range seeds {
		if _, err := raw.Exec(s.sql, s.args...); err != nil {
			t.Fatalf("seed %q: %v", s.sql, err)
		}
	}
	requireNoError(t, raw.Close())

	requireNoError(t, InitDB(true))
	defer CloseDB()

	var recentCount int
	requireNoError(t, db.QueryRow("SELECT COUNT(*) FROM recent_projects").Scan(&recentCount))
	if recentCount != 1 {
		t.Errorf("merged project must have exactly one recents row, got %d", recentCount)
	}

	// The newer access is the one worth keeping.
	var keptDefault int
	requireNoError(t, db.QueryRow("SELECT is_default FROM recent_projects").Scan(&keptDefault))
	if keptDefault != 1 {
		t.Error("the most recently accessed recents row must survive")
	}

	var forgeCount int
	var forgeSource string
	requireNoError(t, db.QueryRow("SELECT COUNT(*) FROM project_forges").Scan(&forgeCount))
	requireNoError(t, db.QueryRow("SELECT source FROM project_forges").Scan(&forgeSource))
	if forgeCount != 1 {
		t.Errorf("merged project must have exactly one forge binding, got %d", forgeCount)
	}
	if forgeSource != "manual" {
		t.Errorf("an explicit binding must not be downgraded to auto by the merge, got %q", forgeSource)
	}

	// Both commands survive; only the auto-execute flag is deduplicated.
	var cmdCount, autoCount int
	requireNoError(t, db.QueryRow("SELECT COUNT(*) FROM terminal_quick_commands").Scan(&cmdCount))
	requireNoError(t, db.QueryRow("SELECT COUNT(*) FROM terminal_quick_commands WHERE auto_execute = 1").Scan(&autoCount))
	if cmdCount != 2 {
		t.Errorf("merged quick commands must be kept, not deleted, got %d", cmdCount)
	}
	if autoCount != 1 {
		t.Errorf("only one auto-execute command may remain in the merged scope, got %d", autoCount)
	}
}

// TestRenameProject_NotifiesHooks pins the invalidation contract for caches
// outside this package. internal/rag keeps its own path→id cache and cannot be
// imported here (it imports this package), so it registers a hook; if the rename
// stops notifying, that cache keeps serving the pre-rename mapping and a project
// later created at the freed-up old path resolves to the renamed project's id.
func TestRenameProject_NotifiesHooks(t *testing.T) {
	raw := openLegacyDB(t)
	defer func() { _ = raw.Close() }()
	dirA := t.TempDir()
	dirB := t.TempDir()
	if _, err := raw.Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title) VALUES ('a', ?, 'claude', 'a')",
		dirA,
	); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	requireNoError(t, raw.Close())

	requireNoError(t, InitDB(true))
	defer CloseDB()

	var got []string
	RegisterProjectRenamedHook(func(paths ...string) { got = append(got, paths...) })
	defer func() {
		projectRenamedMu.Lock()
		projectRenamedHooks = nil
		projectRenamedMu.Unlock()
	}()

	requireNoError(t, RenameProject(dirA, dirB))

	want := []string{NormalizeProjectPath(dirA), NormalizeProjectPath(dirB)}
	if len(got) != len(want) {
		t.Fatalf("hook got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("hook arg %d = %q, want %q", i, got[i], want[i])
		}
	}
}
