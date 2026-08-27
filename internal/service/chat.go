//nolint:errcheck,gocyclo,gosec,goconst,noctx,rowserrcheck // legacy file, nolint-only approach for diff stability
package service

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/platform"
)

// GetChatHistory retrieves all chat messages for a given project path, backend, and session.
// Returns full content (no stripping). Used by non-chat-panel callers (fork, RAG, etc.).
func GetChatHistory(projectPath, backend, sessionID string) ([]model.ChatMessage, error) {
	rows, err := dbRead.Query(
		"SELECT id, role, content, files, backend, streaming, created_at, indexed, queue_id, queued FROM chat_history WHERE project_path = ? AND session_id = ? ORDER BY id ASC",
		projectPath, sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := []model.ChatMessage{}
	for rows.Next() {
		var msg model.ChatMessage
		var filesJSON sql.NullString
		var streaming int
		var indexed int
		var queueID string
		var queued int
		if err := rows.Scan(&msg.ID, &msg.Role, &msg.Content, &filesJSON, &msg.Backend, &streaming, &msg.CreatedAt, &indexed, &queueID, &queued); err != nil {
			return nil, err
		}
		msg.Streaming = streaming != 0
		msg.Indexed = indexed != 0
		msg.QueueID = queueID
		msg.Queued = queued != 0
		if filesJSON.Valid && filesJSON.String != "" {
			msg.Files = unmarshalFilesJSON(filesJSON.String)
		}
		msg.SessionID = sessionID
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

// GetChatHistoryPaged retrieves chat messages with pagination.
// limit=0 means no limit (all messages).
// beforeID: if > 0, only return messages with id < beforeID (cursor-based for lazy load).
// When beforeID == 0 and limit > 0, returns the most recent (limit) messages.
// Returns messages in chronological (ASC) order.
// Also returns the total message count for the session and the count of queued
// messages (plan C) — the frontend subtracts queuedCount from total to compute
// hasMore without counting pending bubbles as loaded history.
func GetChatHistoryPaged(projectPath, backend, sessionID string, limit int, beforeID int) ([]model.ChatMessage, int, int, error) {
	messages := []model.ChatMessage{}
	totalCount := GetChatMessageCount(sessionID)
	queuedCount := GetQueuedCount(sessionID)

	if limit > 0 && beforeID > 0 {
		// Cursor-based: load messages older than beforeID
		query := `SELECT id, role, content, files, backend, streaming, created_at, indexed, queue_id, queued FROM (
			SELECT id, role, content, files, backend, streaming, created_at, indexed, queue_id, queued FROM chat_history
			WHERE project_path = ? AND session_id = ? AND id < ?
			ORDER BY id DESC LIMIT ?
		) sub ORDER BY id ASC`
		rows, err := dbRead.Query(query, projectPath, sessionID, beforeID, limit)
		if err != nil {
			return messages, totalCount, queuedCount, err
		}
		defer rows.Close()
		msgs, err := scanMessages(rows, sessionID)
		return msgs, totalCount, queuedCount, err
	}

	if limit > 0 {
		// Initial load: get the most recent (limit) messages
		query := `SELECT id, role, content, files, backend, streaming, created_at, indexed, queue_id, queued FROM (
			SELECT id, role, content, files, backend, streaming, created_at, indexed, queue_id, queued FROM chat_history
			WHERE project_path = ? AND session_id = ?
			ORDER BY id DESC LIMIT ?
		) sub ORDER BY id ASC`
		rows, err := dbRead.Query(query, projectPath, sessionID, limit)
		if err != nil {
			return messages, totalCount, queuedCount, err
		}
		defer rows.Close()
		msgs, err := scanMessages(rows, sessionID)
		return msgs, totalCount, queuedCount, err
	}

	// No limit: return all messages in chronological order
	query := `SELECT id, role, content, files, backend, streaming, created_at, indexed, queue_id, queued FROM chat_history WHERE project_path = ? AND session_id = ? ORDER BY id ASC`
	rows, err := dbRead.Query(query, projectPath, sessionID)
	if err != nil {
		return messages, totalCount, queuedCount, err
	}
	defer rows.Close()
	msgs, err := scanMessages(rows, sessionID)
	return msgs, totalCount, queuedCount, err
}

// scanMessages scans rows into ChatMessage slice, enriches with summaries,
// and strips heavy content from summarized non-streaming assistant messages.
func scanMessages(rows *sql.Rows, sessionID string) ([]model.ChatMessage, error) {
	messages := []model.ChatMessage{}
	for rows.Next() {
		var msg model.ChatMessage
		var filesJSON sql.NullString
		var streaming int
		var indexed int
		var queueID string
		var queued int
		if err := rows.Scan(&msg.ID, &msg.Role, &msg.Content, &filesJSON, &msg.Backend, &streaming, &msg.CreatedAt, &indexed, &queueID, &queued); err != nil {
			return nil, err
		}
		msg.Streaming = streaming != 0
		msg.Indexed = indexed != 0
		msg.QueueID = queueID
		msg.Queued = queued != 0
		if filesJSON.Valid && filesJSON.String != "" {
			msg.Files = unmarshalFilesJSON(filesJSON.String)
		}
		msg.SessionID = sessionID
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	enrichMessagesWithSummaries(messages)
	return messages, nil
}

// GetChatMessageCount returns the number of messages in a session (including streaming).
func GetChatMessageCount(sessionID string) int {
	var count int
	dbRead.QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sessionID).Scan(&count)
	return count
}

// GetFinalizedMessageCount returns the number of finalized (non-streaming) messages in a session.
// Used to determine whether a session has real content worth preserving for RAG.
func GetFinalizedMessageCount(sessionID string) int {
	var count int
	dbRead.QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND streaming = 0", sessionID).Scan(&count)
	return count
}

// GetUserMessageIndex returns lightweight {id, content, files, createdAt} for all user messages
// in a session, ordered by id ASC. Used for the user message index navigation feature.
// Excludes queued messages — a pending bubble is not a navigable history turn yet.
func GetUserMessageIndex(sessionID string) ([]model.ChatMessage, error) {
	rows, err := dbRead.Query(
		"SELECT id, content, files, created_at FROM chat_history WHERE session_id = ? AND role = 'user' AND streaming = 0 AND queued = 0 ORDER BY id ASC",
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
			msg.Files = unmarshalFilesJSON(filesJSON.String)
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
	err := dbRead.QueryRow("SELECT content FROM chat_history WHERE id = ? AND session_id = ?", id, sessionID).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return ExtractPlainText(content), nil
}

// IsMessageRole checks whether the message with the given ID in the session
// has the specified role.
func IsMessageRole(id int64, sessionID, role string) bool {
	var r string
	err := dbRead.QueryRow(
		"SELECT role FROM chat_history WHERE id = ? AND session_id = ?",
		id, sessionID,
	).Scan(&r)
	if err != nil {
		return false
	}
	return r == role
}

// GetPrecedingUserMessageContent returns the plain-text content of the last
// user message before the given message ID in the same session. Used for
// building fork titles when the fork point is an assistant message.
func GetPrecedingUserMessageContent(afterID int64, sessionID string) (string, error) {
	var content string
	err := dbRead.QueryRow(
		"SELECT content FROM chat_history WHERE session_id = ? AND role = 'user' AND streaming = 0 AND id < ? ORDER BY id DESC LIMIT 1",
		sessionID, afterID,
	).Scan(&content)
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

	err := dbRead.QueryRow(
		"SELECT id, role, content, files, backend, streaming, created_at, indexed, session_id, project_path FROM chat_history WHERE id = ?",
		id,
	).Scan(&msg.ID, &msg.Role, &msg.Content, &filesJSON, &msg.Backend, &streaming, &msg.CreatedAt, &indexed, &msg.SessionID, &msg.ProjectPath)
	if err != nil {
		return nil, err
	}
	msg.Streaming = streaming != 0
	msg.Indexed = indexed != 0
	if filesJSON.Valid && filesJSON.String != "" {
		msg.Files = unmarshalFilesJSON(filesJSON.String)
	}
	return &msg, nil
}

// GetMessagesBySessionID fetches all messages for a session by session_id alone.
// Unlike GetChatHistory, this does not require projectPath or backend — session_id is globally unique.
// Returns messages in chronological order. NOTE: assistant messages that have a
// reading summary are returned with content stripped to an empty blocks array
// (see enrichMessagesWithSummaries) to save bandwidth. Callers that need the
// real content blocks — e.g. push previews, fork context — must use
// GetAssistantRawContents instead.
// Excludes queued messages — callers (fork context, summarization, recent preview)
// want completed history, not messages still waiting for the drain loop (M4).
func GetMessagesBySessionID(sessionID string) ([]model.ChatMessage, error) {
	rows, err := dbRead.Query(
		"SELECT id, role, content, files, backend, streaming, created_at, indexed, queue_id, queued FROM chat_history WHERE session_id = ? AND streaming = 0 AND queued = 0 ORDER BY id ASC",
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows, sessionID)
}

// GetAssistantRawContents returns the raw (unmodified) content JSON of the
// most recent finalized assistant messages in a session, newest first
// (ORDER BY id DESC LIMIT previewAssistantContentLimit). Unlike
// GetMessagesBySessionID it does NOT go through scanMessages, whose
// enrichMessagesWithSummaries replaces the content of summarized non-streaming
// assistant messages with a stripped view (summarizeContentForView) to save
// bandwidth. Callers that need the real content blocks — e.g. push notification
// previews — must use this function.
func GetAssistantRawContents(sessionID string) ([]string, error) {
	rows, err := dbRead.Query(
		"SELECT content FROM chat_history WHERE session_id = ? AND role = 'assistant' AND streaming = 0 AND queued = 0 ORDER BY id DESC LIMIT ?",
		sessionID, previewAssistantContentLimit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var contents []string
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return nil, err
		}
		contents = append(contents, content)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return contents, nil
}

// unmarshalFilesJSON deserializes a files JSON column value, supporting both
// old format ["path1","path2"] and new format [{"path":"...","isDir":true/false}].
func unmarshalFilesJSON(raw string) []model.FileEntry {
	var entries []model.FileEntry
	if err := json.Unmarshal([]byte(raw), &entries); err == nil {
		return entries
	}
	// Fallback: old []string format
	var paths []string
	if err := json.Unmarshal([]byte(raw), &paths); err == nil {
		return model.FileEntriesFromPaths(paths)
	}
	return nil
}

// ExtractPlainText extracts plain text from message content, handling every
// storage format the system has produced. Content is stored in several shapes
// depending on the source (normal chat vs ACP session sync/replay) and on
// historical bugs that embedded raw JSON into text fields, so this function
// must not assume a single format.
//
// Recognized shapes:
//   - Plain text (e.g. "hello world") → returned unchanged.
//   - Block-format JSON ({"blocks":[{"type":"text","text":"..."}]}) → text of
//     all text blocks joined with "\n\n". The frontend extractPlainText joins
//     with a space for single-line previews — both valid for their contexts.
//   - Nested dirty data: a text block whose text field is itself a JSON string
//     (e.g. an ACP notification JSON or a content array serialized into text).
//     Recursively unwraps until real text is found.
//   - Bare content-array JSON ([{"type":"text","text":"..."}]).
//   - ACP notification wrapper ({"content":{"text":"hi","type":"text"},...,
//     "sessionUpdate":"user_message_chunk"}).
//
// Returns the original content unchanged for plain text or unrecognized JSON;
// returns "" for recognized wrappers (blocks/array/ACP notification) that
// carry no extractable text, so callers can distinguish "no content" from
// "not a wrapper" — matching the frontend extractPlainText semantics.
func ExtractPlainText(content string) string {
	if content == "" {
		return content
	}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return content
	}
	// Fast path: not JSON at all → plain text.
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return content
	}

	var raw any
	if json.Unmarshal([]byte(trimmed), &raw) != nil {
		return content
	}
	text := extractTextFromValue(raw, 0)
	if strings.TrimSpace(text) != "" {
		return text
	}
	if isKnownContentWrapper(raw) {
		return ""
	}
	return content
}

// isKnownContentWrapper reports whether the decoded JSON is a content wrapper
// owned by this system: a blocks array, a bare content array, an ACP
// notification, or a standalone content block.
func isKnownContentWrapper(v any) bool {
	switch val := v.(type) {
	case []any:
		return true
	case map[string]any:
		_, hasBlocks := val["blocks"]
		_, hasSessionUpdate := val["sessionUpdate"]
		_, hasText := val["text"]
		return hasBlocks || hasSessionUpdate || hasText
	default:
		return false
	}
}

// maxUnwrapDepth caps recursive unwrapping of nested JSON serializations.
// Real dirty data is ≤2–3 levels deep; the cap degrades pathologically nested
// JSON gracefully instead of recursing unboundedly.
const maxUnwrapDepth = 8

// extractTextFromValue recursively walks decoded JSON and pulls out the first
// meaningful text it can find, unwrapping known wrapper shapes:
//
//   - a JSON object with a "blocks" array (block-format content);
//   - a JSON object with "content" + "sessionUpdate" (an ACP notification that
//     was accidentally stored as text — historical dirty data);
//   - a JSON object with a "text" key (content-block text, possibly itself a
//     nested JSON string);
//   - a JSON array whose elements are text blocks / strings.
//
// This mirrors the block semantics of the rest of the system: only "text"
// content is meaningful for user-facing plain text; thinking/tool_use blocks
// are skipped.
func extractTextFromValue(v any, depth int) string {
	if depth > maxUnwrapDepth {
		return ""
	}
	switch val := v.(type) {
	case string:
		// A string may itself be an embedded JSON serialization (historical
		// dirty data). Unwrap it (propagating depth so the cap actually caps);
		// otherwise return as-is.
		trimmed := strings.TrimSpace(val)
		if trimmed != "" && (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) {
			var inner any
			if json.Unmarshal([]byte(trimmed), &inner) == nil {
				if nested := extractTextFromValue(inner, depth+1); strings.TrimSpace(nested) != "" {
					return nested
				}
			}
		}
		return val
	case map[string]any:
		// 1. {"blocks":[...]} — standard block content.
		if blocks, ok := val["blocks"]; ok {
			if arr, isArr := blocks.([]any); isArr {
				return joinExtractedTexts(extractTextsFromArray(arr, depth))
			}
		}
		// 2. ACP notification wrapper: {"content":{"text":"hi","type":"text"},...}.
		//    Historical bug stored the whole ACP notification JSON as text.
		if _, isAcp := val["sessionUpdate"]; isAcp {
			if contentVal, ok := val["content"]; ok {
				if s := extractTextFromValue(contentVal, depth+1); s != "" {
					return s
				}
			}
		}
		// 3. {"text":"..."} — a content block serialized by itself, or a text
		//    field inside a wrapper that wasn't matched above.
		if textVal, ok := val["text"]; ok {
			if s := extractTextFromValue(textVal, depth+1); s != "" {
				return s
			}
		}
		// 4. {"type":"text","text":"..."} maps already handled by #3; other
		//    object shapes (e.g. metadata) yield nothing.
		return ""
	case []any:
		return joinExtractedTexts(extractTextsFromArray(val, depth))
	default:
		return ""
	}
}

// extractTextsFromArray extracts text from each element of a JSON array,
// honoring the same "text only" semantics as block rendering. Each element may
// be a content block ({"type":"text","text":"..."}), a plain string, or a
// nested wrapper.
func extractTextsFromArray(arr []any, depth int) []string {
	var texts []string
	for _, el := range arr {
		switch elem := el.(type) {
		case map[string]any:
			typ, _ := elem["type"].(string)
			if typ != "" && typ != "text" {
				// thinking/tool_use/warning blocks don't carry user text.
				continue
			}
			if s := extractTextFromValue(elem, depth+1); s != "" {
				texts = append(texts, s)
			}
		case string:
			if s := extractTextFromValue(elem, depth+1); s != "" {
				texts = append(texts, s)
			}
		default:
			if s := extractTextFromValue(el, depth+1); s != "" {
				texts = append(texts, s)
			}
		}
	}
	return texts
}

// joinExtractedTexts joins multiple extracted texts with the same separator the
// rest of the system uses for multi-block content.
func joinExtractedTexts(texts []string) string {
	return strings.Join(texts, "\n\n")
}

// AddChatMessage adds a message to the chat history for a given project path, backend, and session.
// AddChatMessage persists a chat message. The optional queueID argument (when
// non-empty) records the queue_id of the queued user message that this reply
// answers. The frontend uses it to anchor the reply directly after its own
// question, because a queued user message is persisted (and gets its DB id)
// BEFORE later queued messages, so pure id ordering cannot reconstruct the
// conversational order (msg2, reply2, msg3, reply3) once multiple messages are
// queued at once.
func AddChatMessage(projectPath, backend, sessionID, role, content string, files []model.FileEntry, streaming bool, fallbackTitle string, queueID ...string) (int64, error) {
	replyQueueID := ""
	if len(queueID) > 0 {
		replyQueueID = queueID[0]
	}

	// Guard: reject messages to archived sessions
	var isArchived int
	if err := dbRead.QueryRow("SELECT archived FROM chat_sessions WHERE id = ?", sessionID).Scan(&isArchived); err == nil && isArchived == 1 {
		return 0, fmt.Errorf("cannot add message to archived session %s", sessionID)
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

	// Use transaction under write mutex to ensure data consistency
	var msgID int64
	tx, txErr := WriteBegin()
	if txErr != nil {
		return 0, txErr
	}
	defer writeMu.Unlock()
	defer tx.Rollback()

	result, txErr := tx.Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, files, streaming, indexed, queue_id) VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)",
		projectPath, backend, sessionID, role, content, filesJSON, streamingInt, replyQueueID,
	)
	if txErr != nil {
		return 0, txErr
	}

	// Update session's updated_at timestamp
	if _, txErr = tx.Exec("UPDATE chat_sessions SET updated_at = CURRENT_TIMESTAMP WHERE id = ?", sessionID); txErr != nil {
		return 0, txErr
	}

	// If this is the first user message, update session title
	if role == "user" {
		var count int
		if txErr = tx.QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sessionID).Scan(&count); txErr == nil && count == 1 {
			title := ExtractPlainText(content)
			if title == "" && len(files) > 0 {
				title = titleFromFileEntries(files)
			}
			if title == "" {
				title = fallbackTitle
			}
			runes := []rune(title)
			if len(runes) > 50 {
				title = string(runes[:50]) + "..."
			}
			if _, txErr = tx.Exec("UPDATE chat_sessions SET title = ? WHERE id = ?", title, sessionID); txErr != nil {
				return 0, txErr
			}
		}
	}

	if txErr := tx.Commit(); txErr != nil {
		return 0, txErr
	}

	msgID, _ = result.LastInsertId()
	slog.Info("chat: persisted message",
		slog.String("session", sessionID),
		slog.String("role", role),
		slog.Int64("msgID", msgID),
		slog.String("queueID", replyQueueID),
		slog.Bool("streaming", streaming))
	return msgID, nil
}

