//nolint:errcheck,gocyclo,gosec,goconst,noctx,rowserrcheck // legacy file, nolint-only approach for diff stability
package service

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/platform"
	"clawbench/internal/summarize"
)

// GetChatHistory retrieves all chat messages for a given project path, backend, and session.
func GetChatHistory(projectPath, backend, sessionID string) ([]model.ChatMessage, error) {
	msgs, _, err := GetChatHistoryPaged(projectPath, backend, sessionID, 0, 0)
	return msgs, err
}

// GetChatHistoryPaged retrieves chat messages with pagination.
// limit=0 means no limit (all messages).
// beforeID: if > 0, only return messages with id < beforeID (cursor-based for lazy load).
// When beforeID == 0 and limit > 0, returns the most recent (limit) messages.
// Returns messages in chronological (ASC) order.
// Also returns the total message count for the session to avoid a separate COUNT query.
func GetChatHistoryPaged(projectPath, backend, sessionID string, limit int, beforeID int) ([]model.ChatMessage, int, error) {
	messages := []model.ChatMessage{}
	totalCount := GetChatMessageCount(sessionID)

	if limit > 0 && beforeID > 0 {
		// Cursor-based: load messages older than beforeID
		query := `SELECT id, role, content, files, backend, streaming, created_at, indexed FROM (
			SELECT id, role, content, files, backend, streaming, created_at, indexed FROM chat_history
			WHERE project_path = ? AND session_id = ? AND id < ?
			ORDER BY id DESC LIMIT ?
		) sub ORDER BY id ASC`
		rows, err := DBRead.Query(query, projectPath, sessionID, beforeID, limit)
		if err != nil {
			return messages, totalCount, err
		}
		defer rows.Close()
		msgs, err := scanMessages(rows, sessionID)
		return msgs, totalCount, err
	}

	if limit > 0 {
		// Initial load: get the most recent (limit) messages
		query := `SELECT id, role, content, files, backend, streaming, created_at, indexed FROM (
			SELECT id, role, content, files, backend, streaming, created_at, indexed FROM chat_history
			WHERE project_path = ? AND session_id = ?
			ORDER BY id DESC LIMIT ?
		) sub ORDER BY id ASC`
		rows, err := DBRead.Query(query, projectPath, sessionID, limit)
		if err != nil {
			return messages, totalCount, err
		}
		defer rows.Close()
		msgs, err := scanMessages(rows, sessionID)
		return msgs, totalCount, err
	}

	// No limit: return all messages in chronological order
	query := `SELECT id, role, content, files, backend, streaming, created_at, indexed FROM chat_history WHERE project_path = ? AND session_id = ? ORDER BY id ASC`
	rows, err := DBRead.Query(query, projectPath, sessionID)
	if err != nil {
		return messages, totalCount, err
	}
	defer rows.Close()
	msgs, err := scanMessages(rows, sessionID)
	return msgs, totalCount, err
}

// scanMessages scans rows into ChatMessage slice.
func scanMessages(rows *sql.Rows, sessionID string) ([]model.ChatMessage, error) {
	messages := []model.ChatMessage{}
	for rows.Next() {
		var msg model.ChatMessage
		var filesJSON sql.NullString
		var streaming int
		var indexed int
		if err := rows.Scan(&msg.ID, &msg.Role, &msg.Content, &filesJSON, &msg.Backend, &streaming, &msg.CreatedAt, &indexed); err != nil {
			return nil, err
		}
		msg.Streaming = streaming != 0
		msg.Indexed = indexed != 0
		if filesJSON.Valid && filesJSON.String != "" {
			json.Unmarshal([]byte(filesJSON.String), &msg.Files)
		}
		msg.SessionID = sessionID
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Enrich assistant messages with reading summaries
	enrichMessagesWithSummaries(messages)
	return messages, nil
}

// GetChatMessageCount returns the number of messages in a session.
func GetChatMessageCount(sessionID string) int {
	var count int
	DBRead.QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sessionID).Scan(&count)
	return count
}

