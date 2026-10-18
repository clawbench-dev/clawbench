package service

import (
	"log/slog"

	"clawbench/internal/store"
)

// MigrateLegacyThinkingEffortPicks marks already-persisted effort values as
// user picks when they provably came from the user.
//
// Before issue #552 the persisted `context_state.thinkingEffort` was written by
// TWO sources, and the fix now distinguishes them with `userPicked` — only a
// user pick is inherited on later turns. Without this migration every existing
// session would suddenly be treated as "no choice" and fall back to the agent's
// own default ("low" for most OpenCode models), i.e. the reported bug would
// come back for all pre-upgrade sessions at once.
//
// The two legacy writers are distinguishable by SHAPE, not by value:
//
//   - the agent-event path persisted the whole reported state, so
//     `availableLevels` is present;
//   - the user-pick path (PATCH /api/ai/session/update, only ever called from
//     the picker click) persisted a bare `{currentId}`.
//
// So a legacy entry WITHOUT `availableLevels` can only have come from the user.
// An entry WITH levels carries the agent's own value (the user's pick, if any,
// was overwritten by the agent's next report), and leaving it unpicked is the
// conservative and correct choice: inheriting the agent's default is exactly
// the bug.
//
// Implemented as one atomic UPDATE using json_set, so every other key in
// context_state (mode / usage / cruise) is preserved byte-for-byte. Rows that
// already carry the flag are excluded, so re-running is a no-op.
//
// json_valid() guards the whole statement: json_extract on malformed JSON
// raises "malformed JSON" instead of returning NULL, which would make the COUNT
// fail, the migration report false, and runOnce retry forever WITHOUT marking a
// single row — so one corrupt row would leave every pre-upgrade session on the
// agent's default (the reported bug, globally).
func MigrateLegacyThinkingEffortPicks() bool {
	const cond = `
		json_valid(context_state)
		  AND json_extract(context_state, '$.thinkingEffort.currentId') IS NOT NULL
		  AND json_extract(context_state, '$.thinkingEffort.currentId') != ''
		  AND json_extract(context_state, '$.thinkingEffort.availableLevels') IS NULL
		  AND json_extract(context_state, '$.thinkingEffort.userPicked') IS NULL`

	var needed int
	if err := store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM chat_sessions WHERE " + cond,
	).Scan(&needed); err != nil {
		slog.Error("thinking effort pick migration: count failed", "err", err)
		return false
	}
	if needed == 0 {
		return true
	}

	res, err := store.WriteExec(`
		UPDATE chat_sessions
		SET context_state = json_set(context_state, '$.thinkingEffort.userPicked', json('true'))
		WHERE ` + cond)
	if err != nil {
		slog.Error("thinking effort pick migration: update failed", "err", err)
		return false
	}
	affected, _ := res.RowsAffected()
	slog.Info("marked legacy thinking effort picks as user picks",
		"candidates", needed, "updated", affected)
	return true
}
