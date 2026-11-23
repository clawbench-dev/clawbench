package builtin

import (
	"context"
	"sync"

	"clawbench/internal/agentcore"
	"clawbench/internal/ai"
)

// ---------------------------------------------------------------------------
// Mid-turn injection (in-process)
// ---------------------------------------------------------------------------
//
// The built-in agent's turn loop lives in this process, so "join the running
// turn" needs no ACP extension: the running turn is a goroutine that owns an
// agentcore.InjectionMailbox, and this policy drops the message into it. The
// engine reads the mailbox at the next model-request boundary and emits a
// boundary event, which the service layer uses to split the reply into a
// "before" and an "after" half — the same shape CodeBuddy produces with
// `session/steer`.
//
// The registry is keyed by session id and holds at most ONE turn per session
// (the session runner already guarantees that). The mailbox is registered
// before the first model request and removed when the turn ends; a message that
// arrives after the mailbox is sealed is declined and stays queued, so a click
// on "insert" can never lose it.

// injectionRegistry maps a chat session to the mailbox of its running turn.
type injectionRegistry struct {
	mu    sync.Mutex
	turns map[string]*agentcore.InjectionMailbox
}

var activeInjections = &injectionRegistry{turns: map[string]*agentcore.InjectionMailbox{}}

// registerInjectionBox publishes box as the join target for sessionID and
// returns the cleanup to run when the turn ends.
func registerInjectionBox(sessionID string, box *agentcore.InjectionMailbox) func() {
	activeInjections.mu.Lock()
	activeInjections.turns[sessionID] = box
	activeInjections.mu.Unlock()
	return func() {
		activeInjections.mu.Lock()
		// Compare before deleting: a retry can register its own mailbox for the
		// same session before this turn's cleanup runs, and the older turn must
		// not remove the newer one.
		if activeInjections.turns[sessionID] == box {
			delete(activeInjections.turns, sessionID)
		}
		activeInjections.mu.Unlock()
	}
}

// injectionBoxFor returns the mailbox of the session's running turn, or nil.
func injectionBoxFor(sessionID string) *agentcore.InjectionMailbox {
	activeInjections.mu.Lock()
	defer activeInjections.mu.Unlock()
	return activeInjections.turns[sessionID]
}

// midTurn is the in-process policy. It implements
// ai.InProcessMidTurnInjector; the ACP-shaped ai.MidTurnInjector stays unused
// because there is no connection to hand it.
type midTurn struct{}

// InjectInProcess places req.Content into the session's running turn.
//
// A decline (Injected=false) is not an error: no turn is running, or the turn
// already passed its last model request. The caller puts the message back into
// the queue and its own turn delivers it.
func (midTurn) InjectInProcess(_ context.Context, req ai.MidTurnInjectRequest) (ai.MidTurnInjectResult, error) {
	box := injectionBoxFor(req.SessionID)
	if box == nil {
		return ai.MidTurnInjectResult{Reason: "idle"}, nil
	}
	if !box.Push(agentcore.PendingInjection{
		ClientUserMessageID: req.ClientUserMessageID,
		Text:                req.Content,
	}) {
		// The turn is finishing; it will not read the mailbox again.
		return ai.MidTurnInjectResult{Reason: "stale"}, nil
	}
	return ai.MidTurnInjectResult{Injected: true}, nil
}
