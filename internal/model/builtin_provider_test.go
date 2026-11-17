package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBuiltinBackendAcceptsProviderProtocols pins the single most important
// configuration fact about the built-in agent: it must be BINDABLE.
//
// SupportedProtocols drives the settings page's provider picker. An empty list
// hides every provider, so the agent would have no model endpoint, and every
// turn would fail with "no provider bound" — a dead agent that looks correctly
// configured. The two protocols are exactly the two wire formats
// internal/agentcore/llm implements; Gemini is absent on purpose.
func TestBuiltinBackendAcceptsProviderProtocols(t *testing.T) {
	got := SupportedProtocols("builtin")
	assert.ElementsMatch(t, []string{ProviderProtocolOpenAI, ProviderProtocolAnthropic}, got,
		"the built-in backend must accept exactly the wire formats agentcore implements")
	assert.NotContains(t, got, ProviderProtocolGemini,
		"agentcore has no Gemini client; advertising it would accept a binding that always fails")
}

// TestBuiltinBackendProviderEnvIsEmpty pins that the backend injects no
// environment variables.
//
// It calls the endpoint itself, so there is no CLI to hand variables to. An
// accidental variable here would be written into a spawned process that does
// not exist and silently do nothing.
func TestBuiltinBackendProviderEnvIsEmpty(t *testing.T) {
	p := &Provider{ID: 1, Name: "P", Protocol: ProviderProtocolOpenAI, BaseURL: "https://x", APIKey: "k"}
	env, err := BuildProviderEnvForAgent("builtin", "builtin", p, "")
	assert.NoError(t, err)
	assert.Empty(t, env)
}

// TestBuiltinBackendAcceptsAnyAuthMode pins the InProcess flag's one effect.
//
// Every other backend has to match the auth mode to a variable it actually
// reads, because the mode decides which header the spawned CLI sends. The
// built-in agent sends the header itself, so all three modes are expressible —
// and rejecting one would make a valid provider unbindable with no explanation
// the user could act on.
func TestBuiltinBackendAcceptsAnyAuthMode(t *testing.T) {
	for _, mode := range []string{ProviderAuthAuto, ProviderAuthAPIKey, ProviderAuthBearer} {
		for _, protocol := range []string{ProviderProtocolOpenAI, ProviderProtocolAnthropic} {
			assert.NoError(t, ProviderAuthModeError("builtin", protocol, mode),
				"builtin must accept %s auth for %s", mode, protocol)
		}
	}
	// A protocol agentcore cannot speak must still be refused: accepting it
	// would let the user bind an endpoint that fails on the first turn.
	assert.Error(t, ProviderAuthModeError("builtin", ProviderProtocolGemini, ProviderAuthAuto))
}
