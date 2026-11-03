// Package liveshare implements the transport-agnostic core of a live
// screen/audio share room: who is publishing, who is watching, and who is
// allowed to talk.
//
// Deliberately depends on NOTHING outside the standard library. ClawBench wires
// it up in internal/handler, but the package has no knowledge of ClawBench,
// websockets, HTTP, projects or sessions — a standalone relay application could
// import it unchanged. That is why connections are represented by the Sink
// interface rather than a *websocket.Conn.
package liveshare

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
)

// Errors returned by the registry and rooms. Callers map them onto transport
// responses; they are sentinel values so that mapping stays exhaustive.
var (
	// ErrRoomExists means another publisher already owns this project's room.
	ErrRoomExists = errors.New("liveshare: room already has a publisher")
	// ErrRoomNotFound means the token names no live room.
	ErrRoomNotFound = errors.New("liveshare: room not found")
	// ErrTooManyRooms means the registry's room ceiling has been reached.
	ErrTooManyRooms = errors.New("liveshare: too many live rooms")
	// ErrRoomFull means the viewer cap has been reached.
	ErrRoomFull = errors.New("liveshare: room is full")
	// ErrViewerExists means the same viewer id was added twice.
	ErrViewerExists = errors.New("liveshare: viewer already present")
	// ErrViewerNameTaken means the display name is already in use by somebody
	// else in this room. Two participants with one name make the roster and the
	// meeting record ambiguous: every chat line, transcript segment and
	// notification is attributed by name, so "小明" would become two people.
	ErrViewerNameTaken = errors.New("liveshare: display name already in use")
	// ErrTalkNotAllowed means the room has not enabled audience speech.
	ErrTalkNotAllowed = errors.New("liveshare: talking is not enabled for this room")
	// ErrTalkMuted means the publisher blocked this viewer's microphone. It is
	// distinct from ErrTalkNotAllowed so the client can say "the host muted
	// you" instead of "the room has no speech", which is a different situation
	// and a different fix.
	ErrTalkMuted = errors.New("liveshare: viewer is muted by the publisher")
	// ErrScreenNotAllowed means the room has not enabled audience screen sharing.
	ErrScreenNotAllowed = errors.New("liveshare: screen sharing is not enabled for this room")
	// ErrScreenBusy means another viewer is already sharing a screen.
	ErrScreenBusy = errors.New("liveshare: another viewer is already sharing")
)

// Defaults chosen so a room cannot outlive the publisher's intent. The idle
// timeout is what reclaims a room whose publisher vanished without sending an
// explicit end (a killed process, a dropped network).
const (
	// DefaultMaxViewers caps concurrent watchers. Chosen from the publisher's
	// upstream budget: the publisher sends one encoded stream per viewer, so N
	// viewers cost N × bitrate upstream. See the design doc §9.3.
	DefaultMaxViewers = 8
	// DefaultIdleTimeout reclaims a room with no viewers and no activity.
	DefaultIdleTimeout = 30 * time.Minute
	// tokenBytes is the entropy of a share token. 16 bytes → 32 hex chars,
	// matching the file/session share tokens already in use.
	tokenBytes = 16
)

// Sink is the transport a participant is reachable through.
//
// Implementations must be safe for concurrent use: the room fans out to several
// sinks from whichever goroutine produced the event. Send must not block
// indefinitely — a stalled sink would otherwise stall the room.
type Sink interface {
	// Send delivers one message. The concrete type is package-private to the
	// caller (the room only forwards opaque values it was given).
	Send(msg any) error
}

// Viewer is one audience member.
//
// talking is read by the room and written only through its methods, so a caller
// must never mutate it directly.
type Viewer struct {
	// ID is assigned by the room on join and is stable for the connection.
	ID string
	// Name is an optional display label supplied by the viewer.
	Name string

	sink Sink

	// mu guards the mutable fields below. The room holds its own lock while
	// mutating them, but accessors like TalkState() are reachable from outside,
	// so the viewer needs its own.
	mu sync.Mutex

	// talking is the viewer's microphone state. There is no per-viewer grant:
	// the room-level switch is the only authorisation (see talk.go).
	talking bool

	// muted is the publisher's per-viewer block. It is NOT the same thing as
	// `talking`: `talking` is the viewer's own switch, while `muted` is a
	// decision only the publisher can undo. A muted viewer's talk-start is
	// refused, so the microphone stays off until the host unmutes it.
	muted bool
}

