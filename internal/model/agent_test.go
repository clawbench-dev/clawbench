package model_test

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

func TestGetDefaultAgentID_Configured(t *testing.T) {
	model.DefaultAgentID = "coder"
	model.Agents = map[string]*model.Agent{"coder": {ID: "coder"}}
	model.AgentList = []*model.Agent{{ID: "assistant"}, {ID: "coder"}}
	t.Cleanup(func() {
		model.DefaultAgentID = ""
		model.Agents = nil
		model.AgentList = nil
	})

	assert.Equal(t, "coder", model.GetDefaultAgentID())
}

func TestGetDefaultAgentID_ConfiguredNotFound(t *testing.T) {
	model.DefaultAgentID = "nonexistent"
	model.Agents = map[string]*model.Agent{"codebuddy": {ID: "codebuddy"}}
	model.AgentList = []*model.Agent{{ID: "codebuddy"}}
	t.Cleanup(func() {
		model.DefaultAgentID = ""
		model.Agents = nil
		model.AgentList = nil
	})

	// Configured agent not found, fallback to first in list
	assert.Equal(t, "codebuddy", model.GetDefaultAgentID())
}

func TestGetDefaultAgentID_FallbackFirst(t *testing.T) {
	model.DefaultAgentID = ""
	model.Agents = map[string]*model.Agent{"coder": {ID: "coder"}}
	model.AgentList = []*model.Agent{{ID: "coder"}}
	t.Cleanup(func() {
		model.Agents = nil
		model.AgentList = nil
	})

	assert.Equal(t, "coder", model.GetDefaultAgentID())
}

func TestGetDefaultAgentID_NoAgents(t *testing.T) {
	model.DefaultAgentID = ""
	model.Agents = nil
	model.AgentList = nil

	assert.Equal(t, "", model.GetDefaultAgentID())
}

// ---------- DefaultModelID ----------

func TestDefaultModelID_PreferredModel(t *testing.T) {
	agent := &model.Agent{
		PreferredModel: "preferred-model",
		Models: []model.AgentModel{
			{ID: "default-model", Default: true},
		},
	}
	assert.Equal(t, "preferred-model", agent.DefaultModelID())
}

func TestDefaultModelID_NoPreferredModel(t *testing.T) {
	agent := &model.Agent{
		Models: []model.AgentModel{
			{ID: "other-model"},
			{ID: "default-model", Default: true},
		},
	}
	assert.Equal(t, "default-model", agent.DefaultModelID())
}

func TestDefaultModelID_NoDefaultFlag(t *testing.T) {
	agent := &model.Agent{
		Models: []model.AgentModel{
			{ID: "first-model"},
			{ID: "second-model"},
		},
	}
	assert.Equal(t, "first-model", agent.DefaultModelID())
}

func TestDefaultModelID_NoModels(t *testing.T) {
	agent := &model.Agent{}
	assert.Equal(t, "", agent.DefaultModelID())
}

// ---------- BaseModelID ----------

func TestBaseModelID_DefaultFlag(t *testing.T) {
	agent := &model.Agent{
		PreferredModel: "preferred", // should be ignored
		Models: []model.AgentModel{
			{ID: "first"},
			{ID: "flagged", Default: true},
		},
	}
	assert.Equal(t, "flagged", agent.BaseModelID())
}

func TestBaseModelID_FirstInList(t *testing.T) {
	agent := &model.Agent{
		PreferredModel: "preferred", // should be ignored
		Models: []model.AgentModel{
			{ID: "first"},
			{ID: "second"},
		},
	}
	assert.Equal(t, "first", agent.BaseModelID())
}

func TestBaseModelID_NoModels(t *testing.T) {
	agent := &model.Agent{PreferredModel: "ignored"}
	assert.Equal(t, "", agent.BaseModelID())
}

// ---------- EffectiveThinkingEffort ----------

