package service

import (
	"context"
	"database/sql"
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
	groupSessionType = store.SessionTypeGroup
	// groupMemberSessionType is the session_type of a hidden member row.
	groupMemberSessionType = store.SessionTypeGroupMember
	// defaultGroupMaxSpeeches is the member-to-member speech cap when the
	// global config value is unset/invalid. Kept in sync with
	// model.ApplyDefaults's chat.group_max_speeches default.
	defaultGroupMaxSpeeches = 100
	// GroupModeHost is a group whose flow is controlled by a host member that
	// routes to named speakers (the original mode).
	GroupModeHost = "host"
	// GroupModeFree is a group with no host: participants relay the floor to
	// each other by @-mentioning, seeded by the user's own @-mentions (design
	// §13). The mode is chosen at creation (host selected vs not) and is NOT
	// switchable.
	GroupModeFree = "free"
	// defaultGroupUserTarget is the fallback display name of the HUMAN user as a
	// group participant when no nickname is configured. Agents address the user
	// with this exact name (<clawbench-mention targets="User">...). The live
	// value is groupUserTarget() (config-driven, chat.user_nickname).
	defaultGroupUserTarget = "User"
	// groupUserTargetID is a sentinel member row id for the user. It is not a
	// chat_sessions row: the user has no backend connection and no AI turn.
	// "__user__" cannot collide with a UUID member row id.
	groupUserTargetID = "__user__"
)

// groupUserTarget is the reserved display name of the HUMAN user as a group
// participant. It is config-driven (chat.user_nickname, default "User") so the
// user can pick a less jarring name than the language-neutral default; it must
// stay in sync with the frontend's userNickname and must never collide with a
// real member name (enforced bidirectionally — see IsUserNickname).
//
// The value is trimmed defensively: the PATCH path normalizes it, but a
// hand-edited config.yaml bypasses that, and the mention parser trims each
// target before lookup — so an untrimmed nickname would be unreachable.
func groupUserTarget() string {
	if n := strings.TrimSpace(model.ChatUserNickname); n != "" {
		return n
	}
	return defaultGroupUserTarget
}

