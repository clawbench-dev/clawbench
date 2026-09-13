package service_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"clawbench/internal/forge"
	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingProvider counts how many times comments were fetched, so the
// comment-降频 behavior can be asserted.
type recordingProvider struct {
	commentCalls int
	items        []forge.Item
}

func (p *recordingProvider) CurrentUser(context.Context) (forge.Author, error) {
	return forge.Author{Login: "me"}, nil
}

func (p *recordingProvider) ListItems(_ context.Context, opts forge.ListOptions) (forge.ListResult, error) {
	if opts.Page > 1 {
		return forge.ListResult{}, nil
	}
	return forge.ListResult{Items: p.items}, nil
}

func (p *recordingProvider) GetItem(context.Context, forge.ItemType, int) (forge.Item, error) {
	return forge.Item{}, nil
}

func (p *recordingProvider) ListComments(context.Context, forge.ItemType, int, int, int) ([]forge.Comment, error) {
	p.commentCalls++
	return nil, nil
}

// TestForgeSyncer_StateOnlyPassSkipsComments verifies the comment-降频 flag is
// honored: a state-only pass must not fetch comments at all.
func TestForgeSyncer_StateOnlyPassSkipsComments(t *testing.T) {
	setupTestDBForForgeSync(t)
	provider := &recordingProvider{items: []forge.Item{{
		Platform: forge.PlatformGitHub, Type: forge.ItemTypeIssue, Number: 1,
		State: forge.StateOpen, UpdatedAt: time.Now(),
	}}}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, nil)
	binding := service.ProjectForge{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"}

	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), binding, service.SyncOptions{IncludeComments: false}))
	assert.Zero(t, provider.commentCalls, "a state-only pass must not fetch comments")

	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), binding, service.SyncOptions{IncludeComments: true}))
	assert.Greater(t, provider.commentCalls, 0, "a full pass must fetch comments")
}

// TestForgePoller_SyncNowDeduplicatesRepos verifies the poller fetches a repo
// once even when two projects are bound to it.
func TestForgePoller_SyncNowDeduplicatesRepos(t *testing.T) {
	setupTestDBForForgeSync(t)

	// Two projects, same repository.
	for range 2 {
		require.NoError(t, service.UpsertProjectForge(service.ProjectForge{
			ProjectPath: t.TempDir(), Platform: "github", Host: "github.com",
			Owner: "acme", Repo: "widgets", Source: "manual",
		}))
	}

	provider := &recordingProvider{}
	var factoryCalls int
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) {
		factoryCalls++
		return provider, nil
	}, nil)

	limiter := forge.NewLimiter(1000, 1000, 4)
	poller := service.NewForgePoller(syncer, limiter, func() model.Config { return model.Config{} })
	poller.SyncNow()

	assert.Equal(t, 1, factoryCalls, "the same repo bound to two projects must be polled once")
}

// TestForgePoller_SyncNowSkipsWhenNoBindings verifies an empty binding set is a
// cheap no-op.
func TestForgePoller_SyncNowSkipsWhenNoBindings(t *testing.T) {
	setupTestDBForForgeSync(t)
	called := false
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) {
		called = true
		return &recordingProvider{}, nil
	}, nil)
	poller := service.NewForgePoller(syncer, forge.NewLimiter(1000, 1000, 4), func() model.Config { return model.Config{} })

	poller.SyncNow()
	assert.False(t, called, "no bindings means no provider is built")
}

// TestForgePoller_StartStop verifies the lifecycle is clean and idempotent.
func TestForgePoller_StartStop(t *testing.T) {
	setupTestDBForForgeSync(t)
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) {
		return &recordingProvider{}, nil
	}, nil)
	poller := service.NewForgePoller(syncer, forge.NewLimiter(1000, 1000, 4), func() model.Config { return model.Config{} })

	poller.Start()
	poller.Start() // idempotent
	poller.Stop()
	poller.Stop() // idempotent
}

// TestForgePoller_GlobalLifecycle verifies the global start/stop helpers.
func TestForgePoller_GlobalLifecycle(t *testing.T) {
	setupTestDBForForgeSync(t)
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) {
		return &recordingProvider{}, nil
	}, nil)

	service.StartForgePoller(syncer, forge.NewLimiter(1000, 1000, 4), func() model.Config { return model.Config{} })
	service.StartForgePoller(syncer, forge.NewLimiter(1000, 1000, 4), func() model.Config { return model.Config{} }) // no-op
	assert.True(t, service.ForgePollerRunning())

	service.StopForgePoller()
	assert.False(t, service.ForgePollerRunning())
}

