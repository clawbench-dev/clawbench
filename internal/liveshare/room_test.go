package liveshare

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeSink records everything sent to it, so tests can assert on delivery.
type fakeSink struct {
	mu   sync.Mutex
	msgs []any
	err  error
}

func (f *fakeSink) Send(msg any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.msgs = append(f.msgs, msg)
	return nil
}

func (f *fakeSink) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.msgs)
}

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	g := NewRegistry()
	t.Cleanup(func() { g.SetLimits(0, 0) })
	return g
}

func TestRegistry_CreateIsIdempotentPerProject(t *testing.T) {
	g := newTestRegistry(t)

	first, err := g.Create("/proj/a", "屏幕 A")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	second, err := g.Create("/proj/a", "屏幕 A（重命名）")
	if err != nil {
		t.Fatalf("Create again: %v", err)
	}

	if first.Token() != second.Token() {
		t.Fatalf("token must be reused: got %q then %q", first.Token(), second.Token())
	}
	if first != second {
		t.Fatal("second Create must return the same room instance")
	}
	// Nobody is publishing yet (the create call runs before the publisher socket
	// attaches), so the room still takes the new title: that is the window where
	// a retry legitimately carries a corrected name.
	if got := second.Title(); got != "屏幕 A（重命名）" {
		t.Fatalf("title not updated while the room is idle: %q", got)
	}
	if g.Len() != 1 {
		t.Fatalf("want 1 room, got %d", g.Len())
	}
}

// A room that is already publishing keeps its title. The create endpoint is
// idempotent per project, so a retry — or a second device whose form has no
// title at all, which falls back to a DATE PLACEHOLDER — would otherwise rename
// the meeting under everybody who is watching it.
func TestRegistry_CreateKeepsTheTitleOfALiveRoom(t *testing.T) {
	g := newTestRegistry(t)

	room, err := g.Create("/proj/a", "发版评审")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	room.SetPublisher(&fakeSink{})

	again, err := g.Create("/proj/a", "11-03 01:12")
	if err != nil {
		t.Fatalf("Create again: %v", err)
	}
	if again != room {
		t.Fatal("the live room must be reused, not replaced")
	}
	if got := room.Title(); got != "发版评审" {
		t.Fatalf("a live room must keep its title, got %q", got)
	}

	// Once the publisher is gone the room is a leftover and may be renamed: the
	// next meeting starts with whatever the create form says.
	room.SetPublisher(nil)
	if _, err := g.Create("/proj/a", "下一次"); err != nil {
		t.Fatalf("Create after detach: %v", err)
	}
	if got := room.Title(); got != "下一次" {
		t.Fatalf("an idle room must take the new title, got %q", got)
	}
}

func TestRegistry_DistinctProjectsGetDistinctRooms(t *testing.T) {
	g := newTestRegistry(t)

	a, _ := g.Create("/proj/a", "A")
	b, _ := g.Create("/proj/b", "B")

	if a.Token() == b.Token() {
		t.Fatal("different projects must not share a token")
	}
	if g.Len() != 2 {
		t.Fatalf("want 2 rooms, got %d", g.Len())
	}
}

func TestRegistry_TokenIs32LowerHex(t *testing.T) {
	g := newTestRegistry(t)
	room, err := g.Create("/proj/a", "A")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tok := room.Token()
	if len(tok) != 32 {
		t.Fatalf("want 32 chars, got %d (%q)", len(tok), tok)
	}
	for _, r := range tok {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			t.Fatalf("non-lowercase-hex rune %q in %q", r, tok)
		}
	}
}

func TestRegistry_DeleteRemovesRoomAndProjectIndex(t *testing.T) {
	g := newTestRegistry(t)
	room, _ := g.Create("/proj/a", "A")
	tok := room.Token()

	g.Delete(tok)
	if g.Get(tok) != nil {
		t.Fatal("room must be gone after Delete")
	}
	if g.Len() != 0 {
		t.Fatalf("want 0 rooms, got %d", g.Len())
	}

	// The project index must be cleared too, otherwise the next Create for the
	// same project would look up a dead token and (in the bug this guards
	// against) hand back nil.
	again, err := g.Create("/proj/a", "A")
	if err != nil {
		t.Fatalf("Create after Delete: %v", err)
	}
	if again.Token() == tok {
		t.Fatal("recreated room should get a fresh token")
	}
}

func TestRegistry_DeleteUnknownTokenIsNoop(t *testing.T) {
	g := newTestRegistry(t)
	g.Delete("nope") // must not panic
	if g.Len() != 0 {
		t.Fatal("no rooms expected")
	}
}

