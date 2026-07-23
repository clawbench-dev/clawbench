package ai

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── AgentCapabilityRegistry generalized methods ─────────────────────────────

func TestRegistry_GetSelectState_Mode(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("a1", &AgentCapability{
		AvailableModes: []ModeDef{{ID: "ask"}, {ID: "code"}},
	})

	sel := reg.GetSelectState("a1", "mode", "code")
	require.NotNil(t, sel)
	assert.Equal(t, "code", sel.CurrentID)
	assert.Equal(t, "mode", sel.Category)
	assert.Len(t, sel.Available, 2)
	assert.Equal(t, "ask", sel.Available[0].ID)
	assert.Equal(t, "code", sel.Available[1].ID)
}

func TestRegistry_GetSelectState_ThoughtLevel(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("a1", &AgentCapability{
		AvailableThinkingEfforts: []ThinkingEffortDef{{ID: "low"}, {ID: "high"}},
	})

	sel := reg.GetSelectState("a1", "thought_level", "high")
	require.NotNil(t, sel)
	assert.Equal(t, "high", sel.CurrentID)
	assert.Equal(t, "thought_level", sel.Category)
	assert.Len(t, sel.Available, 2)
}

func TestRegistry_GetSelectState_FallbackToConfigOption(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("a1", &AgentCapability{
		ConfigOptionState: &ConfigOptionState{
			ConfigID:  "mode",
			CurrentID: "code",
			Options: []ConfigOptionDef{{
				ID:       "mode",
				Category: "mode",
				Values:   []ConfigOptionValue{{ID: "ask"}, {ID: "code"}},
			}},
		},
	})

	sel := reg.GetSelectState("a1", "mode", "")
	require.NotNil(t, sel)
	assert.Equal(t, "code", sel.CurrentID) // from ConfigOptionState
	assert.Equal(t, "mode", sel.Category)
	assert.Len(t, sel.Available, 2)
}

func TestRegistry_GetSelectState_WithExplicitCurrentID(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("a1", &AgentCapability{
		AvailableThinkingEfforts: []ThinkingEffortDef{{ID: "low"}, {ID: "high"}},
	})

	sel := reg.GetSelectState("a1", "thought_level", "low")
	require.NotNil(t, sel)
	assert.Equal(t, "low", sel.CurrentID) // explicit overrides registry
}

func TestRegistry_GetSelectState_MissingAgent(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	sel := reg.GetSelectState("missing", "mode", "code")
	assert.Nil(t, sel)
}

func TestRegistry_GetSelectState_NoDataForCategory(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("a1", &AgentCapability{
		AvailableModes: []ModeDef{{ID: "code"}},
	})
	sel := reg.GetSelectState("a1", "thought_level", "high")
	assert.Nil(t, sel)
}

// ── UpdateAvailableOptions ──────────────────────────────────────────────────

func TestRegistry_UpdateAvailableOptions_Mode(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	opts := make([]SelectOptionDef, 0, 3)
	opts = append(opts, SelectOptionDef{ID: "ask", Name: "Ask"}, SelectOptionDef{ID: "code", Name: "Code"})
	reg.UpdateAvailableOptions("a1", "mode", opts)

	got := reg.Get("a1")
	require.NotNil(t, got)
	assert.Len(t, got.AvailableModes, 2)
	assert.Equal(t, "ask", got.AvailableModes[0].ID)
}

func TestRegistry_UpdateAvailableOptions_ThoughtLevel(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	opts := []SelectOptionDef{{ID: "low", Name: "Low"}, {ID: "high", Name: "High"}}
	reg.UpdateAvailableOptions("a1", "thought_level", opts)

	got := reg.Get("a1")
	require.NotNil(t, got)
	assert.Len(t, got.AvailableThinkingEfforts, 2)
	assert.Equal(t, "low", got.AvailableThinkingEfforts[0].ID)
}

func TestRegistry_UpdateAvailableOptions_CustomCategory(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	opts := []SelectOptionDef{{ID: "opt1", Name: "Option 1"}}
	reg.UpdateAvailableOptions("a1", "custom", opts)

	// Should store in AvailableOptions map for custom categories
	got := reg.GetAvailableOptions("a1", "custom")
	assert.Len(t, got, 1)
	assert.Equal(t, "opt1", got[0].ID)
}

// ── HasNewAvailableOptions ──────────────────────────────────────────────────

