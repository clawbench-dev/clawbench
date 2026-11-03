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

// closingSink records what it was sent and whether the transport was hung up.
// `kick` is only a removal if BOTH happen, so the test sink has to expose the
// same Close the websocket sink does.
type closingSink struct {
	fakeSink
	closed bool
}

func (c *closingSink) Close() error {
	c.closed = true
	return nil
}

func TestSetMuted_BlocksTalkingUntilUnmuted(t *testing.T) {
	room, v := newTalkRoom(t)
	room.SetTalkEnabled(true)

	if _, err := room.SetMuted(v.ID, true); err != nil {
		t.Fatalf("SetMuted: %v", err)
	}
	// The room switch is still on, so the ONLY thing refusing this is the
	// per-viewer block — the client has to be able to tell those apart.
	if err := room.SetTalking(v.ID, true); !errors.Is(err, ErrTalkMuted) {
		t.Fatalf("want ErrTalkMuted, got %v", err)
	}
	if v.Talking() || !v.Muted() {
		t.Fatalf("viewer must be muted and not speaking, got talking=%v muted=%v", v.Talking(), v.Muted())
	}

	if _, err := room.SetMuted(v.ID, false); err != nil {
		t.Fatalf("SetMuted(false): %v", err)
	}
	if err := room.SetTalking(v.ID, true); err != nil {
		t.Fatalf("after unmute the viewer may speak again, got %v", err)
	}
}

func TestSetMuted_TurnsOffCurrentSpeech(t *testing.T) {
	room, v := newTalkRoom(t)
	room.SetTalkEnabled(true)
	if err := room.SetTalking(v.ID, true); err != nil {
		t.Fatalf("start: %v", err)
	}

	wasTalking, err := room.SetMuted(v.ID, true)
	if err != nil {
		t.Fatalf("SetMuted: %v", err)
	}
	// Leaving the microphone flag set would keep audio flowing while the
	// publisher's list says "muted".
	if !wasTalking {
		t.Fatal("muting a speaking viewer must report that it was talking")
	}
	if v.Talking() {
		t.Fatal("muting must also switch the microphone off")
	}
}

func TestSetMuted_UnknownViewerIsRoomNotFound(t *testing.T) {
	room, _ := newTalkRoom(t)
	if _, err := room.SetMuted("nope", true); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("want ErrRoomNotFound, got %v", err)
	}
}

func TestKickViewer_RemovesSendsAndCloses(t *testing.T) {
	g := NewRegistry()
	g.SetLimits(10, 0)
	room, _ := g.Create("/proj/a", "A")

	sink := &closingSink{}
	v, err := room.AddViewer("小明", sink)
	if err != nil {
		t.Fatalf("AddViewer: %v", err)
	}
	room.SetScreenEnabled(true)
	if err := room.ClaimScreen(v.ID); err != nil {
		t.Fatalf("ClaimScreen: %v", err)
	}

	wasSharing := room.KickViewer(v.ID)
	if !wasSharing {
		t.Fatal("kick must report that the removed viewer was sharing a screen")
	}
	if room.Viewer(v.ID) != nil || room.ViewerCount() != 0 {
		t.Fatal("kicked viewer must be gone from the room")
	}
	if room.ScreenSharer() != "" {
		t.Fatalf("a kicked sharer must release the screen, got %q", room.ScreenSharer())
	}
	// The page has to be able to say WHY it was disconnected, and the socket has
	// to actually go away — otherwise the viewer keeps sending frames under an
	// id the room has forgotten.
	if !sink.closed {
		t.Fatal("kick must close the transport")
	}
	if sink.count() == 0 {
		t.Fatal("kick must send the notice before closing")
	}
	if got, ok := sink.msgs[len(sink.msgs)-1].(ViewerMessage); !ok || got.Type != MsgKicked {
		t.Fatalf("want a kicked frame, got %#v", sink.msgs[len(sink.msgs)-1])
	}
}

func TestKickViewer_UnknownIdIsNoop(t *testing.T) {
	room, _ := newTalkRoom(t)
	if room.KickViewer("nope") {
		t.Fatal("unknown id must not report a screen to release")
	}
}
