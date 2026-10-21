//go:build windows

package main

// selfSignalInterrupt requests a graceful shutdown on Windows.
//
// It cannot use a signal: Go's Windows Process.Signal returns EWINDOWS for
// everything except Kill ($GOROOT/src/os/exec_windows.go), so the old
// `p.Signal(os.Interrupt)` was a silent no-op. The error was discarded, so
// makeRestartFunc reported success, ServeConfigRestart answered 200
// "restarting", and the process kept serving — the same "clicked restart,
// nothing happened" symptom as the missing-binary case, but on every Windows
// install. Console control events are not a substitute either: the server may
// have no console (spawned detached / as a service).
//
// A channel is the portable mechanism: requestShutdown closes shutdownRequests
// and the shutdown goroutine in main selects on it.
func selfSignalInterrupt() {
	requestShutdown()
}