func TestEffectiveThinkingEffort_Preferred(t *testing.T) {
	agent := &model.Agent{
		PreferredThinkingEffort: "high",
		ThinkingEffort:          "medium",
	}
	assert.Equal(t, "high", agent.EffectiveThinkingEffort())
}

func TestEffectiveThinkingEffort_NoPreferred(t *testing.T) {
	agent := &model.Agent{
		ThinkingEffort: "medium",
	}
	assert.Equal(t, "medium", agent.EffectiveThinkingEffort())
}

func TestEffectiveThinkingEffort_Neither(t *testing.T) {
	agent := &model.Agent{}
	assert.Equal(t, "", agent.EffectiveThinkingEffort())
}

// ---------- EffectiveModeID ----------

func TestEffectiveModeID_Preferred(t *testing.T) {
	agent := &model.Agent{PreferredMode: "code"}
	assert.Equal(t, "code", agent.EffectiveModeID())
}

func TestEffectiveModeID_Empty(t *testing.T) {
	agent := &model.Agent{}
	assert.Equal(t, "", agent.EffectiveModeID())
}

// opencode addresses a model as "<provider>/<model>", so a provider's bare ids
// must be namespaced before they reach the picker or a turn (issue #651).
func TestDefaultModelID_NamespacesProviderModelForBackend(t *testing.T) {
	pid := int64(88)
	model.ReplaceProviders([]*model.Provider{{
		ID: 88, Name: "GW", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://gw.example/v1",
		APIKey: "k", Models: []string{"glm-5.3-flash"}, DefaultModel: "glm-5.3-flash",
	}})
	t.Cleanup(func() { model.ReplaceProviders(nil) })

	opencodeAgent := &model.Agent{Backend: "opencode", ProviderID: &pid}
	if got := opencodeAgent.DefaultModelID(); got != "clawbench-openai/glm-5.3-flash" {
		t.Fatalf("default = %q, want the id the picker offers", got)
	}

	// A preference stored in bare form (what the provider list holds) must still
	// be recognized, and must come back namespaced — the picker's value.
	opencodeAgent.PreferredModel = "glm-5.3-flash"
	if got := opencodeAgent.DefaultModelID(); got != "clawbench-openai/glm-5.3-flash" {
		t.Fatalf("default = %q, want the picker form of the preference", got)
	}

	// A backend that takes bare ids is untouched.
	claudeAgent := &model.Agent{Backend: "claude", ProviderID: &pid}
	if got := claudeAgent.DefaultModelID(); got != "glm-5.3-flash" {
		t.Fatalf("default = %q, want the bare id", got)
	}
}

// A gateway can namespace its own ids ("openai/gpt-4o" is a real OpenRouter
// style id). The picker then offers the already-namespaced form, so comparing
// only the BARE form would miss the user's own pick and silently fall back to
// the provider default (issue #651 review).
func TestDefaultModelID_MatchesAlreadyNamespacedProviderID(t *testing.T) {
	pid := int64(91)
	model.ReplaceProviders([]*model.Provider{{
		ID: 91, Name: "GW", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://gw.example/v1",
		APIKey: "k", Models: []string{"openai/gpt-4o", "openai/gpt-5"}, DefaultModel: "openai/gpt-5",
	}})
	t.Cleanup(func() { model.ReplaceProviders(nil) })

	// The picker offers the curated id under OUR provider prefix, so a
	// gateway-namespaced id becomes "clawbench-openai/openai/gpt-4o".
	agent := &model.Agent{Backend: "opencode", ProviderID: &pid, PreferredModel: "clawbench-openai/openai/gpt-4o"}
	if got := agent.DefaultModelID(); got != "clawbench-openai/openai/gpt-4o" {
		t.Fatalf("default = %q, want the user's pick (not the provider default)", got)
	}
}

