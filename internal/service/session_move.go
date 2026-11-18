package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"clawbench/internal/model"
	"clawbench/internal/rag"
	"clawbench/internal/store"
)

// MoveUnavailableMembersError reports a group whose members could not be used
// in the target project. Their agents are either project-scoped to the source
// project or no longer exist, so the group would arrive with members the new
// project cannot see or re-add.
type MoveUnavailableMembersError struct {
	// Names are the member display names (agent id as a fallback).
	Names []string
}

func (e *MoveUnavailableMembersError) Error() string {
	return "group members unavailable in target project: " + strings.Join(e.Names, ", ")
}

// MoveAgentUnavailableError reports a single conversation whose agent is
// project-scoped to the source project (or no longer exists in the target).
// The move is refused: the next turn would be rejected with
// AgentNotInProject, leaving a conversation that can never speak again.
type MoveAgentUnavailableError struct {
	// Name is the agent's display name (id as a fallback).
	Name string
}

func (e *MoveAgentUnavailableError) Error() string {
	return "agent unavailable in target project: " + e.Name
}

// MoveSessionToProject re-binds a conversation to another project (issue #685)
// without touching its history. It reports false when the session may not move
// (a group member row, or a task execution); re-binding to the project it is
// already in is a successful no-op.
//
// Both kinds of user-visible conversation can move:
//
//   - a single-agent conversation ('chat') moves on its own;
//   - a group chat moves as a WHOLE: its timeline row and every member row
//     (including disabled and removed ones, whose old speaker ids the timeline
//     still references) move together. Moving only the group row would leave
//     the members under the old project, where deleting that project would take
//     them with it and break speech attribution.
//
// A task execution ('scheduled') belongs to the task that produced it and is
// refused rather than half-moved. A hidden group-member row is never a move
// target of its own — it follows its group.
//
// The projects row itself is NOT created here: targetProjectID belongs to a
// project the caller verified exists, so a stale path never conjures a new
// project entry.
//
// The message rows must move with the session: history queries filter on BOTH
// project_id and session_id, so a session whose messages stay under the old
// project renders as an empty conversation. The usage ledger follows the
// messages for the same reason (a row attributed to the old project would show
// the conversation's cost on the wrong project's stats page). Project-scoped
// tags are dropped: a tag BELONGS to a project, so the old project's definition
// cannot be shown in the new one. Global tags are project-independent and stay.
// sort_order restarts at the top of the target list, since drag order is per
// project.
//
// The agent's NATIVE session is re-created, not resumed. An ACP/CLI session was
// created for the old working directory and its transcript is looked up per
// project directory, so resuming it under the new one either fails or keeps the
// old context. The move therefore clears external_session_id and stores the
// timeline as pending context:
//
//   - single chat: flag it as a fork background, which the next turn injects
//     with the existing fork-context machinery (budgeted, cleared after the
//     first successful turn);
//   - group: reset each member's seen_cursor to 0, which is exactly what a
//     member joining an existing group gets — its next speech carries the whole
//     timeline as injected context.
//
// The caller closes the cached agent connections afterwards so the next turn
// spawns them under the new directory.
func MoveSessionToProject(sessionID, targetProjectPath string, targetProjectID int64) (bool, error) {
	if targetProjectID == store.GlobalScopeProjectID {
		return false, fmt.Errorf("move target must be a real project")
	}
	sessionType, currentProjectID, err := sessionTypeAndProject(sessionID)
	if err != nil {
		return false, err
	}
	if sessionType != store.SessionTypeChat && sessionType != store.SessionTypeGroup {
		return false, nil
	}
	if currentProjectID == targetProjectID {
		// Already there: nothing to write. Reported as moved so a no-op is not
		// mistaken for "this session cannot move" by the caller.
		return true, nil
	}

	sortOrder, err := nextSortOrderInProject(targetProjectID)
	if err != nil {
		return false, err
	}

	if sessionType == store.SessionTypeGroup {
		return moveGroupToProject(sessionID, targetProjectID, sortOrder)
	}
	return moveChatToProject(sessionID, targetProjectID, sortOrder)
}