func TestRegistry_HasNewAvailableOptions_Mode(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("a1", &AgentCapability{
		AvailableModes: []ModeDef{{ID: "ask"}},
	})

	t.Run("AllKnown", func(t *testing.T) {
		opts := []SelectOptionDef{{ID: "ask"}}
		assert.False(t, reg.HasNewAvailableOptions("a1", "mode", opts))
	})

	t.Run("OneNew", func(t *testing.T) {
		opts := []SelectOptionDef{{ID: "ask"}, {ID: "code"}}
		assert.True(t, reg.HasNewAvailableOptions("a1", "mode", opts))
	})

	t.Run("EmptyNew", func(t *testing.T) {
		assert.False(t, reg.HasNewAvailableOptions("a1", "mode", nil))
	})
}

func TestRegistry_HasNewAvailableOptions_ThoughtLevel(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("a1", &AgentCapability{
		AvailableThinkingEfforts: []ThinkingEffortDef{{ID: "low"}},
	})

	t.Run("AllKnown", func(t *testing.T) {
		opts := []SelectOptionDef{{ID: "low"}}
		assert.False(t, reg.HasNewAvailableOptions("a1", "thought_level", opts))
	})

	t.Run("OneNew", func(t *testing.T) {
		opts := []SelectOptionDef{{ID: "low"}, {ID: "high"}}
		assert.True(t, reg.HasNewAvailableOptions("a1", "thought_level", opts))
	})
}

func TestRegistry_HasNewAvailableOptions_CustomCategory(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	opts := []SelectOptionDef{{ID: "opt1"}}
	// No existing data → any non-empty list is new
	assert.True(t, reg.HasNewAvailableOptions("a1", "custom", opts))
	assert.False(t, reg.HasNewAvailableOptions("a1", "custom", nil))
}

// ── IsOptionAvailable ───────────────────────────────────────────────────────

func TestRegistry_IsOptionAvailable_Mode(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("a1", &AgentCapability{
		AvailableModes: []ModeDef{{ID: "ask"}, {ID: "code"}},
	})

	assert.True(t, reg.IsOptionAvailable("a1", "mode", "ask"))
	assert.True(t, reg.IsOptionAvailable("a1", "mode", "code"))
	assert.False(t, reg.IsOptionAvailable("a1", "mode", "architect"))
	assert.False(t, reg.IsOptionAvailable("missing", "mode", "code"))
}

func TestRegistry_IsOptionAvailable_ThoughtLevel(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("a1", &AgentCapability{
		AvailableThinkingEfforts: []ThinkingEffortDef{{ID: "low"}, {ID: "high"}},
	})

	assert.True(t, reg.IsOptionAvailable("a1", "thought_level", "low"))
	assert.False(t, reg.IsOptionAvailable("a1", "thought_level", "medium"))
}

func TestRegistry_IsOptionAvailable_CustomCategory(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.UpdateAvailableOptions("a1", "custom", []SelectOptionDef{{ID: "opt1"}})

	assert.True(t, reg.IsOptionAvailable("a1", "custom", "opt1"))
	assert.False(t, reg.IsOptionAvailable("a1", "custom", "opt2"))
}

func TestRegistry_IsOptionAvailable_Model(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("a1", &AgentCapability{
		AvailableModels: []model.AgentModel{{ID: "gpt-4"}, {ID: "claude-3"}},
	})

	assert.True(t, reg.IsOptionAvailable("a1", "model", "gpt-4"))
	assert.False(t, reg.IsOptionAvailable("a1", "model", "missing"))
	assert.False(t, reg.IsOptionAvailable("missing", "model", "gpt-4"))
}

func TestRegistry_agentCapForRead(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)

	// Missing agent returns nil
	assert.Nil(t, reg.agentCapForRead("missing"))

	// Existing agent returns capability
	agentCap := &AgentCapability{AvailableModes: []ModeDef{{ID: "code"}}}
	reg.Update("a1", agentCap)
	got := reg.agentCapForRead("a1")
	require.NotNil(t, got)
	assert.Equal(t, "code", got.AvailableModes[0].ID)
}

// ── GetAvailableOptions ─────────────────────────────────────────────────────

func TestRegistry_GetAvailableOptions_Mode(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("a1", &AgentCapability{
		AvailableModes: []ModeDef{{ID: "ask", Name: "Ask"}, {ID: "code", Name: "Code"}},
	})

	opts := reg.GetAvailableOptions("a1", "mode")
	assert.Len(t, opts, 2)
	assert.Equal(t, "ask", opts[0].ID)
	assert.Equal(t, "Code", opts[1].Name)
}

func TestRegistry_GetAvailableOptions_ThoughtLevel(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("a1", &AgentCapability{
		AvailableThinkingEfforts: []ThinkingEffortDef{{ID: "low"}, {ID: "high"}},
	})

	opts := reg.GetAvailableOptions("a1", "thought_level")
	assert.Len(t, opts, 2)
}

