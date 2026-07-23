package ai

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

// ── ACPConn generalized lastSetConfigs ──────────────────────────────────────

func TestACPConn_ShouldSetConfigGeneralized(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "test-gen-config", Backend: "acp-stdio"}, "session-gen-config")

	t.Run("Mode_Initial", func(t *testing.T) {
		assert.True(t, conn.shouldSetConfig("mode", "code"))
	})

	t.Run("Mode_SameAfterMark", func(t *testing.T) {
		conn.markConfigSet("mode", "code")
		assert.False(t, conn.shouldSetConfig("mode", "code"))
	})

	t.Run("Mode_DifferentAfterMark", func(t *testing.T) {
		assert.True(t, conn.shouldSetConfig("mode", "ask"))
	})

	t.Run("ThinkingEffort_Initial", func(t *testing.T) {
		assert.True(t, conn.shouldSetConfig("thinkingEffort", "high"))
	})

	t.Run("ThinkingEffort_SameAfterMark", func(t *testing.T) {
		conn.markConfigSet("thinkingEffort", "high")
		assert.False(t, conn.shouldSetConfig("thinkingEffort", "high"))
	})

	t.Run("Model_Initial", func(t *testing.T) {
		assert.True(t, conn.shouldSetConfig("model", "gpt-4"))
	})

	t.Run("Model_SameAfterMark", func(t *testing.T) {
		conn.markConfigSet("model", "gpt-4")
		assert.False(t, conn.shouldSetConfig("model", "gpt-4"))
	})

	t.Run("CustomCategory_Initial", func(t *testing.T) {
		assert.True(t, conn.shouldSetConfig("custom", "value1"))
	})

	t.Run("CustomCategory_SameAfterMark", func(t *testing.T) {
		conn.markConfigSet("custom", "value1")
		assert.False(t, conn.shouldSetConfig("custom", "value1"))
	})

	t.Run("CustomCategory_DifferentAfterMark", func(t *testing.T) {
		assert.True(t, conn.shouldSetConfig("custom", "value2"))
	})
}

func TestACPConn_ResetLastSetConfigGeneralized(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "test-reset-gen", Backend: "acp-stdio"}, "session-reset-gen")

	conn.markConfigSet("mode", "code")
	conn.markConfigSet("thinkingEffort", "high")
	conn.markConfigSet("model", "gpt-4")
	conn.markConfigSet("custom", "value1")

	assert.False(t, conn.shouldSetConfig("mode", "code"))
	assert.False(t, conn.shouldSetConfig("thinkingEffort", "high"))
	assert.False(t, conn.shouldSetConfig("model", "gpt-4"))
	assert.False(t, conn.shouldSetConfig("custom", "value1"))

	conn.resetLastSetConfig()

	assert.True(t, conn.shouldSetConfig("mode", "code"))
	assert.True(t, conn.shouldSetConfig("thinkingEffort", "high"))
	assert.True(t, conn.shouldSetConfig("model", "gpt-4"))
	assert.True(t, conn.shouldSetConfig("custom", "value1"))
}

func TestACPConn_UnsupportedConfigGeneralized(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "test-unsupported-gen", Backend: "acp-stdio"}, "session-unsupported-gen")

	assert.True(t, conn.shouldSetConfig("custom", "value1"))

	// Mark custom as unsupported
	conn.lastSetConfigMu.Lock()
	conn.unsupportedConfigs = map[string]bool{"custom": true}
	conn.lastSetConfigMu.Unlock()

	assert.False(t, conn.shouldSetConfig("custom", "value1"))
	assert.False(t, conn.shouldSetConfig("custom", "value2"))
	assert.True(t, conn.shouldSetConfig("mode", "code")) // other categories still work

	// Reset clears unsupported tracking
	conn.resetLastSetConfig()
	assert.True(t, conn.shouldSetConfig("custom", "value1"))
}

func TestACPConn_LegacyConfigMethods_UseGeneralized(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "test-legacy-config", Backend: "acp-stdio"}, "session-legacy-config")

	// Legacy shouldSetConfig/markConfigSet for "mode"
	assert.True(t, conn.shouldSetConfig("mode", "code"))
	conn.markConfigSet("mode", "code")
	assert.False(t, conn.shouldSetConfig("mode", "code"))
	assert.True(t, conn.shouldSetConfig("mode", "ask"))

	// Legacy for "thinkingEffort"
	assert.True(t, conn.shouldSetConfig("thinkingEffort", "high"))
	conn.markConfigSet("thinkingEffort", "high")
	assert.False(t, conn.shouldSetConfig("thinkingEffort", "high"))

	// Legacy for "model"
	assert.True(t, conn.shouldSetConfig("model", "gpt-4"))
	conn.markConfigSet("model", "gpt-4")
	assert.False(t, conn.shouldSetConfig("model", "gpt-4"))
}
