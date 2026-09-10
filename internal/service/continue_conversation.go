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
	// Apply the agent's auto-approve default like CreateSession does, so the
	// continued interactive session matches a freshly created one.
	applyAgentAutoApproveDefault(newSessionID, agentID)
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
	if err := copySessionDetailTables(idMap, sourceSessionID, newSessionID); err != nil {
		return "", false, err
	}

	return newSessionID, false, nil
}

// ForkSession creates a new chat session by copying non-streaming messages
// and summaries from the source session. Unlike ContinueFromExecution, this
// does NOT copy external_session_id — the forked session starts fresh.
// If beforeMessageID > 0, only messages up to and including the assistant reply
// following the specified user message are copied. The title is provided by the caller.
func ForkSession(sourceSessionID, projectPath, title string, beforeMessageID int64, overrideAgentID string) (string, error) { //nolint:gocyclo // multi-step session fork with fork-point resolution
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

	// 1b. Override agent if specified (user chose a different agent in the fork dialog)
	if overrideAgentID != "" {
		if agent, ok := model.Agents[overrideAgentID]; ok {
			agentID = overrideAgentID
			agentSource = cancelReasonUser
			if agent.Backend != "" {
				backend = agent.Backend
			}
			// Clear model — let the frontend fall back to the global localStorage preference,
			// same as the create-session flow. The source session's model may not be
			// compatible with the new agent.
			modelName = ""
		}
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
	// Inherit the agent's auto-approve default like CreateSession does, so a
	// forked session matches a freshly created one for the same agent.
	applyAgentAutoApproveDefault(newSessionID, agentID)
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
	if err := copySessionDetailTables(idMap, sourceSessionID, newSessionID); err != nil {
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

// copySessionDetailTables copies chat_tool_calls and chat_thinking rows from the
// source session to the fork/continued session, remapping message_id via idMap.
// Rows whose source message_id is not in idMap (e.g. fork truncation) are skipped.
func copySessionDetailTables(idMap map[int64]int64, sourceSessionID, newSessionID string) error {
	if len(idMap) == 0 {
		return nil
	}

	// chat_tool_calls
	tcRows, err := dbRead.Query(
		"SELECT message_id, tool_id, name, input, output, status, done, summary, duration_ms, created_at FROM chat_tool_calls WHERE session_id = ?",
		sourceSessionID,
	)
	if err != nil {
		return fmt.Errorf("failed to query source tool calls: %w", err)
	}
	defer func() { _ = tcRows.Close() }()
	type toolRow struct {
		messageID  int64
		toolID     string
		name       string
		input      string
		output     string
		status     string
		done       int
		summary    string
		durationMs int
		createdAt  time.Time
	}
	var toolRows []toolRow
	for tcRows.Next() {
		var r toolRow
		if err := tcRows.Scan(&r.messageID, &r.toolID, &r.name, &r.input, &r.output, &r.status, &r.done, &r.summary, &r.durationMs, &r.createdAt); err != nil {
			return fmt.Errorf("failed to scan tool call: %w", err)
		}
		toolRows = append(toolRows, r)
	}
	for _, r := range toolRows {
		newID, ok := idMap[r.messageID]
		if !ok {
			continue
		}
		if _, err := WriteExec(
			"INSERT OR REPLACE INTO chat_tool_calls (message_id, session_id, tool_id, name, input, output, status, done, summary, duration_ms, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			newID, newSessionID, r.toolID, r.name, r.input, r.output, r.status, r.done, r.summary, r.durationMs, r.createdAt,
		); err != nil {
			return fmt.Errorf("failed to copy tool call %s: %w", r.toolID, err)
		}
	}

	// chat_thinking
	thRows, err := dbRead.Query(
		"SELECT message_id, think_id, seq, text, created_at FROM chat_thinking WHERE session_id = ?",
		sourceSessionID,
	)
	if err != nil {
		return fmt.Errorf("failed to query source thinking: %w", err)
	}
	defer func() { _ = thRows.Close() }()
	type thinkRow struct {
		messageID int64
		thinkID   string
		seq       int
		text      string
		createdAt time.Time
	}
	var thinkRows []thinkRow
	for thRows.Next() {
		var r thinkRow
		if err := thRows.Scan(&r.messageID, &r.thinkID, &r.seq, &r.text, &r.createdAt); err != nil {
			return fmt.Errorf("failed to scan thinking: %w", err)
		}
		thinkRows = append(thinkRows, r)
	}
	for _, r := range thinkRows {
		newID, ok := idMap[r.messageID]
		if !ok {
			continue
		}
		if _, err := WriteExec(
			"INSERT OR REPLACE INTO chat_thinking (message_id, session_id, think_id, seq, text, created_at) VALUES (?, ?, ?, ?, ?, ?)",
			newID, newSessionID, r.thinkID, r.seq, r.text, r.createdAt,
		); err != nil {
			return fmt.Errorf("failed to copy thinking %s: %w", r.thinkID, err)
		}
	}

	return nil
}

// RewindResult carries the outcome of an in-place session history truncation.
type RewindResult struct {
	// DeletedCount is the number of chat_history rows removed.
	DeletedCount int64
	// RestoredText is the plain text of the first user message removed by the
	// truncation (the user message immediately following the anchor assistant
	// reply). Empty when no user message was removed.
	RestoredText string
}

// Sentinel errors for TruncateSessionAfterMessage anchor validation. The handler
// maps them to an InvalidRewindPoint client response via errors.Is, so error
// matching does not depend on error-message wording.
var (
	// ErrRewindAnchorNotFound reports that the anchor message id does not exist
	// in the session.
	ErrRewindAnchorNotFound = errors.New("rewind anchor message not found in session")
	// ErrRewindAnchorStreaming reports that the anchor message is still streaming.
	ErrRewindAnchorStreaming = errors.New("rewind anchor message is still streaming")
	// ErrRewindAnchorNotAssistant reports that the anchor message is not an
	// assistant message.
	ErrRewindAnchorNotAssistant = errors.New("rewind anchor must be an assistant message")
)

// ValidateRewindAnchor is a read-only check that the anchor message is a valid
// rewind point: it must exist in the session, be an assistant message, and be
// finalized (streaming=0). It returns one of the ErrRewindAnchor* sentinel
// errors (or nil) WITHOUT mutating anything.
//
// The handler calls it BEFORE cancelling a running session, so an invalid
// rewind request (stale UI, double-click race, client bug) fails fast with a
// 400 instead of first cancelling the user's in-flight AI turn.
func ValidateRewindAnchor(sessionID string, anchorID int64) error {
	var role string
	var streaming int
	err := dbRead.QueryRow(
		"SELECT role, streaming FROM chat_history WHERE id = ? AND session_id = ?",
		anchorID, sessionID,
	).Scan(&role, &streaming)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: message %d in session %s", ErrRewindAnchorNotFound, anchorID, sessionID)
	}
	if err != nil {
		return err
	}
	if streaming == 1 {
		return fmt.Errorf("%w (message %d)", ErrRewindAnchorStreaming, anchorID)
	}
	if role != roleAssistant {
		return fmt.Errorf("%w, message %d is role %q", ErrRewindAnchorNotAssistant, anchorID, role)
	}
	return nil
}

// CountMessagesAfterAnchor returns the number of chat_history rows strictly
// after the anchor message in the session. Read-only; used by the rewind
// handler to short-circuit a no-op rewind (nothing follows the anchor) BEFORE
// cancelling a running session.
func CountMessagesAfterAnchor(sessionID string, anchorID int64) (int, error) {
	var trailingCount int
	err := dbRead.QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND id > ?",
		sessionID, anchorID,
	).Scan(&trailingCount)
	return trailingCount, err
}

// TruncateSessionAfterMessage truncates a session's history IN PLACE: every
// chat_history row with id > anchorID is deleted (the anchor assistant message
// and everything before it are preserved), along with all child rows. It is the
// "rewind/回溯" sibling of ForkSession — instead of copying to a new session,
// the current session is cut back to an earlier assistant reply.
//
// The anchor must be a finalized (streaming=0) assistant message. Because DB ids
// are strictly chronological and the anchor is finalized, id > anchorID captures
// exactly everything semantically after it, including queued user messages and
// any in-flight streaming placeholder.
//
// Child rows are deleted transactionally in the same order as HardDeleteSession.
// chat_tool_calls / chat_thinking / chat_metadata carry ON DELETE CASCADE on
// message_id, but they are deleted explicitly anyway to keep semantics visible
// and tests robust. RAG chunks for the removed messages are purged best-effort
// after commit via the injected range callback (a separate SQLite store that
// cannot be touched inside this DB transaction).
//
// The caller (handler) is responsible for resetting the AI-side session state
// (clearing external_session_id and closing the ACP connection) — this function
// only rewrites the local DB history.
func TruncateSessionAfterMessage(sessionID string, anchorID int64) (RewindResult, error) {
	var res RewindResult

	// 1. Validate the anchor message: must exist, be an assistant message and finalized.
	if err := ValidateRewindAnchor(sessionID, anchorID); err != nil {
		return res, err
	}

	// 2. No-op when nothing follows the anchor.
	trailingCount, err := CountMessagesAfterAnchor(sessionID, anchorID)
	if err != nil {
		return res, err
	}
	if trailingCount == 0 {
		return res, nil
	}

	// 3. Capture the prefill text of the first removed user message BEFORE the
	//    rows are deleted. queued/streaming user rows are included deliberately —
	//    they are user-typed inputs the rewind discards, so they belong in the
	//    input box for re-editing.
	var removedContent sql.NullString
	err = dbRead.QueryRow(
		"SELECT content FROM chat_history WHERE session_id = ? AND id > ? AND role = 'user' ORDER BY id ASC LIMIT 1",
		sessionID, anchorID,
	).Scan(&removedContent)
	if err == nil && removedContent.Valid {
		res.RestoredText = ExtractPlainText(removedContent.String)
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return res, err
	}

	// 4. Transactional delete — mirrors HardDeleteSession (chat.go): child rows
	//    are deleted first, with errors discarded — tts_summaries in particular
	//    only exists after the InitDB migration, not in the base schema, and the
	//    established pattern treats these best-effort cleanups as non-fatal. The
	//    authoritative chat_history delete below carries the real error.
	tx, err := WriteBegin()
	if err != nil {
		return res, err
	}
	// writeMu is held from WriteBegin until explicitly released below. The RAG
	// cleanup (step 5) must run AFTER writeMu.Unlock(): rag.Store shares the
	// service-global writeMu (serviceWriteLocker), so calling it while still
	// holding the mutex would self-deadlock on a non-reentrant lock.
	unlocked := false
	defer func() {
		_ = tx.Rollback() // no-op after Commit
		if !unlocked {
			writeMu.Unlock()
		}
	}()

	childPred := "SELECT id FROM chat_history WHERE session_id = ? AND id > ?"
	// ai_raw_responses: FK on message_id but NO cascade.
	_, _ = tx.Exec(
		"DELETE FROM ai_raw_responses WHERE session_id = ? AND message_id IN ("+childPred+")",
		sessionID, sessionID, anchorID,
	)
	// chat_tool_calls / chat_thinking / chat_metadata: FK ON DELETE CASCADE, but
	// deleted explicitly for visible semantics.
	_, _ = tx.Exec(
		"DELETE FROM chat_tool_calls WHERE session_id = ? AND message_id IN ("+childPred+")",
		sessionID, sessionID, anchorID,
	)
	_, _ = tx.Exec(
		"DELETE FROM chat_thinking WHERE session_id = ? AND message_id IN ("+childPred+")",
		sessionID, sessionID, anchorID,
	)
	_, _ = tx.Exec(
		"DELETE FROM chat_metadata WHERE message_id IN ("+childPred+")",
		sessionID, anchorID,
	)
	// summaries / tts_summaries: no FK on target_id / message_id.
	_, _ = tx.Exec(
		"DELETE FROM summaries WHERE target_type = 'chat_message' AND target_id IN ("+childPred+")",
		sessionID, anchorID,
	)
	_, _ = tx.Exec(
		"DELETE FROM tts_summaries WHERE message_id IN ("+childPred+")",
		sessionID, anchorID,
	)
	// chat_recommendations: message_id has no FK. Orphan rows pointing at removed
	// messages are inert (LatestChatRecommendation queries by exact message_id)
	// but are cleaned anyway to avoid accumulating stale follow-up suggestions.
	_, _ = tx.Exec(
		"DELETE FROM chat_recommendations WHERE session_id = ? AND message_id IN ("+childPred+")",
		sessionID, sessionID, anchorID,
	)

	result, err := tx.Exec("DELETE FROM chat_history WHERE session_id = ? AND id > ?", sessionID, anchorID)
	if err != nil {
		return res, err
	}
	res.DeletedCount, _ = result.RowsAffected()

	if err := tx.Commit(); err != nil {
		return res, err
	}
	// Release the global writeMu BEFORE the RAG cleanup (step 5): rag.Store
	// serializes on the same lock via serviceWriteLocker, so holding it here
	// would self-deadlock.
	writeMu.Unlock()
	unlocked = true

	// 5. Best-effort RAG chunk cleanup (separate store, after the DB transaction
	//    commits). Failures are logged, never fatal. A range predicate
	//    (session_id + message_id > anchor) is used rather than a pre-captured id
	//    list so chunks the RAG indexer inserted concurrently between the
	//    truncation commit and this cleanup are removed too (no snapshot gap).
	if _, ragErr := PurgeRAGChunksAfterMessage(sessionID, anchorID); ragErr != nil {
		slog.Warn("rewind: failed to purge RAG chunks after truncation",
			slog.String("session", sessionID),
			slog.Int64("anchor_message", anchorID),
			slog.String("error", ragErr.Error()))
	}

	slog.Info("session history rewound (truncated)",
		slog.String("session", sessionID),
		slog.Int64("anchor_message", anchorID),
		slog.Int64("deleted_messages", res.DeletedCount))
	return res, nil
}
