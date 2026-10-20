package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestIsValidUsageMeterMetric pins the accepted vocabulary.
//
// The empty string is deliberately INVALID: ApplyDefaults fills the metric only
// when a quota exists, so "" means "unset" and must be rejected by callers that
// validate a rule. Accepting it would let a hand-edited config through with no
// metric to aggregate.
func TestIsValidUsageMeterMetric(t *testing.T) {
	for _, m := range []string{UsageMeterMetricTokens, UsageMeterMetricCredit, UsageMeterMetricCost} {
		require.True(t, IsValidUsageMeterMetric(m), "%q must be valid", m)
	}
	for _, m := range []string{"", "usd", "TOKENS", " token", "cost "} {
		require.False(t, IsValidUsageMeterMetric(m), "%q must be rejected", m)
	}
}
