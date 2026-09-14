package service_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"clawbench/internal/forge"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupTestDBForForgeSync creates an in-memory SQLite with the forge sync tables.
func setupTestDBForForgeSync(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)

	for _, ddl := range []string{service.ProjectForgesDDL, service.ForgeItemsDDL, service.ForgeSyncStateDDL, service.ForgeEventDDL, service.ForgePipelineRunsDDL} {
		_, err := db.Exec(ddl)
		require.NoError(t, err)
	}

	cleanup := service.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})
	return db
}

func testRepoKey() service.ForgeRepoKey {
	return service.ForgeRepoKey{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"}
}

func TestForgeSnapshot_UpsertAndGet(t *testing.T) {
	setupTestDBForForgeSync(t)
	repo := testRepoKey()

	// No snapshot yet.
	got, err := service.GetForgeItemSnapshot(repo, "issue", 7)
	require.NoError(t, err)
	assert.Nil(t, got, "an unknown item has no snapshot")

	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	require.NoError(t, service.UpsertForgeItemSnapshot(service.ForgeItemSnapshot{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
		ItemType: "issue", Number: 7, State: "open",
		LastCommentID: 42, LastCommentUpdatedAt: now, ItemUpdatedAt: now,
	}))

	got, err = service.GetForgeItemSnapshot(repo, "issue", 7)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "open", got.State)
	assert.Equal(t, int64(42), got.LastCommentID)
	assert.Equal(t, now, got.LastCommentUpdatedAt.UTC())
}

func TestForgeSnapshot_UpsertReplaces(t *testing.T) {
	setupTestDBForForgeSync(t)
	repo := testRepoKey()

	require.NoError(t, service.UpsertForgeItemSnapshot(service.ForgeItemSnapshot{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
		ItemType: "pr", Number: 1, State: "open",
	}))
	require.NoError(t, service.UpsertForgeItemSnapshot(service.ForgeItemSnapshot{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
		ItemType: "pr", Number: 1, State: "merged", Merged: true, LastCommentID: 5,
	}))

	got, err := service.GetForgeItemSnapshot(repo, "pr", 1)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "merged", got.State)
	assert.True(t, got.Merged)
	assert.Equal(t, int64(5), got.LastCommentID)

	all, err := service.ListForgeItemSnapshots(repo)
	require.NoError(t, err)
	assert.Len(t, all, 1, "upsert must not create a duplicate row")
}

func TestForgeSnapshot_PruneRemovesStaleRows(t *testing.T) {
	db := setupTestDBForForgeSync(t)
	repo := testRepoKey()

	// Insert two rows, then age one by setting seen_at far in the past.
	require.NoError(t, service.UpsertForgeItemSnapshot(service.ForgeItemSnapshot{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
		ItemType: "issue", Number: 1, State: "open",
	}))
	require.NoError(t, service.UpsertForgeItemSnapshot(service.ForgeItemSnapshot{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
		ItemType: "issue", Number: 2, State: "open",
	}))
	_, err := db.Exec(
		`UPDATE forge_items SET seen_at = ? WHERE number = 2`,
		time.Now().Add(-48*time.Hour).UTC(),
	)
	require.NoError(t, err)

	removed, err := service.PruneForgeItems(repo, time.Now().Add(-24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed, "only the stale row is pruned")

	all, err := service.ListForgeItemSnapshots(repo)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, 1, all[0].Number, "the fresh row survives")
}

func TestForgeSyncWatermark_RoundTrip(t *testing.T) {
	setupTestDBForForgeSync(t)
	repo := testRepoKey()

	// Unset watermark is the zero time.
	wm, err := service.GetForgeSyncWatermark(repo)
	require.NoError(t, err)
	assert.True(t, wm.IsZero(), "an unsynced repo has a zero watermark")

	set := time.Date(2026, 9, 10, 8, 30, 0, 0, time.UTC)
	require.NoError(t, service.SetForgeSyncWatermark(repo, set))

	wm, err = service.GetForgeSyncWatermark(repo)
	require.NoError(t, err)
	assert.Equal(t, set, wm.UTC())

	// Advancing replaces.
	later := set.Add(time.Hour)
	require.NoError(t, service.SetForgeSyncWatermark(repo, later))
	wm, err = service.GetForgeSyncWatermark(repo)
	require.NoError(t, err)
	assert.Equal(t, later, wm.UTC())
}