// A room can be deleted by somebody other than its publisher: the host ends the
// meeting from a second device, or a stale room is rotated out. Detaching the
// sink alone left that publisher page showing "sharing" forever, because nothing
// ever closed its socket and its teardown only runs on close.
func TestRegistryDelete_HangsUpThePublisher(t *testing.T) {
	g := newTestRegistry(t)
	room, err := g.Create("/proj/a", "A")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	pub := &closingSink{}
	room.SetPublisher(pub)

	g.Delete(room.Token())

	if !pub.closed {
		t.Fatal("Delete must close the publisher socket so its page converges")
	}
	if room.HasPublisher() {
		t.Fatal("a deleted room must not keep a publisher")
	}
}

// A reservation is a socket that has not finished upgrading. Deleting the room
// must drop it too, otherwise the room nobody can look up keeps a claim.
func TestRegistryDelete_DropsAPendingReservation(t *testing.T) {
	g := newTestRegistry(t)
	room, err := g.Create("/proj/a", "A")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !room.ReservePublisher() {
		t.Fatal("ReservePublisher")
	}

	g.Delete(room.Token())

	if room.HasPublisher() {
		t.Fatal("a deleted room must not keep a reservation")
	}
}

// Deleting a publisher-less room must stay a harmless no-op: that is the path
// createLiveShareRoom takes when it rotates a stale token out.
func TestRegistryDelete_RoomWithoutPublisherIsFine(t *testing.T) {
	g := newTestRegistry(t)
	room, err := g.Create("/proj/a", "A")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	g.Delete(room.Token()) // must not panic or block
	if g.Get(room.Token()) != nil {
		t.Fatal("room must be gone")
	}
}

func TestRoom_AddViewerRespectsCap(t *testing.T) {
	g := newTestRegistry(t)
	g.SetLimits(2, time.Hour)
	room, _ := g.Create("/proj/a", "A")

	for i := range 2 {
		if _, err := room.AddViewer("", &fakeSink{}); err != nil {
			t.Fatalf("viewer %d: %v", i, err)
		}
	}
	if _, err := room.AddViewer("", &fakeSink{}); !errors.Is(err, ErrRoomFull) {
		t.Fatalf("want ErrRoomFull, got %v", err)
	}
	if room.ViewerCount() != 2 {
		t.Fatalf("want 2 viewers, got %d", room.ViewerCount())
	}
}

func TestRoom_RemoveViewerIsIdempotent(t *testing.T) {
	g := newTestRegistry(t)
	room, _ := g.Create("/proj/a", "A")
	v, _ := room.AddViewer("", &fakeSink{})

	room.RemoveViewer(v.ID)
	room.RemoveViewer(v.ID) // double teardown must not panic or corrupt state
	if room.ViewerCount() != 0 {
		t.Fatalf("want 0 viewers, got %d", room.ViewerCount())
	}
}

func TestRoom_ViewerIDsAreUniqueWithinRoom(t *testing.T) {
	g := newTestRegistry(t)
	g.SetLimits(10, time.Hour)
	room, _ := g.Create("/proj/a", "A")

	seen := map[string]bool{}
	for range 5 {
		v, err := room.AddViewer("", &fakeSink{})
		if err != nil {
			t.Fatalf("AddViewer: %v", err)
		}
		if seen[v.ID] {
			t.Fatalf("duplicate viewer id %q", v.ID)
		}
		seen[v.ID] = true
	}
}

func TestRoom_BroadcastSkipsRequestedViewer(t *testing.T) {
	g := newTestRegistry(t)
	g.SetLimits(10, time.Hour)
	room, _ := g.Create("/proj/a", "A")

	a, _ := room.AddViewer("", &fakeSink{})
	b, _ := room.AddViewer("", &fakeSink{})
	c, _ := room.AddViewer("", &fakeSink{})
	sa := a.sink.(*fakeSink)
	sb := b.sink.(*fakeSink)
	sc := c.sink.(*fakeSink)

	room.BroadcastToViewers("hello", a.ID)

	if sa.count() != 0 {
		t.Fatalf("skipped viewer must receive nothing, got %d", sa.count())
	}
	if sb.count() != 1 || sc.count() != 1 {
		t.Fatalf("others must each receive 1: b=%d c=%d", sb.count(), sc.count())
	}
}

func TestRoom_BroadcastSurvivesFailingSink(t *testing.T) {
	g := newTestRegistry(t)
	room, _ := g.Create("/proj/a", "A")

	bad := &fakeSink{err: ErrRoomFull}
	good := &fakeSink{}
	_, _ = room.AddViewer("", bad)
	_, _ = room.AddViewer("", good)

	// A sink that errors must not stop delivery to the rest.
	room.BroadcastToViewers("x", "")

	if good.count() != 1 {
		t.Fatalf("healthy viewer must still receive the frame, got %d", good.count())
	}
}

