package service

import (
	"log/slog"
	"strings"
	"time"

	"clawbench/internal/store"

	"clawbench/internal/ws"
)

// NotificationsDDL creates the notification-center log.
//
// This table is the notification center's ONLY source of truth: the list, the
// filters and the pagination all read from it and nothing else. It is an
// append-only log of "what happened" — deliberately WITHOUT a read/unread
// column. A notification is not an inbox item: it has either already been
// surfaced (the in-app completion card, a native/IM push) or the user is
// reading the log right now, so a second "read" flag would only create a second
// state to keep in sync with the three subject models (chat_sessions.last_read_at,
// task_executions.read_at, forge_events.read_at). See
// docs/plans/2026-10-15-notification-center-design.md.
//
// created_at is TEXT at sub-second precision, not the DATETIME the other tables
// use. A single forge poll drains a whole batch of events, so several rows can
// land in the same second; a second-precision timestamp cannot order them
// stably and keyset pagination would then repeat or skip rows. The value is
// written by the Go layer (notificationTimestamp) so the format is fixed-width
// and lexicographic ordering equals chronological ordering.
//
// No foreign keys to chat_sessions / scheduled_tasks: a notification
// legitimately outlives its source (that is the whole point of keeping it), and
// the list probes for the source's existence at render time instead.
const NotificationsDDL = `
CREATE TABLE IF NOT EXISTS notifications (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	event_id       TEXT NOT NULL UNIQUE,
	kind           TEXT NOT NULL,
	event_type     TEXT NOT NULL,
	title          TEXT NOT NULL,
	body           TEXT NOT NULL DEFAULT '',
	project_id     INTEGER NOT NULL DEFAULT 0,
	session_id     TEXT NOT NULL DEFAULT '',
	task_id        TEXT NOT NULL DEFAULT '',
	execution_id   TEXT NOT NULL DEFAULT '',
	forge_item_key TEXT NOT NULL DEFAULT '',
	agent_id       TEXT NOT NULL DEFAULT '',
	created_at     TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_notifications_event ON notifications(event_id);
CREATE INDEX IF NOT EXISTS idx_notifications_created ON notifications(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_filter ON notifications(project_id, kind, created_at);
`

// Notification kinds. Kept as constants so the writer and the API filter agree.
const (
	NotificationKindSession = "session"
	NotificationKindTask    = "task"
	NotificationKindForge   = "forge"
)

// notificationRetentionDays / notificationMaxRows are the two retention caps.
// Both apply: rows older than the window are dropped, and the total is capped
// at the row limit (oldest first). The cap is a backstop against a high-frequency
// forge feed filling the disk between TTL sweeps.
const (
	notificationRetentionDays = 30
	notificationMaxRows       = 500
)

// notificationStatusRunning is the in-progress status string. Declared here
// because only this file's recorders consult it (they skip "running" for
// sessions but record it for tasks).
const notificationStatusRunning = "running"

// notificationTimestamp renders the fixed-width, sub-second UTC timestamp used
// by notifications.created_at. Always exactly 3 fractional digits so string
// comparison is chronological and keyset pagination is stable.
func notificationTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// Notification is one row of the notification log, shaped for the API.
type Notification struct {
	ID           int64  `json:"id"`
	Kind         string `json:"kind"`
	EventType    string `json:"eventType"`
	Title        string `json:"title"`
	Body         string `json:"body"`
	ProjectID    int64  `json:"projectId"`
	ProjectPath  string `json:"projectPath"`
	SessionID    string `json:"sessionId,omitempty"`
	TaskID       string `json:"taskId,omitempty"`
	ExecutionID  string `json:"executionId,omitempty"`
	ForgeItemKey string `json:"forgeItemKey,omitempty"`
	AgentID      string `json:"agentId,omitempty"`
	CreatedAt    string `json:"createdAt"`
	// SourceMissing is set by the API layer when the session/task this
	// notification points at no longer exists. Forge items are never marked
	// (they live upstream, with no local delete semantics).
	SourceMissing bool `json:"sourceMissing,omitempty"`
}

