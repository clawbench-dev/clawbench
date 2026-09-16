package rag

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
