package liveshare

// Wire protocol shared by the publisher and viewer sockets.
//
// The messages are plain structs with JSON tags. They live in this package (not
// in internal/handler) so that a standalone relay application reusing this
// package speaks the same protocol without copying field names.
//
// Nothing here carries media. SDP and ICE are opaque strings/objects that the
// server forwards verbatim — it never parses them, which keeps the server out of
// the codec-negotiation business entirely.

// Message types on the publisher socket (server → publisher).
const (
	// MsgViewerJoin tells the publisher a viewer arrived and needs an answer.
	MsgViewerJoin = "viewer-join"
	// MsgViewerLeave tells the publisher a viewer went away.
	MsgViewerLeave = "viewer-leave"
	// MsgOffer carries the viewer's SDP offer.
	MsgOffer = "offer"
	// MsgAnswer carries the publisher's SDP answer.
	MsgAnswer = "answer"
	// MsgICE carries one ICE candidate in either direction.
	MsgICE = "ice"
	// MsgTalkState reports a viewer's speech state change.
	MsgTalkState = "talk-state"
	// MsgScreenState reports who is sharing a screen, to the publisher AND to
	// every viewer. One frame for both sides keeps the two views from drifting.
	MsgScreenState = "screen-state"
)

// Message types on the viewer socket (server → viewer).
const (
	// MsgJoined is the first frame a viewer receives.
	MsgJoined = "joined"
	// MsgFull means the room is at capacity; the socket closes after it.
	MsgFull = "full"
	// MsgPublisherGone means the publisher detached; the share is over.
	MsgPublisherGone = "publisher-gone"
	// MsgTalkEnabled tells a viewer that the room-level switch changed. Without
	// it, a viewer that joined while the switch was off would never learn that
	// the publisher enabled it later (its `joined` frame is long gone).
	MsgTalkEnabled = "talk-enabled"
	// MsgTalkRevoked means the publisher muted this viewer (or the room switch
	// was turned off), so its microphone must stop.
	MsgTalkRevoked = "talk-revoked"
	// MsgTalkUnmuted lifts a per-viewer block. It is a separate frame from
	// MsgTalkEnabled because the two mean different things: this one leaves the
	// microphone switch in the viewer's hands, that one removes it entirely.
	MsgTalkUnmuted = "talk-unmuted"
	// MsgKicked means the publisher removed this viewer from the meeting. The
	// socket closes right after it; the frame is what lets the page say why.
	MsgKicked = "kicked"
	// MsgTalkDisabled means the viewer asked to speak while the room switch is
	// off. The viewer's mic must go back to off.
	MsgTalkDisabled = "talk-disabled"
	// MsgScreenDenied means the viewer asked to share its screen and the room
	// refused: the switch is off, or somebody else is already sharing.
	MsgScreenDenied = "screen-denied"
	// MsgPictureState reports the publisher's picture changing mid-meeting: a
	// screen starting or stopping, the camera turning on or off. One frame for
	// all three fields because they always change together from the publisher's
	// point of view, and because a viewer that applied them separately could
	// briefly render a layout that never existed.
	MsgPictureState = "picture-state"
	// MsgControlEnabled tells a viewer whether it may ask for remote control of
	// the publisher. Sent at join and on every change: the capability belongs to
	// the publisher (input injector + a whole-screen picture), so a viewer
	// cannot work it out and must not offer a button that can only be refused.
	MsgControlEnabled = "control-enabled"
	// MsgError reports a rejected control frame without closing the socket.
	MsgError = "error"
	// MsgRoster carries the FULL participant list: the publisher plus every
	// viewer. Sent to every viewer whenever somebody joins, leaves, speaks or
	// is muted. A full snapshot rather than join/leave deltas because the list
	// is at most nine entries: one frame cannot drift, and a viewer that missed
	// a delta would otherwise show a stale list until it reloaded.
	MsgRoster = "roster"
)

