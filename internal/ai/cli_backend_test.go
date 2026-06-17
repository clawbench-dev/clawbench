package ai

import (
	"context"
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
