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

// Paging bounds for the run list.
//
// The caller's page indexes the FILTERED result, so serving page N requires
// walking the provider from its first page and skipping (N-1)*perPage matches.
// Both bounds exist to keep that walk finite:
//
//   - pipelineProviderPageSize is the page size requested from the provider
//     (the platforms cap at 100).
//   - pipelineScanPageLimit caps how many provider pages one request may read,
//     so a sparse filter cannot scan the whole history. A short or empty page is
//     the natural end of an infinite scroll.
const (
	pipelineProviderPageSize = 100
	pipelineScanPageLimit    = 20
	pipelineDefaultPerPage   = 30
	pipelineMaxPerPage       = 100
)

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
	// Unread is true when this run has activity the user has not seen. Filled
	// from the same rows the dock badge counts.
	Unread bool `json:"unread,omitempty"`
	// PullRequests lists the change requests this run is attached to, so the
	// detail view can link straight to them. Omitted when there are none, which
	// is the normal case for a push-to-branch run — the client must treat the
	// absent field as "no linked change request", not as a failure.
	PullRequests []forgePipelinePullRequestView `json:"pullRequests,omitempty"`
}

// forgePipelinePullRequestView is one linked change request of a run.
type forgePipelinePullRequestView struct {
	// Number is the PR/MR number, which is what the client uses to open the
	// item in-panel.
	Number int `json:"number"`
	// Title is best-effort: GitHub reports it, GitLab's pipeline payload does
	// not. The client falls back to rendering just the number.
	Title string `json:"title,omitempty"`
	// URL is the web link, when the platform (or the adapter) could build one.
	URL string `json:"url,omitempty"`
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
	view := forgePipelineRunView{
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
	for i := range run.PullRequests {
		pr := run.PullRequests[i]
		view.PullRequests = append(view.PullRequests, forgePipelinePullRequestView{
			Number: pr.Number,
			Title:  pr.Title,
			URL:    pr.URL,
		})
	}
	return view
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
	page := clampPipelinePage(atoiDefault(q.Get("page"), 1))
	perPage := clampPipelinePerPage(atoiDefault(q.Get("perPage"), pipelineDefaultPerPage))
	statusFilter := normalizePipelineStatusFilter(q.Get("status"))

	runs, hasMore, err := collectPipelineRuns(forgeContext(r), lister, page, perPage, statusFilter)
	if err != nil {
		writeForgeError(w, err, pf)
		return
	}

	// Tag each run with its unread state. One query for the whole page; the run
	// key carries the run id, since a pipeline event has no item number.
	unreadKeys, err := service.UnreadForgeItemKeys(pf.RepoKey())
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	views := make([]forgePipelineRunView, 0, len(runs))
	for i := range runs {
		view := toPipelineRunView(pf, runs[i])
		view.Unread = unreadKeys[forge.PipelineItemKey(runs[i].ID)]
		views = append(views, view)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		jsonPipelines: views,
		jsonHasMore:   hasMore,
		"nextPage":    page + 1,
		jsonBinding:   bindingView(pf),
	})
}

