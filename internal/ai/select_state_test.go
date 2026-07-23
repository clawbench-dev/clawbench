package ai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── SelectOptionDef ──────────────────────────────────────────────────────────

func TestSelectOptionDef_Fields(t *testing.T) {
	opt := SelectOptionDef{ID: "code", Name: "Code"}
	assert.Equal(t, "code", opt.ID)
	assert.Equal(t, "Code", opt.Name)
}

func TestSelectOptionDef_JSONRoundTrip(t *testing.T) {
	opt := SelectOptionDef{ID: "ask", Name: "Ask"}
	b, err := json.Marshal(opt)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"id"`)
	assert.Contains(t, string(b), `"name"`)
	assert.Contains(t, string(b), `"ask"`)

	var decoded SelectOptionDef
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, opt, decoded)
}

func TestSelectOptionDef_EmptyName(t *testing.T) {
	opt := SelectOptionDef{ID: "low"}
	b, err := json.Marshal(opt)
	require.NoError(t, err)
	// Name with omitempty should be omitted when empty
	assert.NotContains(t, string(b), `"name"`)

	var decoded SelectOptionDef
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, "low", decoded.ID)
	assert.Equal(t, "", decoded.Name)
}

// ── SelectState ──────────────────────────────────────────────────────────────

func TestSelectState_Fields(t *testing.T) {
	s := SelectState{
		CurrentID: "code",
		Available: []SelectOptionDef{{ID: "ask"}, {ID: "code"}},
		Category:  "mode",
	}
	assert.Equal(t, "code", s.CurrentID)
	assert.Len(t, s.Available, 2)
	assert.Equal(t, "mode", s.Category)
}

func TestSelectState_Empty(t *testing.T) {
	s := SelectState{}
	assert.Empty(t, s.CurrentID)
	assert.Nil(t, s.Available)
	assert.Empty(t, s.Category)
}

func TestSelectState_IsEmpty(t *testing.T) {
	t.Run("AllEmpty", func(t *testing.T) {
		s := SelectState{}
		assert.True(t, s.IsEmpty())
	})
	t.Run("WithCurrentID", func(t *testing.T) {
		s := SelectState{CurrentID: "code"}
		assert.False(t, s.IsEmpty())
	})
	t.Run("WithAvailable", func(t *testing.T) {
		s := SelectState{Available: []SelectOptionDef{{ID: "code"}}}
		assert.False(t, s.IsEmpty())
	})
	t.Run("WithCategory", func(t *testing.T) {
		s := SelectState{Category: "mode"}
		assert.False(t, s.IsEmpty())
	})
}

func TestSelectState_IsValidOption(t *testing.T) {
	s := SelectState{
		CurrentID: "code",
		Available: []SelectOptionDef{{ID: "ask"}, {ID: "code"}},
		Category:  "mode",
	}
	assert.True(t, s.IsValidOption("ask"))
	assert.True(t, s.IsValidOption("code"))
	assert.False(t, s.IsValidOption("architect"))
	assert.False(t, s.IsValidOption(""))
}

func TestSelectState_IsValidOption_EmptyAvailable(t *testing.T) {
	s := SelectState{CurrentID: "x"}
	assert.False(t, s.IsValidOption("x"))
}

// ── ModeState JSON compatibility ─────────────────────────────────────────────

func TestModeState_JSONFieldNames(t *testing.T) {
	ms := ModeState{
		CurrentModeID:  "code",
		AvailableModes: []ModeDef{{ID: "ask", Name: "Ask"}, {ID: "code", Name: "Code"}},
	}
	b, err := json.Marshal(ms)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))

	// Must use "currentModeId" (not "currentModeID")
	assert.Contains(t, m, "currentModeId")
	assert.NotContains(t, m, "currentModeID")

	// Must use "availableModes" with nested "id" and "name"
	modes, ok := m["availableModes"].([]any)
	require.True(t, ok)
	require.Len(t, modes, 2)
	firstMode := modes[0].(map[string]any)
	assert.Contains(t, firstMode, "id")
	assert.Contains(t, firstMode, "name")
}

// ── ThinkingEffortState JSON compatibility ───────────────────────────────────

func TestThinkingEffortState_JSONFieldNames(t *testing.T) {
	tes := ThinkingEffortState{
		CurrentID:       "high",
		AvailableLevels: []ThinkingEffortDef{{ID: "low", Name: "Low"}, {ID: "high", Name: "High"}},
	}
	b, err := json.Marshal(tes)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))

	// Must use "currentId"
	assert.Contains(t, m, "currentId")

	// Must use "availableLevels" with nested "id" and "name"
	levels, ok := m["availableLevels"].([]any)
	require.True(t, ok)
	require.Len(t, levels, 2)
	firstLevel := levels[0].(map[string]any)
	assert.Contains(t, firstLevel, "id")
	assert.Contains(t, firstLevel, "name")
}

// ── ConfigOptionValue JSON compatibility ─────────────────────────────────────

func TestConfigOptionValue_JSONFieldNames(t *testing.T) {
	cv := ConfigOptionValue{ID: "low", Name: "Low"}
	b, err := json.Marshal(cv)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Contains(t, m, "id")
	assert.Contains(t, m, "name")
}

// ── SelectOptionDef ↔ ModeDef/ThinkingEffortDef/ConfigOptionValue equivalence ──

func TestSelectOptionDef_ModeDefEquivalent(t *testing.T) {
	// SelectOptionDef must have the same shape as ModeDef
	md := ModeDef{ID: "code", Name: "Code"}
	sod := SelectOptionDef(md)
	assert.Equal(t, md.ID, sod.ID)
	assert.Equal(t, md.Name, sod.Name)

	// JSON must be identical
	mdJSON, _ := json.Marshal(md)
	sodJSON, _ := json.Marshal(sod)
	assert.Equal(t, string(mdJSON), string(sodJSON))
}

func TestSelectOptionDef_ThinkingEffortDefEquivalent(t *testing.T) {
	ted := ThinkingEffortDef{ID: "high", Name: "High"}
	sod := SelectOptionDef(ted)
	assert.Equal(t, ted.ID, sod.ID)
	assert.Equal(t, ted.Name, sod.Name)

	tedJSON, _ := json.Marshal(ted)
	sodJSON, _ := json.Marshal(sod)
	assert.Equal(t, string(tedJSON), string(sodJSON))
}

func TestSelectOptionDef_ConfigOptionValueEquivalent(t *testing.T) {
	cov := ConfigOptionValue{ID: "plan", Name: "Plan"}
	sod := SelectOptionDef(cov)
	assert.Equal(t, cov.ID, sod.ID)
	assert.Equal(t, cov.Name, sod.Name)

	covJSON, _ := json.Marshal(cov)
	sodJSON, _ := json.Marshal(sod)
	assert.Equal(t, string(covJSON), string(sodJSON))
}

// ── ModeState ↔ SelectState conversion ───────────────────────────────────────

func TestSelectState_FromModeState(t *testing.T) {
	ms := &ModeState{
		CurrentModeID:  "code",
		AvailableModes: []ModeDef{{ID: "ask", Name: "Ask"}, {ID: "code", Name: "Code"}},
	}
	sel := NewSelectStateFromMode(ms)
	assert.Equal(t, "code", sel.CurrentID)
	assert.Equal(t, "mode", sel.Category)
	assert.Len(t, sel.Available, 2)
	assert.Equal(t, "ask", sel.Available[0].ID)
	assert.Equal(t, "Code", sel.Available[1].Name)
}

func TestSelectState_FromModeState_Nil(t *testing.T) {
	sel := NewSelectStateFromMode(nil)
	assert.True(t, sel.IsEmpty())
}

func TestSelectState_ToModeState(t *testing.T) {
	sel := SelectState{
		CurrentID: "code",
		Available: []SelectOptionDef{{ID: "ask", Name: "Ask"}, {ID: "code", Name: "Code"}},
		Category:  "mode",
	}
	ms := sel.ToModeState()
	require.NotNil(t, ms)
	assert.Equal(t, "code", ms.CurrentModeID)
	assert.Len(t, ms.AvailableModes, 2)
	assert.Equal(t, "ask", ms.AvailableModes[0].ID)
}

func TestSelectState_ToModeState_Empty(t *testing.T) {
	sel := SelectState{}
	ms := sel.ToModeState()
	assert.Nil(t, ms)
}

// ── ThinkingEffortState ↔ SelectState conversion ─────────────────────────────

func TestSelectState_FromThinkingEffortState(t *testing.T) {
	tes := &ThinkingEffortState{
		CurrentID:       "high",
		AvailableLevels: []ThinkingEffortDef{{ID: "low", Name: "Low"}, {ID: "high", Name: "High"}},
	}
	sel := NewSelectStateFromThinkingEffort(tes)
	assert.Equal(t, "high", sel.CurrentID)
	assert.Equal(t, "thought_level", sel.Category)
	assert.Len(t, sel.Available, 2)
	assert.Equal(t, "low", sel.Available[0].ID)
	assert.Equal(t, "High", sel.Available[1].Name)
}

func TestSelectState_FromThinkingEffortState_Nil(t *testing.T) {
	sel := NewSelectStateFromThinkingEffort(nil)
	assert.True(t, sel.IsEmpty())
}

func TestSelectState_ToThinkingEffortState(t *testing.T) {
	sel := SelectState{
		CurrentID: "high",
		Available: []SelectOptionDef{{ID: "low", Name: "Low"}, {ID: "high", Name: "High"}},
		Category:  "thought_level",
	}
	tes := sel.ToThinkingEffortState()
	require.NotNil(t, tes)
	assert.Equal(t, "high", tes.CurrentID)
	assert.Len(t, tes.AvailableLevels, 2)
	assert.Equal(t, "low", tes.AvailableLevels[0].ID)
}

func TestSelectState_ToThinkingEffortState_Empty(t *testing.T) {
	sel := SelectState{}
	tes := sel.ToThinkingEffortState()
	assert.Nil(t, tes)
}

// ── Round-trip: ModeState → SelectState → ModeState ──────────────────────────

func TestSelectState_ModeRoundTrip(t *testing.T) {
	original := &ModeState{
		CurrentModeID:  "architect",
		AvailableModes: []ModeDef{{ID: "ask", Name: "Ask"}, {ID: "architect", Name: "Architect"}, {ID: "code", Name: "Code"}},
	}
	sel := NewSelectStateFromMode(original)
	roundTrip := sel.ToModeState()
	require.NotNil(t, roundTrip)
	assert.Equal(t, original.CurrentModeID, roundTrip.CurrentModeID)
	assert.Equal(t, original.AvailableModes, roundTrip.AvailableModes)
}

// ── Round-trip: ThinkingEffortState → SelectState → ThinkingEffortState ──────

func TestSelectState_ThinkingEffortRoundTrip(t *testing.T) {
	original := &ThinkingEffortState{
		CurrentID:       "medium",
		AvailableLevels: []ThinkingEffortDef{{ID: "low", Name: "Low"}, {ID: "medium", Name: "Medium"}, {ID: "high", Name: "High"}},
	}
	sel := NewSelectStateFromThinkingEffort(original)
	roundTrip := sel.ToThinkingEffortState()
	require.NotNil(t, roundTrip)
	assert.Equal(t, original.CurrentID, roundTrip.CurrentID)
	assert.Equal(t, original.AvailableLevels, roundTrip.AvailableLevels)
}