func TestRoom_IdleIgnoresRoomsWithViewers(t *testing.T) {
	g := newTestRegistry(t)
	g.SetLimits(10, time.Minute)
	room, _ := g.Create("/proj/a", "A")
	_, _ = room.AddViewer("", &fakeSink{})

	if room.Idle(time.Now().Add(2 * time.Hour)) {
		t.Fatal("a room with viewers must never be considered idle")
	}
}

func TestRoom_IdleAfterTimeoutWithNoViewers(t *testing.T) {
	g := newTestRegistry(t)
	g.SetLimits(10, time.Minute)
	room, _ := g.Create("/proj/a", "A")

	if room.Idle(time.Now().Add(30 * time.Second)) {
		t.Fatal("must not be idle before the timeout")
	}
	if !room.Idle(time.Now().Add(2 * time.Minute)) {
		t.Fatal("must be idle after the timeout")
	}
}

func TestRoom_IdleIgnoresRoomsWithPublisher(t *testing.T) {
	g := newTestRegistry(t)
	g.SetLimits(10, time.Minute)
	room, _ := g.Create("/proj/a", "A")
	room.SetPublisher(&fakeSink{})

	// A room with a live publisher is an ONGOING share — it may simply have no
	// audience yet. Reaping it would 404 the link while the UI still says
	// "sharing", so the publisher check must veto idleness.
	if room.Idle(time.Now().Add(2 * time.Hour)) {
		t.Fatal("a room with a publisher must never be considered idle")
	}
}

func TestRegistry_ReapKeepsRoomsWithPublisher(t *testing.T) {
	g := newTestRegistry(t)
	g.SetLimits(10, time.Minute)

	live, _ := g.Create("/proj/live", "live")
	live.SetPublisher(&fakeSink{})
	orphan, _ := g.Create("/proj/orphan", "orphan")

	dead := g.Reap(time.Now().Add(2 * time.Minute))

	// Only the room with no publisher AND no viewers is reapable.
	if len(dead) != 1 || dead[0].Token() != orphan.Token() {
		t.Fatalf("want only the orphan reaped, got %d", len(dead))
	}
	if g.Get(live.Token()) == nil {
		t.Fatal("room with a publisher must survive")
	}
}

func TestRegistry_ReapDropsOnlyIdleRooms(t *testing.T) {
	g := newTestRegistry(t)
	g.SetLimits(10, time.Minute)

	idle, _ := g.Create("/proj/idle", "idle")
	busy, _ := g.Create("/proj/busy", "busy")
	_, _ = busy.AddViewer("", &fakeSink{})

	dead := g.Reap(time.Now().Add(2 * time.Minute))

	if len(dead) != 1 || dead[0].Token() != idle.Token() {
		t.Fatalf("want only the idle room reaped, got %d", len(dead))
	}
	if g.Get(idle.Token()) != nil {
		t.Fatal("idle room must be gone")
	}
	if g.Get(busy.Token()) == nil {
		t.Fatal("busy room must survive")
	}
}

func TestRegistry_ReapClearsProjectIndex(t *testing.T) {
	g := newTestRegistry(t)
	g.SetLimits(10, time.Minute)
	room, _ := g.Create("/proj/a", "A")
	g.Reap(time.Now().Add(2 * time.Minute))

	// After reaping, Create must mint a NEW token rather than reviving the dead
	// one — a viewer holding the old link must not silently reconnect to a room
	// the publisher no longer owns.
	again, err := g.Create("/proj/a", "A")
	if err != nil {
		t.Fatalf("Create after reap: %v", err)
	}
	if again.Token() == room.Token() {
		t.Fatal("reaped room's token must not be reused")
	}
}

func TestRegistry_ConcurrentCreateSameProjectYieldsOneRoom(t *testing.T) {
	g := newTestRegistry(t)

	const n = 20
	var wg sync.WaitGroup
	tokens := make([]string, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			room, err := g.Create("/proj/race", "A")
			if err != nil {
				t.Errorf("Create: %v", err)
				return
			}
			tokens[i] = room.Token()
		}(i)
	}
	wg.Wait()

	for i := 1; i < n; i++ {
		if tokens[i] != tokens[0] {
			t.Fatalf("token %d = %q, want %q (all concurrent Creates must agree)", i, tokens[i], tokens[0])
		}
	}
	if g.Len() != 1 {
		t.Fatalf("want 1 room, got %d", g.Len())
	}
}

func TestRegistry_SetPublisherAndSend(t *testing.T) {
	g := newTestRegistry(t)
	room, _ := g.Create("/proj/a", "A")

	if room.SendToPublisher("x") {
		t.Fatal("SendToPublisher must report false when no publisher is attached")
	}

	pub := &fakeSink{}
	room.SetPublisher(pub)
	if !room.SendToPublisher("hello") {
		t.Fatal("SendToPublisher must report true when attached")
	}
	if pub.count() != 1 {
		t.Fatalf("publisher got %d messages, want 1", pub.count())
	}

	room.SetPublisher(nil)
	if room.Publisher() != nil {
		t.Fatal("publisher must be nil after detach")
	}
}

