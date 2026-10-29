package service

import (
	"fmt"

	"clawbench/internal/store"
)

// AgentSessionRole says how the agent relates to one listed conversation. It is
// derived, never stored: the same group can be listed once per member INSTANCE,
// so the role is a property of the ENTRY (member row), not of the group.
const (
	AgentSessionRoleChat   = "chat"
	AgentSessionRoleHost   = "host"
	AgentSessionRoleMember = "member"
)

// AgentSessionItem is one conversation that uses an agent.
//
// One item per referencing ROW, not per conversation: an agent added to the
// same group twice (two independent instances) produces two items, because
// AgentUsageCounts.Sessions counts member rows and the list must have the same
// length as the number the settings page shows.
type AgentSessionItem struct {
	// EntryID identifies the row this item came from: the chat row id for a
	// 1:1 chat, or the group MEMBER row id for a membership. Every entry_id is
	// a chat_sessions primary key, so it is unique across the union and can
	// serve as the keyset tie-break.
	EntryID string `json:"entryId"`
	// SessionID is what to open: the chat itself, or the GROUP timeline row
	// for a membership entry (the member row is hidden and has no messages).
	SessionID   string `json:"sessionId"`
	Title       string `json:"title"`
	ProjectPath string `json:"projectPath"`
	SessionType string `json:"sessionType"`
	Role        string `json:"role"`
	// MemberName is the instance's display name inside the group (e.g. a
	// second instance of the same agent). Empty for 1:1 chats.
	MemberName string `json:"memberName"`
	Archived   bool   `json:"archived"`
	// UpdatedAt is the SQLite textual timestamp ("2006-01-02 15:04:05"). Kept as
	// a string, not time.Time: the keyset predicate compares TEXT, and COALESCE
	// (needed for a dangling group) makes the driver report the column as text
	// anyway. Formatting is the same either way, so the client sees one shape.
	UpdatedAt string `json:"updatedAt"`
}

// AgentSessionPage is one page of ListAgentSessions plus what the caller needs
// to fetch the next one.
type AgentSessionPage struct {
	Sessions []AgentSessionItem
	HasMore  bool
	// NextCursor / NextCursorID are the keyset of the last returned row, in the
	// same textual form SQLite stores updated_at ("2006-01-02 15:04:05"). The
	// server formats them so the client never has to re-create the layout.
	NextCursor   string
	NextCursorID string
	// Total is the agent's session count (the same number AgentUsageCounts
	// reports and the settings page displays), so the header and the list
	// cannot disagree.
	Total int
}

// agentSessionsUnion normalises the three ways an agent can be attached to a
// conversation into one row shape.
//
//	1:1 chat          → chat_sessions row with session_type='chat'
//	group member      → the HIDDEN group_member row (archived=0) joined to its
//	                    group timeline row
//	group host        → same membership row, but it is the group's host pointer
//
// The host is NOT matched through the group row's agent_id: in free mode that
// column holds the FIRST MEMBER's agent (it exists only to satisfy NOT NULL and
// drive the list icon), so matching it would report a plain member as host.
// The host pointer lives in the group's context_state (see writeGroupModeInline).
//
// Archived chats are included (the count includes them); archived MEMBER rows
// are excluded (the count excludes them). The group's own archived flag is not
// filtered for memberships, matching the count.
//
// The member→group join is a LEFT JOIN on purpose. GetAgentUsage counts member
// rows unconditionally, so an INNER JOIN would silently drop a member row whose
// group row is missing (a dangling group_id — possible after a partial delete)
// and the header would print a bigger number than the list has rows. Such a row
// degrades to an untitled entry with an empty project, which is still
// openable-by-id and still counted, i.e. the two numbers stay equal.
const agentSessionsUnion = `
	SELECT s.id AS entry_id, s.id AS session_id, s.title AS title,
	       COALESCE(p.path, '') AS project_path, s.session_type AS session_type,
	       'chat' AS role, '' AS member_name, s.archived AS archived,
	       s.updated_at AS updated_at
	FROM chat_sessions s
	LEFT JOIN projects p ON p.id = s.project_id
	WHERE s.session_type = ? AND s.agent_id = ?

	UNION ALL

	SELECT m.id, COALESCE(g.id, ''), COALESCE(g.title, ''), COALESCE(p.path, ''),
	       COALESCE(g.session_type, ''),
	       CASE WHEN m.id = COALESCE(
	                 CASE WHEN json_valid(g.context_state)
	                      THEN json_extract(g.context_state, '$.host_member_id') END, '')
	            THEN 'host' ELSE 'member' END,
	       COALESCE(m.agent_name, ''), COALESCE(g.archived, 0),
	       COALESCE(g.updated_at, m.updated_at)
	FROM chat_sessions m
	LEFT JOIN chat_sessions g ON g.id = m.group_id
	LEFT JOIN projects p ON p.id = g.project_id
	WHERE m.session_type = ? AND m.agent_id = ? AND m.archived = 0
`

