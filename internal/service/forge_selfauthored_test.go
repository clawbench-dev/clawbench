package service_test

import (
	"context"
	"testing"
	"time"

	"clawbench/internal/forge"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"github.com/stretchr/testify/assert"
)

// allowSelfConfig returns a config with the anti-recursion suppression turned
// OFF at the user's request (allow_self_authored_events).
func allowSelfConfig() model.Config {
	cfg := fullNotifyConfig()
	cfg.Forge.AllowSelfAuthoredEvents = true
	return cfg
}

// selfAuthoredOpened drives one `opened` event authored by the credential's own
// account ("me") and reports whether a task fired.
func selfAuthoredOpened(t *testing.T, cfg model.Config) int {
	t.Helper()
	bindProjectRepo(t, triggerRepo())
	fired := 0

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg },
		func(service.ForgeRepoRef) string { return "me" })
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "issue.opened")}, nil
	})
	tr.SetFireForTest(func(int64, service.ForgeQueuedEvent) bool { fired++; return true })

	item := triggerItem(1)
	item.Author = forge.Author{Login: "me"}
	tr.HandleChange(context.Background(), triggerRepo(), item,
		forge.Change{Type: forge.EventOpened, Number: 1, Actor: "me"})

	time.Sleep(50 * time.Millisecond)
	return fired
}

// TestForgeTaskTrigger_SelfAuthoredStillSuppressedByDefault is the guard on the
// guard: the new switch must default to OFF, so an install that never wrote the
// key keeps suppressing and cannot silently start looping.
func TestForgeTaskTrigger_SelfAuthoredStillSuppressedByDefault(t *testing.T) {
	cfg := fullNotifyConfig()
	assert.False(t, cfg.Forge.AllowSelfAuthoredEvents,
		"zero value must mean suppressed; the switch is opt-in")
	assert.Equal(t, 0, selfAuthoredOpened(t, cfg),
		"an issue created under our own account must not fire a task by default")
}

// TestForgeTaskTrigger_SelfAuthoredFiresWhenAllowed covers the opt-in: once the
// user accepts the loop risk, their own issue triggers the task like anyone
// else's.
func TestForgeTaskTrigger_SelfAuthoredFiresWhenAllowed(t *testing.T) {
	assert.Equal(t, 1, selfAuthoredOpened(t, allowSelfConfig()),
		"with the switch on, our own issue must dispatch the task")
}

// TestForgeTaskTrigger_AllowSwitchDoesNotBypassOtherGuards verifies the new flag
// only disables the self-authorship check: the global kill-switch and event
// matching still apply, so "allow self-authored" cannot fire tasks while tasks
// are paused.
func TestForgeTaskTrigger_AllowSwitchDoesNotBypassOtherGuards(t *testing.T) {
	cfg := allowSelfConfig()
	cfg.Forge.PauseEventTasks = true
	assert.Equal(t, 0, selfAuthoredOpened(t, cfg),
		"the kill-switch must still win over allow_self_authored_events")
}
