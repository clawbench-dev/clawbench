// Live screen/audio share (HTTP + WebSocket transport).
//
// This file is the ClawBench binding of internal/liveshare: it owns the project
// scoping, the auth split (management is authenticated, the viewer socket is
// not) and the websocket plumbing. The room model, the speech state machine and
// the wire structs live in the package, which knows nothing about ClawBench so
// that it can be lifted into a standalone relay application unchanged.
//
// Media never touches the server. It forwards SDP and ICE verbatim and stops
// there; the browsers connect directly (P2P). See the design doc
// docs/plans/2026-10-28-desktop-screen-audio-share-design.md.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"clawbench/internal/liveshare"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/coder/websocket"
)

// liveShareWriteTimeout bounds a single frame write. Matches the other WS
// endpoints so a dead peer is detected on the same schedule everywhere.
const liveShareWriteTimeout = 5 * time.Second

// maxLiveRooms caps concurrent rooms across all projects. Each room can hold
// up to liveshare.DefaultMaxViewers sockets, so this also bounds total sockets
// (20 × 9 = 180) independently of the per-room cap.
const maxLiveRooms = 20

// liveRegistry is the process-wide room table. In-memory on purpose: a live
// share is transient, and losing it on restart is the correct semantics.
var liveRegistry = liveshare.NewRegistry()

func init() {
	// The ceiling lives in the registry so check-and-create is atomic; see
	// liveshare.Registry.Create.
	liveRegistry.SetMaxRooms(maxLiveRooms)
}

// liveSink adapts a websocket connection to liveshare.Sink.
//
// Writes are serialized by the shared mutex because both the read loop (error
// frames) and the room's fan-out goroutine write to the same socket, and
// coder/websocket permits only one concurrent writer.
type liveSink struct {
	conn *websocket.Conn
	mu   *sync.Mutex
}

func (s *liveSink) Send(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), liveShareWriteTimeout)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn.Write(ctx, websocket.MessageText, data)
}

// Close hangs up the socket. It is what makes `kick` a removal rather than a
// suggestion: a viewer that keeps its socket could keep sending offers under an
// id the room has already forgotten. Closing from another goroutine unblocks
// the read loop, which then runs the normal teardown path.
func (s *liveSink) Close() error {
	return s.conn.CloseNow()
}

// liveShareManageResponse is the management endpoint's payload.
//
// The two switch fields are not omitempty: `false` is the meaningful answer for
// a room created without them, and the publisher renders exactly that.
type liveShareManageResponse struct {
	Token         string `json:"token,omitempty"`
	Path          string `json:"path,omitempty"`
	TalkEnabled   bool   `json:"talkEnabled"`
	ScreenEnabled bool   `json:"screenEnabled"`
}

// liveShareStatusResponse is the GET payload.
type liveShareStatusResponse struct {
	Active  bool `json:"active"`
	Viewers int  `json:"viewers,omitempty"`
}

