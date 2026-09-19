package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"clawbench/internal/forge"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProvider is a scriptable forge.Provider for syncer tests.
type fakeProvider struct {
	// pages maps item type -> page number -> result.
	pages map[forge.ItemType]map[int]forge.ListResult
	// comments maps item number -> comments.
	comments map[int][]forge.Comment
	// errOnPage, when set, fails the fetch of that page for that type.
	errOnPage map[forge.ItemType]int
	calls     []forge.ListOptions
}

func (f *fakeProvider) CurrentUser(context.Context) (forge.Author, error) {
	return forge.Author{Login: "me"}, nil
}

func (f *fakeProvider) ListItems(_ context.Context, opts forge.ListOptions) (forge.ListResult, error) {
	f.calls = append(f.calls, opts)
	if f.errOnPage != nil {
		if p, ok := f.errOnPage[opts.Type]; ok && p == opts.Page {
			return forge.ListResult{}, errors.New("boom")
		}
	}
	if byPage, ok := f.pages[opts.Type]; ok {
		if res, ok := byPage[opts.Page]; ok {
			return res, nil
		}
	}
	return forge.ListResult{}, nil
}

func (f *fakeProvider) GetItem(context.Context, forge.ItemType, int) (forge.Item, error) {
	return forge.Item{}, nil
}

func (f *fakeProvider) ListComments(_ context.Context, _ forge.ItemType, number, page, perPage int) ([]forge.Comment, error) {
	if page > 1 {
		return nil, nil
	}
	return f.comments[number], nil
}

// fakeSink records dispatched events.
type fakeSink struct {
	events []forge.Change
}

func (s *fakeSink) HandleChange(_ context.Context, _ service.ForgeRepoRef, _ forge.Item, ch forge.Change) {
	s.events = append(s.events, ch)
}

func testBinding() service.ProjectForge {
	return service.ProjectForge{
		ProjectPath: "/proj", Platform: "github", Host: "github.com",
		Owner: "acme", Repo: "widgets", Source: "manual",
	}
}

func issue(state string, updated time.Time) forge.Item {
	return forge.Item{
		Platform: forge.PlatformGitHub, Type: forge.ItemTypeIssue, Number: 1,
		State: forge.State(state), Title: "t", URL: "u", UpdatedAt: updated,
	}
}

// issueCreated is issue() with an explicit creation time, needed by the
// window-gating tests.
func issueCreated(state string, created, updated time.Time) forge.Item {
	it := issue(state, updated)
	it.CreatedAt = created
	return it
}

// TestForgeSyncer_FirstSyncQueriesBoundedWindow is the regression guard for the
// unbounded first sync: `since` must never be the zero time, or a large
// repository is walked from page 1 until GitHub rejects offset pagination past
// 10k items (HTTP 422) — the pass then fails, the watermark never advances, and
// every subsequent tick repeats the whole walk, exhausting the API quota.
func TestForgeSyncer_FirstSyncQueriesBoundedWindow(t *testing.T) {
	setupTestDBForForgeSync(t)
	provider := &fakeProvider{pages: map[forge.ItemType]map[int]forge.ListResult{
		forge.ItemTypeIssue: {1: {Items: []forge.Item{issue("open", time.Now().UTC())}}},
	}}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, &fakeSink{})

	require.NoError(t, syncer.SyncRepo(context.Background(), testBinding()))

	require.NotEmpty(t, provider.calls, "the first sync must query the provider")
	for _, c := range provider.calls {
		assert.False(t, c.Since.IsZero(),
			"first sync must bound the window (type=%s page=%d); a zero Since means a full-history walk", c.Type, c.Page)
	}
}

