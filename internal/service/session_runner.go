package service

import (
	"context"
	"sync"
)

// session_runner.go owns the single source of truth for "is this session
// running, and how do I stop it".
//
// That state used to live in two independent containers — a map[string]bool
// guarded by one mutex, and a sync.Map of cancel funcs — mutated in separate
// critical sections. The split made a "running with nothing to cancel" state
// representable, which CancelSession had to work around with a force-clear
// branch, and it left the scheduler unable to register a cancel at all (its
// executions lived only in the scheduler's own map, so a user cancel fell
// through to the force-clear path).
//
// Here a session's running state and its cancel func are ONE entry, created
// atomically. "Running" is therefore derived, not separately maintained, and
// the inconsistent state cannot be constructed.

// sessionRunner is the registry entry for one running session.
type sessionRunner struct {
	sessionID string
	ctx       context.Context
	cancel    context.CancelFunc

	// turn is the CURRENT turn's registration, or nil between turns.
	//
	// The runner's own context spans every turn it runs (that is what makes a
	// user cancel stop the whole execution), so stopping just one turn needs a
	// narrower handle. "Interrupt and send" uses this: it must stop the reply in
	// flight while leaving the runner alive to deliver the next queued message.
	turn *sessionTurn

	// wake records that work arrived while the runner was between turns. It is
	// written by submitRunner and re-read by retireRunner, both under
	// registryMu. That is what closes the exit race: without it, a submit that
	// lands between the runner's empty-queue check and its removal would be
	// stranded with nobody left to consume it — exactly the "message never gets
	// an answer until I cancel and re-send" symptom.
	wake bool
}

var (
	registryMu sync.Mutex
	registry   = map[string]*sessionRunner{}
)

// submitRunner registers the intent to run work for a session.
//
// If a runner already exists it is woken and reused (created=false). Otherwise a
// new runner — owning a fresh context — is created (created=true) and the caller
// is responsible for starting exactly one execution goroutine. Creation and the
// existence check happen under one lock, so two concurrent submitters can never
// both be told they created it.
func submitRunner(sessionID string) (ctx context.Context, created bool) {
	registryMu.Lock()
	defer registryMu.Unlock()

	if r, ok := registry[sessionID]; ok {
		r.wake = true
		return r.ctx, false
	}
	c, cancel := context.WithCancel(context.Background())
	registry[sessionID] = &sessionRunner{sessionID: sessionID, ctx: c, cancel: cancel}
	return c, true
}

// adoptRunner registers an externally-managed execution (the scheduler) so it is
// cancellable through CancelSession like any other session. The caller keeps
// ownership of ctx/cancel and must still call SetSessionRunning(false) when done.
func adoptRunner(ctx context.Context, cancel context.CancelFunc, sessionID string) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[sessionID] = &sessionRunner{sessionID: sessionID, ctx: ctx, cancel: cancel}
}

// WakeSessionRunner guarantees that work already persisted for a session will be
// consumed.
//
// It exists because the wake mark is consumed, not sticky: a runner that finds
// the queue empty clears the mark and may exit on its next pass. So a caller
// that claimed the session first and persisted the row second can still lose the
// race — the runner can consume the mark, find nothing, and exit before the row
// lands.
//
// Calling this AFTER the row is visible closes that window: either a runner is
// still alive (mark set, so it will not exit before dequeuing), or none is left
// (start one). Both outcomes leave exactly one consumer for the row.
func WakeSessionRunner(sessionID string) {
	if sessionID == "" {
		return
	}
	if wakeRunnerIfPresent(sessionID) {
		return
	}
	// No runner survived: it consumed the earlier mark and exited. Start a
	// consumer for the row that is now visible.
	EnsureConsumer(sessionID)
}

// wakeRunnerIfPresent sets the wake mark on a live runner and reports whether one
// was found.
func wakeRunnerIfPresent(sessionID string) bool {
	registryMu.Lock()
	defer registryMu.Unlock()
	r, ok := registry[sessionID]
	if !ok {
		return false
	}
	r.wake = true
	return true
}

