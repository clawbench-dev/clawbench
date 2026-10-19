package ai

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// toACPMcpServers is where the config shape meets the SDK shape. The two differ
// (maps vs name/value slices), and the SDK marshals Args/Env/Headers WITHOUT
// omitempty — a nil slice becomes JSON null and strict consumers reject the
// whole session/new. These tests pin both the conversion and the nil handling.

func TestToACPMcpServers_Stdio(t *testing.T) {
	got := toACPMcpServers([]model.MCPServer{{
		Name:      "srv",
		Transport: model.MCPTransportStdio,
		Command:   "node",
		Args:      []string{"a.js"},
		Env:       map[string]string{"B": "2", "A": "1"},
	}})
	require.Len(t, got, 1)
	require.NotNil(t, got[0].Stdio)
	require.Nil(t, got[0].Http)
	require.Nil(t, got[0].Sse)
	require.Equal(t, "srv", got[0].Stdio.Name)
	require.Equal(t, "node", got[0].Stdio.Command)
	require.Equal(t, []string{"a.js"}, got[0].Stdio.Args)
	// Sorted by name so the wire payload is deterministic.
	require.Equal(t, []acp.EnvVariable{{Name: "A", Value: "1"}, {Name: "B", Value: "2"}}, got[0].Stdio.Env)
}

func TestToACPMcpServers_EmptySlicesNotNull(t *testing.T) {
	// A stdio server with no args/env must still marshal arrays, not null:
	// codex-acp validates `args`/`env` as arrays and rejects the request
	// otherwise.
	got := toACPMcpServers([]model.MCPServer{{
		Name: "srv", Transport: model.MCPTransportStdio, Command: "node",
	}})
	require.Len(t, got, 1)
	require.NotNil(t, got[0].Stdio.Args)
	require.NotNil(t, got[0].Stdio.Env)
	require.Empty(t, got[0].Stdio.Args)
	require.Empty(t, got[0].Stdio.Env)
}

func TestToACPMcpServers_HTTPAndSSE(t *testing.T) {
	got := toACPMcpServers([]model.MCPServer{
		{Name: "h", Transport: model.MCPTransportHTTP, URL: "https://h/mcp",
			Headers: map[string]string{"Authorization": "Bearer x"}},
		{Name: "s", Transport: model.MCPTransportSSE, URL: "https://s/sse"},
	})
	require.Len(t, got, 2)

	require.NotNil(t, got[0].Http)
	require.Equal(t, "h", got[0].Http.Name)
	require.Equal(t, "http", got[0].Http.Type)
	require.Equal(t, "https://h/mcp", got[0].Http.Url)
	require.Equal(t, []acp.HttpHeader{{Name: "Authorization", Value: "Bearer x"}}, got[0].Http.Headers)

	require.NotNil(t, got[1].Sse)
	require.Equal(t, "sse", got[1].Sse.Type)
	require.NotNil(t, got[1].Sse.Headers, "headers must be [] not null")
	require.Empty(t, got[1].Sse.Headers)
}

func TestToACPMcpServers_UnknownTransportDropped(t *testing.T) {
	// Validation rejects unknown transports at save time, so this only fires for
	// a hand-edited config.yaml. Dropping beats forwarding something the agent
	// will reject.
	got := toACPMcpServers([]model.MCPServer{{Name: "x", Transport: "carrier-pigeon"}})
	require.Empty(t, got)
}

func TestToACPMcpServers_Empty(t *testing.T) {
	require.Nil(t, toACPMcpServers(nil))
	require.Nil(t, toACPMcpServers([]model.MCPServer{}))
}
