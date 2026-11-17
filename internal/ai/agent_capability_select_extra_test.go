package ai

import (
	"testing"

	"clawbench/internal/model"
)

// These tests cover the generalized option accessors added to the capability
// registry: one entry point per category (mode / thought_level / model / any
// other), each backed by the legacy field for the well-known three. They exist
// so a new backend's custom option category needs no new registry method, which
// is exactly where a per-category copy would drift.

// newCategoryRegistry returns a registry with one agent holding the fields under
// test, and no DB attached.
func newCategoryRegistry(t *testing.T, agentID string, caps *AgentCapability) *AgentCapabilityRegistry {
	t.Helper()
	reg := resetGlobalRegistryForTest(t)
	reg.Update(agentID, caps)
	return reg
}

var (
	testModes   = []ModeDef{{ID: "read", Name: "Read"}, {ID: "full", Name: "Full"}}
	testEfforts = []ThinkingEffortDef{{ID: "low", Name: "Low"}, {ID: "high", Name: "High"}}
	testModels  = []model.AgentModel{{ID: "m1", Name: "Model One"}, {ID: "m2", Name: "Model Two"}}
	testCustom  = []SelectOptionDef{{ID: "x", Name: "X"}, {ID: "y", Name: "Y"}}
)

// TestGetAvailableOptions_WellKnownCategories pins the conversion from the legacy
// fields to the generalized option shape, including the nil answer for an agent
// that has reported nothing.
func TestGetAvailableOptions_WellKnownCategories(t *testing.T) {
	const id = "agent-avail"
	reg := newCategoryRegistry(t, id, &AgentCapability{
		AvailableModes:           testModes,
		AvailableThinkingEfforts: testEfforts,
		AvailableModels:          testModels,
		AvailableOptions:         map[string][]SelectOptionDef{"verbosity": testCustom},
	})

	modes := reg.GetAvailableOptions(id, "mode")
	if len(modes) != 2 || modes[0].ID != "read" || modes[1].Name != "Full" {
		t.Fatalf("modes = %+v", modes)
	}
	levels := reg.GetAvailableOptions(id, "thought_level")
	if len(levels) != 2 || levels[1].ID != "high" {
		t.Fatalf("levels = %+v", levels)
	}
	models := reg.GetAvailableOptions(id, "model")
	if len(models) != 2 || models[0].ID != "m1" || models[1].Name != "Model Two" {
		t.Fatalf("models = %+v", models)
	}
	if got := reg.GetAvailableOptions(id, "verbosity"); len(got) != 2 || got[0].ID != "x" {
		t.Fatalf("custom = %+v", got)
	}
	if got := reg.GetAvailableOptions(id, "unknown"); got != nil {
		t.Fatalf("an unreported category must be nil, got %+v", got)
	}

	// An agent with no capability at all answers nil for every category.
	if got := reg.GetAvailableOptions("never-seen", "mode"); got != nil {
		t.Fatalf("unknown agent must be nil, got %+v", got)
	}
}

// TestGetSelectState_WellKnownCategories pins the state shape the pickers read:
// the current id comes from the SESSION, the options from the AGENT.
func TestGetSelectState_WellKnownCategories(t *testing.T) {
	const id = "agent-select"
	reg := newCategoryRegistry(t, id, &AgentCapability{
		AvailableModes:           testModes,
		AvailableThinkingEfforts: testEfforts,
		AvailableModels:          testModels,
		AvailableOptions:         map[string][]SelectOptionDef{"verbosity": testCustom},
	})

	modes := reg.GetSelectState(id, "mode", "full")
	if modes == nil || modes.CurrentID != "full" || modes.Category != "mode" || len(modes.Available) != 2 {
		t.Fatalf("mode state = %+v", modes)
	}
	levels := reg.GetSelectState(id, "thought_level", "high")
	if levels == nil || levels.CurrentID != "high" || levels.Category != "thought_level" || len(levels.Available) != 2 {
		t.Fatalf("thought_level state = %+v", levels)
	}
	models := reg.GetSelectState(id, "model", "m2")
	if models == nil || models.CurrentID != "m2" || models.Category != "model" {
		t.Fatalf("model state = %+v", models)
	}
	if len(models.Available) != 2 || models.Available[0].ID != "m1" || models.Available[1].Name != "Model Two" {
		t.Fatalf("model options = %+v", models.Available)
	}
	custom := reg.GetSelectState(id, "verbosity", "y")
	if custom == nil || custom.CurrentID != "y" || custom.Category != "verbosity" || len(custom.Available) != 2 {
		t.Fatalf("custom state = %+v", custom)
	}
	if got := reg.GetSelectState(id, "unreported", ""); got != nil {
		t.Fatalf("an empty custom category must be nil, got %+v", got)
	}
	if got := reg.GetSelectState("never-seen", "mode", ""); got != nil {
		t.Fatalf("unknown agent must be nil, got %+v", got)
	}
}

