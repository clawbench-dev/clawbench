package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"

	"clawbench/internal/i18n"
	"clawbench/internal/model"
	"clawbench/internal/push/dingtalk"
	"clawbench/internal/push/feishu"
	"clawbench/internal/store"
	"clawbench/internal/ws"
)

// Usage-meter notification stages. Monotonic within a cycle: "" → over → full.
const (
	UsageMeterStageOver = "over"
	UsageMeterStageFull = "full"
)

// UsageMeterEventType is the WS event name for a usage-meter notification.
//
// It is NOT a session/task event: the payload carries pre-rendered title/body
// instead of an id + status the client would have to translate. Adding it here
// means the event must also be taught to every consumer that switches on event
// name — see pending_events.go (IsNotifiableEvent) and the Android
// NativeNotificationPolicy, which is where a missing branch silently drops the
// notification on a backgrounded app.
const UsageMeterEventType = "usage_meter_update"

// UsageMeterPoint is one day of the cumulative series.
type UsageMeterPoint struct {
	Day      string  `json:"day"`      // local date "2006-01-02"
	Actual   float64 `json:"actual"`   // cumulative usage through this day
	Expected float64 `json:"expected"` // linear budget through this day
}

// UsageMeterStatus is the full state of the meter, shaped for GET /api/usage/meter
// and for the header capsule.
type UsageMeterStatus struct {
	Enabled bool   `json:"enabled"`
	Metric  string `json:"metric,omitempty"`
	// AnchorDate/PeriodDays echo the rule so the popover can render "day N of M"
	// without a second request to /api/config. PeriodDays is the length of the
	// CURRENT cycle (in monthly mode it follows the calendar month).
	//
	// The cycle MODE is deliberately not echoed: every consumer derives what it
	// needs from "day N of M" plus the cycle bounds, and the settings page reads
	// the mode from /api/config. A field nobody reads would be dead weight on
	// every broadcast frame.
	AnchorDate string  `json:"anchorDate,omitempty"`
	PeriodDays int     `json:"periodDays,omitempty"`
	Quota      float64 `json:"quota,omitempty"`
	// CycleStart/CycleEnd are RFC3339 instants in the server's local timezone.
	CycleStart string `json:"cycleStart,omitempty"`
	CycleEnd   string `json:"cycleEnd,omitempty"`
	// DayInCycle is 1-based. 0 means "the anchor is still in the future".
	DayInCycle int     `json:"dayInCycle"`
	Actual     float64 `json:"actual"`
	Expected   float64 `json:"expected"`
	// RemainingDays counts whole days left after today (periodDays - dayInCycle).
	RemainingDays int `json:"remainingDays"`
	// ProjectedExhaustDay is the day of the cycle on which the quota is
	// projected to run out at the current rate; 0 means "not within this cycle".
	ProjectedExhaustDay int `json:"projectedExhaustDay"`
	// Series is the per-day cumulative curve, oldest first, ending today.
	Series []UsageMeterPoint `json:"series,omitempty"`
}

// usageMeterNow is overridable in tests.
var usageMeterNow = time.Now

// SetUsageMeterNowForTest swaps the clock. Passing nil restores the default.
// Exported because the tests live in the external service_test package.
// It also clears the check throttle: two checks in one test run back to back,
// and the throttle would silently skip the second one.
func SetUsageMeterNowForTest(fn func() time.Time) {
	if fn == nil {
		usageMeterNow = time.Now
	} else {
		usageMeterNow = fn
	}
	usageMeterLastCheckMu.Lock()
	usageMeterLastCheck = time.Time{}
	usageMeterLastCheckMu.Unlock()
}

// usageMeterStateSaver persists the two internal notification-state fields.
// Wired from main.go to the handler (which owns configMutex and the atomic
// YAML write). Nil in tests that do not exercise persistence, in which case the
// state simply is not saved — the in-memory copy still prevents a duplicate
// within the process lifetime.
var usageMeterStateSaver func(cycleStart, stage string) error

// SetUsageMeterStateSaver installs the persistence hook for the notification
// state. Called once from main.go.
func SetUsageMeterStateSaver(fn func(cycleStart, stage string) error) {
	usageMeterStateSaver = fn
}

// usageMeterCheckMu serializes CheckUsageMeter. The worker tick and a turn
// completion can fire at the same instant; without this both would read the
// same "not yet notified" state and write two identical notifications.
var usageMeterCheckMu sync.Mutex

