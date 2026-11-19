// Pipeline (Gitea Actions / Forgejo Actions) support for the forge provider.
//
// Mirrors the GitHub adapter's shape: pipelines are an OPTIONAL capability, so
// callers type-assert forge.PipelineLister rather than the Provider interface
// growing a method not every platform can serve.
//
// Gitea Actions deliberately imitates GitHub Actions — same status/conclusion
// split, same `branch` filter, same job shape — so the normalization here is the
// same as GitHub's. Two differences matter:
//
//   - There is no server-side "updated after" filter, so `since` is applied
//     locally (same boundary trick as GitHub: a page that is entirely older than
//     the watermark ends the walk).
//   - A run carries no pull-request association at all, and Gitea has no
//     "open pull requests for this branch" filter. The reverse direction (a pull
//     request's runs) is served by the branch filter, so PipelineItemLister is
//     implemented and PipelinePullRequestResolver is not.
package gitea

import (
	"context"
	"net/url"
	"strconv"
	"time"

	"clawbench/internal/forge"
)

// ListPipelineRuns returns workflow runs for this repository, newest first.
//
// Gitea has no "updated after" filter on this endpoint, so `since` is applied
// locally. Because results are newest-first, the first page whose runs are ALL
// older than `since` is the end of the interesting range — reporting the raw
// next page there would make the caller walk the repository's whole run history
// on every poll (the same trap the GitHub adapter documents).
func (p *Provider) ListPipelineRuns(ctx context.Context, since time.Time, page, perPage int) (forge.PipelineRunPage, error) {
	q := url.Values{}
	q.Set("page", strconv.Itoa(pageOrDefault(page)))
	q.Set("limit", strconv.Itoa(perPageOrDefault(perPage)))

	var raw giteaRunList
	hdr, err := p.getRaw(ctx, p.repoPath("/actions/runs"), q, &raw)
	if err != nil {
		return forge.PipelineRunPage{}, err
	}

	total := len(raw.WorkflowRuns)
	out := make([]forge.PipelineRun, 0, total)
	filteredOut := 0
	for i := range raw.WorkflowRuns {
		run := raw.WorkflowRuns[i].toPipelineRun()
		if !since.IsZero() && run.UpdatedAt.Before(since) {
			filteredOut++
			continue
		}
		out = append(out, run)
	}

	// The whole page was older than the watermark: this is the boundary.
	atBoundary := total > 0 && filteredOut == total
	more := !atBoundary && hasMore(hdr, total, perPageOrDefault(perPage))
	next := 0
	if more {
		next = pageOrDefault(page) + 1
	}
	return forge.PipelineRunPage{Runs: out, HasMore: more, NextPage: next}, nil
}

// ListPipelineJobs returns the jobs of one run.
//
// Gitea has no pipeline "stage" concept and no failure_reason, so both stay
// empty; the job name, conclusion and runner are what the UI shows.
func (p *Provider) ListPipelineJobs(ctx context.Context, runID int64) ([]forge.PipelineJob, error) {
	if runID <= 0 {
		return nil, &forge.Error{
			Kind:    forge.ErrKindUnsupported,
			Message: "gitea: invalid run id " + strconv.FormatInt(runID, 10),
		}
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(giteaMaxPerPage))

	var raw struct {
		Jobs []giteaJob `json:"jobs"`
	}
	if err := p.get(ctx, p.repoPath("/actions/runs/"+strconv.FormatInt(runID, 10)+"/jobs"), q, &raw); err != nil {
		return nil, err
	}
	out := make([]forge.PipelineJob, 0, len(raw.Jobs))
	for i := range raw.Jobs {
		out = append(out, raw.Jobs[i].toPipelineJob())
	}
	return out, nil
}

// ListPipelinesForItem returns the runs attached to a change request, newest
// first, capped at limit.
//
// The head branch is the lookup key: every run carries its head branch and the
// list endpoint accepts a `branch` filter, so one request answers it. The branch
// comes from the item itself, which is why Item carries SourceBranch — and why
// this is only ever called from the detail view, where the item was read from
// the pull endpoint.
//
// `limit` caps the result because the caller renders a summary list, not a
// history.
func (p *Provider) ListPipelinesForItem(ctx context.Context, item forge.Item, limit int) ([]forge.PipelineRun, error) {
	if item.Type != forge.ItemTypeChangeRequest || item.SourceBranch == "" {
		// Nothing to filter on: an issue has no CI, and a list-shaped pull row
		// has no branch. An empty result, not an error.
		return nil, nil
	}
	if limit <= 0 {
		limit = defaultItemPipelineLimit
	}
	if limit > giteaMaxPerPage {
		limit = giteaMaxPerPage
	}

	q := url.Values{}
	q.Set("branch", item.SourceBranch)
	q.Set("limit", strconv.Itoa(limit))

	var raw giteaRunList
	if err := p.get(ctx, p.repoPath("/actions/runs"), q, &raw); err != nil {
		return nil, err
	}
	out := make([]forge.PipelineRun, 0, len(raw.WorkflowRuns))
	for i := range raw.WorkflowRuns {
		if len(out) >= limit {
			break
		}
		out = append(out, raw.WorkflowRuns[i].toPipelineRun())
	}
	return out, nil
}

