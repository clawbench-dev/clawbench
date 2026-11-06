package ai

import (
	"context"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	assert.Equal(t, []ConfigOptionValue{{ID: "off", Name: "Off"}, {ID: "on", Name: "On"}}, extras[0].Values)
}

// An option with no values cannot be rendered as a picker, and an option with
// no category is not identifiable on the wire — both are skipped rather than
// producing an empty, unusable row.
func TestExtractExtraConfigOptions_SkipsUnusableOptions(t *testing.T) {
	noValues := extraSelect("mystery", "Mystery", "custom", "x")
	noValues.Select.Options = acp.SessionConfigSelectOptions{}

	noCategory := extraSelect("uncategorised", "Uncategorised", "", "x",
		acp.SessionConfigSelectOption{Value: "x", Name: "X"})

	// Boolean options (advertised only to clients that declare the capability)
	// carry no Select block and are out of scope here.
	boolean := acp.SessionConfigOption{Boolean: &acp.SessionConfigOptionBoolean{
		Id: "toggle", Name: "Toggle", Type: "boolean", CurrentValue: true,
	}}

	extras := extractExtraConfigOptions([]acp.SessionConfigOption{noValues, noCategory, boolean})
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
