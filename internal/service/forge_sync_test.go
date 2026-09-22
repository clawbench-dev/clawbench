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
	wm, err := service.GetForgeSyncWatermark(repo, forge.ItemTypeIssue)
	require.NoError(t, err)
	assert.True(t, wm.IsZero(), "an unsynced repo has a zero watermark")

	set := time.Date(2026, 9, 10, 8, 30, 0, 0, time.UTC)
	require.NoError(t, service.SetForgeSyncWatermark(repo, forge.ItemTypeIssue, set))

	wm, err = service.GetForgeSyncWatermark(repo, forge.ItemTypeIssue)
	require.NoError(t, err)
	assert.Equal(t, set, wm.UTC())

	// Advancing replaces.
	later := set.Add(time.Hour)
	require.NoError(t, service.SetForgeSyncWatermark(repo, forge.ItemTypeIssue, later))
	wm, err = service.GetForgeSyncWatermark(repo, forge.ItemTypeIssue)
	require.NoError(t, err)
	assert.Equal(t, later, wm.UTC())
}

// TestForgeSyncWatermark_TypesAreIndependent is the storage-level half of the
// skipped-issue regression: the two item types must never share a cursor.
//
// They are fetched from endpoints with different time-filtering capabilities, so
// one pass can legitimately see a much newer timestamp for PRs than for issues.
// With a shared cursor that newer value would move the issue window past issues
// that were never fetched, and they would never be fetched again.
func TestForgeSyncWatermark_TypesAreIndependent(t *testing.T) {
	setupTestDBForForgeSync(t)
	repo := testRepoKey()

	issueWM := time.Date(2026, 9, 21, 14, 44, 53, 0, time.UTC)
	prWM := time.Date(2026, 9, 21, 15, 14, 26, 0, time.UTC)

	// The PR cursor starts NULL (no baseline) even when the issue cursor is set.
	require.NoError(t, service.SetForgeSyncWatermark(repo, forge.ItemTypeIssue, issueWM))
	got, err := service.GetForgeSyncWatermark(repo, forge.ItemTypeChangeRequest)
	require.NoError(t, err)
	assert.True(t, got.IsZero(), "setting the issue cursor must not seed the PR cursor")

	// Advancing one leaves the other untouched.
	require.NoError(t, service.SetForgeSyncWatermark(repo, forge.ItemTypeChangeRequest, prWM))

	got, err = service.GetForgeSyncWatermark(repo, forge.ItemTypeIssue)
	require.NoError(t, err)
	assert.Equal(t, issueWM, got.UTC(), "advancing the PR cursor must not move the issue cursor")

	got, err = service.GetForgeSyncWatermark(repo, forge.ItemTypeChangeRequest)
	require.NoError(t, err)
	assert.Equal(t, prWM, got.UTC())

	// A type with no cursor of its own is a silent no-op, not an error: the
	// pipeline baseline comes from its own per-run ledger.
	require.NoError(t, service.SetForgeSyncWatermark(repo, forge.ItemTypePipeline, prWM))
	got, err = service.GetForgeSyncWatermark(repo, forge.ItemTypePipeline)
	require.NoError(t, err)
	assert.True(t, got.IsZero(), "a type without a cursor must report the zero time")
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

	assert.NoError(t, service.SetForgeSyncWatermark(repo, forge.ItemTypeIssue, time.Now()))

	wm, err := service.GetForgeSyncWatermark(repo, forge.ItemTypeIssue)
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

	_, err = service.GetForgeSyncWatermark(repo, forge.ItemTypeIssue)
	require.Error(t, err)

	_, err = service.CountUnreadForgeEvents(repo)
	require.Error(t, err)

	_, err = service.PruneForgeItems(repo, time.Now())
	require.Error(t, err)

	assert.Error(t, service.SetForgeSyncWatermark(repo, forge.ItemTypeIssue, time.Now()))
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

// ── UnreadForgeItems ──

// insertEvent stores one forge event for the overview tests.
//
// It deliberately does not backdate anything: the query orders by
// `created_at DESC, id DESC`, and a later insert always has a >= timestamp with a
// higher id, so reverse-insertion order holds without fiddling with clocks.
func insertEvent(t *testing.T, itemKey, itemType string, number int, eventType, dedupe string) {
	t.Helper()
	_, err := service.InsertForgeEvent(service.ForgeEvent{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
		ItemType: itemType, Number: number, ItemKey: itemKey,
		EventType: eventType, DedupeKey: dedupe,
	})
	require.NoError(t, err)
}

// TestUnreadForgeItems_GroupsByItemAndReportsNewestEvent is the core contract:
// one row per item, labeled by its NEWEST event, with the total event count.
//
// The newest-event half is the mutation-sensitive part: a query that just did
// `SELECT DISTINCT item_key` (or grouped without pinning the row) would report
// whichever event_type SQLite happened to pick, so this asserts the exact value
// rather than merely "non-empty".
func TestUnreadForgeItems_GroupsByItemAndReportsNewestEvent(t *testing.T) {
	setupTestDBForForgeSync(t)

	// Three events on one PR: the newest is the comment.
	insertEvent(t, "pr/7", "pr", 7, "opened", "k1")
	insertEvent(t, "pr/7", "pr", 7, "reopened", "k2")
	insertEvent(t, "pr/7", "pr", 7, "commented", "k3")

	got, err := service.UnreadForgeItems(testRepoKey(), 0)
	require.NoError(t, err)
	require.Len(t, got, 1, "three events on one item are ONE row")

	it := got[0]
	assert.Equal(t, "pr/7", it.ItemKey)
	assert.Equal(t, "pr", it.ItemType)
	assert.Equal(t, 7, it.Number)
	assert.Equal(t, 3, it.EventCount)
	assert.Equal(t, "commented", it.EventType,
		"the label must come from the NEWEST event, not an arbitrary one")
	assert.Zero(t, it.RunID, "a PR has no run id")
}

// TestUnreadForgeItems_ReportsUnreadEventNotOlderReadOne: an item whose older
// events were read and which then got a new one must be labeled by the new one.
func TestUnreadForgeItems_ReportsUnreadEventNotOlderReadOne(t *testing.T) {
	setupTestDBForForgeSync(t)

	insertEvent(t, "pr/9", "pr", 9, "opened", "k1")
	// Read the item, then a fresh event arrives.
	require.NoError(t, service.MarkForgeEventsRead(testRepoKey(), "pr/9"))
	insertEvent(t, "pr/9", "pr", 9, "commented", "k2")

	got, err := service.UnreadForgeItems(testRepoKey(), 0)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "commented", got[0].EventType,
		"the item is unread because of the NEW event, so that is what labels it")
	assert.Equal(t, 2, got[0].EventCount, "the count spans all its events")
}

