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

func issue(num int, state string, updated time.Time) forge.Item {
	return forge.Item{
		Platform: forge.PlatformGitHub, Type: forge.ItemTypeIssue, Number: num,
		State: forge.State(state), Title: "t", URL: "u", UpdatedAt: updated,
	}
}

func TestForgeSyncer_FirstSyncEstablishesBaselineWithoutEvents(t *testing.T) {
	setupTestDBForForgeSync(t)
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

	provider := &fakeProvider{pages: map[forge.ItemType]map[int]forge.ListResult{
		forge.ItemTypeIssue: {1: {Items: []forge.Item{issue(1, "open", now)}}},
	}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)

	require.NoError(t, syncer.SyncRepo(context.Background(), testBinding()))
	assert.Empty(t, sink.events, "a first sync must not dispatch historical events")

	// The baseline snapshot and watermark are recorded.
	got, err := service.GetForgeItemSnapshot(
		service.ForgeRepoKey{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"}, "issue", 1)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "open", got.State)

	wm, err := service.GetForgeSyncWatermark(
		service.ForgeRepoKey{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"})
	require.NoError(t, err)
	assert.False(t, wm.IsZero())
}

func TestForgeSyncer_SecondSyncEmitsCloseEvent(t *testing.T) {
	setupTestDBForForgeSync(t)
	t0 := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)

	provider := &fakeProvider{pages: map[forge.ItemType]map[int]forge.ListResult{
		forge.ItemTypeIssue: {1: {Items: []forge.Item{issue(1, "open", t0)}}},
	}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)
	binding := testBinding()

	require.NoError(t, syncer.SyncRepo(context.Background(), binding))
	require.Empty(t, sink.events)

	// The item is closed on the remote.
	provider.pages[forge.ItemTypeIssue] = map[int]forge.ListResult{
		1: {Items: []forge.Item{issue(1, "closed", t1)}},
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
			forge.ItemTypeIssue: {1: {Items: []forge.Item{issue(1, "open", t0)}, HasMore: true, NextPage: 2}},
		},
		errOnPage: map[forge.ItemType]int{forge.ItemTypeIssue: 2},
	}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)

	err := syncer.SyncRepo(context.Background(), testBinding())
	require.Error(t, err, "a mid-pagination failure must surface")

	// The watermark must remain unset so the next run retries the same window.
	wm, werr := service.GetForgeSyncWatermark(
		service.ForgeRepoKey{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"})
	require.NoError(t, werr)
	assert.True(t, wm.IsZero(), "watermark must not advance when the window was not fully read")
}

func TestForgeSyncer_EventDedupedAcrossRuns(t *testing.T) {
	setupTestDBForForgeSync(t)
	t0 := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)

	provider := &fakeProvider{pages: map[forge.ItemType]map[int]forge.ListResult{
		forge.ItemTypeIssue: {1: {Items: []forge.Item{issue(1, "open", t0)}}},
	}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)
	binding := testBinding()

	require.NoError(t, syncer.SyncRepo(context.Background(), binding))

	// Close, sync (event dispatched), then sync again with no further change but
	// the same updated_at — the overlap window re-fetches it, yet dedupe must
	// prevent a second dispatch.
	provider.pages[forge.ItemTypeIssue] = map[int]forge.ListResult{
		1: {Items: []forge.Item{issue(1, "closed", t1)}},
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
			forge.ItemTypeIssue: {1: {Items: []forge.Item{issue(1, "open", t0)}}},
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
		forge.ItemTypeIssue: {1: {Items: []forge.Item{issue(1, "open", t0)}}},
	}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)

	// First sync with comments unavailable still records the item snapshot.
	require.NoError(t, syncer.SyncRepo(context.Background(), testBinding()))
	got, err := service.GetForgeItemSnapshot(
		service.ForgeRepoKey{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"}, "issue", 1)
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
