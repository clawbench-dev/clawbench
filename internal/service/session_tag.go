//nolint:noctx // db global singleton, context not applicable (matches database.go/chat.go convention)
package service

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Session tag scopes. A "project" tag is only visible/selectable inside the
// project it was created in; a "global" tag is visible in every project.
const (
	SessionTagScopeProject = "project"
	SessionTagScopeGlobal  = "global"
)

// SessionTag is a tag registry entry (a label definition), not a link.
type SessionTag struct {
	Name  string `json:"name"`
	Scope string `json:"scope"`
	// ProjectPath is only meaningful for scope=project; empty for global tags.
	ProjectPath string `json:"projectPath,omitempty"`
	// Count is the number of sessions currently carrying this tag. Populated by
	// ListSessionTags only (it costs a GROUP BY); zero elsewhere.
	Count int `json:"count,omitempty"`
}

// SessionTagRef is a tag attached to a session. Scope is only consulted when
// the tag does not exist yet — an existing tag keeps its original scope, so
// re-tagging never silently widens a project label to global.
type SessionTagRef struct {
	Name  string `json:"name"`
	Scope string `json:"scope,omitempty"`
}

// NormalizeSessionTagName trims and collapses internal whitespace so that
// "  foo  " and "foo" are the same tag. Returns "" for a blank name, which
// callers treat as "ignore this entry".
func NormalizeSessionTagName(name string) string {
	return strings.Join(strings.Fields(name), " ")
}

// normalizeSessionTagScope maps arbitrary input onto a known scope, defaulting
// to project (the conservative choice: a tag created by accident should not
// leak into every project).
func normalizeSessionTagScope(scope string) string {
	if strings.EqualFold(strings.TrimSpace(scope), SessionTagScopeGlobal) {
		return SessionTagScopeGlobal
	}
	return SessionTagScopeProject
}

