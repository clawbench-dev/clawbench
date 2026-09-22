package forge

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// EventType identifies a derived change event.
type EventType string

const (
	EventOpened    EventType = "opened"
	EventClosed    EventType = "closed"
	EventMerged    EventType = "merged"
	EventReopened  EventType = "reopened"
	EventCommented EventType = "commented"
	EventPipeline  EventType = "pipeline_done"
)

// Snapshot is the previously observed state of an item, used to derive events
// by comparison. It mirrors the persisted forge_items row without depending on
// the service package.
type Snapshot struct {
	State string
	// Merged reports whether the item was observed as merged.
	Merged bool
	// LastCommentID is the highest comment id seen so far.
	LastCommentID int64
	// LastCommentUpdatedAt is the newest comment timestamp seen. A comment edit
	// keeps its id but changes this timestamp, so both are needed.
	LastCommentUpdatedAt time.Time
}

// ItemState is the freshly fetched state of an item.
type ItemState struct {
	State                  string
	Merged                 bool
	LatestCommentID        int64
	LatestCommentUpdatedAt time.Time
	// LatestCommentAuthor / LatestCommentBody describe the newest comment and
	// are only populated on passes that fetch comments.
	LatestCommentAuthor string
	LatestCommentBody   string
	// Author is the item creator, used to attribute an `opened` event.
	Author string
	// CreatedAt is when the item was created upstream. It distinguishes a
	// genuinely new item from a pre-existing one that merely surfaced for the
	// first time — see DeriveChanges. Zero when the provider does not report it.
	CreatedAt time.Time
	// MergedAt is when the change request was merged, and it plays the same
	// role for `merged` that CreatedAt plays for `opened`: it distinguishes a PR
	// merged inside this window from one that was already merged when the
	// baseline was established. Zero when unknown or not merged.
	MergedAt time.Time
}

// Change is a derived event ready to be persisted and dispatched.
type Change struct {
	Type EventType
	// Number is the issue/PR number the event concerns.
	Number int
	// PrevState / NewState capture the transition (for payloads and logging).
	PrevState string
	NewState  string
	// CommentID is set for comment events.
	CommentID int64
	// CommentAuthor is the login of the commenter for comment events. It is the
	// acting user, which differs from the item author — anti-recursion must
	// compare against this, not Item.Author.
	CommentAuthor string
	// CommentBody is the raw Markdown of the newest comment (comment events).
	CommentBody string
	// Actor is the login that caused this event, when the provider exposes it.
	// For comment events it is the commenter; for `opened` it is the creator.
	// It is empty when the provider gives no actor (e.g. closed/merged, where
	// ListItems does not report who performed the transition).
	Actor string
	// Pipeline describes the finished CI run (pipeline_done). It carries the
	// run's own detail — ref, commit, duration, linked change requests — which
	// a consumer rendering the event needs and which a synthetic Item cannot
	// express. Nil for every non-pipeline event.
	//
	// It is a pointer to the whole run rather than a handful of scalars so a
	// future field needs no change here: the payload is already fetched by the
	// poller, so copying more of it into the prompt is free.
	Pipeline *PipelineRun
	// PipelineRunID is the platform's run identity for a pipeline event. It is
	// the revision discriminator in DedupeKey: without it two successful runs on
	// the same repository would collide on "state:success" and the second would
	// be dropped as a duplicate.
	//
	// It is kept SEPARATE from Pipeline because identity must survive where the
	// detail does not: a Change reconstructed from a persisted row (or built by
	// a test) has no Pipeline, but its dedupe and item keys still need the id.
	PipelineRunID int64
}

