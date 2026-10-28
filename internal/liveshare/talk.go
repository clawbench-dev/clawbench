package liveshare

// Speech (audience → publisher) state machine.
//
// The transitions are deliberately few, and every rejected transition is an
// ERROR rather than a silent no-op: a client that sends talk-start without a
// grant has a bug, and swallowing it turns "I pressed the button and nothing
// happened" into an undiagnosable report.

// TalkState is a viewer's speech state.
type TalkState int

const (
	// TalkNone means the viewer has not asked to speak.
	TalkNone TalkState = iota
	// TalkPending means the viewer asked and the publisher has not answered.
	TalkPending
	// TalkGranted means the publisher approved; the viewer may speak.
	TalkGranted
	// TalkTalking means the viewer is speaking right now.
	TalkTalking
)

// String makes test failures readable.
func (s TalkState) String() string {
	switch s {
	case TalkNone:
		return "none"
	case TalkPending:
		return "pending"
	case TalkGranted:
		return "granted"
	case TalkTalking:
		return "talking"
	default:
		return "unknown"
	}
}

// talkState derives the state from the flags on Viewer. The two booleans are the
// storage; TalkPending needs its own bit because "asked but not answered" cannot
// be expressed by canTalk/talking alone.
func (v *Viewer) talkState() TalkState {
	switch {
	case v.talking:
		return TalkTalking
	case v.canTalk:
		return TalkGranted
	case v.pending:
		return TalkPending
	default:
		return TalkNone
	}
}

// TalkState reports the viewer's current speech state.
func (v *Viewer) TalkState() TalkState {
	return v.talkState()
}

// RequestTalk records a pending request and reports the new state.
//
// Requires the room-level switch. A viewer may re-request while pending (the
// UI retries on a flaky connection) — that is idempotent, not an error.
func (r *Room) RequestTalk(viewerID string) (TalkState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.talkEnabled {
		return TalkNone, ErrTalkNotAllowed
	}
	v := r.viewers[viewerID]
	if v == nil {
		return TalkNone, ErrRoomNotFound
	}
	if v.canTalk {
		return TalkGranted, nil
	}
	v.pending = true
	return TalkPending, nil
}

// GrantTalk approves a viewer's request. Granting an already-granted viewer is
// idempotent (the publisher may double-click).
func (r *Room) GrantTalk(viewerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	v := r.viewers[viewerID]
	if v == nil {
		return ErrRoomNotFound
	}
	if !r.talkEnabled {
		return ErrTalkNotAllowed
	}
	v.pending = false
	v.canTalk = true
	return nil
}

// RevokeTalk withdraws a grant and stops any speech in progress.
func (r *Room) RevokeTalk(viewerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	v := r.viewers[viewerID]
	if v == nil {
		return ErrRoomNotFound
	}
	v.pending = false
	v.canTalk = false
	v.talking = false
	return nil
}

// MuteAll revokes every grant. Returns the ids that were affected so the caller
// can notify exactly those viewers.
func (r *Room) MuteAll() []string {
	r.mu.Lock()
	var affected []string
	for id, v := range r.viewers {
		if v.canTalk || v.talking || v.pending {
			affected = append(affected, id)
		}
		v.canTalk = false
		v.talking = false
		v.pending = false
	}
	r.mu.Unlock()
	return affected
}

// SetTalking marks a viewer as speaking or silent.
//
// Starting requires an outstanding grant; stopping never does (a viewer whose
// grant was revoked mid-sentence must still be able to send talk-end).
func (r *Room) SetTalking(viewerID string, talking bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	v := r.viewers[viewerID]
	if v == nil {
		return ErrRoomNotFound
	}
	if talking && !v.canTalk {
		return ErrTalkNotGranted
	}
	v.talking = talking
	return nil
}
