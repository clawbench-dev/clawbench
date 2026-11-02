package service

import (
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"
	"clawbench/internal/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// meetingNotifyTestDB installs an in-memory DB with the notification log, the
// projects registry and live_notes — the three tables a meeting notification
// touches (the log itself, the project id, and the presence probe).
func meetingNotifyTestDB(t *testing.T) func() {
	t.Helper()
	db := setupChatTestDB(t)
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS live_notes (
			token TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL DEFAULT 0,
			title TEXT NOT NULL DEFAULT ''
		);
	` + NotificationsDDL)
	require.NoError(t, err)
	cleanup := store.SetDBForTest(db, db)
	store.SeedTestProjectsForTest(t)
	return cleanup
}

// withLanguage pins the configured UI language for one test. The wording of a
// meeting notification is rendered from it (the minutes job has no request in
// scope), so a test that asserts the sentence must control it.
func withLanguage(t *testing.T, lang string) {
	t.Helper()
	prev := model.Language
	model.Language = lang
	t.Cleanup(func() { model.Language = prev })
}

func TestEmitMeetingEvent_LogsControlRequest(t *testing.T) {
	cleanup := meetingNotifyTestDB(t)
	defer cleanup()
	withLanguage(t, "zh")

	EmitMeetingEvent("tok-1", "/proj", MeetingEventControlRequest, "Weekly sync", "v3", "张三")

	rows := storedNotifications(t)
	require.Len(t, rows, 1)
	assert.Equal(t, NotificationKindMeeting, rows[0].Kind)
	assert.Equal(t, MeetingEventControlRequest, rows[0].EventType)
	assert.Equal(t, "Weekly sync", rows[0].Title)
	assert.Equal(t, "张三 请求远程控制你的屏幕", rows[0].Body)
	assert.Equal(t, "tok-1", rows[0].MeetingToken, "the share token is the row's navigation key")
	assert.NotZero(t, rows[0].ProjectID, "the row must be attributable to its project")
	assert.Empty(t, rows[0].SessionID, "a meeting has no session")
}

func TestEmitMeetingEvent_LocalizesFromConfiguredLanguage(t *testing.T) {
	cleanup := meetingNotifyTestDB(t)
	defer cleanup()
	withLanguage(t, "en")

	EmitMeetingEvent("tok-1", "/proj", MeetingEventViewerJoined, "Weekly sync", "v3", "Ada")

	rows := storedNotifications(t)
	require.Len(t, rows, 1)
	assert.Equal(t, "Ada joined the meeting", rows[0].Body)
}

func TestEmitMeetingEvent_MinutesEventsHaveNoActor(t *testing.T) {
	cleanup := meetingNotifyTestDB(t)
	defer cleanup()
	withLanguage(t, "zh")

	EmitMeetingEvent("tok-1", "/proj", MeetingEventMinutesReady, "Weekly sync", "", "")
	EmitMeetingEvent("tok-2", "/proj", MeetingEventMinutesFailed, "Weekly sync", "", "")

	rows := storedNotifications(t)
	require.Len(t, rows, 2)
	// Newest first.
	assert.Equal(t, MeetingEventMinutesFailed, rows[0].EventType)
	assert.Equal(t, "会议纪要生成失败", rows[0].Body)
	assert.Equal(t, MeetingEventMinutesReady, rows[1].EventType)
	assert.Equal(t, "会议纪要已生成", rows[1].Body)
}

// TestEmitMeetingEvent_UnknownTypeIsDropped pins the refusal: a row whose body
// is empty renders as an empty card and gives the client nothing to show.
func TestEmitMeetingEvent_UnknownTypeIsDropped(t *testing.T) {
	cleanup := meetingNotifyTestDB(t)
	defer cleanup()

	EmitMeetingEvent("tok-1", "/proj", "not_a_meeting_event", "Weekly sync", "v3", "张三")

	assert.Empty(t, storedNotifications(t))
}

// TestEmitMeetingEvent_BlankTokenIsDropped: the token is the row's identity and
// its navigation target; without one the log entry can never be opened.
func TestEmitMeetingEvent_BlankTokenIsDropped(t *testing.T) {
	cleanup := meetingNotifyTestDB(t)
	defer cleanup()

	EmitMeetingEvent("", "/proj", MeetingEventControlRequest, "Weekly sync", "v3", "张三")

	assert.Empty(t, storedNotifications(t))
}

// TestEmitMeetingEvent_NoDBDoesNotPanic: the emitter is called from socket
// handlers, and a server running without a database must not crash on a
// meeting event (recording is best-effort, like every other notification).
func TestEmitMeetingEvent_NoDBDoesNotPanic(t *testing.T) {
	cleanup := store.SetDBForTest(nil, nil)
	defer cleanup()

	require.NotPanics(t, func() {
		EmitMeetingEvent("tok-1", "/proj", MeetingEventControlRequest, "Weekly sync", "v3", "张三")
	})
}

func TestMeetingEventMessage_CarriesFacts(t *testing.T) {
	msg := meetingEventMessage("evt-1", "tok-1", "/proj", MeetingEventScreenShare, "Weekly sync", "张三 开始共享屏幕", "v3", "张三")

	assert.Equal(t, ws.MessageTypeEvent, msg.Type)
	assert.Equal(t, "evt-1", msg.ID, "the frame shares the log row's event id")
	assert.Equal(t, "live_event", msg.Event)
	payload, ok := msg.Data.(*ws.MeetingEventData)
	require.True(t, ok)
	assert.Equal(t, "tok-1", payload.Token)
	assert.Equal(t, MeetingEventScreenShare, payload.EventType)
	assert.Equal(t, "Weekly sync", payload.Title)
	assert.Equal(t, "张三 开始共享屏幕", payload.Body)
	assert.Equal(t, "v3", payload.ViewerID)
	assert.Equal(t, "张三", payload.ViewerName)
	assert.Equal(t, "/proj", payload.ProjectPath)
}

// TestMarkMissingSources_ProbesLiveNotes pins the meeting presence probe: the
// in-memory room dies with the meeting, so the RECORD row is what the click
// opens and what decides staleness.
func TestMarkMissingSources_ProbesLiveNotes(t *testing.T) {
	cleanup := meetingNotifyTestDB(t)
	defer cleanup()

	_, err := store.WriteExec(
		"INSERT INTO live_notes (token, title) VALUES ('tok-live', 'Weekly sync')",
	)
	require.NoError(t, err)

	rows := []Notification{
		{ID: 1, Kind: NotificationKindMeeting, MeetingToken: "tok-live"},
		{ID: 2, Kind: NotificationKindMeeting, MeetingToken: "tok-deleted"},
	}
	MarkMissingSources(rows)

	assert.False(t, rows[0].SourceMissing, "the record still exists, so the row is navigable")
	assert.True(t, rows[1].SourceMissing, "a deleted record makes the row stale")
}