// DeriveChanges compares a stored snapshot with the freshly fetched item state
// and returns the events to dispatch. The item number is stamped onto each
// returned change.
//
// A nil prev means the item was not in the snapshot. Whether that is worth
// reporting depends on WHEN the item was created relative to the queried
// window, which is what windowStart carries:
//   - created inside the window → genuinely new since the baseline, report it;
//   - created before the window → pre-existing, surfaced only because it was
//     updated. Its prior state was never observed, so any transition inferred
//     from its current state alone would be a guess (an issue closed in 2020
//     would look like it just closed). Record it and stay silent.
//
// The caller must pass the same window start it gave the provider as `since`.
//
// The derivation is deliberately conservative:
//   - When several transitions occur within one polling interval, only the most
//     informative terminal state is emitted (merged > closed > reopened), so a
//     closed→reopened→merged sequence yields one "merged" rather than noise.
//   - A comment is detected when either its id increases OR an existing
//     comment's updated_at moves forward (an edit keeps the id).
func DeriveChanges(prev *Snapshot, cur ItemState, number int, windowStart time.Time) []Change {
	if prev == nil {
		if !isNewWithinWindow(cur.CreatedAt, windowStart) {
			return nil
		}
		// New item since the baseline. Emit the terminal state so a freshly
		// opened (or already-merged) item is reported exactly once.
		c, ok := deriveNewItemChange(cur)
		if !ok {
			return nil
		}
		c.Number = number
		return []Change{c}
	}

	var changes []Change
	if c, ok := deriveStateChange(prev, cur, windowStart); ok {
		changes = append(changes, c)
	}
	if deriveCommentChange(prev, cur) {
		changes = append(changes, Change{
			Type: EventCommented, CommentID: cur.LatestCommentID,
			CommentAuthor: cur.LatestCommentAuthor,
			CommentBody:   cur.LatestCommentBody,
			Actor:         cur.LatestCommentAuthor,
			PrevState:     prev.State, NewState: cur.State,
		})
	}

	for i := range changes {
		changes[i].Number = number
	}
	return changes
}

// isNewWithinWindow reports whether an item created at createdAt counts as new
// relative to the queried window start.
//
// An unknown createdAt (zero — a provider that does not report it, or a test
// that does not set it) is treated as new: staying silent on an unknown item
// would silently drop events for such providers, whereas reporting a
// pre-existing item at worst emits one spurious event per item, once.
//
// An unknown windowStart (zero — no window restriction was applied, i.e. a
// full fetch) also counts as new, preserving the pre-window semantics.
func isNewWithinWindow(createdAt, windowStart time.Time) bool {
	if createdAt.IsZero() || windowStart.IsZero() {
		return true
	}
	return !createdAt.Before(windowStart)
}

// deriveNewItemChange classifies an item seen for the first time since the
// baseline. A PR that is already merged at first sighting reports merged, not
// opened, so the more informative terminal state wins.
func deriveNewItemChange(cur ItemState) (Change, bool) {
	switch {
	case cur.Merged:
		return Change{Type: EventMerged, NewState: string(StateMerged)}, true
	case cur.State == string(StateClosed):
		return Change{Type: EventClosed, NewState: cur.State}, true
	case cur.State == string(StateOpen):
		return Change{Type: EventOpened, NewState: cur.State, Actor: cur.Author}, true
	default:
		return Change{}, false
	}
}

// deriveStateChange picks the most informative state transition, if any.
// merged takes precedence so a multi-step interval (closed→reopened→merged)
// collapses to the single event worth reporting.
//
// A merge is gated on windowStart for the same reason a new item is: the merge
// timestamp is the only evidence that the merge happened NOW rather than before
// the baseline existed. Without the gate, a provider that starts reporting merge
// state (or a snapshot table whose `merged` column was never populated) turns
// every historical merged PR into a fresh `merged` event at once — hundreds of
// events and notifications for merges nobody just performed.
func deriveStateChange(prev *Snapshot, cur ItemState, windowStart time.Time) (Change, bool) {
	if prev.State == cur.State && (prev.Merged || !cur.Merged) {
		return Change{}, false
	}
	switch {
	case cur.Merged && isNewWithinWindow(cur.MergedAt, windowStart):
		return Change{Type: EventMerged, PrevState: prev.State, NewState: string(StateMerged)}, true
	case cur.State == string(StateClosed) && prev.State != string(StateClosed):
		return Change{Type: EventClosed, PrevState: prev.State, NewState: cur.State}, true
	case cur.State == string(StateOpen) && prev.State == string(StateClosed):
		return Change{Type: EventReopened, PrevState: prev.State, NewState: cur.State}, true
	default:
		return Change{}, false
	}
}

