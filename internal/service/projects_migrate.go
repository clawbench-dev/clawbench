package service

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
)

// legacyProjectPathIndexes are the indexes that reference a project_path column
// and therefore must be dropped before that column can be dropped: SQLite
// refuses "ALTER TABLE ... DROP COLUMN" while any index references the column
// (the same constraint the chat_history.deleted migration works around).
//
// They are not recreated here — createTables recreates every one of them on
// project_id immediately after this migration returns.
var legacyProjectPathIndexes = []string{
	"idx_history_session",
	"idx_sessions_project_backend",
	"idx_sessions_type",
	"idx_sessions_order",
	"idx_tasks_project",
	"idx_history_unread",
	"idx_history_sess_unread",
	"idx_chat_metadata_project_created",
	"idx_session_tags_project",
	"idx_quick_commands_auto_execute",
	"idx_project_forges_path",
	"idx_rag_chunks_project",
}

// legacyProjectPathTables lists every table that may carry a project_path
// column, for the migration's "does this database need converting?" probe. It is
// the union of the in-place and rebuild sets.
var legacyProjectPathTables = []string{
	"chat_history", "chat_sessions", "recent_projects", "project_meta",
	"scheduled_tasks", "queued_messages", tableTerminalQuickCommands,
	tableChatQuickSend, "chat_metadata", "chat_recommendations", "session_tags",
	"btw_questions", "project_forges", "rag_chunks",
}

// Table names referenced in more than one place, hoisted to constants so a
// rename cannot drift between the probe list, the conversion list, and the
// per-project dedupe specs.
const (
	tableTerminalQuickCommands = "terminal_quick_commands"
	tableChatQuickSend         = "chat_quick_send"
)

// legacyPathTables are the tables whose project_path column converts in place:
// add project_id, backfill it from the path→id map, then drop project_path.
//
// nullable marks the two quick-command tables, where project_path IS NULL means
// "global scope" (a command available in every project). Those must keep NULL
// rather than falling back to the 0 sentinel, so their backfill is a plain
// correlated subquery instead of COALESCE(..., 0).
var legacyPathTables = []struct {
	name     string
	nullable bool
}{
	{"chat_history", false},
	{"scheduled_tasks", false},
	{"queued_messages", false},
	{"chat_metadata", false},
	{"chat_recommendations", false},
	{"btw_questions", false},
	{"project_forges", false},
	{tableTerminalQuickCommands, true},
	{tableChatQuickSend, true},
	{"rag_chunks", false},
}

// legacyRebuildTables are the tables whose project_path is part of a UNIQUE or
// PRIMARY KEY constraint. SQLite cannot drop such a column or alter the
// constraint, so these are rebuilt: create with the new definition, copy, swap.
//
// The rewrite rules are applied to the table's STORED CREATE TABLE statement
// (read from sqlite_master) rather than to a hard-coded column list, so any
// column a later migration added is carried over automatically. chat_sessions
// is the reason this matters: its createTables definition is missing nine
// columns (source_session_id, transport, auto_approve, context_state,
// compacted, title_renamed, title_source, pinned, sort_order) that are added by
// separate ALTER statements — a hard-coded copy list would silently drop them.
var legacyRebuildTables = []struct {
	name     string
	rewrites [][2]string
}{
	{"chat_sessions", [][2]string{
		{"project_path TEXT NOT NULL", "project_id INTEGER NOT NULL"},
		{"UNIQUE(project_path, backend, id)", "UNIQUE(backend, id)"},
	}},
	{"recent_projects", [][2]string{
		{"project_path TEXT UNIQUE NOT NULL", "project_id INTEGER NOT NULL UNIQUE"},
	}},
	{"session_tags", [][2]string{
		{"project_path TEXT NOT NULL DEFAULT ''", "project_id INTEGER NOT NULL DEFAULT 0"},
		{"UNIQUE(name, project_path)", "UNIQUE(name, project_id)"},
	}},
}

