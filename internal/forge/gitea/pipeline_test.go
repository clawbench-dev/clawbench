package gitea

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/forge"
)

func TestListPipelineRuns(t *testing.T) {
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/repos/acme/widgets/actions/runs", r.URL.Path)
		assert.Equal(t, "2", r.URL.Query().Get("page"))
		w.Header().Set("X-HasMore", "true")
		_, _ = w.Write([]byte(`{"total_count":3,"workflow_runs":[
			{"id":10,"name":"CI","display_title":"fix: thing","head_branch":"main",
			 "head_sha":"abc","run_number":5,"event":"push","status":"completed",
			 "conclusion":"success","html_url":"https://git.example.com/runs/10",
			 "created_at":"2026-09-10T10:00:00Z","updated_at":"2026-09-10T10:02:00Z",
			 "run_started_at":"2026-09-10T10:00:30Z","actor":{"login":"alice"}},
			{"id":11,"name":"CI","head_branch":"feat","run_number":6,
			 "status":"in_progress","conclusion":"","created_at":"2026-09-10T11:00:00Z",
			 "updated_at":"2026-09-10T11:00:10Z"}
		]}`))
	})
	defer done()

	res, err := p.ListPipelineRuns(context.Background(), time.Time{}, 2, 50)
	require.NoError(t, err)
	require.Len(t, res.Runs, 2)

	first := res.Runs[0]
	assert.Equal(t, int64(10), first.ID)
	assert.Equal(t, "fix: thing", first.Name, "the display title distinguishes rows of one workflow")
	assert.Equal(t, 5, first.Number)
	assert.Equal(t, forge.PipelineSuccess, first.Status)
	assert.Equal(t, "main", first.Ref)
	assert.Equal(t, "abc", first.SHA)
	assert.Equal(t, "push", first.Event)
	assert.Equal(t, "alice", first.Actor)
	assert.Equal(t, 90*time.Second, first.Duration)

	second := res.Runs[1]
	assert.Equal(t, forge.PipelineRunning, second.Status, "an unfinished run must not be read as success")
	assert.Equal(t, "CI", second.Name, "no display title falls back to the workflow name")

	assert.True(t, res.HasMore)
	assert.Equal(t, 3, res.NextPage)
}

// TestListPipelineRuns_StopsAtWatermark pins the boundary: a page entirely older
// than the watermark ends the walk even though the platform advertises more,
// otherwise every poll would re-read the repository's whole history.
func TestListPipelineRuns_StopsAtWatermark(t *testing.T) {
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-HasMore", "true")
		_, _ = w.Write([]byte(`{"workflow_runs":[
			{"id":1,"status":"completed","conclusion":"success","updated_at":"2026-09-01T00:00:00Z"},
			{"id":2,"status":"completed","conclusion":"failure","updated_at":"2026-09-02T00:00:00Z"}
		]}`))
	})
	defer done()

	since := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	res, err := p.ListPipelineRuns(context.Background(), since, 1, 50)
	require.NoError(t, err)
	assert.Empty(t, res.Runs, "runs older than the watermark are not returned")
	assert.False(t, res.HasMore, "the boundary must end the walk")
	assert.Zero(t, res.NextPage)
}

