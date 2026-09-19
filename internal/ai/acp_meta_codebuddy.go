package ai

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// CodeBuddy ACP _meta adapter
// ---------------------------------------------------------------------------
//
// CodeBuddy (`codebuddy --acp`) packs rich metadata into _meta:
//
//   - `_meta.usage` — OpenAI-style token usage:
//     { prompt_tokens, completion_tokens, completion_thinking_tokens,
//       prompt_cache_hit_tokens, prompt_cache_miss_tokens,
//       prompt_cache_write_tokens, cache_read_input_tokens,
//       cache_creation_input_tokens, completion_tokens_details, credit }
//   - `_meta.codebuddy.ai/usageByCategory` — context-window breakdown:
//     { conversation, tools, systemPrompt, skills, mcp, version }
//   - `_meta.codebuddy.ai/*` — trace/identity:
//     { requestId, traceId, traceparent, messageId, messageRequestId,
//       requestModelId, requestModelName, responseModelId,
//       finishReason, outcome, agentPhase, progress }
//
// These appear on both session/update notifications (on the per-variant
// update._meta) and the PromptResponse._meta.

// costFieldCarriesCredit reports whether a backend's ACP-standard
// usage_update.cost field actually carries a credit value rather than a
// monetary cost.
//
// CodeBuddy repurposes the standard `cost` field: it ships the turn's credit
// consumption as cost.amount with an empty currency (the genuine credit is
// separately available as _meta.usage.credit). Persisting it as
// chat_metadata.cost_usd would inflate the cost stats with a unit-less credit
// number, so the ACP cost/currency is discarded at ingestion for this backend.
// Every other agent reports a real monetary cost (e.g. Claude's USD).
func costFieldCarriesCredit(backend string) bool {
	return backend == "codebuddy"
}

const (
	metaKeyCodeBuddyUsage        = "usage"
	metaKeyCodeBuddyByCategory   = "codebuddy.ai/usageByCategory"
	metaKeyCodeBuddyRequestID    = "codebuddy.ai/requestId"
	metaKeyCodeBuddyTraceID      = "codebuddy.ai/traceId"
	metaKeyCodeBuddyTraceParent  = "codebuddy.ai/traceparent"
	metaKeyCodeBuddyMessageID    = "codebuddy.ai/messageId"
	metaKeyCodeBuddyMsgRequestID = "codebuddy.ai/messageRequestId"
	metaKeyCodeBuddyReqModelID   = "codebuddy.ai/requestModelId"
	metaKeyCodeBuddyReqModelName = "codebuddy.ai/requestModelName"
	metaKeyCodeBuddyRespModelID  = "codebuddy.ai/responseModelId"
	metaKeyCodeBuddyFinishReason = "codebuddy.ai/finishReason"
	metaKeyCodeBuddyOutcome      = "codebuddy.ai/outcome"
	metaKeyCodeBuddyAgentPhase   = "codebuddy.ai/agentPhase"
	// metaKeyCodeBuddyErrorMessage carries the refusal payload on a
	// stopReason=refusal PromptResponse: a JSON-encoded
	// {code,message,data} blob whose data.details holds the real cause.
	metaKeyCodeBuddyErrorMessage = "codebuddy.ai/errorMessage"
	// metaKeyCodeBuddyErrorMessageBare is the same payload under an unprefixed
	// key. CodeBuddy uses it on the out-of-band control-command failure path
	// (a plain message string rather than the JSON blob), and both shapes reach
	// the client as stopReason=refusal.
	metaKeyCodeBuddyErrorMessageBare = "errorMessage"
)

// maxErrorDetailRunes bounds the extracted refusal detail so a pathological
// upstream payload cannot bloat every persisted warning block. The details are
// human-readable one-liners (e.g. "Bad substitution: o.gaps.join"), so this is
// far above any real value.
const maxErrorDetailRunes = 300

