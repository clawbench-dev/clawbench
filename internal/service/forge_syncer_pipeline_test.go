package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/forge"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

// pipelineProvider is a fakeProvider that ALSO implements the optional
// forge.PipelineLister capability, which is how the syncer discovers CI
// support. Embedding the base fake keeps the item path working unchanged.
type pipelineProvider struct {
	fakeProvider
	runs []forge.PipelineRun
	// listCalls counts ListPipelineRuns invocations so a test can assert that
	// polling was skipped entirely.
	listCalls int
	// lastSince records the lower bound the syncer passed, so a test can assert
	// the watermark is threaded through.
	lastSince time.Time
}

// ListPipelineRuns returns every configured run regardless of `since`.
//
// The lower-bound filtering is the ADAPTER's job (each platform does it
// differently, and both are covered by their own tests). Re-filtering here
// would make these tests depend on the wall clock, because the syncer's `since`
// derives from the real clock via the watermark — runs would silently vanish on
// the second pass. These tests are about record/dispatch semantics, so the fake
// hands over everything and only records the bound.
func (p *pipelineProvider) ListPipelineRuns(_ context.Context, since time.Time, page, perPage int) (forge.PipelineRunPage, error) {
	p.listCalls++
	p.lastSince = since
	return forge.PipelineRunPage{Runs: p.runs}, nil
}

func pipelineRun(id int64, status forge.PipelineStatus, updated time.Time) forge.PipelineRun {
	return forge.PipelineRun{
		ID: id, Name: "CI", Number: int(id), Status: status,
		Ref: "main", SHA: "abc123", Event: "push", Actor: "octocat",
		URL:       "https://github.com/acme/widgets/actions/runs/1",
		CreatedAt: updated.Add(-time.Minute), UpdatedAt: updated,
	}
}

func pipelineSyncOptions() service.SyncOptions {
	return service.SyncOptions{IncludePipelines: true}
}

// TestSyncPipelines_FirstSyncDoesNotDispatchHistory is the anti-flood rule: a
// fresh install must not fire the task once for every run already in the
// repository's history.
func TestSyncPipelines_FirstSyncDoesNotDispatchHistory(t *testing.T) {
	setupTestDBForForgeSync(t)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

	provider := &pipelineProvider{runs: []forge.PipelineRun{
		pipelineRun(101, forge.PipelineSuccess, now),
		pipelineRun(100, forge.PipelineFailure, now.Add(-time.Hour)),
	}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)

	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), testBinding(), pipelineSyncOptions()))
	assert.Empty(t, sink.events, "a first sync must establish a baseline, not replay history")

	// The runs must still be recorded, or the next sync would treat them as new.
	recorded, err := service.ListForgePipelineRuns(service.ForgeRepoKey{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
	})
	require.NoError(t, err)
	assert.ElementsMatch(t, []int64{100, 101}, recorded)
}

// TestSyncPipelines_NewRunAfterBaselineFires is the payoff: once a baseline
// exists, a newly finished run dispatches.
func TestSyncPipelines_NewRunAfterBaselineFires(t *testing.T) {
	setupTestDBForForgeSync(t)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

	provider := &pipelineProvider{runs: []forge.PipelineRun{
		pipelineRun(100, forge.PipelineSuccess, now),
	}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)

	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), testBinding(), pipelineSyncOptions()))
	require.Empty(t, sink.events, "baseline pass")

	// A second run finishes.
	provider.runs = append(provider.runs, pipelineRun(101, forge.PipelineFailure, now.Add(time.Minute)))
	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), testBinding(), pipelineSyncOptions()))

	require.Len(t, sink.events, 1, "the new run must fire")
	assert.Equal(t, forge.EventPipeline, sink.events[0].Type)
	assert.Equal(t, int64(101), sink.events[0].PipelineRunID)
	require.NotNil(t, sink.events[0].Pipeline, "the run detail must travel with the event")
	assert.Equal(t, forge.PipelineFailure, sink.events[0].Pipeline.Status)
	assert.Equal(t, "octocat", sink.events[0].Actor, "actor is carried so the prompt can judge")
}

