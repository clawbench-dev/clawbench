package ai

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

// ── ACPConn generalized currentSelections ────────────────────────────────────

func TestACPConn_UpdateCachedCurrent(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "test-generalized", Backend: "acp-stdio"}, "session-generalized")

	t.Run("Mode", func(t *testing.T) {
		conn.UpdateCachedCurrent("mode", "code")
		assert.Equal(t, "code", conn.GetCurrentSelection("mode"))
		conn.UpdateCachedCurrent("mode", "ask")
		assert.Equal(t, "ask", conn.GetCurrentSelection("mode"))
	})

	t.Run("ThoughtLevel", func(t *testing.T) {
		conn.UpdateCachedCurrent("thought_level", "high")
		assert.Equal(t, "high", conn.GetCurrentSelection("thought_level"))
	})

	t.Run("Model", func(t *testing.T) {
		conn.UpdateCachedCurrent("model", "gpt-4")
		assert.Equal(t, "gpt-4", conn.GetCurrentSelection("model"))
	})

	t.Run("UnknownCategory", func(t *testing.T) {
		conn.UpdateCachedCurrent("custom", "value1")
		assert.Equal(t, "value1", conn.GetCurrentSelection("custom"))
	})

	t.Run("EmptyDefault", func(t *testing.T) {
		assert.Equal(t, "", conn.GetCurrentSelection("nonexistent"))
	})
}

func TestACPConn_HasCurrentChanged(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "test-changed-gen", Backend: "acp-stdio"}, "session-changed-gen")

	t.Run("Mode_InitialChange", func(t *testing.T) {
		assert.True(t, conn.HasCurrentChanged("mode", "code"))
	})

	t.Run("Mode_NoChange", func(t *testing.T) {
		conn.UpdateCachedCurrent("mode", "code")
		assert.False(t, conn.HasCurrentChanged("mode", "code"))
	})

	t.Run("Mode_Changed", func(t *testing.T) {
		assert.True(t, conn.HasCurrentChanged("mode", "ask"))
	})

	t.Run("ThoughtLevel_InitialChange", func(t *testing.T) {
		assert.True(t, conn.HasCurrentChanged("thought_level", "high"))
	})

	t.Run("ThoughtLevel_NoChange", func(t *testing.T) {
		conn.UpdateCachedCurrent("thought_level", "high")
		assert.False(t, conn.HasCurrentChanged("thought_level", "high"))
	})

	t.Run("EmptyVsEmpty", func(t *testing.T) {
		assert.False(t, conn.HasCurrentChanged("empty_cat", ""))
	})
}

func TestACPConn_LegacyMethods_UseGeneralized(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "test-legacy", Backend: "acp-stdio"}, "session-legacy")

	// Test that old methods delegate to generalized ones
	conn.UpdateCachedCurrentMode("code")
	assert.Equal(t, "code", conn.GetCurrentSelection("mode"))
	assert.Equal(t, "code", conn.GetCurrentModeID())

	conn.UpdateCachedCurrentThinkingEffort("high")
	assert.Equal(t, "high", conn.GetCurrentSelection("thought_level"))
	assert.Equal(t, "high", conn.GetCurrentThinkingEffortID())

	conn.UpdateCachedCurrentModel("gpt-4")
	assert.Equal(t, "gpt-4", conn.GetCurrentSelection("model"))
	assert.Equal(t, "gpt-4", conn.GetCurrentModelID())
}

func TestACPConn_HasCurrentLegacyMethods_UseGeneralized(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "test-legacy-changed", Backend: "acp-stdio"}, "session-legacy-changed")

	// Set up initial state via generalized method
	conn.UpdateCachedCurrent("mode", "code")
	conn.UpdateCachedCurrent("thought_level", "high")

	// Old methods should work
	assert.False(t, conn.HasCurrentModeChanged("code"))
	assert.True(t, conn.HasCurrentModeChanged("ask"))
	assert.False(t, conn.HasCurrentThinkingEffortChanged("high"))
	assert.True(t, conn.HasCurrentThinkingEffortChanged("medium"))

	// New generalized methods should return same results
	assert.False(t, conn.HasCurrentChanged("mode", "code"))
	assert.True(t, conn.HasCurrentChanged("mode", "ask"))
	assert.False(t, conn.HasCurrentChanged("thought_level", "high"))
	assert.True(t, conn.HasCurrentChanged("thought_level", "medium"))
}
