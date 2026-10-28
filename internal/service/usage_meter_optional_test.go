package service_test

import (
	"context"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The rule is a set of independent, OPTIONAL fields. These tests pin the
// "largest readout the configuration supports" contract: every subset of
// {anchor, period, quota} is a valid rule that reports something smaller, and
// only a disabled (or completely empty) rule reports nothing.
//
// The failure this guards against is the opposite: silently inventing the
// missing half. A synthesized 30-day cycle resets the user's running total every
// month; a synthesized baseline turns any spend into "over plan".

// ── Nothing configured ───────────────────────────────────────────────────────

// An enabled rule with no fields at all has nothing to report: "show as much as
// the configuration supports" has no floor when nothing is set. The client
// renders the same placeholder it does for a disabled rule.
func TestUsageMeter_EnabledButEmptyReportsNothing(t *testing.T) {
	setUsageMeterRuleRaw(t, model.UsageMeterConfig{Enabled: true})
	setupDB(t)

	st, err := service.ComputeUsageMeter(context.Background(), time.Now())
	require.NoError(t, err)
	assert.False(t, st.Enabled)
}

// The switch is authoritative: a fully configured rule with Enabled=false must
// stay silent, otherwise turning the feature off would only hide the capsule
// while notifications kept firing.
func TestUsageMeter_DisabledRuleStaysSilentEvenWhenConfigured(t *testing.T) {
	now := time.Now()
	setUsageMeterRuleRaw(t, model.UsageMeterConfig{
		Enabled:    false,
		AnchorDate: anchorOn(now, 5),
		PeriodDays: 30,
		Metric:     model.UsageMeterMetricCost,
		Quota:      1, // deliberately far below the spend seeded below
	})
	db := setupDB(t)
	ensureNotificationTables(t, db)
	ensureAgentsTable(t, db)
	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-off", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 99,
	})

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	assert.False(t, st.Enabled)

	service.SetUsageMeterNowForTest(func() time.Time { return now })
	t.Cleanup(func() { service.SetUsageMeterNowForTest(nil) })
	assert.Empty(t, service.CheckUsageMeter(context.Background()),
		"a disabled rule must not notify, even when the numbers are way past the quota")
}

// ── Anchor only: a total, no cycle, no plan ──────────────────────────────────

func TestUsageMeter_AnchorOnlyReportsTotalWithoutCycleOrPlan(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	db := setupDB(t)
	ensureAgentsTable(t, db)
	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-ao", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-3 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 7.25,
	})

	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 10),
		Metric:     model.UsageMeterMetricCost,
	})

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	require.True(t, st.Enabled)

	assert.InDelta(t, 7.25, st.Actual, 1e-9, "the total since the anchor is still reported")
	assert.Equal(t, 11, st.DayInCycle, "elapsed days since the anchor are real")
	assert.Zero(t, st.PeriodDays, "no period means no cycle length")
	assert.Empty(t, st.CycleEnd, "no cycle means no end")
	assert.Zero(t, st.Expected, "no quota and no cycle: nothing to spread")
	assert.Zero(t, st.RemainingDays)
	assert.Zero(t, st.ProjectedExhaustDay)
	assert.NotEmpty(t, st.CycleStart)
	require.NotEmpty(t, st.Series)
	assert.InDelta(t, st.Actual, st.Series[len(st.Series)-1].Actual, 1e-9,
		"the curve must still end at the total")
}

// A window that never rolls over must not lose history: an entry from before
// what would have been a 30-day boundary still counts.
func TestUsageMeter_AnchorOnlyDoesNotRollOver(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	db := setupDB(t)
	ensureAgentsTable(t, db)
	// 100 days back: past any 30-day cycle, still after the anchor.
	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-ro", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-100 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 3,
	})
	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-ro2", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-2 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 4,
	})

	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 120),
		Metric:     model.UsageMeterMetricCost,
	})

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	assert.InDelta(t, 7.0, st.Actual, 1e-9,
		"with no cycle the window never resets, so a 100-day-old row still counts")
}

// ── No anchor: the whole ledger ──────────────────────────────────────────────

