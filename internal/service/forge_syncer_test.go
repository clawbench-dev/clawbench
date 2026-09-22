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
	// itemsByNumber backs GetItem, which the syncer calls to fill in fields the
	// listing cannot supply (the PR head branch).
	itemsByNumber map[int]forge.Item
	// getItemErr, when set, fails every GetItem call.
	getItemErr error
	// getItemCalls counts GetItem calls, so a test can assert the per-item
	// lookup only happens when an event is actually dispatched.
	getItemCalls int
	calls        []forge.ListOptions
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

func (f *fakeProvider) GetItem(_ context.Context, typ forge.ItemType, number int) (forge.Item, error) {
	f.getItemCalls++
	if f.getItemErr != nil {
		return forge.Item{}, f.getItemErr
	}
	if f.itemsByNumber != nil {
		if it, ok := f.itemsByNumber[number]; ok {
			return it, nil
		}
	}
	return forge.Item{Type: typ, Number: number}, nil
}

func (f *fakeProvider) ListComments(_ context.Context, _ forge.ItemType, number, page, perPage int) ([]forge.Comment, error) {
	if page > 1 {
		return nil, nil
	}
	return f.comments[number], nil
}

// fakeSink records dispatched events, plus the item each was dispatched with so
// a test can assert the fields an event's prompt depends on.
type fakeSink struct {
	events []forge.Change
	items  []forge.Item
}

