package forge

import (
	"context"
	"time"
)

// PipelineStatus is the normalized outcome of a CI run.
//
// The two platforms spell these differently — GitHub splits a run into
// status=queued|in_progress|completed plus a separate conclusion, while GitLab
// reports a single status — so both adapters normalize into this vocabulary and
// every consumer only ever sees these values.
type PipelineStatus string

const (
	// PipelineSuccess and PipelineFailure are the terminal outcomes that fire
	// an event task.
	PipelineSuccess PipelineStatus = "success"
	PipelineFailure PipelineStatus = "failure"
	// PipelineRunning covers every non-terminal state (queued, in_progress,
	// created, pending, preparing, manual, scheduled, ...). A run in this state
	// is deliberately never recorded or dispatched, so it can still fire when
	// it eventually finishes.
	PipelineRunning PipelineStatus = "running"
	// PipelineCancelled and PipelineSkipped are terminal but deliberately do
	// NOT fire a task: neither is a statement about the code's health.
	PipelineCancelled PipelineStatus = "cancelled"
	PipelineSkipped   PipelineStatus = "skipped"
	// PipelineUnknown is the fallback for a conclusion this build does not
	// recognize. It is treated as terminal so it is recorded (and therefore not
	// re-evaluated forever), but it does not fire a task.
	PipelineUnknown PipelineStatus = "unknown"
)

// PipelineRun is one CI run, normalized across platforms.
//
// Known limitation (GitHub): re-running a workflow keeps the same run ID and
// increments an attempt counter, so the second outcome is indistinguishable from
// the first by ID alone. The ledger therefore treats a re-run as already
// handled and its new result does not fire an event. Fixing this would require
// carrying the attempt number through the dedupe key.
type PipelineRun struct {
	// ID is the platform's run identity (GitHub run id / GitLab pipeline id).
	// It is the dedupe discriminator, so it must be stable and unique per repo.
	ID int64
	// Name is the workflow name. GitLab only exposes a pipeline name from 16.3
	// onward, so adapters fall back to the ref when it is empty.
	Name string
	// Number is the human-facing run counter (GitHub run_number / GitLab iid).
	Number int
	// Status is the normalized outcome.
	Status PipelineStatus
	// Ref is the branch or tag the run executed on.
	Ref string
	// SHA is the full commit hash the run executed against.
	SHA string
	// Event is what triggered the run, in the platform's own vocabulary
	// (GitHub "push"/"pull_request"/"schedule", GitLab "push"/"web"/...).
	// It is passed through rather than normalized: it is informational, and the
	// two vocabularies are not equivalent.
	Event string
	// Actor is the login that triggered the run, when the platform reports it.
	Actor string
	// URL is the web URL of the run.
	URL string
	// CreatedAt / UpdatedAt are the platform timestamps.
	CreatedAt time.Time
	UpdatedAt time.Time
	// Duration is how long the run took. Zero means unknown: GitHub's list
	// endpoint does not return a duration, so the adapter derives it from the
	// start/update timestamps and leaves it zero when it cannot.
	Duration time.Duration
	// PullRequests lists the change requests this run is attached to.
	//
	// Empty is a normal outcome, not an error: a push-to-main run has no PR at
	// all, and GitLab only reports the association for merge-request pipelines
	// (see the adapter). A consumer must therefore render "no linked PR" as
	// simply absent rather than as a failure.
	PullRequests []PipelinePullRequest
}

// PipelinePullRequest identifies a change request a run belongs to.
//
// It carries only what is needed to render and open the link: the full item is
// fetched separately when the user actually opens it, so this stays cheap enough
// to travel with every run in a list response.
type PipelinePullRequest struct {
	// Number is the platform's change-request number (GitHub PR number /
	// GitLab merge-request iid).
	Number int
	// Title is the change-request title, when the platform reports it. GitLab's
	// pipeline payload does not, so it is empty there.
	Title string
	// URL is the web URL of the change request, when known.
	URL string
}

// PipelineRunPage is a page of runs plus pagination metadata.
type PipelineRunPage struct {
	Runs     []PipelineRun
	HasMore  bool
	NextPage int
}

// PipelineLister is an OPTIONAL capability implemented by adapters that can
// report CI runs. It is deliberately separate from Provider so the required
// surface stays small and existing implementations (and test fakes) do not have
// to grow a method that only some platforms support. Callers type-assert:
//
//	if lister, ok := provider.(forge.PipelineLister); ok { ... }
type PipelineLister interface {
	// ListPipelineRuns returns runs updated at or after since, newest first.
	// A zero since means "no lower bound".
	//
	// HasMore must be false once the walk has reached the `since` boundary, even
	// if the platform still has older pages. A platform that filters `since`
	// locally (GitHub has no such parameter) would otherwise return an empty
	// page that still advertises more, and the caller would page through the
	// repository's entire history on every poll.
	ListPipelineRuns(ctx context.Context, since time.Time, page, perPage int) (PipelineRunPage, error)
}