// TestForgeSyncer_FirstSyncBaselinesPreExistingItemSilently pins the other half
// of the bounded window: an old item that surfaces on the first sync (it was
// updated recently, but created long ago) must be recorded without emitting a
// transition derived from a state nobody observed.
func TestForgeSyncer_FirstSyncBaselinesPreExistingItemSilently(t *testing.T) {
	setupTestDBForForgeSync(t)
	now := time.Now().UTC()
	old := now.Add(-3 * 365 * 24 * time.Hour)

	provider := &fakeProvider{pages: map[forge.ItemType]map[int]forge.ListResult{
		forge.ItemTypeIssue: {1: {Items: []forge.Item{issueCreated("closed", old, now)}}},
	}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)

	require.NoError(t, syncer.SyncRepo(context.Background(), testBinding()))
	assert.Empty(t, sink.events, "a pre-existing item must not emit a derived transition")

	got, err := service.GetForgeItemSnapshot(testRepoKey(), "issue", 1)
	require.NoError(t, err)
	require.NotNil(t, got, "the item must still be snapshotted as the baseline")
	assert.Equal(t, "closed", got.State)
}

// TestForgeSyncer_LaterSyncReportsItemCreatedAfterWindow is the complement:
// once the baseline exists, an item created after the window really is new and
// must be reported.
func TestForgeSyncer_LaterSyncReportsItemCreatedAfterWindow(t *testing.T) {
	setupTestDBForForgeSync(t)
	sink := &fakeSink{}
	provider := &fakeProvider{pages: map[forge.ItemType]map[int]forge.ListResult{}}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)
	binding := testBinding()

	// First pass establishes the baseline (no items).
	require.NoError(t, syncer.SyncRepo(context.Background(), binding))
	require.Empty(t, sink.events)

	// A brand-new issue appears, created after the baseline window.
	created := time.Now().UTC().Add(time.Minute)
	provider.pages[forge.ItemTypeIssue] = map[int]forge.ListResult{
		1: {Items: []forge.Item{issueCreated("open", created, created)}},
	}
	require.NoError(t, syncer.SyncRepo(context.Background(), binding))

	require.Len(t, sink.events, 1)
	assert.Equal(t, forge.EventOpened, sink.events[0].Type)
	assert.Equal(t, 1, sink.events[0].Number)
}

// TestForgeSyncer_LaterSyncIgnoresOldItemFirstSeen covers the case that used to
// produce a false positive: an item created long before the window, first
// observed on a LATER pass (e.g. it was just updated). Deriving from its
// current state alone would claim it just closed.
func TestForgeSyncer_LaterSyncIgnoresOldItemFirstSeen(t *testing.T) {
	setupTestDBForForgeSync(t)
	sink := &fakeSink{}
	provider := &fakeProvider{pages: map[forge.ItemType]map[int]forge.ListResult{}}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)
	binding := testBinding()

	require.NoError(t, syncer.SyncRepo(context.Background(), binding))

	now := time.Now().UTC()
	old := now.Add(-3 * 365 * 24 * time.Hour)
	provider.pages[forge.ItemTypeIssue] = map[int]forge.ListResult{
		1: {Items: []forge.Item{issueCreated("closed", old, now)}},
	}
	require.NoError(t, syncer.SyncRepo(context.Background(), binding))

	assert.Empty(t, sink.events, "an old item first seen later must not report a transition")
	got, err := service.GetForgeItemSnapshot(testRepoKey(), "issue", 1)
	require.NoError(t, err)
	require.NotNil(t, got, "it must be baselined so the next pass can diff it")
}

