// Pipeline (GitHub Actions) support for the forge provider.
//
// This lives in its own file because pipelines are an OPTIONAL capability: the
// Provider interface does not require them, callers type-assert
// forge.PipelineLister, and a platform without CI simply does not implement it.
package github

import (
	"context"
	"time"

	gogithub "github.com/google/go-github/v85/github"

	"clawbench/internal/forge"
)

// ListPipelineRuns returns workflow runs for this repository, newest first.
//
// GitHub has no server-side "updated after" filter on this endpoint, so `since`
// is applied locally. Because the results are ordered newest-first, the first
// page whose runs are ALL older than `since` is the end of the interesting
// range: everything after it is older still. That page therefore reports
// HasMore=false, which is what stops the caller's walk.
//
// Reporting the raw NextPage instead would be a real bug: the filtered page
// comes back empty while still advertising more pages, so the caller walks the
// repository's entire run history on every poll.
func (p *Provider) ListPipelineRuns(ctx context.Context, since time.Time, page, perPage int) (forge.PipelineRunPage, error) {
	runs, resp, err := p.client.Actions.ListRepositoryWorkflowRuns(ctx, p.owner, p.repo, &gogithub.ListWorkflowRunsOptions{
		ListOptions: gogithub.ListOptions{
			Page:    pageOrDefault(page),
			PerPage: perPageOrDefault(perPage),
		},
	})
	if err != nil {
		return forge.PipelineRunPage{}, wrapErr(err)
	}

	total := len(runs.WorkflowRuns)
	out := make([]forge.PipelineRun, 0, total)
	filteredOut := 0
	for _, r := range runs.WorkflowRuns {
		run := convertWorkflowRun(r)
		if !since.IsZero() && run.UpdatedAt.Before(since) {
			filteredOut++
			continue
		}
		out = append(out, run)
	}

	// The whole page was older than the watermark: this is the boundary.
	atBoundary := total > 0 && filteredOut == total
	hasMore := !atBoundary && resp != nil && resp.NextPage > 0

	next := 0
	if hasMore {
		next = nextPageOf(resp)
	}
	return forge.PipelineRunPage{
		Runs:     out,
		HasMore:  hasMore,
		NextPage: next,
	}, nil
}