func TestListPipelineRuns_KeepsNewerRunsOnAMixedPage(t *testing.T) {
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"workflow_runs":[
			{"id":2,"status":"completed","conclusion":"success","updated_at":"2026-09-06T00:00:00Z"},
			{"id":1,"status":"completed","conclusion":"success","updated_at":"2026-09-01T00:00:00Z"}
		]}`))
	})
	defer done()

	since := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	res, err := p.ListPipelineRuns(context.Background(), since, 1, 50)
	require.NoError(t, err)
	require.Len(t, res.Runs, 1)
	assert.Equal(t, int64(2), res.Runs[0].ID)
	assert.False(t, res.HasMore, "a short page is the end")
}

func TestListPipelineJobs(t *testing.T) {
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/repos/acme/widgets/actions/runs/77/jobs", r.URL.Path)
		_, _ = w.Write([]byte(`{"total_count":2,"jobs":[
			{"id":1,"name":"build","status":"completed","conclusion":"success",
			 "runner_name":"runner-1","html_url":"u1",
			 "started_at":"2026-09-10T10:00:00Z","completed_at":"2026-09-10T10:00:30Z"},
			{"id":2,"name":"test","status":"in_progress","conclusion":""}
		]}`))
	})
	defer done()

	jobs, err := p.ListPipelineJobs(context.Background(), 77)
	require.NoError(t, err)
	require.Len(t, jobs, 2)

	assert.Equal(t, "build", jobs[0].Name)
	assert.Equal(t, forge.PipelineSuccess, jobs[0].Status)
	assert.Equal(t, "runner-1", jobs[0].Runner)
	assert.Equal(t, 30*time.Second, jobs[0].Duration)
	assert.Empty(t, jobs[0].Stage, "Gitea has no stage concept")

	assert.Equal(t, forge.PipelineRunning, jobs[1].Status)
	assert.Zero(t, jobs[1].Duration, "an unfinished job reports no duration")
}

func TestListPipelineJobs_RejectsBadID(t *testing.T) {
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no request may be issued")
	})
	defer done()

	_, err := p.ListPipelineJobs(context.Background(), 0)
	require.Error(t, err)
	var fe *forge.Error
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, forge.ErrKindUnsupported, fe.Kind)
}

// TestListPipelinesForItem pins the branch lookup: Gitea has no "runs for this
// pull" endpoint, but every run carries its head branch and the list accepts a
// branch filter.
func TestListPipelinesForItem(t *testing.T) {
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "feat/x", r.URL.Query().Get("branch"))
		assert.Equal(t, "2", r.URL.Query().Get("limit"))
		_, _ = w.Write([]byte(`{"workflow_runs":[
			{"id":1,"status":"completed","conclusion":"success","updated_at":"2026-09-10T00:00:00Z"},
			{"id":2,"status":"completed","conclusion":"failure","updated_at":"2026-09-09T00:00:00Z"},
			{"id":3,"status":"completed","conclusion":"success","updated_at":"2026-09-08T00:00:00Z"}
		]}`))
	})
	defer done()

	runs, err := p.ListPipelinesForItem(context.Background(), forge.Item{
		Type:         forge.ItemTypeChangeRequest,
		Number:       4,
		SourceBranch: "feat/x",
	}, 2)
	require.NoError(t, err)
	require.Len(t, runs, 2, "the limit caps the summary list")
	assert.Equal(t, int64(1), runs[0].ID)
	assert.Equal(t, int64(2), runs[1].ID)
}

func TestListPipelinesForItem_NoBranchNoRequest(t *testing.T) {
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no request may be issued")
	})
	defer done()

	// An issue has no CI, and a list-shaped pull row has no branch.
	runs, err := p.ListPipelinesForItem(context.Background(), forge.Item{
		Type:   forge.ItemTypeIssue,
		Number: 4,
	}, 10)
	require.NoError(t, err)
	assert.Empty(t, runs)

	runs, err = p.ListPipelinesForItem(context.Background(), forge.Item{
		Type:   forge.ItemTypeChangeRequest,
		Number: 4,
	}, 10)
	require.NoError(t, err)
	assert.Empty(t, runs)
}

func TestJobStatus(t *testing.T) {
	assert.Equal(t, forge.PipelineSuccess, jobStatus("completed", "success"))
	assert.Equal(t, forge.PipelineFailure, jobStatus("completed", "failure"))
	assert.Equal(t, forge.PipelineRunning, jobStatus("in_progress", ""))
	assert.Equal(t, forge.PipelineRunning, jobStatus("queued", ""))
}