// ServeLiveShareManage handles the authenticated publisher-side endpoints.
//
//	POST   → create (or reuse) this project's room
//	GET    → report whether a room is live and how many are watching
//	DELETE → end the share, disconnecting every viewer
func ServeLiveShareManage(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodPost:
		serveLiveShareCreate(w, r, projectPath)
	case http.MethodGet:
		serveLiveShareStatus(w, projectPath)
	case http.MethodDelete:
		serveLiveShareEnd(w, r, projectPath)
	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

// liveShareCreateOptions is what a create request asks the room to START with.
// Every field is a starting state, not a meeting type: each one can be changed
// again while the meeting runs.
type liveShareCreateOptions struct {
	title      string
	notes      bool
	video      bool
	camera     bool
	hostScreen bool
	// allowTalk / allowMemberScreen are the two room-level audience
	// permissions. Absent means "this client does not know about them" → off,
	// which is how every client behaved before these fields existed.
	allowTalk         bool
	allowMemberScreen bool
}

// liveShareCreateBody derives the share title, the capture flags and the
// audience permissions from the create request. The title is author-supplied
// text that ends up on a public page, so it is only ever rendered as text
// (never as HTML) by the SPA.
//
// `video` is derived from camera/screen, and a client that sends neither is a
// legacy one whose picture has always been a screen.
func liveShareCreateBody(r *http.Request) liveShareCreateOptions {
	var body struct {
		Title string `json:"title"`
		Notes bool   `json:"notes"`
		// Video defaults to true: a client that does not know about the
		// camera/screen split is asking for the screen share it has always got.
		Video *bool `json:"video"`
		// Camera and Screen say what the initial picture is made of. Absent
		// means "this client does not know about them", not "off" — hence
		// pointers.
		Camera *bool `json:"camera"`
		Screen *bool `json:"screen"`
		// The audience switches are pointers for the same reason: an old client
		// that never offered them must keep the closed room it has always had.
		AllowTalk         *bool `json:"allowTalk"`
		AllowMemberScreen *bool `json:"allowMemberScreen"`
	}
	opts := liveShareCreateOptions{video: true}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if body.Video != nil {
		opts.video = *body.Video
	}
	if body.Camera != nil || body.Screen != nil {
		opts.camera = body.Camera != nil && *body.Camera
		opts.hostScreen = body.Screen != nil && *body.Screen
		// Derived, never trusted: a client claiming "video" with neither source
		// would leave viewers waiting for a picture that does not exist.
		opts.video = opts.camera || opts.hostScreen
	} else {
		opts.hostScreen = opts.video
	}
	opts.notes = body.Notes
	opts.allowTalk = body.AllowTalk != nil && *body.AllowTalk
	opts.allowMemberScreen = body.AllowMemberScreen != nil && *body.AllowMemberScreen
	opts.title = strings.TrimSpace(body.Title)
	if opts.title != "" {
		opts.title = truncateRunes(opts.title, 80)
	} else {
		// Unnamed. The project's own name used to stand in here, which made
		// every meeting in a project carry the identical title and told the
		// reader nothing. Date it instead; the summary model replaces this
		// placeholder once the minutes exist (service.GenerateLiveNotesTitle).
		opts.title = service.PlaceholderMeetingTitle(time.Now())
	}
	return opts
}

// liveShareReuseGrace is how long a publisher-less room is still treated as
// "about to be attached" rather than "left over from a previous meeting".
// A variable (not a const) so tests can drive the stale path without sleeping.
var liveShareReuseGrace = 10 * time.Second

func serveLiveShareCreate(w http.ResponseWriter, r *http.Request, projectPath string) {
	opts := liveShareCreateBody(r)
	room, err := createLiveShareRoom(projectPath, opts.title)
	if err != nil {
		if errors.Is(err, liveshare.ErrTooManyRooms) {
			writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "LiveShareTooManyRooms")
			return
		}
		slog.Error("liveshare: create room failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	// Everything below is a STARTING state, not a command: a room that already
	// has a publisher (or an audience) is a meeting in progress, and the same
	// POST that the publisher uses to start one is also what a retry, a second
	// device or a script sends. Writing these into a live room would revoke
	// every viewer's microphone (talk-revoked reason=disabled), cut off whoever
	// is sharing a screen (screen-denied reason=disabled) and silently rewrite
	// the picture flags — because every field here defaults to false when the
	// body omits it.
	//
	// 听记 is in the same group, not an exception. It was outside this guard
	// while `joined` was the only way viewers learned the flag; now that a
	// mid-meeting toggle travels as `set-notes`, a second POST must not rewrite
	// it either: the flag is view-only (nothing reads it to gate recording or
	// uploads), so an unchecked create form on another device would leave new
	// viewers told "听记 stopped" — and their pane stops polling — while the
	// publisher is still recording, and the viewers already in the room
	// disagree. Before the publisher attaches (the grace window, a retry) this
	// block still runs, which is exactly the case the old comment was about.
	if !liveRoomInProgress(room) {
		room.SetNotesEnabled(opts.notes)
		room.SetVideoEnabled(opts.video)
		room.SetCameraEnabled(opts.camera)
		room.SetHostScreen(opts.hostScreen)
		// The audience permissions are part of the room's starting state, so a
		// host who ticked them in the create form does not have to tick them
		// again in the meeting. Routed through the same appliers as the control
		// frames so the two paths cannot drift (they are the same switch).
		applySetTalkEnabled(room, opts.allowTalk)
		applySetScreenEnabled(room, opts.allowMemberScreen)
	}

	writeJSON(w, http.StatusOK, liveShareManageResponse{
		Token: room.Token(),
		Path:  "/share/" + room.Token(),
		// Echoed back because the publisher is the ONE client that never
		// receives a `joined` frame: without these it would draw both switches
		// as off while the room has them on.
		TalkEnabled:   room.TalkEnabled(),
		ScreenEnabled: room.ScreenEnabled(),
	})
}

// liveRoomInProgress reports whether a create request landed on a meeting that
// is already running, in which case its body must not be applied.
//
// The criterion is "somebody is on the other side of this room", not "the room
// object exists": a room reused inside the grace window with no publisher yet is
// the same client retrying, and its switches are still initial state. A
// reservation counts as a publisher (a socket is on its way), and viewers count
// on their own — they can only be there because a publisher was attached a
// moment ago, and they are exactly who the reset would hurt.
func liveRoomInProgress(room *liveshare.Room) bool {
	return room.HasPublisher() || room.ViewerCount() > 0
}

// createLiveShareRoom returns the project's room, starting a NEW one when the
// existing room is a leftover.
//
// Rooms are idempotent per project, and a room outlives its publisher: after a
// crash or an app restart it sits in the registry until the idle timeout (30
// min). The token is the ONLY key for the AI 听记 rows, so handing the same token
// back would make the new meeting's transcript start with the previous one's —
// the captions would open pre-filled and the minutes would summarize both
// meetings. Rotating the token keeps the old notes where they are (they age out
// on their own) instead of deleting data.
//
// The grace period protects the normal sequence: POST creates the room, then the
// publisher socket attaches a moment later. Without it a second create in that
// window would rotate the token out from under the first.
func createLiveShareRoom(projectPath, title string) (*liveshare.Room, error) {
	room, err := liveRegistry.Create(projectPath, title)
	if err != nil {
		return nil, err
	}
	if room.HasPublisher() || time.Since(room.CreatedAt()) <= liveShareReuseGrace {
		return room, nil
	}
	slog.Info("liveshare: rotating stale room token",
		"token", room.Token(), "age", time.Since(room.CreatedAt()).String())
	liveRegistry.Delete(room.Token())
	return liveRegistry.Create(projectPath, title)
}

func serveLiveShareStatus(w http.ResponseWriter, projectPath string) {
	room := liveRegistry.FindByProject(projectPath)
	if room == nil {
		writeJSON(w, http.StatusOK, liveShareStatusResponse{})
		return
	}
	writeJSON(w, http.StatusOK, liveShareStatusResponse{Active: true, Viewers: room.ViewerCount()})
}

func serveLiveShareEnd(w http.ResponseWriter, r *http.Request, projectPath string) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "TokenRequired")
		return
	}
	room := liveRegistry.Get(token)
	if room == nil {
		// Ending an already-ended share is not an error: the publisher's UI may
		// retry, and the user-visible outcome is identical.
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	// Ownership check, mirroring LiveSharePublishWS. Without it any signed-in
	// user of ANOTHER project could end someone else's share — the token is
	// handed to every viewer, so it is not a secret from them.
	//
	// Same uniform 404 as an unknown token: the response must not reveal that a
	// room exists which this project does not own.
	if room.ProjectKey() != projectPath {
		http.NotFound(w, r)
		return
	}
	// Tell the viewers before dropping the room, so they see "publisher ended
	// the share" rather than a bare socket close.
	room.BroadcastToViewers(liveshare.ViewerMessage{Type: liveshare.MsgPublisherGone, Reason: "ended"}, "")
	liveRegistry.Delete(token)
	if projectID, err := store.ProjectIDForPath(projectPath); err == nil {
		finalizeLiveNotes(token, projectID, projectPath)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// LiveSharePublishWS handles the publisher socket (authenticated).
//
// WS /api/live-share/publish/ws?token={token}
func LiveSharePublishWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	token := r.URL.Query().Get("token")
	room := liveRegistry.Get(token)
	if room == nil || room.ProjectKey() != projectPath {
		// One uniform 404: a token that exists but belongs to another project
		// must not be distinguishable from a nonexistent one.
		http.NotFound(w, r)
		return
	}
	// Reserve BEFORE upgrading: a second publisher must receive a real HTTP 409,
	// which is impossible once the socket has been upgraded.
	if !room.ReservePublisher() {
		writeLocalizedErrorf(w, r, http.StatusConflict, "LiveShareAlreadyPublishing")
		return
	}
	reserved := true
	defer func() {
		if reserved {
			// We reserved but never attached a real sink (upgrade failed).
			room.ReleaseReservation()
		}
	}()

	conn, err := acceptLiveWS(w, r)
	if err != nil {
		slog.Error("liveshare: publish accept failed", "err", err)
		return
	}
	defer func() { _ = conn.CloseNow() }()

	var writeMu sync.Mutex
	sink := &liveSink{conn: conn, mu: &writeMu}

	// Swap the reservation for the real sink. AttachPublisher is not a claim:
	// we already own the slot, and it clears the reservation flag.
	room.AttachPublisher(sink)
	reserved = false

	// The meeting record starts here, not when the folder is first written: a
	// meeting that recorded nothing still happened, and must appear in the list
	// (and be deletable) like any other.
	projectID, pidErr := store.ProjectIDForPath(projectPath)
	if pidErr != nil {
		slog.Warn("liveshare: meeting record project id failed", "err", pidErr)
	}
	if pidErr == nil {
		// Meetings have no type: what the room happens to carry at this instant
		// is not a property of the record, and it can change a second later.
		if startErr := service.StartLiveNote(token, projectID, room.Title(), time.Now().UnixMilli()); startErr != nil {
			slog.Warn("liveshare: meeting record start failed", "err", startErr)
		}
	}

	// The publisher owns the room's lifetime: when this socket goes away the
	// share is over, and every viewer must be told rather than left hanging.
	//
	// ReleasePublisher is compare-and-clear so a socket that was replaced never
	// deletes the room its successor is using.
	defer func() {
		if !room.ReleasePublisher(sink) {
			return
		}
		room.BroadcastToViewers(liveshare.ViewerMessage{Type: liveshare.MsgPublisherGone, Reason: "ended"}, "")
		liveRegistry.Delete(token)
		// Close the record and archive the chat while the project path is still
		// known. Best effort: losing the archive must not keep the meeting open.
		finalizeLiveNotes(token, projectID, projectPath)
	}()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	for {
		_, data, rerr := conn.Read(ctx)
		if rerr != nil {
			return
		}
		var ctl liveshare.PublisherControl
		if err := json.Unmarshal(data, &ctl); err != nil {
			_ = sink.Send(liveshare.ViewerMessage{Type: liveshare.MsgError, Message: "invalid control frame"})
			continue
		}
		if handlePublisherControl(room, sink, ctl) {
			return
		}
	}
}

// handlePublisherControl applies one publisher frame, reporting whether the
// socket should close.
func handlePublisherControl(room *liveshare.Room, sink *liveSink, ctl liveshare.PublisherControl) (closeConn bool) {
	switch ctl.Type {
	case liveshare.CtrlPublish:
		// The publisher's first frame carries the title; the room already has
		// one from the management call, so that part is informational only. The
		// nickname is NOT: the audience's member list shows it on the host row,
		// and a viewer that joined between attach and this frame has an empty
		// one cached, so the roster is re-sent.
		// Sanitized like the viewer nickname: the host name is rendered as a
		// roster row in every viewer's member list, and a name with a newline
		// in it breaks that list just as badly as a forged transcript line.
		room.SetPublisherName(sanitizeLiveViewerName(ctl.Name))
		broadcastRoster(room, "")
		return false

	case liveshare.CtrlEnd:
		return true

	case liveshare.CtrlAnswer:
		room.SendToViewer(ctl.ViewerID, liveshare.ViewerMessage{
			Type: liveshare.MsgAnswer,
			SDP:  ctl.SDP,
		})
		return false

	case liveshare.CtrlICE:
		// Only the viewer's candidate travels this direction.
		room.SendToViewer(ctl.ViewerID, liveshare.ViewerMessage{
			Type:      liveshare.MsgICE,
			Candidate: ctl.Candidate,
		})
		return false

	case liveshare.CtrlSetTalkEnabled:
		applySetTalkEnabled(room, ctl.Enabled)
		return false

	case liveshare.CtrlTalkRevoke:
		if err := room.SetTalking(ctl.ViewerID, false); err != nil {
			_ = sink.Send(liveshare.PublisherMessage{Type: liveshare.MsgError, ViewerID: ctl.ViewerID, Name: err.Error()})
			return false
		}
		room.SendToViewer(ctl.ViewerID, liveshare.ViewerMessage{Type: liveshare.MsgTalkRevoked, Reason: liveshare.ReasonRevoked})
		notifyTalkState(room, ctl.ViewerID, false)
		return false

	case liveshare.CtrlTalkMuteAll:
		for _, id := range room.MuteAll() {
			room.SendToViewer(id, liveshare.ViewerMessage{Type: liveshare.MsgTalkRevoked, Reason: liveshare.ReasonMuted})
			notifyTalkState(room, id, false)
		}
		return false

	case liveshare.CtrlTalkMute:
		if _, err := room.SetMuted(ctl.ViewerID, ctl.Enabled); err != nil {
			_ = sink.Send(liveshare.PublisherMessage{Type: liveshare.MsgError, ViewerID: ctl.ViewerID, Name: err.Error()})
			return false
		}
		if ctl.Enabled {
			// Muting silences the microphone as well, so the viewer gets the
			// same "stop now" frame it already understands, tagged as a host
			// block (it must NOT clear the room-level speech switch).
			room.SendToViewer(ctl.ViewerID, liveshare.ViewerMessage{
				Type:   liveshare.MsgTalkRevoked,
				Reason: liveshare.ReasonHostMuted,
			})
			notifyTalkState(room, ctl.ViewerID, false)
			return false
		}
		room.SendToViewer(ctl.ViewerID, liveshare.ViewerMessage{Type: liveshare.MsgTalkUnmuted})
		notifyTalkState(room, ctl.ViewerID, false)
		return false

	case liveshare.CtrlKick:
		// The viewer's own teardown reports viewer-leave. Only the picture needs
		// telling here: removal already cleared the sharer slot, so the normal
		// "was it sharing?" branch in that teardown sees nothing to broadcast.
		if room.KickViewer(ctl.ViewerID) {
			broadcastScreenState(room)
		}
		return false

	case liveshare.CtrlSetScreenEnabled:
		applySetScreenEnabled(room, ctl.Enabled)
		return false

	case liveshare.CtrlSetControlEnabled:
		applySetControlEnabled(room, ctl.Enabled)
		return false

	case liveshare.CtrlSetNotes:
		applySetNotes(room, ctl.Enabled)
		return false

	case liveshare.CtrlSetPicture:
		applySetPicture(room, ctl)
		return false

	case liveshare.CtrlScreenRevoke:
		applyScreenRevoke(room, ctl.ViewerID)
		return false

	default:
		_ = sink.Send(liveshare.PublisherMessage{Type: liveshare.MsgError, Name: "unknown control type: " + ctl.Type})
		return false
	}
}

// LiveShareSignalWS handles the viewer socket. PUBLIC: the token is the sole
// credential, exactly like the file/session share endpoints.
//
// WS /api/share/{token}/signal
func LiveShareSignalWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}
	token := liveTokenFromSignalPath(r.URL.Path)
	if token == "" || !validShareToken(token) {
		http.NotFound(w, r)
		return
	}
	room := liveRegistry.Get(token)
	if room == nil {
		http.NotFound(w, r)
		return
	}
	if room.Publisher() == nil {
		// No publisher means the room is a leftover from a socket that has not
		// been reaped yet; treat it as gone rather than letting a viewer attach
		// to a room nobody is publishing to.
		http.NotFound(w, r)
		return
	}
	// The nickname is required. Validated BEFORE the upgrade so a bad name is a
	// real 400 the SPA can explain, instead of a socket that opens and then dies
	// — which the share page can only report as "connection failed".
	name := sanitizeLiveViewerName(r.URL.Query().Get("name"))
	if name == "" || len([]rune(name)) > liveViewerNameMaxRunes {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "ViewerNameRequired")
		return
	}

	conn, err := acceptLiveWS(w, r)
	if err != nil {
		slog.Error("liveshare: signal accept failed", "err", err)
		return
	}
	defer func() { _ = conn.CloseNow() }()

	var writeMu sync.Mutex
	sink := &liveSink{conn: conn, mu: &writeMu}

	viewer, err := room.AddViewer(name, sink)
	if err != nil {
		if errors.Is(err, liveshare.ErrRoomFull) {
			_ = sink.Send(liveshare.ViewerMessage{Type: liveshare.MsgFull})
		}
		return
	}

	defer func() {
		wasSharing := room.ScreenSharer() == viewer.ID
		room.RemoveViewer(viewer.ID)
		room.SendToPublisher(liveshare.PublisherMessage{
			Type:     liveshare.MsgViewerLeave,
			ViewerID: viewer.ID,
		})
		// RemoveViewer already cleared the slot; everyone else still has to be
		// told, or they keep showing a screen that no longer exists.
		if wasSharing {
			broadcastScreenState(room)
		}
		// The audience's member list loses this row too.
		broadcastRoster(room, "")
	}()

	// Re-check after attaching: the publisher may have detached between the
	// check above and AddViewer. Without this the viewer would receive `joined`,
	// send an offer, and wait forever for an answer that can never come.
	if room.Publisher() == nil {
		return
	}

	// Tell the publisher a viewer arrived so it can build a PeerConnection for
	// it, and tell the viewer its id plus whether speech is available.
	room.SendToPublisher(liveshare.PublisherMessage{
		Type:     liveshare.MsgViewerJoin,
		ViewerID: viewer.ID,
		Name:     viewer.Name,
	})
	if err := sink.Send(liveshare.ViewerMessage{
		Type:          liveshare.MsgJoined,
		ViewerID:      viewer.ID,
		Title:         room.Title(),
		ICEServers:    []any{}, // reserved for a future TURN; see signal.go
		TalkEnabled:   room.TalkEnabled(),
		NotesEnabled:  room.NotesEnabled(),
		VideoEnabled:  room.VideoEnabled(),
		CameraEnabled: room.CameraEnabled(),
		HostScreen:    room.HostScreen(),
		ScreenEnabled: room.ScreenEnabled(),
		ScreenSharer:  room.ScreenSharer(),
		// Publisher capability, not a room switch: whether the host can accept
		// a remote-control request at all (injector + whole-screen picture).
		ControlEnabled: room.ControlEnabled(),
		// The roster rides the `joined` frame: the new viewer needs its own id
		// before it can mark "you" in the list, and a second frame would only
		// be a race to see which arrives first.
		Roster: room.Roster(),
	}); err != nil {
		return
	}

	// Everyone else learns that somebody arrived.
	broadcastRoster(room, viewer.ID)

	// The host wants to know someone arrived: a meeting is often attended while
	// the host is looking at another tab, and nothing else in the app reports
	// it. Emitted after `joined` succeeded, so only a viewer that is actually in
	// the room counts.
	notifyMeeting(room, service.MeetingEventViewerJoined, viewer)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	for {
		_, data, rerr := conn.Read(ctx)
		if rerr != nil {
			return
		}
		var ctl liveshare.ViewerControl
		if err := json.Unmarshal(data, &ctl); err != nil {
			_ = sink.Send(liveshare.ViewerMessage{Type: liveshare.MsgError, Message: "invalid control frame"})
			continue
		}
		handleViewerControl(room, viewer, ctl)
	}
}

