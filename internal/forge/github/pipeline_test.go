package github

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/forge"
)

// TestListPipelineRuns_StopsAtTheSinceBoundary is the regression for a real
// bug: `since` is filtered locally on this platform, so a page whose runs are
// ALL older than the watermark came back empty while still advertising the next
// page. The caller walked the repository's entire run history on every poll.
func TestListPipelineRuns_StopsAtTheSinceBoundary(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// GitHub advertises more pages...
		w.Header().Set("Link", `<https://api.github.com/repos/acme/widgets/actions/runs?page=2>; rel="next"`)
		// ...and returns only runs older than the watermark.
		_, _ = w.Write([]byte(`{"workflow_runs":[
			{"id":1,"name":"CI","status":"completed","conclusion":"success",
			 "created_at":"2026-09-01T10:00:00Z","updated_at":"2026-09-01T10:05:00Z"}
		]}`))
	}))

	since := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	res, err := p.ListPipelineRuns(context.Background(), since, 1, 30)
	require.NoError(t, err)

	assert.Empty(t, res.Runs, "everything on this page is older than the watermark")
	assert.False(t, res.HasMore, "the since boundary ends the walk even though the platform has more pages")
	assert.Zero(t, res.NextPage, "no next page once the boundary is reached")
}

// TestListPipelineRuns_KeepsPagingBeforeTheBoundary: a page with at least one
// run inside the window must still advertise the next page, or a busy repo
// would truncate its results.
func TestListPipelineRuns_KeepsPagingBeforeTheBoundary(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Link", `<https://api.github.com/repos/acme/widgets/actions/runs?page=2>; rel="next"`)
		_, _ = w.Write([]byte(`{"workflow_runs":[
			{"id":2,"name":"CI","status":"completed","conclusion":"success",
			 "created_at":"2026-09-14T12:00:00Z","updated_at":"2026-09-14T12:05:00Z"},
			{"id":1,"name":"CI","status":"completed","conclusion":"success",
			 "created_at":"2026-09-01T10:00:00Z","updated_at":"2026-09-01T10:05:00Z"}
		]}`))
	}))

	since := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	res, err := p.ListPipelineRuns(context.Background(), since, 1, 30)
	require.NoError(t, err)

	require.Len(t, res.Runs, 1, "only the in-window run is returned")
	assert.Equal(t, int64(2), res.Runs[0].ID)
	assert.True(t, res.HasMore, "one in-window run means there may be more to read")
}

// TestListPipelineRuns_UnboundedKeepsPaging: with no watermark every page is in
// scope, so paging must follow the platform.
func TestListPipelineRuns_UnboundedKeepsPaging(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Link", `<https://api.github.com/repos/acme/widgets/actions/runs?page=2>; rel="next"`)
		_, _ = w.Write([]byte(`{"workflow_runs":[
			{"id":1,"name":"CI","status":"completed","conclusion":"success",
			 "created_at":"2026-09-01T10:00:00Z","updated_at":"2026-09-01T10:05:00Z"}
		]}`))
	}))

	res, err := p.ListPipelineRuns(context.Background(), time.Time{}, 1, 30)
	require.NoError(t, err)
	require.Len(t, res.Runs, 1)
	assert.True(t, res.HasMore, "a zero since means no lower bound")
}

// TestListPipelineRuns_NormalizesStatus covers the GitHub status/conclusion
// split, which is the part most likely to be got wrong: a run that is still
// going has an EMPTY conclusion, and reading that as a terminal state would
// either fire a bogus event or record the run as already-seen.
func TestListPipelineRuns_NormalizesStatus(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/repos/acme/widgets/actions/runs", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":4,"workflow_runs":[
			{"id":1,"name":"CI","run_number":10,"status":"completed","conclusion":"success",
			 "head_branch":"main","head_sha":"abc123","event":"push",
			 "html_url":"https://github.com/acme/widgets/actions/runs/1",
			 "created_at":"2026-09-14T10:00:00Z","updated_at":"2026-09-14T10:05:00Z",
			 "run_started_at":"2026-09-14T10:00:30Z","actor":{"login":"octocat"}},
			{"id":2,"name":"CI","run_number":11,"status":"completed","conclusion":"timed_out",
			 "head_branch":"main","head_sha":"def456","event":"push",
			 "created_at":"2026-09-14T11:00:00Z","updated_at":"2026-09-14T11:30:00Z"},
			{"id":3,"name":"CI","run_number":12,"status":"in_progress","conclusion":"",
			 "head_branch":"feat","head_sha":"aaa111","event":"pull_request",
			 "created_at":"2026-09-14T12:00:00Z","updated_at":"2026-09-14T12:00:10Z"},
			{"id":4,"name":"CI","run_number":13,"status":"completed","conclusion":"cancelled",
			 "head_branch":"main","head_sha":"bbb222","event":"push",
			 "created_at":"2026-09-14T13:00:00Z","updated_at":"2026-09-14T13:01:00Z"}
		]}`))
	}))

	res, err := p.ListPipelineRuns(context.Background(), time.Time{}, 1, 30)
	require.NoError(t, err)
	require.Len(t, res.Runs, 4)

	assert.Equal(t, forge.PipelineSuccess, res.Runs[0].Status)
	assert.Equal(t, "octocat", res.Runs[0].Actor, "actor must be carried for the prompt")
	assert.Equal(t, "main", res.Runs[0].Ref)
	assert.Equal(t, "push", res.Runs[0].Event)
	assert.Equal(t, 10, res.Runs[0].Number)

	// timed_out is a failure from the user's point of view.
	assert.Equal(t, forge.PipelineFailure, res.Runs[1].Status)

	// An in-progress run has an empty conclusion and must be Running, not a
	// terminal state.
	assert.Equal(t, forge.PipelineRunning, res.Runs[2].Status)
	assert.False(t, forge.PipelineTerminal(res.Runs[2].Status))

	assert.Equal(t, forge.PipelineCancelled, res.Runs[3].Status)
}

// TestListPipelineRuns_AppliesSinceLocally pins the local lower bound: GitHub's
// runs endpoint has no updated-after parameter, so the adapter must filter.
func TestListPipelineRuns_AppliesSinceLocally(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"workflow_runs":[
			{"id":2,"name":"CI","status":"completed","conclusion":"success",
			 "created_at":"2026-09-14T12:00:00Z","updated_at":"2026-09-14T12:05:00Z"},
			{"id":1,"name":"CI","status":"completed","conclusion":"success",
			 "created_at":"2026-09-14T09:00:00Z","updated_at":"2026-09-14T09:05:00Z"}
		]}`))
	}))

	since := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	res, err := p.ListPipelineRuns(context.Background(), since, 1, 30)
	require.NoError(t, err)

	require.Len(t, res.Runs, 1, "runs older than since must be filtered out")
	assert.Equal(t, int64(2), res.Runs[0].ID)
}

