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
		serveLiveShareEnd(w, r)
	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

// liveShareTitle derives the share page title from the request, falling back to
// a neutral label. The title is author-supplied text that ends up on a public
// page, so it is only ever rendered as text (never as HTML) by the SPA.
func liveShareTitle(r *http.Request, projectPath string) string {
	var body struct {
		Title string `json:"title"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if body.Title != "" {
		return truncateRunes(body.Title, 80)
	}
	if base := lastPathSegment(projectPath); base != "" {
		return base
	}
	return "ClawBench"
}

func serveLiveShareCreate(w http.ResponseWriter, r *http.Request, projectPath string) {
	// Check the ceiling before creating. An existing room for this project does
	// not count against it (it is being reused, not added).
	if liveRegistry.FindByProject(projectPath) == nil && liveRegistry.Len() >= maxLiveRooms {
		writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "LiveShareTooManyRooms")
		return
	}

	room, err := liveRegistry.Create(projectPath, liveShareTitle(r, projectPath))
	if err != nil {
		slog.Error("liveshare: create room failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}

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

func serveLiveShareEnd(w http.ResponseWriter, r *http.Request) {
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
	if room.Publisher() != nil {
		writeLocalizedErrorf(w, r, http.StatusConflict, "LiveShareAlreadyPublishing")
		return
	}

	conn, err := acceptLiveWS(w, r)
	if err != nil {
		slog.Error("liveshare: publish accept failed", "err", err)
		return
	}
	defer func() { _ = conn.CloseNow() }()

	var writeMu sync.Mutex
	sink := &liveSink{conn: conn, mu: &writeMu}

	// The publisher owns the room's lifetime: when this socket goes away the
	// share is over, and every viewer must be told rather than left hanging.
	defer func() {
		room.SetPublisher(nil)
		room.BroadcastToViewers(liveshare.ViewerMessage{Type: liveshare.MsgPublisherGone, Reason: "ended"}, "")
		liveRegistry.Delete(token)
	}()

	room.SetPublisher(sink)

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
		// Disabling revokes grants inside the room; tell the affected viewers so
		// their "hold to talk" button disappears instead of silently failing.
		if !ctl.Enabled {
			for _, v := range room.Viewers() {
				room.SendToViewer(v.ID, liveshare.ViewerMessage{
					Type:   liveshare.MsgTalkRevoked,
					Reason: liveshare.ReasonDisabled,
				})
			}
		}
		return false

	case liveshare.CtrlTalkGrant:
		if err := room.GrantTalk(ctl.ViewerID); err != nil {
			_ = sink.Send(liveshare.PublisherMessage{Type: liveshare.MsgError, ViewerID: ctl.ViewerID, Name: err.Error()})
			return false
		}
		room.SendToViewer(ctl.ViewerID, liveshare.ViewerMessage{Type: liveshare.MsgTalkGranted})
		return false

	case liveshare.CtrlTalkRevoke:
		if err := room.RevokeTalk(ctl.ViewerID); err != nil {
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
		room.RemoveViewer(viewer.ID)
		room.SendToPublisher(liveshare.PublisherMessage{
			Type:     liveshare.MsgViewerLeave,
			ViewerID: viewer.ID,
		})
	}()

	// Tell the publisher a viewer arrived so it can build a PeerConnection for
	// it, and tell the viewer its id plus whether speech is available.
	room.SendToPublisher(liveshare.PublisherMessage{
		Type:     liveshare.MsgViewerJoin,
		ViewerID: viewer.ID,
	})
	if err := sink.Send(liveshare.ViewerMessage{
		Type:        liveshare.MsgJoined,
		ViewerID:    viewer.ID,
		Title:       room.Title(),
		ICEServers:  []any{}, // reserved for a future TURN; see signal.go
		TalkEnabled: room.TalkEnabled(),
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

	case liveshare.CtrlTalkRequest:
		st, err := room.RequestTalk(viewer.ID)
		if err != nil {
			reason := "denied"
			if errors.Is(err, liveshare.ErrTalkNotAllowed) {
				reason = liveshare.ReasonDisabled
			}
			room.SendToViewer(viewer.ID, liveshare.ViewerMessage{Type: liveshare.MsgTalkDenied, Reason: reason})
			return
		}
		if st == liveshare.TalkGranted {
			// Already granted (a repeat request): no need to bother the publisher.
			room.SendToViewer(viewer.ID, liveshare.ViewerMessage{Type: liveshare.MsgTalkGranted})
			return
		}
		room.SendToPublisher(liveshare.PublisherMessage{
			Type:     liveshare.MsgTalkRequest,
			ViewerID: viewer.ID,
			Name:     viewer.Name,
		})

	case liveshare.CtrlTalkStart:
		if err := room.SetTalking(viewer.ID, true); err != nil {
			// Rejected loudly: a client that skipped the grant handshake has a
			// bug, and silence would turn it into "the button does nothing".
			room.SendToViewer(viewer.ID, liveshare.ViewerMessage{Type: liveshare.MsgTalkDenied, Reason: liveshare.ReasonNotGranted})
			return
		}
		notifyTalkState(room, viewer.ID, true)

	case liveshare.CtrlTalkEnd:
		if err := room.SetTalking(viewer.ID, false); err != nil {
			return
		}
		notifyTalkState(room, viewer.ID, false)
	}
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