func TestForgeSyncer_FirstSyncEstablishesBaselineWithoutEvents(t *testing.T) {
	setupTestDBForForgeSync(t)
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

	provider := &fakeProvider{pages: map[forge.ItemType]map[int]forge.ListResult{
		forge.ItemTypeIssue: {1: {Items: []forge.Item{issue("open", now)}}},
	}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)

	require.NoError(t, syncer.SyncRepo(context.Background(), testBinding()))
	assert.Empty(t, sink.events, "a first sync must not dispatch historical events")

	// The baseline snapshot and watermark are recorded.
	got, err := service.GetForgeItemSnapshot(
		service.ForgeRepoKey{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"}, "issue", 1,
	)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "open", got.State)

	wm, err := service.GetForgeSyncWatermark(
		service.ForgeRepoKey{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"},
	)
	require.NoError(t, err)
	assert.False(t, wm.IsZero())
}

func TestForgeSyncer_SecondSyncEmitsCloseEvent(t *testing.T) {
	setupTestDBForForgeSync(t)
	t0 := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)

	provider := &fakeProvider{pages: map[forge.ItemType]map[int]forge.ListResult{
		forge.ItemTypeIssue: {1: {Items: []forge.Item{issue("open", t0)}}},
	}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)
	binding := testBinding()

	require.NoError(t, syncer.SyncRepo(context.Background(), binding))
	require.Empty(t, sink.events)

	// The item is closed on the remote.
	provider.pages[forge.ItemTypeIssue] = map[int]forge.ListResult{
		1: {Items: []forge.Item{issue("closed", t1)}},
	}
	require.NoError(t, syncer.SyncRepo(context.Background(), binding))

	require.Len(t, sink.events, 1)
	assert.Equal(t, forge.EventClosed, sink.events[0].Type)
	assert.Equal(t, 1, sink.events[0].Number)
}

func TestForgeSyncer_WatermarkNotAdvancedWhenPaginationFails(t *testing.T) {
	setupTestDBForForgeSync(t)
	t0 := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

	// Page 1 has more pages; page 2 fails. The whole window is not read.
	provider := &fakeProvider{
		pages: map[forge.ItemType]map[int]forge.ListResult{
			forge.ItemTypeIssue: {1: {Items: []forge.Item{issue("open", t0)}, HasMore: true, NextPage: 2}},
		},
		errOnPage: map[forge.ItemType]int{forge.ItemTypeIssue: 2},
	}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)

	err := syncer.SyncRepo(context.Background(), testBinding())
	require.Error(t, err, "a mid-pagination failure must surface")

	// The watermark must remain unset so the next run retries the same window.
	wm, werr := service.GetForgeSyncWatermark(
		service.ForgeRepoKey{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"},
	)
	require.NoError(t, werr)
	assert.True(t, wm.IsZero(), "watermark must not advance when the window was not fully read")
}

func TestForgeSyncer_EventDedupedAcrossRuns(t *testing.T) {
	setupTestDBForForgeSync(t)
	t0 := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)

	provider := &fakeProvider{pages: map[forge.ItemType]map[int]forge.ListResult{
		forge.ItemTypeIssue: {1: {Items: []forge.Item{issue("open", t0)}}},
	}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)
	binding := testBinding()

	require.NoError(t, syncer.SyncRepo(context.Background(), binding))

	// Close, sync (event dispatched), then sync again with no further change but
	// the same updated_at — the overlap window re-fetches it, yet dedupe must
	// prevent a second dispatch.
	provider.pages[forge.ItemTypeIssue] = map[int]forge.ListResult{
		1: {Items: []forge.Item{issue("closed", t1)}},
	}
	require.NoError(t, syncer.SyncRepo(context.Background(), binding))
	require.Len(t, sink.events, 1)

	require.NoError(t, syncer.SyncRepo(context.Background(), binding))
	assert.Len(t, sink.events, 1, "the same event must never dispatch twice")
}

func TestForgeSyncer_CommentEvent(t *testing.T) {
	setupTestDBForForgeSync(t)
	t0 := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)

	provider := &fakeProvider{
		pages: map[forge.ItemType]map[int]forge.ListResult{
			forge.ItemTypeIssue: {1: {Items: []forge.Item{issue("open", t0)}}},
		},
		comments: map[int][]forge.Comment{
			1: {{ID: 10, UpdatedAt: t0}},
		},
	}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)
	binding := testBinding()

	require.NoError(t, syncer.SyncRepo(context.Background(), binding))
	require.Empty(t, sink.events)

	// A new comment arrives.
	provider.comments[1] = []forge.Comment{{ID: 11, UpdatedAt: t1}}
	require.NoError(t, syncer.SyncRepo(context.Background(), binding))

	require.Len(t, sink.events, 1)
	assert.Equal(t, forge.EventCommented, sink.events[0].Type)
	assert.Equal(t, int64(11), sink.events[0].CommentID)
}

