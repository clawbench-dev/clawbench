package gitlab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/forge"
)

func newPipelineTestProvider(t *testing.T, handler http.Handler) *Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p, err := New(Config{Host: srv.URL[len("http://"):], Scheme: "http", Token: "tok"}, "acme", "widgets")
	require.NoError(t, err)
	return p
}

// TestListPipelineRuns_NormalizesStatus covers GitLab's single status field,
// which mixes terminal and non-terminal values in one vocabulary.
func TestListPipelineRuns_NormalizesStatus(t *testing.T) {
	p := newPipelineTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v4/projects/acme%2Fwidgets/pipelines", r.URL.EscapedPath())
		assert.Equal(t, "id", r.URL.Query().Get("order_by"))
		assert.Equal(t, "desc", r.URL.Query().Get("sort"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":47,"iid":12,"name":"Build pipeline","status":"success","ref":"main",
			 "sha":"a91957a8","source":"push","web_url":"https://gitlab.com/acme/widgets/-/pipelines/47",
			 "created_at":"2026-09-14T10:00:00.000Z","updated_at":"2026-09-14T10:05:00.000Z",
			 "user":{"username":"root"}},
			{"id":48,"iid":13,"name":"Build pipeline","status":"failed","ref":"main",
			 "sha":"eb94b618","source":"push",
			 "created_at":"2026-09-14T11:00:00.000Z","updated_at":"2026-09-14T11:05:00.000Z"},
			{"id":49,"iid":14,"name":"Build pipeline","status":"running","ref":"feat",
			 "sha":"ccc333","source":"merge_request_event",
			 "created_at":"2026-09-14T12:00:00.000Z","updated_at":"2026-09-14T12:00:30.000Z"},
			{"id":50,"iid":15,"name":"Build pipeline","status":"canceled","ref":"main",
			 "sha":"ddd444","source":"push",
			 "created_at":"2026-09-14T13:00:00.000Z","updated_at":"2026-09-14T13:01:00.000Z"}
		]`))
	}))

	res, err := p.ListPipelineRuns(context.Background(), time.Time{}, 1, 30)
	require.NoError(t, err)
	require.Len(t, res.Runs, 4)

	assert.Equal(t, forge.PipelineSuccess, res.Runs[0].Status)
	assert.Equal(t, "root", res.Runs[0].Actor)
	assert.Equal(t, "main", res.Runs[0].Ref)
	assert.Equal(t, "push", res.Runs[0].Event)
	assert.Equal(t, 12, res.Runs[0].Number, "iid is the human-facing run number")

	assert.Equal(t, forge.PipelineFailure, res.Runs[1].Status)
	assert.Equal(t, forge.PipelineRunning, res.Runs[2].Status)
	// GitLab spells it "canceled" with one l.
	assert.Equal(t, forge.PipelineCancelled, res.Runs[3].Status)
}

// TestListPipelineRuns_NameFallsBackToRef guards an older-GitLab case: the
// pipeline `name` field only appears from GitLab 16.3, so a self-managed
// instance on an earlier version would render every run nameless.
func TestListPipelineRuns_NameFallsBackToRef(t *testing.T) {
	p := newPipelineTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":47,"iid":12,"status":"success","ref":"release/1.2","sha":"aaa",
			 "created_at":"2026-09-14T10:00:00.000Z","updated_at":"2026-09-14T10:05:00.000Z"}
		]`))
	}))

	res, err := p.ListPipelineRuns(context.Background(), time.Time{}, 1, 30)
	require.NoError(t, err)
	require.Len(t, res.Runs, 1)
	assert.Equal(t, "release/1.2", res.Runs[0].Name)
	assert.Empty(t, res.Runs[0].Actor, "no user in the payload must not panic")
}

// TestListPipelineRuns_AppliesSinceServerSide pins that the lower bound is sent
// to GitLab rather than filtered locally.
func TestListPipelineRuns_AppliesSinceServerSide(t *testing.T) {
	var gotUpdatedAfter string
	p := newPipelineTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUpdatedAfter = r.URL.Query().Get("updated_after")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))

	since := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	_, err := p.ListPipelineRuns(context.Background(), since, 1, 30)
	require.NoError(t, err)
	assert.Equal(t, "2026-09-14T12:00:00Z", gotUpdatedAfter)
}

