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
	// ErrTalkNotAllowed means the room has not enabled audience speech.
	ErrTalkNotAllowed = errors.New("liveshare: talking is not enabled for this room")
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
}

// Talking reports whether this viewer's microphone is on.
func (v *Viewer) Talking() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.talking
}

// Room is a single live share. All methods are safe for concurrent use.
type Room struct {
	mu sync.Mutex

	token      string
	projectKey string
	title      string
	createdAt  time.Time

	publisher Sink
	viewers   map[string]*Viewer
	nextID    int

	// talkEnabled is the room-level switch. Per-viewer grants are on Viewer.
	talkEnabled bool

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

// TalkEnabled reports whether the audience may request speech.
func (r *Room) TalkEnabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.talkEnabled
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

// ReservePublisher marks the room as having a publisher without attaching a
// transport yet. Used to reject a second publisher with a real HTTP 409 BEFORE
// the WebSocket upgrade (once upgraded, the client cannot see a status code).
// Must be followed by SetPublisher with the real sink, or ReleasePublisher.
func (r *Room) ReservePublisher() bool {
	return r.ClaimPublisher(reservedPublisher)
}

// reservedPublisher is the placeholder attached by ReservePublisher. Any real
// sink is != it, so compare-and-clear still works.
var reservedPublisher Sink = reservedSink{}

type reservedSink struct{}

func (reservedSink) Send(any) error { return nil }

// ClaimPublisher attaches s only if no publisher is currently attached.
//
// Check-then-set must be one atomic step: two connections arriving together
// would otherwise both observe "no publisher", and the loser's teardown would
// delete the room the winner is still using.
func (r *Room) ClaimPublisher(s Sink) bool {
	r.mu.Lock()
	if r.publisher != nil {
		r.mu.Unlock()
		return false
	}
	r.publisher = s
	r.mu.Unlock()
	r.touch()
	return true
}

// ReleaseReservation clears a reservation made by ReservePublisher, but only if
// nothing replaced it (a real sink means the upgrade succeeded).
func (r *Room) ReleaseReservation() {
	r.ReleasePublisher(reservedPublisher)
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
func (r *Room) AddViewer(name string, sink Sink) (*Viewer, error) {
	r.mu.Lock()
	if len(r.viewers) >= r.maxViewers {
		r.mu.Unlock()
		return nil, ErrRoomFull
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

// RemoveViewer detaches a viewer. Removing an unknown id is a no-op so that
// double-teardown (read loop + deferred cleanup) cannot fail.
func (r *Room) RemoveViewer(id string) {
	r.mu.Lock()
	delete(r.viewers, id)
	r.mu.Unlock()
	r.touch()
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

// Idle reports whether the room has been inactive past its idle timeout. A room
// with viewers is never idle: someone is still watching it.
func (r *Room) Idle(now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.viewers) > 0 {
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
func (g *Registry) Create(projectKey, title string) (*Room, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if token, ok := g.byProject[projectKey]; ok {
		if room, live := g.rooms[token]; live {
			room.mu.Lock()
			room.title = title
			room.mu.Unlock()
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

	token, err := newToken()
	if err != nil {
		return nil, err
	}
	room := &Room{
		token:       token,
		projectKey:  projectKey,
		title:       title,
		createdAt:   time.Now(),
		viewers:     make(map[string]*Viewer),
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

// Delete removes a room by token, detaching the publisher. Deleting an unknown
// token is a no-op.
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
		room.SetPublisher(nil)
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

// newToken mints a 32-character lowercase hex token.
func newToken() (string, error) {
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