// defaultItemPipelineLimit bounds the per-item run list, matching the other
// adapters so the platforms show a comparable number of rows.
const defaultItemPipelineLimit = 10

// giteaRunList is the list envelope. Gitea returns an object here, not a bare
// array — unlike its issues endpoint.
type giteaRunList struct {
	TotalCount   int        `json:"total_count"`
	WorkflowRuns []giteaRun `json:"workflow_runs"`
}

// giteaRun is one workflow run.
type giteaRun struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	DisplayTitle string     `json:"display_title"`
	HeadBranch   string     `json:"head_branch"`
	HeadSHA      string     `json:"head_sha"`
	RunNumber    int        `json:"run_number"`
	Event        string     `json:"event"`
	Status       string     `json:"status"`
	Conclusion   string     `json:"conclusion"`
	HTMLURL      string     `json:"html_url"`
	CreatedAt    string     `json:"created_at"`
	UpdatedAt    string     `json:"updated_at"`
	RunStartedAt string     `json:"run_started_at"`
	Actor        *giteaUser `json:"actor"`
}

// toPipelineRun normalizes one run.
//
// Gitea splits the outcome in two, like GitHub: `status` says where the run is
// (queued/in_progress/completed) and `conclusion` says how it ended. A run that
// has not completed has an empty conclusion, which must NOT be read as success.
func (g giteaRun) toPipelineRun() forge.PipelineRun {
	status := forge.PipelineRunning
	if g.Status == "completed" {
		status = forge.NormalizeConclusion(g.Conclusion)
	}
	run := forge.PipelineRun{
		ID:        g.ID,
		Name:      g.runName(),
		Number:    g.RunNumber,
		Status:    status,
		Ref:       g.HeadBranch,
		SHA:       g.HeadSHA,
		Event:     g.Event,
		URL:       g.HTMLURL,
		CreatedAt: parseTime(g.CreatedAt),
		UpdatedAt: parseTime(g.UpdatedAt),
		Duration:  g.duration(),
	}
	if g.Actor != nil {
		run.Actor = g.Actor.Login
	}
	return run
}

// runName prefers the display title, falling back to the workflow name: the
// workflow name is constant for every run of that workflow, so a list built from
// it shows identical rows the user cannot tell apart.
func (g giteaRun) runName() string {
	if g.DisplayTitle != "" {
		return g.DisplayTitle
	}
	return g.Name
}

// duration derives the runtime from the run's start and last-update timestamps.
// A negative or missing span yields zero ("unknown") rather than nonsense.
func (g giteaRun) duration() time.Duration {
	start := parseTime(g.RunStartedAt)
	if start.IsZero() {
		start = parseTime(g.CreatedAt)
	}
	end := parseTime(g.UpdatedAt)
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start)
}

// giteaJob is one job of a run.
type giteaJob struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion"`
	HTMLURL     string `json:"html_url"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
	RunnerName  string `json:"runner_name"`
}

func (g giteaJob) toPipelineJob() forge.PipelineJob {
	job := forge.PipelineJob{
		ID:          g.ID,
		Name:        g.Name,
		Status:      jobStatus(g.Status, g.Conclusion),
		Runner:      g.RunnerName,
		URL:         g.HTMLURL,
		StartedAt:   parseTime(g.StartedAt),
		CompletedAt: parseTime(g.CompletedAt),
	}
	if !job.StartedAt.IsZero() && !job.CompletedAt.IsZero() && !job.CompletedAt.Before(job.StartedAt) {
		job.Duration = job.CompletedAt.Sub(job.StartedAt)
	}
	return job
}

// jobStatus normalizes a job's outcome. Jobs carry the same status/conclusion
// split as runs.
func jobStatus(status, conclusion string) forge.PipelineStatus {
	if status == "completed" {
		return forge.NormalizeConclusion(conclusion)
	}
	return forge.NormalizeConclusion(status)
}