// The "custom…" escape hatch exists for ids the provider's list does not hold.
// For a backend that DECLARES the pin to itself, that pin must be the model the
// turn runs — otherwise the id is declared but never selected, and the session
// silently runs a different model (issue #651 review).
func TestDefaultModelID_HonoursUnlistedPinForDeclaringBackend(t *testing.T) {
	pid := int64(95)
	model.ReplaceProviders([]*model.Provider{{
		ID: 95, Name: "GW", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://gw.example/v1",
		APIKey: "k", Models: []string{"gw-a"}, DefaultModel: "gw-a",
	}})
	t.Cleanup(func() { model.ReplaceProviders(nil) })

	opencodeAgent := &model.Agent{Backend: "opencode", ProviderID: &pid, PreferredModel: "gw-custom"}
	if got := opencodeAgent.DefaultModelID(); got != "clawbench-openai/gw-custom" {
		t.Fatalf("default = %q, want the declared pin", got)
	}

	// An uncurated provider must honor it too: that is the whole point of the
	// escape hatch when the endpoint publishes no list.
	model.ReplaceProviders([]*model.Provider{{
		ID: 95, Name: "GW", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://gw.example/v1",
		APIKey: "k",
	}})
	if got := opencodeAgent.DefaultModelID(); got != "clawbench-openai/gw-custom" {
		t.Fatalf("default = %q, want the declared pin on an uncurated provider", got)
	}

	// A backend that cannot declare anything keeps the old safety: an unlisted
	// pin is stale, so the provider default wins.
	claudeAgent := &model.Agent{Backend: "claude", ProviderID: &pid, PreferredModel: "gw-custom"}
	model.ReplaceProviders([]*model.Provider{{
		ID: 95, Name: "GW", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://gw.example/v1",
		APIKey: "k", Models: []string{"gw-a"}, DefaultModel: "gw-a",
	}})
	if got := claudeAgent.DefaultModelID(); got != "gw-a" {
		t.Fatalf("default = %q, want the provider default for a backend that cannot declare a pin", got)
	}
}

// Binding a provider does not take the backend's OWN models away: the picker
// still lists them, so picking one must run it as-is. Namespacing it sends the
// gateway a model name it never served (400) — and because only the fallback
// path goes through here, the same agent kept working in a 1:1 chat (which
// reuses the stored session model) while its group member failed (issue #651,
// group-chat follow-up).
func TestDefaultModelID_KeepsNativeModelUnnamespaced(t *testing.T) {
	pid := int64(97)
	model.ReplaceProviders([]*model.Provider{{
		ID: 97, Name: "GW", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://gw.example/v1",
		APIKey: "k", Models: []string{"glm-5.3-flash"}, DefaultModel: "glm-5.3-flash",
	}})
	t.Cleanup(func() { model.ReplaceProviders(nil) })

	nativeID := "opencode/deepseek-v4.1-flash-free"
	agent := &model.Agent{
		Backend:        "opencode",
		ProviderID:     &pid,
		PreferredModel: nativeID,
		Models: []model.AgentModel{
			{ID: nativeID, Name: "DeepSeek V4.1 Flash Free"},
			{ID: "opencode/glm-5.3", Name: "GLM"},
		},
	}
	if got := agent.DefaultModelID(); got != nativeID {
		t.Fatalf("default = %q, want the native id unchanged", got)
	}

	// An uncurated provider (the "custom…" escape hatch) behaves the same way.
	model.ReplaceProviders([]*model.Provider{{
		ID: 97, Name: "GW", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://gw.example/v1",
		APIKey: "k",
	}})
	if got := agent.DefaultModelID(); got != nativeID {
		t.Fatalf("default = %q, want the native id on an uncurated provider too", got)
	}

	// The provider's own list still wins when a pin names it, even if a native
	// model happens to share the name — that binding is what the user asked for.
	model.ReplaceProviders([]*model.Provider{{
		ID: 97, Name: "GW", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://gw.example/v1",
		APIKey: "k", Models: []string{"glm-5.3-flash"}, DefaultModel: "glm-5.3-flash",
	}})
	agent.PreferredModel = "glm-5.3-flash"
	agent.Models = append(agent.Models, model.AgentModel{ID: "glm-5.3-flash"})
	if got := agent.DefaultModelID(); got != "clawbench-openai/glm-5.3-flash" {
		t.Fatalf("default = %q, want the provider form for a listed provider model", got)
	}
}