// NotificationRecord is a row to append to the log.
type NotificationRecord struct {
	EventID      string
	Kind         string
	EventType    string
	Title        string
	Body         string
	ProjectID    int64
	SessionID    string
	TaskID       string
	ExecutionID  string
	ForgeItemKey string
	AgentID      string
}

// StoreNotification appends one notification to the log.
//
// INSERT OR IGNORE on the UNIQUE event_id makes this idempotent: the same
// underlying event re-emitted (retry, duplicate hook) cannot double-log.
//
// Deliberately NOT gated by anything. In particular it must not reuse
// pending_events' gates — StoreNotifiableEvent skips when push_mode is
// "disabled" and when every connected client already subscribes to the session.
// Both are correct for a PUSH (don't notify) and wrong for a LOG (still record):
// a user who disabled push still wants to browse what happened, and nobody
// watching a session does not mean the event should vanish from the history.
func StoreNotification(rec NotificationRecord) {
	if !store.DBReady() {
		return
	}
	if rec.EventID == "" || rec.Kind == "" {
		// Both are structural: event_id is the dedup identity and kind drives
		// every filter. A row without them is unusable, so refuse loudly rather
		// than log something the API can never show correctly.
		slog.Warn("notifications: refusing to store record without event_id/kind",
			slog.String("kind", rec.Kind), slog.String("event_id", rec.EventID))
		return
	}
	_, err := store.WriteExec(
		`INSERT OR IGNORE INTO notifications
			(event_id, kind, event_type, title, body, project_id, session_id, task_id, execution_id, forge_item_key, agent_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.EventID, rec.Kind, rec.EventType, rec.Title, rec.Body,
		rec.ProjectID, rec.SessionID, rec.TaskID, rec.ExecutionID,
		rec.ForgeItemKey, rec.AgentID, notificationTimestamp(time.Now()),
	)
	if err != nil {
		slog.Warn("notifications: store failed", "error", err, "kind", rec.Kind)
	}
}

// NotificationFilter narrows ListNotifications. Zero values mean "no filter".
type NotificationFilter struct {
	// ProjectID > 0 restricts to one project.
	ProjectID int64
	// Kind is one of the NotificationKind* constants, or "" for all.
	Kind string
	// Since is an inclusive lower bound on created_at (same format as stored),
	// or "" for no lower bound. The API derives it from the time-range filter.
	Since string
	// BeforeCreatedAt/BeforeID are the keyset cursor: return rows strictly older
	// than this (created_at, id) pair. Both empty means "from the newest".
	BeforeCreatedAt string
	BeforeID        int64
	// Limit caps the page. <= 0 falls back to a sane default.
	Limit int
}

const (
	defaultNotificationPageLimit = 50
	maxNotificationPageLimit     = 200
)

// ListNotifications returns a page of the log, newest first.
//
// Keyset pagination on (created_at DESC, id DESC) rather than OFFSET: rows are
// appended continuously, so an OFFSET page would shift under the reader and
// repeat or skip entries.
func ListNotifications(f NotificationFilter) ([]Notification, error) {
	if !store.DBReady() || !store.ReadDBReady() {
		return nil, nil
	}
	limit := f.Limit
	if limit <= 0 {
		limit = defaultNotificationPageLimit
	}
	if limit > maxNotificationPageLimit {
		limit = maxNotificationPageLimit
	}

	query := `SELECT n.id, n.kind, n.event_type, n.title, n.body, n.project_id,
			COALESCE(p.path, ''), n.session_id, n.task_id, n.execution_id,
			n.forge_item_key, n.agent_id, n.created_at
		FROM notifications n
		LEFT JOIN projects p ON p.id = n.project_id
		WHERE 1 = 1`
	args := []any{}
	if f.ProjectID > 0 {
		query += " AND n.project_id = ?"
		args = append(args, f.ProjectID)
	}
	if f.Kind != "" {
		query += " AND n.kind = ?"
		args = append(args, f.Kind)
	}
	if f.Since != "" {
		query += " AND n.created_at >= ?"
		args = append(args, f.Since)
	}
	if f.BeforeCreatedAt != "" {
		// Strictly older, with id as the tiebreaker inside the same timestamp.
		query += " AND (n.created_at < ? OR (n.created_at = ? AND n.id < ?))"
		args = append(args, f.BeforeCreatedAt, f.BeforeCreatedAt, f.BeforeID)
	}
	query += " ORDER BY n.created_at DESC, n.id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := store.ReadDB().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []Notification{}
	for rows.Next() {
		var n Notification
		if err := rows.Scan(
			&n.ID, &n.Kind, &n.EventType, &n.Title, &n.Body, &n.ProjectID,
			&n.ProjectPath, &n.SessionID, &n.TaskID, &n.ExecutionID,
			&n.ForgeItemKey, &n.AgentID, &n.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// CountNotifications returns how many rows match the filter, ignoring paging.
// Used by the panel's "N items" readout so it reflects the filter, not the page.
func CountNotifications(f NotificationFilter) (int, error) {
	if !store.DBReady() || !store.ReadDBReady() {
		return 0, nil
	}
	query := "SELECT COUNT(*) FROM notifications WHERE 1 = 1"
	args := []any{}
	if f.ProjectID > 0 {
		query += " AND project_id = ?"
		args = append(args, f.ProjectID)
	}
	if f.Kind != "" {
		query += " AND kind = ?"
		args = append(args, f.Kind)
	}
	if f.Since != "" {
		query += " AND created_at >= ?"
		args = append(args, f.Since)
	}
	var n int
	if err := store.ReadDB().QueryRow(query, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// ClearNotifications deletes the whole log and reports how many rows went.
func ClearNotifications() (int64, error) {
	if !store.DBReady() {
		return 0, nil
	}
	res, err := store.WriteExec("DELETE FROM notifications")
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CleanupNotifications enforces both retention caps: drop rows past the TTL,
// then trim the total to the row cap (oldest first).
//
// Called from the same periodic sweep as CleanupPendingEvents.
func CleanupNotifications() {
	if !store.DBReady() {
		return
	}
	cutoff := notificationTimestamp(time.Now().AddDate(0, 0, -notificationRetentionDays))
	if res, err := store.WriteExec(
		"DELETE FROM notifications WHERE created_at < ?", cutoff,
	); err != nil {
		slog.Warn("notifications: TTL cleanup failed", "error", err)
	} else if n, _ := res.RowsAffected(); n > 0 {
		slog.Debug("notifications: cleaned up expired", "count", n)
	}

	// Row cap. Order by (created_at, id) DESC so "keep the newest" is
	// unambiguous even within one timestamp.
	if res, err := store.WriteExec(
		`DELETE FROM notifications WHERE id NOT IN (
			SELECT id FROM notifications ORDER BY created_at DESC, id DESC LIMIT ?
		)`,
		notificationMaxRows,
	); err != nil {
		slog.Warn("notifications: row cap failed", "error", err)
	} else if n, _ := res.RowsAffected(); n > 0 {
		slog.Warn("notifications: evicted rows to cap", "count", n, "max", notificationMaxRows)
	}
}

// MarkMissingSources sets SourceMissing on rows whose session/task no longer
// exists. One query per id-space (session, task) for the whole page, so the
// cost is constant regardless of page size.
//
// The probe is keyed off Kind, NOT "whichever id is non-empty". A task
// notification stores BOTH task_id and the scheduled session_id it ran in, and
// that session is a normal `session_type='scheduled'` row that SessionCleanupWorker
// archives and then hard-deletes while the task itself lives on. Probing the
// session first would therefore flag a perfectly healthy task as "source
// deleted" and suppress its navigation. The kind says which id is the subject.
//
// "Archived" is NOT "deleted": an archived session row still exists (and can be
// resumed), so it is reported as present — only a genuinely removed row counts
// as missing. Forge items are skipped entirely: they live upstream and have no
// local delete semantics, so a forge notification is never stale.
func MarkMissingSources(rows []Notification) {
	if len(rows) == 0 || !store.ReadDBReady() {
		return
	}
	// Only collect the ids the probe will actually consult, so a task row's
	// session id is never even queried.
	sessionIDs := distinctNonEmpty(rows, func(n Notification) string {
		if n.Kind != NotificationKindSession {
			return ""
		}
		return n.SessionID
	})
	taskIDs := distinctNonEmpty(rows, func(n Notification) string {
		if n.Kind != NotificationKindTask {
			return ""
		}
		return n.TaskID
	})

	presentSessions := existingIDs("chat_sessions", sessionIDs)
	presentTasks := existingIDs("scheduled_tasks", taskIDs)

	for i := range rows {
		n := &rows[i]
		switch n.Kind {
		case NotificationKindSession:
			if n.SessionID != "" && !presentSessions[n.SessionID] {
				n.SourceMissing = true
			}
		case NotificationKindTask:
			if n.TaskID != "" && !presentTasks[n.TaskID] {
				n.SourceMissing = true
			}
		}
		// Forge (and any future kind): never probed.
	}
}

// distinctNonEmpty collects the unique non-empty values of one field.
func distinctNonEmpty(rows []Notification, get func(Notification) string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, n := range rows {
		v := get(n)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// existingIDs returns the subset of ids present in table (an allowlisted table
// name — never user input). An empty id list short-circuits without a query.
func existingIDs(table string, ids []string) map[string]bool {
	present := map[string]bool{}
	if len(ids) == 0 || !store.ReadDBReady() {
		return present
	}
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := store.ReadDB().Query(
		"SELECT id FROM "+table+" WHERE id IN ("+sqlPlaceholders(len(ids))+")",
		args...,
	)
	if err != nil {
		// Fail open: treat every id as present. A probe failure must not make
		// the whole list look deleted, which would disable every navigation.
		slog.Warn("notifications: source probe failed", "table", table, "error", err)
		for _, id := range ids {
			present[id] = true
		}
		return present
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			slog.Warn("notifications: source probe scan failed", "table", table, "error", err)
			continue
		}
		present[id] = true
	}
	if err := rows.Err(); err != nil {
		slog.Warn("notifications: source probe iteration failed", "table", table, "error", err)
	}
	return present
}

// NotificationRangeSince maps the API's time-range token to an inclusive
// created_at lower bound. Unknown or empty tokens mean "no lower bound", which
// is the safe direction (show more, never silently hide).
func NotificationRangeSince(token string, now time.Time) string {
	switch strings.ToLower(strings.TrimSpace(token)) {
	case "today":
		y, m, d := now.Date()
		return notificationTimestamp(time.Date(y, m, d, 0, 0, 0, 0, now.Location()))
	case "7d":
		return notificationTimestamp(now.AddDate(0, 0, -7))
	case "30d":
		return notificationTimestamp(now.AddDate(0, 0, -30))
	default:
		return ""
	}
}

// ── Emit-site adapters ──────────────────────────────────────────────────────
//
// These build a NotificationRecord from a ws event at the point it is emitted,
// so the three producers (session / task / forge) share one shaping rule. They
// are called next to StoreNotifiableEvent but are NOT gated like it.

// notificationEventBody picks the body for a session/task notification from
// the event's own preview fields, matching what the completion card shows.
func notificationEventBody(data *ws.SessionUpdateData) string {
	if data == nil {
		return ""
	}
	if data.ResponsePreviewPlain != "" {
		return data.ResponsePreviewPlain
	}
	return data.ResponsePreview
}

// RecordSessionNotification logs a session terminal event (completed /
// cancelled / permission_pending).
//
// eventID is the emit site's own WS event id (the same one stored in
// pending_events), NOT a freshly minted one: that is what makes the UNIQUE
// event_id constraint a real dedup key. Minting a new id here would make
// INSERT OR IGNORE inert and let a re-emitted terminal event log twice.
//
// Only the three notifiable statuses are recorded — "running"/"read"/"rewound"
// are state transitions, not notifications, matching IsNotifiableEvent and the
// system-notification set so the log and the popups cover the same events.
func RecordSessionNotification(eventID, sessionID, status string, data *ws.SessionUpdateData) {
	if sessionID == "" || eventID == "" || !isNotifiableSessionStatus(status) {
		return
	}
	// Guard before any DB access (GetSessionProjectPath / ProjectIDForPath touch
	// the handle). Recording is best-effort: no DB means no history, never a
	// panic on a path that used to be DB-free.
	if !store.DBReady() {
		return
	}
	projectID := int64(0)
	if path := GetSessionProjectPath(sessionID); path != "" {
		if id, err := store.ProjectIDForPath(path); err == nil {
			projectID = id
		}
	}
	title, err := GetSessionTitle(sessionID)
	if err != nil {
		title = ""
	}
	rec := NotificationRecord{
		EventID:   eventID,
		Kind:      NotificationKindSession,
		EventType: status,
		Title:     title,
		Body:      notificationEventBody(data),
		ProjectID: projectID,
		SessionID: sessionID,
		AgentID:   GetSessionAgentID(sessionID),
	}
	StoreNotification(rec)
}

// isNotifiableSessionStatus mirrors the set that raises a popup / system
// notification: completed, cancelled, permission_pending. Kept as a local
// predicate (rather than reusing IsNotifiableEvent, which also matches
// user_message chat-stream events and reads a different data shape) so the
// notification log's coverage is stated in one place.
func isNotifiableSessionStatus(status string) bool {
	switch status {
	case statusCompleted, statusCancelled, ws.StatusPermissionPending:
		return true
	default:
		return false
	}
}

// RecordTaskNotification logs a task terminal event. "running" is included on
// purpose: the completion card and system notification both cover it, so the
// log must too (the log mirrors the notification set exactly).
//
// eventID is the emit site's own WS event id, so a re-emitted task event is
// deduped by the UNIQUE event_id rather than logged twice.
func RecordTaskNotification(eventID, taskID, status, executionID, sessionID, projectPath, taskName string, data *ws.TaskUpdateData) {
	if taskID == "" || eventID == "" {
		return
	}
	if status != notificationStatusRunning && status != statusCompleted && status != statusFailed && status != statusCancelled {
		return
	}
	// See RecordSessionNotification: guard before any DB access.
	if !store.DBReady() {
		return
	}
	projectID := int64(0)
	if projectPath != "" {
		if id, err := store.ProjectIDForPath(projectPath); err == nil {
			projectID = id
		}
	}
	title := taskName
	if title == "" && data != nil {
		title = data.SessionTitle
	}
	if title == "" {
		title = sessionID
	}
	body := ""
	if data != nil {
		if data.ResponsePreviewPlain != "" {
			body = data.ResponsePreviewPlain
		} else {
			body = data.ResponsePreview
		}
	}
	StoreNotification(NotificationRecord{
		EventID:     eventID,
		Kind:        NotificationKindTask,
		EventType:   status,
		Title:       title,
		Body:        body,
		ProjectID:   projectID,
		TaskID:      taskID,
		ExecutionID: executionID,
		SessionID:   sessionID,
	})
}

// RecordForgeNotification logs one derived forge change.
//
// eventID is the change's stable dedup identity (forge.DedupeKey), so a change
// re-dispatched is deduped rather than logged twice.
//
// projectID comes from the binding that owns the repository (repo.ProjectPath),
// resolved at write time so the list can filter by project without re-deriving
// the binding later. itemKey is the opaque read key (e.g. "issue/12",
// "pipeline/run:99"); it is stored for deep-linking, not for read state (the log
// has none).
func RecordForgeNotification(eventID string, repo ForgeRepoRef, eventType, title, body, itemKey string) {
	// See RecordSessionNotification: guard before any DB access.
	if eventID == "" || !store.DBReady() {
		return
	}
	projectID := int64(0)
	if repo.ProjectPath != "" {
		if id, err := store.ProjectIDForPath(repo.ProjectPath); err == nil {
			projectID = id
		}
	}
	StoreNotification(NotificationRecord{
		EventID:      eventID,
		Kind:         NotificationKindForge,
		EventType:    eventType,
		Title:        title,
		Body:         body,
		ProjectID:    projectID,
		ForgeItemKey: itemKey,
	})
}
