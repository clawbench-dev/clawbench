package liveshare

import (
	"errors"
	"testing"
	"time"
)

func newTalkRoom(t *testing.T) (*Room, *Viewer) {
	t.Helper()
	g := NewRegistry()
	room, err := g.Create("/proj/a", "A")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	v, err := room.AddViewer("", &fakeSink{})
	if err != nil {
		t.Fatalf("AddViewer: %v", err)
	}
	return room, v
}

func TestRequestTalk_RejectedWhenRoomDisabled(t *testing.T) {
	room, v := newTalkRoom(t)

	// talkEnabled defaults to false: an unconfigured room must never let an
	// anonymous viewer start speaking.
	if _, err := room.RequestTalk(v.ID); !errors.Is(err, ErrTalkNotAllowed) {
		t.Fatalf("want ErrTalkNotAllowed, got %v", err)
	}
	if v.TalkState() != TalkNone {
		t.Fatalf("state must stay none, got %v", v.TalkState())
	}
}

func TestRequestTalk_RejectedForUnknownViewer(t *testing.T) {
	room, _ := newTalkRoom(t)
	room.SetTalkEnabled(true)

	if _, err := room.RequestTalk("nope"); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("want ErrRoomNotFound, got %v", err)
	}
}

func TestTalkLifecycle_PendingGrantTalkingEnd(t *testing.T) {
	room, v := newTalkRoom(t)
	room.SetTalkEnabled(true)

	st, err := room.RequestTalk(v.ID)
	if err != nil {
		t.Fatalf("RequestTalk: %v", err)
	}
	if st != TalkPending {
		t.Fatalf("want pending, got %v", st)
	}

	if err := room.GrantTalk(v.ID); err != nil {
		t.Fatalf("GrantTalk: %v", err)
	}
	if v.TalkState() != TalkGranted {
		t.Fatalf("want granted, got %v", v.TalkState())
	}

	if err := room.SetTalking(v.ID, true); err != nil {
		t.Fatalf("SetTalking(true): %v", err)
	}
	if v.TalkState() != TalkTalking {
		t.Fatalf("want talking, got %v", v.TalkState())
	}

	if err := room.SetTalking(v.ID, false); err != nil {
		t.Fatalf("SetTalking(false): %v", err)
	}
	// Stopping returns to granted, not none: the grant outlives one utterance.
	if v.TalkState() != TalkGranted {
		t.Fatalf("want granted after stop, got %v", v.TalkState())
	}
}

func TestSetTalking_RejectedWithoutGrant(t *testing.T) {
	room, v := newTalkRoom(t)
	room.SetTalkEnabled(true)

	// This is the case that matters: a client that skips the grant handshake
	// must be rejected loudly rather than silently ignored.
	if err := room.SetTalking(v.ID, true); !errors.Is(err, ErrTalkNotGranted) {
		t.Fatalf("want ErrTalkNotGranted, got %v", err)
	}
	if v.Talking() {
		t.Fatal("viewer must not be marked as talking")
	}
}

func TestSetTalking_StopAlwaysAllowed(t *testing.T) {
	room, v := newTalkRoom(t)
	room.SetTalkEnabled(true)
	_ = room.GrantTalk(v.ID)
	_ = room.SetTalking(v.ID, true)

	// Revoke mid-sentence, then let the client report the stop it already
	// started. Refusing this would leave the UI stuck in "talking".
	if err := room.RevokeTalk(v.ID); err != nil {
		t.Fatalf("RevokeTalk: %v", err)
	}
	if err := room.SetTalking(v.ID, false); err != nil {
		t.Fatalf("stopping after revoke must succeed, got %v", err)
	}
}

func TestGrantTalk_RejectedWhenRoomDisabled(t *testing.T) {
	room, v := newTalkRoom(t)

	// A stale publisher UI could still send talk-grant after the switch was
	// turned off; the room must refuse.
	if err := room.GrantTalk(v.ID); !errors.Is(err, ErrTalkNotAllowed) {
		t.Fatalf("want ErrTalkNotAllowed, got %v", err)
	}
}

func TestGrantTalk_IsIdempotent(t *testing.T) {
	room, v := newTalkRoom(t)
	room.SetTalkEnabled(true)

	if err := room.GrantTalk(v.ID); err != nil {
		t.Fatalf("first grant: %v", err)
	}
	if err := room.GrantTalk(v.ID); err != nil {
		t.Fatalf("second grant must be a no-op, got %v", err)
	}
	if !v.CanTalk() {
		t.Fatal("viewer should remain granted")
	}
}

