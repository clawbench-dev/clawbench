package ai

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
)

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
