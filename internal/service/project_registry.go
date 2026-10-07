package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	"clawbench/internal/store"
)

// ProjectListItem is one row of the project registry listing: a project
// directory ever registered, with the statistics that are cheap to derive from
// the database alone.
//
// This deliberately carries no filesystem-derived field except Exists (a single
// os.Stat per row). Anything that requires walking the directory (size, file
// count) or reading git metadata (repo kind) belongs to ProjectDetail, so the
// listing stays fast even with many registered projects.
type ProjectListItem struct {
	// ID is the projects-table primary key — the stable identity used by the
	// detail route (a path can be renamed, an id cannot).
	ID int64 `json:"id"`
	// Path is the canonical project directory. Returned even when the directory
	// no longer exists on disk.
	Path string `json:"path"`
	// SessionCount is the number of chat_sessions rows currently recorded for
	// this project. Sessions hard-deleted are not counted, so a project whose
	// history was cleared reads 0 rather than a stale "was ever used" number.
	SessionCount int `json:"session_count"`
	// LastActiveAt is the most recent session creation time, "2006-01-02
	// 15:04:05" UTC. Empty when the project has no sessions.
	LastActiveAt string `json:"last_active_at"`
	// CreatedAt is when the project was first registered, "2006-01-02 15:04:05"
	// UTC.
	CreatedAt string `json:"created_at"`
	// Exists reports whether the directory is still present on disk.
	Exists bool `json:"exists"`
}

// ProjectDetail is the registry listing row plus the fields that require
// touching the filesystem beyond a single stat.
type ProjectDetail struct {
	ProjectListItem
	// RepoKind classifies the path inside git (main/worktree/subdir/plain).
	// "unknown" when the directory no longer exists and the layout cannot be
	// determined.
	RepoKind string `json:"repo_kind"`
}

// RepoKindUnknown marks a project whose directory is gone, so its git layout
// cannot be read. Kept distinct from "plain" (a real, non-repository directory)
// so the UI does not claim a deleted project is simply outside git.
const RepoKindUnknown = "unknown"

// ListAllProjects returns every registered project with DB-derived statistics,
// most recently active first.
//
// Ordering falls back to the project's registration time for projects with no
// sessions, so a never-used project still takes a stable position instead of
// being dumped at the end.
func ListAllProjects() ([]ProjectListItem, error) {
	rows, err := store.ReadDB().QueryContext(context.Background(), `
		SELECT p.id, p.path, p.created_at,
		       COALESCE(s.cnt, 0) AS session_count,
		       COALESCE(s.last_at, '') AS last_active_at
		  FROM projects p
		  LEFT JOIN (
		        SELECT project_id, COUNT(*) AS cnt, MAX(created_at) AS last_at
		          FROM chat_sessions
		         WHERE project_id != 0
		           AND session_type IN (`+store.VisibleSessionTypeInClause+`)
		         GROUP BY project_id
		  ) s ON s.project_id = p.id
		 WHERE p.path != ''
		 ORDER BY COALESCE(s.last_at, p.created_at) DESC, p.path ASC`)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]ProjectListItem, 0, 32)
	for rows.Next() {
		var (
			item      ProjectListItem
			createdAt sql.NullString
			lastAt    sql.NullString
		)
		if err := rows.Scan(&item.ID, &item.Path, &createdAt, &item.SessionCount, &lastAt); err != nil {
			return nil, fmt.Errorf("scan project row: %w", err)
		}
		if createdAt.Valid {
			item.CreatedAt = createdAt.String
		}
		if lastAt.Valid {
			item.LastActiveAt = lastAt.String
		}
		item.Exists = dirExists(item.Path)
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetProjectDetail returns one project's listing row plus its git repo kind.
//
// Returns (nil, nil) when no project has that id, so the caller can answer 404
// without treating an unknown id as an error.
func GetProjectDetail(id int64) (*ProjectDetail, error) {
	if id == store.GlobalScopeProjectID {
		return nil, nil
	}

	var (
		detail    ProjectDetail
		createdAt sql.NullString
		lastAt    sql.NullString
	)
	err := store.ReadDB().QueryRowContext(
		context.Background(), `
		SELECT p.id, p.path, p.created_at,
		       COALESCE(s.cnt, 0) AS session_count,
		       COALESCE(s.last_at, '') AS last_active_at
		  FROM projects p
		  LEFT JOIN (
		        SELECT project_id, COUNT(*) AS cnt, MAX(created_at) AS last_at
		          FROM chat_sessions
		         WHERE project_id != 0
		           AND session_type IN (`+store.VisibleSessionTypeInClause+`)
		         GROUP BY project_id
		  ) s ON s.project_id = p.id
		 WHERE p.id = ?`, id,
	).Scan(&detail.ID, &detail.Path, &createdAt, &detail.SessionCount, &lastAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get project detail: %w", err)
	}
	if createdAt.Valid {
		detail.CreatedAt = createdAt.String
	}
	if lastAt.Valid {
		detail.LastActiveAt = lastAt.String
	}

	detail.Exists = dirExists(detail.Path)
	if !detail.Exists {
		detail.RepoKind = RepoKindUnknown
	} else {
		detail.RepoKind = string(DetectRepoLayout(detail.Path).Kind)
	}
	return &detail, nil
}

// dirExists reports whether path is an existing directory.
func dirExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