// Talking reports whether this viewer's microphone is on.
func (v *Viewer) Talking() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.talking
}

// Muted reports whether the publisher has blocked this viewer's microphone.
func (v *Viewer) Muted() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.muted
}

// Room is a single live share. All methods are safe for concurrent use.
type Room struct {
	mu sync.Mutex

	token      string
	projectKey string
	title      string
	createdAt  time.Time

	// publisherName is the host's nickname, from the `publish` frame. Empty is
	// a legitimate value (the client did not send one) and only affects the
	// label in the audience's member list.
	publisherName string

	publisher Sink
	// publisherReserved is set between ReservePublisher and AttachPublisher.
	// Distinct from `publisher != nil` so viewers never see a phantom publisher.
	publisherReserved bool
	viewers           map[string]*Viewer
	nextID            int

	// talkEnabled is the room-level switch. Per-viewer grants are on Viewer.
	talkEnabled bool

	// screenEnabled is the room-level switch for audience screen sharing;
	// screenSharer is the viewer currently sharing ("" when nobody is).
	//
	// One sharer at a time is a bandwidth rule, not a UX one: the publisher
	// forwards every shared screen to every other viewer, so N sharers cost N ×
	// N streams of upstream. One at a time keeps it at one per viewer.
	screenEnabled bool
	screenSharer  string

	// videoEnabled tells viewers whether the publisher is sending a picture.
	// Voice meetings keep the same room, signaling and chat; they simply have no
	// video track, and a viewer must not sit on "connecting" waiting for one.
	videoEnabled bool

	// cameraEnabled and hostScreen split "there is a picture" into the two
	// things it can be made of. They are orthogonal: a video meeting may run
	// camera + screen at once (the camera goes out as a second video track and
	// renders as a picture-in-picture tile), a screen-only share has hostScreen
	// but no camera, and a voice meeting has neither.
	//
	// hostScreen is NOT the same as screenEnabled above: that one is the
	// audience's permission to share, this one is what the publisher is showing.
	cameraEnabled bool
	hostScreen    bool

	// notesEnabled tells viewers whether the publisher is running AI 听记.
	// Purely informational: the transcript itself never passes through the room
	// (it is uploaded over HTTP), but a viewer must not show a "听记" control for
	// a share that has none.
	notesEnabled bool

	// controlEnabled tells viewers whether they may ask to drive this
	// publisher's pointer and keyboard. The publisher is the only one that can
	// answer: injection happens on the host machine (it needs the input
	// injector) and only a whole-screen capture has a desktop to map onto.
	controlEnabled bool

	maxViewers  int
	idleTimeout time.Duration
	lastActive  time.Time
}

// Token is the share token (the sole credential for viewers).
func (r *Room) Token() string { return r.token }

// ProjectKey identifies the scope this room belongs to. Opaque to this package:
// ClawBench passes a project path, a standalone relay could pass a tenant id.
func (r *Room) ProjectKey() string { return r.projectKey }

// Title is the human-readable room title shown on the share page.
func (r *Room) Title() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.title
}

// PublisherName is the host's nickname for the audience's member list.
func (r *Room) PublisherName() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.publisherName
}

// SetPublisherName records the host's nickname from the `publish` frame.
func (r *Room) SetPublisherName(name string) {
	r.mu.Lock()
	r.publisherName = name
	r.mu.Unlock()
}

// Roster returns the participant list as the audience sees it: the publisher
// first, then the viewers in join order.
//
// Join order, not map order: a viewer list that reshuffles on every frame would
// make the same meeting look different each time somebody speaks. Ids are
// allocated by nextID, so walking it reproduces the order exactly.
func (r *Room) Roster() []RosterEntry {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]RosterEntry, 0, len(r.viewers)+1)
	if r.publisher != nil {
		out = append(out, RosterEntry{ID: "host", Name: r.publisherName, Host: true})
	}
	for i := 1; i <= r.nextID; i++ {
		v := r.viewers[viewerID(i)]
		if v == nil {
			continue
		}
		v.mu.Lock()
		out = append(out, RosterEntry{
			ID:      v.ID,
			Name:    v.Name,
			Talking: v.talking,
			Muted:   v.muted,
		})
		v.mu.Unlock()
	}
	return out
}

