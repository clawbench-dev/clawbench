package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// The shutdown channel is package-level and single-use (a process shuts down
// once), so these tests observe the SAME close rather than resetting it. Each
// check starts its own waiter before triggering; a close that already happened
// is still observed immediately, so ordering across tests does not matter.

func waitForShutdown(t *testing.T) bool {
	t.Helper()
	select {
	case <-shutdownRequests:
		return true
	case <-time.After(2 * time.Second):
		return false
	}
}

// TestRequestShutdown_ClosesChannel verifies the in-process shutdown path is
// observable by the goroutine that waits on it.
func TestRequestShutdown_ClosesChannel(t *testing.T) {
	requestShutdown()
	assert.True(t, waitForShutdown(t), "requestShutdown must close shutdownRequests")
}

// TestRequestShutdown_Idempotent: a restart request can race a real Ctrl+C, so
// both paths may fire. Closing the channel twice would panic.
func TestRequestShutdown_Idempotent(t *testing.T) {
	assert.NotPanics(t, func() {
		requestShutdown()
		requestShutdown()
		requestShutdown()
	})
}
