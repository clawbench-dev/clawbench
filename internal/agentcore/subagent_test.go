package agentcore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"clawbench/internal/agentcore/llm"
	"clawbench/internal/agentcore/tools"
	"clawbench/internal/model"
)

// taskArgs is the argument blob every test hands to the task tool. The tool
// itself only needs a description (card label) and a prompt (the child's whole
// brief), so one constant keeps the tests focused on behavior.
const taskArgs = `{"description":"look","prompt":"find the file"}`

// reasoningToolRound is a child round that thinks, then calls one tool.
//
// The child's tool call is what puts an EventToolEnd on the wire, which is the
// point where the report accumulator must forget the earlier narration.
func reasoningToolRound(thinking, id, name, args string) func(llm.Request) []llm.Event {
	return func(llm.Request) []llm.Event {
		return []llm.Event{
			{Kind: llm.KindReasoning, Reasoning: thinking},
			{Kind: llm.KindToolCallStart, ToolCall: &llm.ToolCall{ID: id, Name: name}},
			{Kind: llm.KindToolCallDelta, ToolCall: &llm.ToolCall{Arguments: args}},
			{Kind: llm.KindDone},
		}
	}
}

// narrationToolRound is a child round that says something, then calls a tool.
// The text before the tool call is narration, not the report.
func narrationToolRound(text, id, name, args string) func(llm.Request) []llm.Event {
	return func(llm.Request) []llm.Event {
		return []llm.Event{
			{Kind: llm.KindText, Text: text},
			{Kind: llm.KindToolCallStart, ToolCall: &llm.ToolCall{ID: id, Name: name}},
			{Kind: llm.KindToolCallDelta, ToolCall: &llm.ToolCall{Arguments: args}},
			{Kind: llm.KindDone},
		}
	}
}

// collectAll drains a run and returns every event plus the terminal one.
func collectAll(t *testing.T, ch <-chan Event) ([]Event, Event) {
	t.Helper()
	var (
		events   []Event
		terminal Event
	)
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				if terminal.Kind == "" {
					t.Fatal("the run closed without a terminal event")
				}
				return events, terminal
			}
			events = append(events, ev)
			if ev.Kind == EventDone || ev.Kind == EventError {
				terminal = ev
			}
		case <-deadline:
			t.Fatal("the run did not finish")
		}
	}
}

// findEvent returns the first event matching want.
func findEvent(events []Event, want func(Event) bool) (Event, bool) {
	for _, ev := range events {
		if want(ev) {
			return ev, true
		}
	}
	return Event{}, false
}

// TestBuildTools_SubagentPresence pins the safety rule: the task tool exists
// only when there is something to delegate to and depth still allows it.
func TestBuildTools_SubagentPresence(t *testing.T) {
	t.Run("top-level with tools", func(t *testing.T) {
		r := &runner{spec: Spec{
			Client: &scriptedClient{},
			Policy: model.ToolPolicy{Allow: []string{model.CapFSRead}},
		}}
		r.buildTools()
		if r.toolIndex[taskToolName] == nil {
			t.Fatalf("expected %q in the tool list, got %v", taskToolName, r.availableToolNames())
		}
	})

	t.Run("at the depth limit", func(t *testing.T) {
		r := &runner{spec: Spec{
			Client: &scriptedClient{},
			Policy: model.ToolPolicy{Allow: []string{model.CapFSRead}},
			Depth:  maxSubagentDepth,
		}}
		r.buildTools()
		if r.toolIndex[taskToolName] != nil {
			t.Fatal("a sub-agent at the depth limit must not be able to delegate again")
		}
	})

	t.Run("without any capability", func(t *testing.T) {
		r := &runner{spec: Spec{Client: &scriptedClient{}}}
		r.buildTools()
		if r.toolIndex[taskToolName] != nil {
			t.Fatal("an agent with no tools has nothing to delegate to")
		}
	})
}

