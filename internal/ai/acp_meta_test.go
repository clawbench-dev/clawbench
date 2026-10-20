package ai

import (
	"context"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// extractCodeBuddyMeta — OpenAI-style usage + usageByCategory + trace
// ---------------------------------------------------------------------------

func TestExtractCodeBuddyMeta_Usage(t *testing.T) {
	meta := map[string]any{
		"usage": map[string]any{
			"prompt_tokens":               29495,
			"completion_tokens":           3,
			"completion_thinking_tokens":  0,
			"prompt_cache_hit_tokens":     8192,
			"prompt_cache_miss_tokens":    21303,
			"prompt_cache_write_tokens":   0,
			"cache_creation_input_tokens": 0,
			"credit":                      1.48,
		},
	}
	ext := extractCodeBuddyMeta(meta)
	require.NotNil(t, ext)
	require.NotNil(t, ext.Usage)
	u := ext.Usage
	assert.True(t, u.Present)
	assert.Equal(t, 29495, u.InputTokens)
	assert.Equal(t, 3, u.OutputTokens)
	assert.Equal(t, 8192, u.CacheHitTokens)
	assert.Equal(t, 8192, u.CachedReadTokens)
	assert.Equal(t, 21303, u.CacheMissTokens)
	assert.Equal(t, 1.48, u.Credit)
}

func TestExtractCodeBuddyMeta_UsageByCategory(t *testing.T) {
	meta := map[string]any{
		"codebuddy.ai/usageByCategory": map[string]any{
			"conversation": 3894,
			"tools":        22701,
			"systemPrompt": 1769,
			"skills":       1121,
			"mcp":          10,
			"version":      1,
		},
	}
	ext := extractCodeBuddyMeta(meta)
	require.NotNil(t, ext)
	require.NotNil(t, ext.Category)
	assert.True(t, ext.Category.Present)
	assert.Equal(t, int64(3894), ext.Category.Categories["conversation"])
	assert.Equal(t, int64(22701), ext.Category.Categories["tools"])
	assert.Equal(t, int64(1769), ext.Category.Categories["systemPrompt"])
	assert.Equal(t, int64(1121), ext.Category.Categories["skills"])
	assert.Equal(t, int64(10), ext.Category.Categories["mcp"])
}

func TestExtractCodeBuddyMeta_Trace(t *testing.T) {
	meta := map[string]any{
		"codebuddy.ai/requestId":        "req-123",
		"codebuddy.ai/traceId":          "trace-456",
		"codebuddy.ai/messageId":        "msg-789",
		"codebuddy.ai/messageRequestId": "msgreq-abc",
		"codebuddy.ai/requestModelId":   "glm-5.1",
		"codebuddy.ai/requestModelName": "GLM-5.1",
		"codebuddy.ai/responseModelId":  "ep-b3mrev6r",
		"codebuddy.ai/finishReason":     "stop",
		"codebuddy.ai/outcome":          "SUCCESS",
		"codebuddy.ai/agentPhase":       "completing",
	}
	ext := extractCodeBuddyMeta(meta)
	require.NotNil(t, ext)
	require.NotNil(t, ext.Trace)
	tr := ext.Trace
	assert.True(t, tr.HasData())
	assert.Equal(t, "req-123", tr.RequestID)
	assert.Equal(t, "trace-456", tr.TraceID)
	assert.Equal(t, "msg-789", tr.MessageID)
	assert.Equal(t, "msgreq-abc", tr.MessageRequestID)
	assert.Equal(t, "glm-5.1", tr.RequestModelID)
	assert.Equal(t, "GLM-5.1", tr.RequestModelName)
	assert.Equal(t, "ep-b3mrev6r", tr.ResponseModelID)
	assert.Equal(t, "stop", tr.FinishReason)
	assert.Equal(t, "SUCCESS", tr.Outcome)
	assert.Equal(t, "completing", tr.AgentPhase)
}

func TestExtractCodeBuddyMeta_TraceParentFallsBackToTraceID(t *testing.T) {
	// When the dedicated traceId key is absent, the W3C traceparent header's
	// second component carries the root trace id.
	meta := map[string]any{
		"codebuddy.ai/requestId":   "req-123",
		"codebuddy.ai/messageId":   "msg-789",
		"codebuddy.ai/traceparent": "00-fa10f96dcc82d9db346922057b96c706-97fb1ee7db62a4ab-01",
	}
	ext := extractCodeBuddyMeta(meta)
	require.NotNil(t, ext)
	require.NotNil(t, ext.Trace)
	tr := ext.Trace
	assert.Equal(t, "fa10f96dcc82d9db346922057b96c706", tr.TraceID)
	assert.Equal(t, "00-fa10f96dcc82d9db346922057b96c706-97fb1ee7db62a4ab-01", tr.TraceParent)
}

func TestExtractCodeBuddyMeta_Empty(t *testing.T) {
	assert.Nil(t, extractCodeBuddyMeta(nil))
	assert.Nil(t, extractCodeBuddyMeta(map[string]any{}))
	// Only unrecognized keys → nil (no data).
	assert.Nil(t, extractCodeBuddyMeta(map[string]any{"timestamp": "2026-01-01"}))
}

// ---------------------------------------------------------------------------
// extractClaudeMeta — quota.token_count (Claude / Codex shared shape)
// ---------------------------------------------------------------------------

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
	ext := extractClaudeMeta(meta)
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
	ext := extractClaudeMeta(meta)
	require.NotNil(t, ext)
	require.NotNil(t, ext.Usage)
	assert.Equal(t, 30, ext.Usage.ThoughtTokens)
	assert.Equal(t, 13117, ext.Usage.TotalTokens)
}

