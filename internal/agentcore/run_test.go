package agentcore

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"clawbench/internal/agentcore/llm"
	"clawbench/internal/model"
)

// scriptedClient is a model stub that replays a fixed sequence of rounds.
//
// Each round is a function of the request, so a test can assert what the model
// was actually shown (history, tool list) instead of only what it produced.
type scriptedClient struct {
	mu       sync.Mutex
	rounds   []func(req llm.Request) []llm.Event
	requests []llm.Request
	vision   bool
}

func (c *scriptedClient) Model() string        { return "stub" }
func (c *scriptedClient) SupportsVision() bool { return c.vision }

func (c *scriptedClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.Event, error) {
	c.mu.Lock()
	c.requests = append(c.requests, req)
	idx := len(c.requests) - 1
	c.mu.Unlock()

	ch := make(chan llm.Event, 16)
	go func() {
		defer close(ch)
		if idx >= len(c.rounds) {
			ch <- llm.Event{Kind: llm.KindDone}
			return
		}
		for _, ev := range c.rounds[idx](req) {
			ch <- ev
		}
	}()
	return ch, nil
}

func (c *scriptedClient) roundCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.requests)
}

func (c *scriptedClient) request(i int) llm.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requests[i]
}

// textRound answers with plain text and no tool calls.
func textRound(text string) func(llm.Request) []llm.Event {
	return func(llm.Request) []llm.Event {
		return []llm.Event{
			{Kind: llm.KindText, Text: text},
			{Kind: llm.KindUsage, Usage: &llm.Usage{InputTokens: 10, OutputTokens: 5}},
			{Kind: llm.KindDone},
		}
	}
}

// toolRound requests one tool call with the given arguments.
func toolRound(id, name, args string) func(llm.Request) []llm.Event {
	return func(llm.Request) []llm.Event {
		return []llm.Event{
			{Kind: llm.KindToolCallStart, ToolCall: &llm.ToolCall{ID: id, Name: name}},
			{Kind: llm.KindToolCallDelta, ToolCall: &llm.ToolCall{Arguments: args}},
			{Kind: llm.KindDone},
		}
	}
}

// collect drains a run and returns its terminal event plus the emitted kinds.
func collect(t *testing.T, ch <-chan Event) (Event, []EventKind) {
	t.Helper()
	var (
		terminal Event
		kinds    []EventKind
	)
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				if terminal.Kind == "" {
					t.Fatal("the run closed without a terminal event")
				}
				return terminal, kinds
			}
			kinds = append(kinds, ev.Kind)
			if ev.Kind == EventDone || ev.Kind == EventError {
				terminal = ev
			}
		case <-deadline:
			t.Fatal("the run did not finish")
		}
	}
}

// TestRun_AnswersWithoutTools pins the simplest path: no tool calls, one round.
func TestRun_AnswersWithoutTools(t *testing.T) {
	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{textRound("hello")}}
	terminal, _ := collect(t, Run(context.Background(), Spec{
		Client: client, Prompt: "hi", WorkDir: t.TempDir(),
		Policy: model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}))

	if terminal.Kind != EventDone {
		t.Fatalf("expected a done event, got %v (%v)", terminal.Kind, terminal.Err)
	}
	if terminal.StopReason != StopComplete {
		t.Fatalf("expected complete, got %q", terminal.StopReason)
	}
	if client.roundCount() != 1 {
		t.Fatalf("expected 1 round, got %d", client.roundCount())
	}
}

// TestRun_ToolCallFeedsResultBack pins the loop: a tool result must reach the
// model as a tool message, otherwise it re-calls the same tool forever.
func TestRun_ToolCallFeedsResultBack(t *testing.T) {
	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{
		toolRound("c1", "read", `{"path":"a.txt"}`),
		textRound("done reading"),
	}}
	root := t.TempDir()
	writeFile(t, root, "a.txt", "file body")

	terminal, kinds := collect(t, Run(context.Background(), Spec{
		Client: client, Prompt: "read a.txt", WorkDir: root,
		Policy: model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}))

	if terminal.Kind != EventDone {
		t.Fatalf("expected done, got %v (%v)", terminal.Kind, terminal.Err)
	}
	if client.roundCount() != 2 {
		t.Fatalf("expected 2 rounds, got %d", client.roundCount())
	}

	// The second request must carry the tool result as a tool message.
	second := client.request(1)
	foundTool := false
	for _, m := range second.Messages {
		if m.Role != llm.RoleTool {
			continue
		}
		for _, p := range m.Parts {
			if p.ToolResult != nil && p.ToolResult.CallID == "c1" {
				foundTool = true
				body := ""
				for _, rp := range p.ToolResult.Parts {
					body += rp.Text
				}
				if !strings.Contains(body, "file body") {
					t.Fatalf("the tool result must carry the file content, got %q", body)
				}
			}
		}
	}
	if !foundTool {
		t.Fatal("the tool result must be replayed to the model")
	}

	if !containsKind(kinds, EventToolEnd) {
		t.Fatal("the run must report the finished tool call")
	}
}