// TestSubagent_FreshContextAndNarrowedPolicy pins what the child sees: only
// the task prompt (no parent conversation), the parent's system prompt plus a
// sub-agent role, and the parent's already-narrowed tool grant.
func TestSubagent_FreshContextAndNarrowedPolicy(t *testing.T) {
	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{
		toolRound("call_task", taskToolName, taskArgs),
		textRound("child report"),
		textRound("parent answer"),
	}}
	terminal, _ := collect(t, Run(context.Background(), Spec{
		Client:    client,
		Prompt:    "hi",
		WorkDir:   t.TempDir(),
		SessionID: "s1",
		Policy:    model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}))
	if terminal.Kind != EventDone {
		t.Fatalf("expected done, got %v (%v)", terminal.Kind, terminal.Err)
	}

	child := client.request(1)
	if len(child.Messages) != 1 {
		t.Fatalf("the child must start with exactly the task prompt, got %d messages", len(child.Messages))
	}
	if child.Messages[0].Role != llm.RoleUser {
		t.Fatalf("the task prompt must be a user message, got %q", child.Messages[0].Role)
	}
	if got := child.Messages[0].Parts[0].Text; got != "find the file" {
		t.Fatalf("the child's prompt must be the task prompt, got %q", got)
	}
	if !strings.Contains(child.System, "<role>") || !strings.Contains(strings.ToLower(child.System), "sub-agent") {
		t.Fatalf("the child's system prompt must carry the sub-agent role, got %q", child.System)
	}
	names := toolNames(child.Tools)
	if !contains(names, "read") || !contains(names, "glob") || !contains(names, "grep") {
		t.Fatalf("the child must inherit fs.read tools, got %v", names)
	}
	for _, forbidden := range []string{"write", "edit", "bash", taskToolName} {
		if contains(names, forbidden) {
			t.Fatalf("the child must not gain %q, got %v", forbidden, names)
		}
	}
	if child.SessionID == "s1" || child.SessionID == "" {
		t.Fatalf("the child needs its own prompt-cache key, got %q", child.SessionID)
	}

	parent := client.request(2)
	if strings.Contains(parent.System, "<role>") {
		t.Fatal("the parent's system prompt must not become the sub-agent role")
	}
	if !hasToolResult(parent, "child report") {
		t.Fatalf("the parent must receive the child's report as a tool result, got %+v", parent.Messages)
	}
}

// TestSubagent_EventsAreAttributedAndTerminalDoesNotLeak pins the event shape
// the frontend groups by, and the rule that a child's terminal event must not
// end the parent's turn.
func TestSubagent_EventsAreAttributedAndTerminalDoesNotLeak(t *testing.T) {
	workDir := t.TempDir()
	writeFile(t, workDir, "hello")

	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{
		toolRound("call_task", taskToolName, taskArgs),
		reasoningToolRound("child thinking", "child_read", "read", `{"path":"a.txt"}`),
		textRound("child report"),
		textRound("parent answer"),
	}}
	events, terminal := collectAll(t, Run(context.Background(), Spec{
		Client:  client,
		Prompt:  "hi",
		WorkDir: workDir,
		Policy:  model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}))
	if terminal.Kind != EventDone {
		t.Fatalf("expected done, got %v (%v)", terminal.Kind, terminal.Err)
	}

	if ev, ok := findEvent(events, func(e Event) bool {
		return e.Kind == EventReasoning && e.Reasoning == "child thinking"
	}); !ok || ev.ParentToolCallID != "call_task" {
		t.Fatalf("child reasoning must carry the parent call id, got %+v", ev)
	}
	if ev, ok := findEvent(events, func(e Event) bool {
		return e.Kind == EventToolStart && e.ToolName == "read"
	}); !ok || ev.ParentToolCallID != "call_task" {
		t.Fatalf("child tool calls must carry the parent call id, got %+v", ev)
	}
	if ev, ok := findEvent(events, func(e Event) bool {
		return e.Kind == EventText && e.Text == "child report"
	}); !ok || ev.ParentToolCallID != "call_task" {
		t.Fatalf("child text must carry the parent call id, got %+v", ev)
	}
	if ev, ok := findEvent(events, func(e Event) bool {
		return e.Kind == EventReasoningDone
	}); !ok || ev.ParentToolCallID != "call_task" {
		t.Fatalf("the child's reasoning block must be closed with its parent id, got %+v", ev)
	}

	dones := 0
	for _, ev := range events {
		if ev.Kind == EventDone || ev.Kind == EventError {
			dones++
		}
	}
	if dones != 1 {
		t.Fatalf("exactly one terminal event must reach the parent, got %d", dones)
	}

	if ev, ok := findEvent(events, func(e Event) bool {
		return e.Kind == EventToolEnd && e.ToolName == taskToolName
	}); !ok || !strings.Contains(ev.ToolOutput, "child report") {
		t.Fatalf("the task result must carry the child's report, got %+v", ev)
	}

	// The child's round usage must be merged into the parent's total. The
	// child's text round contributes 10/5; the parent's contributes 10/5.
	if terminal.TotalUsage.InputTokens != 20 || terminal.TotalUsage.OutputTokens != 10 {
		t.Fatalf("child usage must be merged into the parent's totals, got %+v", terminal.TotalUsage)
	}
}

