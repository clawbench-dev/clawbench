package service

import (
	"strings"

	"clawbench/internal/store"
	"clawbench/internal/ws"
)

// Meeting event types. One vocabulary serves both the notification-center row
// (notifications.event_type) and the live_event payload, so the log and the
// popup can never disagree about what happened.
const (
	// MeetingEventControlRequest: a viewer asked for remote control of the
	// host's screen. The only meeting event that needs an ANSWER, which is why
	// it is the one the host must not miss.
	MeetingEventControlRequest = "control_request"
	// MeetingEventViewerJoined: somebody entered the meeting.
	MeetingEventViewerJoined = "viewer_joined"
	// MeetingEventScreenShare: a member started sharing their own screen.
	MeetingEventScreenShare = "screen_share"
	// MeetingEventMinutesReady / MeetingEventMinutesFailed: the detached
	// post-meeting job (proofread + summarize, up to ten minutes) finished.
	MeetingEventMinutesReady  = "minutes_ready"
	MeetingEventMinutesFailed = "minutes_failed"
	// MeetingEventRevisedReady / MeetingEventRevisedFailed: a regeneration whose
	// last step was the proofread transcript (no minutes were requested).
	MeetingEventRevisedReady  = "revised_ready"
	MeetingEventRevisedFailed = "revised_failed"
	// MeetingEventTranscriptReady / MeetingEventTranscriptFailed: a regeneration
	// whose last step was the transcript itself. Recognition can run for
	// minutes, so the host must hear about it even after leaving the page.
	MeetingEventTranscriptReady  = "transcript_ready"
	MeetingEventTranscriptFailed = "transcript_failed"
)

// EmitMeetingEvent records one meeting event in the notification log and
// broadcasts it to connected clients.
//
// The two halves have different audiences and different lifetimes, which is why
// one call does both:
//
//   - the notification-center row is the durable log ("this happened"), written
//     even when nobody is watching and independent of every push switch — the
//     same rule the session/task/forge recorders follow;
//   - the WS event is the live signal. The host's client turns it into an
//     in-app toast (page alive, meeting page not showing) or an OS notification
//     (app backgrounded); both are gated client-side, because only the client
//     knows whether the meeting page is the thing on screen.
//
// eventID is shared by the row and the frame so a re-emitted event cannot log
// twice (notifications.event_id is UNIQUE). projectID is resolved here from the
// path, like the session/task adapters, so the log's project filter works; a
// resolution failure leaves the row global rather than dropping it.
func EmitMeetingEvent(token, projectPath, eventType, meetingTitle, viewerID, viewerName string) {
	if token == "" || eventType == "" {
		return
	}
	body := meetingEventBody(eventType, viewerName)
	if body == "" {
		// Unknown type: a row with no sentence renders as an empty card, and
		// the client would have nothing to show. Refuse instead.
		return
	}
	title := strings.TrimSpace(meetingTitle)
	if title == "" {
		title = liveNoteT("MeetingUntitled")
	}

	eventID := ws.GenerateEventID()
	projectID := int64(0)
	if projectPath != "" && store.DBReady() {
		if id, err := store.ProjectIDForPath(projectPath); err == nil {
			projectID = id
		}
	}

	// Log first: it must not depend on a WS manager existing (background jobs
	// and tests run without one).
	StoreNotification(NotificationRecord{
		EventID:      eventID,
		Kind:         NotificationKindMeeting,
		EventType:    eventType,
		Title:        title,
		Body:         body,
		ProjectID:    projectID,
		MeetingToken: token,
	})

	mgr := ws.GetManager()
	if mgr == nil {
		return
	}
	mgr.BroadcastEvent(meetingEventMessage(
		eventID, token, projectPath, eventType, title, body, viewerID, viewerName,
	))
}

// meetingEventMessage builds the live_event frame for one meeting occurrence.
//
// Split out of EmitMeetingEvent so the payload can be asserted without a WS
// manager: BroadcastEvent with no subscribers is a no-op, so a test that only
// went through the emitter could never see the frame it produced.
func meetingEventMessage(eventID, token, projectPath, eventType, title, body, viewerID, viewerName string) ws.ServerMessage {
	return ws.ServerMessage{
		Type:  ws.MessageTypeEvent,
		ID:    eventID,
		Event: "live_event",
		Data: &ws.MeetingEventData{
			Token:       token,
			EventType:   eventType,
			Title:       title,
			Body:        body,
			ViewerID:    viewerID,
			ViewerName:  viewerName,
			ProjectPath: projectPath,
		},
	}
}

// meetingEventBody renders the one-sentence description stored in the log and
// shown in the notification.
//
// Localized from the configured UI language (model.Language), not from an HTTP
// request: the minutes events are produced by a background goroutine with no
// request in scope, and a notification must read the same in both places.
//
// Returns "" for an unknown type, which the caller treats as "do not record".
func meetingEventBody(eventType, viewerName string) string {
	name := strings.TrimSpace(viewerName)
	if name == "" {
		name = liveNoteT("MeetingEventSomeone")
	}
	// i18nNameKey is the shared `{{.Name}}` placeholder every "who did it"
	// sentence takes (same convention the group system messages use).
	data := map[string]any{i18nNameKey: name}
	switch eventType {
	case MeetingEventControlRequest:
		return liveNoteT("MeetingEventControlRequest", data)
	case MeetingEventViewerJoined:
		return liveNoteT("MeetingEventViewerJoined", data)
	case MeetingEventScreenShare:
		return liveNoteT("MeetingEventScreenShare", data)
	case MeetingEventMinutesReady:
		return liveNoteT("MeetingEventMinutesReady")
	case MeetingEventMinutesFailed:
		return liveNoteT("MeetingEventMinutesFailed")
	case MeetingEventRevisedReady:
		return liveNoteT("MeetingEventRevisedReady")
	case MeetingEventRevisedFailed:
		return liveNoteT("MeetingEventRevisedFailed")
	case MeetingEventTranscriptReady:
		return liveNoteT("MeetingEventTranscriptReady")
	case MeetingEventTranscriptFailed:
		return liveNoteT("MeetingEventTranscriptFailed")
	default:
		return ""
	}
}
