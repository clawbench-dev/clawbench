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
	"sync"
	"time"

	"clawbench/internal/liveshare"
	"clawbench/internal/model"
	"clawbench/internal/service"

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

// liveShareManageResponse is the management endpoint's payload.
type liveShareManageResponse struct {
	Token string `json:"token,omitempty"`
	Path  string `json:"path,omitempty"`
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

// liveShareCreateBody derives the share title and the AI 听记 flag from the
// create request. The title is author-supplied text that ends up on a public
// page, so it is only ever rendered as text (never as HTML) by the SPA.
func liveShareCreateBody(r *http.Request, projectPath string) (title string, notes bool) {
	var body struct {
		Title string `json:"title"`
		Notes bool   `json:"notes"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	title = body.Title
	if title != "" {
		title = truncateRunes(title, 80)
	} else if base := lastPathSegment(projectPath); base != "" {
		title = base
	} else {
		title = "ClawBench"
	}
	return title, body.Notes
}

func serveLiveShareCreate(w http.ResponseWriter, r *http.Request, projectPath string) {
	title, notes := liveShareCreateBody(r, projectPath)
	room, err := liveRegistry.Create(projectPath, title)
	if err != nil {
		if errors.Is(err, liveshare.ErrTooManyRooms) {
			writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "LiveShareTooManyRooms")
			return
		}
		slog.Error("liveshare: create room failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	// Set on every create, not just the first: the room is idempotent per
	// project, so a retry (or a second device) may be the one that knows
	// whether 听记 is on.
	room.SetNotesEnabled(notes)

	writeJSON(w, http.StatusOK, liveShareManageResponse{
		Token: room.Token(),
		Path:  "/share/" + room.Token(),
	})
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
		// one from the management call, so this is informational only.
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
		room.SetTalkEnabled(ctl.Enabled)
		// Tell EVERY viewer the switch changed, in both directions. A viewer
		// that joined while the switch was off has no other way to learn it was
		// enabled later — its `joined` frame is long gone, so without this the
		// microphone toggle would never appear.
		for _, v := range room.Viewers() {
			if ctl.Enabled {
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

	case liveshare.CtrlSetScreenEnabled:
		applySetScreenEnabled(room, ctl.Enabled)
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

	conn, err := acceptLiveWS(w, r)
	if err != nil {
		slog.Error("liveshare: signal accept failed", "err", err)
		return
	}
	defer func() { _ = conn.CloseNow() }()

	var writeMu sync.Mutex
	sink := &liveSink{conn: conn, mu: &writeMu}

	viewer, err := room.AddViewer("", sink)
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
	})
	if err := sink.Send(liveshare.ViewerMessage{
		Type:          liveshare.MsgJoined,
		ViewerID:      viewer.ID,
		Title:         room.Title(),
		ICEServers:    []any{}, // reserved for a future TURN; see signal.go
		TalkEnabled:   room.TalkEnabled(),
		NotesEnabled:  room.NotesEnabled(),
		ScreenEnabled: room.ScreenEnabled(),
		ScreenSharer:  room.ScreenSharer(),
	}); err != nil {
		return
	}

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
			// does nothing". The viewer flips its switch back to off on this.
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
		broadcastScreenState(room)

	case liveshare.CtrlScreenStop:
		if room.ReleaseScreen(viewer.ID) {
			broadcastScreenState(room)
		}
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

// notifyTalkState tells the publisher that a viewer started or stopped speaking.
func notifyTalkState(room *liveshare.Room, viewerID string, talking bool) {
	room.SendToPublisher(liveshare.PublisherMessage{
		Type:     liveshare.MsgTalkState,
		ViewerID: viewerID,
		Talking:  talking,
	})
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

// lastPathSegment returns the final component of a slash-separated path,
// handling both POSIX and Windows separators (the project path arrives from a
// cookie and may be either).
func lastPathSegment(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}

// liveShareRoomCountForTest exposes the registry size to tests without
// exporting the registry itself.
func liveShareRoomCountForTest() int {
	return liveRegistry.Len()
}

// ── Idle-room reaper ────────────────────────────────────────────────────────

// liveReapInterval is how often the reaper scans. Rooms are reaped after
// liveshare.DefaultIdleTimeout (30 min) with no viewers, so a minute of slack is
// irrelevant to the outcome but keeps the scan cheap.
const liveReapInterval = time.Minute

// liveNotesCleanupInterval is how often expired transcripts are swept. Notes are
// kept for liveNotesRetention after their last update; an hourly sweep is
// plenty for a 7-day retention window.
const (
	liveNotesCleanupInterval = time.Hour
	liveNotesRetention       = 7 * 24 * time.Hour
)

// StartLiveShareReaper drops rooms that have had no viewers past the idle
// timeout. Without it a room whose publisher vanished without a clean close
// would occupy one of the maxLiveRooms slots forever.
//
// Returns a stop function for tests and shutdown.
func StartLiveShareReaper() func() {
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(liveReapInterval)
		defer t.Stop()
		// Notes retention runs on its own, much slower cadence: the scan is a
		// table sweep and nothing about it is time-critical.
		lastNotesCleanup := time.Now()
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
				if now.Sub(lastNotesCleanup) >= liveNotesCleanupInterval {
					lastNotesCleanup = now
					removed, err := service.CleanupLiveNotes(context.Background(), now.Add(-liveNotesRetention))
					if err != nil {
						slog.Warn("liveshare: notes cleanup failed", "err", err)
					} else if removed > 0 {
						slog.Info("liveshare: cleaned up expired notes", "shares", removed)
					}
				}
			}
		}
	}()
	return func() { close(stop) }
}
