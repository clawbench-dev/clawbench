//nolint:noctx,govet,rowserrcheck // db global, context not applicable; shadowed err is acceptable; legacy db.Query pattern
package service

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"clawbench/internal/model"
)

// restoreArchivedSession restores an archived session by setting archived=0.
// Messages in chat_history are not affected — only the session record needs restoring
// since session-level archival controls visibility.
func restoreArchivedSession(sessionID string) error {
	_, err := WriteExec(
		"UPDATE chat_sessions SET archived = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("failed to restore archived session %s: %w", sessionID, err)
	}
	return nil
}

// CheckContinueSession checks whether a continued chat session already exists
// for the given task execution (including archived ones that can be restored).
// If an archived continued session is found, it is automatically restored
// (both the session record and its messages).
// Returns (exists, sessionID, error).
func CheckContinueSession(execID int64) (bool, string, error) {
	var sourceSessionID string
	err := dbRead.QueryRow("SELECT session_id FROM task_executions WHERE id = ?", execID).Scan(&sourceSessionID)
	if err == sql.ErrNoRows {
		return false, "", fmt.Errorf("execution %d not found", execID)
	}
	if err != nil {
		return false, "", err
	}

	var existingID string
	var existingArchived int
	err = dbRead.QueryRow(
		"SELECT id, archived FROM chat_sessions WHERE source_session_id = ? AND session_type = 'chat' ORDER BY archived ASC, updated_at DESC LIMIT 1",
		sourceSessionID,
	).Scan(&existingID, &existingArchived)
	if err == sql.ErrNoRows {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}

	// Auto-restore archived session so subsequent GET requests can find it
	if existingArchived == 1 {
		if err := restoreArchivedSession(existingID); err != nil {
			return false, "", err
		}
	}

	return true, existingID, nil
}

