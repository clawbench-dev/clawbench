package forge

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestParseNumberQuery pins the boundary between "open this item" and "search
// for this text": a query only counts as a number when nothing but digits
// (optionally prefixed by # or !) remains. Anything looser would hijack real
// searches — e.g. "2fa bug" or "2026 roadmap" must stay text searches.
func TestParseNumberQuery(t *testing.T) {
	cases := []struct {
		query string
		want  int
		ok    bool
	}{
		{"123", 123, true},
		{"#123", 123, true},
		{"!123", 123, true},
		{"  #123  ", 123, true},
		{"1", 1, true},
		{"0", 0, false},
		{"#0", 0, false},
		{"-1", 0, false},
		{"", 0, false},
		{"#", 0, false},
		{"#!1", 0, false},
		{"12a", 0, false},
		{"2fa bug", 0, false},
		{"2026 roadmap", 0, false},
		{"#123 title", 0, false},
		{"fix crash", 0, false},
		// Out of int range must not wrap into a valid number.
		{"99999999999999999999999", 0, false},
	}
	for _, c := range cases {
		got, ok := ParseNumberQuery(c.query)
		assert.Equal(t, c.ok, ok, "query %q: ok", c.query)
		assert.Equal(t, c.want, got, "query %q: number", c.query)
	}
}
