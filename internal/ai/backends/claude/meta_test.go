package claude

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractClaudeMeta_Quota(t *testing.T) {
	meta := map[string]any{
		"quota": map[string]any{
			"token_count": map[string]any{
				"cachedInputTokens":     0,
				"cachedWriteTokens":     41053,
				"inputTokens":           142,
				"outputTokens":          18,
				"reasoningOutputTokens": 0,
				"totalTokens":           41213,
			},
		},
	}
	ext := ExtractMeta(meta)
	require.NotNil(t, ext)
	require.NotNil(t, ext.Usage)
	u := ext.Usage
	assert.True(t, u.Present)
	// inputTokens is the UNCACHED remainder in this bridge's shape
	// (total = input+output+cacheRead+cacheWrite), so it is normalized to the
	// full prompt: 142 + 0 + 41053.
	assert.Equal(t, 41195, u.InputTokens)
	assert.Equal(t, 18, u.OutputTokens)
	assert.Equal(t, 41213, u.TotalTokens)
	assert.Equal(t, 41053, u.CachedWriteTokens)
	assert.Equal(t, 0, u.CachedReadTokens)
	assert.Equal(t, 0, u.ThoughtTokens)
	// Cache split derived from the same identity: no cache read → 0% hit rate
	// with the whole prompt counted as a miss.
	assert.Equal(t, 0, u.CacheHitTokens)
	assert.Equal(t, 142, u.CacheMissTokens)
	assert.Equal(t, 41213, u.InputTokens+u.OutputTokens, "row stays internally consistent")
}

func TestExtractClaudeMeta_ReasoningTokens(t *testing.T) {
	// Codex reports reasoningOutputTokens.
	meta := map[string]any{
		"quota": map[string]any{
			"token_count": map[string]any{
				"inputTokens":           13085,
				"outputTokens":          32,
				"reasoningOutputTokens": 30,
				"totalTokens":           13117,
			},
		},
	}
	ext := ExtractMeta(meta)
	require.NotNil(t, ext)
	require.NotNil(t, ext.Usage)
	assert.Equal(t, 30, ext.Usage.ThoughtTokens)
	assert.Equal(t, 13117, ext.Usage.TotalTokens)
}

func TestExtractClaudeMeta_MissingQuota(t *testing.T) {
	assert.Nil(t, ExtractMeta(nil))
	assert.Nil(t, ExtractMeta(map[string]any{"goal": map[string]any{"supported": true}}))
	// quota without token_count or model_usage → nil.
	assert.Nil(t, ExtractMeta(map[string]any{"quota": map[string]any{"model_usage": []any{}}}))
}

func TestExtractClaudeMeta_ModelUsage(t *testing.T) {
	// Real Codex wire shape: model_usage[].model reveals the actually-executed
	// model (an alias can route to a concrete model), which must surface as
	// the trace's ResponseModelID.
	meta := map[string]any{
		"quota": map[string]any{
			"model_usage": []any{
				map[string]any{
					"model": "deepseek-v4-flash",
					"token_count": map[string]any{
						"cachedInputTokens": 12672,
						"inputTokens":       105,
						"outputTokens":      2,
						"totalTokens":       12779,
					},
				},
			},
			"token_count": map[string]any{
				"cachedInputTokens": 12672,
				"inputTokens":       105,
				"outputTokens":      2,
				"totalTokens":       12779,
			},
		},
	}
	ext := ExtractMeta(meta)
	require.NotNil(t, ext)
	// Aggregate usage extracted from token_count. inputTokens excludes the
	// cached portion, so it is normalized to 105 + 12672 = 12777.
	require.NotNil(t, ext.Usage)
	assert.Equal(t, 12777, ext.Usage.InputTokens)
	assert.Equal(t, 2, ext.Usage.OutputTokens)
	assert.Equal(t, 12779, ext.Usage.TotalTokens)
	assert.Equal(t, 12672, ext.Usage.CachedReadTokens)
	assert.Equal(t, 12672, ext.Usage.CacheHitTokens)
	assert.Equal(t, 105, ext.Usage.CacheMissTokens)
	// Model name surfaced as ResponseModelID.
	require.NotNil(t, ext.Trace)
	assert.Equal(t, "deepseek-v4-flash", ext.Trace.ResponseModelID)
}