// TalkEnabled reports whether the audience may request speech.
func (r *Room) TalkEnabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.talkEnabled
}

// ScreenEnabled reports whether audience screen sharing is allowed.
func (r *Room) ScreenEnabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.screenEnabled
}

// ScreenSharer returns the id of the viewer currently sharing, or "".
func (r *Room) ScreenSharer() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.screenSharer
}

// SetScreenEnabled flips the room-level screen-sharing switch and returns the
// viewer that was stopped by turning it off ("" when nobody was sharing).
//
// Turning it off must stop the current sharer: leaving a screen live after the
// publisher switched the feature off is the same betrayal as leaving a
// microphone live.
func (r *Room) SetScreenEnabled(on bool) (stopped string) {
	r.mu.Lock()
	r.screenEnabled = on
	if !on {
		stopped = r.screenSharer
		r.screenSharer = ""
	}
	r.mu.Unlock()
	r.touch()
	return stopped
}

// ClaimScreen makes viewerID the room's screen sharer.
//
// Errors are returned rather than swallowed: a viewer that starts capturing
// while the switch is off (or while somebody else is sharing) has to learn why
// its screen went nowhere.
func (r *Room) ClaimScreen(viewerID string) error {
	// No defer: touch() takes r.mu itself, so every path has to unlock first.
	r.mu.Lock()
	if !r.screenEnabled {
		r.mu.Unlock()
		return ErrScreenNotAllowed
	}
	if r.screenSharer != "" && r.screenSharer != viewerID {
		r.mu.Unlock()
		return ErrScreenBusy
	}
	if _, ok := r.viewers[viewerID]; !ok {
		r.mu.Unlock()
		return ErrRoomNotFound
	}
	r.screenSharer = viewerID
	r.mu.Unlock()
	r.touch()
	return nil
}

// ReleaseScreen clears viewerID's share, reporting whether it was the current
// sharer. Releasing an id that is not sharing is a no-op so that a stop frame
// racing a disconnect cannot fail.
func (r *Room) ReleaseScreen(viewerID string) bool {
	r.mu.Lock()
	if r.screenSharer == "" || r.screenSharer != viewerID {
		r.mu.Unlock()
		return false
	}
	r.screenSharer = ""
	r.mu.Unlock()
	r.touch()
	return true
}

// VideoEnabled reports whether the publisher is sending video for this room.
func (r *Room) VideoEnabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.videoEnabled
}

// SetVideoEnabled flips the video flag. Set on every create call, like the
// notes flag: a room reused inside the grace window must not inherit the
// previous meeting's mode.
func (r *Room) SetVideoEnabled(on bool) {
	r.mu.Lock()
	r.videoEnabled = on
	r.mu.Unlock()
	r.touch()
}

// CameraEnabled reports whether the publisher's camera is on.
func (r *Room) CameraEnabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cameraEnabled
}

// SetCameraEnabled flips the camera flag. Written on create and again whenever
// the publisher toggles the camera mid-meeting, so a viewer joining late sees
// the current state in its `joined` frame rather than the state at creation.
func (r *Room) SetCameraEnabled(on bool) {
	r.mu.Lock()
	r.cameraEnabled = on
	r.mu.Unlock()
	r.touch()
}

// HostScreen reports whether the publisher's picture is a screen share.
func (r *Room) HostScreen() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hostScreen
}

// SetHostScreen records whether the publisher shares a screen. Set on every
// create call for the same reason as the video flag.
func (r *Room) SetHostScreen(on bool) {
	r.mu.Lock()
	r.hostScreen = on
	r.mu.Unlock()
	r.touch()
}

// NotesEnabled reports whether the publisher is running AI 听记 for this room.
func (r *Room) NotesEnabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.notesEnabled
}