// chatAgentUnavailable reports the agent a single conversation uses when that
// agent cannot be used in the target project (empty otherwise), following the
// same rule as unavailableMembers: a global agent is usable everywhere, a
// project-scoped one only in its own project, and an agent that no longer
// exists does not block the move (the next turn falls back to the default).
func chatAgentUnavailable(sessionID string, targetProjectID int64) (string, error) {
	var agentID string
	if err := store.ReadDB().QueryRow(
		"SELECT COALESCE(agent_id, '') FROM chat_sessions WHERE id = ?", sessionID,
	).Scan(&agentID); err != nil {
		return "", fmt.Errorf("load session agent for move: %w", err)
	}
	agent := model.GetAgent(agentID)
	if agent == nil || agent.ProjectID == nil || *agent.ProjectID == targetProjectID {
		return "", nil
	}
	if agent.Name != "" {
		return agent.Name, nil
	}
	return agent.ID, nil
}

// moveChatToProject moves one single-agent conversation.
func moveChatToProject(sessionID string, targetProjectID int64, sortOrder int) (bool, error) {
	// Refuse BEFORE anything is written: a project-scoped agent would be
	// invisible in the target project, so the conversation could never send
	// another message there.
	if name, err := chatAgentUnavailable(sessionID, targetProjectID); err != nil {
		return false, err
	} else if name != "" {
		return false, &MoveAgentUnavailableError{Name: name}
	}

	// Render the history BEFORE the transaction: it reads the messages this
	// move is about to re-attribute, and the value is a plain string copy.
	forkContext := BuildForkContext(sessionID)

	if err := moveInTransaction(func(tx *sql.Tx) error {
		stateExpr := `CASE WHEN context_state = '' OR context_state IS NULL THEN '{}' ELSE context_state END`
		if forkContext == "" {
			if _, err := tx.ExecContext(context.Background(),
				"UPDATE chat_sessions SET project_id = ?, sort_order = ?, external_session_id = '' WHERE id = ?",
				targetProjectID, sortOrder, sessionID,
			); err != nil {
				return fmt.Errorf("move session: %w", err)
			}
		} else {
			payload, err := json.Marshal(forkContext)
			if err != nil {
				return fmt.Errorf("encode moved session history: %w", err)
			}
			if _, err := tx.ExecContext(context.Background(),
				`UPDATE chat_sessions
				    SET project_id = ?, sort_order = ?, external_session_id = '',
				        context_state = json_set(`+stateExpr+`, '$.fork_context', json(?))
				  WHERE id = ?`,
				targetProjectID, sortOrder, string(payload), sessionID,
			); err != nil {
				return fmt.Errorf("move session: %w", err)
			}
		}
		if err := moveSessionRowsTx(tx, []string{sessionID}, targetProjectID); err != nil {
			return err
		}
		return clearProjectTagLinks(tx, sessionID)
	}); err != nil {
		return false, err
	}

	reattributeMovedRAG(sessionID, targetProjectID)
	return true, nil
}