func TestExtractClaudeMeta_AggregateWinsOverModelUsage(t *testing.T) {
	// When both top-level aggregate token_count AND per-model entries are
	// present but disagree, the aggregate is authoritative (per-model counters
	// must never be merged in — that is what produced total==10x input rows).
	meta := map[string]any{
		"quota": map[string]any{
			"model_usage": []any{
				map[string]any{
					"model": "claude-opus-4-6",
					"token_count": map[string]any{
						"inputTokens": 14170, "outputTokens": 246, "totalTokens": 147352,
					},
				},
			},
			"token_count": map[string]any{
				"inputTokens": 45575, "outputTokens": 52, "totalTokens": 45627,
			},
		},
	}
	ext := ExtractMeta(meta)
	require.NotNil(t, ext)
	require.NotNil(t, ext.Usage)
	assert.Equal(t, 45575, ext.Usage.InputTokens, "aggregate wins over per-model")
	assert.Equal(t, 52, ext.Usage.OutputTokens)
	assert.Equal(t, 45627, ext.Usage.TotalTokens)
	require.NotNil(t, ext.Trace)
	assert.Equal(t, "claude-opus-4-6", ext.Trace.ResponseModelID)
}

func TestExtractClaudeMeta_ZeroAggregateFallsBackToModelUsage(t *testing.T) {
	// Top-level token_count present but all-zero (a zeroed final snapshot):
	// must still fall back to the per-model entry that carries real counters.
	meta := map[string]any{
		"quota": map[string]any{
			"model_usage": []any{
				map[string]any{
					"model": "claude-sonnet-4-6",
					"token_count": map[string]any{
						"inputTokens": 142, "outputTokens": 44, "totalTokens": 40904,
					},
				},
			},
			"token_count": map[string]any{},
		},
	}
	ext := ExtractMeta(meta)
	require.NotNil(t, ext)
	require.NotNil(t, ext.Usage)
	assert.Equal(t, 142, ext.Usage.InputTokens, "per-model counters used when aggregate is all-zero")
	assert.Equal(t, 44, ext.Usage.OutputTokens)
	assert.Equal(t, 40904, ext.Usage.TotalTokens)
}

func TestExtractClaudeMeta_ModelUsage_NoAggregate(t *testing.T) {
	// model_usage present but no top-level token_count: usage still extracted
	// from the per-model entries, model name still surfaced.
	meta := map[string]any{
		"quota": map[string]any{
			"model_usage": []any{
				map[string]any{
					"model": "claude-sonnet-4-6",
					"token_count": map[string]any{
						"inputTokens":  142,
						"outputTokens": 44,
						"totalTokens":  40904,
					},
				},
			},
		},
	}
	ext := ExtractMeta(meta)
	require.NotNil(t, ext)
	require.NotNil(t, ext.Usage)
	assert.Equal(t, 142, ext.Usage.InputTokens)
	assert.Equal(t, 44, ext.Usage.OutputTokens)
	assert.Equal(t, 40904, ext.Usage.TotalTokens)
	require.NotNil(t, ext.Trace)
	assert.Equal(t, "claude-sonnet-4-6", ext.Trace.ResponseModelID)
}

// ---------------------------------------------------------------------------
// extractGenericMeta — recursive fallback for unknown backends
// ---------------------------------------------------------------------------

func TestExtractClaudeMeta_MultiModelUsageNoStitching(t *testing.T) {
	// Two per-model entries each with their own cumulative-ish total: without a
	// top-level aggregate the FIRST informative entry is adopted as the
	// snapshot — never max-stitched across entries (the old code produced
	// total == 10x input outliers here).
	meta := map[string]any{
		"quota": map[string]any{
			"model_usage": []any{
				map[string]any{
					"model":       "claude-sonnet-4-6",
					"token_count": map[string]any{"inputTokens": 45575, "outputTokens": 52, "totalTokens": 45627},
				},
				map[string]any{
					"model":       "claude-opus-4-6",
					"token_count": map[string]any{"inputTokens": 14170, "outputTokens": 246, "totalTokens": 147352},
				},
			},
		},
	}
	ext := ExtractMeta(meta)
	require.NotNil(t, ext)
	require.NotNil(t, ext.Usage)
	assert.Equal(t, 45575, ext.Usage.InputTokens)
	assert.Equal(t, 52, ext.Usage.OutputTokens)
	assert.Equal(t, 45627, ext.Usage.TotalTokens)
	assert.Equal(t, 45627, ext.Usage.InputTokens+ext.Usage.OutputTokens, "single entry snapshot stays consistent")
	require.NotNil(t, ext.Trace)
	assert.Equal(t, "claude-sonnet-4-6", ext.Trace.ResponseModelID)
}

// ---------------------------------------------------------------------------
// ai.MetaExtraction.traceRequestID — nil-safe accessor used by replay-drop logging
// ---------------------------------------------------------------------------

// TestMetaExtraction_TraceRequestID_NilReceiver covers the nil guard: the accessor
// is called on the result of extractMetaUsage, which returns nil when the backend
// has no adapter for the payload. Logging must not panic in that case, because it
// runs on the event path for every replayed usage_update.
