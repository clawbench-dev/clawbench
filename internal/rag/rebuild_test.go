package rag

import (
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestCoordinator builds a coordinator bound to a real store + indexer.
// The indexer is NOT started: these tests drive marking and progress observation
// directly, so no background drain races the assertions.
//
// Cancel is registered as cleanup so the watch goroutine stops before the store
// is closed; otherwise a late poll touches a closed DB.
func newTestCoordinator(t *testing.T, store *Store) *RebuildCoordinator {
	t.Helper()
	coord := NewRebuildCoordinator(nil)
	coord.SetIndexer(NewIndexer(store, nil, model.RAGConfig{
		ChunkSize: 512, BatchSize: 50, PollInterval: "1h",
	}))
	t.Cleanup(coord.Cancel)
	return coord
}

// waitForCoordinatorTerminal polls until the coordinator leaves the running state.
func waitForCoordinatorTerminal(t *testing.T, c *RebuildCoordinator) RebuildStatus {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		st := c.GetStatus()
		if st.Status != "running" {
			return st
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("coordinator never reached a terminal state; last=%+v", c.GetStatus())
	return RebuildStatus{}
}

// TestRebuildCoordinator_ReportsNonZeroProgress is the regression test for a
// self-cancelling progress calculation: Total was recomputed as
// Processed+remaining and Processed as Total-remaining, which made Processed
// permanently 0. A 44k-chunk rebuild therefore reported 0% for its entire
// 158-second run.
func TestRebuildCoordinator_ReportsNonZeroProgress(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	insertTestChunksSQLite(t, store, 4)
	coord := newTestCoordinator(t, store)

	require.NoError(t, coord.Start(store, RebuildFTS))

	// Drain only PART of the queue, so there is genuinely partial progress to
	// observe (draining it all would just jump straight to done).
	chunks, err := store.GetPendingResegmentChunks(2)
	require.NoError(t, err)
	_, err = store.BatchResegment(chunks)
	require.NoError(t, err)

	// Wait for the watcher to observe the partial state.
	require.Eventually(t, func() bool {
		return coord.GetStatus().Processed > 0
	}, 5*time.Second, 20*time.Millisecond,
		"progress must advance as the queue drains, not stay at 0")

	st := coord.GetStatus()
	assert.Equal(t, 4, st.Total, "Total must stay fixed at the initially queued count")
	assert.Equal(t, 2, st.Processed)
	assert.Equal(t, 50, st.ProgressPct, "2 of 4 done must report 50%")

	// Drain the rest so the run can finish.
	rest, err := store.GetPendingResegmentChunks(10)
	require.NoError(t, err)
	_, err = store.BatchResegment(rest)
	require.NoError(t, err)

	final := waitForCoordinatorTerminal(t, coord)
	assert.Equal(t, "done", final.Status)
	assert.Equal(t, 100, final.ProgressPct, "a finished rebuild must report 100%")
}

// TestRebuildCoordinator_ProgressIsMonotonic asserts progress never goes
// backwards while a rebuild runs, which is what the UI progress bar assumes.
func TestRebuildCoordinator_ProgressIsMonotonic(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	insertTestChunksSQLite(t, store, 6)
	coord := newTestCoordinator(t, store)

	require.NoError(t, coord.Start(store, RebuildFTS))

	last := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 6 {
			chunks, err := store.GetPendingResegmentChunks(1)
			if err != nil || len(chunks) == 0 {
				return
			}
			if _, err := store.BatchResegment(chunks); err != nil {
				return
			}
			time.Sleep(120 * time.Millisecond)
		}
	}()

	for {
		st := coord.GetStatus()
		if st.Status != "running" {
			break
		}
		require.GreaterOrEqual(t, st.ProgressPct, last,
			"progress must not regress: %d -> %d", last, st.ProgressPct)
		last = st.ProgressPct
		time.Sleep(30 * time.Millisecond)
	}
	<-done
	assert.Equal(t, 100, coord.GetStatus().ProgressPct)
}

// TestRebuildCoordinator_OneAtATimeAcrossKinds asserts all three kinds share a
// single slot. Previously the FTS and vector paths used separate flags, so a
// vector rebuild could start while an FTS rebuild was running.
func TestRebuildCoordinator_OneAtATimeAcrossKinds(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	insertTestChunksSQLite(t, store, 2)
	coord := newTestCoordinator(t, store)

	require.NoError(t, coord.Start(store, RebuildFTS))
	assert.True(t, coord.IsRunning())

	for _, kind := range []RebuildKind{RebuildVector, RebuildFull, RebuildFTS} {
		err := coord.Start(store, kind)
		assert.ErrorIs(t, err, ErrRebuildInProgress,
			"kind %s must be rejected while another rebuild runs", kind)
	}
}

// TestRebuildCoordinator_RejectsUnknownKind asserts a typo cannot silently start
// a rebuild of the wrong layer.
func TestRebuildCoordinator_RejectsUnknownKind(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	coord := newTestCoordinator(t, store)

	err := coord.Start(store, RebuildKind("bogus"))
	require.Error(t, err)
	assert.False(t, coord.IsRunning())
}

// TestRebuildCoordinator_RefusesWithoutIndexer asserts the coordinator refuses to
// mark work stale when nobody would pick it up, so a rebuild cannot appear
// "accepted" while never running.
func TestRebuildCoordinator_RefusesWithoutIndexer(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	insertTestChunksSQLite(t, store, 2)

	coord := NewRebuildCoordinator(nil) // no indexer bound

	err := coord.Start(store, RebuildFTS)
	require.ErrorIs(t, err, ErrIndexerUnavailable)

	pending, err := store.PendingResegmentCount()
	require.NoError(t, err)
	assert.Equal(t, 0, pending, "a refused rebuild must not mark anything stale")
}

// TestRebuildCoordinator_FtsRefusedWithoutSegmenter asserts the guard runs BEFORE
// marking, so a refusal leaves the store untouched (re-segmenting without a
// segmenter would overwrite correctly split text with raw text).
func TestRebuildCoordinator_FtsRefusedWithoutSegmenter(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	insertTestChunksSQLite(t, store, 2)
	coord := newTestCoordinator(t, store)

	prev := SetSegmenterForTest(nil)
	t.Cleanup(func() { RestoreSegmenterForTest(prev) })

	err := coord.Start(store, RebuildFTS)
	require.ErrorIs(t, err, ErrSegmenterUnavailable)

	pending, err := store.PendingResegmentCount()
	require.NoError(t, err)
	assert.Equal(t, 0, pending, "the refusal must precede marking")

	// The other kinds do not re-segment, so they must still be startable.
	assert.NoError(t, coord.Start(store, RebuildVector),
		"only the fts kind depends on the segmenter")
}

// TestRebuildCoordinator_CancelStopsTracking asserts Cancel ends the client-facing
// run and frees the slot for a new one.
func TestRebuildCoordinator_CancelStopsTracking(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	insertTestChunksSQLite(t, store, 4)
	coord := newTestCoordinator(t, store)

	require.NoError(t, coord.Start(store, RebuildFTS))
	coord.Cancel()

	assert.False(t, coord.IsRunning())
	assert.Equal(t, "cancelled", coord.GetStatus().Status)

	// A cancelled run must not leave the slot occupied.
	require.Eventually(t, func() bool {
		return coord.Start(store, RebuildFTS) == nil
	}, 2*time.Second, 20*time.Millisecond, "a cancelled coordinator must be restartable")
}

// TestRebuildCoordinator_FullRebuildClearsChunksAndRequeuesMessages asserts the
// full kind discards all chunks (so re-chunking can happen) rather than just
// relabelling them.
func TestRebuildCoordinator_FullRebuildClearsChunksAndRequeuesMessages(t *testing.T) {
	setupIndexerServiceDB(t)
	store := setupSQLiteStoreWithDim(t)
	insertTestChunksSQLite(t, store, 3)

	before, err := store.ChunkCount()
	require.NoError(t, err)
	require.Equal(t, 3, before)

	coord := newTestCoordinator(t, store)
	require.NoError(t, coord.Start(store, RebuildFull))

	after, err := store.ChunkCount()
	require.NoError(t, err)
	assert.Equal(t, 0, after,
		"a full rebuild must delete every chunk so the indexer re-chunks from source")

	// Vectors must be dropped too: the rebuild may run with a different model.
	assert.False(t, store.HasVecData())
}

// TestRebuildCoordinator_CancelStopsWatcherBeforeTeardown asserts Cancel stops the
// watch goroutine promptly, so tearing down the resources it polls cannot race a
// late tick.
//
// This is the regression test for a real panic observed in the full package run:
// the watcher polled on a 1s ticker and only re-checked a generation flag after
// waking, so a rebuild that the test did not wait for kept polling after teardown.
// For the `full` kind that poll calls service.UnindexedCount(), which dereferences
// the service package's dbRead — nil once its test harness is torn down — giving
// "invalid memory address or nil pointer dereference". Cancel must therefore SIGNAL
// the goroutine, not just flip a flag it happens to read later.
//
// The `full` kind is used deliberately: it is the one that reaches into the service
// package, and it is the path that actually panicked.
func TestRebuildCoordinator_CancelStopsWatcherBeforeTeardown(t *testing.T) {
	serviceDB := setupIndexerServiceDB(t)

	store := setupSQLiteStoreWithDim(t)
	coord := NewRebuildCoordinator(nil)
	coord.SetIndexer(NewIndexer(store, nil, model.RAGConfig{
		ChunkSize: 512, BatchSize: 50, PollInterval: "1h",
	}))

	// An empty chat_history means the watcher would find nothing to do on its next
	// tick and finish — but only after the tick fires, which is the window that
	// races teardown.
	require.NoError(t, coord.Start(store, RebuildFull))
	require.True(t, coord.IsRunning())

	coord.Cancel()

	// Tear down exactly what the watcher polls: the service DB first (the `full`
	// kind's counter), then the store.
	require.NoError(t, serviceDB.Close())
	_ = store.Close()

	// Wait well past the poll interval. With the ticker-only implementation the
	// watcher fires here and panics on the closed DB.
	time.Sleep(3 * rebuildPollInterval)

	assert.Equal(t, "cancelled", coord.GetStatus().Status,
		"the cancelled state must survive; nothing should have revived it")
}