// usageMeterMinInterval throttles checks so a burst of turn completions does
// not run one aggregate query each. A usage notification is a coarse signal —
// one check per second is more than enough.
const usageMeterMinInterval = time.Second

var (
	usageMeterLastCheck   time.Time
	usageMeterLastCheckMu sync.Mutex
)

// usageMeterRowExpr maps a metric to its PER-ROW value expression.
//
// Separate from the aggregate on purpose: the series needs one value per row so
// it can attribute each row to a local day, while the total needs SUM over the
// same rows. Using the SUM expression in a `SELECT created_at, SUM(...)` without
// GROUP BY would collapse the whole window into a single arbitrary row.
//
// The tokens branch MUST stay identical to usageTotalSumExpr's CASE in
// usage_stats.go: the header capsule and the stats panel's "total" column are
// read side by side, so a divergence shows up as two different numbers for the
// same period.
func usageMeterRowExpr(metric string) (string, bool) {
	switch metric {
	case model.UsageMeterMetricTokens:
		return "CASE WHEN m.total_tokens > 0 THEN m.total_tokens ELSE m.input_tokens + m.output_tokens END", true
	case model.UsageMeterMetricCredit:
		return "m.credit", true
	case model.UsageMeterMetricCost:
		return "m.cost_usd", true
	}
	return "", false
}

// usageMeterCycle locates now within the rule's rolling cycle.
//
// Boundaries are fixed 24h spans from the anchor rather than calendar-date
// differences in "fixed" mode. A DST transition day is 23 or 25 hours long, so a
// calendar difference would make "day 10 of 30" mean different amounts of
// elapsed time on either side of the switch. With fixed spans the cycle length
// is constant; the price is that after a DST change the boundary sits one hour
// off local midnight (twice a year, invisible in practice).
//
// "monthly" mode is the opposite trade: cycles follow the calendar, so their
// length varies (28-31 days) and days are counted by calendar date rather than
// elapsed hours.
type usageMeterCycle struct {
	start time.Time
	end   time.Time
	// dayInCycle is 1-based; 0 when now < anchor.
	dayInCycle int
	// periodDays is the length of THIS cycle. In "fixed" mode it is the rule's
	// PeriodDays; in "monthly" mode it is however many days this month's cycle
	// spans. Every caller must use this rather than rule.PeriodDays — using the
	// rule's value in monthly mode would divide a 31-day month's budget by 30
	// and let `expected` exceed the quota before the month is over.
	periodDays int
}

// locateUsageMeterCycle dispatches on the rule's cycle mode.
func locateUsageMeterCycle(anchor time.Time, rule model.UsageMeterConfig, now time.Time) usageMeterCycle {
	if model.NormalizeUsageMeterCycleMode(rule.CycleMode) == model.UsageMeterCycleMonthly {
		return locateMonthlyCycle(anchor, now)
	}
	return locateFixedCycle(anchor, rule.PeriodDays, now)
}

func locateFixedCycle(anchor time.Time, periodDays int, now time.Time) usageMeterCycle {
	day := 24 * time.Hour
	elapsed := now.Sub(anchor)
	if elapsed < 0 {
		return usageMeterCycle{
			start:      anchor,
			end:        anchor.Add(time.Duration(periodDays) * day),
			periodDays: periodDays,
		}
	}
	elapsedDays := int(elapsed / day)
	periodIndex := elapsedDays / periodDays
	start := anchor.Add(time.Duration(periodIndex*periodDays) * day)
	return usageMeterCycle{
		start:      start,
		end:        start.Add(time.Duration(periodDays) * day),
		dayInCycle: elapsedDays - periodIndex*periodDays + 1,
		periodDays: periodDays,
	}
}

