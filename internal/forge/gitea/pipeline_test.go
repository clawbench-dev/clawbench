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

// The fixtures below use the field names a REAL Forgejo instance returns — which
// are not GitHub's. Getting these wrong is invisible in a test written from the
// GitHub shape, so the names are copied from a live payload on purpose.

func TestListPipelineRuns(t *testing.T) {
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/repos/acme/widgets/actions/runs", r.URL.Path)
		assert.Equal(t, "2", r.URL.Query().Get("page"))
		_, _ = w.Write([]byte(`{"total_count":120,"workflow_runs":[
			{"id":10,"title":"ci: add test workflow","workflow_id":"ci.yml","index_in_repo":5,
			 "trigger_event":"push","prettyref":"main","commit_sha":"abc",
			 "status":"success","started":"2026-11-19T14:22:56+01:00",
			 "stopped":"2026-11-19T14:23:26+01:00",
			 "created":"2026-11-19T14:22:56+01:00","updated":"2026-11-19T14:23:26+01:00",
			 "html_url":"https://git.example.com/runs/1",
			 "trigger_user":{"login":"alice"}},
			{"id":11,"title":"wip","workflow_id":"ci.yml","status":"waiting",
			 "created":"2026-11-19T15:00:00+01:00","updated":"2026-11-19T15:00:00+01:00"}
		]}`))
	})
	defer done()

	res, err := p.ListPipelineRuns(context.Background(), time.Time{}, 2, 50)
	require.NoError(t, err)
	require.Len(t, res.Runs, 2)

	first := res.Runs[0]
	assert.Equal(t, int64(10), first.ID)
	assert.Equal(t, "ci: add test workflow", first.Name, "the title distinguishes runs of one workflow")
	assert.Equal(t, 5, first.Number)
	assert.Equal(t, forge.PipelineSuccess, first.Status, "Gitea's status is already terminal")
	assert.Equal(t, "main", first.Ref, "the branch arrives as prettyref")
	assert.Equal(t, "abc", first.SHA)
	assert.Equal(t, "push", first.Event)
	assert.Equal(t, "alice", first.Actor)
	assert.Equal(t, 30*time.Second, first.Duration)

	second := res.Runs[1]
	assert.Equal(t, forge.PipelineRunning, second.Status, "a waiting run must not be read as success")
	assert.Equal(t, "wip", second.Name)

	assert.True(t, res.HasMore, "2*50 < 120")
	assert.Equal(t, 3, res.NextPage)
}

// TestListPipelineRuns_LastPage checks the pagination signal: this endpoint sends
// no headers, so HasMore comes from the body's total_count.
func TestListPipelineRuns_LastPage(t *testing.T) {
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"total_count":2,"workflow_runs":[
			{"id":1,"status":"failure","updated":"2026-11-19T00:00:00Z"},
			{"id":2,"status":"success","updated":"2026-11-19T00:00:00Z"}
		]}`))
	})
	defer done()

	res, err := p.ListPipelineRuns(context.Background(), time.Time{}, 1, 50)
	require.NoError(t, err)
	assert.Len(t, res.Runs, 2)
	assert.False(t, res.HasMore)
	assert.Zero(t, res.NextPage)
}

// TestListPipelineRuns_StopsAtWatermark pins the boundary: a page entirely older
// than the watermark ends the walk even though the server reports more.
func TestListPipelineRuns_StopsAtWatermark(t *testing.T) {
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"total_count":500,"workflow_runs":[
			{"id":1,"status":"success","updated":"2026-09-01T00:00:00Z"},
			{"id":2,"status":"failure","updated":"2026-09-02T00:00:00Z"}
		]}`))
	})
	defer done()

	since := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	res, err := p.ListPipelineRuns(context.Background(), since, 1, 50)
	require.NoError(t, err)
	assert.Empty(t, res.Runs)
	assert.False(t, res.HasMore, "the boundary must end the walk")
}

