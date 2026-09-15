//nolint:noctx // db global singleton, context not applicable
package service

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"clawbench/internal/forge"
)

// ProjectForgesDDL creates the project_forges table and its indexes.
// Exported so handler tests and other external packages can create this table
// in their test databases, keeping one source of truth for the schema.
const ProjectForgesDDL = `
CREATE TABLE IF NOT EXISTS project_forges (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	project_path TEXT NOT NULL,
	platform     TEXT NOT NULL,
	host         TEXT NOT NULL,
	scheme       TEXT NOT NULL DEFAULT '',
	owner        TEXT NOT NULL,
	repo         TEXT NOT NULL,
	source       TEXT NOT NULL DEFAULT 'auto',
	created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_project_forges_path ON project_forges(project_path);
CREATE INDEX IF NOT EXISTS idx_project_forges_repo ON project_forges(platform, host, owner, repo);
`

// ProjectForge is a persisted binding between a ClawBench project and a remote
// repository.
type ProjectForge struct {
	ID          int64  `json:"id"`
	ProjectPath string `json:"projectPath"`
	Platform    string `json:"platform"`
	Host        string `json:"host"`
	// Scheme is the API scheme ("http" or "https") this binding's host is
	// reached with, or "" when the remote did not say (an ssh or scp remote).
	// It is NOT part of the binding's identity: Host remains the key for
	// credentials, snapshots, rate limits and read state, so the same instance
	// over http and https stays one repository. See forge.ResolveScheme for how
	// "" is resolved at request time.
	Scheme string `json:"scheme,omitempty"`
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	// Source is "auto" (derived from the git remote) or "manual" (set by the
	// user to override the remote).
	Source    string `json:"source"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// Slug returns the canonical owner/repo identifier.
func (p ProjectForge) Slug() string { return p.Owner + "/" + p.Repo }

// RepoKey returns the binding's repository identity.
//
// Read state is stored per REPOSITORY, not per project: one upstream item is one
// item, even when two projects happen to be bound to the same repo. Keeping the
// conversion here means every caller derives the key the same way.
func (p ProjectForge) RepoKey() ForgeRepoKey {
	return ForgeRepoKey{Platform: p.Platform, Host: p.Host, Owner: p.Owner, Repo: p.Repo}
}

// NormalizeProjectPath canonicalizes a project path so the same project always
// maps to one binding row. It resolves symlinks when possible, cleans the path,
// and strips a trailing separator. On any error it falls back to the cleaned
// path rather than failing the caller.
func NormalizeProjectPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	p = filepath.Clean(p)
	// filepath.Clean already removes trailing separators except for the root.
	return p
}

// GetProjectForge returns the binding for a project, or (nil, nil) when none
// exists.
func GetProjectForge(projectPath string) (*ProjectForge, error) {
	if dbRead == nil {
		return nil, nil
	}
	projectPath = NormalizeProjectPath(projectPath)
	if projectPath == "" {
		return nil, nil
	}
	row := dbRead.QueryRow(
		`SELECT id, project_path, platform, host, scheme, owner, repo, source, created_at, updated_at
		 FROM project_forges WHERE project_path = ?`,
		projectPath,
	)
	return scanProjectForge(row)
}

// scanProjectForge reads a single row, returning (nil, nil) on no rows.
func scanProjectForge(row interface {
	Scan(dest ...any) error
},
) (*ProjectForge, error) {
	var pf ProjectForge
	err := row.Scan(&pf.ID, &pf.ProjectPath, &pf.Platform, &pf.Host, &pf.Scheme, &pf.Owner, &pf.Repo, &pf.Source, &pf.CreatedAt, &pf.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &pf, nil
}

// UpsertProjectForge creates or replaces the binding for a project. The
// project_path is normalized first. platform/host/owner/repo must be non-empty.
func UpsertProjectForge(pf ProjectForge) error {
	if db == nil {
		return nil
	}
	projectPath := NormalizeProjectPath(pf.ProjectPath)
	if projectPath == "" {
		return fmt.Errorf("project_forges: project path is required")
	}
	if pf.Platform == "" || pf.Host == "" || pf.Owner == "" || pf.Repo == "" {
		return fmt.Errorf("project_forges: platform, host, owner and repo are required")
	}
	source := pf.Source
	if source == "" {
		source = "manual"
	}
	_, err := WriteExec(
		`INSERT INTO project_forges (project_path, platform, host, scheme, owner, repo, source)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(project_path) DO UPDATE SET
			platform = excluded.platform,
			host = excluded.host,
			scheme = excluded.scheme,
			owner = excluded.owner,
			repo = excluded.repo,
			source = excluded.source,
			updated_at = CURRENT_TIMESTAMP`,
		projectPath, pf.Platform, pf.Host, pf.Scheme, pf.Owner, pf.Repo, source,
	)
	if err != nil {
		slog.Warn("project_forges: upsert failed", "error", err, "project_path", projectPath)
	}
	return err
}

// DeleteProjectForge removes a project's binding.
func DeleteProjectForge(projectPath string) error {
	if db == nil {
		return nil
	}
	projectPath = NormalizeProjectPath(projectPath)
	if projectPath == "" {
		return nil
	}
	_, err := WriteExec("DELETE FROM project_forges WHERE project_path = ?", projectPath)
	return err
}

// AutoBindProjectForge binds a project to a remote derived from its git remote,
// unless the user has explicitly opted out (by unbinding) or a binding already
// exists.
//
// This is a single atomic statement on purpose. The obvious read-then-write
// (GetProjectForge → UpsertProjectForge) has two races: an in-flight GET that
// already read "unbound" could write back *after* the user's DELETE commits
// (resurrecting the binding), and two concurrent GETs — the app fetches the
// binding on mount and the panel fetches it again when opened — would both
// write. ON CONFLICT DO NOTHING also guarantees a user's manual binding is
// never overwritten by a later auto-bind.
//
// Returns true when this call created the binding.
func AutoBindProjectForge(projectPath string, remote forge.Remote) (bool, error) {
	if db == nil {
		return false, nil
	}
	projectPath = NormalizeProjectPath(projectPath)
	if projectPath == "" {
		return false, nil
	}
	if remote.Platform == "" || remote.Host == "" || remote.Owner == "" || remote.Repo == "" {
		return false, fmt.Errorf("project_forges: platform, host, owner and repo are required")
	}
	res, err := WriteExec(
		`INSERT INTO project_forges (project_path, platform, host, scheme, owner, repo, source)
		 SELECT ?, ?, ?, ?, ?, ?, 'auto'
		 WHERE NOT EXISTS (
			SELECT 1 FROM project_meta
			WHERE project_path = ? AND forge_bind_opt_out = 1
		 )
		 ON CONFLICT(project_path) DO NOTHING`,
		projectPath, string(remote.Platform), remote.Host, remote.Scheme, remote.Owner, remote.Repo, projectPath,
	)
	if err != nil {
		slog.Warn("project_forges: auto-bind failed", "error", err, "project_path", projectPath)
		return false, err
	}
	// The count is advisory (did this call create the row?). The statement itself
	// already succeeded, so a driver that cannot report the count is not an error.
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SetForgeBindOptOut records whether the user has explicitly unbound this
// project's forge repository.
//
// While set, AutoBindProjectForge leaves the project unbound. This is what stops
// "unbind" from being undone by the next GET. It is intentionally not scoped to
// a particular remote: the user said they do not want a binding, so changing the
// git remote afterwards does not re-enable auto-binding. An explicit POST
// binding clears it.
func SetForgeBindOptOut(projectPath string, optedOut bool) error {
	if db == nil {
		return nil
	}
	projectPath = NormalizeProjectPath(projectPath)
	if projectPath == "" {
		return nil
	}
	v := 0
	if optedOut {
		v = 1
	}
	_, err := WriteExec(
		`INSERT INTO project_meta (project_path, forge_bind_opt_out)
		 VALUES (?, ?)
		 ON CONFLICT(project_path) DO UPDATE SET
			forge_bind_opt_out = excluded.forge_bind_opt_out,
			updated_at = CURRENT_TIMESTAMP`,
		projectPath, v,
	)
	return err
}

// IsForgeBindOptedOut reports whether the user has explicitly unbound this
// project. A missing row means "not opted out".
func IsForgeBindOptedOut(projectPath string) (bool, error) {
	if dbRead == nil {
		return false, nil
	}
	projectPath = NormalizeProjectPath(projectPath)
	if projectPath == "" {
		return false, nil
	}
	var v int
	err := dbRead.QueryRow(
		`SELECT forge_bind_opt_out FROM project_meta WHERE project_path = ?`, projectPath,
	).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return v == 1, nil
}

// ListProjectForges returns all bindings, newest first.
func ListProjectForges() ([]ProjectForge, error) {
	if dbRead == nil {
		return nil, nil
	}
	rows, err := dbRead.Query(
		`SELECT id, project_path, platform, host, scheme, owner, repo, source, created_at, updated_at
		 FROM project_forges ORDER BY updated_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []ProjectForge
	for rows.Next() {
		var pf ProjectForge
		if err := rows.Scan(&pf.ID, &pf.ProjectPath, &pf.Platform, &pf.Host, &pf.Scheme, &pf.Owner, &pf.Repo, &pf.Source, &pf.CreatedAt, &pf.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, pf)
	}
	return out, rows.Err()
}