// ControlEnabled reports whether viewers may ask to drive the publisher's
// pointer and keyboard.
//
// This is a PUBLISHER capability, not a viewer one: injection happens on the
// host machine, so only the host knows whether it can accept a request (it needs
// the input injector and a whole-screen capture). Viewers grey their button on
// this flag instead of asking and being refused.
func (r *Room) ControlEnabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.controlEnabled
}

// SetControlEnabled advertises whether remote control is available right now.
// Called by the publisher whenever its capability or its picture kind changes.
func (r *Room) SetControlEnabled(on bool) {
	r.mu.Lock()
	r.controlEnabled = on
	r.mu.Unlock()
	r.touch()
}

// SetNotesEnabled flips the AI 听记 flag. Set from the create call, before the
// publisher socket attaches, and again whenever the publisher toggles 听记 while
// the meeting runs — viewers learn the change from a `notes-enabled` frame.
func (r *Room) SetNotesEnabled(on bool) {
	r.mu.Lock()
	r.notesEnabled = on
	r.mu.Unlock()
	r.touch()
}

// SetTalkEnabled flips the room-level speech switch. Turning it off also mutes
// everyone: leaving a viewer's microphone live after the publisher disabled the
// feature would keep sending audio the publisher believes is off.
func (r *Room) SetTalkEnabled(on bool) {
	r.mu.Lock()
	r.talkEnabled = on
	if !on {
		for _, v := range r.viewers {
			v.mu.Lock()
			v.talking = false
			v.mu.Unlock()
		}
	}
	r.mu.Unlock()
	r.touch()
}

// CreatedAt is when the room was minted. Used to tell "this room is being
// reused after its publisher vanished" from "the publisher is about to attach".
func (r *Room) CreatedAt() time.Time { return r.createdAt }

// HasPublisher reports whether a publisher is attached OR reserved. A room
// without one is a leftover: the publisher socket closed (crash, restart) and
// the idle reaper has not collected the room yet.
func (r *Room) HasPublisher() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.publisher != nil || r.publisherReserved
}

// Publisher returns the publisher sink, or nil when no publisher is attached.
func (r *Room) Publisher() Sink {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.publisher
}

// SetPublisher attaches (or detaches, with nil) the publisher.
func (r *Room) SetPublisher(s Sink) {
	r.mu.Lock()
	r.publisher = s
	r.mu.Unlock()
	r.touch()
}

// ReservePublisher claims the publisher slot WITHOUT attaching a transport, so a
// second publisher can be rejected with a real HTTP 409 before the WebSocket
// upgrade (once upgraded, the client cannot see a status code).
//
// The reservation is a separate flag rather than a placeholder sink on purpose:
// Publisher() must keep meaning "a real transport is attached". A placeholder
// would make viewers believe the room is live and then send them `joined` for a
// publisher that can never answer their offer.
//
// Must be followed by AttachPublisher (upgrade succeeded) or ReleaseReservation.
func (r *Room) ReservePublisher() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.publisher != nil || r.publisherReserved {
		return false
	}
	r.publisherReserved = true
	return true
}

// AttachPublisher completes a reservation with the real transport.
func (r *Room) AttachPublisher(s Sink) {
	r.mu.Lock()
	r.publisher = s
	r.publisherReserved = false
	r.mu.Unlock()
	r.touch()
}

// ReleaseReservation drops a reservation that never became a real attachment
// (the upgrade failed). It does not touch an attached publisher.
func (r *Room) ReleaseReservation() {
	r.mu.Lock()
	r.publisherReserved = false
	r.mu.Unlock()
}

// ReleasePublisher detaches s only if it is still the current publisher.
//
// Compare-and-clear: a socket that was replaced (or that lost a race) must not
// tear down the room its successor now owns.
func (r *Room) ReleasePublisher(s Sink) bool {
	r.mu.Lock()
	if r.publisher != s {
		r.mu.Unlock()
		return false
	}
	r.publisher = nil
	r.mu.Unlock()
	r.touch()
	return true
}

// ViewerCount reports how many viewers are currently attached.
func (r *Room) ViewerCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.viewers)
}

