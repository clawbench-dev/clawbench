package service

import (
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Retention purges old finished executions but never a running row, and never
// one inside the window.
func TestDeleteExpiredTaskExecutions(t *testing.T) {
	setupSchedulerExecDB(t)

	insert := func(session, status, age string) {
		t.Helper()
		_, err := store.WriteExec(
			`INSERT INTO task_executions (task_id, session_id, trigger_type, status, created_at)
			 VALUES (1, ?, 'auto', ?, datetime('now', ?))`,
			session, status, age,
		)
		require.NoError(t, err)
	}
	insert("old-done", "completed", "-40 days")
	insert("old-failed", "failed", "-31 days")
	insert("old-running", "running", "-40 days")
	insert("fresh", "completed", "-2 days")

	removed, err := DeleteExpiredTaskExecutions(30)
	require.NoError(t, err)
	assert.EqualValues(t, 2, removed)

	var remaining int
	require.NoError(t, store.ReadDB().QueryRow(`SELECT COUNT(*) FROM task_executions`).Scan(&remaining))
	assert.Equal(t, 2, remaining, "the running row and the fresh row must survive")

	var running int
	require.NoError(t, store.ReadDB().QueryRow(
		`SELECT COUNT(*) FROM task_executions WHERE session_id = 'old-running'`).Scan(&running))
	assert.Equal(t, 1, running, "a running execution must never be purged")
}

// The sweep is off by default and only runs when the rule says so.
func TestMaybeCleanupTaskExecutions_DisabledDoesNothing(t *testing.T) {
	setupSchedulerExecDB(t)

	_, err := store.WriteExec(
		`INSERT INTO task_executions (task_id, session_id, trigger_type, status, created_at)
		 VALUES (1, 'old', 'auto', 'completed', datetime('now', '-100 days'))`)
	require.NoError(t, err)

	orig := model.ConfigInstance
	t.Cleanup(func() { model.ConfigInstance = orig })
	model.ConfigInstance.Tasks.RetentionEnabled = false

	removed, err := MaybeCleanupTaskExecutions()
	require.NoError(t, err)
	assert.Zero(t, removed)

	var count int
	require.NoError(t, store.ReadDB().QueryRow(`SELECT COUNT(*) FROM task_executions`).Scan(&count))
	assert.Equal(t, 1, count)
}

// Notification toggles gate the notification-center row (and, through the same
// flag, the push/IM paths). The WS broadcast and the execution history are not
// touched.
func TestEmitTaskEvent_NotificationToggles(t *testing.T) {
	setupSchedulerExecDB(t)
	_, err := store.WriteExec(NotificationsDDL)
	require.NoError(t, err)

	origPolicy := model.FeaturePolicy
	t.Cleanup(func() { model.FeaturePolicy = origPolicy })

	countNotifications := func() int {
		var n int
		require.NoError(t, store.ReadDB().QueryRow(`SELECT COUNT(*) FROM notifications`).Scan(&n))
		return n
	}

	model.FeaturePolicy.TasksNotifyOnSuccess = false
	emitTaskEvent("1", statusCompleted, "", "", "", "Muted Task")
	assert.Zero(t, countNotifications(), "a muted success must not create a notification")

	model.FeaturePolicy.TasksNotifyOnSuccess = true
	emitTaskEvent("1", statusCompleted, "", "", "", "Loud Task")
	assert.Equal(t, 1, countNotifications(), "an unmuted success must create one")

	model.FeaturePolicy.TasksNotifyOnFailure = false
	emitTaskEvent("1", statusFailed, "", "", "", "Muted Failure")
	assert.Equal(t, 1, countNotifications(), "a muted failure must not create a notification")
}
