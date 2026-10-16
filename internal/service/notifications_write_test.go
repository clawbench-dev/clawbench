package service

import (
	"testing"

	"clawbench/internal/store"

	"clawbench/internal/forge"
	"clawbench/internal/model"
	"clawbench/internal/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forgeItem builds a forge.Item for a given type/number/title.
func forgeItem(t *testing.T, typ string, number int, title string) forge.Item {
	t.Helper()
	return forge.Item{
		Platform: forge.PlatformGitHub,
		Type:     forge.ItemType(typ),
		Number:   number,
		Title:    title,
		State:    forge.StateOpen,
	}
}

// forgeChange builds a forge.Change of the given event type.
func forgeChange(eventType string, number int) forge.Change {
	return forge.Change{Type: forge.EventType(eventType), Number: number}
}

// notificationWriteTestDB installs an in-memory DB that has BOTH the
// notification log and the tables the emit sites read while building a row
// (chat_sessions for the title/project, projects for the registry).
func notificationWriteTestDB(t *testing.T) func() {
	t.Helper()
	db := setupChatTestDB(t)
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL DEFAULT 0,
			backend TEXT NOT NULL DEFAULT 'claude',
			title TEXT NOT NULL DEFAULT '',
			agent_id TEXT DEFAULT '',
			external_session_id TEXT DEFAULT '',
			archived INTEGER NOT NULL DEFAULT 0,
			-- Group membership: ResolveGroupTimelineID reads these to map a
			-- hidden member row to its group timeline.
			session_type TEXT NOT NULL DEFAULT 'chat',
			group_id TEXT DEFAULT ''
		);
		CREATE TABLE IF NOT EXISTS scheduled_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL DEFAULT ''
		);
	` + NotificationsDDL)
	require.NoError(t, err)

	cleanup := store.SetDBForTest(db, db)
	store.SeedTestProjectsForTest(t)
	return cleanup
}

func storedNotifications(t *testing.T) []Notification {
	t.Helper()
	rows, err := ListNotifications(NotificationFilter{})
	require.NoError(t, err)
	return rows
}

// ── Task path ───────────────────────────────────────────────────────────────

func TestRecordTaskNotification_ViaEmitTaskEvent(t *testing.T) {
	cleanup := notificationWriteTestDB(t)
	defer cleanup()

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	emitTaskEvent("42", "completed", "100", "", "/proj", "Nightly Audit")

	rows := storedNotifications(t)
	require.Len(t, rows, 1)
	assert.Equal(t, NotificationKindTask, rows[0].Kind)
	assert.Equal(t, "completed", rows[0].EventType)
	assert.Equal(t, "Nightly Audit", rows[0].Title)
	assert.Equal(t, "42", rows[0].TaskID)
	assert.Equal(t, "100", rows[0].ExecutionID)
}

func TestRecordTaskNotification_IncludesRunning(t *testing.T) {
	// "running" is a start notification the completion card and system
	// notification both cover, so the log must cover it too.
	cleanup := notificationWriteTestDB(t)
	defer cleanup()

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	emitTaskEvent("42", "running", "100", "", "/proj", "Nightly Audit")

	rows := storedNotifications(t)
	require.Len(t, rows, 1)
	assert.Equal(t, "running", rows[0].EventType)
}

func TestRecordTaskNotification_SkipsNonNotifiableStatus(t *testing.T) {
	cleanup := notificationWriteTestDB(t)
	defer cleanup()

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	// "read" is a UI-sync signal, not a notification.
	emitTaskEvent("42", "read", "100", "", "/proj", "Nightly Audit")

	assert.Empty(t, storedNotifications(t))
}

func TestRecordTaskNotification_SurvivesPushDisabled(t *testing.T) {
	// The whole point: push_mode=disabled means "don't PUSH", not "don't
	// RECORD". A user who turned push off still wants the history.
	cleanup := notificationWriteTestDB(t)
	defer cleanup()

	orig := model.ConfigInstance.PushMode
	model.ConfigInstance.PushMode = "disabled"
	defer func() { model.ConfigInstance.PushMode = orig }()

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	emitTaskEvent("42", "failed", "100", "", "/proj", "Nightly Audit")

	rows := storedNotifications(t)
	require.Len(t, rows, 1, "recording must not be gated by push_mode")
	assert.Equal(t, "failed", rows[0].EventType)
}

func TestRecordTaskNotification_SurvivesNilManager(t *testing.T) {
	// No WS manager (e.g. headless) must not stop the log: the history does not
	// depend on a live broadcast channel.
	cleanup := notificationWriteTestDB(t)
	defer cleanup()

	ws.SetManagerForTest(nil)

	require.NotPanics(t, func() {
		emitTaskEvent("42", "completed", "100", "", "/proj", "Nightly Audit")
	})

	rows := storedNotifications(t)
	require.Len(t, rows, 1)
	assert.Equal(t, NotificationKindTask, rows[0].Kind)
}

// ── Session path ────────────────────────────────────────────────────────────

func TestRecordSessionNotification_OnTerminalPush(t *testing.T) {
	cleanup := notificationWriteTestDB(t)
	defer cleanup()

	// A session row so the title resolves.
	_, err := store.WriteExec(
		`INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('s1', ?, 'claude', 'My Session')`,
		store.ProjectIDForTest(t, "/proj"),
	)
	require.NoError(t, err)

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	EmitSessionPushNotification("s1", "completed")

	rows := storedNotifications(t)
	require.Len(t, rows, 1)
	assert.Equal(t, NotificationKindSession, rows[0].Kind)
	assert.Equal(t, "completed", rows[0].EventType)
	assert.Equal(t, "s1", rows[0].SessionID)
}

func TestRecordSessionNotification_PermissionPending(t *testing.T) {
	cleanup := notificationWriteTestDB(t)
	defer cleanup()

	_, err := store.WriteExec(
		`INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('s2', ?, 'claude', 'Approval')`,
		store.ProjectIDForTest(t, "/proj"),
	)
	require.NoError(t, err)

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	EmitSessionEvent("s2", "permission_pending", false, "write_file", `{}`)

	rows := storedNotifications(t)
	require.Len(t, rows, 1)
	assert.Equal(t, ws.StatusPermissionPending, rows[0].EventType)
}

func TestRecordSessionNotification_WSOnlyDoesNotRecord(t *testing.T) {
	// EmitSessionEventWSOnly is the echo path used to broadcast a terminal
	// state the terminal push already recorded. It must NOT log a second row,
	// or every completion would appear twice in the history.
	cleanup := notificationWriteTestDB(t)
	defer cleanup()

	_, err := store.WriteExec(
		`INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('s3', ?, 'claude', 'Echo')`,
		store.ProjectIDForTest(t, "/proj"),
	)
	require.NoError(t, err)

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	EmitSessionEventWSOnly("s3", "completed", false)

	assert.Empty(t, storedNotifications(t), "the WS-only echo must not create a log row")
}

func TestRecordSessionNotification_SkipsRunning(t *testing.T) {
	cleanup := notificationWriteTestDB(t)
	defer cleanup()

	_, err := store.WriteExec(
		`INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('s4', ?, 'claude', 'Run')`,
		store.ProjectIDForTest(t, "/proj"),
	)
	require.NoError(t, err)

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	EmitSessionEvent("s4", "running", false)

	assert.Empty(t, storedNotifications(t), "running is not a notifiable status")
}

// ── Forge path ──────────────────────────────────────────────────────────────

func TestForgeRecorderSink_RecordsChange(t *testing.T) {
	cleanup := notificationWriteTestDB(t)
	defer cleanup()

	rec := NewForgeNotificationRecorder()
	repo := ForgeRepoRef{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widget", ProjectPath: "/proj"}
	item := forgeItem(t, "pr", 42, "Fix the thing")
	rec.HandleChange(t.Context(), repo, item, forgeChange("merged", 42))

	rows := storedNotifications(t)
	require.Len(t, rows, 1)
	assert.Equal(t, NotificationKindForge, rows[0].Kind)
	assert.Equal(t, "merged", rows[0].EventType)
	assert.Contains(t, rows[0].Title, "acme/widget")
	assert.Equal(t, "Fix the thing", rows[0].Body)
	assert.Equal(t, "pr/42", rows[0].ForgeItemKey)
}

func TestForgeRecorderSink_PipelineUsesRunKey(t *testing.T) {
	cleanup := notificationWriteTestDB(t)
	defer cleanup()

	rec := NewForgeNotificationRecorder()
	repo := ForgeRepoRef{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widget", ProjectPath: "/proj"}
	item := forgeItem(t, "pipeline", 0, "CI run")
	change := forgeChange("pipeline_done", 0)
	change.PipelineRunID = 555
	rec.HandleChange(t.Context(), repo, item, change)

	rows := storedNotifications(t)
	require.Len(t, rows, 1)
	// A pipeline's number is always 0, so its identity is the run id — matching
	// the frontend's forgeTargetItemKey.
	assert.Equal(t, "pipeline/run:555", rows[0].ForgeItemKey)
}

func TestForgeRecorderSink_SurvivesDisabledToggles(t *testing.T) {
	// The dispatcher's IM push is gated by per-event toggles; the log is a
	// separate sink and must record regardless. Here the recorder is exercised
	// directly, which is what makes it independent of those toggles.
	cleanup := notificationWriteTestDB(t)
	defer cleanup()

	orig := model.ConfigInstance.Forge.Notify
	model.ConfigInstance.Forge.Notify = model.ForgeNotifyConfig{}
	defer func() { model.ConfigInstance.Forge.Notify = orig }()

	rec := NewForgeNotificationRecorder()
	repo := ForgeRepoRef{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widget", ProjectPath: "/proj"}
	rec.HandleChange(t.Context(), repo, forgeItem(t, "issue", 1, "An issue"), forgeChange("opened", 1))

	assert.Len(t, storedNotifications(t), 1)
}

// ── Composite sink wiring ───────────────────────────────────────────────────

func TestCompositeSink_ReachesRecorder(t *testing.T) {
	// The recorder must be reachable through the fan-out the server actually
	// builds, not only when called directly.
	cleanup := notificationWriteTestDB(t)
	defer cleanup()

	rec := NewForgeNotificationRecorder()
	composite := NewForgeCompositeSink(nil, rec) // nil sink is skipped

	repo := ForgeRepoRef{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widget", ProjectPath: "/proj"}
	composite.HandleChange(t.Context(), repo, forgeItem(t, "issue", 7, "Seven"), forgeChange("closed", 7))

	assert.Len(t, storedNotifications(t), 1)
}

// ── Dedup identity ──────────────────────────────────────────────────────────

// TestForgeRecorderSink_DedupesSameChange is the regression guard for the
// INSERT OR IGNORE being inert: the recorder must derive its event_id from the
// change itself, so re-dispatching the same change logs ONE row.
func TestForgeRecorderSink_DedupesSameChange(t *testing.T) {
	cleanup := notificationWriteTestDB(t)
	defer cleanup()

	rec := NewForgeNotificationRecorder()
	repo := ForgeRepoRef{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widget", ProjectPath: "/proj"}
	item := forgeItem(t, "pr", 42, "Fix the thing")
	change := forgeChange("merged", 42)

	rec.HandleChange(t.Context(), repo, item, change)
	rec.HandleChange(t.Context(), repo, item, change) // same change again

	assert.Len(t, storedNotifications(t), 1, "the same change must not log twice")
}

// TestForgeRecorderSink_DistinctChangesAreDistinct pins the other direction:
// the dedup key must not be so coarse that two real events collapse into one.
func TestForgeRecorderSink_DistinctChangesAreDistinct(t *testing.T) {
	cleanup := notificationWriteTestDB(t)
	defer cleanup()

	rec := NewForgeNotificationRecorder()
	repo := ForgeRepoRef{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widget", ProjectPath: "/proj"}
	item := forgeItem(t, "pr", 42, "Fix the thing")

	// opened and reopened both end in state=open; they must stay distinct.
	rec.HandleChange(t.Context(), repo, item, forgeChange("opened", 42))
	rec.HandleChange(t.Context(), repo, item, forgeChange("reopened", 42))

	assert.Len(t, storedNotifications(t), 2)
}

// TestSessionRecordSurvivesNilManager is the regression guard for the session
// record being dropped when no WS manager exists: the log must not depend on a
// live broadcast channel (emitTaskEvent already behaves this way).
func TestSessionRecordSurvivesNilManager(t *testing.T) {
	cleanup := notificationWriteTestDB(t)
	defer cleanup()

	_, err := store.WriteExec(
		`INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('s-nil', ?, 'claude', 'No Manager')`,
		store.ProjectIDForTest(t, "/proj"),
	)
	require.NoError(t, err)

	ws.SetManagerForTest(nil)

	require.NotPanics(t, func() {
		EmitSessionEvent("s-nil", "permission_pending", false, "write_file", `{}`)
	})

	rows := storedNotifications(t)
	require.Len(t, rows, 1, "recording must not depend on a WS manager")
	assert.Equal(t, ws.StatusPermissionPending, rows[0].EventType)
}

// TestEmitSessionEvent_NoDBNoManagerDoesNotPanic guards the path that used to
// return before touching anything.
//
// Making the record run above the mgr == nil guard moved the payload build
// (which reads chat_sessions) into a path that previously never executed
// without a manager. A teardown that has already closed the DB handle must not
// crash the caller — recording is best-effort.
func TestEmitSessionEvent_NoDBNoManagerDoesNotPanic(t *testing.T) {
	// Install an EMPTY database, then drop the handle while the manager is nil:
	// exactly the state a test teardown leaves behind.
	db, teardown := setupNotificationsDB(t)
	defer teardown()
	cleanup := store.SetDBForTest(db, db)

	ws.SetManagerForTest(nil)
	cleanup() // handle removed, manager still nil

	require.NotPanics(t, func() {
		EmitSessionEvent("s-closed", "completed", false)
	})
}

// ── Group member rows must not leak into the log ────────────────────────────

// TestRecordSessionNotification_MemberRowLogsGroup is the regression guard for
// the notification log pointing at a hidden group MEMBER row.
//
// A member's ACP connection is keyed by its MEMBER row id, so the
// permission_pending callback (cmd/server/main.go) reaches the recorder with a
// member id. A member row has no title and is hidden from the session list, so
// clicking such a row opened an empty standalone agent chat instead of the
// group. The log must always name the group timeline.
func TestRecordSessionNotification_MemberRowLogsGroup(t *testing.T) {
	cleanup := notificationWriteTestDB(t)
	defer cleanup()

	pid := store.ProjectIDForTest(t, "/proj")
	_, err := store.WriteExec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, session_type) VALUES ('grp-1', ?, 'claude', 'Standup', 'group')`, pid,
	)
	require.NoError(t, err)
	// The member row carries NO title (that is what makes it unusable as a
	// navigation target) and points at its group.
	_, err = store.WriteExec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, session_type, group_id) VALUES ('mem-1', ?, 'claude', '', 'group_member', 'grp-1')`, pid,
	)
	require.NoError(t, err)

	ws.SetManagerForTest(nil)

	EmitSessionEvent("mem-1", "permission_pending", false, "write_file", `{}`)

	rows := storedNotifications(t)
	require.Len(t, rows, 1)
	assert.Equal(t, "grp-1", rows[0].SessionID,
		"a member row must be logged as its group timeline, not the hidden member")
	assert.Equal(t, "Standup", rows[0].Title, "the row should carry the group's title")
}

// TestResolveGroupTimelineID pins the mapping itself, including the identity
// cases it must NOT rewrite.
func TestResolveGroupTimelineID(t *testing.T) {
	cleanup := notificationWriteTestDB(t)
	defer cleanup()

	pid := store.ProjectIDForTest(t, "/proj")
	for _, row := range []struct{ id, typ, group string }{
		{"grp-1", "group", ""},
		{"mem-1", "group_member", "grp-1"},
		{"chat-1", "chat", ""},
	} {
		_, err := store.WriteExec(
			`INSERT INTO chat_sessions (id, project_id, backend, title, session_type, group_id) VALUES (?, ?, 'claude', '', ?, ?)`,
			row.id, pid, row.typ, row.group,
		)
		require.NoError(t, err)
	}

	assert.Equal(t, "grp-1", ResolveGroupTimelineID("mem-1"), "member -> group")
	assert.Equal(t, "grp-1", ResolveGroupTimelineID("grp-1"), "group is its own timeline")
	assert.Equal(t, "chat-1", ResolveGroupTimelineID("chat-1"), "ordinary chat unchanged")
	assert.Equal(t, "unknown", ResolveGroupTimelineID("unknown"), "unknown id unchanged")
}

// TestMarkMissingSources_RepairsLegacyMemberRows covers rows already on disk
// from before the write-time fix: the retention window is 30 days, so a
// write-time-only fix would leave them broken until they age out.
func TestMarkMissingSources_RepairsLegacyMemberRows(t *testing.T) {
	cleanup := notificationWriteTestDB(t)
	defer cleanup()

	pid := store.ProjectIDForTest(t, "/proj")
	_, err := store.WriteExec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, session_type) VALUES ('grp-1', ?, 'claude', 'Standup', 'group')`, pid,
	)
	require.NoError(t, err)
	_, err = store.WriteExec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, session_type, group_id) VALUES ('mem-1', ?, 'claude', '', 'group_member', 'grp-1')`, pid,
	)
	require.NoError(t, err)

	// A row stored the old way: pointing straight at the member row.
	rows := []Notification{
		{ID: 1, Kind: NotificationKindSession, SessionID: "mem-1"},
		{ID: 2, Kind: NotificationKindSession, SessionID: "grp-1"},
	}
	MarkMissingSources(rows)

	assert.Equal(t, "grp-1", rows[0].SessionID, "a legacy member-row target is repaired to the group")
	assert.False(t, rows[0].SourceMissing, "the group exists, so the repaired row is navigable")
	assert.Equal(t, "grp-1", rows[1].SessionID, "a group row is left alone")
}

// TestMarkMissingSources_RepairFailureIsNonFatal pins the best-effort contract:
// when the member lookup cannot run, rows keep their stored id rather than the
// list failing or being marked missing.
func TestMarkMissingSources_RepairFailureIsNonFatal(t *testing.T) {
	// No chat_sessions table at all -> the repair query errors.
	db, teardown := setupNotificationsDB(t)
	defer teardown()
	cleanup := store.SetDBForTest(db, db)
	defer cleanup()

	rows := []Notification{{ID: 1, Kind: NotificationKindSession, SessionID: "mem-1"}}
	require.NotPanics(t, func() { MarkMissingSources(rows) })
	assert.Equal(t, "mem-1", rows[0].SessionID, "unrepairable rows keep their stored id")
}
