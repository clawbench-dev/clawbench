package ai

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// --- CLIBackend ExecuteStream ---

func TestCLIBackend_ExecuteStream_CommandFailure(t *testing.T) {
	b := &CLIBackend{
		BackendName: "test",
		Cmd:         "nonexistent-cli-command-12345",
		BuildArgsFn: func(req ChatRequest) []string { return []string{} },
		NewParserFn: func() LineParser { return &StreamParser{} },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := b.ExecuteStream(ctx, ChatRequest{
		Prompt:    "test",
		SessionID: "test-session",
		WorkDir:   t.TempDir(),
	})
	// Command does not exist, so Start should fail
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "test stream: failed to start command")
}

func TestCLIBackend_ExecuteStream_ContextCancellation(t *testing.T) {
	b := &CLIBackend{
		BackendName: "test",
		Cmd:         "sleep", // will be cancelled
		BuildArgsFn: func(req ChatRequest) []string { return []string{"300"} },
		NewParserFn: func() LineParser { return &StreamParser{} },
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel before calling ExecuteStream
	cancel()

	_, err := b.ExecuteStream(ctx, ChatRequest{
		Prompt:    "test",
		SessionID: "test-session",
		WorkDir:   t.TempDir(),
	})
	// With context already cancelled, either the command fails to start
	// or starts and is immediately killed. Either way, an error is expected.
	assert.Error(t, err, "pre-cancelled context should produce an error")
}

// --- CLIBackend filterLine helpers ---

func TestFilterSkipNonJSON(t *testing.T) {
	f := filterSkipNonJSON()

	_, ok := f("")
	assert.False(t, ok)

	_, ok = f("not json")
	assert.False(t, ok)

	line, ok := f(`{"type":"content"}`)
	assert.True(t, ok)
	assert.Equal(t, `{"type":"content"}`, line)
}

// TestCLIBackend_ExecuteStream_ContextCancelReapsProcess verifies ISS-232 fix:
// When the context is cancelled mid-stream, the deferred cleanup must call
// cmd.Wait() (with timeout) to reap the child process and avoid zombies.
func TestCLIBackend_ExecuteStream_ContextCancelReapsProcess(t *testing.T) {
	b := &CLIBackend{
		BackendName: "test",
		Cmd:         "cat",
		BuildArgsFn: func(req ChatRequest) []string { return []string{} },
		NewParserFn: func() LineParser { return &StreamParser{} },
	}

	ctx, cancel := context.WithCancel(context.Background())

	ch, err := b.ExecuteStream(ctx, ChatRequest{
		Prompt:    "test",
		SessionID: "test-iss232",
		WorkDir:   t.TempDir(),
	})
	assert.NoError(t, err, "cat should start successfully")

	// Cancel the context after a brief delay to allow the goroutine to start
	time.Sleep(100 * time.Millisecond)
	cancel()

	// The channel should close within a reasonable time (cmd.Wait cleanup).
	// Drain events until the channel is closed.
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case _, open := <-ch:
			if !open {
				return
			}
		case <-timer.C:
			t.Fatal("timed out waiting for channel to close — process may not have been reaped (ISS-232)")
		}
	}
}

// TestCLIBackend_ExecuteStream_WaitReapsWhenChildHoldsPipe verifies that a
// spawned child inheriting the stdout pipe can no longer block the stream
// forever. The parent `sh` exits immediately, but the backgrounded grandchild
// `sleep` holds the pipe open. The stream must still terminate promptly:
// Wait() is started immediately and closes the parent's pipe ends after the
// process exits, unblocking the read loop.
func TestCLIBackend_ExecuteStream_WaitReapsWhenChildHoldsPipe(t *testing.T) {
	b := &CLIBackend{
		BackendName: "test",
		Cmd:         "sh",
		BuildArgsFn: func(req ChatRequest) []string {
			return []string{"-c", `echo '{"type":"result","session_id":"sess"}'; (sleep 10 2>/dev/null) 2>/dev/null &`}
		},
		NewParserFn: func() LineParser { return &StreamParser{} },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := b.ExecuteStream(ctx, ChatRequest{
		Prompt:    "test",
		SessionID: "test-grandchild-pipe",
		WorkDir:   t.TempDir(),
	})
	assert.NoError(t, err, "sh should start successfully")

	gotDone := false
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				// Channel closed — the stream terminated despite the grandchild
				// holding the pipe. Success.
				assert.True(t, gotDone, "expected a done event before the stream closed")
				return
			}
			if ev.Type == "done" {
				gotDone = true
			}
		case <-timer.C:
			t.Fatal("timed out waiting for channel to close — grandchild held the stdout pipe and the stream blocked indefinitely")
		}
	}
}

