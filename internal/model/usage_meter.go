package model

import "time"

// Usage-meter metric identifiers. Kept in the model package because both the
// config layer (validation, defaults) and the service layer (SQL selection)
// need the same vocabulary; the frontend mirrors these strings.
const (
	UsageMeterMetricTokens = "tokens"
	UsageMeterMetricCredit = "credit"
	UsageMeterMetricCost   = "cost"
)

// MaxUsageMeterPeriodDays caps the cycle length. It matches the usage-stats
// query's own range cap (service.maxRangeDays = 370) — a cycle longer than that
// could not be aggregated in a single query, so allowing it would only produce
// a rule that always errors.
const MaxUsageMeterPeriodDays = 366

// UsageMeterAnchorLayouts are the accepted shapes of UsageMeterConfig.AnchorDate,
// most specific first.
//
// The date-only form is what every existing install has, so it must keep
// working unchanged (and keeps its documented "00:00 local" meaning). The
// date-time forms let a user pin the cycle to an arbitrary instant — which is
// what you need when a billing period starts mid-day, or when you want the
// rollover to happen at a time you are awake to see it.
//
// Deliberately NO zone suffix: the value is a wall-clock reading in the
// server's local timezone, exactly like cron expressions. Accepting an offset
// would make the cycle boundary move when the server's TZ changes, and two
// rules that looked identical in the UI would roll over at different instants.
var UsageMeterAnchorLayouts = []string{
	"2006-01-02T15:04:05",
	"2006-01-02T15:04",
	"2006-01-02",
}

// ParseUsageMeterAnchor parses AnchorDate in the server's local timezone.
//
// This is the ONLY parser for the anchor: the PATCH validator, the service
// arithmetic and the worker all call it, so "what the settings page accepts" and
// "what the meter computes" cannot drift apart.
func ParseUsageMeterAnchor(s string) (time.Time, error) {
	var lastErr error
	for _, layout := range UsageMeterAnchorLayouts {
		t, err := time.ParseInLocation(layout, s, time.Local)
		if err == nil {
			return t, nil
		}
		lastErr = err
	}
	return time.Time{}, lastErr
}

// FormatUsageMeterAnchor renders an instant in the canonical anchor form.
//
// Midnight is written date-only so a rule that never had a time component keeps
// the short form (and its config.yaml stays byte-identical across a save/load
// cycle). Any other instant keeps the seconds, which is the precision the
// settings page can edit.
func FormatUsageMeterAnchor(t time.Time) string {
	if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0 {
		return t.Format("2006-01-02")
	}
	return t.Format("2006-01-02T15:04:05")
}

// IsValidUsageMeterMetric reports whether m is one of the three supported
// metrics. The empty string is NOT valid here: callers that want "unset" must
// check for "" first (ApplyDefaults fills it only when a quota exists).
func IsValidUsageMeterMetric(m string) bool {
	switch m {
	case UsageMeterMetricTokens, UsageMeterMetricCredit, UsageMeterMetricCost:
		return true
	}
	return false
}