// TestSyncPipelines_InProgressRunFiresWhenItFinishes is the regression for the
// scalar-watermark design that was rejected: a run still going at first sight
// must NOT be recorded, or its completion would be lost.
func TestSyncPipelines_InProgressRunFiresWhenItFinishes(t *testing.T) {
	setupTestDBForForgeSync(t)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

	provider := &pipelineProvider{runs: []forge.PipelineRun{
		pipelineRun(100, forge.PipelineSuccess, now),
		pipelineRun(200, forge.PipelineRunning, now), // still going at baseline
	}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)

	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), testBinding(), pipelineSyncOptions()))
	require.Empty(t, sink.events, "baseline pass")

	// The in-flight run finishes.
	provider.runs[1] = pipelineRun(200, forge.PipelineFailure, now.Add(time.Minute))
	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), testBinding(), pipelineSyncOptions()))

	require.Len(t, sink.events, 1, "a run that was in progress at baseline must still fire")
	assert.Equal(t, int64(200), sink.events[0].PipelineRunID)
}

// TestSyncPipelines_OutOfOrderCompletionIsNotLost covers the exact failure mode
// a scalar watermark would produce: run 100 is in progress while 101 finishes
// first, so a "highest run id seen" watermark would skip 100 forever.
func TestSyncPipelines_OutOfOrderCompletionIsNotLost(t *testing.T) {
	setupTestDBForForgeSync(t)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

	provider := &pipelineProvider{runs: []forge.PipelineRun{
		pipelineRun(100, forge.PipelineRunning, now), // lower id, still going
		pipelineRun(101, forge.PipelineSuccess, now), // higher id, finishes first
	}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)

	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), testBinding(), pipelineSyncOptions()))
	require.Empty(t, sink.events, "baseline pass records only 101")

	// 101 re-appears (still recorded, must not fire again) and 100 finishes.
	provider.runs[0] = pipelineRun(100, forge.PipelineFailure, now.Add(time.Minute))
	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), testBinding(), pipelineSyncOptions()))

	require.Len(t, sink.events, 1, "the lower-id run must not be skipped")
	assert.Equal(t, int64(100), sink.events[0].PipelineRunID)
}

// TestSyncPipelines_SameRunDispatchesOnce guards the overlap window: the same
// run re-fetched on a later pass must not notify twice.
func TestSyncPipelines_SameRunDispatchesOnce(t *testing.T) {
	setupTestDBForForgeSync(t)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

	provider := &pipelineProvider{runs: []forge.PipelineRun{
		pipelineRun(100, forge.PipelineSuccess, now),
	}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)

	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), testBinding(), pipelineSyncOptions()))
	provider.runs = append(provider.runs, pipelineRun(101, forge.PipelineSuccess, now.Add(time.Minute)))

	// Three passes over the same set of runs.
	for range 3 {
		require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), testBinding(), pipelineSyncOptions()))
	}

	require.Len(t, sink.events, 1, "the same run must never dispatch twice")
	assert.Equal(t, int64(101), sink.events[0].PipelineRunID)
}

// TestSyncPipelines_TwoSuccessesBothFire is the end-to-end form of the F2
// regression: two SUCCESSFUL runs on one repository must both fire. Before the
// run-id dedupe revision they collided on "state:success" and the second was
// silently dropped.
func TestSyncPipelines_TwoSuccessesBothFire(t *testing.T) {
	setupTestDBForForgeSync(t)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

	provider := &pipelineProvider{runs: []forge.PipelineRun{
		pipelineRun(100, forge.PipelineSuccess, now),
	}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)
	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), testBinding(), pipelineSyncOptions()))

	for _, id := range []int64{101, 102} {
		provider.runs = append(provider.runs, pipelineRun(id, forge.PipelineSuccess, now.Add(time.Duration(id)*time.Minute)))
		require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), testBinding(), pipelineSyncOptions()))
	}

	require.Len(t, sink.events, 2, "each successful run is a distinct event")
	assert.Equal(t, int64(101), sink.events[0].PipelineRunID)
	assert.Equal(t, int64(102), sink.events[1].PipelineRunID)
}

// TestSyncPipelines_CancelledAndSkippedDoNotFire pins the trigger vocabulary:
// they are terminal (so recorded) but not worth waking a task for.
func TestSyncPipelines_CancelledAndSkippedDoNotFire(t *testing.T) {
	setupTestDBForForgeSync(t)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

	provider := &pipelineProvider{runs: []forge.PipelineRun{
		pipelineRun(100, forge.PipelineSuccess, now),
	}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)
	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), testBinding(), pipelineSyncOptions()))

	provider.runs = append(provider.runs,
		pipelineRun(101, forge.PipelineCancelled, now.Add(time.Minute)),
		pipelineRun(102, forge.PipelineSkipped, now.Add(2*time.Minute)),
		pipelineRun(103, forge.PipelineUnknown, now.Add(3*time.Minute)),
	)
	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), testBinding(), pipelineSyncOptions()))

	assert.Empty(t, sink.events, "cancelled/skipped/unknown must not fire a task")

	// But they ARE recorded, so they are not re-evaluated on every later pass.
	recorded, err := service.ListForgePipelineRuns(service.ForgeRepoKey{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
	})
	require.NoError(t, err)
	assert.ElementsMatch(t, []int64{100, 101, 102, 103}, recorded)
}

