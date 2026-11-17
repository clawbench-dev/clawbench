package main

import (
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Closing the pipe the owner holds must request a shutdown. This is the whole
// contract of --exit-on-stdin-close: on Windows there is no catchable signal
// to receive, so the pipe is the only "please quit" the embedder can send.
func TestWatchParentPipe_EOFRequestsShutdown(t *testing.T) {
	pr, pw := io.Pipe()
	called := make(chan struct{}, 1)
	watchParentPipe(pr, func() { called <- struct{}{} })

	require.NoError(t, pw.Close())

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("closing the pipe must request a shutdown")
	}
}

// A read error is just as final as EOF: a torn-down pipe means nobody is left
// to look after this server. Missing this branch would leave an orphan server
// running after the shell it belonged to went away.
func TestWatchParentPipe_ReadErrorRequestsShutdown(t *testing.T) {
	pr, pw := io.Pipe()
	called := make(chan struct{}, 1)
	watchParentPipe(pr, func() { called <- struct{}{} })

	require.NoError(t, pw.CloseWithError(io.ErrClosedPipe))

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("a failed read must request a shutdown")
	}
}

// Data on stdin is not a shutdown, and it must not end the watch either. The
// shell never writes to the pipe today, but a single byte must be consumed and
// the read repeated rather than treated as "owner gone" (or as "nothing more
// will ever arrive").
func TestWatchParentPipe_DataDoesNotShutdown(t *testing.T) {
	pr, pw := io.Pipe()
	called := make(chan struct{}, 1)
	watchParentPipe(pr, func() { called <- struct{}{} })

	_, err := pw.Write([]byte("x"))
	require.NoError(t, err)

	select {
	case <-called:
		t.Fatal("data on stdin must not be read as a close")
	case <-time.After(100 * time.Millisecond):
	}

	// The watch survives the byte: the close that follows still fires. Without
	// the loop the goroutine would have returned after buffering "x" and this
	// would time out.
	require.NoError(t, pw.Close())
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("the pipe close after data must still request a shutdown")
	}
}

// The watcher must be inert unless the flag is present: a server someone runs
// from a terminal that later closes must keep serving, not quit on EOF.
func TestEnableParentPipeExit_RequiresFlag(t *testing.T) {
	orig := os.Args
	t.Cleanup(func() { os.Args = orig })

	os.Args = []string{"clawbench"}
	assert.False(t, hasFlag("--exit-on-stdin-close"))

	os.Args = []string{"clawbench", "--exit-on-stdin-close"}
	assert.True(t, hasFlag("--exit-on-stdin-close"))
}
