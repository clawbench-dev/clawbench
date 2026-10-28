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
	// MsgTalkDisabled means the viewer asked to speak while the room switch is
	// off. The viewer's mic must go back to off.
	MsgTalkDisabled = "talk-disabled"
	// MsgError reports a rejected control frame without closing the socket.
	MsgError = "error"
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
	// CtrlAnswer carries the publisher's SDP answer for one viewer.
	CtrlAnswer = "answer"
	// CtrlICE carries one ICE candidate (publisher → viewer direction is the
	// server's job; this constant names the frame the publisher sends).
	CtrlICE = "ice"
	// CtrlSetTalkEnabled flips the room-level speech switch.
	CtrlSetTalkEnabled = "set-talk-enabled"
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
	ICEServers  []any  `json:"iceServers"`
	TalkEnabled bool   `json:"talkEnabled,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Message     string `json:"message,omitempty"`
}

// PublisherControl is a frame received from the publisher.
type PublisherControl struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	ViewerID  string `json:"viewerId"`
	SDP       string `json:"sdp"`
	Candidate any    `json:"candidate"`
	Enabled   bool   `json:"enabled"`
}

// ViewerControl is a frame received from a viewer.
type ViewerControl struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	SDP       string `json:"sdp"`
	Candidate any    `json:"candidate"`
}
