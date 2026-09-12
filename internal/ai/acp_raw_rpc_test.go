package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Raw JSON-RPC side channel tests (no real agent)
// ---------------------------------------------------------------------------

// rawRPCWriter captures everything written through it. It is safe for
// concurrent use so CallRaw can be driven from a goroutine.
type rawRPCWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *rawRPCWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *rawRPCWriter) lines() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []string
	for _, l := range strings.Split(w.buf.String(), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// liveDone returns a channel that never closes (connection alive).
func liveDone() <-chan struct{} { return make(chan struct{}) }

// TestACPRawRPC_CallRawCorrelatesResponse verifies the basic request/response
// round trip: the request goes out with a prefixed string id, and the matching
// response line is delivered back to the caller.
func TestACPRawRPC_CallRawCorrelatesResponse(t *testing.T) {
	w := &rawRPCWriter{}
	rpc := newACPRawRPC(w, liveDone())

	type result struct {
		raw json.RawMessage
		err error
	}
	done := make(chan result, 1)
	go func() {
		raw, err := rpc.CallRaw(context.Background(), "session/steer", map[string]any{
			"sessionId": "s1",
		})
		done <- result{raw, err}
	}()

	// Wait for the request to hit the wire, then answer it.
	var id string
	require.Eventually(t, func() bool {
		ls := w.lines()
		if len(ls) == 0 {
			return false
		}
		var msg struct {
			ID     string `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal([]byte(ls[0]), &msg) != nil {
			return false
		}
		if msg.Method != "session/steer" {
			return false
		}
		id = msg.ID
		return true
	}, 2*time.Second, 5*time.Millisecond, "request should reach the wire")

	assert.True(t, strings.HasPrefix(id, rawRPCIDPrefix), "id %q should carry the raw prefix", id)

	rpc.DispatchRawResponse([]byte(`{"jsonrpc":"2.0","id":"` + id + `","result":{"steered":true}}`))

	select {
	case got := <-done:
		require.NoError(t, got.err)
		assert.JSONEq(t, `{"steered":true}`, string(got.raw))
	case <-time.After(2 * time.Second):
		t.Fatal("CallRaw did not return after its response was dispatched")
	}
}

// TestACPRawRPC_IgnoresForeignAndNumericIDs is the collision guard: responses
// belonging to the ACP SDK (numeric ids) or to another channel must never be
// claimed by this router, and must not fire any pending caller.
func TestACPRawRPC_IgnoresForeignAndNumericIDs(t *testing.T) {
	w := &rawRPCWriter{}
	rpc := newACPRawRPC(w, liveDone())

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := rpc.CallRaw(ctx, "session/steer", nil)
		done <- err
	}()

	require.Eventually(t, func() bool { return len(w.lines()) == 1 }, 2*time.Second, 5*time.Millisecond)

	// None of these belong to us.
	rpc.DispatchRawResponse([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))         // SDK numeric id
	rpc.DispatchRawResponse([]byte(`{"jsonrpc":"2.0","id":"1","result":{}}`))       // SDK string-number id
	rpc.DispatchRawResponse([]byte(`{"jsonrpc":"2.0","id":"other-1","result":{}}`)) // another channel
	rpc.DispatchRawResponse([]byte(`{"jsonrpc":"2.0","id":"cb-nope","result":{}}`)) // unknown prefixed id
	rpc.DispatchRawResponse([]byte(`not json at all`))                              // garbage
	rpc.DispatchRawResponse([]byte(`{"jsonrpc":"2.0","method":"session/update"}`))  // notification, no id

	// The caller must still be waiting (nothing was misrouted to it), and then
	// time out on its own context.
	select {
	case err := <-done:
		t.Fatalf("CallRaw returned early with err=%v; a foreign response was misrouted", err)
	case <-time.After(150 * time.Millisecond):
	}

	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(2 * time.Second):
		t.Fatal("CallRaw did not honour context cancellation")
	}

	// Pending must be cleaned up after the timeout.
	rpc.mu.Lock()
	pending := len(rpc.pending)
	rpc.mu.Unlock()
	assert.Zero(t, pending, "timed-out request should be removed from pending")
}

// TestACPRawRPC_JSONRPCError verifies a JSON-RPC error object surfaces as
// *RawRPCError rather than being mistaken for a result.
func TestACPRawRPC_JSONRPCError(t *testing.T) {
	w := &rawRPCWriter{}
	rpc := newACPRawRPC(w, liveDone())

	done := make(chan error, 1)
	go func() {
		_, err := rpc.CallRaw(context.Background(), "session/steer", nil)
		done <- err
	}()

	var id string
	require.Eventually(t, func() bool {
		ls := w.lines()
		if len(ls) == 0 {
			return false
		}
		var msg struct {
			ID string `json:"id"`
		}
		if json.Unmarshal([]byte(ls[0]), &msg) != nil {
			return false
		}
		id = msg.ID
		return id != ""
	}, 2*time.Second, 5*time.Millisecond)

	rpc.DispatchRawResponse([]byte(`{"jsonrpc":"2.0","id":"` + id + `","error":{"code":-32601,"message":"Method not found"}}`))

	select {
	case err := <-done:
		require.Error(t, err)
		var rpcErr *RawRPCError
		require.ErrorAs(t, err, &rpcErr)
		assert.Equal(t, -32601, rpcErr.Code)
		assert.Equal(t, "Method not found", rpcErr.Message)
	case <-time.After(2 * time.Second):
		t.Fatal("CallRaw did not return after an error response")
	}
}

// TestACPRawRPC_ConnectionDeathUnblocks verifies a caller waiting on a dead
// connection is released with errRawConnClosed rather than hanging.
func TestACPRawRPC_ConnectionDeathUnblocks(t *testing.T) {
	w := &rawRPCWriter{}
	done := make(chan struct{})
	rpc := newACPRawRPC(w, done)

	errCh := make(chan error, 1)
	go func() {
		_, err := rpc.CallRaw(context.Background(), "session/steer", nil)
		errCh <- err
	}()

	require.Eventually(t, func() bool { return len(w.lines()) == 1 }, 2*time.Second, 5*time.Millisecond)
	close(done)

	select {
	case err := <-errCh:
		assert.ErrorIs(t, err, errRawConnClosed)
	case <-time.After(2 * time.Second):
		t.Fatal("CallRaw did not unblock when the connection died")
	}
}

// TestACPRawRPC_ConcurrentIDsAreUnique verifies ids are unique per connection so
// two in-flight raw calls cannot steal each other's response.
func TestACPRawRPC_ConcurrentIDsAreUnique(t *testing.T) {
	w := &rawRPCWriter{}
	rpc := newACPRawRPC(w, liveDone())

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			// Never answered; context ends the call.
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			_, _ = rpc.CallRaw(ctx, "session/steer", nil)
		}()
	}

	require.Eventually(t, func() bool { return len(w.lines()) == n }, 3*time.Second, 10*time.Millisecond)

	seen := map[string]bool{}
	for _, l := range w.lines() {
		var msg struct {
			ID string `json:"id"`
		}
		require.NoError(t, json.Unmarshal([]byte(l), &msg))
		assert.False(t, seen[msg.ID], "duplicate raw id %q", msg.ID)
		seen[msg.ID] = true
	}
	assert.Len(t, seen, n)

	wg.Wait()
}

// TestLockedWriter_SerializesWrites verifies the shared writer never interleaves
// two goroutines' bytes (the reason it must be handed to the SDK).
func TestLockedWriter_SerializesWrites(t *testing.T) {
	var sink bytes.Buffer
	// A writer that yields between writes to widen the race window.
	slow := &yieldingWriter{dst: &sink}
	lw := &lockedWriter{dst: slow}

	var wg sync.WaitGroup
	const writers, reps = 8, 50
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func() {
			defer wg.Done()
			// Each write is one atomic line.
			for j := 0; j < reps; j++ {
				_, _ = lw.Write([]byte("0123456789\n"))
			}
		}()
	}
	wg.Wait()

	// Every line must be exactly the payload — no interleaving.
	for _, line := range strings.Split(strings.TrimRight(sink.String(), "\n"), "\n") {
		assert.Equal(t, "0123456789", line, "write was interleaved")
	}
}

// yieldingWriter yields the scheduler before writing, to expose interleaving if
// the caller does not hold a lock.
type yieldingWriter struct {
	mu  sync.Mutex
	dst *bytes.Buffer
}

func (w *yieldingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	time.Sleep(time.Microsecond)
	return w.dst.Write(p)
}