func (s *fakeSink) HandleChange(_ context.Context, _ service.ForgeRepoRef, item forge.Item, ch forge.Change) {
	s.events = append(s.events, ch)
	s.items = append(s.items, item)
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
		forge.ItemTypeIssue,
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
		forge.ItemTypeIssue,
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

// prItem builds a change request, mirroring issue() for the PR side. CreatedAt
// is left zero (unknown), which DeriveChanges treats as "new" — the same
// convention CreatedAt already documents.
func prItem(state string, updated time.Time) forge.Item {
	return forge.Item{
		Platform: forge.PlatformGitHub, Type: forge.ItemTypeChangeRequest, Number: 2,
		State: forge.State(state), Title: "t", URL: "u", UpdatedAt: updated,
	}
}

// TestForgeSyncer_PRPassDoesNotSkipIssues is the regression for the issue that
// silently vanished from a real install.
//
// The two item types are fetched from endpoints with different time-filtering
// capabilities: GitHub's issue list honors `since`, its PR list cannot (there
// is no such parameter), so the PR half re-reads the whole history and can take
// minutes. When both halves fed ONE cursor, a PR seen later in the same pass
// pushed the cursor past an issue the issue half had already finished looking
// for — and that issue was never fetched again.
//
// Concretely: the issue pass ran at 14:44 and saw nothing new; the PR pass then
// ran until it observed a PR updated at 15:14, moving the shared cursor to
// 15:14; an issue created at 15:13 fell outside every subsequent window.
func TestForgeSyncer_PRPassDoesNotSkipIssues(t *testing.T) {
	setupTestDBForForgeSync(t)
	repoKey := testRepoKey()

	// A baseline exists for both types, so neither pass is a first sync.
	base := time.Date(2026, 9, 21, 14, 0, 0, 0, time.UTC)
	require.NoError(t, service.SetForgeSyncWatermark(repoKey, forge.ItemTypeIssue, base))
	require.NoError(t, service.SetForgeSyncWatermark(repoKey, forge.ItemTypeChangeRequest, base))

	// The issue the user filed at 15:13 — one minute BEFORE the PR timestamp,
	// which is exactly why a shared cursor loses it.
	issueCreatedAt := time.Date(2026, 9, 21, 15, 13, 36, 0, time.UTC)
	// The PR the PR pass observes, updated a minute later.
	prUpdatedAt := time.Date(2026, 9, 21, 15, 14, 26, 0, time.UTC)

	provider := &fakeProvider{pages: map[forge.ItemType]map[int]forge.ListResult{
		forge.ItemTypeIssue: {
			1: {Items: []forge.Item{issueCreated("open", issueCreatedAt, issueCreatedAt)}},
		},
		forge.ItemTypeChangeRequest: {
			1: {Items: []forge.Item{prItem("open", prUpdatedAt)}},
		},
	}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)

	require.NoError(t, syncer.SyncRepo(context.Background(), testBinding()))

	// The issue was fetched, so it must have been reported.
	require.Len(t, sink.events, 2, "the new issue and the new PR must both be dispatched")
	assert.Equal(t, forge.EventOpened, sink.events[0].Type)
	assert.Equal(t, 1, sink.events[0].Number, "the issue must not be skipped by the PR pass")

	// The issue cursor must sit at the ISSUE's timestamp, not the PR's.
	issueWM, err := service.GetForgeSyncWatermark(repoKey, forge.ItemTypeIssue)
	require.NoError(t, err)
	assert.Equal(t, issueCreatedAt.UTC(), issueWM.UTC(),
		"the issue cursor must not be dragged forward by the PR pass")

	prWM, err := service.GetForgeSyncWatermark(repoKey, forge.ItemTypeChangeRequest)
	require.NoError(t, err)
	assert.Equal(t, prUpdatedAt.UTC(), prWM.UTC(),
		"the PR cursor advances independently")

	// And the next pass, with no new activity, must be silent rather than
	// re-reporting or losing anything.
	sink.events = nil
	require.NoError(t, syncer.SyncRepo(context.Background(), testBinding()))
	assert.Empty(t, sink.events, "an unchanged repo must produce no further events")
}

// TestForgeSyncer_MergedOutsideWindowIsSilent is the guard for the flood that
// enabling merge-state visibility would otherwise cause.
//
// Every historical PR currently sits in the snapshot table as "closed,
// unmerged", because GitHub's PR list endpoint never reported merge state. The
// moment that state becomes visible, a naive derivation sees closed→merged for
// each of them and announces a merge — hundreds of events and notifications for
// merges nobody just performed. The merge timestamp is what separates a merge
// that happened inside this window from one that predates the baseline.
func TestForgeSyncer_MergedOutsideWindowIsSilent(t *testing.T) {
	setupTestDBForForgeSync(t)
	repoKey := testRepoKey()

	base := time.Date(2026, 9, 21, 14, 0, 0, 0, time.UTC)
	require.NoError(t, service.SetForgeSyncWatermark(repoKey, forge.ItemTypeChangeRequest, base))

	// An old PR, first seen as closed/unmerged — the state the flood starts from.
	oldCreated := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	closed := prItem("closed", base.Add(time.Minute))
	closed.CreatedAt = oldCreated

	provider := &fakeProvider{pages: map[forge.ItemType]map[int]forge.ListResult{
		forge.ItemTypeChangeRequest: {1: {Items: []forge.Item{closed}}},
	}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)

	// Baseline pass: silent, because the PR was created long before the window.
	require.NoError(t, syncer.SyncRepo(context.Background(), testBinding()))
	require.Empty(t, sink.events, "a first-seen old PR is baselined silently")

	// Merge state becomes visible, and the PR was in fact merged months ago.
	oldMerge := time.Date(2026, 5, 3, 6, 50, 39, 0, time.UTC)
	merged := prItem("merged", base.Add(2*time.Minute))
	merged.CreatedAt = oldCreated
	merged.MergedAt = &oldMerge
	provider.pages[forge.ItemTypeChangeRequest] = map[int]forge.ListResult{
		1: {Items: []forge.Item{merged}},
	}
	require.NoError(t, syncer.SyncRepo(context.Background(), testBinding()))
	assert.Empty(t, sink.events,
		"a PR merged before the window must be baselined, not announced")

	// The merge is still recorded, so the state is not lost.
	got, err := service.GetForgeItemSnapshot(repoKey, "pr", 2)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, got.Merged, "the merge must still be recorded in the snapshot")

	// And it stays silent rather than re-firing on every later pass.
	sink.events = nil
	require.NoError(t, syncer.SyncRepo(context.Background(), testBinding()))
	assert.Empty(t, sink.events, "the suppression must be stable, not a one-pass reprieve")
}