// ServeForgeItemPipelines lists the CI runs attached to one change request, for
// the "CI" section of the PR detail view.
//
// This is the reverse of the linked-PR lookup on /api/forge/pipeline: it answers
// "did this change pass CI?", which is what a reviewer wants while reading a PR.
//
// It is a separate endpoint rather than a field on /api/forge/item because it
// costs a request per call, so it must only be fetched when the user actually
// opens the CI section — folding it into the item payload would charge every PR
// open for a list most users never expand.
//
//	GET /api/forge/item-pipelines?type=pr&number=N
//
// A platform without CI support, or a change request with no runs, both answer
// 200 with an empty list: neither is an error.
func ServeForgeItemPipelines(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	number := atoiDefault(r.URL.Query().Get("number"), 0)
	if number <= 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", nil)
		return
	}
	// Only a change request can have CI attached in this sense; an issue number
	// would address a different resource.
	typ := forge.ItemTypeIssue
	if r.URL.Query().Get("type") == string(forge.ItemTypeChangeRequest) {
		typ = forge.ItemTypeChangeRequest
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
	lister, ok := provider.(forge.PipelineItemLister)
	if !ok {
		// The platform has no CI surface. Not an error: the section is simply
		// absent, which the client renders by hiding it.
		writeJSON(w, http.StatusOK, map[string]any{
			jsonPipelines: []forgePipelineRunView{},
			jsonBinding:   bindingView(pf),
		})
		return
	}

	item, err := provider.GetItem(forgeContext(r), typ, number)
	if err != nil {
		writeForgeError(w, err, pf)
		return
	}

	runs, err := lister.ListPipelinesForItem(forgeContext(r), item, 0)
	if err != nil {
		writeForgeError(w, err, pf)
		return
	}

	views := make([]forgePipelineRunView, 0, len(runs))
	for i := range runs {
		views = append(views, toPipelineRunView(pf, runs[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		jsonPipelines: views,
		jsonBinding:   bindingView(pf),
	})
}

// collectPipelineRuns returns one page of runs matching an optional status
// filter, plus whether further pages may exist.
//
// The status filter is applied here rather than server-side because GitHub's
// runs endpoint has no conclusion filter and GitLab's status values do not map
// one-to-one onto the normalized vocabulary — asking the provider to filter
// would mean two different definitions of "failed".
//
// That local filter is what forces the walk to restart from provider page 1:
// the caller's `page` indexes the FILTERED result, while the provider pages the
// unfiltered one, so there is no provider page that corresponds to "the Nth page
// of failures". Slicing a walk that started at the caller's page would re-serve
// already-shown runs (and skip others) — which is exactly what it used to do.
//
// The walk is bounded so a sparse filter (e.g. `status=running` on a repo with
// no running runs) cannot scan the entire history on every request. When the
// bound is reached the page is short or empty, which ends the caller's infinite
// scroll: later pages re-walk and slice a deeper window, so scrolling still
// terminates rather than looping.
func collectPipelineRuns(
	ctx context.Context,
	lister forge.PipelineLister,
	page, perPage int,
	statusFilter string,
) ([]forge.PipelineRun, bool, error) {
	skip := (page - 1) * perPage
	// One extra so "is there another page" is answerable without a second walk.
	want := skip + perPage + 1

	collected := make([]forge.PipelineRun, 0, want)
	for p := 1; p <= pipelineScanPageLimit; p++ {
		res, err := lister.ListPipelineRuns(ctx, time.Time{}, p, pipelineProviderPageSize)
		if err != nil {
			return nil, false, err
		}
		for i := range res.Runs {
			run := res.Runs[i]
			if statusFilter != "" && string(run.Status) != statusFilter {
				continue
			}
			collected = append(collected, run)
		}
		if len(collected) >= want {
			break
		}
		if !res.HasMore || res.NextPage <= 0 {
			break
		}
	}

	if skip >= len(collected) {
		return nil, false, nil
	}
	end := skip + perPage
	if end > len(collected) {
		end = len(collected)
	}
	return collected[skip:end], len(collected) > end, nil
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
		writeForgeError(w, err, pf)
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

	// Resolve linked change requests for platforms that cannot report them
	// inline (GitLab). This costs one request, which is why it lives HERE and
	// not in the list path: the list is polled and paged, the detail is opened
	// once by a user. Best-effort like jobs — a failed lookup must not hide the
	// run itself, which is the primary payload.
	if resolver, ok := provider.(forge.PipelinePullRequestResolver); ok {
		resolved, err := resolver.ResolvePipelinePullRequests(forgeContext(r), run)
		if err == nil {
			run.PullRequests = resolved
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

// clampPipelinePage coerces the page number into a usable range.
//
// A negative or zero page would make the skip offset negative, which would
// slice the result backwards (or panic). Treating it as page 1 is the least
// surprising recovery and matches how a bad `state` is handled.
func clampPipelinePage(page int) int {
	if page < 1 {
		return 1
	}
	return page
}

// clampPipelinePerPage bounds the page size.
//
// A non-positive value is the dangerous case: the caller's slice end would be
// <= the start, so the page comes back empty while still reporting more pages —
// an infinite scroll that never fills. Clamping to the default keeps the
// contract "you always get a usable page".
func clampPipelinePerPage(n int) int {
	if n <= 0 {
		return pipelineDefaultPerPage
	}
	if n > pipelineMaxPerPage {
		return pipelineMaxPerPage
	}
	return n
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