// TestUnreadForgeItems_PipelineCarriesRunID: a pipeline stores Number 0 for
// every run, so the run id is only recoverable from the key.
func TestUnreadForgeItems_PipelineCarriesRunID(t *testing.T) {
	setupTestDBForForgeSync(t)

	insertEvent(t, forge.PipelineItemKey(555), "pipeline", 0, "pipeline_done", "p555")

	got, err := service.UnreadForgeItems(testRepoKey(), 0)
	require.NoError(t, err)
	require.Len(t, got, 1)

	it := got[0]
	assert.Equal(t, "pipeline/run:555", it.ItemKey)
	assert.Equal(t, "pipeline", it.ItemType)
	assert.Zero(t, it.Number, "a pipeline has no item number")
	assert.Equal(t, int64(555), it.RunID, "the run id must be recovered from the key")
}

// TestUnreadForgeItems_TwoPipelineRunsDoNotCollapse: each run is its own row.
func TestUnreadForgeItems_TwoPipelineRunsDoNotCollapse(t *testing.T) {
	setupTestDBForForgeSync(t)

	insertEvent(t, forge.PipelineItemKey(100), "pipeline", 0, "pipeline_done", "p100")
	insertEvent(t, forge.PipelineItemKey(101), "pipeline", 0, "pipeline_done", "p101")

	got, err := service.UnreadForgeItems(testRepoKey(), 0)
	require.NoError(t, err)
	require.Len(t, got, 2, "two runs are two items, not one")
	// Newest activity first.
	assert.Equal(t, int64(101), got[0].RunID)
	assert.Equal(t, int64(100), got[1].RunID)
}

