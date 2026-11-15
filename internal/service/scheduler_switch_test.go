package service

import (
	"sync"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/store"

	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Turning the tasks switch off must make manual triggers refuse outright, not
// silently start a run that the executor then skips — the caller needs to know.
func TestScheduler_TriggerTaskRefusedWhileDisabled(t *testing.T) {
	s := NewScheduler()
	t.Cleanup(s.Stop)

	s.SetEnabled(false)
	assert.True(t, s.TasksDisabled())

	err := s.TriggerTask(42)
	require.ErrorIs(t, err, ErrTasksDisabled)
}

// SetEnabled(false) cancels running executions and must NOT mark them
// restart_interrupted: that flag tells the NEXT process to resume them, which
// is exactly the catch-up the design forbids.
func TestScheduler_SetEnabledFalseCancelsWithoutRestartResume(t *testing.T) {
	setupSchedulerExecDB(t)

	// A row the shutdown path would mark, to prove this path does not.
	_, err := store.WriteExec(
		`INSERT INTO task_executions (task_id, session_id, trigger_type, status) VALUES (1, 'sess-switch', 'manual', 'running')`)
	require.NoError(t, err)

	s := NewScheduler()
	t.Cleanup(s.Stop)

	cancelled := make(chan struct{})
	var once sync.Once
	s.runningExecutions.Store("exec-switch", &RunningExecution{
		ID:         "sess-switch",
		TaskID:     1,
		CancelFunc: func() { once.Do(func() { close(cancelled) }) },
		Phase:      RunningPhaseAI,
	})

	s.SetEnabled(false)

	select {
	case <-cancelled:
	default:
		t.Fatal("SetEnabled(false) must cancel running executions")
	}

	var marked int
	require.NoError(t, store.ReadDB().QueryRow(
		`SELECT restart_interrupted FROM task_executions WHERE session_id = 'sess-switch'`,
	).Scan(&marked))
	assert.Zero(t, marked, "a run cancelled by the switch must not be resumed on the next start")
}

// The executor itself refuses to start a run that was already queued when the
// switch went off, and releases the per-task running flag so the task is usable
// again once the switch comes back.
func TestScheduler_ExecuteTaskSkippedWhileDisabled(t *testing.T) {
	setupSchedulerExecDB(t)

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"switch-agent": {ID: "switch-agent", Name: "Switch Agent", Backend: "codebuddy"},
	}
	t.Cleanup(func() { model.Agents = origAgents })

	s := NewScheduler()
	t.Cleanup(s.Stop)
	s.SetEnabled(false)

	task := &model.ScheduledTask{
		ID:          1,
		ProjectPath: "/tmp",
		Name:        "Skipped",
		CronExpr:    "0 * * * *",
		AgentID:     "switch-agent",
		Prompt:      "should not run",
		Status:      "active",
		RepeatMode:  "unlimited",
	}

	// Simulate the trigger having claimed the per-task flag before the switch
	// went off.
	s.taskRunning.Store(int64(1), struct{}{})
	s.executeTask(task, "/tmp", "manual", nil, nil)

	var executions int
	require.NoError(t, store.ReadDB().QueryRow(`SELECT COUNT(*) FROM task_executions`).Scan(&executions))
	assert.Zero(t, executions, "no execution row may be written while tasks are disabled")

	_, stillRunning := s.taskRunning.Load(int64(1))
	assert.False(t, stillRunning, "the per-task running flag must be released")
}

// The configured zone must actually reach the cron parser: the same expression
// fires at 09:00 in Asia/Shanghai, not 09:00 server time. A task that carries
// its own CRON_TZ= prefix keeps it.
func TestScheduler_TimezoneAppliesToCronExpressions(t *testing.T) {
	setupSchedulerExecDB(t)

	s := NewScheduler()
	t.Cleanup(s.Stop)
	require.NoError(t, s.SetTimezone("Asia/Shanghai"))

	spec := s.cronSpec("0 9 * * *")
	assert.Equal(t, "CRON_TZ=Asia/Shanghai 0 9 * * *", spec)

	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	sch, err := cron.ParseStandard(spec)
	require.NoError(t, err)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	next := sch.Next(base)
	assert.Equal(t, 9, next.In(loc).Hour(), "09:00 must be Shanghai time")

	assert.Equal(t, "CRON_TZ=UTC 0 9 * * *", s.cronSpec("CRON_TZ=UTC 0 9 * * *"),
		"an explicit per-task zone must win over the global setting")

	require.Error(t, s.SetTimezone("Not/AZone"), "an unknown zone must be rejected")
	assert.Equal(t, "Asia/Shanghai", s.timezone, "a rejected value must not change the current zone")
}
