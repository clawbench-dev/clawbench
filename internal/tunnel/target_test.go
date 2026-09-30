package tunnel

import (
	"errors"
	"testing"
)

func TestParseTarget_Valid(t *testing.T) {
	cases := []struct {
		name string
		host string
		port string
		want Target
	}{
		{"explicit host", "10.0.0.5", "8080", Target{Host: "10.0.0.5", Port: 8080}},
		{"empty host defaults to loopback", "", "5173", Target{Host: "127.0.0.1", Port: 5173}},
		{"localhost normalizes to loopback", "localhost", "5173", Target{Host: "127.0.0.1", Port: 5173}},
		{"ipv6 literal kept verbatim", "::1", "3000", Target{Host: "::1", Port: 3000}},
		{"port 1 is the low bound", "127.0.0.1", "1", Target{Host: "127.0.0.1", Port: 1}},
		{"port 65535 is the high bound", "127.0.0.1", "65535", Target{Host: "127.0.0.1", Port: 65535}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseTarget(tc.host, tc.port)
			if err != nil {
				t.Fatalf("ParseTarget(%q, %q) unexpected error: %v", tc.host, tc.port, err)
			}
			if got != tc.want {
				t.Fatalf("ParseTarget(%q, %q) = %+v, want %+v", tc.host, tc.port, got, tc.want)
			}
		})
	}
}

func TestParseTarget_Invalid(t *testing.T) {
	cases := []struct {
		name string
		host string
		port string
	}{
		{"missing port", "127.0.0.1", ""},
		{"non-numeric port", "127.0.0.1", "http"},
		{"port zero", "127.0.0.1", "0"},
		{"negative port", "127.0.0.1", "-1"},
		{"port above 65535", "127.0.0.1", "65536"},
		{"float port", "127.0.0.1", "80.5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseTarget(tc.host, tc.port)
			if err == nil {
				t.Fatalf("ParseTarget(%q, %q) must fail", tc.host, tc.port)
			}
			if !errors.Is(err, ErrInvalidPort) {
				t.Fatalf("error must wrap ErrInvalidPort, got %v", err)
			}
		})
	}
}

func TestTarget_Addr(t *testing.T) {
	if got := (Target{Host: "127.0.0.1", Port: 8080}).Addr(); got != "127.0.0.1:8080" {
		t.Fatalf("Addr() = %q, want 127.0.0.1:8080", got)
	}
	// IPv6 literals must be bracketed or the dial string is ambiguous.
	if got := (Target{Host: "::1", Port: 8080}).Addr(); got != "[::1]:8080" {
		t.Fatalf("Addr() = %q, want [::1]:8080", got)
	}
}