// handleViewerControl applies one viewer frame.
func handleViewerControl(room *liveshare.Room, viewer *liveshare.Viewer, ctl liveshare.ViewerControl) {
	switch ctl.Type {
	case liveshare.CtrlJoin:
		// Informational: the join already happened when the socket attached.

	case liveshare.CtrlOffer:
		room.SendToPublisher(liveshare.PublisherMessage{
			Type:     liveshare.MsgOffer,
			ViewerID: viewer.ID,
			SDP:      ctl.SDP,
		})

	case liveshare.CtrlICE:
		room.SendToPublisher(liveshare.PublisherMessage{
			Type:      liveshare.MsgICE,
			ViewerID:  viewer.ID,
			Candidate: ctl.Candidate,
		})

	case liveshare.CtrlTalkStart:
		if err := room.SetTalking(viewer.ID, true); err != nil {
			// Rejected loudly: a client that turns its mic on with the room
			// switch off has a bug, and silence would turn it into "the toggle
			// does nothing". The two reasons are kept apart on the wire: a
			// per-viewer mute must not take the microphone switch away, since
			// the viewer may speak again the moment the host unmutes.
			if errors.Is(err, liveshare.ErrTalkMuted) {
				room.SendToViewer(viewer.ID, liveshare.ViewerMessage{
					Type:   liveshare.MsgTalkRevoked,
					Reason: liveshare.ReasonHostMuted,
				})
				return
			}
			room.SendToViewer(viewer.ID, liveshare.ViewerMessage{
				Type:   liveshare.MsgTalkDisabled,
				Reason: liveshare.ReasonDisabled,
			})
			return
		}
		notifyTalkState(room, viewer.ID, true)

	case liveshare.CtrlTalkEnd:
		if err := room.SetTalking(viewer.ID, false); err != nil {
			return
		}
		notifyTalkState(room, viewer.ID, false)

	case liveshare.CtrlScreenStart:
		if err := room.ClaimScreen(viewer.ID); err != nil {
			reason := liveshare.ReasonDisabled
			if errors.Is(err, liveshare.ErrScreenBusy) {
				reason = liveshare.ReasonBusy
			}
			// Denied loudly: the viewer is already capturing locally, so it has
			// to know to stop and release the camera/microphone indicator.
			room.SendToViewer(viewer.ID, liveshare.ViewerMessage{
				Type:   liveshare.MsgScreenDenied,
				Reason: reason,
			})
			return
		}
		notifyMeeting(room, service.MeetingEventScreenShare, viewer)
		broadcastScreenState(room)

	case liveshare.CtrlScreenStop:
		if room.ReleaseScreen(viewer.ID) {
			broadcastScreenState(room)
		}

	case liveshare.CtrlControlRequest:
		// The request reached the publisher over the P2P control channel
		// already; this is the server's copy, used only to notify a host who is
		// not on the meeting page. No room state changes, so a duplicate or a
		// request while control is disabled is harmless.
		notifyMeeting(room, service.MeetingEventControlRequest, viewer)
	}
}