// Without an anchor the window starts at the beginning of the ledger. The
// lower bound must not become the zero time formatted as a date (year 1) nor
// silently drop every row.
func TestUsageMeter_NoAnchorCountsWholeLedger(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	db := setupDB(t)
	ensureAgentsTable(t, db)
	for i, seed := range []usageSeed{
		{
			project: "/a", sessionID: "s-na1", agentID: "cb", agentName: "CB", backend: "codebuddy",
			model: "m", createdAt: now.Add(-400 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 1,
		},
		{
			project: "/a", sessionID: "s-na2", agentID: "cb", agentName: "CB", backend: "codebuddy",
			model: "m", createdAt: now.Add(-1 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 2,
		},
	} {
		_ = i
		insertUsageSeed(t, db, seed)
	}

	setUsageMeterRule(t, model.UsageMeterConfig{
		PeriodDays: 30,
		Metric:     model.UsageMeterMetricCost,
		Quota:      10,
	})

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	require.True(t, st.Enabled)
	assert.InDelta(t, 3.0, st.Actual, 1e-9, "the whole ledger is counted, including a 400-day-old row")
	assert.Empty(t, st.CycleStart, "no anchor means no start to report")
	assert.Empty(t, st.CycleEnd, "no anchor means the window never ends")
	assert.Zero(t, st.DayInCycle, "there is no day counter without a start")
	assert.Zero(t, st.Expected, "a quota with no cycle cannot be spread over days")
	assert.Zero(t, st.ProjectedExhaustDay)
}

// ── Quota + cycle but no anchor is impossible; quota + anchor but no cycle ───

// A quota spread over a cycle needs BOTH halves. With only one, `expected`
// must stay 0 — a non-zero baseline would make the notification path report an
// overspend against a plan that does not exist.
func TestUsageMeter_QuotaWithoutCycleHasNoBaseline(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	db := setupDB(t)
	ensureNotificationTables(t, db)
	ensureAgentsTable(t, db)
	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-qc", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-2 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 50,
	})

	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 10),
		Metric:     model.UsageMeterMetricCost,
		Quota:      20, // already exceeded, but there is no plan to be "over"
	})

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	assert.Zero(t, st.Expected)
	assert.Zero(t, st.RemainingDays)
	assert.Zero(t, st.ProjectedExhaustDay)

	service.SetUsageMeterNowForTest(func() time.Time { return now })
	t.Cleanup(func() { service.SetUsageMeterNowForTest(nil) })
	assert.Empty(t, service.CheckUsageMeter(context.Background()),
		"no baseline means no 'over plan' alert — the quota alone is not a plan")
}

// ...but a quota WITH a cycle and a usage past it is "full", which does not
// depend on the baseline at all. This is the mirror of the test above: it keeps
// the new `Expected <= 0` guard from silencing a genuine exhaustion.
func TestUsageMeter_QuotaWithCycleStillReportsFull(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	db := setupDB(t)
	ensureNotificationTables(t, db)
	ensureAgentsTable(t, db)
	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-full", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-2 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 50,
	})

	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 10),
		PeriodDays: 30,
		Metric:     model.UsageMeterMetricCost,
		Quota:      20,
	})

	service.SetUsageMeterNowForTest(func() time.Time { return now })
	t.Cleanup(func() { service.SetUsageMeterNowForTest(nil) })
	assert.Equal(t, service.UsageMeterStageFull, service.CheckUsageMeter(context.Background()))
}

// ── Future anchor with no cycle ─────────────────────────────────────────────

// A future anchor still means "nothing counted yet", with or without a period.
func TestUsageMeter_FutureAnchorWithoutPeriodReportsDayZero(t *testing.T) {
	now := localMidnight(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)).Add(12 * time.Hour)
	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, -5),
		Metric:     model.UsageMeterMetricCost,
	})
	setupDB(t)

	st, err := service.ComputeUsageMeter(context.Background(), now)
	require.NoError(t, err)
	require.True(t, st.Enabled)
	assert.Zero(t, st.DayInCycle)
	assert.Zero(t, st.Actual)
	assert.Zero(t, st.Expected)
}
