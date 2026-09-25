package model

import "testing"

// TestNormalizeDirection pins the defaulting contract: only the explicit
// reverse value is preserved, and everything else — including the empty string
// that old rows carry — collapses to forward. Callers rely on this to treat a
// legacy port entry as a normal ssh -L mapping.
func TestNormalizeDirection(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"reverse is preserved", DirectionReverse, DirectionReverse},
		{"forward is preserved", DirectionForward, DirectionForward},
		{"empty defaults to forward", "", DirectionForward},
		{"unknown defaults to forward", "sideways", DirectionForward},
		{"case-sensitive: REVERSE is not a direction", "REVERSE", DirectionForward},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeDirection(tc.input); got != tc.want {
				t.Errorf("NormalizeDirection(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestForwardedPort_IsReverse verifies the predicate keys off the exact reverse
// constant: a port left at its zero value (no direction set) is a forward
// mapping, which is what every pre-existing row is.
func TestForwardedPort_IsReverse(t *testing.T) {
	cases := []struct {
		name      string
		direction string
		want      bool
	}{
		{"zero value is forward", "", false},
		{"forward", DirectionForward, false},
		{"reverse", DirectionReverse, true},
		{"unknown is not reverse", "sideways", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &ForwardedPort{Direction: tc.direction}
			if got := p.IsReverse(); got != tc.want {
				t.Errorf("IsReverse() with direction %q = %v, want %v", tc.direction, got, tc.want)
			}
		})
	}
}