func TestRegistry_GetAvailableOptions_MissingAgent(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	opts := reg.GetAvailableOptions("missing", "mode")
	assert.Nil(t, opts)
}

// ── Legacy methods delegate to generalized ──────────────────────────────────

func TestRegistry_LegacyMethodsViaGeneralized(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)

	// UpdateAvailableOptions("mode", ...) should be equivalent to UpdateModes
	opts := make([]SelectOptionDef, 0, 3)
	opts = append(opts, SelectOptionDef{ID: "ask", Name: "Ask"}, SelectOptionDef{ID: "code", Name: "Code"})
	reg.UpdateAvailableOptions("a1", "mode", opts)

	got := reg.Get("a1")
	require.NotNil(t, got)
	assert.Equal(t, []ModeDef{{ID: "ask", Name: "Ask"}, {ID: "code", Name: "Code"}}, got.AvailableModes)

	// GetAvailableOptions("mode", ...) should return same data
	retrievedOpts := reg.GetAvailableOptions("a1", "mode")
	assert.Equal(t, opts, retrievedOpts)

	// HasNewAvailableOptions should be equivalent to HasNewAvailableModes
	assert.False(t, reg.HasNewAvailableOptions("a1", "mode", opts))
	assert.True(t, reg.HasNewAvailableOptions("a1", "mode", append(opts, SelectOptionDef{ID: "architect"})))

	// IsOptionAvailable should be equivalent to IsModeAvailable
	assert.True(t, reg.IsOptionAvailable("a1", "mode", "ask"))
	assert.False(t, reg.IsOptionAvailable("a1", "mode", "missing"))
}

// ── AvailableOptions field in AgentCapability ────────────────────────────────

func TestAgentCapability_AvailableOptionsField(t *testing.T) {
	c := &AgentCapability{
		AvailableOptions: map[string][]SelectOptionDef{
			"custom": {{ID: "opt1", Name: "Option 1"}},
		},
	}
	assert.True(t, c.HasData())
}

func TestAgentCapability_HasData_WithAvailableOptions(t *testing.T) {
	c := &AgentCapability{
		AvailableOptions: map[string][]SelectOptionDef{
			"custom": {{ID: "opt1"}},
		},
	}
	assert.True(t, c.HasData())
}

// ── Merge preserves AvailableOptions ────────────────────────────────────────

func TestRegistry_Merge_PreservesAvailableOptions(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("a1", &AgentCapability{
		AvailableOptions: map[string][]SelectOptionDef{
			"custom": {{ID: "opt1"}},
		},
	})

	// Merge with other fields — AvailableOptions must be preserved
	reg.Update("a1", &AgentCapability{
		AvailableCommands: []AvailableCommandInfo{{Name: "init"}},
	})

	got := reg.Get("a1")
	require.NotNil(t, got)
	assert.NotNil(t, got.AvailableOptions)
	assert.Len(t, got.AvailableOptions["custom"], 1)
}

func TestRegistry_Merge_OverwritesAvailableOptions(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("a1", &AgentCapability{
		AvailableOptions: map[string][]SelectOptionDef{
			"custom": {{ID: "old"}},
		},
	})

	reg.Update("a1", &AgentCapability{
		AvailableOptions: map[string][]SelectOptionDef{
			"custom": {{ID: "new1"}, {ID: "new2"}},
		},
	})

	got := reg.Get("a1")
	require.NotNil(t, got)
	assert.Len(t, got.AvailableOptions["custom"], 2)
	assert.Equal(t, "new1", got.AvailableOptions["custom"][0].ID)
}

// ── ForceUpdate includes AvailableOptions ────────────────────────────────────

func TestRegistry_ForceUpdate_WithAvailableOptions(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	applied := reg.ForceUpdate("a1", &AgentCapability{
		AvailableOptions: map[string][]SelectOptionDef{
			"custom": {{ID: "opt1", Name: "Option 1"}},
		},
	})
	assert.True(t, applied)

	got := reg.Get("a1")
	require.NotNil(t, got)
	assert.NotNil(t, got.AvailableOptions)
	assert.Len(t, got.AvailableOptions["custom"], 1)
}

// ── model.AgentModel category ───────────────────────────────────────────────

func TestRegistry_GetSelectState_Model(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("a1", &AgentCapability{
		AvailableModels: []model.AgentModel{{ID: "m1", Name: "M1"}, {ID: "m2", Name: "M2"}},
	})

	sel := reg.GetSelectState("a1", "model", "m2")
	require.NotNil(t, sel)
	assert.Equal(t, "m2", sel.CurrentID)
	assert.Equal(t, "model", sel.Category)
	assert.Len(t, sel.Available, 2)
}
