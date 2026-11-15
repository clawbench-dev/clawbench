package service

import (
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/store"
)

// taskCleanupInterval bounds how often the retention sweep may run. The main
// 60-second cleanup tick calls MaybeCleanupTaskExecutions; the sweep itself only
// needs to happen a few times a day.
const taskCleanupInterval = 6 * time.Hour

// lastTaskCleanup is the unix second of the last sweep. Zero means "never run
// in this process", so enabling retention takes effect on the next tick.
var lastTaskCleanup atomic.Int64

// MaybeCleanupTaskExecutions purges task execution rows older than the
// configured retention window, at most once per taskCleanupInterval.
//
// It reads the config on every call (rather than capturing it at startup) so a
// PATCH takes effect without a restart. Disabled retention also resets the
// timer, so turning it back on later does not wait out a stale interval.
func MaybeCleanupTaskExecutions() (int64, error) {
	cfg := model.ConfigInstance.Tasks
	if !cfg.RetentionEnabled || cfg.RetentionDays <= 0 {
		lastTaskCleanup.Store(0)
		return 0, nil
	}

	now := time.Now()
	last := lastTaskCleanup.Load()
	if last != 0 && now.Sub(time.Unix(last, 0)) < taskCleanupInterval {
		return 0, nil
	}
	// Claim the slot before doing the work so two ticks cannot both sweep.
	if !lastTaskCleanup.CompareAndSwap(last, now.Unix()) {
		return 0, nil
	}

	removed, err := DeleteExpiredTaskExecutions(cfg.RetentionDays)
	if err != nil {
		// Release the claim so the next tick retries instead of waiting six
		// hours after a transient DB error.
		lastTaskCleanup.Store(0)
		return 0, err
	}
	if removed > 0 {
		slog.Info("task execution retention: purged old executions",
			slog.Int64("removed", removed), slog.Int("days", cfg.RetentionDays))
	}
	return removed, nil
}

// DeleteExpiredTaskExecutions removes execution rows older than days, excluding
// rows still marked running (a long run must not lose its history row while it
// is writing to it).
func DeleteExpiredTaskExecutions(days int) (int64, error) {
	if days <= 0 {
		return 0, nil
	}
	res, err := store.WriteExec(
		`DELETE FROM task_executions
		  WHERE status != 'running'
		    AND created_at < datetime('now', ?)`,
		fmt.Sprintf("-%d days", days),
	)
	if err != nil {
		return 0, fmt.Errorf("purge task executions: %w", err)
	}
	removed, _ := res.RowsAffected()
	return removed, nil
}