// TestUnreadForgeItems_ExcludesFullyReadItems
func TestUnreadForgeItems_ExcludesFullyReadItems(t *testing.T) {
	setupTestDBForForgeSync(t)

	insertEvent(t, "pr/1", "pr", 1, "closed", "k1")
	insertEvent(t, "pr/2", "pr", 2, "closed", "k2")
	require.NoError(t, service.MarkForgeEventsRead(testRepoKey(), "pr/1"))

	got, err := service.UnreadForgeItems(testRepoKey(), 0)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "pr/2", got[0].ItemKey)
}

// TestUnreadForgeItems_ScopesToRepo: another repository's events must not leak.
func TestUnreadForgeItems_ScopesToRepo(t *testing.T) {
	setupTestDBForForgeSync(t)

	insertEvent(t, "pr/1", "pr", 1, "closed", "k1")
	_, err := service.InsertForgeEvent(service.ForgeEvent{
		Platform: "github", Host: "github.com", Owner: "other", Repo: "thing",
		ItemType: "pr", Number: 1, ItemKey: "pr/1",
		EventType: "closed", DedupeKey: "k1",
	})
	require.NoError(t, err)

	got, err := service.UnreadForgeItems(testRepoKey(), 0)
	require.NoError(t, err)
	require.Len(t, got, 1, "only this repo's events count")
}

