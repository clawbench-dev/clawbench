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
