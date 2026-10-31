package service_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"
)

func setupProvidersDB(t *testing.T) {
	t.Helper()
	db, err := service.InitInMemoryDB()
	require.NoError(t, err)
	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
		model.ReplaceProviders(nil)
	})
}

func TestProviderCRUDRoundTrip(t *testing.T) {
	setupProvidersDB(t)

	created, err := service.CreateProvider(model.Provider{
		Name:     "MiniMax",
		Protocol: model.ProviderProtocolAnthropic,
		BaseURL:  "https://api.minimax.io/anthropic",
		APIKey:   "sk-1",
	})
	require.NoError(t, err)
	assert.NotZero(t, created.ID)
	assert.Equal(t, model.ProviderAuthAuto, created.AuthMode, "auth mode must default to auto")

	// The in-memory snapshot the spawn paths read must be updated.
	cached := model.GetProvider(created.ID)
	require.NotNil(t, cached, "provider must be visible to the spawn path after create")
	assert.Equal(t, "sk-1", cached.APIKey)

	list, err := service.ListProviders()
	require.NoError(t, err)
	require.Len(t, list, 1)

	// Empty apiKey on update means "keep the stored secret".
	newURL := "https://api.minimaxi.com/anthropic"
	empty := ""
	updated, err := service.UpdateProvider(created.ID, service.ProviderPatch{BaseURL: &newURL, APIKey: &empty})
	require.NoError(t, err)
	assert.Equal(t, newURL, updated.BaseURL)
	assert.Equal(t, "sk-1", updated.APIKey, "an empty apiKey must not clear the secret")

	// Explicit clear is the only way to remove it.
	updated, err = service.UpdateProvider(created.ID, service.ProviderPatch{ClearAPIKey: true})
	require.NoError(t, err)
	assert.Empty(t, updated.APIKey)

	require.NoError(t, service.DeleteProvider(created.ID))
	assert.Nil(t, model.GetProvider(created.ID))
	_, err = service.GetProvider(created.ID)
	assert.ErrorIs(t, err, service.ErrProviderNotFound)
}

func TestProviderNameIsUnique(t *testing.T) {
	setupProvidersDB(t)

	_, err := service.CreateProvider(model.Provider{Name: "Dup", Protocol: model.ProviderProtocolOpenAI})
	require.NoError(t, err)
	_, err = service.CreateProvider(model.Provider{Name: "dup", Protocol: model.ProviderProtocolOpenAI})
	assert.ErrorIs(t, err, service.ErrProviderNameTaken, "names are unique case-insensitively")
}

func TestProviderRejectsInvalidFields(t *testing.T) {
	setupProvidersDB(t)

	_, err := service.CreateProvider(model.Provider{Name: "X", Protocol: "bogus"})
	assert.ErrorIs(t, err, service.ErrProviderInvalid)

	_, err = service.CreateProvider(model.Provider{Name: "", Protocol: model.ProviderProtocolOpenAI})
	assert.ErrorIs(t, err, service.ErrProviderInvalid)

	_, err = service.CreateProvider(model.Provider{Name: "Y", Protocol: model.ProviderProtocolOpenAI, AuthMode: "bogus"})
	assert.ErrorIs(t, err, service.ErrProviderInvalid)
}

func TestDeleteProviderRefusedWhileReferenced(t *testing.T) {
	setupProvidersDB(t)

	p, err := service.CreateProvider(model.Provider{Name: "InUse", Protocol: model.ProviderProtocolAnthropic})
	require.NoError(t, err)

	require.NoError(t, service.SaveAgent(store.WriteDB(), &model.Agent{
		ID: "a1", Name: "Agent One", Backend: "claude", ProviderID: &p.ID,
	}))

	count, err := service.CountAgentsByProvider(p.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	err = service.DeleteProvider(p.ID)
	assert.ErrorIs(t, err, service.ErrProviderInUse)

	names, err := service.ProviderAgentNames(p.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"Agent One"}, names)

	// Unbinding makes it deletable again.
	require.NoError(t, service.PatchAgentFields("a1", service.AgentPatch{ClearProviderID: true}))
	require.NoError(t, service.DeleteProvider(p.ID))
}