// AddViewer registers a viewer and returns its id.
//
// A display name is unique within the room, and the check happens under the
// same lock as the insert: two people dialing at once with one name must not
// both pass a read-then-write check. Comparison ignores case and surrounding
// space, because "Alex" and "alex" are the same label to everyone reading the
// roster, and the whole point of the rule is that a name identifies one person.
func (r *Room) AddViewer(name string, sink Sink) (*Viewer, error) {
	r.mu.Lock()
	if len(r.viewers) >= r.maxViewers {
		r.mu.Unlock()
		return nil, ErrRoomFull
	}
	if r.nameTakenLocked(name) {
		r.mu.Unlock()
		return nil, ErrViewerNameTaken
	}
	r.nextID++
	v := &Viewer{
		ID:   viewerID(r.nextID),
		Name: name,
		sink: sink,
	}
	r.viewers[v.ID] = v
	r.mu.Unlock()
	r.touch()
	return v, nil
}

// nameTakenLocked is the one implementation of the uniqueness rule. The caller
// must hold r.mu.
func (r *Room) nameTakenLocked(name string) bool {
	want := strings.TrimSpace(name)
	if want == "" {
		// An unnamed participant is not an identity: refusing every later empty
		// name would turn a missing label into a locked room.
		return false
	}
	if strings.EqualFold(strings.TrimSpace(r.publisherName), want) {
		return true
	}
	for _, v := range r.viewers {
		if strings.EqualFold(strings.TrimSpace(v.Name), want) {
			return true
		}
	}
	return false
}

// RemoveViewer detaches a viewer. Removing an unknown id is a no-op so that
// double-teardown (read loop + deferred cleanup) cannot fail.
func (r *Room) RemoveViewer(id string) {
	r.mu.Lock()
	delete(r.viewers, id)
	// A viewer that closes its tab mid-share must not leave the room "busy"
	// forever — nobody else could ever start sharing again.
	if r.screenSharer == id {
		r.screenSharer = ""
	}
	r.mu.Unlock()
	r.touch()
}

// KickViewer removes a viewer and asks its transport to hang up.
//
// Removal and the hang-up are one operation on purpose: leaving the socket open
// would let a removed viewer keep sending offers and control frames under an id
// the room no longer knows. The frame is sent BEFORE the transport closes so the
// client can explain why it was disconnected instead of reporting a dropped
// connection.
//
// Returns whether the viewer was sharing its screen, so the caller can tell
// everybody else that the picture is gone.
func (r *Room) KickViewer(id string) (wasSharing bool) {
	r.mu.Lock()
	v := r.viewers[id]
	if v == nil {
		r.mu.Unlock()
		return false
	}
	delete(r.viewers, id)
	wasSharing = r.screenSharer == id
	if wasSharing {
		r.screenSharer = ""
	}
	r.mu.Unlock()

	_ = v.sink.Send(ViewerMessage{Type: MsgKicked})
	if closer, ok := v.sink.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
	r.touch()
	return wasSharing
}

// Viewer returns the viewer with the given id, or nil.
func (r *Room) Viewer(id string) *Viewer {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.viewers[id]
}

// Viewers returns a snapshot of the current viewers.
func (r *Room) Viewers() []*Viewer {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Viewer, 0, len(r.viewers))
	for _, v := range r.viewers {
		out = append(out, v)
	}
	return out
}

// SendToPublisher forwards a message to the publisher, reporting whether one
// was attached.
func (r *Room) SendToPublisher(msg any) bool {
	p := r.Publisher()
	if p == nil {
		return false
	}
	_ = p.Send(msg)
	return true
}

// SendToViewer forwards a message to one viewer.
func (r *Room) SendToViewer(id string, msg any) bool {
	r.mu.Lock()
	v := r.viewers[id]
	r.mu.Unlock()
	if v == nil {
		return false
	}
	_ = v.sink.Send(msg)
	return true
}

// BroadcastToViewers forwards a message to every viewer except skipID (pass ""
// to include all). Sinks are copied under the lock and called after releasing
// it, so a slow viewer cannot stall the room.
func (r *Room) BroadcastToViewers(msg any, skipID string) {
	r.mu.Lock()
	sinks := make([]Sink, 0, len(r.viewers))
	for id, v := range r.viewers {
		if id == skipID {
			continue
		}
		sinks = append(sinks, v.sink)
	}
	r.mu.Unlock()
	for _, s := range sinks {
		_ = s.Send(msg)
	}
}

