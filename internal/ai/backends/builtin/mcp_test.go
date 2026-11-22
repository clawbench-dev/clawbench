package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestBuiltinBackendSupportsMCP guards the registry fact the whole feature
// depends on: model.ResolveMCPServers drops every server for a backend whose
// spec is registered as MCPUnsupported. If builtin ever lands on that list
// (a probe that only tests ACP could add it), MCP would go silent.
func TestBuiltinBackendSupportsMCP(t *testing.T) {
	assert.True(t, model.BackendSupportsMCP(BackendID))
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

// TestExecuteStream_CallsAnMCPTool is the end-to-end wiring check for MCP: a
// turn where the model calls a tool the MCP server published, and the result
// travels back through the normal tool card frames.
//
// Both ends are fakes (an OpenAI-compatible endpoint plays the model, an HTTP
// MCP server plays the tool provider), because the alternative is a test that
// needs the internet and a real remote server.
func TestExecuteStream_CallsAnMCPTool(t *testing.T) {
	mcpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"fake-wiki","version":"1"}}}`, req.ID)
		case "tools/list":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"lookup","description":"Look something up.","inputSchema":{"type":"object","properties":{"q":{"type":"string"}}}}]}}`, req.ID)
		case "tools/call":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":"answer from the wiki"}]}}`, req.ID)
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer mcpSrv.Close()

	const askForTheTool = `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"m1","function":{"name":"mcp__fake__lookup","arguments":"{\"q\":\"react\"}"}}]}}]}`
	modelSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		if strings.Contains(string(raw), "answer from the wiki") {
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"The wiki says: answer from the wiki.\"}}]}\n\ndata: [DONE]\n\n")
			return
		}
		for _, f := range []string{
			askForTheTool,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`[DONE]`,
		} {
			_, _ = io.WriteString(w, "data: "+f+"\n\n")
		}
	}))
	defer modelSrv.Close()

	agentID := withProvider(t, modelSrv.URL)
	model.PublishMCPSnapshot(model.MCPConfig{Servers: []model.MCPServer{{
		Name: "fake", Transport: model.MCPTransportHTTP, URL: mcpSrv.URL,
	}}})
	model.UpdateAgent(agentID, func(a *model.Agent) { a.MCPServers = model.MCPServerPolicy{Mode: model.MCPPolicyAll} })
	t.Cleanup(func() { model.PublishMCPSnapshot(model.MCPConfig{}) })

	ch, err := NewForTest().ExecuteStream(context.Background(), ai.ChatRequest{
		AgentID: agentID, Prompt: "look react up", WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}

	var (
		sawStart, sawEnd bool
		sawAnswer        bool
		seenName         string
	)
	for _, ev := range collectStream(t, ch) {
		switch ev.Type {
		case "tool_use":
			if ev.Tool == nil {
				t.Fatalf("a tool_use frame must carry the call")
			}
			seenName = ev.Tool.Name
			if !ev.Tool.Done {
				sawStart = true
				continue
			}
			sawEnd = true
			if ev.Tool.Status != "success" {
				t.Fatalf("the MCP call must succeed, got %q (%q)", ev.Tool.Status, ev.Tool.Output)
			}
			if !strings.Contains(ev.Tool.Output, "answer from the wiki") {
				t.Fatalf("the tool output must be the server's text, got %q", ev.Tool.Output)
			}
		case "content":
			if strings.Contains(ev.Content, "The wiki says") {
				sawAnswer = true
			}
		}
	}
	if seenName != "mcp__fake__lookup" {
		t.Fatalf("the MCP tool must be declared under its namespaced name, got %q", seenName)
	}
	if !sawStart || !sawEnd || !sawAnswer {
		t.Fatalf("missing frames: start=%v end=%v answer=%v", sawStart, sawEnd, sawAnswer)
	}
}