// meetingNotifyKey identifies one occurrence of "who did what in which meeting".
//
// actor is the viewer id, except for viewer_joined where it is the nickname: a
// reconnect gets a FRESH viewer id (ids are per-connection), so keying the join
// on the id would notify the host again for the same person every time their
// network hiccups — while the nickname is exactly what the host reads.
type meetingNotifyKey struct {
	token     string
	eventType string
	actor     string
}

// meetingNotifyDedupeWindow is how long a repeat of the same occurrence is
// suppressed. Long enough that a reconnect loop or a held-down request cannot
// flood, short enough that a genuine second ask still reaches the host.
const meetingNotifyDedupeWindow = 30 * time.Second

// meetingNotifyDedupeMaxEntries bounds the table. Entries only matter inside the
// window, so pruning is enough; the cap keeps a burst of one-off keys (many
// tokens, many viewer ids) from growing it without limit.
const meetingNotifyDedupeMaxEntries = 512

// meetingNotifyLimiter suppresses repeated meeting notifications.
//
// `control-request` is the one frame an ANONYMOUS viewer can send in a loop: the
// token is the only credential, and each occurrence used to write a notification
// row (the 500-row cap then evicts real events) and broadcast a frame that pops a
// toast/system notification on the host. A reconnect loop is the same problem
// one step earlier, arriving as viewer_joined.
//
// A time window rather than a rate limit, and the actor is part of the key: a
// second request from the same viewer 30s later is a new ask (the host may have
// missed the first), and a DIFFERENT viewer's identical request is never merged
// with this one — two people asking for control are two facts.
type meetingNotifyLimiter struct {
	mu     sync.Mutex
	last   map[meetingNotifyKey]time.Time
	window time.Duration
	// now is a test seam. The read loops run in their own goroutines, so a test
	// cannot deterministically sleep its way to the end of the window; overriding
	// the clock under the same lock keeps the assertion honest and race-free.
	now func() time.Time
}

