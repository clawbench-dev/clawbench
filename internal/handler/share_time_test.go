package handler

import "testing"

func TestFormatShareTime(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			// SQLite's DEFAULT CURRENT_TIMESTAMP writes bare UTC text. Sent
			// as-is the browser would read it as local time; the helper must
			// stamp it as UTC so the client localizes correctly.
			name: "sqlite utc text gains an explicit Z",
			in:   "2026-10-09 13:32:07",
			want: "2026-10-09T13:32:07Z",
		},
		{
			name: "empty stays empty",
			in:   "",
			want: "",
		},
		{
			// A value we cannot parse is passed through rather than dropped:
			// an unformattable timestamp is still better than a blank one.
			name: "unparseable is passed through",
			in:   "not-a-time",
			want: "not-a-time",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatShareTime(tc.in); got != tc.want {
				t.Fatalf("formatShareTime(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