// TestListPipelineRuns_OrderByPairsWithUpdatedAfter pins the pairing that
// GitLab requires, and that the docs do not mention. Every other combination
// returns 500:
//
//	order_by=id         + updated_after
//	order_by=updated_at without it (on large projects)
//
// so the two parameters must move together.
func TestListPipelineRuns_OrderByPairsWithUpdatedAfter(t *testing.T) {
	t.Run("no lower bound uses id and omits updated_after", func(t *testing.T) {
		var gotOrderBy, gotUpdatedAfter string
		var hadUpdatedAfter bool
		p := newPipelineTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotOrderBy = r.URL.Query().Get("order_by")
			gotUpdatedAfter = r.URL.Query().Get("updated_after")
			_, hadUpdatedAfter = r.URL.Query()["updated_after"]
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		}))

		_, err := p.ListPipelineRuns(context.Background(), time.Time{}, 1, 30)
		require.NoError(t, err)
		assert.Equal(t, "id", gotOrderBy)
		assert.False(t, hadUpdatedAfter, "updated_after must be absent, not empty")
		assert.Empty(t, gotUpdatedAfter)
	})

	t.Run("a lower bound switches to updated_at", func(t *testing.T) {
		var gotOrderBy, gotUpdatedAfter string
		p := newPipelineTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotOrderBy = r.URL.Query().Get("order_by")
			gotUpdatedAfter = r.URL.Query().Get("updated_after")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		}))

		since := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
		_, err := p.ListPipelineRuns(context.Background(), since, 1, 30)
		require.NoError(t, err)
		// order_by=id here would make GitLab answer 500.
		assert.Equal(t, "updated_at", gotOrderBy)
		assert.Equal(t, "2026-09-14T12:00:00Z", gotUpdatedAfter)
	})
}

// TestListPipelineRuns_PaginationFromNextPageHeader covers the paging contract.
func TestListPipelineRuns_PaginationFromNextPageHeader(t *testing.T) {
	p := newPipelineTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Next-Page", "3")
		_, _ = w.Write([]byte(`[]`))
	}))

	res, err := p.ListPipelineRuns(context.Background(), time.Time{}, 1, 30)
	require.NoError(t, err)
	assert.True(t, res.HasMore)
	assert.Equal(t, 3, res.NextPage)
}