func TestRoom_SendToViewerUnknownID(t *testing.T) {
	g := newTestRegistry(t)
	room, _ := g.Create("/proj/a", "A")

	if room.SendToViewer("nope", "x") {
		t.Fatal("sending to an unknown viewer must report false")
	}
}

func TestRoom_ViewersSnapshotIsACopy(t *testing.T) {
	g := newTestRegistry(t)
	g.SetLimits(10, time.Hour)
	room, _ := g.Create("/proj/a", "A")
	_, _ = room.AddViewer("", &fakeSink{})

	snap := room.Viewers()
	_, _ = room.AddViewer("", &fakeSink{})

	if len(snap) != 1 {
		t.Fatalf("snapshot must not observe later additions, got %d", len(snap))
	}
	if room.ViewerCount() != 2 {
		t.Fatalf("room must have 2 viewers, got %d", room.ViewerCount())
	}
}

func TestSetTalkEnabledOffMutesSpeakingViewer(t *testing.T) {
	g := newTestRegistry(t)
	room, _ := g.Create("/proj/a", "A")
	v, _ := room.AddViewer("", &fakeSink{})

	room.SetTalkEnabled(true)
	if err := room.SetTalking(v.ID, true); err != nil {
		t.Fatalf("SetTalking: %v", err)
	}
	if !v.Talking() {
		t.Fatal("viewer should be speaking")
	}

	room.SetTalkEnabled(false)

	if v.Talking() {
		t.Fatal("disabling audience speech must stop speech in progress")
	}
}

func TestReservePublisher_BlocksSecondAndHidesFromPublisher(t *testing.T) {
	g := newTestRegistry(t)
	room, _ := g.Create("/proj/a", "A")

	if !room.ReservePublisher() {
		t.Fatal("first reservation must succeed")
	}
	// A second publisher must be refused BEFORE the upgrade, which is why the
	// reservation exists.
	if room.ReservePublisher() {
		t.Fatal("second reservation must fail")
	}
	// But Publisher() must stay nil: a viewer that attached now would be told
	// `joined` and then wait forever for an answer that cannot come.
	if room.Publisher() != nil {
		t.Fatal("a reservation must not look like an attached publisher")
	}
}

func TestReservePublisher_ReleaseAllowsAnotherAttempt(t *testing.T) {
	g := newTestRegistry(t)
	room, _ := g.Create("/proj/a", "A")

	_ = room.ReservePublisher()
	room.ReleaseReservation() // the upgrade failed

	if !room.ReservePublisher() {
		t.Fatal("a released reservation must be reusable")
	}
}

func TestAttachPublisher_ClearsReservation(t *testing.T) {
	g := newTestRegistry(t)
	room, _ := g.Create("/proj/a", "A")
	_ = room.ReservePublisher()

	sink := &fakeSink{}
	room.AttachPublisher(sink)

	if room.Publisher() != sink {
		t.Fatal("publisher must be the attached sink")
	}
	// The reservation must be gone, otherwise a later ReleaseReservation would
	// not matter but a second ReservePublisher would wrongly fail forever.
	if room.ReservePublisher() {
		t.Fatal("an attached publisher must still block a new reservation")
	}
}

func TestAttachPublisher_NotVisibleAsReservationAfterRelease(t *testing.T) {
	g := newTestRegistry(t)
	room, _ := g.Create("/proj/a", "A")
	sink := &fakeSink{}
	room.AttachPublisher(sink)

	// ReleaseReservation only clears the reservation flag; it must never detach
	// a real publisher.
	room.ReleaseReservation()

	if room.Publisher() != sink {
		t.Fatal("ReleaseReservation must not detach an attached publisher")
	}
}

// ── Audience screen sharing ─────────────────────────────────────────────────

func TestScreenShare_RequiresTheRoomSwitch(t *testing.T) {
	g := newTestRegistry(t)
	room, err := g.Create("/p", "t")
	if err != nil {
		t.Fatal(err)
	}
	v, err := room.AddViewer("", &fakeSink{})
	if err != nil {
		t.Fatal(err)
	}

	if err := room.ClaimScreen(v.ID); !errors.Is(err, ErrScreenNotAllowed) {
		t.Fatalf("claim with the switch off: got %v, want ErrScreenNotAllowed", err)
	}

	room.SetScreenEnabled(true)
	if err := room.ClaimScreen(v.ID); err != nil {
		t.Fatalf("claim after enabling: %v", err)
	}
	if got := room.ScreenSharer(); got != v.ID {
		t.Fatalf("sharer = %q, want %q", got, v.ID)
	}
}

