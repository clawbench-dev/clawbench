package ai

// ---------------------------------------------------------------------------
// Claude / Codex ACP _meta adapter
// ---------------------------------------------------------------------------
//
// Claude (`npx @agentclientprotocol/claude-agent-acp`) and Codex
// (`npx @agentclientprotocol/codex-acp`) share the same bridge protocol stack
// and report per-model token counts in the PromptResponse._meta:
//
//	_meta.quota.token_count = {
//	  cachedInputTokens, cachedWriteTokens, inputTokens, outputTokens,
//	  reasoningOutputTokens, totalTokens
//	}
//	_meta.quota.model_usage = [ { model: "...", token_count: {...} } ]
//
// The aggregate token_count is the same shape across both agents. They also
// both advertise capability extensions in initialize._meta (goal, steering,
// jetbrains.air, ...) which carry no per-turn data and are ignored here.

const (
	metaKeyQuota      = "quota"
	metaKeyTokenCount = "token_count"
	metaKeyModelUsage = "model_usage"
)

// tokenUsageFromClaudeTokenCount builds a metaTokenUsage from a Claude/Codex
// _meta.quota.token_count map (shared aggregate and per-model breakdown shape).
func tokenUsageFromClaudeTokenCount(tc map[string]any) *metaTokenUsage {
	u := &metaTokenUsage{
		InputTokens:       metaInt(tc["inputTokens"]),
		OutputTokens:      metaInt(tc["outputTokens"]),
		TotalTokens:       metaInt(tc["totalTokens"]),
		CachedReadTokens:  metaInt(tc["cachedInputTokens"]),
		CachedWriteTokens: metaInt(tc["cachedWriteTokens"]),
		ThoughtTokens:     metaInt(tc["reasoningOutputTokens"]),
	}
	if u.InputTokens != 0 || u.OutputTokens != 0 || u.TotalTokens != 0 ||
		u.CachedReadTokens != 0 || u.CachedWriteTokens != 0 || u.ThoughtTokens != 0 {
		u.Present = true
	}
	return u
}

// extractClaudeMeta parses a Claude/Codex _meta payload.
func extractClaudeMeta(meta map[string]any) *metaExtraction {
	if len(meta) == 0 {
		return nil
	}
	quota, ok := meta[metaKeyQuota].(map[string]any)
	if !ok {
		return nil
	}
	ext := &metaExtraction{}

	// Aggregate per-model token counts.
	// _meta.quota.token_count = { cachedInputTokens, cachedWriteTokens,
	// inputTokens, outputTokens, reasoningOutputTokens, totalTokens }
	//
	// This aggregate is the authoritative single-request token snapshot for the
	// turn. Per-model breakdowns below only identify which model ran — they are
	// NOT merged into Usage, because each model_usage entry can carry its own
	// (sometimes cumulative) counter set and per-field max-merging them would
	// stitch counters from different requests into one inconsistent row.
	if tc, ok := quota[metaKeyTokenCount].(map[string]any); ok {
		ext.Usage = tokenUsageFromClaudeTokenCount(tc)
	}

	// Per-model breakdown. _meta.quota.model_usage = [ { model: "...",
	// token_count: {...} } ]. The model name reveals which model actually ran
	// (Codex can route an alias to a concrete model, e.g. deepseek-v4-flash).
	// It maps to ResponseModelID — "the model that actually responded" — the
	// same semantic as CodeBuddy's codebuddy.ai/responseModelId (its actual
	// serving endpoint, ep-*), so the two agents populate one consistent field.
	// When the top-level aggregate token_count is absent or carries no token
	// counters, the FIRST per-model entry carrying counters is adopted as a
	// fallback snapshot — single entry, never max-stitched across entries.
	if mus, ok := quota[metaKeyModelUsage].([]any); ok && len(mus) > 0 {
		if trace := claudeModelUsageTrace(mus); trace.HasData() {
			ext.Trace = trace
		}
		if ext.Usage == nil || !ext.Usage.hasTokenCounters() {
			ext.Usage = claudeModelUsageFallback(mus)
		}
	}

	if !ext.HasData() {
		return nil
	}
	return ext
}

// claudeModelUsageTrace extracts the response model id from a model_usage
// breakdown (the first entry naming a model).
func claudeModelUsageTrace(mus []any) *metaTrace {
	trace := &metaTrace{}
	for _, m := range mus {
		entry, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if modelName := metaString(entry["model"]); modelName != "" && trace.ResponseModelID == "" {
			trace.ResponseModelID = modelName
		}
	}
	return trace
}

// claudeModelUsageFallback returns the FIRST per-model token_count snapshot
// when no top-level aggregate was present. Single entry — never max-stitched.
func claudeModelUsageFallback(mus []any) *metaTokenUsage {
	for _, m := range mus {
		entry, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if tc, ok := entry[metaKeyTokenCount].(map[string]any); ok {
			if u := tokenUsageFromClaudeTokenCount(tc); u != nil && u.hasTokenCounters() {
				return u
			}
		}
	}
	return nil
}