// retireRunner removes a session's runner when its work queue looks empty.
//
// Returns true when the runner was removed and the caller must exit. Returns
// false when work arrived after the caller last looked, in which case the caller
// must keep going rather than exit and strand that work.
//
// This replaces the old "self-heal after 100ms" workaround: the decision to exit
// and the check for late work are now one atomic step.
func retireRunner(sessionID string) bool {
	registryMu.Lock()
	defer registryMu.Unlock()

	r, ok := registry[sessionID]
	if !ok {
		return true // already gone (cancelled) — nothing to retire
	}
	if r.wake {
		r.wake = false
		return false
	}
	delete(registry, sessionID)
	return true
}

// removeRunner drops the registry entry without cancelling its context.
//
// Used by the run's own winding-down path (SetSessionRunning(false) and the
// terminal-event helper): the owner is exiting anyway, and cancelling there
// would abort work that still has to run (emitting the terminal event,
// persisting the final message).
func removeRunner(sessionID string) {
	registryMu.Lock()
	delete(registry, sessionID)
	registryMu.Unlock()
}

// finishRunner removes the runner and cancels its context.
//
// Removal and cancel are one operation from the registry's point of view, so no
// observer can see a session that is still "running" with nothing left to
// cancel it. This is the replacement for the old LIFO defer chain that
// unregistered the cancel func before clearing the running flag.
func finishRunner(sessionID string) {
	registryMu.Lock()
	r, ok := registry[sessionID]
	if ok {
		delete(registry, sessionID)
	}
	registryMu.Unlock()

	if ok {
		r.cancel()
	}
}

// takeRunner removes and returns the runner so the caller can cancel it.
//
// The cancel func is invoked by the caller OUTSIDE registryMu: cancelling runs
// arbitrary executor cleanup, and holding the lock across it would risk
// lock-order inversion with the ACP connection manager, whose idle sweep calls
// back into IsSessionRunning while holding its own mutex.
func takeRunner(sessionID string) (*sessionRunner, bool) {
	registryMu.Lock()
	defer registryMu.Unlock()
	r, ok := registry[sessionID]
	if ok {
		delete(registry, sessionID)
	}
	return r, ok
}

// runnerContext returns the execution context for a running session, or nil.
func runnerContext(sessionID string) context.Context {
	registryMu.Lock()
	defer registryMu.Unlock()
	if r, ok := registry[sessionID]; ok {
		return r.ctx
	}
	return nil
}

// isRunnerRegistered reports whether a runner exists for the session.
func isRunnerRegistered(sessionID string) bool {
	registryMu.Lock()
	defer registryMu.Unlock()
	_, ok := registry[sessionID]
	return ok
}

// runningRunnerIDs snapshots the registered session ids.
func runningRunnerIDs() []string {
	registryMu.Lock()
	defer registryMu.Unlock()
	ids := make([]string, 0, len(registry))
	for id := range registry {
		ids = append(ids, id)
	}
	return ids
}

// allRunners snapshots the registry so shutdown can cancel every execution
// without holding the lock while cancelling.
func allRunners() []*sessionRunner {
	registryMu.Lock()
	defer registryMu.Unlock()
	out := make([]*sessionRunner, 0, len(registry))
	for _, r := range registry {
		out = append(out, r)
	}
	return out
}

// FinishSessionRun removes a session's runner and cancels its execution
// context. Callers use it as the single deferred cleanup for an execution
// goroutine, replacing the previous sequence of separate unregister/cancel/
// clear-flag defers whose order was load-bearing.
func FinishSessionRun(sessionID string) {
	finishRunner(sessionID)
}

// RegisterExternalExecution registers an execution whose lifecycle is managed
// outside the interactive runner (the scheduler). It becomes cancellable through
// CancelSession, and the caller remains responsible for clearing the running
// state when the execution ends.
func RegisterExternalExecution(ctx context.Context, cancel context.CancelFunc, sessionID string) {
	adoptRunner(ctx, cancel, sessionID)
}
