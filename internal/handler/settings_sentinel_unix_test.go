//go:build !windows

package handler

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// The sentinel must never wait forever. A drain that never finishes — a session
// close stuck on a socket write, a wedged finalize — would leave it waiting for
// good, and the service down with nobody left to start it. That is the
// 2026-11-11 outage with a different trigger.
//
// The stand-in for the outgoing server traps SIGTERM and loops, so it never
// exits on its own: this test can only pass if the sentinel escalates to
// SIGKILL and then starts the replacement.
func TestUnixSentinelKillsAfterWait(t *testing.T) {
	stubborn := exec.Command("/bin/sh", "-c", "trap '' TERM; while :; do sleep 0.05; done")
	if err := stubborn.Start(); err != nil {
		t.Fatalf("start stand-in server: %v", err)
	}
	// Reap it the moment it dies. The sentinel is not its parent, so without
	// this it keeps answering `kill -0` as a zombie and the post-kill wait runs
	// to its full bound — correct behavior, but it would make this test slow
	// for no reason.
	exited := make(chan struct{})
	go func() { _ = stubborn.Wait(); close(exited) }()
	t.Cleanup(func() { _ = stubborn.Process.Kill() })

	dir := t.TempDir()
	marker := filepath.Join(dir, "replacement-started")
	replacement := filepath.Join(dir, "replacement")
	body := "#!/bin/sh\n: > " + shellQuote(marker) + "\n"
	if err := os.WriteFile(replacement, []byte(body), 0o755); err != nil {
		t.Fatalf("write replacement: %v", err)
	}

	// One second of waiting instead of the production thirty; the escalation
	// path is identical.
	script := buildUnixSentinelScript(stubborn.Process.Pid, replacement, nil, 1)
	done := make(chan error, 1)
	go func() { done <- exec.Command("/bin/sh", "-c", script).Run() }()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("sentinel did not return: the wait for the outgoing process is unbounded")
	}

	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("replacement was not started: %v", err)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("sentinel started the replacement without stopping the outgoing process")
	}
}

// A timeout that fires during a normal drain would kill a process that was
// about to exit on its own, taking the last unwritten bytes of a reply with it.
func TestSentinelWaitCoversShutdownDrain(t *testing.T) {
	// Worst case: 5s stream finalize + 10s HTTP drain + 3s ACP exit grace + 3s
	// second finalize + ~4s terminal close ≈ 25s. An observed restart drained
	// in 8s.
	if sentinelWaitSeconds < 20 {
		t.Fatalf("sentinelWaitSeconds = %d is shorter than a real shutdown drain (~25s)", sentinelWaitSeconds)
	}
}