// TestSubagent_ReportStartsAfterTheLastToolCall pins what counts as the report:
// narration before a tool call is not the deliverable, and replaying it would
// give the parent a rambling tool result.
func TestSubagent_ReportStartsAfterTheLastToolCall(t *testing.T) {
	workDir := t.TempDir()
	writeFile(t, workDir, "hello")

	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{
		toolRound("call_task", taskToolName, taskArgs),
		narrationToolRound("let me look", "child_read", "read", `{"path":"a.txt"}`),
		textRound("final report"),
		textRound("parent answer"),
	}}
	events, terminal := collectAll(t, Run(context.Background(), Spec{
		Client:  client,
		Prompt:  "hi",
		WorkDir: workDir,
		Policy:  model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}))
	if terminal.Kind != EventDone {
		t.Fatalf("expected done, got %v (%v)", terminal.Kind, terminal.Err)
	}
	ev, ok := findEvent(events, func(e Event) bool {
		return e.Kind == EventToolEnd && e.ToolName == taskToolName
	})
	if !ok {
		t.Fatal("the task call must finish")
	}
	if ev.ToolOutput != "final report" {
		t.Fatalf("the report must start after the last tool call, got %q", ev.ToolOutput)
	}
}

// TestSubagent_SharesTheTurnBudget pins that a sub-agent cannot get a fresh
// budget: with MaxRounds=1 the parent's first round already spends it, so the
// child makes zero model calls and the turn ends on the cap.
func TestSubagent_SharesTheTurnBudget(t *testing.T) {
	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{
		toolRound("call_task", taskToolName, taskArgs),
		textRound("closing"),
	}}
	events, terminal := collectAll(t, Run(context.Background(), Spec{
		Client:  client,
		Prompt:  "hi",
		WorkDir: t.TempDir(),
		Policy:  model.ToolPolicy{Allow: []string{model.CapFSRead}, MaxRounds: 1},
	}))
	if terminal.Kind != EventDone {
		t.Fatalf("expected done, got %v (%v)", terminal.Kind, terminal.Err)
	}
	if terminal.StopReason != StopMaxRounds || terminal.Limit != 1 {
		t.Fatalf("expected the shared round cap to stop the turn, got %q limit=%d", terminal.StopReason, terminal.Limit)
	}
	// Parent round 1 + the parent's closing round. The child got no round of
	// its own because the budget was already spent.
	if client.roundCount() != 2 {
		t.Fatalf("expected 2 model calls (parent + closing), got %d", client.roundCount())
	}
	if ev, ok := findEvent(events, func(e Event) bool {
		return e.Kind == EventToolEnd && e.ToolName == taskToolName
	}); !ok || !ev.ToolIsError {
		t.Fatalf("a child that never ran must come back as an error result, got %+v", ev)
	}
}

