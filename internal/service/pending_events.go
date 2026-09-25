//nolint:govet,noctx // db global singleton, context not applicable
package service

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"clawbench/internal/ws"
)

// PendingEvent represents a persisted event for offline clients.
type PendingEvent struct {
	ID        int64  `json:"-"`
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	Payload   string `json:"payload"`
	ExpiresAt string `json:"expires_at"`
	CreatedAt string `json:"created_at"`
	// SuppressNotification marks an event whose subject (session / task
	// execution) has already been read. The client must still advance its
	// cursor over the event — only the notification is withheld. Without this
	// flag the offline-recovery path re-notifies replies the user already
	// read, because "read" lives in chat_sessions.last_read_at /
	// task_executions.read_at while the event log is a separate table that
	// nothing prunes on read.
	//
	// The polarity is deliberately opt-in (absent ⇒ notify): an older server
	// that does not set it keeps today's behavior, and an older client that
	// does not read it is unaffected. Inverting it would make every event
	// silently non-notifying for clients that predate the field.
	SuppressNotification bool `json:"suppress_notification,omitempty"`
}

const (
	// pendingEventTTL is the default TTL for terminal events (completed/cancelled/failed).
	pendingEventTTL = 24 * time.Hour
	// pendingEventPermPendTTL is the TTL for permission_pending events (7 days).
	pendingEventPermPendTTL = 7 * 24 * time.Hour
	// pendingEventMaxRows is the maximum total rows in pending_events.
	pendingEventMaxRows = 1000
	// statusCancelled is the cancelled status string used across event types.
	statusCancelled = "cancelled"
	// statusCompleted is the completed status string used across event types.
	statusCompleted = "completed"
	// eventTypeTaskUpdate is the event name for scheduled-task run updates. It is
	// referenced by the notifiable predicate, the read gate and the subject
	// extractor, so it lives in one place to keep them from drifting.
	eventTypeTaskUpdate = "task_update"
	// statusFailed is the failed status string for task runs.
	statusFailed = "failed"
)

// IsNotifiableEvent returns true if the event is a terminal state that
// should be persisted for offline clients.
func IsNotifiableEvent(event string, data any) bool {
	var status string
	switch d := data.(type) {
	case *ws.SessionUpdateData:
		status = d.Status
	case *ws.TaskUpdateData:
		status = d.Status
	case ws.ChatStreamData: // value type — what StreamHub.Emit constructs
		if d.EventType == eventTypeUserMessage {
			return true
		}
		return false
	case *ws.ChatStreamData: // pointer variant for compatibility
		if d.EventType == eventTypeUserMessage {
			return true
		}
		return false
	case map[string]any:
		if s, ok := d["status"].(string); ok {
			status = s
		}
	default:
		return false
	}
	switch event {
	case eventTypeSessionUpdate:
		return status == statusCompleted || status == statusCancelled || status == "permission_pending"
	case eventTypeTaskUpdate:
		return status == statusCompleted || status == statusFailed || status == statusCancelled
	default:
		return false
	}
}

// pendingEventExpiresAt returns the expires_at value for an event type.
// permission_pending events get 7-day TTL; others get 24h.
func pendingEventExpiresAt(event, status string) string {
	if event == eventTypeSessionUpdate && status == "permission_pending" {
		return time.Now().Add(pendingEventPermPendTTL).UTC().Format(time.RFC3339)
	}
	return time.Now().Add(pendingEventTTL).UTC().Format(time.RFC3339)
}

// StorePendingEvent persists a notifiable event to the global event log.
func StorePendingEvent(eventID, eventType, payload, expiresAt string) error {
	if db == nil {
		return nil
	}
	_, err := WriteExec(
		`INSERT OR IGNORE INTO pending_events (event_id, event_type, payload, expires_at) VALUES (?, ?, ?, ?)`,
		eventID, eventType, payload, expiresAt,
	)
	return err
}

// DeletePendingEvent removes a specific event from the pending event log.
// Used after DingTalk push succeeds to prevent duplicate Android notifications.
func DeletePendingEvent(eventID string) error {
	if db == nil {
		return nil
	}
	_, err := WriteExec(
		`DELETE FROM pending_events WHERE event_id = ?`,
		eventID,
	)
	return err
}