// TestListPipelineRuns_DurationDerivedFromTimestamps: GitHub's list endpoint
// returns no duration, so the adapter derives it and leaves it zero when the
// span is not usable.
func TestListPipelineRuns_DurationDerivedFromTimestamps(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"workflow_runs":[
			{"id":1,"name":"CI","status":"completed","conclusion":"success",
			 "created_at":"2026-09-14T10:00:00Z","updated_at":"2026-09-14T10:05:00Z",
			 "run_started_at":"2026-09-14T10:01:00Z"},
			{"id":2,"name":"CI","status":"completed","conclusion":"success",
			 "created_at":"2026-09-14T10:00:00Z","updated_at":"2026-09-14T09:00:00Z"}
		]}`))
	}))

	res, err := p.ListPipelineRuns(context.Background(), time.Time{}, 1, 30)
	require.NoError(t, err)

	assert.Equal(t, 4*time.Minute, res.Runs[0].Duration, "run_started_at -> updated_at")
	assert.Zero(t, res.Runs[1].Duration, "an inverted span must yield unknown, not a negative")
}

// TestListPipelineRuns_FallsBackToDisplayTitle: a workflow with no name must
// still render something.
func TestListPipelineRuns_FallsBackToDisplayTitle(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"workflow_runs":[
			{"id":1,"display_title":"fix the thing","status":"completed","conclusion":"failure",
			 "created_at":"2026-09-14T10:00:00Z","updated_at":"2026-09-14T10:05:00Z"}
		]}`))
	}))

	res, err := p.ListPipelineRuns(context.Background(), time.Time{}, 1, 30)
	require.NoError(t, err)
	assert.Equal(t, "fix the thing", res.Runs[0].Name)
}

// TestListPipelineJobs checks job normalization, including the absence of a
// stage concept on GitHub.
func TestListPipelineJobs(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/repos/acme/widgets/actions/runs/77/jobs", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":2,"jobs":[
			{"id":501,"run_id":77,"name":"build","status":"completed","conclusion":"success",
			 "html_url":"https://github.com/acme/widgets/actions/runs/77/job/501",
			 "started_at":"2026-09-14T10:00:00Z","completed_at":"2026-09-14T10:02:00Z",
			 "runner_name":"GitHub Actions 1"},
			{"id":502,"run_id":77,"name":"test","status":"completed","conclusion":"failure",
			 "html_url":"https://github.com/acme/widgets/actions/runs/77/job/502",
			 "started_at":"2026-09-14T10:02:00Z","completed_at":"2026-09-14T10:05:00Z"}
		]}`))
	}))

	jobs, err := p.ListPipelineJobs(context.Background(), 77)
	require.NoError(t, err)
	require.Len(t, jobs, 2)

	assert.Equal(t, "build", jobs[0].Name)
	assert.Equal(t, forge.PipelineSuccess, jobs[0].Status)
	assert.Equal(t, "GitHub Actions 1", jobs[0].Runner)
	assert.Equal(t, 2*time.Minute, jobs[0].Duration)
	// GitHub has no stage concept; the UI must cope with it being empty.
	assert.Empty(t, jobs[0].Stage)

	assert.Equal(t, forge.PipelineFailure, jobs[1].Status)
	assert.Equal(t, 3*time.Minute, jobs[1].Duration)
}

// TestListPipelineJobs_InProgressHasNoConclusion mirrors the run-level rule.
func TestListPipelineJobs_InProgressHasNoConclusion(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jobs":[
			{"id":1,"name":"build","status":"in_progress","conclusion":"",
			 "started_at":"2026-09-14T10:00:00Z"}
		]}`))
	}))

	jobs, err := p.ListPipelineJobs(context.Background(), 77)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, forge.PipelineRunning, jobs[0].Status)
	assert.Zero(t, jobs[0].Duration, "a job still running has no duration")
}

// TestProviderSatisfiesPipelineInterfaces is the compile-time contract: the
// adapter must be usable wherever the optional capabilities are expected.
func TestProviderSatisfiesPipelineInterfaces(t *testing.T) {
	var _ forge.PipelineLister = (*Provider)(nil)
	var _ forge.PipelineJobLister = (*Provider)(nil)
}
