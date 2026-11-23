package ai

import (
	"context"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// extraSelect builds a select config option for the tests below. `category`
// is the agent's own semantic label — "model_config" is what codex-acp uses
// for Fast mode, and the whole point of the extraction is that ClawBench does
// not need to know that name.
func extraSelect(id, name, category, current string, values ...acp.SessionConfigSelectOption) acp.SessionConfigOption {
	cat := acp.SessionConfigOptionCategory(category)
	ungrouped := acp.SessionConfigSelectOptionsUngrouped(values)
	desc := "1.5x speed, increased usage"
	return acp.SessionConfigOption{Select: &acp.SessionConfigOptionSelect{
		Id:           acp.SessionConfigId(id),
		Name:         name,
		Category:     &cat,
		CurrentValue: acp.SessionConfigValueId(current),
		Description:  &desc,
		Options:      acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
	}}
}

func fastModeOption(current string) acp.SessionConfigOption {
	return extraSelect("fast-mode", "Fast mode", "model_config", current,
		acp.SessionConfigSelectOption{Value: "off", Name: "Off"},
		acp.SessionConfigSelectOption{Value: "on", Name: "On"},
	)
}

// The three categories ClawBench already renders must not be re-surfaced as
// generic extras, or every agent would gain a duplicate picker.
func TestExtractExtraConfigOptions_OnlyUnknownCategories(t *testing.T) {
	opts := []acp.SessionConfigOption{
		extraSelect("mode", "Mode", "mode", "code",
			acp.SessionConfigSelectOption{Value: "code", Name: "Code"}),
		extraSelect("model", "Model", "model", "gpt",
			acp.SessionConfigSelectOption{Value: "gpt", Name: "GPT"}),
		extraSelect("effort", "Thinking", "thought_level", "high",
			acp.SessionConfigSelectOption{Value: "high", Name: "High"}),
		fastModeOption("off"),
	}

	extras := extractExtraConfigOptions(opts)

	require.Len(t, extras, 1)
	assert.Equal(t, "fast-mode", extras[0].ConfigID)
	assert.Equal(t, "Fast mode", extras[0].Name)
	assert.Equal(t, "model_config", extras[0].Category)
	assert.Equal(t, "1.5x speed, increased usage", extras[0].Description)
	assert.Equal(t, "off", extras[0].CurrentID)
	assert.Equal(t, "select", extras[0].Type)
	assert.Equal(t, []ConfigOptionValue{{ID: "off", Name: "Off"}, {ID: "on", Name: "On"}}, extras[0].Values)
}

// A boolean option has no value list. Its current value becomes the same
// "true"/"false" string the rest of the config path already uses, so the HTTP
// body and session cache do not need a second value type.
func TestExtractExtraConfigOptions_Boolean(t *testing.T) {
	category := acp.SessionConfigOptionCategory("model_config")
	desc := "1.5x speed"
	opts := []acp.SessionConfigOption{{Boolean: &acp.SessionConfigOptionBoolean{
		Id: "fast-mode", Name: "Fast mode", Category: &category,
		Description: &desc, Type: "boolean", CurrentValue: true,
	}}}

	extras := extractExtraConfigOptions(opts)

	require.Len(t, extras, 1)
	assert.Equal(t, "fast-mode", extras[0].ConfigID)
	assert.Equal(t, "Fast mode", extras[0].Name)
	assert.Equal(t, "1.5x speed", extras[0].Description)
	assert.Equal(t, "model_config", extras[0].Category)
	assert.Equal(t, "boolean", extras[0].Type)
	assert.Equal(t, "true", extras[0].CurrentID)
	assert.Empty(t, extras[0].Values)
}

// A boolean whose category is one of the three dedicated pickers would
// duplicate that picker; it is skipped like the select case.
func TestExtractExtraConfigOptions_SkipsWellKnownBoolean(t *testing.T) {
	category := acp.SessionConfigOptionCategory("mode")
	opts := []acp.SessionConfigOption{{Boolean: &acp.SessionConfigOptionBoolean{
		Id: "toggle", Name: "Toggle", Category: &category, Type: "boolean", CurrentValue: true,
	}}}
	assert.Empty(t, extractExtraConfigOptions(opts))
}

// An option with no values cannot be rendered as a picker, and an option with
// no category is not identifiable on the wire — both are skipped rather than
// producing an empty, unusable row.
func TestExtractExtraConfigOptions_SkipsUnusableOptions(t *testing.T) {
	noValues := extraSelect("mystery", "Mystery", "custom", "x")
	noValues.Select.Options = acp.SessionConfigSelectOptions{}

	noCategory := extraSelect("uncategorised", "Uncategorised", "", "x",
		acp.SessionConfigSelectOption{Value: "x", Name: "X"})

	extras := extractExtraConfigOptions([]acp.SessionConfigOption{noValues, noCategory})
	assert.Empty(t, extras)
}

// A resume re-states the option list and the agent's own idea of the current
// value. The user's pick must win: otherwise every respawn silently flips the
// toggle back to the agent's default.
func TestSetExtraConfigOptions_KeepsUserPickAcrossResume(t *testing.T) {
	conn := newLiveConnForConfigTest("extra-keep")
	installConfigSendRecorder(t, conn)

	conn.SetExtraConfigOptions([]ExtraConfigOption{{
		ConfigID: "fast-mode", Category: "model_config", CurrentID: "off",
		Values: []ConfigOptionValue{{ID: "off"}, {ID: "on"}},
	}})
	conn.SetSessionConfigOption(context.Background(), "fast-mode", "on")

	got := conn.GetExtraConfigOptions()
	require.Len(t, got, 1)
	require.Equal(t, "on", got[0].CurrentID, "the pick must land on the session cache")

	// Agent re-reports its own default on resume.
	conn.SetExtraConfigOptions([]ExtraConfigOption{{
		ConfigID: "fast-mode", Category: "model_config", CurrentID: "off",
		Values: []ConfigOptionValue{{ID: "off"}, {ID: "on"}},
	}})

	got = conn.GetExtraConfigOptions()
	require.Len(t, got, 1)
	assert.Equal(t, "on", got[0].CurrentID, "a resume must not overwrite the user's pick")
}

// A session whose model does not support the option gets an empty list; that
// must be visible as "nothing to render", not as a stale toggle — and the empty
// list has to stay NON-NIL so it survives as [] on the wire (nil makes the hub
// drop the event, leaving the toggle on screen forever).
func TestSetExtraConfigOptions_EmptyListClears(t *testing.T) {
	conn := newLiveConnForConfigTest("extra-clear")
	installConfigSendRecorder(t, conn)

	conn.SetExtraConfigOptions([]ExtraConfigOption{{
		ConfigID: "fast-mode", Category: "model_config", CurrentID: "off",
		Values: []ConfigOptionValue{{ID: "off"}, {ID: "on"}},
	}})
	conn.SetSessionConfigOption(context.Background(), "fast-mode", "on")
	require.True(t, conn.HasExtraConfigOptions())

	conn.SetExtraConfigOptions(nil)

	got := conn.GetExtraConfigOptions()
	assert.NotNil(t, got, "an empty list must stay non-nil so the client is told to clear")
	assert.Empty(t, got)

	// The pick died with the option. When it comes back (model switch back),
	// the AGENT's value wins — a stale "on" from a model that never had it
	// would make the UI lie about what will actually run.
	conn.SetExtraConfigOptions([]ExtraConfigOption{{
		ConfigID: "fast-mode", Category: "model_config", CurrentID: "off",
		Values: []ConfigOptionValue{{ID: "off"}, {ID: "on"}},
	}})
	got = conn.GetExtraConfigOptions()
	require.Len(t, got, 1)
	assert.Equal(t, "off", got[0].CurrentID)
}

func TestEmitSessionStateEvents_EmitsConfigOptionsUpdate(t *testing.T) {
	conn := newLiveConnForConfigTest("extra-emit")
	conn.SetExtraConfigOptions([]ExtraConfigOption{{
		ConfigID: "fast-mode", Name: "Fast mode", Category: "model_config", CurrentID: "on",
		Values: []ConfigOptionValue{{ID: "off"}, {ID: "on"}},
	}})

	ch := make(chan StreamEvent, 8)
	conn.EmitSessionStateEvents(ch)
	close(ch)

	var emitted *StreamEvent
	for evt := range ch {
		if evt.Type == "config_options_update" {
			e := evt
			emitted = &e
		}
	}

	require.NotNil(t, emitted, "the drawer can only render the option if the event reaches it")
	require.Len(t, emitted.ConfigOptions, 1)
	assert.Equal(t, "fast-mode", emitted.ConfigOptions[0].ConfigID)
	assert.Equal(t, "on", emitted.ConfigOptions[0].CurrentID)
}

// Without this, a respawn drops the toggle and the user has to flip it again
// after every crash/reconnect.
func TestReapplyConfigAfterResume_ReappliesExtraOptions(t *testing.T) {
	conn := newLiveConnForConfigTest("extra-reapply")
	rec := installConfigSendRecorder(t, conn)

	conn.mu.Lock()
	conn.reapplyConfigAfterResume(context.Background(), "acp-sid-extra-reapply", cachedConfigSnapshot{
		extras: map[string]string{"fast-mode": "on"},
	})
	conn.mu.Unlock()

	sends := rec.all()
	require.Len(t, sends, 1)
	assert.Equal(t, "fast-mode", sends[0].configID)
	assert.Equal(t, "on", sends[0].value)
}

// The agent's own notification is the correction path: it states what is
// actually running, so it must win over the optimistic value the drawer applied
// on click (the agent may have refused or rewritten the pick).
func TestUpsertExtraConfigOption_AdoptsAgentValue(t *testing.T) {
	conn := newLiveConnForConfigTest("extra-upsert")
	conn.SetExtraConfigOptions([]ExtraConfigOption{{
		ConfigID: "fast-mode", Name: "Fast mode", Category: "model_config", CurrentID: "off",
		Values: []ConfigOptionValue{{ID: "off"}, {ID: "on"}},
	}})
	conn.UpdateCachedCurrent("fast-mode", "on") // the user's optimistic pick

	conn.UpsertExtraConfigOption(ExtraConfigOption{
		ConfigID: "fast-mode", Category: "model_config", CurrentID: "off",
	})

	got := conn.GetExtraConfigOptions()
	require.Len(t, got, 1)
	assert.Equal(t, "off", got[0].CurrentID, "the agent's report must correct the optimistic pick")
	// A minimal update carries no labels; they must survive or the picker renders blank.
	assert.Equal(t, "Fast mode", got[0].Name)
	assert.Len(t, got[0].Values, 2)
}

// An option the agent starts offering mid-session is added, not ignored.
func TestUpsertExtraConfigOption_AddsUnknownOption(t *testing.T) {
	conn := newLiveConnForConfigTest("extra-upsert-new")

	conn.UpsertExtraConfigOption(ExtraConfigOption{
		ConfigID: "fast-mode", Name: "Fast mode", Category: "model_config", CurrentID: "on",
		Values: []ConfigOptionValue{{ID: "off"}, {ID: "on"}},
	})

	got := conn.GetExtraConfigOptions()
	require.Len(t, got, 1)
	assert.Equal(t, "fast-mode", got[0].ConfigID)
	assert.Equal(t, "on", got[0].CurrentID)
}

// An agent that only advertises mode/model/thought_level must NOT be marked as
// "advertises extras": that would make every ACP session emit an empty
// config_options_update on every turn.
func TestApplyExtractedState_DoesNotFlagExtrasForOrdinaryAgents(t *testing.T) {
	conn := newLiveConnForConfigTest("extra-ordinary")

	conn.applyExtractedState(sessionStateExtracted{extrasKnown: true})

	assert.False(t, conn.HasExtraConfigOptions())
	assert.Nil(t, conn.GetExtraConfigOptions())
}

func TestSnapshotCachedConfig_IncludesExtraPicks(t *testing.T) {
	conn := newLiveConnForConfigTest("extra-snapshot")

	assert.Nil(t, conn.snapshotCachedConfig().extras, "nothing picked yet")

	conn.SetExtraConfigOptions([]ExtraConfigOption{{
		ConfigID: "fast-mode", Category: "model_config", CurrentID: "off",
		Values: []ConfigOptionValue{{ID: "off"}, {ID: "on"}},
	}})
	conn.UpdateCachedCurrent("fast-mode", "on")

	assert.Equal(t, map[string]string{"fast-mode": "on"}, conn.snapshotCachedConfig().extras)
}

// The option list must reach the agent-level registry, keyed by the model it was
// advertised for (issue #746). Without that, a NEW session has no list until its
// first message creates an ACP session, and the "advanced" tab cannot render.
func TestApplyExtractedState_ReportsExtrasPerModel(t *testing.T) {
	agent := &model.Agent{ID: "extras-report-agent", Backend: "codex", AcpCommand: "codex"}
	conn := newACPConn(agent, "extras-report-sid")

	reg := GetAgentCapabilityRegistry()
	reg.ResetForTest(agent.ID)
	t.Cleanup(func() { reg.ResetForTest(agent.ID) })

	modelCategory := acp.SessionConfigOptionCategoryModel
	modelOpt := acp.SessionConfigOption{Select: &acp.SessionConfigOptionSelect{
		Id:           acp.SessionConfigId("model"),
		Name:         "Model",
		Category:     &modelCategory,
		CurrentValue: acp.SessionConfigValueId("m1"),
		Options: acp.SessionConfigSelectOptions{Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
			{Value: "m1", Name: "M1"},
		}},
	}}
	newResp := &acp.NewSessionResponse{
		ConfigOptions: []acp.SessionConfigOption{modelOpt, fastModeOption("off")},
	}

	ext := conn.extractSessionState(func() (*acp.NewSessionResponse, *acp.ResumeSessionResponse) {
		return newResp, nil
	})
	conn.applyExtractedState(ext)

	got := reg.GetExtraConfigOptions(agent.ID, "m1")
	require.Len(t, got, 1, "the list must be cached for the model that advertised it")
	assert.Equal(t, "fast-mode", got[0].ConfigID)
	// The registry keeps the option's SHAPE, never the value a session ran
	// (issue #746) — the value comes from the session or the per-model default.
	assert.Empty(t, got[0].CurrentID)
	assert.Nil(t, reg.GetExtraConfigOptions(agent.ID, "other-model"))
}

