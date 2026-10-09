//nolint:errcheck,gocyclo,gosec,goconst,noctx,rowserrcheck // legacy file, nolint-only approach for diff stability
package service

import (
	"context"
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
	"unicode/utf8"

	"clawbench/internal/store"

	"clawbench/internal/ai"
	"clawbench/internal/grouprouting"
	"clawbench/internal/model"
	"clawbench/internal/platform"
	"clawbench/internal/ws"
)

// GetChatHistory retrieves all chat messages for a given project path, backend, and session.
// Returns full content (no stripping). Used by non-chat-panel callers (fork, RAG, etc.).
func GetChatHistory(projectPath, backend, sessionID string) ([]model.ChatMessage, error) {
	projectID, err := store.ProjectIDForPath(projectPath)
	if err != nil {
		return nil, err
	}
	rows, err := store.ReadDB().Query(
		"SELECT id, role, content, files, backend, streaming, created_at, indexed, agent_id FROM chat_history WHERE project_id = ? AND session_id = ? ORDER BY id ASC",
		projectID, sessionID,
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
		if err := rows.Scan(&msg.ID, &msg.Role, &msg.Content, &filesJSON, &msg.Backend, &streaming, &msg.CreatedAt, &indexed, &msg.AgentID); err != nil {
			return nil, err
		}
		msg.Streaming = streaming != 0
		msg.Indexed = indexed != 0
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
// Also returns the total message count for the session. Queued messages are NOT
// part of chat_history any more (they live in queued_messages until dequeued),
// so the total and the returned rows are both pure conversation history — the
// caller no longer has to subtract a queued count to compute hasMore.
func GetChatHistoryPaged(projectPath, backend, sessionID string, limit int, beforeID int) ([]model.ChatMessage, int, error) {
	projectID, idErr := store.ProjectIDForPath(projectPath)
	if idErr != nil {
		return []model.ChatMessage{}, 0, idErr
	}
	messages := []model.ChatMessage{}
	totalCount, countErr := GetChatMessageCount(sessionID)
	if countErr != nil {
		// A count failure must not silently truncate pagination: fall back to
		// "unknown total" (0). Callers that only use the count for hasMore treat
		// 0 as "load everything", so nothing is lost; callers that expose the
		// total to the UI surface it as 0 alongside the returned query error.
		slog.Warn("GetChatHistoryPaged: GetChatMessageCount failed", "session_id", sessionID, "err", countErr)
		totalCount = 0
	}

	if limit > 0 && beforeID > 0 {
		// Cursor-based: load messages older than beforeID
		query := `SELECT id, role, content, files, backend, streaming, created_at, indexed, agent_id FROM (
			SELECT id, role, content, files, backend, streaming, created_at, indexed, agent_id FROM chat_history
			WHERE project_id = ? AND session_id = ? AND id < ?
			ORDER BY id DESC LIMIT ?
		) sub ORDER BY id ASC`
		rows, err := store.ReadDB().Query(query, projectID, sessionID, beforeID, limit)
		if err != nil {
			return messages, totalCount, err
		}
		defer rows.Close()
		msgs, err := scanMessages(rows, sessionID)
		return msgs, totalCount, err
	}

	if limit > 0 {
		// Initial load: get the most recent (limit) messages
		query := `SELECT id, role, content, files, backend, streaming, created_at, indexed, agent_id FROM (
			SELECT id, role, content, files, backend, streaming, created_at, indexed, agent_id FROM chat_history
			WHERE project_id = ? AND session_id = ?
			ORDER BY id DESC LIMIT ?
		) sub ORDER BY id ASC`
		rows, err := store.ReadDB().Query(query, projectID, sessionID, limit)
		if err != nil {
			return messages, totalCount, err
		}
		defer rows.Close()
		msgs, err := scanMessages(rows, sessionID)
		return msgs, totalCount, err
	}

	// No limit: return all messages in chronological order
	query := `SELECT id, role, content, files, backend, streaming, created_at, indexed, agent_id FROM chat_history WHERE project_id = ? AND session_id = ? ORDER BY id ASC`
	rows, err := store.ReadDB().Query(query, projectID, sessionID)
	if err != nil {
		return messages, totalCount, err
	}
	defer rows.Close()
	msgs, err := scanMessages(rows, sessionID)
	return msgs, totalCount, err
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
		if err := rows.Scan(&msg.ID, &msg.Role, &msg.Content, &filesJSON, &msg.Backend, &streaming, &msg.CreatedAt, &indexed, &msg.AgentID); err != nil {
			return nil, err
		}
		msg.Streaming = streaming != 0
		msg.Indexed = indexed != 0
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
func GetChatMessageCount(sessionID string) (int, error) {
	var count int
	if err := store.ReadDB().QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sessionID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// GetFinalizedMessageCount returns the number of finalized (non-streaming) messages in a session.
// Used to determine whether a session has real content worth preserving for RAG.
func GetFinalizedMessageCount(sessionID string) (int, error) {
	var count int
	if err := store.ReadDB().QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND streaming = 0", sessionID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// indexSummaryMaxRunes caps the fallback preview derived from an assistant
// reply's own text when no reading summary has been stored yet. It is
// deliberately shorter than model.ResponsePreviewMaxRunes: the index row is a
// one-line scannable preview, and the frontend truncates to 100 code points for
// display anyway — this just bounds what crosses the wire.
const indexSummaryMaxRunes = 200

// GetConversationIndex returns lightweight rows for every finalized message in a
// session — both user messages and assistant replies — ordered by id ASC, for the
// conversation index navigation feature.
//
// User rows carry {id, role, content, files, createdAt}. Assistant rows carry
// {id, role, summary, createdAt}: the stored reading summary when one exists,
// otherwise a preview extracted from the reply's own conclusion so a row is
// never blank for a reply that does have text. Only the fallback path reads the
// (potentially large) assistant content column.
//
// Excludes in-flight (streaming) rows, whose content is still being written.
// Queued messages are absent by construction: they live in queued_messages
// until they are dequeued into chat_history.
func GetConversationIndex(sessionID string) ([]model.ChatMessage, error) {
	// Resolve the session's group-chat policy ONCE, before the rows cursor is
	// opened: IsGroupSession issues its own query, and calling it inside the
	// loop would need a second read connection while the cursor still holds the
	// first (deadlock on the 1-connection test pool, needless contention in prod).
	isGroup := IsGroupSession(sessionID)
	rows, err := store.ReadDB().Query(
		`SELECT h.id, h.role, h.content, h.files, h.created_at, COALESCE(s.summary, ''), COALESCE(h.agent_id, '')
		 FROM chat_history h
		 LEFT JOIN summaries s ON s.target_type = 'chat_message' AND s.target_id = h.id
		 WHERE h.session_id = ? AND h.streaming = 0
		 ORDER BY h.id ASC`,
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
		var summary string
		if err := rows.Scan(&msg.ID, &msg.Role, &msg.Content, &filesJSON, &msg.CreatedAt, &summary, &msg.AgentID); err != nil {
			return nil, err
		}
		if msg.Role == roleAssistant {
			// The assistant's content column is the heavy one (full block JSON);
			// replace it with the summary text before it leaves this function so
			// the response stays lightweight. A stored summary wins; otherwise
			// derive a bounded preview from the reply's conclusion.
			text := summary
			if text == "" {
				if blocks, perr := parseMessageBlocks(msg.Content); perr == nil && len(blocks) > 0 {
					text = assistantConclusionWithPolicy(isGroup, blocks)
				} else {
					// Plain-text assistant content (no block JSON) — use it as-is.
					text = ExtractPlainText(msg.Content)
				}
			}
			msg.Content = ""
			preview := truncateIndexText(text, indexSummaryMaxRunes)
			msg.Summary = &preview
		} else if filesJSON.Valid && filesJSON.String != "" {
			msg.Files = unmarshalFilesJSON(filesJSON.String)
		}
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}

// truncateIndexText caps text at maxRunes, appending an ellipsis when anything
// was cut. Operates on runes so multi-byte characters are never split.
func truncateIndexText(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	if utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	return string([]rune(text)[:maxRunes]) + "…"
}

// GetMessageContent returns the plain text content of a message by its ID,
// scoped to the specified session. Returns empty string if not found.
func GetMessageContent(id int64, sessionID string) (string, error) {
	var content string
	err := store.ReadDB().QueryRow("SELECT content FROM chat_history WHERE id = ? AND session_id = ?", id, sessionID).Scan(&content)
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
	err := store.ReadDB().QueryRow(
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
	err := store.ReadDB().QueryRow(
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

	// project_path is resolved through projects so the wire type keeps carrying a
	// path even though the column is now an id.
	err := store.ReadDB().QueryRow(
		`SELECT h.id, h.role, h.content, h.files, h.backend, h.streaming, h.created_at, h.indexed, h.session_id,
		        COALESCE(p.path, ''), h.agent_id
		   FROM chat_history h
		   LEFT JOIN projects p ON p.id = h.project_id
		  WHERE h.id = ?`,
		id,
	).Scan(&msg.ID, &msg.Role, &msg.Content, &filesJSON, &msg.Backend, &streaming, &msg.CreatedAt, &indexed, &msg.SessionID, &msg.ProjectPath, &msg.AgentID)
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
// Callers (fork context, summarization, recent preview) want completed history;
// queued messages are absent by construction (they live in queued_messages
// until dequeued), so no queued filter is needed.
func GetMessagesBySessionID(sessionID string) ([]model.ChatMessage, error) {
	rows, err := store.ReadDB().Query(
		"SELECT id, role, content, files, backend, streaming, created_at, indexed, agent_id FROM chat_history WHERE session_id = ? AND streaming = 0 ORDER BY id ASC",
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows, sessionID)
}

// GetMessagesBySessionIDRaw fetches all finalized messages for a session by
// session_id alone, with the same query as GetMessagesBySessionID but WITHOUT
// the summary-based content stripping (no scanMessages / enrichMessagesWithSummaries).
// It preserves the real content blocks of assistant messages that have a reading
// summary. Callers that need the full history — e.g. fork context injection —
// must use this function.
func GetMessagesBySessionIDRaw(sessionID string) ([]model.ChatMessage, error) {
	rows, err := store.ReadDB().Query(
		"SELECT id, role, content, files, backend, streaming, created_at, indexed, agent_id FROM chat_history WHERE session_id = ? AND streaming = 0 ORDER BY id ASC",
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
		var streaming int
		var indexed int
		if err := rows.Scan(&msg.ID, &msg.Role, &msg.Content, &filesJSON, &msg.Backend, &streaming, &msg.CreatedAt, &indexed, &msg.AgentID); err != nil {
			return nil, err
		}
		msg.Streaming = streaming != 0
		msg.Indexed = indexed != 0
		if filesJSON.Valid && filesJSON.String != "" {
			msg.Files = unmarshalFilesJSON(filesJSON.String)
		}
		msg.SessionID = sessionID
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
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
	rows, err := store.ReadDB().Query(
		"SELECT content FROM chat_history WHERE session_id = ? AND role = 'assistant' AND streaming = 0 ORDER BY id DESC LIMIT ?",
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

// jsonNull is the string form of the JSON null literal. Columns holding a JSON
// blob written by json.Marshal of a nil slice render as "null"; callers compare
// against this constant to treat that as an absent/empty value.
const jsonNull = "null"

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
//
//nolint:gocognit // recursive JSON unwrap enumerates wrapper shapes by design
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
			if contentVal, ok := val[contentKeyContent]; ok {
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

// AddChatMessage adds a message to the chat history for a given project path,
// backend, and session.
//
// Ordering note: a queued user message is NOT inserted here at enqueue time any
// more. It is materialized by MaterializeQueuedMessage only when the drain loop
// dequeues it (or mid-turn injection delivers it), which is immediately before
// its assistant reply is created. DB id order therefore equals conversational
// order, and replies need no anchor.
func AddChatMessage(projectPath, backend, sessionID, role, content string, files []model.FileEntry, streaming bool, fallbackTitle string) (int64, error) {
	return AddChatMessageWithAgent(projectPath, backend, sessionID, role, content, files, streaming, fallbackTitle, "")
}

// AddChatMessageWithAgent is AddChatMessage plus speaker attribution.
//
// agentID is written to chat_history.agent_id. For a group-chat member turn it
// is the member session ROW id (see design §4.3), NOT a real agent id; for
// ordinary single-agent turns it is empty.
func AddChatMessageWithAgent(projectPath, backend, sessionID, role, content string, files []model.FileEntry, streaming bool, fallbackTitle, agentID string) (int64, error) {
	// Guard: reject messages to archived sessions
	var isArchived int
	if err := store.ReadDB().QueryRow("SELECT archived FROM chat_sessions WHERE id = ?", sessionID).Scan(&isArchived); err == nil && isArchived == 1 {
		return 0, fmt.Errorf("cannot add message to archived session %s", sessionID)
	}

	streamingInt := 0
	if streaming {
		streamingInt = 1
	}

	// Resolved before store.WriteBegin: store.ProjectIDForPath may write on a cache miss and
	// the write mutex is held for the transaction below.
	projectID, idErr := store.ProjectIDForPath(projectPath)
	if idErr != nil {
		return 0, idErr
	}

	// Use transaction under write mutex to ensure data consistency
	var msgID int64
	var titled bool
	tx, txErr := store.WriteBegin()
	if txErr != nil {
		return 0, txErr
	}
	defer store.WriteUnlock()
	defer tx.Rollback()

	msgID, titled, txErr = insertChatMessageTx(tx, projectID, backend, sessionID, role, content, files, streamingInt, fallbackTitle, agentID)
	if txErr != nil {
		return 0, txErr
	}

	if txErr := tx.Commit(); txErr != nil {
		return 0, txErr
	}

	// Schedule the AI rename AFTER the commit: the local title is already
	// durable, so the background call reads a consistent history, and a failure
	// there cannot roll back the user's message. Only the message that earned
	// the local title triggers it.
	if titled {
		ScheduleAutoRename(sessionID, ExtractPlainText(content))
	}

	slog.Info("chat: persisted message",
		slog.String("session", sessionID),
		slog.String("role", role),
		slog.Int64("msgID", msgID),
		slog.Bool("streaming", streaming))
	return msgID, nil
}

// AddSystemMessage appends a role='system' timeline row to a session.
//
// System events record membership changes ("X joined the discussion") so the
// host and every member learn about them (decisions #40/#44). They belong to
// nobody: agent_id is empty, which is what keeps them out of the author filter
// in buildInjectionText and lets the frontend render them as a centered row.
//
// Unread counts are unaffected by construction — they count role='assistant'
// only. Requires N1's widened CHECK (role IN ('user','assistant','system')).
func AddSystemMessage(projectPath, sessionID, text string) (int64, error) {
	return AddChatMessageWithAgent(projectPath, groupBackend(sessionID), sessionID, "system", text, nil, false, "", "")
}

// ErrChatQuoteNotFound is returned when the addressed quote entry does not
// exist on the given message (wrong id, or the message carries no quotes).
var ErrChatQuoteNotFound = errors.New("chat quote not found")

// UpdateChatQuoteNote replaces the annotation on one quote attachment of a
// user message.
//
// Scoped by session_id as well as message id so a caller cannot edit another
// session's message by guessing an id. Only user rows are eligible: an
// assistant message never carries user-authored quotes.
//
// The entry is addressed by its stable quote id, NOT by array index — the index
// the client saw can drift (entries are added/removed), and a wrong index would
// silently rewrite the wrong quote's note.
//
// Concurrency: a streaming turn already built its prompt from the in-memory
// files at launch, so this cannot retroactively change what the AI saw. A
// still-queued row (queued=1) IS re-read by the drain loop, so the edit lands in
// its prompt — the user fixing their note before it runs. The UPDATE and the
// dequeue SELECT serialize on the write mutex, so there is no torn read.
func UpdateChatQuoteNote(sessionID string, messageID int64, quoteID, note string) ([]model.FileEntry, error) {
	if sessionID == "" || messageID <= 0 || quoteID == "" {
		return nil, ErrChatQuoteNotFound
	}

	// Guard: reject edits to archived sessions, mirroring AddChatMessage.
	var isArchived int
	if err := store.ReadDB().QueryRow("SELECT archived FROM chat_sessions WHERE id = ?", sessionID).Scan(&isArchived); err == nil && isArchived == 1 {
		return nil, fmt.Errorf("cannot edit a message in archived session %s", sessionID)
	}

	tx, txErr := store.WriteBegin()
	if txErr != nil {
		return nil, txErr
	}
	defer store.WriteUnlock()
	defer tx.Rollback()

	var role, filesJSON string
	if err := tx.QueryRow(
		"SELECT role, COALESCE(files, '') FROM chat_history WHERE id = ? AND session_id = ?",
		messageID, sessionID,
	).Scan(&role, &filesJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrChatQuoteNotFound
		}
		return nil, err
	}
	if role != "user" {
		return nil, ErrChatQuoteNotFound
	}

	entries := unmarshalFilesJSON(filesJSON)

	found := false
	for i := range entries {
		if entries[i].IsQuote() && entries[i].ID == quoteID {
			entries[i].Note = note
			found = true
			break
		}
	}
	if !found {
		return nil, ErrChatQuoteNotFound
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec("UPDATE chat_history SET files = ? WHERE id = ? AND session_id = ?", string(data), messageID, sessionID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return entries, nil
}

// insertChatMessageTx performs the chat_history INSERT plus the shared
// session touches (updated_at refresh and first-user-message title generation)
// inside the caller's transaction.
//
// It returns the LastInsertId (msgID) and whether this call wrote the session's
// local title (titled), so the caller can schedule the AI rename for exactly the
// message that earned the title. The caller owns Commit/Rollback.
func insertChatMessageTx(tx *sql.Tx, projectID int64, backend, sessionID, role, content string, files []model.FileEntry, streamingInt int, fallbackTitle, agentID string) (int64, bool, error) {
	var filesJSON string
	if len(files) > 0 {
		data, _ := json.Marshal(files)
		filesJSON = string(data)
	}

	result, txErr := tx.Exec(
		"INSERT INTO chat_history (project_id, backend, session_id, role, content, files, streaming, indexed, agent_id) VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)",
		projectID, backend, sessionID, role, content, filesJSON, streamingInt, agentID,
	)
	if txErr != nil {
		return 0, false, txErr
	}

	// Update session's updated_at timestamp
	if _, txErr = tx.Exec("UPDATE chat_sessions SET updated_at = CURRENT_TIMESTAMP WHERE id = ?", sessionID); txErr != nil {
		return 0, false, txErr
	}

	titled := false
	if role == "user" {
		var err error
		titled, err = maybeAutoTitleSessionTx(tx, sessionID, content, files, fallbackTitle)
		if err != nil {
			return 0, false, err
		}
	}

	msgID, err := result.LastInsertId()
	return msgID, titled, err
}

// Session title sources, ordered by priority (higher rank wins). The stored
// title is always the effective title; priority is enforced only on WRITE, so
// every read path keeps returning chat_sessions.title unchanged.
//
// 会话标题来源，按优先级排序（rank 高者胜）。title 列始终保存有效标题；优先级只在
// 写入时门控，因此所有读取路径照常返回 chat_sessions.title，无需改动。
const (
	// TitleSourcePlaceholder: auto placeholder (e.g. "New Session 3"),
	// replaceable by the first user message.
	TitleSourcePlaceholder = "placeholder"
	// TitleSourceAuto: derived from the first user message / attachments.
	TitleSourceAuto = "auto"
	// TitleSourceCustom: deliberately chosen by the user (manual rename,
	// meaningful title at creation, task name, fork, imported title).
	TitleSourceCustom = "custom"
)

// titleSourceRank maps a title_source value to its priority. An empty or
// unknown value (e.g. a minimal test schema without the column) ranks 0, which
// preserves the historical "auto-title may overwrite" behavior.
func titleSourceRank(source string) int {
	switch source {
	case TitleSourceAuto:
		return 1
	case TitleSourceCustom:
		return 2
	default: // "" or TitleSourcePlaceholder
		return 0
	}
}

// maybeAutoTitleSessionTx sets the session title from a user message, unless the
// title was deliberately chosen (title_source='custom'): a manual rename, or a
// meaningful title supplied at creation (task, continue, imported session).
//
// The gate is title_source, not the message count. A placeholder title (rank 0)
// may be replaced; once the title becomes 'auto' (rank 1) its rank blocks every
// later message, so only the session's FIRST user message ever re-titles it.
//
// Two kinds of session are placeholder and therefore auto-title:
//
//   - A freshly created blank session — its "New Session 3" placeholder is
//     replaced by what the user actually asked.
//   - A FORK, whose copied history means its own first message is never message
//     #1. Gating on `count == 1` (as this used to) left a fork showing the
//     inherited "🔀 <source title>" prefix forever, never describing its own
//     topic; forks now leave the title replaceable so the first message after
//     the fork point names it. A fork's title is still locked the moment the
//     user renames it by hand (that writes 'custom', rank 2).
//
// The deliberate-title paths (task, continue-from-execution, ACP import) all
// write 'custom', so broadening the gate to "any message while placeholder"
// cannot clobber them.
//
// The returned bool reports whether a title was actually written by THIS call.
// Callers use it to decide whether to schedule the AI rename (service.
// session_auto_title.go): the AI layer must only run for the same one message
// that earned the local title, not on every message of the session.
func maybeAutoTitleSessionTx(tx *sql.Tx, sessionID, content string, files []model.FileEntry, fallbackTitle string) (bool, error) {
	var source string
	columnMissing := false
	// Best-effort read. A missing column means a minimal test schema; anything
	// else is unexpected and worth surfacing, since it would silently overwrite a
	// locked title.
	if err := tx.QueryRow("SELECT COALESCE(title_source, '') FROM chat_sessions WHERE id = ?", sessionID).Scan(&source); err != nil {
		if strings.Contains(err.Error(), "no such column") {
			columnMissing = true
		} else {
			slog.Warn("chat: could not read title_source; auto-title may overwrite a locked title",
				slog.String("session", sessionID), slog.String("err", err.Error()))
		}
	}
	if columnMissing {
		// No title_source to gate on, so the message count is the only signal
		// available. Keep the historical "first message only" rule exactly — with
		// no column every message would otherwise look like a placeholder.
		var count int
		if txErr := tx.QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sessionID).Scan(&count); txErr != nil || count != 1 {
			// A read failure here means "cannot prove this is the first message",
			// so skip titling rather than risk clobbering a locked title.
			return false, nil //nolint:nilerr // best-effort title; a failed count read must not fail the message insert
		}
	} else if titleSourceRank(source) >= titleSourceRank(TitleSourceAuto) {
		// Already auto-titled or deliberately named — never clobber.
		return false, nil
	}
	title := sessionTitleSourceText(tx, sessionID, content)
	if title == "" && len(files) > 0 {
		title = titleFromFileEntries(files)
	}
	if title == "" {
		title = fallbackTitle
	}
	if runes := []rune(title); len(runes) > 50 {
		title = string(runes[:50]) + "..."
	}
	if _, err := tx.Exec("UPDATE chat_sessions SET title = ?, title_source = ? WHERE id = ?", title, TitleSourceAuto, sessionID); err != nil {
		return false, err
	}
	return true, nil
}

// sessionTitleSourceText returns the text a session title should be derived
// from. For a GROUP session the user's message may carry the @ protocol,
// including `private` (密送) notes — those must never become the title (a note
// addressed to one member would be exposed to everyone who sees the session
// list). The protocol is stripped for group sessions only: a single chat has no
// protocol, so a message that merely DISCUSSES the tag syntax keeps its text.
//
// A read failure falls back to the raw text: titling is best-effort and must
// never fail the message insert, and the pre-existing behavior is preserved.
func sessionTitleSourceText(tx *sql.Tx, sessionID, content string) string {
	plain := ExtractPlainText(content)
	var sessionType string
	if err := tx.QueryRow("SELECT COALESCE(session_type, '') FROM chat_sessions WHERE id = ?", sessionID).Scan(&sessionType); err != nil {
		return plain
	}
	if sessionType == store.SessionTypeGroup {
		return strings.TrimSpace(grouprouting.StripProtocolTags(plain))
	}
	return plain
}

// applyAutoTitle sets the session title from a user message outside a
// transaction. Used by the enqueue path, which persists into queued_messages
// (not chat_history) and therefore cannot use maybeAutoTitleSessionTx's
// chat_history count fallback: it wraps the same logic in its own transaction.
//
// It returns whether the title was written, so the caller can schedule the AI
// rename for exactly this message.
func applyAutoTitle(sessionID, content string, files []model.FileEntry, fallbackTitle string) (bool, error) {
	tx, err := store.WriteBegin()
	if err != nil {
		return false, err
	}
	defer store.WriteUnlock()
	defer tx.Rollback()
	titled, err := maybeAutoTitleSessionTx(tx, sessionID, content, files, fallbackTitle)
	if err != nil {
		return false, err
	}
	return titled, tx.Commit()
}

// titleFromFileEntries builds a session title from file entries by extracting
// basenames and joining them with commas. Returns empty string if no files.
//
// Quote entries have no meaningful basename (their Path is a label, empty for
// a quote taken from a chat message), so the first line of the quoted text is
// used instead. Without this a quote-only message would title itself from a
// bare filename or fall through to the generic fallback, even though the
// quoted text is right there and far more descriptive.
func titleFromFileEntries(files []model.FileEntry) string {
	if len(files) == 0 {
		return ""
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		if f.IsQuote() {
			if line := firstNonEmptyLine(f.Text); line != "" {
				names = append(names, line)
				continue
			}
			// Empty quoted text (e.g. a quote staged before typing) still has
			// the source label to fall back on, which beats no title at all.
			// Deliberately falls through to the basename below.
		}
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

// firstNonEmptyLine returns the first line of s that has non-whitespace
// content, trimmed, capped at maxTitleLineRunes so one long quoted line cannot
// blow up the title. Empty when every line is blank.
func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if runes := []rune(line); len(runes) > maxTitleLineRunes {
			return string(runes[:maxTitleLineRunes])
		}
		return line
	}
	return ""
}

// maxTitleLineRunes caps a title derived from a quote's first line.
const maxTitleLineRunes = 50

// ConversationProject is a project that has at least one conversation on record.
type ConversationProject struct {
	// Path is the project directory as stored with the session. It is returned
	// even when the directory no longer exists on disk.
	Path string `json:"path"`
	// SessionCount is the number of sessions recorded for the project.
	SessionCount int `json:"session_count"`
	// LastActiveAt is the most recent session creation time, "2006-01-02 15:04:05" UTC.
	LastActiveAt string `json:"last_active_at"`
	// Exists reports whether the directory is still present on disk. A deleted
	// project is still listed — its history remains searchable — but the flag
	// lets the caller tell the two apart.
	Exists bool `json:"exists"`
}

// GetConversationProjects lists every project that has conversation history,
// newest activity first.
//
// The source is the union of chat_sessions and the denormalized chat_metadata
// ledger: chat_sessions drops rows when a session is hard-deleted, while
// chat_metadata is written at message time and survives session deletion. Using
// both means a project whose sessions were all deleted still appears as long as
// any message was ever recorded for it.
//
// Paths whose directory no longer exists are INCLUDED (Exists=false). Unlike
// GetRecentProjects, which purges vanished directories from the recent list,
// this endpoint exists to make old history discoverable, so a deleted project
// must stay listed.
func GetConversationProjects() ([]ConversationProject, error) {
	rows, err := store.ReadDB().QueryContext(context.Background(), `
		SELECT p.path, COUNT(*), MAX(agg.last_at) FROM (
			SELECT project_id, created_at AS last_at FROM chat_sessions
			 WHERE project_id != 0 AND session_type IN (`+store.VisibleSessionTypeInClause+`)
			UNION ALL
			SELECT project_id, created_at AS last_at FROM chat_metadata WHERE project_id != 0
		) agg
		JOIN projects p ON p.id = agg.project_id
		GROUP BY agg.project_id
		ORDER BY MAX(agg.last_at) DESC, p.path ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := make([]ConversationProject, 0, 16)
	for rows.Next() {
		var p ConversationProject
		if err := rows.Scan(&p.Path, &p.SessionCount, &p.LastActiveAt); err != nil {
			return nil, err
		}
		if info, statErr := os.Stat(p.Path); statErr == nil && info.IsDir() {
			p.Exists = true
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

// GetRecentProjects returns the most recent project paths as a flat list.
//
// It filters out paths whose directories no longer exist on disk (removing
// those rows from the database) and caps the result at the configured limit.
// Callers that need to know which paths belong to the same git repository use
// GetRecentProjectGroups instead; this flat form exists for callers that only
// need the most recent path, such as the default-project fallback.
func GetRecentProjects() ([]string, error) {
	limit := model.RecentProjectsMaxCount
	if limit <= 0 {
		limit = 10
	}

	valid, err := loadRecentProjectRows(context.Background())
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0, limit)
	for _, r := range valid {
		if len(paths) >= limit {
			break
		}
		paths = append(paths, r.path)
	}
	return paths, nil
}

// AddRecentProject upserts a project path and prunes old entries beyond configured limit.
func AddRecentProject(projectPath string) error {
	projectID, idErr := store.ProjectIDForPath(projectPath)
	if idErr != nil {
		return idErr
	}
	_, err := store.WriteExec(
		"INSERT INTO recent_projects (project_id, accessed_at) VALUES (?, CURRENT_TIMESTAMP) "+
			"ON CONFLICT(project_id) DO UPDATE SET accessed_at = CURRENT_TIMESTAMP",
		projectID,
	)
	if err != nil {
		return err
	}
	limit := model.RecentProjectsMaxCount
	if limit <= 0 {
		limit = 10
	}
	_, err = store.WriteExec(
		"DELETE FROM recent_projects WHERE id NOT IN (SELECT id FROM recent_projects ORDER BY accessed_at DESC LIMIT ?)",
		limit,
	)
	return err
}

// RemoveRecentProject deletes a project path from the recent projects list.
// If the removed project was the default, its is_default flag is cleared first.
func RemoveRecentProject(projectPath string) error {
	projectID, idErr := store.ProjectIDForPath(projectPath)
	if idErr != nil {
		return idErr
	}
	_, _ = store.WriteExec("UPDATE recent_projects SET is_default = 0 WHERE project_id = ? AND is_default = 1", projectID)
	_, err := store.WriteExec("DELETE FROM recent_projects WHERE project_id = ?", projectID)
	return err
}

// GetDefaultProject returns the project path marked as default (is_default=1),
// or falls back to the most recently accessed project, or the user's home directory,
// or the first root path. This is the server-side source of truth for project selection.
// It does NOT update accessed_at (avoids the self-reinforcing loop).
func GetDefaultProject() (string, error) {
	// 1. Try is_default=1 row
	var path string
	err := store.ReadDB().QueryRow(
		`SELECT p.path FROM recent_projects r JOIN projects p ON p.id = r.project_id
		  WHERE r.is_default = 1 LIMIT 1`).Scan(&path)
	if err == nil {
		// Verify directory still exists
		if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
			return path, nil
		}
		// Stale default — clear it
		_, _ = store.WriteExec("UPDATE recent_projects SET is_default = 0 WHERE is_default = 1")
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
	_, _ = store.WriteExec("UPDATE recent_projects SET is_default = 0 WHERE is_default = 1")
	// Ensure the project exists in recent_projects (upsert with accessed_at update)
	if err := AddRecentProject(projectPath); err != nil {
		return err
	}
	// Set the new default
	projectID, idErr := store.ProjectIDForPath(projectPath)
	if idErr != nil {
		return idErr
	}
	_, err := store.WriteExec("UPDATE recent_projects SET is_default = 1 WHERE project_id = ?", projectID)
	return err
}

// generateSessionID generates a standard UUID v4 format session ID.
func generateSessionID() string {
	return generateUUID("", "chat_sessions", "id")
}

// unreadCountSubquery is the per-session unread-reply count, correlated on the
// outer session alias `s`. Shared by GetSessions, GetSessionsPaged and
// GetOverviewSessions so all three agree on what "unread" means — if they
// drifted, the badge and the list would disagree about the same session.
//
// It is a correlated scalar subquery, NOT a grouped LEFT JOIN. The join form
// materialized every assistant message in the project (archived sessions
// included) into a temp B-tree and then grouped it, only to join a handful of
// surviving rows back — with 15.9k assistant messages in one project that
// measured ~79ms per call, on a read pool of just 2 connections. The correlated
// form seeks per listed session via idx_history_sess_unread and measures ~0ms.
// Both forms return identical rows (verified against a real DB).
//
// Two details are load-bearing, and both are asserted by tests:
//
//  1. h.project_id = s.project_id must stay. It is redundant for rows
//     written by current code, but historic rows can disagree (messages
//     persisted under the cookie's project instead of the session's, ISS-420).
//     Dropping it would count those as unread here while UpdateLastRead anchors
//     on a different set — the two sides must agree or the badge never clears.
//  2. idx_history_sess_unread must exist. The subquery always mentions
//     project_id (see point 1), and idx_history_unread also leads with
//     project_id — so without a session_id-leading alternative the planner
//     picks idx_history_unread and rescans the whole project once per listed
//     session (~186ms vs 0.01ms measured on a 15.9k-message project). The
//     index is what makes the seek win; it is not merely an optimisation.
//
// COUNT never returns NULL, so no COALESCE is needed (the old LEFT JOIN did
// need one, because a missed join yields NULL).
const unreadCountSubquery = `(SELECT COUNT(*) FROM chat_history h
			WHERE h.session_id = s.id
			  AND h.project_id = s.project_id
			  AND h.role = 'assistant' AND h.streaming = 0
			  AND (s.last_read_at IS NULL OR COALESCE(h.completed_at, h.created_at) > s.last_read_at)
		) AS unread_count`

// sessionsQueryBase is the prefix of GetSessions' query, up to (not including)
// the backend filter and ORDER BY. Package-level so the query-plan test can
// EXPLAIN the query production actually runs — asserting on unreadCountSubquery
// alone would miss a caller that switched back to the grouped-join form.
const sessionsQueryBase = `SELECT s.id, s.title, s.backend, s.agent_id, s.agent_source, s.model, s.session_type, s.source_session_id, s.pinned, s.sort_order, s.created_at, s.updated_at, s.last_read_at,
		` + unreadCountSubquery + `
		FROM chat_sessions s
		WHERE s.project_id = ? AND s.archived = 0 AND s.session_type IN (` + store.VisibleSessionTypeInClause + `)`

// overviewSessionsQuery is GetOverviewSessions' full query. Package-level for
// the same reason as sessionsQueryBase.
const overviewSessionsQuery = `SELECT s.id, s.title, s.backend, s.agent_id, s.agent_source, s.model, s.session_type, s.source_session_id, s.created_at, s.updated_at, s.last_read_at, COALESCE(p.path, ''),
		` + unreadCountSubquery + `
		FROM chat_sessions s
		LEFT JOIN projects p ON p.id = s.project_id
		WHERE s.archived = 0 AND s.session_type IN (` + store.VisibleSessionTypeInClause + `)
		ORDER BY s.updated_at DESC, s.id DESC`

// pagedSessionsQueryBase is the prefix of GetSessionsPaged' query, up to (not
// including) the backend/tag/cursor filters and ORDER BY/LIMIT. Package-level
// for the same reason as sessionsQueryBase.
const pagedSessionsQueryBase = `SELECT s.id, s.title, s.backend, s.agent_id, s.agent_source, s.model, s.session_type, s.source_session_id, s.pinned, s.sort_order, s.created_at, s.updated_at, s.last_read_at,
		` + unreadCountSubquery + `
		FROM chat_sessions s
		WHERE s.project_id = ? AND s.archived = 0 AND s.session_type IN (` + store.VisibleSessionTypeInClause + `)`

// GetSessions retrieves chat sessions for a given project path, ordered by
// pinned DESC, sort_order ASC, created_at DESC — pinned sessions are a fixed
// block at the top, and the rest follow the user's manual drag order (ties
// newest-first). If backend is non-empty, filters by backend; otherwise
// returns all backends.
// Only returns interactive sessions (session_type in chat/group; excludes
// scheduled and hidden group_member rows).
//
// The unread count is unreadCountSubquery — see its doc comment for why it is a
// correlated subquery rather than a grouped join.
func GetSessions(projectPath, backend string) ([]model.ChatSession, error) {
	projectID, idErr := store.ProjectIDForPath(projectPath)
	if idErr != nil {
		return []model.ChatSession{}, idErr
	}
	sessions := []model.ChatSession{}
	query := sessionsQueryBase
	args := []interface{}{projectID}
	if backend != "" {
		query += " AND s.backend = ?"
		args = append(args, backend)
	}
	query += " ORDER BY s.pinned DESC, s.sort_order ASC, s.created_at DESC, s.id DESC"

	rows, err := store.ReadDB().Query(query, args...)
	if err != nil {
		return sessions, err
	}
	defer rows.Close()

	for rows.Next() {
		var s model.ChatSession
		var lastRead sql.NullTime
		var sourceSessionID sql.NullString
		if err := rows.Scan(&s.ID, &s.Title, &s.Backend, &s.AgentID, &s.AgentSource, &s.Model, &s.SessionType, &sourceSessionID, &s.Pinned, &s.SortOrder, &s.CreatedAt, &s.UpdatedAt, &lastRead, &s.UnreadCount); err != nil {
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

// GetOverviewSessions returns all non-archived chat sessions across every
// project (for the floating window overview panel), including per-session
// unread counts. Unread is computed per-project (matched by project_path) so
// sessions in different projects don't interfere.
//
// The unread count is unreadCountSubquery — see its doc comment for why it is a
// correlated subquery rather than a grouped join.
func GetOverviewSessions() ([]model.ChatSession, error) {
	sessions := []model.ChatSession{}
	query := overviewSessionsQuery
	rows, err := store.ReadDB().Query(query)
	if err != nil {
		return sessions, err
	}
	defer rows.Close()
	for rows.Next() {
		var s model.ChatSession
		var lastRead sql.NullTime
		var sourceSessionID sql.NullString
		if err := rows.Scan(&s.ID, &s.Title, &s.Backend, &s.AgentID, &s.AgentSource, &s.Model, &s.SessionType, &sourceSessionID, &s.CreatedAt, &s.UpdatedAt, &lastRead, &s.ProjectPath, &s.UnreadCount); err != nil {
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
// ordered by pinned DESC, sort_order ASC, created_at DESC — pinned sessions are
// a fixed block at the top, and the rest follow the user's manual drag order
// (ties newest-first). limit=0 means no limit (returns all sessions).
//
// The cursor is the full sort key of the last row of the previous page, not
// just its created_at. Ordering is (pinned DESC, sort_order ASC, created_at
// DESC, id DESC), so the keyset predicate must compare all four columns
// lexicographically:
//
//	pinned < cursorPinned
//	OR (pinned = cursorPinned AND (sort_order > cursorSortOrder
//	      OR (sort_order = cursorSortOrder AND (created_at < cursor
//	            OR (created_at = cursor AND id < cursorID)))))
//
// Comparing created_at alone re-returns every row sharing the cursor's
// (pinned, sort_order) on each page, duplicating rows across pages.
// cursorSortOrder and cursorPinned are optional: when either is nil the legacy
// created_at-only predicate is used, so callers that do not track the full sort
// key still page without error.
//
// Returns sessions and hasMore flag.
// GetSessionsPaged returns a keyset-paginated page of active chat sessions for
// projectPath. tagName, when non-empty, restricts the page to sessions carrying
// a tag of that name visible in this project (global, or this project's own).
func GetSessionsPaged(projectPath, backend string, limit int, cursor string, cursorID string, cursorSortOrder *int, cursorPinned *bool, tagName string) ([]model.ChatSession, bool, error) {
	// No limit: return all sessions
	if limit <= 0 {
		sessions, err := GetSessions(projectPath, backend)
		if err != nil {
			return nil, false, err
		}
		if tagName != "" {
			sessions, err = FilterSessionsByTag(sessions, projectPath, tagName)
			if err != nil {
				return nil, false, err
			}
		}
		return sessions, false, nil
	}

	// Build main query with cursor and limit+1.
	// The unread count is unreadCountSubquery — see its doc comment for why it
	// is a correlated subquery rather than a grouped join.
	projectID, idErr := store.ProjectIDForPath(projectPath)
	if idErr != nil {
		return nil, false, idErr
	}
	query := pagedSessionsQueryBase
	args := []interface{}{projectID}
	if backend != "" {
		query += " AND s.backend = ?"
		args = append(args, backend)
	}
	if tagName != "" {
		// EXISTS rather than a JOIN: a join would multiply rows per tag and
		// break the LIMIT/keyset arithmetic below. The visibility predicate
		// mirrors ListSessionTags so a session can only be matched by a
		// definition it can actually see (global, or its own project's).
		query += ` AND EXISTS (
			SELECT 1 FROM session_tag_links l
			JOIN session_tags t ON t.id = l.tag_id
			WHERE l.session_id = s.id
			  AND t.name = ? COLLATE NOCASE
			  AND (t.scope = 'global' OR t.project_id = ?))`
		args = append(args, tagName, projectID)
	}
	if cursor != "" && cursorID != "" {
		if cursorSortOrder != nil && cursorPinned != nil {
			// Full keyset: pinned is the leading sort column (descending), then
			// sort_order (ascending), then created_at DESC / id DESC. A row is
			// "after" the cursor when it sits in a later pinned group, or in the
			// same group with a greater sort_order, or with the same sort_order
			// and an older created_at / smaller id.
			pinnedInt := 0
			if *cursorPinned {
				pinnedInt = 1
			}
			query += " AND (s.pinned < ? OR (s.pinned = ? AND (s.sort_order > ? OR (s.sort_order = ? AND (s.created_at < ? OR (s.created_at = ? AND s.id < ?))))))"
			args = append(args, pinnedInt, pinnedInt, *cursorSortOrder, *cursorSortOrder, cursor, cursor, cursorID)
		} else {
			// Legacy cursor (created_at + id only), for callers that do not track
			// the full sort key.
			query += " AND (s.created_at < ? OR (s.created_at = ? AND s.id < ?))"
			args = append(args, cursor, cursor, cursorID)
		}
	}
	query += " ORDER BY s.pinned DESC, s.sort_order ASC, s.created_at DESC, s.id DESC LIMIT ?"
	args = append(args, limit+1)

	rows, err := store.ReadDB().Query(query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var sessions []model.ChatSession
	for rows.Next() {
		var s model.ChatSession
		var lastRead sql.NullTime
		var sourceSessionID sql.NullString
		if err := rows.Scan(&s.ID, &s.Title, &s.Backend, &s.AgentID, &s.AgentSource, &s.Model, &s.SessionType, &sourceSessionID, &s.Pinned, &s.SortOrder, &s.CreatedAt, &s.UpdatedAt, &lastRead, &s.UnreadCount); err != nil {
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

// FilterSessionsByTag narrows an in-memory session slice to those carrying a
// tag of the given name visible in projectPath. Only used by the unpaginated
// path (limit <= 0); the paged path pushes the same predicate into SQL.
func FilterSessionsByTag(sessions []model.ChatSession, projectPath, tagName string) ([]model.ChatSession, error) {
	if len(sessions) == 0 {
		return sessions, nil
	}
	ids := make([]string, 0, len(sessions))
	for i := range sessions {
		ids = append(ids, sessions[i].ID)
	}
	m, err := GetTagsForSessions(ids)
	if err != nil {
		return nil, err
	}
	out := sessions[:0:0]
	for i := range sessions {
		for _, t := range m[sessions[i].ID] {
			if strings.EqualFold(t.Name, tagName) && (t.Scope == SessionTagScopeGlobal || t.ProjectPath == projectPath) {
				out = append(out, sessions[i])
				break
			}
		}
	}
	return out, nil
}

// ListProjectTagsInUse returns the tags actually carried by at least one active
// chat session in projectPath, most-used first, then by name.
//
// This is what the session-list filter bar shows: unlike ListSessionTags (the
// dialog's candidate list, which includes every global tag), a tag nobody in
// this project uses would be a filter that always yields an empty list.
//
// Grouping is by NAME (COLLATE NOCASE), not by definition id, and the count is
// the number of DISTINCT sessions carrying any visible definition of that name.
// That mirrors the filter's predicate exactly (`t.name = ? COLLATE NOCASE AND
// (t.scope='global' OR t.project_path=?)`), so the count on a chip always equals
// the number of sessions clicking it returns. Grouping by t.id instead would
// report one definition's count while the filter returned the union of both —
// a chip reading "2" that lists 3 sessions.
func ListProjectTagsInUse(projectPath string) ([]SessionTag, error) {
	projectID, idErr := store.ProjectIDForPath(projectPath)
	if idErr != nil {
		return nil, idErr
	}
	rows, err := store.ReadDB().Query(`
		SELECT MIN(t.name) AS name,
		       MIN(CASE WHEN t.scope = 'global' THEN 0 ELSE 1 END) AS is_project,
		       COUNT(DISTINCT s.id) AS cnt
		FROM session_tags t
		JOIN session_tag_links l ON l.tag_id = t.id
		JOIN chat_sessions s ON s.id = l.session_id
		WHERE (t.scope = 'global' OR t.project_id = ?)
		  AND s.project_id = ? AND s.archived = 0 AND s.session_type IN (`+store.VisibleSessionTypeInClause+`)
		GROUP BY t.name COLLATE NOCASE
		ORDER BY cnt DESC, name COLLATE NOCASE`, projectID, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	tags := []SessionTag{}
	for rows.Next() {
		var t SessionTag
		var isProject int
		if err := rows.Scan(&t.Name, &isProject, &t.Count); err != nil {
			return nil, err
		}
		// One chip per name. When a global and a project definition share the
		// name, report the global scope — matching ListSessionTags, which
		// prefers the global definition (ORDER BY ... scope ASC).
		if isProject == 0 {
			t.Scope = SessionTagScopeGlobal
			t.ProjectPath = ""
		} else {
			t.Scope = SessionTagScopeProject
			t.ProjectPath = projectPath
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// UpdateLastRead sets the last_read_at timestamp for a session to now.
// Must run synchronously so that subsequent GetSessions queries (triggered by
// loadSessionsOnce after switchSession) see the updated last_read_at.
// Previously ran asynchronously (goroutine), which caused a race: the session
// list still showed unread messages after the user opened the session.
func UpdateLastRead(sessionID string) {
	// Set last_read_at to at least the newest finalized assistant message's
	// timestamp, but never below now. The unread query compares
	// COALESCE(h.completed_at, h.created_at) > s2.last_read_at with
	// second-precision SQLite DATETIME — if a message finalized in the same
	// second as the mark-read call, CURRENT_TIMESTAMP would still leave it
	// "unread". Anchoring last_read_at to the newest message timestamp makes the
	// comparison robust (last_read_at >= that timestamp ⇒ not unread). The
	// subquery MUST use the same COALESCE expression as the unread queries, or
	// the two sides disagree about which replies have been seen.
	//
	// MAX(CURRENT_TIMESTAMP, ...) is essential: on the cancel path the frontend
	// marks the session read when the "cancelled" session_update arrives, which
	// is emitted BEFORE the executor finalizes the interrupted reply
	// (streaming=1 → 0). At that instant the only finalized assistant row is the
	// PREVIOUS turn's, so a bare COALESCE would anchor last_read_at backwards to
	// that older timestamp — the interrupted reply then flips the session back
	// to unread as soon as it is finalized, even though the user is looking at
	// it. Taking the max with CURRENT_TIMESTAMP keeps the anchor monotonic.
	// Falls back to CURRENT_TIMESTAMP when no finalized assistant message exists.
	//
	// Note this anchors at most to "now": a reply still streaming when the read
	// happens is excluded (streaming = 0), so it will legitimately register as
	// unread once it lands. That is intended — the frontend re-marks the session
	// read on the completion event when the user is actually looking at it.
	store.WriteExec(`
		UPDATE chat_sessions
		SET last_read_at = MAX(
			CURRENT_TIMESTAMP,
			COALESCE(
				(SELECT MAX(COALESCE(completed_at, created_at)) FROM chat_history
				 WHERE session_id = ? AND role = 'assistant' AND streaming = 0),
				CURRENT_TIMESTAMP
			)
		)
		WHERE id = ?`, sessionID, sessionID)
	// Broadcast a status change so connected clients (e.g. the Android floating
	// window) can refresh their unread counts. WS-only: no push notification and
	// no pending event — reading a session must not create a notification.
	EmitSessionEventWSOnly(sessionID, "read", false)
}

// GetSessionBackend returns the backend of a session, or empty string if not found or archived.
func GetSessionBackend(sessionID string) string {
	var backend string
	err := store.ReadDB().QueryRow("SELECT backend FROM chat_sessions WHERE id = ? AND archived = 0", sessionID).Scan(&backend)
	if err != nil {
		return ""
	}
	return backend
}

// GetSessionProjectPath returns the project path of a session, or empty string if not found.
func GetSessionProjectPath(sessionID string) string {
	var projectPath string
	err := store.ReadDB().QueryRow(
		`SELECT COALESCE(p.path, '') FROM chat_sessions s
		   LEFT JOIN projects p ON p.id = s.project_id
		  WHERE s.id = ? AND s.archived = 0`, sessionID).Scan(&projectPath)
	if err != nil {
		return ""
	}
	return projectPath
}

// SessionLookupOutcome classifies a project-path lookup so callers can tell a
// missing session from a failing database.
type SessionLookupOutcome int

const (
	SessionFound SessionLookupOutcome = iota
	SessionMissing
	SessionLookupError
)

// LookupSessionProjectPathActive resolves an active session's project path with
// the failure cause preserved: SessionMissing means the row does not exist (or
// is archived), SessionLookupError means the query itself failed.
//
// The distinction matters for ownership checks. The path is resolved through a
// LEFT JOIN on projects, and an unmappable path is backfilled to the 0 sentinel
// — which never has a projects row (the table is AUTOINCREMENT). Such an
// orphaned session therefore resolves to "" with SessionFound, while a missing
// session resolves to "" with SessionMissing. Comparing the path unconditionally
// rejects the orphan; callers use the outcome to keep reporting 404 for a
// session that does not exist and 500 for a broken database, instead of
// collapsing all three into "forbidden".
func LookupSessionProjectPathActive(sessionID string) (string, SessionLookupOutcome) {
	var projectPath string
	err := store.ReadDB().QueryRow(
		`SELECT COALESCE(p.path, '') FROM chat_sessions s
		   LEFT JOIN projects p ON p.id = s.project_id
		  WHERE s.id = ? AND s.archived = 0`, sessionID).Scan(&projectPath)
	switch {
	case err == nil:
		return projectPath, SessionFound
	case errors.Is(err, sql.ErrNoRows):
		return "", SessionMissing
	default:
		return "", SessionLookupError
	}
}

// GetSessionProjectPathIncludeArchived returns the project path of a session
// regardless of archived status, or empty string if not found. Used by
// read-only lookups that must work for archived sessions too (e.g. session
// search's lazy first-message preview).
func GetSessionProjectPathIncludeArchived(sessionID string) string {
	var projectPath string
	err := store.ReadDB().QueryRow(
		`SELECT COALESCE(p.path, '') FROM chat_sessions s
		   LEFT JOIN projects p ON p.id = s.project_id
		  WHERE s.id = ?`, sessionID).Scan(&projectPath)
	if err != nil {
		return ""
	}
	return projectPath
}

// GetLatestSessionID returns the ID and backend of the most recently updated chat session
// for a project. Returns sql.ErrNoRows if no sessions exist.
func GetLatestSessionID(projectPath string) (sessionID, backend string, err error) {
	projectID, idErr := store.ProjectIDForPath(projectPath)
	if idErr != nil {
		return "", "", idErr
	}
	err = store.ReadDB().QueryRow(
		`SELECT id, backend FROM chat_sessions
		 WHERE project_id = ? AND archived = 0 AND session_type IN (`+store.VisibleSessionTypeInClause+`)
		 ORDER BY updated_at DESC, id DESC LIMIT 1`,
		projectID,
	).Scan(&sessionID, &backend)
	return
}

// GetMessageIDBeforeTime resolves a legacy "before" (created_at timestamp) cursor
// to the corresponding message ID. This provides backward compatibility for older
// clients that still send ?before=<timestamp> instead of ?before_id=<id>.
// Returns the max ID of messages created before the given timestamp, or 0 if none found.
func GetMessageIDBeforeTime(projectPath, backend, sessionID, beforeTime string) (int, error) {
	projectID, idErr := store.ProjectIDForPath(projectPath)
	if idErr != nil {
		return 0, idErr
	}
	var id sql.NullInt64
	err := store.ReadDB().QueryRow(
		`SELECT MAX(id) FROM chat_history
		 WHERE project_id = ? AND backend = ? AND session_id = ?
		 AND created_at < ?`,
		projectID, backend, sessionID, beforeTime,
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return int(id.Int64), nil
}

// GetSessionModel returns the model ID of a session, or empty string if not found or archived.
func GetSessionModel(sessionID string) string {
	var modelID string
	err := store.ReadDB().QueryRow("SELECT model FROM chat_sessions WHERE id = ? AND archived = 0", sessionID).Scan(&modelID)
	if err != nil {
		return ""
	}
	return modelID
}

// UpdateSessionModel updates the model field for a session.
// Called when the user selects a different model so that subsequent loads
// restore the user's choice instead of the agent default.
func UpdateSessionModel(sessionID, modelID string) error {
	_, err := store.WriteExec("UPDATE chat_sessions SET model = ? WHERE id = ?", modelID, sessionID)
	return err
}

// UpdateSessionTransport updates the transport field for a session.
func UpdateSessionTransport(sessionID, transport string) error {
	_, err := store.WriteExec("UPDATE chat_sessions SET transport = ? WHERE id = ?", transport, sessionID)
	return err
}

// GetSessionTransport returns the transport for a session, or empty string if not set.
func GetSessionTransport(sessionID string) string {
	var transport string
	err := store.ReadDB().QueryRow("SELECT COALESCE(transport, '') FROM chat_sessions WHERE id = ? AND archived = 0", sessionID).Scan(&transport)
	if err != nil {
		return ""
	}
	return transport
}

// GetSessionAutoApprove returns whether auto-approve mode is enabled for a session.
func GetSessionAutoApprove(sessionID string) bool {
	var val int
	err := store.ReadDB().QueryRow("SELECT auto_approve FROM chat_sessions WHERE id = ? AND archived = 0", sessionID).Scan(&val)
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
	_, err := store.WriteExec("UPDATE chat_sessions SET auto_approve = ? WHERE id = ?", val, sessionID)
	return err
}

// SaveMetadata persists message metadata to the chat_metadata table.
// This enables SQL-based analytical queries (token usage, cost, model stats)
// while the same metadata remains embedded in chat_history.content JSON for
// backward compatibility with the frontend.
//
// chat_metadata is a standalone usage ledger: it carries no foreign key to
// chat_history so a row survives the message/session being deleted. The
// project/backend/agent attribution is denormalized here at write time so the
// stats query never has to join back to a possibly-deleted session.
func SaveMetadata(messageID int64, meta *ai.Metadata) error {
	if messageID <= 0 || meta == nil {
		return nil
	}
	isError := 0
	if meta.IsError {
		isError = 1
	}
	// usageByCategory is a map — persist as a JSON string column.
	var categoryJSON string
	if len(meta.UsageByCategory) > 0 {
		if b, err := json.Marshal(meta.UsageByCategory); err == nil {
			categoryJSON = string(b)
		}
	}
	// Resolve denormalized attribution from the message's own row. A missing
	// row (ErrNoRows) is benign — the message was already deleted — and leaves
	// the columns empty. Any other error is propagated rather than swallowed:
	// writing a row with project_id = 0 would make it permanently invisible
	// to every project-scoped stats query, i.e. silent under-counting.
	var projectID int64
	var backend, agentID, clawbenchSessionID string
	if err := store.ReadDB().QueryRow(
		`SELECT h.project_id, h.backend, h.session_id, COALESCE(s.agent_id, '')
		 FROM chat_history h
		 LEFT JOIN chat_sessions s ON s.id = h.session_id
		 WHERE h.id = ?`, messageID,
	).Scan(&projectID, &backend, &clawbenchSessionID, &agentID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("resolve metadata attribution for message %d: %w", messageID, err)
	}
	_, err := store.WriteExec(
		`
		INSERT OR REPLACE INTO chat_metadata
			(message_id, mode, thinking_effort, transport, model, input_tokens, output_tokens,
			 duration_ms, wall_ms, cost_usd, stop_reason, is_error, error_message,
			 cached_read_tokens, cached_write_tokens, thought_tokens, total_tokens,
			 cache_creation_tokens, cache_hit_tokens, cache_miss_tokens, credit,
			 usage_by_category, session_id,
			 request_id, trace_id, agent_message_id, message_request_id, request_model_name,
			 response_model_id, finish_reason, outcome, agent_phase,
			 project_id, backend, agent_id, clawbench_session_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		messageID, meta.Mode, meta.ThinkingEffort, meta.Transport, meta.Model,
		meta.InputTokens, meta.OutputTokens, meta.DurationMs, meta.WallMs,
		meta.CostUSD, meta.StopReason, isError, meta.ErrorMessage,
		meta.CachedReadTokens, meta.CachedWriteTokens, meta.ThoughtTokens, meta.TotalTokens,
		meta.CacheCreationTokens, meta.CacheHitTokens, meta.CacheMissTokens, meta.Credit,
		categoryJSON, meta.SessionID,
		meta.RequestID, meta.TraceID, meta.MessageID, meta.MessageRequestID, meta.RequestModelName,
		meta.ResponseModelID, meta.FinishReason, meta.Outcome, meta.AgentPhase,
		projectID, backend, agentID, clawbenchSessionID,
	)
	return err
}

// GetLatestUserModel returns the most recent model the user explicitly chose
// for the given agent+project. Returns "" if no user preference exists
// (caller should fall back to agent defaults).
// Used by tasks to respect the user's global model preference.
//
// Only interactive chat rows count (session_type='chat'). A GROUP row must NOT
// win this lookup: its agent_id is its HOST, and the HTTP handler persists the
// request's modelId onto the group row BEFORE the group branch
// (handler/chat.go UpdateSessionModel), so a group send with an explicit model
// would otherwise masquerade as the host agent's latest user preference and
// silently change the default model a task uses. group_member rows are hidden
// and carry no model; scheduled rows never get a user-chosen model either, so
// restricting to 'chat' loses no real preference.
func GetLatestUserModel(agentID, projectPath string) string {
	projectID, idErr := store.ProjectIDForPath(projectPath)
	if idErr != nil {
		return ""
	}
	var modelID string
	err := store.ReadDB().QueryRow(
		"SELECT model FROM chat_sessions WHERE agent_id = ? AND project_id = ? AND archived = 0 AND model != '' AND session_type = ? ORDER BY updated_at DESC LIMIT 1",
		agentID, projectID, store.SessionTypeChat,
	).Scan(&modelID)
	if err != nil {
		return ""
	}
	return modelID
}

// CreateSession creates a new chat session and returns its ID.
// agentSource tracks how the agent was chosen: "default" (auto-assigned) or "user" (manually selected).
// sessionType is "chat" or "scheduled"; empty string defaults to "chat".
//
// The title is treated as a placeholder that the first user message may
// replace (auto-titling). Callers that pass a deliberately chosen title
// (task name, fork title, user-supplied name) should use
// CreateSessionWithLockedTitle instead so it is not overwritten.
//
// When the agent is configured with AutoApprove=true, the session's
// chat_sessions.auto_approve flag is initialized to 1 at creation time, so the
// choice survives a frontend reload instead of living only in in-memory UI
// state. This is a creation-time snapshot: toggling the agent default later
// does not rewrite existing sessions.
func CreateSession(projectPath, backend, title, agentID, modelName, agentSource, sessionType string) (string, error) {
	return createSession(projectPath, backend, title, agentID, modelName, agentSource, sessionType, false)
}

// CreateSessionWithLockedTitle creates a session whose title was deliberately
// chosen by the caller (e.g. a task name, a fork/continue title, or a
// user-supplied name at creation). It marks chat_sessions.title_source='custom'
// so the first-message auto-title does not overwrite it.
func CreateSessionWithLockedTitle(projectPath, backend, title, agentID, modelName, agentSource, sessionType string) (string, error) {
	return createSession(projectPath, backend, title, agentID, modelName, agentSource, sessionType, true)
}

func createSession(projectPath, backend, title, agentID, modelName, agentSource, sessionType string, lockTitle bool) (string, error) {
	if sessionType == "" {
		sessionType = "chat"
	}
	sessionID := generateSessionID()
	if sessionID == "" {
		return "", fmt.Errorf("failed to generate unique session ID after 10 attempts")
	}
	projectID, idErr := store.ProjectIDForPath(projectPath)
	if idErr != nil {
		return "", idErr
	}
	// sort_order is left at its default 0: a new session ties the current top
	// row and wins on the created_at DESC tiebreak, so it lands at the top of
	// the manual order (#492) without disturbing the rows the user dragged.
	_, err := store.WriteExec(
		"INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, agent_source, model, session_type, external_session_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		sessionID, projectID, backend, title, agentID, agentSource, modelName, sessionType, "",
	)
	if err != nil {
		return "", err
	}
	if lockTitle {
		markSessionTitleRenamed(sessionID)
	} else {
		markSessionTitlePlaceholder(sessionID)
	}
	applyAgentAutoApproveDefault(sessionID, agentID)
	slog.Info("session created",
		slog.String("session", sessionID),
		slog.String("backend", backend),
		slog.String("agent", agentID),
		slog.String("type", sessionType),
		slog.String("source", agentSource))
	return sessionID, nil
}

// markSessionTitleRenamed sets chat_sessions.title_source='custom' so the
// first-message auto-title does not overwrite a deliberately chosen title.
// Implemented as a guarded UPDATE (not part of the INSERT) so session creation
// does not depend on the column being present in minimal schemas; failure is
// non-fatal.
func markSessionTitleRenamed(sessionID string) {
	if _, err := store.WriteExec("UPDATE chat_sessions SET title_source = ? WHERE id = ?", TitleSourceCustom, sessionID); err != nil {
		slog.Warn("failed to mark session title as custom",
			slog.String("session", sessionID),
			slog.String("err", err.Error()))
	}
}

// markSessionTitlePlaceholder sets chat_sessions.title_source='placeholder' for
// a session created with an auto placeholder title, so the first user message
// may replace it. Guarded UPDATE like markSessionTitleRenamed; failure is
// non-fatal (an empty/unknown source also ranks as placeholder).
func markSessionTitlePlaceholder(sessionID string) {
	if _, err := store.WriteExec("UPDATE chat_sessions SET title_source = ? WHERE id = ?", TitleSourcePlaceholder, sessionID); err != nil {
		slog.Warn("failed to mark session title as placeholder",
			slog.String("session", sessionID),
			slog.String("err", err.Error()))
	}
}

// applyAgentAutoApproveDefault initializes a freshly created session's
// chat_sessions.auto_approve from the agent's configured default. Called by
// every session-creation path (new session, fork, continue-from-execution) so
// the agent-level "auto-approve new sessions" preference is persisted
// consistently instead of living only in frontend display state.
//
// Implemented as a guarded UPDATE rather than part of the INSERT so session
// creation does not depend on the auto_approve column being present in minimal
// schemas. Failure is non-fatal: the session exists, only the flag is missing.
func applyAgentAutoApproveDefault(sessionID, agentID string) {
	agent := model.GetAgent(agentID)
	if agent == nil || !agent.AutoApprove {
		return
	}
	if _, err := store.WriteExec("UPDATE chat_sessions SET auto_approve = 1 WHERE id = ?", sessionID); err != nil {
		slog.Warn("failed to initialize session auto-approve from agent default",
			slog.String("session", sessionID),
			slog.String("agent", agentID),
			slog.String("err", err.Error()))
	}
}

// UpdateSessionSourceID sets the source_session_id for a chat session.
// Used by acp-load to track the ACP session origin (format: "acp:{acpSessionId}").
func UpdateSessionSourceID(sessionID, sourceSessionID string) error {
	_, err := store.WriteExec("UPDATE chat_sessions SET source_session_id = ? WHERE id = ?", sourceSessionID, sessionID)
	return err
}

// SetSessionTitleLocked updates the title of a chat session and marks its
// source as deliberately chosen (title_source='custom') so the first-message
// auto-title will not overwrite it. Used by the manual rename endpoint and by
// ACP session import, where the title is derived from the CLI's own transcript.
func SetSessionTitleLocked(sessionID, title string) error {
	_, err := store.WriteExec("UPDATE chat_sessions SET title = ?, title_source = ? WHERE id = ?", title, TitleSourceCustom, sessionID)
	return err
}

// UpdateSessionPinned sets the pinned marker for a session.
//
// Pinning is now purely a visual marker (a corner wedge): since #492 the list
// order is the user's manual drag order (sort_order), so pinning no longer
// moves a row. It is still persisted separately so the marker survives a
// reload and a drag reorder does not clear it.
//
// Deliberately does NOT touch updated_at: pinning is a UI preference, not
// session activity. updated_at drives the relative-time label in the session
// list and GetLatestSessionID's "most recent session" pick, so bumping it would
// make an old pinned session read as "just now" and hijack the default session.
func UpdateSessionPinned(sessionID string, pinned bool) error {
	_, err := store.WriteExec("UPDATE chat_sessions SET pinned = ? WHERE id = ?", pinned, sessionID)
	return err
}

// ReorderSessions persists a manual drag order for one project's chat sessions.
// ids is the visible order the user just dropped; the i-th id becomes
// sort_order i (0 = top of the NON-pinned block).
//
// Pinned sessions are excluded from the reorder: they are a fixed block at the
// top (pinned DESC leads the ORDER BY), so their sort_order is irrelevant to
// their position and rewriting it would only lose the order they had before
// being pinned. Any pinned id posted by the client is ignored, which also makes
// the endpoint safe against a client that has not yet adopted the pinned-block
// rule.
//
// Scoped to projectPath, session_type='chat' and archived=0 so a stale or
// malicious id cannot renumber a session in another project, a scheduled task,
// or an archived row. The archived filter is what keeps this cheap: the list
// only ever shows non-archived rows, but a project accumulates far more
// archived ones (measured: 3 visible vs 1990 archived), and renumbering those
// would rewrite ~2000 rows per drag for no visible effect. Ids that do not
// match are silently ignored (a row archived between load and drop simply
// disappears from the list).
//
// Non-pinned, non-archived sessions NOT in ids are renumbered to len(ids),
// len(ids)+1, ... in their previous relative order. The client posts every row
// it displays, so this set is normally empty — it is a safety net for a row
// created between the client's load and the drop, keeping it below the dragged
// block instead of letting it interleave (a fresh session defaults to
// sort_order 0 and would otherwise sort above the whole dragged prefix).
//
// Deliberately does NOT touch updated_at — see UpdateSessionPinned for why a UI
// preference must not hijack the "most recent session" pick.
func ReorderSessions(projectPath string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	projectID, idErr := store.ProjectIDForPath(projectPath)
	if idErr != nil {
		return idErr
	}
	// One statement, so a crash cannot leave the project half-renumbered. The
	// `posted` CTE carries the target order; `rest` numbers the remaining
	// visible rows by their current order, starting at len(ids). Both the CTE
	// and the UPDATE carry the same (pinned, archived) filter as the read query,
	// so only rows the user can actually see are touched.
	var posted strings.Builder
	args := make([]any, 0, len(ids)*2+4)
	for i, id := range ids {
		if i > 0 {
			posted.WriteString(",")
		}
		posted.WriteString("(?,?)")
		args = append(args, id, i)
	}
	args = append(args, projectID, len(ids), projectID)

	query := `WITH posted(id, ord) AS (VALUES ` + posted.String() + `),
		rest AS (
			SELECT s.id AS rid, ROW_NUMBER() OVER (ORDER BY s.sort_order ASC, s.created_at DESC, s.id DESC) - 1 AS rn
			FROM chat_sessions s
			WHERE s.project_id = ? AND s.session_type IN (` + store.VisibleSessionTypeInClause + `) AND s.pinned = 0 AND s.archived = 0
			  AND s.id NOT IN (SELECT id FROM posted)
		)
		UPDATE chat_sessions SET sort_order = COALESCE(
			(SELECT ord FROM posted WHERE posted.id = chat_sessions.id),
			(SELECT ? + rn FROM rest WHERE rest.rid = chat_sessions.id)
		)
		WHERE project_id = ? AND session_type IN (` + store.VisibleSessionTypeInClause + `) AND pinned = 0 AND archived = 0`

	_, err := store.WriteExec(query, args...)
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
	projectID, idErr := store.ProjectIDForPath(projectPath)
	if idErr != nil {
		return idErr
	}
	_, err := store.WriteExec("UPDATE chat_sessions SET archived = 1, updated_at = CURRENT_TIMESTAMP WHERE project_id = ? AND id = ?", projectID, sessionID)
	return err
}

// GetSessionCount returns the number of chat sessions for a given project.
// Only counts sessions with session_type='chat' (excludes scheduled sessions).
func GetSessionCount(projectPath string) (int, error) {
	projectID, idErr := store.ProjectIDForPath(projectPath)
	if idErr != nil {
		return 0, idErr
	}
	var count int
	err := store.ReadDB().QueryRow("SELECT COUNT(*) FROM chat_sessions WHERE project_id = ? AND archived = 0 AND session_type IN ("+store.VisibleSessionTypeInClause+")", projectID).Scan(&count)
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
	projectID, idErr := store.ProjectIDForPath(projectPath)
	if idErr != nil {
		return 0, idErr
	}
	prefix := baseTitle + " "
	rows, err := store.ReadDB().Query(
		`SELECT title FROM chat_sessions
		 WHERE project_id = ? AND archived = 0 AND session_type = 'chat'`,
		projectID,
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

// FirstMessage is the earliest message of a session, used to lazily populate
// the browse-mode detail preview without loading the whole session.
type FirstMessage struct {
	MessageID int64
	Role      string
	Content   string
	CreatedAt time.Time
}

// GetSessionFirstMessage returns the earliest message of a session (by
// created_at then id), including archived sessions. Content is returned as
// plain text. Returns a zero FirstMessage (nil error) when the session has no
// messages.
func GetSessionFirstMessage(sessionID string) (*FirstMessage, error) {
	var msg FirstMessage
	var content string
	err := store.ReadDB().QueryRow(
		`SELECT id, role, content, created_at FROM chat_history
		 WHERE session_id = ? ORDER BY created_at ASC, id ASC LIMIT 1`,
		sessionID,
	).Scan(&msg.MessageID, &msg.Role, &content, &msg.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	msg.Content = ExtractPlainText(content)
	return &msg, nil
}

// GetSessionTitle returns the title of an active (non-archived) session.
func GetSessionTitle(sessionID string) (string, error) {
	var title string
	err := store.ReadDB().QueryRow("SELECT title FROM chat_sessions WHERE id = ? AND archived = 0", sessionID).Scan(&title)
	if err != nil {
		return "", err
	}
	return title, nil
}

// GetSessionTitleRenamed reports whether the session title was set manually by
// the user (title_source='custom'), which suppresses first-message
// auto-titling. Derived from title_source, the source of truth.
func GetSessionTitleRenamed(sessionID string) (bool, error) {
	var source string
	err := store.ReadDB().QueryRow("SELECT COALESCE(title_source, '') FROM chat_sessions WHERE id = ?", sessionID).Scan(&source)
	if err != nil {
		return false, err
	}
	return source == TitleSourceCustom, nil
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

	rows, err := store.ReadDB().Query("SELECT id, title FROM chat_sessions WHERE id IN ("+placeholders+") AND archived = 0", args...)
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
	if patches := buildContextStatePatch(event); len(patches) > 0 {
		PatchContextStateMerge(sessionID, patches)
	}
}

// buildContextStatePatch extracts a chat_sessions.context_state patch map from
// a stream event, or nil if the event carries no persistent context state.
// Shared by the immediate persistence path (PersistContextStateFromEvent) and
// the batched flush path (SessionExecutor.pendingContextPatches) so the
// marshaling stays identical.
func buildContextStatePatch(event ai.StreamEvent) map[string]string {
	switch event.Type {
	case "mode_update":
		if event.Mode == nil {
			return nil
		}
		modeJSON, err := json.Marshal(ModeStatePersist{
			CurrentModeID:  event.Mode.CurrentModeID,
			AvailableModes: convertModeDefsFromAI(event.Mode.AvailableModes),
		})
		if err != nil {
			slog.Warn("persist context state: marshal mode", "session", event.Mode.CurrentModeID, "error", err)
			return nil
		}
		return map[string]string{"mode": string(modeJSON)}

	case "thinking_effort_update":
		if event.ThinkingEffort == nil {
			return nil
		}
		effortJSON, err := json.Marshal(ThinkingEffortPersist{
			CurrentID:       event.ThinkingEffort.CurrentID,
			AvailableLevels: convertThinkingEffortDefsFromAI(event.ThinkingEffort.AvailableLevels),
		})
		if err != nil {
			slog.Warn("persist context state: marshal thinking effort", "session", event.ThinkingEffort.CurrentID, "error", err)
			return nil
		}
		return map[string]string{"thinkingEffort": string(effortJSON)}

	case "usage_update":
		if event.Usage == nil {
			return nil
		}
		// UsageStatePersist is a type alias of ai.UsageState, so marshaling the
		// event payload directly carries every field (including the per-agent
		// _meta extensions: cache hit/miss/creation, credit, usageByCategory).
		usageJSON, err := json.Marshal(event.Usage)
		if err != nil {
			slog.Warn("persist context state: marshal usage", "session", event.Usage.TotalTokens, "error", err)
			return nil
		}
		return map[string]string{"usage": string(usageJSON)}
	}
	return nil
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
	if _, err := store.WriteExec("UPDATE chat_sessions SET context_state = ? WHERE id = ?", string(data), sessionID); err != nil {
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
	// usage is a partial update, not a full snapshot: an agent notification
	// carrying only used=0/size=0/cost must never regress a previously-known
	// context window persisted in the DB. Merge it against the stored usage
	// (read-modify-write) before applying the json_set chain — mirror of the
	// in-memory MergeUsageState in internal/ai. Otherwise a naked trailing
	// usage_update wipes context_state.usage to {used:0,size:0} forever.
	if rawUsage, ok := patches["usage"]; ok {
		patches = mergeUsagePatchIntoDB(sessionID, patches, rawUsage)
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
	if _, err := store.WriteExec(query, args...); err != nil {
		slog.Warn("patchContextStateMerge: write failed", "err", err, "sid", sessionID)
	}
}

// mergeUsagePatchIntoDB merges an incoming usage patch against the usage
// currently stored in chat_sessions.context_state so a partial (naked
// used=0/size=0) notification never regresses a known context window. When the
// stored state is absent or the incoming patch is strictly more informative,
// the patch is kept as-is. Returns the (possibly updated) patch map.
func mergeUsagePatchIntoDB(sessionID string, patches map[string]string, rawUsage string) map[string]string {
	var incoming ai.UsageState
	if err := json.Unmarshal([]byte(rawUsage), &incoming); err != nil {
		slog.Warn("mergeUsagePatch: unmarshal incoming usage failed", "err", err, "sid", sessionID)
		return patches
	}
	var stored ai.UsageState
	storedRaw := ""
	// NOTE on the read-modify-write window: this non-atomic SELECT→UPDATE is
	// safe only because each session's usage writes flow through a single
	// SessionExecutor flush loop (single writer per session); concurrent usage
	// writers on the same session would need a lock here.
	if err := store.ReadDB().QueryRow("SELECT json_extract(context_state, '$.usage') FROM chat_sessions WHERE id = ?", sessionID).Scan(&storedRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Session row does not exist (deleted/never created) — nothing to
			// merge or protect; the subsequent UPDATE is a harmless no-op.
			return patches
		}
		slog.Warn("mergeUsagePatch: read stored usage failed", "err", err, "sid", sessionID)
		return patches
	}
	if storedRaw == "" || storedRaw == jsonNull {
		// No prior state — nothing to protect, write the incoming patch as-is.
		return patches
	}
	if err := json.Unmarshal([]byte(storedRaw), &stored); err != nil {
		slog.Warn("mergeUsagePatch: unmarshal stored usage failed", "err", err, "sid", sessionID)
		return patches
	}
	merged := ai.MergeUsageState(&stored, &incoming)
	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		slog.Warn("mergeUsagePatch: marshal merged usage failed", "err", err, "sid", sessionID)
		return patches
	}
	patches["usage"] = string(mergedJSON)
	return patches
}

// GetContextState reads and parses the context_state JSON for a session.
// Returns nil if the column is empty or parsing fails.
func GetContextState(sessionID string) *ContextState {
	var raw string
	if err := store.ReadDB().QueryRow("SELECT COALESCE(context_state, '') FROM chat_sessions WHERE id = ? AND archived = 0", sessionID).Scan(&raw); err != nil || raw == "" {
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
	err := store.ReadDB().QueryRow(
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
	err := store.ReadDB().QueryRow(
		`SELECT s.backend, COALESCE(p.path, ''), s.title, s.agent_id, s.model, COALESCE(s.transport, ''), s.auto_approve
		   FROM chat_sessions s
		   LEFT JOIN projects p ON p.id = s.project_id
		  WHERE s.id = ? AND s.archived = 0`,
		sessionID,
	).Scan(&info.Backend, &info.ProjectPath, &info.Title, &info.AgentID, &info.Model, &info.Transport, &info.AutoApprove)
	if err != nil {
		return nil
	}
	return info
}

// GetSessionProjectPathAny returns a session's project_path whether or not the
// session is archived, plus whether the session exists at all.
//
// This exists for ownership/attribution checks that must still work on an
// archived session: GetSessionFullInfo filters archived=0, so using it to
// resolve "which project owns this session" would silently degrade to an empty
// path for an archived row (which is how a tag could get filed under no project
// at all and become unreachable).
func GetSessionProjectPathAny(sessionID string) (string, bool) {
	var projectPath string
	err := store.ReadDB().QueryRow(
		`SELECT COALESCE(p.path, '') FROM chat_sessions s
		   LEFT JOIN projects p ON p.id = s.project_id
		  WHERE s.id = ?`, sessionID,
	).Scan(&projectPath)
	if err != nil {
		return "", false
	}
	return projectPath, true
}

// GetSessionAgentID returns the agent_id of an active (non-archived) session.
func GetSessionAgentID(sessionID string) string {
	var agentID string
	store.ReadDB().QueryRow("SELECT agent_id FROM chat_sessions WHERE id = ? AND archived = 0", sessionID).Scan(&agentID)
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
	store.ReadDB().QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND role = 'assistant' AND streaming = 0", sessionID).Scan(&count)
	return count
}

// UpdateStreamingMessageByID updates the content of a SPECIFIC streaming
// assistant row, identified by BOTH its session and its id.
//
// Why this exists: UpdateStreamingMessage locates the row by
// `session_id + streaming=1 ORDER BY id DESC LIMIT 1`. That is only correct
// when a session has exactly one in-flight stream. A group turn can run
// several members CONCURRENTLY on one shared timeline (parallel speaking), so
// "the latest streaming row" would let one member's write land on another's
// row. Callers that own a specific placeholder id (the executor, via
// cfg.StreamingMessageID) must use this variant instead.
//
// The session predicate is defense-in-depth, not decoration: FlushStreamingNow
// iterates EVERY registered executor, so a stale executor whose row id happens
// to collide with a live row in another session must not be able to write it.
//
// streaming is left untouched (this is an in-flight content flush, not a
// finalize), matching UpdateStreamingMessage.
func UpdateStreamingMessageByID(sessionID string, messageID int64, content string) error {
	if messageID <= 0 || sessionID == "" {
		return nil
	}
	_, err := store.WriteExec(
		"UPDATE chat_history SET content = ? WHERE id = ? AND session_id = ? AND streaming = 1",
		content, messageID, sessionID,
	)
	return err
}

// UpdateStreamingMessage updates the content of the latest streaming assistant message for a session.
// Uses subquery with ORDER BY id DESC LIMIT 1 to target only the most recent streaming=1 row,
// preventing accidental updates to stale streaming rows left by failed finalizations.
//
// Only valid when the session has ONE in-flight stream (single chat, or a
// sequential group turn). For concurrent streams on one timeline use
// UpdateStreamingMessageByID.
func UpdateStreamingMessage(projectPath, backend, sessionID, content string) error {
	projectID, idErr := store.ProjectIDForPath(projectPath)
	if idErr != nil {
		return idErr
	}
	_, err := store.WriteExec(
		`UPDATE chat_history SET content = ? WHERE id = (
			SELECT id FROM chat_history
			WHERE project_id = ? AND backend = ? AND session_id = ? AND role = 'assistant' AND streaming = 1
			ORDER BY id DESC LIMIT 1
		)`,
		content, projectID, backend, sessionID,
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
	err := store.ReadDB().QueryRow(
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

// CreateStreamingMessage inserts a fresh streaming assistant placeholder and
// returns its id. Used when a mid-turn injection splits an assistant reply: the
// "after" half needs its own streaming row so it finalizes independently of the
// "before" half.
//
// No anchor is needed: the injected question is materialized into chat_history
// at injection time (before this row is created), so the new row's higher id
// already places it directly below that question.
func CreateStreamingMessage(projectPath, backend, sessionID string) (int64, error) {
	return CreateStreamingMessageWithAgent(projectPath, backend, sessionID, "")
}

// CreateStreamingMessageWithAgent is CreateStreamingMessage plus speaker
// attribution (agentID = member row id for group turns, "" otherwise). Needed
// so a mid-turn split's "after" row keeps its speaker.
func CreateStreamingMessageWithAgent(projectPath, backend, sessionID, agentID string) (int64, error) {
	emptyContent, err := json.Marshal(map[string]any{"blocks": []any{}})
	if err != nil {
		return 0, err
	}
	return AddChatMessageWithAgent(projectPath, backend, sessionID, "assistant", string(emptyContent), nil, true, "", agentID)
}

// FinalizeStreamingMessageByID finalizes a SPECIFIC streaming assistant row,
// identified by BOTH its session and its id. The concurrency-safe counterpart
// of FinalizeStreamingMessage: a group turn running several members
// concurrently on one timeline must finalize each member's own placeholder, not
// "the latest streaming row" (which may belong to a sibling). Returns the row
// id (0 if the row was not found or not streaming).
func FinalizeStreamingMessageByID(sessionID string, messageID int64, content string) (int64, error) {
	return finalizeStreamingMessageByID(sessionID, messageID, content, true)
}

// FinalizeCancelledStreamingMessageByID is FinalizeCancelledStreamingMessage by
// explicit session+id. See FinalizeStreamingMessageByID for why the id variant
// is required under concurrent streams.
func FinalizeCancelledStreamingMessageByID(sessionID string, messageID int64, content string) (int64, error) {
	return finalizeStreamingMessageByID(sessionID, messageID, content, false)
}

// finalizeStreamingMessageByID performs the shared finalize write against an
// explicit session+row id. stampCompletedAt carries the same meaning as in
// finalizeStreamingMessage (normal completion vs user cancel).
func finalizeStreamingMessageByID(sessionID string, messageID int64, content string, stampCompletedAt bool) (int64, error) {
	if messageID <= 0 || sessionID == "" {
		return 0, nil
	}
	// Both values are compile-time constants chosen by the branch below, never
	// caller input, so building the SET clause by concatenation is safe.
	completedAtSet := "completed_at = CURRENT_TIMESTAMP"
	if !stampCompletedAt {
		completedAtSet = "completed_at = NULL"
	}
	result, err := store.WriteExec(
		"UPDATE chat_history SET content = ?, streaming = 0, indexed = 0, "+completedAtSet+
			" WHERE id = ? AND session_id = ? AND streaming = 1",
		content, messageID, sessionID,
	)
	if err != nil {
		return 0, err
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return 0, nil
	}
	return messageID, nil
}

// FinalizeStreamingMessage marks the latest streaming assistant message as complete and updates its content.
// Also marks the message as unindexed (indexed=0) so the RAG indexer picks it up.
// Uses subquery with ORDER BY id DESC LIMIT 1 to target only the most recent streaming=1 row,
// preventing accidental finalization of stale streaming rows left by previous failed finalizations.
// Returns the message ID of the finalized message (0 if not found).
//
// Only valid when the session has ONE in-flight stream. For concurrent streams
// on one timeline use FinalizeStreamingMessageByID.
//
// Stamps completed_at with CURRENT_TIMESTAMP — the moment the reply actually
// landed. created_at cannot serve that purpose: it is written when the turn
// starts, so a session read mid-turn would have last_read_at past created_at
// and the finished reply would never register as unread. See the column's
// comment in database.go.
//
// A user-cancelled turn must NOT take this path — see
// FinalizeCancelledStreamingMessage.
func FinalizeStreamingMessage(projectPath, backend, sessionID, content string) (int64, error) {
	return finalizeStreamingMessage(projectPath, backend, sessionID, content, true)
}

// FinalizeCancelledStreamingMessage finalizes a reply the user cancelled while
// watching it. Identical to FinalizeStreamingMessage except that it leaves
// completed_at NULL.
//
// Why the distinction: the cancel action lives in the session the user is
// looking at, so the frontend marks that session read as soon as the
// "cancelled" event arrives — BEFORE the executor finalizes the interrupted
// reply (the agent process has to tear down first, which can take seconds).
// Stamping completed_at at finalize time would move the reply's timestamp past
// that read and flip the session back to unread even though the user is staring
// at it — reintroducing exactly the bug e76a6d960 fixed. Leaving completed_at
// NULL lets the unread query fall back to created_at (the turn start, which
// precedes the cancel-time read), so the session stays read.
//
// A normal completion must not take this path: there the user may well have
// switched away before the reply landed, so the landing time is what decides
// whether it is unread.
func FinalizeCancelledStreamingMessage(projectPath, backend, sessionID, content string) (int64, error) {
	return finalizeStreamingMessage(projectPath, backend, sessionID, content, false)
}

// finalizeStreamingMessage performs the shared finalize write. stampCompletedAt
// selects whether the row records the moment it landed (normal completion) or
// stays NULL (user cancel, so the unread query falls back to created_at).
func finalizeStreamingMessage(projectPath, backend, sessionID, content string, stampCompletedAt bool) (int64, error) {
	// Both values are compile-time constants chosen by the branch below, never
	// caller input, so building the SET clause by concatenation is safe.
	completedAtSet := "completed_at = CURRENT_TIMESTAMP"
	if !stampCompletedAt {
		completedAtSet = "completed_at = NULL"
	}
	projectID, idErr := store.ProjectIDForPath(projectPath)
	if idErr != nil {
		return 0, idErr
	}
	result, err := store.WriteExec(
		`UPDATE chat_history SET content = ?, streaming = 0, indexed = 0, `+completedAtSet+` WHERE id = (
			SELECT id FROM chat_history
			WHERE project_id = ? AND backend = ? AND session_id = ? AND role = 'assistant' AND streaming = 1
			ORDER BY id DESC LIMIT 1
		)`,
		content, projectID, backend, sessionID,
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
	err = store.ReadDB().QueryRow(
		"SELECT id FROM chat_history WHERE project_id = ? AND backend = ? AND session_id = ? AND role = 'assistant' AND streaming = 0 ORDER BY id DESC LIMIT 1",
		projectID, backend, sessionID,
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
	err := store.ReadDB().QueryRow(
		"SELECT id FROM chat_history WHERE session_id = ? AND role = 'assistant' AND streaming = 1 ORDER BY id DESC LIMIT 1",
		sessionID,
	).Scan(&id)
	if err == nil {
		return id
	}
	// Fallback: latest finalized message
	err = store.ReadDB().QueryRow(
		"SELECT id FROM chat_history WHERE session_id = ? AND role = 'assistant' AND streaming = 0 ORDER BY id DESC LIMIT 1",
		sessionID,
	).Scan(&id)
	if err != nil {
		return 0
	}
	return id
}

// GetLiveRunState returns the state a client that just subscribed needs in order
// to render a run already in flight: the streaming assistant row's id, the
// question it answers (the nearest preceding user row), and the streaming row's
// speaker (group-member row id; empty for ordinary single-agent turns). Backs
// ws.StreamHub.EmitLiveRunStateToClient.
//
// Unlike GetStreamingMessageID this does NOT fall back to a finalized message:
// the caller's whole purpose is "a turn is running, hand me its state", and
// emitting a stream_start for an idle session would open a phantom placeholder.
// messageID is 0 when nothing is streaming.
//
// The question is resolved by id order (the greatest user id below the
// streaming row), NOT by a queue id: in this model a queued message is
// materialized into chat_history immediately before the reply it produces, so
// id order IS the conversation order. The old queue-id lookup also broke for
// rows whose queue_id was empty. questionID is 0 for a run with no preceding
// user row (e.g. some scheduled runs).
//
// speakerID MUST travel with the re-emitted stream_start: the subscribe-time
// recovery is how a client that switched away and back learns about a live
// turn, and without the speaker the placeholder it creates is speakerless —
// the group speaker header then stays missing for the rest of the turn (the
// frontend's db_load merge can heal it only because it also adopts agentId;
// see rebuildFromDb).
//
// Returns the NEWEST stream only. Callers that must recover a session with
// several concurrent streams (parallel group speaking) use
// GetLiveRunStates instead.
func GetLiveRunState(sessionID string) (messageID int64, questionID int64, questionContent, speakerID string) {
	states := GetLiveRunStates(sessionID)
	if len(states) == 0 {
		return 0, 0, "", ""
	}
	st := states[0]
	return st.MessageID, st.QuestionID, st.QuestionContent, st.SpeakerID
}

// GetLiveRunStates returns EVERY in-flight stream for a session, newest row
// first. A session normally has one, but a group turn can run several members
// CONCURRENTLY on one timeline (parallel speaking), and a client that subscribes
// mid-flight must be told about all of them or it recovers only one bubble.
//
// Each entry resolves its own question (the greatest user id below that
// streaming row), so each recovered bubble is anchored to the right question.
func GetLiveRunStates(sessionID string) []ws.LiveStreamState {
	states := queryLiveStreamRows(sessionID)

	// Resolve each stream's question independently (id order is the conversation
	// order — see GetLiveRunState). Done AFTER the row cursor is closed: the read
	// pool is small (2 connections), and issuing a second query while a cursor is
	// still open holds one connection per in-flight stream.
	for i := range states {
		var qID int64
		var qContent string
		if qErr := store.ReadDB().QueryRow(
			"SELECT id, content FROM chat_history WHERE session_id = ? AND role = 'user' AND id < ? ORDER BY id DESC LIMIT 1",
			sessionID, states[i].MessageID,
		).Scan(&qID, &qContent); qErr == nil {
			states[i].QuestionID = qID
			states[i].QuestionContent = qContent
		}
	}
	return states
}

// queryLiveStreamRows reads the streaming assistant rows for a session (newest
// first) and closes its cursor before returning, so the caller may issue further
// queries without holding a read-pool connection.
func queryLiveStreamRows(sessionID string) []ws.LiveStreamState {
	rows, err := store.ReadDB().Query(
		"SELECT id, COALESCE(agent_id, '') FROM chat_history WHERE session_id = ? AND role = 'assistant' AND streaming = 1 ORDER BY id DESC",
		sessionID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var states []ws.LiveStreamState
	for rows.Next() {
		var st ws.LiveStreamState
		if err := rows.Scan(&st.MessageID, &st.SpeakerID); err != nil || st.MessageID <= 0 {
			continue
		}
		states = append(states, st)
	}
	// A mid-iteration error means the list is partial; returning it would make
	// the caller believe fewer streams are live than there are. Drop the whole
	// result so the caller falls back to a full reload.
	if err := rows.Err(); err != nil {
		slog.Warn("queryLiveStreamRows: rows error, returning no live state",
			slog.String("session", sessionID), slog.String("err", err.Error()))
		return nil
	}
	return states
}

// UpdateMessageContent updates the content of a specific message by its ID.
func UpdateMessageContent(messageID int, content string) error {
	_, err := store.WriteExec("UPDATE chat_history SET content = ? WHERE id = ?", content, messageID)
	return err
}

// UpdateExternalSessionID sets the external session ID for a ClawBench session.
func UpdateExternalSessionID(sessionID, externalID string) error {
	_, err := store.WriteExec("UPDATE chat_sessions SET external_session_id = ? WHERE id = ?", externalID, sessionID)
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
	_, _ = store.WriteExec("UPDATE chat_sessions SET external_session_id = '' WHERE id = ?", sessionID)
}

// MarkSessionCompacted flags a session whose context was just compacted by the
// agent (auto compaction or a user-issued /compact). The next turn re-injects
// the system prompt once, then clears the flag — see ConsumeSessionCompacted.
//
// Best-effort: losing the flag degrades to the pre-existing behavior (no extra
// injection) rather than failing the turn, so errors are logged only.
func MarkSessionCompacted(sessionID string) {
	if sessionID == "" {
		return
	}
	if _, err := store.WriteExec("UPDATE chat_sessions SET compacted = 1 WHERE id = ?", sessionID); err != nil {
		slog.Warn("markSessionCompacted: write failed", "err", err, "sid", sessionID)
		return
	}
	slog.Info("session compacted: system prompt will be re-injected on the next turn",
		slog.String("session", sessionID))
}

// ConsumeSessionCompacted reports whether the session was compacted since the
// last turn and clears the flag in the same statement.
//
// Read-and-clear is deliberate: the requirement is "inject on the NEXT message
// only". A plain read would keep re-injecting for every later turn until some
// other path cleared it, and a clear-then-read pair would drop the flag when two
// sends race. One conditional UPDATE makes the check and the clear atomic:
// RowsAffected == 1 means this caller is the one that observed the flag, so
// exactly one turn re-injects.
func ConsumeSessionCompacted(sessionID string) bool {
	if sessionID == "" || !store.DBReady() {
		return false
	}
	res, err := store.WriteExec("UPDATE chat_sessions SET compacted = 0 WHERE id = ? AND compacted = 1", sessionID)
	if err != nil {
		slog.Warn("consumeSessionCompacted: write failed", "err", err, "sid", sessionID)
		return false
	}
	affected, err := res.RowsAffected()
	if err != nil {
		slog.Warn("consumeSessionCompacted: rows affected failed", "err", err, "sid", sessionID)
		return false
	}
	return affected == 1
}

// GetExternalSessionID returns the external session ID for a ClawBench session.
func GetExternalSessionID(sessionID string) string {
	if !store.ReadDBReady() {
		return ""
	}
	var externalID string
	err := store.ReadDB().QueryRow("SELECT external_session_id FROM chat_sessions WHERE id = ?", sessionID).Scan(&externalID)
	if err != nil {
		return ""
	}
	return externalID
}

// HardDeleteSession removes a session and all its associated data regardless
// of deletion status. Used by ACP LoadSession to clean up existing sessions
// before recreating them with fresh replay data, and by DestroySession for
// user-initiated permanent deletion.
// Deletes in order: chat_tool_calls → summaries →
// tts_summaries → chat_history → task_executions → chat_sessions.
//
// chat_metadata (the usage ledger) is deliberately NOT deleted: it records
// tokens/cost that were really consumed, and must survive so usage statistics
// do not under-count. It has no foreign key to chat_history, so removing the
// history rows leaves it intact.
func HardDeleteSession(sessionID string) error {
	tx, err := store.WriteBegin()
	if err != nil {
		return err
	}
	defer store.WriteUnlock()
	defer tx.Rollback()

	_, _ = tx.Exec("DELETE FROM chat_tool_calls WHERE session_id = ?", sessionID)
	_, _ = tx.Exec("DELETE FROM chat_thinking WHERE session_id = ?", sessionID)
	// Delete summaries and tts_summaries before chat_history (they reference chat_history.id)
	_, _ = tx.Exec("DELETE FROM summaries WHERE target_type = 'chat_message' AND target_id IN (SELECT id FROM chat_history WHERE session_id = ?)", sessionID)
	_, _ = tx.Exec("DELETE FROM tts_summaries WHERE message_id IN (SELECT id FROM chat_history WHERE session_id = ?)", sessionID)
	_, _ = tx.Exec("DELETE FROM chat_history WHERE session_id = ?", sessionID)
	_, _ = tx.Exec("DELETE FROM task_executions WHERE session_id = ?", sessionID)
	// Revoke the conversation share link: the frozen snapshot is a copy of the
	// messages being deleted, so leaving it readable would defeat the deletion.
	// (Archived sessions keep their share — their messages survive.)
	_, _ = tx.Exec("DELETE FROM session_shares WHERE session_id = ?", sessionID)
	// Drop the session's tag links too: the link table has no FK to
	// chat_sessions, so without this the rows would linger forever. The tag
	// definitions themselves are preserved (other sessions may use them).
	_, _ = tx.Exec("DELETE FROM session_tag_links WHERE session_id = ?", sessionID)
	// /btw side questions belong to the conversation they were asked about;
	// without this they would linger as orphan markers.
	_, _ = tx.Exec("DELETE FROM btw_questions WHERE session_id = ?", sessionID)
	// A group's member rows are independent chat_sessions rows (session_type
	// 'group_member', group_id = the group). Deleting only the group row would
	// leave every member row behind forever, orphaned and invisible (decision
	// #48). Cascade them in the SAME transaction as the group row, before it is
	// deleted (the group_id link points at the row being removed).
	_, _ = tx.Exec("DELETE FROM chat_sessions WHERE group_id = ? AND session_type = ?", sessionID, groupMemberSessionType)
	// A group's undelivered private notes belong to the group; without this
	// they would linger forever (the table has no FK to chat_sessions).
	_, _ = tx.Exec("DELETE FROM group_pending_bcc WHERE group_id = ?", sessionID)
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
// and child rows (tool calls, thinking, summaries) then inserts
// the new messages, all in one transaction — on any error the transaction rolls
// back so the original history is preserved. Returns the number of messages
// inserted.
//
// chat_metadata (the usage ledger) is intentionally NOT deleted. Replayed
// messages carry no usage data (only {"transport":"acp"}), so replacing the
// history cannot double-count; keeping the ledger preserves the real token/cost
// consumed by the messages being replaced.
func ReplaceSessionHistory(sessionID, projectPath, backend string, messages []ReplayMessage) (int, error) {
	// Resolved before store.WriteBegin: store.ProjectIDForPath writes on a cache miss, and
	// the write mutex is held for the whole transaction below.
	projectID, idErr := store.ProjectIDForPath(projectPath)
	if idErr != nil {
		return 0, idErr
	}
	tx, err := store.WriteBegin()
	if err != nil {
		return 0, err
	}
	defer store.WriteUnlock()
	defer tx.Rollback()

	_, _ = tx.Exec("DELETE FROM chat_tool_calls WHERE session_id = ?", sessionID)
	_, _ = tx.Exec("DELETE FROM chat_thinking WHERE session_id = ?", sessionID)
	_, _ = tx.Exec("DELETE FROM summaries WHERE target_type = 'chat_message' AND target_id IN (SELECT id FROM chat_history WHERE session_id = ?)", sessionID)
	_, _ = tx.Exec("DELETE FROM tts_summaries WHERE message_id IN (SELECT id FROM chat_history WHERE session_id = ?)", sessionID)
	// The replayed history replaces the whole conversation, so the /btw markers
	// anchored into the old message ids no longer point at anything.
	_, _ = tx.Exec("DELETE FROM btw_questions WHERE session_id = ?", sessionID)
	if _, err := tx.Exec("DELETE FROM chat_history WHERE session_id = ?", sessionID); err != nil {
		return 0, err
	}

	for _, m := range messages {
		res, err := tx.Exec(
			"INSERT INTO chat_history (project_id, backend, session_id, role, content, streaming, indexed, external_message_id) VALUES (?, ?, ?, ?, ?, 0, 0, ?)",
			projectID, backend, sessionID, m.Role, m.Content, m.ExtMsgID,
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
	if len(parsed.Metadata) > 0 && string(parsed.Metadata) != jsonNull {
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

	rows, err := store.ReadDB().Query(query, args...)
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
	//
	// Group sessions skip the content stripping: the group render pipeline
	// parses mention tags (`<clawbench-mention>`) and private notes (the same
	// tag with the `private` attribute) out of the BLOCKS, and stripping
	// replaces blocks with [] — so a speaker's private-note card (and the
	// routing chips) vanished as soon as a message gained a summary and the
	// view was reloaded (switch session and back). Group messages are short;
	// the bandwidth saving is not worth losing the structured view. A
	// non-group session keeps the original behavior.
	isGroup := len(messages) > 0 && GetSessionType(messages[0].SessionID) == groupSessionType
	for i := range messages {
		if messages[i].Role == "assistant" {
			if summary, ok := summaryMap[messages[i].ID]; ok {
				messages[i].Summary = &summary
			}
			if cards, ok := cardMap[messages[i].ID]; ok {
				messages[i].SummaryCards = cards
			}
			if !isGroup && messages[i].Summary != nil && *messages[i].Summary != "" && !messages[i].Streaming {
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
	if !store.ReadDBReady() {
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
		if err := store.ReadDB().QueryRow(
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