// ContinueFromExecution creates a new chat session from a scheduled task execution,
// copying the original session's chat_history and summaries. If a continued session
// already exists (and is not archived), it returns the existing session ID with
// alreadyExists=true.
//
// In production, DB has MaxOpenConns=1 so all writes are serialized through a single
// connection — this provides the same atomicity guarantee as BEGIN IMMEDIATE without
// the risk of connection-pool deadlocks in test environments.
func ContinueFromExecution(execID int64, projectPath string) (sessionID string, alreadyExists bool, err error) { //nolint:gocognit,gocyclo // multi-step session continuation with dedup
	// 1. Get execution info
	var sourceSessionID string
	var taskID int64
	var execStatus string
	var execCreatedAt time.Time
	err = dbRead.QueryRow(
		"SELECT session_id, task_id, status, created_at FROM task_executions WHERE id = ?",
		execID,
	).Scan(&sourceSessionID, &taskID, &execStatus, &execCreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, fmt.Errorf("execution %d not found", execID)
	}
	if err != nil {
		return "", false, err
	}

	// 2. Check execution status
	if execStatus == "running" {
		return "", false, fmt.Errorf("execution %d is still running", execID)
	}

	// 3. Get task name and validate project ownership
	var taskName string
	var taskProjectPath string
	err = dbRead.QueryRow(
		"SELECT name, project_path FROM scheduled_tasks WHERE id = ?",
		taskID,
	).Scan(&taskName, &taskProjectPath)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, fmt.Errorf("task %d not found", taskID)
	}
	if err != nil {
		return "", false, err
	}

	// 4. Validate project ownership
	if taskProjectPath != projectPath {
		return "", false, fmt.Errorf("execution %d does not belong to project %q", execID, projectPath)
	}

	// 5. Get source session metadata (without archived=0 — archived sessions still have valid metadata)
	var backend, agentID, agentSource, modelName, sessProjectPath, externalSessionID string
	err = dbRead.QueryRow(
		"SELECT backend, agent_id, agent_source, model, project_path, external_session_id FROM chat_sessions WHERE id = ?",
		sourceSessionID,
	).Scan(&backend, &agentID, &agentSource, &modelName, &sessProjectPath, &externalSessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, fmt.Errorf("source session %s not found", sourceSessionID)
	}
	if err != nil {
		return "", false, err
	}

	// 6. Dedup check — if a continued session already exists (even archived), restore it
	var existingID string
	var existingArchived int
	err = dbRead.QueryRow(
		"SELECT id, archived FROM chat_sessions WHERE source_session_id = ? AND session_type = 'chat' ORDER BY archived ASC, updated_at DESC LIMIT 1",
		sourceSessionID,
	).Scan(&existingID, &existingArchived)
	if err == nil {
		if existingArchived == 1 {
			// Restore archived session and its messages
			if err := restoreArchivedSession(existingID); err != nil {
				return "", false, err
			}
		}
		return existingID, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, err
	}

	// 7. Max session count check
	if model.SessionMaxCount > 0 {
		var count int
		err = dbRead.QueryRow(
			"SELECT COUNT(*) FROM chat_sessions WHERE project_path = ? AND archived = 0 AND session_type = 'chat'",
			sessProjectPath,
		).Scan(&count)
		if err != nil {
			return "", false, err
		}
		if count >= model.SessionMaxCount {
			return "", false, fmt.Errorf("session limit reached (%d/%d)", count, model.SessionMaxCount)
		}
	}

	// 8. Create new chat session
	newSessionID := generateSessionID()
	// Prefix title with execution date+time (no year) to identify which run this came from
	execTime := execCreatedAt.Format("01-02 15:04")
	displayTitle := "⏰ [" + execTime + "] " + taskName
	// Copy external_session_id from the source session so that --resume works correctly.
	// The continued session inherits the CLI backend's session context, allowing the
	// same resume flow as a normal session (no special-casing needed).
	_, err = WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, source_session_id, external_session_id, last_read_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'chat', ?, ?, CURRENT_TIMESTAMP)",
		newSessionID, sessProjectPath, backend, displayTitle, agentID, agentSource, modelName, sourceSessionID, externalSessionID,
	)
	if err != nil {
		return "", false, fmt.Errorf("failed to create continued session: %w", err)
	}
	slog.Info("continued session created",
		slog.String("session", newSessionID),
		slog.String("source_session", sourceSessionID),
		slog.String("external_session_id", externalSessionID),
		slog.String("backend", backend),
		slog.String("agent", agentID),
		slog.Int64("execution", execID))

	// 9. Copy chat_history (only streaming=0)
	// NOTE: We intentionally do NOT copy created_at. The Go SQLite driver (modernc.org/sqlite)
	// converts DATETIME columns to ISO 8601 UTC format (e.g. "2026-05-29T01:59:53Z") when reading,
	// but CURRENT_TIMESTAMP produces "YYYY-MM-DD HH:MM:SS" local format. Writing the ISO format
	// back would break string-based time comparisons (e.g. unread count query uses
	// h.created_at > s2.last_read_at). Instead, we let the database assign CURRENT_TIMESTAMP,
	// which guarantees format consistency. Message ordering relies on auto-increment id, not created_at.
	rows, err := dbRead.Query(
		"SELECT id, project_path, role, content, files, backend FROM chat_history WHERE session_id = ? AND streaming = 0 ORDER BY id",
		sourceSessionID,
	)
	if err != nil {
		return "", false, fmt.Errorf("failed to query source messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type sourceMsg struct {
		id          int64
		projectPath string
		role        string
		content     string
		files       sql.NullString
		backend     string
	}
	var messages []sourceMsg
	for rows.Next() {
		var m sourceMsg
		if err := rows.Scan(&m.id, &m.projectPath, &m.role, &m.content, &m.files, &m.backend); err != nil {
			return "", false, fmt.Errorf("failed to scan source message: %w", err)
		}
		messages = append(messages, m)
	}

	// Insert messages and build old ID -> new ID mapping for summaries
	idMap := make(map[int64]int64)
	for _, m := range messages {
		result, err := WriteExec(
			"INSERT INTO chat_history (project_path, role, content, files, session_id, backend, streaming) VALUES (?, ?, ?, ?, ?, ?, 0)",
			m.projectPath, m.role, m.content, m.files, newSessionID, m.backend,
		)
		if err != nil {
			return "", false, fmt.Errorf("failed to copy message %d: %w", m.id, err)
		}
		newID, _ := result.LastInsertId()
		idMap[m.id] = newID
	}

	// 10. Copy summaries (chat_message type — covers both interactive and
	// scheduled sessions since the scheduler now stores summaries as "chat_message"
	// keyed by the assistant message ID, same as interactive sessions).
	for oldID, newID := range idMap {
		var summary string
		err := dbRead.QueryRow(
			"SELECT summary FROM summaries WHERE target_type = 'chat_message' AND target_id = ?",
			oldID,
		).Scan(&summary)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return "", false, fmt.Errorf("failed to query summary for message %d: %w", oldID, err)
		}
		_, err = WriteExec(
			"INSERT OR REPLACE INTO summaries (target_type, target_id, summary, created_at) VALUES ('chat_message', ?, ?, CURRENT_TIMESTAMP)",
			newID, summary,
		)
		if err != nil {
			return "", false, fmt.Errorf("failed to copy summary for message %d: %w", oldID, err)
		}
	}

	return newSessionID, false, nil
}

