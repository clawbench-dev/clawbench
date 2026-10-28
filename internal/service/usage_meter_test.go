package service_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setUsageMeterRule installs a rule and restores the previous one afterwards.
// Every test in this file mutates the process-wide ConfigInstance, so the
// restore is not optional — a leaked rule makes later tests in the same package
// emit notifications.
//
// A configured rule is switched ON unless the test says otherwise: that is what
// ApplyDefaults does for a config.yaml written before the enable flag existed
// (a missing key means "inherit from the rule", not "off"). Tests that want the
// off state either pass a rule with nothing configured or set Enabled
// explicitly through setUsageMeterRuleRaw.
func setUsageMeterRule(t *testing.T, rule model.UsageMeterConfig) {
	t.Helper()
	if model.UsageMeterConfigured(rule) {
		rule.Enabled = true
	}
	setUsageMeterRuleRaw(t, rule)
}

// setUsageMeterRuleRaw installs a rule verbatim, including Enabled.
func setUsageMeterRuleRaw(t *testing.T, rule model.UsageMeterConfig) {
	t.Helper()
	prev := model.ConfigInstance.UsageMeter
	model.ConfigInstance.UsageMeter = rule
	t.Cleanup(func() { model.ConfigInstance.UsageMeter = prev })
}

// ensureNotificationTables creates the two tables CheckUsageMeter writes to.
// The shared test schema predates the notification center, so a test that
// exercises the notification path must create them itself.
func ensureNotificationTables(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(service.NotificationsDDL)
	require.NoError(t, err)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS pending_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id TEXT NOT NULL UNIQUE,
			event_type TEXT NOT NULL,
			payload TEXT NOT NULL,
			expires_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`)
	require.NoError(t, err)
}

// checkAt runs one CheckUsageMeter at the given instant, clearing the throttle
// first.
//
// The throttle is why this helper exists: CheckUsageMeter skips a call that
// arrives within one second of the previous one, so two back-to-back calls in a
// test would silently skip the second and make "only one notification" pass
// regardless of the implementation. Resetting the clock also clears the
// throttle (see SetUsageMeterNowForTest).
func checkAt(now time.Time) string {
	service.SetUsageMeterNowForTest(func() time.Time { return now })
	return service.CheckUsageMeter(context.Background())
}

// anchorOn returns a local-midnight anchor date string N days before `now`.
func anchorOn(now time.Time, daysAgo int) string {
	return now.AddDate(0, 0, -daysAgo).Format("2006-01-02")
}

// localMidnight returns the local 00:00 of the given day.
func localMidnight(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

// ── Cycle location ───────────────────────────────────────────────────────────

// An anchor carrying a time of day must place the cycle boundary at that
// instant, not at local midnight. Without this the settings page could accept
// "2026-10-05T14:30" and the meter would still roll over at 00:00 — the value
// would look saved and silently mean something else.
func TestUsageMeterCycle_AnchorWithTimePlacesBoundaryAtThatInstant(t *testing.T) {
	anchor := time.Date(2026, 10, 5, 14, 30, 0, 0, time.Local)
	// 3 hours after the anchor: still day 1, and the cycle start is the anchor
	// itself rather than that day's midnight.
	now := anchor.Add(3 * time.Hour)

	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: "2026-10-05T14:30:00",
		PeriodDays: 30,
		Metric:     model.UsageMeterMetricCost,
		Quota:      30,
	})
	setupDB(t)

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	require.True(t, st.Enabled)

	assert.Equal(t, 1, st.DayInCycle, "3h after the anchor is still day 1")

	start, err := time.Parse(time.RFC3339, st.CycleStart)
	require.NoError(t, err)
	require.True(t, anchor.Equal(start),
		"cycle start must be the anchor instant %v, got %v", anchor, start)

	// And one second BEFORE the anchor the cycle has not started at all: with a
	// midnight anchor this instant would already be inside day 1.
	before := anchor.Add(-1 * time.Second)
	stBefore, err := service.ComputeUsageMeter(context.Background(), before)
	require.NoError(t, err)
	assert.Equal(t, 0, stBefore.DayInCycle,
		"an instant before a 14:30 anchor must not be counted as day 1")
}

// A date-only anchor keeps its original meaning: local midnight. This is the
// compatibility guarantee for every pre-existing install.
func TestUsageMeterCycle_DateOnlyAnchorStillMeansLocalMidnight(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)

	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: "2026-10-05",
		PeriodDays: 30,
		Metric:     model.UsageMeterMetricCost,
		Quota:      30,
	})
	setupDB(t)

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	require.True(t, st.Enabled)

	start, err := time.Parse(time.RFC3339, st.CycleStart)
	require.NoError(t, err)
	want := time.Date(2026, 10, 5, 0, 0, 0, 0, time.Local)
	require.True(t, want.Equal(start), "date-only anchor must start at local midnight, got %v", start)
	assert.Equal(t, 15, st.DayInCycle)
}

func TestUsageMeterCycle_DayCounting(t *testing.T) {
	// A 30-day cycle anchored 10 days ago: we are on day 11.
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(13 * time.Hour)
	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 10),
		PeriodDays: 30,
		Metric:     model.UsageMeterMetricCost,
		Quota:      30,
	})
	setupDB(t)

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	require.True(t, st.Enabled)
	assert.Equal(t, 11, st.DayInCycle, "10 whole days elapsed → day 11")
	assert.InDelta(t, 30*11.0/30, st.Expected, 1e-9, "expected = quota * day / period")
	assert.Equal(t, 19, st.RemainingDays)
}

// The whole point of "day N has N days of budget": the first day must NOT have
// a zero baseline, or every cycle start produces a false "over plan" alert on
// the first turn.
func TestUsageMeterCycle_FirstDayHasOneDayOfBudget(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(2 * time.Hour)
	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 0),
		PeriodDays: 30,
		Metric:     model.UsageMeterMetricCost,
		Quota:      30,
	})
	setupDB(t)

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	assert.Equal(t, 1, st.DayInCycle)
	assert.InDelta(t, 1.0, st.Expected, 1e-9, "day 1 must have exactly one day of budget")
}

// A whole day later the baseline must still be "day 2", not "1.5 days": the
// partial day never produces a fractional allowance.
func TestUsageMeterCycle_WholeDayRounding(t *testing.T) {
	base := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local))
	anchor := anchorOn(base, 2)

	for _, tc := range []struct {
		name   string
		offset time.Duration
		want   int
	}{
		{"just after midnight", time.Hour, 3},
		{"late at night", 23*time.Hour + 59*time.Minute, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := base.Add(tc.offset)
			setUsageMeterRule(t, model.UsageMeterConfig{
				AnchorDate: anchor, PeriodDays: 30,
				Metric: model.UsageMeterMetricCost, Quota: 30,
			})
			setupDB(t)
			st, err := service.ComputeUsageMeter(context.Background(), now)
			require.NoError(t, err)
			assert.Equal(t, tc.want, st.DayInCycle)
			assert.InDelta(t, float64(tc.want), st.Expected, 1e-9)
		})
	}
}

// On the last day the baseline equals the whole quota, which is exactly why
// "over" and "full" coincide there and the notification logic must prefer full.
func TestUsageMeterCycle_LastDayBaselineIsFullQuota(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 29),
		PeriodDays: 30,
		Metric:     model.UsageMeterMetricCost,
		Quota:      30,
	})
	setupDB(t)

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	assert.Equal(t, 30, st.DayInCycle)
	assert.InDelta(t, 30.0, st.Expected, 1e-9)
	assert.Equal(t, 0, st.RemainingDays)
}

// Past the period length the cycle rolls over instead of running off the end.
func TestUsageMeterCycle_RollsOver(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	// Anchor 35 days ago with a 30-day period → 5 days into the second cycle.
	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 35),
		PeriodDays: 30,
		Metric:     model.UsageMeterMetricCost,
		Quota:      30,
	})
	setupDB(t)

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	assert.Equal(t, 6, st.DayInCycle, "35 elapsed days → day 6 of the second cycle")
	assert.InDelta(t, 30*6.0/30, st.Expected, 1e-9)
}

// An anchor in the future means no cycle has started: day 0, no budget, and
// nothing to compare against.
func TestUsageMeterCycle_FutureAnchor(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, -5),
		PeriodDays: 30,
		Metric:     model.UsageMeterMetricCost,
		Quota:      30,
	})
	setupDB(t)

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	assert.Equal(t, 0, st.DayInCycle)
	assert.InDelta(t, 0.0, st.Expected, 1e-9)
}

// ── Monthly cycles ───────────────────────────────────────────────────────────

// A "monthly" rule anchored on the 1st is a plain calendar month: the cycle runs
// from the 1st to the 1st, and the budget is spread over the ACTUAL number of
// days in that month. Dividing a 31-day month by a configured period_days of 30
// would let `expected` pass the quota on day 30 and then keep climbing.
func TestUsageMeterCycle_MonthlyCalendarMonth(t *testing.T) {
	// October has 31 days. Day 31, noon: the last day of the cycle.
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.Local)
	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: "2026-10-01",
		CycleMode:  model.UsageMeterCycleMonthly,
		// Deliberately 30: monthly mode must ignore it.
		PeriodDays: 30,
		Metric:     model.UsageMeterMetricCost,
		Quota:      31,
	})
	setupDB(t)

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	require.True(t, st.Enabled)

	start, err := time.Parse(time.RFC3339, st.CycleStart)
	require.NoError(t, err)
	end, err := time.Parse(time.RFC3339, st.CycleEnd)
	require.NoError(t, err)
	assert.True(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local).Equal(start),
		"cycle must start on the 1st, got %v", start)
	assert.True(t, time.Date(2026, 11, 1, 0, 0, 0, 0, time.Local).Equal(end),
		"cycle must end on the 1st of the next month, got %v", end)

	assert.Equal(t, 31, st.PeriodDays, "period must be the calendar month's length, not period_days")
	assert.Equal(t, 31, st.DayInCycle)
	assert.InDelta(t, 31.0, st.Expected, 1e-9,
		"on the last day of a 31-day month the baseline must be the full quota")
	assert.Equal(t, 0, st.RemainingDays)

	// The series must use the same divisor. A 31-point curve whose last
	// baseline reads 32.03 (quota/30 × 31) would contradict the headline number
	// right above it and draw the plan line above the quota line.
	require.Len(t, st.Series, 31, "one point per day of the month")
	assert.InDelta(t, 31.0, st.Series[30].Expected, 1e-9,
		"the last point's baseline must be the full quota, not quota/period_days×31")
	assert.InDelta(t, 1.0, st.Series[0].Expected, 1e-9,
		"day 1 has exactly one day of budget (quota/31)")
}

// A cycle whose anchor day is still ahead of today belongs to the PREVIOUS
// month. Without this, an anchor on the 5th would report a negative or
// clamped-to-1 day on the 1st.
func TestUsageMeterCycle_MonthlyAnchorDayAheadFallsBackOneMonth(t *testing.T) {
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.Local)
	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: "2026-10-05",
		CycleMode:  model.UsageMeterCycleMonthly,
		PeriodDays: 30,
		Metric:     model.UsageMeterMetricCost,
		Quota:      30,
	})
	setupDB(t)

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)

	start, err := time.Parse(time.RFC3339, st.CycleStart)
	require.NoError(t, err)
	end, err := time.Parse(time.RFC3339, st.CycleEnd)
	require.NoError(t, err)
	assert.True(t, time.Date(2026, 10, 5, 0, 0, 0, 0, time.Local).Equal(start),
		"on Nov 2nd the running cycle is Oct 5 → Nov 5, got start %v", start)
	assert.True(t, time.Date(2026, 11, 5, 0, 0, 0, 0, time.Local).Equal(end),
		"cycle end must be Nov 5, got %v", end)
	assert.Equal(t, 31, st.PeriodDays, "Oct 5 → Nov 5 spans 31 days")
	// Oct 5 is day 1, so Nov 2 is day 29.
	assert.Equal(t, 29, st.DayInCycle)
}

// A 31st anchor clamps to the end of a short month, and does NOT drift: the
// cycle after February still starts on the 31st of March.
func TestUsageMeterCycle_MonthlyClampsToShortMonthWithoutDrifting(t *testing.T) {
	now := time.Date(2027, 2, 20, 12, 0, 0, 0, time.Local)
	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: "2027-01-31",
		CycleMode:  model.UsageMeterCycleMonthly,
		PeriodDays: 30,
		Metric:     model.UsageMeterMetricCost,
		Quota:      28,
	})
	setupDB(t)

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)

	start, err := time.Parse(time.RFC3339, st.CycleStart)
	require.NoError(t, err)
	end, err := time.Parse(time.RFC3339, st.CycleEnd)
	require.NoError(t, err)
	assert.True(t, time.Date(2027, 1, 31, 0, 0, 0, 0, time.Local).Equal(start),
		"cycle must start on Jan 31, got %v", start)
	assert.True(t, time.Date(2027, 2, 28, 0, 0, 0, 0, time.Local).Equal(end),
		"Feb has no 31st, so the cycle must end on the 28th, got %v", end)
	assert.Equal(t, 28, st.PeriodDays)

	// The month after: the boundary is back on the 31st, proving the clamp is
	// per-month rather than a permanent shift to the 28th.
	//
	// This is the case that catches drift: the cycle running in March STARTED on
	// Feb 28 (clamped), so deriving the next boundary from the start's day
	// would put it on Mar 28 — a permanent shift. Only deriving it from the
	// ANCHOR's 31 gives Mar 31. Both ends must be asserted: checking the start
	// alone passes under the buggy derivation too.
	after := time.Date(2027, 3, 10, 12, 0, 0, 0, time.Local)
	stAfter, err := service.ComputeUsageMeter(context.Background(), after)
	require.NoError(t, err)
	startAfter, err := time.Parse(time.RFC3339, stAfter.CycleStart)
	require.NoError(t, err)
	endAfter, err := time.Parse(time.RFC3339, stAfter.CycleEnd)
	require.NoError(t, err)
	assert.True(t, time.Date(2027, 2, 28, 0, 0, 0, 0, time.Local).Equal(startAfter),
		"the cycle running on Mar 10 started Feb 28, got %v", startAfter)
	assert.True(t, time.Date(2027, 3, 31, 0, 0, 0, 0, time.Local).Equal(endAfter),
		"the boundary after a clamped Feb must return to the anchor's 31st, got %v", endAfter)
	assert.Equal(t, 31, stAfter.PeriodDays, "Feb 28 → Mar 31 spans 31 days")

	// And the cycle after that one starts on Mar 31 (not Apr 28).
	april := time.Date(2027, 4, 5, 12, 0, 0, 0, time.Local)
	stApr, err := service.ComputeUsageMeter(context.Background(), april)
	require.NoError(t, err)
	startApr, err := time.Parse(time.RFC3339, stApr.CycleStart)
	require.NoError(t, err)
	assert.True(t, time.Date(2027, 3, 31, 0, 0, 0, 0, time.Local).Equal(startApr),
		"after a short month the boundary must return to the 31st, got %v", startApr)
}

// The anchor's time of day still applies in monthly mode: the cycle restarts at
// that wall-clock instant, not at midnight.
func TestUsageMeterCycle_MonthlyKeepsAnchorTimeOfDay(t *testing.T) {
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.Local)
	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: "2026-10-05T09:30:00",
		CycleMode:  model.UsageMeterCycleMonthly,
		PeriodDays: 30,
		Metric:     model.UsageMeterMetricCost,
		Quota:      30,
	})
	setupDB(t)

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	start, err := time.Parse(time.RFC3339, st.CycleStart)
	require.NoError(t, err)
	assert.True(t, time.Date(2026, 11, 5, 9, 30, 0, 0, time.Local).Equal(start),
		"monthly cycles must keep the anchor's 09:30 boundary, got %v", start)
	assert.Equal(t, 2, st.DayInCycle, "Nov 6 is day 2 of a cycle that began Nov 5 at 09:30")
}

// A monthly cycle crosses the year boundary without special-casing December.
func TestUsageMeterCycle_MonthlyCrossesYearBoundary(t *testing.T) {
	now := time.Date(2027, 1, 3, 12, 0, 0, 0, time.Local)
	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: "2026-12-15",
		CycleMode:  model.UsageMeterCycleMonthly,
		PeriodDays: 30,
		Metric:     model.UsageMeterMetricCost,
		Quota:      30,
	})
	setupDB(t)

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	start, err := time.Parse(time.RFC3339, st.CycleStart)
	require.NoError(t, err)
	end, err := time.Parse(time.RFC3339, st.CycleEnd)
	require.NoError(t, err)
	assert.True(t, time.Date(2026, 12, 15, 0, 0, 0, 0, time.Local).Equal(start), "start = %v", start)
	assert.True(t, time.Date(2027, 1, 15, 0, 0, 0, 0, time.Local).Equal(end), "end = %v", end)
	assert.Equal(t, 31, st.PeriodDays)
	assert.Equal(t, 20, st.DayInCycle, "Dec 15 → Jan 3 is 19 elapsed days → day 20")
}

// The projection must clamp to THIS cycle's length. In a 31-day month a
// projection landing on day 30.5 is still inside the cycle (→ day 31); clamping
// to the configured 30 would report "will not run out" instead.
func TestUsageMeterCycle_MonthlyProjectionUsesCalendarLength(t *testing.T) {
	now := time.Date(2026, 10, 21, 0, 0, 0, 0, time.Local)
	db := setupDB(t)
	ensureAgentsTable(t, db)

	// 20 elapsed days at the rate that exhausts the quota on day 30.5.
	quota := 100.0
	actual := quota * 20 / 30.5
	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-proj", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-24 * time.Hour).UTC().Format("2006-01-02 15:04:05"),
		costUSD: actual,
	})

	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: "2026-10-01",
		CycleMode:  model.UsageMeterCycleMonthly,
		// Deliberately shorter than the month, which is the whole point.
		PeriodDays: 30,
		Metric:     model.UsageMeterMetricCost,
		Quota:      quota,
	})

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	require.Equal(t, 31, st.PeriodDays)
	assert.Equal(t, 31, st.ProjectedExhaustDay,
		"day 30.5 of a 31-day month is inside the cycle; clamping to period_days=30 reports 0")
}

// A monthly rule must work from a fresh install, where period_days is still 0
// because the settings page hides that field in monthly mode. Requiring it
// would make the meter silently report itself disabled right after the user
// saved a valid-looking rule.
func TestUsageMeterCycle_MonthlyWorksWithZeroPeriodDays(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.Local)
	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: "2026-10-01",
		CycleMode:  model.UsageMeterCycleMonthly,
		PeriodDays: 0,
		Metric:     model.UsageMeterMetricCost,
		Quota:      31,
	})
	setupDB(t)

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	require.True(t, st.Enabled, "a monthly rule does not need period_days")
	assert.Equal(t, 31, st.PeriodDays)
	assert.Equal(t, 15, st.DayInCycle)
}

// A FIXED rule with no period is not an error: it means "no cycle". The meter
// still reports the total since the anchor, it just has no day axis, no
// baseline and no rollover. Filling in 30 would reset the user's running total
// every month without ever being asked to.
func TestUsageMeterCycle_FixedWithoutPeriodIsUnbounded(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.Local)
	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: "2026-10-01",
		CycleMode:  model.UsageMeterCycleFixed,
		PeriodDays: 0,
		Metric:     model.UsageMeterMetricCost,
		Quota:      31,
	})
	setupDB(t)

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	require.True(t, st.Enabled, "a rule without a period still has a total to report")
	assert.Zero(t, st.PeriodDays, "no cycle means no cycle length")
	assert.Empty(t, st.CycleEnd, "an unbounded window has no end")
	assert.Zero(t, st.RemainingDays, "there is no 'days left' without a cycle")
	// Day 15 since the anchor still counts: the elapsed days are real even
	// though nothing rolls over.
	assert.Equal(t, 15, st.DayInCycle)
	// No cycle → the quota cannot be spread over days → no baseline, no
	// projection. Showing one would invent a plan the user never set.
	assert.Zero(t, st.Expected)
	assert.Zero(t, st.ProjectedExhaustDay)
}

// An empty cycle_mode must keep behaving exactly like "fixed". This is the
// compatibility guarantee for every install written before the field existed.
func TestUsageMeterCycle_EmptyModeIsFixed(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(13 * time.Hour)
	for _, mode := range []string{"", model.UsageMeterCycleFixed} {
		t.Run("mode="+mode, func(t *testing.T) {
			setUsageMeterRule(t, model.UsageMeterConfig{
				AnchorDate: anchorOn(now, 10),
				CycleMode:  mode,
				PeriodDays: 30,
				Metric:     model.UsageMeterMetricCost,
				Quota:      30,
			})
			setupDB(t)

			st, err := service.ComputeUsageMeter(context.Background(), now)
			require.NoError(t, err)
			assert.Equal(t, 11, st.DayInCycle)
			assert.Equal(t, 30, st.PeriodDays, "a fixed cycle echoes period_days")
			// Fixed cycles are 24h spans from the anchor, so the start is the
			// anchor's own midnight 10 days back.
			start, err := time.Parse(time.RFC3339, st.CycleStart)
			require.NoError(t, err)
			assert.True(t, localMidnight(now.AddDate(0, 0, -10)).Equal(start))
		})
	}
}

// A monthly rule still reports day 0 (and no baseline) when the anchor is in
// the future — the same contract as the fixed mode.
func TestUsageMeterCycle_MonthlyFutureAnchor(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)
	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: "2026-11-05",
		CycleMode:  model.UsageMeterCycleMonthly,
		PeriodDays: 30,
		Metric:     model.UsageMeterMetricCost,
		Quota:      30,
	})
	setupDB(t)

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	assert.Equal(t, 0, st.DayInCycle)
	assert.InDelta(t, 0.0, st.Expected, 1e-9)
}

// ── Aggregation ──────────────────────────────────────────────────────────────

// Usage is summed across ALL projects — the meter is a global budget, so a
// project filter would under-count.
func TestUsageMeter_SumsAllProjects(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	db := setupDB(t)
	ensureAgentsTable(t, db)

	// Two projects, both inside the current cycle.
	for _, seed := range []usageSeed{
		{
			project: "/a", sessionID: "s-a", agentID: "cb", agentName: "CB", backend: "codebuddy",
			model: "m", createdAt: now.Add(-48 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 1.5,
		},
		{
			project: "/b", sessionID: "s-b", agentID: "cb", agentName: "CB", backend: "codebuddy",
			model: "m", createdAt: now.Add(-24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 2.5,
		},
	} {
		insertUsageSeed(t, db, seed)
	}

	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 5), PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 30,
	})

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	assert.InDelta(t, 4.0, st.Actual, 1e-9, "usage from every project must be counted")
}

// A row from the previous cycle must not leak into this one.
func TestUsageMeter_ExcludesPreviousCycle(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	db := setupDB(t)
	ensureAgentsTable(t, db)

	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-old", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-10 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 99,
	})
	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-new", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-1 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 3,
	})

	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 5), PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 30,
	})

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	assert.InDelta(t, 3.0, st.Actual, 1e-9, "only the current cycle counts")
}

// Token totals fall back to input+output when total_tokens was never recorded,
// matching the stats panel's total column.
func TestUsageMeter_TokenFallbackWhenTotalMissing(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	db := setupDB(t)
	ensureAgentsTable(t, db)

	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-t", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-1 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05"),
		input: 700, output: 300, total: 0, // total omitted by the backend parser
	})

	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 5), PeriodDays: 30,
		Metric: model.UsageMeterMetricTokens, Quota: 100000,
	})

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	assert.InDelta(t, 1000.0, st.Actual, 1e-9)
}

func TestUsageMeter_CreditMetric(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	db := setupDB(t)
	ensureAgentsTable(t, db)

	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-c", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-1 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05"),
		credit: 12.5,
	})

	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 5), PeriodDays: 30,
		Metric: model.UsageMeterMetricCredit, Quota: 100,
	})

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	assert.InDelta(t, 12.5, st.Actual, 1e-9)
}

// The series must be continuous (one point per elapsed day, including days with
// no usage) and end with the same total the aggregate reports.
func TestUsageMeter_SeriesIsContinuousAndConsistent(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	db := setupDB(t)
	ensureAgentsTable(t, db)

	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-1", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-3 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 2,
	})
	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-2", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-1 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 3,
	})

	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 5), PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 30,
	})

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	require.Len(t, st.Series, 6, "one point per elapsed day (day 1..6)")
	// Cumulative and monotonic.
	for i := 1; i < len(st.Series); i++ {
		assert.GreaterOrEqual(t, st.Series[i].Actual, st.Series[i-1].Actual)
	}
	assert.InDelta(t, st.Actual, st.Series[len(st.Series)-1].Actual, 1e-9,
		"the last series point must equal the aggregate total")
}

// ── Disabled / malformed rules ───────────────────────────────────────────────

func TestUsageMeter_DisabledWhenQuotaZero(t *testing.T) {
	setUsageMeterRule(t, model.UsageMeterConfig{Quota: 0})
	setupDB(t)
	st, err := service.ComputeUsageMeter(context.Background(), time.Now())
	require.NoError(t, err)
	assert.False(t, st.Enabled)
}

// A hand-edited config.yaml can carry a rule the PATCH validator would reject.
// The meter reports itself disabled rather than erroring: it feeds a header
// badge, and a 500 there would be noise.
func TestUsageMeter_InvalidRuleIsDisabledNotError(t *testing.T) {
	for _, tc := range []struct {
		name string
		rule model.UsageMeterConfig
	}{
		{"unknown metric", model.UsageMeterConfig{Quota: 10, PeriodDays: 30, Metric: "usd", AnchorDate: "2026-10-01"}},
		{"bad anchor", model.UsageMeterConfig{Quota: 10, PeriodDays: 30, Metric: model.UsageMeterMetricCost, AnchorDate: "not-a-date"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setUsageMeterRule(t, tc.rule)
			setupDB(t)
			st, err := service.ComputeUsageMeter(context.Background(), time.Now())
			require.NoError(t, err)
			assert.False(t, st.Enabled)
		})
	}
}

// ── Projection ───────────────────────────────────────────────────────────────

// Spending faster than the plan must project an exhaustion day inside the cycle.
func TestUsageMeter_ProjectsExhaustionDay(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	db := setupDB(t)
	ensureAgentsTable(t, db)

	// Day 6, quota 30, already spent 15 → 2.5/day → 12 days total to burn 30.
	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-p", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-3 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 15,
	})

	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 5), PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 30,
	})

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	assert.Greater(t, st.ProjectedExhaustDay, 0)
	assert.LessOrEqual(t, st.ProjectedExhaustDay, 30)
}

// ── Notification state machine ───────────────────────────────────────────────

// The stage decision is unexported; exercise it through CheckUsageMeter's
// observable effect (the notifications table).
func TestCheckUsageMeter_NotifiesOnceOnOverPlan(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	db := setupDB(t)
	ensureNotificationTables(t, db)
	ensureAgentsTable(t, db)

	// Day 6 of 30 with quota 30 → expected 6. Spend 10 → over plan, not full.
	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-n", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-1 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 10,
	})

	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 5), PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 30,
	})
	t.Cleanup(func() { service.SetUsageMeterNowForTest(nil) })

	stage := checkAt(now)
	assert.Equal(t, service.UsageMeterStageOver, stage)

	rows, err := service.ListNotifications(service.NotificationFilter{Kind: service.NotificationKindUsage})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, service.UsageMeterStageOver, rows[0].EventType)
	assert.NotEmpty(t, rows[0].Title)

	// A second check in the same cycle must stay silent. Uses checkAt (which
	// clears the throttle): calling CheckUsageMeter directly would also return
	// "" — because of the one-second throttle, not because of stage dedup — so
	// the assertion would pass even with dedup removed.
	assert.Equal(t, "", checkAt(now.Add(time.Minute)))
	rows, err = service.ListNotifications(service.NotificationFilter{Kind: service.NotificationKindUsage})
	require.NoError(t, err)
	assert.Len(t, rows, 1, "one notification per stage per cycle")
}

// Spending straight past the quota on the first check must report "full", not
// "over": the two conditions coincide and only the stronger one is sent.
func TestCheckUsageMeter_FullWinsOverOver(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	db := setupDB(t)
	ensureNotificationTables(t, db)
	ensureAgentsTable(t, db)

	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-f", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-1 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 45,
	})

	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 5), PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 30,
	})
	t.Cleanup(func() { service.SetUsageMeterNowForTest(nil) })

	assert.Equal(t, service.UsageMeterStageFull, checkAt(now))
	rows, err := service.ListNotifications(service.NotificationFilter{Kind: service.NotificationKindUsage})
	require.NoError(t, err)
	require.Len(t, rows, 1, "only the full notification, never both")
	assert.Equal(t, service.UsageMeterStageFull, rows[0].EventType)
}

// A new cycle start re-arms notification even though the stored stage says
// "already told you" — the stored cycle start no longer matches.
func TestCheckUsageMeter_NewCycleRearms(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	db := setupDB(t)
	ensureNotificationTables(t, db)
	ensureAgentsTable(t, db)

	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-r", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-1 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 45,
	})

	// Stored state belongs to some OTHER cycle.
	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 5), PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 30,
		NotifiedCycleStart: "2020-01-01T00:00:00+08:00",
		NotifiedStage:      service.UsageMeterStageFull,
	})
	t.Cleanup(func() { service.SetUsageMeterNowForTest(nil) })

	assert.Equal(t, service.UsageMeterStageFull, checkAt(now),
		"a stale cycle start must not suppress the new cycle's notification")
}

// Under the baseline: nothing to say.
func TestCheckUsageMeter_QuietUnderPlan(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	db := setupDB(t)
	ensureNotificationTables(t, db)
	ensureAgentsTable(t, db)

	// Day 6 → expected 6; spend 5.
	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-q", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-1 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 5,
	})

	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 5), PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 30,
	})
	t.Cleanup(func() { service.SetUsageMeterNowForTest(nil) })

	assert.Equal(t, "", checkAt(now))
}

// Once the cycle has reported "full", it must never report "over" again — the
// stage is monotonic. A downgrade would send a second, weaker notification for
// a cycle the user has already been told is exhausted.
func TestCheckUsageMeter_StageNeverDowngrades(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	db := setupDB(t)
	ensureNotificationTables(t, db)
	ensureAgentsTable(t, db)

	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-d", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-1 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 45,
	})

	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 5), PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 30,
		// The cycle start the rule computes is what the service stores, so a
		// literal here would be re-armed. Use the stage alone: an empty
		// cycle start means "different cycle", so seed the matching one by
		// running the first check and then re-checking.
	})
	t.Cleanup(func() { service.SetUsageMeterNowForTest(nil) })

	require.Equal(t, service.UsageMeterStageFull, checkAt(now))

	// Simulate the stored state after that notification.
	model.ConfigInstance.UsageMeter.NotifiedStage = service.UsageMeterStageFull
	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	model.ConfigInstance.UsageMeter.NotifiedCycleStart = st.CycleStart

	assert.Equal(t, "", checkAt(now),
		"a full cycle must not fall back to an over-plan notification")
}

// The series must place a row on the LOCAL day it belongs to, not the UTC day
// its timestamp happens to fall on.
//
// chat_metadata.created_at is UTC; the cycle boundary is local midnight. A row
// written at 23:30 UTC is "tomorrow" by UTC reckoning but still "today" locally
// on any server east of UTC. Bucketing by the raw UTC date drops the row onto
// the following day, so the curve shows usage one day late and the final point
// can disagree with the aggregate total.
//
// The assertion therefore names the day: asserting only "last point == total"
// is a tautology, because a shifted bucket still ends at the same cumulative
// sum.
func TestUsageMeter_SeriesBucketsByLocalDayNotUTCDay(t *testing.T) {
	base := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local))
	now := base.Add(12 * time.Hour)
	db := setupDB(t)
	ensureAgentsTable(t, db)

	// A UTC timestamp that is 23:30 on the previous UTC day. Locally (UTC+8) it
	// is 07:30 today; in UTC terms it is still yesterday.
	utcLate := time.Date(2026, 10, 18, 23, 30, 0, 0, time.UTC)
	localDay := utcLate.Local().Format("2006-01-02")
	if utcLate.Format("2006-01-02") == localDay {
		// The whole point of this test is the UTC/local date divergence, which
		// does not exist on a UTC host. Skip rather than fail: CI runners are
		// UTC, and the timezone is an environment property, not a defect.
		t.Skip("host timezone is UTC; the UTC/local date divergence cannot be exercised")
	}

	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-tz", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: utcLate.Format("2006-01-02 15:04:05"), costUSD: 2,
	})

	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 5), PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 30,
	})

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	require.NotEmpty(t, st.Series)

	// The usage must appear ON the local day, and every earlier day must be 0.
	for _, p := range st.Series {
		if p.Day == localDay {
			assert.InDelta(t, 2.0, p.Actual, 1e-9,
				"usage must land on local day %s", localDay)
		} else if p.Day < localDay {
			assert.InDelta(t, 0.0, p.Actual, 1e-9,
				"day %s precedes the row and must be empty", p.Day)
		}
	}
	assert.InDelta(t, st.Actual, st.Series[len(st.Series)-1].Actual, 1e-9)
}

// The over → full progression is the feature's second notification and has to
// work: a user who is warned once, keeps spending, and then exhausts the quota
// must be told. Only the initial-full and the never-downgrade paths were
// covered, so removing the progression went undetected.
func TestCheckUsageMeter_OverThenFullProgression(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	db := setupDB(t)
	ensureNotificationTables(t, db)
	ensureAgentsTable(t, db)

	// Day 6 of 30, quota 30 → expected 6. Spend 10: over plan, not full.
	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-prog", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-1 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 10,
	})

	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 5), PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 30,
	})
	t.Cleanup(func() { service.SetUsageMeterNowForTest(nil) })

	require.Equal(t, service.UsageMeterStageOver, checkAt(now))

	// Spend past the quota in the same cycle.
	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-prog2", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-1 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 25,
	})

	assert.Equal(t, service.UsageMeterStageFull, checkAt(now.Add(time.Minute)),
		"after an over-plan warning, exhausting the quota must produce a second notification")

	rows, err := service.ListNotifications(service.NotificationFilter{Kind: service.NotificationKindUsage})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	// Newest first.
	assert.Equal(t, service.UsageMeterStageFull, rows[0].EventType)
	assert.Equal(t, service.UsageMeterStageOver, rows[1].EventType)
}

// ── Rule validation branches ─────────────────────────────────────────────────

// A malformed rule is reported as DISABLED, not as an error: the caller is a
// header badge, and a 500 there would be noise on every poll. The PATCH
// validator rejects such rules, so this only happens for a hand-edited
// config.yaml — which must degrade quietly rather than break the header.
func TestComputeUsageMeter_MalformedRuleIsDisabledNotAnError(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	setupDB(t)

	for _, tc := range []struct {
		name string
		rule model.UsageMeterConfig
	}{
		{"unknown metric", model.UsageMeterConfig{Quota: 10, PeriodDays: 30, Metric: "usd", AnchorDate: anchorOn(now, 5)}},
		{"malformed anchor", model.UsageMeterConfig{Quota: 10, PeriodDays: 30, Metric: model.UsageMeterMetricCost, AnchorDate: "10/05/2026"}},
		{"unknown metric", model.UsageMeterConfig{Quota: 10, PeriodDays: 30, Metric: "usd", AnchorDate: anchorOn(now, 5)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setUsageMeterRule(t, tc.rule)
			st, err := service.ComputeUsageMeter(context.Background(), now)
			require.NoError(t, err, "a malformed rule must not surface as an error")
			require.NotNil(t, st)
			assert.False(t, st.Enabled)
		})
	}
}

// An anchor in the FUTURE means the cycle has not started: nothing has been
// counted, and there is no budget to compare against yet. Reporting a
// day-in-cycle of 0 (rather than a negative one) is what keeps the projection
// and the "over plan" predicate quiet until the cycle actually begins.
func TestComputeUsageMeter_FutureAnchorReportsDayZero(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	db := setupDB(t)
	ensureAgentsTable(t, db)

	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-f", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 5,
	})
	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, -10), PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 30,
	})

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	assert.True(t, st.Enabled, "a future anchor is a valid rule, just an empty cycle")
	assert.Zero(t, st.DayInCycle)
	assert.Zero(t, st.Expected)
	assert.Zero(t, st.ProjectedExhaustDay, "no projection before the cycle starts")
}

// No usage in the cycle must not project an exhaustion day: the rate is
// undefined, and a divide-by-zero guess would show "runs out today" on a fresh
// cycle.
func TestComputeUsageMeter_NoUsageProjectsNothing(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	db := setupDB(t)
	ensureAgentsTable(t, db)

	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 5), PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 30,
	})

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	assert.Zero(t, st.Actual)
	assert.Zero(t, st.ProjectedExhaustDay)
}

// A pace that outlasts the cycle projects nothing: "it will not run out this
// cycle" is the absence of a projection, not day 30.
func TestComputeUsageMeter_SlowPaceProjectsNothing(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	db := setupDB(t)
	ensureAgentsTable(t, db)

	// Day 6 of 30, quota 30, spent 1 → 0.2/day → 150 days to burn the quota.
	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-slow", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 1,
	})
	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 5), PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 30,
	})

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	assert.Zero(t, st.ProjectedExhaustDay, "a quota that outlasts the cycle must not project a day")
}

// Already past the quota: the "projected" day is today, never a past day.
func TestComputeUsageMeter_PastQuotaProjectsToday(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	db := setupDB(t)
	ensureAgentsTable(t, db)

	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-over", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 90,
	})
	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 5), PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 30,
	})

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	require.Greater(t, st.DayInCycle, 0)
	assert.Equal(t, st.DayInCycle, st.ProjectedExhaustDay,
		"past the quota the projection is today, not a day already gone")
}
