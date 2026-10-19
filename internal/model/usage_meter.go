package model

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