// TestRun_DeniedCapabilityRemovesToolFromSpec pins the safety model: a denied
// capability means the tool is ABSENT, not merely refused at call time. A model
// that is told "write is not allowed" can argue; one that never sees a write
// tool cannot.
func TestRun_DeniedCapabilityRemovesToolFromSpec(t *testing.T) {
	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{textRound("ok")}}
	_, _ = collect(t, Run(context.Background(), Spec{
		Client: client, Prompt: "hi", WorkDir: t.TempDir(),
		Policy: model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}))

	names := toolNames(client.request(0).Tools)
	for _, forbidden := range []string{"write", "edit", "bash"} {
		if contains(names, forbidden) {
			t.Fatalf("%q must not be declared to the model under a read-only policy (got %v)", forbidden, names)
		}
	}
	for _, want := range []string{"read", "glob", "grep"} {
		if !contains(names, want) {
			t.Fatalf("%q must be declared under a read policy (got %v)", want, names)
		}
	}
}

// TestRun_EmptyPolicyDeclaresNoTools pins that "no grant" really means no
// tools, rather than a default set.
func TestRun_EmptyPolicyDeclaresNoTools(t *testing.T) {
	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{textRound("ok")}}
	_, _ = collect(t, Run(context.Background(), Spec{
		Client: client, Prompt: "hi", WorkDir: t.TempDir(),
	}))
	if got := client.request(0).Tools; len(got) != 0 {
		t.Fatalf("expected no tools, got %d", len(got))
	}
}

// TestRun_ExecGrantsBash pins the capability→tool mapping from the other side.
func TestRun_ExecGrantsBash(t *testing.T) {
	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{textRound("ok")}}
	_, _ = collect(t, Run(context.Background(), Spec{
		Client: client, Prompt: "hi", WorkDir: t.TempDir(),
		Policy: model.ToolPolicy{Allow: []string{model.CapExec}},
	}))
	names := toolNames(client.request(0).Tools)
	if !contains(names, "bash") {
		t.Fatalf("exec must grant bash, got %v", names)
	}
}

// TestRun_UnknownToolIsReportedNotFatal pins that a hallucinated tool name
// produces a tool error the model can read, rather than ending the turn.
func TestRun_UnknownToolIsReportedNotFatal(t *testing.T) {
	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{
		toolRound("c1", "delete_everything", `{}`),
		textRound("sorry"),
	}}
	terminal, _ := collect(t, Run(context.Background(), Spec{
		Client: client, Prompt: "hi", WorkDir: t.TempDir(),
		Policy: model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}))
	if terminal.Kind != EventDone {
		t.Fatalf("an unknown tool must not end the run, got %v", terminal.Kind)
	}
	second := client.request(1)
	var body string
	for _, m := range second.Messages {
		for _, p := range m.Parts {
			if p.ToolResult != nil {
				for _, rp := range p.ToolResult.Parts {
					body += rp.Text
				}
			}
		}
	}
	if !strings.Contains(body, "no tool named") {
		t.Fatalf("the model must be told the tool does not exist, got %q", body)
	}
	if !strings.Contains(body, "read") {
		t.Fatalf("the error must list the tools that do exist, got %q", body)
	}
}

// TestRun_RoundBudgetStops pins the round cap, which is what keeps a looping
// agent from running until the user notices the bill.
func TestRun_RoundBudgetStops(t *testing.T) {
	// Every round calls a tool, so only the budget can end the run.
	rounds := make([]func(llm.Request) []llm.Event, 50)
	for i := range rounds {
		rounds[i] = toolRound("c", "glob", `{"pattern":"*.go"}`)
	}
	client := &scriptedClient{rounds: rounds}

	terminal, _ := collect(t, Run(context.Background(), Spec{
		Client: client, Prompt: "loop", WorkDir: t.TempDir(),
		Policy: model.ToolPolicy{Allow: []string{model.CapFSRead}, MaxRounds: 3},
	}))

	if terminal.Kind != EventDone {
		t.Fatalf("a budget stop is not a failure, got %v (%v)", terminal.Kind, terminal.Err)
	}
	if terminal.StopReason != StopMaxRounds {
		t.Fatalf("expected max_rounds, got %q", terminal.StopReason)
	}
	if client.roundCount() > 4 {
		t.Fatalf("the round budget must bound model calls, got %d", client.roundCount())
	}
}

