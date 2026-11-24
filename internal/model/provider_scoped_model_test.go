package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A provider-scoped id is how the picker names a model that belongs to one of
// several providers. It has to survive a round trip exactly, or the turn would
// ask the wrong endpoint for a model it never served.
func TestProviderScopedModelID_RoundTrip(t *testing.T) {
	id := ProviderScopedModelID(42, "gpt-4o")
	assert.Equal(t, "p42:gpt-4o", id)

	pid, bare, ok := ParseProviderScopedModelID(id)
	require.True(t, ok)
	assert.Equal(t, int64(42), pid)
	assert.Equal(t, "gpt-4o", bare)

	assert.Equal(t, "gpt-4o", ProviderModelName(id), "display form drops the scope")
}

// A plain id is what every session created before this feature holds, and what
// a single-provider backend keeps using. It must not be mistaken for a scoped
// one — that would send the model name "gpt-4o" to a provider named "p0".
func TestParseProviderScopedModelID_PlainIDsAreNotScoped(t *testing.T) {
	for _, id := range []string{
		"",
		"gpt-4o",
		"claude-sonnet-4-5",
		"p:model",    // no number
		"p0:model",   // provider ids start at 1
		"p12:",       // no model
		"px12:model", // not a number
		"12:model",   // no prefix
		"prefix-model",
	} {
		pid, bare, ok := ParseProviderScopedModelID(id)
		assert.False(t, ok, "%q must not parse as scoped", id)
		assert.Equal(t, int64(0), pid)
		assert.Equal(t, id, bare, "an unscoped id is returned unchanged")
	}
}

// The picker offers every provider this backend can actually call. A protocol
// the backend has no slot for, and a credential issued for another backend's
// own service, must both stay out — offering them would hand the user a choice
// that fails on the first turn.
func TestSelectableProviderModels_ListsOnlyUsableProviders(t *testing.T) {
	registerTestSpec(t, "test-inprocess", ProviderEnvSpec{
		Anthropic: ProtoEnv("", "", ""),
		OpenAI:    ProtoEnv("", "", ""),
		InProcess: true,
	})

	ReplaceProviders([]*Provider{
		{
			ID: 1, Name: "MiniMax", Protocol: ProviderProtocolOpenAI,
			BaseURL: "https://api.minimax.example", APIKey: "k1",
			Models: []string{"MiniMax-M2", "MiniMax-Text-01"}, DefaultModel: "MiniMax-M2",
		},
		{
			ID: 2, Name: "Claude 代理", Protocol: ProviderProtocolAnthropic,
			BaseURL: "https://anthropic.example", APIKey: "k2",
			Models: []string{"claude-sonnet-4-5"}, DefaultModel: "claude-sonnet-4-5",
		},
		{
			// No Gemini slot on this backend.
			ID: 3, Name: "Google", Protocol: ProviderProtocolGemini,
			BaseURL: "https://gemini.example", APIKey: "k3",
			Models: []string{"gemini-2.5-pro"},
		},
		{
			// An own-service credential: only the backend it names may use it.
			ID: 4, Name: "Cursor 官方", Protocol: ProviderProtocolPrivate,
			Backend: "cursor", APIKey: "k4",
			Models: []string{"cursor-fast"},
		},
		{
			// Curated by nobody — the picker cannot name its models.
			ID: 5, Name: "空", Protocol: ProviderProtocolOpenAI,
			BaseURL: "https://empty.example", APIKey: "k5",
		},
	})
	t.Cleanup(func() { ReplaceProviders(nil) })

	agent := &Agent{ID: "builtin-test", Backend: "test-inprocess", BuiltinBackend: true}
	got := agent.SelectableProviderModels()

	require.Len(t, got, 3, "only the two usable providers' curated models")

	// ListProviders sorts by name, so assert by content rather than position.
	byID := map[string]AgentModel{}
	for _, m := range got {
		byID[m.ID] = m
	}
	mm := byID[ProviderScopedModelID(1, "MiniMax-M2")]
	assert.Equal(t, "MiniMax-M2", mm.Name)
	assert.Equal(t, "MiniMax", mm.Provider)
	assert.Equal(t, int64(1), mm.ProviderID)
	assert.True(t, mm.Default, "the provider's own default is flagged")
	assert.False(t, byID[ProviderScopedModelID(1, "MiniMax-Text-01")].Default)

	cl := byID[ProviderScopedModelID(2, "claude-sonnet-4-5")]
	assert.Equal(t, "Claude 代理", cl.Provider)

	for _, m := range got {
		assert.NotEqual(t, int64(3), m.ProviderID, "gemini is not reachable from this backend")
		assert.NotEqual(t, int64(4), m.ProviderID, "another backend's own-service token must not leak in")
	}
}