// migrateProjectsToIDs converts every project-path column to a project id,
// backed by the new projects registry.
//
// It runs as ONE transaction, and every statement goes through tx.Exec: the
// write mutex is already held by WriteBegin, so calling WriteExec/WriteBegin
// here would deadlock.
//
// The path→id mapping cannot be built in SQL alone: canonicalization
// (NormalizeProjectPath) resolves symlinks and relative segments, which SQLite
// cannot do. So the distinct paths are read out, canonicalized in Go, and
// written back into a temp table that the per-table backfills join against.
func migrateProjectsToIDs() error { //nolint:gocyclo // ordered multi-table conversion: index drops, path→id map, in-place conversions, rebuilds, RAG, shares; the sequence is the contract
	tx, err := WriteBegin()
	if err != nil {
		return err
	}
	defer writeMu.Unlock()
	defer func() { _ = tx.Rollback() }()

	// The projects table does not exist yet on an upgrading database: this runs
	// before createTables, which is where the DDL normally lives.
	if _, ddlErr := tx.ExecContext(context.Background(), ProjectsDDL); ddlErr != nil {
		return fmt.Errorf("create projects table: %w", ddlErr)
	}

	// ── Fold project_meta into projects ──────────────────────────────────────
	// project_meta was path-keyed with a single live column (forge_bind_opt_out);
	// its next_session_number was written and read by nothing. Folding it in
	// avoids a second path-keyed table.
	hasProjectMeta, err := txTableExists(tx, "project_meta")
	if err != nil {
		return err
	}
	if hasProjectMeta {
		if err := foldProjectMetaIntoProjects(tx); err != nil {
			return err
		}
	}

	// ── Indexes first ────────────────────────────────────────────────────────
	// DROP COLUMN fails while any index still references the column.
	for _, idx := range legacyProjectPathIndexes {
		if _, err := tx.ExecContext(context.Background(), "DROP INDEX IF EXISTS "+idx); err != nil {
			return fmt.Errorf("drop index %s: %w", idx, err)
		}
	}

	// ── Build the path → id map ──────────────────────────────────────────────
	if err := buildProjectIDMap(tx); err != nil {
		return err
	}

	// ── Collapse per-project duplicates ──────────────────────────────────────
	// Canonicalization merges spellings of one directory (a symlink and its
	// target, or a trailing "/."), which can put two rows into the SAME project
	// scope. Where the new schema enforces one row per project, the extra rows
	// must go before the rebuild recreates that constraint — otherwise the copy
	// fails and, because InitDB's error exits the process, the server refuses to
	// start after an upgrade.
	//
	// Coverage comes from projectScopeSpecs, which is hand-maintained: this call
	// does NOT discover the tables automatically. See the note on that variable.
	if err := dropDuplicateScopedRows(tx); err != nil {
		return err
	}

	// ── In-place conversions ─────────────────────────────────────────────────
	for _, t := range legacyPathTables {
		if err := convertLegacyPathColumn(tx, t.name, t.nullable); err != nil {
			return err
		}
	}

	// ── Rebuilds (UNIQUE/PK contains project_path) ───────────────────────────
	// session_tag_links is handled first and separately: it has an inbound
	// foreign key to session_tags(id), and dropping session_tags with
	// PRAGMA foreign_keys=ON fires that FK's ON DELETE CASCADE, which would
	// silently delete every tag assignment. Rebuilding the links table around
	// the tags rebuild is also where duplicate tags (two projects that each
	// defined "bug", now merged onto one project id) get repointed.
	if err := rebuildSessionTagsAndLinks(tx); err != nil {
		return err
	}
	for _, t := range legacyRebuildTables {
		if t.name == "session_tags" {
			continue // done above, together with its links
		}
		if err := rebuildLegacyTable(tx, t.name, t.rewrites); err != nil {
			return err
		}
	}

	// ── RAG ──────────────────────────────────────────────────────────────────
	// rag_vec is a vec0 virtual table whose project_path is a metadata column,
	// so it cannot be altered; drop it and let the RAG store rebuild it. The
	// store's migrateEmbeddingsToVec() reconstructs every vector from the
	// float64 BLOB preserved in rag_chunks.embedding on the next rag.Init, so
	// this does NOT trigger re-embedding (the embedder is never called).
	// The service package cannot recreate it: vec0 needs the extension and a
	// known embedding dimension, both owned by internal/rag.
	if _, err := tx.ExecContext(context.Background(), "DROP TABLE IF EXISTS rag_vec"); err != nil {
		return fmt.Errorf("drop rag_vec: %w", err)
	}
	// Rows flagged embedded but holding no vector can never be recovered from a
	// BLOB. Clear the flag so the backfill pass re-embeds them instead of
	// leaving them invisible to vector search forever (the backfill only looks
	// at has_embedding = 0).
	//
	// Guarded on the columns: rag_chunks is owned by internal/rag and its
	// has_embedding / embedding columns are added by that package's
	// addMissingColumns, which runs AFTER InitDB. A database old enough to
	// predate them must not abort the migration here.
	if hasFlag, err := txColumnExists(tx, "rag_chunks", "has_embedding"); err != nil {
		return err
	} else if hasFlag {
		if _, err := tx.ExecContext(context.Background(),
			"UPDATE rag_chunks SET has_embedding = 0 WHERE has_embedding = 1 AND embedding IS NULL",
		); err != nil {
			return fmt.Errorf("repair rag_chunks embedding flags: %w", err)
		}
	}

	// ── file_shares gains its owning project ─────────────────────────────────
	if err := backfillFileShareProject(tx); err != nil {
		return err
	}

	if _, err := tx.ExecContext(context.Background(), "DROP TABLE IF EXISTS _proj_map"); err != nil {
		return fmt.Errorf("drop temp project map: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit projects migration: %w", err)
	}

	// The cache is keyed by canonical path; nothing is cached yet at startup,
	// but clear it so a repeated InitDB in tests cannot serve stale ids.
	ResetProjectIDCacheForTest()
	slog.Info("migrated project paths to project ids")
	return nil
}

