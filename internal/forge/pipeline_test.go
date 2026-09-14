package forge_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"clawbench/internal/forge"
)

// TestDedupeKey_PipelineRunsAreDistinct is the core regression for CI events.
//
// Every pipeline event carries the same itemType ("pipeline") and number (0),
// and its NewState is only ever "success" or "failure". Without a run-id
// revision two successful runs on one repository produce an identical key, and
// the second INSERT ... ON CONFLICT DO NOTHING silently drops it — the user
// would simply never be told about the second run.
func TestDedupeKey_PipelineRunsAreDistinct(t *testing.T) {
	repo := forge.Remote{Platform: forge.PlatformGitHub, Host: "github.com", Owner: "acme", Repo: "widgets"}

	first := forge.Change{
		Type: forge.EventPipeline, Number: 0,
		NewState: "success", PipelineRunID: 100,
	}
	second := forge.Change{
		Type: forge.EventPipeline, Number: 0,
		NewState: "success", PipelineRunID: 101,
	}

	k1 := forge.DedupeKey(repo, forge.ItemTypePipeline, 0, first)
	k2 := forge.DedupeKey(repo, forge.ItemTypePipeline, 0, second)

	assert.NotEqual(t, k1, k2, "two successful runs must not share a dedupe key")
	assert.Contains(t, k1, "run:100")
	assert.Contains(t, k2, "run:101")
}

// TestDedupeKey_PipelineSameRunIsStable verifies the other half of the
// contract: the same run re-derived on a later poll must produce the SAME key,
// or the overlap window would re-notify every pass.
func TestDedupeKey_PipelineSameRunIsStable(t *testing.T) {
	repo := forge.Remote{Platform: forge.PlatformGitHub, Host: "github.com", Owner: "acme", Repo: "widgets"}
	ev := forge.Change{Type: forge.EventPipeline, NewState: "failure", PipelineRunID: 42}

	assert.Equal(t,
		forge.DedupeKey(repo, forge.ItemTypePipeline, 0, ev),
		forge.DedupeKey(repo, forge.ItemTypePipeline, 0, ev),
	)
}

// TestDedupeKey_StateEventsUnchanged guards the pre-existing behavior: the
// pipeline branch must not alter how state and comment events are keyed.
func TestDedupeKey_StateEventsUnchanged(t *testing.T) {
	repo := forge.Remote{Platform: forge.PlatformGitHub, Host: "github.com", Owner: "acme", Repo: "widgets"}

	merged := forge.DedupeKey(repo, forge.ItemTypeChangeRequest, 7,
		forge.Change{Type: forge.EventMerged, NewState: "merged"})
	assert.Contains(t, merged, "state:merged")

	comment := forge.DedupeKey(repo, forge.ItemTypeIssue, 7,
		forge.Change{Type: forge.EventCommented, CommentID: 99})
	assert.Contains(t, comment, "comment:99")
}

// TestNormalizeConclusion covers the union of GitHub conclusions and GitLab
// statuses, including the GitHub values that mean failure without being spelled
// "failure".
func TestNormalizeConclusion(t *testing.T) {
	cases := []struct {
		raw  string
		want forge.PipelineStatus
	}{
		// Terminal, fires.
		{"success", forge.PipelineSuccess},
		{"passed", forge.PipelineSuccess},
		{"failure", forge.PipelineFailure},
		{"failed", forge.PipelineFailure},
		{"timed_out", forge.PipelineFailure},
		{"startup_failure", forge.PipelineFailure},

		// Terminal, does not fire.
		{"cancelled", forge.PipelineCancelled},
		{"canceled", forge.PipelineCancelled},
		{"skipped", forge.PipelineSkipped},
		{"neutral", forge.PipelineSkipped},

		// Non-terminal.
		{"queued", forge.PipelineRunning},
		{"in_progress", forge.PipelineRunning},
		{"pending", forge.PipelineRunning},
		{"running", forge.PipelineRunning},
		{"created", forge.PipelineRunning},
		{"preparing", forge.PipelineRunning},
		{"waiting_for_resource", forge.PipelineRunning},
		{"manual", forge.PipelineRunning},
		{"scheduled", forge.PipelineRunning},
		// An empty conclusion means the run has not finished.
		{"", forge.PipelineRunning},

		// Unrecognized is not silently treated as success.
		{"something_new", forge.PipelineUnknown},
	}

	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			assert.Equal(t, tc.want, forge.NormalizeConclusion(tc.raw))
		})
	}
}

// TestPipelineTerminal pins which statuses may be recorded. A non-terminal run
// must NOT be recorded, or it would be treated as already-seen and its eventual
// completion would be lost.
func TestPipelineTerminal(t *testing.T) {
	assert.False(t, forge.PipelineTerminal(forge.PipelineRunning))
	assert.True(t, forge.PipelineTerminal(forge.PipelineSuccess))
	assert.True(t, forge.PipelineTerminal(forge.PipelineFailure))
	assert.True(t, forge.PipelineTerminal(forge.PipelineCancelled))
	assert.True(t, forge.PipelineTerminal(forge.PipelineSkipped))
	assert.True(t, forge.PipelineTerminal(forge.PipelineUnknown))
}

// TestPipelineFiresTask pins the trigger decision: only success and failure.
func TestPipelineFiresTask(t *testing.T) {
	assert.True(t, forge.PipelineFiresTask(forge.PipelineSuccess))
	assert.True(t, forge.PipelineFiresTask(forge.PipelineFailure))

	assert.False(t, forge.PipelineFiresTask(forge.PipelineRunning))
	assert.False(t, forge.PipelineFiresTask(forge.PipelineCancelled))
	assert.False(t, forge.PipelineFiresTask(forge.PipelineSkipped))
	assert.False(t, forge.PipelineFiresTask(forge.PipelineUnknown))
}