// allow records the occurrence and reports whether it should be notified.
func (l *meetingNotifyLimiter) allow(key meetingNotifyKey) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if last, seen := l.last[key]; seen && now.Sub(last) < l.window {
		return false
	}
	if len(l.last) >= meetingNotifyDedupeMaxEntries {
		l.evictLocked(now)
	}
	l.last[key] = now
	return true
}

// evictLocked keeps the table bounded.
//
// Expired entries go first (they cannot suppress anything any more). If the
// window is somehow full of live entries, the oldest one is dropped: the cap is
// a hard bound, and forgetting one key only costs a possible duplicate
// notification — never a lost one.
func (l *meetingNotifyLimiter) evictLocked(now time.Time) {
	oldestKey := meetingNotifyKey{}
	var oldest time.Time
	for k, at := range l.last {
		if now.Sub(at) >= l.window {
			delete(l.last, k)
			continue
		}
		if oldest.IsZero() || at.Before(oldest) {
			oldestKey, oldest = k, at
		}
	}
	if len(l.last) >= meetingNotifyDedupeMaxEntries && !oldest.IsZero() {
		delete(l.last, oldestKey)
	}
}

var meetingNotifyDedupe = &meetingNotifyLimiter{
	last:   make(map[meetingNotifyKey]time.Time),
	window: meetingNotifyDedupeWindow,
	now:    time.Now,
}

