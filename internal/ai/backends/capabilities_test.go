package backends_test

import (
	"reflect"
	"testing"

	"clawbench/internal/ai/backends"
	"clawbench/internal/model"

	// Register every backend so the table below sees the real set.
	_ "clawbench/internal/ai/backends/all"
)

// TestCapabilities_RepresentativeBackends pins the snapshot for the shapes that
// are easy to get wrong. It is the executable version of the "which backend
// supports what" table: if a backend's declaration changes, this fails and the
// reader can see exactly which capability moved.
func TestCapabilities_RepresentativeBackends(t *testing.T) {
	cases := []struct {
		backend string
		want    model.BackendCapabilities
	}{
		{
			// ACP + mid-turn steering; the endpoint is delivered through both
			// env variables and a private settings.json.
			backend: "claude",
			want: model.BackendCapabilities{
				HasACP:               true,
				SupportsMidTurn:      true,
				MidTurnSplitsReply:   false,
				HasModelSource:       true,
				CanBindProvider:      true,
				ProviderProtocols:    []string{model.ProviderProtocolAnthropic},
				ProviderMethods:      []string{model.ProviderMethodEnv, model.ProviderMethodConfigDir},
				ProviderSecretInFile: true,
			},
		},
		{
			// The only backend whose mid-turn injection SPLITS the reply.
			backend: "codebuddy",
			want: model.BackendCapabilities{
				HasACP:                 true,
				SupportsMidTurn:        true,
				MidTurnSplitsReply:     true,
				HasModelSource:         true,
				CanBindProvider:        true,
				ProviderProtocols:      []string{model.ProviderProtocolOpenAI, model.ProviderProtocolPrivate},
				ProviderMethods:        []string{model.ProviderMethodEnv, model.ProviderMethodConfigDir, model.ProviderMethodOwnService},
				ProviderDeclaresModels: true,
			},
		},
		{
			// ACP-only, credential-only: it can be bound, but the only thing it
			// accepts is its own account key.
			backend: "cursor",
			want: model.BackendCapabilities{
				HasACP:            true,
				CanBindProvider:   true,
				ProviderProtocols: []string{model.ProviderProtocolPrivate},
				ProviderMethods:   []string{model.ProviderMethodOwnService},
				ProviderKeyOnly:   true,
			},
		},
		{
			// The provider belongs to its Gateway, so no channel reaches the
			// bridge we spawn. This is "ClawBench cannot bind", not "OpenClaw
			// cannot be configured with a provider".
			backend: "openclaw",
			want: model.BackendCapabilities{
				HasACP:                     true,
				ACPModesMirrorThoughtLevel: true,
				CanBindProvider:            false,
			},
		},
		{
			// In-process: ClawBench calls the endpoint itself, so there is no
			// env channel even though both protocols are declared.
			backend: "builtin",
			want: model.BackendCapabilities{
				HasACP:            false,
				CanBindProvider:   true,
				ProviderProtocols: []string{model.ProviderProtocolAnthropic, model.ProviderProtocolOpenAI},
				ProviderMethods:   []string{model.ProviderMethodInProcess},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.backend, func(t *testing.T) {
			got, ok := backends.Capabilities(c.backend)
			if !ok {
				t.Fatalf("%q is not registered", c.backend)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Capabilities(%q)\n got: %+v\nwant: %+v", c.backend, got, c.want)
			}
		})
	}
}

// TestCapabilities_UnknownBackend keeps the unregistered case explicit: a
// missing backend must not be reported as a capable one.
func TestCapabilities_UnknownBackend(t *testing.T) {
	if _, ok := backends.Capabilities("no-such-backend"); ok {
		t.Fatal("an unknown backend must report ok=false")
	}
}

// TestCapabilities_EveryBackendIsCovered checks the snapshot against the
// registry itself: every plugin must have an entry, and "cannot bind" must
// agree with "no delivery channel declared". The second half catches a spec
// that declares a channel nobody can reach.
func TestCapabilities_EveryBackendIsCovered(t *testing.T) {
	all := backends.AllCapabilities()
	for _, p := range backends.All() {
		caps, ok := all[p.ID]
		if !ok {
			t.Errorf("backend %q has no capability snapshot", p.ID)
			continue
		}
		if !caps.CanBindProvider && len(caps.ProviderMethods) > 0 {
			t.Errorf("backend %q cannot bind a provider but declares channels %v",
				p.ID, caps.ProviderMethods)
		}
		if caps.MidTurnSplitsReply && !caps.SupportsMidTurn {
			t.Errorf("backend %q splits a reply it cannot inject into", p.ID)
		}
	}
}
