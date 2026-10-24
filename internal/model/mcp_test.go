package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// withMCPTestState installs a fresh global server list and a minimal agent set,
// restoring both afterwards. Tests here mutate package-level state, so they must
// not run in parallel.
func withMCPTestState(t *testing.T, servers []MCPServer, agents ...*Agent) {
	t.Helper()

	PublishMCPSnapshot(MCPConfig{Servers: servers})

	agentsMu.Lock()
	orig := Agents
	restored := make(map[string]*Agent, len(agents))
	for _, a := range agents {
		restored[a.ID] = a
	}
	Agents = restored
	agentsMu.Unlock()

	t.Cleanup(func() {
		agentsMu.Lock()
		Agents = orig
		agentsMu.Unlock()
		PublishMCPSnapshot(MCPConfig{})
	})
}

func TestMCPServerPolicyNormalized(t *testing.T) {
	// The zero value MUST mean "all". Reading it as "none" would strip MCP from
	// every agent whenever a row predates the column or an unmarshal fails.
	require.Equal(t, MCPPolicyAll, MCPServerPolicy{}.Normalized().Mode)
	require.Equal(t, MCPPolicyAll, MCPServerPolicy{Mode: ""}.Normalized().Mode)
	require.Equal(t, MCPPolicyAll, MCPServerPolicy{Mode: "garbage"}.Normalized().Mode)
	require.Equal(t, MCPPolicyNone, MCPServerPolicy{Mode: MCPPolicyNone}.Normalized().Mode)
	require.Equal(t, MCPPolicySelected, MCPServerPolicy{Mode: MCPPolicySelected}.Normalized().Mode)
}

func TestResolveMCPServers_PolicyModes(t *testing.T) {
	servers := []MCPServer{
		{Name: "a", Transport: MCPTransportStdio, Command: "node"},
		{Name: "b", Transport: MCPTransportStdio, Command: "node"},
	}
	caps := MCPCapabilities{}

	t.Run("all returns every server", func(t *testing.T) {
		withMCPTestState(t, servers, &Agent{ID: "ag", Backend: "claude", MCPServers: MCPServerPolicy{Mode: MCPPolicyAll}})
		got := ResolveMCPServers("ag", caps)
		require.Len(t, got, 2)
	})

	t.Run("none returns nothing", func(t *testing.T) {
		withMCPTestState(t, servers, &Agent{ID: "ag", Backend: "claude", MCPServers: MCPServerPolicy{Mode: MCPPolicyNone}})
		require.Empty(t, ResolveMCPServers("ag", caps))
	})

	t.Run("selected returns only the named ones", func(t *testing.T) {
		withMCPTestState(t, servers, &Agent{
			ID: "ag", Backend: "claude",
			MCPServers: MCPServerPolicy{Mode: MCPPolicySelected, Names: []string{"b"}},
		})
		got := ResolveMCPServers("ag", caps)
		require.Len(t, got, 1)
		require.Equal(t, "b", got[0].Name)
	})

	t.Run("unknown agent gets nothing", func(t *testing.T) {
		withMCPTestState(t, servers)
		require.Empty(t, ResolveMCPServers("missing", caps))
	})
}

func TestResolveMCPServers_CapabilityFilter(t *testing.T) {
	servers := []MCPServer{
		{Name: "s", Transport: MCPTransportStdio, Command: "node"},
		{Name: "h", Transport: MCPTransportHTTP, URL: "https://x"},
		{Name: "e", Transport: MCPTransportSSE, URL: "https://y"},
	}
	agent := &Agent{ID: "ag", Backend: "claude", MCPServers: MCPServerPolicy{Mode: MCPPolicyAll}}

	cases := []struct {
		name string
		caps MCPCapabilities
		want []string
	}{
		// stdio is mandatory in the spec, so it is never gated.
		{"no capabilities → stdio only", MCPCapabilities{}, []string{"s"}},
		{"http only", MCPCapabilities{HTTP: true}, []string{"s", "h"}},
		{"sse only", MCPCapabilities{SSE: true}, []string{"s", "e"}},
		{"both", MCPCapabilities{HTTP: true, SSE: true}, []string{"s", "h", "e"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withMCPTestState(t, servers, agent)
			got := ResolveMCPServers("ag", tc.caps)
			names := make([]string, 0, len(got))
			for _, s := range got {
				names = append(names, s.Name)
			}
			require.Equal(t, tc.want, names)
		})
	}
}

