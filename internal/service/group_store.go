package service

import (
	"encoding/json"
	"fmt"

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

// AddGroupMember creates a hidden member row for the group and returns its id.
// displayName is stored in the member row's title (design §4.4/#36).
func AddGroupMember(projectPath, groupID, backend, agentID, displayName string) (string, error) {
	memberID, err := CreateSession(projectPath, backend, displayName, agentID, "", "default", groupMemberSessionType)
	if err != nil {
		return "", fmt.Errorf("create member session: %w", err)
	}
	if err := SetSessionGroupID(memberID, groupID); err != nil {
		return "", err
	}
	return memberID, nil
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
