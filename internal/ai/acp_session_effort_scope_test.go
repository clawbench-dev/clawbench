package ai

import (
	"context"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// Issue #637 — the thinking-level list is per MODEL, not per agent.
//
// OpenCode advertises a different set for each model (glm-5.3: low/high/max/
// default, deepseek-v4-pro: high/max/default), and the agent-level registry
// only held whichever session refreshed it last. Two sessions on the same
// agent therefore showed each other's levels: the same model looked different
// depending on which entry point/session you opened, and a level such as "max"
// could disappear from one of them.

func thinkingSelectOption(values ...string) acp.SessionConfigOption {
	cat := acp.SessionConfigOptionCategoryThoughtLevel
	opts := make(acp.SessionConfigSelectOptionsUngrouped, 0, len(values))
	for _, v := range values {
		opts = append(opts, acp.SessionConfigSelectOption{Value: acp.SessionConfigValueId(v), Name: v})
	}
	return acp.SessionConfigOption{
		Select: &acp.SessionConfigOptionSelect{
			Category:     &cat,
			Id:           "thought_level",
			Name:         "Thinking",
			CurrentValue: acp.SessionConfigValueId(values[0]),
			Options:      acp.SessionConfigSelectOptions{Ungrouped: &opts},
		},
	}
}

func modelSelectOption(modelID string) acp.SessionConfigOption {
	cat := acp.SessionConfigOptionCategoryModel
	return acp.SessionConfigOption{
		Select: &acp.SessionConfigOptionSelect{
			Category:     &cat,
			Id:           "model",
			Name:         "Model",
			CurrentValue: acp.SessionConfigValueId(modelID),
		},
	}
}

func effortIDs(levels []ThinkingEffortDef) []string {
	ids := make([]string, 0, len(levels))
	for _, l := range levels {
		ids = append(ids, l.ID)
	}
	return ids
}

func TestThinkingEffortsByModel_ScopedByModel(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)

	reg.UpdateThinkingEffortsForModel("effort-scope-a", "glm-5.3",
		[]ThinkingEffortDef{{ID: "low"}, {ID: "high"}, {ID: "max"}, {ID: "default"}})
	reg.UpdateThinkingEffortsForModel("effort-scope-a", "deepseek-v4-pro",
		[]ThinkingEffortDef{{ID: "high"}, {ID: "max"}, {ID: "default"}})

	glm := reg.GetThinkingEffortStateForModel("effort-scope-a", "glm-5.3", "max")
	require.NotNil(t, glm)
	assert.Equal(t, []string{"low", "high", "max", "default"}, effortIDs(glm.AvailableLevels))

	deepseek := reg.GetThinkingEffortStateForModel("effort-scope-a", "deepseek-v4-pro", "high")
	require.NotNil(t, deepseek)
	assert.Equal(t, []string{"high", "max", "default"}, effortIDs(deepseek.AvailableLevels))

	// A model that never reported anything yields NOTHING — deliberately no
	// fallback to the agent-level list, which would show another model's levels.
	reg.UpdateThinkingEfforts("effort-scope-a", []ThinkingEffortDef{{ID: "none"}, {ID: "high"}})
	assert.Nil(t, reg.GetThinkingEffortStateForModel("effort-scope-a", "unseen-model", ""),
		"an unknown model must not borrow another model's list")
}

func TestUpdateThinkingEffortsForModel_IgnoresEmpty(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.UpdateThinkingEffortsForModel("effort-scope-empty", "glm-5.3", []ThinkingEffortDef{{ID: "low"}})

	// An empty list carries no information and must not overwrite a known one.
	reg.UpdateThinkingEffortsForModel("effort-scope-empty", "glm-5.3", nil)
	levels, ok := reg.ThinkingEffortsForModel("effort-scope-empty", "glm-5.3")
	require.True(t, ok)
	assert.Equal(t, []string{"low"}, effortIDs(levels))

	// An EMPTY model id is a legitimate key: agents that advertise levels
	// without any model option (OpenClaw) store theirs there, and a session
	// with no model reads the same key.
	reg.UpdateThinkingEffortsForModel("effort-scope-empty", "", []ThinkingEffortDef{{ID: "high"}})
	levels, ok = reg.ThinkingEffortsForModel("effort-scope-empty", "")
	require.True(t, ok, "the no-model key must be usable")
	assert.Equal(t, []string{"high"}, effortIDs(levels))
}

