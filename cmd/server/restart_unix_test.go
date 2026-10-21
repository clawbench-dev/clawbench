//go:build !windows

package main

import (
	"errors"
	"syscall"
	"testing"
	"time"
)

// TestSelfSignalInterrupt_FallsBackToShutdownRequest covers the fallback that
// keeps the restart from lying about its outcome: if the self-delivered signal
// cannot be sent, the shutdown must still be requested in-process. The old
// implementation discarded the error, so makeRestartFunc returned success and
// the handler answered 200 while the process kept serving.
func TestSelfSignalInterrupt_FallsBackToShutdownRequest(t *testing.T) {
	orig := signalSelf
	signalSelf = func(sig syscall.Signal) error { return errors.New("kill failed") }
	t.Cleanup(func() { signalSelf = orig })

	selfSignalInterrupt()

	select {
	case <-shutdownRequests:
	case <-time.After(2 * time.Second):
		t.Fatal("selfSignalInterrupt did not request shutdown after the signal failed")
	}
}
