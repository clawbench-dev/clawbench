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

// Usage-meter cycle modes. They decide how a cycle rolls over, and therefore
// how "how much may I have spent by now" is derived.
const (
	// UsageMeterCycleFixed rolls every PeriodDays days from the anchor.
	UsageMeterCycleFixed = "fixed"
	// UsageMeterCycleMonthly rolls on the anchor's day-of-month. The anchor's
	// time of day still applies, so "the 5th at 09:00" means the cycle restarts
	// at 09:00 on the 5th of every month.
	UsageMeterCycleMonthly = "monthly"
)

// MaxUsageMeterPeriodDays caps the cycle length. It matches the usage-stats
// query's own range cap (service.maxRangeDays = 370) — a cycle longer than that
// could not be aggregated in a single query, so allowing it would only produce
// a rule that always errors.
const MaxUsageMeterPeriodDays = 366

// NormalizeUsageMeterCycleMode maps the stored value onto a known mode.
//
// Empty means "fixed": every install written before cycle_mode existed has an
// empty value, and its behavior must not change. An unrecognized value is also
// treated as fixed rather than rejected here — the PATCH validator rejects it,
// so this only covers a hand-edited config.yaml, where falling back to the
// historical behavior is safer than refusing to compute anything.
func NormalizeUsageMeterCycleMode(mode string) string {
	if mode == UsageMeterCycleMonthly {
		return UsageMeterCycleMonthly
	}
	return UsageMeterCycleFixed
}

// IsValidUsageMeterCycleMode reports whether mode is a supported cycle mode.
// The empty string IS valid here (it means "fixed"), unlike Metric, because it
// is the documented value of every pre-existing install.
func IsValidUsageMeterCycleMode(mode string) bool {
	switch mode {
	case "", UsageMeterCycleFixed, UsageMeterCycleMonthly:
		return true
	}
	return false
}

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

// UsageMeterConfigured reports whether the rule carries anything at all.
//
// The meter no longer needs a quota to be useful: an anchor alone means "how
// much have I spent since then", a quota alone means "against this much in
// total". So "is there anything to show" is the OR of the fields the user can
// fill, and only a rule with none of them has nothing to say.
//
// Metric is deliberately excluded: it defaults to cost and therefore says
// nothing about whether the user configured the feature.
func UsageMeterConfigured(rule UsageMeterConfig) bool {
	return rule.AnchorDate != "" || rule.Quota > 0 || rule.PeriodDays > 0
}

// IsValidUsageMeterMetric reports whether m is one of the three supported
// metrics. The empty string is NOT valid here: callers that want "unset" must
// check for "" first (ApplyDefaults fills it only when the rule is configured).
func IsValidUsageMeterMetric(m string) bool {
	switch m {
	case UsageMeterMetricTokens, UsageMeterMetricCredit, UsageMeterMetricCost:
		return true
	}
	return false
}
