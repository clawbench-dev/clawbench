package model

import (
	"reflect"
	"testing"
)

// TestProviderMethods pins the delivery-method taxonomy for the backends whose
// shape is easy to get wrong. Each entry is a channel a bound provider can
// travel through; a backend may have several.
func TestProviderMethods(t *testing.T) {
	cases := []struct {
		backend string
		want    []string
	}{
		// Env variables only.
		{"zcode", []string{ProviderMethodEnv}},
		{"deepseek", []string{ProviderMethodEnv}},
		{"grok", []string{ProviderMethodEnv}},
		// Env + a private config directory.
		{"claude", []string{ProviderMethodEnv, ProviderMethodConfigDir}},
		{"kimi", []string{ProviderMethodEnv, ProviderMethodConfigDir}},
		{"pi", []string{ProviderMethodEnv, ProviderMethodConfigDir}},
		// Env + config directory + the official-service credential.
		{"codebuddy", []string{ProviderMethodEnv, ProviderMethodConfigDir, ProviderMethodOwnService}},
		// Env + config CONTENT (no file on disk).
		{"opencode", []string{ProviderMethodEnv, ProviderMethodInlineConfig}},
		// Edits the user's own file; the key travels as an env reference.
		{"codex", []string{ProviderMethodConfigFile}},
		// Credential-only backends: the private protocol is the only channel.
		{"cursor", []string{ProviderMethodOwnService}},
		{"commandcode", []string{ProviderMethodOwnService}},
		// Both an endpoint override and the official-service credential.
		{"antigravity", []string{ProviderMethodEnv, ProviderMethodOwnService}},
		// The built-in agent declares both protocols with EMPTY variable names
		// (it calls the endpoint itself). It must not be reported as an env
		// backend — nothing is injected into a spawned process.
		{"builtin", []string{ProviderMethodInProcess}},
		// No channel at all: openclaw's provider belongs to its Gateway; qoder
		// has no provider configuration.
		{"openclaw", nil},
		{"qoder", nil},
		{"custom-acp", nil},
	}
	for _, c := range cases {
		t.Run(c.backend, func(t *testing.T) {
			got := ProviderMethods(c.backend)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("ProviderMethods(%q) = %v, want %v", c.backend, got, c.want)
			}
		})
	}
}

// TestProviderMethods_UnknownBackendIsEmpty keeps the unregistered case
// explicit: an unknown id must not be mistaken for "no channels declared".
func TestProviderMethods_UnknownBackendIsEmpty(t *testing.T) {
	if got := ProviderMethods("no-such-backend"); got != nil {
		t.Fatalf("ProviderMethods(unknown) = %v, want nil", got)
	}
}

// TestLookupBackendCapabilities_UnwiredFallsBack pins the conservative default:
// a build without the backends package reports nothing as capable rather than
// panicking.
func TestLookupBackendCapabilities_UnwiredFallsBack(t *testing.T) {
	orig := LookupBackendCapabilitiesFn
	t.Cleanup(func() { LookupBackendCapabilitiesFn = orig })
	LookupBackendCapabilitiesFn = nil

	caps := LookupBackendCapabilities("claude")
	if caps.CanBindProvider || caps.SupportsMidTurn {
		t.Fatalf("unwired lookup must return the zero value, got %+v", caps)
	}
}
