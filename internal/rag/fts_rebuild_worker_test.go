package rag

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ws"
)

// TestFTSRebuildWorker_StartRunsInBackground asserts the worker returns
// immediately from Start and reports progress through GetStatus, which is what
// makes the HTTP handler able to answer 202 without blocking for minutes.
func TestFTSRebuildWorker_StartRunsInBackground(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	insertTestChunksSQLite(t, store, 3)

	w := NewFTSRebuildWorker(nil)
	require.True(t, w.Start(store))

	// Terminal state must arrive without the caller blocking on Start.
	status := waitForTerminal(t, w)
	assert.Equal(t, "done", status.Status)
	assert.Equal(t, int64(3), status.Indexed)
	assert.Equal(t, 100, status.ProgressPct)
	assert.Empty(t, status.Error)
}

// TestFTSRebuildWorker_RejectsConcurrentStart asserts only one rebuild can run
// at a time, so the handler can map the rejection to 409.
func TestFTSRebuildWorker_RejectsConcurrentStart(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	insertTestChunksSQLite(t, store, 3)

	w := NewFTSRebuildWorker(nil)
	require.True(t, w.Start(store), "first start accepted")

	// Wait until it is genuinely running before the second attempt, otherwise
	// this could pass simply because the first run already finished.
	require.Eventually(t, func() bool { return w.IsRunning() }, 2*time.Second, 5*time.Millisecond,
		"first rebuild should be running")

	assert.False(t, w.Start(store), "second concurrent start must be rejected")

	waitForTerminal(t, w)
}

// TestFTSRebuildWorker_RefusesWithoutSegmenter asserts the worker records an
// error and starts nothing when the segmenter is missing.
func TestFTSRebuildWorker_RefusesWithoutSegmenter(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	insertTestChunksSQLite(t, store, 2)

	prev := SetSegmenterForTest(nil)
	t.Cleanup(func() { RestoreSegmenterForTest(prev) })

	w := NewFTSRebuildWorker(nil)
	assert.False(t, w.Start(store), "start must be refused without a segmenter")
	assert.False(t, w.IsRunning())

	status := w.GetStatus()
	assert.Equal(t, "error", status.Status)
	assert.Contains(t, status.Error, ErrSegmenterUnavailable.Error())

	// The store must be untouched: segmentation would have overwritten good data.
	var count int
	require.NoError(t, store.db.QueryRow("SELECT COUNT(*) FROM rag_chunks").Scan(&count))
	assert.Equal(t, 2, count)
}

// TestFTSRebuildWorker_CancelStopsRun asserts Cancel marks the run cancelled and
// clears the running flag, so a later start is accepted.
func TestFTSRebuildWorker_CancelStopsRun(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	insertTestChunksSQLite(t, store, 3)

	w := NewFTSRebuildWorker(nil)
	require.True(t, w.Start(store))

	w.Cancel()

	assert.False(t, w.IsRunning())
	assert.Equal(t, "cancelled", w.GetStatus().Status)

	// A cancelled worker must accept a new run.
	require.Eventually(t, func() bool { return w.Start(store) }, 2*time.Second, 10*time.Millisecond,
		"a cancelled worker must be startable again")
	status := waitForTerminal(t, w)
	assert.Equal(t, "done", status.Status)
}

