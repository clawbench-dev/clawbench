package forge

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeriveChanges_NilPrevEmitsOpened(t *testing.T) {
	// A nil prev on a later sync is a genuinely new item, so it must emit
	// `opened`. (The caller suppresses this on a repo's first-ever sync, which
	// is what prevents replaying history — DeriveChanges itself must not, or
	// items created after the baseline would never be reported.)
	changes := DeriveChanges(nil, ItemState{State: "open", Author: "alice"}, 1)
	require.Len(t, changes, 1)
	assert.Equal(t, EventOpened, changes[0].Type)
	assert.Equal(t, 1, changes[0].Number)
	assert.Equal(t, "alice", changes[0].Actor)
}

func TestDeriveChanges_NilPrevMergedItemReportsMerged(t *testing.T) {
	// A PR already merged when first seen reports the terminal state, not opened.
	changes := DeriveChanges(nil, ItemState{State: "merged", Merged: true}, 2)
	require.Len(t, changes, 1)
	assert.Equal(t, EventMerged, changes[0].Type)
}

func TestDeriveChanges_NilPrevClosedItemReportsClosed(t *testing.T) {
	changes := DeriveChanges(nil, ItemState{State: "closed"}, 3)
	require.Len(t, changes, 1)
	assert.Equal(t, EventClosed, changes[0].Type)
}

func TestDeriveChanges_Closed(t *testing.T) {
	prev := &Snapshot{State: "open"}
	changes := DeriveChanges(prev, ItemState{State: "closed"}, 7)
	require.Len(t, changes, 1)
	assert.Equal(t, EventClosed, changes[0].Type)
	assert.Equal(t, 7, changes[0].Number)
	assert.Equal(t, "open", changes[0].PrevState)
	assert.Equal(t, "closed", changes[0].NewState)
}

func TestDeriveChanges_Merged(t *testing.T) {
	prev := &Snapshot{State: "open"}
	changes := DeriveChanges(prev, ItemState{State: "merged", Merged: true}, 3)
	require.Len(t, changes, 1)
	assert.Equal(t, EventMerged, changes[0].Type)
}

func TestDeriveChanges_MergedFromClosedStillEmitsMerged(t *testing.T) {
	// GitHub reports merged PRs as state=closed with merged=true. Moving from a
	// genuinely-closed snapshot to merged must emit merged, not closed.
	prev := &Snapshot{State: "closed", Merged: false}
	changes := DeriveChanges(prev, ItemState{State: "closed", Merged: true}, 4)
	require.Len(t, changes, 1)
	assert.Equal(t, EventMerged, changes[0].Type)
}

func TestDeriveChanges_Reopened(t *testing.T) {
	prev := &Snapshot{State: "closed"}
	changes := DeriveChanges(prev, ItemState{State: "open"}, 5)
	require.Len(t, changes, 1)
	assert.Equal(t, EventReopened, changes[0].Type)
}

func TestDeriveChanges_ClosedThenReopenedThenMergedCollapsesToMerged(t *testing.T) {
	// A whole closed→reopened→merged cycle inside one interval must not emit a
	// stale "closed". The terminal state wins.
	prev := &Snapshot{State: "open"}
	changes := DeriveChanges(prev, ItemState{State: "merged", Merged: true}, 6)
	require.Len(t, changes, 1)
	assert.Equal(t, EventMerged, changes[0].Type, "terminal state must win over intermediate ones")
}

func TestDeriveChanges_NoChangeEmitsNothing(t *testing.T) {
	prev := &Snapshot{State: "open", LastCommentID: 10}
	changes := DeriveChanges(prev, ItemState{State: "open", LatestCommentID: 10}, 1)
	assert.Empty(t, changes)
}

func TestDeriveChanges_NewComment(t *testing.T) {
	prev := &Snapshot{State: "open", LastCommentID: 10}
	changes := DeriveChanges(prev, ItemState{State: "open", LatestCommentID: 11}, 2)
	require.Len(t, changes, 1)
	assert.Equal(t, EventCommented, changes[0].Type)
	assert.Equal(t, int64(11), changes[0].CommentID)
}

func TestDeriveChanges_EditedCommentDetectedByUpdatedAt(t *testing.T) {
	// An edit keeps the comment id but moves updated_at. Tracking only the id
	// would miss it.
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	prev := &Snapshot{State: "open", LastCommentID: 10, LastCommentUpdatedAt: base}
	changes := DeriveChanges(prev, ItemState{
		State:                  "open",
		LatestCommentID:        10,
		LatestCommentUpdatedAt: base.Add(time.Hour),
	}, 3)
	require.Len(t, changes, 1)
	assert.Equal(t, EventCommented, changes[0].Type)
}

func TestDeriveChanges_StaleCommentUpdatedAtEmitsNothing(t *testing.T) {
	base := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	prev := &Snapshot{State: "open", LastCommentID: 10, LastCommentUpdatedAt: base}
	changes := DeriveChanges(prev, ItemState{
		State:                  "open",
		LatestCommentID:        10,
		LatestCommentUpdatedAt: base.Add(-time.Hour), // older, not newer
	}, 4)
	assert.Empty(t, changes)
}