func TestScreenShare_OnlyOneSharerAtATime(t *testing.T) {
	g := newTestRegistry(t)
	room, _ := g.Create("/p", "t")
	room.SetScreenEnabled(true)
	a, _ := room.AddViewer("", &fakeSink{})
	b, _ := room.AddViewer("", &fakeSink{})

	if err := room.ClaimScreen(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := room.ClaimScreen(b.ID); !errors.Is(err, ErrScreenBusy) {
		t.Fatalf("second claim: got %v, want ErrScreenBusy", err)
	}
	// Re-claiming by the current sharer is idempotent: a client that resends
	// screen-start (a retry) must not be told it is busy with itself.
	if err := room.ClaimScreen(a.ID); err != nil {
		t.Fatalf("re-claim by the sharer: %v", err)
	}
}

func TestScreenShare_DisablingStopsTheSharer(t *testing.T) {
	g := newTestRegistry(t)
	room, _ := g.Create("/p", "t")
	room.SetScreenEnabled(true)
	a, _ := room.AddViewer("", &fakeSink{})
	if err := room.ClaimScreen(a.ID); err != nil {
		t.Fatal(err)
	}

	stopped := room.SetScreenEnabled(false)
	if stopped != a.ID {
		t.Fatalf("stopped = %q, want %q", stopped, a.ID)
	}
	if got := room.ScreenSharer(); got != "" {
		t.Fatalf("sharer should be cleared, got %q", got)
	}
	if err := room.ClaimScreen(a.ID); !errors.Is(err, ErrScreenNotAllowed) {
		t.Fatalf("claim after disabling: got %v, want ErrScreenNotAllowed", err)
	}
}

func TestScreenShare_ReleaseAndDisconnect(t *testing.T) {
	g := newTestRegistry(t)
	room, _ := g.Create("/p", "t")
	room.SetScreenEnabled(true)
	a, _ := room.AddViewer("", &fakeSink{})
	b, _ := room.AddViewer("", &fakeSink{})
	if err := room.ClaimScreen(a.ID); err != nil {
		t.Fatal(err)
	}

	// A non-sharer cannot release somebody else's share.
	if room.ReleaseScreen(b.ID) {
		t.Fatal("non-sharer released the share")
	}
	if !room.ReleaseScreen(a.ID) {
		t.Fatal("sharer could not release")
	}
	if room.ReleaseScreen(a.ID) {
		t.Fatal("double release must be a no-op")
	}

	// Closing the tab while sharing must free the slot, otherwise the room is
	// "busy" forever and nobody can ever share again.
	if err := room.ClaimScreen(a.ID); err != nil {
		t.Fatal(err)
	}
	room.RemoveViewer(a.ID)
	if got := room.ScreenSharer(); got != "" {
		t.Fatalf("disconnect left a phantom sharer %q", got)
	}
	if err := room.ClaimScreen(b.ID); err != nil {
		t.Fatalf("claim after the sharer left: %v", err)
	}
}

func TestRoom_ScreenSwitchGettersAndSetters(t *testing.T) {
	g := newTestRegistry(t)
	room, _ := g.Create("/proj/a", "A")

	if room.ScreenEnabled() {
		t.Fatal("screen sharing must start disabled")
	}
	room.SetScreenEnabled(true)
	if !room.ScreenEnabled() {
		t.Fatal("SetScreenEnabled(true) must be observable")
	}
}

// Remote control starts UNAVAILABLE: a room nobody advertised for must not let a
// viewer offer a button whose request the publisher could only refuse.
func TestRoom_ControlEnabledStartsOffAndFollowsThePublisher(t *testing.T) {
	g := newTestRegistry(t)
	room, _ := g.Create("/proj/a", "A")

	if room.ControlEnabled() {
		t.Fatal("remote control must start unavailable")
	}
	room.SetControlEnabled(true)
	if !room.ControlEnabled() {
		t.Fatal("SetControlEnabled(true) must be observable")
	}
	room.SetControlEnabled(false)
	if room.ControlEnabled() {
		t.Fatal("SetControlEnabled(false) must be observable")
	}
}

// Turning the switch off must report the viewer that was sharing so the caller
// can send it an explicit denial — the browser keeps capturing otherwise.
func TestRoom_SetScreenEnabledOffStopsSharer(t *testing.T) {
	g := newTestRegistry(t)
	room, _ := g.Create("/proj/a", "A")
	v, err := room.AddViewer("", &fakeSink{})
	if err != nil {
		t.Fatalf("AddViewer: %v", err)
	}
	room.SetScreenEnabled(true)
	if err := room.ClaimScreen(v.ID); err != nil {
		t.Fatalf("ClaimScreen: %v", err)
	}
	if room.ScreenSharer() != v.ID {
		t.Fatalf("want sharer %q, got %q", v.ID, room.ScreenSharer())
	}

	if stopped := room.SetScreenEnabled(false); stopped != v.ID {
		t.Fatalf("want stopped=%q, got %q", v.ID, stopped)
	}
	if room.ScreenSharer() != "" {
		t.Fatalf("sharer must be cleared, got %q", room.ScreenSharer())
	}
	if stopped := room.SetScreenEnabled(false); stopped != "" {
		t.Fatalf("nothing to stop, got %q", stopped)
	}
}

// A viewer that is not in the room cannot claim the screen: it would never
// receive the stream and nobody could revoke it.
func TestRoom_ClaimScreenRejectsUnknownViewer(t *testing.T) {
	g := newTestRegistry(t)
	room, _ := g.Create("/proj/a", "A")
	room.SetScreenEnabled(true)

	if err := room.ClaimScreen("ghost"); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("want ErrRoomNotFound, got %v", err)
	}
	if room.ScreenSharer() != "" {
		t.Fatal("a rejected claim must not set the sharer")
	}
}