func TestExtractClaudeMeta_MissingQuota(t *testing.T) {
	assert.Nil(t, extractClaudeMeta(nil))
	assert.Nil(t, extractClaudeMeta(map[string]any{"goal": map[string]any{"supported": true}}))
	// quota without token_count or model_usage → nil.
	assert.Nil(t, extractClaudeMeta(map[string]any{"quota": map[string]any{"model_usage": []any{}}}))
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
	ext := extractClaudeMeta(meta)
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
	ext := extractClaudeMeta(meta)
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
	ext := extractClaudeMeta(meta)
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
	ext := extractClaudeMeta(meta)
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

func TestExtractGenericMeta_OpenAIKeys(t *testing.T) {
	meta := map[string]any{
		"nested": map[string]any{
			"usage": map[string]any{
				"prompt_tokens":            100,
				"completion_tokens":        20,
				"prompt_cache_hit_tokens":  50,
				"prompt_cache_miss_tokens": 50,
			},
		},
	}
	ext := extractGenericMeta(meta)
	require.NotNil(t, ext)
	require.NotNil(t, ext.Usage)
	assert.Equal(t, 100, ext.Usage.InputTokens)
	assert.Equal(t, 20, ext.Usage.OutputTokens)
	assert.Equal(t, 50, ext.Usage.CacheHitTokens)
	assert.Equal(t, 50, ext.Usage.CacheMissTokens)
}

func TestExtractGenericMeta_BridgeKeys(t *testing.T) {
	meta := map[string]any{
		"quota": map[string]any{
			"token_count": map[string]any{
				"cachedWriteTokens": 5,
				"totalTokens":       30,
			},
		},
	}
	ext := extractGenericMeta(meta)
	require.NotNil(t, ext)
	assert.Equal(t, 5, ext.Usage.CachedWriteTokens)
	assert.Equal(t, 30, ext.Usage.TotalTokens)
}

func TestExtractGenericMeta_NoMatches(t *testing.T) {
	assert.Nil(t, extractGenericMeta(map[string]any{"foo": "bar", "baz": []any{1, 2, 3}}))
}

// ---------------------------------------------------------------------------
// extractMetaUsage dispatch
// ---------------------------------------------------------------------------

func TestExtractMetaUsage_Dispatch(t *testing.T) {
	cbMeta := map[string]any{"usage": map[string]any{"prompt_tokens": 10}}
	ext := extractMetaUsage("codebuddy", cbMeta)
	require.NotNil(t, ext)
	assert.Equal(t, 10, ext.Usage.InputTokens)

	clMeta := map[string]any{"quota": map[string]any{"token_count": map[string]any{"inputTokens": 5}}}
	for _, backend := range []string{"claude", "codex", "qoder"} {
		ext := extractMetaUsage(backend, clMeta)
		require.NotNil(t, ext, backend)
		assert.Equal(t, 5, ext.Usage.InputTokens, backend)
	}

	// Unknown backend falls through to generic scan.
	ext = extractMetaUsage("mystery-agent", map[string]any{"deep": map[string]any{"outputTokens": 7}})
	require.NotNil(t, ext)
	assert.Equal(t, 7, ext.Usage.OutputTokens)

	// OpenCode reports no _meta extensions.
	assert.Nil(t, extractMetaUsage("opencode", map[string]any{"timestamp": "t"}))
}

// ---------------------------------------------------------------------------
// applyMetaExtractionToUsageState / applyMetaExtractionToMetadata
// ---------------------------------------------------------------------------

func TestApplyMetaExtractionToUsageState(t *testing.T) {
	ext := &metaExtraction{
		Usage: &metaTokenUsage{
			Present:             true,
			InputTokens:         29495,
			OutputTokens:        3,
			CachedReadTokens:    8192,
			CacheHitTokens:      8192,
			CacheMissTokens:     21303,
			CacheCreationTokens: 0,
			Credit:              1.48,
		},
		Category: &metaCategoryUsage{Present: true, Categories: map[string]int64{"tools": 22701}},
	}
	state := &UsageState{Used: 29495, Size: 200000}
	applyMetaExtractionToUsageState(state, ext)
	assert.Equal(t, 29495, state.InputTokens)
	assert.Equal(t, 3, state.OutputTokens)
	assert.Equal(t, 8192, state.CachedReadTokens)
	assert.Equal(t, 8192, state.CacheHitTokens)
	assert.Equal(t, 21303, state.CacheMissTokens)
	assert.Equal(t, 1.48, state.Credit)
	assert.Equal(t, int64(22701), state.UsageByCategory["tools"])
}

func TestApplyMetaExtractionToMetadata(t *testing.T) {
	ext := &metaExtraction{
		Usage: &metaTokenUsage{
			Present:           true,
			InputTokens:       142,
			OutputTokens:      18,
			TotalTokens:       41213,
			CachedWriteTokens: 41053,
			ThoughtTokens:     30,
		},
		Trace: &metaTrace{
			RequestID:        "req-1",
			TraceID:          "trace-1",
			MessageID:        "msg-1",
			MessageRequestID: "msgreq-1",
			RequestModelID:   "glm-5.1",
			RequestModelName: "GLM-5.1",
			ResponseModelID:  "ep-x",
			FinishReason:     "stop",
			Outcome:          "SUCCESS",
			AgentPhase:       "completing",
		},
		Category: &metaCategoryUsage{Present: true, Categories: map[string]int64{"conversation": 3894}},
	}
	meta := &Metadata{Model: "preset-model"}
	applyMetaExtractionToMetadata(meta, ext)
	assert.Equal(t, 142, meta.InputTokens)
	assert.Equal(t, 18, meta.OutputTokens)
	assert.Equal(t, 41213, meta.TotalTokens)
	assert.Equal(t, 41053, meta.CachedWriteTokens)
	assert.Equal(t, 30, meta.ThoughtTokens)
	assert.Equal(t, "req-1", meta.RequestID)
	assert.Equal(t, "trace-1", meta.TraceID)
	assert.Equal(t, "msg-1", meta.MessageID)
	assert.Equal(t, "msgreq-1", meta.MessageRequestID)
	// Existing model preserved — RequestModelID only fills when empty.
	assert.Equal(t, "preset-model", meta.Model)
	assert.Equal(t, "GLM-5.1", meta.RequestModelName)
	assert.Equal(t, "ep-x", meta.ResponseModelID)
	assert.Equal(t, "stop", meta.FinishReason)
	assert.Equal(t, "SUCCESS", meta.Outcome)
	assert.Equal(t, int64(3894), meta.UsageByCategory["conversation"])
}

// ---------------------------------------------------------------------------
// mapACPSessionUpdate — usage_update merges per-agent _meta
// ---------------------------------------------------------------------------

func TestMapACPSessionUpdate_UsageUpdate_CodeBuddyMeta(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}
	meta := map[string]any{
		"usage": map[string]any{
			"prompt_tokens":            29495,
			"completion_tokens":        3,
			"prompt_cache_hit_tokens":  8192,
			"prompt_cache_miss_tokens": 21303,
			"credit":                   1.48,
		},
		"codebuddy.ai/usageByCategory": map[string]any{"tools": 22701},
	}
	update := acp.SessionUpdate{
		UsageUpdate: &acp.SessionUsageUpdate{
			Meta: meta,
			Used: 29495,
			Size: 200000,
		},
	}
	mapACPSessionUpdate(update, ch, context.Background(), conn, nil)

	var found *StreamEvent
	select {
	case evt := <-ch:
		found = &evt
	default:
		t.Fatal("expected usage_update event")
	}
	require.NotNil(t, found.Usage)
	assert.Equal(t, 29495, found.Usage.Used)
	assert.Equal(t, 200000, found.Usage.Size)
	assert.Equal(t, 29495, found.Usage.InputTokens)
	assert.Equal(t, 3, found.Usage.OutputTokens)
	assert.Equal(t, 8192, found.Usage.CacheHitTokens)
	assert.Equal(t, 21303, found.Usage.CacheMissTokens)
	assert.Equal(t, 1.48, found.Usage.Credit)
	assert.Equal(t, int64(22701), found.Usage.UsageByCategory["tools"])

	// Cached state also reflects the merged usage.
	require.NotNil(t, conn.GetCachedUsageState())
	assert.Equal(t, 29495, conn.GetCachedUsageState().InputTokens)
}

