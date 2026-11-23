package agentcore

import "sync"

// PendingInjection is one user message that arrived while a turn was running
// and is waiting for the turn's next model request.
type PendingInjection struct {
	// ClientUserMessageID is the caller's id for the message (the chat queue id).
	// It travels back on the boundary event so the service layer can cut the
	// assistant reply at the exact point the message entered the conversation.
	ClientUserMessageID string
	// Text is the complete message the model must see. Attachments are already
	// rendered into it as path hints by the caller, exactly as a normal turn.
	Text string
}

// InjectionMailbox carries mid-turn messages from a backend's injection policy
// into a running turn.
//
// The turn loop reads it at the only point where a new message can actually
// change anything: between model rounds. A message that arrives while the model
// is streaming is therefore answered by the NEXT round (the current reply is
// already on the wire and cannot be rewritten) — the boundary event marks that
// split.
//
// Sealing is what makes acceptance honest. The policy may only report "injected"
// if the turn is still going to read the mailbox, so the terminal path seals and
// takes atomically: a Push either lands in the batch that earns another round, or
// it is refused and the caller keeps the message queued. There is no window in
// which a message is accepted by a turn that then ends without answering it.
type InjectionMailbox struct {
	mu      sync.Mutex
	pending []PendingInjection
	sealed  bool
}

// NewInjectionMailbox returns an open mailbox for one turn.
func NewInjectionMailbox() *InjectionMailbox {
	return &InjectionMailbox{}
}

// Push accepts a message for the next model-request boundary.
//
// It reports false when the turn has already passed its last boundary (sealed)
// or when the mailbox belongs to a finished turn. A false result is not an
// error: the caller falls back to normal queueing, so the message is delivered
// by its own turn instead.
func (m *InjectionMailbox) Push(p PendingInjection) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sealed {
		return false
	}
	m.pending = append(m.pending, p)
	return true
}

// Pop removes and returns the oldest waiting message.
//
// Messages are delivered ONE per model round on purpose. Absorbing a batch
// would emit several boundaries back to back with no model output between them,
// and the caller splits the reply at each boundary — leaving an empty assistant
// row between two consecutive user bubbles.
func (m *InjectionMailbox) Pop() (PendingInjection, bool) {
	if m == nil {
		return PendingInjection{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.pending) == 0 {
		return PendingInjection{}, false
	}
	p := m.pending[0]
	m.pending = m.pending[1:]
	return p, true
}

// PopOrSeal either removes and returns the oldest waiting message (mailbox
// stays open, the caller must run a round for it) or, when none are waiting,
// seals the mailbox and reports sealed=true so the caller can end the turn.
//
// The two outcomes are decided under one lock on purpose: that is what makes
// "accepted" mean "will be answered".
func (m *InjectionMailbox) PopOrSeal() (pending PendingInjection, ok bool, sealed bool) {
	if m == nil {
		return PendingInjection{}, false, true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.pending) > 0 {
		p := m.pending[0]
		m.pending = m.pending[1:]
		return p, true, false
	}
	m.sealed = true
	return PendingInjection{}, false, true
}

// Seal closes the mailbox and returns anything that arrived before the close.
//
// Used by the paths that cannot run another round (cancellation, failure): the
// messages are already materialized and announced, so they stay in history
// unanswered — the caller logs them rather than pretending they were consumed.
func (m *InjectionMailbox) Seal() []PendingInjection {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sealed = true
	out := m.pending
	m.pending = nil
	return out
}
