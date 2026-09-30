package model_test

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

// resetAgentGlobals restores the package-level agent state after a test that
// mutates it through the locked accessors. Every accessor-based test below
// funnels through here so a failure cannot leak state into the next test.
func resetAgentGlobals(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		model.ReplaceAgents(nil, nil)
		model.SetDefaultAgentID("")
	})
}

// The accessors added for the async model discovery (ReplaceAgents and friends)
// are the only supported way to mutate the shared agent set once the server is
// live. These tests pin their contract directly: the mutation tests below cover
// the lines that the concurrent race test cannot (it only reads through them).

func TestSetDefaultAgentID_OverridesFallback(t *testing.T) {
	resetAgentGlobals(t)
	model.ReplaceAgents(
		map[string]*model.Agent{"a": {ID: "a"}, "b": {ID: "b"}},
		[]*model.Agent{{ID: "a"}, {ID: "b"}},
	)

	model.SetDefaultAgentID("b")
	assert.Equal(t, "b", model.GetDefaultAgentID(), "configured id must win over the first list entry")
}

func TestSetDefaultAgentID_UnknownFallsBackToFirst(t *testing.T) {
	resetAgentGlobals(t)
	model.ReplaceAgents(
		map[string]*model.Agent{"a": {ID: "a"}},
		[]*model.Agent{{ID: "a"}},
	)

	model.SetDefaultAgentID("ghost")
	assert.Equal(t, "a", model.GetDefaultAgentID(), "an id absent from the map must fall back to the first agent")
}

func TestAddAgent_AddsToMapAndList(t *testing.T) {
	resetAgentGlobals(t)
	model.ReplaceAgents(map[string]*model.Agent{}, nil)

	model.AddAgent(&model.Agent{ID: "new", Backend: "claude"})

	assert.True(t, model.HasAgent("new"))
	assert.Equal(t, "new", model.GetAgent("new").ID)
	list := model.GetAgentList()
	assert.Len(t, list, 1)
	assert.Equal(t, "new", list[0].ID)
}

func TestDeleteAgent_RemovesFromMapAndList(t *testing.T) {
	resetAgentGlobals(t)
	model.ReplaceAgents(
		map[string]*model.Agent{"a": {ID: "a"}, "b": {ID: "b"}},
		[]*model.Agent{{ID: "a"}, {ID: "b"}},
	)

	model.DeleteAgent("a")

	assert.False(t, model.HasAgent("a"), "deleted agent must leave the map")
	assert.True(t, model.HasAgent("b"), "sibling must survive")
	list := model.GetAgentList()
	assert.Len(t, list, 1)
	assert.Equal(t, "b", list[0].ID)
}

func TestDeleteAgent_UnknownIsNoop(t *testing.T) {
	resetAgentGlobals(t)
	model.ReplaceAgents(
		map[string]*model.Agent{"a": {ID: "a"}},
		[]*model.Agent{{ID: "a"}},
	)

	model.DeleteAgent("ghost")

	assert.True(t, model.HasAgent("a"))
	assert.Len(t, model.GetAgentList(), 1)
}

func TestSetAgentList_ReplacesOrderOnly(t *testing.T) {
	resetAgentGlobals(t)
	model.ReplaceAgents(
		map[string]*model.Agent{"a": {ID: "a"}, "b": {ID: "b"}},
		[]*model.Agent{{ID: "a"}, {ID: "b"}},
	)

	model.SetAgentList([]*model.Agent{{ID: "b"}, {ID: "a"}})

	list := model.GetAgentList()
	assert.Equal(t, []string{"b", "a"}, []string{list[0].ID, list[1].ID}, "list order must follow the replacement")
	assert.True(t, model.HasAgent("a"), "SetAgentList must not touch the map")
}

// UpdateAgent is the copy-on-write editor. The handler used to assign fields on
// the pointer returned by GetAgent(), which races the accessor readers (they
// hold that pointer and read its fields without the lock).
func TestUpdateAgent_AppliesToExistingAgent(t *testing.T) {
	resetAgentGlobals(t)
	a := &model.Agent{ID: "a", Name: "old"}
	model.ReplaceAgents(map[string]*model.Agent{"a": a}, []*model.Agent{a})

	updated := model.UpdateAgent("a", func(agent *model.Agent) {
		agent.Name = "new"
		agent.PreferredModel = "m1"
	})

	assert.NotNil(t, updated, "an existing id must return the published copy")
	assert.Equal(t, "new", updated.Name)
	got := model.GetAgent("a")
	assert.Equal(t, "new", got.Name, "the mutation must be visible through the accessor")
	assert.Equal(t, "m1", got.PreferredModel)
}

func TestUpdateAgent_CopiesRatherThanMutatingThePublishedPointer(t *testing.T) {
	resetAgentGlobals(t)
	original := &model.Agent{ID: "a", Name: "old"}
	model.ReplaceAgents(map[string]*model.Agent{"a": original}, []*model.Agent{original})

	// A reader that captured the pointer must keep seeing the frozen snapshot:
	// that is what makes the unlocked field reads safe.
	model.UpdateAgent("a", func(agent *model.Agent) { agent.Name = "new" })

	assert.Equal(t, "old", original.Name, "the previously published object must not be mutated")
	assert.Equal(t, "new", model.GetAgent("a").Name)
}

func TestUpdateAgent_ReplacesTheListEntryToo(t *testing.T) {
	resetAgentGlobals(t)
	original := &model.Agent{ID: "a", Name: "old"}
	model.ReplaceAgents(map[string]*model.Agent{"a": original}, []*model.Agent{original})

	model.UpdateAgent("a", func(agent *model.Agent) { agent.Name = "new" })

	list := model.GetAgentList()
	assert.Len(t, list, 1)
	assert.Equal(t, "new", list[0].Name, "the ordered list must point at the copy")
}

func TestUpdateAgent_UnknownIDIsNoop(t *testing.T) {
	resetAgentGlobals(t)
	model.ReplaceAgents(map[string]*model.Agent{}, nil)

	called := false
	updated := model.UpdateAgent("missing", func(*model.Agent) { called = true })

	assert.Nil(t, updated, "an unknown id must return nil")
	assert.False(t, called, "fn must not run when the agent does not exist")
}

func TestUpdateAgent_EmptyIDIsNoop(t *testing.T) {
	resetAgentGlobals(t)
	model.ReplaceAgents(map[string]*model.Agent{"": {ID: ""}}, []*model.Agent{{ID: ""}})

	called := false
	updated := model.UpdateAgent("", func(*model.Agent) { called = true })

	assert.Nil(t, updated)
	assert.False(t, called, "an empty id must not resolve")
}