// PreferredModelForProvider is what the bind path stores. It has to agree with
// what DefaultModelID() would run, or the settings page and the turn disagree.
func TestPreferredModelForProvider(t *testing.T) {
	gw := &model.Provider{
		ID: 300, Name: "GW", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://gw.example/v1",
		Models: []string{"gw-a", "gw-b"}, DefaultModel: "gw-b",
	}

	cases := []struct {
		name  string
		agent *model.Agent
		p     *model.Provider
		want  string
	}{
		{
			name:  "stale pin is replaced by the provider default",
			agent: &model.Agent{Backend: "claude", PreferredModel: "old-model"},
			p:     gw, want: "gw-b",
		},
		{
			name:  "served pin survives",
			agent: &model.Agent{Backend: "claude", PreferredModel: "gw-a"},
			p:     gw, want: "gw-a",
		},
		{
			name:  "no pin stays empty",
			agent: &model.Agent{Backend: "claude"},
			p:     gw, want: "",
		},
		{
			// The escape hatch: a backend that declares the id inline keeps a
			// hand-typed pin, so a re-bind must not clobber it.
			name:  "declarable backend keeps an unlisted pin",
			agent: &model.Agent{Backend: "opencode", PreferredModel: "gw-custom"},
			p:     gw, want: "gw-custom",
		},
		{
			// Nothing to validate against: an uncurated provider is exactly the
			// case the escape hatch exists for.
			name:  "uncurated provider leaves the pin alone",
			agent: &model.Agent{Backend: "claude", PreferredModel: "whatever"},
			p:     &model.Provider{ID: 301, Name: "GW2", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://gw.example/v1"},
			want:  "whatever",
		},
		{
			name:  "nil provider leaves the pin alone",
			agent: &model.Agent{Backend: "claude", PreferredModel: "kept"},
			p:     nil, want: "kept",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.agent.PreferredModelForProvider(tc.p); got != tc.want {
				t.Fatalf("PreferredModelForProvider = %q, want %q", got, tc.want)
			}
		})
	}
}

// ── Per-model config-option picks (issue #746) ──────────────────────────────

func TestAgent_ConfigOptionsForModel(t *testing.T) {
	a := &model.Agent{}
	assert.Nil(t, a.ConfigOptionsForModel("m1"), "nothing remembered yields nil")

	a.SetConfigOptionsForModel("m1", map[string]string{"fast-mode": "on"})
	got := a.ConfigOptionsForModel("m1")
	assert.Equal(t, "on", got["fast-mode"])
	assert.Nil(t, a.ConfigOptionsForModel("m2"), "picks are per model")

	// The returned map is a copy: mutating it must not change the agent (readers
	// hold *Agent without a lock, so a shared map would race them).
	got["fast-mode"] = "off"
	assert.Equal(t, "on", a.ConfigOptionsForModel("m1")["fast-mode"])

	// Copy-on-write: the map is replaced, so a previously handed-out value stays
	// frozen for readers that still hold it.
	before := a.PreferredConfigOptions["m1"]
	a.SetConfigOptionsForModel("m1", map[string]string{"fast-mode": "off"})
	assert.Equal(t, "on", before["fast-mode"], "the previous map must not be mutated in place")
	assert.Equal(t, "off", a.ConfigOptionsForModel("m1")["fast-mode"])

	// An empty map removes the model's entry entirely.
	a.SetConfigOptionsForModel("m1", nil)
	assert.Nil(t, a.ConfigOptionsForModel("m1"))
}