// GetPendingEvents returns non-expired events optionally after a cursor event_id.
// Results are ordered by id ASC. If the cursor event_id has expired and been
// cleaned up, returns an empty slice (client should reset cursor and re-fetch).
//
// Events whose session/execution has already been read are returned with
// SuppressNotification set rather than dropped: the client still needs to
// advance its cursor past them (otherwise the next fetch would re-read them
// forever), but must not raise a notification for a reply the user has seen.
// See suppressAlreadyReadEvents.
func GetPendingEvents(afterEventID string) ([]PendingEvent, error) {
	if db == nil || dbRead == nil {
		return nil, nil
	}

	var rows *sql.Rows
	var err error
	if afterEventID != "" {
		// Check if cursor event still exists; if not, return empty
		// to signal client to reset cursor
		var cursorExists int
		if err := dbRead.QueryRow(
			`SELECT COUNT(*) FROM pending_events WHERE event_id = ?`,
			afterEventID,
		).Scan(&cursorExists); err != nil {
			return nil, err
		}
		if cursorExists == 0 {
			return []PendingEvent{}, nil
		}
		rows, err = dbRead.Query(
			`SELECT event_id, event_type, payload, expires_at, created_at
			 FROM pending_events
			 WHERE expires_at >= datetime('now')
			   AND id > (SELECT id FROM pending_events WHERE event_id = ?)
			 ORDER BY id ASC`,
			afterEventID,
		)
	} else {
		rows, err = dbRead.Query(
			`SELECT event_id, event_type, payload, expires_at, created_at
			 FROM pending_events
			 WHERE expires_at >= datetime('now')
			 ORDER BY id ASC`,
		)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var events []PendingEvent
	for rows.Next() {
		var e PendingEvent
		if err := rows.Scan(&e.EventID, &e.EventType, &e.Payload, &e.ExpiresAt, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	suppressAlreadyReadEvents(events)
	return events, nil
}

// suppressAlreadyReadEvents marks events whose subject has already been read.
//
// The pending-event log and the read state live in different tables and nothing
// links them: UpdateLastRead (chat.go) bumps chat_sessions.last_read_at and
// MarkExecutionRead bumps task_executions.read_at, while pending_events rows are
// only removed when a DingTalk/Feishu push succeeds. So a reply the user read in
// the app stays in the log for its full TTL and the offline-recovery path
// notifies it again — the reported "已读消息仍弹出原生通知".
//
// The predicate is per SUBJECT, not per event: a session is "read" when
// last_read_at is set and no finalized reply landed after it — literally the
// badge's own definition. That means if a session was read and then received a
// NEW reply, the session counts as unread again and its older (already-read)
// events also notify. That is a deliberate limit: comparing each event's
// created_at against last_read_at would need the same same-second handling
// UpdateLastRead goes to great lengths over, and getting it wrong silences a
// real completion. Over-notifying one stale event is the benign direction, and
// it is also the pre-existing behavior — so this is not a regression.
//
// permission_pending is deliberately exempt: it is not a reply to read but an
// approval request still blocking the session, and it must keep notifying until
// the request is actually answered. (The event carries no resolved-marker, so
// there is nothing to compare against; the 7-day TTL is the backstop.)
//
// Failures are non-fatal: on a query error the event keeps its default
// (notify) state. Wrongly notifying is the pre-existing behavior and is
// recoverable; wrongly silencing would lose a real completion.
func suppressAlreadyReadEvents(events []PendingEvent) {
	if len(events) == 0 || dbRead == nil {
		return
	}

	subjects, sessionIDs, executionIDs := collectReadGateSubjects(events)
	readSessions := lookupReadSessions(sessionIDs)
	readExecutions := lookupReadExecutions(executionIDs)

	// Apply. The two families use different read models and must not be mixed:
	// a task run's session being read says nothing about whether that run's
	// result was seen (the task badge is per-execution and would still be lit).
	for i := range events {
		s := subjects[i]
		if s.byExecution {
			if readExecutions[s.executionID] {
				events[i].SuppressNotification = true
			}
			continue
		}
		if s.sessionID != "" && readSessions[s.sessionID] {
			events[i].SuppressNotification = true
		}
	}
}

// readGateSubject identifies the read-state subject of a pending event.
//
// byExecution selects WHICH read state decides suppression, and the choice is
// not interchangeable. Task runs are tracked per-execution
// (task_executions.read_at) while sessions are tracked per-session
// (chat_sessions.last_read_at). A task_update carries BOTH ids (emitTaskEvent
// sets SessionID alongside ExecutionID), so checking the session first would
// suppress an unread task result whenever its run's session happened to be
// read — the task badge would still be lit while the notification never fired.
type readGateSubject struct {
	sessionID   string
	executionID string
	byExecution bool
}

// collectReadGateSubjects resolves each event's read-state subject and returns
// the per-event subjects alongside the distinct ids to look up — so the read
// state costs one query per table rather than one per event. Events that are
// not gated (other event types, permission_pending, unparseable payloads) get a
// zero subject and are skipped by the caller.
func collectReadGateSubjects(events []PendingEvent) (subjects []readGateSubject, sessionIDs, executionIDs []string) {
	subjects = make([]readGateSubject, len(events))
	sessionIDs = make([]string, 0, len(events))
	executionIDs = make([]string, 0, len(events))
	seenSession := make(map[string]bool)
	seenExecution := make(map[string]bool)

	for i := range events {
		ev := &events[i]
		if !isReadGatedEvent(ev.EventType) {
			continue
		}
		sid, execID, status := pendingEventSubject(*ev)
		if status == ws.StatusPermissionPending {
			continue
		}

		if ev.EventType == eventTypeTaskUpdate {
			// The execution is the authority. An empty execution id (the early
			// failure emitted before the execution row exists, scheduler.go
			// emitTaskEvent with "") has no read state at all, so it keeps its
			// zero subject and notifies — the safe direction.
			if execID == "" {
				continue
			}
			subjects[i] = readGateSubject{executionID: execID, byExecution: true}
			if !seenExecution[execID] {
				seenExecution[execID] = true
				executionIDs = append(executionIDs, execID)
			}
			continue
		}

		subjects[i] = readGateSubject{sessionID: sid}
		if sid != "" && !seenSession[sid] {
			seenSession[sid] = true
			sessionIDs = append(sessionIDs, sid)
		}
	}
	return subjects, sessionIDs, executionIDs
}

// isReadGatedEvent reports whether an event type participates in the read gate.
// Only the two families whose subject has a read state are gated; chat_stream
// user_message (the other notifiable type) has no read model and is never
// suppressed.
func isReadGatedEvent(eventType string) bool {
	return eventType == eventTypeSessionUpdate || eventType == eventTypeTaskUpdate
}

// pendingEventSubject extracts the session id, execution id and status from a
// stored event payload. The payload is a full ws.ServerMessage; parsing is
// tolerant because older rows may predate any given field.
func pendingEventSubject(e PendingEvent) (sessionID, executionID, status string) {
	var msg ws.ServerMessage
	if err := json.Unmarshal([]byte(e.Payload), &msg); err != nil {
		return "", "", ""
	}
	d, ok := msg.Data.(map[string]any)
	if !ok {
		return "", "", ""
	}
	sessionID, _ = d["session_id"].(string)
	if raw, present := d["execution_id"]; present {
		executionID = anyToString(raw)
	}
	status, _ = d["status"].(string)
	return sessionID, executionID, status
}

// anyToString renders a JSON-decoded scalar as a string. execution_id is sent as
// a string by current code, but a numeric value would decode to float64 and must
// still match the execution's (TEXT) id in the lookup.
func anyToString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	default:
		return ""
	}
}

// lookupReadSessions returns the set of session ids that have been read.
//
// "Read" reuses the exact comparison the unread badge uses
// (unreadCountSubquery in chat.go): a session is read when last_read_at is set
// and no finalized assistant message landed after it. Keeping the two in sync
// matters — if this gate disagreed with the badge, a session could show as read
// while still notifying, or vice versa.
func lookupReadSessions(sessionIDs []string) map[string]bool {
	read := make(map[string]bool, len(sessionIDs))
	if len(sessionIDs) == 0 || dbRead == nil {
		return read
	}
	args := make([]any, 0, len(sessionIDs))
	for _, id := range sessionIDs {
		args = append(args, id)
	}
	rows, err := dbRead.Query(
		`SELECT s.id FROM chat_sessions s
		 WHERE s.id IN (`+sqlPlaceholders(len(sessionIDs))+`)
		   AND s.last_read_at IS NOT NULL
		   AND NOT EXISTS (
		     SELECT 1 FROM chat_history h
		     WHERE h.session_id = s.id
		       AND h.project_path = s.project_path
		       AND h.role = 'assistant'
		       AND h.streaming = 0
		       AND COALESCE(h.completed_at, h.created_at) > s.last_read_at
		   )`,
		args...,
	)
	if err != nil {
		slog.Warn("pending_events: read-state lookup failed", "error", err)
		return read
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			// Partial set — fail open (fewer suppressed), but log it so a
			// schema/type mismatch is not silently invisible.
			slog.Warn("pending_events: read-state row scan failed", "error", err)
			return read
		}
		read[id] = true
	}
	if err := rows.Err(); err != nil {
		slog.Warn("pending_events: read-state lookup iteration failed", "error", err)
	}
	return read
}