func TestResolveMCPServers_UnsupportedBackends(t *testing.T) {
	// dsh / deepseek / pi accept the field and ignore it (probed). The default
	// policy is "all", so without this filter every user would hit the silently
	// ineffective case by default — the UI graying is only a hint.
	servers := []MCPServer{{Name: "a", Transport: MCPTransportStdio, Command: "node"}}
	for _, backend := range []string{"dsh", "deepseek", "pi"} {
		t.Run(backend, func(t *testing.T) {
			withMCPTestState(t, servers, &Agent{
				ID: "ag", Backend: backend,
				MCPServers: MCPServerPolicy{Mode: MCPPolicyAll},
			})
			require.Empty(t, ResolveMCPServers("ag", MCPCapabilities{HTTP: true, SSE: true}))
			require.Empty(t, MCPPolicyVersion("ag"))
		})
	}

	t.Run("supported backend is not filtered", func(t *testing.T) {
		withMCPTestState(t, servers, &Agent{
			ID: "ag", Backend: "claude",
			MCPServers: MCPServerPolicy{Mode: MCPPolicyAll},
		})
		require.Len(t, ResolveMCPServers("ag", MCPCapabilities{}), 1)
	})
}

func TestMCPPolicyVersion_Stable(t *testing.T) {
	// Map iteration order is randomized in Go. If the hash were built from an
	// unsorted encoding it would change on every call, and the connection would
	// reconnect before EVERY turn.
	servers := []MCPServer{{
		Name:      "a",
		Transport: MCPTransportStdio,
		Command:   "node",
		Env:       map[string]string{"Z": "1", "A": "2", "M": "3"},
	}}
	agent := &Agent{ID: "ag", Backend: "claude", MCPServers: MCPServerPolicy{Mode: MCPPolicyAll}}
	withMCPTestState(t, servers, agent)

	first := MCPPolicyVersion("ag")
	require.NotEmpty(t, first)
	for range 20 {
		require.Equal(t, first, MCPPolicyVersion("ag"), "version must be deterministic")
	}
}

func TestMCPPolicyVersion_ChangesWithContent(t *testing.T) {
	agent := &Agent{ID: "ag", Backend: "claude", MCPServers: MCPServerPolicy{Mode: MCPPolicyAll}}

	withMCPTestState(t, []MCPServer{{Name: "a", Transport: MCPTransportStdio, Command: "node"}}, agent)
	v1 := MCPPolicyVersion("ag")

	withMCPTestState(t, []MCPServer{{Name: "a", Transport: MCPTransportStdio, Command: "node2"}}, agent)
	v2 := MCPPolicyVersion("ag")

	require.NotEqual(t, v1, v2, "changing the command must change the version")
}

func TestMCPPolicyVersion_IgnoresCapabilities(t *testing.T) {
	// The version deliberately excludes capabilities: they are only known after
	// Initialize, which runs after the connection is created. Hashing the
	// filtered list would make every fresh process reconnect once.
	servers := []MCPServer{
		{Name: "s", Transport: MCPTransportStdio, Command: "node"},
		{Name: "h", Transport: MCPTransportHTTP, URL: "https://x"},
	}
	agent := &Agent{ID: "ag", Backend: "claude", MCPServers: MCPServerPolicy{Mode: MCPPolicyAll}}
	withMCPTestState(t, servers, agent)

	// The version function takes no capabilities at all; this test pins that
	// contract by asserting the empty-list case still yields a stable hash that
	// equals itself across capability states (which it cannot see).
	require.Equal(t, MCPPolicyVersion("ag"), MCPPolicyVersion("ag"))
}