// TestFTSRebuildWorker_StaleGoroutineCannotClobberState asserts the generation
// guard directly.
//
// The scenario it protects: Cancel() clears `running` immediately, so the user
// can start a new rebuild while the old goroutine is still finishing (it only
// observes cancellation every ftsRebuildProgressEvery chunks, so it can linger).
// Without the guard the stale goroutine's final state write overwrites the new
// run's status.
//
// Calling run() with a generation that does not match the worker's current one
// reproduces a stale goroutine deterministically, instead of racing a real one.
func TestFTSRebuildWorker_StaleGoroutineCannotClobberState(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	insertTestChunksSQLite(t, store, 3)

	w := NewFTSRebuildWorker(nil)

	// Simulate a NEWER run being in flight: the worker's generation has moved on
	// and its status reflects that newer run.
	w.mu.Lock()
	w.generation = 7
	w.running = true
	w.status = FTSRebuildStatus{Status: "running", Phase: "resegmenting", ProgressPct: 42}
	w.mu.Unlock()

	// A stale goroutine (generation 6) finishes and tries to write its outcome.
	w.run(context.Background(), 6, store)

	st := w.GetStatus()
	assert.Equal(t, "running", st.Status,
		"a stale goroutine must not overwrite the current run's status")
	assert.Equal(t, 42, st.ProgressPct, "stale progress must not overwrite current progress")
	assert.True(t, w.IsRunning(), "a stale goroutine must not clear the running flag")
}

// TestFTSRebuildWorker_ProgressIsMonotonic asserts progress never regresses and
// that status is safe to read concurrently with the running rebuild (the
// handler polls GetStatus from other goroutines).
func TestFTSRebuildWorker_ProgressIsMonotonic(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	insertTestChunksSQLite(t, store, 5)

	w := NewFTSRebuildWorker(nil)
	require.True(t, w.Start(store))

	var wg sync.WaitGroup
	stop := make(chan struct{})
	lastSeen := 0
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			st := w.GetStatus()
			// Progress must be monotonic within a single run.
			require.GreaterOrEqual(t, st.ProgressPct, lastSeen,
				"progress regressed: %d -> %d", lastSeen, st.ProgressPct)
			lastSeen = st.ProgressPct
		}
	}()

	status := waitForTerminal(t, w)
	close(stop)
	wg.Wait()

	assert.Equal(t, "done", status.Status)
	assert.Equal(t, 100, status.ProgressPct)
}