// moveGroupToProject moves a group timeline together with every member row.
func moveGroupToProject(groupID string, targetProjectID int64, sortOrder int) (bool, error) {
	members, err := ListGroupMembersIncludingRemoved(groupID)
	if err != nil {
		return false, fmt.Errorf("load group members for move: %w", err)
	}
	if unavailable := unavailableMembers(members, targetProjectID); len(unavailable) > 0 {
		return false, &MoveUnavailableMembersError{Names: unavailable}
	}

	if err := moveInTransaction(func(tx *sql.Tx) error {
		// The group row itself carries no agent session, but its identity fields
		// mirror the host; clear the native id so no stale resume is attempted.
		// A pending fork background is dropped too: after the move every member
		// re-reads the real timeline (cursors reset below), so keeping the old
		// fork text would inject the same history twice.
		if _, err := tx.ExecContext(context.Background(),
			`UPDATE chat_sessions
			    SET project_id = ?, sort_order = ?, external_session_id = '',
			        context_state = json_remove(
			            CASE WHEN context_state = '' OR context_state IS NULL THEN '{}' ELSE context_state END,
			            '$.fork_context')
			  WHERE id = ?`,
			targetProjectID, sortOrder, groupID,
		); err != nil {
			return fmt.Errorf("move group session: %w", err)
		}
		// Members: re-file every row (removed ones included — chat_history.agent_id
		// points at them) and reset the visible ones to "nothing seen yet", so
		// their next speech receives the full timeline just like a member who
		// joined an existing group. Disabled members reset too: enabling them
		// later must not start them with no memory of the group.
		if _, err := tx.ExecContext(context.Background(),
			`UPDATE chat_sessions
			    SET project_id = ?, external_session_id = '',
			        context_state = CASE
			            WHEN removed = 0 THEN json_set(
			                CASE WHEN context_state = '' OR context_state IS NULL THEN '{}' ELSE context_state END,
			                '$.seen_cursor', 0)
			            ELSE context_state
			        END
			  WHERE group_id = ? AND session_type = ?`,
			targetProjectID, groupID, store.SessionTypeGroupMember,
		); err != nil {
			return fmt.Errorf("move group members: %w", err)
		}
		if err := moveSessionRowsTx(tx, []string{groupID}, targetProjectID); err != nil {
			return err
		}
		return clearProjectTagLinks(tx, groupID)
	}); err != nil {
		return false, err
	}

	reattributeMovedRAG(groupID, targetProjectID)
	return true, nil
}

// unavailableMembers returns the display names of members whose agent cannot be
// used in the target project. Removed members are skipped: their rows exist only
// for historical speech attribution and will never speak again.
//
// Agent visibility mirrors agentVisibleInProject: a global agent is usable
// everywhere, a project-scoped one only in its own project. Checking here is
// not cosmetic — the new project's pickers would not list these agents, so the
// group could never re-add or replace them.
func unavailableMembers(members []GroupMember, targetProjectID int64) []string {
	var names []string
	for _, m := range members {
		if m.Removed {
			continue
		}
		agent := model.GetAgent(m.AgentID)
		if agent != nil && (agent.ProjectID == nil || *agent.ProjectID == targetProjectID) {
			continue
		}
		name := m.Name
		if name == "" {
			name = m.AgentID
		}
		names = append(names, name)
	}
	return names
}