// The registry is the fallback that lets a NEW session render the tab before its
// first message, so a set_config_option response may only ADD knowledge about a
// model. An empty list there is not evidence ("this agent answered with just the
// changed option"), and erasing a model's entry would silently hide the tab.
func TestCacheExtrasForModel_RecordsPerModelOnly(t *testing.T) {
	agent := &model.Agent{ID: "cache-extras-agent", Backend: "codex", AcpCommand: "codex"}
	conn := newACPConn(agent, "cache-extras-sid")

	reg := GetAgentCapabilityRegistry()
	reg.ResetForTest(agent.ID)
	t.Cleanup(func() { reg.ResetForTest(agent.ID) })

	conn.cacheExtrasForModel(agent.ID, "m1", []ExtraConfigOption{{ConfigID: "fast-mode"}})
	require.Len(t, reg.GetExtraConfigOptions(agent.ID, "m1"), 1)

	conn.cacheExtrasForModel(agent.ID, "m1", nil)
	require.Len(t, reg.GetExtraConfigOptions(agent.ID, "m1"), 1,
		"an empty list from a partial response must not erase a learned model")
	assert.Nil(t, reg.GetExtraConfigOptions(agent.ID, "m2"))
}

// ForceUpdate replaces the whole capability struct on the first session of every
// process. The per-model option lists are accumulated knowledge and must be
// carried through — otherwise every restart (and every first message) drops them
// and the C1 fallback finds nothing, so a stored pick is never applied.
func TestForceUpdate_KeepsExtraConfigOptionsByModel(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.UpdateExtraConfigOptions("a1", "m1", []ExtraConfigOption{{ConfigID: "fast-mode"}})

	applied := reg.ForceUpdateIfNeeded("a1", nil, nil, nil, nil, nil, false, false, false, false)
	require.True(t, applied)

	got := reg.GetExtraConfigOptions("a1", "m1")
	require.Len(t, got, 1, "the per-model option list must survive ForceUpdate")
	assert.Equal(t, "fast-mode", got[0].ConfigID)
}
