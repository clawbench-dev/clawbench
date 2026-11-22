// Package meta parses CodeBuddy's private `_meta` payload.
//
// CodeBuddy (`codebuddy --acp`) packs far more into `_meta` than the ACP
// standard asks for, under its own `codebuddy.ai/*` namespace plus an
// OpenAI-style `usage` block. The keys, the shapes and the quirks (a usage
// block per MODEL CALL rather than per turn, a credit total that is re-reported
// as a running sum, a refusal whose real cause hides inside a JSON envelope)
// are all this backend's own business — so they live here rather than in
// internal/ai, where they used to sit as a 300-line file that the shared event
// loop reached into by name.
//
// The shared layer only sees the result: an ai.MetaExtraction.
package meta

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"clawbench/internal/ai"
)

const (
	KeyUsage      = "usage"
	KeyByCategory = "codebuddy.ai/usageByCategory"
	//nolint:gosec // G101: a _meta key name, not a credential
	KeyRequestCredits = "codebuddy.ai/requestCredits"
	RequestID         = "codebuddy.ai/requestId"
	// ConversationRequestID is the turn-level request id. It is preferred over
	// RequestID in the chunk de-duplication key, matching upstream's
	// `conversationRequestId ?? requestId`.
	ConversationRequestID = "codebuddy.ai/conversationRequestId"
	TraceID               = "codebuddy.ai/traceId"
	TraceParent           = "codebuddy.ai/traceparent"
	MessageID             = "codebuddy.ai/messageId"
	MessageRequestID      = "codebuddy.ai/messageRequestId"
	ReqModelID            = "codebuddy.ai/requestModelId"
	ReqModelName          = "codebuddy.ai/requestModelName"
	RespModelID           = "codebuddy.ai/responseModelId"
	FinishReason          = "codebuddy.ai/finishReason"
	Outcome               = "codebuddy.ai/outcome"
	AgentPhase            = "codebuddy.ai/agentPhase"
	// ErrorMessage carries the refusal payload on a stopReason=refusal
	// PromptResponse: a JSON-encoded {code,message,data} blob whose data.details
	// holds the real cause.
	ErrorMessage = "codebuddy.ai/errorMessage"
	// ErrorMessageBare is the same payload under an unprefixed key. CodeBuddy
	// uses it on the out-of-band control-command failure path (a plain message
	// string rather than the JSON blob), and both shapes reach the client as
	// stopReason=refusal.
	ErrorMessageBare = "errorMessage"
)

// maxErrorDetailRunes bounds the extracted refusal detail so a pathological
// upstream payload cannot bloat every persisted warning block. The details are
// human-readable one-liners (e.g. "Bad substitution: o.gaps.join"), so this is
// far above any real value.
const maxErrorDetailRunes = 300

// Extract parses a CodeBuddy _meta payload into the shared extraction shape.
// nil is returned when the payload carries nothing this backend recognizes, so
// the caller's HasData check behaves the same as for every other backend.
func Extract(m map[string]any) *ai.MetaExtraction {
	if len(m) == 0 {
		return nil
	}
	ext := &ai.MetaExtraction{}

	// OpenAI-style usage block.
	if usage, ok := m[KeyUsage].(map[string]any); ok {
		ext.Usage = tokenUsage(usage)
		// CodeBuddy emits one usage_update per MODEL CALL within a turn, each
		// carrying only that call's counters (verified on the raw wire: a
		// 4-command turn produced prompt_tokens 38229/38328/38427/38526/38625
		// under a single requestId). The turn total is their SUM, so the
		// accumulator must add them rather than adopt the last one.
		// messageId identifies the call and is the dedup key.
		ext.Usage.Accumulate = true
		ext.Usage.CallID = ai.MetaString(m[MessageID])
	}

	// Context-window breakdown.
	if cat, ok := m[KeyByCategory].(map[string]any); ok {
		usage := &ai.MetaCategoryUsage{Categories: map[string]int64{}}
		for _, k := range []string{"conversation", "tools", "systemPrompt", "skills", "mcp"} {
			if v, ok := cat[k]; ok {
				usage.Categories[k] = int64(ai.MetaInt(v))
			}
		}
		if len(usage.Categories) > 0 {
			usage.Present = true
		}
		ext.Category = usage
	}

	// Per-request credit totals. CodeBuddy accumulates the credit of a whole
	// REQUEST (which may span several model calls) server-side and re-reports
	// the running total on every notification, so a throttled or dropped frame
	// does not lose credit. It is also the only credit source on the cost-only
	// paths (generation tools, team-member turns) which carry no usage block.
	ext.RequestCredits = requestCredits(m[KeyRequestCredits])

	// Trace/identity namespace.
	t := &ai.MetaTrace{
		RequestID:        ai.MetaString(m[RequestID]),
		TraceID:          ai.MetaString(m[TraceID]),
		TraceParent:      ai.MetaString(m[TraceParent]),
		MessageID:        ai.MetaString(m[MessageID]),
		MessageRequestID: ai.MetaString(m[MessageRequestID]),
		RequestModelID:   ai.MetaString(m[ReqModelID]),
		RequestModelName: ai.MetaString(m[ReqModelName]),
		ResponseModelID:  ai.MetaString(m[RespModelID]),
		FinishReason:     ai.MetaString(m[FinishReason]),
		Outcome:          ai.MetaString(m[Outcome]),
		AgentPhase:       ai.MetaString(m[AgentPhase]),
	}
	// The traceparent header (when the dedicated traceId key is absent) also
	// carries the root trace id as its first component (00-<traceid>-<spanid>-01).
	if t.TraceID == "" && t.TraceParent != "" {
		// Last field wins on truncation: a malformed header yields parts whose
		// tail is the least trustworthy, and this only ever fills a blank.
		if parts := strings.Split(t.TraceParent, "-"); len(parts) >= 2 {
			t.TraceID = parts[1]
		}
	}
	if t.HasData() {
		ext.Trace = t
	}

	if !ext.HasData() {
		return nil
	}
	return ext
}