func TestListPipelineRuns_KeepsNewerRunsOnAMixedPage(t *testing.T) {
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"total_count":2,"workflow_runs":[
			{"id":2,"status":"success","updated":"2026-09-06T00:00:00Z"},
			{"id":1,"status":"success","updated":"2026-09-01T00:00:00Z"}
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

// TestListPipelineRuns_ZeroTimestampsAreUnknown guards the Gitea zero value: an
// unset TimeStamp arrives as "0001-01-01T00:00:00Z", and treating it as real
// would make the incremental filter drop the run as "older than the watermark".
func TestListPipelineRuns_ZeroTimestampsAreUnknown(t *testing.T) {
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"total_count":1,"workflow_runs":[
			{"id":1,"status":"waiting","created":"0001-01-01T00:00:00Z","updated":"0001-01-01T00:00:00Z"}
		]}`))
	})
	defer done()

	since := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	res, err := p.ListPipelineRuns(context.Background(), since, 1, 50)
	require.NoError(t, err)
	require.Len(t, res.Runs, 1, "an unknown timestamp must not be read as ancient")
	assert.True(t, res.Runs[0].UpdatedAt.IsZero())
}

func TestListPipelineJobs(t *testing.T) {
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/repos/acme/widgets/actions/runs/77/jobs", r.URL.Path)
		// A bare array, not {"jobs": [...]}.
		_, _ = w.Write([]byte(`[
			{"id":1,"name":"build","status":"success","runner_name":"runner-1","html_url":"u1",
			 "started_at":"2026-11-19T14:22:56+01:00","stopped_at":"2026-11-19T14:23:26+01:00"},
			{"id":2,"name":"failing","status":"waiting"}
		]`))
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
	assert.Zero(t, jobs[1].Duration)
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

// TestListPipelinesForItem pins the lookup key: Gitea matches `ref` against a
// FULL ref, and silently ignores a bare `branch` — using it would return the
// repository's whole run history on a pull request page.
func TestListPipelinesForItem(t *testing.T) {
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		assert.Equal(t, "refs/heads/feat/x", q.Get("ref"))
		assert.Empty(t, q.Get("branch"), "branch is ignored by Gitea and must not be sent")
		assert.Equal(t, "2", q.Get("limit"))
		_, _ = w.Write([]byte(`{"total_count":3,"workflow_runs":[
			{"id":1,"status":"success"},{"id":2,"status":"failure"},{"id":3,"status":"success"}
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
}

func TestListPipelinesForItem_NoBranchNoRequest(t *testing.T) {
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no request may be issued")
	})
	defer done()

	runs, err := p.ListPipelinesForItem(context.Background(), forge.Item{Type: forge.ItemTypeIssue, Number: 4}, 10)
	require.NoError(t, err)
	assert.Empty(t, runs)

	runs, err = p.ListPipelinesForItem(context.Background(), forge.Item{Type: forge.ItemTypeChangeRequest, Number: 4}, 10)
	require.NoError(t, err)
	assert.Empty(t, runs)
}

func TestRunStatus(t *testing.T) {
	assert.Equal(t, forge.PipelineSuccess, runStatus("success"))
	assert.Equal(t, forge.PipelineFailure, runStatus("failure"))
	assert.Equal(t, forge.PipelineCancelled, runStatus("cancelled"))
	assert.Equal(t, forge.PipelineSkipped, runStatus("skipped"))
	// Non-terminal, including values this build does not know: treating an
	// unknown status as terminal would swallow its eventual outcome.
	assert.Equal(t, forge.PipelineRunning, runStatus("waiting"))
	assert.Equal(t, forge.PipelineRunning, runStatus("running"))
	assert.Equal(t, forge.PipelineRunning, runStatus("blocked"))
	assert.Equal(t, forge.PipelineRunning, runStatus("something-new"))
	assert.Equal(t, forge.PipelineRunning, runStatus(""))
}

func TestParseRunTime_TreatsZeroValueAsUnknown(t *testing.T) {
	assert.True(t, parseRunTime("").IsZero())
	assert.True(t, parseRunTime("0001-01-01T00:00:00Z").IsZero())
	assert.Equal(t, 2026, parseRunTime("2026-11-19T14:22:56+01:00").Year())
}