// ListSessionTags returns the tag candidates selectable in projectPath:
// every global tag plus the project tags belonging to projectPath.
//
// Uniqueness is (name, project_path), so a project tag named "bug" and a
// global tag named "bug" are two distinct rows. Only the global one is visible
// outside its own project, and inside that project the global definition wins
// (de-duplicated by name below) so a session never shows the same label twice.
func ListSessionTags(projectPath string) ([]SessionTag, error) {
	rows, err := dbRead.Query(`
		SELECT t.name, t.scope, t.project_path,
		       (SELECT COUNT(*) FROM session_tag_links l WHERE l.tag_id = t.id) AS cnt
		FROM session_tags t
		WHERE t.scope = 'global' OR t.project_path = ?
		ORDER BY t.name COLLATE NOCASE, t.scope ASC`, projectPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	tags := []SessionTag{}
	seen := map[string]bool{}
	for rows.Next() {
		var t SessionTag
		if err := rows.Scan(&t.Name, &t.Scope, &t.ProjectPath, &t.Count); err != nil {
			return nil, err
		}
		// ORDER BY scope ASC puts 'global' before 'project' (g < p), so the
		// first row seen for a name is the global definition.
		if seen[t.Name] {
			continue
		}
		seen[t.Name] = true
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// GetSessionTags returns the tags attached to one session, name-ordered.
func GetSessionTags(sessionID string) ([]SessionTag, error) {
	m, err := GetTagsForSessions([]string{sessionID})
	if err != nil {
		return nil, err
	}
	tags := m[sessionID]
	if tags == nil {
		tags = []SessionTag{}
	}
	return tags, nil
}

// GetTagsForSessions batch-loads tags for a set of session ids in a single
// query, returning a map keyed by session id. Sessions without tags are absent
// from the map (callers must treat a missing key as "no tags").
//
// This exists so the session list endpoint can hydrate tags for a whole page
// without an N+1 query and without joining into the main session query (which
// would multiply rows per tag and break the keyset pagination).
func GetTagsForSessions(sessionIDs []string) (map[string][]SessionTag, error) {
	out := map[string][]SessionTag{}
	if len(sessionIDs) == 0 {
		return out, nil
	}

	// Build the IN (...) placeholder list. SQLite has a variable limit
	// (default 999); a page is at most a few hundred sessions, and this is a
	// second query, so a plain expansion is fine. De-duplicate first so a
	// caller passing the same id twice cannot double-count.
	seen := make(map[string]bool, len(sessionIDs))
	ids := make([]string, 0, len(sessionIDs))
	for _, id := range sessionIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return out, nil
	}

	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT l.session_id, t.name, t.scope, t.project_path
		FROM session_tag_links l
		JOIN session_tags t ON t.id = l.tag_id
		WHERE l.session_id IN (%s)
		ORDER BY t.name COLLATE NOCASE`, placeholders)
	rows, err := dbRead.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var sessionID string
		var t SessionTag
		if err := rows.Scan(&sessionID, &t.Name, &t.Scope, &t.ProjectPath); err != nil {
			return nil, err
		}
		out[sessionID] = append(out[sessionID], t)
	}
	return out, rows.Err()
}

// SetSessionTags replaces the full tag set of one session with refs.
//
// Semantics: the payload is authoritative for this session. Tags present in
// refs are linked, tags absent are unlinked. Tags that do not exist yet are
// created with the scope from the ref (defaulting to project). Unlinking never
// deletes the registry entry — a tag disappears from the candidate list only
// when explicitly deleted via DeleteSessionTag, so removing it from the last
// session does not lose the label definition.
//
// Runs in a single write transaction: a partially-applied tag set would leave
// the UI showing tags the user removed.
func SetSessionTags(sessionID, projectPath string, refs []SessionTagRef) error {
	// Normalize + de-duplicate by name, preserving first-seen order so the
	// caller's ordering is stable for the returned list.
	names := make([]string, 0, len(refs))
	scopeByName := map[string]string{}
	for _, ref := range refs {
		name := NormalizeSessionTagName(ref.Name)
		if name == "" {
			continue
		}
		if _, dup := scopeByName[name]; dup {
			continue
		}
		scopeByName[name] = normalizeSessionTagScope(ref.Scope)
		names = append(names, name)
	}

	tx, err := WriteBegin()
	if err != nil {
		return err
	}
	defer WriteUnlock()
	defer func() { _ = tx.Rollback() }()

	// Clear the existing links for this session first; re-inserting below is
	// simpler and safer than diffing, and the link table is tiny.
	if _, err := tx.Exec(`DELETE FROM session_tag_links WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("failed to clear session tags: %w", err)
	}

	for _, name := range names {
		// Resolve to an existing definition when one is visible in this
		// project (global first, then this project's own), otherwise create a
		// new row. The lookup order mirrors ListSessionTags so a session never
		// links to a shadowed definition.
		tagID, err := resolveOrCreateSessionTag(tx, name, scopeByName[name], projectPath)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`
			INSERT INTO session_tag_links (session_id, tag_id) VALUES (?, ?)
			ON CONFLICT(session_id, tag_id) DO NOTHING`, sessionID, tagID); err != nil {
			return fmt.Errorf("failed to link session tag %q: %w", name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit session tags: %w", err)
	}
	return nil
}

// resolveOrCreateSessionTag returns the id of the tag definition visible in
// projectPath, creating it if absent.
//
// An existing definition always wins and keeps its own scope/project_path —
// that is what makes "re-tagging an existing label" non-destructive. The
// caller's requested scope only applies to a brand-new definition.
func resolveOrCreateSessionTag(tx *sql.Tx, name, scope, projectPath string) (int64, error) {
	// Prefer the global definition, then this project's own.
	tagID, err := findSessionTagID(tx, name, projectPath)
	if err == nil {
		return tagID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("failed to look up session tag %q: %w", name, err)
	}

	owner := projectPathForScope(scope, projectPath)
	res, err := tx.Exec(`INSERT INTO session_tags (name, scope, project_path) VALUES (?, ?, ?)`,
		name, scope, owner)
	if err != nil {
		return 0, fmt.Errorf("failed to create session tag %q: %w", name, err)
	}
	return res.LastInsertId()
}

// findSessionTagID looks up the definition a session in projectPath would see
// for `name`: the global one first, then the project's own. Returns
// sql.ErrNoRows when neither exists.
func findSessionTagID(tx *sql.Tx, name, projectPath string) (int64, error) {
	var tagID int64
	err := tx.QueryRow(`SELECT id FROM session_tags WHERE name = ? AND scope = 'global'`, name).Scan(&tagID)
	if err == nil {
		return tagID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	err = tx.QueryRow(`SELECT id FROM session_tags WHERE name = ? AND project_path = ?`,
		name, projectPath).Scan(&tagID)
	if err != nil {
		return 0, err
	}
	return tagID, nil
}

// projectPathForScope stores the owning project only for project-scoped tags;
// global tags carry an empty path so they are never matched by the
// project_path filter in ListSessionTags.
func projectPathForScope(scope, projectPath string) string {
	if scope == SessionTagScopeGlobal {
		return ""
	}
	return projectPath
}

// DeleteSessionTag removes a tag definition and every link to it, i.e. the tag
// disappears from all sessions that used it.
//
// Deletion targets the definition the caller can actually see in projectPath:
// the global tag if one exists, otherwise this project's own tag. A project
// therefore cannot delete another project's label (it is not visible to it).
func DeleteSessionTag(name, projectPath string) error {
	normalized := NormalizeSessionTagName(name)
	if normalized == "" {
		return fmt.Errorf("tag name is required")
	}

	tx, err := WriteBegin()
	if err != nil {
		return err
	}
	defer WriteUnlock()
	defer func() { _ = tx.Rollback() }()

	tagID, err := findSessionTagID(tx, normalized, projectPath)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("session tag %q not found", normalized)
	}
	if err != nil {
		return fmt.Errorf("failed to find session tag %q: %w", normalized, err)
	}

	if _, err := tx.Exec(`DELETE FROM session_tag_links WHERE tag_id = ?`, tagID); err != nil {
		return fmt.Errorf("failed to unlink session tag %q: %w", normalized, err)
	}
	if _, err := tx.Exec(`DELETE FROM session_tags WHERE id = ?`, tagID); err != nil {
		return fmt.Errorf("failed to delete session tag %q: %w", normalized, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit session tag deletion: %w", err)
	}
	return nil
}

// DeleteSessionTagsForSession removes every link belonging to a session. Called
// when a session is destroyed so its tags do not linger in the link table.
func DeleteSessionTagsForSession(sessionID string) error {
	_, err := WriteExec(`DELETE FROM session_tag_links WHERE session_id = ?`, sessionID)
	return err
}
