package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPipelineGitLab answers the GitLab pipelines endpoints.
func mockPipelineGitLab(t *testing.T, env *testEnv, handler http.Handler) {
	t.Helper()
	bindMockGitLab(t, env, handler)
}

// pipelineListBody is a two-run GitLab pipeline list: one success, one failed.
const pipelineListBody = `[
	{"id":47,"iid":12,"name":"Build pipeline","status":"success","ref":"main",
	 "sha":"a91957a858320c0e17f3a0eca7cfacbff50ea29a","source":"push",
	 "web_url":"https://gitlab.example.com/acme/widgets/-/pipelines/47",
	 "created_at":"2026-09-14T10:00:00.000Z","updated_at":"2026-09-14T10:05:00.000Z",
	 "user":{"username":"root"}},
	{"id":48,"iid":13,"name":"Build pipeline","status":"failed","ref":"release/1.2",
	 "sha":"eb94b618fb5865b26e80fdd8ae531b7a63ad851a","source":"push",
	 "web_url":"https://gitlab.example.com/acme/widgets/-/pipelines/48",
	 "created_at":"2026-09-14T11:00:00.000Z","updated_at":"2026-09-14T11:05:00.000Z"}
]`

// TestServeForgePipelines_AgainstMockGitLab drives the whole path: binding →
// provider → mocked GitLab → normalized view.
func TestServeForgePipelines_AgainstMockGitLab(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	mockPipelineGitLab(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.True(t, strings.HasSuffix(r.URL.Path, "/pipelines"), "unexpected path %s", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pipelineListBody))
	}))

	req := newRequest(t, http.MethodGet, "/api/forge/pipelines", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgePipelines, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	runs, ok := resp["pipelines"].([]any)
	require.True(t, ok, "response must carry a pipelines array")
	require.Len(t, runs, 2)

	first := runs[0].(map[string]any)
	assert.Equal(t, "success", first["status"])
	assert.Equal(t, "Build pipeline", first["name"])
	assert.Equal(t, "main", first["ref"])
	assert.Equal(t, "root", first["actor"])
	assert.Equal(t, float64(47), first["id"])
	// A short sha is what the UI shows; the full one is carried.
	assert.Equal(t, "a91957a858320c0e17f3a0eca7cfacbff50ea29a", first["sha"])
	assert.Equal(t, "acme/widgets", first["slug"])

	second := runs[1].(map[string]any)
	assert.Equal(t, "failure", second["status"], "GitLab 'failed' normalizes to failure")
	assert.Equal(t, "release/1.2", second["ref"])
}

// TestServeForgePipelines_StatusFilter: the status filter is applied
// server-side so the UI can default to failures.
func TestServeForgePipelines_StatusFilter(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	mockPipelineGitLab(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pipelineListBody))
	}))

	req := newRequest(t, http.MethodGet, "/api/forge/pipelines?status=failure", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgePipelines, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	runs := resp["pipelines"].([]any)
	require.Len(t, runs, 1, "only the failed run must survive the filter")
	assert.Equal(t, "failure", runs[0].(map[string]any)["status"])
}

// TestServeForgePipelines_UnboundProject: no binding is a 404 with a code the
// frontend can act on, not a 500.
func TestServeForgePipelines_UnboundProject(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/forge/pipelines", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgePipelines, req)

	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "NoForgeBinding", resp["code"])
}

