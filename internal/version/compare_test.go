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

		// Build time suffix stripped (mmddHHMM format)
		{"v1.0.0-07291030", "1.0.0", 0},
		{"v1.0.1-07291030", "1.0.0", 1},
		{"v0.70.0-5-g830bb6c-07291030", "0.70.0-5-g830bb6c", 0},
		{"v0.70.0-5-g830bb6c-07291030", "0.70.0", 1},

		// Edge cases: short hash / dev with build time suffix
		{"a0f87a96-07291030", "a0f87a96", 0},
		{"dev-07291030", "dev", 0},
	}

	for _, tt := range tests {
		got := CompareVersions(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestStripBuildTimeSuffix(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"v1.0.0-07291030", "v1.0.0"},
		{"v0.70.0-5-g830bb6c-07291030", "v0.70.0-5-g830bb6c"},
		{"v0.70.0-5-g830bb6c", "v0.70.0-5-g830bb6c"},
		{"a0f87a96-07291030", "a0f87a96"},
		{"dev-07291030", "dev"},
		{"v1.0.0", "v1.0.0"},
		{"07291030", "07291030"}, // no dash, not a suffix
		{"v1.0.0-5", "v1.0.0-5"}, // too short (not 8 digits)
	}

	for _, tt := range tests {
		got := stripBuildTimeSuffix(tt.input)
		if got != tt.want {
			t.Errorf("stripBuildTimeSuffix(%q) = %q, want %q", tt.input, got, tt.want)
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
		{"g7702c47", true},
		{"abcdef1", true}, // 7 hex chars
		{"0", false},      // too short for git hash, no dots
		{"123", false},    // too short
		{"abc", false},    // too short
		{"v0.66.0-5-g7702c473-07291030", true},
		{"v1.0.0-07291030", false},
		{"a0f87a96-07291030", true}, // short hash + build time suffix
		{"dev-07291030", true},      // dev + build time suffix
		{"g7702c473def", true}, // 12 hex chars with g prefix
		{"release-1.0", true},  // has pre-release suffix
	}

	for _, tt := range tests {
		got := IsDevBuild(tt.v)
		if got != tt.want {
			t.Errorf("IsDevBuild(%q) = %v, want %v", tt.v, got, tt.want)
		}
	}
}