func TestForgeEvents_InsertDedupesAndCountsUnread(t *testing.T) {
	setupTestDBForForgeSync(t)

	ev := service.ForgeEvent{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
		ItemType: "issue", Number: 1, ItemKey: "issue/1", EventType: "closed", DedupeKey: "k1",
	}
	fresh, err := service.InsertForgeEvent(ev)
	require.NoError(t, err)
	assert.True(t, fresh, "the first insert is fresh")

	// The same dedupe key must not insert twice.
	fresh, err = service.InsertForgeEvent(ev)
	require.NoError(t, err)
	assert.False(t, fresh, "a duplicate dedupe key must be ignored")

	repo := testRepoKey()
	n, err := service.CountUnreadForgeEvents(repo)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	require.NoError(t, service.MarkForgeEventsRead(repo, ""))
	n, err = service.CountUnreadForgeEvents(repo)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "marking read clears the unread count")

	// A new event is unread again.
	_, err = service.InsertForgeEvent(service.ForgeEvent{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
		ItemType: "issue", Number: 1, ItemKey: "issue/1", EventType: "reopened", DedupeKey: "k2",
	})
	require.NoError(t, err)
	n, err = service.CountUnreadForgeEvents(repo)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

// TestForgeEvents_CountsDistinctItemsNotEvents: the badge answers "how many
// items have new activity", so several events on one item must count once.
// Counting raw events is what made the badge number match nothing the user could
// see in the panel.
func TestForgeEvents_CountsDistinctItemsNotEvents(t *testing.T) {
	setupTestDBForForgeSync(t)
	repo := testRepoKey()

	insert := func(number int, eventType, key string) {
		t.Helper()
		_, err := service.InsertForgeEvent(service.ForgeEvent{
			Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
			ItemType: "issue", Number: number, ItemKey: fmt.Sprintf("issue/%d", number),
			EventType: eventType, DedupeKey: key,
		})
		require.NoError(t, err)
	}

	// Three changes to issue 1, one to issue 2.
	insert(1, "opened", "a")
	insert(1, "commented", "b")
	insert(1, "closed", "c")
	insert(2, "opened", "d")

	n, err := service.CountUnreadForgeEvents(repo)
	require.NoError(t, err)
	assert.Equal(t, 2, n, "four events across two items is two unread items")
}

// TestForgeEvents_PipelineRunsDoNotCollapse: every CI run carries number 0, so a
// key built from (item_type, number) would fold all of them into one bucket and
// undercount. The run id must be part of the key.
//
// The keys are built via forge.ItemKey — the function production uses — rather
// than hardcoded, so this actually exercises the key derivation instead of
// asserting that two distinct literals are distinct.
func TestForgeEvents_PipelineRunsDoNotCollapse(t *testing.T) {
	setupTestDBForForgeSync(t)
	repo := testRepoKey()

	for _, runID := range []int64{100, 101} {
		change := forge.Change{Type: forge.EventPipeline, PipelineRunID: runID}
		key := forge.ItemKey(forge.ItemTypePipeline, 0, change)
		_, err := service.InsertForgeEvent(service.ForgeEvent{
			Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
			ItemType: "pipeline", Number: 0, ItemKey: key,
			EventType: "pipeline_done", DedupeKey: fmt.Sprintf("run-%d", runID),
		})
		require.NoError(t, err)
	}

	n, err := service.CountUnreadForgeEvents(repo)
	require.NoError(t, err)
	assert.Equal(t, 2, n, "two runs are two unread items, not one")
}

// TestItemKey_Shape pins the key format, so a change to it cannot silently
// invalidate stored rows.
func TestItemKey_Shape(t *testing.T) {
	assert.Equal(t, "issue/42", forge.ItemKeyForNumber(forge.ItemTypeIssue, 42))
	assert.Equal(t, "pr/7", forge.ItemKeyForNumber(forge.ItemTypeChangeRequest, 7))

	// A pipeline has no item number: the run id must carry the identity.
	assert.Equal(t, "pipeline/run:555",
		forge.ItemKey(forge.ItemTypePipeline, 0, forge.Change{PipelineRunID: 555}))
	assert.NotEqual(t,
		forge.ItemKey(forge.ItemTypePipeline, 0, forge.Change{PipelineRunID: 1}),
		forge.ItemKey(forge.ItemTypePipeline, 0, forge.Change{PipelineRunID: 2}),
		"two runs must not share a key")
}

