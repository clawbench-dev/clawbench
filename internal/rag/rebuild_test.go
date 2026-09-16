package rag

import (
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/ws"

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

// ---------------------------------------------------------------------------
// Start guard: nil store
// ---------------------------------------------------------------------------

// TestRebuildCoordinator_RejectsNilStore asserts the nil-store guard runs before
// anything is mutated. The coordinator is reachable from an HTTP handler where
// the store is nil until RAG initializes, so this path is live, not theoretical.
func TestRebuildCoordinator_RejectsNilStore(t *testing.T) {
	coord := NewRebuildCoordinator(nil)
	coord.SetIndexer(NewIndexer(nil, nil, model.RAGConfig{ChunkSize: 512, BatchSize: 50, PollInterval: "1h"}))

	err := coord.Start(nil, RebuildFTS)
	require.ErrorIs(t, err, ErrStoreUnavailable)
	assert.False(t, coord.IsRunning(), "a rejected start must not occupy the slot")
}

// ---------------------------------------------------------------------------
// remaining: unknown kind
// ---------------------------------------------------------------------------

// TestRebuildCoordinator_RemainingRejectsUnknownKind asserts the counter reports an
// error for a kind it cannot measure, rather than silently returning 0 — a 0 would
// be read as "nothing left" and mark the rebuild done immediately.
func TestRebuildCoordinator_RemainingRejectsUnknownKind(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	coord := newTestCoordinator(t, store)

	n, err := coord.remaining(store, RebuildKind("bogus"))
	require.Error(t, err)
	assert.Equal(t, 0, n)
}

// ---------------------------------------------------------------------------
// watch: blocked vector rebuild without a healthy embedder
// ---------------------------------------------------------------------------

// TestRebuildCoordinator_VectorBlockedWithoutEmbedder covers the blockage report.
// A vector rebuild drains PendingEmbeddingCount, which only the embedder can
// lower; without a healthy embedder the watcher would otherwise poll at 0% forever
// and the UI would show a run that can never progress. It must report "blocked".
//
// The chunks are inserted with HasEmbedding=true, then ResetVectorOnly flips them
// back to pending — so there IS work queued, which is the condition for the guard.
func TestRebuildCoordinator_VectorBlockedWithoutEmbedder(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	insertTestChunksSQLite(t, store, 3)

	prev := EmbedderHealthy()
	SetEmbedderHealthy(false)
	t.Cleanup(func() { SetEmbedderHealthy(prev) })

	coord := newTestCoordinator(t, store)
	require.NoError(t, coord.Start(store, RebuildVector))

	status := waitForCoordinatorTerminal(t, coord)
	assert.Equal(t, "blocked", status.Status,
		"a vector rebuild with pending work and no embedder must report blocked")
	assert.Contains(t, status.Error, "embedding service unavailable")
}

// ---------------------------------------------------------------------------
// watch: finish is ignored for a superseded generation
// ---------------------------------------------------------------------------

// TestRebuildCoordinator_FinishIgnoresSupersededGeneration asserts a stale watcher
// cannot revive state after Cancel bumped the generation. Without the guard the
// cancelled status would be overwritten by the abandoned run's own verdict.
func TestRebuildCoordinator_FinishIgnoresSupersededGeneration(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	coord := newTestCoordinator(t, store)

	require.NoError(t, coord.Start(store, RebuildFTS))
	coord.Cancel()

	// Simulate the superseded watcher waking up after Cancel: it still holds the
	// pre-Cancel generation.
	coord.mu.Lock()
	staleGen := coord.generation - 1
	coord.mu.Unlock()

	coord.finish(staleGen, "done", "", time.Now())

	assert.Equal(t, "cancelled", coord.GetStatus().Status,
		"a superseded watcher must not overwrite the terminal state")
	assert.False(t, coord.IsRunning())
}

// ---------------------------------------------------------------------------
// broadcastRebuild: hub/manager wiring
// ---------------------------------------------------------------------------

// TestBroadcastRebuild_NilHubIsNoOp asserts a missing hub is tolerated: the status
// endpoint is authoritative, so broadcast is best-effort and must not panic before
// the WebSocket layer exists.
func TestBroadcastRebuild_NilHubIsNoOp(t *testing.T) {
	assert.NotPanics(t, func() { broadcastRebuild(nil, RebuildStatus{Status: "done"}) })
}

// TestBroadcastRebuild_NilManagerIsNoOp covers a hub that exists but was built
// without a Manager. The guard must return instead of dereferencing nil.
func TestBroadcastRebuild_NilManagerIsNoOp(t *testing.T) {
	hub := ws.NewStreamHub(nil)
	assert.Nil(t, hub.Manager(), "precondition: this hub has no manager")
	assert.NotPanics(t, func() { broadcastRebuild(hub, RebuildStatus{Status: "done"}) })
}

// TestBroadcastRebuild_DeliversToDisconnectedSubscriber asserts the event actually
// reaches the fan-out layer with the documented type/event name. A disconnected
// subscription is used so the message lands in the replay buffer, which is
// observable without standing up a real WebSocket connection.
func TestBroadcastRebuild_DeliversToDisconnectedSubscriber(t *testing.T) {
	mgr := ws.NewManagerForTest()
	hub := mgr.StreamHub()

	sub := mgr.Subscribe(nil, nil, "client-1", "")
	require.NotNil(t, sub)
	mgr.DisconnectClient("client-1") // start the buffer window

	broadcastRebuild(hub, RebuildStatus{Kind: "fts", Status: "done", ProgressPct: 100})

	events := sub.GetBufferedEvents()
	require.NotEmpty(t, events, "the rebuild event must be buffered for replay")

	var found bool
	for _, ev := range events {
		if ev.Event != "rag_rebuild" {
			continue
		}
		found = true
		assert.Equal(t, ws.MessageTypeEvent, ev.Type)
		assert.NotEmpty(t, ev.ID, "each broadcast event carries a generated id")
	}
	assert.True(t, found, "expected a rag_rebuild event, got %+v", events)
}
