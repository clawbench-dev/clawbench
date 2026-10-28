package liveshare

import (
	"errors"
	"testing"
)

// Speech model: ONE room-level switch plus ONE per-viewer microphone flag.
// There is no request/grant handshake — the room switch is the authorisation.

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

func TestSetTalking_RejectedWhenRoomDisabled(t *testing.T) {
	room, v := newTalkRoom(t)

	// The room switch defaults to off and is the ONLY authorisation. A client
	// that turns its mic on anyway must be refused, not silently accepted.
	err := room.SetTalking(v.ID, true)
	if !errors.Is(err, ErrTalkNotAllowed) {
		t.Fatalf("want ErrTalkNotAllowed, got %v", err)
	}
	if v.Talking() {
		t.Fatal("viewer must not be marked as speaking")
	}
	if v.TalkState() != TalkNone {
		t.Fatalf("state must stay none, got %v", v.TalkState())
	}
}

func TestSetTalking_AllowedWhenRoomEnabled(t *testing.T) {
	room, v := newTalkRoom(t)
	room.SetTalkEnabled(true)

	if err := room.SetTalking(v.ID, true); err != nil {
		t.Fatalf("SetTalking(true): %v", err)
	}
	if !v.Talking() {
		t.Fatal("viewer should be speaking")
	}
	if v.TalkState() != TalkTalking {
		t.Fatalf("want talking, got %v", v.TalkState())
	}

	if err := room.SetTalking(v.ID, false); err != nil {
		t.Fatalf("SetTalking(false): %v", err)
	}
	if v.Talking() {
		t.Fatal("viewer should be muted")
	}
	if v.TalkState() != TalkNone {
		t.Fatalf("want none, got %v", v.TalkState())
	}
}

func TestSetTalking_StopAlwaysAllowed(t *testing.T) {
	room, v := newTalkRoom(t)
	room.SetTalkEnabled(true)
	if err := room.SetTalking(v.ID, true); err != nil {
		t.Fatalf("start: %v", err)
	}

	// The publisher turns the switch off (which mutes everyone), then the viewer
	// reports its own stop. Refusing that would leave the client stuck "on".
	room.SetTalkEnabled(false)
	if err := room.SetTalking(v.ID, false); err != nil {
		t.Fatalf("stopping must always succeed, got %v", err)
	}
}

func TestSetTalking_IsIdempotent(t *testing.T) {
	room, v := newTalkRoom(t)
	room.SetTalkEnabled(true)

	for range 2 {
		if err := room.SetTalking(v.ID, true); err != nil {
			t.Fatalf("repeated start must not fail, got %v", err)
		}
	}
	if !v.Talking() {
		t.Fatal("viewer should be speaking")
	}
}

func TestSetTalkEnabled_OffMutesEveryone(t *testing.T) {
	g := NewRegistry()
	g.SetLimits(10, 0)
	room, _ := g.Create("/proj/a", "A")
	room.SetTalkEnabled(true)

	a, _ := room.AddViewer("", &fakeSink{})
	b, _ := room.AddViewer("", &fakeSink{})
	_ = room.SetTalking(a.ID, true)
	_ = room.SetTalking(b.ID, true)

	room.SetTalkEnabled(false)

	// Leaving a microphone live after the publisher disabled the feature would
	// keep sending audio the publisher believes is off.
	if a.Talking() || b.Talking() {
		t.Fatalf("disabling must mute everyone: a=%v b=%v", a.Talking(), b.Talking())
	}
}

func TestSetTalkEnabled_OffThenOnDoesNotRestoreSpeech(t *testing.T) {
	room, v := newTalkRoom(t)
	room.SetTalkEnabled(true)
	_ = room.SetTalking(v.ID, true)

	room.SetTalkEnabled(false)
	room.SetTalkEnabled(true)

	// Re-enabling must not silently resume someone's microphone: they have to
	// turn it on again, which is what the viewer UI shows.
	if v.Talking() {
		t.Fatal("re-enabling the room must not resume speech by itself")
	}
}

func TestMuteAll_ReturnsOnlySpeakingViewers(t *testing.T) {
	g := NewRegistry()
	g.SetLimits(10, 0)
	room, _ := g.Create("/proj/a", "A")
	room.SetTalkEnabled(true)

	silent, _ := room.AddViewer("", &fakeSink{})
	talking, _ := room.AddViewer("", &fakeSink{})
	_ = room.SetTalking(talking.ID, true)

	affected := room.MuteAll()

	// Only the speaking viewer should be notified; a silent one has nothing to
	// be told.
	if len(affected) != 1 || affected[0] != talking.ID {
		t.Fatalf("want exactly [%s], got %v", talking.ID, affected)
	}
	if talking.Talking() {
		t.Fatal("viewer must be muted")
	}
	if silent.Talking() {
		t.Fatal("silent viewer must stay silent")
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

	if err := room.SetTalking("nope", true); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("want ErrRoomNotFound, got %v", err)
	}
	if err := room.SetTalking("nope", false); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("want ErrRoomNotFound, got %v", err)
	}
}

func TestTalkState_String(t *testing.T) {
	cases := map[TalkState]string{
		TalkNone:      "none",
		TalkTalking:   "talking",
		TalkState(99): "unknown",
	}
	for st, want := range cases {
		if got := st.String(); got != want {
			t.Fatalf("TalkState(%d).String() = %q, want %q", int(st), got, want)
		}
	}
}