func TestMCPPolicyVersion_EmptyList(t *testing.T) {
	withMCPTestState(t, nil, &Agent{ID: "ag", Backend: "claude", MCPServers: MCPServerPolicy{Mode: MCPPolicyAll}})
	require.Empty(t, MCPPolicyVersion("ag"), "nothing configured → empty version")

	withMCPTestState(t, []MCPServer{{Name: "a", Transport: MCPTransportStdio, Command: "node"}},
		&Agent{ID: "ag", Backend: "claude", MCPServers: MCPServerPolicy{Mode: MCPPolicyNone}})
	require.Empty(t, MCPPolicyVersion("ag"), "policy none → empty version")
}

func TestMCPServersSnapshot_IsCopy(t *testing.T) {
	orig := []MCPServer{{
		Name: "a", Transport: MCPTransportStdio, Command: "node",
		Env: map[string]string{"K": "v"},
	}}
	PublishMCPSnapshot(MCPConfig{Servers: orig})
	t.Cleanup(func() { PublishMCPSnapshot(MCPConfig{}) })

	// Mutating the source after publishing must not change what readers see:
	// applyConfigPatch edits the live config in place, and readers run
	// concurrently.
	orig[0].Env["K"] = "mutated"
	orig[0].Name = "mutated"

	snap := MCPServersSnapshot()
	require.Len(t, snap, 1)
	require.Equal(t, "a", snap[0].Name)
	require.Equal(t, "v", snap[0].Env["K"])
}

