package forge

import (
	"fmt"
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
	// PipelineStatus / PipelineURL describe a finished CI run (pipeline_done).
	PipelineStatus string
	PipelineURL    string
	// PipelineRunID is the platform's run identity for a pipeline event. It is
	// the revision discriminator in DedupeKey: without it two successful runs on
	// the same repository would collide on "state:success" and the second would
	// be dropped as a duplicate.
	PipelineRunID int64
}

// DeriveChanges compares a stored snapshot with the freshly fetched item state
// and returns the events to dispatch. The item number is stamped onto each
// returned change.
//
// A nil prev means the item was not in the snapshot. The caller is responsible
// for not calling this on a repo's first-ever sync (which would replay the whole
// history); on any later sync a nil prev is a genuinely new item.
//
// The derivation is deliberately conservative:
//   - When several transitions occur within one polling interval, only the most
//     informative terminal state is emitted (merged > closed > reopened), so a
//     closed→reopened→merged sequence yields one "merged" rather than noise.
//   - A comment is detected when either its id increases OR an existing
//     comment's updated_at moves forward (an edit keeps the id).
func DeriveChanges(prev *Snapshot, cur ItemState, number int) []Change {
	if prev == nil {
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
	if c, ok := deriveStateChange(prev, cur); ok {
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
func deriveStateChange(prev *Snapshot, cur ItemState) (Change, bool) {
	if prev.State == cur.State && (prev.Merged || !cur.Merged) {
		return Change{}, false
	}
	switch {
	case cur.Merged:
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