// ListAgentSessions returns one page of the conversations that use agentID,
// newest activity first, across ALL projects.
//
// Cross-project on purpose: the count it must agree with (GetAgentUsage) has no
// project predicate, so narrowing the list to the caller's project would make
// the header and the rows disagree.
//
// Keyset pagination on (updated_at, entry_id): pass the previous page's
// NextCursor/NextCursorID. limit<=0 means "no limit" (the whole list).
func ListAgentSessions(agentID string, limit int, cursor, cursorID string) (AgentSessionPage, error) {
	var page AgentSessionPage

	args := []any{store.SessionTypeChat, agentID, store.SessionTypeGroupMember, agentID}

	// Total comes from the SAME union the rows do, so the header number and the
	// rows cannot drift. It is pinned to GetAgentUsage's Sessions by test —
	// that number is what the settings page prints, so the two definitions have
	// to stay equal.
	if err := store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM ("+agentSessionsUnion+") x", args...,
	).Scan(&page.Total); err != nil {
		return page, fmt.Errorf("count sessions for agent %s: %w", agentID, err)
	}

	query := "SELECT entry_id, session_id, title, project_path, session_type, role, member_name, archived, updated_at FROM (" +
		agentSessionsUnion + ") x"

	// Tie-break on entry_id so entries sharing a timestamp (every membership of
	// one group shares the group's updated_at) are neither skipped nor repeated
	// across pages.
	if cursor != "" && cursorID != "" {
		query += " WHERE (x.updated_at < ? OR (x.updated_at = ? AND x.entry_id < ?))"
		args = append(args, cursor, cursor, cursorID)
	}
	query += " ORDER BY x.updated_at DESC, x.entry_id DESC"
	if limit > 0 {
		// One extra row detects whether a further page exists.
		query += " LIMIT ?"
		args = append(args, limit+1)
	}

	rows, err := store.ReadDB().Query(query, args...)
	if err != nil {
		return page, fmt.Errorf("list sessions for agent %s: %w", agentID, err)
	}
	defer func() { _ = rows.Close() }()

	page.Sessions = []AgentSessionItem{}
	for rows.Next() {
		var it AgentSessionItem
		var archived int
		if err := rows.Scan(
			&it.EntryID, &it.SessionID, &it.Title, &it.ProjectPath,
			&it.SessionType, &it.Role, &it.MemberName, &archived, &it.UpdatedAt,
		); err != nil {
			return page, fmt.Errorf("scan session of agent %s: %w", agentID, err)
		}
		it.Archived = archived != 0
		page.Sessions = append(page.Sessions, it)
	}
	if err := rows.Err(); err != nil {
		return page, fmt.Errorf("iterate sessions of agent %s: %w", agentID, err)
	}

	if limit > 0 && len(page.Sessions) > limit {
		page.HasMore = true
		page.Sessions = page.Sessions[:limit]
	}
	if n := len(page.Sessions); n > 0 {
		last := page.Sessions[n-1]
		page.NextCursor = last.UpdatedAt
		page.NextCursorID = last.EntryID
	}
	return page, nil
}
