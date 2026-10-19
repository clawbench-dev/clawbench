package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
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
	// without a second request to /api/config.
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
// differences. A DST transition day is 23 or 25 hours long, so a calendar
// difference would make "day 10 of 30" mean different amounts of elapsed time
// on either side of the switch. With fixed spans the cycle length is constant;
// the price is that after a DST change the boundary sits one hour off local
// midnight (twice a year, invisible in practice).
type usageMeterCycle struct {
	start       time.Time
	end         time.Time
	dayInCycle  int // 1-based; 0 when now < anchor
	periodIndex int
}

func locateUsageMeterCycle(anchor time.Time, periodDays int, now time.Time) usageMeterCycle {
	day := 24 * time.Hour
	elapsed := now.Sub(anchor)
	if elapsed < 0 {
		return usageMeterCycle{start: anchor, end: anchor.Add(time.Duration(periodDays) * day)}
	}
	elapsedDays := int(elapsed / day)
	periodIndex := elapsedDays / periodDays
	start := anchor.Add(time.Duration(periodIndex*periodDays) * day)
	return usageMeterCycle{
		start:       start,
		end:         start.Add(time.Duration(periodDays) * day),
		dayInCycle:  elapsedDays - periodIndex*periodDays + 1,
		periodIndex: periodIndex,
	}
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
	if !model.IsValidUsageMeterMetric(rule.Metric) || rule.PeriodDays <= 0 {
		// A malformed rule is reported as disabled rather than as an error: the
		// caller is a header badge, and a 500 there would be noise. The PATCH
		// validator rejects such rules, so this only happens for a hand-edited
		// config.yaml.
		slog.Warn("usage meter: rule is incomplete, treating as disabled",
			"metric", rule.Metric, "period_days", rule.PeriodDays)
		return &UsageMeterStatus{Enabled: false}, nil
	}
	anchor, err := time.ParseInLocation("2006-01-02", rule.AnchorDate, time.Local)
	if err != nil {
		slog.Warn("usage meter: invalid anchor date, treating as disabled",
			"anchor", rule.AnchorDate, "error", err)
		return &UsageMeterStatus{Enabled: false}, nil
	}

	cycle := locateUsageMeterCycle(anchor, rule.PeriodDays, now)
	st := &UsageMeterStatus{
		Enabled:    true,
		Metric:     rule.Metric,
		AnchorDate: rule.AnchorDate,
		PeriodDays: rule.PeriodDays,
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

	st.Expected = rule.Quota * float64(cycle.dayInCycle) / float64(rule.PeriodDays)
	st.RemainingDays = rule.PeriodDays - cycle.dayInCycle
	if st.RemainingDays < 0 {
		st.RemainingDays = 0
	}

	if !store.ReadDBReady() {
		return st, nil
	}

	rowExpr, _ := usageMeterRowExpr(rule.Metric)
	startArg := cycle.start.UTC().Format("2006-01-02 15:04:05")
	endArg := now.UTC().Format("2006-01-02 15:04:05")

	if err := store.ReadDB().QueryRowContext(ctx,
		"SELECT COALESCE(SUM("+rowExpr+"), 0) FROM chat_metadata m "+
			"WHERE m.created_at >= ? AND m.created_at < ?",
		startArg, endArg,
	).Scan(&st.Actual); err != nil {
		return nil, fmt.Errorf("usage meter total: %w", err)
	}

	series, err := queryUsageMeterSeries(ctx, rowExpr, cycle, now)
	if err != nil {
		return nil, err
	}
	st.Series = series

	// Rate-based projection: how many days of the cycle the remaining quota
	// lasts at the pace observed so far. Measured against elapsed time rather
	// than whole days so day 1 does not divide by one day of usage that has
	// only been accumulating for an hour.
	elapsedHours := now.Sub(cycle.start).Hours()
	if st.Actual > 0 && elapsedHours > 0 {
		ratePerDay := st.Actual / (elapsedHours / 24)
		if ratePerDay > 0 {
			daysToExhaust := rule.Quota / ratePerDay
			if daysToExhaust <= float64(rule.PeriodDays) {
				day := int(math.Ceil(daysToExhaust))
				if day < cycle.dayInCycle {
					// Already past the quota: the "projected" day is today.
					day = cycle.dayInCycle
				}
				if day > rule.PeriodDays {
					day = rule.PeriodDays
				}
				st.ProjectedExhaustDay = day
			}
		}
	}

	return st, nil
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
	perDay := rule.Quota / float64(rule.PeriodDays)

	points := make([]UsageMeterPoint, 0, cycle.dayInCycle)
	var cumulative float64
	// Iterate whole days from the cycle start through today. Days with no rows
	// still appear so the curve is continuous (a flat segment, not a gap).
	for day := 1; day <= cycle.dayInCycle; day++ {
		date := cycle.start.AddDate(0, 0, day-1)
		key := date.Format("2006-01-02")
		cumulative += daily[key]
		points = append(points, UsageMeterPoint{
			Day:      key,
			Actual:   cumulative,
			Expected: perDay * float64(day),
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
	if model.ConfigInstance.PushMode != "disabled" {
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

	// IM channels. PushForgeEvent-style: transport only, wording comes from here.
	// A successful IM push deletes the pending event so a later Android
	// reconnect does not raise a second notification for the same crossing.
	if dingtalk.IsStarted() && dingtalk.PushUsageMeterEvent(title, body) {
		_ = DeletePendingEvent(eventID)
	} else if feishu.IsStarted() && feishu.PushUsageMeterEvent(title, body) {
		_ = DeletePendingEvent(eventID)
	}

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
// a non-USD amount is summed as-is and labelled as such rather than converted.
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

// usageMeterNeedsCheck reports whether a check could possibly do anything.
// Used by the worker to skip the aggregate query when the rule is off.
func usageMeterNeedsCheck() bool {
	return model.ConfigInstance.UsageMeter.Quota > 0
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
