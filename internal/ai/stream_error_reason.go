package ai

import "strings"

// limitErrorPhrases are the message substrings that identify an account-level
// limit (out of credit / over the plan quota / rate limited) when the backend's
// error TYPE is missing or generic. Deliberately PHRASES, not bare words:
// "balance" alone matches "load balancer" and "insufficient" alone matches
// "insufficient permissions", and either false positive would tell a user with
// a working account to go top up — while also disabling the retry that would
// have fixed a transient failure.
var limitErrorPhrases = []string{
	"insufficient balance",
	"insufficient_quota",
	"insufficient quota",
	"insufficient credit",
	"account balance",
	"credit limit",
	"creditlimit",
	"payment required",
	"usage limit reached",
	"usage_not_included",
	"quota exceeded",
	"quota_exceeded",
	"exceeded your current quota", // OpenAI's wording
	"credit balance",              // Anthropic's "Your credit balance is too low"
	"budget limit",
	"too many requests",
	"rate limit",
	"rate-limit",
	"throttl",
}

// isLimitErrorText reports whether an error message names a condition only the
// user can fix (top up, wait out a limit, switch model).
//
// Shared by every backend that has to classify an in-band error frame: OpenCode
// (structured error type + HTTP status + message) and the plain-JSON CLIs
// (deepseek/grok/pi/stream-json/codex), which only give us the message text.
// Scanning the agent's own error message is safe because assistant content is
// never inspected — a model merely talking about quotas cannot trip this.
func isLimitErrorText(text string) bool {
	haystack := strings.ToLower(text)
	for _, phrase := range limitErrorPhrases {
		if strings.Contains(haystack, phrase) {
			return true
		}
	}
	return false
}

// streamErrorReason picks the warning reason for an in-band error frame.
//
// A limit failure is a PERMANENT failure (abnormalTerminalFailure in the
// service layer) so auto-continue does not re-send a request that can only fail
// again — and so the UI can name the real remedy instead of a generic
// "request failed". Everything else is a request-level failure, which the
// retry path may well fix.
//
// Without a reason the error event still becomes a warning block, but
// classifyTurnAbnormality cannot tell it apart from a clean finish: the turn
// then reports success to auto-continue, auto-cruise, the notification push and
// scheduled tasks.
func streamErrorReason(text string) string {
	if isLimitErrorText(text) {
		return ReasonQuotaExhausted
	}
	return ReasonRequestFailed
}
