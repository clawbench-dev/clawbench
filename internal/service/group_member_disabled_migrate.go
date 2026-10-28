package service

import (
	"log/slog"

	"clawbench/internal/store"
)

// MigrateGroupMemberDisabled converts the legacy "removed member" representation
// into the current "禁用" (disabled) one.
//
// Before disabled existed, removing a member from a group archived its hidden
// member row (chat_sessions.archived = 1). That representation made the member
// unrecoverable: it was filtered out of the roster's name map and the @ menu, no
// entry point could clear the flag, and re-adding the same agent created a NEW
// instance with a new context window (issue #597). Disabling is the same
// scheduling exclusion expressed as a reversible flag, so every archived member
// row is translated here.
//
// Only session_type='group_member' rows are touched. An archived CHAT or GROUP
// row keeps its meaning (the user archived that conversation) and must not be
// resurrected.
//
// Idempotent and ledger-gated: after it runs no group_member row is left with
// archived = 1, so a re-run (or a database that never had any) is a no-op.
func MigrateGroupMemberDisabled() bool {
	res, err := store.WriteExec(
		`UPDATE chat_sessions SET archived = 0, disabled = 1
		 WHERE session_type = ? AND archived = 1`,
		store.SessionTypeGroupMember,
	)
	if err != nil {
		slog.Error("group member disabled migration failed", "err", err)
		return false
	}
	if n, nErr := res.RowsAffected(); nErr == nil && n > 0 {
		slog.Info("converted archived group members to disabled", "rows", n)
	}
	return true
}