// deriveCommentChange reports comment activity: a new comment id, or an edit
// (same id, newer updated_at).
func deriveCommentChange(prev *Snapshot, cur ItemState) bool {
	if cur.LatestCommentID > prev.LastCommentID {
		return true
	}
	return cur.LatestCommentID == prev.LastCommentID &&
		cur.LatestCommentID != 0 &&
		cur.LatestCommentUpdatedAt.After(prev.LastCommentUpdatedAt)
}

// DedupeKey builds a stable identity for an event so the same event is never
// dispatched twice. It combines the item identity, the event TYPE, and a
// revision discriminator rather than relying on timestamps, which can collide
// at second granularity.
//
// The event type must be part of the key: `opened` and `reopened` both end in
// state=open, so a state-only revision would make a later reopen collide with
// the original open and be dropped as a duplicate.
func DedupeKey(repo Remote, itemType ItemType, number int, ev Change) string {
	revision := ""
	switch ev.Type {
	case EventCommented:
		revision = fmt.Sprintf("comment:%d", ev.CommentID)
	case EventPipeline:
		// A CI event has no item number and its NewState is only success or
		// failure, so a state-based revision would make every successful run on
		// a repository collide with the previous one. The run id is the only
		// value that distinguishes two runs.
		revision = fmt.Sprintf("run:%d", ev.PipelineRunID)
	default:
		revision = fmt.Sprintf("state:%s", ev.NewState)
	}
	return fmt.Sprintf("%s|%s|%s|%s|%d|%s|%s|%s",
		repo.Platform, repo.Host, repo.Owner, repo.Repo, number, itemType, string(ev.Type), revision)
}

// ItemKeyForNumber identifies a numbered item (an issue or a change request).
//
// Use this where the item is known to be numbered, so no synthetic Change is
// needed and a pipeline can never be passed by accident.
func ItemKeyForNumber(itemType ItemType, number int) string {
	return fmt.Sprintf("%s/%d", itemType, number)
}

// pipelineKeyPrefix is the fixed part of a pipeline item key. Kept beside
// PipelineItemKey so the format and its inverse cannot drift apart.
const pipelineKeyPrefix = "pipeline/run:"

// PipelineItemKey identifies one CI run, which has no item number.
func PipelineItemKey(runID int64) string {
	return fmt.Sprintf("%s%d", pipelineKeyPrefix, runID)
}

// ParsePipelineRunID recovers the run id from a pipeline item key.
//
// Returns ok=false for any other key, so callers never mistake an issue/PR key
// for a pipeline. This exists because a pipeline event stores Number 0 for every
// run: the run id is only recoverable from the key, and a caller that re-derived
// it from (type, number) would produce a run id of 0.
func ParsePipelineRunID(itemKey string) (int64, bool) {
	rest, ok := strings.CutPrefix(itemKey, pipelineKeyPrefix)
	if !ok || rest == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// ItemKey identifies the thing an event is about, for grouping events into
// "items with new activity".
//
// It cannot be derived from (itemType, number) alone: a pipeline event carries
// Number 0 for every run, because a CI run is not an item and has no number. The
// run id is what distinguishes two runs, so it becomes the key.
func ItemKey(itemType ItemType, number int, ev Change) string {
	if itemType == ItemTypePipeline {
		return PipelineItemKey(ev.PipelineRunID)
	}
	return ItemKeyForNumber(itemType, number)
}

// StateRank orders terminal states so a multi-transition interval can pick the
// most informative one.
func StateRank(state State) int {
	switch state {
	case StateMerged:
		return 3
	case StateClosed:
		return 2
	case StateOpen:
		return 1
	default:
		return 0
	}
}