// waitForTerminal polls the worker until it leaves the running state.
func waitForTerminal(t *testing.T, w *FTSRebuildWorker) FTSRebuildStatus {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		st := w.GetStatus()
		if st.Status != "running" {
			return st
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("worker did not reach a terminal state; last status: %+v", w.GetStatus())
	return FTSRebuildStatus{}
}

// TestFTSRebuildWorker_ContextIsHonouredByStore is a guard that the worker's
// cancellation actually reaches the store: a pre-cancelled context passed
// straight to the store must abort without writing.
func TestFTSRebuildWorker_ContextIsHonouredByStore(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	insertTestChunksSQLite(t, store, 3)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := store.RebuildFTS(ctx, nil)
	require.ErrorIs(t, err, context.Canceled)
}

// TestFTSRebuildWorker_RecordsCancelledOutcome asserts the worker's own
// cancelled branch: when the context is cancelled the run must report
// "cancelled" (not "error"), clear the phase, and clear the running flag so the
// session is startable again. The UI distinguishes these — a cancel is a user
// action, not a failure.
func TestFTSRebuildWorker_RecordsCancelledOutcome(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	insertTestChunksSQLite(t, store, 3)

	w := NewFTSRebuildWorker(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Run directly with a matching generation so the guard lets the outcome
	// through; a real Cancel() bumps the generation and would suppress it.
	w.mu.Lock()
	w.generation = 11
	w.running = true
	w.mu.Unlock()

	w.run(ctx, 11, store)

	st := w.GetStatus()
	assert.Equal(t, "cancelled", st.Status, "a cancelled context must report cancelled, not error")
	assert.Empty(t, st.Phase, "cancelled runs have no phase")
	assert.False(t, w.IsRunning(), "a cancelled run must clear the running flag")
	assert.Empty(t, st.Error, "cancelling is not an error")
}

// TestFTSRebuildWorker_RecordsErrorOutcome asserts the worker surfaces a store
// failure as an error status with the underlying message, so the polling client
// can show why the rebuild failed instead of hanging on "running".
func TestFTSRebuildWorker_RecordsErrorOutcome(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	insertTestChunksSQLite(t, store, 2)
	// Closing the store makes RebuildFTS fail deterministically.
	require.NoError(t, store.Close())

	w := NewFTSRebuildWorker(nil)
	w.mu.Lock()
	w.generation = 12
	w.running = true
	w.mu.Unlock()

	w.run(context.Background(), 12, store)

	st := w.GetStatus()
	assert.Equal(t, "error", st.Status)
	assert.Empty(t, st.Phase, "failed runs have no phase")
	assert.False(t, w.IsRunning(), "a failed run must clear the running flag")
	assert.NotEmpty(t, st.Error, "the underlying failure must be reported to the client")
}

// TestFTSRebuildWorker_RecoversFromPanic asserts a panic inside the store does
// not take down the process and does not leave the worker stuck in "running"
// forever — which would permanently wedge the rebuild button with no way to
// retry short of a restart.
func TestFTSRebuildWorker_RecoversFromPanic(t *testing.T) {
	w := NewFTSRebuildWorker(nil)
	w.mu.Lock()
	w.generation = 13
	w.running = true
	w.mu.Unlock()

	// A nil store panics inside RebuildFTS. The deferred recover must convert it
	// into an error status.
	require.NotPanics(t, func() { w.run(context.Background(), 13, nil) })

	st := w.GetStatus()
	assert.Equal(t, "error", st.Status)
	assert.False(t, w.IsRunning(), "a panicking run must not leave the worker wedged as running")
}

// TestFTSRebuildWorker_PanicDoesNotClobberNewerRun asserts the panic recovery
// honours the same generation guard as the normal exit path: a stale goroutine
// that panics after a newer run started must not reset the newer run's state.
func TestFTSRebuildWorker_PanicDoesNotClobberNewerRun(t *testing.T) {
	w := NewFTSRebuildWorker(nil)

	// Simulate a newer run in flight.
	w.mu.Lock()
	w.generation = 21
	w.running = true
	w.status = FTSRebuildStatus{Status: "running", Phase: "resegmenting", ProgressPct: 55}
	w.mu.Unlock()

	// A stale goroutine (generation 20) panics on its way out.
	require.NotPanics(t, func() { w.run(context.Background(), 20, nil) })

	st := w.GetStatus()
	assert.Equal(t, "running", st.Status, "a stale panic must not overwrite the current run's status")
	assert.Equal(t, 55, st.ProgressPct)
	assert.True(t, w.IsRunning(), "a stale panic must not clear the running flag")
}

// TestBroadcastFTSRebuild asserts the broadcast seam reaches subscribed clients
// and is a no-op when no hub is configured (polling is the authoritative path,
// so a missing hub must not panic).
func TestBroadcastFTSRebuild(t *testing.T) {
	t.Run("nil hub is a no-op", func(t *testing.T) {
		require.NotPanics(t, func() {
			broadcastFTSRebuild(nil, FTSRebuildStatus{Status: "done"})
		})
	})

	t.Run("hub without manager is a no-op", func(t *testing.T) {
		require.NotPanics(t, func() {
			broadcastFTSRebuild(ws.NewStreamHub(nil), FTSRebuildStatus{Status: "done"})
		})
	})

	t.Run("delivers to a subscribed client", func(t *testing.T) {
		mgr := ws.NewManagerForTest()
		var writeMu sync.Mutex
		sub := mgr.Subscribe(nil, &writeMu, "client-1", "")
		mgr.DisconnectClient("client-1") // buffer instead of writing to a nil conn

		broadcastFTSRebuild(mgr.StreamHub(), FTSRebuildStatus{Status: "done", ProgressPct: 100})

		buffered := sub.GetBufferedEvents()
		require.Len(t, buffered, 1, "the rebuild status must reach the subscribed client")
		assert.Equal(t, "rag_fts_rebuild", buffered[0].Event)
	})
}
