package model

import (
	"strings"
	"testing"
)

// The private protocol is not CodeBuddy-only: every backend that reads a fixed
// credential for its OWN service can offer it, so the provider form can hide the
// endpoint, auth mode and model list those backends never use.

func TestPrivateProtocol_KeyOnlyBackends(t *testing.T) {
	cases := []struct {
		backend string
		envVar  string
	}{
		{"cursor", "CURSOR_API_KEY"},
		{"commandcode", "COMMAND_CODE_API_KEY"},
		{"antigravity", "GEMINI_API_KEY"},
	}
	for _, c := range cases {
		t.Run(c.backend, func(t *testing.T) {
			p := &Provider{
				ID: 1, Name: c.backend + " key", Protocol: ProviderProtocolPrivate,
				APIKey: "k-1", AuthMode: ProviderAuthAuto,
			}
			if !ProviderOwnServiceMode(c.backend, p) {
				t.Fatalf("%s must accept the private protocol", c.backend)
			}
			if !ProviderEnvSupportedProvider(c.backend, p) {
				t.Fatalf("%s must accept a private-protocol provider", c.backend)
			}
			if !containsString(SupportedProtocols(c.backend), ProviderProtocolPrivate) {
				t.Fatalf("%s must advertise the private protocol", c.backend)
			}
			env, err := BuildProviderEnv(c.backend, p, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(strings.Join(env, ","), c.envVar+"=k-1") {
				t.Fatalf("%s env = %v, want %s", c.backend, env, c.envVar)
			}
		})
	}
}

// A backend without its own official service must reject the private protocol:
// there is no variable to inject and no default endpoint to talk to.
func TestPrivateProtocol_RejectedWithoutOwnService(t *testing.T) {
	p := &Provider{ID: 1, Name: "p", Protocol: ProviderProtocolPrivate, APIKey: "k", AuthMode: ProviderAuthAuto}
	if ProviderOwnServiceMode("claude", p) {
		t.Fatal("claude has no own-service mode")
	}
	if ProviderEnvSupportedProvider("claude", p) {
		t.Fatal("claude must reject the private protocol")
	}
	if containsString(SupportedProtocols("claude"), ProviderProtocolPrivate) {
		t.Fatal("claude must not advertise the private protocol")
	}
}