// failingRoundClient makes one chosen round fail at the transport level, which
// is how a provider outage reaches the engine.
type failingRoundClient struct {
	*scriptedClient
	failAt int
	calls  int
}

func (c *failingRoundClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.Event, error) {
	idx := c.calls
	c.calls++
	if idx == c.failAt {
		return nil, errors.New("boom")
	}
	return c.scriptedClient.Stream(ctx, req)
}

// TestSubagent_FailureBecomesAToolError pins that one child failing does not
// fail the parent's turn: the model gets an error result and can recover.
func TestSubagent_FailureBecomesAToolError(t *testing.T) {
	client := &failingRoundClient{
		scriptedClient: &scriptedClient{rounds: []func(llm.Request) []llm.Event{
			toolRound("call_task", taskToolName, taskArgs),
			textRound("parent recovered"),
		}},
		failAt: 1, // the child's first (and only) model call
	}
	events, terminal := collectAll(t, Run(context.Background(), Spec{
		Client:  client,
		Prompt:  "hi",
		WorkDir: t.TempDir(),
		Policy:  model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}))
	if terminal.Kind != EventDone {
		t.Fatalf("a child failure must not end the parent's turn, got %v (%v)", terminal.Kind, terminal.Err)
	}
	ev, ok := findEvent(events, func(e Event) bool {
		return e.Kind == EventToolEnd && e.ToolName == taskToolName
	})
	if !ok || !ev.ToolIsError {
		t.Fatalf("the task result must be an error, got %+v", ev)
	}
	if !strings.Contains(ev.ToolOutput, "boom") {
		t.Fatalf("the error must reach the model, got %q", ev.ToolOutput)
	}
}

// cancellingClient cancels the run on the child's model call, simulating the
// user pressing stop while the sub-agent is working.
type cancellingClient struct {
	cancel context.CancelFunc
	calls  int
}

func (c *cancellingClient) Model() string        { return "stub" }
func (c *cancellingClient) SupportsVision() bool { return false }

func (c *cancellingClient) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	c.calls++
	ch := make(chan llm.Event, 4)
	if c.calls == 1 {
		ch <- llm.Event{Kind: llm.KindToolCallStart, ToolCall: &llm.ToolCall{ID: "call_task", Name: taskToolName}}
		ch <- llm.Event{Kind: llm.KindToolCallDelta, ToolCall: &llm.ToolCall{Arguments: taskArgs}}
		ch <- llm.Event{Kind: llm.KindDone}
		close(ch)
		return ch, nil
	}
	// The child's call: stop the run and leave the stream open. runModelRound
	// must notice ctx.Done rather than wait for a terminal event.
	c.cancel()
	return ch, nil
}

// TestSubagent_CancellationPropagates pins that stopping the parent stops the
// child instead of leaving it running until the provider answers.
func TestSubagent_CancellationPropagates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &cancellingClient{cancel: cancel}

	terminal, _ := collect(t, Run(ctx, Spec{
		Client:  client,
		Prompt:  "hi",
		WorkDir: t.TempDir(),
		Policy:  model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}))
	if terminal.Kind != EventError || !errors.Is(terminal.Err, context.Canceled) {
		t.Fatalf("expected a cancellation, got %v (%v)", terminal.Kind, terminal.Err)
	}
}

