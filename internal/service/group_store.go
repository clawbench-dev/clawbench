package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"clawbench/internal/model"
	"clawbench/internal/store"
)

// group_store.go holds the group-chat persistence layer. A "group" is a
// chat_sessions row (session_type='group') owning the timeline; each member is
// a hidden chat_sessions row (session_type='group_member', group_id set) that
// carries the connection binding. See
// docs/plans/2026-10-04-ai-group-chat-design.md §4.

const (
	// groupSessionType is the session_type of a group timeline row.
	groupSessionType = "group"
	// groupMemberSessionType is the session_type of a hidden member row.
	groupMemberSessionType = "group_member"
	// defaultGroupMaxRounds is the round cap when the group has no override.
	defaultGroupMaxRounds = 10
)

// GroupMember is one member of a group (a hidden chat_sessions row).
type GroupMember struct {
	ID      string // member session row id (also the speaker id in chat_history.agent_id)
	AgentID string // real agent id
	Name    string // display name (member row title)
	Backend string
	Left    bool // archived=1: removed from the group but its speech is kept
}

// CreateGroup creates a group timeline row plus its host member row. The host
// member is a normal member (it both controls the flow and participates), so
// the group is usable immediately.
//
// title may be empty; the caller substitutes a placeholder (e.g. host name).
func CreateGroup(projectPath, title, backend, hostAgentID, hostName string) (groupID, hostMemberID string, err error) {
	groupID, err = CreateSession(projectPath, backend, title, hostAgentID, "", "default", groupSessionType)
	if err != nil {
		return "", "", fmt.Errorf("create group session: %w", err)
	}
	hostMemberID, err = AddGroupMember(projectPath, groupID, backend, hostAgentID, hostName)
	if err != nil {
		return "", "", err
	}
	if err := SetGroupHostMember(groupID, hostMemberID); err != nil {
		return "", "", err
	}
	return groupID, hostMemberID, nil
}

// AddGroupMember adds an agent to the group, or REJOINS it if a member row for
// the same (group, agent) already exists.
//
// Why dedup: a member's only identity is its agent — the routing tag addresses
// members by NAME, and resolveTargets maps names through a map, so two rows for
// the same agent would make one of them unreachable (a "mute" member that can
// be selected but never spoken to). Re-adding an agent that LEFT the group
// therefore reuses its original row and clears the archived flag (the A
// contract), preserving its past speech attribution instead of creating a
// duplicate row. displayName is only applied on first creation; the row's
// stored title is authoritative afterwards.
//
// Returns the member row id (existing or new).
func AddGroupMember(projectPath, groupID, backend, agentID, displayName string) (string, error) {
	if existing, ok := findGroupMemberByAgent(groupID, agentID); ok {
		if existing.Left {
			if err := setGroupMemberArchived(existing.ID, false); err != nil {
				return "", err
			}
		}
		return existing.ID, nil
	}
	memberID, err := CreateSession(projectPath, backend, displayName, agentID, "", "default", groupMemberSessionType)
	if err != nil {
		return "", fmt.Errorf("create member session: %w", err)
	}
	if err := SetSessionGroupID(memberID, groupID); err != nil {
		return "", err
	}
	return memberID, nil
}

// findGroupMemberByAgent returns the group's member row for agentID, preferring
// an active (non-archived) row when both exist. ok is false when the agent is
// not a member.
func findGroupMemberByAgent(groupID, agentID string) (GroupMember, bool) {
	rows, err := store.ReadDB().Query(
		`SELECT id, agent_id, title, backend, archived FROM chat_sessions
		 WHERE group_id = ? AND session_type = ? AND agent_id = ?
		 ORDER BY archived ASC, created_at ASC, id ASC`,
		groupID, groupMemberSessionType, agentID,
	)
	if err != nil {
		return GroupMember{}, false
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var m GroupMember
		var archived int
		if err := rows.Scan(&m.ID, &m.AgentID, &m.Name, &m.Backend, &archived); err != nil {
			return GroupMember{}, false
		}
		m.Left = archived != 0
		return m, true
	}
	return GroupMember{}, false
}

// setGroupMemberArchived flips a member row's archived flag (false = rejoined).
func setGroupMemberArchived(memberID string, archived bool) error {
	v := 0
	if archived {
		v = 1
	}
	_, err := store.WriteExec("UPDATE chat_sessions SET archived = ? WHERE id = ?", v, memberID)
	return err
}

// SetSessionGroupID points a member row at its owning group.
func SetSessionGroupID(memberID, groupID string) error {
	_, err := store.WriteExec("UPDATE chat_sessions SET group_id = ? WHERE id = ?", groupID, memberID)
	return err
}

