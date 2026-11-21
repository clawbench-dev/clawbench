package service

import (
	"database/sql"
	"testing"

	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNotificationSubjects_EveryKindRegisteredOnce is the guard that makes the
// registry the single per-kind extension point.
//
// Adding a NotificationKind* constant without an entry here would leave the
// kind unprobed (its rows would never be marked stale) and would make the
// writer log an "unregistered kind" warning. The test's literal list is the
// only way to enumerate constants in Go, so adding a kind intentionally fails
// here until it is declared — that is the point, not a nuisance.
func TestNotificationSubjects_EveryKindRegisteredOnce(t *testing.T) {
	kinds := []string{
		NotificationKindSession,
		NotificationKindTask,
		NotificationKindForge,
		NotificationKindUsage,
		NotificationKindMeeting,
		NotificationKindMail,
	}

	seen := map[string]bool{}
	for _, spec := range notificationSubjects {
		assert.NotEmpty(t, spec.kind, "registry entry with no kind")
		assert.False(t, seen[spec.kind], "duplicate registry entry for %q", spec.kind)
		seen[spec.kind] = true
	}
	for _, kind := range kinds {
		_, ok := notificationSubjectFor(kind)
		assert.True(t, ok, "kind %q has no entry in notificationSubjects", kind)
	}
	assert.Len(t, notificationSubjects, len(kinds),
		"registry declares a kind missing from this test's list")
}

// TestNotificationSubjects_SourceDeclaration pins which kinds can go stale.
// Forge items live upstream and a usage threshold is an aggregate, so neither
// may acquire a probe by accident.
func TestNotificationSubjects_SourceDeclaration(t *testing.T) {
	for _, kind := range []string{NotificationKindForge, NotificationKindUsage} {
		spec, ok := notificationSubjectFor(kind)
		assert.True(t, ok, "%q must be registered", kind)
		assert.False(t, spec.hasSource(), "%q must declare no local source", kind)
	}
	for _, kind := range []string{
		NotificationKindSession, NotificationKindTask,
		NotificationKindMeeting, NotificationKindMail,
	} {
		spec, ok := notificationSubjectFor(kind)
		assert.True(t, ok, "%q must be registered", kind)
		assert.True(t, spec.hasSource(), "%q must declare a source", kind)
	}
}

// TestMarkMissingSources_UnknownKindIsNeverStale covers a row written by a
// newer build (or a typo'd kind): it must stay clickable rather than be marked
// "source deleted" by a probe that does not know what its id means.
func TestMarkMissingSources_UnknownKindIsNeverStale(t *testing.T) {
	cleanup := notificationsTestDB(t)
	defer cleanup()

	rows := []Notification{{ID: 1, Kind: "plugin_new", SubjectID: "gone"}}
	MarkMissingSources(rows)

	assert.False(t, rows[0].SourceMissing)
}

// legacyNotificationsDDL is the shape this table had before subject_id: one
// identity column per kind. It is frozen here on purpose — the migration test
// must keep testing the shape real databases still have, not the shape the
// current DDL creates.
const legacyNotificationsDDL = `
CREATE TABLE notifications (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	event_id        TEXT NOT NULL UNIQUE,
	kind            TEXT NOT NULL,
	event_type      TEXT NOT NULL,
	title           TEXT NOT NULL,
	body            TEXT NOT NULL DEFAULT '',
	project_id      INTEGER NOT NULL DEFAULT 0,
	session_id      TEXT NOT NULL DEFAULT '',
	task_id         TEXT NOT NULL DEFAULT '',
	execution_id    TEXT NOT NULL DEFAULT '',
	forge_item_key  TEXT NOT NULL DEFAULT '',
	agent_id        TEXT NOT NULL DEFAULT '',
	meeting_token   TEXT NOT NULL DEFAULT '',
	mail_message_id INTEGER NOT NULL DEFAULT 0,
	created_at      TEXT NOT NULL
)`

// TestMigrateNotificationSubjects_BackfillsAndDropsLegacyColumns is the
// upgrade-path guard: an existing database must end up with every subject in
// subject_id and none of the per-kind columns left behind.
//
// It also pins idempotence (a second run is a no-op), which is what makes a
// crash between two drops safe to resume.
func TestMigrateNotificationSubjects_BackfillsAndDropsLegacyColumns(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(legacyNotificationsDDL + `;
		CREATE TABLE projects (id INTEGER PRIMARY KEY AUTOINCREMENT, path TEXT NOT NULL);
		INSERT INTO notifications
			(event_id, kind, event_type, title, session_id, task_id, forge_item_key, meeting_token, mail_message_id, created_at)
		VALUES
			('e1', 'session', 'completed',     'S', 's-1', '',      '',        '',   0,  '2026-01-01T00:00:00.000Z'),
			('e2', 'task',    'failed',        'T', '',    't-1',   '',        '',   0,  '2026-01-01T00:00:00.000Z'),
			('e3', 'forge',   'merged',        'F', '',    '',      'issue/1', '',   0,  '2026-01-01T00:00:00.000Z'),
			('e4', 'meeting', 'viewer_joined', 'M', '',    '',      '',        'tok-1', 0, '2026-01-01T00:00:00.000Z'),
			('e5', 'mail',    'received',      'N', '',    '',      '',        '',   42, '2026-01-01T00:00:00.000Z'),
			('e6', 'usage',   'over',          'U', '',    '',      '',        '',   0,  '2026-01-01T00:00:00.000Z')
	`)
	require.NoError(t, err)
	cleanup := store.SetDBForTest(db, db)
	defer cleanup()

	require.NoError(t, migrateNotificationSubjects())

	rows, err := ListNotifications(NotificationFilter{})
	require.NoError(t, err)
	got := map[int64]string{}
	for _, n := range rows {
		got[n.ID] = n.SubjectID
	}
	assert.Equal(t, map[int64]string{
		1: "s-1",
		2: "t-1",
		3: "issue/1",
		4: "tok-1",
		5: "42",
		6: "",
	}, got)

	for _, column := range []string{
		"session_id", "task_id", "forge_item_key", "meeting_token", "mail_message_id",
	} {
		has, err := notificationHasColumn(column)
		require.NoError(t, err)
		assert.False(t, has, "legacy column %q must be dropped", column)
	}

	// Re-running must be a no-op, not an error or a second rewrite.
	require.NoError(t, migrateNotificationSubjects())
}

// TestMigrateNotificationSubjects_NewShapeIsUntouched covers a fresh database:
// the DDL already has subject_id, so the migration must leave it alone.
func TestMigrateNotificationSubjects_NewShapeIsUntouched(t *testing.T) {
	cleanup := notificationsTestDB(t)
	defer cleanup()

	StoreNotification(NotificationRecord{
		EventID: "e1", Kind: NotificationKindSession, EventType: "completed",
		Title: "S", SubjectID: "s-1",
	})

	require.NoError(t, migrateNotificationSubjects())

	rows, err := ListNotifications(NotificationFilter{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "s-1", rows[0].SubjectID)
}