// ListPipelineJobs returns the jobs of one workflow run.
//
// GitHub has no pipeline "stage" concept, so Stage is left empty; the job name
// and conclusion are what the UI shows.
func (p *Provider) ListPipelineJobs(ctx context.Context, runID int64) ([]forge.PipelineJob, error) {
	jobs, _, err := p.client.Actions.ListWorkflowJobs(ctx, p.owner, p.repo, runID, &gogithub.ListWorkflowJobsOptions{
		ListOptions: gogithub.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, wrapErr(err)
	}

	out := make([]forge.PipelineJob, 0, len(jobs.Jobs))
	for _, j := range jobs.Jobs {
		out = append(out, forge.PipelineJob{
			ID:          j.GetID(),
			Name:        j.GetName(),
			Status:      jobStatus(j),
			Runner:      j.GetRunnerName(),
			URL:         j.GetHTMLURL(),
			StartedAt:   j.GetStartedAt().Time,
			CompletedAt: j.GetCompletedAt().Time,
			Duration:    jobDuration(j),
		})
	}
	return out, nil
}

// ListPipelinesForItem returns the workflow runs attached to a change request,
// newest first.
//
// The head branch is the lookup key: GitHub has no "runs for this PR" endpoint,
// but every run carries its head branch and the list endpoint accepts a `branch`
// filter, so one request answers it. The branch comes from the item itself, which
// is why Item carries SourceBranch.
//
// `limit` caps the result because the caller renders a summary list, not a
// history: a long-lived PR accumulates many runs and only the recent ones are
// interesting. A limit <= 0 means the default.
func (p *Provider) ListPipelinesForItem(ctx context.Context, item forge.Item, limit int) ([]forge.PipelineRun, error) {
	if item.SourceBranch == "" {
		// Nothing to filter on. An issue (or a payload that omitted the branch)
		// has no CI to show; that is an empty result, not an error.
		return nil, nil
	}
	if limit <= 0 {
		limit = defaultItemPipelineLimit
	}

	runs, _, err := p.client.Actions.ListRepositoryWorkflowRuns(ctx, p.owner, p.repo, &gogithub.ListWorkflowRunsOptions{
		Branch: item.SourceBranch,
		ListOptions: gogithub.ListOptions{
			Page:    1,
			PerPage: perPageOrDefault(limit),
		},
	})
	if err != nil {
		return nil, wrapErr(err)
	}

	out := make([]forge.PipelineRun, 0, len(runs.WorkflowRuns))
	for _, r := range runs.WorkflowRuns {
		if len(out) >= limit {
			break
		}
		out = append(out, convertWorkflowRun(r))
	}
	return out, nil
}

// defaultItemPipelineLimit bounds the per-item run list. A PR's CI history is
// unbounded in principle, and the UI shows a short summary.
const defaultItemPipelineLimit = 10

// convertWorkflowRun normalizes one GitHub run.
//
// GitHub splits the outcome in two: `status` says where the run is
// (queued/in_progress/completed) and `conclusion` says how it ended. A run that
// has not completed has an empty conclusion, which must NOT be read as success.
func convertWorkflowRun(r *gogithub.WorkflowRun) forge.PipelineRun {
	status := forge.PipelineRunning
	if r.GetStatus() == "completed" {
		status = forge.NormalizeConclusion(r.GetConclusion())
	}

	return forge.PipelineRun{
		ID:           r.GetID(),
		Name:         runName(r),
		Number:       r.GetRunNumber(),
		Status:       status,
		Ref:          r.GetHeadBranch(),
		SHA:          r.GetHeadSHA(),
		Event:        r.GetEvent(),
		Actor:        actorLogin(r),
		URL:          r.GetHTMLURL(),
		CreatedAt:    r.GetCreatedAt().Time,
		UpdatedAt:    r.GetUpdatedAt().Time,
		Duration:     runDuration(r),
		PullRequests: convertRunPullRequests(r),
	}
}

// convertRunPullRequests extracts the change requests a run is attached to.
//
// GitHub reports these inline on every workflow run, so linking a run to its PR
// costs no extra request. It is a LIST because a run can belong to more than one
// PR — the same commit pushed to a branch with several open PRs against it runs
// the workflow once per PR association.
//
// An entry with no number is dropped: it could not be opened, and rendering a
// link that goes nowhere is worse than rendering none. Title and URL are
// best-effort, since older payloads omit them.
func convertRunPullRequests(r *gogithub.WorkflowRun) []forge.PipelinePullRequest {
	prs := r.PullRequests
	if len(prs) == 0 {
		return nil
	}
	out := make([]forge.PipelinePullRequest, 0, len(prs))
	for _, pr := range prs {
		if pr == nil || pr.GetNumber() == 0 {
			continue
		}
		out = append(out, forge.PipelinePullRequest{
			Number: pr.GetNumber(),
			Title:  pr.GetTitle(),
			URL:    pr.GetHTMLURL(),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// runName prefers the display title, falling back to the workflow name.
//
// `name` is the WORKFLOW's name ("CI", "PR Lint", "Auto Merge"), which is
// constant for every run of that workflow — so a run list built from it shows
// the same title on every row and the user cannot tell one run from another.
// `display_title` is what distinguishes them: the head commit's subject for a
// push/PR run, or the workflow's own name for a manually dispatched one.
//
// The fallback matters for older payloads that omit display_title.
func runName(r *gogithub.WorkflowRun) string {
	if t := r.GetDisplayTitle(); t != "" {
		return t
	}
	return r.GetName()
}

// actorLogin returns the user who triggered the run. TriggeringActor is the
// more precise field (it distinguishes a re-run by someone else from the
// original actor), but it is newer, so Actor is the fallback.
func actorLogin(r *gogithub.WorkflowRun) string {
	if u := r.GetTriggeringActor(); u != nil && u.GetLogin() != "" {
		return u.GetLogin()
	}
	if u := r.GetActor(); u != nil {
		return u.GetLogin()
	}
	return ""
}

// runDuration derives the runtime from the run's start and last-update
// timestamps. GitHub's list endpoint does not return a duration, so a negative
// or missing span yields zero ("unknown") rather than a nonsense value.
func runDuration(r *gogithub.WorkflowRun) time.Duration {
	start := r.GetRunStartedAt().Time
	if start.IsZero() {
		start = r.GetCreatedAt().Time
	}
	end := r.GetUpdatedAt().Time
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start)
}

// jobStatus normalizes a job's outcome. Jobs carry the same status/conclusion
// split as runs.
func jobStatus(j *gogithub.WorkflowJob) forge.PipelineStatus {
	if j.GetStatus() == "completed" {
		return forge.NormalizeConclusion(j.GetConclusion())
	}
	return forge.NormalizeConclusion(j.GetStatus())
}

// jobDuration derives a job's runtime from its own timestamps.
func jobDuration(j *gogithub.WorkflowJob) time.Duration {
	start := j.GetStartedAt().Time
	end := j.GetCompletedAt().Time
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start)
}

// nextPageOf extracts the next page number, or 0 when there is none.
func nextPageOf(resp *gogithub.Response) int {
	if resp == nil {
		return 0
	}
	return resp.NextPage
}
