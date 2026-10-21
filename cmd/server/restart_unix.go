//go:build !windows

package main

import (
	"log/slog"
	"syscall"
)

// signalSelf is the function called by selfSignalInterrupt to deliver the signal.
// Overridden in tests to avoid killing the test process.
var signalSelf = func(sig syscall.Signal) error {
	return syscall.Kill(syscall.Getpid(), sig)
}

// selfSignalInterrupt delivers SIGINT to this process, which the
// signal.NotifyContext in main turns into a graceful shutdown.
//
// If the signal cannot be delivered, fall back to the in-process request.
// Discarding the error (as this used to) made a failed restart indistinguishable
// from a successful one: the handler answered 200 "restarting" and the process
// kept serving.
func selfSignalInterrupt() {
	if err := signalSelf(syscall.SIGINT); err != nil {
		slog.Warn("failed to self-signal for restart, using in-process shutdown request",
			"error", err)
		requestShutdown()
	}
}