// lookupReadExecutions returns the set of task execution ids that have been read.
func lookupReadExecutions(executionIDs []string) map[string]bool {
	read := make(map[string]bool, len(executionIDs))
	if len(executionIDs) == 0 || dbRead == nil {
		return read
	}
	args := make([]any, 0, len(executionIDs))
	for _, id := range executionIDs {
		args = append(args, id)
	}
	rows, err := dbRead.Query(
		`SELECT id FROM task_executions
		 WHERE id IN (`+sqlPlaceholders(len(executionIDs))+`)
		   AND read_at IS NOT NULL`,
		args...,
	)
	if err != nil {
		slog.Warn("pending_events: execution read-state lookup failed", "error", err)
		return read
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			slog.Warn("pending_events: execution read-state row scan failed", "error", err)
			return read
		}
		read[id] = true
	}
	if err := rows.Err(); err != nil {
		slog.Warn("pending_events: execution read-state lookup iteration failed", "error", err)
	}
	return read
}

// sqlPlaceholders builds "?,?,?" for an IN clause of n bind parameters.
func sqlPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// CleanupPendingEvents removes expired events and caps total rows.
func CleanupPendingEvents() {
	if db == nil {
		return
	}
	result, err := WriteExec(`DELETE FROM pending_events WHERE expires_at < datetime('now')`)
	if err != nil {
		slog.Warn("pending_events: cleanup failed", "error", err)
	} else if n, _ := result.RowsAffected(); n > 0 {
		slog.Debug("pending_events: cleaned up expired", "count", n)
	}
	// Cap total rows
	capResult, capErr := WriteExec(
		`DELETE FROM pending_events WHERE id NOT IN (
			SELECT id FROM pending_events ORDER BY created_at DESC LIMIT ?
		)`,
		pendingEventMaxRows,
	)
	if capErr != nil {
		slog.Warn("pending_events: row cap failed", "error", capErr)
	} else if n, _ := capResult.RowsAffected(); n > 0 {
		slog.Warn("pending_events: evicted rows to cap", "count", n, "max", pendingEventMaxRows)
	}
}

