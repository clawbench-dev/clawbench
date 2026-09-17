package service

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"clawbench/internal/model"
)

// RecentProjectItem is one path inside a recent-projects group.
type RecentProjectItem struct {
	Path string   `json:"path"`
	Kind RepoKind `json:"kind"`
}

// RecentProjectGroup collects recent projects that belong to one git
// repository. Paths of the same repository — the main worktree, its linked
// worktrees, and subdirectories opened as their own project — share one group,
// keyed by the git common dir.
//
// A path that is not inside a repository forms its own single-item group with
// an empty RepoRoot, so the response shape is uniform and the frontend decides
// what to render: it flattens groups whose Items has length 1.
type RecentProjectGroup struct {
	// RepoRoot is the main worktree root of the repository, used as the group
	// label and as a stable render key. Empty for a non-repository group.
	RepoRoot string `json:"repoRoot"`
	// GroupName is the display name for the group header (the base name of
	// RepoRoot). Empty for a non-repository group.
	GroupName string `json:"groupName"`
	// Items are the group's paths, most recently accessed first.
	Items []RecentProjectItem `json:"items"`
}

// recentProjectRow is a recent_projects row after stale filtering, in
// most-recently-accessed-first order.
type recentProjectRow struct {
	path string
}

// loadRecentProjectRows reads every recent_projects row and drops entries whose
// directory no longer exists (deleting those rows, as the flat listing always
// did). Rows come back ordered by accessed_at DESC, which the grouping relies
// on for ordering both groups and their items.
//
// There is deliberately no SQL LIMIT here: the display window is applied after
// grouping so that sibling worktrees of a displayed project can be pulled in
// without consuming a slot of their own.
func loadRecentProjectRows(ctx context.Context) ([]recentProjectRow, error) {
	rows, err := dbRead.QueryContext(ctx, "SELECT project_path FROM recent_projects ORDER BY accessed_at DESC, id DESC")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var all []recentProjectRow
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		all = append(all, recentProjectRow{path: p})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var valid, stale []recentProjectRow
	for _, r := range all {
		info, statErr := os.Stat(r.path)
		if statErr == nil && info.IsDir() {
			valid = append(valid, r)
		} else {
			stale = append(stale, r)
		}
	}

	for _, r := range stale {
		if delErr := RemoveRecentProject(r.path); delErr != nil {
			slog.Warn("failed to remove stale recent project", slog.String("path", r.path), slog.String("err", delErr.Error()))
		} else {
			slog.Info("removed stale recent project", slog.String("path", r.path))
		}
	}

	return valid, nil
}

// GetRecentProjectGroups returns the recent projects grouped by git repository.
//
// The display window is `recent_projects.max_count` rows, taken in
// most-recently-accessed-first order. Same-repository siblings of a displayed
// project are then pulled in even when they fall outside that window: they are
// part of the same project, so they do not consume a slot of their own. This is
// what keeps a group from being split by the window edge when one worktree was
// last opened long before its siblings.
//
// Group order follows the most recent member (equivalently, the order in which
// each group first appears in the time-ordered rows), and items inside a group
// stay in time order. The main worktree is not pinned to the top.
func GetRecentProjectGroups() ([]RecentProjectGroup, error) {
	valid, err := loadRecentProjectRows(context.Background())
	if err != nil {
		return nil, err
	}
	if len(valid) == 0 {
		return []RecentProjectGroup{}, nil
	}

	// Resolve the repository layout once per path; it is both the grouping key
	// and the source of the per-item kind.
	layoutByPath := make(map[string]RepoLayout, len(valid))
	for _, r := range valid {
		layoutByPath[r.path] = DetectRepoLayout(r.path)
	}

	return groupRecentProjectRows(valid, layoutByPath), nil
}

// groupRecentProjectRows applies the display window and folds the rows into
// groups, preserving time order. `rows` must be sorted most-recent-first and
// `layoutByPath` must cover every row.
func groupRecentProjectRows(rows []recentProjectRow, layoutByPath map[string]RepoLayout) []RecentProjectGroup {
	limit := model.RecentProjectsMaxCount
	if limit <= 0 {
		limit = 10
	}

	commonOf := func(p string) string { return layoutByPath[p].CommonDir }

	// The display window is the first `limit` rows. Everything else is only
	// eligible as a same-repository sibling of a windowed row.
	windowed := rows
	if len(windowed) > limit {
		windowed = windowed[:limit]
	}

	inWindow := make(map[string]bool, len(windowed))
	// Repositories represented in the window — siblings of these join the list.
	repoInWindow := make(map[string]bool, len(windowed))
	for _, r := range windowed {
		inWindow[r.path] = true
		if c := commonOf(r.path); c != "" {
			repoInWindow[c] = true
		}
	}

	var displayed []recentProjectRow
	for _, r := range rows {
		if inWindow[r.path] {
			displayed = append(displayed, r)
			continue
		}
		// Outside the window: keep it only as a sibling of a displayed project.
		if c := commonOf(r.path); c != "" && repoInWindow[c] {
			displayed = append(displayed, r)
		}
	}

	// Group preserving first-appearance order, which is time order because
	// `displayed` is still sorted by accessed_at DESC.
	groups := make([]RecentProjectGroup, 0, len(displayed))
	indexByKey := make(map[string]int, len(displayed))
	for _, r := range displayed {
		layout := layoutByPath[r.path]
		key := layout.CommonDir
		if key == "" {
			key = "\x00plain\x00" + r.path // each non-repo path is its own group
		}

		idx, ok := indexByKey[key]
		if !ok {
			group := RecentProjectGroup{}
			if layout.IsRepo() {
				group.RepoRoot = mainWorktreeRoot(layout.CommonDir)
				group.GroupName = filepath.Base(group.RepoRoot)
			}
			groups = append(groups, group)
			idx = len(groups) - 1
			indexByKey[key] = idx
		}
		groups[idx].Items = append(groups[idx].Items, RecentProjectItem{
			Path: r.path,
			Kind: layout.Kind,
		})
	}

	return groups
}

// mainWorktreeRoot derives the main worktree's root from a repository's common
// git dir. For an ordinary repository the common dir is `<root>/.git`, so the
// parent is the root. A repository using a separate git dir (`--separate-git-dir`,
// bare) does not follow that shape; the common dir itself is then the best
// label available, and the group header only shows its base name.
func mainWorktreeRoot(commonDir string) string {
	if filepath.Base(commonDir) == ".git" {
		return filepath.Dir(commonDir)
	}
	return commonDir
}
