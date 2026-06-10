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
