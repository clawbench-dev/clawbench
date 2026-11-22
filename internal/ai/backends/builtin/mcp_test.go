package builtin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/agentcore/llm"
	"clawbench/internal/ai"
	"clawbench/internal/model"
)

type visionClient struct{ vision bool }

func (c visionClient) Model() string        { return "stub" }
func (c visionClient) SupportsVision() bool { return c.vision }
func (c visionClient) Stream(context.Context, llm.Request) (<-chan llm.Event, error) {
	ch := make(chan llm.Event)
	close(ch)
	return ch, nil
}

// withMCP installs a global server list and one agent for the duration of a
// test. Both are process-wide registries, so the cleanup is part of the helper
// rather than each test's responsibility.
func withMCP(t *testing.T, policy model.MCPServerPolicy, servers ...model.MCPServer) string {
	t.Helper()
	const id = "builtin-mcp-test-agent"
	model.PublishMCPSnapshot(model.MCPConfig{Servers: servers})
	if model.Agents == nil {
		model.Agents = map[string]*model.Agent{}
	}
	model.Agents[id] = &model.Agent{ID: id, Backend: BackendID, MCPServers: policy}
	t.Cleanup(func() {
		delete(model.Agents, id)
		model.PublishMCPSnapshot(model.MCPConfig{})
	})
	return id
}

// TestAttachMCP_PolicyNoneAttachesNothing pins the switch the settings page
// writes: mode=none must not even resolve servers.
func TestAttachMCP_PolicyNoneAttachesNothing(t *testing.T) {
	id := withMCP(t, model.MCPServerPolicy{Mode: model.MCPPolicyNone}, model.MCPServer{
		Name: "bad", Transport: model.MCPTransportStdio, Command: "clawbench-no-such-mcp-binary",
	})

	tools, notes, release := attachMCP(context.Background(), ai.ChatRequest{AgentID: id}, visionClient{})
	assert.Empty(t, tools)
	assert.Empty(t, notes)
	assert.Nil(t, release)
}

// TestAttachMCP_FailureBecomesANote: a server that cannot start must not fail
// the turn; the model is told why the capability is missing.
func TestAttachMCP_FailureBecomesANote(t *testing.T) {
	id := withMCP(t, model.MCPServerPolicy{Mode: model.MCPPolicyAll}, model.MCPServer{
		Name: "missing", Transport: model.MCPTransportStdio, Command: "clawbench-no-such-mcp-binary",
	})

	tools, notes, release := attachMCP(context.Background(), ai.ChatRequest{AgentID: id}, visionClient{})
	assert.Empty(t, tools)
	require.NotNil(t, release)
	defer release()

	require.Len(t, notes, 1)
	assert.Contains(t, notes[0], "missing")
	assert.Contains(t, notes[0], "not found in PATH")
}

// TestAttachMCP_DropsSSEBeforeDialing pins the transport gate: our client does
// not implement legacy sse, so it must never be attempted (and never reported
// as a failure either — it is filtered, not broken).
func TestAttachMCP_DropsSSEBeforeDialing(t *testing.T) {
	id := withMCP(t, model.MCPServerPolicy{Mode: model.MCPPolicyAll}, model.MCPServer{
		Name: "legacy", Transport: model.MCPTransportSSE, URL: "http://127.0.0.1:1/sse",
	})

	tools, notes, release := attachMCP(context.Background(), ai.ChatRequest{AgentID: id}, visionClient{})
	assert.Empty(t, tools)
	assert.Empty(t, notes)
	assert.Nil(t, release)
}

// TestAttachMCP_SelectedPolicyHonoursNames pins that the agent's checklist is
// the actual grant, not a UI hint.
func TestAttachMCP_SelectedPolicyHonoursNames(t *testing.T) {
	id := withMCP(t, model.MCPServerPolicy{Mode: model.MCPPolicySelected, Names: []string{"other"}}, model.MCPServer{
		Name: "missing", Transport: model.MCPTransportStdio, Command: "clawbench-no-such-mcp-binary",
	})

	tools, notes, release := attachMCP(context.Background(), ai.ChatRequest{AgentID: id}, visionClient{})
	assert.Empty(t, tools)
	assert.Empty(t, notes)
	assert.Nil(t, release)
}
