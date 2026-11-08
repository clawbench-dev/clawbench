package ai

import (
	"encoding/json"
	"log/slog"
	"strings"
)

// OpenCodeStreamMessage represents a single JSON line from `opencode run --format json`
type OpenCodeStreamMessage struct {
	Type      string          `json:"type"` // "step_start", "text", "reasoning", "tool_use", "step_finish", "retry", "error"
	Timestamp float64         `json:"timestamp"`
	SessionID string          `json:"sessionID"`
	Attempt   int             `json:"attempt"` // type="retry": which retry attempt this is
	Part      json.RawMessage `json:"part"`    // Varies by type — parse separately
	Error     *OpenCodeError  `json:"error"`   // type="error"/"retry" only
}

// OpenCodeError is the failure payload of type="error" and type="retry" frames.
//
// OpenCode reports upstream failures as ordinary stdout frames rather than as a
// non-zero exit with stderr text, so this is the ONLY place the real cause of a
// failed turn appears (verified against opencode 2.2.6: a 402 produces
// {"type":"error","error":{"type":"provider.quota","message":"Insufficient
// Balance","status":402}} and exit code 1, with an empty stderr). Dropping
// these frames — the previous behavior — left the user with the generic
// "AI backend exited abnormally" banner and no idea the account was out of
// credit.
type OpenCodeError struct {
	Type    string `json:"type"` // "provider.quota", "provider.rate-limit", "provider.transport", ...
	Message string `json:"message"`
	Status  int    `json:"status"` // upstream HTTP status when known (402, 429, ...)
}

// OpenCodeTextPart is the part for type="text" and type="reasoning" messages
type OpenCodeTextPart struct {
	Type string `json:"type"` // "text" or "reasoning"
	Text string `json:"text"`
}

// OpenCodeToolPart is the part for type="tool_use" messages
type OpenCodeToolPart struct {
	Type   string             `json:"type"` // "tool"
	Tool   string             `json:"tool"`
	CallID string             `json:"callID"`
	State  *OpenCodeToolState `json:"state"`
}

// OpenCodeToolState holds tool execution status and I/O
type OpenCodeToolState struct {
	Status string          `json:"status"` // "completed", "running"
	Input  json.RawMessage `json:"input"`
	Output string          `json:"output"`
}

// OpenCodeFinishPart is the part for type="step_finish" messages
type OpenCodeFinishPart struct {
	Reason string          `json:"reason"` // "stop" or "tool-calls"
	Tokens *OpenCodeTokens `json:"tokens,omitempty"`
	Cost   float64         `json:"cost"`
}

// OpenCodeTokens holds token usage from step_finish.
//
// The identity verified over 42,538 real steps in OpenCode's own storage is
//
//	total = input + output + cache.read + cache.write + reasoning
//
// i.e. the cached prompt is reported OUTSIDE input (same convention as
// Claude/Codex), which the ledger normalizes at ingestion.
type OpenCodeTokens struct {
	Total     int              `json:"total"`
	Input     int              `json:"input"`
	Output    int              `json:"output"`
	Reasoning int              `json:"reasoning"`
	Cache     OpenCodeCacheUse `json:"cache"`
}

// OpenCodeCacheUse is the cache split carried inside step_finish tokens.
type OpenCodeCacheUse struct {
	Read  int `json:"read"`
	Write int `json:"write"`
}

// OpenCodeStreamParser parses JSON Lines output from `opencode run --format json`.
// It is separate from the shared StreamParser because OpenCode's format is
// fundamentally different (different top-level types, nesting in "part", multi-step lifecycle).
//
// CONTRACT for the `error` frame: it is treated as the TURN'S TERMINAL event —
// the parser emits the classified error followed by `done`, and the CLI driver
// suppresses its own generic exit warning. That holds for opencode 2.2.6, whose
// renderer reports an error only once, when the run ends in failure.
//
// The parser is also reused by MiMo-Code (an OpenCode fork) via
// internal/ai/backends/mimo. If that binary ever emits `error` as a MID-STEP
// signal and then continues, this parser would end the turn early — a fork that
// behaves that way needs its own flag rather than silently inheriting this one.
type OpenCodeStreamParser struct {
	sessionID string // captured from any message that has a sessionID

	// ToolNameMap maps backend-specific tool names to canonical names.
	// When set, ParseLine uses this map instead of the global normalizeToolName().
	ToolNameMap map[string]string

	// InputRemaps maps input field names for tool input normalization.
	// When set, ParseLine uses this map for normalizing tool input fields.
	InputRemaps map[string]string

	// turn accumulates the usage of every step in the CURRENT turn.
	//
	// OpenCode emits one step_finish per model call — a tool-using turn has one
	// per round, and only the LAST one carries reason="stop". Recording just
	// that step (the previous behavior) discarded every intermediate call:
	// measured against OpenCode's own storage, one 812-step session reports
	// 267,293,125 tokens across its steps but only 11,705,915 in its stop
	// steps — 22.8x under. The turn total is the SUM over steps.
	turn openCodeTurnUsage

	// errorReported is set when handleErrorFrame emitted the turn's terminal
	// error, so the CLI driver can skip its generic "backend exited abnormally"
	// warning. See ReportedStreamError.
	errorReported bool
}