// TestUpdateAvailableOptions_DelegatesToTheLegacyFields pins that writing through
// the generalized setter lands in the SAME field the legacy readers use: two
// tables describing one thing is the drift this indirection must not introduce.
func TestUpdateAvailableOptions_DelegatesToTheLegacyFields(t *testing.T) {
	const id = "agent-update"

	t.Run("mode", func(t *testing.T) {
		reg := newCategoryRegistry(t, id, &AgentCapability{})
		reg.UpdateAvailableOptions(id, "mode", testCustom)
		caps := reg.Get(id)
		if len(caps.AvailableModes) != 2 || caps.AvailableModes[0].ID != "x" {
			t.Fatalf("modes = %+v", caps.AvailableModes)
		}
		if !reg.IsModeAvailable(id, "y") {
			t.Fatal("the mode must be readable through the legacy validator too")
		}
	})

	t.Run("thought_level", func(t *testing.T) {
		reg := newCategoryRegistry(t, id, &AgentCapability{})
		reg.UpdateAvailableOptions(id, "thought_level", testCustom)
		if caps := reg.Get(id); len(caps.AvailableThinkingEfforts) != 2 || caps.AvailableThinkingEfforts[1].ID != "y" {
			t.Fatalf("levels = %+v", caps.AvailableThinkingEfforts)
		}
	})

	t.Run("model", func(t *testing.T) {
		reg := newCategoryRegistry(t, id, &AgentCapability{})
		reg.UpdateAvailableOptions(id, "model", testCustom)
		if caps := reg.Get(id); len(caps.AvailableModels) != 2 || caps.AvailableModels[0].ID != "x" {
			t.Fatalf("models = %+v", caps.AvailableModels)
		}
		if !reg.IsModelAvailable(id, "y") {
			t.Fatal("the model must be readable through the legacy validator too")
		}
	})

	t.Run("custom", func(t *testing.T) {
		reg := newCategoryRegistry(t, id, &AgentCapability{})
		reg.UpdateAvailableOptions(id, "verbosity", testCustom)
		if got := reg.GetAvailableOptions(id, "verbosity"); len(got) != 2 || got[1].ID != "y" {
			t.Fatalf("custom = %+v", got)
		}
	})
}

// TestHasNewAvailableOptions pins the change detection per category, which is
// what tells the chat drawer to re-render a picker.
func TestHasNewAvailableOptions(t *testing.T) {
	const id = "agent-new"
	reg := newCategoryRegistry(t, id, &AgentCapability{
		AvailableModes:           []ModeDef{{ID: "read"}},
		AvailableThinkingEfforts: []ThinkingEffortDef{{ID: "low"}},
		AvailableModels:          []model.AgentModel{{ID: "m1"}},
		AvailableOptions:         map[string][]SelectOptionDef{"verbosity": {{ID: "x"}}},
	})

	// One case per category, each with the ids that category actually holds —
	// comparing a set against a DIFFERENT category's ids would report every
	// category as changed and prove nothing.
	cases := []struct {
		category string
		same     []SelectOptionDef
		novel    []SelectOptionDef
	}{
		{"mode", []SelectOptionDef{{ID: "read"}}, []SelectOptionDef{{ID: "read"}, {ID: "write"}}},
		{"thought_level", []SelectOptionDef{{ID: "low"}}, []SelectOptionDef{{ID: "low"}, {ID: "high"}}},
		{"model", []SelectOptionDef{{ID: "m1"}}, []SelectOptionDef{{ID: "m1"}, {ID: "m2"}}},
		{"verbosity", []SelectOptionDef{{ID: "x"}}, []SelectOptionDef{{ID: "x"}, {ID: "z"}}},
	}
	for _, c := range cases {
		if reg.HasNewAvailableOptions(id, c.category, c.same) {
			t.Errorf("%s: an identical set must not report a change", c.category)
		}
		if !reg.HasNewAvailableOptions(id, c.category, c.novel) {
			t.Errorf("%s: a new id must be reported", c.category)
		}
	}
	same := cases[0].same

	// An agent with no options yet: anything non-empty is new.
	if reg.HasNewAvailableOptions("never-seen", "verbosity", same) != true {
		t.Fatal("an unknown agent with incoming options must report a change")
	}
	if reg.HasNewAvailableOptions("never-seen", "verbosity", nil) {
		t.Fatal("an unknown agent with no incoming options has nothing new")
	}
}