func TestUpdateProviderProtocolRejectsIncompatibleAgents(t *testing.T) {
	setupProvidersDB(t)

	p, err := service.CreateProvider(model.Provider{Name: "Proto", Protocol: model.ProviderProtocolAnthropic})
	require.NoError(t, err)
	require.NoError(t, service.SaveAgent(store.WriteDB(), &model.Agent{
		ID: "claude-agent", Name: "Claude Agent", Backend: "claude", ProviderID: &p.ID,
	}))

	openai := model.ProviderProtocolOpenAI
	_, err = service.UpdateProvider(p.ID, service.ProviderPatch{Protocol: &openai})
	var conflict *service.ProviderProtocolConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, []string{"Claude Agent"}, conflict.AgentNames)

	// A protocol both sides support goes through.
	agentP, err := service.CreateProvider(model.Provider{Name: "Proto2", Protocol: model.ProviderProtocolAnthropic})
	require.NoError(t, err)
	require.NoError(t, service.SaveAgent(store.WriteDB(), &model.Agent{
		ID: "opencode-agent", Name: "OpenCode Agent", Backend: "opencode", ProviderID: &agentP.ID,
	}))
	_, err = service.UpdateProvider(agentP.ID, service.ProviderPatch{Protocol: &openai})
	assert.NoError(t, err)
}

func TestAgentProviderIDSurvivesReload(t *testing.T) {
	setupProvidersDB(t)

	p, err := service.CreateProvider(model.Provider{Name: "Reload", Protocol: model.ProviderProtocolAnthropic})
	require.NoError(t, err)
	require.NoError(t, service.SaveAgent(store.WriteDB(), &model.Agent{
		ID: "reload-agent", Name: "Reload Agent", Backend: "claude", ProviderID: &p.ID,
	}))

	require.NoError(t, service.LoadAgentsIntoMemory())
	loaded := model.GetAgent("reload-agent")
	require.NotNil(t, loaded)
	require.NotNil(t, loaded.ProviderID, "provider_id must survive the live reload path")
	assert.Equal(t, p.ID, *loaded.ProviderID)

	// Clearing writes NULL, not 0 (0 would be a dangling reference).
	require.NoError(t, service.PatchAgentFields("reload-agent", service.AgentPatch{ClearProviderID: true}))
	require.NoError(t, service.LoadAgentsIntoMemory())
	loaded = model.GetAgent("reload-agent")
	require.NotNil(t, loaded)
	assert.Nil(t, loaded.ProviderID)
}

func TestProviderBindingConflictOnlyForSameBackend(t *testing.T) {
	setupProvidersDB(t)

	p1, err := service.CreateProvider(model.Provider{Name: "P1", Protocol: model.ProviderProtocolOpenAI})
	require.NoError(t, err)
	p2, err := service.CreateProvider(model.Provider{Name: "P2", Protocol: model.ProviderProtocolOpenAI})
	require.NoError(t, err)

	require.NoError(t, service.SaveAgent(store.WriteDB(), &model.Agent{ID: "c1", Name: "Codex One", Backend: "codex", ProviderID: &p1.ID}))
	require.NoError(t, service.SaveAgent(store.WriteDB(), &model.Agent{ID: "o1", Name: "OpenCode One", Backend: "opencode", ProviderID: &p1.ID}))

	names, err := service.ProviderBindingConflict("codex", "c2", p2.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"Codex One"}, names, "only same-backend agents conflict")

	names, err = service.ProviderBindingConflict("codex", "c1", p1.ID)
	require.NoError(t, err)
	assert.Empty(t, names, "the agent itself and identical bindings are not conflicts")

	assert.NotEmpty(t, service.ProviderConfigPaths("codex"))
	assert.Empty(t, service.ProviderConfigPaths("claude"), "env backends need no file consent")

	// errors.Is must still work for the sentinel checks used by the handler.
	assert.False(t, errors.Is(service.ErrProviderInUse, service.ErrProviderNotFound))
}

func TestDeleteProviderRefusedWhenSummaryUsesIt(t *testing.T) {
	setupProvidersDB(t)

	p, err := service.CreateProvider(model.Provider{Name: "Summary", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://x", APIKey: "k"})
	require.NoError(t, err)

	pid := p.ID
	model.ConfigInstance.AISummary.ProviderID = &pid
	t.Cleanup(func() { model.ConfigInstance.AISummary.ProviderID = nil })

	err = service.DeleteProvider(p.ID)
	assert.ErrorIs(t, err, service.ErrProviderUsedBySummary)

	// Switching the summary provider away makes it deletable again.
	model.ConfigInstance.AISummary.ProviderID = nil
	require.NoError(t, service.DeleteProvider(p.ID))
}

func TestUpdateProviderRejectsGeminiWhenSummaryUsesIt(t *testing.T) {
	setupProvidersDB(t)

	p, err := service.CreateProvider(model.Provider{Name: "Summary", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://x", APIKey: "k"})
	require.NoError(t, err)
	pid := p.ID
	model.ConfigInstance.AISummary.ProviderID = &pid
	t.Cleanup(func() { model.ConfigInstance.AISummary.ProviderID = nil })

	gemini := model.ProviderProtocolGemini
	_, err = service.UpdateProvider(p.ID, service.ProviderPatch{Protocol: &gemini})
	assert.ErrorIs(t, err, service.ErrProviderUsedBySummary)
}