// StoreNotifiableEvent persists a notifiable WS event so a client that missed
// it live can recover it on reconnect.
//
// Storage is conditional on the event not having reached every interested
// client. The old gate asked "is any client disconnected?", which is the wrong
// question: a browser can be connected yet hold no subscription for the
// session the event belongs to (WS reconnect replaced the connection and the
// re-subscribe had not landed). Such an event is dropped live — and if storage
// is skipped too, it is lost for good: not delivered, not replayable, so the
// client shows the assistant reply with no question bubble above it until a
// full history reload.
//
// So the gate is now per-session: store unless every connected client is
// actually subscribed to this event's session.
func StoreNotifiableEvent(msg ws.ServerMessage) {
	if !IsNotifiableEvent(msg.Event, msg.Data) {
		return
	}
	// Conditional storage: skip only when this event's session is being watched
	// by every connected client (nobody could have missed it).
	if sessionID := notifiableSessionID(msg); sessionID != "" {
		if mgr := ws.GetManager(); mgr != nil && mgr.AllConnectedClientsSubscribe(sessionID) {
			return
		}
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		slog.Warn("pending_events: marshal failed", "error", err)
		return
	}
	// Determine status for expires_at calculation
	status := ""
	switch d := msg.Data.(type) {
	case *ws.SessionUpdateData:
		status = d.Status
	case *ws.TaskUpdateData:
		status = d.Status
	case map[string]any:
		if s, ok := d["status"].(string); ok {
			status = s
		}
	}
	expiresAt := pendingEventExpiresAt(msg.Event, status)
	if err := StorePendingEvent(msg.ID, msg.Event, string(payload), expiresAt); err != nil {
		slog.Warn("pending_events: store failed", "error", err)
	}
}

// notifiableSessionID extracts the session an event belongs to, or "" when the
// event is not session-scoped (e.g. a task update) — those keep the previous
// unconditional-storage behavior.
func notifiableSessionID(msg ws.ServerMessage) string {
	switch d := msg.Data.(type) {
	case ws.ChatStreamData:
		return d.SessionID
	case *ws.ChatStreamData:
		return d.SessionID
	case *ws.SessionUpdateData:
		return d.SessionID
	case map[string]any:
		if s, ok := d["session_id"].(string); ok {
			return s
		}
	}
	return ""
}