// TestServeForgePipelines_RejectsNonGet keeps the method contract.
func TestServeForgePipelines_RejectsNonGet(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/forge/pipelines", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgePipelines, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// TestServeForgePipelines_PagesDoNotOverlap is the regression for a real bug:
// the status filter is applied locally, so the caller's page indexes the
// FILTERED result while the provider pages the unfiltered one. Walking the
// provider from the caller's page therefore re-served already-shown runs.
func TestServeForgePipelines_PagesDoNotOverlap(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	// 220 runs, every 10th failing: 22 failures spread across BOTH provider
	// pages (runs 0..99 are page 1, 100..199 are page 2). The density matters —
	// with failures clustered at the start of each provider page the two
	// implementations happen to agree, so the fixture would not discriminate.
	// With perPage=3, page 2 must be runs 1030/1040/1050; an implementation that
	// starts its walk at provider page 2 returns 1100/1110/1120 instead.
	all := make([]string, 0, 220)
	for i := 1000; i < 1220; i++ {
		status := "success"
		if i%10 == 0 {
			status = "failed"
		}
		all = append(all, fmt.Sprintf(`{"id":%d,"iid":%d,"name":"CI","status":"%s","ref":"main",`+
			`"sha":"abc","source":"push","created_at":"2026-09-14T10:00:00.000Z",`+
			`"updated_at":"2026-09-14T10:05:00.000Z"}`, i, i, status))
	}
	body := "[" + strings.Join(all, ",") + "]"

	mockPipelineGitLab(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Emulate the provider's own 100-per-page paging.
		page := atoiDefault(r.URL.Query().Get("page"), 1)
		per := 100
		start := (page - 1) * per
		if start >= len(all) {
			_, _ = w.Write([]byte("[]"))
			return
		}
		end := start + per
		if end > len(all) {
			end = len(all)
		}
		next := ""
		if end < len(all) {
			next = fmt.Sprintf("%d", page+1)
		}
		w.Header().Set("X-Next-Page", next)
		_, _ = w.Write([]byte("[" + strings.Join(all[start:end], ",") + "]"))
	}))
	_ = body

	get := func(page string) []any {
		t.Helper()
		req := newRequest(t, http.MethodGet, "/api/forge/pipelines?status=failure&perPage=3&page="+page, nil)
		withProjectCookie(req, env.ProjectDir)
		withAuthCookie(req, model.SessionToken)
		w := callHandler(ServeForgePipelines, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		return resp["pipelines"].([]any)
	}

	seen := map[float64]bool{}
	for _, page := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12"} {
		runs := get(page)
		for _, r := range runs {
			id := r.(map[string]any)["id"].(float64)
			assert.Falsef(t, seen[id], "run %v was served on an earlier page (pages overlap)", id)
			seen[id] = true
		}
	}
	// No run may be served twice — that is the bug. Also assert the walk reaches
	// BOTH provider pages: 1050 lives on provider page 1, 1100 on page 2.
	assert.True(t, seen[1050], "a failure from provider page 1 must be served")
	assert.True(t, seen[1100], "a failure from provider page 2 must be served")
}

// TestServeForgePipelines_ClampsPerPage covers the panic/empty-page guard.
func TestServeForgePipelines_ClampsPerPage(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	mockPipelineGitLab(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pipelineListBody))
	}))

	// A non-positive perPage used to panic (negative) or return an empty page
	// that still claimed more pages (zero).
	for _, perPage := range []string{"0", "-5", "-1"} {
		req := newRequest(t, http.MethodGet, "/api/forge/pipelines?perPage="+perPage, nil)
		withProjectCookie(req, env.ProjectDir)
		withAuthCookie(req, model.SessionToken)
		w := callHandler(ServeForgePipelines, req)

		require.Equal(t, http.StatusOK, w.Code, "perPage=%s must not fail", perPage)
		var resp map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.NotEmpty(t, resp["pipelines"], "perPage=%s must still return the default page", perPage)
	}
}

// TestServeForgePipelines_ClampsPage: a non-positive page must not produce a
// negative slice offset.
func TestServeForgePipelines_ClampsPage(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	mockPipelineGitLab(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pipelineListBody))
	}))

	for _, page := range []string{"0", "-3"} {
		req := newRequest(t, http.MethodGet, "/api/forge/pipelines?page="+page, nil)
		withProjectCookie(req, env.ProjectDir)
		withAuthCookie(req, model.SessionToken)
		w := callHandler(ServeForgePipelines, req)
		require.Equal(t, http.StatusOK, w.Code, "page=%s must not fail", page)
	}
}

// TestServeForgePipelines_SparseFilterIsBounded: a filter that matches nothing
// must terminate, not walk the entire history. The walk is capped, so the
// response is an empty page with no more pages — which ends the caller's
// infinite scroll instead of looping it.
func TestServeForgePipelines_SparseFilterIsBounded(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	var providerCalls int
	mockPipelineGitLab(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls++
		w.Header().Set("Content-Type", "application/json")
		// Always advertise another page: without a scan cap this would never end.
		w.Header().Set("X-Next-Page", "2")
		_, _ = w.Write([]byte(`[{"id":1,"iid":1,"name":"CI","status":"success","ref":"main",
			"sha":"abc","source":"push","created_at":"2026-09-14T10:00:00.000Z",
			"updated_at":"2026-09-14T10:05:00.000Z"}]`))
	}))

	req := newRequest(t, http.MethodGet, "/api/forge/pipelines?status=running&perPage=10", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgePipelines, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp["pipelines"])
	assert.LessOrEqual(t, providerCalls, 20, "the provider walk must be bounded")
}

