package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractParentToolCallID(t *testing.T) {
	t.Run("codebuddy flat key", func(t *testing.T) {
		meta := map[string]any{"codebuddy.ai/parentToolCallId": "call_00_abc"}
		assert.Equal(t, "call_00_abc", extractParentToolCallID("codebuddy", meta))
	})

	t.Run("codebuddy no parent key is top-level", func(t *testing.T) {
		meta := map[string]any{"codebuddy.ai/toolName": "Read", "codebuddy.ai/requestId": "r1"}
		assert.Equal(t, "", extractParentToolCallID("codebuddy", meta))
	})

	t.Run("claude nested claudeCode.parentToolUseId", func(t *testing.T) {
		meta := map[string]any{"claudeCode": map[string]any{"parentToolUseId": "toolu_123", "toolName": "Read"}}
		assert.Equal(t, "toolu_123", extractParentToolCallID("claude", meta))
	})

	t.Run("qoder shares claude encoding", func(t *testing.T) {
		meta := map[string]any{"claudeCode": map[string]any{"parentToolUseId": "toolu_456"}}
		assert.Equal(t, "toolu_456", extractParentToolCallID("qoder", meta))
	})

	t.Run("claude without parent is top-level", func(t *testing.T) {
		meta := map[string]any{"claudeCode": map[string]any{"toolName": "Bash"}}
		assert.Equal(t, "", extractParentToolCallID("claude", meta))
	})

	t.Run("claude claudeCode wrong shape does not panic", func(t *testing.T) {
		meta := map[string]any{"claudeCode": "not-a-map"}
		assert.Equal(t, "", extractParentToolCallID("claude", meta))
	})

	t.Run("nil and empty meta", func(t *testing.T) {
		assert.Equal(t, "", extractParentToolCallID("codebuddy", nil))
		assert.Equal(t, "", extractParentToolCallID("claude", map[string]any{}))
	})

	t.Run("unknown backend best-effort accepts either encoding", func(t *testing.T) {
		assert.Equal(t, "call_x", extractParentToolCallID("grok", map[string]any{"codebuddy.ai/parentToolCallId": "call_x"}))
		assert.Equal(t, "toolu_y", extractParentToolCallID("grok", map[string]any{"claudeCode": map[string]any{"parentToolUseId": "toolu_y"}}))
		assert.Equal(t, "", extractParentToolCallID("grok", map[string]any{"other": "v"}))
	})

	t.Run("codex has no parent encoding", func(t *testing.T) {
		meta := map[string]any{"codex": map[string]any{"subagent": map[string]any{"threadId": "t1"}}}
		assert.Equal(t, "", extractParentToolCallID("codex", meta))
	})
}
