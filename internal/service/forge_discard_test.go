package service

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/require"
)

// TestDiscardQueuedForgeEvents_DropsPendingWork pins the "no catch-up" rule of
// the tasks master switch: an event that arrived while a task was busy is
// discarded when tasks are switched off. Keeping it would make the queue fire as
// soon as the switch came back — exactly the backfill the design forbids.
func TestDiscardQueuedForgeEvents_DropsPendingWork(t *testing.T) {
	prev := GlobalForgeTaskTrigger
	t.Cleanup(func() { GlobalForgeTaskTrigger = prev })

	// With no trigger constructed (forge disabled at startup) this is a no-op
	// rather than a nil dereference.
	GlobalForgeTaskTrigger = nil
	DiscardQueuedForgeEvents()

	trigger := NewForgeTaskTrigger(nil, func() model.Config { return model.ConfigInstance }, nil)
	trigger.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{{ID: 7}}, nil
	})
	trigger.queues[7] = []ForgeQueuedEvent{{}, {}}
	trigger.queues[9] = []ForgeQueuedEvent{{}}
	GlobalForgeTaskTrigger = trigger

	DiscardQueuedForgeEvents()

	trigger.mu.Lock()
	defer trigger.mu.Unlock()
	require.Empty(t, trigger.queues, "every per-task queue must be emptied")
	require.Empty(t, trigger.running, "a drained task must not be marked running")
}

// TestDiscardQueued_NilTriggerIsSafe keeps the helper usable from the config
// patch path, which may run before a trigger exists.
func TestDiscardQueued_NilTriggerIsSafe(t *testing.T) {
	var trigger *ForgeTaskTrigger
	trigger.DiscardQueued()
}