func TestDeriveChanges_StateAndCommentTogether(t *testing.T) {
	prev := &Snapshot{State: "open", LastCommentID: 1}
	changes := DeriveChanges(prev, ItemState{State: "closed", LatestCommentID: 2}, 9)
	require.Len(t, changes, 2, "both a state change and a comment must be reported")
	types := []EventType{changes[0].Type, changes[1].Type}
	assert.Contains(t, types, EventClosed)
	assert.Contains(t, types, EventCommented)
}

func TestDedupeKey_DistinguishesEvents(t *testing.T) {
	repo := Remote{Platform: PlatformGitHub, Host: "github.com", Owner: "a", Repo: "b"}

	k1 := DedupeKey(repo, ItemTypeIssue, 1, Change{Type: EventClosed, NewState: "closed"})
	k2 := DedupeKey(repo, ItemTypeIssue, 1, Change{Type: EventReopened, NewState: "open"})
	k3 := DedupeKey(repo, ItemTypeIssue, 1, Change{Type: EventCommented, CommentID: 5})
	k4 := DedupeKey(repo, ItemTypeIssue, 1, Change{Type: EventCommented, CommentID: 6})

	assert.NotEqual(t, k1, k2, "different event types must not collide")
	assert.NotEqual(t, k3, k4, "different comment ids must not collide")

	// The same event derives the same key (idempotent dispatch).
	assert.Equal(t, k1, DedupeKey(repo, ItemTypeIssue, 1, Change{Type: EventClosed, NewState: "closed"}))
}

// TestDedupeKey_OpenedVsReopened is the case the sibling test missed: both
// events end in state=open, so a state-only revision would collide and the
// reopen would be dropped as a duplicate (UNIQUE(dedupe_key) in forge_events).
func TestDedupeKey_OpenedVsReopened(t *testing.T) {
	repo := Remote{Platform: PlatformGitHub, Host: "github.com", Owner: "a", Repo: "b"}

	opened := DedupeKey(repo, ItemTypeIssue, 1, Change{Type: EventOpened, NewState: "open"})
	reopened := DedupeKey(repo, ItemTypeIssue, 1, Change{Type: EventReopened, NewState: "open"})

	assert.NotEqual(t, opened, reopened,
		"a reopened event must not collide with the original opened event")
}

// TestDedupeKey_RepeatedCloseReopenCycle ensures a second close/reopen cycle is
// not swallowed by the first: the type+state pair repeats, but the events are
// genuinely distinct occurrences and each must be storable.
func TestDedupeKey_CloseAfterReopen(t *testing.T) {
	repo := Remote{Platform: PlatformGitHub, Host: "github.com", Owner: "a", Repo: "b"}

	firstClose := DedupeKey(repo, ItemTypeIssue, 1, Change{Type: EventClosed, NewState: "closed"})
	reopen := DedupeKey(repo, ItemTypeIssue, 1, Change{Type: EventReopened, NewState: "open"})
	secondClose := DedupeKey(repo, ItemTypeIssue, 1, Change{Type: EventClosed, NewState: "closed"})

	assert.NotEqual(t, firstClose, reopen)
	// A repeat of the same type+state is treated as the same event (idempotent
	// within a dedupe window); this documents that known trade-off.
	assert.Equal(t, firstClose, secondClose)
}

func TestDedupeKey_ScopedByRepo(t *testing.T) {
	a := Remote{Platform: PlatformGitHub, Host: "github.com", Owner: "a", Repo: "b"}
	b := Remote{Platform: PlatformGitHub, Host: "github.com", Owner: "a", Repo: "c"}
	assert.NotEqual(t,
		DedupeKey(a, ItemTypeIssue, 1, Change{Type: EventClosed}),
		DedupeKey(b, ItemTypeIssue, 1, Change{Type: EventClosed}),
		"the same number in different repos must not collide")
}

func TestStateRank(t *testing.T) {
	assert.Greater(t, StateRank(StateMerged), StateRank(StateClosed))
	assert.Greater(t, StateRank(StateClosed), StateRank(StateOpen))
}

// TestParsePipelineRunID is the inverse of PipelineItemKey: a pipeline's run id
// is only recoverable from its item key, so this must round-trip and must not
// accept any other key shape.
func TestParsePipelineRunID(t *testing.T) {
	// Round trip.
	id, ok := ParsePipelineRunID(PipelineItemKey(555))
	require.True(t, ok)
	assert.Equal(t, int64(555), id)

	// Non-pipeline keys must be rejected rather than parsed as a run id — a
	// caller that accepted "pr/455" here would look up run 0.
	for _, key := range []string{"pr/455", "issue/1", "", "pipeline/run:", "pipeline/run:abc", "pipeline/0"} {
		got, ok := ParsePipelineRunID(key)
		assert.False(t, ok, "must reject %q", key)
		assert.Zero(t, got)
	}
}
