package backends_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai"
	_ "clawbench/internal/ai/backends/all"
)

func TestExtractMetaUsage_Dispatch(t *testing.T) {
	cbMeta := map[string]any{"usage": map[string]any{"prompt_tokens": 10}}
	ext := ai.ExtractMetaUsageForTest("codebuddy", cbMeta)
	require.NotNil(t, ext)
	assert.Equal(t, 10, ext.Usage.InputTokens)

	clMeta := map[string]any{"quota": map[string]any{"token_count": map[string]any{"inputTokens": 5}}}
	for _, backend := range []string{"claude", "codex", "qoder"} {
		ext := ai.ExtractMetaUsageForTest(backend, clMeta)
		require.NotNil(t, ext, backend)
		assert.Equal(t, 5, ext.Usage.InputTokens, backend)
	}

	// Unknown backend falls through to generic scan.
	ext = ai.ExtractMetaUsageForTest("mystery-agent", map[string]any{"deep": map[string]any{"outputTokens": 7}})
	require.NotNil(t, ext)
	assert.Equal(t, 7, ext.Usage.OutputTokens)

	// OpenCode reports no _meta extensions.
	assert.Nil(t, ai.ExtractMetaUsageForTest("opencode", map[string]any{"timestamp": "t"}))
}
