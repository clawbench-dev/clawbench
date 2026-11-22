package codebuddy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

// writeUserMCPConfig seeds ~/.codebuddy/.mcp.json under a temporary HOME and
// returns the JSON payload the hook is expected to forward.
func writeUserMCPConfig(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".codebuddy")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(body), 0o600))
}

// TestSpawnArgs_LegacyMCPConfigOnlyWhenClawbenchHasNone pins the compatibility
// period: while mcp.servers is empty the user's own ~/.codebuddy/.mcp.json is
// forwarded; once ClawBench has servers the protocol field carries them and
// passing both would register the same server twice.
func TestSpawnArgs_LegacyMCPConfigOnlyWhenClawbenchHasNone(t *testing.T) {
	writeUserMCPConfig(t, `{"mcpServers":{"tavily":{"command":"tavily","args":[]}}}`)

	args := spawnArgs(ai.ACPSpawnContext{MCPServerCount: 0})
	require.Len(t, args, 2, "expected --mcp-config <json>, got %v", args)
	assert.Equal(t, "--mcp-config", args[0])
	assert.JSONEq(t, `{"tavily":{"command":"tavily","args":[]}}`, args[1])

	args = spawnArgs(ai.ACPSpawnContext{MCPServerCount: 1})
	assert.Empty(t, args, "must not pass --mcp-config when ClawBench has its own MCP servers")
}

func TestSpawnArgs_NoUserFileMeansNoArgs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	assert.Empty(t, spawnArgs(ai.ACPSpawnContext{MCPServerCount: 0}))
}

func TestSpawnArgs_EmptyServerMapIsIgnored(t *testing.T) {
	writeUserMCPConfig(t, `{"mcpServers":{}}`)
	assert.Empty(t, spawnArgs(ai.ACPSpawnContext{MCPServerCount: 0}))
}

func TestSpawnHooks_Registered(t *testing.T) {
	hooks := spawnHooks()
	require.NotNil(t, hooks)
	assert.NotNil(t, hooks.Args)
	assert.Nil(t, hooks.Env, "codebuddy needs no extra env")
}

// The hook must be attached to the registered plugin, or the shared lifecycle
// never calls it.
func TestSpawnHooks_AttachedToPlugin(t *testing.T) {
	hooks := backends.LookupACPSpawnHooks("codebuddy")
	require.NotNil(t, hooks, "codebuddy's spawn hooks must be reachable through the registry")
	assert.NotNil(t, hooks.Args)
}

// An own-service provider (a dedicated / self-hosted edition) carries a
// customer address that has no environment variable, so it must ride along as
// --settings. This is the branch that keeps those editions reachable at all.
func TestSpawnArgs_OwnServiceSettings(t *testing.T) {
	provider := &model.Provider{
		ID: 1, Name: "CB", Protocol: model.ProviderProtocolPrivate, APIKey: "ck-1",
		Deployment: model.ProviderDeploymentSelfhosted, BaseURL: "https://cb.example.com",
	}
	model.ReplaceProviders([]*model.Provider{provider})
	pid := provider.ID
	agent := &model.Agent{ID: "cb-agent", Name: "cb", Backend: "codebuddy", ProviderID: &pid}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})
	t.Cleanup(func() {
		model.ReplaceProviders(nil)
		model.ReplaceAgents(nil, nil)
	})

	args := spawnArgs(ai.ACPSpawnContext{Agent: agent, SessionID: "sid", MCPServerCount: 1})
	require.Len(t, args, 2, "expected --settings <json>, got %v", args)
	assert.Equal(t, "--settings", args[0])
	assert.JSONEq(t, `{"enterpriseEndpoint":"https://cb.example.com"}`, args[1])
}
