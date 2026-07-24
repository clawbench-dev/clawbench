package version

import "testing"

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		// Equal versions
		{"1.0.0", "1.0.0", 0},
		{"v1.0.0", "1.0.0", 0},
		{"v1.0.0", "v1.0.0", 0},

		// a < b
		{"1.0.0", "1.0.1", -1},
		{"1.0.0", "1.1.0", -1},
		{"1.0.0", "2.0.0", -1},
		{"v1.0.0", "1.0.1", -1},
		{"v1.0.0", "v2.0.0", -1},

		// a > b
		{"1.0.1", "1.0.0", 1},
		{"1.1.0", "1.0.0", 1},
		{"2.0.0", "1.99.99", 1},
		{"v2.0.0", "1.0.0", 1},

		// Different lengths
		{"1.0", "1.0.0", 0},
		{"1.0.1", "1.0", 1},
		{"1.0", "1.0.1", -1},

		// Dev/non-release versions
		{"0", "1.0.0", -1},
		{"dev", "1.0.0", -1},
		{"abcdef1", "1.0.0", -1},

		// Dev builds (with suffix) are newer than same release version
		{"0.66.0-5-gabc", "0.66.0", 1},
		{"0.66.0", "0.66.0-5-gabc", -1},

		// Build time suffix stripped
		{"v1.0.0 (2026-07-24 10:30:00)", "1.0.0", 0},
		{"v1.0.1 (2026-07-24)", "1.0.0", 1},
	}

	for _, tt := range tests {
		got := CompareVersions(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestIsDevBuild(t *testing.T) {
	tests := []struct {
		v    string
		want bool
	}{
		{"v0.66.0-5-g7702c473", true},
		{"0.66.0-5-gabc", true},
		{"v0.66.0", false},
		{"1.0.0", false},
		{"dev", true},
		{"g7702c4", true},
		{"abcdef1", true},
		{"v0.66.0-5-gabc (2026-07-24 10:30:00)", true},
		{"v1.0.0 (2026-07-24 10:30:00)", false},
	}

	for _, tt := range tests {
		got := IsDevBuild(tt.v)
		if got != tt.want {
			t.Errorf("IsDevBuild(%q) = %v, want %v", tt.v, got, tt.want)
		}
	}
}
