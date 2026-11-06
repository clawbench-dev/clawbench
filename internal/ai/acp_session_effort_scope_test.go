package ai

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// Issue #637 — the thinking-level list is per MODEL, not per agent.
//
// OpenCode advertises a different set for each model (glm-5.3: low/high/max/
// default, deepseek-v4-pro: high/max/default, a non-reasoning model: none at
// all), and the agent-level registry only holds whichever session refreshed it
// last. Two sessions on the same agent therefore showed each other's levels:
// the same model looked different depending on which entry point/session you
// opened, and a level such as "max" could disappear from one of them.

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

func effortIDs(levels []ThinkingEffortDef) []string {
	ids := make([]string, 0, len(levels))
	for _, l := range levels {
		ids = append(ids, l.ID)
	}
	return ids
}

func TestSetSessionThinkingEfforts_EmptyDoesNotErase(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "effort-scope-set", Backend: "acp-stdio"}, "effort-scope-set")

	conn.SetSessionThinkingEfforts([]ThinkingEffortDef{{ID: "low"}, {ID: "max"}})
	levels, ok := conn.GetSessionThinkingEfforts()
	require.True(t, ok)
	assert.Equal(t, []string{"low", "max"}, effortIDs(levels))

	// A response that omits the option must not be read as "the model has no
	// levels" — that would wipe the picker on every resume.
	conn.SetSessionThinkingEfforts(nil)
	levels, ok = conn.GetSessionThinkingEfforts()
	require.True(t, ok)
	assert.Equal(t, []string{"low", "max"}, effortIDs(levels))
}

func TestSessionThinkingEffortState_UnknownFallsBackToRegistry(t *testing.T) {
	agent := &model.Agent{ID: "effort-scope-unknown", Backend: "acp-stdio"}
	conn := newACPConn(agent, "effort-scope-unknown")

	// Never reported for this session: the caller must fall back to the
	// agent-level registry rather than showing an empty picker.
	assert.Nil(t, conn.SessionThinkingEffortState())
}

func TestEmitSessionStateEvents_UsesThisSessionsLevels(t *testing.T) {
	agent := &model.Agent{ID: "effort-scope-emit", Backend: "acp-stdio", AcpCommand: "echo"}
	reg := GetAgentCapabilityRegistry()
	// The agent-level list belongs to whichever session refreshed it last —
	// here a different model's list.
	reg.UpdateThinkingEfforts("effort-scope-emit", []ThinkingEffortDef{{ID: "none"}, {ID: "high"}})

	conn := newACPConn(agent, "effort-scope-emit")
	conn.SetSessionThinkingEfforts([]ThinkingEffortDef{
		{ID: "low", Name: "Low"},
		{ID: "high", Name: "High"},
		{ID: "max", Name: "Max"},
		{ID: "default", Name: "Default"},
	})
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
	assert.Equal(t, []string{"low", "high", "max", "default"}, effortIDs(got.AvailableLevels),
		"the session's own model list must win over the agent-level registry")
}

func TestApplyConfigOptionsFromSetResponse_RefreshesSessionLevels(t *testing.T) {
	agent := &model.Agent{ID: "effort-scope-switch", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "effort-scope-switch")
	conn.SetSessionThinkingEfforts([]ThinkingEffortDef{{ID: "low"}, {ID: "high"}, {ID: "max"}})

	// Model switch response for a model whose list is a SUBSET: the previous
	// (wider) list must not survive.
	conn.applyConfigOptionsFromSetResponse([]acp.SessionConfigOption{
		thinkingSelectOption("high", "max", "default"),
	})

	levels, ok := conn.GetSessionThinkingEfforts()
	require.True(t, ok)
	assert.Equal(t, []string{"high", "max", "default"}, effortIDs(levels))

	// And the agent-level registry is still refreshed as before.
	reg := GetAgentCapabilityRegistry()
	es := reg.GetThinkingEffortState("effort-scope-switch", "")
	require.NotNil(t, es)
	assert.Equal(t, []string{"high", "max", "default"}, effortIDs(es.AvailableLevels))
}