// TestRun_ToolCallBudgetStops pins the per-turn tool cap independently of the
// round cap.
func TestRun_ToolCallBudgetStops(t *testing.T) {
	rounds := make([]func(llm.Request) []llm.Event, 20)
	for i := range rounds {
		rounds[i] = toolRound("c", "glob", `{"pattern":"*.go"}`)
	}
	client := &scriptedClient{rounds: rounds}

	terminal, _ := collect(t, Run(context.Background(), Spec{
		Client: client, Prompt: "loop", WorkDir: t.TempDir(),
		Policy: model.ToolPolicy{Allow: []string{model.CapFSRead}, MaxToolCalls: 2, MaxRounds: 20},
	}))

	if terminal.Kind != EventDone {
		t.Fatalf("a budget stop is not a failure, got %v", terminal.Kind)
	}
	if terminal.StopReason != StopMaxToolCalls {
		t.Fatalf("expected max_tool_calls, got %q", terminal.StopReason)
	}
}

// TestRun_CancellationIsReportedAsCancellation pins that pressing stop does not
// surface as a generic failure.
func TestRun_CancellationIsReportedAsCancellation(t *testing.T) {
	blocking := func(llm.Request) []llm.Event {
		// Never terminates on its own; the context must end the run.
		time.Sleep(5 * time.Second)
		return []llm.Event{{Kind: llm.KindDone}}
	}
	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{blocking}}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	terminal, _ := collect(t, Run(ctx, Spec{
		Client: client, Prompt: "hi", WorkDir: t.TempDir(),
		Policy: model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}))
	if terminal.Kind != EventError || !errors.Is(terminal.Err, context.Canceled) {
		t.Fatalf("cancellation must surface as context.Canceled, got %v / %v", terminal.Kind, terminal.Err)
	}
}

// TestRun_ModelErrorIsTerminal pins that a client failure ends the run with an
// error instead of looping on a broken endpoint.
func TestRun_ModelErrorIsTerminal(t *testing.T) {
	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{
		func(llm.Request) []llm.Event {
			return []llm.Event{{Kind: llm.KindError, Err: errors.New("boom")}}
		},
	}}
	terminal, _ := collect(t, Run(context.Background(), Spec{
		Client: client, Prompt: "hi", WorkDir: t.TempDir(),
		Policy: model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}))
	if terminal.Kind != EventError || !strings.Contains(terminal.Err.Error(), "boom") {
		t.Fatalf("a model error must be terminal, got %v / %v", terminal.Kind, terminal.Err)
	}
}

// TestRunSync_ReturnsTextAndCounters pins the background-task entry point.
func TestRunSync_ReturnsTextAndCounters(t *testing.T) {
	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{
		toolRound("c1", "read", `{"path":"a.txt"}`),
		textRound("final answer"),
	}}
	root := t.TempDir()
	writeFile(t, root, "a.txt", "x")

	res, err := RunSync(context.Background(), Spec{
		Client: client, Prompt: "go", WorkDir: root,
		Policy: model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "final answer" {
		t.Fatalf("expected the final answer, got %q", res.Text)
	}
	if res.Rounds != 2 {
		t.Fatalf("expected 2 rounds, got %d", res.Rounds)
	}
	if res.ToolCalls != 1 {
		t.Fatalf("expected 1 tool call, got %d", res.ToolCalls)
	}
	if res.StopReason != StopComplete {
		t.Fatalf("expected complete, got %q", res.StopReason)
	}
}

// TestBuildSystemPrompt_StatesDeniedCapabilities pins that the model is told
// what it cannot do.
//
// Without this it reads a missing tool as "I forgot" and spends rounds hunting
// for a way to write the file.
func TestBuildSystemPrompt_StatesDeniedCapabilities(t *testing.T) {
	got := buildSystemPrompt("You are helpful.", "/tmp/x", nil, model.ToolPolicy{})
	for _, want := range []string{"You are helpful.", "/tmp/x", "cannot read files", "run shell commands"} {
		if !strings.Contains(got, want) {
			t.Fatalf("system prompt must contain %q, got:\n%s", want, got)
		}
	}
}

// TestBuildSystemPrompt_OmitsDenialsWhenGranted pins that a fully granted agent
// is not told about restrictions it does not have.
func TestBuildSystemPrompt_OmitsDenialsWhenGranted(t *testing.T) {
	policy := model.ToolPolicy{Allow: []string{model.CapExec}}.Normalized()
	got := buildSystemPrompt("", "/tmp/x", nil, policy)
	if strings.Contains(got, "You cannot") {
		t.Fatalf("a fully granted agent must not be told it is restricted, got:\n%s", got)
	}
}

// TestRun_NoVisionClientGetsTextOnlyRead pins that an image-incapable model is
// offered a read tool that explains itself, instead of receiving bytes it will
// choke on.
func TestRun_NoVisionClientGetsTextOnlyRead(t *testing.T) {
	client := &scriptedClient{vision: false, rounds: []func(llm.Request) []llm.Event{textRound("ok")}}
	_, _ = collect(t, Run(context.Background(), Spec{
		Client: client, Prompt: "hi", WorkDir: t.TempDir(),
		Policy: model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}))

	for _, spec := range client.request(0).Tools {
		if spec.Name != "read" {
			continue
		}
		if strings.Contains(spec.Description, "come back as an image") {
			t.Fatal("a text-only model must not be promised image results")
		}
	}
}