// An external CLI is spawned with ONE endpoint, so a second provider's models
// would be a choice that cannot take effect. Those backends keep the bound
// provider's list only.
func TestSelectableProviderModels_ExternalBackendGetsNothing(t *testing.T) {
	registerTestSpec(t, "test-external", ProviderEnvSpec{
		Anthropic: ProtoEnv("ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY", ""),
	})
	ReplaceProviders([]*Provider{{
		ID: 9, Name: "GW", Protocol: ProviderProtocolAnthropic,
		BaseURL: "https://gw.example", APIKey: "k",
		Models: []string{"m1"},
	}})
	t.Cleanup(func() { ReplaceProviders(nil) })

	agent := &Agent{ID: "cli-test", Backend: "test-external"}
	assert.Empty(t, agent.SelectableProviderModels())
}

// The scoped pin is the user's pick, so it must survive a rebind: the default
// resolver compares it against the provider's bare list and would otherwise
// drop it and fall back to that provider's own default.
func TestDefaultModelID_KeepsAProviderScopedPin(t *testing.T) {
	ReplaceProviders([]*Provider{
		{
			ID: 1, Name: "Bound", Protocol: ProviderProtocolOpenAI,
			BaseURL: "https://bound.example", APIKey: "k",
			Models: []string{"bound-default", "shared-name"}, DefaultModel: "bound-default",
		},
		{
			ID: 2, Name: "Other", Protocol: ProviderProtocolOpenAI,
			BaseURL: "https://other.example", APIKey: "k",
			Models: []string{"shared-name"}, DefaultModel: "shared-name",
		},
	})
	t.Cleanup(func() { ReplaceProviders(nil) })

	bound := int64(1)
	agent := &Agent{
		Backend:        "builtin",
		ProviderID:     &bound,
		PreferredModel: ProviderScopedModelID(2, "shared-name"),
	}
	assert.Equal(t, ProviderScopedModelID(2, "shared-name"), agent.DefaultModelID(),
		"the pick names provider 2, not the bound one")
}

// Rebinding must not discard a scoped pin. For the built-in agent the pin — not
// the binding — decides which endpoint a turn uses, so rewriting it to the new
// provider's default would silently undo the user's choice.
func TestPreferredModelForProvider_KeepsALiveScopedPin(t *testing.T) {
	ReplaceProviders([]*Provider{
		{ID: 1, Name: "Bound", Protocol: ProviderProtocolOpenAI, Models: []string{"bound-default"}, DefaultModel: "bound-default"},
		{ID: 2, Name: "Other", Protocol: ProviderProtocolOpenAI, Models: []string{"shared-name"}, DefaultModel: "shared-name"},
	})
	t.Cleanup(func() { ReplaceProviders(nil) })

	agent := &Agent{Backend: "builtin", PreferredModel: ProviderScopedModelID(2, "shared-name")}
	// Rebinding to provider 1 must leave the pin on provider 2 alone.
	assert.Equal(t, ProviderScopedModelID(2, "shared-name"),
		agent.PreferredModelForProvider(GetProvider(1)))
}

// A scoped pin whose provider was deleted cannot run. Reporting it as the
// default would put a dead id on the chip while the turn silently falls back to
// the binding, so the default must come from the list instead.
func TestDefaultModelID_DropsAScopedPinWhoseProviderIsGone(t *testing.T) {
	ReplaceProviders([]*Provider{{
		ID: 1, Name: "Bound", Protocol: ProviderProtocolOpenAI,
		Models: []string{"bound-default"}, DefaultModel: "bound-default",
	}})
	t.Cleanup(func() { ReplaceProviders(nil) })

	bound := int64(1)
	agent := &Agent{
		Backend:        "builtin",
		ProviderID:     &bound,
		PreferredModel: ProviderScopedModelID(999, "gone-model"),
	}
	assert.Equal(t, "bound-default", agent.DefaultModelID(),
		"the deleted provider's model must not be reported as the default")
}