func TestRequestTalk_IsIdempotentWhilePending(t *testing.T) {
	room, v := newTalkRoom(t)
	room.SetTalkEnabled(true)

	if _, err := room.RequestTalk(v.ID); err != nil {
		t.Fatalf("first request: %v", err)
	}
	// The viewer UI retries on a flaky socket; that must not be an error.
	st, err := room.RequestTalk(v.ID)
	if err != nil {
		t.Fatalf("second request must not fail, got %v", err)
	}
	if st != TalkPending {
		t.Fatalf("want pending, got %v", st)
	}
}

func TestRequestTalk_AfterGrantReturnsGranted(t *testing.T) {
	room, v := newTalkRoom(t)
	room.SetTalkEnabled(true)
	_ = room.GrantTalk(v.ID)

	st, err := room.RequestTalk(v.ID)
	if err != nil {
		t.Fatalf("RequestTalk: %v", err)
	}
	if st != TalkGranted {
		t.Fatalf("want granted (not pending), got %v", st)
	}
}

func TestRevokeTalk_ClearsAllThreeFlags(t *testing.T) {
	room, v := newTalkRoom(t)
	room.SetTalkEnabled(true)
	_ = room.GrantTalk(v.ID)
	_ = room.SetTalking(v.ID, true)

	if err := room.RevokeTalk(v.ID); err != nil {
		t.Fatalf("RevokeTalk: %v", err)
	}
	if v.CanTalk() || v.Talking() {
		t.Fatalf("revoke must clear grant and speech: canTalk=%v talking=%v", v.CanTalk(), v.Talking())
	}
	if v.TalkState() != TalkNone {
		t.Fatalf("want none, got %v", v.TalkState())
	}
}

func TestMuteAll_ReturnsOnlyAffectedViewers(t *testing.T) {
	g := NewRegistry()
	g.SetLimits(10, time.Hour)
	room, _ := g.Create("/proj/a", "A")
	room.SetTalkEnabled(true)

	silent, _ := room.AddViewer("", &fakeSink{})
	granted, _ := room.AddViewer("", &fakeSink{})
	pending, _ := room.AddViewer("", &fakeSink{})
	talking, _ := room.AddViewer("", &fakeSink{})

	_ = room.GrantTalk(granted.ID)
	_, _ = room.RequestTalk(pending.ID)
	_ = room.GrantTalk(talking.ID)
	_ = room.SetTalking(talking.ID, true)

	affected := room.MuteAll()

	// Only the three with a live state should be notified; the silent viewer
	// must not receive a pointless "revoked" frame.
	if len(affected) != 3 {
		t.Fatalf("want 3 affected, got %d (%v)", len(affected), affected)
	}
	for _, id := range affected {
		if id == silent.ID {
			t.Fatal("a viewer with no state must not be reported as affected")
		}
	}
	for _, v := range []*Viewer{granted, pending, talking} {
		if v.CanTalk() || v.Talking() {
			t.Fatalf("viewer %s not muted", v.ID)
		}
	}
}

func TestMuteAll_OnEmptyRoom(t *testing.T) {
	g := NewRegistry()
	room, _ := g.Create("/proj/a", "A")

	if got := room.MuteAll(); len(got) != 0 {
		t.Fatalf("want no affected viewers, got %v", got)
	}
}

func TestTalk_UnknownViewerIsRoomNotFound(t *testing.T) {
	room, _ := newTalkRoom(t)
	room.SetTalkEnabled(true)

	if err := room.GrantTalk("nope"); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("GrantTalk: want ErrRoomNotFound, got %v", err)
	}
	if err := room.RevokeTalk("nope"); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("RevokeTalk: want ErrRoomNotFound, got %v", err)
	}
	if err := room.SetTalking("nope", false); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("SetTalking: want ErrRoomNotFound, got %v", err)
	}
}

func TestTalkState_String(t *testing.T) {
	cases := map[TalkState]string{
		TalkNone:      "none",
		TalkPending:   "pending",
		TalkGranted:   "granted",
		TalkTalking:   "talking",
		TalkState(99): "unknown",
	}
	for st, want := range cases {
		if got := st.String(); got != want {
			t.Fatalf("TalkState(%d).String() = %q, want %q", int(st), got, want)
		}
	}
}
