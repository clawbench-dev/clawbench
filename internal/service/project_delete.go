package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"clawbench/internal/rag"
	"clawbench/internal/store"
)

// Sentinel errors returned by DeleteProjectData so the handler can map them to
// distinct HTTP statuses without string matching.
var (
	// ErrProjectNotFound: no project row has the given id.
	ErrProjectNotFound = errors.New("project not found")
	// ErrProjectDeleteForbidden: the id is the reserved global-scope sentinel.
	ErrProjectDeleteForbidden = errors.New("project cannot be deleted")
	// ErrProjectHasRunningSessions: a session in the project is currently
	// running; deleting would strand its runner.
	ErrProjectHasRunningSessions = errors.New("project has running sessions")
)

// projectScopedDeleteTables are the tables whose ownership is the project
// itself (no session linkage), so a plain project_id match is exact.
var projectScopedDeleteTables = []string{
	tableChatQuickSend,
	tableRecentProjects,
	"file_shares",
	tableProjectForges,
	tableSessionTags,
	"scheduled_tasks",
	tableTerminalQuickCommands,
}

// sessionLinkedDeleteTables carry BOTH session_id and project_id. They are
// deleted by `session_id IN (project's sessions) OR project_id = ?`: a row's
// project_id is not guaranteed to equal its session's project (historic rows
// were persisted under the request cookie's project — see the ISS-420 note on
// idx_history_sess_unread), so a project_id-only match would leave rows behind
// while deleting their session. Matching both sides covers either attribution.
var sessionLinkedDeleteTables = []string{
	"chat_history",
	"chat_metadata",
	"chat_recommendations",
	"queued_messages",
	"btw_questions",
}

// DeleteProjectData permanently removes a project and every row scoped to it.
//
// Scope: all project-scoped data plus the child rows that hang off the project's
// sessions, tasks and tags. The directory on disk is NOT touched — this is a
// data operation, not a filesystem one.
//
// What is deliberately NOT touched: global session tags (project_id = 0), which
// are visible in every project and belong to no single one.
//
// Refuses to run when the project has a running session (the runner would be
// orphaned) or when the id is the global-scope sentinel. Whether the project is
// the caller's CURRENT project is the handler's concern, checked against the
// request cookie.
func DeleteProjectData(id int64) error {
	if id == store.GlobalScopeProjectID {
		return ErrProjectDeleteForbidden
	}

	path, ok, err := store.ProjectPathForID(id)
	if err != nil {
		return fmt.Errorf("resolve project path: %w", err)
	}
	if !ok {
		return ErrProjectNotFound
	}

	if err := deleteProjectRows(id); err != nil {
		return err
	}

	// Post-commit, best-effort: drop the caches that key off this project so a
	// later re-registration of the same directory starts clean, and purge the
	// RAG index. Failures here are logged, not returned: the project rows are
	// already gone, so reporting the delete as failed would be a lie and would
	// invite a retry that finds nothing to delete.
	store.ForgetProjectPath(path)
	if err := rag.DeleteProjectData(id); err != nil {
		slog.Warn("failed to purge RAG data for deleted project",
			slog.Int64("project_id", id), slog.String("path", path), slog.String("err", err.Error()))
	}
	if rs := rag.StoreForCleanup(); rs != nil {
		rs.InvalidateProjectPathCache(path)
	}
	return nil
}