// notifyMeeting records one meeting event and pushes it to connected clients.
//
// The project path comes from the room: a meeting belongs to the project that
// created it, and the viewer socket carries no project (its token is the only
// credential). `viewer` is nil for events that have no actor (the minutes).
//
// Repeats are dropped BEFORE the emitter runs, so a suppressed occurrence costs
// neither a row nor a frame.
func notifyMeeting(room *liveshare.Room, eventType string, viewer *liveshare.Viewer) {
	viewerID, name := "", ""
	if viewer != nil {
		viewerID, name = viewer.ID, viewer.Name
	}
	actor := viewerID
	if eventType == service.MeetingEventViewerJoined {
		actor = name
	}
	if !meetingNotifyDedupe.allow(meetingNotifyKey{
		token:     room.Token(),
		eventType: eventType,
		actor:     actor,
	}) {
		return
	}
	service.EmitMeetingEvent(room.Token(), room.ProjectKey(), eventType, room.Title(), viewerID, name)
}

// applySetPicture records the publisher's current picture and tells every
// viewer.
//
// Viewers only: the picture is publisher-side, so the publisher already knows.
// The flags also live on the room (not just in this frame) so a viewer joining
// afterwards learns the current state from its `joined` frame instead of
// rendering a layout that ended minutes ago.
func applySetPicture(room *liveshare.Room, ctl liveshare.PublisherControl) {
	room.SetVideoEnabled(ctl.Video)
	room.SetCameraEnabled(ctl.Camera)
	room.SetHostScreen(ctl.Screen)
	room.BroadcastToViewers(liveshare.ViewerMessage{
		Type:          liveshare.MsgPictureState,
		VideoEnabled:  ctl.Video,
		CameraEnabled: ctl.Camera,
		HostScreen:    ctl.Screen,
	}, "")
}

// applySetTalkEnabled flips the room-level audience-speech switch and tells
// every viewer, in both directions. A viewer that joined while the switch was
// off has no other way to learn it was enabled later — its `joined` frame is
// long gone, so without this the microphone toggle would never appear.
//
// Shared with the create path, which sets the switch before any viewer exists;
// splitting the notification out would let the two paths drift.
func applySetTalkEnabled(room *liveshare.Room, enabled bool) {
	room.SetTalkEnabled(enabled)
	for _, v := range room.Viewers() {
		if enabled {
			room.SendToViewer(v.ID, liveshare.ViewerMessage{
				Type:        liveshare.MsgTalkEnabled,
				TalkEnabled: true,
			})
			continue
		}
		// Disabling also mutes everyone; the room already cleared the flags,
		// so the viewer just needs to stop and flip its toggle back.
		room.SendToViewer(v.ID, liveshare.ViewerMessage{
			Type:   liveshare.MsgTalkRevoked,
			Reason: liveshare.ReasonDisabled,
		})
		notifyTalkState(room, v.ID, false)
	}
}

// applySetScreenEnabled flips the room switch and stops the current sharer.
//
// Split out of the control switch purely to keep that function's cyclomatic
// complexity within the linter's budget; the behavior is the switch case.
func applySetScreenEnabled(room *liveshare.Room, enabled bool) {
	stopped := room.SetScreenEnabled(enabled)
	if stopped != "" {
		// The former sharer needs an explicit denial: its browser is still
		// capturing, and nothing else will tell it to stop.
		room.SendToViewer(stopped, liveshare.ViewerMessage{
			Type:   liveshare.MsgScreenDenied,
			Reason: liveshare.ReasonDisabled,
		})
	}
	broadcastScreenState(room)
}

// applySetNotes records the publisher's AI 听记 switch and tells every viewer.
//
// Mirrors applySetTalkEnabled, for the same reason: the `joined` frame is long
// gone for anybody already watching, so a mid-meeting toggle has no other way to
// reach them. Both directions matter — turning 听记 ON has to make the transcript
// pane appear, turning it OFF has to stop a viewer polling a transcript that will
// never grow again (hence the dedicated frame type, whose boolean is never
// omitted).
func applySetNotes(room *liveshare.Room, enabled bool) {
	room.SetNotesEnabled(enabled)
	room.BroadcastToViewers(liveshare.NotesEnabledMessage{
		Type:         liveshare.MsgNotesEnabled,
		NotesEnabled: enabled,
	}, "")
}