// TestSyncPipelines_SkippedWhenNotRequested: the capability is opt-in per pass,
// so the quota is not spent when no task subscribes.
func TestSyncPipelines_SkippedWhenNotRequested(t *testing.T) {
	setupTestDBForForgeSync(t)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

	provider := &pipelineProvider{runs: []forge.PipelineRun{
		pipelineRun(100, forge.PipelineSuccess, now),
	}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)

	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), testBinding(), service.SyncOptions{}))
	assert.Zero(t, provider.listCalls, "no pipeline request when IncludePipelines is off")
	assert.Empty(t, sink.events)
}

// TestSyncPipelines_ThreadsTheWatermarkThrough: the item watermark (minus the
// overlap window) must reach the adapter as the lower bound, or every pass would
// re-read the full history.
func TestSyncPipelines_ThreadsTheWatermarkThrough(t *testing.T) {
	setupTestDBForForgeSync(t)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

	// An established repo with a known watermark.
	watermark := now.Add(-time.Hour)
	require.NoError(t, service.SetForgeSyncWatermark(service.ForgeRepoKey{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
	}, watermark))

	provider := &pipelineProvider{runs: []forge.PipelineRun{
		pipelineRun(100, forge.PipelineSuccess, now),
	}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)

	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), testBinding(), pipelineSyncOptions()))

	assert.False(t, provider.lastSince.IsZero(), "the watermark must be threaded through as `since`")
	// The syncer subtracts an overlap window, so the bound is at or just before
	// the stored watermark.
	assert.WithinDuration(t, watermark, provider.lastSince, 5*time.Second,
		"since must be the watermark minus the overlap window")
}

// TestSyncPipelines_ProviderWithoutCapabilityIsNotAnError: a platform with no
// CI surface must be a silent no-op, not a sync failure.
func TestSyncPipelines_ProviderWithoutCapabilityIsNotAnError(t *testing.T) {
	setupTestDBForForgeSync(t)

	// The base fakeProvider deliberately does NOT implement PipelineLister.
	provider := &fakeProvider{pages: map[forge.ItemType]map[int]forge.ListResult{}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)

	err := syncer.SyncRepoWithOptions(context.Background(), testBinding(), pipelineSyncOptions())
	require.NoError(t, err, "an unsupported platform must not fail the sync")
	assert.Empty(t, sink.events)
}

// TestSyncPipelines_BaselineComesFromLedgerNotWatermark is the regression for a
// real bug: the CI baseline used to be derived from the ITEM watermark.
//
// A repository can be polled for months with no pipeline subscriber, so its item
// watermark is set while its CI ledger is empty. Keying "is this the first CI
// pass?" off that watermark answered "no", so every historical run newer than the
// watermark was dispatched — the exact flood the baseline exists to prevent.
func TestSyncPipelines_BaselineComesFromLedgerNotWatermark(t *testing.T) {
	setupTestDBForForgeSync(t)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

	// The repo has been polled before (watermark set), but CI has never run.
	require.NoError(t, service.SetForgeSyncWatermark(service.ForgeRepoKey{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
	}, now.Add(-time.Hour)))

	provider := &pipelineProvider{runs: []forge.PipelineRun{
		pipelineRun(100, forge.PipelineFailure, now),
		pipelineRun(101, forge.PipelineSuccess, now.Add(time.Minute)),
	}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)

	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), testBinding(), pipelineSyncOptions()))

	// The first CI pass must baseline, not replay: an empty ledger wins over a
	// set item watermark.
	assert.Empty(t, sink.events, "an empty CI ledger means baseline, whatever the item watermark says")

	recorded, err := service.ListForgePipelineRuns(service.ForgeRepoKey{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
	})
	require.NoError(t, err)
	assert.ElementsMatch(t, []int64{100, 101}, recorded, "the historical runs must still be recorded")

	// A genuinely new run after the baseline does fire.
	provider.runs = append(provider.runs, pipelineRun(102, forge.PipelineFailure, now.Add(2*time.Minute)))
	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), testBinding(), pipelineSyncOptions()))
	require.Len(t, sink.events, 1)
	assert.Equal(t, int64(102), sink.events[0].PipelineRunID)
}