// PipelineJob is one job within a run, normalized across platforms.
type PipelineJob struct {
	ID int64
	// Name is the job name.
	Name string
	// Stage is the pipeline stage (GitLab only; empty on GitHub, which has no
	// stage concept).
	Stage string
	// Status is the normalized job outcome.
	Status PipelineStatus
	// FailureReason is the platform's explanation for a failure (GitLab only).
	FailureReason string
	// Runner is the name of the machine that executed the job.
	Runner string
	// URL is the web URL of the job.
	URL string
	// StartedAt / CompletedAt are the job timestamps.
	StartedAt   time.Time
	CompletedAt time.Time
	// Duration is the job's runtime. Zero means unknown.
	Duration time.Duration
}

// PipelineJobLister is the optional capability for reading a run's jobs. It is
// separate from PipelineLister so an adapter could support runs without jobs.
type PipelineJobLister interface {
	ListPipelineJobs(ctx context.Context, runID int64) ([]PipelineJob, error)
}

// PipelinePullRequestResolver is the OPTIONAL capability for finding the change
// requests a run belongs to, for platforms that cannot report the association
// inline.
//
// It is deliberately separate from PipelineLister because it costs a request per
// run: callers must invoke it only where the extra latency and rate-limit budget
// are acceptable (the detail view), never while listing. An adapter that already
// reports the association inline (GitHub) simply does not implement it.
type PipelinePullRequestResolver interface {
	// ResolvePipelinePullRequests returns the change requests attached to a run,
	// or an empty slice when there are none.
	//
	// `run` is the whole run rather than just its id because the lookup key
	// differs per platform (GitLab matches on the run's source branch).
	//
	// Implementations must treat "no association found" as an empty result, not
	// an error: a run that belongs to no change request is the normal case.
	ResolvePipelinePullRequests(ctx context.Context, run PipelineRun) ([]PipelinePullRequest, error)
}

// PipelineItemLister is the OPTIONAL capability for listing the CI runs of one
// change request — the reverse of PipelinePullRequestResolver.
//
// It answers "did this change pass CI?", which is the question a reviewer asks
// while looking at a PR. Both platforms need a request for it (neither reports
// the association on the item payload), so like the resolver it is a detail-view
// capability and must never be called while listing.
//
// An adapter that cannot answer it simply does not implement the interface; the
// caller renders no CI section rather than an error.
type PipelineItemLister interface {
	// ListPipelinesForItem returns the runs attached to a change request, newest
	// first, capped at limit.
	//
	// `item` is the whole change request because the lookup key differs per
	// platform (GitHub matches on the head branch, GitLab on the item number).
	//
	// No association is an empty result, not an error.
	ListPipelinesForItem(ctx context.Context, item Item, limit int) ([]PipelineRun, error)
}

// NormalizeConclusion maps a platform conclusion/status string to a
// PipelineStatus. It is shared by both adapters so the mapping cannot drift
// between them.
//
// The accepted vocabulary is the union of GitHub conclusions and GitLab
// statuses; unrecognized values become PipelineUnknown rather than being
// silently treated as success.
func NormalizeConclusion(raw string) PipelineStatus {
	switch raw {
	case "success", "passed":
		return PipelineSuccess
	case "failure", "failed", "timed_out", "startup_failure", "error":
		// timed_out and startup_failure are GitHub conclusions that mean the run
		// did not do its job; they are failures from the user's point of view.
		return PipelineFailure
	case "cancelled", "canceled":
		return PipelineCancelled
	case "skipped", "neutral":
		// neutral means the run finished without a verdict; it is not a failure.
		return PipelineSkipped
	case "queued", "in_progress", "requested", "waiting", "pending", "running",
		"created", "preparing", "waiting_for_resource", "manual", "scheduled",
		"canceling", "waiting_for_callback":
		return PipelineRunning
	case "":
		return PipelineRunning
	default:
		return PipelineUnknown
	}
}

// PipelineTerminal reports whether a status is final. Only terminal runs are
// recorded, so a run that is still going can fire when it finishes.
func PipelineTerminal(s PipelineStatus) bool {
	switch s {
	case PipelineRunning:
		return false
	default:
		return true
	}
}

// PipelineFiresTask reports whether a terminal status should trigger an event
// task. Success and failure are the two outcomes worth acting on; cancelled and
// skipped are terminal but say nothing about the code, and unknown is not
// trusted enough to act on.
func PipelineFiresTask(s PipelineStatus) bool {
	return s == PipelineSuccess || s == PipelineFailure
}