// TestRunSync_SubagentTextStaysOutOfTheAnswer pins the background-task path:
// with no UI to group a child's blocks, RunSync must still return only the
// parent's answer, not the child's narration.
func TestRunSync_SubagentTextStaysOutOfTheAnswer(t *testing.T) {
	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{
		toolRound("call_task", taskToolName, taskArgs),
		textRound("child report"),
		textRound("parent answer"),
	}}
	res, err := RunSync(context.Background(), Spec{
		Client:  client,
		Prompt:  "hi",
		WorkDir: t.TempDir(),
		Policy:  model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}, nil)
	if err != nil {
		t.Fatalf("RunSync failed: %v", err)
	}
	if res.Text != "parent answer" {
		t.Fatalf("the answer must contain only the parent's text, got %q", res.Text)
	}
}

// TestTurnBudget_SubagentCap pins the per-turn fan-out cap and the zero value
// that means "unlimited".
func TestTurnBudget_SubagentCap(t *testing.T) {
	const limit = 2
	b := NewTurnBudget()
	for i := 1; i <= limit; i++ {
		got, ok := b.TakeSubagent(limit)
		if !ok || got != i {
			t.Fatalf("sub-agent %d must be allowed with ordinal %d, got %d ok=%v", i, i, got, ok)
		}
	}
	if got, ok := b.TakeSubagent(limit); ok {
		t.Fatalf("the cap must refuse the next sub-agent, got ordinal %d", got)
	}

	unlimited := NewTurnBudget()
	for i := range 10 {
		if _, ok := unlimited.TakeSubagent(0); !ok {
			t.Fatalf("zero must mean unlimited, refused at %d", i+1)
		}
	}
}

// TestTaskTool_RejectsBadInput pins the three ways a delegation is refused
// before any model call: no arguments, malformed JSON, and an empty brief.
func TestTaskTool_RejectsBadInput(t *testing.T) {
	tool := &taskTool{r: &runner{spec: Spec{Budget: NewTurnBudget(), Client: &scriptedClient{}}}}
	for _, raw := range []string{"", "not json", `{"description":"x","prompt":"  "}`} {
		res := tool.Run(tools.Env{}, json.RawMessage(raw))
		if !res.IsError {
			t.Fatalf("input %q must be refused, got %+v", raw, res)
		}
	}
}

// TestTaskTool_RejectsOverFanOut pins the cap at the tool boundary: once the
// configured per-turn slots are gone, a delegation is refused without starting
// a child.
func TestTaskTool_RejectsOverFanOut(t *testing.T) {
	const limit = 2
	budget := NewTurnBudget()
	for i := range limit {
		if _, ok := budget.TakeSubagent(limit); !ok {
			t.Fatalf("slot %d must be available", i+1)
		}
	}
	tool := &taskTool{r: &runner{spec: Spec{
		Budget: budget,
		Client: &scriptedClient{},
		Policy: model.ToolPolicy{MaxSubagents: limit},
	}}}
	res := tool.Run(tools.Env{}, json.RawMessage(taskArgs))
	if !res.IsError || !strings.Contains(res.Text, "sub-agents") {
		t.Fatalf("expected a fan-out refusal, got %+v", res)
	}
}

// TestTruncateSubagentReport pins the cap that keeps one child from spending
// the parent's whole context on its own report.
func TestTruncateSubagentReport(t *testing.T) {
	long := strings.Repeat("a", MaxSubagentReportRunes+10)
	got := truncateSubagentReport(long)
	if !strings.Contains(got, "[The sub-agent's report was truncated.]") {
		t.Fatal("a truncated report must say so")
	}
	if len([]rune(got)) <= MaxSubagentReportRunes {
		t.Fatalf("the truncation note must be appended, got %d runes", len([]rune(got)))
	}
}

// hasToolResult reports whether a request carries a tool result containing
// want. It is how the tests assert what the PARENT model was actually shown.
func hasToolResult(req llm.Request, want string) bool {
	for _, msg := range req.Messages {
		for _, part := range msg.Parts {
			if part.ToolResult == nil {
				continue
			}
			for _, p := range part.ToolResult.Parts {
				if strings.Contains(p.Text, want) {
					return true
				}
			}
		}
	}
	return false
}