func TestGetCachedStateByClawbenchSID_PrefersSessionLevels(t *testing.T) {
	agent := &model.Agent{ID: "effort-scope-cache", Backend: "acp-stdio", AcpCommand: "echo"}
	reg := GetAgentCapabilityRegistry()
	reg.UpdateThinkingEfforts("effort-scope-cache", []ThinkingEffortDef{{ID: "none"}, {ID: "high"}})

	conn := newACPConn(agent, "effort-scope-cache")
	conn.SetSessionThinkingEfforts([]ThinkingEffortDef{{ID: "high"}, {ID: "max"}, {ID: "default"}})
	conn.SetCurrentThinkingEffortID("high")

	mgr := GetACPConnManager()
	mgr.SetConnForTest("effort-scope-cache", conn)
	defer mgr.RemoveConn("effort-scope-cache")

	state := mgr.GetCachedStateByClawbenchSID("effort-scope-cache")
	require.NotNil(t, state.Effort)
	assert.Equal(t, "high", state.Effort.CurrentID)
	assert.Equal(t, []string{"high", "max", "default"}, effortIDs(state.Effort.AvailableLevels))
}

func TestApplyConfigSelectToSessionCache_UpdatesSessionLevels(t *testing.T) {
	agent := &model.Agent{ID: "effort-scope-notify", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "effort-scope-notify")
	conn.SetSessionThinkingEfforts([]ThinkingEffortDef{{ID: "low"}, {ID: "high"}})

	// The agent pushes a new list through config_option_update instead of the
	// set_config_option response. The session copy must follow, or it would
	// outrank the refreshed registry everywhere it is preferred.
	sel := SelectState{
		Category:  "thought_level",
		CurrentID: "max",
		Available: []SelectOptionDef{{ID: "high"}, {ID: "max"}},
	}
	applyConfigSelectToSessionCache(sel, conn, GetAgentCapabilityRegistry(), true, true)

	levels, ok := conn.GetSessionThinkingEfforts()
	require.True(t, ok)
	assert.Equal(t, []string{"high", "max"}, effortIDs(levels))
	assert.Equal(t, "max", conn.GetCurrentThinkingEffortID())
}

func TestEmitSessionStateEvents_FallsBackToRegistryWithoutSessionList(t *testing.T) {
	agent := &model.Agent{ID: "effort-scope-fallback", Backend: "acp-stdio", AcpCommand: "echo"}
	reg := GetAgentCapabilityRegistry()
	reg.UpdateThinkingEfforts("effort-scope-fallback", []ThinkingEffortDef{{ID: "high", Name: "High"}})

	conn := newACPConn(agent, "effort-scope-fallback")
	conn.SetCurrentThinkingEffortID("high")

	ch := make(chan StreamEvent, 16)
	conn.EmitSessionStateEvents(ch)

	var got *ThinkingEffortState
	for _, e := range drainStreamEvents(ch) {
		if e.Type == "thinking_effort_update" {
			got = e.ThinkingEffort
		}
	}
	require.NotNil(t, got, "agent-wide backends must keep their registry fallback")
	assert.Equal(t, []string{"high"}, effortIDs(got.AvailableLevels))
}

func TestApplyConfigOptionsFromSetResponse_PartialResponseKeepsLevels(t *testing.T) {
	agent := &model.Agent{ID: "effort-scope-partial", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "effort-scope-partial")
	conn.SetSessionThinkingEfforts([]ThinkingEffortDef{{ID: "low"}, {ID: "high"}, {ID: "max"}})

	// An agent that answers with ONLY the changed option: a mode change must
	// not be read as "the model has no thinking levels".
	modeCat := acp.SessionConfigOptionCategoryMode
	conn.applyConfigOptionsFromSetResponse([]acp.SessionConfigOption{
		{
			Select: &acp.SessionConfigOptionSelect{
				Category:     &modeCat,
				Id:           "mode",
				Name:         "Mode",
				CurrentValue: acp.SessionConfigValueId("plan"),
			},
		},
	})

	levels, ok := conn.GetSessionThinkingEfforts()
	require.True(t, ok, "a mode-only response must not clear the level list")
	assert.Equal(t, []string{"low", "high", "max"}, effortIDs(levels))
}

func TestApplyExtractedState_RecordsSessionLevels(t *testing.T) {
	agent := &model.Agent{ID: "effort-scope-extract", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "effort-scope-extract")

	// session/new | resume | load carry the session's own level list.
	conn.applyExtractedState(sessionStateExtracted{
		efforts:         []ThinkingEffortDef{{ID: "high"}, {ID: "max"}, {ID: "default"}},
		effortCurrentID: "high",
	})

	levels, ok := conn.GetSessionThinkingEfforts()
	require.True(t, ok)
	assert.Equal(t, []string{"high", "max", "default"}, effortIDs(levels))
	assert.Equal(t, "high", conn.GetCurrentThinkingEffortID())
}
