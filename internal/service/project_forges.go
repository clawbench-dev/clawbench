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
	Owner       string `json:"owner"`
	Repo        string `json:"repo"`
	// Source is "auto" (derived from the git remote) or "manual" (set by the
	// user to override the remote).
	Source    string `json:"source"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// Slug returns the canonical owner/repo identifier.
func (p ProjectForge) Slug() string { return p.Owner + "/" + p.Repo }

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
		`SELECT id, project_path, platform, host, owner, repo, source, created_at, updated_at
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
	err := row.Scan(&pf.ID, &pf.ProjectPath, &pf.Platform, &pf.Host, &pf.Owner, &pf.Repo, &pf.Source, &pf.CreatedAt, &pf.UpdatedAt)
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
		`INSERT INTO project_forges (project_path, platform, host, owner, repo, source)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(project_path) DO UPDATE SET
			platform = excluded.platform,
			host = excluded.host,
			owner = excluded.owner,
			repo = excluded.repo,
			source = excluded.source,
			updated_at = CURRENT_TIMESTAMP`,
		projectPath, pf.Platform, pf.Host, pf.Owner, pf.Repo, source,
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

// ListProjectForges returns all bindings, newest first.
func ListProjectForges() ([]ProjectForge, error) {
	if dbRead == nil {
		return nil, nil
	}
	rows, err := dbRead.Query(
		`SELECT id, project_path, platform, host, owner, repo, source, created_at, updated_at
		 FROM project_forges ORDER BY updated_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []ProjectForge
	for rows.Next() {
		var pf ProjectForge
		if err := rows.Scan(&pf.ID, &pf.ProjectPath, &pf.Platform, &pf.Host, &pf.Owner, &pf.Repo, &pf.Source, &pf.CreatedAt, &pf.UpdatedAt); err != nil {
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
		Owner:       remote.Owner,
		Repo:        remote.Repo,
		Source:      source,
	}
}