// foldProjectMetaIntoProjects copies project_meta rows into projects and drops
// the old table. forge_bind_opt_out is the only live column; created_at /
// updated_at are carried over so the registry keeps its history.
func foldProjectMetaIntoProjects(tx *sql.Tx) error {
	rows, err := tx.QueryContext(context.Background(), "SELECT project_path, forge_bind_opt_out, created_at, updated_at FROM project_meta")
	if err != nil {
		return fmt.Errorf("read project_meta: %w", err)
	}
	defer func() { _ = rows.Close() }()
	type metaRow struct {
		path      string
		optOut    int
		createdAt any
		updatedAt any
	}
	var metas []metaRow
	for rows.Next() {
		var m metaRow
		if err := rows.Scan(&m.path, &m.optOut, &m.createdAt, &m.updatedAt); err != nil {
			return fmt.Errorf("scan project_meta: %w", err)
		}
		metas = append(metas, m)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate project_meta: %w", err)
	}

	for _, m := range metas {
		canon := NormalizeProjectPath(m.path)
		if canon == "" {
			continue
		}
		// ON CONFLICT keeps an existing projects row (created by a sibling
		// table's backfill) and only lifts the opt-out flag onto it.
		if _, err := tx.ExecContext(context.Background(),
			`INSERT INTO projects (path, forge_bind_opt_out, created_at, updated_at)
			 VALUES (?, ?, ?, ?)
			 ON CONFLICT(path) DO UPDATE SET
			   forge_bind_opt_out = excluded.forge_bind_opt_out,
			   updated_at = excluded.updated_at`,
			canon, m.optOut, m.createdAt, m.updatedAt,
		); err != nil {
			return fmt.Errorf("fold project_meta row %q: %w", m.path, err)
		}
	}

	if _, err := tx.ExecContext(context.Background(), "DROP TABLE project_meta"); err != nil {
		return fmt.Errorf("drop project_meta: %w", err)
	}
	return nil
}

// buildProjectIDMap reads every distinct project path from every legacy table,
// canonicalizes it, registers it in projects, and records the raw-spelling →
// id mapping in a temp table so each table's backfill is one UPDATE.
//
// The temp table is per-connection and WriteBegin pins one connection for the
// transaction, so it is visible to every later statement here.
func buildProjectIDMap(tx *sql.Tx) error {
	if _, err := tx.ExecContext(context.Background(),
		"CREATE TEMP TABLE _proj_map (raw TEXT PRIMARY KEY, project_id INTEGER NOT NULL)",
	); err != nil {
		return fmt.Errorf("create temp project map: %w", err)
	}

	rawPaths, err := collectDistinctProjectPaths(tx)
	if err != nil {
		return err
	}

	// canon → id, so two raw spellings of one directory share a single row.
	canonIDs := map[string]int64{}
	for _, raw := range rawPaths {
		canon := NormalizeProjectPath(raw)
		if canon == "" {
			continue
		}
		id, ok := canonIDs[canon]
		if !ok {
			if _, err := tx.ExecContext(context.Background(),
				"INSERT INTO projects (path) VALUES (?) ON CONFLICT(path) DO NOTHING", canon,
			); err != nil {
				return fmt.Errorf("insert project %q: %w", canon, err)
			}
			if err := tx.QueryRowContext(context.Background(), "SELECT id FROM projects WHERE path = ?", canon).Scan(&id); err != nil {
				return fmt.Errorf("read project id for %q: %w", canon, err)
			}
			canonIDs[canon] = id
		}
		if _, err := tx.ExecContext(context.Background(),
			"INSERT INTO _proj_map (raw, project_id) VALUES (?, ?) ON CONFLICT(raw) DO NOTHING",
			raw, id,
		); err != nil {
			return fmt.Errorf("map project path %q: %w", raw, err)
		}
	}
	return nil
}