// applySetControlEnabled advertises the publisher's remote-control capability.
//
// Split out of the control switch for the same reason as the screen switch: the
// case stays one line. Every viewer is told, not only the ones that asked — a
// viewer that joined while the capability was off has no other way to learn it
// came back.
func applySetControlEnabled(room *liveshare.Room, enabled bool) {
	room.SetControlEnabled(enabled)
	room.BroadcastToViewers(liveshare.ViewerMessage{
		Type:           liveshare.MsgControlEnabled,
		ControlEnabled: enabled,
	}, "")
}

// applyScreenRevoke stops one viewer's share without touching the switch.
func applyScreenRevoke(room *liveshare.Room, viewerID string) {
	if !room.ReleaseScreen(viewerID) {
		return
	}
	room.SendToViewer(viewerID, liveshare.ViewerMessage{
		Type:   liveshare.MsgScreenDenied,
		Reason: liveshare.ReasonRevoked,
	})
	broadcastScreenState(room)
}

// broadcastScreenState tells the publisher and every viewer who is sharing.
//
// One frame for both sides: the publisher renders a preview and the viewers
// switch their main picture, and both must agree on WHO — a viewer that keeps
// showing a stale sharer is indistinguishable from a frozen stream.
func broadcastScreenState(room *liveshare.Room) {
	sharer := room.ScreenSharer()
	room.BroadcastToViewers(liveshare.ViewerMessage{
		Type:          liveshare.MsgScreenState,
		ViewerID:      sharer,
		Sharing:       sharer != "",
		ScreenEnabled: room.ScreenEnabled(),
	}, "")
	room.SendToPublisher(liveshare.PublisherMessage{
		Type:          liveshare.MsgScreenState,
		ViewerID:      sharer,
		Sharing:       sharer != "",
		ScreenEnabled: room.ScreenEnabled(),
	})
}

// notifyTalkState pushes one viewer's microphone state to the publisher.
//
// The mute flag is read back from the room rather than passed in: every caller
// (the viewer's own toggle, the host's per-viewer mute, mute-all) would
// otherwise have to remember to keep the two in step, and the publisher list
// renders them side by side.
func notifyTalkState(room *liveshare.Room, viewerID string, talking bool) {
	muted := false
	if v := room.Viewer(viewerID); v != nil {
		muted = v.Muted()
	}
	room.SendToPublisher(liveshare.PublisherMessage{
		Type:     liveshare.MsgTalkState,
		ViewerID: viewerID,
		Talking:  talking,
		Muted:    &muted,
	})
	// "Who is speaking" is part of the audience's member list too, and this is
	// the single funnel every speech change goes through.
	broadcastRoster(room, "")
}

// broadcastRoster pushes the full participant list to every viewer but skipID.
//
// Called on join, leave, publish (the host's nickname) and every speech change.
// A full snapshot costs one small frame and cannot drift; deltas would leave a
// viewer that missed one showing a wrong list until it reloaded.
func broadcastRoster(room *liveshare.Room, skipID string) {
	room.BroadcastToViewers(liveshare.ViewerMessage{
		Type:   liveshare.MsgRoster,
		Roster: room.Roster(),
	}, skipID)
}

// acceptLiveWS upgrades the connection with the same origin allow-list the other
// WS endpoints use. Without it a cross-origin page could open the socket.
func acceptLiveWS(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	return websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: wsOriginPatterns(r)})
}

// liveTokenFromSignalPath extracts {token} from /api/share/{token}/signal.
func liveTokenFromSignalPath(urlPath string) string {
	const prefix = "/api/share/"
	const suffix = "/signal"
	if len(urlPath) <= len(prefix)+len(suffix) {
		return ""
	}
	if urlPath[:len(prefix)] != prefix || urlPath[len(urlPath)-len(suffix):] != suffix {
		return ""
	}
	token := urlPath[len(prefix) : len(urlPath)-len(suffix)]
	// Reject anything with another separator: /api/share/a/b/signal is not a
	// shape this endpoint serves.
	for _, c := range token {
		if c == '/' {
			return ""
		}
	}
	return token
}

// truncateRunes shortens s to at most n runes, appending an ellipsis when cut.
// Rune-based so a multi-byte title is never split mid-character.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// liveShareRoomCountForTest exposes the registry size to tests without
// exporting the registry itself.
func liveShareRoomCountForTest() int {
	return liveRegistry.Len()
}

// liveViewerNameMaxRunes bounds a viewer's nickname. Kept equal to
// model.MaxUserNicknameRunes so the account nickname (which the desktop join
// page forwards) is always accepted. The cap exists so a hand-written URL
// cannot bloat the room.
const liveViewerNameMaxRunes = model.MaxUserNicknameRunes

