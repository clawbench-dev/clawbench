// Pipeline (Gitea Actions / Forgejo Actions) support for the forge provider.
//
// Mirrors the GitHub adapter's SHAPE (pipelines are an optional capability, so
// callers type-assert forge.PipelineLister) but NOT its wire format: Gitea's
// Actions API looks like GitHub's at a glance and differs in almost every field
// name, and it reports a single `status` instead of GitHub's status+conclusion
// pair. The payloads parsed here were taken from a live Forgejo instance.
//
// Two capability notes, both verified against that instance:
//
//   - The list endpoint has no time filter, so `since` is applied locally. It
//     also sends no pagination HEADERS; the body carries `total_count`, which is
//     what "is there another page" is computed from.
//   - Runs carry no pull-request association. The reverse lookup (a pull
//     request's runs) uses `ref=refs/heads/<branch>`; the `branch` parameter is
//     silently ignored by Gitea, so it must not be used.
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
// `since` is applied locally: Gitea has no "updated after" filter here. Because
// results are newest-first, a page whose runs are ALL older than `since` ends the
// walk — reporting more there would make the caller re-read the repository's
// whole run history on every poll.
func (p *Provider) ListPipelineRuns(ctx context.Context, since time.Time, page, perPage int) (forge.PipelineRunPage, error) {
	page = pageOrDefault(page)
	limit := perPageOrDefault(perPage)

	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	q.Set("limit", strconv.Itoa(limit))

	var raw giteaRunList
	if err := p.get(ctx, p.repoPath("/actions/runs"), q, &raw); err != nil {
		return forge.PipelineRunPage{}, err
	}

	total := len(raw.WorkflowRuns)
	out := make([]forge.PipelineRun, 0, total)
	filteredOut := 0
	for i := range raw.WorkflowRuns {
		run := raw.WorkflowRuns[i].toPipelineRun()
		if !since.IsZero() && !run.UpdatedAt.IsZero() && run.UpdatedAt.Before(since) {
			filteredOut++
			continue
		}
		out = append(out, run)
	}

	// The whole page was older than the watermark: this is the boundary.
	atBoundary := total > 0 && filteredOut == total
	more := !atBoundary && page*limit < raw.TotalCount
	next := 0
	if more {
		next = page + 1
	}
	return forge.PipelineRunPage{Runs: out, HasMore: more, NextPage: next}, nil
}

// ListPipelineJobs returns the jobs of one run.
//
// The endpoint answers with a bare ARRAY, not an object with a `jobs` key.
// Gitea has no pipeline "stage" concept and no failure_reason, so both stay
// empty; the job name, status and runner are what the UI shows.
func (p *Provider) ListPipelineJobs(ctx context.Context, runID int64) ([]forge.PipelineJob, error) {
	if runID <= 0 {
		return nil, &forge.Error{
			Kind:    forge.ErrKindUnsupported,
			Message: "gitea: invalid run id " + strconv.FormatInt(runID, 10),
		}
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(giteaMaxPerPage))

	var raw []giteaJob
	if err := p.get(ctx, p.repoPath("/actions/runs/"+strconv.FormatInt(runID, 10)+"/jobs"), q, &raw); err != nil {
		return nil, err
	}
	out := make([]forge.PipelineJob, 0, len(raw))
	for i := range raw {
		out = append(out, raw[i].toPipelineJob())
	}
	return out, nil
}

// ListPipelinesForItem returns the runs attached to a change request, newest
// first, capped at limit.
//
// The head branch is the lookup key, spelled as a full ref: Gitea's `ref` filter
// matches `refs/heads/<branch>` exactly, and a bare branch name returns NOTHING.
// (The `branch` parameter is accepted but ignored, so it must never be used —
// that is why this is a detail-view call whose correctness was checked live.)
//
// `limit` caps the result because the caller renders a summary list.
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
	q.Set("ref", "refs/heads/"+item.SourceBranch)
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
// array — and `total_count` is the only pagination signal (no headers).
type giteaRunList struct {
	TotalCount   int        `json:"total_count"`
	WorkflowRuns []giteaRun `json:"workflow_runs"`
}

