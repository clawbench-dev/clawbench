package ai

import "testing"

// Every backend that reports an in-band error frame must tag it with a reason;
// without one the warning block is indistinguishable from a clean finish, so a
// quota error was reported as a completed turn (auto-continue retried it,
// auto-cruise kept going, scheduled tasks recorded `completed`).
func TestStreamErrorReason(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"insufficient balance", "402: Insufficient Balance", ReasonQuotaExhausted},
		{"openai quota", "You exceeded your current quota, please check your plan", ReasonQuotaExhausted},
		{"rate limited", "API rate limit exceeded", ReasonQuotaExhausted},
		{"anthropic credit", "Your credit balance is too low to access the API", ReasonQuotaExhausted},
		{"generic failure", "unexpected end of JSON input", ReasonRequestFailed},
		{"empty", "", ReasonRequestFailed},
		// Deliberately NOT a limit: "insufficient permissions" is an auth/scope
		// problem, and treating it as "go top up" would send the user to the
		// wrong remedy (and disable the retry that might fix it).
		{"permissions", "insufficient permissions for this model", ReasonRequestFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := streamErrorReason(tc.text); got != tc.want {
				t.Fatalf("streamErrorReason(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}
