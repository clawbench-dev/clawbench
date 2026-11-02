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

func TestUpdateProviderRejectsEmptyBaseURLWhenSummaryUsesIt(t *testing.T) {
	setupProvidersDB(t)

	p, err := service.CreateProvider(model.Provider{Name: "Summary", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://x", APIKey: "k"})
	require.NoError(t, err)
	pid := p.ID
	model.ConfigInstance.AISummary.ProviderID = &pid
	t.Cleanup(func() { model.ConfigInstance.AISummary.ProviderID = nil })

	// The summary PATCH refuses to bind a provider without an endpoint; this is
	// the other half — clearing it later must not strand the existing binding.
	empty := ""
	_, err = service.UpdateProvider(p.ID, service.ProviderPatch{BaseURL: &empty})
	assert.ErrorIs(t, err, service.ErrProviderUsedBySummary)

	// Unrelated edits still go through.
	name := "Summary 2"
	_, err = service.UpdateProvider(p.ID, service.ProviderPatch{Name: &name})
	require.NoError(t, err)

	// Once the summary model no longer uses it, the endpoint can be cleared.
	model.ConfigInstance.AISummary.ProviderID = nil
	_, err = service.UpdateProvider(p.ID, service.ProviderPatch{BaseURL: &empty})
	require.NoError(t, err)
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

func TestProviderWireAPIPersistsAndDefaults(t *testing.T) {
	setupProvidersDB(t)

	// Unset means the Responses API, which is what codex always got before the
	// field existed — an old row must not change behavior.
	def, err := service.CreateProvider(model.Provider{Name: "Default", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://x", APIKey: "k"})
	require.NoError(t, err)
	assert.Equal(t, model.ProviderWireResponses, def.WireAPI)

	chat, err := service.CreateProvider(model.Provider{
		Name: "ChatOnly", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://y", APIKey: "k",
		WireAPI: model.ProviderWireChat,
	})
	require.NoError(t, err)
	assert.Equal(t, model.ProviderWireChat, chat.WireAPI)

	read, err := service.GetProvider(chat.ID)
	require.NoError(t, err)
	assert.Equal(t, model.ProviderWireChat, read.WireAPI, "the column must survive a round trip")

	// The in-memory snapshot is what the codex block is rendered from.
	assert.Equal(t, model.ProviderWireChat, model.GetProvider(chat.ID).WireAPI)

	responses := model.ProviderWireResponses
	updated, err := service.UpdateProvider(chat.ID, service.ProviderPatch{WireAPI: &responses})
	require.NoError(t, err)
	assert.Equal(t, model.ProviderWireResponses, updated.WireAPI)

	// Fail fast on a value codex would not understand.
	bogus := "grpc"
	_, err = service.UpdateProvider(chat.ID, service.ProviderPatch{WireAPI: &bogus})
	assert.ErrorIs(t, err, service.ErrProviderInvalid)
}

func TestProviderModelsPersistAndNormalize(t *testing.T) {
	setupProvidersDB(t)

	created, err := service.CreateProvider(model.Provider{
		Name: "GW", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://gw.example/v1", APIKey: "k",
		Models: []string{" m1 ", "m2", "m1", ""},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"m1", "m2"}, created.Models)
	// An unset default falls back to the first curated model.
	assert.Equal(t, "m1", created.DefaultModel)

	read, err := service.GetProvider(created.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"m1", "m2"}, read.Models)
	assert.Equal(t, "m1", read.DefaultModel)

	models := []string{"m3"}
	def := "m3"
	updated, err := service.UpdateProvider(created.ID, service.ProviderPatch{Models: &models, DefaultModel: &def})
	require.NoError(t, err)
	assert.Equal(t, []string{"m3"}, updated.Models)
	assert.Equal(t, "m3", updated.DefaultModel)

	// A default outside the list is corrected rather than stored.
	def = "nope"
	updated, err = service.UpdateProvider(created.ID, service.ProviderPatch{DefaultModel: &def})
	require.NoError(t, err)
	assert.Equal(t, "m3", updated.DefaultModel)
}

// An auth mode no backend can express is a dead row: it saved fine, the test
// button went green, and every turn then failed with "does not support Bearer
// auth". Rejecting it at write time is the only place the user can still see
// which field is wrong.
func TestProviderRejectsDeadAuthMode(t *testing.T) {
	setupProvidersDB(t)

	for _, tc := range []struct {
		name     string
		protocol string
		auth     string
	}{
		{name: "openai+bearer", protocol: model.ProviderProtocolOpenAI, auth: model.ProviderAuthBearer},
		{name: "gemini+bearer", protocol: model.ProviderProtocolGemini, auth: model.ProviderAuthBearer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.CreateProvider(model.Provider{
				Name: tc.name, Protocol: tc.protocol, BaseURL: "https://gw.example", APIKey: "k", AuthMode: tc.auth,
			})
			require.ErrorIs(t, err, service.ErrProviderInvalid)
			assert.Contains(t, err.Error(), tc.auth, "the error must name the auth mode")
			assert.Contains(t, err.Error(), tc.protocol, "the error must name the protocol")
		})
	}

	// The two combinations that must keep working: anthropic gateways that only
	// accept a Bearer token, and the x-api-key default everywhere else.
	anthropicBearer, err := service.CreateProvider(model.Provider{
		Name: "AnthropicBearer", Protocol: model.ProviderProtocolAnthropic, BaseURL: "https://gw.example", APIKey: "k",
		AuthMode: model.ProviderAuthBearer,
	})
	require.NoError(t, err, "anthropic + bearer is a real gateway mode")
	assert.Equal(t, model.ProviderAuthBearer, anthropicBearer.AuthMode)

	openaiKey, err := service.CreateProvider(model.Provider{
		Name: "OpenAIKey", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://gw.example", APIKey: "k",
		AuthMode: model.ProviderAuthAPIKey,
	})
	require.NoError(t, err)
	assert.Equal(t, model.ProviderAuthAPIKey, openaiKey.AuthMode)

	// Editing into a dead combination is refused the same way.
	bearer := model.ProviderAuthBearer
	_, err = service.UpdateProvider(openaiKey.ID, service.ProviderPatch{AuthMode: &bearer})
	assert.ErrorIs(t, err, service.ErrProviderInvalid)

	// ...and the row is untouched by the rejected update.
	stored, err := service.GetProvider(openaiKey.ID)
	require.NoError(t, err)
	assert.Equal(t, model.ProviderAuthAPIKey, stored.AuthMode)
}

// The settings form branches on detail.field to replace the raw reason with its
// own wording, so the service has to name the field it rejected — a bare
// "invalid provider" can only be shown verbatim.
func TestProviderValidationErrorNamesField(t *testing.T) {
	setupProvidersDB(t)

	cases := []struct {
		name  string
		p     model.Provider
		field string
	}{
		{
			name:  "dead auth mode",
			p:     model.Provider{Name: "A", Protocol: model.ProviderProtocolOpenAI, AuthMode: model.ProviderAuthBearer},
			field: "authMode",
		},
		{
			name:  "unknown auth mode",
			p:     model.Provider{Name: "B", Protocol: model.ProviderProtocolOpenAI, AuthMode: "bogus"},
			field: "authMode",
		},
		{
			name:  "unknown protocol",
			p:     model.Provider{Name: "C", Protocol: "bogus"},
			field: "protocol",
		},
		{
			name:  "missing name",
			p:     model.Provider{Protocol: model.ProviderProtocolOpenAI},
			field: "name",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.CreateProvider(tc.p)
			require.ErrorIs(t, err, service.ErrProviderInvalid, "the sentinel must keep working")
			var fieldErr *service.ProviderFieldError
			require.ErrorAs(t, err, &fieldErr)
			assert.Equal(t, tc.field, fieldErr.Field)
			assert.NotEmpty(t, fieldErr.Reason)
			// The message keeps the historical shape so logs stay readable.
			assert.Contains(t, err.Error(), "invalid provider:")
		})
	}
}
