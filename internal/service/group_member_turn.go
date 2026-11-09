package service

import (
	"context"
	"log/slog"
	"sync"
)

// group_member_turn.go owns the per-MEMBER turn handle inside a group turn.
//
// Why this exists: a group turn registers ONE cancel under the GROUP id (see
// RunGroupTurnDrain), so the stop button can only stop the whole round. Each
// member turn runs with SessionID = its own member row, but
// RegisterSessionTurnCancel is keyed on the session RUNNER, and a member row has
// none (only the group id does) — so that registration was silently dropped.
//
// This registry is the missing address: "stop THIS member's current speech".
// The handle lives only for the duration of that member's turn, so a late click
// (after the speech ended) cannot kill a later one — the entry is already gone.

// groupMemberTurnHandle is one member speech's registration.
type groupMemberTurnHandle struct {
	// id makes the handle addressable: an unregister must not clear a NEWER
	// speech of the same member (only one speech per member runs at a time, but
	// the teardown of the previous one can land after the next one started).
	id     uint64
	cancel context.CancelFunc
	// groupID is checked on interrupt so a caller cannot stop a member of a
	// DIFFERENT group that happens to reuse the same member row id.
	groupID string
}

var (
	groupMemberTurnMu sync.Mutex
	// groupMemberTurns is keyed by MEMBER ROW id (a member row belongs to exactly
	// one group, so the id alone is unique).
	groupMemberTurns = map[string]*groupMemberTurnHandle{}
)

// RegisterGroupMemberTurnCancel records the cancel func for one member turn and
// returns its id. Overwrites any previous entry for the same member (a member
// speaks at most once at a time).
func RegisterGroupMemberTurnCancel(groupID, memberID string, cancel context.CancelFunc) uint64 {
	if memberID == "" || cancel == nil {
		return 0
	}
	id := turnSeq.Add(1)
	groupMemberTurnMu.Lock()
	groupMemberTurns[memberID] = &groupMemberTurnHandle{id: id, cancel: cancel, groupID: groupID}
	groupMemberTurnMu.Unlock()
	return id
}

// UnregisterGroupMemberTurnCancel clears the member's handle, but only when it is
// still the SAME turn (id check). Without the check, a turn that ended while a
// newer speech of the same member had already started would wipe the newer
// handle and leave it un-stoppable.
func UnregisterGroupMemberTurnCancel(memberID string, id uint64) {
	groupMemberTurnMu.Lock()
	if h, ok := groupMemberTurns[memberID]; ok && h.id == id {
		delete(groupMemberTurns, memberID)
	}
	groupMemberTurnMu.Unlock()
}

// InterruptGroupMemberTurn stops ONE member's current speech: the round and the
// other members keep going.
//
// Returns false when that member is not speaking right now (already finished, or
// never started) — the caller then reports "nothing stopped" instead of
// pretending.
func InterruptGroupMemberTurn(groupID, memberID string) bool {
	groupMemberTurnMu.Lock()
	h, ok := groupMemberTurns[memberID]
	if !ok || h.groupID != groupID {
		groupMemberTurnMu.Unlock()
		return false
	}
	// Read-and-clear under the lock: only the caller that removes the handle
	// wins, so two concurrent clicks cannot both report success.
	//
	// The reason is recorded HERE, while the handle still exists and the lock is
	// held — NOT after the unlock. The turn's teardown (executeTurn) clears this
	// slot right after the runner returns; storing it after releasing the lock
	// left a window where that clear ran first and the reason then sat in the
	// slot until the member's NEXT speech, which would be skipped for no reason.
	// Only claim an empty slot: a user cancel that already owns the outcome must
	// not be rewritten into "member stopped".
	//
	// The trade-off: a click landing in the last few microseconds of a speech
	// (terminal event already processed, executor already read the slot) can now
	// stamp a COMPLETE reply as stopped. That mislabels one turn's badge and
	// skips one cursor advance — far cheaper than poisoning the next speech, and
	// it cannot leak past this turn (the teardown still clears the slot).
	if _, loaded := sessionCancelReasons.LoadOrStore(memberID, cancelReasonMemberStop); loaded {
		slog.Info("group: member turn already had a cancel reason; keeping it",
			"member", memberID)
	}
	delete(groupMemberTurns, memberID)
	groupMemberTurnMu.Unlock()

	h.cancel()
	return true
}