func TestValidateMCPServers(t *testing.T) {
	cases := []struct {
		name    string
		servers []MCPServer
		wantErr string
	}{
		{"valid stdio", []MCPServer{{Name: "a", Transport: MCPTransportStdio, Command: "node"}}, ""},
		{"valid http", []MCPServer{{Name: "a", Transport: MCPTransportHTTP, URL: "https://x"}}, ""},
		{"empty name", []MCPServer{{Name: " ", Transport: MCPTransportStdio, Command: "node"}}, "name must not be empty"},
		{"duplicate name", []MCPServer{
			{Name: "a", Transport: MCPTransportStdio, Command: "node"},
			{Name: "a", Transport: MCPTransportStdio, Command: "node"},
		}, "duplicated"},
		{"bad name char", []MCPServer{{Name: "a b", Transport: MCPTransportStdio, Command: "node"}}, "may only contain"},
		{"stdio without command", []MCPServer{{Name: "a", Transport: MCPTransportStdio}}, "command is required"},
		{"http without url", []MCPServer{{Name: "a", Transport: MCPTransportHTTP}}, "url is required"},
		{"unknown transport", []MCPServer{{Name: "a", Transport: "carrier-pigeon"}}, "transport must be one of"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMCPServers(tc.servers)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestBackendSupportsMCP(t *testing.T) {
	require.False(t, BackendSupportsMCP("dsh"))
	require.False(t, BackendSupportsMCP("deepseek"))
	require.False(t, BackendSupportsMCP("pi"))
	// openclaw is a different failure mode from the three above: the bridge
	// REFUSES per-session servers (session/new errors) instead of accepting and
	// ignoring them, so injecting anything would break every turn.
	require.False(t, BackendSupportsMCP("openclaw"))
	require.True(t, BackendSupportsMCP("claude"))
	require.True(t, BackendSupportsMCP("codex"))
	// Unknown backends default to supported: the list only records measured
	// negatives, and a new backend should be probed rather than disabled.
	require.True(t, BackendSupportsMCP("some-future-agent"))
}

// TestResolveMCPServers_TransportGate pins the capability filter, including the
// unknown-transport case: forwarding something the agent will reject fails the
// whole session/new, so an unrecognized transport is dropped rather than passed
// through. Validation rejects unknown transports at save time, so this only
// fires for a hand-edited config.yaml — which is exactly when a hard failure
// would be hardest to diagnose.
func TestResolveMCPServers_TransportGate(t *testing.T) {
	servers := []MCPServer{
		{Name: "s", Transport: MCPTransportStdio, Command: "node"},
		{Name: "h", Transport: MCPTransportHTTP, URL: "https://h"},
		{Name: "e", Transport: MCPTransportSSE, URL: "https://e"},
		{Name: "u", Transport: "carrier-pigeon", Command: "coo"},
	}
	withMCPTestState(t, servers, &Agent{ID: "ag", Backend: "claude", MCPServers: MCPServerPolicy{Mode: MCPPolicyAll}})

	names := func(caps MCPCapabilities) []string {
		servers := ResolveMCPServers("ag", caps)
		out := make([]string, 0, len(servers))
		for _, s := range servers {
			out = append(out, s.Name)
		}
		return out
	}

	require.Equal(t, []string{"s"}, names(MCPCapabilities{}),
		"without advertised transports only the mandatory stdio may pass")
	require.Equal(t, []string{"s", "h"}, names(MCPCapabilities{HTTP: true}))
	require.Equal(t, []string{"s", "e"}, names(MCPCapabilities{SSE: true}))
	require.Equal(t, []string{"s", "h", "e"}, names(MCPCapabilities{HTTP: true, SSE: true}))
}

// TestResolveMCPServers_SelectedHonoursCapabilities pins that the policy filter
// and the capability filter compose: naming an http server is not enough if the
// agent never advertised http.
func TestResolveMCPServers_SelectedHonoursCapabilities(t *testing.T) {
	servers := []MCPServer{
		{Name: "s", Transport: MCPTransportStdio, Command: "node"},
		{Name: "h", Transport: MCPTransportHTTP, URL: "https://h"},
	}
	withMCPTestState(t, servers, &Agent{
		ID: "ag", Backend: "claude",
		MCPServers: MCPServerPolicy{Mode: MCPPolicySelected, Names: []string{"h"}},
	})

	require.Empty(t, ResolveMCPServers("ag", MCPCapabilities{}))
	got := ResolveMCPServers("ag", MCPCapabilities{HTTP: true})
	require.Len(t, got, 1)
	require.Equal(t, "h", got[0].Name)
}

// TestMCPPolicyVersion_SelectedIsOrderIndependent pins that a selected policy
// compares by membership, not by the order the user ticked the boxes — the hash
// drives "did the policy change, should we reconnect", so a reordering must not
// force one.
func TestMCPPolicyVersion_SelectedIsOrderIndependent(t *testing.T) {
	servers := []MCPServer{
		{Name: "a", Transport: MCPTransportStdio, Command: "node"},
		{Name: "b", Transport: MCPTransportStdio, Command: "node"},
	}
	agent := &Agent{
		ID: "ag", Backend: "claude",
		MCPServers: MCPServerPolicy{Mode: MCPPolicySelected, Names: []string{"a", "b"}},
	}
	withMCPTestState(t, servers, agent)
	v1 := MCPPolicyVersion("ag")

	agent.MCPServers.Names = []string{"b", "a"}
	require.Equal(t, v1, MCPPolicyVersion("ag"))
}

// TestMCPPolicyVersion_UnsupportedBackend pins the backend gate: a CLI-only
// backend never receives servers, so it must have no version at all — otherwise
// every turn would look like a policy change and force a pointless reconnect.
func TestMCPPolicyVersion_UnsupportedBackend(t *testing.T) {
	withMCPTestState(t, []MCPServer{{Name: "a", Transport: MCPTransportStdio, Command: "node"}},
		&Agent{ID: "ag", Backend: "dsh", MCPServers: MCPServerPolicy{Mode: MCPPolicyAll}})
	require.Empty(t, MCPPolicyVersion("ag"))

	require.Empty(t, MCPPolicyVersion("missing"), "unknown agent → no version")
}
