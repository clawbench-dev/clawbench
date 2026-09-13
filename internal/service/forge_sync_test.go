package service_test

import (
	"database/sql"
	"testing"
	"time"

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

	for _, ddl := range []string{service.ProjectForgesDDL, service.ForgeItemsDDL, service.ForgeSyncStateDDL, service.ForgeEventDDL} {
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
		ItemType: "issue", Number: 1, EventType: "closed", DedupeKey: "k1",
	}
	fresh, err := service.InsertForgeEvent(ev)
	require.NoError(t, err)
	assert.True(t, fresh, "the first insert is fresh")

	// The same dedupe key must not insert twice.
	fresh, err = service.InsertForgeEvent(ev)
	require.NoError(t, err)
	assert.False(t, fresh, "a duplicate dedupe key must be ignored")

	n, err := service.CountUnreadForgeEvents()
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	require.NoError(t, service.MarkForgeEventsRead())
	n, err = service.CountUnreadForgeEvents()
	require.NoError(t, err)
	assert.Equal(t, 0, n, "opening the tab clears the unread count")

	// A new event is unread again.
	_, err = service.InsertForgeEvent(service.ForgeEvent{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
		ItemType: "issue", Number: 1, EventType: "reopened", DedupeKey: "k2",
	})
	require.NoError(t, err)
	n, err = service.CountUnreadForgeEvents()
	require.NoError(t, err)
	assert.Equal(t, 1, n)
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

	count, err := service.CountUnreadForgeEvents()
	require.NoError(t, err)
	assert.Zero(t, count)

	assert.NoError(t, service.MarkForgeEventsRead())
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

	_, err = service.CountUnreadForgeEvents()
	require.Error(t, err)

	_, err = service.PruneForgeItems(repo, time.Now())
	require.Error(t, err)

	assert.Error(t, service.SetForgeSyncWatermark(repo, time.Now()))
	assert.Error(t, service.MarkForgeEventsRead())
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