// RequestIDOf returns just the turn's requestId, for the replay baseline.
func RequestIDOf(m map[string]any) string {
	if len(m) == 0 {
		return ""
	}
	return ai.MetaString(m[RequestID])
}

// tokenUsage builds a MetaTokenUsage from the OpenAI-style _meta.usage block.
func tokenUsage(m map[string]any) *ai.MetaTokenUsage {
	u := &ai.MetaTokenUsage{
		InputTokens:         ai.MetaInt(m["prompt_tokens"]),
		OutputTokens:        ai.MetaInt(m["completion_tokens"]),
		TotalTokens:         ai.MetaInt(m["total_tokens"]),
		ThoughtTokens:       ai.MetaInt(m["completion_thinking_tokens"]),
		CachedReadTokens:    ai.MetaInt(m["prompt_cache_hit_tokens"]),
		CachedWriteTokens:   ai.MetaInt(m["prompt_cache_write_tokens"]),
		CacheCreationTokens: ai.MetaInt(m["cache_creation_input_tokens"]),
		CacheHitTokens:      ai.MetaInt(m["prompt_cache_hit_tokens"]),
		CacheMissTokens:     ai.MetaInt(m["prompt_cache_miss_tokens"]),
		Credit:              ai.MetaFloat(m["credit"]),
	}
	// Also accept cache_read_input_tokens as a cache-read source. max, not sum:
	// the two spellings describe the same counter.
	u.CachedReadTokens = maxInt(u.CachedReadTokens, ai.MetaInt(m["cache_read_input_tokens"]))
	if u.InputTokens != 0 || u.OutputTokens != 0 || u.TotalTokens != 0 ||
		u.CachedReadTokens != 0 || u.CachedWriteTokens != 0 ||
		u.CacheCreationTokens != 0 || u.CacheHitTokens != 0 || u.CacheMissTokens != 0 ||
		u.ThoughtTokens != 0 || u.Credit != 0 {
		u.Present = true
	}
	return u
}

// maxInt returns the larger of a and b.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// requestCredits parses _meta["codebuddy.ai/requestCredits"], the agent's own
// per-request running credit totals.
//
// Non-positive entries are dropped: an absent entry means the same thing to
// creditTotal(), and this also keeps a malformed value from being recorded as a
// real 0 alongside a valid sibling. Returns nil when nothing usable remains so
// HasData() does not report an empty extraction.
func requestCredits(v any) map[string]float64 {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		return nil
	}
	credits := make(map[string]float64, len(m))
	for rid, raw := range m {
		if rid == "" {
			continue
		}
		if c := ai.MetaFloat(raw); c > 0 {
			credits[rid] = c
		}
	}
	if len(credits) == 0 {
		return nil
	}
	return credits
}

// RefusalDetail extracts the human-readable failure reason from a
// PromptResponse._meta on a refusal.
//
// CodeBuddy reports stopReason=refusal with a generic JSON-RPC code (-32603
// Internal error) and stashes the real cause in _meta as a JSON string:
//
//	{"code":-32603,"message":"Internal error","data":{"details":"Bad substitution: x"}}
//
// The `details` field carries the actionable text; `message` alone is the
// generic "Internal error". When the payload parses but neither field carries
// usable text, the result is "" so the caller falls back to the generic
// refusal copy rather than echoing a JSON envelope as if it were a reason.
//
// A bare (non-JSON) string is returned as-is: CodeBuddy uses that shape too
// (the sensitive-input block path and the out-of-band control-command failure
// path put the message directly there), and a malformed/truncated blob is
// likewise surfaced verbatim — the text still tells the user more than the
// bare -32603 code.
func RefusalDetail(m map[string]any) string {
	if len(m) == 0 {
		return ""
	}
	raw := ai.MetaString(m[ErrorMessage])
	if raw == "" {
		raw = ai.MetaString(m[ErrorMessageBare])
	}
	if raw == "" {
		return ""
	}

	detail, parsed := refusalDetailFromPayload(raw)
	if !parsed {
		// Not a JSON object (bare string, or a malformed/truncated blob): the
		// raw text is all we have, and it beats the bare code.
		detail = raw
	}
	return truncateRunes(strings.TrimSpace(detail), maxErrorDetailRunes)
}

// refusalDetailFromPayload pulls data.details (preferred) or message out of a
// JSON-encoded error payload. The bool reports whether raw parsed as a JSON
// object at all: when it did but neither field carried usable text, the caller
// must NOT echo the raw blob (a JSON envelope like {"code":-32603} is noise,
// not a reason), whereas unparseable text is worth surfacing verbatim.
func refusalDetailFromPayload(raw string) (detail string, parsed bool) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "{") {
		return "", false
	}
	var payload struct {
		Message string `json:"message"`
		Data    struct {
			Details string `json:"details"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return "", false
	}
	if details := strings.TrimSpace(payload.Data.Details); details != "" {
		return details, true
	}
	// "Internal error" and friends are the generic placeholder, not a cause.
	msg := strings.TrimSpace(payload.Message)
	if msg == "" || msg == "Internal error" {
		return "", true
	}
	return msg, true
}

// truncateRunes truncates s to at most n runes, appending an ellipsis marker
// when it was cut. Rune-based so a multi-byte detail never splits mid-character.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
