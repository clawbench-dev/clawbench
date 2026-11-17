package ai

import (
	"context"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// These tests cover the config-option notification paths added for v2 agents:
// an agent that advertises one of the well-known categories (mode, thought_level,
// model) or a backend-specific one (codex's model_config) through
// config_option_update rather than through its NewSession response.
//
// The observation is the EVENT, not the handler: what the drawer renders is
// whatever reaches the channel.

// configOptionUpdate builds a config_option_update carrying one select option.
func configOptionUpdate(category acp.SessionConfigOptionCategory, id, current string, opts acp.SessionConfigSelectOptions) acp.SessionUpdate {
	cat := category
	return acp.SessionUpdate{ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{
		ConfigOptions: []acp.SessionConfigOption{{
			Select: &acp.SessionConfigOptionSelect{
				Category:     &cat,
				Id:           acp.SessionConfigId(id),
				Name:         id,
				CurrentValue: acp.SessionConfigValueId(current),
				Options:      opts,
			},
		}},
	}}
}

func ungroupedOptions(ids ...string) acp.SessionConfigSelectOptions {
	out := make(acp.SessionConfigSelectOptionsUngrouped, 0, len(ids))
	for _, id := range ids {
		out = append(out, acp.SessionConfigSelectOption{Value: acp.SessionConfigValueId(id), Name: id})
	}
	return acp.SessionConfigSelectOptions{Ungrouped: &out}
}

func collectUntilIdle(ch chan StreamEvent) []StreamEvent {
	var out []StreamEvent
	for {
		select {
		case ev := <-ch:
			out = append(out, ev)
		default:
			return out
		}
	}
}

// TestConfigOptionUpdate_ModelOptionUpdatesThePicker pins the model path: the
// first list is forwarded (the drawer has nothing yet), a repeated one is NOT
// (every repeat would re-render the picker), and a grown list is forwarded
// again.
func TestConfigOptionUpdate_ModelOptionUpdatesThePicker(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	const agentID = "cfg-model-agent"
	reg.Update(agentID, &AgentCapability{})
	conn := newTestACPConn(t, agentID)

	ch := make(chan StreamEvent, 8)
	first := configOptionUpdate(acp.SessionConfigOptionCategoryModel, "model", "a", ungroupedOptions("a", "b"))
	mapACPSessionUpdate(first, ch, context.Background(), conn, nil)

	events := collectUntilIdle(ch)
	require.Len(t, events, 1, "the first model list must reach the drawer")
	assert.Equal(t, "model_list_update", events[0].Type)
	require.NotNil(t, events[0].ModelList)
	assert.Equal(t, "a", events[0].ModelList.CurrentModelID)
	assert.Len(t, events[0].ModelList.Models, 2)
	assert.Len(t, reg.Get(agentID).AvailableModels, 2)

	// The same list again: nothing new, nothing forwarded.
	mapACPSessionUpdate(first, ch, context.Background(), conn, nil)
	assert.Empty(t, collectUntilIdle(ch), "an unchanged model list must not re-render the picker")

	// A grown list is new information.
	grown := configOptionUpdate(acp.SessionConfigOptionCategoryModel, "model", "b", ungroupedOptions("a", "b", "c"))
	mapACPSessionUpdate(grown, ch, context.Background(), conn, nil)
	events = collectUntilIdle(ch)
	require.Len(t, events, 1)
	assert.Len(t, events[0].ModelList.Models, 3)
}

// TestConfigOptionUpdate_ModelOptionWithoutConnStillCarriesPrices pins the
// connection-less branch: the list cannot be enriched with the CLI baseline, but
// it must still reach the drawer (with prices) rather than vanish.
func TestConfigOptionUpdate_ModelOptionWithoutConnStillCarriesPrices(t *testing.T) {
	ch := make(chan StreamEvent, 8)
	upd := configOptionUpdate(acp.SessionConfigOptionCategoryModel, "model", "a", ungroupedOptions("a", "b"))
	mapACPSessionUpdate(upd, ch, context.Background(), nil, nil)

	events := collectUntilIdle(ch)
	require.Len(t, events, 1)
	assert.Equal(t, "model_list_update", events[0].Type)
	require.NotNil(t, events[0].ModelList)
	assert.Len(t, events[0].ModelList.Models, 2)
}

// TestConfigOptionUpdate_ModeOptionReachesTheDrawer pins the mode path and the
// agent-level config state: the DESCRIPTIONS are stored on the agent, but the
// per-session current value must not be (it would leak one session's pick into
// every other session's picker).
func TestConfigOptionUpdate_ModeOptionReachesTheDrawer(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	const agentID = "cfg-mode-agent"
	reg.Update(agentID, &AgentCapability{})
	conn := newTestACPConn(t, agentID)

	ch := make(chan StreamEvent, 8)
	upd := configOptionUpdate(acp.SessionConfigOptionCategoryMode, "mode", "full", ungroupedOptions("read", "full"))
	mapACPSessionUpdate(upd, ch, context.Background(), conn, nil)

	events := collectUntilIdle(ch)
	require.NotEmpty(t, events)
	var sawMode bool
	for _, ev := range events {
		if ev.Type == "mode_update" {
			sawMode = true
			require.NotNil(t, ev.Mode)
			assert.Equal(t, "full", ev.Mode.CurrentModeID)
		}
	}
	assert.True(t, sawMode, "the mode options must reach the drawer")

	caps := reg.Get(agentID)
	require.NotNil(t, caps)
	require.NotNil(t, caps.ConfigOptionState, "the agent must remember the mode descriptions")
	assert.Empty(t, caps.ConfigOptionState.CurrentID,
		"the current value is per-session and must not enter the shared registry")
}

// TestConfigOptionUpdate_ThoughtLevelOptionReachesTheDrawer pins the level path.
func TestConfigOptionUpdate_ThoughtLevelOptionReachesTheDrawer(t *testing.T) {
	resetGlobalRegistryForTest(t)
	const agentID = "cfg-level-agent"
	conn := newTestACPConn(t, agentID)

	ch := make(chan StreamEvent, 8)
	upd := configOptionUpdate(acp.SessionConfigOptionCategoryThoughtLevel, "thought_level", "high", ungroupedOptions("low", "high"))
	mapACPSessionUpdate(upd, ch, context.Background(), conn, nil)

	events := collectUntilIdle(ch)
	require.NotEmpty(t, events)
	assert.Equal(t, "thinking_effort_update", events[0].Type)
}

// TestConfigOptionUpdate_UnknownCategoryIsMergedIntoTheExtras pins the default
// branch: a backend-specific option (codex's model_config) is merged into the
// connection's extras AND forwarded, because a notification stating what the
// agent actually runs must correct the optimistic value the drawer applied.
func TestConfigOptionUpdate_UnknownCategoryIsMergedIntoTheExtras(t *testing.T) {
	resetGlobalRegistryForTest(t)
	const agentID = "cfg-extra-agent"
	conn := newTestACPConn(t, agentID)

	ch := make(chan StreamEvent, 8)
	upd := configOptionUpdate("model_config", "model_config", "fast", ungroupedOptions("fast", "slow"))
	mapACPSessionUpdate(upd, ch, context.Background(), conn, nil)

	events := collectUntilIdle(ch)
	require.Len(t, events, 1, "the corrected value must reach the drawer")
	assert.Equal(t, "config_options_update", events[0].Type)
	require.Len(t, events[0].ConfigOptions, 1)
	assert.Equal(t, "model_config", events[0].ConfigOptions[0].ConfigID)
	assert.Equal(t, "fast", events[0].ConfigOptions[0].CurrentID)

	// The connection now holds it, so a later reader sees what the agent runs.
	extras := conn.GetExtraConfigOptions()
	require.Len(t, extras, 1)
	assert.Equal(t, "fast", extras[0].CurrentID)
}

// TestConfigOptionUpdate_UnknownCategoryWithoutConnIsDropped pins the guard: with
// nothing to merge into, the option is skipped instead of forwarded as an
// untyped event nobody can apply.
func TestConfigOptionUpdate_UnknownCategoryWithoutConnIsDropped(t *testing.T) {
	ch := make(chan StreamEvent, 8)
	upd := configOptionUpdate("model_config", "model_config", "fast", ungroupedOptions("fast"))
	mapACPSessionUpdate(upd, ch, context.Background(), nil, nil)
	assert.Empty(t, collectUntilIdle(ch), "a custom option needs a connection to be stored on")
}

// TestConfigOptionUpdate_SkipsOptionsItCannotUse pins the two defensive skips: a
// non-select option and a select with no category are ignored rather than
// crashing or being misrouted into the extras.
func TestConfigOptionUpdate_SkipsOptionsItCannotUse(t *testing.T) {
	resetGlobalRegistryForTest(t)
	const agentID = "cfg-skip-agent"
	conn := newTestACPConn(t, agentID)

	uncategorized := ungroupedOptions("a")
	ch := make(chan StreamEvent, 8)
	upd := acp.SessionUpdate{ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{
		ConfigOptions: []acp.SessionConfigOption{
			{},
			{Select: &acp.SessionConfigOptionSelect{Id: "no-category", Options: uncategorized}},
		},
	}}
	mapACPSessionUpdate(upd, ch, context.Background(), conn, nil)
	assert.Empty(t, collectUntilIdle(ch))
	assert.Empty(t, conn.GetExtraConfigOptions(), "an option with no category must not become an extra")
}

// newTestACPConn builds a connected-enough ACPConn for event-mapping tests: only
// the identity is used by these paths, nothing talks to a subprocess.
func newTestACPConn(t *testing.T, agentID string) *ACPConn {
	t.Helper()
	agent := &model.Agent{ID: agentID, Backend: "test-backend", Name: agentID}
	return NewACPConnForTest(agent, "cb-"+agentID)
}
