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
func setUsageMeterRule(t *testing.T, rule model.UsageMeterConfig) {
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

// ── Aggregation ──────────────────────────────────────────────────────────────

// Usage is summed across ALL projects — the meter is a global budget, so a
// project filter would under-count.
func TestUsageMeter_SumsAllProjects(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	db := setupDB(t)
	ensureAgentsTable(t, db)

	// Two projects, both inside the current cycle.
	for _, seed := range []usageSeed{
		{project: "/a", sessionID: "s-a", agentID: "cb", agentName: "CB", backend: "codebuddy",
			model: "m", createdAt: now.Add(-48 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 1.5},
		{project: "/b", sessionID: "s-b", agentID: "cb", agentName: "CB", backend: "codebuddy",
			model: "m", createdAt: now.Add(-24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 2.5},
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
		{"zero period", model.UsageMeterConfig{Quota: 10, PeriodDays: 0, Metric: model.UsageMeterMetricCost, AnchorDate: "2026-10-01"}},
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