// openCodeTurnUsage is the running total for one turn.
type openCodeTurnUsage struct {
	input      int
	output     int
	total      int
	reasoning  int
	cacheRead  int
	cacheWrite int
	// steps counts the step_finish parts folded in so far. Used to decide
	// whether a turn reported anything at all.
	steps int
}

// reset clears the accumulator for a new turn.
func (u *openCodeTurnUsage) reset() { *u = openCodeTurnUsage{} }

// GetCapturedSessionID returns the OpenCode session ID (ses_xxx) captured from
// parsed stream messages. Available as soon as the first message (e.g., step_start) is parsed.
func (p *OpenCodeStreamParser) GetCapturedSessionID() string { return p.sessionID }

// ReportedStreamError reports whether this parser already emitted the turn's
// terminal error event (see streamErrorReporter).
//
// The CLI driver uses it to suppress its own "AI backend exited abnormally"
// warning. OpenCode reports upstream failures as a stdout frame AND exits 1, so
// without this every quota/limit failure produced TWO warning blocks: the
// classified one and a generic retryable backend_exit — and that second one
// made auto-continue re-send a request that can only fail again.
func (p *OpenCodeStreamParser) ReportedStreamError() bool { return p.errorReported }

// ParseLine parses a single JSON line from OpenCode's stream-json output and sends
// StreamEvent(s) to the provided channel.
func (p *OpenCodeStreamParser) ParseLine(line string, ch chan<- StreamEvent) {
	var msg OpenCodeStreamMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		slog.Debug("opencode stream: skipping unparseable line", "line", line, "error", err)
		return
	}

	// Capture session ID from any message that has one
	if msg.SessionID != "" {
		p.sessionID = msg.SessionID
	}

	switch msg.Type {
	case "text":
		p.emitTextLike(msg.Part, "content", ch)

	case "reasoning":
		p.emitTextLike(msg.Part, "thinking", ch)

	case "tool_use":
		if tc := parseOpenCodeToolEvent(&msg, p.ToolNameMap, p.InputRemaps); tc != nil {
			emitStreamEvent(ch, "opencode", StreamEvent{Type: "tool_use", Tool: tc})
		}

	case "step_finish":
		p.parseStepFinish(msg.Part, ch)
		// reason="tool-calls" means more steps coming — no event

	case "step_start":
		// Structural — no event

	case "error":
		p.handleErrorFrame(msg, ch)

	case "retry":
		// OpenCode retries transient upstream failures on its own (measured: 11
		// attempts over ~85s for a persistent 429, then a terminal "error"
		// frame). It emits nothing else meanwhile, so without this the UI shows
		// a bare spinner for over a minute — indistinguishable from a hang.
		//
		// The notice is TRANSIENT (no block is accumulated): a retry that
		// recovers must leave no trace in the transcript. A warning block was
		// tried and reverted — it is permanent, so a 429 that recovered on the
		// next attempt left a stale "rate limited" card on a turn that actually
		// succeeded, and because classification reads reasons off blocks that
		// stale card also made the turn look failed (a scheduled task reported
		// `failed` for work that had completed). The terminal frame below is the
		// only place the turn's real outcome is known, and for an exhausted
		// account (HTTP 402) it arrives in ~0.2s anyway.
		slog.Debug("opencode stream: retrying upstream failure",
			"error_type", errorTypeOf(msg.Error),
			"status", errorStatusOf(msg.Error))
		emitStreamEvent(ch, "opencode", StreamEvent{
			Type: "retry_notice",
			RetryNotice: &RetryNoticeData{
				Attempt:    msg.Attempt,
				Message:    errorMessageOf(msg.Error),
				HTTPStatus: errorStatusOf(msg.Error),
			},
		})

	default:
		slog.Debug("opencode stream: skipping unknown message type", "type", msg.Type)
	}
}

