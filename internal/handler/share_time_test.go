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

// A share row's created_at can arrive in two shapes depending on how the driver
// scanned the DATETIME column. Accepting only SQLite's bare text silently
// produced a zero createdAt in every share payload (the exported JSON claimed the
// share was created in year 1) — and the old test only asserted "non-empty", so
// the zero time passed.
func TestParseShareTime(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   string
		wantOK bool
	}{
		{
			// What GetSessionShareByToken actually returns today: the driver
			// parses the column into a time.Time and it is formatted back out.
			name:   "rfc3339 from the driver",
			in:     "2026-10-22T15:18:10Z",
			want:   "2026-10-22T15:18:10Z",
			wantOK: true,
		},
		{
			// What SQLite's DEFAULT CURRENT_TIMESTAMP writes on disk.
			name:   "bare sqlite utc text",
			in:     "2026-10-22 15:18:10",
			want:   "2026-10-22T15:18:10Z",
			wantOK: true,
		},
		{
			name:   "rfc3339 with an offset is normalized to utc",
			in:     "2026-10-22T23:18:10+08:00",
			want:   "2026-10-22T15:18:10Z",
			wantOK: true,
		},
		{
			name:   "empty",
			in:     "",
			wantOK: false,
		},
		{
			name:   "unparseable",
			in:     "not-a-time",
			wantOK: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseShareTime(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("parseShareTime(%q) ok = %v, want %v", tc.in, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if formatted := got.Format("2006-01-02T15:04:05Z07:00"); formatted != tc.want {
				t.Fatalf("parseShareTime(%q) = %q, want %q", tc.in, formatted, tc.want)
			}
		})
	}
}

// formatShareTime and parseShareTime must round-trip: formatShareTime's whole job
// is producing a value parseShareTime (and the browser) accepts.
func TestShareTimeRoundTrip(t *testing.T) {
	for _, in := range []string{"2026-10-22 15:18:10", "2026-10-22T15:18:10Z"} {
		formatted := formatShareTime(in)
		back, ok := parseShareTime(formatted)
		if !ok {
			t.Fatalf("parseShareTime(formatShareTime(%q) = %q) failed", in, formatted)
		}
		if got := back.Format("2006-01-02T15:04:05Z07:00"); got != "2026-10-22T15:18:10Z" {
			t.Fatalf("round trip of %q = %q", in, got)
		}
	}
}