// Publisher control messages (client → server).
const (
	// CtrlPublish is the publisher's first frame.
	CtrlPublish = "publish"
	// CtrlEnd ends the share and disconnects every viewer.
	CtrlEnd = "end"
	// CtrlTalkRevoke silences one viewer.
	CtrlTalkRevoke = "talk-revoke"
	// CtrlTalkMuteAll revokes every grant at once.
	CtrlTalkMuteAll = "talk-mute-all"
	// CtrlTalkMute blocks (Enabled=true) or unblocks one viewer's microphone.
	// Distinct from CtrlTalkRevoke: that one silences the microphone for now,
	// this one refuses to let it come back on.
	CtrlTalkMute = "talk-mute"
	// CtrlKick removes one viewer from the room and hangs up its socket.
	CtrlKick = "kick"
	// CtrlAnswer carries the publisher's SDP answer for one viewer.
	CtrlAnswer = "answer"
	// CtrlICE carries one ICE candidate (publisher → viewer direction is the
	// server's job; this constant names the frame the publisher sends).
	CtrlICE = "ice"
	// CtrlSetTalkEnabled flips the room-level speech switch.
	CtrlSetTalkEnabled = "set-talk-enabled"
	// CtrlSetScreenEnabled flips the room-level screen-sharing switch. Turning
	// it off also stops whoever is sharing.
	CtrlSetScreenEnabled = "set-screen-enabled"
	// CtrlSetPicture replaces the room's picture state (screen / camera /
	// whether there is one at all). The publisher is the only writer, so the
	// three flags always travel together — sending them one at a time would let
	// a viewer render a combination that never happened.
	CtrlSetPicture = "set-picture"
	// CtrlScreenRevoke stops one viewer's screen share without disabling the
	// switch for everyone else.
	CtrlScreenRevoke = "screen-revoke"
	// CtrlSetControlEnabled advertises whether this publisher can accept
	// remote-control requests: it needs the input injector AND a whole-screen
	// picture. Viewers grey their "request control" button on it.
	CtrlSetControlEnabled = "set-control-enabled"
)

// Viewer control messages (client → server).
const (
	// CtrlJoin is the viewer's first frame.
	CtrlJoin = "join"
	// CtrlTalkStart reports that the viewer turned its microphone on.
	CtrlTalkStart = "talk-start"
	// CtrlTalkEnd reports that the viewer stopped speaking.
	CtrlTalkEnd = "talk-end"
	// CtrlOffer carries the viewer's SDP offer.
	CtrlOffer = "offer"
	// CtrlScreenStart announces that the viewer's screen track is live.
	CtrlScreenStart = "screen-start"
	// CtrlScreenStop announces that the viewer stopped sharing.
	CtrlScreenStop = "screen-stop"
	// CtrlControlRequest reports that the viewer asked the publisher for remote
	// control. The REQUEST itself still travels over the P2P control channel
	// (`ctrl-req`), which is what the publisher's UI answers — it must arrive
	// without a server round trip. This frame is the server's copy: it changes
	// no room state, it exists only so a notification can be raised for a host
	// who is not looking at the meeting page. Without it the server never sees
	// the request at all, and the host learns about it only if they happen to
	// be on the meeting tab.
	CtrlControlRequest = "control-request"
)

// PublisherMessage is a frame sent to the publisher.
//
// Every field is omitempty except Type, so each message type serializes to only
// the fields it means — a talk-state frame must not carry an empty SDP.
type PublisherMessage struct {
	Type     string `json:"type"`
	ViewerID string `json:"viewerId,omitempty"`
	Name     string `json:"name,omitempty"`
	SDP      string `json:"sdp,omitempty"`
	// Candidate is the raw RTCIceCandidateInit JSON. Forwarded verbatim; the
	// server never inspects it (its shape is the browser's, not ours).
	Candidate any `json:"candidate,omitempty"`
	// Talking is meaningful only on MsgTalkState.
	Talking bool `json:"talking,omitempty"`
	// Muted is meaningful only on MsgTalkState: whether the publisher has
	// blocked this viewer's microphone. It travels with Talking because the two
	// always change together when the host mutes somebody (the block goes on
	// and the microphone goes off), and a publisher list that applied them
	// separately could briefly show "speaking" next to "muted".
	//
	// A pointer, not a bool: `false` is the meaningful answer ("the block was
	// lifted") and must actually reach the wire, so it cannot be omitempty. A
	// nil pointer keeps the "only the fields this frame means" property for
	// every other message type.
	Muted *bool `json:"muted,omitempty"`
	// Sharing is meaningful only on MsgScreenState: whether ViewerID is
	// currently sharing a screen.
	Sharing bool `json:"sharing,omitempty"`
	// ScreenEnabled is the room-level screen-sharing switch.
	ScreenEnabled bool `json:"screenEnabled,omitempty"`
}

// Reasons carried on talk-revoked / talk-denied. Constants (not literals) so the
// handler and the tests cannot drift apart on spelling.
const (
	// ReasonDisabled means the room-level audience-speech switch is off.
	ReasonDisabled = "disabled"
	// ReasonRevoked means the publisher withdrew this viewer's approval.
	ReasonRevoked = "revoked"
	// ReasonMuted means a mute-all cleared every approval.
	ReasonMuted = "muted"
	// ReasonHostMuted means the publisher blocked this one viewer. The viewer
	// keeps its microphone switch, but turning it on is refused until the host
	// lifts the block (talk-unmuted).
	ReasonHostMuted = "host"
	// ReasonBusy means somebody else is already sharing a screen. Only one
	// sharer at a time: every forwarded screen is another stream the publisher
	// must upload to every viewer.
	ReasonBusy = "busy"
)

