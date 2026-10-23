package model

import (
	"strconv"
	"testing"
	"time"

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

// The anchor accepts a date with an optional time of day. The date-only form
// must keep meaning local midnight — every pre-existing install stores that
// shape, and a changed meaning would silently move every cycle boundary.
func TestParseUsageMeterAnchor(t *testing.T) {
	valid := []struct {
		name string
		in   string
		want time.Time
	}{
		{"date only is local midnight", "2026-10-05", time.Date(2026, 10, 5, 0, 0, 0, 0, time.Local)},
		{"date and time", "2026-10-05T14:30:00", time.Date(2026, 10, 5, 14, 30, 0, 0, time.Local)},
		{"date and hour/minute only", "2026-10-05T14:30", time.Date(2026, 10, 5, 14, 30, 0, 0, time.Local)},
		{"midnight written with a time", "2026-10-05T00:00:00", time.Date(2026, 10, 5, 0, 0, 0, 0, time.Local)},
		{"end of day", "2026-10-05T23:59:59", time.Date(2026, 10, 5, 23, 59, 59, 0, time.Local)},
	}
	for _, tc := range valid {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseUsageMeterAnchor(tc.in)
			require.NoError(t, err)
			require.True(t, tc.want.Equal(got), "got %v, want %v", got, tc.want)
			// Local, not UTC: the anchor is a wall-clock reading, so the same
			// string must resolve to the same local time on any host.
			require.Equal(t, time.Local, got.Location())
		})
	}

	// A zone suffix must be rejected. Accepting one would let two rules that look
	// identical in the UI roll over at different instants, and would make the
	// boundary depend on the server's TZ.
	invalid := []string{
		"", "not-a-date", "10/05/2026", "2026-13-45",
		"2026-10-05T14:30:00Z", "2026-10-05T14:30:00+08:00",
		"2026-10-05 14:30:00", "2026-10-05T25:00:00", "2026-10-05T14:70:00",
	}
	for _, in := range invalid {
		t.Run("rejects "+in, func(t *testing.T) {
			_, err := ParseUsageMeterAnchor(in)
			require.Error(t, err, "%q must be rejected", in)
		})
	}
}

// Midnight must round-trip to the SHORT form so an untouched rule's
// config.yaml stays byte-identical across a save/load cycle (a rewrite that
// added "T00:00:00" would churn the file on every settings save).
func TestFormatUsageMeterAnchor(t *testing.T) {
	cases := []struct {
		name string
		in   time.Time
		want string
	}{
		{"midnight collapses to date", time.Date(2026, 10, 5, 0, 0, 0, 0, time.Local), "2026-10-05"},
		{"a time is kept", time.Date(2026, 10, 5, 14, 30, 0, 0, time.Local), "2026-10-05T14:30:00"},
		{"seconds are kept", time.Date(2026, 10, 5, 14, 30, 45, 0, time.Local), "2026-10-05T14:30:45"},
		{"one second past midnight is not midnight", time.Date(2026, 10, 5, 0, 0, 1, 0, time.Local), "2026-10-05T00:00:01"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, FormatUsageMeterAnchor(tc.in))
		})
	}

	// Round-trip: whatever we write must parse back to the same instant.
	for _, h := range []int{0, 1, 12, 23} {
		t.Run("round trip hour "+strconv.Itoa(h), func(t *testing.T) {
			orig := time.Date(2026, 10, 5, h, 30, 0, 0, time.Local)
			got, err := ParseUsageMeterAnchor(FormatUsageMeterAnchor(orig))
			require.NoError(t, err)
			require.True(t, orig.Equal(got), "round trip changed %v into %v", orig, got)
		})
	}
}