// TestListPipelineJobs checks GitLab's richer job payload, which is what makes
// the job table useful for locating a failure.
func TestListPipelineJobs(t *testing.T) {
	p := newPipelineTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v4/projects/acme%2Fwidgets/pipelines/47/jobs", r.URL.EscapedPath())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":501,"name":"rspec","stage":"test","status":"failed",
			 "failure_reason":"script_failure","duration":540.5,
			 "web_url":"https://gitlab.com/acme/widgets/-/jobs/501",
			 "started_at":"2026-09-14T10:01:00.000Z","finished_at":"2026-09-14T10:10:00.000Z",
			 "runner":{"description":"docker-runner-1"}},
			{"id":502,"name":"build","stage":"build","status":"success",
			 "duration":30,"started_at":"2026-09-14T10:00:00.000Z","finished_at":"2026-09-14T10:00:30.000Z"}
		]`))
	}))

	jobs, err := p.ListPipelineJobs(context.Background(), 47)
	require.NoError(t, err)
	require.Len(t, jobs, 2)

	assert.Equal(t, "rspec", jobs[0].Name)
	assert.Equal(t, "test", jobs[0].Stage, "GitLab reports a real stage")
	assert.Equal(t, forge.PipelineFailure, jobs[0].Status)
	assert.Equal(t, "script_failure", jobs[0].FailureReason)
	assert.Equal(t, "docker-runner-1", jobs[0].Runner)
	assert.Equal(t, 540*time.Second+500*time.Millisecond, jobs[0].Duration)

	assert.Equal(t, forge.PipelineSuccess, jobs[1].Status)
	assert.Equal(t, "build", jobs[1].Stage)
}

// TestListPipelineJobs_RejectsInvalidID: a zero/negative id would build a
// nonsense URL, so it is rejected before the request.
func TestListPipelineJobs_RejectsInvalidID(t *testing.T) {
	p := newPipelineTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no request should be issued for an invalid id")
	}))

	_, err := p.ListPipelineJobs(context.Background(), 0)
	require.Error(t, err)
}

// TestProviderSatisfiesPipelineInterfaces is the compile-time contract.
func TestProviderSatisfiesPipelineInterfaces(t *testing.T) {
	var _ forge.PipelineLister = (*Provider)(nil)
	var _ forge.PipelineJobLister = (*Provider)(nil)
}

// TestListPipelineRuns_AttachesMergeRequestFromRef: a merge-request pipeline
// carries `refs/merge-requests/<iid>/head`, which is the only association GitLab
// exposes in the pipeline payload — so it must be recovered from the ref.
func TestListPipelineRuns_AttachesMergeRequestFromRef(t *testing.T) {
	p := newPipelineTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":47,"iid":12,"name":"MR pipeline","status":"success",
			 "ref":"refs/merge-requests/42/head","sha":"a91957a8","source":"merge_request_event",
			 "created_at":"2026-09-14T10:00:00.000Z","updated_at":"2026-09-14T10:05:00.000Z"}
		]`))
	}))

	res, err := p.ListPipelineRuns(context.Background(), time.Time{}, 1, 30)
	require.NoError(t, err)
	require.Len(t, res.Runs, 1)
	require.Len(t, res.Runs[0].PullRequests, 1, "an MR pipeline must link to its MR")

	pr := res.Runs[0].PullRequests[0]
	assert.Equal(t, 42, pr.Number, "the iid comes from the ref")
	assert.Contains(t, pr.URL, "/acme/widgets/-/merge_requests/42",
		"the web URL is built server-side from the project path")
}

// TestListPipelineRuns_BranchPushHasNoMergeRequest: a plain branch push reports
// nothing about any open MR, and resolving it would cost a request per row — so
// the association is deliberately absent rather than guessed.
func TestListPipelineRuns_BranchPushHasNoMergeRequest(t *testing.T) {
	p := newPipelineTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":47,"iid":12,"name":"CI","status":"success","ref":"main",
			 "sha":"a91957a8","source":"push",
			 "created_at":"2026-09-14T10:00:00.000Z","updated_at":"2026-09-14T10:05:00.000Z"}
		]`))
	}))

	res, err := p.ListPipelineRuns(context.Background(), time.Time{}, 1, 30)
	require.NoError(t, err)
	require.Len(t, res.Runs, 1)
	assert.Nil(t, res.Runs[0].PullRequests, "a branch push must not claim an MR")
}

// TestMergeRequestRefIID pins the parser's boundary: it must recover a real iid
// and reject every near-miss, since a bogus iid would render a link to a
// non-existent merge request.
func TestMergeRequestRefIID(t *testing.T) {
	ok := []struct {
		ref  string
		want int
	}{
		{"refs/merge-requests/1/head", 1},
		{"refs/merge-requests/42/head", 42},
		{"refs/merge-requests/7/merge", 7},
		{"refs/merge-requests/123456/head", 123456},
	}
	for _, tc := range ok {
		got, found := mergeRequestRefIID(tc.ref)
		assert.True(t, found, "ref %q should parse", tc.ref)
		assert.Equal(t, tc.want, got, "ref %q", tc.ref)
	}

	bad := []string{
		"main",
		"refs/heads/merge-requests/1/head",
		"refs/merge-requests/head",     // no digits
		"refs/merge-requests/abc/head", // not a number
		"refs/merge-requests//head",    // empty
		"refs/merge-requests/12",       // truncated: no trailing segment
		"refs/merge-requests/0/head",   // iid 0 is not a real MR
		"refs/merge-requests/-3/head",  // negative
		"",
	}
	for _, ref := range bad {
		_, found := mergeRequestRefIID(ref)
		assert.False(t, found, "ref %q must not parse into an iid", ref)
	}
}
