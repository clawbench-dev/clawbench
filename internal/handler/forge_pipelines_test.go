package handler

import (
	"encoding/json"
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