// TestCLIBackend_ExecuteStream_ChildHoldsStderrClosesPromptly verifies that a
// spawned child inheriting only stderr (stdout redirected) cannot delay the
// stream end: the scanner reaches EOF on stdout quickly, but Wait() is held up
// by exec's stderr copy goroutine until the child exits. The stream must still
// close promptly (bounded by the 2s diagnostic wait), not wait for the child.
func TestCLIBackend_ExecuteStream_ChildHoldsStderrClosesPromptly(t *testing.T) {
	b := &CLIBackend{
		BackendName: "test",
		Cmd:         "sh",
		BuildArgsFn: func(req ChatRequest) []string {
			return []string{"-c", `echo '{"type":"result","session_id":"sess"}'; (sleep 20 1>/dev/null) &`}
		},
		NewParserFn: func() LineParser { return &StreamParser{} },
	}
	t.Cleanup(func() { _ = exec.Command("pkill", "-f", "sleep 20 1").Run() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := b.ExecuteStream(ctx, ChatRequest{
		Prompt:    "test",
		SessionID: "test-grandchild-stderr",
		WorkDir:   t.TempDir(),
	})
	assert.NoError(t, err, "sh should start successfully")

	gotDone := false
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				assert.True(t, gotDone, "expected a done event before the stream closed")
				return
			}
			if ev.Type == "done" {
				gotDone = true
			}
		case <-timer.C:
			t.Fatal("timed out waiting for channel to close — a child holding stderr delayed Wait() and the stream end")
		}
	}
}

// TestCLIBackend_ExecuteStream_WatchdogTerminatesStalledStream verifies the
// no-progress watchdog: when the CLI produces output and then goes silent
// without exiting, the stream is terminated (channel closed) by the watchdog.
// Without it the process group would live forever and the session would stay
// in the streaming state indefinitely.
func TestCLIBackend_ExecuteStream_WatchdogTerminatesStalledStream(t *testing.T) {
	b := &CLIBackend{
		BackendName: "test",
		Cmd:         "sh",
		BuildArgsFn: func(req ChatRequest) []string {
			return []string{"-c", `echo '{"type":"assistant","subtype":"text","text":"hi"}'; sleep 311`}
		},
		NewParserFn:       func() LineParser { return &StreamParser{} },
		NoProgressTimeout: 500 * time.Millisecond,
	}
	t.Cleanup(func() { _ = exec.Command("pkill", "-f", "sleep 311").Run() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := b.ExecuteStream(ctx, ChatRequest{
		Prompt:    "test",
		SessionID: "test-watchdog",
		WorkDir:   t.TempDir(),
	})
	assert.NoError(t, err)

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // channel closed — the watchdog terminated the stalled stream
			}
		case <-timer.C:
			t.Fatal("timed out waiting for the watchdog to terminate the stalled stream")
		}
	}
}

// TestCLIBackend_ExecuteStream_WatchdogDisabled verifies that a negative
// NoProgressTimeout disables the watchdog: a quiet but alive process is not
// force-killed and the stream only ends when the process exits on its own.
func TestCLIBackend_ExecuteStream_WatchdogDisabled(t *testing.T) {
	b := &CLIBackend{
		BackendName: "test",
		Cmd:         "sh",
		BuildArgsFn: func(req ChatRequest) []string {
			return []string{"-c", `echo '{"type":"assistant","subtype":"text","text":"hi"}'; sleep 2`}
		},
		NewParserFn:       func() LineParser { return &StreamParser{} },
		NoProgressTimeout: -1,
	}

	start := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := b.ExecuteStream(ctx, ChatRequest{
		Prompt:    "test",
		SessionID: "test-watchdog-disabled",
		WorkDir:   t.TempDir(),
	})
	assert.NoError(t, err)

	gotContent := false
	var closedAt time.Time
loop:
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				closedAt = time.Now()
				break loop
			}
			if ev.Type == "content" {
				gotContent = true
			}
		case <-time.After(5 * time.Second):
			t.Fatal("channel never closed (process leaked?)")
		}
	}

	assert.True(t, gotContent, "expected a content event")
	// The watchdog is disabled: the stream must survive the quiet window
	// between the content line and the process exiting (~2s). If the watchdog
	// fired anyway it would force-close the channel almost immediately.
	if closedAt.Sub(start) < 1500*time.Millisecond {
		t.Fatalf("stream closed after %v — disabled watchdog must not kill a quiet but alive process", closedAt.Sub(start))
	}
}

// TestCLIBackend_ExecuteStream_CancelKillsProcessGroup verifies that cancelling
// the context kills the whole process group. The command spawns a child
// (`sleep`) that inherits the pipes; killing only the leader would leave the
// child holding the pipe open and the channel would never close.
func TestCLIBackend_ExecuteStream_CancelKillsProcessGroup(t *testing.T) {
	b := &CLIBackend{
		BackendName: "test",
		Cmd:         "sh",
		BuildArgsFn: func(req ChatRequest) []string {
			return []string{"-c", `echo '{"type":"assistant","subtype":"text","text":"hi"}'; sleep 313`}
		},
		NewParserFn: func() LineParser { return &StreamParser{} },
	}
	t.Cleanup(func() { _ = exec.Command("pkill", "-f", "sleep 313").Run() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := b.ExecuteStream(ctx, ChatRequest{
		Prompt:    "test",
		SessionID: "test-cancel-group",
		WorkDir:   t.TempDir(),
	})
	assert.NoError(t, err)

	time.Sleep(150 * time.Millisecond)
	cancel()

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // channel closed — the whole process group was killed
			}
		case <-timer.C:
			t.Fatal("timed out waiting for channel to close after cancel — child process group was not killed, pipe held open")
		}
	}
}