// TestForgePoller_PrunesStaleSnapshots verifies the sync cycle actually prunes
// snapshots for items the repo no longer returns. Without a production caller
// for PruneForgeItems the forge_items table would grow forever.
func TestForgePoller_PrunesStaleSnapshots(t *testing.T) {
	setupTestDBForForgeSync(t)

	require.NoError(t, service.UpsertProjectForge(service.ProjectForge{
		ProjectPath: t.TempDir(), Platform: "github", Host: "github.com",
		Owner: "acme", Repo: "widgets", Source: "manual",
	}))

	repoKey := service.ForgeRepoKey{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"}
	// A snapshot row that has not been seen for longer than the retention window.
	require.NoError(t, service.UpsertForgeItemSnapshot(service.ForgeItemSnapshot{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
		ItemType: "issue", Number: 999, State: "open",
	}))
	// Backdate it past the retention cutoff.
	_, err := service.UnsafeDBForTest().Exec(
		`UPDATE forge_items SET seen_at = ? WHERE number = 999`, time.Now().Add(-60*24*time.Hour),
	)
	require.NoError(t, err)

	// Sanity: the row must exist before the sync, or the assertion below would
	// pass vacuously.
	before, err := service.ListForgeItemSnapshots(repoKey)
	require.NoError(t, err)
	require.Len(t, before, 1, "precondition: the stale snapshot exists")

	provider := &recordingProvider{} // returns no items, so 999 is now stale
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) {
		return provider, nil
	}, nil)
	poller := service.NewForgePoller(syncer, forge.NewLimiter(1000, 1000, 4), func() model.Config { return model.Config{} })
	poller.SyncNow()

	snaps, err := service.ListForgeItemSnapshots(repoKey)
	require.NoError(t, err)
	for _, s := range snaps {
		assert.NotEqual(t, 999, s.Number, "a snapshot unseen past the retention window must be pruned")
	}
}

// failingProvider returns an error from ListItems, so the sync aborts and the
// poller must log-and-continue rather than panic or stop the loop.
type failingProvider struct{ err error }

func (p *failingProvider) CurrentUser(context.Context) (forge.Author, error) {
	return forge.Author{}, nil
}

func (p *failingProvider) ListItems(context.Context, forge.ListOptions) (forge.ListResult, error) {
	return forge.ListResult{}, p.err
}

func (p *failingProvider) GetItem(context.Context, forge.ItemType, int) (forge.Item, error) {
	return forge.Item{}, nil
}

func (p *failingProvider) ListComments(context.Context, forge.ItemType, int, int, int) ([]forge.Comment, error) {
	return nil, nil
}

// TestForgePoller_SyncNowSurvivesProviderError verifies a provider failure does
// not abort the whole cycle: the poller logs it and returns normally.
func TestForgePoller_SyncNowSurvivesProviderError(t *testing.T) {
	setupTestDBForForgeSync(t)
	require.NoError(t, service.UpsertProjectForge(service.ProjectForge{
		ProjectPath: t.TempDir(), Platform: "github", Host: "github.com",
		Owner: "acme", Repo: "widgets", Source: "manual",
	}))

	provider := &failingProvider{err: &forge.Error{Kind: forge.ErrKindNetwork, Message: "unreachable"}}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, nil)
	poller := service.NewForgePoller(syncer, forge.NewLimiter(1000, 1000, 4), func() model.Config { return model.Config{} })

	assert.NotPanics(t, poller.SyncNow)
}

// TestForgePoller_SyncNowSurvivesFactoryError verifies a provider-construction
// failure is also contained.
func TestForgePoller_SyncNowSurvivesFactoryError(t *testing.T) {
	setupTestDBForForgeSync(t)
	require.NoError(t, service.UpsertProjectForge(service.ProjectForge{
		ProjectPath: t.TempDir(), Platform: "github", Host: "github.com",
		Owner: "acme", Repo: "widgets", Source: "manual",
	}))

	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) {
		return nil, assert.AnError
	}, nil)
	poller := service.NewForgePoller(syncer, forge.NewLimiter(1000, 1000, 4), func() model.Config { return model.Config{} })

	assert.NotPanics(t, poller.SyncNow)
}