// ForkSession creates a new chat session by copying non-streaming messages
// and summaries from the source session. Unlike ContinueFromExecution, this
// does NOT copy external_session_id — the forked session starts fresh.
// If beforeMessageID > 0, only messages up to and including the assistant reply
// following the specified user message are copied. The title is provided by the caller.
func ForkSession(sourceSessionID, projectPath, title string, beforeMessageID int64) (string, error) {
	// 1. Get source session metadata
	var backend, agentID, agentSource, modelName, sessProjectPath string
	err := dbRead.QueryRow(
		"SELECT backend, agent_id, agent_source, model, project_path FROM chat_sessions WHERE id = ? AND archived = 0",
		sourceSessionID,
	).Scan(&backend, &agentID, &agentSource, &modelName, &sessProjectPath)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("source session %s not found", sourceSessionID)
	}
	if err != nil {
		return "", err
	}

	// 2. Validate project ownership
	if sessProjectPath != projectPath {
		return "", fmt.Errorf("session %s does not belong to project %q", sourceSessionID, projectPath)
	}

	// 2b. Validate beforeMessageID if provided, and resolve the cut point
	cutBeforeID := beforeMessageID
	if beforeMessageID > 0 {
		var role string
		var streaming int
		err = dbRead.QueryRow(
			"SELECT role, streaming FROM chat_history WHERE id = ? AND session_id = ?",
			beforeMessageID, sourceSessionID,
		).Scan(&role, &streaming)
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("message %d not found in session %s", beforeMessageID, sourceSessionID)
		}
		if err != nil {
			return "", err
		}
		if streaming == 1 {
			return "", fmt.Errorf("cannot fork from a streaming message (message %d)", beforeMessageID)
		}
		switch role {
		case roleUser:
			// User message: find the next non-streaming assistant reply and include it
			var asstID int64
			err = dbRead.QueryRow(
				"SELECT id FROM chat_history WHERE session_id = ? AND role = 'assistant' AND streaming = 0 AND id > ? ORDER BY id LIMIT 1",
				sourceSessionID, beforeMessageID,
			).Scan(&asstID)
			if err == nil {
				cutBeforeID = asstID
			}
			// If no assistant reply found (e.g. last message is user), cut at the user message
		case roleAssistant:
			// Assistant message: fork directly at this message
			cutBeforeID = beforeMessageID
		default:
			return "", fmt.Errorf("fork point must be a user or assistant message, message %d is role %q", beforeMessageID, role)
		}
	}

	// 3. Max session count check
	if err := checkSessionLimit(sessProjectPath); err != nil {
		return "", err
	}

	// 4. Title is provided by handler (localized prefix + source title)

	// 5. Create new session (no external_session_id inheritance)
	newSessionID := generateSessionID()
	_, err = WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, source_session_id) VALUES (?, ?, ?, ?, ?, ?, ?, 'chat', ?)",
		newSessionID, sessProjectPath, backend, title, agentID, agentSource, modelName, sourceSessionID,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create forked session: %w", err)
	}
	slog.Info("session forked",
		slog.String("session", newSessionID),
		slog.String("source_session", sourceSessionID),
		slog.String("backend", backend),
		slog.String("agent", agentID))

	// 6. Copy messages and summaries
	idMap, err := copySessionMessages(sourceSessionID, newSessionID, cutBeforeID)
	if err != nil {
		return "", err
	}
	if err := copySessionSummaries(idMap); err != nil {
		return "", err
	}

	return newSessionID, nil
}