// TestUnreadForgeItems_ExcludesEmptyItemKey: legacy rows with no key cannot be
// acted on (there is nothing to pass back to the read endpoint), so they must
// not appear as phantom rows.
func TestUnreadForgeItems_ExcludesEmptyItemKey(t *testing.T) {
	setupTestDBForForgeSync(t)

	insertEvent(t, "", "pr", 3, "closed", "k1")

	got, err := service.UnreadForgeItems(testRepoKey(), 0)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestUnreadForgeItems_RespectsLimit
func TestUnreadForgeItems_RespectsLimit(t *testing.T) {
	setupTestDBForForgeSync(t)
	for i := 1; i <= 5; i++ {
		insertEvent(t, fmt.Sprintf("pr/%d", i), "pr", i, "closed", fmt.Sprintf("k%d", i))
	}

	got, err := service.UnreadForgeItems(testRepoKey(), 2)
	require.NoError(t, err)
	require.Len(t, got, 2)
	// Newest first: 5 then 4.
	assert.Equal(t, "pr/5", got[0].ItemKey)
	assert.Equal(t, "pr/4", got[1].ItemKey)
}

// ── ListForgeActivityItems (the read/unread filter) ──

// TestListForgeActivityItems_ReadFilterReturnsOnlyFullyReadItems is the core
// contract of the "read" view: an item shows up there only once EVERY one of its
// events has been read.
//
// This is the mutation-sensitive half of the feature. A filter implemented as a
// condition on `read_at` (rather than on the aggregate) would also return items
// that merely have one read event, so an item with a read older event plus a
// fresh unread one would leak into the read view — which is exactly the bug this
// asserts against.
func TestListForgeActivityItems_ReadFilterReturnsOnlyFullyReadItems(t *testing.T) {
	setupTestDBForForgeSync(t)

	insertEvent(t, "pr/1", "pr", 1, "closed", "k1")
	insertEvent(t, "pr/2", "pr", 2, "closed", "k2")
	// pr/3 gets a read event and then a NEW unread one: it is unread, not read.
	insertEvent(t, "pr/3", "pr", 3, "opened", "k3")
	require.NoError(t, service.MarkForgeEventsRead(testRepoKey(), "pr/3"))
	insertEvent(t, "pr/3", "pr", 3, "commented", "k4")

	require.NoError(t, service.MarkForgeEventsRead(testRepoKey(), "pr/1"))

	got, err := service.ListForgeActivityItems(testRepoKey(), service.ForgeActivityRead, 0)
	require.NoError(t, err)
	require.Len(t, got, 1, "only the fully read item belongs in the read view")
	assert.Equal(t, "pr/1", got[0].ItemKey)
	assert.True(t, got[0].Read, "a row in the read view must report itself read")
}

// TestListForgeActivityItems_UnreadFilterMatchesUnreadForgeItems pins the two
// spellings together: the generic filter's unread case must return exactly what
// the narrow UnreadForgeItems helper does.
func TestListForgeActivityItems_UnreadFilterMatchesUnreadForgeItems(t *testing.T) {
	setupTestDBForForgeSync(t)

	insertEvent(t, "pr/1", "pr", 1, "closed", "k1")
	insertEvent(t, "pr/2", "pr", 2, "closed", "k2")
	require.NoError(t, service.MarkForgeEventsRead(testRepoKey(), "pr/2"))

	viaFilter, err := service.ListForgeActivityItems(testRepoKey(), service.ForgeActivityUnread, 0)
	require.NoError(t, err)
	viaHelper, err := service.UnreadForgeItems(testRepoKey(), 0)
	require.NoError(t, err)

	require.Len(t, viaFilter, 1)
	require.Len(t, viaHelper, 1)
	assert.Equal(t, viaHelper[0].ItemKey, viaFilter[0].ItemKey)
	assert.False(t, viaFilter[0].Read, "an unread row must report itself unread")
}

// TestListForgeActivityItems_AllFilterReturnsBothStates: the "all" view is the
// union, newest activity first, with each row carrying its own state.
func TestListForgeActivityItems_AllFilterReturnsBothStates(t *testing.T) {
	setupTestDBForForgeSync(t)

	insertEvent(t, "pr/1", "pr", 1, "closed", "k1")
	require.NoError(t, service.MarkForgeEventsRead(testRepoKey(), "pr/1"))
	insertEvent(t, "pr/2", "pr", 2, "commented", "k2")

	got, err := service.ListForgeActivityItems(testRepoKey(), service.ForgeActivityAll, 0)
	require.NoError(t, err)
	require.Len(t, got, 2, "the all view is the union of both states")

	// Newest activity first: pr/2 was inserted last.
	assert.Equal(t, "pr/2", got[0].ItemKey)
	assert.False(t, got[0].Read)
	assert.Equal(t, "pr/1", got[1].ItemKey)
	assert.True(t, got[1].Read)
}

// TestListForgeActivityItems_ScopesToRepoAndSkipsEmptyKey: the filter must not
// loosen the two guards the unread query already had.
func TestListForgeActivityItems_ScopesToRepoAndSkipsEmptyKey(t *testing.T) {
	setupTestDBForForgeSync(t)

	insertEvent(t, "", "pr", 3, "closed", "k1")
	_, err := service.InsertForgeEvent(service.ForgeEvent{
		Platform: "github", Host: "github.com", Owner: "other", Repo: "thing",
		ItemType: "pr", Number: 1, ItemKey: "pr/1",
		EventType: "closed", DedupeKey: "k1",
	})
	require.NoError(t, err)

	got, err := service.ListForgeActivityItems(testRepoKey(), service.ForgeActivityAll, 0)
	require.NoError(t, err)
	assert.Empty(t, got, "another repo's rows and keyless rows are not this repo's activity")
}

// TestParseForgeActivityFilter covers the fallback contract: an unknown or empty
// value is the DEFAULT view, not an error and not "all".
func TestParseForgeActivityFilter(t *testing.T) {
	// A slice rather than a map: two of these inputs are whitespace variants
	// that must stay distinct from their trimmed spelling, and the keys are the
	// thing under test.
	cases := []struct {
		raw  string
		want service.ForgeActivityFilter
	}{
		{"", service.ForgeActivityUnread},
		{"unread", service.ForgeActivityUnread},
		{"read", service.ForgeActivityRead},
		{"all", service.ForgeActivityAll},
		{"bogus", service.ForgeActivityUnread},
		{"READ", service.ForgeActivityUnread},   // case-sensitive on purpose
		{" read ", service.ForgeActivityUnread}, // not trimmed: a query typo is not a value
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, service.ParseForgeActivityFilter(tc.raw), "input %q", tc.raw)
	}
}