func TestEmitSessionStateEvents_UsesTheSessionsModel(t *testing.T) {
	agent := &model.Agent{ID: "effort-scope-emit", Backend: "acp-stdio", AcpCommand: "echo"}
	reg := resetGlobalRegistryForTest(t)
	reg.UpdateThinkingEffortsForModel("effort-scope-emit", "glm-5.3",
		[]ThinkingEffortDef{{ID: "low"}, {ID: "high"}, {ID: "max"}, {ID: "default"}})
	reg.UpdateThinkingEffortsForModel("effort-scope-emit", "deepseek-v4-pro",
		[]ThinkingEffortDef{{ID: "high"}, {ID: "max"}, {ID: "default"}})

	conn := newACPConn(agent, "effort-scope-emit")
	conn.SetCurrentModelID("deepseek-v4-pro")
	conn.SetCurrentThinkingEffortID("max")

	ch := make(chan StreamEvent, 16)
	conn.EmitSessionStateEvents(ch)

	var got *ThinkingEffortState
	for _, e := range drainStreamEvents(ch) {
		if e.Type == "thinking_effort_update" {
			got = e.ThinkingEffort
		}
	}
	require.NotNil(t, got, "expected a thinking_effort_update event")
	assert.Equal(t, "max", got.CurrentID)
	assert.Equal(t, []string{"high", "max", "default"}, effortIDs(got.AvailableLevels),
		"the session's model list must win over the other model's")
}

func TestApplyConfigOptionsFromSetResponse_RecordsPerModel(t *testing.T) {
	agent := &model.Agent{ID: "effort-scope-switch", Backend: "acp-stdio", AcpCommand: "echo"}
	reg := resetGlobalRegistryForTest(t)
	conn := newACPConn(agent, "effort-scope-switch")
	conn.SetCurrentModelID("glm-5.3")

	// Model switch response: the new list belongs to the model the response
	// names, not to the one the session was running before.
	conn.applyConfigOptionsFromSetResponse([]acp.SessionConfigOption{
		modelSelectOption("deepseek-v4-pro"),
		thinkingSelectOption("high", "max", "default"),
	}, "")

	levels, ok := reg.ThinkingEffortsForModel("effort-scope-switch", "deepseek-v4-pro")
	require.True(t, ok)
	assert.Equal(t, []string{"high", "max", "default"}, effortIDs(levels))

	_, ok = reg.ThinkingEffortsForModel("effort-scope-switch", "glm-5.3")
	assert.False(t, ok, "the list must not be attributed to the previous model")
}

func TestApplyConfigSelectToSessionCache_RecordsPerModel(t *testing.T) {
	agent := &model.Agent{ID: "effort-scope-notify", Backend: "acp-stdio", AcpCommand: "echo"}
	reg := resetGlobalRegistryForTest(t)
	conn := newACPConn(agent, "effort-scope-notify")
	conn.SetCurrentModelID("glm-5.3")

	// The agent pushes a new list through config_option_update instead of the
	// set_config_option response.
	sel := SelectState{
		Category:  "thought_level",
		CurrentID: "max",
		Available: []SelectOptionDef{{ID: "high"}, {ID: "max"}},
	}
	applyConfigSelectToSessionCache(sel, conn, reg, true, true)

	levels, ok := reg.ThinkingEffortsForModel("effort-scope-notify", "glm-5.3")
	require.True(t, ok)
	assert.Equal(t, []string{"high", "max"}, effortIDs(levels))
	assert.Equal(t, "max", conn.GetCurrentThinkingEffortID())
}

func TestApplyExtractedState_RecordsUnderReportedModel(t *testing.T) {
	agent := &model.Agent{ID: "effort-scope-extract", Backend: "acp-stdio", AcpCommand: "echo"}
	reg := resetGlobalRegistryForTest(t)
	conn := newACPConn(agent, "effort-scope-extract")
	// The user's pick for the NEXT turn is already pre-applied.
	conn.SetCurrentModelID("user-pick")

	// session/new | resume | load: the list describes the model the agent is
	// actually running, which may differ from the pre-applied user pick.
	conn.applyExtractedState(sessionStateExtracted{
		efforts:         []ThinkingEffortDef{{ID: "high"}, {ID: "max"}, {ID: "default"}},
		effortCurrentID: "high",
		modelCurrentID:  "reported-model",
	})

	levels, ok := reg.ThinkingEffortsForModel("effort-scope-extract", "reported-model")
	require.True(t, ok)
	assert.Equal(t, []string{"high", "max", "default"}, effortIDs(levels))

	_, ok = reg.ThinkingEffortsForModel("effort-scope-extract", "user-pick")
	assert.False(t, ok, "the list must not be attributed to the not-yet-applied pick")
	assert.Equal(t, "user-pick", conn.GetCurrentModelID(), "the pre-applied pick is still preserved")
}