// TestForgeEvents_ScopedToRepo: the badge points at one project's repository, so
// another repository's activity must not inflate it.
func TestForgeEvents_ScopedToRepo(t *testing.T) {
	setupTestDBForForgeSync(t)

	_, err := service.InsertForgeEvent(service.ForgeEvent{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
		ItemType: "issue", Number: 1, ItemKey: "issue/1", EventType: "opened", DedupeKey: "mine",
	})
	require.NoError(t, err)
	_, err = service.InsertForgeEvent(service.ForgeEvent{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "other",
		ItemType: "issue", Number: 9, ItemKey: "issue/9", EventType: "opened", DedupeKey: "theirs",
	})
	require.NoError(t, err)

	n, err := service.CountUnreadForgeEvents(testRepoKey())
	require.NoError(t, err)
	assert.Equal(t, 1, n, "only the requested repository counts")
}

// TestForgeEvents_MarkOneItemRead: opening one row must leave the others unread.
func TestForgeEvents_MarkOneItemRead(t *testing.T) {
	setupTestDBForForgeSync(t)
	repo := testRepoKey()

	for i, key := range []string{"issue/1", "issue/2"} {
		_, err := service.InsertForgeEvent(service.ForgeEvent{
			Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
			ItemType: "issue", Number: i + 1, ItemKey: key, EventType: "opened",
			DedupeKey: key,
		})
		require.NoError(t, err)
	}

	require.NoError(t, service.MarkForgeEventsRead(repo, "issue/1"))

	n, err := service.CountUnreadForgeEvents(repo)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "marking one item read must not clear the other")

	keys, err := service.UnreadForgeItemKeys(repo)
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"issue/2": true}, keys)
}

// TestForgeEvents_ReArmsAfterNewActivity: marking read only touches existing
// rows, so later activity on the same item goes unread again with no watermark.
func TestForgeEvents_ReArmsAfterNewActivity(t *testing.T) {
	setupTestDBForForgeSync(t)
	repo := testRepoKey()

	_, err := service.InsertForgeEvent(service.ForgeEvent{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
		ItemType: "issue", Number: 1, ItemKey: "issue/1", EventType: "opened", DedupeKey: "a",
	})
	require.NoError(t, err)
	require.NoError(t, service.MarkForgeEventsRead(repo, "issue/1"))

	_, err = service.InsertForgeEvent(service.ForgeEvent{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
		ItemType: "issue", Number: 1, ItemKey: "issue/1", EventType: "commented", DedupeKey: "b",
	})
	require.NoError(t, err)

	n, err := service.CountUnreadForgeEvents(repo)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "new activity on a read item is unread again")
}

// TestPruneForgeEvents_KeepsUnread: an unread row is the user's only record that
// something changed, so pruning must never drop one.
func TestPruneForgeEvents_KeepsUnread(t *testing.T) {
	setupTestDBForForgeSync(t)
	repo := testRepoKey()
	old := time.Now().Add(-90 * 24 * time.Hour)

	insert := func(key, dedupe string) {
		t.Helper()
		_, err := service.InsertForgeEvent(service.ForgeEvent{
			Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
			ItemType: "issue", Number: 1, ItemKey: key, EventType: "opened", DedupeKey: dedupe,
		})
		require.NoError(t, err)
	}
	insert("issue/1", "read-old")
	insert("issue/2", "unread-old")

	// Age both rows, then mark only the first read.
	require.NoError(t, service.SetForgeEventCreatedAtForTest(repo, "issue/1", old))
	require.NoError(t, service.SetForgeEventCreatedAtForTest(repo, "issue/2", old))
	require.NoError(t, service.MarkForgeEventsRead(repo, "issue/1"))

	removed, err := service.PruneForgeEvents(repo, time.Now().Add(-30*24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed, "only the old read row is eligible")

	n, err := service.CountUnreadForgeEvents(repo)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "the old UNREAD row must survive")
}

// TestSchema_ForgeSyncTablesExist verifies InitDB creates the sync tables.
func TestSchema_ForgeSyncTablesExist(t *testing.T) {
	setupTestDBForForgeSync(t)
	db := service.ReadDB()
	for _, table := range []string{"forge_items", "forge_sync_state", "forge_events"} {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		require.NoError(t, err, "table %s must exist", table)
	}
}