func TestForgeSyncer_CommentFailureDoesNotAbortSync(t *testing.T) {
	setupTestDBForForgeSync(t)
	t0 := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

	provider := &fakeProvider{pages: map[forge.ItemType]map[int]forge.ListResult{
		forge.ItemTypeIssue: {1: {Items: []forge.Item{issue("open", t0)}}},
	}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)

	// First sync with comments unavailable still records the item snapshot.
	require.NoError(t, syncer.SyncRepo(context.Background(), testBinding()))
	got, err := service.GetForgeItemSnapshot(
		service.ForgeRepoKey{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"}, "issue", 1,
	)
	require.NoError(t, err)
	require.NotNil(t, got, "the item snapshot must be recorded even if comments fail")
}

func TestForgeSyncer_ProviderFactoryErrorSurfaces(t *testing.T) {
	setupTestDBForForgeSync(t)
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) {
		return nil, errors.New("no credentials")
	}, &fakeSink{})

	err := syncer.SyncRepo(context.Background(), testBinding())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no credentials")
}

// TestForgeSyncer_StateOnlyFirstPassDoesNotReplayComments is the regression
// guard for a real production bug: after binding a repository, every historical
// comment on every issue/PR was pushed as if it had just been posted.
//
// Trigger: the poller runs state passes every 60s but comment passes only every
// 5min. When a repository is bound, the next state tick (within a minute) wins,
// so the FIRST pass for that repo is state-only. It correctly records state and
// sets the watermark — so the following pass is no longer a "first sync" — but
// it has no comment data, leaving last_comment_id at 0. The next comment pass
// then compares 0 against every existing comment and reports them all as new.
func TestForgeSyncer_StateOnlyFirstPassDoesNotReplayComments(t *testing.T) {
	setupTestDBForForgeSync(t)
	t0 := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)

	provider := &fakeProvider{
		pages: map[forge.ItemType]map[int]forge.ListResult{
			forge.ItemTypeIssue: {1: {Items: []forge.Item{issue("closed", t0)}}},
		},
		// The item already carries an old comment from before the install.
		comments: map[int][]forge.Comment{
			1: {{ID: 500, UpdatedAt: t0}},
		},
	}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)
	binding := testBinding()

	// Simulate the poller's startup ordering: a state-only pass runs first
	// (IncludeComments=false) while the watermark is still zero.
	require.NoError(t, syncer.SyncRepoWithOptions(
		context.Background(), binding, service.SyncOptions{IncludeComments: false}))

	// The first pass must establish the baseline silently, even without
	// comment data.
	assert.Empty(t, sink.events, "a first pass must never dispatch historical events")

	// A subsequent comment-inclusive pass must not resurrect the old comment.
	require.NoError(t, syncer.SyncRepoWithOptions(
		context.Background(), binding, service.SyncOptions{IncludeComments: true}))
	assert.Empty(t, sink.events,
		"a pre-existing comment must not be reported as new after the baseline")

	// And a genuinely new comment still fires.
	t2 := t1.Add(time.Hour)
	provider.comments[1] = []forge.Comment{{ID: 501, UpdatedAt: t2}}
	require.NoError(t, syncer.SyncRepoWithOptions(
		context.Background(), binding, service.SyncOptions{IncludeComments: true}))
	require.Len(t, sink.events, 1)
	assert.Equal(t, forge.EventCommented, sink.events[0].Type)
}
