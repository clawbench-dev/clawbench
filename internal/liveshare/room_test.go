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
	// The retitled call must update the title: the publisher may rename while
	// the room is live, and a stale title would show on the share page.
	if got := second.Title(); got != "屏幕 A（重命名）" {
		t.Fatalf("title not updated: %q", got)
	}
	if g.Len() != 1 {
		t.Fatalf("want 1 room, got %d", g.Len())
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
