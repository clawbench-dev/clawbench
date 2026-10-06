package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
//
// This is the host-only convenience wrapper over CreateGroupWithMembers (used
// by tests and any caller that adds members later); the one-step UI path calls
// CreateGroupWithMembers directly.
func CreateGroup(projectPath, title, backend, hostAgentID, hostName string) (groupID, hostMemberID string, err error) {
	return CreateGroupWithMembers(projectPath, title, hostAgentID, []GroupMemberSpec{
		{AgentID: hostAgentID, Backend: backend, DisplayName: hostName},
	})
}

// GroupMemberSpec is one member to create at group-creation time. DisplayName
// is only used on first creation (the row's stored title is authoritative
// afterwards).
type GroupMemberSpec struct {
	AgentID     string
	Backend     string
	DisplayName string
}

// CreateGroupWithMembers creates a group timeline row AND all its member rows
// (host included) in ONE transaction, so a failure leaves no half-built group
// behind. This is the one-step creation path (design §7.1, decision #25): the
// frontend picks members and host together and calls this once.
//
// hostAgentID must appear in specs; the matching spec's row becomes the host
// pointer. specs is expected to be non-empty and every spec to carry a
// non-empty AgentID — an invalid spec aborts the whole transaction.
//
// The host pointer is written inline (not via SetGroupHostMember, which takes
// the write lock and would deadlock inside this transaction). title_source is
// set to 'placeholder' inline for the same reason, so the group's host-name
// title is still replaceable by auto-title after the first message.
func CreateGroupWithMembers(projectPath, title, hostAgentID string, specs []GroupMemberSpec) (groupID, hostMemberID string, err error) {
	if len(specs) == 0 {
		return "", "", fmt.Errorf("create group: no members")
	}
	if len(specs) > maxGroupMembers {
		return "", "", ErrGroupMemberLimit
	}
	// Validate before touching the DB so a bad spec cannot even open a
	// transaction (and no id is generated for a doomed request).
	for _, s := range specs {
		if s.AgentID == "" {
			return "", "", fmt.Errorf("create group: member with empty agent id")
		}
	}
	projectID, idErr := store.ProjectIDForPath(projectPath)
	if idErr != nil {
		return "", "", idErr
	}

	// Generate every id BEFORE opening the transaction. generateSessionID
	// probes the DB for uniqueness via store.ReadDB(); a read issued while the
	// write transaction is open deadlocks whenever the read and write pools are
	// the same single-connection handle (the handler test fixture does exactly
	// that: store.SetDBForTest(db, db) with MaxOpenConns(1)).
	groupID = generateSessionID()
	if groupID == "" {
		return "", "", fmt.Errorf("failed to generate unique session ID")
	}
	memberIDs := make([]string, len(specs))
	for i := range specs {
		memberIDs[i] = generateSessionID()
		if memberIDs[i] == "" {
			return "", "", fmt.Errorf("failed to generate unique member session ID")
		}
	}

	tx, err := store.WriteBegin()
	if err != nil {
		return "", "", err
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()

	// The group row's backend mirrors its host (the group itself never runs a
	// turn; the host does). specs[0] is not assumed to be the host.
	groupBackend := ""
	for _, s := range specs {
		if s.AgentID == hostAgentID {
			groupBackend = s.Backend
			break
		}
	}

	if _, err := tx.Exec(
		"INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, agent_source, model, session_type, external_session_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		groupID, projectID, groupBackend, title, hostAgentID, "default", "", groupSessionType, "",
	); err != nil {
		return "", "", fmt.Errorf("create group session: %w", err)
	}
	// Inline (not markSessionTitlePlaceholder: that takes the write lock).
	if _, err := tx.Exec("UPDATE chat_sessions SET title_source = ? WHERE id = ?", TitleSourcePlaceholder, groupID); err != nil {
		return "", "", fmt.Errorf("mark group title placeholder: %w", err)
	}

	for i, s := range specs {
		if _, err := tx.Exec(
			"INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, agent_source, model, session_type, group_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			memberIDs[i], projectID, s.Backend, s.DisplayName, s.AgentID, "default", "", groupMemberSessionType, groupID,
		); err != nil {
			return "", "", fmt.Errorf("create member session: %w", err)
		}
		if s.AgentID == hostAgentID && hostMemberID == "" {
			hostMemberID = memberIDs[i]
		}
	}
	if hostMemberID == "" {
		return "", "", fmt.Errorf("create group: host agent %q not among members", hostAgentID)
	}

	// Host pointer inline: json_set on the group row's context_state.
	hostJSON, _ := json.Marshal(hostMemberID)
	if _, err := tx.Exec(
		"UPDATE chat_sessions SET context_state = json_set(CASE WHEN context_state = '' OR context_state IS NULL THEN '{}' ELSE context_state END, '$.host_member_id', json(?)) WHERE id = ?",
		string(hostJSON), groupID,
	); err != nil {
		return "", "", fmt.Errorf("set host member: %w", err)
	}

	if err := tx.Commit(); err != nil {
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
		// Re-adding an ACTIVE member is a no-op reuse: the active count does not
		// change, so the cap does not apply. No system event either — nothing
		// changed on the timeline.
		if !existing.Left {
			return existing.ID, nil
		}
		// Rejoining a left member reactivates an existing row, but that still
		// grows the active count by one, so it must clear the cap too.
		if err := ensureGroupMemberSlot(groupID); err != nil {
			return "", err
		}
		if err := setGroupMemberArchived(existing.ID, false); err != nil {
			return "", err
		}
		writeMemberSystemEvent(projectPath, groupID, agentID, displayName, " 重新加入")
		return existing.ID, nil
	}
	if err := ensureGroupMemberSlot(groupID); err != nil {
		return "", err
	}
	memberID, err := CreateSession(projectPath, backend, displayName, agentID, "", "default", groupMemberSessionType)
	if err != nil {
		return "", fmt.Errorf("create member session: %w", err)
	}
	if err := SetSessionGroupID(memberID, groupID); err != nil {
		return "", err
	}
	writeMemberSystemEvent(projectPath, groupID, agentID, displayName, "加入了讨论")
	return memberID, nil
}

// writeMemberSystemEvent appends a membership-change row to the group timeline
// (decisions #40/#41). The text carries the member's specialty so readers know
// what the newcomer is for, and is written best-effort: a failure to record a
// system event must never fail the membership change itself.
func writeMemberSystemEvent(projectPath, groupID, agentID, displayName, suffix string) {
	text := displayName + memberSpecialtySuffix(agentID) + suffix
	msgID, err := AddSystemMessage(projectPath, groupID, text)
	if err != nil {
		slog.Warn("group: writing member system event failed", "group", groupID, "err", err)
		return
	}
	// Broadcast only after the row is committed, so subscribers can render it
	// immediately (decisions #40/#43). Best-effort like the write itself.
	emitGroupSystemMessage(groupID, msgID, text)
}

// memberSpecialtySuffix renders "（specialty）" or "" — omitted entirely when the
// agent has no specialty, so a description-less member does not read "Name（）".
func memberSpecialtySuffix(agentID string) string {
	if s := GetAgentSpecialty(agentID); s != "" {
		return "（" + s + "）"
	}
	return ""
}

// ensureGroupMemberSlot returns ErrGroupMemberLimit when the group is already at
// maxGroupMembers active members. Callers invoke it only on paths that would
// ADD an active member (new row, or rejoining a left one).
func ensureGroupMemberSlot(groupID string) error {
	members, err := ListGroupMembers(groupID)
	if err != nil {
		return err
	}
	if countActiveMembers(members) >= maxGroupMembers {
		return ErrGroupMemberLimit
	}
	return nil
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
	//
	// is_host comes from the GROUP row's context_state (the host pointer is
	// stored there, not on the member row), so it is resolved with a correlated
	// subquery per member — still one statement for all groups. The frontend
	// uses it to lead the stack with the host.
	query := fmt.Sprintf(`
		SELECT m.group_id, m.id, m.agent_id, m.title, m.backend,
		       (m.id = COALESCE(json_extract(g.context_state, '$.host_member_id'), '')) AS is_host
		FROM chat_sessions m
		LEFT JOIN chat_sessions g ON g.id = m.group_id
		WHERE m.group_id IN (%s) AND m.session_type = ? AND m.archived = 0
		ORDER BY m.group_id, m.created_at ASC, m.id ASC`, placeholders)
	args = append(args, groupMemberSessionType)

	rows, err := store.ReadDB().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var groupID string
		var p model.GroupMemberPreview
		var isHost int
		if err := rows.Scan(&groupID, &p.ID, &p.AgentID, &p.Name, &p.Backend, &isHost); err != nil {
			return nil, err
		}
		p.IsHost = isHost != 0
		out[groupID] = append(out[groupID], p)
	}
	return out, rows.Err()
}