// locateMonthlyCycle puts the cycle boundary on the anchor's day-of-month,
// keeping its time of day.
//
// The anchor supplies three things: which day of the month the cycle restarts,
// the wall-clock instant it restarts, and (implicitly, via its own month) the
// first cycle. So an anchor of "2026-10-05T09:00" means the cycle restarts at
// 09:00 on the 5th of every month — which makes "the 1st at 00:00" a plain
// calendar month without a second configuration knob.
//
// Day-of-month clamping: an anchor on the 31st restarts on the 28th in
// February. The NEXT boundary is still computed from the anchor's 31, not from
// the clamped 28, so a 31-anchored rule does not drift permanently to the 28th
// after one February.
func locateMonthlyCycle(anchor, now time.Time) usageMeterCycle {
	if now.Before(anchor) {
		end := nextMonthlyBoundary(anchor, anchor)
		return usageMeterCycle{
			start:      anchor,
			end:        end,
			periodDays: calendarDaySpan(anchor, end),
		}
	}

	// The boundary in NOW's month, not the anchor's: the anchor may be years
	// old, so its own month's boundary would sit far in the past.
	start := monthlyBoundaryIn(anchor, now.Year(), now.Month())
	if start.After(now) {
		// This month's boundary is still ahead of us (the anchor is the 5th and
		// today is the 1st), so the current cycle began in the previous month.
		prev := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -1, 0)
		start = monthlyBoundaryIn(anchor, prev.Year(), prev.Month())
	}
	end := nextMonthlyBoundary(anchor, start)

	periodDays := calendarDaySpan(start, end)
	dayInCycle := calendarDaySpan(start, now) + 1
	if dayInCycle < 1 {
		dayInCycle = 1
	}
	if dayInCycle > periodDays {
		dayInCycle = periodDays
	}
	return usageMeterCycle{
		start:      start,
		end:        end,
		dayInCycle: dayInCycle,
		periodDays: periodDays,
	}
}

// monthlyBoundaryIn returns the cycle boundary inside the given month, using
// the anchor's day-of-month and wall-clock time, clamped to that month's length.
func monthlyBoundaryIn(anchor time.Time, year int, month time.Month) time.Time {
	day := anchor.Day()
	if last := daysInMonth(year, month); day > last {
		day = last
	}
	return time.Date(year, month, day,
		anchor.Hour(), anchor.Minute(), anchor.Second(), anchor.Nanosecond(), anchor.Location())
}

// nextMonthlyBoundary returns the boundary one month after `start`, using the
// ANCHOR's day-of-month so a clamped month does not shift later boundaries.
func nextMonthlyBoundary(anchor, start time.Time) time.Time {
	next := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, start.Location()).AddDate(0, 1, 0)
	return monthlyBoundaryIn(anchor, next.Year(), next.Month())
}

