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
}

// DeriveChanges compares a stored snapshot (nil on first sighting) with the
// freshly fetched item state and returns the events to dispatch. The item
// number is stamped onto each returned change.
//
// The derivation is deliberately conservative:
//   - A first sighting (nil snapshot) emits nothing: binding a repo must not
//     replay its entire history as notifications.
//   - When several transitions occur within one polling interval, only the most
//     informative terminal state is emitted (merged > reopened > closed), so a
//     closed→reopened→merged sequence yields one "merged" rather than noise.
//   - A comment is detected when either its id increases OR an existing
//     comment's updated_at moves forward (an edit keeps the id).
func DeriveChanges(prev *Snapshot, cur ItemState, number int) []Change {
	if prev == nil {
		// First sighting: establish the baseline without emitting.
		return nil
	}

	var changes []Change
	if c, ok := deriveStateChange(prev, cur); ok {
		changes = append(changes, c)
	}
	if deriveCommentChange(prev, cur) {
		changes = append(changes, Change{
			Type: EventCommented, CommentID: cur.LatestCommentID,
			PrevState: prev.State, NewState: cur.State,
		})
	}

	for i := range changes {
		changes[i].Number = number
	}
	return changes
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
// dispatched twice. It combines the item identity, the event type, and a
// revision discriminator (state or comment id) rather than relying on
// timestamps, which can collide at second granularity.
func DedupeKey(repo Remote, itemType ItemType, number int, ev Change) string {
	revision := string(ev.Type)
	switch ev.Type {
	case EventCommented:
		revision = fmt.Sprintf("comment:%d", ev.CommentID)
	case EventOpened, EventClosed, EventMerged, EventReopened:
		revision = fmt.Sprintf("state:%s", ev.NewState)
	}
	return fmt.Sprintf("%s|%s|%s|%s|%d|%s|%s",
		repo.Platform, repo.Host, repo.Owner, repo.Repo, number, itemType, revision)
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
