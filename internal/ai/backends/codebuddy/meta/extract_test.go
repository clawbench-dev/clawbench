package meta

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	ext := Extract(meta)
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
	ext := Extract(meta)
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
	ext := Extract(meta)
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
	ext := Extract(meta)
	require.NotNil(t, ext)
	require.NotNil(t, ext.Trace)
	tr := ext.Trace
	assert.Equal(t, "fa10f96dcc82d9db346922057b96c706", tr.TraceID)
	assert.Equal(t, "00-fa10f96dcc82d9db346922057b96c706-97fb1ee7db62a4ab-01", tr.TraceParent)
}

func TestExtractCodeBuddyMeta_Empty(t *testing.T) {
	assert.Nil(t, Extract(nil))
	assert.Nil(t, Extract(map[string]any{}))
	// Only unrecognized keys → nil (no data).
	assert.Nil(t, Extract(map[string]any{"timestamp": "2026-01-01"}))
}

// ---------------------------------------------------------------------------
// extractClaudeMeta — quota.token_count (Claude / Codex shared shape)
// ---------------------------------------------------------------------------

func TestExtractCodeBuddyMeta_RequestCredits(t *testing.T) {
	ext := Extract(map[string]any{
		"codebuddy.ai/requestCredits": map[string]any{"req-1": 3.85},
	})
	require.NotNil(t, ext)
	assert.Equal(t, map[string]float64{"req-1": 3.85}, ext.RequestCredits)
	assert.Equal(t, 3.85, ext.CreditTotal())

	// Junk entries are dropped rather than counted as zero.
	ext = Extract(map[string]any{
		"codebuddy.ai/requestCredits": map[string]any{"": 9.0, "req-2": "nope", "req-3": 1.5},
	})
	require.NotNil(t, ext)
	assert.Equal(t, map[string]float64{"req-3": 1.5}, ext.RequestCredits)
}

// The cost-only paths (generation tools, team-member turns) carry credit with
// NO usage block. Before this, costFieldCarriesCredit discarded the amount and
// the billing vanished entirely.

// The credit-vs-money decision must come from the backend's own registration,
// not from an id comparison in the core. Two failure modes this pins:
//
//   - codebuddy declares it, so its `cost` field is still read as credit;
//   - a backend that does NOT declare it is treated as money even when its
//     registered behavior exists for other reasons (ExtractMeta), which is the
//     regression a bare `backend == ""` check would not catch.
