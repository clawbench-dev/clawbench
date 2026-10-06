package store

import (
	"strings"
	"time"
)

// Session archive filter values for session search. "all" includes both active
// and archived sessions.
const (
	SessionArchiveFilterAll      = "all"
	SessionArchiveFilterActive   = "active"
	SessionArchiveFilterArchived = "archived"
)

// Session sort order values for session search. "relevance" keeps the
// search-engine ordering (score desc); the time orders re-sort the result set
// by the session's displayed timestamp.
const (
	SessionSortRelevance = "relevance"
	SessionSortNewest    = "newest"
	SessionSortOldest    = "oldest"
)

// groupMemberSessionTypeForQuery is the session_type of a hidden group member
// row (mirrors service.groupMemberSessionType). A member that left a group is
// archived=1 by design and must never be treated as retention-expired
// (decision #48); this package cannot import service, so the value is
// duplicated here.
const groupMemberSessionTypeForQuery = "group_member"

// NormalizeSessionArchiveFilter maps a raw filter string to a known value,
// defaulting to "all" for empty/unknown input.
func NormalizeSessionArchiveFilter(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case SessionArchiveFilterActive:
		return SessionArchiveFilterActive
	case SessionArchiveFilterArchived:
		return SessionArchiveFilterArchived
	default:
		return SessionArchiveFilterAll
	}
}

// NormalizeSessionSortOrder maps a raw sort string to a known value, defaulting
// to "relevance" for empty/unknown input.
func NormalizeSessionSortOrder(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case SessionSortNewest:
		return SessionSortNewest
	case SessionSortOldest:
		return SessionSortOldest
	default:
		return SessionSortRelevance
	}
}

// Session type filter values for session search. "task" is the user-facing name
// for sessions whose stored session_type is 'scheduled' (one per task
// execution); "chat" is an interactive conversation.
const (
	SessionTypeFilterAll  = "all"
	SessionTypeFilterChat = "chat"
	SessionTypeFilterTask = "task"
)

// NormalizeSessionTypeFilter maps a raw type filter to a known value, defaulting
// to "all" for empty/unknown input.
func NormalizeSessionTypeFilter(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case SessionTypeFilterChat:
		return SessionTypeFilterChat
	case SessionTypeFilterTask:
		return SessionTypeFilterTask
	default:
		return SessionTypeFilterAll
	}
}

// SessionTypeDBValue maps a type filter to the value stored in
// chat_sessions.session_type, or "" when the filter is unfiltered ("all").
// Callers use the empty string as "no predicate".
func SessionTypeDBValue(filter string) string {
	switch NormalizeSessionTypeFilter(filter) {
	case SessionTypeFilterChat:
		return "chat"
	case SessionTypeFilterTask:
		return "scheduled"
	default:
		return ""
	}
}

// RecentSession is a lightweight listing row used by session search's "browse
// all" mode (empty query): every chat session for the project, newest first,
// including archived ones that can still be resumed/restored. It deliberately
// carries no message content — the browse list must stay cheap, so the first
// message is fetched lazily only when a session's detail view is opened.
type RecentSession struct {
	ID          string
	Title       string
	Backend     string
	ProjectPath string
	Archived    bool
	CreatedAt   time.Time
	// SessionType is the raw stored value: "chat" or "scheduled". Browse mode
	// only ever lists "chat" rows, but the field is populated so callers can
	// render a type badge without special-casing browse mode.
	SessionType string
}