// emitTextLike handles the `text` and `reasoning` frames, which share a payload
// shape and differ only in the event type they produce.
//
// Extracted from ParseLine to keep that dispatch within the lint budget
// (gocyclo > 15 fails the release lint, which runs unfiltered), same as
// parseStepFinish.
func (p *OpenCodeStreamParser) emitTextLike(raw json.RawMessage, eventType string, ch chan<- StreamEvent) {
	var part OpenCodeTextPart
	if err := json.Unmarshal(raw, &part); err != nil {
		slog.Debug("opencode stream: skipping unparseable text part", "event_type", eventType, "error", err)
		return
	}
	if part.Text == "" {
		return
	}
	// Strip the leading \n\n that OpenCode prepends to text responses
	text := strings.TrimPrefix(part.Text, "\n\n")
	if text == "" {
		return
	}
	emitStreamEvent(ch, "opencode", StreamEvent{Type: eventType, Content: text})
}

// handleErrorFrame turns OpenCode's terminal `{"type":"error"}` frame into a
// visible, classified failure.
//
// This frame is the ONLY place a failed turn's real cause appears. OpenCode
// reports upstream failures on stdout and exits 1 with an EMPTY stderr
// (verified against 2.2.6 with a 402 mock: one error frame, no stderr text), so
// ignoring it — the previous behavior, it fell into the "unknown type" debug
// log — left the user with the generic "AI backend exited abnormally" banner
// and no hint that the account was out of credit.
//
// Both events are emitted: the error is what carries the classification and the
// message, and the trailing done is what closes the turn. Without a terminal
// event the executor treats the closed channel as "the process died", which
// auto-continue then retries — for a quota error that means re-sending a request
// that can only fail again (and, for a 429, one that takes ~85s per attempt).
func (p *OpenCodeStreamParser) handleErrorFrame(msg OpenCodeStreamMessage, ch chan<- StreamEvent) {
	errText := "AI request failed"
	reason := ReasonRequestFailed
	httpStatus := 0
	detail := ""
	if msg.Error != nil {
		if msg.Error.Message != "" {
			errText = msg.Error.Message
		}
		detail = msg.Error.Message
		httpStatus = msg.Error.Status
		if isOpenCodeQuotaError(msg.Error) {
			reason = ReasonQuotaExhausted
		}
	}
	slog.Error("opencode stream: agent reported an error",
		"error_type", errorTypeOf(msg.Error),
		"status", httpStatus,
		"message", errText,
		"reason", reason)
	// `error` is a critical event, so the send gets the full delivery grace
	// (non-blocking first, then up to criticalSendTimeout). The report matters
	// for CLASSIFICATION, not just display: this flag suppresses the CLI
	// driver's generic "backend exited abnormally" warning, and that warning is
	// the only thing that would otherwise tell classification the turn failed.
	// Setting the flag on a dropped send therefore makes a failed turn look like
	// a clean finish — a scheduled task would record `completed`.
	//
	// On a drop the flag stays false, so the driver's generic warning (tagged
	// ReasonBackendExit, i.e. retryable) still lands: the turn degrades to the
	// pre-fix behavior rather than to a false success.
	p.errorReported = emitCriticalStreamEventReport(ch, "opencode", StreamEvent{
		Type:        "error",
		Error:       errText,
		Reason:      reason,
		HTTPStatus:  httpStatus,
		ErrorDetail: detail,
		ErrorSource: "agent",
	})
	emitStreamEvent(ch, "opencode", StreamEvent{Type: "done"})
}

// errorTypeOf returns the provider error type for logging ("" when absent).
func errorTypeOf(e *OpenCodeError) string {
	if e == nil {
		return ""
	}
	return e.Type
}

// errorStatusOf returns the upstream HTTP status of an error (0 when absent).
func errorStatusOf(e *OpenCodeError) int {
	if e == nil {
		return 0
	}
	return e.Status
}

// errorMessageOf returns the upstream error text ("" when absent).
func errorMessageOf(e *OpenCodeError) string {
	if e == nil {
		return ""
	}
	return e.Message
}