// deleteProjectRows runs every row deletion in one transaction. Split out of
// DeleteProjectData so the guard clauses and the post-commit cleanup do not
// share the function's cyclomatic budget.
//
// The running-session guard is re-checked HERE, after acquiring the write lock,
// rather than only in the caller: between the caller's check and this lock a run
// could have started, and deleting the session rows out from under a live
// runner is exactly the orphan the guard exists to prevent.
func deleteProjectRows(id int64) error {
	tx, err := store.WriteBegin()
	if err != nil {
		return err
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()

	hasRunning, err := projectHasRunningSession(id)
	if err != nil {
		return fmt.Errorf("check running sessions: %w", err)
	}
	if hasRunning {
		return ErrProjectHasRunningSessions
	}

	ctx := context.Background()

	// A chat_history row belongs to the project if EITHER its session does, or
	// its own project_id says so. The two can disagree (historic rows persisted
	// under the request cookie's project — see the ISS-420 note on
	// idx_history_sess_unread), so both sides are matched. This predicate is
	// reused verbatim for the summaries / tts_summaries deletes below: they key
	// on chat_history.id, so they must cover EXACTLY the same row set or a
	// mis-attributed message leaves its summary/TTS behind.
	const historyPredicate = "session_id IN (SELECT id FROM chat_sessions WHERE project_id = ?) OR project_id = ?"

	// Order matters: every child row that references chat_history / chat_sessions
	// / scheduled_tasks / session_tags must go before its parent, since the
	// deletes below are keyed on the parent still existing.
	sessionChildren := []struct {
		query string
		args  []any
	}{
		{"DELETE FROM chat_tool_calls WHERE session_id IN (SELECT id FROM chat_sessions WHERE project_id = ?)", []any{id}},
		{"DELETE FROM chat_thinking WHERE session_id IN (SELECT id FROM chat_sessions WHERE project_id = ?)", []any{id}},
		{"DELETE FROM summaries WHERE target_type = 'chat_message' AND target_id IN (SELECT id FROM chat_history WHERE " + historyPredicate + ")", []any{id, id}},
		{"DELETE FROM tts_summaries WHERE message_id IN (SELECT id FROM chat_history WHERE " + historyPredicate + ")", []any{id, id}},
		{"DELETE FROM task_executions WHERE task_id IN (SELECT id FROM scheduled_tasks WHERE project_id = ?)", []any{id}},
		{"DELETE FROM session_shares WHERE session_id IN (SELECT id FROM chat_sessions WHERE project_id = ?)", []any{id}},
	}
	for _, c := range sessionChildren {
		if _, err := tx.ExecContext(ctx, c.query, c.args...); err != nil {
			return fmt.Errorf("delete child rows: %w", err)
		}
	}

	// Tag links are matched on BOTH sides (the session's links and the project's
	// own tag definitions), so this query takes the id twice.
	if _, err := tx.ExecContext(
		ctx,
		"DELETE FROM session_tag_links WHERE session_id IN (SELECT id FROM chat_sessions WHERE project_id = ?) OR tag_id IN (SELECT id FROM session_tags WHERE project_id = ?)",
		id, id,
	); err != nil {
		return fmt.Errorf("delete session tag links: %w", err)
	}

	// Tables with both a session_id and a project_id: match either attribution.
	for _, table := range sessionLinkedDeleteTables {
		if _, err := tx.ExecContext(
			ctx,
			"DELETE FROM "+table+" WHERE "+historyPredicate,
			id, id,
		); err != nil {
			return fmt.Errorf("delete from %s: %w", table, err)
		}
	}

	// Tables owned by the project directly.
	for _, table := range projectScopedDeleteTables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE project_id = ?", id); err != nil {
			return fmt.Errorf("delete from %s: %w", table, err)
		}
	}

	// The project's own sessions, then the registry row.
	if _, err := tx.ExecContext(ctx, "DELETE FROM chat_sessions WHERE project_id = ?", id); err != nil {
		return fmt.Errorf("delete chat sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM projects WHERE id = ?", id); err != nil {
		return fmt.Errorf("delete project row: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit project delete: %w", err)
	}
	return nil
}

// projectHasRunningSession reports whether any session of the project is
// currently running.
func projectHasRunningSession(projectID int64) (bool, error) {
	running := GetRunningSessionIDs()
	if len(running) == 0 {
		return false, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(running)), ",")
	args := make([]any, 0, len(running)+1)
	args = append(args, projectID)
	for _, id := range running {
		args = append(args, id)
	}
	var count int
	err := store.ReadDB().QueryRowContext(
		context.Background(),
		"SELECT COUNT(*) FROM chat_sessions WHERE project_id = ? AND id IN ("+placeholders+")",
		args...,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
