package service_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/service"
	"clawbench/internal/store"
)

// setupEffortMigrationDB installs the full chat_sessions fixture (the migration
// reads context_state, and the assertions read it back through GetContextState).
func setupEffortMigrationDB(t *testing.T) {
	t.Helper()
	setupDB(t)
}

// TestMigrateLegacyThinkingEffortPicks pins the upgrade path (issue #552).
//
// Legacy rows came from two writers with DIFFERENT shapes:
//   - the picker (PATCH) wrote a bare {currentId},
//   - the agent-event path wrote {currentId, availableLevels}.
//
// Only the first is provably a user choice. Without the migration, every
// pre-upgrade session would be treated as "no choice" and fall back to the
// agent's own default — the reported bug, for every existing session at once.
func TestMigrateLegacyThinkingEffortPicks(t *testing.T) {
	setupEffortMigrationDB(t)

	// A: picker-written (no availableLevels) -> must be marked.
	_, err := store.WriteExec(`INSERT INTO chat_sessions (id, project_id, backend, title, context_state) VALUES (?, 1, 'claude', 'T', ?)`,
		"pick", `{"thinkingEffort":{"currentId":"max"}}`)
	require.NoError(t, err)
	// B: agent-written (has availableLevels) -> must stay unpicked.
	_, err = store.WriteExec(`INSERT INTO chat_sessions (id, project_id, backend, title, context_state) VALUES (?, 1, 'claude', 'T', ?)`,
		"agent", `{"thinkingEffort":{"currentId":"low","availableLevels":[{"id":"low"},{"id":"max"}]}}`)
	require.NoError(t, err)
	// C: no effort at all -> untouched.
	_, err = store.WriteExec(`INSERT INTO chat_sessions (id, project_id, backend, title, context_state) VALUES (?, 1, 'claude', 'T', ?)`,
		"none", `{"mode":{"currentModeId":"plan"}}`)
	require.NoError(t, err)
	// D: already flagged -> untouched (idempotence).
	_, err = store.WriteExec(`INSERT INTO chat_sessions (id, project_id, backend, title, context_state) VALUES (?, 1, 'claude', 'T', ?)`,
		"already", `{"thinkingEffort":{"currentId":"high","userPicked":true}}`)
	require.NoError(t, err)

	assert.True(t, service.MigrateLegacyThinkingEffortPicks())

	effort, _, ok := service.UserThinkingEffortPick("pick")
	assert.True(t, ok, "a picker-written value must become a user pick")
	assert.Equal(t, "max", effort)

	_, _, ok = service.UserThinkingEffortPick("agent")
	assert.False(t, ok, "an agent-written value must NOT become a user pick")

	state := service.GetContextState("none")
	require.NotNil(t, state)
	assert.Nil(t, state.ThinkingEffort, "a row without effort must be untouched")

	// The unrelated keys survive the rewrite.
	state = service.GetContextState("pick")
	require.NotNil(t, state)

	// Idempotent: a second run finds nothing to do.
	assert.True(t, service.MigrateLegacyThinkingEffortPicks())
}

// TestMigrateLegacyThinkingEffortPicks_PreservesOtherKeys pins that the rewrite
// only touches the effort flag: mode / usage / cruise must survive byte-for-byte,
// or an upgrade would wipe unrelated session state.
func TestMigrateLegacyThinkingEffortPicks_PreservesOtherKeys(t *testing.T) {
	setupEffortMigrationDB(t)

	_, err := store.WriteExec(`INSERT INTO chat_sessions (id, project_id, backend, title, context_state) VALUES (?, 1, 'claude', 'T', ?)`,
		"mixed", `{"mode":{"currentModeId":"plan"},"thinkingEffort":{"currentId":"max"},"cruise":true,"usage":{"used":1234,"size":200000}}`)
	require.NoError(t, err)

	assert.True(t, service.MigrateLegacyThinkingEffortPicks())

	state := service.GetContextState("mixed")
	require.NotNil(t, state)
	require.NotNil(t, state.Mode)
	assert.Equal(t, "plan", state.Mode.CurrentModeID)
	require.NotNil(t, state.Usage)
	assert.Equal(t, 1234, state.Usage.Used)
	assert.Equal(t, 200000, state.Usage.Size)
	assert.True(t, service.GetSessionCruise("mixed"))
	require.NotNil(t, state.ThinkingEffort)
	assert.Equal(t, "max", state.ThinkingEffort.CurrentID)
	assert.True(t, state.ThinkingEffort.UserPicked)
}

// TestMigrateLegacyThinkingEffortPicks_MalformedRowDoesNotBlock pins the
// json_valid() guard.
//
// json_extract on malformed JSON RAISES ("malformed JSON") instead of returning
// NULL, so without the guard the COUNT fails, the migration reports false, and
// runOnce retries forever — while marking ZERO rows. One corrupt row would then
// leave every pre-upgrade session falling back to the agent's "low" default:
// the reported bug, applied globally.
func TestMigrateLegacyThinkingEffortPicks_MalformedRowDoesNotBlock(t *testing.T) {
	setupEffortMigrationDB(t)

	_, err := store.WriteExec(`INSERT INTO chat_sessions (id, project_id, backend, title, context_state) VALUES (?, 1, 'claude', 'T', ?)`,
		"broken", `{"thinkingEffort": oops}`)
	require.NoError(t, err)
	_, err = store.WriteExec(`INSERT INTO chat_sessions (id, project_id, backend, title, context_state) VALUES (?, 1, 'claude', 'T', ?)`,
		"valid-pick", `{"thinkingEffort":{"currentId":"max"}}`)
	require.NoError(t, err)

	require.True(t, service.MigrateLegacyThinkingEffortPicks(),
		"a malformed row must not abort the whole migration")

	effort, _, ok := service.UserThinkingEffortPick("valid-pick")
	assert.True(t, ok, "the valid row must still be marked")
	assert.Equal(t, "max", effort)
}

// TestMigrateLegacyThinkingEffortPicks_AgentReportedEmptyLevelsStaysUnpicked
// pins that a legacy agent-written row with NO availableLevels is still treated
// as agent-reported when it cannot have come from the picker.
//
// The picker always writes the value the user clicked, so it is never empty;
// an empty currentId is only reachable from an agent state that reported no
// level. Marking it would store a meaningless pick.
func TestMigrateLegacyThinkingEffortPicks_AgentReportedEmptyLevelsStaysUnpicked(t *testing.T) {
	setupEffortMigrationDB(t)

	_, err := store.WriteExec(`INSERT INTO chat_sessions (id, project_id, backend, title, context_state) VALUES (?, 1, 'claude', 'T', ?)`,
		"empty-id", `{"thinkingEffort":{"currentId":""}}`)
	require.NoError(t, err)

	require.True(t, service.MigrateLegacyThinkingEffortPicks())
	_, _, ok := service.UserThinkingEffortPick("empty-id")
	assert.False(t, ok, "an empty legacy currentId must not become a user pick")
}
