package handler

import (
	"strings"
	"testing"
)

// TestWithResolveHint pins the hint that makes a bare name-lookup failure
// actionable. The project builds without cgo (see AGENTS.md), so Go resolves
// names itself and skips the system's NSS modules; a user who cannot reach a
// LAN name needs to be told that an IP address or /etc/hosts will work.
func TestWithResolveHint(t *testing.T) {
	cases := []struct {
		name     string
		msg      string
		wantHint bool
	}{
		{
			name:     "go resolver lookup failure",
			msg:      `Failed to connect to http://nas.local:8080/v1: Post "http://nas.local:8080/v1/chat/completions": dial tcp: lookup nas.local on 127.0.0.53:53: no such host`,
			wantHint: true,
		},
		{
			name:     "resolver reported a broken upstream",
			msg:      `dial tcp: lookup git.corp.internal on 10.0.0.1:53: server misbehaving`,
			wantHint: true,
		},
		{
			name:     "connection refused is not a lookup failure",
			msg:      `Failed to connect to http://127.0.0.1:9999/v1: dial tcp 127.0.0.1:9999: connect: connection refused`,
			wantHint: false,
		},
		{
			name:     "auth failure is not a lookup failure",
			msg:      "API error (HTTP 401): invalid api key",
			wantHint: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := withResolveHint(tc.msg)
			if !strings.HasPrefix(got, tc.msg) {
				t.Fatalf("hint must be appended, not replace the message:\n got %q\nwant prefix %q", got, tc.msg)
			}
			hasHint := got != tc.msg
			if hasHint != tc.wantHint {
				t.Fatalf("hint added = %v, want %v (message: %q)", hasHint, tc.wantHint, got)
			}
			if tc.wantHint && !strings.Contains(got, "/etc/hosts") {
				t.Fatalf("hint must name a way out (IP address or /etc/hosts), got %q", got)
			}
		})
	}
}

// TestWithResolveHint_IsIdempotent guards the double-append case: a message
// that already carries the hint must not grow a second copy when it passes
// through the same path again.
func TestWithResolveHint_IsIdempotent(t *testing.T) {
	msg := `dial tcp: lookup nas.local on 127.0.0.53:53: no such host`
	once := withResolveHint(msg)
	twice := withResolveHint(once)
	if twice != once {
		t.Fatalf("hint applied twice:\n first %q\nsecond %q", once, twice)
	}
}