func TestMapACPSessionUpdate_AgentMessageChunk_AccumulatesMeta(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}
	update := acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "好"}},
			Meta: map[string]any{
				"codebuddy.ai/requestId": "req-123",
				"codebuddy.ai/messageId": "msg-456",
			},
		},
	}
	mapACPSessionUpdate(update, ch, context.Background(), conn, nil)

	// Content events emitted (thinking_done + content).
	var types []string
collectLoop:
	for range 2 {
		select {
		case evt := <-ch:
			types = append(types, evt.Type)
		default:
			break collectLoop
		}
	}
	assert.Contains(t, types, "content")

	// Meta accumulated on the connection.
	acc := conn.getMetaAccum()
	require.NotNil(t, acc)
	require.NotNil(t, acc.Trace)
	assert.Equal(t, "req-123", acc.Trace.RequestID)
	assert.Equal(t, "msg-456", acc.Trace.MessageID)
}

func TestMapACPSessionUpdate_UsageUpdate_NoMeta(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{agent: &model.Agent{ID: "opencode", Backend: "opencode"}}
	update := acp.SessionUpdate{
		UsageUpdate: &acp.SessionUsageUpdate{Used: 12576, Size: 204800},
	}
	mapACPSessionUpdate(update, ch, context.Background(), conn, nil)

	select {
	case evt := <-ch:
		assert.Equal(t, 12576, evt.Usage.Used)
		assert.Equal(t, 0, evt.Usage.InputTokens)
		assert.Empty(t, evt.Usage.UsageByCategory)
	default:
		t.Fatal("expected usage_update event")
	}
}

// ---------------------------------------------------------------------------
// MergeUsageState — partial-update merge semantics (context panel zeros bug)
// ---------------------------------------------------------------------------

