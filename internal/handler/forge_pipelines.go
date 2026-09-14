package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"clawbench/internal/forge"
	"clawbench/internal/service"
)

// Pipeline (CI) endpoints.
//
// Like the issue/PR endpoints these are strictly read-only: they issue GETs
// through the forge provider and never mutate anything upstream. Re-running or
// cancelling a pipeline is therefore NOT offered — the integration has no write
// surface at all, by design.

// jsonPipelines is the response key for a run list.
const jsonPipelines = "pipelines"

// forgePipelineRunView is the frontend-facing shape of one CI run.
type forgePipelineRunView struct {
	Platform  string `json:"platform"`
	Host      string `json:"host"`
	Owner     string `json:"owner"`
	Repo      string `json:"repo"`
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Number    int    `json:"number"`
	Status    string `json:"status"`
	Ref       string `json:"ref"`
	SHA       string `json:"sha"`
	Event     string `json:"event"`
	Actor     string `json:"actor"`
	URL       string `json:"url"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	// DurationSeconds is omitted when unknown (GitHub's list endpoint does not
	// report it), so the UI can distinguish "instant" from "not reported".
	DurationSeconds int64  `json:"durationSeconds,omitempty"`
	Slug            string `json:"slug"`
}

// forgePipelineJobView is the frontend-facing shape of one job in a run.
type forgePipelineJobView struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	Stage           string `json:"stage,omitempty"`
	Status          string `json:"status"`
	FailureReason   string `json:"failureReason,omitempty"`
	Runner          string `json:"runner,omitempty"`
	URL             string `json:"url"`
	DurationSeconds int64  `json:"durationSeconds,omitempty"`
	StartedAt       string `json:"startedAt,omitempty"`
	CompletedAt     string `json:"completedAt,omitempty"`
}

func toPipelineRunView(pf *service.ProjectForge, run forge.PipelineRun) forgePipelineRunView {
	var slug, host, owner, repo string
	if pf != nil {
		slug, host, owner, repo = pf.Slug(), pf.Host, pf.Owner, pf.Repo
	}
	return forgePipelineRunView{
		Platform:        pf.Platform,
		Host:            host,
		Owner:           owner,
		Repo:            repo,
		ID:              run.ID,
		Name:            run.Name,
		Number:          run.Number,
		Status:          string(run.Status),
		Ref:             run.Ref,
		SHA:             run.SHA,
		Event:           run.Event,
		Actor:           run.Actor,
		URL:             run.URL,
		CreatedAt:       formatForgeTime(run.CreatedAt),
		UpdatedAt:       formatForgeTime(run.UpdatedAt),
		DurationSeconds: int64(run.Duration / time.Second),
		Slug:            slug,
	}
}

func toPipelineJobView(job forge.PipelineJob) forgePipelineJobView {
	return forgePipelineJobView{
		ID:              job.ID,
		Name:            job.Name,
		Stage:           job.Stage,
		Status:          string(job.Status),
		FailureReason:   job.FailureReason,
		Runner:          job.Runner,
		URL:             job.URL,
		DurationSeconds: int64(job.Duration / time.Second),
		StartedAt:       formatForgeTime(job.StartedAt),
		CompletedAt:     formatForgeTime(job.CompletedAt),
	}
}

// formatForgeTime renders a timestamp, leaving a zero time empty rather than
// emitting a misleading year-1 date.
func formatForgeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05Z")
}

// ServeForgePipelines lists CI runs for the project's bound repository.
//
//	GET /api/forge/pipelines?status=failed|success|running|all&page=N&perPage=N
//
// A platform without CI support returns 400 with a classified code, because the
// frontend needs to distinguish "no pipelines" from "this platform has none".
func ServeForgePipelines(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	pf, err := service.GetProjectForge(projectPath)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	if pf == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			strReqError: "no repository bound to this project",
			jsonCode:    jsonNoForgeBinding,
		})
		return
	}

	provider, err := newForgeProvider(pf)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{strReqError: err.Error()})
		return
	}
	lister, ok := provider.(forge.PipelineLister)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			strReqError: "this forge platform does not expose CI pipelines",
			jsonCode:    "ForgeNoPipelines",
		})
		return
	}

	q := r.URL.Query()
	page := atoiDefault(q.Get("page"), 1)
	perPage := atoiDefault(q.Get("perPage"), 30)
	statusFilter := normalizePipelineStatusFilter(q.Get("status"))

	runs, hasMore, err := collectPipelineRuns(forgeContext(r), lister, page, perPage, statusFilter)
	if err != nil {
		writeForgeError(w, err)
		return
	}

	views := make([]forgePipelineRunView, 0, len(runs))
	for i := range runs {
		views = append(views, toPipelineRunView(pf, runs[i]))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		jsonPipelines: views,
		jsonHasMore:   hasMore,
		"nextPage":    page + 1,
		jsonBinding:   bindingView(pf),
	})
}

// collectPipelineRuns gathers up to perPage runs matching an optional status
// filter, starting at page.
//
// The provider pages over runs, so this keeps requesting pages until the filter
// has yielded a full page or the provider runs out. The filter is applied here
// rather than server-side because GitHub's runs endpoint has no conclusion
// filter and GitLab's status values do not map one-to-one onto the normalized
// vocabulary — asking the provider to filter would mean two different
// definitions of "failed".
func collectPipelineRuns(
	ctx context.Context,
	lister forge.PipelineLister,
	page, perPage int,
	statusFilter string,
) ([]forge.PipelineRun, bool, error) {
	var runs []forge.PipelineRun
	for p := page; ; p++ {
		res, err := lister.ListPipelineRuns(ctx, time.Time{}, p, 100)
		if err != nil {
			return nil, false, err
		}
		for i := range res.Runs {
			run := res.Runs[i]
			if statusFilter != "" && string(run.Status) != statusFilter {
				continue
			}
			runs = append(runs, run)
		}
		if !res.HasMore || len(runs) >= perPage || res.NextPage <= 0 {
			break
		}
	}

	hasMore := len(runs) > perPage
	if hasMore {
		runs = runs[:perPage]
	}
	return runs, hasMore, nil
}

// ServeForgePipeline returns one run plus its jobs.
//
//	GET /api/forge/pipeline?id=<runID>
//
// The jobs are best-effort: a failure to list them still returns the run, since
// the run's own metadata is the primary payload and a job listing can fail for
// reasons unrelated to it (a very old run whose jobs were pruned, for example).
func ServeForgePipeline(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	runID, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("id")), 10, 64)
	if err != nil || runID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			strReqError: "invalid or missing pipeline id",
			jsonCode:    "ForgeInvalidPipelineID",
		})
		return
	}

	pf, err := service.GetProjectForge(projectPath)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	if pf == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			strReqError: "no repository bound to this project",
			jsonCode:    jsonNoForgeBinding,
		})
		return
	}

	provider, err := newForgeProvider(pf)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{strReqError: err.Error()})
		return
	}
	lister, ok := provider.(forge.PipelineLister)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			strReqError: "this forge platform does not expose CI pipelines",
			jsonCode:    "ForgeNoPipelines",
		})
		return
	}

	// Find the run by scanning pages. The providers have no get-single-run
	// method on the optional interface, and runs are ordered newest-first, so
	// this normally terminates on the first page.
	run, found, err := findPipelineRun(forgeContext(r), lister, runID)
	if err != nil {
		writeForgeError(w, err)
		return
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{
			strReqError: "pipeline run not found",
			jsonCode:    "ForgeNotFound",
		})
		return
	}

	jobs := []forgePipelineJobView{}
	if jl, ok := provider.(forge.PipelineJobLister); ok {
		raw, err := jl.ListPipelineJobs(forgeContext(r), runID)
		if err != nil {
			// Best-effort: log-and-continue is deliberate, see the doc comment.
			jobs = []forgePipelineJobView{}
		} else {
			for i := range raw {
				jobs = append(jobs, toPipelineJobView(raw[i]))
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"pipeline":  toPipelineRunView(pf, run),
		"jobs":      jobs,
		jsonBinding: bindingView(pf),
	})
}

// findPipelineRun scans pages for one run id, bounded so a bogus id cannot make
// the server walk the whole history.
func findPipelineRun(ctx context.Context, lister forge.PipelineLister, runID int64) (forge.PipelineRun, bool, error) {
	const maxPages = 5
	for p := 1; p <= maxPages; p++ {
		res, err := lister.ListPipelineRuns(ctx, time.Time{}, p, 100)
		if err != nil {
			return forge.PipelineRun{}, false, err
		}
		for i := range res.Runs {
			if res.Runs[i].ID == runID {
				return res.Runs[i], true, nil
			}
		}
		if !res.HasMore || res.NextPage <= 0 {
			break
		}
	}
	return forge.PipelineRun{}, false, nil
}

// normalizePipelineStatusFilter validates the status query parameter. An
// unrecognized value means "no filter" rather than an error, matching how the
// item endpoints treat a bad `state`.
func normalizePipelineStatusFilter(s string) string {
	switch strings.TrimSpace(s) {
	case string(forge.PipelineFailure), string(forge.PipelineSuccess),
		string(forge.PipelineRunning), string(forge.PipelineCancelled),
		string(forge.PipelineSkipped):
		return strings.TrimSpace(s)
	default:
		return ""
	}
}
