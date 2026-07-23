package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── selectStateFromConfigState ──────────────────────────────────────────────

func TestSelectStateFromConfigState_Mode(t *testing.T) {
	cs := &ConfigOptionState{
		ConfigID:  "mode",
		CurrentID: "code",
		Options: []ConfigOptionDef{{
			ID:       "mode",
			Category: "mode",
			Values: []ConfigOptionValue{
				{ID: "ask", Name: "Ask"},
				{ID: "code", Name: "Code"},
			},
		}},
	}

	sel := selectStateFromConfigState(cs, "mode")
	require.NotNil(t, sel)
	assert.Equal(t, "code", sel.CurrentID)
	assert.Equal(t, "mode", sel.Category)
	assert.Len(t, sel.Available, 2)
	assert.Equal(t, "ask", sel.Available[0].ID)
	assert.Equal(t, "Code", sel.Available[1].Name)
}

func TestSelectStateFromConfigState_ThoughtLevel(t *testing.T) {
	cs := &ConfigOptionState{
		ConfigID:  "thinkingEffort",
		CurrentID: "high",
		Options: []ConfigOptionDef{{
			ID:       "thinkingEffort",
			Category: "thought_level",
			Values: []ConfigOptionValue{
				{ID: "low", Name: "Low"},
				{ID: "high", Name: "High"},
			},
		}},
	}

	sel := selectStateFromConfigState(cs, "thought_level")
	require.NotNil(t, sel)
	assert.Equal(t, "high", sel.CurrentID)
	assert.Equal(t, "thought_level", sel.Category)
	assert.Len(t, sel.Available, 2)
}

func TestSelectStateFromConfigState_Nil(t *testing.T) {
	sel := selectStateFromConfigState(nil, "mode")
	assert.Nil(t, sel)
}

func TestSelectStateFromConfigState_NoMatchingCategory(t *testing.T) {
	cs := &ConfigOptionState{
		ConfigID:  "mode",
		CurrentID: "code",
		Options: []ConfigOptionDef{{
			ID:       "mode",
			Category: "mode",
			Values:   []ConfigOptionValue{{ID: "code"}},
		}},
	}

	sel := selectStateFromConfigState(cs, "thought_level")
	assert.Nil(t, sel)
}

func TestSelectStateFromConfigState_EmptyValues(t *testing.T) {
	cs := &ConfigOptionState{
		ConfigID:  "mode",
		CurrentID: "",
		Options: []ConfigOptionDef{{
			ID:       "mode",
			Category: "mode",
			Values:   []ConfigOptionValue{},
		}},
	}

	sel := selectStateFromConfigState(cs, "mode")
	assert.Nil(t, sel)
}

func TestSelectStateFromConfigState_ValuesButNoCurrentID(t *testing.T) {
	cs := &ConfigOptionState{
		ConfigID:  "mode",
		CurrentID: "",
		Options: []ConfigOptionDef{{
			ID:       "mode",
			Category: "mode",
			Values: []ConfigOptionValue{
				{ID: "ask", Name: "Ask"},
			},
		}},
	}

	sel := selectStateFromConfigState(cs, "mode")
	require.NotNil(t, sel)
	assert.Equal(t, "", sel.CurrentID)
	assert.Len(t, sel.Available, 1)
}

// ── modeStateFromConfigState delegates to selectStateFromConfigState ────────

func TestModeStateFromConfigState_Delegates(t *testing.T) {
	cs := &ConfigOptionState{
		ConfigID:  "mode",
		CurrentID: "code",
		Options: []ConfigOptionDef{{
			ID:       "mode",
			Category: "mode",
			Values: []ConfigOptionValue{
				{ID: "ask", Name: "Ask"},
				{ID: "code", Name: "Code"},
			},
		}},
	}

	// Old function should still work
	ms := modeStateFromConfigState(cs)
	require.NotNil(t, ms)
	assert.Equal(t, "code", ms.CurrentModeID)
	assert.Len(t, ms.AvailableModes, 2)

	// New function should produce equivalent results
	sel := selectStateFromConfigState(cs, "mode")
	require.NotNil(t, sel)
	assert.Equal(t, ms.CurrentModeID, sel.CurrentID)
	assert.Len(t, ms.AvailableModes, len(sel.Available))
}

// ── thinkingEffortStateFromConfigState delegates to selectStateFromConfigState ──

func TestThinkingEffortStateFromConfigState_Delegates(t *testing.T) {
	cs := &ConfigOptionState{
		ConfigID:  "thinkingEffort",
		CurrentID: "high",
		Options: []ConfigOptionDef{{
			ID:       "thinkingEffort",
			Category: "thought_level",
			Values: []ConfigOptionValue{
				{ID: "low", Name: "Low"},
				{ID: "high", Name: "High"},
			},
		}},
	}

	// Old function should still work
	tes := thinkingEffortStateFromConfigState(cs)
	require.NotNil(t, tes)
	assert.Equal(t, "high", tes.CurrentID)
	assert.Len(t, tes.AvailableLevels, 2)

	// New function should produce equivalent results
	sel := selectStateFromConfigState(cs, "thought_level")
	require.NotNil(t, sel)
	assert.Equal(t, tes.CurrentID, sel.CurrentID)
	assert.Len(t, tes.AvailableLevels, len(sel.Available))
}
