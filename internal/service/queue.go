package service

import (
	"sync"
	"time"
)

// sessionDrainChans stores per-session channels used to signal the drain loop
// when a new message is enqueued, replacing the old 50ms sleep+retry hack.
var sessionDrainChans sync.Map // map[string]chan struct{}

func getDrainChan(sessionID string) chan struct{} {
	val, _ := sessionDrainChans.LoadOrStore(sessionID, make(chan struct{}, 1))
	return val.(chan struct{}) //nolint:errcheck // LoadOrStore always returns chan struct{}
}

// SignalDrain wakes up a waiting drain loop for a session, if any. Non-blocking:
// if no drain loop is waiting (buffer full), the signal is dropped — the next
// drain-loop check of the DB queue will pick the message up anyway.
func SignalDrain(sessionID string) {
	select {
	case getDrainChan(sessionID) <- struct{}{}:
	default:
	}
}

// WaitForEnqueue blocks until a message is enqueued or the timeout expires.
// The drain loop calls this instead of time.Sleep to get immediate wake-up.
func WaitForEnqueue(sessionID string, timeout time.Duration) bool {
	select {
	case <-getDrainChan(sessionID):
		return true
	case <-time.After(timeout):
		return false
	}
}