// TestForgeSyncer_ListErrorAbortsAndKeepsWatermark verifies a mid-pagination
// failure leaves the watermark untouched, so the next run re-reads the same
// window instead of skipping the items it never saw.
func TestForgeSyncer_ListErrorAbortsAndKeepsWatermark(t *testing.T) {
	setupTestDBForForgeSync(t)
	repoKey := service.ForgeRepoKey{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"}

	// Seed a watermark so the run is not a first sync.
	seeded := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, service.SetForgeSyncWatermark(repoKey, seeded))

	provider := &failingProvider{err: &forge.Error{Kind: forge.ErrKindServer, Message: "boom"}}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, nil)
	binding := service.ProjectForge{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"}

	err := syncer.SyncRepoWithOptions(context.Background(), binding, service.SyncOptions{})
	require.Error(t, err, "a list failure must surface to the caller")

	got, err := service.GetForgeSyncWatermark(repoKey)
	require.NoError(t, err)
	assert.WithinDuration(t, seeded, got, time.Second,
		"a failed run must not advance the watermark")
}

// TestForgeSyncer_ProviderFactoryError covers the factory-failure branch.
func TestForgeSyncer_ProviderFactoryError(t *testing.T) {
	setupTestDBForForgeSync(t)
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) {
		return nil, assert.AnError
	}, nil)
	binding := service.ProjectForge{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"}

	err := syncer.SyncRepoWithOptions(context.Background(), binding, service.SyncOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "build provider")
}

// TestForgeSyncer_WatermarkError covers the watermark-read failure branch.
func TestForgeSyncer_WatermarkError(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	cleanup := service.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})

	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) {
		return &recordingProvider{}, nil
	}, nil)
	binding := service.ProjectForge{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"}

	err = syncer.SyncRepoWithOptions(context.Background(), binding, service.SyncOptions{})
	require.Error(t, err, "a missing forge_sync_state table must surface")
	assert.Contains(t, err.Error(), "read watermark")
}

// TestForgeSyncer_FirstSyncWithNoItemsInitializesWatermark covers the branch
// that seeds the watermark on an empty first sync, so history is not re-fetched
// forever.
func TestForgeSyncer_FirstSyncWithNoItemsInitializesWatermark(t *testing.T) {
	setupTestDBForForgeSync(t)
	repoKey := service.ForgeRepoKey{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"}

	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) {
		return &recordingProvider{}, nil // no items
	}, nil)
	binding := service.ProjectForge{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"}

	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), binding, service.SyncOptions{}))

	wm, err := service.GetForgeSyncWatermark(repoKey)
	require.NoError(t, err)
	assert.False(t, wm.IsZero(), "an empty first sync must still seed the watermark")
}

// TestForgeSyncer_PaginationFollowsNextPage covers the paging loop: a page
// reporting hasMore must be followed by fetching the next page. Both item types
// are drained, so each type contributes two calls (page 1 then page 2).
func TestForgeSyncer_PaginationFollowsNextPage(t *testing.T) {
	setupTestDBForForgeSync(t)
	provider := &pagingProvider{pages: map[int][]forge.Item{
		1: {{
			Platform: forge.PlatformGitHub, Type: forge.ItemTypeIssue, Number: 1,
			State: forge.StateOpen, UpdatedAt: time.Now(),
		}},
		2: {{
			Platform: forge.PlatformGitHub, Type: forge.ItemTypeIssue, Number: 2,
			State: forge.StateOpen, UpdatedAt: time.Now(),
		}},
	}}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, nil)
	binding := service.ProjectForge{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"}

	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), binding, service.SyncOptions{}))
	assert.Equal(t, 4, provider.listCalls,
		"each item type must fetch page 1 then page 2 (hasMore is set)")

	// The second page's item must have been processed, not just fetched.
	snaps, err := service.ListForgeItemSnapshots(service.ForgeRepoKey{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
	})
	require.NoError(t, err)
	numbers := map[int]bool{}
	for _, s := range snaps {
		numbers[s.Number] = true
	}
	assert.True(t, numbers[1] && numbers[2], "both pages' items must be persisted")
}

// pagingProvider serves a fixed item set per page and reports hasMore until the
// last page.
type pagingProvider struct {
	pages     map[int][]forge.Item
	listCalls int
}

func (p *pagingProvider) CurrentUser(context.Context) (forge.Author, error) {
	return forge.Author{}, nil
}

func (p *pagingProvider) ListItems(_ context.Context, opts forge.ListOptions) (forge.ListResult, error) {
	p.listCalls++
	items := p.pages[opts.Page]
	_, hasNext := p.pages[opts.Page+1]
	return forge.ListResult{Items: items, HasMore: hasNext, NextPage: opts.Page + 1}, nil
}

func (p *pagingProvider) GetItem(context.Context, forge.ItemType, int) (forge.Item, error) {
	return forge.Item{}, nil
}

func (p *pagingProvider) ListComments(context.Context, forge.ItemType, int, int, int) ([]forge.Comment, error) {
	return nil, nil
}