func TestRoom_ReleaseScreenUnknownSharerIsNoop(t *testing.T) {
	g := newTestRegistry(t)
	room, _ := g.Create("/proj/a", "A")
	if room.ReleaseScreen("ghost") {
		t.Fatal("releasing a non-sharer must report false")
	}
}

// The camera and host-screen flags say WHAT the picture is made of, so they are
// independent of each other and of the video flag. A reused room must take the
// new meeting's values, not the previous meeting's.
func TestRoom_CameraAndHostScreenFlags(t *testing.T) {
	g := newTestRegistry(t)
	room, _ := g.Create("/proj/a", "A")

	// Defaults mirror the pre-video-meeting world: the picture was a screen and
	// there was no camera.
	if !room.HostScreen() {
		t.Fatal("a legacy room's picture must be a screen")
	}
	if room.CameraEnabled() {
		t.Fatal("a legacy room must not claim a camera")
	}

	room.SetCameraEnabled(true)
	room.SetHostScreen(false)
	if !room.CameraEnabled() {
		t.Fatal("SetCameraEnabled(true) must be observable")
	}
	if room.HostScreen() {
		t.Fatal("SetHostScreen(false) must be observable")
	}

	// Both off is a real state: a video meeting whose camera was switched off
	// and which shares no screen. The video flag is what says there is no
	// picture, not these two.
	room.SetCameraEnabled(false)
	if room.CameraEnabled() {
		t.Fatal("SetCameraEnabled(false) must be observable")
	}
}

func TestRoom_NotesSwitch(t *testing.T) {
	g := newTestRegistry(t)
	room, _ := g.Create("/proj/a", "A")

	if room.NotesEnabled() {
		t.Fatal("notes must start disabled")
	}
	room.SetNotesEnabled(true)
	if !room.NotesEnabled() {
		t.Fatal("SetNotesEnabled(true) must be observable")
	}
	room.SetNotesEnabled(false)
	if room.NotesEnabled() {
		t.Fatal("SetNotesEnabled(false) must be observable")
	}
}

// HasPublisher is what tells a stale room (publisher crashed, idle reaper has
// not run yet) from a live one; a reservation counts, because the publisher is
// already on its way.
func TestRoom_HasPublisherAndCreatedAt(t *testing.T) {
	g := newTestRegistry(t)
	room, err := g.Create("/proj/a", "A")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if room.HasPublisher() {
		t.Fatal("a fresh room has no publisher")
	}
	if room.CreatedAt().IsZero() {
		t.Fatal("CreatedAt must be stamped at creation")
	}

	if !room.ReservePublisher() {
		t.Fatal("ReservePublisher must succeed on an idle room")
	}
	if !room.HasPublisher() {
		t.Fatal("a reservation counts as a publisher on its way")
	}
	room.ReleaseReservation()
	if room.HasPublisher() {
		t.Fatal("a released reservation is not a publisher")
	}

	room.SetPublisher(&fakeSink{})
	if !room.HasPublisher() {
		t.Fatal("an attached publisher must be reported")
	}
	room.SetPublisher(nil)
	if room.HasPublisher() {
		t.Fatal("a detached publisher must be reported as gone")
	}
}

func TestRoster_PublisherFirstThenJoinOrder(t *testing.T) {
	g := NewRegistry()
	g.SetLimits(10, 0)
	room, err := g.Create("/proj/a", "A")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !room.ReservePublisher() {
		t.Fatal("ReservePublisher")
	}
	room.AttachPublisher(&fakeSink{})
	room.SetPublisherName("泷泽")

	first, err := room.AddViewer("小明", &fakeSink{})
	if err != nil {
		t.Fatalf("AddViewer: %v", err)
	}
	second, err := room.AddViewer("小红", &fakeSink{})
	if err != nil {
		t.Fatalf("AddViewer: %v", err)
	}

	roster := room.Roster()
	if len(roster) != 3 {
		t.Fatalf("want host + 2 viewers, got %#v", roster)
	}
	// The host is pinned: the audience's list reads as "who is in this meeting",
	// not "who is watching me".
	if !roster[0].Host || roster[0].ID != "host" || roster[0].Name != "泷泽" {
		t.Fatalf("host must be first with its nickname, got %#v", roster[0])
	}
	// Join order, not map order: a list that reshuffles every frame is unusable.
	if roster[1].ID != first.ID || roster[2].ID != second.ID {
		t.Fatalf("viewers must be in join order, got %q then %q", roster[1].ID, roster[2].ID)
	}
	if roster[1].Name != "小明" || roster[2].Name != "小红" {
		t.Fatalf("names must survive, got %#v", roster[1:])
	}
}

