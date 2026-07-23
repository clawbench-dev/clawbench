package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// ── handleConfigOptionSelect tests ──────────────────────────────────────────

func TestHandleConfigOptionSelect_ModeNewOptions(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	agent := &AgentCapability{
		AvailableModes: []ModeDef{{ID: "ask"}},
	}
	reg.caps["a1"] = agent

	conn := newACPConn(&model.Agent{ID: "a1", Backend: "acp-stdio"}, "s1")
	ch := make(chan StreamEvent, 10)

	sel := SelectState{
		CurrentID: "code",
		Available: []SelectOptionDef{{ID: "ask"}, {ID: "code"}},
		Category:  "mode",
	}

	events := handleConfigOptionSelect(sel, conn, ch)

	// Should forward WS events because new mode "code" was added
	require.NotEmpty(t, events, "should forward events when new options available")

	// Registry should be updated
	got := reg.Get("a1")
	require.NotNil(t, got)
	assert.Len(t, got.AvailableModes, 2)

	// Connection cache should be updated
	assert.Equal(t, "code", conn.GetCurrentSelection("mode"))
}

func TestHandleConfigOptionSelect_ThoughtLevelChanged(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	agent := &AgentCapability{
		AvailableThinkingEfforts: []ThinkingEffortDef{{ID: "low"}, {ID: "high"}},
	}
	reg.caps["a1"] = agent

	conn := newACPConn(&model.Agent{ID: "a1", Backend: "acp-stdio"}, "s1")
	conn.UpdateCachedCurrent("thought_level", "low")
	ch := make(chan StreamEvent, 10)

	sel := SelectState{
		CurrentID: "high",
		Available: []SelectOptionDef{{ID: "low"}, {ID: "high"}},
		Category:  "thought_level",
	}

	events := handleConfigOptionSelect(sel, conn, ch)

	// Should forward WS events because current changed
	require.NotEmpty(t, events, "should forward events when current changed")

	// Connection cache should be updated
	assert.Equal(t, "high", conn.GetCurrentSelection("thought_level"))
}

func TestHandleConfigOptionSelect_NoChange(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	agent := &AgentCapability{
		AvailableModes: []ModeDef{{ID: "ask"}, {ID: "code"}},
	}
	reg.caps["a1"] = agent

	conn := newACPConn(&model.Agent{ID: "a1", Backend: "acp-stdio"}, "s1")
	conn.UpdateCachedCurrent("mode", "code")
	ch := make(chan StreamEvent, 10)

	sel := SelectState{
		CurrentID: "code",
		Available: []SelectOptionDef{{ID: "ask"}, {ID: "code"}},
		Category:  "mode",
	}

	events := handleConfigOptionSelect(sel, conn, ch)

	// No change → no WS events forwarded
	assert.Empty(t, events, "should not forward events when nothing changed")
}

func TestHandleConfigOptionSelect_ModeValidation(t *testing.T) {
	// Mode has special validation: bridge adapter check via IsOptionAvailable.
	// The WS event is still forwarded (the original code forwards config_update
	// before validation), but the session cache is NOT updated for invalid modes.
	reg := resetGlobalRegistryForTest(t)
	agent := &AgentCapability{
		AvailableModes: []ModeDef{{ID: "ask"}, {ID: "code"}},
	}
	reg.caps["a1"] = agent

	conn := newACPConn(&model.Agent{ID: "a1", Backend: "acp-stdio"}, "s1")
	conn.UpdateCachedCurrent("mode", "code")
	ch := make(chan StreamEvent, 10)

	sel := SelectState{
		CurrentID: "invalid_mode", // not in available modes
		Available: []SelectOptionDef{{ID: "ask"}, {ID: "code"}},
		Category:  "mode",
	}

	events := handleConfigOptionSelect(sel, conn, ch)

	// WS event is forwarded because currentModeId changed (matches original behavior)
	require.NotEmpty(t, events, "should forward events even for unrecognized mode (original behavior)")
	// But cache should remain unchanged (validation prevents cache update)
	assert.Equal(t, "code", conn.GetCurrentSelection("mode"), "cache should not update for unrecognized mode")
}

func TestHandleConfigOptionSelect_NilConn(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	_ = reg // not used when conn is nil

	ch := make(chan StreamEvent, 10)

	sel := SelectState{
		CurrentID: "code",
		Available: []SelectOptionDef{{ID: "ask"}, {ID: "code"}},
		Category:  "mode",
	}

	events := handleConfigOptionSelect(sel, nil, ch)

	// Should always forward when conn is nil
	require.NotEmpty(t, events, "should forward events when conn is nil")
}

func TestHandleConfigOptionSelect_EmptySelectState(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "a1", Backend: "acp-stdio"}, "s1")
	ch := make(chan StreamEvent, 10)

	sel := SelectState{}

	events := handleConfigOptionSelect(sel, conn, ch)
	assert.Empty(t, events, "should not forward events for empty select state")
}