// GetUserMessageIndex returns lightweight {id, content, files, createdAt} for all user messages
// in a session, ordered by id ASC. Used for the user message index navigation feature.
func GetUserMessageIndex(sessionID string) ([]model.ChatMessage, error) {
	rows, err := DBRead.Query(
		"SELECT id, content, files, created_at FROM chat_history WHERE session_id = ? AND role = 'user' AND streaming = 0 ORDER BY id ASC",
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := []model.ChatMessage{}
	for rows.Next() {
		var msg model.ChatMessage
		var filesJSON sql.NullString
		if err := rows.Scan(&msg.ID, &msg.Content, &filesJSON, &msg.CreatedAt); err != nil {
			return nil, err
		}
		msg.Role = "user"
		if filesJSON.Valid && filesJSON.String != "" {
			json.Unmarshal([]byte(filesJSON.String), &msg.Files)
		}
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}

// GetMessageContent returns the plain text content of a message by its ID,
// scoped to the specified session. Returns empty string if not found.
func GetMessageContent(id int64, sessionID string) (string, error) {
	var content string
	err := DBRead.QueryRow("SELECT content FROM chat_history WHERE id = ? AND session_id = ?", id, sessionID).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return ExtractPlainText(content), nil
}

// GetMessageByID fetches a single chat message by its database ID.
// Returns the complete message including all content blocks (text, thinking, tool_use).
func GetMessageByID(id int64) (*model.ChatMessage, error) {
	var msg model.ChatMessage
	var filesJSON sql.NullString
	var streaming int
	var indexed int

	err := DBRead.QueryRow(
		"SELECT id, role, content, files, backend, streaming, created_at, indexed, session_id, project_path FROM chat_history WHERE id = ?",
		id,
	).Scan(&msg.ID, &msg.Role, &msg.Content, &filesJSON, &msg.Backend, &streaming, &msg.CreatedAt, &indexed, &msg.SessionID, &msg.ProjectPath)
	if err != nil {
		return nil, err
	}
	msg.Streaming = streaming != 0
	msg.Indexed = indexed != 0
	if filesJSON.Valid && filesJSON.String != "" {
		json.Unmarshal([]byte(filesJSON.String), &msg.Files)
	}
	return &msg, nil
}

// GetMessagesBySessionID fetches all messages for a session by session_id alone.
// Unlike GetChatHistory, this does not require projectPath or backend — session_id is globally unique.
// Returns messages in chronological order with all content blocks (text, thinking, tool_use).
func GetMessagesBySessionID(sessionID string) ([]model.ChatMessage, error) {
	rows, err := DBRead.Query(
		"SELECT id, role, content, files, backend, streaming, created_at, indexed FROM chat_history WHERE session_id = ? AND streaming = 0 ORDER BY id ASC",
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows, sessionID)
}

// ExtractPlainText extracts plain text from content that may be block-format JSON
// (e.g. {"blocks":[{"type":"text","text":"hello"}]}) or plain text.
// Returns the original content unchanged if it's not block-format JSON.
func ExtractPlainText(content string) string {
	if !strings.HasPrefix(content, `{"blocks":`) {
		return content
	}
	var wrapper struct {
		Blocks []model.ContentBlock `json:"blocks"`
	}
	if json.Unmarshal([]byte(content), &wrapper) != nil {
		return content
	}
	text := summarize.ExtractTextFromBlocks(wrapper.Blocks)
	if text == "" {
		return content
	}
	return text
}

// AddChatMessage adds a message to the chat history for a given project path, backend, and session.
func AddChatMessage(projectPath, backend, sessionID, role, content string, files []string, streaming bool, fallbackTitle string) (int64, error) {
	// Guard: reject messages to soft-deleted sessions
	var isDeleted int
	if err := DB.QueryRow("SELECT deleted FROM chat_sessions WHERE id = ?", sessionID).Scan(&isDeleted); err == nil && isDeleted == 1 {
		return 0, fmt.Errorf("cannot add message to deleted session %s", sessionID)
	}

	var filesJSON string
	if len(files) > 0 {
		data, _ := json.Marshal(files)
		filesJSON = string(data)
	}

	streamingInt := 0
	if streaming {
		streamingInt = 1
	}

	// Use transaction to ensure data consistency
	tx, err := DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, files, streaming, indexed) VALUES (?, ?, ?, ?, ?, ?, ?, 0)",
		projectPath, backend, sessionID, role, content, filesJSON, streamingInt,
	)
	if err != nil {
		return 0, err
	}

	// Update session's updated_at timestamp
	_, err = tx.Exec("UPDATE chat_sessions SET updated_at = CURRENT_TIMESTAMP WHERE id = ?", sessionID)
	if err != nil {
		return 0, err
	}

	// If this is the first user message, update session title
	if role == "user" {
		var count int
		err = tx.QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sessionID).Scan(&count)
		if err == nil && count == 1 {
			title := ExtractPlainText(content)
			if len(files) > 0 && title == "" {
				title = fallbackTitle
			}
			if title == "" {
				title = fallbackTitle
			}
			runes := []rune(title)
			if len(runes) > 50 {
				title = string(runes[:50]) + "..."
			}
			_, err = tx.Exec("UPDATE chat_sessions SET title = ? WHERE id = ?", title, sessionID)
			if err != nil {
				return 0, err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	messageID, _ := result.LastInsertId()
	return messageID, nil
}

// GetRecentProjects returns the most recent project paths.
// It filters out paths whose directories no longer exist on disk
// and removes those stale entries from the database.
func GetRecentProjects() ([]string, error) {
	limit := model.RecentProjectsMaxCount
	if limit <= 0 {
		limit = 10
	}
	var paths []string
	rows, err := DBRead.Query("SELECT project_path FROM recent_projects ORDER BY accessed_at DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Filter out projects whose directories no longer exist
	var valid []string
	var stale []string
	for _, p := range paths {
		info, statErr := os.Stat(p)
		if statErr == nil && info.IsDir() {
			valid = append(valid, p)
		} else {
			stale = append(stale, p)
		}
	}

	// Clean up stale entries from database
	for _, p := range stale {
		if delErr := RemoveRecentProject(p); delErr != nil {
			slog.Warn("failed to remove stale recent project", slog.String("path", p), slog.String("err", delErr.Error()))
		} else {
			slog.Info("removed stale recent project", slog.String("path", p))
		}
	}

	return valid, nil
}

// AddRecentProject upserts a project path and prunes old entries beyond configured limit.
func AddRecentProject(projectPath string) error {
	_, err := DB.Exec(
		"INSERT INTO recent_projects (project_path, accessed_at) VALUES (?, CURRENT_TIMESTAMP) "+
			"ON CONFLICT(project_path) DO UPDATE SET accessed_at = CURRENT_TIMESTAMP",
		projectPath,
	)
	if err != nil {
		return err
	}
	limit := model.RecentProjectsMaxCount
	if limit <= 0 {
		limit = 10
	}
	_, err = DB.Exec(
		"DELETE FROM recent_projects WHERE id NOT IN (SELECT id FROM recent_projects ORDER BY accessed_at DESC LIMIT ?)",
		limit,
	)
	return err
}

// RemoveRecentProject deletes a project path from the recent projects list.
// If the removed project was the default, its is_default flag is cleared first.
func RemoveRecentProject(projectPath string) error {
	_, _ = DB.Exec("UPDATE recent_projects SET is_default = 0 WHERE project_path = ? AND is_default = 1", projectPath)
	_, err := DB.Exec("DELETE FROM recent_projects WHERE project_path = ?", projectPath)
	return err
}

// GetDefaultProject returns the project path marked as default (is_default=1),
// or falls back to the most recently accessed project, or the user's home directory,
// or the first root path. This is the server-side source of truth for project selection.
// It does NOT update accessed_at (avoids the self-reinforcing loop).
func GetDefaultProject() (string, error) {
	// 1. Try is_default=1 row
	var path string
	err := DBRead.QueryRow("SELECT project_path FROM recent_projects WHERE is_default = 1 LIMIT 1").Scan(&path)
	if err == nil {
		// Verify directory still exists
		if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
			return path, nil
		}
		// Stale default — clear it
		_, _ = DB.Exec("UPDATE recent_projects SET is_default = 0 WHERE is_default = 1")
	}

	// 2. Fall back to most recently accessed (DO NOT update accessed_at)
	recents, err := GetRecentProjects()
	if err == nil && len(recents) > 0 {
		return recents[0], nil
	}

	// 3. Home directory
	if homeDir := platform.UserHomeDir(); homeDir != "" {
		return homeDir, nil
	}

	// 4. First root path
	if len(model.RootPaths) > 0 {
		return model.RootPaths[0], nil
	}

	return "", fmt.Errorf("no project path available")
}

// SetDefaultProject marks the given project path as the default project.
// It clears any existing default first (only one row can have is_default=1).
// This should only be called on user-initiated project switches.
func SetDefaultProject(projectPath string) error {
	// Clear existing default
	_, _ = DB.Exec("UPDATE recent_projects SET is_default = 0 WHERE is_default = 1")
	// Ensure the project exists in recent_projects (upsert with accessed_at update)
	if err := AddRecentProject(projectPath); err != nil {
		return err
	}
	// Set the new default
	_, err := DB.Exec("UPDATE recent_projects SET is_default = 1 WHERE project_path = ?", projectPath)
	return err
}

// generateSessionID generates a standard UUID v4 format session ID.
func generateSessionID() string {
	return generateUUID("", "chat_sessions", "id")
}

// GetSessions retrieves chat sessions for a given project path.
// If backend is non-empty, filters by backend; otherwise returns all backends.
// Only returns sessions with session_type='chat' (excludes scheduled sessions).
func GetSessions(projectPath, backend string) ([]model.ChatSession, error) {
	sessions := []model.ChatSession{}
	query := `SELECT s.id, s.title, s.backend, s.agent_id, s.agent_source, s.model, s.session_type, s.source_session_id, s.created_at, s.updated_at, s.last_read_at,
		COALESCE(unread.cnt, 0) AS unread_count
		FROM chat_sessions s
		LEFT JOIN (
			SELECT h.session_id, COUNT(*) AS cnt
			FROM chat_history h
			JOIN chat_sessions s2 ON s2.id = h.session_id
			WHERE h.project_path = ?
			  AND h.role = 'assistant' AND h.streaming = 0
			  AND (s2.last_read_at IS NULL OR h.created_at > s2.last_read_at)
			GROUP BY h.session_id
		) unread ON unread.session_id = s.id
		WHERE s.project_path = ? AND s.deleted = 0 AND s.session_type = 'chat'`
	args := []interface{}{projectPath, projectPath}
	if backend != "" {
		query += " AND s.backend = ?"
		args = append(args, backend)
	}
	query += " ORDER BY s.updated_at DESC, s.id DESC"

	rows, err := DBRead.Query(query, args...)
	if err != nil {
		return sessions, err
	}
	defer rows.Close()

	for rows.Next() {
		var s model.ChatSession
		var lastRead sql.NullTime
		var sourceSessionID sql.NullString
		if err := rows.Scan(&s.ID, &s.Title, &s.Backend, &s.AgentID, &s.AgentSource, &s.Model, &s.SessionType, &sourceSessionID, &s.CreatedAt, &s.UpdatedAt, &lastRead, &s.UnreadCount); err != nil {
			return nil, err
		}
		if lastRead.Valid {
			s.LastReadAt = &lastRead.Time
		}
		if sourceSessionID.Valid {
			s.SourceSessionID = sourceSessionID.String
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// GetSessionsPaged retrieves chat sessions with cursor-based pagination.
// limit=0 means no limit (returns all sessions).
// cursor and cursorID: when non-empty, only return sessions with
//
//	(updated_at < cursor) OR (updated_at = cursor AND id < cursorID)
//
// Returns sessions and hasMore flag.
func GetSessionsPaged(projectPath, backend string, limit int, cursor string, cursorID string) ([]model.ChatSession, bool, error) {
	// No limit: return all sessions
	if limit <= 0 {
		sessions, err := GetSessions(projectPath, backend)
		if err != nil {
			return nil, false, err
		}
		return sessions, false, nil
	}

	// Build main query with cursor and limit+1
	query := `SELECT s.id, s.title, s.backend, s.agent_id, s.agent_source, s.model, s.session_type, s.source_session_id, s.created_at, s.updated_at, s.last_read_at,
		COALESCE(unread.cnt, 0) AS unread_count
		FROM chat_sessions s
		LEFT JOIN (
			SELECT h.session_id, COUNT(*) AS cnt
			FROM chat_history h
			JOIN chat_sessions s2 ON s2.id = h.session_id
			WHERE h.project_path = ?
			  AND h.role = 'assistant' AND h.streaming = 0
			  AND (s2.last_read_at IS NULL OR h.created_at > s2.last_read_at)
			GROUP BY h.session_id
		) unread ON unread.session_id = s.id
		WHERE s.project_path = ? AND s.deleted = 0 AND s.session_type = 'chat'`
	args := []interface{}{projectPath, projectPath}
	if backend != "" {
		query += " AND s.backend = ?"
		args = append(args, backend)
	}
	if cursor != "" && cursorID != "" {
		query += " AND (s.updated_at < ? OR (s.updated_at = ? AND s.id < ?))"
		args = append(args, cursor, cursor, cursorID)
	}
	query += " ORDER BY s.updated_at DESC, s.id DESC LIMIT ?"
	args = append(args, limit+1)

	rows, err := DBRead.Query(query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var sessions []model.ChatSession
	for rows.Next() {
		var s model.ChatSession
		var lastRead sql.NullTime
		var sourceSessionID sql.NullString
		if err := rows.Scan(&s.ID, &s.Title, &s.Backend, &s.AgentID, &s.AgentSource, &s.Model, &s.SessionType, &sourceSessionID, &s.CreatedAt, &s.UpdatedAt, &lastRead, &s.UnreadCount); err != nil {
			return nil, false, err
		}
		if lastRead.Valid {
			s.LastReadAt = &lastRead.Time
		}
		if sourceSessionID.Valid {
			s.SourceSessionID = sourceSessionID.String
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	hasMore := len(sessions) > limit
	if hasMore {
		sessions = sessions[:limit]
	}

	return sessions, hasMore, nil
}

// UpdateLastRead sets the last_read_at timestamp for a session to now.
// Runs asynchronously to avoid blocking the read path with a DB write.
func UpdateLastRead(sessionID string) {
	go DB.Exec("UPDATE chat_sessions SET last_read_at = CURRENT_TIMESTAMP WHERE id = ?", sessionID)
}

// GetSessionBackend returns the backend of a session, or empty string if not found or deleted.
func GetSessionBackend(sessionID string) string {
	var backend string
	err := DBRead.QueryRow("SELECT backend FROM chat_sessions WHERE id = ? AND deleted = 0", sessionID).Scan(&backend)
	if err != nil {
		return ""
	}
	return backend
}

// GetSessionProjectPath returns the project path of a session, or empty string if not found.
func GetSessionProjectPath(sessionID string) string {
	var projectPath string
	err := DBRead.QueryRow("SELECT project_path FROM chat_sessions WHERE id = ?", sessionID).Scan(&projectPath)
	if err != nil {
		return ""
	}
	return projectPath
}

// GetLatestSessionID returns the ID and backend of the most recently updated chat session
// for a project. Returns sql.ErrNoRows if no sessions exist.
func GetLatestSessionID(projectPath string) (sessionID, backend string, err error) {
	err = DBRead.QueryRow(
		`SELECT id, backend FROM chat_sessions
		 WHERE project_path = ? AND deleted = 0 AND session_type = 'chat'
		 ORDER BY updated_at DESC, id DESC LIMIT 1`,
		projectPath,
	).Scan(&sessionID, &backend)
	return
}

// GetMessageIDBeforeTime resolves a legacy "before" (created_at timestamp) cursor
// to the corresponding message ID. This provides backward compatibility for older
// clients that still send ?before=<timestamp> instead of ?before_id=<id>.
// Returns the max ID of messages created before the given timestamp, or 0 if none found.
func GetMessageIDBeforeTime(projectPath, backend, sessionID, beforeTime string) (int, error) {
	var id sql.NullInt64
	err := DBRead.QueryRow(
		`SELECT MAX(id) FROM chat_history
		 WHERE project_path = ? AND backend = ? AND session_id = ?
		 AND created_at < ?`,
		projectPath, backend, sessionID, beforeTime,
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return int(id.Int64), nil
}

// GetSessionModel returns the model ID of a session, or empty string if not found or deleted.
func GetSessionModel(sessionID string) string {
	var modelID string
	err := DBRead.QueryRow("SELECT model FROM chat_sessions WHERE id = ? AND deleted = 0", sessionID).Scan(&modelID)
	if err != nil {
		return ""
	}
	return modelID
}

// UpdateSessionModel updates the model field for a session.
// Called when the user selects a different model so that subsequent loads
// restore the user's choice instead of the agent default.
func UpdateSessionModel(sessionID, modelID string) error {
	_, err := DB.Exec("UPDATE chat_sessions SET model = ? WHERE id = ?", modelID, sessionID)
	return err
}

// UpdateSessionTransport updates the transport field for a session.
func UpdateSessionTransport(sessionID, transport string) error {
	_, err := DB.Exec("UPDATE chat_sessions SET transport = ? WHERE id = ?", transport, sessionID)
	return err
}

// GetSessionTransport returns the transport for a session, or empty string if not set.
func GetSessionTransport(sessionID string) string {
	var transport string
	err := DBRead.QueryRow("SELECT COALESCE(transport, '') FROM chat_sessions WHERE id = ? AND deleted = 0", sessionID).Scan(&transport)
	if err != nil {
		return ""
	}
	return transport
}

// GetSessionAutoApprove returns whether auto-approve mode is enabled for a session.
func GetSessionAutoApprove(sessionID string) bool {
	var val int
	err := DBRead.QueryRow("SELECT auto_approve FROM chat_sessions WHERE id = ? AND deleted = 0", sessionID).Scan(&val)
	if err != nil {
		return false
	}
	return val == 1
}

// UpdateSessionAutoApprove updates the auto_approve flag for a session.
func UpdateSessionAutoApprove(sessionID string, enabled bool) error {
	val := 0
	if enabled {
		val = 1
	}
	_, err := DB.Exec("UPDATE chat_sessions SET auto_approve = ? WHERE id = ?", val, sessionID)
	return err
}

// SaveMetadata persists message metadata to the chat_metadata table.
// This enables SQL-based analytical queries (token usage, cost, model stats)
// while the same metadata remains embedded in chat_history.content JSON for
// backward compatibility with the frontend.
func SaveMetadata(messageID int64, meta *ai.Metadata) error {
	if messageID <= 0 || meta == nil {
		return nil
	}
	isError := 0
	if meta.IsError {
		isError = 1
	}
	_, err := DB.Exec(
		`
		INSERT OR REPLACE INTO chat_metadata
			(message_id, mode, thinking_effort, transport, model, input_tokens, output_tokens,
			 duration_ms, wall_ms, cost_usd, stop_reason, is_error, error_message)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		messageID, meta.Mode, meta.ThinkingEffort, meta.Transport, meta.Model,
		meta.InputTokens, meta.OutputTokens, meta.DurationMs, meta.WallMs,
		meta.CostUSD, meta.StopReason, isError, meta.ErrorMessage,
	)
	return err
}

// GetLatestUserModel returns the most recent model the user explicitly chose
// for the given agent+project. Returns "" if no user preference exists
// (caller should fall back to agent defaults).
// Used by scheduled tasks to respect the user's global model preference.
func GetLatestUserModel(agentID, projectPath string) string {
	var modelID string
	err := DBRead.QueryRow(
		"SELECT model FROM chat_sessions WHERE agent_id = ? AND project_path = ? AND deleted = 0 AND model != '' ORDER BY updated_at DESC LIMIT 1",
		agentID, projectPath,
	).Scan(&modelID)
	if err != nil {
		return ""
	}
	return modelID
}

// CreateSession creates a new chat session and returns its ID.
// agentSource tracks how the agent was chosen: "default" (auto-assigned) or "user" (manually selected).
// sessionType is "chat" or "scheduled"; empty string defaults to "chat".
func CreateSession(projectPath, backend, title, agentID, modelName, agentSource, sessionType string) (string, error) {
	if sessionType == "" {
		sessionType = "chat"
	}
	sessionID := generateSessionID()
	if sessionID == "" {
		return "", fmt.Errorf("failed to generate unique session ID after 10 attempts")
	}
	_, err := DB.Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, external_session_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		sessionID, projectPath, backend, title, agentID, agentSource, modelName, sessionType, "",
	)
	if err != nil {
		return "", err
	}
	slog.Info("session created",
		slog.String("session", sessionID),
		slog.String("backend", backend),
		slog.String("agent", agentID),
		slog.String("type", sessionType),
		slog.String("source", agentSource))
	return sessionID, nil
}

// UpdateSessionSourceID sets the source_session_id for a chat session.
// Used by acp-load to track the ACP session origin (format: "acp:{acpSessionId}").
func UpdateSessionSourceID(sessionID, sourceSessionID string) error {
	_, err := DB.Exec("UPDATE chat_sessions SET source_session_id = ? WHERE id = ?", sourceSessionID, sessionID)
	return err
}

// UpdateSessionTitle updates the title of a chat session.
func UpdateSessionTitle(sessionID, title string) error {
	_, err := DB.Exec("UPDATE chat_sessions SET title = ? WHERE id = ?", title, sessionID)
	return err
}

// DeleteSession soft-deletes a chat session.
// Sets deleted=1 on the session record and updates updated_at so it serves as the deletion timestamp.
// Messages in chat_history are NOT soft-deleted — session-level soft-delete is sufficient
// since all message queries are scoped to sessions, and deleted sessions are excluded.
// Data remains for RAG search but is hidden from UI; purged by cleanup worker after retention period.
func DeleteSession(projectPath, backend, sessionID string) error {
	// Soft-delete the session record, update timestamp to mark deletion time.
	// backend param kept for API compatibility but not used in WHERE —
	// session ID (UUID) is already unique; filtering by backend could cause
	// silent no-op when the client sends a wrong/empty backend value.
	_, err := DB.Exec("UPDATE chat_sessions SET deleted = 1, updated_at = CURRENT_TIMESTAMP WHERE project_path = ? AND id = ?", projectPath, sessionID)
	return err
}

// GetSessionCount returns the number of chat sessions for a given project.
// Only counts sessions with session_type='chat' (excludes scheduled sessions).
func GetSessionCount(projectPath string) (int, error) {
	var count int
	err := DBRead.QueryRow("SELECT COUNT(*) FROM chat_sessions WHERE project_path = ? AND deleted = 0 AND session_type = 'chat'", projectPath).Scan(&count)
	return count, err
}

// GetSessionTitle returns the title of an active (non-deleted) session.
func GetSessionTitle(sessionID string) (string, error) {
	var title string
	err := DBRead.QueryRow("SELECT title FROM chat_sessions WHERE id = ? AND deleted = 0", sessionID).Scan(&title)
	if err != nil {
		return "", err
	}
	return title, nil
}

// GetSessionTitlesBatch fetches titles for multiple sessions in a single query.
func GetSessionTitlesBatch(sessionIDs []string) (map[string]string, error) {
	if len(sessionIDs) == 0 {
		return map[string]string{}, nil
	}

	placeholders := ""
	args := make([]any, len(sessionIDs))
	for i, id := range sessionIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args[i] = id
	}

	rows, err := DBRead.Query("SELECT id, title FROM chat_sessions WHERE id IN ("+placeholders+") AND deleted = 0", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	titles := make(map[string]string, len(sessionIDs))
	for rows.Next() {
		var id, title string
		if err := rows.Scan(&id, &title); err != nil {
			continue
		}
		if title != "" {
			titles[id] = title
		}
	}
	return titles, rows.Err()
}

// GetSessionTitlesBatchIncludeDeleted fetches titles for multiple sessions
// including soft-deleted ones. Used by RAG search to show titles even for
// deleted sessions whose chunks are still indexed.
func GetSessionTitlesBatchIncludeDeleted(sessionIDs []string) (map[string]string, error) {
	if len(sessionIDs) == 0 {
		return map[string]string{}, nil
	}

	placeholders := ""
	args := make([]any, len(sessionIDs))
	for i, id := range sessionIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args[i] = id
	}

	rows, err := DBRead.Query("SELECT id, title FROM chat_sessions WHERE id IN ("+placeholders+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	titles := make(map[string]string, len(sessionIDs))
	for rows.Next() {
		var id, title string
		if err := rows.Scan(&id, &title); err != nil {
			continue
		}
		if title != "" {
			titles[id] = title
		}
	}
	return titles, rows.Err()
}

// SessionInfo contains session metadata for the chat view.
type SessionInfo struct {
	Title       string
	Backend     string
	AgentID     string
	Model       string
	Transport   string
	AutoApprove bool
	ProjectPath string // populated by GetSessionFullInfo only
}

// GetSessionInfo fetches session metadata (title, backend, agent_id, model, transport)
// in a single query instead of separate queries.
func GetSessionInfo(sessionID string) (*SessionInfo, error) {
	info := &SessionInfo{}
	err := DBRead.QueryRow(
		`SELECT title, backend, agent_id, model, COALESCE(transport, '')
		 FROM chat_sessions WHERE id = ? AND deleted = 0`,
		sessionID,
	).Scan(&info.Title, &info.Backend, &info.AgentID, &info.Model, &info.Transport)
	if err != nil {
		return nil, err
	}
	return info, nil
}

// GetSessionFullInfo fetches all session metadata including project_path in a single query.
// This replaces the common pattern of calling GetSessionBackend + GetSessionProjectPath +
// GetSessionInfo (3 separate PK lookups on the same row) with a single query.
// Returns nil if the session is not found or soft-deleted.
func GetSessionFullInfo(sessionID string) *SessionInfo {
	info := &SessionInfo{}
	err := DBRead.QueryRow(
		`SELECT backend, project_path, title, agent_id, model, COALESCE(transport, ''), auto_approve
		 FROM chat_sessions WHERE id = ? AND deleted = 0`,
		sessionID,
	).Scan(&info.Backend, &info.ProjectPath, &info.Title, &info.AgentID, &info.Model, &info.Transport, &info.AutoApprove)
	if err != nil {
		return nil
	}
	return info
}

// GetSessionAgentID returns the agent_id of an active (non-deleted) session.
func GetSessionAgentID(sessionID string) string {
	var agentID string
	DBRead.QueryRow("SELECT agent_id FROM chat_sessions WHERE id = ? AND deleted = 0", sessionID).Scan(&agentID)
	return agentID
}

// SessionHasAssistant checks if a session already has finalized assistant replies (for Claude --resume).
func SessionHasAssistant(sessionID string) bool {
	return GetAssistantMessageCount(sessionID) > 0
}

// GetAssistantMessageCount returns the number of finalized assistant messages in a session.
// Used to determine when to re-inject the system prompt for CLI backends without --system-prompt.
func GetAssistantMessageCount(sessionID string) int {
	var count int
	DBRead.QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND role = 'assistant' AND streaming = 0", sessionID).Scan(&count)
	return count
}

// UpdateStreamingMessage updates the content of the streaming assistant message for a session.
func UpdateStreamingMessage(projectPath, backend, sessionID, content string) error {
	_, err := DB.Exec(
		"UPDATE chat_history SET content = ? WHERE project_path = ? AND backend = ? AND session_id = ? AND role = 'assistant' AND streaming = 1",
		content, projectPath, backend, sessionID,
	)
	return err
}

// FinalizeStreamingMessage marks the streaming assistant message as complete and updates its content.
// Also marks the message as unindexed (indexed=0) so the RAG indexer picks it up.
// Returns the message ID of the finalized message (0 if not found).
func FinalizeStreamingMessage(projectPath, backend, sessionID, content string) (int64, error) {
	result, err := DB.Exec(
		"UPDATE chat_history SET content = ?, streaming = 0, indexed = 0 WHERE project_path = ? AND backend = ? AND session_id = ? AND role = 'assistant' AND streaming = 1",
		content, projectPath, backend, sessionID,
	)
	if err != nil {
		return 0, err
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return 0, nil
	}
	// Look up the message ID for the just-finalized row
	var msgID int64
	err = DBRead.QueryRow(
		"SELECT id FROM chat_history WHERE project_path = ? AND backend = ? AND session_id = ? AND role = 'assistant' AND streaming = 0 ORDER BY id DESC LIMIT 1",
		projectPath, backend, sessionID,
	).Scan(&msgID)
	if err != nil {
		return 0, nil //nolint:nilerr // message finalized but ID lookup failed — non-fatal
	}
	return msgID, nil
}

// GetStreamingMessageID returns the ID of the current or most recent assistant message for a session.
// Prefers the actively streaming message (streaming=1) so that stream_start events
// and tool call detail APIs reference the correct message ID during streaming.
// Falls back to the latest finalized message (streaming=0) if no active stream exists.
// Returns 0 if not found.
func GetStreamingMessageID(sessionID string) int64 {
	var id int64
	// Prefer actively streaming message
	err := DBRead.QueryRow(
		"SELECT id FROM chat_history WHERE session_id = ? AND role = 'assistant' AND streaming = 1 ORDER BY id DESC LIMIT 1",
		sessionID,
	).Scan(&id)
	if err == nil {
		return id
	}
	// Fallback: latest finalized message
	err = DBRead.QueryRow(
		"SELECT id FROM chat_history WHERE session_id = ? AND role = 'assistant' AND streaming = 0 ORDER BY id DESC LIMIT 1",
		sessionID,
	).Scan(&id)
	if err != nil {
		return 0
	}
	return id
}

// UpdateMessageContent updates the content of a specific message by its ID.
func UpdateMessageContent(messageID int, content string) error {
	_, err := DB.Exec("UPDATE chat_history SET content = ? WHERE id = ?", content, messageID)
	return err
}

// SaveRawResponse saves the raw AI backend output for debugging/analysis.
// Called only after the AI response is fully complete.
func SaveRawResponse(sessionID, backend string, messageID int64, rawOutput string) error {
	_, err := DB.Exec(
		"INSERT INTO ai_raw_responses (session_id, message_id, backend, raw_output) VALUES (?, ?, ?, ?)",
		sessionID, messageID, backend, rawOutput,
	)
	return err
}

// UpdateExternalSessionID sets the external session ID for a ClawBench session.
func UpdateExternalSessionID(sessionID, externalID string) error {
	_, err := DB.Exec("UPDATE chat_sessions SET external_session_id = ? WHERE id = ?", externalID, sessionID)
	if err != nil {
		return err
	}
	slog.Info("external_session_id updated",
		slog.String("session", sessionID),
		slog.String("external_session_id", externalID))
	return nil
}

// ClearExternalSessionID clears the external session ID for a ClawBench session.
// Called when transport switches from CLI to ACP — ACP manages its own session
// mapping internally, so the CLI's external_session_id must not leak into the
// ACP connection pool's GetOrCreateConn pre-population logic.
func ClearExternalSessionID(sessionID string) {
	_, _ = DB.Exec("UPDATE chat_sessions SET external_session_id = '' WHERE id = ?", sessionID)
}

// GetExternalSessionID returns the external session ID for a ClawBench session.
func GetExternalSessionID(sessionID string) string {
	var externalID string
	err := DBRead.QueryRow("SELECT external_session_id FROM chat_sessions WHERE id = ?", sessionID).Scan(&externalID)
	if err != nil {
		return ""
	}
	return externalID
}

// UnindexedMessage represents a chat message that has not yet been indexed by RAG.
type UnindexedMessage struct {
	ID          int64     `json:"id"`
	Content     string    `json:"content"`
	Role        string    `json:"role"`
	SessionID   string    `json:"session_id"`
	ProjectPath string    `json:"project_path"`
	Backend     string    `json:"backend"`
	CreatedAt   time.Time `json:"created_at"`
}

// GetUnindexedMessages fetches chat messages that have not been indexed by RAG.
// Returns up to limit messages ordered by creation time DESC (newest first).
func GetUnindexedMessages(limit int) ([]UnindexedMessage, error) {
	rows, err := DBRead.Query(
		"SELECT id, content, role, session_id, project_path, backend, created_at FROM chat_history WHERE indexed = 0 AND streaming = 0 ORDER BY created_at DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []UnindexedMessage
	for rows.Next() {
		var m UnindexedMessage
		if err := rows.Scan(&m.ID, &m.Content, &m.Role, &m.SessionID, &m.ProjectPath, &m.Backend, &m.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

// MarkMessageIndexed marks a chat message as indexed by RAG.
func MarkMessageIndexed(messageID int64) error {
	_, err := DB.Exec("UPDATE chat_history SET indexed = 1 WHERE id = ?", messageID)
	return err
}

// UnindexedCount returns the number of messages waiting to be indexed by RAG.
func UnindexedCount() (int, error) {
	var count int
	err := DBRead.QueryRow("SELECT COUNT(*) FROM chat_history WHERE indexed = 0 AND streaming = 0").Scan(&count)
	return count, err
}

// GetExpiredDeletedSessions returns session IDs of soft-deleted sessions
// whose updated_at (set to deletion time) is older than the cutoff.
func GetExpiredDeletedSessions(cutoff time.Time) ([]string, error) {
	rows, err := DBRead.Query("SELECT id FROM chat_sessions WHERE deleted = 1 AND updated_at < ?", cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// PurgeDeletedData hard-deletes soft-deleted sessions and their associated data.
// Deletes in order: ai_raw_responses → chat_history → chat_sessions.
// Returns counts of purged sessions and messages.
func PurgeDeletedData(sessionIDs []string) (sessionsPurged int64, messagesPurged int64, err error) {
	if len(sessionIDs) == 0 {
		return 0, 0, nil
	}

	tx, err := DB.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	// Build placeholders for IN clause: (?, ?, ...)
	placeholders := ""
	args := make([]any, len(sessionIDs))
	for i, id := range sessionIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args[i] = id
	}

	// Delete ai_raw_responses for these sessions
	_, _ = tx.Exec("DELETE FROM ai_raw_responses WHERE session_id IN ("+placeholders+")", args...)

	// Delete chat_tool_calls for these sessions
	_, _ = tx.Exec("DELETE FROM chat_tool_calls WHERE session_id IN ("+placeholders+")", args...)

	// Delete chat_history for these sessions (includes deleted messages)
	result, err := tx.Exec("DELETE FROM chat_history WHERE session_id IN ("+placeholders+")", args...)
	if err != nil {
		return 0, 0, err
	}
	messagesPurged, _ = result.RowsAffected()

	// Delete task_executions for purged scheduled sessions
	_, _ = tx.Exec("DELETE FROM task_executions WHERE session_id IN ("+placeholders+")", args...)

	// Delete the session records
	result, err = tx.Exec("DELETE FROM chat_sessions WHERE id IN ("+placeholders+") AND deleted = 1", args...)
	if err != nil {
		return 0, 0, err
	}
	sessionsPurged, _ = result.RowsAffected()

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return sessionsPurged, messagesPurged, nil
}

// HardDeleteSession removes a session and all its associated data regardless
// of deletion status. Used by ACP LoadSession to clean up existing sessions
// before recreating them with fresh replay data.
// Deletes in order: ai_raw_responses → chat_history → task_executions → chat_sessions.
func HardDeleteSession(sessionID string) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, _ = tx.Exec("DELETE FROM ai_raw_responses WHERE session_id = ?", sessionID)
	_, _ = tx.Exec("DELETE FROM chat_tool_calls WHERE session_id = ?", sessionID)
	_, _ = tx.Exec("DELETE FROM chat_history WHERE session_id = ?", sessionID)
	_, _ = tx.Exec("DELETE FROM task_executions WHERE session_id = ?", sessionID)
	_, err = tx.Exec("DELETE FROM chat_sessions WHERE id = ?", sessionID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// enrichMessagesWithSummaries populates the Summary field for assistant messages
// by batch-querying the summaries table. Only messages with role "assistant" are queried.
func enrichMessagesWithSummaries(messages []model.ChatMessage) {
	// Collect IDs of assistant messages
	var assistantIDs []int64
	for _, msg := range messages {
		if msg.Role == "assistant" {
			assistantIDs = append(assistantIDs, msg.ID)
		}
	}
	if len(assistantIDs) == 0 {
		return
	}

	// Batch query summaries for all assistant messages
	query := "SELECT target_id, summary FROM summaries WHERE target_type = 'chat_message' AND target_id IN ("
	args := make([]any, len(assistantIDs))
	for i, id := range assistantIDs {
		if i > 0 {
			query += ","
		}
		query += "?"
		args[i] = id
	}
	query += ")"

	rows, err := DBRead.Query(query, args...)
	if err != nil {
		return
	}
	defer rows.Close()

	// Build map of message ID -> summary
	summaryMap := make(map[int64]string)
	for rows.Next() {
		var targetID int64
		var summary string
		if err := rows.Scan(&targetID, &summary); err != nil {
			continue
		}
		summaryMap[targetID] = summary
	}

	// Enrich messages
	for i := range messages {
		if messages[i].Role == "assistant" {
			if summary, ok := summaryMap[messages[i].ID]; ok {
				messages[i].Summary = &summary
			}
		}
	}
}
