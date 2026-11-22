package agentcore

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"clawbench/internal/agentcore/llm"
	"clawbench/internal/agentcore/tools"
	"clawbench/internal/model"
)

// fakeExtraTool is the smallest possible caller-supplied tool.
type fakeExtraTool struct {
	name string
	run  func(env tools.Env, args json.RawMessage) tools.Result
}

func (t *fakeExtraTool) Name() string         { return t.name }
func (t *fakeExtraTool) Snippet() string      { return "an external tool" }
func (t *fakeExtraTool) Guidelines() []string { return nil }
func (t *fakeExtraTool) Spec() tools.Spec {
	return tools.Spec{Name: t.name, Parameters: map[string]any{"type": "object"}}
}

func (t *fakeExtraTool) Run(env tools.Env, args json.RawMessage) tools.Result {
	if t.run != nil {
		return t.run(env, args)
	}
	return tools.Result{Text: "ran"}
}

// TestRun_ExtraToolsAreDeclaredAndRun covers the whole path a caller-supplied
// tool travels: declared to the model, executed, and its result fed back.
func TestRun_ExtraToolsAreDeclaredAndRun(t *testing.T) {
	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{
		toolRound("c1", "mcp__demo__echo", `{"x":1}`),
		textRound("done"),
	}}

	terminal, _ := collect(t, Run(context.Background(), Spec{
		Client:     client,
		Prompt:     "use the tool",
		WorkDir:    t.TempDir(),
		ExtraTools: []tools.Tool{&fakeExtraTool{name: "mcp__demo__echo"}},
	}))

	if terminal.Kind != EventDone {
		t.Fatalf("expected a completed run, got %v (%v)", terminal.Kind, terminal.Err)
	}
	first := client.request(0)
	if !contains(toolNames(first.Tools), "mcp__demo__echo") {
		t.Fatalf("the extra tool must be declared to the model, got %v", toolNames(first.Tools))
	}

	second := client.request(1)
	found := false
	for _, msg := range second.Messages {
		for _, part := range msg.Parts {
			if part.ToolResult == nil {
				continue
			}
			for _, out := range part.ToolResult.Parts {
				if strings.Contains(out.Text, "ran") {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("the tool result must be fed back to the model, got %+v", second.Messages)
	}
}

// TestRun_ExtraToolNameCollisionIsDropped keeps a caller from shadowing a
// built-in tool: providers reject duplicate declarations, and the built-in one
// is the behavior this package guarantees.
func TestRun_ExtraToolNameCollisionIsDropped(t *testing.T) {
	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{textRound("ok")}}

	terminal, _ := collect(t, Run(context.Background(), Spec{
		Client:     client,
		Prompt:     "hi",
		WorkDir:    t.TempDir(),
		Policy:     model.ToolPolicy{Allow: []string{model.CapFSRead}},
		ExtraTools: []tools.Tool{&fakeExtraTool{name: "read"}},
	}))
	if terminal.Kind != EventDone {
		t.Fatalf("expected a completed run, got %v", terminal.Kind)
	}

	declared := toolNames(client.request(0).Tools)
	count := 0
	for _, name := range declared {
		if name == "read" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("read must be declared exactly once, got %v", declared)
	}
}

// TestBuildSystemPrompt_ToolNotes pins the "unavailable" section: a server that
// failed to attach must be explained, not silently absent.
func TestBuildSystemPrompt_ToolNotes(t *testing.T) {
	got := buildSystemPrompt("", "/tmp/x", nil, model.ToolPolicy{},
		[]string{"github: command \"npx\" not found in PATH"})
	if !strings.Contains(got, "<unavailable>") {
		t.Fatalf("expected an unavailable section, got:\n%s", got)
	}
	if !strings.Contains(got, "github: command") {
		t.Fatalf("the note must reach the prompt, got:\n%s", got)
	}

	clean := buildSystemPrompt("", "/tmp/x", nil, model.ToolPolicy{}, nil)
	if strings.Contains(clean, "<unavailable>") {
		t.Fatalf("an empty notes list must not leave an empty section, got:\n%s", clean)
	}
}

// TestBuildSystemPrompt_MCPRule pins the boundary statement: the file-path
// rules do not describe what an MCP server can reach.
func TestBuildSystemPrompt_MCPRule(t *testing.T) {
	withMCP := buildSystemPrompt("", "/tmp/x", []tools.Tool{
		&fakeExtraTool{name: "mcp__demo__echo"},
	}, model.ToolPolicy{}, nil)
	if !strings.Contains(withMCP, "external MCP server") {
		t.Fatalf("expected the MCP boundary rule, got:\n%s", withMCP)
	}
	if !strings.Contains(withMCP, "do not apply to them") {
		t.Fatalf("the rule must say the path fence does not cover MCP, got:\n%s", withMCP)
	}

	without := buildSystemPrompt("", "/tmp/x", []tools.Tool{
		&fakeExtraTool{name: "read"},
	}, model.ToolPolicy{}, nil)
	if strings.Contains(without, "external MCP server") {
		t.Fatalf("the MCP rule must not appear without MCP tools, got:\n%s", without)
	}
}