// refusalDetailFromMeta extracts the human-readable failure reason from a
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
func refusalDetailFromMeta(meta map[string]any) string {
	if len(meta) == 0 {
		return ""
	}
	raw := metaString(meta[metaKeyCodeBuddyErrorMessage])
	if raw == "" {
		raw = metaString(meta[metaKeyCodeBuddyErrorMessageBare])
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

// tokenUsageFromCodeBuddyUsage builds a metaTokenUsage from the OpenAI-style
// _meta.usage block that CodeBuddy reports.
func tokenUsageFromCodeBuddyUsage(m map[string]any) *metaTokenUsage {
	u := &metaTokenUsage{
		InputTokens:         metaInt(m["prompt_tokens"]),
		OutputTokens:        metaInt(m["completion_tokens"]),
		TotalTokens:         metaInt(m["total_tokens"]),
		ThoughtTokens:       metaInt(m["completion_thinking_tokens"]),
		CachedReadTokens:    metaInt(m["prompt_cache_hit_tokens"]),
		CachedWriteTokens:   metaInt(m["prompt_cache_write_tokens"]),
		CacheCreationTokens: metaInt(m["cache_creation_input_tokens"]),
		CacheHitTokens:      metaInt(m["prompt_cache_hit_tokens"]),
		CacheMissTokens:     metaInt(m["prompt_cache_miss_tokens"]),
		Credit:              metaFloat(m["credit"]),
	}
	// Also accept cache_read_input_tokens as a cache-read source.
	u.CachedReadTokens = metaMaxInt(u.CachedReadTokens, metaInt(m["cache_read_input_tokens"]))
	if u.InputTokens != 0 || u.OutputTokens != 0 || u.TotalTokens != 0 ||
		u.CachedReadTokens != 0 || u.CachedWriteTokens != 0 ||
		u.CacheCreationTokens != 0 || u.CacheHitTokens != 0 || u.CacheMissTokens != 0 ||
		u.ThoughtTokens != 0 || u.Credit != 0 {
		u.Present = true
	}
	return u
}

// extractCodeBuddyMeta parses a CodeBuddy _meta payload.
func extractCodeBuddyMeta(meta map[string]any) *metaExtraction {
	if len(meta) == 0 {
		return nil
	}
	ext := &metaExtraction{}

	// OpenAI-style usage block.
	if m, ok := meta[metaKeyCodeBuddyUsage].(map[string]any); ok {
		ext.Usage = tokenUsageFromCodeBuddyUsage(m)
	}

	// Context-window breakdown.
	if m, ok := meta[metaKeyCodeBuddyByCategory].(map[string]any); ok {
		cat := &metaCategoryUsage{Categories: map[string]int64{}}
		for _, k := range []string{"conversation", "tools", "systemPrompt", "skills", "mcp"} {
			if v, ok := m[k]; ok {
				cat.Categories[k] = int64(metaInt(v))
			}
		}
		if len(cat.Categories) > 0 {
			cat.Present = true
		}
		ext.Category = cat
	}

	// Trace/identity namespace.
	t := &metaTrace{
		RequestID:        metaString(meta[metaKeyCodeBuddyRequestID]),
		TraceID:          metaString(meta[metaKeyCodeBuddyTraceID]),
		TraceParent:      metaString(meta[metaKeyCodeBuddyTraceParent]),
		MessageID:        metaString(meta[metaKeyCodeBuddyMessageID]),
		MessageRequestID: metaString(meta[metaKeyCodeBuddyMsgRequestID]),
		RequestModelID:   metaString(meta[metaKeyCodeBuddyReqModelID]),
		RequestModelName: metaString(meta[metaKeyCodeBuddyReqModelName]),
		ResponseModelID:  metaString(meta[metaKeyCodeBuddyRespModelID]),
		FinishReason:     metaString(meta[metaKeyCodeBuddyFinishReason]),
		Outcome:          metaString(meta[metaKeyCodeBuddyOutcome]),
		AgentPhase:       metaString(meta[metaKeyCodeBuddyAgentPhase]),
	}
	// The traceparent header (when the dedicated traceId key is absent) also
	// carries the root trace id as its first component (00-<traceid>-<spanid>-01).
	if t.TraceID == "" && t.TraceParent != "" {
		if parts := splitTraceParent(t.TraceParent); len(parts) >= 2 {
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