// TestMergeUsageState_NakedZeroDoesNotRegressExisting verifies the core
// regression: a naked usage_update (used=0, size=0, cost only) must NOT wipe a
// previously-known context window. This is the "context panel shows all zeros
// after many turns" bug — CodeBuddy sends such naked notifications between the
// informative ones, and unconditional overwrite let a trailing naked one wipe
// {used:300k, size:1e6} to {0,0}.
func TestMergeUsageState_NakedZeroDoesNotRegressExisting(t *testing.T) {
	existing := &UsageState{
		Used:            300000,
		Size:            1000000,
		InputTokens:     301000,
		OutputTokens:    5000,
		Cost:            5.0,
		CacheHitTokens:  290000,
		UsageByCategory: map[string]int64{"conversation": 250000, "tools": 50000},
	}
	// Naked notification: only cost, used=0, size=0, no _meta.
	naked := &UsageState{Used: 0, Size: 0, Cost: 6.08}

	merged := MergeUsageState(existing, naked)

	// Known window is a session invariant — never regressed by absent values.
	assert.Equal(t, 1000000, merged.Size, "size must stay sticky")
	assert.Equal(t, 300000, merged.Used, "used must not regress to 0")
	// Token/cache/category fields keep existing values (partial notification omitted them).
	assert.Equal(t, 301000, merged.InputTokens)
	assert.Equal(t, 5000, merged.OutputTokens)
	assert.Equal(t, 290000, merged.CacheHitTokens)
	assert.Equal(t, int64(250000), merged.UsageByCategory["conversation"])
	// Cost is monotonic cumulative — the naked notification's higher cost applies.
	assert.Equal(t, 6.08, merged.Cost)
}

// TestMergeUsageState_UsedZeroButCategoryFallback verifies that when a
// notification carries used=0 but provides the usageByCategory breakdown
// (CodeBuddy's authoritative context occupancy), used is derived from the
// category sum instead of staying 0.
func TestMergeUsageState_UsedZeroButCategoryFallback(t *testing.T) {
	existing := &UsageState{Used: 1000, Size: 200000, Cost: 1.0}
	incoming := &UsageState{
		Used:            0,
		Size:            200000,
		UsageByCategory: map[string]int64{"conversation": 8000, "tools": 2000, "systemPrompt": 500},
	}

	merged := MergeUsageState(existing, incoming)

	assert.Equal(t, 10500, merged.Used, "used should fall back to the usageByCategory sum")
	assert.Equal(t, 200000, merged.Size)
	assert.Equal(t, 1.0, merged.Cost, "cost must not regress when incoming cost is 0")
	assert.Equal(t, int64(8000), merged.UsageByCategory["conversation"])
}

// TestMergeUsageState_RealUpdateApplies verifies that a genuinely informative
// notification (non-zero used/size) still updates the state normally.
func TestMergeUsageState_RealUpdateApplies(t *testing.T) {
	existing := &UsageState{Used: 1000, Size: 200000, Cost: 1.0}
	incoming := &UsageState{Used: 15000, Size: 200000, Cost: 1.2}

	merged := MergeUsageState(existing, incoming)

	assert.Equal(t, 15000, merged.Used)
	assert.Equal(t, 200000, merged.Size)
	assert.Equal(t, 1.2, merged.Cost)
}

// TestMergeUsageState_ExistingNil verifies the first-observation path: an
// incoming notification is adopted as-is when there is no prior state.
func TestMergeUsageState_ExistingNil(t *testing.T) {
	incoming := &UsageState{
		Used: 0, Size: 0, Cost: 0.5,
		UsageByCategory: map[string]int64{"conversation": 100},
	}

	merged := MergeUsageState(nil, incoming)

	require.NotNil(t, merged)
	assert.Equal(t, 0, merged.Used, "no existing state — nothing to protect, incoming used as-is")
	assert.Equal(t, int64(100), merged.UsageByCategory["conversation"])

	// The result must not alias the input map (later mutations must not leak).
	merged.UsageByCategory["conversation"] = 999
	assert.Equal(t, int64(100), incoming.UsageByCategory["conversation"])
}

// TestMergeUsageState_ZeroCategoryKeepsExisting verifies an incoming empty
// (absent) category breakdown keeps the existing one rather than clearing it.
func TestMergeUsageState_ZeroCategoryKeepsExisting(t *testing.T) {
	existing := &UsageState{
		Used:            100,
		Size:            200000,
		UsageByCategory: map[string]int64{"conversation": 80},
	}
	incoming := &UsageState{Used: 100, Size: 200000} // no category

	merged := MergeUsageState(existing, incoming)

	assert.Equal(t, int64(80), merged.UsageByCategory["conversation"])
}

// TestMapACPSessionUpdate_UsageUpdate_NakedZeroKeepsCachedState verifies the
// notification-path guard: a naked used=0/size=0 usage_update must not wipe a
// previously-cached non-zero window on the connection.
func TestMapACPSessionUpdate_UsageUpdate_NakedZeroKeepsCachedState(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}
	// First: a real usage_update establishes the window.
	conn.SetCachedUsageState(&UsageState{Used: 300000, Size: 1000000, InputTokens: 301000})

	// Then a naked notification arrives (used=0, size=0, cost only).
	naked := acp.SessionUpdate{
		UsageUpdate: &acp.SessionUsageUpdate{Used: 0, Size: 0, Cost: &acp.Cost{Amount: 6.08}},
	}
	mapACPSessionUpdate(naked, ch, context.Background(), conn, nil)

	// The forwarded event reflects what the agent actually said.
	select {
	case evt := <-ch:
		require.NotNil(t, evt.Usage)
		assert.Equal(t, 0, evt.Usage.Used, "forwarded event keeps the agent's raw values")
		assert.Equal(t, 0, evt.Usage.Size)
	default:
		t.Fatal("expected usage_update event")
	}

	// But the cached state must retain the previously-known window.
	cached := conn.GetCachedUsageState()
	require.NotNil(t, cached)
	assert.Equal(t, 1000000, cached.Size, "cached window must survive a naked notification")
	assert.Equal(t, 300000, cached.Used, "cached used must survive a naked notification")
	assert.Equal(t, 0.0, cached.Cost, "CodeBuddy's usage_update.cost carries credit, not money — discarded")
}