// giteaRun is one workflow run, using Gitea's own field names.
//
// Note how far this is from GitHub's: the title is `title` (the commit subject),
// the branch is `prettyref`, the event is `trigger_event`, the actor is
// `trigger_user`, and the timestamps are `created`/`updated`/`started`/`stopped`.
// There is no `conclusion` at all — `status` is already terminal when the run
// finishes.
type giteaRun struct {
	ID           int64      `json:"id"`
	Title        string     `json:"title"`
	WorkflowID   string     `json:"workflow_id"`
	Index        int        `json:"index_in_repo"`
	TriggerEvent string     `json:"trigger_event"`
	PrettyRef    string     `json:"prettyref"`
	CommitSHA    string     `json:"commit_sha"`
	Status       string     `json:"status"`
	Started      string     `json:"started"`
	Stopped      string     `json:"stopped"`
	Created      string     `json:"created"`
	Updated      string     `json:"updated"`
	HTMLURL      string     `json:"html_url"`
	TriggerUser  *giteaUser `json:"trigger_user"`
}

// toPipelineRun normalizes one run.
func (g giteaRun) toPipelineRun() forge.PipelineRun {
	run := forge.PipelineRun{
		ID:        g.ID,
		Name:      g.runName(),
		Number:    g.Index,
		Status:    runStatus(g.Status),
		Ref:       g.PrettyRef,
		SHA:       g.CommitSHA,
		Event:     g.TriggerEvent,
		URL:       g.HTMLURL,
		CreatedAt: parseRunTime(g.Created),
		UpdatedAt: parseRunTime(g.Updated),
		Duration:  g.duration(),
	}
	if g.TriggerUser != nil {
		run.Actor = g.TriggerUser.Login
	}
	return run
}

// runName prefers the run's title (the commit subject, which distinguishes runs
// of one workflow) and falls back to the workflow file name.
func (g giteaRun) runName() string {
	if g.Title != "" {
		return g.Title
	}
	return g.WorkflowID
}

// duration derives the runtime from the run's own start/stop stamps. A missing
// or inverted span yields zero ("unknown") rather than nonsense.
func (g giteaRun) duration() time.Duration {
	start := parseRunTime(g.Started)
	end := parseRunTime(g.Stopped)
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start)
}

// giteaJob is one job of a run.
type giteaJob struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	HTMLURL    string `json:"html_url"`
	StartedAt  string `json:"started_at"`
	StoppedAt  string `json:"stopped_at"`
	RunnerName string `json:"runner_name"`
}

func (g giteaJob) toPipelineJob() forge.PipelineJob {
	job := forge.PipelineJob{
		ID:          g.ID,
		Name:        g.Name,
		Status:      runStatus(g.Status),
		Runner:      g.RunnerName,
		URL:         g.HTMLURL,
		StartedAt:   parseRunTime(g.StartedAt),
		CompletedAt: parseRunTime(g.StoppedAt),
	}
	if !job.StartedAt.IsZero() && !job.CompletedAt.IsZero() && !job.CompletedAt.Before(job.StartedAt) {
		job.Duration = job.CompletedAt.Sub(job.StartedAt)
	}
	return job
}

// runStatus maps Gitea's single status value onto the forge vocabulary.
//
// Gitea does NOT split status and conclusion the way GitHub does: a finished run
// reports "success"/"failure"/"cancelled"/"skipped" directly. Anything else
// (waiting, running, blocked, unknown) is non-terminal, which is what makes the
// poller look at it again — an unrecognized value must therefore NOT be treated
// as terminal, or its eventual outcome would never fire an event.
func runStatus(status string) forge.PipelineStatus {
	switch status {
	case "success":
		return forge.PipelineSuccess
	case "failure":
		return forge.PipelineFailure
	case "cancelled":
		return forge.PipelineCancelled
	case "skipped":
		return forge.PipelineSkipped
	default:
		return forge.PipelineRunning
	}
}

// parseRunTime parses a Gitea timestamp, treating its zero value as "unknown".
//
// Gitea marshals an unset TimeStamp as "0001-01-01T00:00:00Z", which parses
// fine and would otherwise look like a real (ancient) time — enough to make the
// incremental filter drop every run as "older than the watermark".
func parseRunTime(s string) time.Time {
	t := parseTime(s)
	if t.IsZero() || t.Year() < 2000 {
		return time.Time{}
	}
	return t
}