// ListGroupMembers returns every member row of a group, including removed
// (archived) ones so the timeline can still label their past speech. Active
// members are those with Left == false.
func ListGroupMembers(groupID string) ([]GroupMember, error) {
	rows, err := store.ReadDB().Query(
		`SELECT id, agent_id, title, backend, archived FROM chat_sessions
		 WHERE group_id = ? AND session_type = ?
		 ORDER BY created_at ASC, id ASC`,
		groupID, groupMemberSessionType,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	members := make([]GroupMember, 0)
	for rows.Next() {
		var m GroupMember
		var archived int
		if err := rows.Scan(&m.ID, &m.AgentID, &m.Name, &m.Backend, &archived); err != nil {
			return nil, err
		}
		m.Left = archived != 0
		members = append(members, m)
	}
	return members, rows.Err()
}

// countActiveMembers returns how many members have not left the group.
func countActiveMembers(members []GroupMember) int {
	n := 0
	for _, m := range members {
		if !m.Left {
			n++
		}
	}
	return n
}

// GroupMembersForGroups returns the ACTIVE (non-archived) members of each group
// id, keyed by group id, as compact previews for the session list's stacked
// avatars. One query for all ids — the sessions list must not fan out a query
// per group (the same reason GetTagsForSessions exists). Group ids with no
// active members are absent from the map; empty input returns an empty map
// without touching the DB.
func GroupMembersForGroups(groupIDs []string) (map[string][]model.GroupMemberPreview, error) {
	out := map[string][]model.GroupMemberPreview{}
	if len(groupIDs) == 0 {
		return out, nil
	}

	// De-duplicate and drop empties before building IN (...). SQLite's default
	// variable limit is 999; a session-list page is far smaller, and this is a
	// second query, so a plain expansion is fine (mirrors GetTagsForSessions).
	seen := make(map[string]bool, len(groupIDs))
	ids := make([]string, 0, len(groupIDs))
	for _, id := range groupIDs {
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
	args := make([]any, 0, len(ids)+1)
	for _, id := range ids {
		args = append(args, id)
	}

	// archived = 0 keeps left members out of the list preview (the full roster,
	// left members included, is only in GET /api/group/members). Order matches
	// ListGroupMembers so the stack is stable across reloads.
	query := fmt.Sprintf(`
		SELECT group_id, id, agent_id, title, backend
		FROM chat_sessions
		WHERE group_id IN (%s) AND session_type = ? AND archived = 0
		ORDER BY group_id, created_at ASC, id ASC`, placeholders)
	args = append(args, groupMemberSessionType)

	rows, err := store.ReadDB().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var groupID string
		var p model.GroupMemberPreview
		if err := rows.Scan(&groupID, &p.ID, &p.AgentID, &p.Name, &p.Backend); err != nil {
			return nil, err
		}
		out[groupID] = append(out[groupID], p)
	}
	return out, rows.Err()
}

// RemoveGroupMember soft-removes a member: the row is archived (kept) so its
// past speech in the group timeline stays attributed. Idempotent.
func RemoveGroupMember(groupID, memberID string) error {
	_, err := store.WriteExec(
		"UPDATE chat_sessions SET archived = 1 WHERE id = ? AND group_id = ?",
		memberID, groupID,
	)
	return err
}

// GetGroupHostMember returns the host member row id (empty if unset).
func GetGroupHostMember(groupID string) string {
	var hostID string
	_ = store.ReadDB().QueryRow(
		"SELECT COALESCE(json_extract(context_state, '$.host_member_id'), '') FROM chat_sessions WHERE id = ?",
		groupID,
	).Scan(&hostID)
	return hostID
}

// SetGroupHostMember records which member is the host (stored in the group
// row's context_state JSON).
func SetGroupHostMember(groupID, memberID string) error {
	val, err := json.Marshal(memberID)
	if err != nil {
		return err
	}
	PatchContextStateMerge(groupID, map[string]string{"host_member_id": string(val)})
	return nil
}

// GetGroupMaxRounds returns the group's configured round cap, or the default
// when unset/invalid.
func GetGroupMaxRounds(groupID string) int {
	var raw string
	_ = store.ReadDB().QueryRow(
		"SELECT COALESCE(json_extract(context_state, '$.maxRounds'), '') FROM chat_sessions WHERE id = ?",
		groupID,
	).Scan(&raw)
	if raw == "" {
		return defaultGroupMaxRounds
	}
	var n int
	if err := json.Unmarshal([]byte(raw), &n); err != nil || n <= 0 {
		return defaultGroupMaxRounds
	}
	return n
}

// SetGroupMaxRounds stores the group's round cap in its context_state JSON.
func SetGroupMaxRounds(groupID string, n int) error {
	if n <= 0 {
		return fmt.Errorf("maxRounds must be positive, got %d", n)
	}
	PatchContextStateMerge(groupID, map[string]string{"maxRounds": fmt.Sprintf("%d", n)})
	return nil
}

// GetAgentDisplayName returns an agent's display name (falling back to its id,
// then "AI"), used to seed a member row's title and the group's placeholder.
func GetAgentDisplayName(agentID string) string {
	if a := model.GetAgent(agentID); a != nil && a.Name != "" {
		return a.Name
	}
	if agentID != "" {
		return agentID
	}
	return "AI"
}

// GetSessionType returns a session's session_type ("" if not found).
func GetSessionType(sessionID string) string {
	var t string
	_ = store.ReadDB().QueryRow("SELECT session_type FROM chat_sessions WHERE id = ?", sessionID).Scan(&t)
	return t
}

// GetMemberCursor returns a member's seen_cursor: the group-timeline high-water
// mark it has already been shown. 0 means "nothing seen yet".
func GetMemberCursor(memberID string) int64 {
	var raw string
	_ = store.ReadDB().QueryRow(
		"SELECT COALESCE(json_extract(context_state, '$.seen_cursor'), '') FROM chat_sessions WHERE id = ?",
		memberID,
	).Scan(&raw)
	if raw == "" {
		return 0
	}
	var n int64
	if err := json.Unmarshal([]byte(raw), &n); err != nil {
		return 0
	}
	return n
}

// SetMemberCursor records a member's seen_cursor.
func SetMemberCursor(memberID string, cursor int64) {
	PatchContextStateMerge(memberID, map[string]string{"seen_cursor": fmt.Sprintf("%d", cursor)})
}

// GroupTimelineHighWater returns the largest chat_history id on the group
// timeline (0 if empty). Used as the per-speech cursor.
func GroupTimelineHighWater(groupID string) int64 {
	var id int64
	_ = store.ReadDB().QueryRow(
		"SELECT COALESCE(MAX(id), 0) FROM chat_history WHERE session_id = ?", groupID,
	).Scan(&id)
	return id
}
