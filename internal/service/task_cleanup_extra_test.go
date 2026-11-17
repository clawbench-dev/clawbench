package service

import (
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"

	"github.com/stretchr/testify/require"
)

// TestMaybeCleanupTaskExecutions_ThrottlesAndRearms pins the two behaviors the
// 60-second cleanup tick depends on:
//
//   - a sweep runs at most once per taskCleanupInterval, so the tick does not
//     delete rows every minute;
//   - turning retention OFF resets the timer, so turning it back on sweeps
//     immediately instead of waiting out a stale interval.
//
// Getting the second part wrong looks like "the setting does nothing" for up to
// six hours after the user re-enables it.
func TestMaybeCleanupTaskExecutions_ThrottlesAndRearms(t *testing.T) {
	setupSchedulerExecDB(t)
	orig := model.ConfigInstance
	t.Cleanup(func() {
		model.ConfigInstance = orig
		lastTaskCleanup.Store(0)
	})

	insertOld := func(session string) {
		t.Helper()
		_, err := store.WriteExec(
			`INSERT INTO task_executions (task_id, session_id, trigger_type, status, created_at)
			 VALUES (1, ?, 'auto', 'completed', datetime('now', '-100 days'))`, session,
		)
		require.NoError(t, err)
	}
	count := func() int {
		t.Helper()
		var n int
		require.NoError(t, store.ReadDB().QueryRow(`SELECT COUNT(*) FROM task_executions`).Scan(&n))
		return n
	}

	model.ConfigInstance.Tasks.RetentionEnabled = true
	model.ConfigInstance.Tasks.RetentionDays = 30
	insertOld("first")
	insertOld("second")

	lastTaskCleanup.Store(0)
	removed, err := MaybeCleanupTaskExecutions()
	require.NoError(t, err)
	require.EqualValues(t, 2, removed, "the first call with an armed timer sweeps")
	require.Zero(t, count())

	// A second call inside the interval does nothing, even though it is due by
	// configuration: the throttle is what keeps this off every tick.
	insertOld("third")
	removed, err = MaybeCleanupTaskExecutions()
	require.NoError(t, err)
	require.Zero(t, removed, "the sweep must be throttled")
	require.Equal(t, 1, count())

	// Switching retention off resets the timer...
	model.ConfigInstance.Tasks.RetentionEnabled = false
	removed, err = MaybeCleanupTaskExecutions()
	require.NoError(t, err)
	require.Zero(t, removed)
	require.EqualValues(t, 0, lastTaskCleanup.Load(), "disabling must rearm, not leave a stale claim")

	// ...so re-enabling sweeps immediately.
	model.ConfigInstance.Tasks.RetentionEnabled = true
	removed, err = MaybeCleanupTaskExecutions()
	require.NoError(t, err)
	require.EqualValues(t, 1, removed, "re-enabled retention must sweep on the next tick")
	require.Zero(t, count())

	// A non-positive window is treated as "off" too: it would delete nothing or
	// everything depending on SQLite's parsing, neither of which the user asked
	// for.
	lastTaskCleanup.Store(0)
	model.ConfigInstance.Tasks.RetentionDays = 0
	removed, err = MaybeCleanupTaskExecutions()
	require.NoError(t, err)
	require.Zero(t, removed)
	require.EqualValues(t, 0, lastTaskCleanup.Load())
}

// TestMaybeCleanupTaskExecutions_ReleasesClaimOnFailure: a failing sweep must
// release the claim, otherwise a transient DB error silences retention for six
// hours.
func TestMaybeCleanupTaskExecutions_ReleasesClaimOnFailure(t *testing.T) {
	setupSchedulerExecDB(t)
	orig := model.ConfigInstance
	t.Cleanup(func() {
		model.ConfigInstance = orig
		lastTaskCleanup.Store(0)
	})

	// Drop the table so the DELETE fails with a real SQL error.
	_, err := store.WriteExec(`DROP TABLE task_executions`)
	require.NoError(t, err)

	model.ConfigInstance.Tasks.RetentionEnabled = true
	model.ConfigInstance.Tasks.RetentionDays = 7
	lastTaskCleanup.Store(0)

	_, err = MaybeCleanupTaskExecutions()
	require.Error(t, err)
	require.EqualValues(t, 0, lastTaskCleanup.Load(),
		"a failed sweep must be retried on the next tick, not six hours later")

	// A window of zero or less is a no-op rather than a SQL error.
	removed, err := DeleteExpiredTaskExecutions(0)
	require.NoError(t, err)
	require.Zero(t, removed)
}

// TestDeleteExpiredTaskExecutions_UsesTheConfiguredWindow pins that the window
// is a parameter, not a constant: the setting says how many days of history the
// user keeps, and one day off is one day of history lost.
func TestDeleteExpiredTaskExecutions_UsesTheConfiguredWindow(t *testing.T) {
	setupSchedulerExecDB(t)
	insert := func(session, age string) {
		t.Helper()
		_, err := store.WriteExec(
			`INSERT INTO task_executions (task_id, session_id, trigger_type, status, created_at)
			 VALUES (1, ?, 'auto', 'completed', datetime('now', ?))`, session, age,
		)
		require.NoError(t, err)
	}
	insert("eight-days", "-8 days")
	insert("two-days", "-2 days")

	removed, err := DeleteExpiredTaskExecutions(7)
	require.NoError(t, err)
	require.EqualValues(t, 1, removed)

	var remaining string
	require.NoError(t, store.ReadDB().QueryRow(`SELECT session_id FROM task_executions`).Scan(&remaining))
	require.Equal(t, "two-days", remaining)
}