// checkSessionLimit returns an error if the session count has reached the maximum.
func checkSessionLimit(projectPath string) error {
	if model.SessionMaxCount <= 0 {
		return nil
	}
	var count int
	err := dbRead.QueryRow(
		"SELECT COUNT(*) FROM chat_sessions WHERE project_path = ? AND archived = 0 AND session_type = 'chat'",
		projectPath,
	).Scan(&count)
	if err != nil {
		return err
	}
	if count >= model.SessionMaxCount {
		return fmt.Errorf("session limit reached (%d/%d)", count, model.SessionMaxCount)
	}
	return nil
}

// copySessionMessages copies non-streaming messages from sourceSessionID to newSessionID.
// If beforeMessageID > 0, only messages with id <= beforeMessageID are copied.
// Returns a map from old message IDs to new message IDs.
func copySessionMessages(sourceSessionID, newSessionID string, beforeMessageID int64) (map[int64]int64, error) {
	query := "SELECT id, project_path, role, content, files, backend FROM chat_history WHERE session_id = ? AND streaming = 0"
	args := []any{sourceSessionID}
	if beforeMessageID > 0 {
		query += " AND id <= ?"
		args = append(args, beforeMessageID)
	}
	query += " ORDER BY id"
	rows, err := dbRead.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query source messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type sourceMsg struct {
		id          int64
		projectPath string
		role        string
		content     string
		files       sql.NullString
		backend     string
	}
	var messages []sourceMsg
	for rows.Next() {
		var m sourceMsg
		if err := rows.Scan(&m.id, &m.projectPath, &m.role, &m.content, &m.files, &m.backend); err != nil {
			return nil, fmt.Errorf("failed to scan source message: %w", err)
		}
		messages = append(messages, m)
	}

	idMap := make(map[int64]int64)
	for _, m := range messages {
		result, err := WriteExec(
			"INSERT INTO chat_history (project_path, role, content, files, session_id, backend, streaming) VALUES (?, ?, ?, ?, ?, ?, 0)",
			m.projectPath, m.role, m.content, m.files, newSessionID, m.backend,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to copy message %d: %w", m.id, err)
		}
		newID, _ := result.LastInsertId()
		idMap[m.id] = newID
	}
	return idMap, nil
}

// copySessionSummaries copies summaries from old message IDs to new message IDs.
func copySessionSummaries(idMap map[int64]int64) error {
	for oldID, newID := range idMap {
		var summary string
		err := dbRead.QueryRow(
			"SELECT summary FROM summaries WHERE target_type = 'chat_message' AND target_id = ?",
			oldID,
		).Scan(&summary)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to query summary for message %d: %w", oldID, err)
		}
		_, err = WriteExec(
			"INSERT OR REPLACE INTO summaries (target_type, target_id, summary, created_at) VALUES ('chat_message', ?, ?, CURRENT_TIMESTAMP)",
			newID, summary,
		)
		if err != nil {
			return fmt.Errorf("failed to copy summary for message %d: %w", oldID, err)
		}
	}
	return nil
}