// ── handleConfigOptionSelect: thought_level with newOpts ────────────────────

func TestHandleConfigOptionSelect_ThoughtLevelNewOptions(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	agent := &AgentCapability{
		AvailableThinkingEfforts: []ThinkingEffortDef{{ID: "low"}},
	}
	reg.caps["a1"] = agent

	conn := newACPConn(&model.Agent{ID: "a1", Backend: "acp-stdio"}, "s1")
	conn.UpdateCachedCurrent("thought_level", "low")
	ch := make(chan StreamEvent, 10)

	sel := SelectState{
		CurrentID: "medium",
		Available: []SelectOptionDef{{ID: "low"}, {ID: "medium"}, {ID: "high"}},
		Category:  "thought_level",
	}

	events := handleConfigOptionSelect(sel, conn, ch)

	// Should forward because new effort levels were added
	require.NotEmpty(t, events, "should forward events when new options available")
	assert.Contains(t, events, "thinking_effort_update")

	// Registry should be updated with new levels
	got := reg.Get("a1")
	require.NotNil(t, got)
	assert.Len(t, got.AvailableThinkingEfforts, 3)

	// Connection cache should be updated
	assert.Equal(t, "medium", conn.GetCurrentSelection("thought_level"))
}

// ── handleConfigOptionSelect: unknown category ──────────────────────────────

func TestHandleConfigOptionSelect_UnknownCategory(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.caps["a1"] = &AgentCapability{}

	conn := newACPConn(&model.Agent{ID: "a1", Backend: "acp-stdio"}, "s1")
	ch := make(chan StreamEvent, 10)

	sel := SelectState{
		CurrentID: "value1",
		Available: []SelectOptionDef{{ID: "value1"}, {ID: "value2"}},
		Category:  "custom_category",
	}

	events := handleConfigOptionSelect(sel, conn, ch)

	// Should forward config_update for unknown categories
	require.NotEmpty(t, events, "should forward events for unknown category")
	assert.Contains(t, events, "config_update")

	// Cache should be updated for the custom category
	assert.Equal(t, "value1", conn.GetCurrentSelection("custom_category"))
}

// ── buildStreamEventFromSelectState tests ────────────────────────────────────

func TestBuildStreamEventFromSelectState_Mode(t *testing.T) {
	sel := SelectState{
		CurrentID: "code",
		Available: []SelectOptionDef{{ID: "ask", Name: "Ask"}, {ID: "code", Name: "Code"}},
		Category:  "mode",
	}
	evt := buildStreamEventFromSelectState(sel)
	assert.Equal(t, "mode_update", evt.Type)
	require.NotNil(t, evt.Mode)
	assert.Equal(t, "code", evt.Mode.CurrentModeID)
	require.Len(t, evt.Mode.AvailableModes, 2)
	assert.Equal(t, "Ask", evt.Mode.AvailableModes[0].Name)
}

func TestBuildStreamEventFromSelectState_ThoughtLevel(t *testing.T) {
	sel := SelectState{
		CurrentID: "high",
		Available: []SelectOptionDef{{ID: "low", Name: "Low"}, {ID: "high", Name: "High"}},
		Category:  "thought_level",
	}
	evt := buildStreamEventFromSelectState(sel)
	assert.Equal(t, "thinking_effort_update", evt.Type)
	require.NotNil(t, evt.ThinkingEffort)
	assert.Equal(t, "high", evt.ThinkingEffort.CurrentID)
	require.Len(t, evt.ThinkingEffort.AvailableLevels, 2)
	assert.Equal(t, "Low", evt.ThinkingEffort.AvailableLevels[0].Name)
}

func TestBuildStreamEventFromSelectState_UnknownCategory(t *testing.T) {
	sel := SelectState{
		CurrentID: "v1",
		Available: []SelectOptionDef{{ID: "v1"}, {ID: "v2"}},
		Category:  "custom",
	}
	evt := buildStreamEventFromSelectState(sel)
	assert.Equal(t, "config_update", evt.Type)
	require.NotNil(t, evt.Config)
	assert.Equal(t, "custom", evt.Config.ConfigID)
	assert.Equal(t, "v1", evt.Config.CurrentID)
}

// ── streamEventTypeFromCategory tests ────────────────────────────────────────

func TestStreamEventTypeFromCategory(t *testing.T) {
	assert.Equal(t, "mode_update", streamEventTypeFromCategory("mode"))
	assert.Equal(t, "thinking_effort_update", streamEventTypeFromCategory("thought_level"))
	assert.Equal(t, "config_update", streamEventTypeFromCategory("custom"))
	assert.Equal(t, "config_update", streamEventTypeFromCategory(""))
}
