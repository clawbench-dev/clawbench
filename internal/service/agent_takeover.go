package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"clawbench/internal/i18n"
	"clawbench/internal/model"
	"clawbench/internal/store"
)

// ErrAgentHasRunningSessions is returned when an agent is deleted while one of
// its conversations (or one of its group turns) is still running. Everything
// else is allowed: the sessions are handed to the replacement agent instead of
// blocking the deletion, which is what issue #606 asked for.
var ErrAgentHasRunningSessions = errors.New("agent has running sessions")

// AgentTakeoverResult reports what a deletion moved to the replacement agent.
// The counts are shown in the success toast so the user knows the deletion was
// not a no-op for their conversations.
type AgentTakeoverResult struct {
	Sessions int `json:"sessions"`
	Tasks    int `json:"tasks"`
	Groups   int `json:"groups"`

	// notices are the system rows to write AFTER the transaction commits. They
	// cannot be written inside it: AddSystemMessage takes the same process-wide
	// write lock the transaction holds.
	notices []takeoverNotice
}

// takeoverNotice is one "this conversation changed hands" system row.
//
// GroupMember marks a membership, whose timeline is the GROUP row. It is an
// explicit flag rather than "MemberName != ”": agent_name is NOT NULL DEFAULT
// ”, so a member row with an empty display name would otherwise be announced on
// the group timeline with the 1:1 wording.
type takeoverNotice struct {
	ProjectPath string
	SessionID   string
	MemberName  string
	GroupMember bool
}

// rowQueryer is the narrow slice of *sql.DB / *sql.Tx this file needs. The
// running-session check runs BOTH before the transaction (handler guard) and
// inside it (re-check under the write lock); inside, it must go through the tx —
// reading through store.ReadDB() while the write connection is held deadlocks on
// a single-connection database (the :memory: test fixture, and any deployment
// with a one-connection read pool).
type rowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// AgentHasRunningSessions reports whether any conversation of agentID is running
// right now, including a group turn in which the agent is a speaking member.
//
// The two sources are disjoint: GetRunningSessionIDs carries 1:1 sessions and
// GROUP row ids (whose agent_id is the host), while a plain member exists only
// as a hidden group_member row that the group orchestrator tracks in
// groupActiveMembers. Checking just one of them would let a deletion slip
// through mid-turn for the other kind.
func AgentHasRunningSessions(agentID string) (bool, error) {
	if agentID == "" {
		return false, nil
	}
	return anySessionOfAgentRunning(context.Background(), store.ReadDB(), agentID, runningSessionIDsIncludingGroupMembers())
}

// runningSessionIDsIncludingGroupMembers snapshots both sources of "busy": the
// runner registry (1:1 sessions and group ROWS) and the orchestrator's active
// member rows.
func runningSessionIDsIncludingGroupMembers() []string {
	running := GetRunningSessionIDs()

	groupActiveMembersMu.RLock()
	for memberID := range groupActiveMembers {
		running = append(running, memberID)
	}
	groupActiveMembersMu.RUnlock()
	return running
}

