package ai

// OpenCodeLimitTypes are OpenCode's own error-type identifiers for "the account
// cannot make this call right now". Matched on the TYPE before any message text
// is inspected, because these are stable machine-readable values while the
// message wording is the upstream provider's and changes without notice.
//
// Every entry was read out of the opencode 2.2.6 binary. The provider.*-limit
// ones matter beyond accuracy: their messages ("OpenCode Go usage limit
// reached…", "subscription_sharing_usage_limit_exceeded") contain none of the
// usual phrases, so before this list they were classified as request_failed —
// which auto-continue then retried against an account that cannot pay.
//
// Lives in the core package because the ACP error classifier also consults it
// (acp_backend.go); the CLI-side parser in internal/ai/backends/opencode reads
// the same table so the two transports cannot drift.
var OpenCodeLimitTypes = map[string]bool{
	"provider.quota":         true,
	"provider.rate-limit":    true,
	"provider.free-limit":    true,
	"provider.go-limit":      true,
	"provider.chatgpt-limit": true,
	"budgetlimiterror":       true,
	"gousagelimiterror":      true,
	"freeusagelimiterror":    true,
	"creditlimitexceeded":    true,
}
