package service

import (
	"testing"

	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustExec(t *testing.T, query string, args ...any) {
	t.Helper()
	_, err := store.WriteExec(query, args...)
	require.NoError(t, err)
}

// subjectProbeSchema adds the tables the presence probe reads but the shared
// notifications fixture does not create. Everything is done on the raw handle
// BEFORE it is installed, so a failed DDL cannot leave a half-installed global
// handle behind for the next test.
func subjectProbeSchema(t *testing.T) func() {
	t.Helper()
	db, teardown := setupNotificationsDB(t)
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS live_notes (token TEXT PRIMARY KEY)`,
		`CREATE TABLE IF NOT EXISTS mail_messages (id INTEGER PRIMARY KEY)`,
		// chat_sessions exists already; the group-repair probe needs these two.
		`ALTER TABLE chat_sessions ADD COLUMN session_type TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE chat_sessions ADD COLUMN group_id TEXT NOT NULL DEFAULT ''`,
	} {
		_, err := db.Exec(ddl)
		require.NoError(t, err)
	}
	cleanup := store.SetDBForTest(db, db)
	store.SeedTestProjectsForTest(t)
	return func() {
		cleanup()
		teardown()
	}
}

func TestMarkMissingSources_FlagsOnlyGoneSubjects(t *testing.T) {
	cleanup := subjectProbeSchema(t)
	defer cleanup()

	mustExec(t, "INSERT INTO chat_sessions (id) VALUES ('s-live')")
	mustExec(t, "INSERT INTO scheduled_tasks (id) VALUES (7)")
	mustExec(t, "INSERT INTO live_notes (token) VALUES ('tok-live')")
	mustExec(t, "INSERT INTO mail_messages (id) VALUES (42)")

	rows := []Notification{
		{ID: 1, Kind: NotificationKindSession, SubjectID: "s-live"},    // present
		{ID: 2, Kind: NotificationKindSession, SubjectID: "s-gone"},    // deleted
		{ID: 3, Kind: NotificationKindTask, SubjectID: "7"},            // present
		{ID: 4, Kind: NotificationKindTask, SubjectID: "8"},            // deleted
		{ID: 5, Kind: NotificationKindMeeting, SubjectID: "tok-live"},  // present (record row)
		{ID: 6, Kind: NotificationKindMeeting, SubjectID: "tok-gone"},  // deleted record
		{ID: 7, Kind: NotificationKindMail, SubjectID: "42"},           // present
		{ID: 8, Kind: NotificationKindMail, SubjectID: "43"},           // deleted
		{ID: 9, Kind: NotificationKindMail, SubjectID: "not-a-number"}, // malformed: never probed
		{ID: 10, Kind: NotificationKindUsage, SubjectID: "x"},          // no local source
		{ID: 11, Kind: "plugin_new", SubjectID: "x"},                   // unregistered kind
	}
	MarkMissingSources(rows)

	got := map[int64]bool{}
	for _, n := range rows {
		got[n.ID] = n.SourceMissing
	}
	assert.Equal(t, map[int64]bool{
		1: false, 2: true,
		3: false, 4: true,
		5: false, 6: true,
		7: false, 8: true,
		9: false, // malformed id must not be judged stale
		10: false, 11: false,
	}, got)
}

// A page with no source-bearing rows still costs zero queries and flags nothing.
func TestMarkMissingSources_EmptyAndSourcelessPages(t *testing.T) {
	cleanup := subjectProbeSchema(t)
	defer cleanup()

	MarkMissingSources(nil) // len == 0 short-circuit
	rows := []Notification{
		{ID: 1, Kind: NotificationKindUsage},
		{ID: 2, Kind: NotificationKindSession, SubjectID: ""}, // no id: never probed
	}
	MarkMissingSources(rows)
	assert.False(t, rows[0].SourceMissing)
	assert.False(t, rows[1].SourceMissing)
}

func TestRepairGroupMemberTargets_RewritesMemberRowToGroup(t *testing.T) {
	cleanup := subjectProbeSchema(t)
	defer cleanup()

	mustExec(t, "INSERT INTO chat_sessions (id, session_type, group_id) VALUES ('member-1', ?, 'group-1')",
		groupMemberSessionType)
	mustExec(t, "INSERT INTO chat_sessions (id, session_type, group_id) VALUES ('group-1', 'group', '')")

	rows := []Notification{
		{ID: 1, Kind: NotificationKindSession, SubjectID: "member-1"},
		{ID: 2, Kind: NotificationKindSession, SubjectID: "group-1"},
		{ID: 3, Kind: NotificationKindTask, SubjectID: "member-1"}, // other kinds untouched
	}
	repairGroupMemberTargets(rows)

	assert.Equal(t, "group-1", rows[0].SubjectID, "member rows must point at the group timeline")
	assert.Equal(t, "group-1", rows[1].SubjectID)
	assert.Equal(t, "member-1", rows[2].SubjectID)
}

func TestRepairGroupMemberTargets_NoSessionsIsNoop(t *testing.T) {
	cleanup := subjectProbeSchema(t)
	defer cleanup()

	rows := []Notification{{ID: 1, Kind: NotificationKindTask, SubjectID: "7"}}
	repairGroupMemberTargets(rows)
	assert.Equal(t, "7", rows[0].SubjectID)

	// Session rows that are not member rows keep their stored id.
	mustExec(t, "INSERT INTO chat_sessions (id) VALUES ('plain')")
	rows = []Notification{{ID: 2, Kind: NotificationKindSession, SubjectID: "plain"}}
	repairGroupMemberTargets(rows)
	assert.Equal(t, "plain", rows[0].SubjectID)
}

func TestNotificationSubjectIDOf(t *testing.T) {
	text := notificationSubject{kind: NotificationKindSession}
	id, ok := text.idOf(&Notification{SubjectID: ""})
	assert.False(t, ok)
	assert.Empty(t, id)

	id, ok = text.idOf(&Notification{SubjectID: "s-1"})
	assert.True(t, ok)
	assert.Equal(t, "s-1", id)

	integer := notificationSubject{kind: NotificationKindMail, integer: true}
	_, ok = integer.idOf(&Notification{SubjectID: "abc"})
	assert.False(t, ok, "a malformed integer id must not be probed")

	id, ok = integer.idOf(&Notification{SubjectID: "42"})
	assert.True(t, ok)
	assert.Equal(t, "42", id)
}

func TestSubjectPresenceMissing_FailsOpenWhenProbeDidNotRun(t *testing.T) {
	var unrun subjectPresence // present == nil
	assert.False(t, unrun.missing(&Notification{Kind: NotificationKindSession, SubjectID: "gone"}),
		"a probe that never ran must not mark anything stale")

	run := subjectPresence{present: map[string]bool{}}
	assert.True(t, run.missing(&Notification{Kind: NotificationKindSession, SubjectID: "gone"}))
	assert.False(t, run.missing(&Notification{Kind: NotificationKindUsage, SubjectID: "x"}),
		"kinds without a local source are never stale")
}

func TestDistinctNonEmpty(t *testing.T) {
	rows := []Notification{
		{SubjectID: "a"}, {SubjectID: ""}, {SubjectID: "a"}, {SubjectID: "b"},
	}
	got := distinctNonEmpty(rows, func(n Notification) string { return n.SubjectID })
	assert.Equal(t, []string{"a", "b"}, got)
	assert.Empty(t, distinctNonEmpty(nil, func(n Notification) string { return n.SubjectID }))
}

func TestExistingIDs_EmptyInputShortCircuits(t *testing.T) {
	cleanup := subjectProbeSchema(t)
	defer cleanup()

	assert.Empty(t, existingIDs("chat_sessions", "id", nil))
	assert.Empty(t, existingInt64IDs("mail_messages", "id", nil))
}