// TestMapACPSessionUpdate_UsageUpdate_CodeBuddyCostDiscarded is the regression
// guard for the CodeBuddy credit-as-cost defect: CodeBuddy repurposes the
// ACP-standard usage_update.cost field to carry its credit consumption (with an
// empty currency), so ingesting it as UsageState.Cost would persist a unit-less
// credit number into chat_metadata.cost_usd and corrupt the cost stats.
func TestMapACPSessionUpdate_UsageUpdate_CodeBuddyCostDiscarded(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}

	update := acp.SessionUpdate{
		UsageUpdate: &acp.SessionUsageUpdate{
			Used: 29495,
			Size: 200000,
			Cost: &acp.Cost{Amount: 1.48}, // CodeBuddy credit, empty currency
			Meta: map[string]any{
				"usage": map[string]any{"credit": 1.48},
			},
		},
	}
	mapACPSessionUpdate(update, ch, context.Background(), conn, nil)

	var evt *StreamEvent
	select {
	case e := <-ch:
		evt = &e
	default:
		t.Fatal("expected usage_update event")
	}
	require.NotNil(t, evt.Usage)
	assert.Equal(t, 0.0, evt.Usage.Cost, "CodeBuddy cost field must be discarded")
	assert.Equal(t, "", evt.Usage.Currency, "no fabricated currency for the discarded cost")
	// The genuine credit still flows through the _meta extension.
	assert.Equal(t, 1.48, evt.Usage.Credit, "real credit from _meta.usage.credit is preserved")

	cached := conn.GetCachedUsageState()
	require.NotNil(t, cached)
	assert.Equal(t, 0.0, cached.Cost, "discarded cost must not be cached")
	assert.Equal(t, 1.48, cached.Credit)
}

// TestMapACPSessionUpdate_UsageUpdate_ClaudeCostPreserved verifies the discard
// is scoped to CodeBuddy: a backend reporting a genuine monetary cost (Claude
// reports USD on the final usage_update of a turn) still populates Cost.
func TestMapACPSessionUpdate_UsageUpdate_ClaudeCostPreserved(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{agent: &model.Agent{ID: "cl", Backend: "claude"}}

	update := acp.SessionUpdate{
		UsageUpdate: &acp.SessionUsageUpdate{
			Used: 53000,
			Size: 200000,
			Cost: &acp.Cost{Amount: 0.1568, Currency: "USD"},
		},
	}
	mapACPSessionUpdate(update, ch, context.Background(), conn, nil)

	var evt *StreamEvent
	select {
	case e := <-ch:
		evt = &e
	default:
		t.Fatal("expected usage_update event")
	}
	require.NotNil(t, evt.Usage)
	assert.InDelta(t, 0.1568, evt.Usage.Cost, 0.0001, "Claude's real USD cost must be preserved")
	assert.Equal(t, "USD", evt.Usage.Currency)
}

// TestMergeUsageState_AuthoritativeWindowWithZeroUsed verifies the distinction
// between a real empty/compacted context (used=0 with a reported window
// size>0 — trust it) and a naked notification (size=0 — must not regress).
func TestMergeUsageState_AuthoritativeWindowWithZeroUsed(t *testing.T) {
	existing := &UsageState{Used: 300000, Size: 1000000, Cost: 5.0}
	// Agent reports a real window with empty context (new session/compaction).
	incoming := &UsageState{Used: 0, Size: 200000, Cost: 5.5}

	merged := MergeUsageState(existing, incoming)

	assert.Equal(t, 200000, merged.Size, "a reported window replaces the stale one")
	assert.Equal(t, 0, merged.Used, "a genuine 0 used within a reported window is trusted — not sticky")
	assert.Equal(t, 5.5, merged.Cost)
}

// TestMergeUsageState_NonZeroDropIsAccepted verifies that a genuine non-zero
// used drop (turn-to-turn fluctuation) is applied, not mistakenly sticky.
func TestMergeUsageState_NonZeroDropIsAccepted(t *testing.T) {
	existing := &UsageState{Used: 50000, Size: 200000, Cost: 3.0}
	incoming := &UsageState{Used: 30000, Size: 200000, Cost: 3.2}

	merged := MergeUsageState(existing, incoming)

	assert.Equal(t, 30000, merged.Used, "a genuine non-zero drop within a reported window is accepted")
	assert.Equal(t, 200000, merged.Size)
	assert.Equal(t, 3.2, merged.Cost)
}

// TestMergeUsageState_NakedZeroWithWindowBackfill verifies that a naked
// (size=0, used=0) notification keeps the known window, while a notification
// with a window plus a populated usageByCategory (but used omitted) backfills
// used from the category sum.
func TestMergeUsageState_NakedZeroWithWindowBackfill(t *testing.T) {
	// Naked (size=0, used=0) — keep existing.
	existing := &UsageState{Used: 300000, Size: 1000000, Cost: 5.0}
	merged := MergeUsageState(existing, &UsageState{Used: 0, Size: 0, Cost: 5.5})
	assert.Equal(t, 300000, merged.Used)
	assert.Equal(t, 1000000, merged.Size)

	// Window + populated category but used omitted — backfill from the sum.
	existing2 := &UsageState{Used: 1000, Size: 200000, Cost: 5.0}
	incoming2 := &UsageState{
		Size:            200000,
		Used:            0,
		UsageByCategory: map[string]int64{"conversation": 8000, "tools": 2000},
	}
	merged2 := MergeUsageState(existing2, incoming2)
	assert.Equal(t, 10000, merged2.Used, "used backfilled from the usageByCategory sum")
	assert.Equal(t, 200000, merged2.Size)
}