// GetRecentSessions returns chat sessions for a project in the given time
// order, including archived ones. When projectPath is empty it returns sessions
// across all projects (CLI global browse). limit <= 0 returns all sessions.
// archiveFilter narrows to active/archived (or all); fromTime/toTime (both
// optional, "2006-01-02 15:04:05" local text) bound the session creation time;
// sortOrder selects newest/oldest time ordering (relevance falls back to newest
// here, since browse mode has no search score).
//
// typeFilter narrows by session type. Browse mode is deliberately limited to
// interactive sessions: "all"/"chat" both list session_type='chat', and only an
// explicit "task" switches to 'scheduled'. Selecting "task" therefore lists task
// executions instead of conversations — it never mixes the two.
//
// Cursor pagination: pass the last row's created_at (formatted "2006-01-02
// 15:04:05") and id to fetch the next page. The returned bool reports whether
// more rows remain after this page.
//
//nolint:errcheck,gocyclo,noctx // legacy query moved from service; rationale documented at the call site
func GetRecentSessions(projectPath string, limit int, archiveFilter, typeFilter, sortOrder, fromTime, toTime, cursor, cursorID string) ([]RecentSession, bool, error) {
	// Browse mode never mixes session types: each selection lists exactly one
	// type, and "all" means "all conversations" (not "conversations + tasks").
	// "Conversations" now includes group chats, which are user-visible sessions.
	sessionTypes := []string{"chat", "group"}
	if NormalizeSessionTypeFilter(typeFilter) == SessionTypeFilterTask {
		sessionTypes = []string{"scheduled", "scheduled"}
	}
	query := `SELECT s.id, s.title, s.backend, COALESCE(p.path, ''), s.archived, s.created_at, s.session_type
		FROM chat_sessions s
		LEFT JOIN projects p ON p.id = s.project_id
		WHERE s.session_type IN (?, ?)`
	args := []interface{}{sessionTypes[0], sessionTypes[1]}
	if projectPath != "" {
		projectID, idErr := ProjectIDForPath(projectPath)
		if idErr != nil {
			return nil, false, idErr
		}
		query += " AND s.project_id = ?"
		args = append(args, projectID)
	}
	switch NormalizeSessionArchiveFilter(archiveFilter) {
	case SessionArchiveFilterActive:
		query += " AND s.archived = 0"
	case SessionArchiveFilterArchived:
		query += " AND s.archived = 1"
	}
	if fromTime != "" {
		query += " AND s.created_at >= ?"
		args = append(args, fromTime)
	}
	if toTime != "" {
		query += " AND s.created_at <= ?"
		args = append(args, toTime)
	}
	oldestFirst := NormalizeSessionSortOrder(sortOrder) == SessionSortOldest
	if cursor != "" && cursorID != "" {
		// Tie-break on id so rows sharing a timestamp are neither skipped nor
		// duplicated across pages.
		if oldestFirst {
			query += " AND (s.created_at > ? OR (s.created_at = ? AND s.id > ?))"
		} else {
			query += " AND (s.created_at < ? OR (s.created_at = ? AND s.id < ?))"
		}
		args = append(args, cursor, cursor, cursorID)
	}
	if oldestFirst {
		query += " ORDER BY s.created_at ASC, s.id ASC"
	} else {
		query += " ORDER BY s.created_at DESC, s.id DESC"
	}
	if limit > 0 {
		// Fetch one extra row to detect whether a further page exists.
		query += " LIMIT ?"
		args = append(args, limit+1)
	}

	rows, err := dbRead.Query(query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	sessions := []RecentSession{}
	for rows.Next() {
		var s RecentSession
		var archived int
		if err := rows.Scan(&s.ID, &s.Title, &s.Backend, &s.ProjectPath, &archived, &s.CreatedAt, &s.SessionType); err != nil {
			return nil, false, err
		}
		s.Archived = archived != 0
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	hasMore := false
	if limit > 0 && len(sessions) > limit {
		hasMore = true
		sessions = sessions[:limit]
	}
	return sessions, hasMore, nil
}

// SearchSessionsByTitle matches sessions whose title contains every term, newest
// first.
//
// Session search indexes message content, so a session the user renamed — or
// one whose auto-title was truncated to 50 runes — is unreachable by the words
// in its name. This is the name channel that runs alongside the content
// channel; callers merge the two.
//
// terms are matched with AND: each must appear somewhere in the title, so a
// multi-term query narrows instead of widening. Terms are supplied by the
// caller rather than derived here because segmentation lives in the rag package
// (which imports this one), and both channels must split a query identically.
// An empty terms slice returns no matches — callers that want the whole project
// use GetRecentSessions.
//
// The filters mirror the content channel's (project, archive, type, time range,
// plus the single-session and excluded-session scopes) so both channels draw
// from the same population. sessionID narrows to one session, excludeSessionID
// drops one — the latter is how the /cb-chatsearch command keeps the current
// conversation out of its own results.
//
// The match is case-insensitive for ASCII and literal for everything else:
// SQLite's LIKE folds A-Z only, and a Chinese title has no case.
// limit <= 0 returns every match.
//
//nolint:errcheck,gocyclo,noctx // legacy query moved from service; rationale documented at the call site
func SearchSessionsByTitle(projectPath string, terms []string, limit int, archiveFilter, typeFilter, fromTime, toTime, sessionID, excludeSessionID string) ([]RecentSession, error) {
	if len(terms) == 0 {
		return []RecentSession{}, nil
	}
	// No DB (RAG running standalone, or early startup): there are no titles to
	// search, and querying would dereference a nil handle.
	if !DBReady() {
		return []RecentSession{}, nil
	}

	sessionTypes := []string{"chat", "group"}
	if NormalizeSessionTypeFilter(typeFilter) == SessionTypeFilterTask {
		sessionTypes = []string{"scheduled", "scheduled"}
	}
	query := `SELECT s.id, s.title, s.backend, COALESCE(p.path, ''), s.archived, s.created_at, s.session_type
		FROM chat_sessions s
		LEFT JOIN projects p ON p.id = s.project_id
		WHERE s.session_type IN (?, ?)`
	args := []interface{}{sessionTypes[0], sessionTypes[1]}
	if projectPath != "" {
		projectID, idErr := ProjectIDForPath(projectPath)
		if idErr != nil {
			return nil, idErr
		}
		query += " AND s.project_id = ?"
		args = append(args, projectID)
	}
	switch NormalizeSessionArchiveFilter(archiveFilter) {
	case SessionArchiveFilterActive:
		query += " AND s.archived = 0"
	case SessionArchiveFilterArchived:
		query += " AND s.archived = 1"
	}
	if fromTime != "" {
		query += " AND s.created_at >= ?"
		args = append(args, fromTime)
	}
	if toTime != "" {
		query += " AND s.created_at <= ?"
		args = append(args, toTime)
	}
	if sessionID != "" {
		query += " AND s.id = ?"
		args = append(args, sessionID)
	}
	if excludeSessionID != "" {
		query += " AND s.id != ?"
		args = append(args, excludeSessionID)
	}
	// One LIKE per term, ANDed: every term must appear somewhere in the title.
	// ESCAPE '\' keeps a literal % / _ in the query from acting as a wildcard.
	for _, term := range terms {
		query += " AND s.title LIKE ? ESCAPE '\\'"
		args = append(args, "%"+escapeLikePattern(term)+"%")
	}
	query += " ORDER BY s.created_at DESC, s.id DESC"
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
		if err := rows.Scan(&s.ID, &s.Title, &s.Backend, &s.ProjectPath, &archived, &s.CreatedAt, &s.SessionType); err != nil {
			return nil, err
		}
		s.Archived = archived != 0
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// escapeLikePattern escapes the SQL LIKE wildcards in s so it matches
// literally. Callers must pair it with `ESCAPE '\'` — without the clause the
// backslashes are treated as ordinary characters and the escape is inert.
func escapeLikePattern(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_")
	return r.Replace(s)
}

// EscapeLikePatternForTest exposes escapeLikePattern so the external test
// package can assert the escaping directly. Must only be called from _test.go.
func EscapeLikePatternForTest(s string) string { return escapeLikePattern(s) }

// GetSessionTitlesBatchIncludeArchived fetches titles for multiple sessions
// including archived ones. Used by RAG search to show titles even for
// archived sessions whose chunks are still indexed.
//
//nolint:errcheck,noctx // legacy query moved from service; rationale documented at the call site
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
//
//nolint:errcheck,noctx // legacy query moved from service; rationale documented at the call site
func GetUnindexedMessages(limit int) ([]UnindexedMessage, error) {
	rows, err := dbRead.Query(
		`SELECT h.id, h.content, h.role, h.session_id, COALESCE(p.path, ''), h.backend, h.created_at
		   FROM chat_history h
		   LEFT JOIN projects p ON p.id = h.project_id
		  WHERE h.indexed = 0 AND h.streaming = 0
		  ORDER BY h.created_at DESC LIMIT ?`,
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
//
//nolint:noctx // legacy query moved from service; rationale documented at the call site
func UnindexedCount() (int, error) {
	var count int
	err := dbRead.QueryRow("SELECT COUNT(*) FROM chat_history WHERE indexed = 0 AND streaming = 0").Scan(&count)
	return count, err
}

// TotalMessageCount returns the total number of finalized (non-streaming) messages.
//
//nolint:noctx // legacy query moved from service; rationale documented at the call site
func TotalMessageCount() (int, error) {
	var count int
	err := dbRead.QueryRow("SELECT COUNT(*) FROM chat_history WHERE streaming = 0").Scan(&count)
	return count, err
}

// IndexedMessageCount returns the number of messages that have been indexed by RAG.
//
//nolint:noctx // legacy query moved from service; rationale documented at the call site
func IndexedMessageCount() (int, error) {
	var count int
	err := dbRead.QueryRow("SELECT COUNT(*) FROM chat_history WHERE indexed = 1 AND streaming = 0").Scan(&count)
	return count, err
}

// MessageIndexCounts returns (total, indexed) message counts in a single query.
//
//nolint:noctx // legacy query moved from service; rationale documented at the call site
func MessageIndexCounts() (total int, indexed int, err error) {
	err = dbRead.QueryRow(
		"SELECT COUNT(*), COALESCE(SUM(CASE WHEN indexed = 1 THEN 1 ELSE 0 END), 0) FROM chat_history WHERE streaming = 0",
	).Scan(&total, &indexed)
	return
}

// GetExpiredArchivedSessions returns session IDs of archived sessions
// whose updated_at (set to archive time) is older than the cutoff.
//
//nolint:errcheck,noctx // legacy query moved from service; rationale documented at the call site
func GetExpiredArchivedSessions(cutoff time.Time) ([]string, error) {
	// Exclude hidden group member rows (session_type='group_member'). A member
	// that LEFT a group is archived=1 by design and must be retained: its past
	// speech in the group timeline is attributed by its row id (decision #48).
	// Without this filter, enabling ArchiveRetention would treat "left the
	// group" as "expired" and hard-delete the row, orphaning that attribution.
	rows, err := dbRead.Query("SELECT id FROM chat_sessions WHERE archived = 1 AND session_type != ? AND updated_at < ?", groupMemberSessionTypeForQuery, cutoff)
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
// Deletes in order: chat_tool_calls → summaries → tts_summaries →
// chat_history → task_executions → chat_sessions.
// Returns counts of purged sessions and messages.
//
// chat_metadata (the usage ledger) is deliberately left untouched — see
// HardDeleteSession. Its rows have no foreign key to chat_history, so the
// usage history for purged sessions remains queryable.
//
//nolint:errcheck,noctx // legacy query moved from service; rationale documented at the call site
func PurgeArchivedData(sessionIDs []string) (sessionsPurged int64, messagesPurged int64, err error) {
	if len(sessionIDs) == 0 {
		return 0, 0, nil
	}

	tx, err := WriteBegin()
	if err != nil {
		return 0, 0, err
	}
	defer WriteUnlock()
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

	// Revoke conversation share links first: the snapshot is a copy of the
	// messages about to be purged.
	_, _ = tx.Exec("DELETE FROM session_shares WHERE session_id IN ("+placeholders+")", args...)

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

	// Delete session tag links for purged sessions. The link table has no FK to
	// chat_sessions, so without this the rows survive the purge forever and the
	// tag's session count stays permanently inflated (HardDeleteSession does the
	// same cleanup; this retention path was missed).
	_, _ = tx.Exec("DELETE FROM session_tag_links WHERE session_id IN ("+placeholders+")", args...)

	// Delete /btw side questions for purged sessions (no FK to chat_sessions).
	_, _ = tx.Exec("DELETE FROM btw_questions WHERE session_id IN ("+placeholders+")", args...)

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