// AddQueuedMessage persists a user message to chat_history with queued=1 so it
// waits for the drain loop. It reuses AddChatMessage for the archived-session
// guard, session title generation on first message, and updated_at refresh
// (B3). The message is marked indexed=1 to skip RAG indexing until it is
// drained and finalized (M4).
func AddQueuedMessage(projectPath, backend, sessionID, content string, files []model.FileEntry, queueID string, fallbackTitle string) (int64, error) {
	if queueID == "" {
		queueID = "q-" + time.Now().Format("20060102150405") + "-" + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	msgID, err := AddChatMessage(projectPath, backend, sessionID, "user", content, files, false, fallbackTitle)
	if err != nil {
		return 0, err
	}
	if _, err := WriteExec(
		"UPDATE chat_history SET queue_id = ?, queued = 1, indexed = 1 WHERE id = ?",
		queueID, msgID,
	); err != nil {
		return 0, err
	}
	return msgID, nil
}

// DequeueQueuedMessage atomically claims the next queued message for a session
// (oldest first). Uses a transaction under the global write mutex so two
// concurrent drain loops can never consume the same row. The row stays in
// chat_history with queued=0 (it becomes a normal conversation record).
//
// Returns (msg, true, nil) on success, (zeroMsg, false, nil) when the queue is
// empty, and (zeroMsg, false, err) on a real DB error — the drain loop must
// treat the latter as a retryable failure, NOT as "queue empty", or the
// message is silently lost (B4).
func DequeueQueuedMessage(sessionID string) (model.ChatMessage, bool, error) {
	tx, err := WriteBegin()
	if err != nil {
		return model.ChatMessage{}, false, err
	}
	defer writeMu.Unlock()
	defer tx.Rollback()

	var msg model.ChatMessage
	var filesJSON sql.NullString
	var queueID string
	var queued int
	err = tx.QueryRow(`
		SELECT id, role, content, files, backend, created_at, queue_id, queued
		FROM chat_history WHERE session_id = ? AND queued = 1
		ORDER BY id ASC LIMIT 1
	`, sessionID).Scan(&msg.ID, &msg.Role, &msg.Content, &filesJSON, &msg.Backend, &msg.CreatedAt, &queueID, &queued)
	if err == sql.ErrNoRows {
		return model.ChatMessage{}, false, nil // genuinely empty
	}
	if err != nil {
		return model.ChatMessage{}, false, err // real DB error — retry, don't exit
	}

	// Claim the row: flip queued=0 and reset indexed=0 so the drained user
	// message becomes a normal conversation record eligible for RAG indexing
	// (it was set indexed=1 at enqueue to skip indexing while still queued — M4).
	res, err := tx.Exec("UPDATE chat_history SET queued = 0, indexed = 0 WHERE id = ? AND queued = 1", msg.ID)
	if err != nil {
		return model.ChatMessage{}, false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Already claimed by another drain loop — treat as empty for this call.
		return model.ChatMessage{}, false, nil
	}
	if err := tx.Commit(); err != nil {
		return model.ChatMessage{}, false, err
	}

	msg.SessionID = sessionID
	msg.QueueID = queueID
	msg.Queued = queued != 0
	if filesJSON.Valid && filesJSON.String != "" {
		msg.Files = unmarshalFilesJSON(filesJSON.String)
	}
	return msg, true, nil
}

// DequeueQueuedMessageByID atomically claims the queued message with the given
// id (the row just inserted by AddQueuedMessage). Same transaction semantics as
// DequeueQueuedMessage, but targets a specific row instead of "oldest first".
// Used to consume exactly the message an execution goroutine is about to run
// directly, so a concurrent enqueue's earlier row is left to the drain loop
// (R1).
func DequeueQueuedMessageByID(sessionID string, msgID int64) (model.ChatMessage, bool, error) {
	tx, err := WriteBegin()
	if err != nil {
		return model.ChatMessage{}, false, err
	}
	defer writeMu.Unlock()
	defer tx.Rollback()

	var msg model.ChatMessage
	var filesJSON sql.NullString
	var queueID string
	var queued int
	err = tx.QueryRow(`
		SELECT id, role, content, files, backend, created_at, queue_id, queued
		FROM chat_history WHERE session_id = ? AND id = ? AND queued = 1
	`, sessionID, msgID).Scan(&msg.ID, &msg.Role, &msg.Content, &filesJSON, &msg.Backend, &msg.CreatedAt, &queueID, &queued)
	if err == sql.ErrNoRows {
		return model.ChatMessage{}, false, nil // row not queued (already claimed/cleared)
	}
	if err != nil {
		return model.ChatMessage{}, false, err
	}

	res, err := tx.Exec("UPDATE chat_history SET queued = 0, indexed = 0 WHERE id = ? AND queued = 1", msg.ID)
	if err != nil {
		return model.ChatMessage{}, false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return model.ChatMessage{}, false, nil
	}
	if err := tx.Commit(); err != nil {
		return model.ChatMessage{}, false, err
	}

	msg.SessionID = sessionID
	msg.QueueID = queueID
	msg.Queued = queued != 0
	if filesJSON.Valid && filesJSON.String != "" {
		msg.Files = unmarshalFilesJSON(filesJSON.String)
	}
	return msg, true, nil
}

// ClearQueuedMessages marks every queued message of a session as consumed
// (queued=0). Used by session cancel/force-cancel — the rows stay in
// chat_history as normal conversation records.
func ClearQueuedMessages(sessionID string) error {
	_, err := WriteExec("UPDATE chat_history SET queued = 0 WHERE session_id = ? AND queued = 1", sessionID)
	return err
}

// GetQueuedQueueIDs returns the non-empty queue_ids of a session's queued
// messages, oldest first. Used to emit queue_cancel with the exact ids.
func GetQueuedQueueIDs(sessionID string) ([]string, error) {
	rows, err := dbRead.Query(
		"SELECT queue_id FROM chat_history WHERE session_id = ? AND queued = 1 AND queue_id != '' ORDER BY id ASC",
		sessionID,
	)
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

// GetQueuedCount returns the number of queued messages for a session.
func GetQueuedCount(sessionID string) int {
	var count int
	dbRead.QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND queued = 1", sessionID).Scan(&count)
	return count
}

// GetQueuedMessages returns the queued messages of a session, oldest first.
func GetQueuedMessages(sessionID string) ([]model.ChatMessage, error) {
	rows, err := dbRead.Query(
		"SELECT id, role, content, files, backend, streaming, created_at, indexed, queue_id, queued FROM chat_history WHERE session_id = ? AND queued = 1 ORDER BY id ASC",
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows, sessionID)
}

// CancelQueuedMessage marks a single queued message as consumed by queue_id.
func CancelQueuedMessage(sessionID, queueID string) error {
	_, err := WriteExec("UPDATE chat_history SET queued = 0 WHERE session_id = ? AND queue_id = ? AND queued = 1", sessionID, queueID)
	return err
}

// titleFromFileEntries builds a session title from file entries by extracting
// basenames and joining them with commas. Returns empty string if no files.
func titleFromFileEntries(files []model.FileEntry) string {
	if len(files) == 0 {
		return ""
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		name := filepath.Base(f.Path)
		if name != "" && name != "." {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return strings.Join(names, ", ")
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
	rows, err := dbRead.Query("SELECT project_path FROM recent_projects ORDER BY accessed_at DESC LIMIT ?", limit)
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
	_, err := WriteExec(
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
	_, err = WriteExec(
		"DELETE FROM recent_projects WHERE id NOT IN (SELECT id FROM recent_projects ORDER BY accessed_at DESC LIMIT ?)",
		limit,
	)
	return err
}

// RemoveRecentProject deletes a project path from the recent projects list.
// If the removed project was the default, its is_default flag is cleared first.
func RemoveRecentProject(projectPath string) error {
	_, _ = WriteExec("UPDATE recent_projects SET is_default = 0 WHERE project_path = ? AND is_default = 1", projectPath)
	_, err := WriteExec("DELETE FROM recent_projects WHERE project_path = ?", projectPath)
	return err
}

// GetDefaultProject returns the project path marked as default (is_default=1),
// or falls back to the most recently accessed project, or the user's home directory,
// or the first root path. This is the server-side source of truth for project selection.
// It does NOT update accessed_at (avoids the self-reinforcing loop).
func GetDefaultProject() (string, error) {
	// 1. Try is_default=1 row
	var path string
	err := dbRead.QueryRow("SELECT project_path FROM recent_projects WHERE is_default = 1 LIMIT 1").Scan(&path)
	if err == nil {
		// Verify directory still exists
		if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
			return path, nil
		}
		// Stale default — clear it
		_, _ = WriteExec("UPDATE recent_projects SET is_default = 0 WHERE is_default = 1")
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
	_, _ = WriteExec("UPDATE recent_projects SET is_default = 0 WHERE is_default = 1")
	// Ensure the project exists in recent_projects (upsert with accessed_at update)
	if err := AddRecentProject(projectPath); err != nil {
		return err
	}
	// Set the new default
	_, err := WriteExec("UPDATE recent_projects SET is_default = 1 WHERE project_path = ?", projectPath)
	return err
}

// generateSessionID generates a standard UUID v4 format session ID.
func generateSessionID() string {
	return generateUUID("", "chat_sessions", "id")
}

// GetSessions retrieves chat sessions for a given project path,
// ordered by created_at DESC (newest first; fixed order, unaffected by interaction).
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
		WHERE s.project_path = ? AND s.archived = 0 AND s.session_type = 'chat'`
	args := []interface{}{projectPath, projectPath}
	if backend != "" {
		query += " AND s.backend = ?"
		args = append(args, backend)
	}
	query += " ORDER BY s.created_at DESC, s.id DESC"

	rows, err := dbRead.Query(query, args...)
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

// GetSessionsPaged retrieves chat sessions with cursor-based pagination,
// ordered by created_at DESC (newest first; fixed order, unaffected by interaction).
// limit=0 means no limit (returns all sessions).
// cursor and cursorID: when non-empty, only return sessions with
//
//	(created_at < cursor) OR (created_at = cursor AND id < cursorID)
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
		WHERE s.project_path = ? AND s.archived = 0 AND s.session_type = 'chat'`
	args := []interface{}{projectPath, projectPath}
	if backend != "" {
		query += " AND s.backend = ?"
		args = append(args, backend)
	}
	if cursor != "" && cursorID != "" {
		query += " AND (s.created_at < ? OR (s.created_at = ? AND s.id < ?))"
		args = append(args, cursor, cursor, cursorID)
	}
	query += " ORDER BY s.created_at DESC, s.id DESC LIMIT ?"
	args = append(args, limit+1)

	rows, err := dbRead.Query(query, args...)
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
// Must run synchronously so that subsequent GetSessions queries (triggered by
// loadSessionsOnce after switchSession) see the updated last_read_at.
// Previously ran asynchronously (goroutine), which caused a race: the session
// list still showed unread messages after the user opened the session.
func UpdateLastRead(sessionID string) {
	WriteExec("UPDATE chat_sessions SET last_read_at = CURRENT_TIMESTAMP WHERE id = ?", sessionID)
}

// GetSessionBackend returns the backend of a session, or empty string if not found or archived.
func GetSessionBackend(sessionID string) string {
	var backend string
	err := dbRead.QueryRow("SELECT backend FROM chat_sessions WHERE id = ? AND archived = 0", sessionID).Scan(&backend)
	if err != nil {
		return ""
	}
	return backend
}

// GetSessionProjectPath returns the project path of a session, or empty string if not found.
func GetSessionProjectPath(sessionID string) string {
	var projectPath string
	err := dbRead.QueryRow("SELECT project_path FROM chat_sessions WHERE id = ? AND archived = 0", sessionID).Scan(&projectPath)
	if err != nil {
		return ""
	}
	return projectPath
}

// GetLatestSessionID returns the ID and backend of the most recently updated chat session
// for a project. Returns sql.ErrNoRows if no sessions exist.
func GetLatestSessionID(projectPath string) (sessionID, backend string, err error) {
	err = dbRead.QueryRow(
		`SELECT id, backend FROM chat_sessions
		 WHERE project_path = ? AND archived = 0 AND session_type = 'chat'
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
	err := dbRead.QueryRow(
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

// GetSessionModel returns the model ID of a session, or empty string if not found or archived.
func GetSessionModel(sessionID string) string {
	var modelID string
	err := dbRead.QueryRow("SELECT model FROM chat_sessions WHERE id = ? AND archived = 0", sessionID).Scan(&modelID)
	if err != nil {
		return ""
	}
	return modelID
}

// UpdateSessionModel updates the model field for a session.
// Called when the user selects a different model so that subsequent loads
// restore the user's choice instead of the agent default.
func UpdateSessionModel(sessionID, modelID string) error {
	_, err := WriteExec("UPDATE chat_sessions SET model = ? WHERE id = ?", modelID, sessionID)
	return err
}

// UpdateSessionTransport updates the transport field for a session.
func UpdateSessionTransport(sessionID, transport string) error {
	_, err := WriteExec("UPDATE chat_sessions SET transport = ? WHERE id = ?", transport, sessionID)
	return err
}

// GetSessionTransport returns the transport for a session, or empty string if not set.
func GetSessionTransport(sessionID string) string {
	var transport string
	err := dbRead.QueryRow("SELECT COALESCE(transport, '') FROM chat_sessions WHERE id = ? AND archived = 0", sessionID).Scan(&transport)
	if err != nil {
		return ""
	}
	return transport
}

// GetSessionAutoApprove returns whether auto-approve mode is enabled for a session.
func GetSessionAutoApprove(sessionID string) bool {
	var val int
	err := dbRead.QueryRow("SELECT auto_approve FROM chat_sessions WHERE id = ? AND archived = 0", sessionID).Scan(&val)
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
	_, err := WriteExec("UPDATE chat_sessions SET auto_approve = ? WHERE id = ?", val, sessionID)
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
	_, err := WriteExec(
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
	err := dbRead.QueryRow(
		"SELECT model FROM chat_sessions WHERE agent_id = ? AND project_path = ? AND archived = 0 AND model != '' ORDER BY updated_at DESC LIMIT 1",
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
	_, err := WriteExec(
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
	_, err := WriteExec("UPDATE chat_sessions SET source_session_id = ? WHERE id = ?", sourceSessionID, sessionID)
	return err
}

// UpdateSessionTitle updates the title of a chat session.
func UpdateSessionTitle(sessionID, title string) error {
	_, err := WriteExec("UPDATE chat_sessions SET title = ? WHERE id = ?", title, sessionID)
	return err
}

// ArchiveSession archives a chat session.
// Sets archived=1 on the session record and updates updated_at so it serves as the archive timestamp.
// Messages in chat_history are NOT archived — session-level archiving is sufficient
// since all message queries are scoped to sessions, and archived sessions are excluded.
// Data remains for RAG search but is hidden from UI; purged by cleanup worker after retention period.
func ArchiveSession(projectPath, backend, sessionID string) error {
	// Archive the session record, update timestamp to mark archive time.
	// backend param kept for API compatibility but not used in WHERE —
	// session ID (UUID) is already unique; filtering by backend could cause
	// silent no-op when the client sends a wrong/empty backend value.
	_, err := WriteExec("UPDATE chat_sessions SET archived = 1, updated_at = CURRENT_TIMESTAMP WHERE project_path = ? AND id = ?", projectPath, sessionID)
	return err
}

// GetSessionCount returns the number of chat sessions for a given project.
// Only counts sessions with session_type='chat' (excludes scheduled sessions).
func GetSessionCount(projectPath string) (int, error) {
	var count int
	err := dbRead.QueryRow("SELECT COUNT(*) FROM chat_sessions WHERE project_path = ? AND archived = 0 AND session_type = 'chat'", projectPath).Scan(&count)
	return count, err
}

// NextSessionNumber returns the auto-title number for a new unnamed session.
//
// The number is max(existing numbered unnamed-session titles) + 1, so the
// unnamed sessions in a project are numbered 1, 2, 3, ... based on the largest
// number currently in use. Explicitly-named sessions never affect it, and a
// number that still exists is never reused; once no numbered unnamed session
// remains the count resets to 1.
//
// baseTitle is the localized base auto-title (e.g. "新会话"). A session whose
// title matches "baseTitle N" is treated as unnamed with number N.
func NextSessionNumber(projectPath, baseTitle string) (int, error) {
	prefix := baseTitle + " "
	rows, err := dbRead.Query(
		`SELECT title FROM chat_sessions
		 WHERE project_path = ? AND archived = 0 AND session_type = 'chat'`,
		projectPath,
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	maxN := 0
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			return 0, err
		}
		if !strings.HasPrefix(title, prefix) {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(title, prefix))
		if err != nil {
			continue
		}
		if n > maxN {
			maxN = n
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return maxN + 1, nil
}

// RecentSession is a lightweight listing row used by session search's "browse
// all" mode (empty query): every chat session for the project, newest first,
// including archived ones that can still be resumed/restored. First* fields
// describe the session's first message, shown as the detail preview chunk.
type RecentSession struct {
	ID             string
	Title          string
	Backend        string
	ProjectPath    string
	Archived       bool
	CreatedAt      time.Time
	MessageCount   int
	FirstContent   string
	FirstRole      string
	FirstMessageID int64
	FirstCreatedAt time.Time
}

// GetRecentSessions returns all chat sessions for a project ordered newest-first
// by creation time, including archived ones. When projectPath is empty it
// returns sessions across all projects (CLI global browse). limit <= 0 returns
// all sessions.
func GetRecentSessions(projectPath string, limit int) ([]RecentSession, error) {
	query := `SELECT s.id, s.title, s.backend, s.project_path, s.archived, s.created_at,
		COUNT(h.id) AS message_count,
		(SELECT h2.content FROM chat_history h2 WHERE h2.session_id = s.id ORDER BY h2.created_at ASC, h2.id ASC LIMIT 1) AS first_content,
		(SELECT h2.role FROM chat_history h2 WHERE h2.session_id = s.id ORDER BY h2.created_at ASC, h2.id ASC LIMIT 1) AS first_role,
		(SELECT h2.id FROM chat_history h2 WHERE h2.session_id = s.id ORDER BY h2.created_at ASC, h2.id ASC LIMIT 1) AS first_message_id,
		(SELECT h2.created_at FROM chat_history h2 WHERE h2.session_id = s.id ORDER BY h2.created_at ASC, h2.id ASC LIMIT 1) AS first_created_at
		FROM chat_sessions s
		LEFT JOIN chat_history h ON h.session_id = s.id
		WHERE s.session_type = 'chat'`
	args := []interface{}{}
	if projectPath != "" {
		query += " AND s.project_path = ?"
		args = append(args, projectPath)
	}
	query += " GROUP BY s.id ORDER BY s.created_at DESC, s.id DESC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := dbRead.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := []RecentSession{}
	for rows.Next() {
		var s RecentSession
		var archived int
		var firstContent, firstRole sql.NullString
		var firstMessageID sql.NullInt64
		var firstCreatedAt sql.NullTime
		if err := rows.Scan(&s.ID, &s.Title, &s.Backend, &s.ProjectPath, &archived, &s.CreatedAt, &s.MessageCount,
			&firstContent, &firstRole, &firstMessageID, &firstCreatedAt); err != nil {
			return nil, err
		}
		s.Archived = archived != 0
		if firstContent.Valid {
			s.FirstContent = firstContent.String
		}
		if firstRole.Valid {
			s.FirstRole = firstRole.String
		}
		if firstMessageID.Valid {
			s.FirstMessageID = firstMessageID.Int64
		}
		if firstCreatedAt.Valid {
			s.FirstCreatedAt = firstCreatedAt.Time
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// GetSessionTitle returns the title of an active (non-archived) session.
func GetSessionTitle(sessionID string) (string, error) {
	var title string
	err := dbRead.QueryRow("SELECT title FROM chat_sessions WHERE id = ? AND archived = 0", sessionID).Scan(&title)
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

	rows, err := dbRead.Query("SELECT id, title FROM chat_sessions WHERE id IN ("+placeholders+") AND archived = 0", args...)
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

// GetSessionTitlesBatchIncludeArchived fetches titles for multiple sessions
// including archived ones. Used by RAG search to show titles even for
// archived sessions whose chunks are still indexed.
func GetSessionTitlesBatchIncludeArchived(sessionIDs []string) (map[string]string, error) {
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

	rows, err := dbRead.Query("SELECT id, title FROM chat_sessions WHERE id IN ("+placeholders+")", args...)
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

// ContextState holds persisted session context info (mode, thinking effort, usage)
// for restoring display after server restart. Stored as JSON in chat_sessions.context_state.
type ContextState struct {
	Mode           *ModeStatePersist      `json:"mode,omitempty"`
	ThinkingEffort *ThinkingEffortPersist `json:"thinkingEffort,omitempty"`
	Usage          *UsageStatePersist     `json:"usage,omitempty"`
}

// ModeStatePersist is the DB-persisted form of ai.ModeState.
type ModeStatePersist struct {
	CurrentModeID  string    `json:"currentModeId"`
	AvailableModes []ModeDef `json:"availableModes,omitempty"`
}

// ModeDef is a lightweight mode descriptor for DB persistence.
type ModeDef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// ThinkingEffortPersist is the DB-persisted form of ai.ThinkingEffortState.
type ThinkingEffortPersist struct {
	CurrentID       string              `json:"currentId"`
	AvailableLevels []ThinkingEffortDef `json:"availableLevels,omitempty"`
}

// ThinkingEffortDef is a lightweight thinking effort descriptor for DB persistence.
type ThinkingEffortDef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// UsageStatePersist is the DB-persisted form of ai.UsageState.
// Type alias ensures compile-time parity with ws.ContextStateUsage (also ai.UsageState)
// — both must serialize identical JSON shapes for DB/WS consistency.
type UsageStatePersist = ai.UsageState

// PersistContextStateFromEvent extracts context state from a StreamEvent
// and persists it to DB using atomic json_set() partial updates.
// This is called from SessionExecutor.forwardEvent so that mode, thinking effort,
// and usage state survive server restarts.
func PersistContextStateFromEvent(sessionID string, event ai.StreamEvent) {
	if sessionID == "" {
		return
	}
	switch event.Type {
	case "mode_update":
		if event.Mode == nil {
			return
		}
		modeJSON, err := json.Marshal(ModeStatePersist{
			CurrentModeID:  event.Mode.CurrentModeID,
			AvailableModes: convertModeDefsFromAI(event.Mode.AvailableModes),
		})
		if err != nil {
			slog.Warn("persist context state: marshal mode", "session", sessionID, "error", err)
			return
		}
		PatchContextStateMerge(sessionID, map[string]string{"mode": string(modeJSON)})

	case "thinking_effort_update":
		if event.ThinkingEffort == nil {
			return
		}
		effortJSON, err := json.Marshal(ThinkingEffortPersist{
			CurrentID:       event.ThinkingEffort.CurrentID,
			AvailableLevels: convertThinkingEffortDefsFromAI(event.ThinkingEffort.AvailableLevels),
		})
		if err != nil {
			slog.Warn("persist context state: marshal thinking effort", "session", sessionID, "error", err)
			return
		}
		PatchContextStateMerge(sessionID, map[string]string{"thinkingEffort": string(effortJSON)})

	case "usage_update":
		if event.Usage == nil {
			return
		}
		usageJSON, err := json.Marshal(UsageStatePersist{
			Used:              event.Usage.Used,
			Size:              event.Usage.Size,
			InputTokens:       event.Usage.InputTokens,
			OutputTokens:      event.Usage.OutputTokens,
			TotalTokens:       event.Usage.TotalTokens,
			CachedReadTokens:  event.Usage.CachedReadTokens,
			CachedWriteTokens: event.Usage.CachedWriteTokens,
			ThoughtTokens:     event.Usage.ThoughtTokens,
			Cost:              event.Usage.Cost,
			Currency:          event.Usage.Currency,
		})
		if err != nil {
			slog.Warn("persist context state: marshal usage", "session", sessionID, "error", err)
			return
		}
		PatchContextStateMerge(sessionID, map[string]string{"usage": string(usageJSON)})
	}
}

// convertModeDefsFromAI converts ai.ModeDef slices to service.ModeDef for DB persistence.
func convertModeDefsFromAI(modes []ai.ModeDef) []ModeDef {
	if len(modes) == 0 {
		return nil
	}
	result := make([]ModeDef, len(modes))
	for i, m := range modes {
		result[i] = ModeDef{ID: m.ID, Name: m.Name}
	}
	return result
}

// convertThinkingEffortDefsFromAI converts ai.ThinkingEffortDef slices to service.ThinkingEffortDef for DB persistence.
func convertThinkingEffortDefsFromAI(levels []ai.ThinkingEffortDef) []ThinkingEffortDef {
	if len(levels) == 0 {
		return nil
	}
	result := make([]ThinkingEffortDef, len(levels))
	for i, l := range levels {
		result[i] = ThinkingEffortDef{ID: l.ID, Name: l.Name}
	}
	return result
}

// SaveContextState persists the context_state JSON for a session.
// Best-effort: errors are logged but not returned, since losing context state
// is non-critical (the display will be restored once the ACP agent reconnects).
func SaveContextState(sessionID string, state *ContextState) {
	if state == nil || sessionID == "" {
		return
	}
	data, err := json.Marshal(state)
	if err != nil {
		slog.Warn("saveContextState: marshal failed", "err", err)
		return
	}
	if _, err := WriteExec("UPDATE chat_sessions SET context_state = ? WHERE id = ?", string(data), sessionID); err != nil {
		slog.Warn("saveContextState: write failed", "err", err, "sid", sessionID)
	}
}

// PatchContextStateMerge updates specific fields of the context_state JSON using
// SQLite json_set() for atomic partial updates. This avoids the read-merge-write
// race condition where concurrent mode+usage updates could overwrite each other.
// Each call only modifies the fields provided; other fields in the JSON remain intact.
// If the column is empty/NULL, json_set operates on a fresh '{}' object.
// Best-effort: errors are logged but not returned.
func PatchContextStateMerge(sessionID string, patches map[string]string) {
	if sessionID == "" || len(patches) == 0 {
		return
	}
	// Build json_set chain: json_set(context_state, '$.mode', json('...'), '$.usage', json('...'))
	// Start from '{}' if column is empty, so json_set works on a valid JSON object.
	query := "UPDATE chat_sessions SET context_state = json_set(CASE WHEN context_state = '' OR context_state IS NULL THEN '{}' ELSE context_state END"
	args := []any{}
	for key, val := range patches {
		query += fmt.Sprintf(", '$.%s', json(?)", key)
		args = append(args, val)
	}
	query += ") WHERE id = ?"
	args = append(args, sessionID)
	if _, err := WriteExec(query, args...); err != nil {
		slog.Warn("patchContextStateMerge: write failed", "err", err, "sid", sessionID)
	}
}

// GetContextState reads and parses the context_state JSON for a session.
// Returns nil if the column is empty or parsing fails.
func GetContextState(sessionID string) *ContextState {
	var raw string
	if err := dbRead.QueryRow("SELECT COALESCE(context_state, '') FROM chat_sessions WHERE id = ? AND archived = 0", sessionID).Scan(&raw); err != nil || raw == "" {
		return nil
	}
	var state ContextState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		slog.Warn("getContextState: unmarshal failed", "err", err, "sid", sessionID)
		return nil
	}
	return &state
}

// GetSessionInfo fetches session metadata (title, backend, agent_id, model, transport)
// in a single query instead of separate queries.
func GetSessionInfo(sessionID string) (*SessionInfo, error) {
	info := &SessionInfo{}
	err := dbRead.QueryRow(
		`SELECT title, backend, agent_id, model, COALESCE(transport, '')
		 FROM chat_sessions WHERE id = ? AND archived = 0`,
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
// Returns nil if the session is not found or archived.
func GetSessionFullInfo(sessionID string) *SessionInfo {
	info := &SessionInfo{}
	err := dbRead.QueryRow(
		`SELECT backend, project_path, title, agent_id, model, COALESCE(transport, ''), auto_approve
		 FROM chat_sessions WHERE id = ? AND archived = 0`,
		sessionID,
	).Scan(&info.Backend, &info.ProjectPath, &info.Title, &info.AgentID, &info.Model, &info.Transport, &info.AutoApprove)
	if err != nil {
		return nil
	}
	return info
}

// GetSessionAgentID returns the agent_id of an active (non-archived) session.
func GetSessionAgentID(sessionID string) string {
	var agentID string
	dbRead.QueryRow("SELECT agent_id FROM chat_sessions WHERE id = ? AND archived = 0", sessionID).Scan(&agentID)
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
	dbRead.QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND role = 'assistant' AND streaming = 0", sessionID).Scan(&count)
	return count
}

// UpdateStreamingMessage updates the content of the latest streaming assistant message for a session.
// Uses subquery with ORDER BY id DESC LIMIT 1 to target only the most recent streaming=1 row,
// preventing accidental updates to stale streaming rows left by failed finalizations.
func UpdateStreamingMessage(projectPath, backend, sessionID, content string) error {
	_, err := WriteExec(
		`UPDATE chat_history SET content = ? WHERE id = (
			SELECT id FROM chat_history
			WHERE project_path = ? AND backend = ? AND session_id = ? AND role = 'assistant' AND streaming = 1
			ORDER BY id DESC LIMIT 1
		)`,
		content, projectPath, backend, sessionID,
	)
	return err
}

// SessionHasRealAssistantContent checks whether a session has at least one
// finalized assistant message with real AI content (text, tool_use, or thinking
// blocks — not just a cancellation/error warning placeholder).
// Used by buildChatRequest to distinguish "first message interrupted before AI
// responded" from "stream interrupted after AI produced content".
func SessionHasRealAssistantContent(sessionID string) bool {
	var content string
	err := dbRead.QueryRow(
		"SELECT content FROM chat_history WHERE session_id = ? AND role = 'assistant' AND streaming = 0 ORDER BY id ASC LIMIT 1",
		sessionID,
	).Scan(&content)
	if err != nil || content == "" {
		return false
	}
	// Error messages stored by handler are plain text (not JSON blocks format).
	// They represent backend failures, not real AI content.
	if !strings.HasPrefix(strings.TrimSpace(content), "{") {
		return false
	}
	var parsed struct {
		Blocks []struct {
			Type string `json:"type"`
		} `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return false
	}
	for _, b := range parsed.Blocks {
		if b.Type != "warning" {
			return true
		}
	}
	return false
}

// FinalizeStreamingMessage marks the latest streaming assistant message as complete and updates its content.
// Also marks the message as unindexed (indexed=0) so the RAG indexer picks it up.
// Uses subquery with ORDER BY id DESC LIMIT 1 to target only the most recent streaming=1 row,
// preventing accidental finalization of stale streaming rows left by previous failed finalizations.
// Returns the message ID of the finalized message (0 if not found).
func FinalizeStreamingMessage(projectPath, backend, sessionID, content string) (int64, error) {
	result, err := WriteExec(
		`UPDATE chat_history SET content = ?, streaming = 0, indexed = 0 WHERE id = (
			SELECT id FROM chat_history
			WHERE project_path = ? AND backend = ? AND session_id = ? AND role = 'assistant' AND streaming = 1
			ORDER BY id DESC LIMIT 1
		)`,
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
	err = dbRead.QueryRow(
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
	err := dbRead.QueryRow(
		"SELECT id FROM chat_history WHERE session_id = ? AND role = 'assistant' AND streaming = 1 ORDER BY id DESC LIMIT 1",
		sessionID,
	).Scan(&id)
	if err == nil {
		return id
	}
	// Fallback: latest finalized message
	err = dbRead.QueryRow(
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
	_, err := WriteExec("UPDATE chat_history SET content = ? WHERE id = ?", content, messageID)
	return err
}

// PruneRawResponses keeps only the most recent maxRows rows in ai_raw_responses.
// Called at server startup to prevent unbounded growth of this debug-only table.
func PruneRawResponses(maxRows int) {
	if maxRows <= 0 {
		return
	}
	result, err := WriteExec(
		"DELETE FROM ai_raw_responses WHERE id NOT IN (SELECT id FROM ai_raw_responses ORDER BY id DESC LIMIT ?)",
		maxRows,
	)
	if err != nil {
		slog.Error("failed to prune ai_raw_responses", slog.String("err", err.Error()))
		return
	}
	if n, _ := result.RowsAffected(); n > 0 {
		slog.Info("pruned ai_raw_responses", slog.Int64("deleted", n), slog.Int("kept", maxRows))
	}
}

// SaveRawResponse saves the raw AI backend output for debugging/analysis.
// Called only after the AI response is fully complete.
func SaveRawResponse(sessionID, backend string, messageID int64, rawOutput string) error {
	_, err := WriteExec(
		"INSERT INTO ai_raw_responses (session_id, message_id, backend, raw_output) VALUES (?, ?, ?, ?)",
		sessionID, messageID, backend, rawOutput,
	)
	return err
}

// UpdateExternalSessionID sets the external session ID for a ClawBench session.
func UpdateExternalSessionID(sessionID, externalID string) error {
	_, err := WriteExec("UPDATE chat_sessions SET external_session_id = ? WHERE id = ?", externalID, sessionID)
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
	_, _ = WriteExec("UPDATE chat_sessions SET external_session_id = '' WHERE id = ?", sessionID)
}

// GetExternalSessionID returns the external session ID for a ClawBench session.
func GetExternalSessionID(sessionID string) string {
	var externalID string
	err := dbRead.QueryRow("SELECT external_session_id FROM chat_sessions WHERE id = ?", sessionID).Scan(&externalID)
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
	rows, err := dbRead.Query(
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
	_, err := WriteExec("UPDATE chat_history SET indexed = 1 WHERE id = ?", messageID)
	return err
}

// MarkMessagesIndexed marks multiple chat messages as indexed by RAG in a single query.
func MarkMessagesIndexed(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	_, err := WriteExec("UPDATE chat_history SET indexed = 1 WHERE id IN ("+placeholders+")", args...)
	return err
}

// ResetAllIndexed resets all chat messages' indexed flag back to 0,
// so the RAG indexer will re-index them from scratch.
// Streaming (in-progress) messages are intentionally excluded because
// FinalizeStreamingMessage always sets indexed=0 upon completion,
// so they will be picked up by the indexer naturally.
func ResetAllIndexed() (int64, error) {
	result, err := WriteExec("UPDATE chat_history SET indexed = 0 WHERE streaming = 0")
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// UnindexedCount returns the number of messages waiting to be indexed by RAG.
func UnindexedCount() (int, error) {
	var count int
	err := dbRead.QueryRow("SELECT COUNT(*) FROM chat_history WHERE indexed = 0 AND streaming = 0").Scan(&count)
	return count, err
}

// TotalMessageCount returns the total number of finalized (non-streaming) messages.
func TotalMessageCount() (int, error) {
	var count int
	err := dbRead.QueryRow("SELECT COUNT(*) FROM chat_history WHERE streaming = 0").Scan(&count)
	return count, err
}

// IndexedMessageCount returns the number of messages that have been indexed by RAG.
func IndexedMessageCount() (int, error) {
	var count int
	err := dbRead.QueryRow("SELECT COUNT(*) FROM chat_history WHERE indexed = 1 AND streaming = 0").Scan(&count)
	return count, err
}

// MessageIndexCounts returns (total, indexed) message counts in a single query.
func MessageIndexCounts() (total int, indexed int, err error) {
	err = dbRead.QueryRow(
		"SELECT COUNT(*), COALESCE(SUM(CASE WHEN indexed = 1 THEN 1 ELSE 0 END), 0) FROM chat_history WHERE streaming = 0",
	).Scan(&total, &indexed)
	return
}

// GetExpiredArchivedSessions returns session IDs of archived sessions
// whose updated_at (set to archive time) is older than the cutoff.
func GetExpiredArchivedSessions(cutoff time.Time) ([]string, error) {
	rows, err := dbRead.Query("SELECT id FROM chat_sessions WHERE archived = 1 AND updated_at < ?", cutoff)
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

// PurgeArchivedData hard-deletes archived sessions and their associated data.
// Deletes in order: ai_raw_responses → chat_tool_calls → summaries →
// tts_summaries → chat_history → task_executions → chat_sessions.
// Returns counts of purged sessions and messages.
func PurgeArchivedData(sessionIDs []string) (sessionsPurged int64, messagesPurged int64, err error) {
	if len(sessionIDs) == 0 {
		return 0, 0, nil
	}

	tx, err := WriteBegin()
	if err != nil {
		return 0, 0, err
	}
	defer writeMu.Unlock()
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

	// Delete chat_thinking for these sessions
	_, _ = tx.Exec("DELETE FROM chat_thinking WHERE session_id IN ("+placeholders+")", args...)

	// Delete summaries and tts_summaries before chat_history (they reference chat_history.id)
	_, _ = tx.Exec("DELETE FROM summaries WHERE target_type = 'chat_message' AND target_id IN (SELECT id FROM chat_history WHERE session_id IN ("+placeholders+"))", args...)
	_, _ = tx.Exec("DELETE FROM tts_summaries WHERE message_id IN (SELECT id FROM chat_history WHERE session_id IN ("+placeholders+"))", args...)

	// Delete chat_history for these sessions (includes archived sessions' messages)
	result, err := tx.Exec("DELETE FROM chat_history WHERE session_id IN ("+placeholders+")", args...)
	if err != nil {
		return 0, 0, err
	}
	messagesPurged, _ = result.RowsAffected()

	// Delete task_executions for purged scheduled sessions
	_, _ = tx.Exec("DELETE FROM task_executions WHERE session_id IN ("+placeholders+")", args...)

	// Delete the session records
	result, err = tx.Exec("DELETE FROM chat_sessions WHERE id IN ("+placeholders+") AND archived = 1", args...)
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
// before recreating them with fresh replay data, and by DestroySession for
// user-initiated permanent deletion.
// Deletes in order: ai_raw_responses → chat_tool_calls → summaries →
// tts_summaries → chat_history → task_executions → chat_sessions.
func HardDeleteSession(sessionID string) error {
	tx, err := WriteBegin()
	if err != nil {
		return err
	}
	defer writeMu.Unlock()
	defer tx.Rollback()

	_, _ = tx.Exec("DELETE FROM ai_raw_responses WHERE session_id = ?", sessionID)
	_, _ = tx.Exec("DELETE FROM chat_tool_calls WHERE session_id = ?", sessionID)
	_, _ = tx.Exec("DELETE FROM chat_thinking WHERE session_id = ?", sessionID)
	// Delete summaries and tts_summaries before chat_history (they reference chat_history.id)
	_, _ = tx.Exec("DELETE FROM summaries WHERE target_type = 'chat_message' AND target_id IN (SELECT id FROM chat_history WHERE session_id = ?)", sessionID)
	_, _ = tx.Exec("DELETE FROM tts_summaries WHERE message_id IN (SELECT id FROM chat_history WHERE session_id = ?)", sessionID)
	_, _ = tx.Exec("DELETE FROM chat_history WHERE session_id = ?", sessionID)
	_, _ = tx.Exec("DELETE FROM task_executions WHERE session_id = ?", sessionID)
	_, err = tx.Exec("DELETE FROM chat_sessions WHERE id = ?", sessionID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ReplayMessage is a single message from a LoadSession replay, ready to persist.
type ReplayMessage struct {
	Role      string
	Content   string // JSON: {"blocks":[...], "metadata":{...}}
	ExtMsgID  string // external ACP messageId
	ToolCalls []model.ContentBlock
}

// ReplaceSessionHistory atomically replaces a session's chat history with the
// given messages (and their tool calls). It deletes the session's prior history
// and child rows (tool calls, thinking, summaries, raw responses) then inserts
// the new messages, all in one transaction — on any error the transaction rolls
// back so the original history is preserved. Returns the number of messages
// inserted.
func ReplaceSessionHistory(sessionID, projectPath, backend string, messages []ReplayMessage) (int, error) {
	tx, err := WriteBegin()
	if err != nil {
		return 0, err
	}
	defer writeMu.Unlock()
	defer tx.Rollback()

	_, _ = tx.Exec("DELETE FROM ai_raw_responses WHERE session_id = ?", sessionID)
	_, _ = tx.Exec("DELETE FROM chat_tool_calls WHERE session_id = ?", sessionID)
	_, _ = tx.Exec("DELETE FROM chat_thinking WHERE session_id = ?", sessionID)
	_, _ = tx.Exec("DELETE FROM summaries WHERE target_type = 'chat_message' AND target_id IN (SELECT id FROM chat_history WHERE session_id = ?)", sessionID)
	_, _ = tx.Exec("DELETE FROM tts_summaries WHERE message_id IN (SELECT id FROM chat_history WHERE session_id = ?)", sessionID)
	if _, err := tx.Exec("DELETE FROM chat_history WHERE session_id = ?", sessionID); err != nil {
		return 0, err
	}

	for _, m := range messages {
		res, err := tx.Exec(
			"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming, indexed, external_message_id) VALUES (?, ?, ?, ?, ?, 0, 0, ?)",
			projectPath, backend, sessionID, m.Role, m.Content, m.ExtMsgID,
		)
		if err != nil {
			return 0, err
		}
		msgID, _ := res.LastInsertId()
		for i := range m.ToolCalls {
			tc := &m.ToolCalls[i]
			inputJSON, _ := json.Marshal(tc.Input)
			// Inline the tool-call upsert on tx (not UpsertToolCall, which acquires
			// writeMu and writes via the global db handle — both would deadlock and
			// break the transaction's atomicity).
			if _, err := tx.Exec(`
				INSERT INTO chat_tool_calls (message_id, session_id, tool_id, name, input, output, status, done, summary, duration_ms)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(tool_id, message_id) DO UPDATE SET
					input = excluded.input,
					output = CASE WHEN excluded.output != '' THEN excluded.output ELSE chat_tool_calls.output END,
					status = excluded.status,
					done = excluded.done,
					summary = excluded.summary,
					duration_ms = CASE WHEN excluded.duration_ms > 0 THEN excluded.duration_ms ELSE chat_tool_calls.duration_ms END
			`, msgID, sessionID, tc.ID, tc.Name, string(inputJSON), tc.Output, tc.Status, tc.Done, tc.Summary, tc.DurationMs); err != nil {
				slog.Warn("service: failed to persist replay tool call", "session_id", sessionID, "tool_id", tc.ID, "error", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(messages), nil
}

// summarizeContentForView strips the heavy blocks from assistant message content
// but preserves the metadata (and cancelled flag) so the frontend message-detail
// panel can still show model/token/cost/duration/session info for summarized
// messages in summary view. Returns "" when content isn't parseable JSON
// (matching the previous empty-content behavior).
func summarizeContentForView(content string) string {
	var parsed struct {
		Metadata  json.RawMessage `json:"metadata"`
		Cancelled bool            `json:"cancelled"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return ""
	}
	out := map[string]any{"blocks": []any{}}
	if len(parsed.Metadata) > 0 && string(parsed.Metadata) != "null" {
		out["metadata"] = parsed.Metadata
	}
	if parsed.Cancelled {
		out["cancelled"] = true
	}
	b, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(b)
}

// enrichMessagesWithSummaries populates the Summary and SummaryCards fields for
// assistant messages by batch-querying the summaries table. Only messages with
// role "assistant" are queried. The heavy content of messages that have a
// reading summary and are not streaming is stripped to save bandwidth.
func enrichMessagesWithSummaries(messages []model.ChatMessage) {
	// Collect IDs of assistant messages
	assistantIDs := make([]int64, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "assistant" {
			assistantIDs = append(assistantIDs, msg.ID)
		}
	}
	if len(assistantIDs) == 0 {
		return
	}

	// Batch query summaries for all assistant messages
	query := "SELECT target_id, summary, COALESCE(summary_cards, '') FROM summaries WHERE target_type = 'chat_message' AND target_id IN ("
	args := make([]any, len(assistantIDs))
	for i, id := range assistantIDs {
		if i > 0 {
			query += ","
		}
		query += "?"
		args[i] = id
	}
	query += ")"

	rows, err := dbRead.Query(query, args...)
	if err != nil {
		return
	}
	defer rows.Close()

	// Build map of message ID -> summary
	summaryMap := make(map[int64]string)
	cardMap := make(map[int64]*model.SummaryCards)
	for rows.Next() {
		var targetID int64
		var summary string
		var cardsJSON string
		if err := rows.Scan(&targetID, &summary, &cardsJSON); err != nil {
			continue
		}
		summaryMap[targetID] = summary
		if cardsJSON != "" {
			var cards model.SummaryCards
			if jerr := json.Unmarshal([]byte(cardsJSON), &cards); jerr == nil {
				cardMap[targetID] = &cards
			}
		}
	}

	// Enrich messages
	for i := range messages {
		if messages[i].Role == "assistant" {
			if summary, ok := summaryMap[messages[i].ID]; ok {
				messages[i].Summary = &summary
			}
			if cards, ok := cardMap[messages[i].ID]; ok {
				messages[i].SummaryCards = cards
			}
			if messages[i].Summary != nil && *messages[i].Summary != "" && !messages[i].Streaming {
				messages[i].Content = summarizeContentForView(messages[i].Content)
			}
		}
	}

	// Backfill: for non-streaming assistant messages that have no summary,
	// trigger async summarization so the next loadHistory returns summaries.
	go backfillMissingSummaries(assistantIDs, summaryMap)
}

// backfillMissingSummaries triggers async summarization for assistant messages
// that lack a reading summary. This heals historical data from the period when
// triggerChatSummarization was never called (skipEvent=true in all
// SetSessionRunning(false) callers).
//
// This function reads message content from DB (not from the messages slice) to
// avoid a data race: the caller's enrichMessagesWithSummaries may have already
// replaced msg.Content with the stripped summary-view version, and the HTTP
// handler may be concurrently serializing the messages slice as JSON.
func backfillMissingSummaries(assistantIDs []int64, summaryMap map[int64]string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("backfillMissingSummaries panic", slog.Any("err", r))
		}
	}()
	if dbRead == nil {
		return
	}
	for _, id := range assistantIDs {
		if _, found := summaryMap[id]; found {
			continue // already has a summary
		}
		// Read original content from DB to avoid data race with enrichMessagesWithSummaries
		// (which may have stripped content for summary view) and the HTTP handler
		// (which may be concurrently reading the messages slice).
		var content, sessionID string
		if err := dbRead.QueryRow(
			"SELECT content, session_id FROM chat_history WHERE id = ? AND streaming = 0",
			id,
		).Scan(&content, &sessionID); err != nil {
			continue
		}
		blocks, err := parseMessageBlocks(content)
		if err != nil || len(blocks) == 0 {
			continue
		}
		projectPath := GetSessionProjectPath(sessionID)
		summarizeMessageOnce(id, blocks, projectPath, sessionID)
	}
}