// ErrCannotRemoveHost is returned when a caller tries to remove the group's
// host member. The host is the only router, so removing it would leave the
// group unable to run another turn.
var ErrCannotRemoveHost = errors.New("cannot remove the group host")

// ErrGroupMemberLimit is returned when adding a member would push the group's
// ACTIVE member count past maxGroupMembers.
var ErrGroupMemberLimit = errors.New("group member limit reached")

// MaxGroupMembers caps a group's ACTIVE members (decision #78). Exported so the
// handler can report the limit to the user.
//
// Why a cap at all: every member is a separate ACP subprocess carrying its own
// node/npx runtime (hundreds of MB), and the ACP connection pool has no upper
// bound (its conns map is unbounded; minAliveConns is a floor, not a ceiling).
// A group turn touches every member, so an uncapped roster can spawn dozens of
// agents at once. Ten is also the point past which a "discussion" stops being
// useful to a model.
const MaxGroupMembers = 10

// maxGroupMembers is the internal alias used by the store.
const maxGroupMembers = MaxGroupMembers

// RemoveGroupMember soft-removes a member: the row is archived (kept) so its
// past speech in the group timeline stays attributed. Idempotent.
//
// The host member cannot be removed (ErrCannotRemoveHost): it is the group's
// only router, so dropping it would silently strand the group.
func RemoveGroupMember(groupID, memberID string) error {
	if memberID != "" && memberID == GetGroupHostMember(groupID) {
		return ErrCannotRemoveHost
	}
	_, err := store.WriteExec(
		"UPDATE chat_sessions SET archived = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND group_id = ?",
		memberID, groupID,
	)
	if err != nil {
		return err
	}
	// Record the departure on the timeline (decision #40). Best-effort: the
	// member is already archived, so a write failure must not surface as a
	// failed removal. Skip when the row did not change (already left) to keep
	// the removal idempotent without spamming the timeline.
	if name := memberDisplayName(memberID); name != "" {
		text := name + " 已离场"
		if msgID, err := AddSystemMessage(GetSessionProjectPathAnyPath(groupID), groupID, text); err != nil {
			slog.Warn("group: writing departure system event failed", "group", groupID, "err", err)
		} else {
			emitGroupSystemMessage(groupID, msgID, text)
		}
	}
	return nil
}

// memberDisplayName returns a member row's display name (empty if unknown).
func memberDisplayName(memberID string) string {
	var name string
	_ = store.ReadDB().QueryRow("SELECT COALESCE(title, '') FROM chat_sessions WHERE id = ?", memberID).Scan(&name)
	return name
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