// moveInTransaction runs fn inside the process-wide write transaction. The
// transaction body is a closure so the write lock is released before the
// post-commit RAG re-attribution — the RAG store takes the same lock on its own
// database handle, and holding it across that call deadlocks.
func moveInTransaction(fn func(tx *sql.Tx) error) error {
	tx, err := store.WriteBegin()
	if err != nil {
		return err
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// moveSessionRowsTx moves the history and every row that denormalizes the
// project id for the given session rows (a single chat, or a group timeline).
func moveSessionRowsTx(tx *sql.Tx, sessionIDs []string, targetProjectID int64) error {
	for _, sessionID := range sessionIDs {
		// EVERY row, including a still-streaming one: the caller refuses to move a
		// running session, but a reply finalized in a concurrent request just
		// before must not be stranded under the old project.
		if _, err := tx.ExecContext(context.Background(),
			"UPDATE chat_history SET project_id = ? WHERE session_id = ?",
			targetProjectID, sessionID,
		); err != nil {
			return fmt.Errorf("move session history: %w", err)
		}
		// The usage ledger denormalizes its own project attribution at write time.
		// Re-derive it from the (already moved) messages instead of touching it
		// here: the ledger row also carries a per-message wall-clock created_at the
		// migration code considers; rewriting through the same subquery keeps the
		// two consistent. Rows without a surviving message keep 0 (unattributable).
		if _, err := tx.ExecContext(context.Background(),
			`UPDATE chat_metadata
			    SET project_id = COALESCE((SELECT h.project_id FROM chat_history h WHERE h.id = chat_metadata.message_id), 0)
			  WHERE message_id IN (SELECT id FROM chat_history WHERE session_id = ?)`,
			sessionID,
		); err != nil {
			return fmt.Errorf("move session usage ledger: %w", err)
		}
		// The remaining rows that denormalize the project id. A queued message is
		// materialized later with the id it captured at ENQUEUE time, so leaving it
		// behind would strand that message under the old project (invisible in the
		// moved conversation). /btw records render their stored project path, and
		// recommendations are written with the project that produced them.
		for _, table := range []string{tableQueuedMessages, tableBtwQuestions, tableChatRecommendations} {
			if _, err := tx.ExecContext(context.Background(),
				"UPDATE "+table+" SET project_id = ? WHERE session_id = ?",
				targetProjectID, sessionID,
			); err != nil {
				return fmt.Errorf("move session %s: %w", table, err)
			}
		}
	}
	return nil
}

// clearProjectTagLinks drops the moved session's links to PROJECT-scoped tags.
//
// The new project has no definition for them, and session rows do not
// visibility-filter their linked tags, so leaving the links would paint the row
// with a label the new project cannot list or filter by. Global tags are
// project-independent and stay attached.
func clearProjectTagLinks(tx *sql.Tx, sessionID string) error {
	if _, err := tx.ExecContext(context.Background(),
		`DELETE FROM session_tag_links
		  WHERE session_id = ?
		    AND tag_id IN (SELECT id FROM session_tags WHERE scope <> 'global')`,
		sessionID,
	); err != nil {
		return fmt.Errorf("move session project tags: %w", err)
	}
	return nil
}

// reattributeMovedRAG repoints the moved conversation's search index after the
// transaction committed.
//
// Best-effort: the RAG index is written by its own database handle under the
// same process-wide write lock, so touching it inside the transaction would
// deadlock. Leaving chunks on the old project would show the moved conversation
// in the OLD project's search results and hide it from the new one's; a failure
// here is logged rather than reported as a failed move — the conversation has
// already moved, and a retry would find nothing to do.
func reattributeMovedRAG(sessionID string, targetProjectID int64) {
	if err := rag.ReattributeSessionProject(sessionID, targetProjectID); err != nil {
		slog.Warn("failed to move RAG chunks for moved session",
			slog.String("session_id", sessionID),
			slog.Int64("project_id", targetProjectID),
			slog.String("err", err.Error()))
	}
}

// sessionTypeAndProject returns a session's stored type and owning project id.
func sessionTypeAndProject(sessionID string) (string, int64, error) {
	var sessionType string
	var projectID int64
	err := store.ReadDB().QueryRow(
		"SELECT COALESCE(session_type, ?), project_id FROM chat_sessions WHERE id = ?",
		store.SessionTypeChat, sessionID,
	).Scan(&sessionType, &projectID)
	if err != nil {
		return "", 0, fmt.Errorf("load session for move: %w", err)
	}
	return sessionType, projectID, nil
}

// nextSortOrderInProject returns the position a moved conversation takes in
// the target project: above every existing row (drag order is ascending).
func nextSortOrderInProject(projectID int64) (int, error) {
	var minOrder sql.NullInt64
	if err := store.ReadDB().QueryRow(
		"SELECT MIN(sort_order) FROM chat_sessions WHERE project_id = ?", projectID,
	).Scan(&minOrder); err != nil {
		return 0, fmt.Errorf("load target sort order: %w", err)
	}
	return int(minOrder.Int64) - 1, nil
}

// IsSessionRunning reports whether a runner is registered for the session. A
// seam (var) so handler tests can exercise the move guards without spawning a
// real turn.
var IsSessionRunning = isRunnerRegistered

// SessionHasStreamingMessage reports whether the conversation has a message row
// still being written. Moving such a session would re-file a half-written reply
// from the old project, since the writing turn captured the project id at its
// start.
func SessionHasStreamingMessage(sessionID string) (bool, error) {
	var count int
	err := store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND streaming = 1", sessionID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check streaming message: %w", err)
	}
	return count > 0, nil
}