// GroupMember is one member of a group (a hidden chat_sessions row).
type GroupMember struct {
	ID      string // member session row id (also the speaker id in chat_history.agent_id)
	AgentID string // real agent id
	Name    string // display name (member row agent_name)
	Backend string
	Avatar  string // per-member avatar SVG snapshot (may be empty → fall back to agent avatar)
	Left    bool   // archived=1: removed from the group but its speech is kept
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

// validateGroupSpecs validates the specs and enforces the member cap. The cap
// now counts INSTANCES (not unique agents): the same agent may be added to one
// group multiple times, each an independent member with its own name.
// Extracted to keep CreateGroupWithMembers' complexity in budget.
func validateGroupSpecs(specs []GroupMemberSpec) error {
	for _, s := range specs {
		if s.AgentID == "" {
			return fmt.Errorf("create group: member with empty agent id")
		}
	}
	if len(specs) > maxGroupMembers {
		return ErrGroupMemberLimit
	}
	return nil
}

// assignMemberNames gives every spec a UNIQUE display name, in the order the
// specs appear.
//
// Naming rule (design §4, updated for the built-in pool): the FIRST instance of
// an agent keeps its bare name ("Claude"); later instances draw the first
// unused name from groupNamePool ("Alice", "Bob", …). When the pool is
// exhausted the name falls back to the pre-pool rule ("<base> <ordinal>").
//
// The starting ordinal is (existing member rows for that agent in this group) +
// (ordinal within this batch); because member rows are only ever archived
// (never deleted) and a re-add always creates a NEW instance, the ordinal is
// monotonic and a historical nickname always refers to the same member. A
// collision (with another member's name, a pool name, or the reserved user
// nickname) advances until unique, so routing by name stays unambiguous.
//
// existingCounts maps agent id → number of member rows already in the group
// (archived included). It may be nil (no existing members). taken holds names
// already in use in the group, including the reserved user nickname.
//
// A spec's DisplayName, when non-empty, is used as the BASE name (callers such
// as CreateGroup may pass an explicit name); otherwise the base is derived from
// the agent's display name.
func assignMemberNames(specs []GroupMemberSpec, existingCounts map[string]int, taken map[string]bool) {
	used := make(map[string]bool, len(taken)+len(specs))
	for n := range taken {
		used[n] = true
	}
	used[groupUserTarget()] = true
	nextOrdinal := make(map[string]int, len(specs))
	for i := range specs {
		s := &specs[i]
		base := strings.TrimSpace(s.DisplayName)
		if base == "" {
			base = GetAgentDisplayName(s.AgentID)
		}
		// Instance index: existing rows + already-named in this batch, then +1.
		k := existingCounts[s.AgentID] + nextOrdinal[s.AgentID] + 1
		candidate := ""
		if k == 1 {
			// First instance keeps the agent's own name.
			candidate = base
		} else {
			// Duplicate instance: prefer an unused pool name.
			candidate = nextPoolName(used)
		}
		if candidate == "" || used[candidate] {
			// Pool exhausted, or the first instance's bare name collides:
			// fall back to "<base> <ordinal>", advancing until unique.
			candidate = base
			if k > 1 {
				candidate = fmt.Sprintf("%s %d", base, k)
			}
			for used[candidate] {
				k++
				candidate = fmt.Sprintf("%s %d", base, k)
			}
		}
		nextOrdinal[s.AgentID]++
		used[candidate] = true
		s.DisplayName = candidate
	}
}

// groupIdentityFields picks the group row's (backend, agent_id) identity: the
// host's in host mode, the first member's in free mode (a group row has NOT NULL
// backend and its agent_id drives the list icon). Extracted from
// CreateGroupWithMembers to keep that function's cognitive complexity in budget.
func groupIdentityFields(mode string, specs []GroupMemberSpec, hostAgentID string) (backend, agentID string) {
	if mode != GroupModeHost {
		return specs[0].Backend, specs[0].AgentID
	}
	for _, s := range specs {
		if s.AgentID == hostAgentID {
			return s.Backend, s.AgentID
		}
	}
	return "", ""
}

// GroupMemberSpec is one member to create. DisplayName, when non-empty, is the
// BASE display name (the service appends a numeric suffix for repeat instances
// of the same agent); when empty the base is derived from the agent's name. The
// assigned name is stored in the member row's agent_name.
type GroupMemberSpec struct {
	AgentID     string
	Backend     string
	DisplayName string
}

// CreateGroupWithMembers creates a group timeline row AND all its member rows
// in ONE transaction, so a failure leaves no half-built group behind. This is
// the one-step creation path (design §7.1, decision #25).
//
// hostAgentID selects the mode (design §13.1): when it names one of the specs,
// that member becomes the host pointer and the group is host mode; when it is
// empty, the group is FREE mode (no host) and the user seeds speakers with
// @-mentions instead. specs is expected to be non-empty and every spec to carry
// a non-empty AgentID — an invalid spec aborts the whole transaction.
//
// The host pointer (host mode) and the mode marker are written inline (not via
// SetGroupHostMember/SetGroupMode, which take the write lock and would deadlock
// inside this transaction). title_source is set to 'placeholder' inline for the
// same reason, so the group's title is still replaceable by auto-title after
// the first message.
func CreateGroupWithMembers(projectPath, title, hostAgentID string, specs []GroupMemberSpec) (groupID, hostMemberID string, err error) {
	if len(specs) == 0 {
		return "", "", fmt.Errorf("create group: no members")
	}
	// Validate before touching the DB. The same agent MAY appear multiple times
	// (each occurrence is an independent instance with its own name); the cap
	// therefore counts instances, not unique agents. Every spec must carry a
	// non-empty AgentID. The Web drawer can send duplicates by design; POST
	// /api/group/create takes an arbitrary array.
	if verr := validateGroupSpecs(specs); verr != nil {
		return "", "", verr
	}

	// Mode: a named host that is among the members ⇒ host mode; no host ⇒ free.
	mode := GroupModeFree
	if hostAgentID != "" {
		mode = GroupModeHost
	}
	// Free mode needs at least two members to be a "group" (design §13.1 #F10):
	// a one-member free group is a single chat wearing a group's clothes.
	if mode == GroupModeFree && len(specs) < 2 {
		return "", "", ErrFreeGroupNeedsTwoMembers
	}

	// Seed each spec with a unique display name. A brand-new group has no
	// existing member rows, so the ordinal starts at 1 for every agent; the
	// reserved user nickname is the only pre-taken name.
	assignMemberNames(specs, nil, nil)

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

	// The group row's identity fields mirror a member (the group itself never
	// runs a turn). Host mode uses the host; free mode has no host, so the
	// FIRST member fills backend (NOT NULL) and agent_id (list icon), and the
	// title is a generic placeholder.
	groupBackend, groupAgentID := groupIdentityFields(mode, specs, hostAgentID)
	if rowErr := insertGroupRow(tx, groupID, projectID, groupBackend, groupAgentID, title); rowErr != nil {
		return "", "", rowErr
	}

	hostMemberID, err = insertGroupMemberRows(tx, groupID, projectID, hostAgentID, mode, specs, memberIDs)
	if err != nil {
		return "", "", err
	}

	// Mode marker (and, in host mode, the host pointer) inline on the group
	// row's context_state. One statement so the mode write cannot half-apply.
	if err := writeGroupModeInline(tx, groupID, mode, hostMemberID); err != nil {
		return "", "", err
	}

	if err := tx.Commit(); err != nil {
		return "", "", err
	}
	return groupID, hostMemberID, nil
}

// insertGroupRow writes the group timeline row and marks its title a
// placeholder (inline, not markSessionTitlePlaceholder which takes the write
// lock). Extracted from CreateGroupWithMembers.
func insertGroupRow(tx *sql.Tx, groupID string, projectID int64, backend, agentID, title string) error {
	if _, err := tx.ExecContext(
		context.Background(),
		"INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, agent_source, model, session_type, external_session_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		groupID, projectID, backend, title, agentID, "default", "", groupSessionType, "",
	); err != nil {
		return fmt.Errorf("create group session: %w", err)
	}
	// Inline (not markSessionTitlePlaceholder: that takes the write lock).
	if _, err := tx.ExecContext(context.Background(), "UPDATE chat_sessions SET title_source = ? WHERE id = ?", TitleSourcePlaceholder, groupID); err != nil {
		return fmt.Errorf("mark group title placeholder: %w", err)
	}
	return nil
}

// insertGroupMemberRows writes every member row and returns the host's member
// id (host mode only). The member's display name goes into agent_name; `title`
// is left empty (a member row is a hidden session, so a session title is
// meaningless — see the agent_name migration note in database.go). Extracted
// from CreateGroupWithMembers to keep that function's complexity in budget.
//
// Host selection: the FIRST instance of the host agent becomes the host. With
// duplicate instances of the same agent, hostMemberID is set on the first
// match and never overwritten.
func insertGroupMemberRows(tx *sql.Tx, groupID string, projectID int64, hostAgentID, mode string, specs []GroupMemberSpec, memberIDs []string) (hostMemberID string, err error) {
	for i, s := range specs {
		if _, err := tx.ExecContext(
			context.Background(),
			"INSERT INTO chat_sessions (id, project_id, backend, title, agent_name, agent_id, agent_source, model, session_type, group_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			memberIDs[i], projectID, s.Backend, "", s.DisplayName, s.AgentID, "default", "", groupMemberSessionType, groupID,
		); err != nil {
			return "", fmt.Errorf("create member session: %w", err)
		}
		if mode == GroupModeHost && s.AgentID == hostAgentID && hostMemberID == "" {
			hostMemberID = memberIDs[i]
		}
	}
	if mode == GroupModeHost && hostMemberID == "" {
		return "", fmt.Errorf("create group: host agent %q not among members", hostAgentID)
	}
	return hostMemberID, nil
}

// writeGroupModeInline writes the group's mode marker (and, in host mode, the
// host pointer) into the row's context_state in the SAME statement. Extracted
// from CreateGroupWithMembers.
func writeGroupModeInline(tx *sql.Tx, groupID, mode, hostMemberID string) error {
	modeJSON, _ := json.Marshal(mode)
	setArgs := "'$.group_mode', json(?)"
	args := []any{string(modeJSON)}
	if mode == GroupModeHost {
		hostJSON, _ := json.Marshal(hostMemberID)
		setArgs += ", '$.host_member_id', json(?)"
		args = append(args, string(hostJSON))
	}
	updateSQL := "UPDATE chat_sessions SET context_state = json_set(CASE WHEN context_state = '' OR context_state IS NULL THEN '{}' ELSE context_state END, " + setArgs + ") WHERE id = ?"
	args = append(args, groupID)
	if _, err := tx.ExecContext(context.Background(), updateSQL, args...); err != nil {
		return fmt.Errorf("set group mode/host: %w", err)
	}
	return nil
}

// AddGroupMember adds an agent to the group as a NEW instance. There is no
// rejoin/reuse: adding an agent that is already (or was previously) a member
// creates another independent instance with its own name, so the same agent can
// appear several times. displayName is the display name to store in agent_name;
// callers that need the auto-numbered name should go through AddGroupMembers
// (which assigns unique names across the batch).
//
// Returns the new member row id.
func AddGroupMember(projectPath, groupID, backend, agentID, displayName string) (string, error) {
	if err := ensureGroupMemberSlot(groupID); err != nil {
		return "", err
	}
	memberID, err := createMemberRow(projectPath, groupID, backend, agentID, displayName)
	if err != nil {
		return "", err
	}
	writeMemberSystemEvent(projectPath, groupID, agentID, displayName, "加入了讨论")
	return memberID, nil
}

// createMemberRow inserts one hidden member session row with the display name in
// agent_name and an empty title, then binds it to the group.
func createMemberRow(projectPath, groupID, backend, agentID, displayName string) (string, error) {
	memberID, err := CreateSession(projectPath, backend, "", agentID, "", "default", groupMemberSessionType)
	if err != nil {
		return "", fmt.Errorf("create member session: %w", err)
	}
	if err := SetMemberAgentName(memberID, displayName); err != nil {
		return "", err
	}
	if err := SetSessionGroupID(memberID, groupID); err != nil {
		return "", err
	}
	return memberID, nil
}

// AddGroupMembers adds several agents to a group in one call, enforcing the
// member cap (decision #78) on the WHOLE batch and writing every row in ONE
// transaction: if the cap rejects the batch, OR any row fails to insert, NO row
// is committed. The cap counts INSTANCES: every spec is a new member, and
// duplicates of one agent each consume a slot.
//
// Every spec gets a UNIQUE display name assigned here (design §4): existing
// member rows of the same agent in this group (archived included) determine the
// starting ordinal, and the name is bumped on collision, so routing by name
// stays unambiguous.
//
// Why a service-level batch rather than looping AddGroupMember in the handler
// (which is what O19's spec explicitly forbids): the per-call slot check cannot
// see the rest of the batch, and per-call writes commit independently — a loop
// commits the earlier additions and then returns an error on a later one, so
// the request "fails" but the roster silently changed.
//
// Returns the member row id for each spec, in input order.
//
// Concurrency note: the active-count read and the transaction are not under one
// lock, so two concurrent callers could each read "11 active" and both add one
// (a TOCTOU that could reach 13). This is a single-user, low-frequency
// operation (the drawer's add-members action), so the window is not closed with
// a write-lock-held read.
func AddGroupMembers(projectPath, groupID string, specs []GroupMemberSpec) ([]string, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	members, err := ListGroupMembers(groupID)
	if err != nil {
		return nil, err
	}
	active := countActiveMembers(members)

	// Each spec is a new instance, so every non-empty spec consumes a slot.
	netNew := 0
	for _, s := range specs {
		if s.AgentID != "" {
			netNew++
		}
	}
	if active+netNew > maxGroupMembers {
		return nil, ErrGroupMemberLimit
	}

	// Drop empty specs (defensive; callers never send them) so ids, names and
	// rows stay aligned.
	kept := make([]GroupMemberSpec, 0, len(specs))
	for _, s := range specs {
		if s.AgentID != "" {
			kept = append(kept, s)
		}
	}
	if len(kept) == 0 {
		return nil, nil
	}

	// Seed unique names. existingCounts counts ALL member rows (archived
	// included) per agent, so the ordinal is monotonic across add/remove cycles.
	existingCounts := make(map[string]int, len(members))
	taken := make(map[string]bool, len(members))
	for _, m := range members {
		existingCounts[m.AgentID]++
		taken[m.Name] = true
	}
	assignMemberNames(kept, existingCounts, taken)

	projectID, idErr := store.ProjectIDForPath(projectPath)
	if idErr != nil {
		return nil, idErr
	}

	// Generate every id BEFORE opening the transaction (generateSessionID probes
	// the DB via store.ReadDB(); a read inside the write transaction deadlocks
	// when the read and write pools share one connection, as the handler test
	// fixture does).
	memberIDs := make([]string, len(kept))
	for i := range kept {
		memberIDs[i] = generateSessionID()
		if memberIDs[i] == "" {
			return nil, fmt.Errorf("failed to generate unique member session ID")
		}
	}

	// Insert every row in ONE transaction. The write lock is acquired and
	// released inside insertGroupMemberRowsTx (NOT via a deferred unlock in this
	// function): the lock is a non-reentrant mutex, and the best-effort side
	// effects below call WriteExec, which would deadlock if this frame still held
	// the lock.
	if err := insertGroupMemberRowsTx(groupID, projectID, kept, memberIDs); err != nil {
		return nil, err
	}

	// Best-effort side effects AFTER commit (a failure here must not roll back
	// the membership change): the per-agent auto-approve default (matching the
	// single-add path) and the timeline system events.
	for i, s := range kept {
		applyAgentAutoApproveDefault(memberIDs[i], s.AgentID)
		writeMemberSystemEvent(projectPath, groupID, s.AgentID, s.DisplayName, "加入了讨论")
	}
	return memberIDs, nil
}

// insertGroupMemberRowsTx inserts the member rows for AddGroupMembers in one
// transaction, acquiring and releasing the write lock itself so the caller can
// run best-effort side effects afterwards without holding the lock.
func insertGroupMemberRowsTx(groupID string, projectID int64, specs []GroupMemberSpec, memberIDs []string) error {
	tx, err := store.WriteBegin()
	if err != nil {
		return err
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()
	// Free mode + empty host: the add path tracks no host pointer.
	if _, err := insertGroupMemberRows(tx, groupID, projectID, "", GroupModeFree, specs, memberIDs); err != nil {
		return err
	}
	return tx.Commit()
}

// writeMemberSystemEvent appends a membership-change row to the group timeline
// (decisions #40/#41). The text carries the member's specialty so readers know
// what the newcomer is for, and is written best-effort: a failure to record a
// system event must never fail the membership change itself.
func writeMemberSystemEvent(projectPath, groupID, agentID, displayName, suffix string) {
	writeGroupSystemMessage(projectPath, groupID, displayName+memberSpecialtySuffix(agentID)+suffix)
}

// writeGroupSystemMessage appends an arbitrary role='system' row to the group
// timeline and broadcasts it. Best-effort: a failure to record a system event
// must never fail the operation that produced it.
func writeGroupSystemMessage(projectPath, groupID, text string) {
	msgID, err := AddSystemMessage(projectPath, groupID, text)
	if err != nil {
		slog.Warn("group: writing system message failed", "group", groupID, "err", err)
		return
	}
	// Broadcast only after the row is committed, so subscribers can render it
	// immediately (decisions #40/#43). Best-effort like the write itself.
	emitGroupSystemMessage(groupID, msgID, text)
}

// addPendingBcc stores a private note whose target was not named in this round,
// so it is delivered with that target's next turn (see the group_pending_bcc
// DDL). A no-op when the target name or content is empty.
func addPendingBcc(groupID, targetName, content string) {
	target := strings.TrimSpace(targetName)
	if groupID == "" || target == "" || strings.TrimSpace(content) == "" {
		return
	}
	if _, err := store.WriteExec(
		`INSERT INTO group_pending_bcc (group_id, target_name, content) VALUES (?, ?, ?)`,
		groupID, target, content,
	); err != nil {
		slog.Warn("group: storing pending bcc failed", "group", groupID, "target", target, "err", err)
	}
}

// pendingBccForTarget returns the stored notes for one target, oldest first,
// with their row ids so the caller can delete exactly what it delivered.
func pendingBccForTarget(groupID, targetName string) []pendingBcc {
	target := strings.TrimSpace(targetName)
	if groupID == "" || target == "" {
		return nil
	}
	rows, err := store.ReadDB().Query(
		`SELECT id, content FROM group_pending_bcc WHERE group_id = ? AND target_name = ? ORDER BY id ASC`,
		groupID, target,
	)
	if err != nil {
		slog.Warn("group: reading pending bcc failed", "group", groupID, "target", target, "err", err)
		return nil
	}
	defer func() { _ = rows.Close() }()
	out := []pendingBcc{}
	for rows.Next() {
		var p pendingBcc
		if err := rows.Scan(&p.ID, &p.Content); err != nil {
			slog.Warn("group: scanning pending bcc failed", "err", err)
			continue
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("group: iterating pending bcc failed", "group", groupID, "target", target, "err", err)
	}
	return out
}

// deletePendingBcc removes delivered notes by row id. Called only after a turn
// that actually consumed them: a failed/cancelled turn keeps its rows so the
// note is re-delivered next time (decision #69 semantics).
func deletePendingBcc(ids []int64) {
	if len(ids) == 0 {
		return
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	if _, err := store.WriteExec(
		`DELETE FROM group_pending_bcc WHERE id IN (`+strings.Join(placeholders, ",")+`)`, args...,
	); err != nil {
		slog.Warn("group: deleting delivered pending bcc failed", "err", err)
	}
}

// deletePendingBccForTarget drops every stored note for one target in a group.
// Used when the target is the USER (its notes are shown in the UI card rather
// than delivered to an AI turn) and when a member leaves.
func deletePendingBccForTarget(groupID, targetName string) {
	target := strings.TrimSpace(targetName)
	if groupID == "" || target == "" {
		return
	}
	if _, err := store.WriteExec(
		`DELETE FROM group_pending_bcc WHERE group_id = ? AND target_name = ?`, groupID, target,
	); err != nil {
		slog.Warn("group: clearing pending bcc for target failed", "group", groupID, "target", target, "err", err)
	}
}

// deletePendingBccForGroup drops every stored note for a group. Used when the
// group is archived/deleted so the table cannot grow without bound.
func deletePendingBccForGroup(groupID string) {
	if groupID == "" {
		return
	}
	if _, err := store.WriteExec(`DELETE FROM group_pending_bcc WHERE group_id = ?`, groupID); err != nil {
		slog.Warn("group: clearing pending bcc for group failed", "group", groupID, "err", err)
	}
}

// RenameUserPendingBcc migrates every pending private note addressed to the
// human user from oldName to newName, so a nickname change does not orphan an
// undelivered note (notes are keyed by target name and delivered on that
// target's next turn). Called when chat.user_nickname changes; a no-op when the
// names match or either is empty.
func RenameUserPendingBcc(oldName, newName string) {
	oldT := strings.TrimSpace(oldName)
	newT := strings.TrimSpace(newName)
	if oldT == "" || newT == "" || oldT == newT {
		return
	}
	if _, err := store.WriteExec(
		`UPDATE group_pending_bcc SET target_name = ? WHERE target_name = ?`, newT, oldT,
	); err != nil {
		slog.Warn("group: renaming pending bcc target failed", "old", oldT, "new", newT, "err", err)
	}
}

// pendingBcc is one stored private note awaiting delivery.
type pendingBcc struct {
	ID      int64
	Content string
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
// ADD a new member instance.
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

// SetSessionGroupID points a member row at its owning group.
func SetSessionGroupID(memberID, groupID string) error {
	_, err := store.WriteExec("UPDATE chat_sessions SET group_id = ? WHERE id = ?", groupID, memberID)
	return err
}

// SetMemberAgentName sets a member row's authoritative display name (the
// agent_name column). It never touches `title`, which is left empty for member
// rows (see the agent_name migration note in database.go).
func SetMemberAgentName(memberID, name string) error {
	_, err := store.WriteExec("UPDATE chat_sessions SET agent_name = ? WHERE id = ?", name, memberID)
	return err
}

// SetMemberAvatar stores a member's own avatar SVG (agent_avatar) and returns
// how many rows were actually updated. The WHERE clause pins both the member row
// AND its group, so a caller cannot write an avatar onto a member of a different
// group even if it learns the row id; only group_member rows are touched (a
// plain session has no agent_avatar meaning). A member id belonging to another
// group matches no row, so the count is 0 and the caller reports an honest
// "saved" total.
func SetMemberAvatar(groupID, memberID, avatar string) (int64, error) {
	res, err := store.WriteExec(
		`UPDATE chat_sessions SET agent_avatar = ? WHERE id = ? AND group_id = ? AND session_type = ?`,
		avatar, memberID, groupID, groupMemberSessionType,
	)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, nil
}

// ListGroupMembers returns every member row of a group, including removed
// (archived) ones so the timeline can still label their past speech. Active
// members are those with Left == false.
//
// Order is JOIN order (created_at, then rowid): free mode speaks every active
// member in roster order when the user @s nobody, so the order must be stable
// and match the order members were added. created_at has second granularity, so
// members added in one transaction tie; rowid (insertion order) breaks the tie
// deterministically, whereas `id` (a random UUID) would not.
func ListGroupMembers(groupID string) ([]GroupMember, error) {
	rows, err := store.ReadDB().Query(
		`SELECT id, agent_id, agent_name, backend, agent_avatar, archived FROM chat_sessions
		 WHERE group_id = ? AND session_type = ?
		 ORDER BY created_at ASC, rowid ASC`,
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
		if err := rows.Scan(&m.ID, &m.AgentID, &m.Name, &m.Backend, &m.Avatar, &archived); err != nil {
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
		SELECT m.group_id, m.id, m.agent_id, m.agent_name, m.backend, m.agent_avatar,
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
		if err := rows.Scan(&groupID, &p.ID, &p.AgentID, &p.Name, &p.Backend, &p.Avatar, &isHost); err != nil {
			return nil, err
		}
		p.IsHost = isHost != 0
		out[groupID] = append(out[groupID], p)
	}
	return out, rows.Err()
}

// GroupMemberSpeakersForGroups returns each group's member-row-id → display-name
// map, INCLUDING removed (archived) members. It is the speaker-resolution
// companion to GroupMembersForGroups (which is active-only, for the avatar
// stack): a message's chat_history.agent_id may name a member who has since
// left, and their past speech must still resolve to their own name rather than
// a generic "Assistant" (mirrors ListGroupMembers' include-left rule).
//
// One query for all ids. Groups with no members are absent from the map; empty
// input returns an empty map without touching the DB.
func GroupMemberSpeakersForGroups(groupIDs []string) (map[string]map[string]string, error) {
	out := map[string]map[string]string{}
	if len(groupIDs) == 0 {
		return out, nil
	}

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

	// No archived filter: left members are included on purpose (their speech is
	// kept in the timeline). The name is the member row's agent_name.
	query := fmt.Sprintf(
		`SELECT group_id, id, agent_name FROM chat_sessions
		 WHERE group_id IN (%s) AND session_type = ?`, placeholders)
	args = append(args, groupMemberSessionType)

	rows, err := store.ReadDB().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var groupID, memberID, name string
		if err := rows.Scan(&groupID, &memberID, &name); err != nil {
			return nil, err
		}
		if out[groupID] == nil {
			out[groupID] = map[string]string{}
		}
		out[groupID][memberID] = name
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

// ErrFreeGroupNeedsTwoMembers is returned when a free-mode group (no host) is
// created with fewer than two members (design §13.1 #F10).
var ErrFreeGroupNeedsTwoMembers = errors.New("free group needs at least two members")

// MaxGroupMembers caps a group's ACTIVE members (decision #78). Exported so the
// handler can report the limit to the user. The count is INSTANCES, not unique
// agents: the same agent may be added several times, and each instance is a
// separate member (and a separate ACP subprocess).
//
// Why a cap at all: every member is a separate ACP subprocess carrying its own
// node/npx runtime (hundreds of MB), and the ACP connection pool has no upper
// bound (its conns map is unbounded; minAliveConns is a floor, not a ceiling).
// A group turn touches every member, so an uncapped roster can spawn dozens of
// agents at once. Twelve is also past the point where a "discussion" stops being
// useful to a model.
const MaxGroupMembers = 12

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
	res, err := store.WriteExec(
		"UPDATE chat_sessions SET archived = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND group_id = ? AND archived = 0",
		memberID, groupID,
	)
	if err != nil {
		return err
	}
	// Record the departure on the timeline (decision #40) ONLY when a row
	// actually changed. Without this check a repeated removal (already archived)
	// or a member id belonging to a DIFFERENT group would still append a
	// "X 已离场" row — polluting the minutes with an event about someone who did
	// not leave THIS group, and feeding it into every member's cursor/context.
	// The UPDATE is already scoped by group_id, so RowsAffected == 0 covers both.
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return nil
	}
	// Best-effort: the member is already archived, so a write failure must not
	// surface as a failed removal.
	if name := memberDisplayName(memberID); name != "" {
		// Drop any private note awaiting this member: it can never be delivered
		// now (left members are excluded from name resolution), so keeping it
		// would be silent loss plus unbounded growth of group_pending_bcc.
		deletePendingBccForTarget(groupID, name)
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
	_ = store.ReadDB().QueryRow("SELECT COALESCE(agent_name, '') FROM chat_sessions WHERE id = ?", memberID).Scan(&name)
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

// GetGroupMode returns the group's mode ("host" or "free"), defaulting to
// GroupModeHost when the field is missing/invalid. The default matters for
// robustness: a group row whose context_state predates the mode field (or was
// corrupted) is treated as host mode, which fails loudly in the orchestrator
// ("no host member") rather than silently behaving as a free group.
//
// The key is `group_mode`, NOT `mode`: ContextState already uses `$.mode` for
// the persisted agent mode (ModeStatePersist), so sharing the key made
// GetContextState fail to unmarshal a group row ("cannot unmarshal string into
// ... ModeStatePersist") — a real collision caught by the free-mode E2E.
func GetGroupMode(groupID string) string {
	var mode string
	_ = store.ReadDB().QueryRow(
		"SELECT COALESCE(json_extract(context_state, '$.group_mode'), '') FROM chat_sessions WHERE id = ?",
		groupID,
	).Scan(&mode)
	if mode == GroupModeFree {
		return GroupModeFree
	}
	return GroupModeHost
}

// IsFreeGroup reports whether sessionID is a free-chat-mode group.
func IsFreeGroup(sessionID string) bool {
	return IsGroupSession(sessionID) && GetGroupMode(sessionID) == GroupModeFree
}

// SetGroupAutoApprove applies the auto-approve flag to EVERY member row of a
// group (decision #61).
//
// Why not the group row alone: the group row owns no ACP connection — each
// member owns one, keyed by its member row id (group_orchestrator.go builds the
// turn with SessionID = the member row id), and the ACP permission handler
// reads getSessionAutoApprove(req.SessionID), i.e. the MEMBER row. Writing only
// the group row therefore made auto-approve a dead switch: members still popped
// permission cards.
//
// Best-effort per member (the write is idempotent); the first error is returned
// after attempting all members so one bad row cannot leave the roster split.
// The group row itself is also written so the toggle round-trips through the
// group session's own GET/PATCH.
func SetGroupAutoApprove(groupID string, enabled bool) error {
	val := 0
	if enabled {
		val = 1
	}
	members, err := ListGroupMembers(groupID)
	if err != nil {
		return err
	}
	var firstErr error
	for _, m := range members {
		if _, err := store.WriteExec("UPDATE chat_sessions SET auto_approve = ? WHERE id = ?", val, m.ID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	// The group row is not a member; keep it in sync too so the group session's
	// own auto_approve column reflects the roster.
	if _, err := store.WriteExec("UPDATE chat_sessions SET auto_approve = ? WHERE id = ?", val, groupID); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
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

// GetGroupMaxSpeeches returns the configured cap on how many times the agents
// may speak to each other in a group turn before the discussion is forced to
// stop and the floor returns to the human. It is a GLOBAL setting
// (chat.group_max_speeches), not per group — the same cap applies to host and
// free mode alike, and it counts MEMBER speeches only (the host's routing /
// summary turns do not count).
//
// A non-positive runtime value falls back to the default (ApplyDefaults clamps
// the config, but a value set directly on the global var in a test/edge path
// must not silently mean "stop immediately").
func GetGroupMaxSpeeches() int {
	if model.ChatGroupMaxSpeeches > 0 {
		return model.ChatGroupMaxSpeeches
	}
	return defaultGroupMaxSpeeches
}

// GetGroupParallelDefault returns whether the user's free-mode seed runs its
// members CONCURRENTLY by default (the action-bar "并发执行" switch).
//
// Defaults to false when unset/invalid: sequential is the long-standing
// behavior, so an old group (or one whose context_state was corrupted) keeps it.
// The key is `parallelDefault` (not `parallel`): it is a DEFAULT applied to the
// user's seed, not a per-message mode, and it must not collide with the
// per-tag `mode="parallel"` attribute an agent may write.
func GetGroupParallelDefault(groupID string) bool {
	var raw string
	_ = store.ReadDB().QueryRow(
		"SELECT COALESCE(json_extract(context_state, '$.parallelDefault'), '') FROM chat_sessions WHERE id = ?",
		groupID,
	).Scan(&raw)
	// SQLite's json_extract renders JSON true as 1 and false as 0; anything
	// else (empty/absent/invalid) is the sequential default.
	return raw == "1" || raw == "true"
}

// SetGroupParallelDefault stores the group's user-seed concurrency default in
// its context_state JSON. Honored in free mode only (host mode has no user
// routing — the host routes), but stored unconditionally so the switch
// round-trips even if the group's mode ever changed.
func SetGroupParallelDefault(groupID string, enabled bool) error {
	PatchContextStateMerge(groupID, map[string]string{"parallelDefault": fmt.Sprintf("%t", enabled)})
	return nil
}

// GetAgentDisplayName returns an agent's display name (falling back to its id,
// then "AI"), used as the base for a member row's agent_name and the group's
// placeholder title.
func GetAgentDisplayName(agentID string) string {
	if a := model.GetAgent(agentID); a != nil && a.Name != "" {
		return a.Name
	}
	if agentID != "" {
		return agentID
	}
	return "AI"
}

// GetSessionType returns a session's session_type ("" if not found or the DB is
// not ready). The readiness guard keeps callers that run before/without a DB
// (unit tests, early startup) from panicking on a nil reader; "" is the safe
// default — it is not a group, so no group-chat policy is applied.
func GetSessionType(sessionID string) string {
	if !store.ReadDBReady() {
		return ""
	}
	var t string
	_ = store.ReadDB().QueryRow("SELECT session_type FROM chat_sessions WHERE id = ?", sessionID).Scan(&t)
	return t
}

// IsGroupSession reports whether sessionID is a group chat timeline row. It is
// the exported form of `GetSessionType(id) == groupSessionType`, so callers
// outside this package (the HTTP handler) do not have to spell the literal
// "group" and risk drifting from the canonical constant.
func IsGroupSession(sessionID string) bool {
	return GetSessionType(sessionID) == groupSessionType
}

// IsGroupSessionType reports whether a stored session_type value is a group
// timeline row. For callers that already hold the stored string (e.g. a session
// list row) and would otherwise compare it to a literal.
func IsGroupSessionType(storedType string) bool {
	return storedType == groupSessionType
}

// IsGroupMemberSession reports whether sessionID is a hidden group MEMBER row
// (as opposed to a group timeline row or an ordinary chat). Exported so callers
// outside this package can guard operations that make no sense on a member row
// (e.g. renaming a session) without spelling the literal.
func IsGroupMemberSession(sessionID string) bool {
	return GetSessionType(sessionID) == groupMemberSessionType
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