// daysInMonth returns the number of days in the given month.
func daysInMonth(year int, month time.Month) int {
	// Day 0 of the NEXT month is the last day of this one; time.Date normalizes
	// month 13 to January of the next year.
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// calendarDaySpan counts the calendar days between two instants, comparing
// DATES rather than elapsed time.
//
// Monthly cycles are calendar-shaped, so a DST transition must not turn a
// one-day step into "0 days" (spring forward, 23h) or "2 days" (fall back,
// 25h). Normalizing to UTC midnight makes the subtraction exact.
func calendarDaySpan(from, to time.Time) int {
	fy, fm, fd := from.Date()
	ty, tm, td := to.Date()
	a := time.Date(fy, fm, fd, 0, 0, 0, 0, time.UTC)
	b := time.Date(ty, tm, td, 0, 0, 0, 0, time.UTC)
	return int(b.Sub(a) / (24 * time.Hour))
}

// ComputeUsageMeter aggregates the rule against the ledger.
//
// This is the ONLY implementation of the meter arithmetic. The handler, the
// worker and the turn-completion trigger all call it; a second implementation
// anywhere would drift from this one and show up as "the header says one thing
// and the notification says another".
func ComputeUsageMeter(ctx context.Context, now time.Time) (*UsageMeterStatus, error) {
	rule := model.ConfigInstance.UsageMeter
	if rule.Quota <= 0 {
		return &UsageMeterStatus{Enabled: false}, nil
	}
	if !model.IsValidUsageMeterMetric(rule.Metric) || !usageMeterPeriodUsable(rule) {
		// A malformed rule is reported as disabled rather than as an error: the
		// caller is a header badge, and a 500 there would be noise. The PATCH
		// validator rejects such rules, so this only happens for a hand-edited
		// config.yaml.
		slog.Warn("usage meter: rule is incomplete, treating as disabled",
			"metric", rule.Metric, "cycle_mode", rule.CycleMode,
			"period_days", rule.PeriodDays)
		return &UsageMeterStatus{Enabled: false}, nil
	}
	anchor, err := model.ParseUsageMeterAnchor(rule.AnchorDate)
	if err != nil {
		slog.Warn("usage meter: invalid anchor date, treating as disabled",
			"anchor", rule.AnchorDate, "error", err)
		return &UsageMeterStatus{Enabled: false}, nil
	}

	cycle := locateUsageMeterCycle(anchor, rule, now)
	st := &UsageMeterStatus{
		Enabled:    true,
		Metric:     rule.Metric,
		AnchorDate: rule.AnchorDate,
		// PeriodDays is the length of the CURRENT cycle, not the configured
		// one: in monthly mode that is 28-31 and varies month to month, and
		// every consumer ("day N of M", the chart's x axis, the per-day budget)
		// must agree with the arithmetic below.
		PeriodDays: cycle.periodDays,
		Quota:      rule.Quota,
		CycleStart: cycle.start.Format(time.RFC3339),
		CycleEnd:   cycle.end.Format(time.RFC3339),
		DayInCycle: cycle.dayInCycle,
	}
	if cycle.dayInCycle == 0 {
		// The anchor is in the future: nothing has been counted yet and there is
		// no budget to compare against.
		return st, nil
	}

	st.Expected = rule.Quota * float64(cycle.dayInCycle) / float64(cycle.periodDays)
	st.RemainingDays = cycle.periodDays - cycle.dayInCycle
	if st.RemainingDays < 0 {
		st.RemainingDays = 0
	}

	if !store.ReadDBReady() {
		return st, nil
	}

	rowExpr, _ := usageMeterRowExpr(rule.Metric)
	actual, err := sumUsageInCycle(ctx, rowExpr, cycle, now)
	if err != nil {
		return nil, err
	}
	st.Actual = actual

	series, err := queryUsageMeterSeries(ctx, rowExpr, cycle, now)
	if err != nil {
		return nil, err
	}
	st.Series = series

	st.ProjectedExhaustDay = projectExhaustDay(st.Actual, rule, cycle, now)

	return st, nil
}

// usageMeterPeriodUsable reports whether the rule carries the cycle length its
// mode needs.
//
// Only "fixed" needs period_days. A "monthly" rule derives its length from the
// calendar, so requiring the field would disable a perfectly valid monthly rule
// that was configured from a fresh install (where period_days starts at 0
// because the settings page hides the field). ApplyDefaults normally fills it
// in, but a rule can reach here straight after a PATCH that ran before the
// defaults were re-applied.
func usageMeterPeriodUsable(rule model.UsageMeterConfig) bool {
	if model.NormalizeUsageMeterCycleMode(rule.CycleMode) == model.UsageMeterCycleMonthly {
		return true
	}
	return rule.PeriodDays > 0
}

// sumUsageInCycle totals one metric over the cycle window.
func sumUsageInCycle(ctx context.Context, rowExpr string, cycle usageMeterCycle, now time.Time) (float64, error) {
	var actual float64
	err := store.ReadDB().QueryRowContext(ctx,
		"SELECT COALESCE(SUM("+rowExpr+"), 0) FROM chat_metadata m "+
			"WHERE m.created_at >= ? AND m.created_at < ?",
		cycle.start.UTC().Format("2006-01-02 15:04:05"),
		now.UTC().Format("2006-01-02 15:04:05"),
	).Scan(&actual)
	if err != nil {
		return 0, fmt.Errorf("usage meter total: %w", err)
	}
	return actual, nil
}

// projectExhaustDay estimates which day of the cycle the quota runs out on.
//
// Rate-based: how many days of the cycle the remaining quota lasts at the pace
// observed so far. Measured against elapsed time rather than whole days so day
// 1 does not divide by one day of usage that has only been accumulating for an
// hour. Returns 0 when no projection is possible or the quota outlasts the
// cycle.
func projectExhaustDay(actual float64, rule model.UsageMeterConfig, cycle usageMeterCycle, now time.Time) int {
	elapsedHours := now.Sub(cycle.start).Hours()
	if actual <= 0 || elapsedHours <= 0 {
		return 0
	}
	ratePerDay := actual / (elapsedHours / 24)
	if ratePerDay <= 0 {
		return 0
	}
	// cycle.periodDays, not rule.PeriodDays: in monthly mode the cycle length
	// follows the calendar, and clamping to the configured 30 would report "day
	// 30" for a 31-day month.
	periodDays := cycle.periodDays
	daysToExhaust := rule.Quota / ratePerDay
	if daysToExhaust > float64(periodDays) {
		return 0
	}
	day := int(math.Ceil(daysToExhaust))
	if day < cycle.dayInCycle {
		// Already past the quota: the "projected" day is today.
		day = cycle.dayInCycle
	}
	if day > periodDays {
		day = periodDays
	}
	return day
}

// queryUsageMeterSeries returns the cumulative actual-vs-expected curve for the
// cycle, oldest first.
//
// Day attribution is done in Go from the RAW timestamp, not by SQL date
// bucketing. `chat_metadata.created_at` is UTC, while the cycle boundary is
// local midnight, so a single UTC day can span two local days (and vice versa).
// Grouping in SQL — by `date(created_at)` or by `date(created_at,'localtime')` —
// forces each row onto one calendar day and shifts usage across the boundary.
//
// The per-row scan is bounded by the cycle window (at most a year of rows), and
// the query only selects two columns, so the cost is a linear pass over one
// cycle's ledger rows.
func queryUsageMeterSeries(ctx context.Context, rowExpr string, cycle usageMeterCycle, now time.Time) ([]UsageMeterPoint, error) {
	rows, err := store.ReadDB().QueryContext(ctx,
		"SELECT m.created_at, "+rowExpr+" AS v "+
			"FROM chat_metadata m WHERE m.created_at >= ? AND m.created_at < ?",
		cycle.start.UTC().Format("2006-01-02 15:04:05"),
		now.UTC().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return nil, fmt.Errorf("usage meter series: %w", err)
	}
	defer func() { _ = rows.Close() }()

	daily := make(map[string]float64)
	for rows.Next() {
		var createdAt string
		var v float64
		if err := rows.Scan(&createdAt, &v); err != nil {
			return nil, fmt.Errorf("usage meter series scan: %w", err)
		}
		// The column is a TEXT DATETIME. Production writes it via
		// CURRENT_TIMESTAMP ("2006-01-02 15:04:05", UTC); tests and imports may
		// supply RFC3339. Accept both rather than silently dropping a row, which
		// would make the curve disagree with the aggregate total.
		t, perr := parseUsageMeterTimestamp(createdAt)
		if perr != nil {
			slog.Warn("usage meter: unparsable created_at, row omitted from series",
				"created_at", createdAt, "error", perr)
			continue
		}
		daily[t.Local().Format("2006-01-02")] += v
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage meter series rows: %w", err)
	}

	rule := model.ConfigInstance.UsageMeter
	// The baseline is spread over THIS cycle's length. In monthly mode that is
	// the calendar month's length, not the configured period_days — otherwise a
	// 31-day month's curve would reach the quota on day 30 and then keep
	// climbing past it for the rest of the month.
	perDay := rule.Quota / float64(cycle.periodDays)

	// Build the curve from the buckets that actually exist, then fill the gaps.
	//
	// The day list is NOT derived from `cycle.dayInCycle` by calendar
	// arithmetic. `dayInCycle` counts whole 24h spans from the anchor (so the
	// cycle length stays constant across DST), while `AddDate` counts calendar
	// days. Across a spring-forward transition those two disagree by an hour,
	// and enumerating with AddDate then either drops the current day's bucket or
	// repeats the previous one — the curve's last point stops matching the
	// aggregate total. Walking the sorted bucket keys instead makes
	// `sum(series) == actual` hold by construction, whatever the offsets do.
	keys := make([]string, 0, len(daily))
	for k := range daily {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// The cycle covers [start, now); a bucket can only fall inside it, but clamp
	// anyway so a stray row (clock skew, a hand-edited row) cannot extend the
	// axis past the cycle.
	firstKey := cycle.start.In(time.Local).Format("2006-01-02")
	lastKey := now.In(time.Local).Format("2006-01-02")

	points := make([]UsageMeterPoint, 0, len(keys))
	var cumulative float64
	// A bucket that has no usage still needs a point so the curve is continuous
	// (a flat segment, not a gap). Walk calendar days from the cycle start to
	// today, pulling each day's bucket — but iterate the day axis by calendar
	// date while summing whatever buckets land in it.
	day := cycle.start.In(time.Local)
	endDay := now.In(time.Local)
	for !day.After(endDay) {
		key := day.Format("2006-01-02")
		if key >= firstKey && key <= lastKey {
			cumulative += daily[key]
			points = append(points, UsageMeterPoint{
				Day:      key,
				Actual:   cumulative,
				Expected: perDay * float64(len(points)+1),
			})
		}
		day = day.AddDate(0, 0, 1)
	}

	// Any bucket the day walk could not reach (a date before the cycle start in
	// local terms, e.g. an anchor whose local midnight maps to the previous UTC
	// day) is appended so no usage is silently dropped from the curve.
	seen := make(map[string]bool, len(points))
	for _, p := range points {
		seen[p.Day] = true
	}
	for _, k := range keys {
		if seen[k] {
			continue
		}
		cumulative += daily[k]
		points = append(points, UsageMeterPoint{
			Day:      k,
			Actual:   cumulative,
			Expected: perDay * float64(len(points)+1),
		})
	}
	return points, nil
}

// usageMeterStageToSend decides whether this cycle needs a notification.
//
// Rules:
//   - Reaching the quota wins over crossing the baseline: on the last day
//     expected == quota, so both conditions are true at once and sending two
//     near-identical notifications would be noise.
//   - Each stage fires at most once per cycle, and never moves backwards.
//   - `actual > expected` is strict: expected is exactly "you may have used
//     this much", so equality is still on plan.
func usageMeterStageToSend(st *UsageMeterStatus, notifiedStage string) string {
	if !st.Enabled || st.DayInCycle <= 0 || st.Quota <= 0 {
		return ""
	}
	if st.Actual >= st.Quota && notifiedStage != UsageMeterStageFull {
		return UsageMeterStageFull
	}
	if st.Actual > st.Expected && notifiedStage == "" {
		return UsageMeterStageOver
	}
	return ""
}

// CheckUsageMeter computes the meter and emits a notification when the cycle
// crossed a threshold. Returns the stage that was notified ("" for none).
//
// Called from the periodic worker and from the turn-completion path, so it must
// be cheap when there is nothing to do (disabled rule → one config read) and
// must never panic on a missing DB.
func CheckUsageMeter(ctx context.Context) string {
	if model.ConfigInstance.UsageMeter.Quota <= 0 {
		// No rule: no threshold to cross, so no notification. The capsule still
		// has to LEARN that the rule went away, though — without this the client
		// keeps rendering the last numbers it was pushed (or never learns the
		// meter was turned off) until something else makes it refetch. The user
		// sees a stale capsule that only corrects itself when they click it.
		//
		// Broadcast the disabled state once. It is cheap (no aggregate query) and
		// idempotent: broadcastUsageMeterNumbers compares against the last
		// broadcast, so repeated ticks after the transition send nothing.
		broadcastDisabledUsageMeter()
		return ""
	}

	// Throttle: a drain can complete several turns in a row, and each would
	// otherwise run two aggregate queries.
	usageMeterLastCheckMu.Lock()
	if !usageMeterLastCheck.IsZero() && time.Since(usageMeterLastCheck) < usageMeterMinInterval {
		usageMeterLastCheckMu.Unlock()
		return ""
	}
	usageMeterLastCheck = time.Now()
	usageMeterLastCheckMu.Unlock()

	usageMeterCheckMu.Lock()
	defer usageMeterCheckMu.Unlock()

	now := usageMeterNow()
	st, err := ComputeUsageMeter(ctx, now)
	if err != nil {
		slog.Warn("usage meter: compute failed", "error", err)
		return ""
	}
	if !st.Enabled {
		return ""
	}

	// Re-read the rule AFTER the aggregate query: the user may have edited it
	// (or the cycle may have rolled) while we were querying. Reading it once at
	// the start would let a stale rule decide whether to notify.
	rule := model.ConfigInstance.UsageMeter
	notifiedStage := rule.NotifiedStage
	// A cycle start that differs from the stored one means the cycle rolled
	// over (or the rule was just changed): re-arm from scratch.
	if rule.NotifiedCycleStart != st.CycleStart {
		notifiedStage = ""
	}

	// The capsule is a live readout: it must track usage as it lands, not only
	// when a threshold is crossed. Broadcast the headline numbers whenever they
	// changed since the last broadcast — this is a DATA update, not a
	// notification, so it is not gated on push_mode and is never persisted.
	broadcastUsageMeterNumbers(st)

	stage := usageMeterStageToSend(st, notifiedStage)
	if stage == "" {
		return ""
	}

	title, body := renderUsageMeterMessage(stage, st)
	eventID := ws.GenerateEventID()

	RecordUsageNotification(eventID, stage, title, body)

	// WS broadcast + pending-event write-ahead. Gated on push_mode like the
	// other notification paths: "disabled" means the user turned notifications
	// off entirely, and the notification-center row above is unaffected.
	if model.ConfigInstance.PushMode != model.PushModeDisabled {
		broadcastUsageMeterStage(eventID, stage, title, body, st)
	}

	// IM channels. PushForgeEvent-style: transport only, wording comes from here.
	// A successful IM push deletes the pending event so a later Android
	// reconnect does not raise a second notification for the same crossing.
	if dingtalk.IsStarted() && dingtalk.PushUsageMeterEvent(title, body) {
		_ = DeletePendingEvent(eventID)
	} else if feishu.IsStarted() && feishu.PushUsageMeterEvent(title, body) {
		_ = DeletePendingEvent(eventID)
	}

	// Update the in-memory rule FIRST: memory is the authority for "already
	// told you", and the next check reads it. Relying on the saver alone would
	// re-send on every check whenever the saver is nil or its write fails
	// (read-only config dir, full disk) — a duplicate every 5 minutes.
	//
	// Safe without configMutex: this goroutine already holds usageMeterCheckMu,
	// and the only concurrent writer of these two fields is the PATCH path
	// (which also clears them). A lost update there is benign — the user asked
	// for a new rule, so re-notifying is the intended outcome.
	model.ConfigInstance.UsageMeter.NotifiedCycleStart = st.CycleStart
	model.ConfigInstance.UsageMeter.NotifiedStage = stage

	// Persist the new stage last: if the process dies between the push and this
	// write, the next check re-sends rather than silently swallowing it. A
	// duplicate notification is the benign failure direction here.
	if usageMeterStateSaver != nil {
		if err := usageMeterStateSaver(st.CycleStart, stage); err != nil {
			slog.Warn("usage meter: failed to persist notification state", "error", err)
		}
	}
	return stage
}

// broadcastUsageMeterStage pushes the crossing to connected clients and to the
// pending-event log.
func broadcastUsageMeterStage(eventID, stage, title, body string, st *UsageMeterStatus) {
	payload := map[string]any{
		"status": stage,
		"title":  title,
		"body":   body,
		// The numbers travel with the event so a client can render the
		// capsule without a follow-up request.
		"metric":   st.Metric,
		"actual":   st.Actual,
		"expected": st.Expected,
		"quota":    st.Quota,
	}
	msg := ws.ServerMessage{
		Type:  ws.MessageTypeEvent,
		ID:    eventID,
		Event: UsageMeterEventType,
		Data:  payload,
	}
	StoreNotifiableEvent(msg)
	if mgr := ws.GetManager(); mgr != nil {
		mgr.BroadcastEvent(msg)
	}
}

// renderUsageMeterMessage localizes the notification text at write time.
//
// Stored as rendered text (not an i18n key) on purpose: a notification records
// what the user was told at that moment. Switching the UI language later must
// not rewrite history — that is also why the usage kind's title differs from
// the session/task kinds, whose titles are data (a session name).
func renderUsageMeterMessage(stage string, st *UsageMeterStatus) (string, string) {
	loc := i18n.LocalizerForLocale(model.Language)
	unit := usageMeterUnitSuffix(st.Metric)
	data := map[string]any{
		"Actual":   formatUsageMeterValue(st.Metric, st.Actual) + unit,
		"Expected": formatUsageMeterValue(st.Metric, st.Expected) + unit,
		"Quota":    formatUsageMeterValue(st.Metric, st.Quota) + unit,
		"Day":      st.DayInCycle,
		"Days":     st.PeriodDays,
	}
	if stage == UsageMeterStageFull {
		return i18n.T(loc, "UsageMeterFullTitle"),
			i18n.T(loc, "UsageMeterFullBody", data)
	}
	return i18n.T(loc, "UsageMeterOverTitle"),
		i18n.T(loc, "UsageMeterOverBody", data)
}

// usageMeterUnitSuffix returns the display unit for a metric. Cost is always
// reported in USD: chat_metadata stores a single cost column and no currency, so
// a non-USD amount is summed as-is and labeled as such rather than converted.
func usageMeterUnitSuffix(metric string) string {
	switch metric {
	case model.UsageMeterMetricTokens:
		return ""
	case model.UsageMeterMetricCredit:
		return " credits"
	default:
		return ""
	}
}

// formatUsageMeterValue renders a metric value for notification text. Compact
// tiers keep the sentence short; the UI uses its own formatter for the capsule.
func formatUsageMeterValue(metric string, v float64) string {
	switch metric {
	case model.UsageMeterMetricTokens:
		return compactCount(v)
	case model.UsageMeterMetricCredit:
		return fmt.Sprintf("%.2f", v)
	default:
		return fmt.Sprintf("$%.2f", v)
	}
}

// compactCount renders a count with K/M/B tiers, matching the thresholds used
// by the frontend formatter (web/src/utils/usageFormat.ts).
func compactCount(v float64) string {
	n := math.Round(v)
	switch {
	case n >= 999_500_000:
		return fmt.Sprintf("%.1fB", n/1e9)
	case n >= 999_950:
		return fmt.Sprintf("%.1fM", n/1e6)
	case n >= 999.5:
		return fmt.Sprintf("%.1fK", n/1e3)
	default:
		return fmt.Sprintf("%.0f", n)
	}
}

// parseUsageMeterTimestamp accepts the two shapes a DATETIME column can hold:
// SQLite's CURRENT_TIMESTAMP text (space-separated, UTC) and RFC3339.
//
// Both are interpreted as UTC when they carry no zone — that is how the write
// path stores them, and treating them as local would shift every row by the
// server's offset.
func parseUsageMeterTimestamp(s string) (time.Time, error) {
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
	} {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized timestamp %q", s)
}

// UsageMeterTickEventType is the WS event that says "the meter's numbers moved".
//
// Deliberately separate from UsageMeterEventType (usage_meter_update): that one
// means "a threshold was crossed, tell the user", so every consumer raises a
// notification for it. Reusing it here would pop a system notification after
// every completed turn. This event only refreshes the header capsule.
const UsageMeterTickEventType = "usage_meter_tick"

// Last broadcast numbers, so a no-op check does not spam every client. Guarded
// by usageMeterCheckMu (callers hold it), which is also what keeps the
// broadcast ordered with respect to the threshold event.
var usageMeterLastBroadcast struct {
	actual, expected, quota float64
	metric                  string
	// enabled is part of the change key: turning the rule off must re-broadcast
	// even though every number stays the same, because it changes what the
	// client renders (capsule vs placeholder).
	enabled bool
	valid   bool
}

// broadcastUsageMeterNumbers pushes the headline numbers to every client when
// they differ from the last broadcast, and reports whether it did. Caller must
// hold usageMeterCheckMu.
//
// The series is NOT included: it is only needed by the popover, which refetches
// on open, and shipping it every turn would be a large frame for no benefit.
func broadcastUsageMeterNumbers(st *UsageMeterStatus) bool {
	last := usageMeterLastBroadcast
	if last.valid &&
		last.actual == st.Actual &&
		last.expected == st.Expected &&
		last.quota == st.Quota &&
		last.metric == st.Metric &&
		last.enabled == st.Enabled {
		return false
	}
	// Record BEFORE looking up the manager: if no manager is wired (tests, or
	// the window before the hub starts) the numbers still advanced, and not
	// recording them would make every later check look like a change.
	usageMeterLastBroadcast = struct {
		actual, expected, quota float64
		metric                  string
		enabled                 bool
		valid                   bool
	}{st.Actual, st.Expected, st.Quota, st.Metric, st.Enabled, true}

	mgr := ws.GetManager()
	if mgr == nil {
		return true
	}

	// `enabled` travels with the payload: it is what tells the client to drop
	// the numbers and render the placeholder instead. Without it a disabled
	// frame is indistinguishable from a metered one whose figures happen to be
	// zero, and the capsule would keep showing the old quota.
	mgr.BroadcastEvent(ws.ServerMessage{
		Type:  ws.MessageTypeEvent,
		Event: UsageMeterTickEventType,
		Data: map[string]any{
			"enabled":  st.Enabled,
			"metric":   st.Metric,
			"actual":   st.Actual,
			"expected": st.Expected,
			"quota":    st.Quota,
			"day":      st.DayInCycle,
			"days":     st.PeriodDays,
		},
	})
	return true
}

// broadcastDisabledUsageMeter pushes the "no rule" state to every client.
//
// Called when the rule is turned off (quota <= 0). The capsule has to be told,
// or it keeps showing the quota it was last pushed until something else makes
// the client refetch — in practice, the user clicking the capsule.
//
// Deliberately carries no numbers: the client replaces the whole readout with
// the placeholder, and sending stale figures would let a stale value survive a
// client that only patches the fields it receives.
func broadcastDisabledUsageMeter() {
	st := &UsageMeterStatus{Enabled: false}
	usageMeterCheckMu.Lock()
	defer usageMeterCheckMu.Unlock()
	broadcastUsageMeterNumbers(st)
}
