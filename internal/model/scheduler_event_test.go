package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSplitEventTypes covers the comma-separated parser used by event-triggered
// tasks: empty entries are dropped so a hand-edited "a,, b" still yields a
// clean subscription list.
func TestSplitEventTypes(t *testing.T) {
	assert.Nil(t, SplitEventTypes(""), "an empty subscription is nil, not [\"\"]")
	assert.Equal(t, []string{"opened"}, SplitEventTypes("opened"))
	assert.Equal(t, []string{"opened", "closed"}, SplitEventTypes("opened,closed"))
	assert.Equal(t, []string{"a", "b"}, SplitEventTypes("a,, b"), "blank entries must be dropped")
	assert.Equal(t, []string{"a", "b"}, SplitEventTypes(" a , b "))
	assert.Equal(t, []string{"commented"}, SplitEventTypes(",,commented,,"))
}

// TestScheduledTask_IsEventTriggered pins the trigger-mode discriminator: only
// the exact "event" value counts, so a legacy empty mode stays cron.
func TestScheduledTask_IsEventTriggered(t *testing.T) {
	assert.True(t, (&ScheduledTask{TriggerMode: "event"}).IsEventTriggered())
	assert.False(t, (&ScheduledTask{TriggerMode: "cron"}).IsEventTriggered())
	assert.False(t, (&ScheduledTask{}).IsEventTriggered(), "an unset mode must default to cron")
	assert.False(t, (&ScheduledTask{TriggerMode: "EVENT"}).IsEventTriggered(),
		"the value is matched exactly; the writer stores lowercase")
}

// TestScheduledTask_EventTypeList delegates to the shared parser.
func TestScheduledTask_EventTypeList(t *testing.T) {
	task := &ScheduledTask{EventTypes: "opened, merged"}
	assert.Equal(t, []string{"opened", "merged"}, task.EventTypeList())
	assert.Nil(t, (&ScheduledTask{}).EventTypeList())
}