// ---------------------------------------------------------------------------
// metaMergeExtraction — latest-informative-snapshot semantics
// ---------------------------------------------------------------------------

func usageExt(i, o, t int) *metaExtraction {
	return &metaExtraction{Usage: &metaTokenUsage{Present: true, InputTokens: i, OutputTokens: o, TotalTokens: t}}
}

func TestMetaMergeExtraction_LatestInformativeSnapshotReplaces(t *testing.T) {
	// A later informative notification is a full re-report of current usage and
	// must REPLACE the accumulator wholesale — even when an earlier one carried
	// a larger input (the pre-change max-stitch kept the bigger value and
	// produced internally inconsistent input/output/total).
	var acc metaExtraction
	metaMergeExtraction(&acc, usageExt(32635, 84, 32719))
	metaMergeExtraction(&acc, usageExt(32752, 30, 32782))
	require.NotNil(t, acc.Usage)
	assert.Equal(t, 32752, acc.Usage.InputTokens, "latest input wins, not the max")
	assert.Equal(t, 30, acc.Usage.OutputTokens)
	assert.Equal(t, 32782, acc.Usage.TotalTokens)
	assert.Equal(t, 32782, acc.Usage.InputTokens+acc.Usage.OutputTokens, "row stays internally consistent")
}

func TestMetaMergeExtraction_NakedNotificationDoesNotRegress(t *testing.T) {
	// A cost-only "naked" notification (all token counters zero) must not wipe
	// the adopted token snapshot.
	var acc metaExtraction
	metaMergeExtraction(&acc, usageExt(32635, 84, 32719))
	naked := &metaExtraction{Usage: &metaTokenUsage{Present: true, Credit: 1.64}}
	metaMergeExtraction(&acc, naked)
	require.NotNil(t, acc.Usage)
	assert.Equal(t, 32635, acc.Usage.InputTokens, "naked notification must not regress input")
	assert.Equal(t, 84, acc.Usage.OutputTokens)
	assert.Equal(t, 32719, acc.Usage.TotalTokens)
}

func TestMetaMergeExtraction_CategoryIndependentOfTokenSnapshot(t *testing.T) {
	// CodeBuddy may deliver usageByCategory on a notification whose token
	// counters are zero; the category block must still be adopted.
	var acc metaExtraction
	metaMergeExtraction(&acc, usageExt(32635, 84, 32719))
	catOnly := &metaExtraction{Category: &metaCategoryUsage{Present: true, Categories: map[string]int64{"tools": 24108, "conversation": 5447}}}
	metaMergeExtraction(&acc, catOnly)
	require.NotNil(t, acc.Usage)
	assert.Equal(t, 32635, acc.Usage.InputTokens, "token snapshot kept")
	require.NotNil(t, acc.Category)
	assert.Equal(t, int64(24108), acc.Category.Categories["tools"])
	assert.Equal(t, int64(5447), acc.Category.Categories["conversation"])
}

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
	ext := extractClaudeMeta(meta)
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
// metaExtraction.traceRequestID — nil-safe accessor used by replay-drop logging
// ---------------------------------------------------------------------------

// TestMetaExtraction_TraceRequestID_NilReceiver covers the nil guard: the accessor
// is called on the result of extractMetaUsage, which returns nil when the backend
// has no adapter for the payload. Logging must not panic in that case, because it
// runs on the event path for every replayed usage_update.
func TestMetaExtraction_TraceRequestID_NilReceiver(t *testing.T) {
	var ext *metaExtraction
	assert.Equal(t, "", ext.traceRequestID(), "a nil extraction must yield an empty id")
}

// TestMetaExtraction_TraceRequestID_NoTrace covers the second guard: an extraction
// that carries usage but no trace identity (usage_update payloads often omit
// requestId) must yield "", not dereference the nil Trace.
func TestMetaExtraction_TraceRequestID_NoTrace(t *testing.T) {
	ext := &metaExtraction{Usage: &metaTokenUsage{Present: true}}
	assert.Equal(t, "", ext.traceRequestID(), "usage-only extraction has no request id")
}

// TestMetaExtraction_TraceRequestID_ReturnsRequestID covers the happy path, so the
// value that lands in the replay-drop log is actually the trace identity.
func TestMetaExtraction_TraceRequestID_ReturnsRequestID(t *testing.T) {
	ext := &metaExtraction{Trace: &metaTrace{RequestID: "req-abc", TraceID: "trace-xyz"}}
	assert.Equal(t, "req-abc", ext.traceRequestID(),
		"the accessor must return RequestID, not TraceID")
}

// ---------------------------------------------------------------------------
// Per-call usage accumulation
// ---------------------------------------------------------------------------