// Idle reports whether the room can be reaped: past the idle timeout with no
// viewers AND no publisher attached.
//
// The publisher check is essential. A room with a live publisher socket is an
// ONGOING share — the publisher may simply have no audience yet. Reaping it would
// delete a room the publisher is still broadcasting to: the share link would
// start 404ing while its UI still says "sharing".
func (r *Room) Idle(now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.viewers) > 0 || r.publisher != nil {
		return false
	}
	return now.Sub(r.lastActive) > r.idleTimeout
}

// touch records activity. Called after the caller has released r.mu so that the
// lock is never re-entered.
func (r *Room) touch() {
	r.mu.Lock()
	r.lastActive = time.Now()
	r.mu.Unlock()
}

// Registry owns every live room, keyed by token, plus a project index so that
// creating a share twice in the same project reuses the first room.
//
// All state is in memory: a live share is inherently transient, and losing it on
// restart is the correct semantics (the link stops working, which is what a
// viewer would expect after the server went away).
type Registry struct {
	mu        sync.Mutex
	rooms     map[string]*Room
	byProject map[string]string

	maxViewers  int
	idleTimeout time.Duration

	// maxRooms caps the total number of live rooms. Enforced inside Create so
	// the check and the insert are one atomic step — a handler-side check would
	// let two concurrent creates both pass it.
	maxRooms int
}

// NewRegistry returns an empty registry with the default limits.
func NewRegistry() *Registry {
	return &Registry{
		rooms:       make(map[string]*Room),
		byProject:   make(map[string]string),
		maxViewers:  DefaultMaxViewers,
		idleTimeout: DefaultIdleTimeout,
	}
}

// SetLimits overrides the viewer cap and idle timeout. Intended for tests and
// for callers that want a different policy; values <= 0 are ignored.
func (g *Registry) SetLimits(maxViewers int, idleTimeout time.Duration) {
	g.mu.Lock()
	if maxViewers > 0 {
		g.maxViewers = maxViewers
	}
	if idleTimeout > 0 {
		g.idleTimeout = idleTimeout
	}
	g.mu.Unlock()
}

// SetMaxRooms caps the total number of live rooms (0 = unlimited).
func (g *Registry) SetMaxRooms(n int) {
	g.mu.Lock()
	g.maxRooms = n
	g.mu.Unlock()
}

// Create returns the room for projectKey, creating it when absent.
//
// Idempotent by design: the publisher's "start sharing" button may be pressed
// twice (double click, retry after a flaky response), and minting a second token
// would silently invalidate the link the user already copied.
//
// title is an INITIAL value. A room that is already publishing keeps the title
// it has; only a room nobody is publishing to (a fresh one, or one still waiting
// for its publisher to attach) takes the new one.
func (g *Registry) Create(projectKey, title string) (*Room, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if token, ok := g.byProject[projectKey]; ok {
		if room, live := g.rooms[token]; live {
			// The title is a STARTING state, like the room's switches: a meeting
			// that is already publishing keeps the name its audience is looking
			// at. This path is taken by every retry and by a second device (the
			// create endpoint is idempotent per project), and the caller falls
			// back to a DATE PLACEHOLDER when the body carries no title — so an
			// unconditional write would rename a running meeting for everybody
			// watching it, plus every notification raised afterwards.
			//
			// Checked here rather than in the caller because this is the only
			// writer of title: a caller-side "look up the live room first" has a
			// window where the room is reaped between the lookup and Create, and
			// the brand-new room would inherit the dead meeting's name.
			if !room.HasPublisher() {
				room.mu.Lock()
				room.title = title
				room.mu.Unlock()
			}
			room.touch()
			return room, nil
		}
		// Stale index entry (the room was reaped): fall through and recreate.
		delete(g.byProject, projectKey)
	}

	// Ceiling applies only to NEW rooms: reusing an existing one does not add
	// to the count, so it must not be blocked at the limit.
	if g.maxRooms > 0 && len(g.rooms) >= g.maxRooms {
		return nil, ErrTooManyRooms
	}

	token, err := NewToken()
	if err != nil {
		return nil, err
	}
	room := &Room{
		token:      token,
		projectKey: projectKey,
		title:      title,
		createdAt:  time.Now(),
		viewers:    make(map[string]*Viewer),
		// Default true: a room created by an older client (or by a test) still
		// means "screen share", which is what every room did before voice mode.
		videoEnabled: true,
		// Same reasoning: before video meetings existed the picture WAS a
		// screen, and there was no camera. The create call overwrites both for
		// a room that is not live yet (see serveLiveShareCreate).
		hostScreen:  true,
		maxViewers:  g.maxViewers,
		idleTimeout: g.idleTimeout,
		lastActive:  time.Now(),
	}
	g.rooms[token] = room
	g.byProject[projectKey] = token
	return room, nil
}