// anySessionOfAgentRunning answers the same question for a caller that already
// holds the running-id snapshot and a queryer (the deletion transaction
// re-checks under the write lock and must not take the group map twice).
func anySessionOfAgentRunning(ctx context.Context, q rowQueryer, agentID string, running []string) (bool, error) {
	if len(running) == 0 {
		return false, nil
	}
	placeholders := ""
	args := make([]any, 0, len(running)+1)
	args = append(args, agentID)
	for i, id := range running {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	var count int
	err := q.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM chat_sessions WHERE agent_id = ? AND id IN ("+placeholders+")",
		args...,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// DeleteAgentWithTakeover deletes agentID and hands every conversation it owned
// to replacement, in ONE transaction.
//
// Why the sessions are rewritten instead of left dangling: chat_sessions has no
// foreign key to agents, so a plain DELETE would leave rows whose agent_id
// matches nothing. Such a session still opens (the history endpoint never reads
// the agent) but cannot run a turn — resolveAgentConfig returns an empty system
// prompt and command — and the session list loses its agent name entirely.
//
// What a takeover rewrites, and why each column matters:
//
//	agent_id / backend     the turn runs the replacement's CLI/ACP backend;
//	                       keeping the old backend would spawn the old tool.
//	agent_source           'default' — nobody picked this agent for this row.
//	model                  model ids are per-backend; the old id would be
//	                       rejected (or silently mis-resolved) by the new one.
//	transport              a session-level CLI/ACP override describes the OLD
//	                       agent; the replacement's own transport must win.
//	external_session_id    the native session belongs to a process that is
//	                       gone. Clearing it makes the next turn rebuild the
//	                       context from chat_history (see BuildChatRequest).
//	context_state          mode / thinkingEffort are the old agent's capability
//	                       sets (replaying them is a -32602), and usage is the
//	                       old context window's token count.
//	agent_takeover_from    the display name of the deleted agent. Doubles as the
//	                       turn-path switch: while the replacement has not yet
//	                       captured its own session id, the next turn must start
//	                       a FRESH native session instead of resuming.
//
// Group rows are handled separately (see recomputeGroupIdentityTx): a group row
// mirrors its host (host mode) or its first member (free mode), so rebinding a
// member row would otherwise leave the group naming a deleted agent.
//
// Guarantee boundary: "no conversation is running" is enforced at check time
// (twice — handler, then again under the write lock), not at commit time. A
// session CREATED between the re-check and the commit keeps the deleted agent id
// and will fail its first turn (resolveAgentConfig returns an empty system prompt
// and command). Session creation does not take configMutex, so this window
// cannot be closed here without serialising every new session behind it; it is
// milliseconds wide and the failure is a clear error rather than silent damage.
func DeleteAgentWithTakeover(old *model.Agent, replacement *model.Agent) (AgentTakeoverResult, error) {
	var res AgentTakeoverResult
	if old == nil || old.ID == "" {
		return res, fmt.Errorf("agent to delete is missing")
	}
	if replacement == nil || replacement.ID == "" {
		return res, fmt.Errorf("takeover target agent is missing")
	}

	reuseNative := canReuseNativeSession(old, replacement)
	res, err := deleteAgentTakeoverTx(old, replacement, reuseNative)
	if err != nil {
		return res, err
	}

	// AFTER the commit and after the write lock is released: each notice takes
	// the write lock itself. Best-effort — a failed notice must not report the
	// deletion as failed, since the rows are already gone.
	writeTakeoverNotices(res.notices, old.Name, replacement.Name, reuseNative)
	res.notices = nil
	return res, nil
}

// canReuseNativeSession reports whether the replacement can simply RESUME the
// deleted agent's native session instead of rebuilding the context from
// chat_history.
//
// The native session id only means something to the process that issued it, so
// "same AI backend" is the necessary condition and the command/transport are the
// sufficient ones:
//
//   - different backend        → different CLI/adapter, different session store.
//   - same backend, CLI        → same binary, same store; the id is still valid.
//   - same backend, ACP        → the session lives inside whatever process the
//     ACP command starts, so two custom ACP agents that both call themselves
//     "custom-acp" do NOT share it. The command must match as well.
//   - different transport      → a CLI session id handed to an ACP adapter (or
//     the reverse) is a namespace mismatch; the pool would fail the resume.
//
// Reusing keeps the conversation's real context (no 100k-char re-injection, no
// loss of tool history). What it does NOT carry over is the agent's own system
// prompt on backends that only inject it on a fresh session — the takeover sets
// `compacted=1` so the next turn re-announces it once (see ShouldInjectSystemPrompt).
func canReuseNativeSession(old, replacement *model.Agent) bool {
	if old == nil || replacement == nil {
		return false
	}
	if old.Backend == "" || old.Backend != replacement.Backend {
		return false
	}
	if old.AcpCommand != replacement.AcpCommand {
		return false
	}
	return old.Transport == replacement.Transport
}

// deleteAgentTakeoverTx performs the whole rewrite under one transaction and
// returns the notices to write afterwards.
func deleteAgentTakeoverTx(old *model.Agent, replacement *model.Agent, reuseNative bool) (AgentTakeoverResult, error) {
	agentID := old.ID
	oldName := old.Name
	var res AgentTakeoverResult

	tx, err := store.WriteBegin()
	if err != nil {
		return res, err
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()

	ctx := context.Background()

	// Re-check under the write lock: between the handler's check and this lock a
	// turn could have started, and rewriting a running session's identity would
	// strand that executor on a half-swapped row. Queried through the tx — a
	// store.ReadDB() call here would wait forever for the connection this
	// transaction is holding.
	hasRunning, err := anySessionOfAgentRunning(ctx, tx, agentID, runningSessionIDsIncludingGroupMembers())
	if err != nil {
		return res, fmt.Errorf("check running sessions of agent %s: %w", agentID, err)
	}
	if hasRunning {
		return res, ErrAgentHasRunningSessions
	}

	notices, err := collectTakeoverNoticesTx(ctx, tx, agentID)
	if err != nil {
		return res, err
	}
	res.notices = notices

	// Groups whose roster contains this agent, captured BEFORE the rewrite so the
	// group rows can be re-identified from their (already rewritten) member rows.
	groupIDs, err := affectedGroupIDsTx(ctx, tx, agentID)
	if err != nil {
		return res, err
	}

	// Counted BEFORE the rewrite, and with the SAME definition GetAgentUsage uses
	// (1:1 chats including archived, plus active group memberships). The blanket
	// UPDATE below also touches group timeline rows, archived member rows and
	// scheduled execution sessions; reporting RowsAffected would make the success
	// toast print a different number than the confirm dialog the user just read.
	var sessionCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM chat_sessions WHERE agent_id = ? AND session_type = ?) +
			(SELECT COUNT(*) FROM chat_sessions WHERE agent_id = ? AND session_type = ? AND archived = 0)`,
		agentID, store.SessionTypeChat, agentID, store.SessionTypeGroupMember,
	).Scan(&sessionCount); err != nil {
		return res, fmt.Errorf("count sessions of agent %s: %w", agentID, err)
	}
	res.Sessions = sessionCount

	// external_session_id is kept when the replacement speaks the SAME backend:
	// the id still names a real session in that CLI/adapter, so the next turn can
	// simply resume it — no context rebuild, no re-injection. Otherwise it is
	// cleared and the turn path rebuilds from chat_history.
	//
	// compacted=1 is set ONLY on the reuse path: resuming skips the system prompt
	// (ShouldInjectSystemPrompt is false on resume), so the replacement's own
	// instructions would never reach the model on backends that only inject on a
	// fresh session. The flag is consumed by exactly one turn.
	//
	// The `transport = ''` half of both CASEs reads the OLD row value (SQLite
	// evaluates every SET expression against the pre-update row): a session with
	// its own transport override may have established its native session in the
	// OTHER mode (CLI id vs ACP id), which the replacement cannot resume. Only
	// sessions that followed the agent's own transport are safe to keep.
	_, err = tx.ExecContext(ctx, `
		UPDATE chat_sessions SET
			agent_id = ?,
			backend = ?,
			agent_source = 'default',
			model = '',
			transport = '',
			external_session_id = CASE WHEN ? AND transport = '' THEN external_session_id ELSE '' END,
			compacted = CASE WHEN ? AND transport = '' THEN 1 ELSE compacted END,
			agent_takeover_from = ?,
			context_state = CASE
				WHEN context_state IS NULL OR context_state = '' THEN '{}'
				ELSE json_remove(context_state, '$.mode', '$.thinkingEffort', '$.usage')
			END
		WHERE agent_id = ?`,
		replacement.ID, replacement.Backend, reuseNative, reuseNative, oldName, agentID,
	)
	if err != nil {
		return res, fmt.Errorf("reassign sessions of agent %s: %w", agentID, err)
	}

	// A group's row carries its host's (or first member's) identity. Rebinding a
	// member row does not touch it, so it would keep naming the deleted agent and
	// the list would show an empty icon/name for a group that still works.
	for _, groupID := range groupIDs {
		if err := recomputeGroupIdentityTx(ctx, tx, groupID); err != nil {
			return res, err
		}
	}
	res.Groups = len(groupIDs)

	// Tasks are conversations too (each run creates a scheduled session), and an
	// agent_id left dangling makes every future run fail. They move with the
	// sessions rather than being paused: the user asked for a takeover, not for
	// their schedules to silently stop.
	taskRes, err := tx.ExecContext(ctx,
		"UPDATE scheduled_tasks SET agent_id = ?, updated_at = CURRENT_TIMESTAMP WHERE agent_id = ?",
		replacement.ID, agentID,
	)
	if err != nil {
		return res, fmt.Errorf("reassign tasks of agent %s: %w", agentID, err)
	}
	tasks, _ := taskRes.RowsAffected()
	res.Tasks = int(tasks)

	if _, err := tx.ExecContext(ctx, "DELETE FROM agents WHERE id = ?", agentID); err != nil {
		return res, fmt.Errorf("delete agent %s: %w", agentID, err)
	}

	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("commit agent takeover for %s: %w", agentID, err)
	}
	return res, nil
}

// collectTakeoverNoticesTx lists the conversations that deserve a visible
// "changed hands" row: live 1:1 chats and live groups that had this agent as a
// member. Archived rows are skipped because AddSystemMessage refuses them — and
// nobody is looking at an archived timeline anyway.
func collectTakeoverNoticesTx(ctx context.Context, tx *sql.Tx, agentID string) ([]takeoverNotice, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT s.id, s.session_type, COALESCE(s.group_id, ''), COALESCE(s.agent_name, ''),
		       COALESCE(p.path, ''), s.archived
		  FROM chat_sessions s
		  LEFT JOIN projects p ON p.id = s.project_id
		 WHERE s.agent_id = ? AND s.session_type IN (?, ?)`,
		agentID, store.SessionTypeChat, store.SessionTypeGroupMember,
	)
	if err != nil {
		return nil, fmt.Errorf("list takeover notices for agent %s: %w", agentID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []takeoverNotice
	for rows.Next() {
		var (
			sessionID, sessionType, groupID, memberName, projectPath string
			archived                                                 int
		)
		if err := rows.Scan(&sessionID, &sessionType, &groupID, &memberName, &projectPath, &archived); err != nil {
			return nil, fmt.Errorf("scan takeover notice of agent %s: %w", agentID, err)
		}
		if archived != 0 {
			continue
		}
		if sessionType == store.SessionTypeGroupMember {
			if groupID == "" {
				continue
			}
			out = append(out, takeoverNotice{ProjectPath: projectPath, SessionID: groupID, MemberName: memberName, GroupMember: true})
			continue
		}
		out = append(out, takeoverNotice{ProjectPath: projectPath, SessionID: sessionID})
	}
	return out, rows.Err()
}

// affectedGroupIDsTx returns the group rows whose roster contained agentID.
func affectedGroupIDsTx(ctx context.Context, tx *sql.Tx, agentID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT group_id FROM chat_sessions
		 WHERE agent_id = ? AND session_type = ? AND group_id != ''`,
		agentID, store.SessionTypeGroupMember,
	)
	if err != nil {
		return nil, fmt.Errorf("list groups of agent %s: %w", agentID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var groupID string
		if err := rows.Scan(&groupID); err != nil {
			return nil, fmt.Errorf("scan group of agent %s: %w", agentID, err)
		}
		out = append(out, groupID)
	}
	return out, rows.Err()
}

// recomputeGroupIdentityTx re-derives a group row's (backend, agent_id) from its
// member rows, using the same rule as groupIdentityFields: the host in host
// mode, the first member in free mode. A group with no members left is skipped.
func recomputeGroupIdentityTx(ctx context.Context, tx *sql.Tx, groupID string) error {
	var hostMemberID, currentAgent, currentBackend string
	err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(json_extract(context_state, '$.host_member_id'), ''), agent_id, backend
		   FROM chat_sessions WHERE id = ?`, groupID,
	).Scan(&hostMemberID, &currentAgent, &currentBackend)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read group %s identity: %w", groupID, err)
	}

	memberID := hostMemberID
	if memberID == "" {
		// Free mode: the group row mirrors the FIRST member (see
		// groupIdentityFields, which returns specs[0]). "First" is INSERTION order,
		// so the ordering must be rowid — not created_at (SQLite CURRENT_TIMESTAMP
		// has second precision, and a group's members are inserted in one
		// transaction, so every row ties) and not id (a random UUID, which would
		// pick an arbitrary member).
		err = tx.QueryRowContext(ctx,
			`SELECT id FROM chat_sessions
			  WHERE group_id = ? AND session_type = ?
			  ORDER BY rowid ASC LIMIT 1`,
			groupID, store.SessionTypeGroupMember,
		).Scan(&memberID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read first member of group %s: %w", groupID, err)
		}
	}

	var memberAgent, memberBackend string
	err = tx.QueryRowContext(ctx,
		"SELECT agent_id, backend FROM chat_sessions WHERE id = ?", memberID,
	).Scan(&memberAgent, &memberBackend)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read member %s of group %s: %w", memberID, groupID, err)
	}
	if memberAgent == currentAgent && memberBackend == currentBackend {
		return nil
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE chat_sessions SET agent_id = ?, backend = ? WHERE id = ?",
		memberAgent, memberBackend, groupID,
	); err != nil {
		return fmt.Errorf("update group %s identity: %w", groupID, err)
	}
	return nil
}