// TestForgeSyncer_MergedInsideWindowIsReported is the complement: a merge that
// really did happen in this window must still be announced.
func TestForgeSyncer_MergedInsideWindowIsReported(t *testing.T) {
	setupTestDBForForgeSync(t)
	repoKey := testRepoKey()

	base := time.Date(2026, 9, 21, 14, 0, 0, 0, time.UTC)
	require.NoError(t, service.SetForgeSyncWatermark(repoKey, forge.ItemTypeChangeRequest, base))

	// First pass: an old PR, open, baselined silently.
	oldCreated := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	open := prItem("open", base.Add(time.Minute))
	open.CreatedAt = oldCreated

	provider := &fakeProvider{pages: map[forge.ItemType]map[int]forge.ListResult{
		forge.ItemTypeChangeRequest: {1: {Items: []forge.Item{open}}},
	}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)
	require.NoError(t, syncer.SyncRepo(context.Background(), testBinding()))
	require.Empty(t, sink.events, "a first-seen old PR is baselined silently")

	// Second pass: it was merged just now, so the merge is inside the window.
	mergeTime := time.Now().UTC().Add(time.Minute)
	merged := prItem("merged", mergeTime)
	merged.CreatedAt = oldCreated
	merged.MergedAt = &mergeTime
	provider.pages[forge.ItemTypeChangeRequest] = map[int]forge.ListResult{
		1: {Items: []forge.Item{merged}},
	}
	require.NoError(t, syncer.SyncRepo(context.Background(), testBinding()))

	require.Len(t, sink.events, 1)
	assert.Equal(t, forge.EventMerged, sink.events[0].Type,
		"a merge inside the window must be reported")
}

// TestForgeSyncer_PRSourceBranchIsEnrichedOnDispatch pins the head branch on a
// dispatched PR event.
//
// The windowed PR listing goes through the issues endpoint — the only one that
// accepts a time bound — and the issue shape carries no head ref. Without an
// explicit fill-in, every PR event task would silently lose its SOURCE_BRANCH
// prompt variable (RenderEventContext skips empty values), which is a change
// nobody would notice until a task that relied on the branch misbehaved.
func TestForgeSyncer_PRSourceBranchIsEnrichedOnDispatch(t *testing.T) {
	setupTestDBForForgeSync(t)
	repoKey := testRepoKey()

	base := time.Date(2026, 9, 21, 14, 0, 0, 0, time.UTC)
	require.NoError(t, service.SetForgeSyncWatermark(repoKey, forge.ItemTypeChangeRequest, base))

	// The listing (issues endpoint) has no head branch…
	listed := prItem("open", base.Add(time.Minute))
	listed.SourceBranch = ""
	// …but the item detail does.
	detail := listed
	detail.SourceBranch = "feature/x"

	provider := &fakeProvider{
		pages: map[forge.ItemType]map[int]forge.ListResult{
			forge.ItemTypeChangeRequest: {1: {Items: []forge.Item{listed}}},
		},
		itemsByNumber: map[int]forge.Item{2: detail},
	}

	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)

	require.NoError(t, syncer.SyncRepo(context.Background(), testBinding()))
	require.Len(t, sink.events, 1, "the new PR must be dispatched")

	got := sink.items[0]
	assert.Equal(t, "feature/x", got.SourceBranch,
		"a dispatched PR event must carry the head branch for the SOURCE_BRANCH variable")
	assert.Equal(t, 1, provider.getItemCalls, "the branch must be looked up once per dispatched event")

	// A pass that derives no events must not pay for the lookup.
	provider.getItemCalls = 0
	sink.events = nil
	sink.items = nil
	require.NoError(t, syncer.SyncRepo(context.Background(), testBinding()))
	assert.Empty(t, sink.events)
	assert.Zero(t, provider.getItemCalls,
		"a pass with no events must not fetch item details")
}

// TestForgeSyncer_SourceBranchLookupFailureIsNotFatal: losing the branch degrades
// one prompt variable, whereas failing the sync would lose the event entirely —
// and the snapshot is already written, so it would never be re-derived.
func TestForgeSyncer_SourceBranchLookupFailureIsNotFatal(t *testing.T) {
	setupTestDBForForgeSync(t)
	repoKey := testRepoKey()

	base := time.Date(2026, 9, 21, 14, 0, 0, 0, time.UTC)
	require.NoError(t, service.SetForgeSyncWatermark(repoKey, forge.ItemTypeChangeRequest, base))

	listed := prItem("open", base.Add(time.Minute))
	provider := &fakeProvider{
		pages: map[forge.ItemType]map[int]forge.ListResult{
			forge.ItemTypeChangeRequest: {1: {Items: []forge.Item{listed}}},
		},
		getItemErr: assert.AnError,
	}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)

	require.NoError(t, syncer.SyncRepo(context.Background(), testBinding()),
		"a failed branch lookup must not fail the sync")
	require.Len(t, sink.events, 1, "the event must still be dispatched")
}