// CodeBuddy emits ONE usage_update PER MODEL CALL within a turn, each carrying
// only that call's counters under a shared requestId. Verified on the raw wire:
// a 4-command turn produced prompt_tokens 38229/38328/38427/38526/38625 with a
// single requestId and five distinct messageIds.
//
// Adopting the latest snapshot (the previous behavior) discarded every call but
// the last, under-counting a real DB by 30.9x (1.19B stored vs 36.8B actual
// across 678 sessions). The turn total must be the SUM.
func TestMetaAccum_CodeBuddySumsPerCallUsage(t *testing.T) {
	conn := &ACPConn{}
	calls := []struct {
		messageID  string
		prompt     int
		completion int
		credit     float64
	}{
		{"msg-1", 38229, 227, 1.93},
		{"msg-2", 38328, 58, 1.92},
		{"msg-3", 38427, 58, 1.93},
		{"msg-4", 38526, 58, 1.93},
		{"msg-5", 38625, 2, 1.94},
	}
	for _, c := range calls {
		ext := extractCodeBuddyMeta(map[string]any{
			"codebuddy.ai/messageId": c.messageID,
			"codebuddy.ai/requestId": "req-1",
			"usage": map[string]any{
				"prompt_tokens":     c.prompt,
				"completion_tokens": c.completion,
				"total_tokens":      c.prompt + c.completion,
				"credit":            c.credit,
			},
		})
		require.NotNil(t, ext)
		conn.mergeMetaExtraction(ext)
	}

	acc := conn.getAndClearMetaAccum()
	require.NotNil(t, acc)
	require.NotNil(t, acc.Usage)
	assert.Equal(t, 38229+38328+38427+38526+38625, acc.Usage.InputTokens,
		"the turn total is the sum over every model call, not the last call")
	assert.Equal(t, 227+58+58+58+2, acc.Usage.OutputTokens)
	assert.InDelta(t, 1.93+1.92+1.93+1.93+1.94, acc.Usage.Credit, 1e-9)
}

// A re-delivered notification for the SAME call must not be billed twice. The
// messageId is the dedup key; a genuinely new call (different messageId) must
// still count.
func TestMetaAccum_CodeBuddyDedupsByMessageID(t *testing.T) {
	conn := &ACPConn{}
	mk := func(msgID string, prompt int) *metaExtraction {
		return extractCodeBuddyMeta(map[string]any{
			"codebuddy.ai/messageId": msgID,
			"codebuddy.ai/requestId": "req-1",
			"usage": map[string]any{
				"prompt_tokens":     prompt,
				"completion_tokens": 10,
				"total_tokens":      prompt + 10,
			},
		})
	}

	conn.mergeMetaExtraction(mk("msg-1", 1000))
	conn.mergeMetaExtraction(mk("msg-1", 1000)) // retry of the same call
	conn.mergeMetaExtraction(mk("msg-2", 2000))

	acc := conn.getAndClearMetaAccum()
	require.NotNil(t, acc)
	require.NotNil(t, acc.Usage)
	assert.Equal(t, 3000, acc.Usage.InputTokens,
		"the duplicate delivery of msg-1 must not be counted twice")
}

// Agents whose counters are already a turn aggregate (Claude/Codex per-model
// token_count) must keep the latest-snapshot semantics: summing them would
// multiply the turn.
func TestMetaAccum_NonAccumulatingAgentKeepsLatestSnapshot(t *testing.T) {
	conn := &ACPConn{}
	conn.mergeMetaExtraction(&metaExtraction{Usage: &metaTokenUsage{InputTokens: 100, Present: true}})
	conn.mergeMetaExtraction(&metaExtraction{Usage: &metaTokenUsage{InputTokens: 250, Present: true}})

	acc := conn.getAndClearMetaAccum()
	require.NotNil(t, acc)
	require.NotNil(t, acc.Usage)
	assert.Equal(t, 250, acc.Usage.InputTokens,
		"a non-accumulating counter is a turn aggregate; the latest wins")
}

// The accumulator is per turn: a new Prompt must not inherit the previous
// turn's calls, or the totals would grow monotonically forever.
func TestMetaAccum_ResetClearsCallDedup(t *testing.T) {
	conn := &ACPConn{}
	mk := func() *metaExtraction {
		return extractCodeBuddyMeta(map[string]any{
			"codebuddy.ai/messageId": "msg-1",
			"usage":                  map[string]any{"prompt_tokens": 1000, "completion_tokens": 1},
		})
	}
	conn.mergeMetaExtraction(mk())
	require.Equal(t, 1000, conn.getAndClearMetaAccum().Usage.InputTokens)

	// Same messageId in the NEXT turn is a new call, not a duplicate.
	conn.mergeMetaExtraction(mk())
	acc := conn.getAndClearMetaAccum()
	require.NotNil(t, acc)
	assert.Equal(t, 1000, acc.Usage.InputTokens,
		"the dedup set must reset with the accumulator each turn")
}

// ---------------------------------------------------------------------------
// Credit — three sources, one cumulative figure
// ---------------------------------------------------------------------------

// The agent's own per-request running total is parsed from
// _meta["codebuddy.ai/requestCredits"] and is preferred over the per-call sum
// because it self-heals: a dropped notification's amount is already folded
// into the next total.
func TestExtractCodeBuddyMeta_RequestCredits(t *testing.T) {
	ext := extractCodeBuddyMeta(map[string]any{
		"codebuddy.ai/requestCredits": map[string]any{"req-1": 3.85},
	})
	require.NotNil(t, ext)
	assert.Equal(t, map[string]float64{"req-1": 3.85}, ext.RequestCredits)
	assert.Equal(t, 3.85, ext.creditTotal())

	// Junk entries are dropped rather than counted as zero.
	ext = extractCodeBuddyMeta(map[string]any{
		"codebuddy.ai/requestCredits": map[string]any{"": 9.0, "req-2": "nope", "req-3": 1.5},
	})
	require.NotNil(t, ext)
	assert.Equal(t, map[string]float64{"req-3": 1.5}, ext.RequestCredits)
}

