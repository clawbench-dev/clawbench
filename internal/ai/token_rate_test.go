package ai

import "testing"

func TestDeriveTokensPerSecond(t *testing.T) {
	cases := []struct {
		name         string
		outputTokens int
		genMs        int
		want         float64
		ok           bool
	}{
		{"typical turn", 500, 10000, 50, true},
		{"short but usable", 50, 1000, 50, true},
		{"single fast burst", 240, 3000, 80, true},
		// A CLI that prints the finished answer in one event has no streaming
		// window; the "rate" would be arbitrarily large. Not measured.
		{"no streaming window", 400, 0, 0, false},
		{"window too short", 400, 499, 0, false},
		// A one-word reply's speed is dominated by one pause, not throughput.
		{"sample too small", 3, 5000, 0, false},
		{"no output tokens", 0, 5000, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := DeriveTokensPerSecond(tc.outputTokens, tc.genMs)
			if ok != tc.ok {
				t.Fatalf("DeriveTokensPerSecond(%d, %d) ok = %v, want %v", tc.outputTokens, tc.genMs, ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Fatalf("DeriveTokensPerSecond(%d, %d) = %v, want %v", tc.outputTokens, tc.genMs, got, tc.want)
			}
			if !ok && got != 0 {
				t.Fatalf("unmeasured rate must be 0, got %v", got)
			}
		})
	}
}
