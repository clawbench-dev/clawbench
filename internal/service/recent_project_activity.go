package service

import (
	"context"
	"time"

	"clawbench/internal/store"
)

// RecentProjectActivity is one project that saw session activity inside the
// recency window but has NO active session right now (nothing running, nothing
// awaiting approval, nothing unread).
//
// It exists so the session list can show a collapsed card for a project the
// user was working in this morning even after every reply has been read: the
// card carries the project name and when it was last touched, and expands into
// the project's full session list on demand.
type RecentProjectActivity struct {
	// Path is the project's canonical directory (identity key, same as the
	// overview endpoint's group name).
	Path string `json:"path"`
	// LastActiveAt is the most recent session activity in that project,
	// "2006-01-02 15:04:05" UTC. Drives the card's relative-time label.
	LastActiveAt string `json:"lastActiveAt"`
}

// RecentProjectActivityWindow is how far back a project counts as "recently
// active". Deliberately a constant rather than a setting: the card exists to
// keep yesterday's work within reach, and a configurable window would add a
// knob without changing what the list is for.
const RecentProjectActivityWindow = 24 * time.Hour

// ListRecentlyActiveProjects returns projects whose latest session activity
// falls inside RecentProjectActivityWindow, most recent first.
//
// `exclude` is the caller's current project: its sessions are already the main
// list, so a card for it would duplicate the rows right above.
//
// A project is judged by MAX(updated_at) over its non-archived interactive
// sessions — the same timestamp the session rows themselves display, so the
// card's "2 小时前" agrees with the newest row inside it.
func ListRecentlyActiveProjects(exclude string) ([]RecentProjectActivity, error) {
	cutoff := time.Now().UTC().Add(-RecentProjectActivityWindow).Format("2006-01-02 15:04:05")
	rows, err := store.ReadDB().QueryContext(context.Background(), `
		SELECT COALESCE(p.path, '') AS path, MAX(s.updated_at) AS last_at
		  FROM chat_sessions s
		  JOIN projects p ON p.id = s.project_id
		 WHERE s.archived = 0
		   AND s.project_id != 0
		   AND s.session_type IN (`+store.VisibleSessionTypeInClause+`)
		   AND s.updated_at >= ?
		 GROUP BY p.path
		 ORDER BY last_at DESC, p.path ASC`, cutoff)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []RecentProjectActivity{}
	for rows.Next() {
		var item RecentProjectActivity
		var lastAt string
		if err := rows.Scan(&item.Path, &lastAt); err != nil {
			return nil, err
		}
		if item.Path == "" || item.Path == exclude {
			continue
		}
		item.LastActiveAt = lastAt
		out = append(out, item)
	}
	return out, rows.Err()
}