// UniqueForgeRepos returns the distinct (platform, host, owner, repo) tuples
// across all bindings. Polling is keyed by repo, not by project row, so the
// same repository bound to two projects is fetched once.
func UniqueForgeRepos() ([]ForgeRepoRef, error) {
	if dbRead == nil {
		return nil, nil
	}
	rows, err := dbRead.Query(
		`SELECT DISTINCT platform, host, owner, repo FROM project_forges`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []ForgeRepoRef
	for rows.Next() {
		var r ForgeRepoRef
		if err := rows.Scan(&r.Platform, &r.Host, &r.Owner, &r.Repo); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ForgeRepoRef identifies a repository independently of any project binding.
type ForgeRepoRef struct {
	Platform string
	Host     string
	Owner    string
	Repo     string
}

// Key returns a stable identity string for the repository.
func (r ForgeRepoRef) Key() string {
	return r.Platform + "|" + r.Host + "|" + r.Owner + "/" + r.Repo
}

// FromRemote builds a binding from a parsed remote URL.
func ProjectForgeFromRemote(projectPath string, remote forge.Remote, source string) ProjectForge {
	return ProjectForge{
		ProjectPath: NormalizeProjectPath(projectPath),
		Platform:    string(remote.Platform),
		Host:        remote.Host,
		Scheme:      remote.Scheme,
		Owner:       remote.Owner,
		Repo:        remote.Repo,
		Source:      source,
	}
}
