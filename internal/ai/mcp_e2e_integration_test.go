//go:build integration

package ai

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// TestMCPE2E_ConfigPathInjection proves the PRODUCTION config path (not a
// hardcoded probe server) reaches the agent: the servers come from the same
// resolver the live code uses, and the model must call one.
func TestMCPE2E_ConfigPathInjection(t *testing.T) {
	nodePath, serverScript, backendList := probePrereqs(t)

	var b mcpProbeBackend
	for _, cand := range backendList {
		if cand.ID == "claude" {
			b = cand
		}
	}
	if b.ID == "" {
		t.Skip("claude not in the filtered set")
	}
	skipIfBackendMissing(t, b)

	// Install a global server list and an agent that may use it.
	model.PublishMCPSnapshot(model.MCPConfig{Servers: []model.MCPServer{{
		Name:      "clawbench-probe-" + probeNonce,
		Transport: model.MCPTransportStdio,
		Command:   nodePath,
		Args:      []string{serverScript},
		Env:       map[string]string{"MCP_PROBE_MARKER": mcpProbeMarker, "MCP_PROBE_TOOL": mcpProbeToolName},
	}}})
	t.Cleanup(func() { model.PublishMCPSnapshot(model.MCPConfig{}) })

	// ResolveMCPServers reads the CURRENT agent record (never a connection's
	// snapshot), so the test needs one registered.
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"claude": {ID: "claude", Backend: "claude", MCPServers: model.MCPServerPolicy{Mode: model.MCPPolicyAll}},
	}
	t.Cleanup(func() { model.Agents = origAgents })

	ctx, cancel := contextWithTimeout(t, 300*time.Second)
	defer cancel()

	SetAutoApproveGetter(func(string) bool { return true })
	t.Cleanup(func() { SetAutoApproveGetter(func(string) bool { return false }) })

	// Resolve through the production helper.
	resolved := model.ResolveMCPServers("claude", model.MCPCapabilities{})
	require.Len(t, resolved, 1, "the global list must resolve for a supported backend")

	// The conversion the four injection sites use.
	acpServers := toACPMcpServers(resolved)
	require.Len(t, acpServers, 1)

	workDir := t.TempDir()
	proc := startMCPProbe(t, ctx, b)
	defer proc.kill()

	sid, err := newProbeSession(t, ctx, proc.extProbeSetup, workDir, acpServers)
	require.NoError(t, err)

	if warm := probeWarmupDuration(); warm > 0 {
		time.Sleep(warm)
	}
	_, marker := probeTurnWithRetry(t, ctx, proc.extProbeSetup, sid, "config-path")
	require.True(t, marker, "the production resolver's servers must actually reach the model")
}