// TestServeForgePipelines_HasMoreAndNextPage pins the paging contract the
// frontend's infinite scroll depends on.
func TestServeForgePipelines_HasMoreAndNextPage(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	// 5 failing runs; perPage=2 means three pages then no more.
	all := make([]string, 0, 5)
	for i := range 5 {
		all = append(all, fmt.Sprintf(`{"id":%d,"iid":%d,"name":"CI","status":"failed","ref":"main",`+
			`"sha":"abc","source":"push","created_at":"2026-09-14T10:00:00.000Z",`+
			`"updated_at":"2026-09-14T10:05:00.000Z"}`, i, i))
	}
	mockPipelineGitLab(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[" + strings.Join(all, ",") + "]"))
	}))

	req := newRequest(t, http.MethodGet, "/api/forge/pipelines?status=failure&perPage=2&page=1", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgePipelines, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp["pipelines"], 2)
	assert.Equal(t, true, resp["hasMore"])
	assert.Equal(t, float64(2), resp["nextPage"])

	// The last page is short and reports no more.
	req = newRequest(t, http.MethodGet, "/api/forge/pipelines?status=failure&perPage=2&page=3", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w = callHandler(ServeForgePipelines, req)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp["pipelines"], 1)
	assert.Equal(t, false, resp["hasMore"])
}

// TestServeForgePipeline_ReturnsRunAndJobs covers the detail endpoint, which is
// what makes a failure locatable.
func TestServeForgePipeline_ReturnsRunAndJobs(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	mockPipelineGitLab(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/jobs"):
			_, _ = w.Write([]byte(`[
				{"id":501,"name":"rspec","stage":"test","status":"failed",
				 "failure_reason":"script_failure","duration":540.5,
				 "web_url":"https://gitlab.example.com/acme/widgets/-/jobs/501",
				 "started_at":"2026-09-14T10:01:00.000Z","finished_at":"2026-09-14T10:10:00.000Z",
				 "runner":{"description":"docker-runner-1"}},
				{"id":502,"name":"build","stage":"build","status":"success","duration":30}
			]`))
		case strings.HasSuffix(r.URL.Path, "/pipelines"):
			_, _ = w.Write([]byte(pipelineListBody))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	req := newRequest(t, http.MethodGet, "/api/forge/pipeline?id=48", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgePipeline, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	run := resp["pipeline"].(map[string]any)
	assert.Equal(t, float64(48), run["id"])
	assert.Equal(t, "failure", run["status"])

	jobs := resp["jobs"].([]any)
	require.Len(t, jobs, 2)
	first := jobs[0].(map[string]any)
	assert.Equal(t, "rspec", first["name"])
	assert.Equal(t, "test", first["stage"], "GitLab reports a stage")
	assert.Equal(t, "script_failure", first["failureReason"])
	assert.Equal(t, "docker-runner-1", first["runner"])
	assert.Equal(t, float64(540), first["durationSeconds"])
}

// TestServeForgePipeline_RejectsMissingOrBadID: a bad id must not reach the
// provider at all.
func TestServeForgePipeline_RejectsMissingOrBadID(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	mockPipelineGitLab(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no provider call should happen for an invalid id")
	}))

	for _, query := range []string{"", "?id=", "?id=abc", "?id=0", "?id=-5"} {
		req := newRequest(t, http.MethodGet, "/api/forge/pipeline"+query, nil)
		withProjectCookie(req, env.ProjectDir)
		withAuthCookie(req, model.SessionToken)
		w := callHandler(ServeForgePipeline, req)
		assert.Equal(t, http.StatusBadRequest, w.Code, "query %q must be rejected", query)
	}
}

// TestServeForgePipeline_UnknownRunIsNotFound: an id outside the window is a
// 404, not an empty success the UI would render as a blank page.
func TestServeForgePipeline_UnknownRunIsNotFound(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	mockPipelineGitLab(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pipelineListBody))
	}))

	req := newRequest(t, http.MethodGet, "/api/forge/pipeline?id=999999", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgePipeline, req)

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

// TestServeForgePipeline_JobsFailureStillReturnsRun: the job listing is
// best-effort. A failure there must not hide the run's own metadata, which is
// the primary payload.
func TestServeForgePipeline_JobsFailureStillReturnsRun(t *testing.T) {
	env, teardown := setupForgeEnv(t)
	defer teardown()

	mockPipelineGitLab(t, env, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/jobs") {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"boom"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pipelineListBody))
	}))

	req := newRequest(t, http.MethodGet, "/api/forge/pipeline?id=47", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeForgePipeline, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(47), resp["pipeline"].(map[string]any)["id"])
	assert.Empty(t, resp["jobs"], "jobs fall back to an empty list, not an error")
}