func TestRoster_KeepsJoinOrderAfterSomebodyLeaves(t *testing.T) {
	g := NewRegistry()
	g.SetLimits(10, 0)
	room, _ := g.Create("/proj/a", "A")
	room.ReservePublisher()
	room.AttachPublisher(&fakeSink{})

	first, _ := room.AddViewer("a", &fakeSink{})
	second, _ := room.AddViewer("b", &fakeSink{})
	room.RemoveViewer(first.ID)
	third, _ := room.AddViewer("c", &fakeSink{})

	roster := room.Roster()
	if len(roster) != 3 { // host + b + c
		t.Fatalf("want 3 entries, got %#v", roster)
	}
	if roster[1].ID != second.ID || roster[2].ID != third.ID {
		t.Fatalf("a gap must not reorder the survivors, got %#v", roster[1:])
	}
}

func TestRoster_ReflectsSpeechAndHostMute(t *testing.T) {
	room, v := newTalkRoom(t)
	room.SetTalkEnabled(true)
	if err := room.SetTalking(v.ID, true); err != nil {
		t.Fatalf("SetTalking: %v", err)
	}
	if _, err := room.SetMuted(v.ID, true); err != nil {
		t.Fatalf("SetMuted: %v", err)
	}

	// Muting clears the microphone, so the snapshot must not claim both.
	// newTalkRoom has no publisher, so the viewer is the only entry.
	entry := room.Roster()[0]
	if entry.Talking {
		t.Fatal("a muted viewer must not read as talking")
	}
	if !entry.Muted {
		t.Fatal("the block must be visible in the roster")
	}
}

func TestRoster_EmptyRoomHasNoHost(t *testing.T) {
	g := NewRegistry()
	room, _ := g.Create("/proj/a", "A")

	// No publisher attached: a host row would be a phantom.
	if got := room.Roster(); len(got) != 0 {
		t.Fatalf("want an empty roster, got %#v", got)
	}
}

// A name identifies one person: the roster, the meeting chat and the transcript
// all attribute lines by name, so two "小明" rows would merge two people into
// one speaker. The room refuses the second one instead of accepting an
// ambiguous meeting.
func TestAddViewer_RefusesNameAlreadyInTheRoom(t *testing.T) {
	g := NewRegistry()
	room, _ := g.Create("/proj/a", "A")

	if _, err := room.AddViewer("小明", &fakeSink{}); err != nil {
		t.Fatalf("first join must succeed: %v", err)
	}
	_, err := room.AddViewer("小明", &fakeSink{})
	if !errors.Is(err, ErrViewerNameTaken) {
		t.Fatalf("want ErrViewerNameTaken, got %v", err)
	}
	if n := room.ViewerCount(); n != 1 {
		t.Fatalf("a refused viewer must not take a slot, have %d", n)
	}
}

// Case and surrounding space are not identity: "Alex", "alex" and " Alex " are
// one label to everyone reading the roster, which is the whole point of the
// rule. (Names are trimmed upstream, so this is the belt to that braces.)
func TestAddViewer_NameComparisonIgnoresCaseAndPadding(t *testing.T) {
	g := NewRegistry()
	room, _ := g.Create("/proj/a", "A")
	if _, err := room.AddViewer("Alex", &fakeSink{}); err != nil {
		t.Fatalf("AddViewer: %v", err)
	}
	for _, name := range []string{"alex", "ALEX", "  Alex  "} {
		if _, err := room.AddViewer(name, &fakeSink{}); !errors.Is(err, ErrViewerNameTaken) {
			t.Fatalf("%q must be refused, got %v", name, err)
		}
	}
}

// The host is a participant too: a viewer named like the host would make the
// audience list show two identical rows, one of them mislabelled.
func TestAddViewer_RefusesThePublisherName(t *testing.T) {
	g := NewRegistry()
	room, _ := g.Create("/proj/a", "A")
	room.ReservePublisher()
	room.AttachPublisher(&fakeSink{})
	room.SetPublisherName("泷泽")

	if _, err := room.AddViewer("泷泽", &fakeSink{}); !errors.Is(err, ErrViewerNameTaken) {
		t.Fatalf("want ErrViewerNameTaken, got %v", err)
	}
	if _, err := room.AddViewer("小明", &fakeSink{}); err != nil {
		t.Fatalf("a different name must be accepted: %v", err)
	}
}

