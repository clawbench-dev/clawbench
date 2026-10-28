package liveshare

// Speech (audience → publisher) state machine.
//
// Deliberately simple: the publisher owns ONE room-level switch, and each viewer
// owns ONE microphone toggle. There is no per-viewer approval handshake — a
// viewer whose mic is on may speak, and the publisher can silence the whole room
// at any moment.
//
// The earlier design had request/grant/deny per viewer. It was removed because
// the extra step bought nothing the room switch did not already provide: the
// switch is the security boundary (it is off by default, and the token only
// becomes a write credential once the publisher turns it on), and a second
// per-person confirmation on top of it was friction without a threat model.
//
// Every rejected transition is an ERROR rather than a silent no-op: a client
// that turns its mic on in a room with speech disabled has a bug, and swallowing
// it turns "the button does nothing" into an undiagnosable report.

// TalkState is a viewer's speech state.
type TalkState int

const (
	// TalkNone means the viewer's microphone is off.
	TalkNone TalkState = iota
	// TalkTalking means the viewer's microphone is on and audio is flowing.
	TalkTalking
)

// String makes test failures readable.
func (s TalkState) String() string {
	switch s {
	case TalkNone:
		return "none"
	case TalkTalking:
		return "talking"
	default:
		return "unknown"
	}
}

// TalkState reports the viewer's current speech state.
//
// The flag is written under the room's mutex, so reading it here needs the same
// lock. Callers that already hold it must use talkStateLocked instead.
func (v *Viewer) TalkState() TalkState {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.talkStateLocked()
}

// talkStateLocked is TalkState for callers already holding v.mu.
func (v *Viewer) talkStateLocked() TalkState {
	if v.talking {
		return TalkTalking
	}
	return TalkNone
}

// SetTalking turns one viewer's microphone on or off.
//
// Turning it on requires the room-level switch: that is the whole authorisation
// model. Turning it off is always allowed, so a viewer can silence itself even
// after the publisher disabled the room switch.
func (r *Room) SetTalking(viewerID string, talking bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	v := r.viewers[viewerID]
	if v == nil {
		return ErrRoomNotFound
	}
	if talking && !r.talkEnabled {
		return ErrTalkNotAllowed
	}
	v.mu.Lock()
	v.talking = talking
	v.mu.Unlock()
	return nil
}

// MuteAll turns every viewer's microphone off. Returns the ids that were
// actually speaking so the caller can notify exactly those viewers.
func (r *Room) MuteAll() []string {
	r.mu.Lock()
	var affected []string
	for id, v := range r.viewers {
		v.mu.Lock()
		if v.talking {
			affected = append(affected, id)
		}
		v.talking = false
		v.mu.Unlock()
	}
	r.mu.Unlock()
	return affected
}
