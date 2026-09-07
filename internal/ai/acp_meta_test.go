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
	assert.Equal(t, 142, u.InputTokens)
	assert.Equal(t, 18, u.OutputTokens)
	assert.Equal(t, 41213, u.TotalTokens)
	assert.Equal(t, 41053, u.CachedWriteTokens)
	assert.Equal(t, 0, u.CachedReadTokens)
	assert.Equal(t, 0, u.ThoughtTokens)
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
	// Aggregate usage extracted from token_count.
	require.NotNil(t, ext.Usage)
	assert.Equal(t, 105, ext.Usage.InputTokens)
	assert.Equal(t, 2, ext.Usage.OutputTokens)
	assert.Equal(t, 12779, ext.Usage.TotalTokens)
	assert.Equal(t, 12672, ext.Usage.CachedReadTokens)
	// Model name surfaced as ResponseModelID.
	require.NotNil(t, ext.Trace)
	assert.Equal(t, "deepseek-v4-flash", ext.Trace.ResponseModelID)
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
	conn.SetCachedUsageState(&UsageState{Used: 300000, Size: 1000000, InputTokens: 301000, Cost: 5.0})

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
	assert.Equal(t, 6.08, cached.Cost, "cost is monotonic — the naked notification's higher cost applies")
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
