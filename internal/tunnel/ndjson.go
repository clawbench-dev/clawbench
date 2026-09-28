package tunnel

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Control message types for the -R control stream (design doc §4.4). The
// control plane is deliberately tiny and low-frequency, so it travels as
// newline-delimited JSON rather than a binary framing protocol.
const (
	// MsgBind is client -> server: "listen on this server-side port".
	MsgBind = "bind"
	// MsgBound is server -> client: the bind succeeded; port is the actual one.
	MsgBound = "bound"
	// MsgBindErr is server -> client: the bind failed; code/msg explain why.
	MsgBindErr = "bind_err"
	// MsgIncoming is server -> client: a connection arrived on a bound port;
	// token is the single-use claim credential for it.
	MsgIncoming = "incoming"
	// MsgUnbind is client -> server: release a previously bound port.
	MsgUnbind = "unbind"
	// MsgUnbound is server -> client: the release completed.
	MsgUnbound = "unbound"
	// MsgPing is client -> server keepalive.
	MsgPing = "ping"
	// MsgPong is server -> client keepalive reply.
	MsgPong = "pong"
)

// bind_err codes, aligned with the HTTP status mapping in design doc §4.6.
const (
	// BindErrNotAllowed corresponds to 403: the port is outside allowed_ports.
	BindErrNotAllowed = 2
	// BindErrReservedOrTaken corresponds to 409: the port is reserved for
	// ClawBench itself or already bound by another control stream. A port held
	// by an unrelated process is NOT this code: cross-platform errno handling
	// is unreliable, so it is reported as BindErrListenFailed instead (see
	// ListenReverse in bind.go).
	BindErrReservedOrTaken = 3
	// BindErrListenFailed is a bind failure that is not "address in use"
	// (e.g. permission denied).
	BindErrListenFailed = 4
	// BindErrInternal is an unexpected server-side failure.
	BindErrInternal = 6
)

// ErrMalformedControl is returned for a control line that is not valid JSON, is
// missing a type, or carries an unknown type. It is a distinct sentinel so the
// handler can decide whether to keep the stream alive (unknown/malformed lines
// are dropped) rather than treating it as a transport failure.
var ErrMalformedControl = errors.New("malformed control message")

// ControlMessage is one NDJSON line on the -R control stream.
//
// Every field except Type is optional, so a single struct covers all eight
// message shapes. Port is omitted when zero: for `bind` a missing port is the
// documented "let the OS choose" request, and for `ping`/`pong` there is no
// port at all.
type ControlMessage struct {
	Type  string `json:"type"`
	Port  int    `json:"port,omitempty"`
	Code  int    `json:"code,omitempty"`
	Msg   string `json:"msg,omitempty"`
	Token string `json:"token,omitempty"`
}

// DecodeControl parses one control line. The line may carry a trailing newline
// (and CR) or not. Anything that is not a well-formed known message is an
// error wrapping ErrMalformedControl — never a panic.
func DecodeControl(line []byte) (ControlMessage, error) {
	trimmed := trimControlLine(line)
	if len(trimmed) == 0 {
		return ControlMessage{}, fmt.Errorf("%w: empty line", ErrMalformedControl)
	}

	var m ControlMessage
	if err := json.Unmarshal(trimmed, &m); err != nil {
		return ControlMessage{}, fmt.Errorf("%w: %w", ErrMalformedControl, err)
	}
	if !knownControlType(m.Type) {
		return ControlMessage{}, fmt.Errorf("%w: unknown type %q", ErrMalformedControl, m.Type)
	}
	return m, nil
}

// EncodeControl renders a message as one NDJSON line, newline included.
//
// json.Marshal cannot fail for ControlMessage (every field is a string or int),
// so the error is intentionally discarded rather than propagated into every
// call site.
func EncodeControl(m ControlMessage) []byte {
	b, err := json.Marshal(m)
	if err != nil {
		// Unreachable; returning nil would make the writer close the stream,
		// which is the only safe reaction to an unencodable message anyway.
		return nil
	}
	return append(b, '\n')
}

// knownControlType reports whether t is one of the eight protocol types.
func knownControlType(t string) bool {
	switch t {
	case MsgBind, MsgBound, MsgBindErr, MsgIncoming, MsgUnbind, MsgUnbound, MsgPing, MsgPong:
		return true
	default:
		return false
	}
}

// trimControlLine strips a trailing "\n" and an optional preceding "\r".
func trimControlLine(line []byte) []byte {
	if n := len(line); n > 0 && line[n-1] == '\n' {
		line = line[:n-1]
	}
	if n := len(line); n > 0 && line[n-1] == '\r' {
		line = line[:n-1]
	}
	return line
}