// ViewerMessage is a frame sent to a viewer.
type ViewerMessage struct {
	Type      string `json:"type"`
	ViewerID  string `json:"viewerId,omitempty"`
	Title     string `json:"title,omitempty"`
	SDP       string `json:"sdp,omitempty"`
	Candidate any    `json:"candidate,omitempty"`
	// ICEServers is reserved for a future TURN/STUN addition. Always empty
	// today; the viewer already passes it straight to RTCPeerConnection, so
	// enabling relay later needs no client change.
	ICEServers  []any `json:"iceServers"`
	TalkEnabled bool  `json:"talkEnabled,omitempty"`
	// VideoEnabled tells the viewer whether a picture is coming RIGHT NOW.
	// Meetings have no type: a picture can start or stop at any moment, and
	// MsgPictureState carries every change after `joined`. Without a picture the
	// page renders a placeholder instead of waiting for a track that may never
	// arrive. Pointer-to-bool would be needed to distinguish "absent" from
	// "false"; both mean "no picture", which is the same UI.
	VideoEnabled bool `json:"video"`
	// CameraEnabled tells the viewer that the publisher's camera is on, so the
	// camera tile can be offered at all. It is independent of VideoEnabled: a
	// screen-only share has a picture but no camera.
	CameraEnabled bool `json:"camera"`
	// HostScreen tells the viewer that the publisher's picture IS a screen.
	// Remote control and annotation only make sense then — injecting pointer
	// coordinates into a camera image means nothing. It changes mid-meeting
	// whenever the host switches between a screen and a camera.
	HostScreen bool `json:"hostScreen"`
	// NotesEnabled tells the viewer that this share is running AI 听记, so the
	// transcript panel can be offered at all. It carries no transcript.
	NotesEnabled bool `json:"notesEnabled,omitempty"`
	// ControlEnabled tells the viewer that the publisher can be driven right
	// now (input injector present, whole screen shared). Absent means "cannot":
	// offering a request the host can only refuse is the bug this prevents.
	ControlEnabled bool `json:"controlEnabled"`
	// ScreenEnabled / ScreenSharer are the screen-sharing state at join time.
	// Later changes arrive as MsgScreenState.
	ScreenEnabled bool   `json:"screenEnabled,omitempty"`
	ScreenSharer  string `json:"screenSharer,omitempty"`
	// Sharing is meaningful only on MsgScreenState.
	Sharing bool   `json:"sharing,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
	// Roster is meaningful only on MsgRoster.
	Roster []RosterEntry `json:"roster,omitempty"`
}

// RosterEntry is one participant as the audience sees them.
//
// Deliberately not a *Viewer: this is a value snapshot copied under the room
// lock, so a frame cannot be mutated by a later state change while it is being
// serialized.
type RosterEntry struct {
	// ID is "host" for the publisher and the room's viewer id otherwise.
	ID string `json:"id"`
	// Name is the nickname the server accepted at join. Empty for the publisher
	// when its client sent none; the page then shows the role label instead.
	Name string `json:"name,omitempty"`
	// Host marks the publisher row so the list can pin it and label the role.
	Host bool `json:"host,omitempty"`
	// Talking is that viewer's microphone state.
	Talking bool `json:"talking,omitempty"`
	// Muted is the publisher's per-viewer block.
	Muted bool `json:"muted,omitempty"`
}

// PublisherControl is a frame received from the publisher.
type PublisherControl struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	// Name is the publisher's nickname, meaningful only on CtrlPublish. It is
	// what the audience's member list shows for the host row; absent means the
	// client did not send one and the page falls back to the role label.
	Name      string `json:"name"`
	ViewerID  string `json:"viewerId"`
	SDP       string `json:"sdp"`
	Candidate any    `json:"candidate"`
	Enabled   bool   `json:"enabled"`
	// Video / Camera / Screen are meaningful only on CtrlSetPicture: whether a
	// picture is being sent, whether it contains a camera tile, and whether it
	// is a screen (as opposed to a camera-only picture).
	Video  bool `json:"video"`
	Camera bool `json:"camera"`
	Screen bool `json:"screen"`
}

// ViewerControl is a frame received from a viewer.
type ViewerControl struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	SDP       string `json:"sdp"`
	Candidate any    `json:"candidate"`
}
