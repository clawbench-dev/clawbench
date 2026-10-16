package service

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

// setupNotificationsDB creates an in-memory DB with the notifications table
// plus the two source tables the existence probe reads (chat_sessions,
// scheduled_tasks), matching the columns the probe touches.
func setupNotificationsDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS projects (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			path TEXT NOT NULL,
			UNIQUE(path)
		);
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			archived INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS scheduled_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL DEFAULT ''
		);
	` + NotificationsDDL)
	require.NoError(t, err)
	return db, func() { db.Close() }
}

// notificationsTestDB installs the in-memory DB as the package handle and seeds
// the project registry the list query joins against.
func notificationsTestDB(t *testing.T) func() {
	t.Helper()
	db, teardown := setupNotificationsDB(t)
	cleanup := store.SetDBForTest(db, db)
	store.SeedTestProjectsForTest(t)
	return func() {
		cleanup()
		teardown()
	}
}

func TestNotificationsTableCreated(t *testing.T) {
	db, teardown := setupNotificationsDB(t)
	defer teardown()

	for _, name := range []string{"notifications"} {
		var n int
		require.NoError(t, db.QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", name,
		).Scan(&n))
		assert.Equal(t, 1, n, "table %s should exist", name)
	}
	for _, idx := range []string{"idx_notifications_event", "idx_notifications_created", "idx_notifications_filter"} {
		var n int
		require.NoError(t, db.QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?", idx,
		).Scan(&n))
		assert.Equal(t, 1, n, "index %s should exist", idx)
	}

	// The read/unread column must NOT exist: notifications are an append-only
	// log with no read state. This is the core design decision, so pin it.
	var hasReadAt int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('notifications') WHERE name IN ('read_at','read')",
	).Scan(&hasReadAt))
	assert.Equal(t, 0, hasReadAt, "notifications must have no read-state column")
}

func TestStoreNotification_AppendsAndDedupes(t *testing.T) {
	cleanup := notificationsTestDB(t)
	defer cleanup()

	StoreNotification(NotificationRecord{
		EventID: "evt-1", Kind: NotificationKindSession, EventType: "completed",
		Title: "Session A", Body: "done", SessionID: "s1", AgentID: "claude",
	})
	StoreNotification(NotificationRecord{
		EventID: "evt-2", Kind: NotificationKindTask, EventType: "failed",
		Title: "Task B", TaskID: "7",
	})

	// Same event id again — must be ignored, not duplicated.
	StoreNotification(NotificationRecord{
		EventID: "evt-1", Kind: NotificationKindSession, EventType: "completed",
		Title: "Session A (retry)", Body: "done", SessionID: "s1",
	})

	rows, err := ListNotifications(NotificationFilter{})
	require.NoError(t, err)
	require.Len(t, rows, 2, "duplicate event_id must not create a second row")
}

func TestStoreNotification_RefusesMissingIdentity(t *testing.T) {
	cleanup := notificationsTestDB(t)
	defer cleanup()

	// No event_id, no kind: unusable rows are refused, not stored.
	StoreNotification(NotificationRecord{Kind: NotificationKindSession, Title: "no id"})
	StoreNotification(NotificationRecord{EventID: "e", Title: "no kind"})

	rows, err := ListNotifications(NotificationFilter{})
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestStoreNotification_RecordsRegardlessOfPushGates(t *testing.T) {
	// The writer must NOT consult push_mode or WS subscriptions. Both gates are
	// correct for a push and wrong for a log; this test pins the writer itself,
	// independent of the emit sites (which the write-path tests cover).
	cleanup := notificationsTestDB(t)
	defer cleanup()

	StoreNotification(NotificationRecord{
		EventID: "evt-log", Kind: NotificationKindForge, EventType: "merged",
		Title: "repo · PR #1",
	})
	rows, err := ListNotifications(NotificationFilter{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, NotificationKindForge, rows[0].Kind)
}

func TestListNotifications_Filters(t *testing.T) {
	cleanup := notificationsTestDB(t)
	defer cleanup()

	p1 := store.ProjectIDForTest(t, "/proj1")
	p2 := store.ProjectIDForTest(t, "/proj2")
	old := notificationTimestamp(time.Now().AddDate(0, 0, -10))

	insert := func(eventID, kind string, projectID int64, createdAt string) {
		_, err := store.WriteExec(
			`INSERT INTO notifications (event_id, kind, event_type, title, body, project_id, created_at)
			 VALUES (?, ?, 'x', 't', '', ?, ?)`,
			eventID, kind, projectID, createdAt,
		)
		require.NoError(t, err)
	}
	now := notificationTimestamp(time.Now())
	insert("a", NotificationKindSession, p1, now)
	insert("b", NotificationKindTask, p1, now)
	insert("c", NotificationKindForge, p2, now)
	insert("d", NotificationKindSession, p1, old)

	t.Run("by project", func(t *testing.T) {
		rows, err := ListNotifications(NotificationFilter{ProjectID: p1})
		require.NoError(t, err)
		assert.Len(t, rows, 3)
	})
	t.Run("by kind", func(t *testing.T) {
		rows, err := ListNotifications(NotificationFilter{Kind: NotificationKindSession})
		require.NoError(t, err)
		assert.Len(t, rows, 2)
	})
	t.Run("by project and kind", func(t *testing.T) {
		rows, err := ListNotifications(NotificationFilter{ProjectID: p1, Kind: NotificationKindTask})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, "b", eventIDOf(t, rows[0].ID))
	})
	t.Run("by since excludes old", func(t *testing.T) {
		since := notificationTimestamp(time.Now().AddDate(0, 0, -1))
		rows, err := ListNotifications(NotificationFilter{Since: since})
		require.NoError(t, err)
		assert.Len(t, rows, 3, "the 10-day-old row must be excluded")
	})
	t.Run("count matches filter", func(t *testing.T) {
		n, err := CountNotifications(NotificationFilter{ProjectID: p1})
		require.NoError(t, err)
		assert.Equal(t, 3, n)
	})
}

func eventIDOf(t *testing.T, id int64) string {
	t.Helper()
	var eventID string
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT event_id FROM notifications WHERE id = ?", id,
	).Scan(&eventID))
	return eventID
}

func TestListNotifications_KeysetPagination(t *testing.T) {
	cleanup := notificationsTestDB(t)
	defer cleanup()

	// Five rows, ALL in the same millisecond — the case that a second-precision
	// timestamp cannot page through stably.
	ts := notificationTimestamp(time.Now())
	for i := range 5 {
		_, err := store.WriteExec(
			`INSERT INTO notifications (event_id, kind, event_type, title, created_at)
			 VALUES (?, 'session', 'x', ?, ?)`,
			fmt.Sprintf("e%d", i), fmt.Sprintf("t%d", i), ts,
		)
		require.NoError(t, err)
	}

	seen := map[string]bool{}
	var cursorAt string
	var cursorID int64
	for range 5 {
		rows, err := ListNotifications(NotificationFilter{
			Limit: 2, BeforeCreatedAt: cursorAt, BeforeID: cursorID,
		})
		require.NoError(t, err)
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			eventID := eventIDOf(t, r.ID)
			require.False(t, seen[eventID], "row %s returned twice", eventID)
			seen[eventID] = true
		}
		last := rows[len(rows)-1]
		cursorAt, cursorID = last.CreatedAt, last.ID
	}
	assert.Len(t, seen, 5, "paging must return every row exactly once")
}

func TestCleanupNotifications_TTLAndRowCap(t *testing.T) {
	cleanup := notificationsTestDB(t)
	defer cleanup()

	// One row well past the TTL.
	_, err := store.WriteExec(
		`INSERT INTO notifications (event_id, kind, event_type, title, created_at)
		 VALUES ('old', 'session', 'x', 't', ?)`,
		notificationTimestamp(time.Now().AddDate(0, 0, -(notificationRetentionDays+5))),
	)
	require.NoError(t, err)
	// One fresh row.
	_, err = store.WriteExec(
		`INSERT INTO notifications (event_id, kind, event_type, title, created_at)
		 VALUES ('fresh', 'session', 'x', 't', ?)`,
		notificationTimestamp(time.Now()),
	)
	require.NoError(t, err)

	CleanupNotifications()

	rows, err := ListNotifications(NotificationFilter{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "fresh", eventIDOf(t, rows[0].ID), "expired row must be dropped")
}

func TestCleanupNotifications_RowCap(t *testing.T) {
	cleanup := notificationsTestDB(t)
	defer cleanup()

	// Insert more rows than the cap allows, all fresh, with increasing ids.
	base := time.Now().Add(-time.Hour)
	for i := range notificationMaxRows + 10 {
		_, err := store.WriteExec(
			`INSERT INTO notifications (event_id, kind, event_type, title, created_at)
			 VALUES (?, 'session', 'x', 't', ?)`,
			fmt.Sprintf("e%d", i),
			notificationTimestamp(base.Add(time.Duration(i)*time.Millisecond)),
		)
		require.NoError(t, err)
	}

	CleanupNotifications()

	n, err := CountNotifications(NotificationFilter{})
	require.NoError(t, err)
	assert.Equal(t, notificationMaxRows, n, "row cap must trim to the limit")

	// The newest rows survive, the oldest are evicted.
	var newestEvent string
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT event_id FROM notifications ORDER BY created_at DESC, id DESC LIMIT 1",
	).Scan(&newestEvent))
	assert.Equal(t, fmt.Sprintf("e%d", notificationMaxRows+9), newestEvent)
}

func TestClearNotifications(t *testing.T) {
	cleanup := notificationsTestDB(t)
	defer cleanup()

	StoreNotification(NotificationRecord{EventID: "x", Kind: NotificationKindSession, Title: "t"})
	StoreNotification(NotificationRecord{EventID: "y", Kind: NotificationKindSession, Title: "t"})

	n, err := ClearNotifications()
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)

	rows, err := ListNotifications(NotificationFilter{})
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestMarkMissingSources(t *testing.T) {
	cleanup := notificationsTestDB(t)
	defer cleanup()

	// One session exists, one was hard-deleted.
	_, err := store.WriteExec("INSERT INTO chat_sessions (id, archived) VALUES ('s-live', 0)")
	require.NoError(t, err)
	// An archived session still exists — it must NOT be reported missing.
	_, err = store.WriteExec("INSERT INTO chat_sessions (id, archived) VALUES ('s-archived', 1)")
	require.NoError(t, err)
	// A task exists; another does not.
	_, err = store.WriteExec("INSERT INTO scheduled_tasks (id, name) VALUES (3, 'live')")
	require.NoError(t, err)

	rows := []Notification{
		{ID: 1, Kind: NotificationKindSession, SessionID: "s-live"},
		{ID: 2, Kind: NotificationKindSession, SessionID: "s-archived"},
		{ID: 3, Kind: NotificationKindSession, SessionID: "s-gone"},
		{ID: 4, Kind: NotificationKindTask, TaskID: "3"},
		{ID: 5, Kind: NotificationKindTask, TaskID: "99"},
		{ID: 6, Kind: NotificationKindForge, ForgeItemKey: "issue/1"},
	}
	MarkMissingSources(rows)

	assert.False(t, rows[0].SourceMissing, "existing session")
	assert.False(t, rows[1].SourceMissing, "archived session still exists")
	assert.True(t, rows[2].SourceMissing, "hard-deleted session")
	assert.False(t, rows[3].SourceMissing, "existing task")
	assert.True(t, rows[4].SourceMissing, "deleted task")
	assert.False(t, rows[5].SourceMissing, "forge items are never probed")
}

// TestMarkMissingSources_TaskRowSurvivesPurgedSession is the regression guard
// for a task notification whose scheduled session was cleaned up.
//
// A task notification stores BOTH task_id and the scheduled session_id it ran
// in. That session is an ordinary `session_type='scheduled'` row which
// SessionCleanupWorker archives and then hard-deletes, while the task lives on.
// If the probe consulted the session id first, a perfectly healthy task would be
// flagged "source deleted" and lose its navigation.
func TestMarkMissingSources_TaskRowSurvivesPurgedSession(t *testing.T) {
	cleanup := notificationsTestDB(t)
	defer cleanup()

	// The task exists; its session does NOT (purged).
	_, err := store.WriteExec("INSERT INTO scheduled_tasks (id, name) VALUES (7, 'live task')")
	require.NoError(t, err)

	rows := []Notification{
		{ID: 1, Kind: NotificationKindTask, TaskID: "7", SessionID: "purged-session"},
	}
	MarkMissingSources(rows)

	assert.False(t, rows[0].SourceMissing,
		"a live task must not be flagged missing because its purged session is gone")
}

// TestMarkMissingSources_SessionRowIgnoresStrayTaskID is the mirror guard: a
// session row is judged by its session, never by a stray task id.
func TestMarkMissingSources_SessionRowIgnoresStrayTaskID(t *testing.T) {
	cleanup := notificationsTestDB(t)
	defer cleanup()

	_, err := store.WriteExec("INSERT INTO chat_sessions (id, archived) VALUES ('s-live', 0)")
	require.NoError(t, err)

	rows := []Notification{
		{ID: 1, Kind: NotificationKindSession, SessionID: "s-live", TaskID: "does-not-exist"},
	}
	MarkMissingSources(rows)

	assert.False(t, rows[0].SourceMissing)
}

func TestMarkMissingSources_ProbeFailureFailsOpen(t *testing.T) {
	// With no read DB installed, the probe cannot run. It must treat ids as
	// present so a transient DB problem does not mark the entire list deleted.
	db, teardown := setupNotificationsDB(t)
	defer teardown()
	// Deliberately do NOT install the handle: ReadDBReady() is false.
	_ = db

	rows := []Notification{{ID: 1, Kind: NotificationKindSession, SessionID: "s1"}}
	MarkMissingSources(rows)
	assert.False(t, rows[0].SourceMissing)
}

func TestNotificationTimeRangeSince(t *testing.T) {
	now := time.Date(2026, 10, 15, 14, 30, 0, 0, time.UTC)

	today := NotificationRangeSince("today", now)
	assert.Equal(t, "2026-10-15T00:00:00.000Z", today)

	week := NotificationRangeSince("7d", now)
	assert.Equal(t, "2026-10-08T14:30:00.000Z", week)

	month := NotificationRangeSince("30d", now)
	assert.Equal(t, "2026-09-15T14:30:00.000Z", month)

	// Unknown / empty tokens mean "no lower bound" (show more, never hide).
	assert.Equal(t, "", NotificationRangeSince("", now))
	assert.Equal(t, "", NotificationRangeSince("bogus", now))
}

func TestNotificationTimestamp_FixedWidthSubSecond(t *testing.T) {
	// Two times 1ms apart must format to lexicographically ordered strings,
	// which is what makes keyset pagination stable within a second.
	a := notificationTimestamp(time.Date(2026, 10, 15, 1, 2, 3, 100*int(time.Millisecond), time.UTC))
	b := notificationTimestamp(time.Date(2026, 10, 15, 1, 2, 3, 200*int(time.Millisecond), time.UTC))
	assert.Less(t, a, b)
	assert.Len(t, a, len("2006-01-02T15:04:05.000Z"))
}