// TestForgeSync_NilDBGuards covers the "no database" branches of the snapshot,
// watermark and event accessors. The forge integration is optional, so every
// one of them must be a silent no-op rather than a panic.
func TestForgeSync_NilDBGuards(t *testing.T) {
	cleanup := service.SetDBForTest(nil, nil)
	t.Cleanup(cleanup)

	repo := testRepoKey()

	s, err := service.GetForgeItemSnapshot(repo, "issue", 1)
	require.NoError(t, err)
	assert.Nil(t, s)

	assert.NoError(t, service.UpsertForgeItemSnapshot(service.ForgeItemSnapshot{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
		ItemType: "issue", Number: 1, State: "open",
	}))

	n, err := service.PruneForgeItems(repo, time.Now())
	require.NoError(t, err)
	assert.Zero(t, n)

	list, err := service.ListForgeItemSnapshots(repo)
	require.NoError(t, err)
	assert.Nil(t, list)

	assert.NoError(t, service.SetForgeSyncWatermark(repo, time.Now()))

	wm, err := service.GetForgeSyncWatermark(repo)
	require.NoError(t, err)
	assert.True(t, wm.IsZero())

	fresh, err := service.InsertForgeEvent(service.ForgeEvent{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
		ItemType: "issue", Number: 1, EventType: "opened", DedupeKey: "k",
	})
	require.NoError(t, err)
	assert.False(t, fresh)

	count, err := service.CountUnreadForgeEvents(repo)
	require.NoError(t, err)
	assert.Zero(t, count)

	assert.NoError(t, service.MarkForgeEventsRead(repo, ""))
}

// TestForgeSync_QueryErrors covers the error branches: a query against a
// database that lacks the forge tables must surface, not be swallowed.
func TestForgeSync_QueryErrors(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	cleanup := service.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})

	repo := testRepoKey()

	_, err = service.GetForgeItemSnapshot(repo, "issue", 1)
	require.Error(t, err)

	_, err = service.ListForgeItemSnapshots(repo)
	require.Error(t, err)

	_, err = service.GetForgeSyncWatermark(repo)
	require.Error(t, err)

	_, err = service.CountUnreadForgeEvents(repo)
	require.Error(t, err)

	_, err = service.PruneForgeItems(repo, time.Now())
	require.Error(t, err)

	assert.Error(t, service.SetForgeSyncWatermark(repo, time.Now()))
	assert.Error(t, service.MarkForgeEventsRead(repo, ""))
}

// TestForgeSnapshot_NullableTimestamps covers the NullTime handling: a snapshot
// written without comment/item timestamps must read back as zero times rather
// than a scan error, and one written with them must round-trip.
func TestForgeSnapshot_NullableTimestamps(t *testing.T) {
	setupTestDBForForgeSync(t)
	repo := testRepoKey()

	// No timestamps: the NULL columns must decode to zero values.
	require.NoError(t, service.UpsertForgeItemSnapshot(service.ForgeItemSnapshot{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
		ItemType: "issue", Number: 1, State: "open",
	}))
	got, err := service.GetForgeItemSnapshot(repo, "issue", 1)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, got.LastCommentUpdatedAt.IsZero())
	assert.True(t, got.ItemUpdatedAt.IsZero())

	// With timestamps: they must round-trip (truncated to the second by SQLite).
	commentAt := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	itemAt := time.Date(2026, 9, 2, 11, 30, 0, 0, time.UTC)
	require.NoError(t, service.UpsertForgeItemSnapshot(service.ForgeItemSnapshot{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
		ItemType: "issue", Number: 2, State: "closed",
		LastCommentID: 9, LastCommentUpdatedAt: commentAt, ItemUpdatedAt: itemAt,
		CommentsBaselined: true,
	}))
	got, err = service.GetForgeItemSnapshot(repo, "issue", 2)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int64(9), got.LastCommentID)
	assert.WithinDuration(t, commentAt, got.LastCommentUpdatedAt, time.Second)
	assert.WithinDuration(t, itemAt, got.ItemUpdatedAt, time.Second)
	assert.True(t, got.CommentsBaselined)

	// ListForgeItemSnapshots applies the same NULL handling.
	list, err := service.ListForgeItemSnapshots(repo)
	require.NoError(t, err)
	require.Len(t, list, 2)
	for _, s := range list {
		if s.Number == 1 {
			assert.True(t, s.LastCommentUpdatedAt.IsZero())
			assert.True(t, s.ItemUpdatedAt.IsZero())
		}
	}
}