// writeTakeoverNotices appends the "changed hands" rows to each affected
// timeline. Best-effort by design: the deletion already committed.
func writeTakeoverNotices(notices []takeoverNotice, oldName, newName string, reusedNative bool) {
	if len(notices) == 0 {
		return
	}
	loc := i18n.LocalizerForLocale(model.Language)
	for _, n := range notices {
		if n.SessionID == "" {
			continue
		}
		if n.GroupMember {
			text := i18n.T(loc, "AgentTakeoverGroupNotice", map[string]any{
				"Member": n.MemberName,
				"New":    newName,
			})
			writeGroupSystemMessage(n.ProjectPath, n.SessionID, text)
			continue
		}
		// Two wordings: a rebuilt context really was re-injected, a resumed one
		// was not — telling the user otherwise would send them looking for a
		// re-injection that never happened.
		key := "AgentTakeoverSessionNotice"
		if reusedNative {
			key = "AgentTakeoverSessionNoticeResumed"
		}
		text := i18n.T(loc, key, map[string]any{
			"Old": oldName,
			"New": newName,
		})
		writeSessionSystemMessage(n.ProjectPath, n.SessionID, text)
	}
}

// writeSessionSystemMessage is the single-chat counterpart of
// writeGroupSystemMessage: persist the row, then broadcast it so open clients
// render it without reloading history.
func writeSessionSystemMessage(projectPath, sessionID, text string) {
	msgID, err := AddSystemMessage(projectPath, sessionID, text)
	if err != nil {
		slog.Warn("agent takeover: writing system message failed", "session", sessionID, "err", err)
		return
	}
	emitGroupSystemMessage(sessionID, msgID, text)
}