// Get returns the room for a token, or nil.
func (g *Registry) Get(token string) *Room {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.rooms[token]
}

// Delete removes a room by token, detaching AND hanging up its publisher.
// Deleting an unknown token is a no-op.
//
// Hanging up matters because the room can be deleted by somebody other than the
// publisher's own socket: the host ends the meeting from a second device (the
// DELETE endpoint), or a stale room is rotated out before a new meeting starts.
// Forgetting the sink alone left that publisher page showing "sharing" forever —
// its socket stayed open and nothing ever told it the room was gone, so only a
// reload or a new create noticed. Closing the socket is the signal its teardown
// already handles (the same one a dropped connection gives).
//
// Viewers are the caller's business: the registry does not know how to phrase
// "the publisher is gone", so `Reap` returns the rooms instead of discarding
// them silently.
func (g *Registry) Delete(token string) {
	g.mu.Lock()
	room, ok := g.rooms[token]
	if ok {
		delete(g.rooms, token)
		if room.projectKey != "" && g.byProject[room.projectKey] == token {
			delete(g.byProject, room.projectKey)
		}
	}
	g.mu.Unlock()
	if ok {
		room.endPublisher()
	}
}

// endPublisher detaches the publisher and closes its transport.
//
// The reservation is cleared too: a socket that reserved the slot but had not
// finished its upgrade when the room was deleted must not keep a claim on a room
// nobody can look up any more.
func (r *Room) endPublisher() {
	r.mu.Lock()
	p := r.publisher
	r.publisher = nil
	r.publisherReserved = false
	r.mu.Unlock()
	r.touch()
	if p == nil {
		return
	}
	if closer, ok := p.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}

// Reap drops every room that has been idle past the timeout and returns the
// tokens it removed. Callers broadcast "publisher gone" to the removed rooms'
// viewers themselves (the registry does not know how to phrase that), so the
// rooms are returned rather than merely discarded.
func (g *Registry) Reap(now time.Time) []*Room {
	g.mu.Lock()
	var dead []*Room
	for token, room := range g.rooms {
		if room.Idle(now) {
			dead = append(dead, room)
			delete(g.rooms, token)
			if room.projectKey != "" && g.byProject[room.projectKey] == token {
				delete(g.byProject, room.projectKey)
			}
		}
	}
	g.mu.Unlock()
	return dead
}

// Len reports how many live rooms exist.
func (g *Registry) Len() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.rooms)
}

// FindByProject returns the room owned by projectKey, or nil. Used by the status
// endpoint, which knows the project but not the token.
func (g *Registry) FindByProject(projectKey string) *Room {
	g.mu.Lock()
	defer g.mu.Unlock()
	token, ok := g.byProject[projectKey]
	if !ok {
		return nil
	}
	return g.rooms[token]
}

// NewToken mints a 32-character lowercase hex token.
//
// Exported because the notes pipeline mints its own token for solo (room-less)
// sessions: the format has to stay identical to a share token, otherwise the
// notes endpoints would accept one and reject the other.
func NewToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// viewerID renders the per-room viewer id. Sequential rather than random: it is
// only ever compared within one room, and a readable id makes logs legible.
func viewerID(n int) string {
	return "v" + itoa(n)
}

// itoa avoids pulling strconv into the hot path signature; the range is tiny.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
