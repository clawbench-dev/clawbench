package service_test

import (
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpdateProvider_EmptyingModelsIsCheckedAgainstTheInMemorySnapshot pins the
// union rule of the config-dir guard: the DATABASE is the durable truth, but the
// spawn path resolves providers against the in-memory agent snapshot, and the two
// can disagree for a moment after a rebind. Missing an agent here means letting
// the edit through and breaking that agent's next turn — so the check must look
// at both, and must only fire for backends that read the declared list.
func TestUpdateProvider_EmptyingModelsIsCheckedAgainstTheInMemorySnapshot(t *testing.T) {
	setupProvidersDB(t)
	model.ReplaceAgents(map[string]*model.Agent{}, nil)
	t.Cleanup(func() { model.ReplaceAgents(map[string]*model.Agent{}, nil) })

	created, err := service.CreateProvider(model.Provider{
		Name: "SnapshotOnly", Protocol: model.ProviderProtocolOpenAI,
		BaseURL: "https://api.example.com/v1", APIKey: "k", Models: []string{"m1"},
	})
	require.NoError(t, err)

	// The agent exists ONLY in the snapshot (no rows in the database).
	configDirAgent := &model.Agent{ID: "pi-live", Name: "Pi Live", Backend: "pi", ProviderID: &created.ID}
	envAgent := &model.Agent{ID: "zcode-live", Name: "Zcode Live", Backend: "zcode", ProviderID: &created.ID}
	model.ReplaceAgents(
		map[string]*model.Agent{configDirAgent.ID: configDirAgent, envAgent.ID: envAgent},
		[]*model.Agent{configDirAgent, envAgent},
	)

	empty := []string{}
	_, err = service.UpdateProvider(created.ID, service.ProviderPatch{Models: &empty})
	require.Error(t, err)
	var conflict *service.ProviderModelsConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, []string{"Pi Live"}, conflict.AgentNames,
		"only the config-directory agent in the in-memory snapshot is reported")
	assert.Contains(t, conflict.Error(), "Pi Live",
		"the message must name the agent, or the user cannot find what to rebind")

	// An unrelated edit still goes through: the guard reads the model list, not
	// "any patch".
	name := "Renamed"
	updated, err := service.UpdateProvider(created.ID, service.ProviderPatch{Name: &name})
	require.NoError(t, err)
	assert.Equal(t, "Renamed", updated.Name)
	assert.Equal(t, []string{"m1"}, updated.Models)

	// A row that was already empty is not blocked: there is no binding that could
	// break, and those agents are caught when they rebind.
	_, err = service.UpdateProvider(created.ID, service.ProviderPatch{Models: &empty})
	require.Error(t, err, "the first empty edit is refused while the agent is bound")

	// Once the agent is gone the same edit succeeds and persists.
	model.ReplaceAgents(map[string]*model.Agent{}, nil)
	_, err = service.UpdateProvider(created.ID, service.ProviderPatch{Models: &empty})
	require.NoError(t, err)
	stored, err := service.GetProvider(created.ID)
	require.NoError(t, err)
	assert.Empty(t, stored.Models)
}