// openCodeLimitTypes are OpenCode's own error-type identifiers for "the account
// cannot make this call right now". Matched on the TYPE before any message text
// is inspected, because these are stable machine-readable values while the
// message wording is the upstream provider's and changes without notice.
//
// Every entry was read out of the opencode 2.2.6 binary. The provider.*-limit
// ones matter beyond accuracy: their messages ("OpenCode Go usage limit
// reached…", "subscription_sharing_usage_limit_exceeded") contain none of the
// phrases below, so before this list they were classified as request_failed —
// which auto-continue then retried against an account that cannot pay.
var openCodeLimitTypes = map[string]bool{
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

// isOpenCodeQuotaError reports whether an error frame means the account is out
// of credit or over its limit, i.e. retrying the same request cannot help.
//
// Only ever called for `type:"error"` frames — the agent's own terminal
// verdict for the turn. Assistant text is never scanned, so a model that merely
// talks about quotas cannot trip this.
//
// The message fallback uses the shared phrase list (stream_error_reason.go) so
// every backend agrees on what "out of credit" looks like; OpenCode additionally
// gets the structured fast paths (HTTP 402, its own limit error types).
func isOpenCodeQuotaError(e *OpenCodeError) bool {
	if e == nil {
		return false
	}
	if e.Status == 402 {
		return true
	}
	if openCodeLimitTypes[strings.ToLower(e.Type)] {
		return true
	}
	return isLimitErrorText(e.Message)
}

// parseStepFinish handles a step_finish part, emitting the turn's terminal
// metadata + done events when the step ended the turn.
//
// Split out of ParseLine to keep that dispatch within the complexity budget:
// the token/cost mapping has enough conditionals of its own.
func (p *OpenCodeStreamParser) parseStepFinish(raw json.RawMessage, ch chan<- StreamEvent) {
	var part OpenCodeFinishPart
	if err := json.Unmarshal(raw, &part); err != nil {
		slog.Debug("opencode stream: skipping unparseable step_finish part", "error", err)
		return
	}
	// EVERY step counts, not just the terminal one. A tool-using turn emits one
	// step_finish per model call and only the last carries reason="stop";
	// keeping just that step threw away all the intermediate calls (22.8x
	// under-count measured against OpenCode's own storage). Fold this step into
	// the turn accumulator and only emit when the turn actually ends.
	if part.Tokens != nil {
		p.turn.input += part.Tokens.Input
		p.turn.output += part.Tokens.Output
		p.turn.total += part.Tokens.Total
		p.turn.reasoning += part.Tokens.Reasoning
		p.turn.cacheRead += part.Tokens.Cache.Read
		p.turn.cacheWrite += part.Tokens.Cache.Write
	}
	p.turn.steps++
	if part.Reason != "stop" {
		return
	}

	meta := &Metadata{
		SessionID:  p.sessionID,
		StopReason: "stop",
		CostUSD:    part.Cost,
	}
	p.applyTurnUsage(meta)
	// step_finish.cost is the SESSION running total, not this step's cost: in a
	// production DB the value rises monotonically across a session
	// (0.017 → 0.413) while per-turn tokens fall (9280 → 719), which no
	// per-turn price can produce. The ledger diffs it against the previous
	// turn's cost_session_total.
	if meta.CostUSD > 0 {
		meta.CostCumulative = true
		meta.CostSessionTotal = meta.CostUSD
	}
	emitStreamEvent(ch, "opencode", StreamEvent{Type: "metadata", Meta: meta})
	emitStreamEvent(ch, "opencode", StreamEvent{Type: "done"})
	p.turn.reset()
}

// applyTurnUsage copies the accumulated turn totals onto a Metadata.
//
// CACHE NORMALIZATION: OpenCode reports the cached prompt OUTSIDE input
// (verified over 42,538 steps in its own storage: total = input + output +
// cache.read + cache.write + reasoning). The stats panel assumes the CodeBuddy
// shape where input is the FULL prompt, so the cache counters are folded into
// input — otherwise OpenCode's cached traffic is invisible and its cache-hit
// rate has no denominator, exactly the defect fixed for Claude/Codex.
func (p *OpenCodeStreamParser) applyTurnUsage(meta *Metadata) {
	t := &p.turn
	meta.InputTokens = t.input
	meta.OutputTokens = t.output
	meta.ThoughtTokens = t.reasoning
	meta.CachedReadTokens = t.cacheRead
	meta.CachedWriteTokens = t.cacheWrite
	if t.cacheRead > 0 || t.cacheWrite > 0 {
		meta.CacheHitTokens = t.cacheRead
		meta.CacheMissTokens = t.input
		meta.InputTokens += t.cacheRead + t.cacheWrite
	}
	// Prefer the agent's own total; fall back to the normalized components when
	// it did not report one (older builds), and never let it fall below
	// input+output — the ledger's SUM-ability invariant.
	meta.TotalTokens = t.total
	if meta.TotalTokens < meta.InputTokens+meta.OutputTokens {
		meta.TotalTokens = meta.InputTokens + meta.OutputTokens
	}
}