// TestSyncPipelines_PartialBaselineDoesNotLeakHistory: a mid-walk failure leaves
// the ledger partly populated. The runs that were never seen are then treated as
// new, which is correct — they were never baselined, so they are genuinely
// unreported rather than a leak of already-known history.
func TestSyncPipelines_PartialBaselineDoesNotLeakHistory(t *testing.T) {
	setupTestDBForForgeSync(t)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

	// Pre-seed the ledger with one run, simulating a baseline that completed for
	// page 1 before failing on page 2.
	repo := service.ForgeRepoKey{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"}
	fresh, err := service.RecordPipelineRun(repo, 100)
	require.NoError(t, err)
	require.True(t, fresh)

	// A run that was never baselined arrives: it must fire, because the user was
	// never told about it.
	provider := &pipelineProvider{runs: []forge.PipelineRun{
		pipelineRun(100, forge.PipelineSuccess, now),
		pipelineRun(101, forge.PipelineFailure, now.Add(time.Minute)),
	}}
	sink := &fakeSink{}
	syncer := service.NewForgeSyncer(func(service.ProjectForge) (forge.Provider, error) { return provider, nil }, sink)

	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), testBinding(), pipelineSyncOptions()))
	require.Len(t, sink.events, 1, "the unbaselined run must fire")
	assert.Equal(t, int64(101), sink.events[0].PipelineRunID)
}

// TestAnyTaskSubscribesPipeline covers the quota gate: CI polling costs an extra
// request per repository, so it must only run when something is listening.
//
// It uses the scheduler DB (which has scheduled_tasks) rather than the forge DB.
func TestAnyTaskSubscribesPipeline(t *testing.T) {
	setupSchedulerDB(t)
	sched := service.NewScheduler()

	require.False(t, service.AnyTaskSubscribesPipelineForTest(),
		"no tasks at all means no CI polling")

	insert := func(status, triggerMode, eventTypes string) {
		t.Helper()
		task := &model.ScheduledTask{
			ProjectPath: "/proj", Name: "t",
			TriggerMode: triggerMode, EventTypes: eventTypes,
		}
		// A cron task must carry a valid expression: AddTask validates it, and
		// the point of this fixture is that a cron task is irrelevant even when
		// it names a pipeline event.
		if triggerMode == "cron" {
			task.CronExpr = "0 9 * * *"
		}
		require.NoError(t, sched.AddTask(task))
		// AddTask always creates an ACTIVE task (it resets Status), so a paused
		// fixture must be paused afterwards rather than created that way.
		if status == "paused" {
			sched.PauseTask(task.ID)
		}
	}

	// A cron task is irrelevant however it is spelled.
	insert("active", "cron", "pipeline_done")
	assert.False(t, service.AnyTaskSubscribesPipelineForTest(),
		"a cron task must not enable CI polling")

	// An event task that subscribes to something else is also irrelevant.
	insert("active", "event", "issue.opened")
	assert.False(t, service.AnyTaskSubscribesPipelineForTest(),
		"an unrelated subscription must not enable CI polling")

	// A paused pipeline task must not either.
	insert("paused", "event", "pipeline_done")
	assert.False(t, service.AnyTaskSubscribesPipelineForTest(),
		"a paused task must not enable CI polling")

	// The real thing.
	insert("active", "event", "pipeline_done")
	assert.True(t, service.AnyTaskSubscribesPipelineForTest(),
		"an active pipeline subscriber must enable CI polling")
}

// TestAnyTaskSubscribesPipeline_KindScopedSpellingIsNotEnough: a pipeline is a
// repository-level event, so the PR-scoped spelling (accepted for backward
// compatibility) must NOT turn CI polling on — it can never match.
func TestAnyTaskSubscribesPipeline_KindScopedSpellingIsNotEnough(t *testing.T) {
	setupSchedulerDB(t)
	sched := service.NewScheduler()

	require.NoError(t, sched.AddTask(&model.ScheduledTask{
		ProjectPath: "/proj", Name: "t", Status: "active",
		TriggerMode: "event", EventTypes: "pr.pipeline_done",
	}))

	assert.False(t, service.AnyTaskSubscribesPipelineForTest(),
		"the legacy PR-scoped spelling is not offered and must not enable polling")
}