// Leaving frees the name: a viewer that reconnects (reload, network blip) must
// not find its own previous slot blocking it forever.
func TestAddViewer_NameIsFreedWhenItsOwnerLeaves(t *testing.T) {
	g := NewRegistry()
	room, _ := g.Create("/proj/a", "A")
	first, err := room.AddViewer("小明", &fakeSink{})
	if err != nil {
		t.Fatalf("AddViewer: %v", err)
	}
	room.RemoveViewer(first.ID)

	if _, err := room.AddViewer("小明", &fakeSink{}); err != nil {
		t.Fatalf("the name must be reusable after its owner left: %v", err)
	}
}

// Two sockets dialing at once with one name: the check and the insert are one
// critical section, so exactly one of them gets in. A read-then-write check
// would let both pass and produce the duplicate this rule exists to prevent.
func TestAddViewer_ConcurrentSameNameAdmitsExactlyOne(t *testing.T) {
	g := NewRegistry()
	room, _ := g.Create("/proj/a", "A")
	g.SetLimits(10, 0)

	const attempts = 16
	var wg sync.WaitGroup
	results := make([]error, attempts)
	for i := range attempts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, results[i] = room.AddViewer("小明", &fakeSink{})
		}(i)
	}
	wg.Wait()

	ok := 0
	for _, err := range results {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrViewerNameTaken):
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("exactly one join must succeed, got %d", ok)
	}
}

// An unnamed participant is not an identity. newTalkRoom joins with "", and
// refusing the second empty name would turn a missing label into a locked room.
func TestAddViewer_EmptyNamesDoNotCollide(t *testing.T) {
	g := NewRegistry()
	room, _ := g.Create("/proj/a", "A")
	if _, err := room.AddViewer("", &fakeSink{}); err != nil {
		t.Fatalf("first unnamed join: %v", err)
	}
	if _, err := room.AddViewer("", &fakeSink{}); err != nil {
		t.Fatalf("a second unnamed join must be allowed: %v", err)
	}
}


// newTestRoom builds a room the way the registry does, with a token the test
// chooses so failures name the case.
func newTestRoom(t *testing.T, token string) *Room {
	t.Helper()
	return &Room{
		token:      token,
		projectKey: "/p",
		title:      "t",
		createdAt:  time.Now(),
		viewers:    make(map[string]*Viewer),
	}
}

// The relay mode is fixed at creation time: a second create must not rewrite it
// under a live meeting. These pin the getters/setters the SFU path reads.
func TestRoomModeLifecycle(t *testing.T) {
	r := newTestRoom(t, "tok")

	if got := r.Mode(); got != ModeP2P {
		t.Fatalf("a fresh room must be P2P, got %q", got)
	}
	if r.UseRelay() {
		t.Fatal("a P2P room must not use the relay")
	}
	if got := r.LiveKit(); got != (LiveKitConfig{}) {
		t.Fatalf("a P2P room has no relay snapshot, got %+v", got)
	}

	cfg := LiveKitConfig{URL: "wss://relay.example.com", APIURL: "https://relay.example.com", APIKey: "k", APISecret: "s"}
	if !r.SetMode(ModeSFU, cfg, 4) {
		t.Fatal("SetMode must succeed before a publisher attaches")
	}
	if got := r.Mode(); got != ModeSFU {
		t.Fatalf("mode must be SFU, got %q", got)
	}
	if !r.UseRelay() {
		t.Fatal("a configured SFU room uses the relay")
	}
	if got := r.LiveKit(); got != cfg {
		t.Fatalf("relay snapshot must be kept, got %+v", got)
	}

	// An SFU request without credentials falls back to P2P rather than
	// producing a room that claims to relay but cannot.
	r2 := newTestRoom(t, "tok2")
	if !r2.SetMode(ModeSFU, LiveKitConfig{}, 7) {
		t.Fatal("SetMode must succeed on a fresh room")
	}
	if got := r2.Mode(); got != ModeP2P {
		t.Fatalf("unconfigured relay must stay P2P, got %q", got)
	}
}

func TestRoomSetModeRefusedWithPublisher(t *testing.T) {
	r := newTestRoom(t, "tok")
	r.ReservePublisher()
	cfg := LiveKitConfig{URL: "wss://relay.example.com", APIURL: "https://relay.example.com", APIKey: "k", APISecret: "s"}
	if r.SetMode(ModeSFU, cfg, 4) {
		t.Fatal("mode must not change once a publisher is reserved")
	}
	if got := r.Mode(); got != ModeP2P {
		t.Fatalf("mode must stay P2P, got %q", got)
	}
}