// collectDistinctProjectPaths gathers every non-empty project_path across the
// legacy tables, skipping tables that do not exist (a database old enough to
// predate project scoping entirely, or one that never created a RAG table).
func collectDistinctProjectPaths(tx *sql.Tx) ([]string, error) {
	seen := map[string]bool{}
	var out []string

	tables := make([]string, 0, len(legacyPathTables)+len(legacyRebuildTables))
	for _, t := range legacyPathTables {
		tables = append(tables, t.name)
	}
	for _, t := range legacyRebuildTables {
		tables = append(tables, t.name)
	}

	for _, table := range tables {
		ok, err := txTableExists(tx, table)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		hasCol, err := txColumnExists(tx, table, "project_path")
		if err != nil {
			return nil, err
		}
		if !hasCol {
			continue
		}
		paths, err := distinctProjectPathsInTable(tx, table)
		if err != nil {
			return nil, err
		}
		for _, p := range paths {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out, nil
}

// distinctProjectPathsInTable reads the non-empty project_path values of one
// table. Scoped to its own function so the rows handle is closed before the
// caller's next table is probed, rather than accumulating deferred closes
// across the whole probe loop.
func distinctProjectPathsInTable(tx *sql.Tx, table string) ([]string, error) {
	rows, err := tx.QueryContext(context.Background(),
		"SELECT DISTINCT project_path FROM "+table+" WHERE project_path IS NOT NULL AND project_path != ''",
	)
	if err != nil {
		return nil, fmt.Errorf("collect project paths from %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scan project path from %s: %w", table, err)
		}
		paths = append(paths, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project paths from %s: %w", table, err)
	}
	return paths, nil
}

// convertLegacyPathColumn adds project_id to a table, backfills it from the
// path→id map, and drops the old project_path column.
func convertLegacyPathColumn(tx *sql.Tx, table string, nullable bool) error {
	ok, err := txTableExists(tx, table)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	hasCol, err := txColumnExists(tx, table, "project_path")
	if err != nil {
		return err
	}
	if !hasCol {
		return nil // already converted (idempotent rerun)
	}

	colType := "INTEGER NOT NULL DEFAULT 0"
	if nullable {
		colType = "INTEGER DEFAULT NULL"
	}
	if _, err := tx.ExecContext(context.Background(), "ALTER TABLE "+table+" ADD COLUMN project_id "+colType); err != nil {
		return fmt.Errorf("add project_id to %s: %w", table, err)
	}

	// A NULL project_path (global scope) must stay NULL: the correlated subquery
	// yields NULL for it, and only the non-nullable tables coalesce to the 0
	// sentinel so their NOT NULL constraint holds.
	backfill := "UPDATE " + table + " SET project_id = (SELECT project_id FROM _proj_map WHERE raw = " + table + ".project_path)"
	if !nullable {
		backfill = "UPDATE " + table + " SET project_id = COALESCE((SELECT project_id FROM _proj_map WHERE raw = " + table + ".project_path), 0)"
	}
	if _, err := tx.ExecContext(context.Background(), backfill); err != nil {
		return fmt.Errorf("backfill project_id on %s: %w", table, err)
	}

	if _, err := tx.ExecContext(context.Background(), "ALTER TABLE "+table+" DROP COLUMN project_path"); err != nil {
		return fmt.Errorf("drop project_path from %s: %w", table, err)
	}
	return nil
}

// projectScopeSpec names one table's "one row per project" rule: the columns
// that identify a row within a project scope, plus the ordering that decides
// which duplicate survives.
type projectScopeSpec struct {
	table string
	// keyExprs are the non-project columns forming the uniqueness rule. Empty
	// means the project itself is the whole key (at most one row per project).
	keyExprs []string
	// keepOrder is the ORDER BY deciding the survivor; the first row wins. When
	// empty, the lowest id wins (oldest row, deterministic).
	keepOrder string
	// demoteFlag, when set, names a 0/1 column to clear on the losers instead of
	// deleting them. Used where several rows may share a project scope and only
	// their flag value must be unique.
	demoteFlag string
}

// projectScopeSpecs are the tables whose rebuilt schema allows at most one row
// per project (or per project+key). Canonicalization can merge two legacy rows
// into one scope, so they must be deduplicated before the rebuild recreates the
// constraint. project_forges and recent_projects also carry a UNIQUE index that
// createTables recreates after the migration, so a leftover duplicate would fail
// there too, just later.
//
// This is a HAND-MAINTAINED list, not a derived one. A table that gains a
// UNIQUE(...project_id) constraint must be added here, or the rebuild's
// INSERT ... SELECT fails on the duplicate and — because InitDB's error exits
// the process — the server refuses to start after an upgrade.
// TestRebuildTables_ProjectScopedUniquenessIsDeduplicated enforces that.
//
// session_tags is deliberately absent: it carries UNIQUE(name, project_id) but
// is deduplicated by rebuildSessionTagsAndLinks instead, which has to run
// separately anyway to preserve its inbound foreign key from session_tag_links.
var projectScopeSpecs = []projectScopeSpec{
	// UNIQUE(project_id): one recents entry per project. Keep the most recently
	// accessed, so the merge does not lose the newer "when did I last open it".
	{table: "recent_projects", keepOrder: "accessed_at DESC, id DESC"},
	// UNIQUE(project_id) via idx_project_forges_path: one binding per project.
	// Prefer a manual binding over an auto one, then the most recent.
	{table: "project_forges", keepOrder: "CASE source WHEN 'manual' THEN 0 ELSE 1 END, updated_at DESC, id DESC"},
	// Unique per project scope, not per project: many commands may be scoped to
	// one project, but only one may be auto-executed. Clearing the flag on the
	// losers (rather than deleting them) keeps the user's commands.
	{
		table: tableTerminalQuickCommands, keyExprs: []string{"auto_execute = 1"},
		keepOrder: "id DESC", demoteFlag: "auto_execute",
	},
}

// dropDuplicateScopedRows removes (or, for the auto-execute flag, demotes) rows
// that canonicalization merged into a single project scope, so the rebuild can
// recreate each table's per-project uniqueness.
func dropDuplicateScopedRows(tx *sql.Tx) error {
	for _, spec := range projectScopeSpecs {
		ok, err := txTableExists(tx, spec.table)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		hasCol, err := txColumnExists(tx, spec.table, "project_path")
		if err != nil {
			return err
		}
		if !hasCol {
			continue // already converted (idempotent rerun)
		}

		// Scope the row by its canonical project id; an unmappable path keeps the
		// 0 sentinel, which is also what the rebuild's COALESCE produces.
		scope := "COALESCE((SELECT project_id FROM _proj_map WHERE raw = " + spec.table + ".project_path), 0)"
		partition := scope
		for _, k := range spec.keyExprs {
			partition += ", " + k
		}
		order := spec.keepOrder
		if order == "" {
			order = "id ASC"
		}

		// ROW_NUMBER over the merged scope: 1 is the survivor. Written as a
		// subquery so the ordering expression is not repeated in the WHERE.
		ranked := "SELECT id, ROW_NUMBER() OVER (PARTITION BY " + partition + " ORDER BY " + order + ") AS rn FROM " + spec.table
		var stmt string
		if spec.demoteFlag != "" {
			stmt = "UPDATE " + spec.table + " SET " + spec.demoteFlag + " = 0 WHERE id IN (SELECT id FROM (" + ranked + ") WHERE rn > 1)"
		} else {
			stmt = "DELETE FROM " + spec.table + " WHERE id IN (SELECT id FROM (" + ranked + ") WHERE rn > 1)"
		}
		if _, err := tx.ExecContext(context.Background(), stmt); err != nil {
			return fmt.Errorf("dedupe %s per project scope: %w", spec.table, err)
		}
	}
	return nil
}

// rebuildLegacyTable recreates a table whose project_path sits in a UNIQUE or
// PRIMARY KEY constraint, which SQLite cannot alter in place.
//
// The new table is built by rewriting the table's stored DDL (so later-added
// columns survive), the rows are copied with project_path resolved to an id,
// and the two tables are swapped.
func rebuildLegacyTable(tx *sql.Tx, table string, rewrites [][2]string) error { //nolint:gocyclo // existence/column probes + DDL rewrite + copy + swap, each step with its own error branch
	ok, err := txTableExists(tx, table)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	hasCol, err := txColumnExists(tx, table, "project_path")
	if err != nil {
		return err
	}
	if !hasCol {
		return nil // already converted (idempotent rerun)
	}

	newTable := table + "_projects_new"
	stored, err := txCreateTableSQL(tx, table)
	if err != nil {
		return err
	}
	newDDL := strings.Replace(stored, "CREATE TABLE "+table, "CREATE TABLE "+newTable, 1)
	if newDDL == stored {
		// Quoted name variant (CREATE TABLE "chat_sessions").
		newDDL = strings.Replace(stored, "CREATE TABLE \""+table+"\"", "CREATE TABLE "+newTable, 1)
	}
	for _, rw := range rewrites {
		newDDL = strings.ReplaceAll(newDDL, rw[0], rw[1])
	}
	if strings.Contains(newDDL, "project_path") {
		return fmt.Errorf("rebuild %s: project_path survived the DDL rewrite", table)
	}

	if _, createErr := tx.ExecContext(context.Background(), newDDL); createErr != nil {
		return fmt.Errorf("create %s: %w", newTable, createErr)
	}

	cols, err := txColumnNames(tx, table)
	if err != nil {
		return err
	}
	insertCols := make([]string, 0, len(cols))
	selectExprs := make([]string, 0, len(cols))
	for _, c := range cols {
		if c == "project_path" {
			insertCols = append(insertCols, "project_id")
			selectExprs = append(selectExprs, "COALESCE((SELECT project_id FROM _proj_map WHERE raw = "+table+".project_path), 0)")
			continue
		}
		insertCols = append(insertCols, c)
		selectExprs = append(selectExprs, c)
	}
	copySQL := "INSERT INTO " + newTable + " (" + strings.Join(insertCols, ", ") + ") " +
		"SELECT " + strings.Join(selectExprs, ", ") + " FROM " + table
	if _, err := tx.ExecContext(context.Background(), copySQL); err != nil {
		return fmt.Errorf("copy %s rows: %w", table, err)
	}

	if _, err := tx.ExecContext(context.Background(), "DROP TABLE "+table); err != nil {
		return fmt.Errorf("drop %s: %w", table, err)
	}
	if _, err := tx.ExecContext(context.Background(), "ALTER TABLE "+newTable+" RENAME TO "+table); err != nil {
		return fmt.Errorf("rename %s: %w", newTable, err)
	}
	return nil
}

// linkRow is one session_tag_links row as the tags rebuild reads it.
type linkRow struct {
	sessionID string
	tagID     int64
	createdAt any
}

// tagRow is one session_tags row with its project id already resolved.
type tagRow struct {
	id        int64
	name      string
	scope     string
	projectID int64
	createdAt any
}

// readSessionTagLinks reads every tag assignment. The rows handle is closed
// before returning so the caller can drop the table immediately after.
func readSessionTagLinks(tx *sql.Tx) ([]linkRow, error) {
	rows, err := tx.QueryContext(context.Background(), "SELECT session_id, tag_id, created_at FROM session_tag_links")
	if err != nil {
		return nil, fmt.Errorf("read session_tag_links: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var links []linkRow
	for rows.Next() {
		var l linkRow
		if err := rows.Scan(&l.sessionID, &l.tagID, &l.createdAt); err != nil {
			return nil, fmt.Errorf("scan session_tag_links: %w", err)
		}
		links = append(links, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session_tag_links: %w", err)
	}
	return links, nil
}

// readSessionTags reads every tag with its project id resolved through the
// path→id map built earlier in the migration.
func readSessionTags(tx *sql.Tx) ([]tagRow, error) {
	rows, err := tx.QueryContext(context.Background(),
		`SELECT id, name, scope,
		        COALESCE((SELECT project_id FROM _proj_map WHERE raw = session_tags.project_path), 0),
		        created_at
		   FROM session_tags
		  ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("read session_tags: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var tags []tagRow
	for rows.Next() {
		var t tagRow
		if err := rows.Scan(&t.id, &t.name, &t.scope, &t.projectID, &t.createdAt); err != nil {
			return nil, fmt.Errorf("scan session_tags: %w", err)
		}
		tags = append(tags, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session_tags: %w", err)
	}
	return tags, nil
}

// rebuildSessionTagsAndLinks converts session_tags to a project id and rebuilds
// session_tag_links around it.
//
// Two problems force this shape:
//
//   - session_tag_links has a foreign key to session_tags(id) with ON DELETE
//     CASCADE. With PRAGMA foreign_keys=ON (the default here), dropping
//     session_tags fires that cascade and deletes every tag assignment, so the
//     links table has to be dropped first — it has no inbound foreign keys of
//     its own, so removing it is safe.
//
//   - Canonicalization merges projects, so two projects that each defined a tag
//     named "bug" collapse onto one project_id and would violate
//     UNIQUE(name, project_id). The duplicate is dropped and its assignments are
//     repointed at the surviving tag row.
func rebuildSessionTagsAndLinks(tx *sql.Tx) error { //nolint:gocyclo // read links, read+dedupe tags, rebuild both tables, restore with remap; the foreign-key ordering is the contract
	tagsExist, err := txTableExists(tx, "session_tags")
	if err != nil {
		return err
	}
	if !tagsExist {
		return nil
	}
	hasCol, err := txColumnExists(tx, "session_tags", "project_path")
	if err != nil {
		return err
	}
	if !hasCol {
		return nil // already converted (idempotent rerun)
	}

	linksExist, err := txTableExists(tx, "session_tag_links")
	if err != nil {
		return err
	}
	var links []linkRow
	if linksExist {
		links, err = readSessionTagLinks(tx)
		if err != nil {
			return err
		}
		if _, dropErr := tx.ExecContext(context.Background(), "DROP TABLE session_tag_links"); dropErr != nil {
			return fmt.Errorf("drop session_tag_links: %w", dropErr)
		}
	}

	// Read the tags, resolve each to its project id, and pick one survivor per
	// (name, project_id).
	tags, err := readSessionTags(tx)
	if err != nil {
		return err
	}

	type tagKey struct {
		name      string
		projectID int64
	}
	survivor := map[tagKey]int64{} // (name, project) → surviving tag id
	remap := map[int64]int64{}     // dropped tag id → surviving tag id
	var keep []tagRow
	for _, t := range tags {
		key := tagKey{name: t.name, projectID: t.projectID}
		if existing, ok := survivor[key]; ok {
			remap[t.id] = existing
			continue
		}
		survivor[key] = t.id
		keep = append(keep, t)
	}

	if _, err := tx.ExecContext(context.Background(), `
		CREATE TABLE session_tags_projects_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			scope TEXT NOT NULL DEFAULT 'project',
			project_id INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(name, project_id)
		)`); err != nil {
		return fmt.Errorf("create session_tags_projects_new: %w", err)
	}
	for _, t := range keep {
		if _, err := tx.ExecContext(context.Background(),
			"INSERT INTO session_tags_projects_new (id, name, scope, project_id, created_at) VALUES (?, ?, ?, ?, ?)",
			t.id, t.name, t.scope, t.projectID, t.createdAt,
		); err != nil {
			return fmt.Errorf("copy session tag %d: %w", t.id, err)
		}
	}
	if _, err := tx.ExecContext(context.Background(), "DROP TABLE session_tags"); err != nil {
		return fmt.Errorf("drop session_tags: %w", err)
	}
	if _, err := tx.ExecContext(context.Background(), "ALTER TABLE session_tags_projects_new RENAME TO session_tags"); err != nil {
		return fmt.Errorf("rename session_tags_projects_new: %w", err)
	}

	// Recreate the links table with its foreign key pointing at the rebuilt
	// session_tags, then restore the assignments with duplicate tag ids remapped.
	if _, err := tx.ExecContext(context.Background(), `
		CREATE TABLE session_tag_links (
			session_id TEXT NOT NULL,
			tag_id INTEGER NOT NULL REFERENCES session_tags(id) ON DELETE CASCADE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(session_id, tag_id)
		)`); err != nil {
		return fmt.Errorf("recreate session_tag_links: %w", err)
	}
	for _, l := range links {
		tagID := l.tagID
		if mapped, ok := remap[tagID]; ok {
			tagID = mapped
		}
		// OR IGNORE: a session that carried both the surviving and the dropped
		// duplicate now has one link, not a UNIQUE violation.
		if _, err := tx.ExecContext(context.Background(),
			"INSERT OR IGNORE INTO session_tag_links (session_id, tag_id, created_at) VALUES (?, ?, ?)",
			l.sessionID, tagID, l.createdAt,
		); err != nil {
			return fmt.Errorf("restore session tag link: %w", err)
		}
	}
	if len(remap) > 0 {
		slog.Info("merged duplicate session tags onto merged projects", slog.Int("dropped", len(remap)))
	}
	return nil
}

// backfillFileShareProject assigns each existing file share to the project it
// was created from, so the management drawer can be project-scoped.
//
// The row only records the shared file's absolute path and its confinement
// root, never a project, so the owner is inferred: the longest project path
// that contains the file. A share of a file outside every project cannot be
// attributed that way, and the user's choice is to park those on the current
// default project — the link stays revocable from the drawer instead of
// becoming invisible in every project.
func backfillFileShareProject(tx *sql.Tx) error {
	ok, err := txTableExists(tx, "file_shares")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	hasCol, err := txColumnExists(tx, "file_shares", "project_id")
	if err != nil {
		return err
	}
	if !hasCol {
		if _, addColErr := tx.ExecContext(context.Background(), "ALTER TABLE file_shares ADD COLUMN project_id INTEGER NOT NULL DEFAULT 0"); addColErr != nil {
			return fmt.Errorf("add project_id to file_shares: %w", addColErr)
		}
	}

	// Every known project, for the longest-prefix match below.
	projects, err := readAllProjectPaths(tx)
	if err != nil {
		return err
	}

	// Fallback project: the default one, else the most recently accessed.
	// recent_projects was rebuilt earlier in this transaction, so this reads
	// project_id directly rather than going through GetDefaultProject (which
	// still speaks in paths and would consult the not-yet-migrated table).
	var fallbackID int64
	_ = tx.QueryRowContext(context.Background(), `
		SELECT r.project_id FROM recent_projects r
		 ORDER BY r.is_default DESC, r.accessed_at DESC, r.id DESC
		 LIMIT 1`).Scan(&fallbackID)

	shares, err := readUnattributedShares(tx)
	if err != nil {
		return err
	}

	for _, s := range shares {
		projectID := longestProjectPrefix(projects, s.path)
		if projectID == 0 {
			projectID = fallbackID
		}
		if projectID == 0 {
			continue // nothing to attribute it to; leave the 0 sentinel
		}
		if _, err := tx.ExecContext(context.Background(), "UPDATE file_shares SET project_id = ? WHERE token = ?", projectID, s.token); err != nil {
			return fmt.Errorf("backfill file share project: %w", err)
		}
	}
	return nil
}

// readAllProjectPaths reads the id/path of every registered project, for the
// share backfill's longest-prefix match.
func readAllProjectPaths(tx *sql.Tx) ([]projectPathEntry, error) {
	projRows, err := tx.QueryContext(context.Background(), "SELECT id, path FROM projects")
	if err != nil {
		return nil, fmt.Errorf("read projects for share backfill: %w", err)
	}
	defer func() { _ = projRows.Close() }()
	var projects []projectPathEntry
	for projRows.Next() {
		var p projectPathEntry
		if err := projRows.Scan(&p.id, &p.path); err != nil {
			return nil, fmt.Errorf("scan project for share backfill: %w", err)
		}
		projects = append(projects, p)
	}
	if err := projRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projects for share backfill: %w", err)
	}
	return projects, nil
}

// readUnattributedShares reads the shares still parked on the 0 sentinel, i.e.
// those the backfill has to attribute.
func readUnattributedShares(tx *sql.Tx) ([]shareEntry, error) {
	shareRows, err := tx.QueryContext(context.Background(), "SELECT token, path FROM file_shares WHERE project_id = 0")
	if err != nil {
		return nil, fmt.Errorf("read file_shares for backfill: %w", err)
	}
	defer func() { _ = shareRows.Close() }()
	var shares []shareEntry
	for shareRows.Next() {
		var s shareEntry
		if err := shareRows.Scan(&s.token, &s.path); err != nil {
			return nil, fmt.Errorf("scan file share for backfill: %w", err)
		}
		shares = append(shares, s)
	}
	if err := shareRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate file_shares for backfill: %w", err)
	}
	return shares, nil
}

// shareEntry is one file_shares row as the backfill reads it.
type shareEntry struct {
	token string
	path  string
}

// projectPathEntry is one projects row as the share backfill needs it.
type projectPathEntry struct {
	id   int64
	path string
}

// longestProjectPrefix returns the id of the project whose path is the longest
// ancestor of filePath, or 0 when no project contains it.
//
// The separator boundary matters: /home/u/proj must not match a file under
// /home/u/proj-other.
func longestProjectPrefix(projects []projectPathEntry, filePath string) int64 {
	var bestID int64
	var bestLen int
	for _, p := range projects {
		if p.path == "" {
			continue
		}
		if filePath != p.path && !strings.HasPrefix(filePath, p.path+string(filepath.Separator)) {
			continue
		}
		if len(p.path) > bestLen {
			bestLen = len(p.path)
			bestID = p.id
		}
	}
	return bestID
}

// ─── small tx-scoped schema helpers ──────────────────────────────────────────

func txTableExists(tx *sql.Tx, table string) (bool, error) {
	var n int
	if err := tx.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table,
	).Scan(&n); err != nil {
		return false, fmt.Errorf("check table %s: %w", table, err)
	}
	return n > 0, nil
}

func txColumnExists(tx *sql.Tx, table, column string) (bool, error) {
	var n int
	if err := tx.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?", table, column,
	).Scan(&n); err != nil {
		return false, fmt.Errorf("check column %s.%s: %w", table, column, err)
	}
	return n > 0, nil
}

func txColumnNames(tx *sql.Tx, table string) ([]string, error) {
	rows, err := tx.QueryContext(context.Background(), "SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return nil, fmt.Errorf("read columns of %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, fmt.Errorf("scan column of %s: %w", table, err)
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

func txCreateTableSQL(tx *sql.Tx, table string) (string, error) {
	var ddl string
	if err := tx.QueryRowContext(context.Background(),
		"SELECT COALESCE(sql, '') FROM sqlite_master WHERE type='table' AND name=?", table,
	).Scan(&ddl); err != nil {
		return "", fmt.Errorf("read DDL of %s: %w", table, err)
	}
	if ddl == "" {
		return "", fmt.Errorf("no DDL found for table %s", table)
	}
	return ddl, nil
}
