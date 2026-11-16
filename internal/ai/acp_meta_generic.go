package ai

// ---------------------------------------------------------------------------
// Generic ACP _meta fallback
// ---------------------------------------------------------------------------
//
// Used for backends without a dedicated adapter (or whose _meta keys are
// unknown). It recursively scans the payload for recognizable token/cost key
// patterns (OpenAI-style `*_tokens`, bridge `*Tokens`, `token_count`) and
// picks up any values found. This keeps future agents working out of the box
// without a per-agent adapter, at the cost of being less precise about key
// ownership.

// tokenKeyPatterns maps a recognized _meta key to its canonical usage field.
// OpenAI-style keys (CodeBuddy) and camelCase bridge keys (Claude/Codex) are
// both covered.
var genericTokenKeys = map[string]func(u *MetaTokenUsage, v any){
	"prompt_tokens":               func(u *MetaTokenUsage, v any) { u.InputTokens = metaInt(v) },
	"inputTokens":                 func(u *MetaTokenUsage, v any) { u.InputTokens = metaInt(v) },
	"completion_tokens":           func(u *MetaTokenUsage, v any) { u.OutputTokens = metaInt(v) },
	"outputTokens":                func(u *MetaTokenUsage, v any) { u.OutputTokens = metaInt(v) },
	"total_tokens":                func(u *MetaTokenUsage, v any) { u.TotalTokens = metaInt(v) },
	"totalTokens":                 func(u *MetaTokenUsage, v any) { u.TotalTokens = metaInt(v) },
	"prompt_cache_hit_tokens":     func(u *MetaTokenUsage, v any) { u.CacheHitTokens = metaInt(v); u.CachedReadTokens = metaInt(v) },
	"cachedInputTokens":           func(u *MetaTokenUsage, v any) { u.CachedReadTokens = metaInt(v) },
	"cache_read_input_tokens":     func(u *MetaTokenUsage, v any) { u.CachedReadTokens = metaInt(v) },
	"prompt_cache_write_tokens":   func(u *MetaTokenUsage, v any) { u.CachedWriteTokens = metaInt(v) },
	"cache_creation_input_tokens": func(u *MetaTokenUsage, v any) { u.CacheCreationTokens = metaInt(v) },
	"cachedWriteTokens":           func(u *MetaTokenUsage, v any) { u.CachedWriteTokens = metaInt(v) },
	"prompt_cache_miss_tokens":    func(u *MetaTokenUsage, v any) { u.CacheMissTokens = metaInt(v) },
	"completion_thinking_tokens":  func(u *MetaTokenUsage, v any) { u.ThoughtTokens = metaInt(v) },
	"reasoningOutputTokens":       func(u *MetaTokenUsage, v any) { u.ThoughtTokens = metaInt(v) },
	"thoughtTokens":               func(u *MetaTokenUsage, v any) { u.ThoughtTokens = metaInt(v) },
	"credit":                      func(u *MetaTokenUsage, v any) { u.Credit = metaFloat(v) },
}

// genericScanDepth bounds the recursive scan to avoid descending into large
// unrelated payloads (e.g. tool inputs).
const genericScanDepth = 4

// extractGenericMeta scans a _meta payload for recognizable token keys.
func extractGenericMeta(meta map[string]any) *MetaExtraction {
	if len(meta) == 0 {
		return nil
	}
	u := &MetaTokenUsage{}
	found := scanMetaForTokens(meta, u, 0)
	if !found {
		return nil
	}
	u.Present = true
	return &MetaExtraction{Usage: u}
}

// scanMetaForTokens recursively walks v, applying known key handlers.
func scanMetaForTokens(v any, u *MetaTokenUsage, depth int) bool {
	if depth > genericScanDepth || v == nil {
		return false
	}
	found := false
	switch val := v.(type) {
	case map[string]any:
		for k, sub := range val {
			if handler, ok := genericTokenKeys[k]; ok {
				handler(u, sub)
				found = true
				continue
			}
			if scanMetaForTokens(sub, u, depth+1) {
				found = true
			}
		}
	case []any:
		for _, sub := range val {
			if scanMetaForTokens(sub, u, depth+1) {
				found = true
			}
		}
	}
	return found
}