// TestPruneForgeEvents_RespectsCutoff: only rows older than the cutoff go.
func TestPruneForgeEvents_RespectsCutoff(t *testing.T) {
	setupTestDBForForgeSync(t)
	repo := testRepoKey()
	now := time.Now()

	_, err := service.InsertForgeEvent(service.ForgeEvent{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
		ItemType: "issue", Number: 1, ItemKey: "issue/1", EventType: "opened", DedupeKey: "recent",
	})
	require.NoError(t, err)
	require.NoError(t, service.MarkForgeEventsRead(repo, ""))

	// A recent read row survives a 30-day cutoff.
	removed, err := service.PruneForgeEvents(repo, now.Add(-30*24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(0), removed, "a recent row must not be pruned")

	// Backdated past the cutoff, it goes.
	require.NoError(t, service.SetForgeEventCreatedAtForTest(repo, "issue/1", now.Add(-90*24*time.Hour)))
	removed, err = service.PruneForgeEvents(repo, now.Add(-30*24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed)
}

// TestForgeSyncer_WritesItemKey is the guard for the ONLY production writer of
// item_key (forge_syncer.go's persistAndDispatch).
//
// Every other event test builds service.ForgeEvent{ItemKey: "..."} by hand, so
// they exercise the read side against a literal the writer is never proven to
// produce. Deleting the writer's ItemKey line left the whole suite green while
// every row got item_key = ”, which both CountUnreadForgeEvents and
// UnreadForgeItemKeys filter out — the badge would read 0 forever and no row
// would ever show a dot, i.e. exactly the bug this feature fixed.
//
// So this drives the real SyncRepo path and asserts on the stored column.
func TestForgeSyncer_WritesItemKey(t *testing.T) {
	setupTestDBForForgeSync(t)
	t0 := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)

	provider := &fakeProvider{pages: map[forge.ItemType]map[int]forge.ListResult{
		forge.ItemTypeIssue: {1: {Items: []forge.Item{issue("open", t0)}}},
	}}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, &fakeSink{})
	binding := testBinding()

	// First pass establishes the baseline (no events), second emits a close.
	require.NoError(t, syncer.SyncRepo(context.Background(), binding))
	provider.pages[forge.ItemTypeIssue] = map[int]forge.ListResult{
		1: {Items: []forge.Item{issue("closed", t1)}},
	}
	require.NoError(t, syncer.SyncRepo(context.Background(), binding))

	var key string
	require.NoError(t, service.ReadDB().QueryRow(
		`SELECT item_key FROM forge_events WHERE number = 1`,
	).Scan(&key))
	assert.Equal(t, "issue/1", key,
		"the syncer must store the item key; an empty key makes the row invisible to the badge")

	// And it must be countable through the same query the badge uses.
	n, err := service.CountUnreadForgeEvents(testRepoKey())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

// TestForgeSyncer_WritesPipelineItemKey: a pipeline event has number 0, so its
// key must carry the run id. Every run must land in its own bucket.
//
// Uses the pipelineProvider fake (which implements the optional PipelineLister)
// and drives the real sync path, so it fails if persistAndDispatch stops writing
// the key for the synthetic pipeline item.
func TestForgeSyncer_WritesPipelineItemKey(t *testing.T) {
	setupTestDBForForgeSync(t)
	t0 := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)

	provider := &pipelineProvider{runs: []forge.PipelineRun{pipelineRun(100, forge.PipelineSuccess, t0)}}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, &fakeSink{})

	// First pass baselines the run (recorded, not dispatched).
	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), testBinding(), pipelineSyncOptions()))

	// A second run appears.
	provider.runs = append(provider.runs, pipelineRun(101, forge.PipelineSuccess, t1))
	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), testBinding(), pipelineSyncOptions()))

	rows, err := service.ReadDB().Query(
		`SELECT item_key FROM forge_events WHERE item_type = 'pipeline' ORDER BY item_key`,
	)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var keys []string
	for rows.Next() {
		var k string
		require.NoError(t, rows.Scan(&k))
		keys = append(keys, k)
	}
	require.NoError(t, rows.Err())

	// Run 100 was baselined on the first pass, so only 101 dispatched.
	require.Len(t, keys, 1, "only the run that appeared after the baseline fires")
	assert.Equal(t, "pipeline/run:101", keys[0],
		"the key must carry the run id, not a shared number-0 bucket")

	n, err := service.CountUnreadForgeEvents(testRepoKey())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}
