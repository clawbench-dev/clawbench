// Pipeline (GitLab CI) support for the forge provider.
//
// Mirrors the GitHub adapter's shape: pipelines are an optional capability, so
// callers type-assert forge.PipelineLister rather than the Provider interface
// growing a method GitLab-agnostic code does not need.
package gitlab

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"clawbench/internal/forge"
)

// ListPipelineRuns returns pipelines for this project, newest first.
//
// GitLab supports a real server-side `updated_after`, so the lower bound is
// applied by the API rather than locally.
//
// order_by and updated_after must be chosen TOGETHER, because GitLab errors on
// every other pairing:
//
//	order_by=id          + updated_after  → 500 Internal Server Error
//	order_by=updated_at  without it       → 500 on large projects
//	order_by=id          without it       → 200
//	order_by=updated_at  + updated_after  → 200
//
// The underlying cause is that updated_after filters on a column the query can
// only scan when the results are ordered by it; ordering by id instead leaves
// the planner without a usable index and the request times out as a 500. The
// two branches below are therefore a pair, not two independent choices.
func (p *Provider) ListPipelineRuns(ctx context.Context, since time.Time, page, perPage int) (forge.PipelineRunPage, error) {
	q := url.Values{}
	if since.IsZero() {
		q.Set("order_by", "id")
	} else {
		q.Set("order_by", "updated_at")
	}
	q.Set("sort", "desc")
	q.Set("page", strconv.Itoa(pageOrDefault(page)))
	q.Set("per_page", strconv.Itoa(perPageOrDefault(perPage)))
	if !since.IsZero() {
		q.Set("updated_after", since.UTC().Format(time.RFC3339))
	}

	var raw []gitlabPipeline
	hdr, err := p.getRaw(ctx, "/projects/"+p.project+"/pipelines", q, &raw)
	if err != nil {
		return forge.PipelineRunPage{}, err
	}

	out := make([]forge.PipelineRun, 0, len(raw))
	for i := range raw {
		out = append(out, raw[i].toPipelineRun())
	}
	hasMore, nextPage := paginationFromHeader(hdr)
	return forge.PipelineRunPage{
		Runs:     out,
		HasMore:  hasMore,
		NextPage: nextPage,
	}, nil
}

// ListPipelineJobs returns the jobs of one pipeline.
//
// Unlike GitHub, GitLab reports a real `stage` and a `failure_reason`, which is
// what makes the job table genuinely useful for locating a failure.
func (p *Provider) ListPipelineJobs(ctx context.Context, runID int64) ([]forge.PipelineJob, error) {
	if runID <= 0 {
		return nil, &forge.Error{
			Kind:    forge.ErrKindUnsupported,
			Message: "gitlab: invalid pipeline id " + strconv.FormatInt(runID, 10),
		}
	}
	q := url.Values{}
	q.Set("per_page", "100")

	path := "/projects/" + p.project + "/pipelines/" + strconv.FormatInt(runID, 10) + "/jobs"
	var raw []gitlabJob
	if err := p.get(ctx, path, q, &raw); err != nil {
		return nil, err
	}

	out := make([]forge.PipelineJob, 0, len(raw))
	for i := range raw {
		out = append(out, raw[i].toPipelineJob())
	}
	return out, nil
}

// gitlabPipeline is the subset of GitLab's pipeline payload this package uses.
type gitlabPipeline struct {
	ID        int64  `json:"id"`
	IID       int    `json:"iid"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Ref       string `json:"ref"`
	SHA       string `json:"sha"`
	Source    string `json:"source"`
	WebURL    string `json:"web_url"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	User      *struct {
		Username string `json:"username"`
	} `json:"user"`
}

func (g gitlabPipeline) toPipelineRun() forge.PipelineRun {
	return forge.PipelineRun{
		ID:        g.ID,
		Name:      g.pipelineName(),
		Number:    g.IID,
		Status:    forge.NormalizeConclusion(g.Status),
		Ref:       g.Ref,
		SHA:       g.SHA,
		Event:     g.Source,
		Actor:     g.actorLogin(),
		URL:       g.WebURL,
		CreatedAt: parseTime(g.CreatedAt),
		UpdatedAt: parseTime(g.UpdatedAt),
		// The list endpoint does not return a duration; it is only available on
		// the single-pipeline endpoint, so the run list shows none.
		Duration: 0,
	}
}

// pipelineName falls back to the ref: `name` only appears in the pipeline API
// from GitLab 16.3 onward, so a self-managed instance on an older version would
// otherwise render every run nameless.
func (g gitlabPipeline) pipelineName() string {
	if g.Name != "" {
		return g.Name
	}
	return g.Ref
}

func (g gitlabPipeline) actorLogin() string {
	if g.User == nil {
		return ""
	}
	return g.User.Username
}

// gitlabJob is the subset of GitLab's job payload this package uses.
type gitlabJob struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	Stage         string  `json:"stage"`
	Status        string  `json:"status"`
	FailureReason string  `json:"failure_reason"`
	WebURL        string  `json:"web_url"`
	StartedAt     string  `json:"started_at"`
	FinishedAt    string  `json:"finished_at"`
	Duration      float64 `json:"duration"`
	Runner        *struct {
		Description string `json:"description"`
	} `json:"runner"`
}

func (g gitlabJob) toPipelineJob() forge.PipelineJob {
	job := forge.PipelineJob{
		ID:            g.ID,
		Name:          g.Name,
		Stage:         g.Stage,
		Status:        forge.NormalizeConclusion(g.Status),
		FailureReason: g.FailureReason,
		URL:           g.WebURL,
		StartedAt:     parseTime(g.StartedAt),
		CompletedAt:   parseTime(g.FinishedAt),
	}
	if g.Runner != nil {
		job.Runner = g.Runner.Description
	}
	if g.Duration > 0 {
		job.Duration = time.Duration(g.Duration * float64(time.Second))
	}
	return job
}

// paginationFromHeader derives paging state from GitLab's X-Next-Page header.
// It shares the header vocabulary with listResult so the two paths cannot
// disagree about what "more pages" means.
func paginationFromHeader(hdr http.Header) (hasMore bool, nextPage int) {
	if hdr == nil {
		return false, 0
	}
	next := hdr.Get("X-Next-Page")
	if next == "" {
		return false, 0
	}
	n, err := strconv.Atoi(next)
	if err != nil || n <= 0 {
		return false, 0
	}
	return true, n
}