func TestRegistry_ThinkingEffortsByModelSurviveSaveAndLoad(t *testing.T) {
	db := setupCapabilityDB(t)
	origDB := getRegistryDB()
	t.Cleanup(func() { SetRegistryDB(origDB) })
	SetRegistryDB(db)

	_, err := db.Exec("INSERT INTO agents (id, transport) VALUES ('effort-scope-persist', 'acp-stdio')")
	require.NoError(t, err)

	reg := resetGlobalRegistryForTest(t)
	reg.UpdateThinkingEffortsForModel("effort-scope-persist", "glm-5.3",
		[]ThinkingEffortDef{{ID: "low"}, {ID: "max"}})
	reg.UpdateThinkingEffortsForModel("effort-scope-persist", "deepseek-v4-pro",
		[]ThinkingEffortDef{{ID: "high"}, {ID: "max"}})

	// persistAsync runs in a goroutine; wait for BOTH entries to land (the two
	// updates are written one after the other).
	require.Eventually(t, func() bool {
		var raw string
		if err := db.QueryRow("SELECT acp_thinking_efforts_by_model FROM agents WHERE id = 'effort-scope-persist'").Scan(&raw); err != nil {
			return false
		}
		return strings.Contains(raw, "glm-5.3") && strings.Contains(raw, "deepseek-v4-pro")
	}, 2*time.Second, 20*time.Millisecond, "per-model levels must be persisted")

	reloaded := resetGlobalRegistryForTest(t)
	reloaded.LoadFromDB(db)

	glm, ok := reloaded.ThinkingEffortsForModel("effort-scope-persist", "glm-5.3")
	require.True(t, ok, "per-model lists must survive a restart")
	assert.Equal(t, []string{"low", "max"}, effortIDs(glm))
	deepseek, ok := reloaded.ThinkingEffortsForModel("effort-scope-persist", "deepseek-v4-pro")
	require.True(t, ok)
	assert.Equal(t, []string{"high", "max"}, effortIDs(deepseek))
}

func TestSetConfigOption_UpdatesCachedModel(t *testing.T) {
	agent := &model.Agent{ID: "effort-scope-cachemodel", Backend: "opencode", AcpCommand: "echo"}
	conn := NewACPConnForTest(agent, "effort-scope-cachemodel")
	conn.alive = true
	conn.conn = newLiveConnForTest(t)
	conn.SetCurrentModelID("glm-5.3")
	conn.SetConfigOptionFnForTest(func(context.Context, string, string, string) error { return nil })

	// The click-time switch goes through this path; the cached model must
	// follow, or the per-model level list is resolved against the old model.
	require.NoError(t, conn.setConfigOptionWithCrashCheck(context.Background(), "sid",
		configOptionSpec{configID: "model", value: "deepseek-v4-pro", label: "model"}))
	assert.Equal(t, "deepseek-v4-pro", conn.GetCurrentModelID())
}

func TestEmitSessionStateEvents_NoModelAgentUsesEmptyKey(t *testing.T) {
	agent := &model.Agent{ID: "effort-scope-nomodel", Backend: "openclaw", AcpCommand: "openclaw acp"}
	reg := resetGlobalRegistryForTest(t)
	// OpenClaw advertises thought_level but no model option, so its list is
	// stored under the empty key and must still reach the drawer.
	reg.UpdateThinkingEffortsForModel("effort-scope-nomodel", "", []ThinkingEffortDef{{ID: "low"}, {ID: "high"}})

	conn := newACPConn(agent, "effort-scope-nomodel")
	conn.SetCurrentThinkingEffortID("high")

	ch := make(chan StreamEvent, 16)
	conn.EmitSessionStateEvents(ch)

	var got *ThinkingEffortState
	for _, e := range drainStreamEvents(ch) {
		if e.Type == "thinking_effort_update" {
			got = e.ThinkingEffort
		}
	}
	require.NotNil(t, got, "an agent without a model option must still get its levels")
	assert.Equal(t, []string{"low", "high"}, effortIDs(got.AvailableLevels))
}

func TestApplyConfigOptionsFromSetResponse_AttributedToRequestedModel(t *testing.T) {
	agent := &model.Agent{ID: "effort-scope-requested", Backend: "acp-stdio", AcpCommand: "echo"}
	reg := resetGlobalRegistryForTest(t)
	conn := newACPConn(agent, "effort-scope-requested")
	conn.SetCurrentModelID("glm-5.3")

	// The agent answers with ONLY the changed option (no model option), while
	// the cached current model is still the one being left. The list belongs to
	// the model the call was setting.
	conn.applyConfigOptionsFromSetResponse([]acp.SessionConfigOption{
		thinkingSelectOption("high", "max", "default"),
	}, "deepseek-v4-pro")

	levels, ok := reg.ThinkingEffortsForModel("effort-scope-requested", "deepseek-v4-pro")
	require.True(t, ok)
	assert.Equal(t, []string{"high", "max", "default"}, effortIDs(levels))

	_, ok = reg.ThinkingEffortsForModel("effort-scope-requested", "glm-5.3")
	assert.False(t, ok, "must not be attributed to the model being left")
}
