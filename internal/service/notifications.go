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
//
// subject_id is the ONE identity column for every kind: a session id, a task
// id, a live_notes share token, a forge item key, or a mail message id (as
// decimal text). The per-kind columns this replaced (session_id / task_id /
// forge_item_key / meeting_token / mail_message_id) are migrated into it and
// dropped, so a new kind needs no schema change. See notification_subjects.go.
const NotificationsDDL = `
CREATE TABLE IF NOT EXISTS notifications (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	event_id       TEXT NOT NULL UNIQUE,
	kind           TEXT NOT NULL,
	event_type     TEXT NOT NULL,
	title          TEXT NOT NULL,
	body           TEXT NOT NULL DEFAULT '',
	project_id     INTEGER NOT NULL DEFAULT 0,
	subject_id     TEXT NOT NULL DEFAULT '',
	execution_id   TEXT NOT NULL DEFAULT '',
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
	// NotificationKindUsage is a usage-meter threshold crossing. Unlike the
	// other three it has no subject row to navigate to: clicking it opens the
	// meter itself (wide screen) or the settings page (narrow screen).
	NotificationKindUsage = "usage"
	// NotificationKindMeeting is one thing that happened in a running meeting:
	// a remote-control request, a viewer joining, a member starting to share a
	// screen, or the post-meeting minutes finishing. It has no session: the
	// meeting is identified by its share token (live_notes.token), which is
	// what the click opens.
	NotificationKindMeeting = "meeting"
	// NotificationKindMail is newly arrived inbox mail. Its subject is the
	// cached mail_messages row, so the click opens that message in the mail
	// panel. Unlike forge/meeting it carries no project: a mailbox belongs to
	// the person, not to a repository.
	NotificationKindMail = "mail"
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
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	EventType string `json:"eventType"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	ProjectID int64  `json:"projectId"`
	// SubjectID is the identity of what this row points at, interpreted by
	// Kind: a session id, a task id, a live_notes share token, a forge item
	// key, or a mail message id (decimal). Empty for kinds with no local
	// subject (usage). The registry knows which table/key it lives in.
	SubjectID   string `json:"subjectId,omitempty"`
	ExecutionID string `json:"executionId,omitempty"`
	AgentID     string `json:"agentId,omitempty"`
	ProjectPath string `json:"projectPath"`
	CreatedAt   string `json:"createdAt"`
	// SourceMissing is set by the API layer when the subject this notification
	// points at no longer exists. Forge items are never marked (they live
	// upstream, with no local delete semantics).
	SourceMissing bool `json:"sourceMissing,omitempty"`
}

// NotificationRecord is a row to append to the log.
type NotificationRecord struct {
	EventID     string
	Kind        string
	EventType   string
	Title       string
	Body        string
	ProjectID   int64
	SubjectID   string
	ExecutionID string
	AgentID     string
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
	// A kind nobody registered is either a typo or a writer that skipped the
	// registry entry. Store it anyway (the log must not lose a fact), but say
	// so where the mistake is made: the client can only render such a row as
	// "other", so without this warning the defect would surface as a vague UI
	// oddity instead of a log line naming the kind.
	if _, known := notificationSubjectFor(rec.Kind); !known {
		slog.Warn("notifications: unregistered kind; client will show it as 'other'",
			slog.String("kind", rec.Kind), slog.String("event_id", rec.EventID))
	}
	_, err := store.WriteExec(
		`INSERT OR IGNORE INTO notifications
			(event_id, kind, event_type, title, body, project_id, subject_id, execution_id, agent_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.EventID, rec.Kind, rec.EventType, rec.Title, rec.Body,
		rec.ProjectID, rec.SubjectID, rec.ExecutionID, rec.AgentID,
		notificationTimestamp(time.Now()),
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
			COALESCE(p.path, ''), n.subject_id, n.execution_id, n.agent_id, n.created_at
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
			&n.ProjectPath, &n.SubjectID, &n.ExecutionID, &n.AgentID, &n.CreatedAt,
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

	// A group MEMBER row is an internal connection binding (no title, hidden
	// from the session list). Its ACP connection is keyed by the member id, so
	// the permission_pending callback in cmd/server/main.go reaches this with a
	// MEMBER id — and clicking such a row in the log opened an empty standalone
	// "Claude" chat instead of the group. Log the group timeline, which is what
	// the user actually sees and can navigate to.
	sessionID = ResolveGroupTimelineID(sessionID)

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
		SubjectID: sessionID,
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
		SubjectID:   taskID,
		ExecutionID: executionID,
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
		EventID:   eventID,
		Kind:      NotificationKindForge,
		EventType: eventType,
		Title:     title,
		Body:      body,
		ProjectID: projectID,
		SubjectID: itemKey,
	})
}

// RecordUsageNotification logs a usage-meter threshold crossing.
//
// Title and body arrive pre-rendered (see renderUsageMeterMessage) because they
// carry numbers, which no client-side enum can produce. projectID stays 0: the
// meter aggregates ALL projects, so attributing the row to one of them would
// make the notification center's project filter lie.
func RecordUsageNotification(eventID, stage, title, body string) {
	if eventID == "" || title == "" {
		return
	}
	if !store.DBReady() {
		return
	}
	StoreNotification(NotificationRecord{
		EventID:   eventID,
		Kind:      NotificationKindUsage,
		EventType: stage,
		Title:     title,
		Body:      body,
	})
}