// The cost-only paths (generation tools, team-member turns) carry credit with
// NO usage block. Before this, costFieldCarriesCredit discarded the amount and
// the billing vanished entirely.
func TestMapACPSessionUpdate_CostOnlyCreditIsCaptured(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}

	// Exactly the shape a generation-tool billing arrives in: used=0, size=0,
	// cost carries the credit, and _meta has no usage block.
	mapACPSessionUpdate(acp.SessionUpdate{
		UsageUpdate: &acp.SessionUsageUpdate{
			Used: 0, Size: 0,
			Cost: &acp.Cost{Amount: 12.5},
			Meta: map[string]any{"codebuddy.ai/requestId": "req-gen"},
		},
	}, ch, context.Background(), conn, nil)

	var evt *StreamEvent
	select {
	case e := <-ch:
		evt = &e
	default:
		t.Fatal("expected usage_update event")
	}
	require.NotNil(t, evt.Usage)
	assert.Equal(t, 0.0, evt.Usage.Cost, "credit must never be persisted as money")
	assert.Equal(t, 12.5, evt.Usage.Credit, "a cost-only billing still consumes credit")
}

// A replayed usage_update from the previous turn must not re-bill its credit.
func TestMapACPSessionUpdate_ReplayedFrameDropsCredit(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}
	// The previous turn used req-old; it is now part of the completed baseline.
	conn.setLastCompletedRequestID("req-old")

	mapACPSessionUpdate(acp.SessionUpdate{
		UsageUpdate: &acp.SessionUsageUpdate{
			Used: 0, Size: 0,
			Cost: &acp.Cost{Amount: 99.0},
			Meta: map[string]any{
				"codebuddy.ai/requestId":      "req-old",
				"codebuddy.ai/requestCredits": map[string]any{"req-old": 99.0},
			},
		},
	}, ch, context.Background(), conn, nil)

	var evt *StreamEvent
	select {
	case e := <-ch:
		evt = &e
	default:
		t.Fatal("expected usage_update event")
	}
	require.NotNil(t, evt.Usage)
	assert.Equal(t, 0.0, evt.Usage.Credit, "the previous turn's credit must not be billed twice")
}

// The forwarded credit is the TURN CUMULATIVE figure, not the current frame's
// own amount — otherwise the session panel shows the last model call's credit
// (jumping every few hundred ms) while the ledger stores the turn sum.
func TestMapACPSessionUpdate_CreditIsTurnCumulative(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}

	frame := func(msgID string, credit float64) {
		mapACPSessionUpdate(acp.SessionUpdate{
			UsageUpdate: &acp.SessionUsageUpdate{
				Used: 30000, Size: 200000,
				Meta: map[string]any{
					"codebuddy.ai/requestId": "req-1",
					"codebuddy.ai/messageId": msgID,
					"usage":                  map[string]any{"prompt_tokens": 30000, "completion_tokens": 5, "credit": credit},
				},
			},
		}, ch, context.Background(), conn, nil)
	}
	frame("msg-1", 1.25)
	frame("msg-2", 1.5)

	// Drain both forwarded events; each carries the running total as of that
	// frame, so the LAST one must read 2.75 rather than 1.5.
	var last *StreamEvent
	for len(ch) > 0 {
		e := <-ch
		last = &e
	}
	require.NotNil(t, last)
	require.NotNil(t, last.Usage)
	assert.InDelta(t, 2.75, last.Usage.Credit, 1e-9,
		"forwarded credit must be the turn total, not the current call's amount")
}

// requestCredits are cumulative on the agent side, so merge takes the max per
// requestId. Summing would double-bill every re-report.
func TestMetaMergeExtraction_RequestCreditsMaxPerRequest(t *testing.T) {
	var acc metaExtraction
	metaMergeExtraction(&acc, &metaExtraction{RequestCredits: map[string]float64{"req-1": 3.0}})
	metaMergeExtraction(&acc, &metaExtraction{RequestCredits: map[string]float64{"req-1": 3.85}})
	// An out-of-order (stale) frame must not regress the total.
	metaMergeExtraction(&acc, &metaExtraction{RequestCredits: map[string]float64{"req-1": 3.2}})
	assert.Equal(t, map[string]float64{"req-1": 3.85}, acc.RequestCredits)

	// A second request accumulates on top of the first.
	metaMergeExtraction(&acc, &metaExtraction{RequestCredits: map[string]float64{"req-2": 1.0}})
	assert.InDelta(t, 4.85, acc.creditTotal(), 1e-9)
}

// The per-request total already includes the per-call credits, so the two must
// never be added together — the largest single source wins.
func TestMetaExtraction_CreditTotalNeverSumsSources(t *testing.T) {
	ext := &metaExtraction{
		Usage:          &metaTokenUsage{Credit: 1.5},
		CostCredit:     2.0,
		RequestCredits: map[string]float64{"req-1": 3.85},
	}
	assert.InDelta(t, 3.85, ext.creditTotal(), 1e-9,
		"requestCredits covers the same billings; it must not be added to them")
}