// sanitizeLiveViewerName strips what a nickname must never carry.
//
// The rule is the one the account nickname already follows (validUserNickname):
// control characters are not allowed. Here they are REMOVED rather than
// rejected, because the share page forwards the account nickname automatically
// and refusing to join over an invisible byte is a dead end the reader cannot
// fix. The dangerous one is `\n`: the nickname is written verbatim as the
// `speaker` of every line in the archived chat.md and into notification bodies,
// so a name carrying a newline forges a whole extra line ("主持人：会议已结束")
// in a document that is later read as the record of the meeting.
//
// Quoting/angle brackets/comma are left alone: those matter to the group-routing
// protocol, which a meeting nickname never enters, and mangling a legitimate
// name ("张, 李") would be worse than the risk.
func sanitizeLiveViewerName(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		// Space rather than deletion, so a byte that was meant as a separator
		// does not glue two words together. The runs are collapsed below.
		if r < 0x20 || r == 0x7f {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// closeLiveNoteIntervals ends the meeting record and stamps every interval that
// never got a close event.
//
// The publisher is gone, so nothing is still open from the reader's point of
// view: an interval with no end would render as "opened at 10:03" with no answer
// to "until when" — the one question the timeline exists to answer. A screen
// share that was still running when the publisher vanished likewise ends with
// the meeting, not at an invented later time.
func closeLiveNoteIntervals(token string, now int64) {
	if err := service.EndLiveNote(token, now); err != nil {
		slog.Warn("liveshare: close meeting record failed", "err", err)
	}
	if err := service.CloseOpenLiveNoteFiles(token, now); err != nil {
		slog.Warn("liveshare: close open files failed", "err", err)
	}
	if err := service.CloseOpenLiveNoteScreenShare(token, now); err != nil {
		slog.Warn("liveshare: close open screen share failed", "err", err)
	}
}

// finalizeLiveNotes closes a meeting record and archives its chat.
//
// The archive is the same bargain the minutes and the transcripts get: a copy
// on disk that survives the app. It is written at the END of the meeting (not
// on every message) because the file is a readable artifact, not a write-ahead
// log — the database is the live copy.
func finalizeLiveNotes(token string, projectID int64, projectPath string) {
	now := time.Now().UnixMilli()
	closeLiveNoteIntervals(token, now)

	note, exists, err := service.GetLiveNote(token)
	if err != nil || !exists {
		return
	}
	startedAt := note.StartedAt
	if startedAt <= 0 {
		startedAt = now
	}

	// Both archives are optional: a meeting may have chat but no files, or
	// files but no chat (AI 听记 off). The folder is created only when one of
	// them actually has something to put in it.
	files, filesErr := service.ListLiveNoteFiles(token, 0)
	if filesErr != nil {
		slog.Warn("liveshare: list meeting files failed", "err", filesErr)
		files = nil
	}
	messages, chatErr := service.ListLiveNoteChat(token, 0)
	if chatErr != nil {
		slog.Warn("liveshare: list meeting chat failed", "err", chatErr)
		messages = nil
	}
	if len(files) == 0 && len(messages) == 0 {
		return
	}

	paths, err := service.EnsureMeetingDir(token, projectID, projectPath, note.Title, startedAt)
	if err != nil {
		slog.Warn("liveshare: meeting archive folder unavailable", "err", err)
		return
	}
	if len(files) > 0 {
		if _, err := service.ArchiveLiveNoteFiles(paths, projectPath, files, time.UnixMilli(startedAt)); err != nil {
			slog.Warn("liveshare: archive meeting files failed", "err", err)
		}
	}
	if len(messages) > 0 {
		if _, err := service.WriteLiveNoteChatFile(paths, messages); err != nil {
			slog.Warn("liveshare: write chat archive failed", "err", err)
		}
	}
}

// ── Idle-room reaper ────────────────────────────────────────────────────────

// liveReapInterval is how often the reaper scans. Rooms are reaped after
// liveshare.DefaultIdleTimeout (30 min) with no viewers, so a minute of slack is
// irrelevant to the outcome but keeps the scan cheap.
const liveReapInterval = time.Minute

// liveMeetingSweepInterval is how often stale meeting records are closed.
// Meeting rows are permanent; the only thing swept is the END time of a record
// whose publisher vanished without a clean close (crash, killed app). Without
// it the list would show a meeting that never finishes.
const liveMeetingSweepInterval = time.Hour

// liveMeetingStaleAfter is how long a record may stay open before the sweep
// closes it. Long enough that a genuinely running meeting is never touched.
const liveMeetingStaleAfter = 12 * time.Hour

// StartLiveShareReaper drops rooms that have had no viewers past the idle
// timeout. Without it a room whose publisher vanished without a clean close
// would occupy one of the maxLiveRooms slots forever.
//
// It also closes the meeting records the PREVIOUS process left open, before the
// loop starts: rooms live only in memory, so at this instant no open record can
// belong to a running meeting. That has to happen here rather than on the
// hourly tick — waiting for the 12h staleness cutoff leaves a crashed meeting
// showing "进行中" for most of a day.
//
// Returns a stop function for tests and shutdown.
func StartLiveShareReaper() func() {
	if closed, err := service.CloseOrphanedLiveNotes(time.Now().UnixMilli()); err != nil {
		slog.Warn("liveshare: close orphaned meetings failed", "err", err)
	} else if closed > 0 {
		slog.Info("liveshare: closed meetings orphaned by the previous run", "meetings", closed)
	}
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(liveReapInterval)
		defer t.Stop()
		// Closing stale meetings runs on its own, much slower cadence: the scan
		// is a table sweep and nothing about it is time-critical.
		lastMeetingSweep := time.Now()
		for {
			select {
			case <-stop:
				return
			case now := <-t.C:
				for _, room := range liveRegistry.Reap(now) {
					// Tell any stragglers before the room disappears, so they see
					// "share ended" rather than a bare socket close.
					room.BroadcastToViewers(liveshare.ViewerMessage{
						Type:   liveshare.MsgPublisherGone,
						Reason: "expired",
					}, "")
					room.SetPublisher(nil)
					slog.Info("liveshare: reaped idle room", "token", room.Token())
				}
				if now.Sub(lastMeetingSweep) >= liveMeetingSweepInterval {
					lastMeetingSweep = now
					closed, err := service.CloseStaleLiveNotes(now.Add(-liveMeetingStaleAfter).UnixMilli())
					if err != nil {
						slog.Warn("liveshare: close stale meetings failed", "err", err)
					} else if closed > 0 {
						slog.Info("liveshare: closed stale meetings", "meetings", closed)
					}
				}
			}
		}
	}()
	return func() { close(stop) }
}
