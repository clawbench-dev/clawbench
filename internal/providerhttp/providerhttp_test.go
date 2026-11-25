package providerhttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsOpenCode(t *testing.T) {
	cases := []struct {
		name     string
		baseURL  string
		presetID string
		want     bool
	}{
		{"catalog preset", "https://example.com/v1", "opencode", true},
		{"go preset", "https://example.com/v1", "opencode-go", true},
		{"preset is case-insensitive", "", " OpenCode-Go ", true},
		{"inference host", "https://opencode.ai/inference/go/openai/v1", "", true},
		{"host is a suffix, not a substring", "https://opencode.ai.example.com/v1", "", false},
		{"unrelated provider", "https://api.deepseek.com", "deepseek", false},
		{"empty", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsOpenCode(tc.baseURL, tc.presetID); got != tc.want {
				t.Fatalf("IsOpenCode(%q, %q) = %v, want %v", tc.baseURL, tc.presetID, got, tc.want)
			}
		})
	}
}

func TestApplySetsOpenCodeHeaders(t *testing.T) {
	req := httptest.NewRequest("POST", "https://opencode.ai/inference/go/openai/v1/chat/completions", http.NoBody)

	Apply(req, "https://opencode.ai/inference/go/openai/v1", "", "sess-42")

	if got := req.Header.Get(OpenCodeSessionHeader); got != "sess-42" {
		t.Fatalf("%s = %q, want the caller's session id", OpenCodeSessionHeader, got)
	}
	if ua := req.Header.Get("User-Agent"); !strings.HasPrefix(ua, "clawbench/") {
		t.Fatalf("User-Agent = %q, want our own client name", ua)
	}
}

func TestApplyLeavesOtherProvidersUntouched(t *testing.T) {
	req := httptest.NewRequest("POST", "https://api.deepseek.com/v1/chat/completions", http.NoBody)

	Apply(req, "https://api.deepseek.com", "deepseek", "sess-42")

	if got := req.Header.Get(OpenCodeSessionHeader); got != "" {
		t.Fatalf("a non-OpenCode endpoint must not receive %s, got %q", OpenCodeSessionHeader, got)
	}
	if got := req.Header.Get("User-Agent"); got != "" {
		t.Fatalf("a non-OpenCode endpoint must keep the default User-Agent, got %q", got)
	}
}

func TestApplyGeneratesOneOffSessionID(t *testing.T) {
	req := httptest.NewRequest("POST", "https://opencode.ai/inference/go/openai/v1/chat/completions", http.NoBody)

	Apply(req, "https://opencode.ai/inference/go/openai/v1", "", "")

	got := req.Header.Get(OpenCodeSessionHeader)
	if !strings.HasPrefix(got, "ses_") || len(got) <= len("ses_") {
		t.Fatalf("%s = %q, want a generated ses_ id", OpenCodeSessionHeader, got)
	}
}

func TestSessionIDPrefersTheStableID(t *testing.T) {
	if got := SessionID("conv-1"); got != "conv-1" {
		t.Fatalf("SessionID kept a stable id as %q", got)
	}
	a, b := SessionID(""), SessionID("")
	if a == b {
		t.Fatal("two generated ids must differ")
	}
}