// TestIsOptionAvailable pins the validator for every category, including the
// custom one and the unknown-agent answer.
func TestIsOptionAvailable(t *testing.T) {
	const id = "agent-valid"
	reg := newCategoryRegistry(t, id, &AgentCapability{
		AvailableModes:           []ModeDef{{ID: "read"}},
		AvailableThinkingEfforts: []ThinkingEffortDef{{ID: "low"}},
		AvailableModels:          []model.AgentModel{{ID: "m1"}},
		AvailableOptions:         map[string][]SelectOptionDef{"verbosity": {{ID: "x"}}},
	})

	yes := [][3]string{
		{"mode", "read"},
		{"thought_level", "low"},
		{"model", "m1"},
		{"verbosity", "x"},
	}
	for _, c := range yes {
		if !reg.IsOptionAvailable(id, c[0], c[1]) {
			t.Errorf("%s/%s must be available", c[0], c[1])
		}
	}
	no := [][3]string{
		{"mode", "write"},
		{"thought_level", "high"},
		{"model", "m2"},
		{"verbosity", "y"},
	}
	for _, c := range no {
		if reg.IsOptionAvailable(id, c[0], c[1]) {
			t.Errorf("%s/%s must not be available", c[0], c[1])
		}
	}
	for _, category := range []string{"mode", "thought_level", "model", "verbosity"} {
		if reg.IsOptionAvailable("never-seen", category, "anything") {
			t.Errorf("%s: an unknown agent must have nothing available", category)
		}
	}
}

// TestThinkingEffortsForModel_ScopedPerModel pins the per-model table that fixed
// issue #637: one model's levels must not appear under another's id, an empty
// list carries no information, and the returned slice is a copy so a caller
// cannot mutate the registry.
func TestThinkingEffortsForModel_ScopedPerModel(t *testing.T) {
	const id = "agent-by-model"
	reg := newCategoryRegistry(t, id, &AgentCapability{})

	reg.UpdateThinkingEffortsForModel(id, "glm", []ThinkingEffortDef{{ID: "high"}})
	reg.UpdateThinkingEffortsForModel(id, "deepseek", []ThinkingEffortDef{{ID: "max"}})
	// An empty list must be ignored rather than clearing the recorded one.
	reg.UpdateThinkingEffortsForModel(id, "glm", nil)

	glm, ok := reg.ThinkingEffortsForModel(id, "glm")
	if !ok || len(glm) != 1 || glm[0].ID != "high" {
		t.Fatalf("glm levels = %+v ok=%v", glm, ok)
	}
	if _, ok := reg.ThinkingEffortsForModel(id, "never-ran"); ok {
		t.Fatal("an unlearned model must report no list, not another model's")
	}
	if _, ok := reg.ThinkingEffortsForModel("never-seen", "glm"); ok {
		t.Fatal("an unknown agent must report no list")
	}

	// The copy must not be the registry's own slice.
	glm[0].ID = "mutated"
	again, _ := reg.ThinkingEffortsForModel(id, "glm")
	if again[0].ID != "high" {
		t.Fatal("the caller mutated the stored list")
	}
}

// TestGetThinkingEffortStateForModel_FallsBackToTheStaticListOnly pins the scope
// rule: a backend with a fixed list answers for any model, but a runtime agent
// with no entry for this model answers nil rather than showing another model's
// levels.
func TestGetThinkingEffortStateForModel_FallsBackToTheStaticListOnly(t *testing.T) {
	const dynamicID = "agent-effort-dynamic"
	reg := newCategoryRegistry(t, dynamicID, &AgentCapability{
		AvailableThinkingEfforts: []ThinkingEffortDef{{ID: "agent-wide"}},
	})
	if got := reg.GetThinkingEffortStateForModel(dynamicID, "unknown-model", "x"); got != nil {
		t.Fatalf("no per-model entry and no static list must be nil, got %+v", got)
	}

	// The static list answers for any model, which is what makes the built-in
	// agent's picker appear before it has run anything.
	const staticBackend = "static-backend-test"
	const staticAgent = "agent-effort-static"
	RegisterStaticCapabilities(staticBackend, &AgentCapability{
		AvailableThinkingEfforts: []ThinkingEffortDef{{ID: "auto"}, {ID: "high"}},
	})
	t.Cleanup(func() { staticCapabilities.Delete(staticBackend) })
	if model.Agents == nil {
		model.Agents = map[string]*model.Agent{}
	}
	model.Agents[staticAgent] = &model.Agent{ID: staticAgent, Backend: staticBackend}
	t.Cleanup(func() { delete(model.Agents, staticAgent) })

	got := reg.GetThinkingEffortStateForModel(staticAgent, "any-model", "high")
	if got == nil || len(got.AvailableLevels) != 2 || got.AvailableLevels[1].ID != "high" {
		t.Fatalf("static levels not used: %+v", got)
	}
	if got.CurrentID != "high" {
		t.Fatalf("the session's current id must be carried, got %q", got.CurrentID)
	}
}
