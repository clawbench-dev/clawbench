package agentcore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"clawbench/internal/agentcore/llm"
	"clawbench/internal/agentcore/tools"
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

// TestRun_DoneReportsContextUsage pins the terminal numbers the chat UI's
// context panel reads: used = the conversation's estimated size, window = the
// model's catalog window. The estimate must track the transcript, not be a
// constant — the panel's bar is what the user sees fill up.
func TestRun_DoneReportsContextUsage(t *testing.T) {
	run := func(reply string) Event {
		t.Helper()
		client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{textRound(reply)}}
		terminal, _ := collect(t, Run(context.Background(), Spec{
			Client: client, Prompt: "hi", WorkDir: t.TempDir(),
			ContextWindow: 100000,
		}))
		if terminal.Kind != EventDone {
			t.Fatalf("expected done, got %v (%v)", terminal.Kind, terminal.Err)
		}
		return terminal
	}

	short := run("hello")
	if short.WindowTokens != 100000 {
		t.Fatalf("the window must come from the spec, got %d", short.WindowTokens)
	}
	if short.ContextTokens <= 0 {
		t.Fatalf("a known window must report used tokens, got %d", short.ContextTokens)
	}

	long := run(strings.Repeat("word ", 2000))
	if long.ContextTokens <= short.ContextTokens {
		t.Fatalf("a longer transcript must estimate larger: short=%d long=%d",
			short.ContextTokens, long.ContextTokens)
	}
}

// TestRun_UnknownWindowReportsNoContextUsage pins the deliberate zero: a 0/0
// pair would render as an empty bar at 0%, which reads as "nothing used"
// rather than "not measurable".
func TestRun_UnknownWindowReportsNoContextUsage(t *testing.T) {
	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{textRound("hello")}}
	terminal, _ := collect(t, Run(context.Background(), Spec{
		Client: client, Prompt: "hi", WorkDir: t.TempDir(),
	}))
	if terminal.ContextTokens != 0 || terminal.WindowTokens != 0 {
		t.Fatalf("an unknown window must report no usage, got %d/%d",
			terminal.ContextTokens, terminal.WindowTokens)
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
	writeFile(t, root, "file body")

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
	// Three budgeted rounds plus ONE closing round: the run must spend a final
	// call explaining where the work stands instead of ending on a tool result.
	if got := client.roundCount(); got != 4 {
		t.Fatalf("expected 3 budgeted rounds + 1 closing round, got %d calls", got)
	}
	if terminal.Rounds != 4 {
		t.Fatalf("the totals must include the closing call, got %d", terminal.Rounds)
	}
	if terminal.Limit != 3 {
		t.Fatalf("the limit must be the configured cap, not the closing call, got %d", terminal.Limit)
	}
	closing := client.request(3)
	if len(closing.Tools) != 0 {
		t.Fatalf("the closing round must be called without tools, got %d", len(closing.Tools))
	}
	last := closing.Messages[len(closing.Messages)-1]
	if last.Role != llm.RoleUser || !strings.Contains(last.Parts[0].Text, "used up this turn") {
		t.Fatalf("the closing round must be asked for a hand-off, got %+v", last)
	}
}

// TestRun_NoBudgetMeansNoCeiling pins the default: a policy that sets neither
// budget runs until the model stops on its own. The old builds substituted a
// fixed number here (24, then 100), which cut long tasks off mid-way; this test
// runs past the last of those numbers so a default cannot quietly come back.
func TestRun_NoBudgetMeansNoCeiling(t *testing.T) {
	const rounds = 120 // more than the old 100-round default
	script := make([]func(llm.Request) []llm.Event, 0, rounds+1)
	for range rounds {
		script = append(script, toolRound("c", "glob", `{"pattern":"*.go"}`))
	}
	script = append(script, textRound("done"))
	client := &scriptedClient{rounds: script}

	terminal, _ := collect(t, Run(context.Background(), Spec{
		Client: client, Prompt: "loop", WorkDir: t.TempDir(),
		Policy: model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}))

	if terminal.Kind != EventDone || terminal.StopReason != StopComplete {
		t.Fatalf("an unset budget must run to completion, got %v/%q", terminal.Kind, terminal.StopReason)
	}
	if got := client.roundCount(); got != rounds+1 {
		t.Fatalf("expected %d calls (no ceiling), got %d", rounds+1, got)
	}
	if terminal.Rounds != rounds+1 {
		t.Fatalf("expected all %d rounds to be counted, got %d", rounds+1, terminal.Rounds)
	}
}

// TestRun_ToolArgsArriveBeforeTheToolRuns pins that the chat card can name what
// is running: the arguments are announced as soon as they are complete, BEFORE
// the tool executes. Only announcing them on the result (or not at all) left a
// long bash call as a bare tool name for its whole duration.
func TestRun_ToolArgsArriveBeforeTheToolRuns(t *testing.T) {
	rounds := []func(llm.Request) []llm.Event{
		toolRound("c1", "glob", `{"pattern":"*.go"}`),
		textRound("done"),
	}
	client := &scriptedClient{rounds: rounds}

	var (
		order []EventKind
		args  Event
	)
	for ev := range Run(context.Background(), Spec{
		Client: client, Prompt: "go", WorkDir: t.TempDir(),
		Policy: model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}) {
		switch ev.Kind {
		case EventToolStart, EventToolArgs, EventToolEnd:
			order = append(order, ev.Kind)
			if ev.Kind == EventToolArgs {
				args = ev
			}
		}
	}

	want := []EventKind{EventToolStart, EventToolArgs, EventToolEnd}
	if len(order) != len(want) {
		t.Fatalf("tool events = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("tool events = %v, want %v", order, want)
		}
	}
	if args.ToolCallID != "c1" {
		t.Fatalf("args event must identify the call, got %q", args.ToolCallID)
	}
	if args.ToolInput != `{"pattern":"*.go"}` {
		t.Fatalf("args event must carry the final arguments, got %q", args.ToolInput)
	}
}

// TestRun_ClosingRoundTextIsStreamed pins the user-visible half of the budget
// stop: whatever the closing round says must reach the transcript, and a tool
// call it emits anyway must not open a card that can never finish.
func TestRun_ClosingRoundTextIsStreamed(t *testing.T) {
	rounds := []func(llm.Request) []llm.Event{
		toolRound("c1", "glob", `{"pattern":"*.go"}`),
		// The closing round: text plus a stray tool call (a provider that
		// ignores the empty tool list).
		func(llm.Request) []llm.Event {
			return []llm.Event{
				{Kind: llm.KindText, Text: "已定位到配置面板，还差保存逻辑。"},
				{Kind: llm.KindToolCallStart, ToolCall: &llm.ToolCall{ID: "stray", Name: "glob"}},
				{Kind: llm.KindDone},
			}
		},
	}
	client := &scriptedClient{rounds: rounds}

	var (
		terminal Event
		text     strings.Builder
		starts   int
	)
	for ev := range Run(context.Background(), Spec{
		Client: client, Prompt: "go", WorkDir: t.TempDir(),
		Policy: model.ToolPolicy{Allow: []string{model.CapFSRead}, MaxRounds: 1},
	}) {
		switch ev.Kind {
		case EventText:
			text.WriteString(ev.Text)
		case EventToolStart:
			starts++
		case EventDone:
			terminal = ev
		}
	}

	if terminal.StopReason != StopMaxRounds {
		t.Fatalf("expected max_rounds, got %q", terminal.StopReason)
	}
	if !strings.Contains(text.String(), "还差保存逻辑") {
		t.Fatalf("the closing text must be streamed to the user, got %q", text.String())
	}
	if starts != 1 {
		t.Fatalf("a stray tool call in the closing round must not be announced, got %d starts", starts)
	}
}

// TestRun_EmptyRoundIsNotAnAnswer pins the other half of "the turn ended with
// nothing in it": a round that produced only reasoning used to be accepted as
// the final answer. It must be treated as a stall and continued.
func TestRun_EmptyRoundIsNotAnAnswer(t *testing.T) {
	rounds := []func(llm.Request) []llm.Event{
		func(llm.Request) []llm.Event {
			return []llm.Event{
				{Kind: llm.KindReasoning, Reasoning: "I should look at the config panel."},
				{Kind: llm.KindDone},
			}
		},
		textRound("配置面板在 settings 里，已加上开关。"),
	}
	client := &scriptedClient{rounds: rounds}

	var (
		terminal Event
		text     strings.Builder
	)
	for ev := range Run(context.Background(), Spec{
		Client: client, Prompt: "go", WorkDir: t.TempDir(),
		Policy: model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}) {
		if ev.Kind == EventText {
			text.WriteString(ev.Text)
		}
		if ev.Kind == EventDone {
			terminal = ev
		}
	}

	if client.roundCount() != 2 {
		t.Fatalf("an empty round must be followed by a continuation round, got %d calls", client.roundCount())
	}
	if !strings.Contains(text.String(), "已加上开关") {
		t.Fatalf("the answer after the nudge must reach the user, got %q", text.String())
	}
	if terminal.StopReason != StopComplete {
		t.Fatalf("expected a normal completion after the nudge, got %q", terminal.StopReason)
	}
	second := client.request(1)
	last := second.Messages[len(second.Messages)-1]
	if last.Role != llm.RoleUser || !strings.Contains(last.Parts[0].Text, "no message to the user") {
		t.Fatalf("the continuation round must be asked to continue, got %+v", last)
	}
}

// truncatedRound answers with text that the endpoint cut off at its output cap.
func truncatedRound(text string) func(llm.Request) []llm.Event {
	return func(llm.Request) []llm.Event {
		return []llm.Event{
			{Kind: llm.KindText, Text: text},
			{Kind: llm.KindDone, Truncated: true},
		}
	}
}

// TestRun_TruncatedReplyIsContinued pins that a reply cut off at the output cap
// is not accepted as the answer. The old behavior ended the turn mid-sentence
// and looked exactly like a normal finish.
func TestRun_TruncatedReplyIsContinued(t *testing.T) {
	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{
		truncatedRound("配置面板在 settings 里，"),
		textRound("保存逻辑在 useSettingsConfig。"),
	}}

	var (
		terminal Event
		text     strings.Builder
	)
	for ev := range Run(context.Background(), Spec{
		Client: client, Prompt: "go", WorkDir: t.TempDir(),
		Policy: model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}) {
		if ev.Kind == EventText {
			text.WriteString(ev.Text)
		}
		if ev.Kind == EventDone {
			terminal = ev
		}
	}

	if client.roundCount() != 2 {
		t.Fatalf("a truncated reply must be continued, got %d calls", client.roundCount())
	}
	if !strings.Contains(text.String(), "保存逻辑") {
		t.Fatalf("the continuation must reach the user, got %q", text.String())
	}
	if terminal.Truncated {
		t.Fatal("the run finished on an untruncated round, so it must not be marked truncated")
	}
	second := client.request(1)
	last := second.Messages[len(second.Messages)-1]
	if last.Role != llm.RoleUser || !strings.Contains(last.Parts[0].Text, "cut off") {
		t.Fatalf("the continuation round must be asked to carry on, got %+v", last)
	}
}

// TestRun_TruncationContinuationsAreBounded pins the cap: an endpoint whose
// output limit is far too small must not turn the whole round budget into
// continuations, and the terminal event must say the answer is a prefix.
func TestRun_TruncationContinuationsAreBounded(t *testing.T) {
	rounds := make([]func(llm.Request) []llm.Event, 10)
	for i := range rounds {
		rounds[i] = truncatedRound("partial ")
	}
	client := &scriptedClient{rounds: rounds}

	terminal, _ := collect(t, Run(context.Background(), Spec{
		Client: client, Prompt: "go", WorkDir: t.TempDir(),
		Policy: model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}))

	if got := client.roundCount(); got != maxTruncationContinuations+1 {
		t.Fatalf("expected 1 round + %d continuations, got %d", maxTruncationContinuations, got)
	}
	if terminal.Kind != EventDone {
		t.Fatalf("a truncated answer is still a done turn, got %v", terminal.Kind)
	}
	if !terminal.Truncated {
		t.Fatal("the terminal event must report that the answer is incomplete")
	}
}

// TestRun_CancelDuringClosingRoundIsACancellation pins that pressing stop while
// the closing summary is running reports a cancellation, not a completed turn.
func TestRun_CancelDuringClosingRoundIsACancellation(t *testing.T) {
	rounds := []func(llm.Request) []llm.Event{
		toolRound("c1", "glob", `{"pattern":"*.go"}`),
		func(llm.Request) []llm.Event {
			// The closing round is still in flight when the context ends.
			time.Sleep(500 * time.Millisecond)
			return []llm.Event{{Kind: llm.KindText, Text: "too late"}}
		},
	}
	client := &scriptedClient{rounds: rounds}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	terminal, _ := collect(t, Run(ctx, Spec{
		Client: client, Prompt: "go", WorkDir: t.TempDir(),
		Policy: model.ToolPolicy{Allow: []string{model.CapFSRead}, MaxRounds: 1},
	}))
	if terminal.Kind != EventError || !errors.Is(terminal.Err, context.Canceled) {
		t.Fatalf("expected a cancellation, got %v (%v)", terminal.Kind, terminal.Err)
	}
}

// TestRun_EmptyRoundNudgeIsBounded pins the cap: an endpoint that keeps
// returning nothing must not be nudged forever.
func TestRun_EmptyRoundNudgeIsBounded(t *testing.T) {
	empty := func(llm.Request) []llm.Event {
		return []llm.Event{{Kind: llm.KindDone}}
	}
	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{empty, empty, empty, empty}}

	terminal, _ := collect(t, Run(context.Background(), Spec{
		Client: client, Prompt: "go", WorkDir: t.TempDir(),
		Policy: model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}))

	if got := client.roundCount(); got != maxEmptyRoundNudges+1 {
		t.Fatalf("expected %d calls (one nudge, then stop), got %d", maxEmptyRoundNudges+1, got)
	}
	if terminal.Kind != EventDone || terminal.StopReason != StopComplete {
		t.Fatalf("a persistently empty model must still end cleanly, got %v/%q", terminal.Kind, terminal.StopReason)
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
	writeFile(t, root, "x")

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
	got := buildSystemPrompt("You are helpful.", "", "/tmp/x", nil, model.ToolPolicy{})
	for _, want := range []string{
		"You are helpful.",
		"/tmp/x",
		"cannot read files",
		"run shell commands",
		"write or modify files",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("system prompt must contain %q, got:\n%s", want, got)
		}
	}
}

// TestBuildSystemPrompt_UsesPiStyleSections pins the prompt's shape.
//
// Sections are wrapped rather than flattened into prose because a model reads a
// tagged list as rules to follow and the same text as prose as something to
// summarize. The preamble stays unwrapped and first: it is the agent's own
// instructions and must outrank the mechanical text below it.
func TestBuildSystemPrompt_UsesPiStyleSections(t *testing.T) {
	list := tools.All(true, false, false)
	got := buildSystemPrompt("AGENT-INSTRUCTIONS", "", "/tmp/x", list, model.ToolPolicy{Allow: []string{model.CapFSRead}})

	if !strings.HasPrefix(got, "AGENT-INSTRUCTIONS") {
		t.Fatalf("the agent's own prompt must lead and be unwrapped, got:\n%s", got)
	}
	for _, tag := range []string{"<tools>", "</tools>", "<rules>", "</rules>", "<environment>", "</environment>"} {
		if !strings.Contains(got, tag) {
			t.Fatalf("prompt must contain section tag %q, got:\n%s", tag, got)
		}
	}
	// Order matters: tools before rules before environment.
	// The Contains loop above proved every index is >= 0, so the comparison
	// below is about order and nothing else.
	toolsAt, rulesAt, envAt := strings.Index(got, "<tools>"), strings.Index(got, "<rules>"), strings.Index(got, "<environment>")
	if toolsAt > rulesAt || rulesAt > envAt {
		t.Fatalf("sections are out of order, got:\n%s", got)
	}
}

// TestBuildSystemPrompt_ToolListCarriesPurpose pins that each tool is described,
// not merely named.
//
// A bare list of names tells the model nothing about WHICH tool to reach for, so
// it shells out to bash for work a dedicated tool does better — and the whole
// point of granting read/glob/grep separately is that they are better at it.
func TestBuildSystemPrompt_ToolListCarriesPurpose(t *testing.T) {
	list := tools.All(true, true, false)
	got := buildSystemPrompt("", "", "/tmp/x", list, model.ToolPolicy{Allow: []string{model.CapFSRead, model.CapFSWrite}})

	for _, t2 := range list {
		snippet := t2.Snippet()
		if snippet == "" {
			continue
		}
		want := "- " + t2.Name() + ": " + snippet
		if !strings.Contains(got, want) {
			t.Fatalf("the tool list must describe %q, got:\n%s", t2.Name(), got)
		}
	}
}

// TestBuildSystemPrompt_OnlyGrantedToolsAreListed pins that a denied capability
// produces no mention of the tool — not even in the rules.
//
// The tool's own guidelines would otherwise leak the existence of a tool the
// model cannot call, which invites it to try. This caught a real case: read's
// guidelines said "read before editing", which a read-only agent has no use for
// and which points at a tool that is not installed.
func TestBuildSystemPrompt_OnlyGrantedToolsAreListed(t *testing.T) {
	// Read-only: the write/edit/bash guidelines must not appear, and neither
	// may any rule that only makes sense with them.
	list := tools.All(true, false, false)
	got := buildSystemPrompt("", "", "/tmp/x", list, model.ToolPolicy{Allow: []string{model.CapFSRead}})

	for _, forbidden := range []string{
		"Use write only for new files",
		"Use edit for targeted changes",
		"Use bash for commands",
		"old_string",
		"Read the file first",
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("a read-only prompt must not carry advice for an ungranted tool (%q), got:\n%s", forbidden, got)
		}
	}
}

// TestBuildSystemPrompt_OmitsDenialsWhenGranted pins that a fully granted agent
// is not told about restrictions it does not have.
func TestBuildSystemPrompt_OmitsDenialsWhenGranted(t *testing.T) {
	policy := model.ToolPolicy{Allow: []string{model.CapExec}}.Normalized()
	got := buildSystemPrompt("", "", "/tmp/x", nil, policy)
	if strings.Contains(got, "You cannot") {
		t.Fatalf("a fully granted agent must not be told it is restricted, got:\n%s", got)
	}
}

// TestBuildSystemPrompt_NoWorkDirIsStated pins the empty-workdir case: the model
// must know the file tools are unusable rather than trying them and failing.
func TestBuildSystemPrompt_NoWorkDirIsStated(t *testing.T) {
	got := buildSystemPrompt("", "", "", nil, model.ToolPolicy{})
	if !strings.Contains(got, "No working directory") {
		t.Fatalf("an empty workdir must be stated, got:\n%s", got)
	}
}

// TestLoadProjectRules_Missing pins the no-file case: an absent AGENTS.md must
// yield nothing rather than an error or a stray heading.
//
// Also covers the two shapes that are NOT "rules": no working directory at all,
// and a directory that happens to be named AGENTS.md.
func TestLoadProjectRules_Missing(t *testing.T) {
	dir := t.TempDir()

	if got := loadProjectRules(""); got != "" {
		t.Fatalf("no workdir must yield no rules, got %q", got)
	}
	if got := loadProjectRules(dir); got != "" {
		t.Fatalf("no AGENTS.md must yield no rules, got %q", got)
	}

	// A directory named AGENTS.md is not a rules file. Without the IsDir check
	// os.ReadFile fails anyway, but on some systems it returns something
	// non-obvious, and a silent "read the directory" is worse than a skip.
	if err := os.Mkdir(filepath.Join(dir, ProjectRulesFile), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if got := loadProjectRules(dir); got != "" {
		t.Fatalf("a directory named %s must yield no rules, got %q", ProjectRulesFile, got)
	}

	// Empty and whitespace-only files are also "no rules" — injecting the
	// heading over an empty body would tell the model it has rules it does not.
	if err := os.Remove(filepath.Join(dir, ProjectRulesFile)); err != nil {
		t.Fatalf("rmdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ProjectRulesFile), []byte("  \n\t\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := loadProjectRules(dir); got != "" {
		t.Fatalf("a whitespace-only file must yield no rules, got %q", got)
	}
}

// TestLoadProjectRules_ReadsFile pins that the file's text reaches the prompt,
// headed so the model can tell project rules from its own instructions.
func TestLoadProjectRules_ReadsFile(t *testing.T) {
	dir := t.TempDir()
	body := "Compile the frontend after touching web/src.\nNever commit secrets."
	if err := os.WriteFile(filepath.Join(dir, ProjectRulesFile), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	rules := loadProjectRules(dir)
	if !strings.Contains(rules, "Compile the frontend after touching web/src.") {
		t.Fatalf("the file's text must be loaded, got %q", rules)
	}

	got := buildSystemPrompt("AGENT-INSTRUCTIONS", rules, dir, tools.All(true, false, false),
		model.ToolPolicy{Allow: []string{model.CapFSRead}})
	// The agent's own instructions still lead the prompt.
	if !strings.HasPrefix(got, "AGENT-INSTRUCTIONS") {
		t.Fatalf("the agent's own prompt must still lead, got:\n%s", got)
	}
	// The rules are attributed, not dropped in bare.
	if !strings.Contains(got, "defines the following rules in its AGENTS.md") {
		t.Fatalf("project rules must be introduced as such, got:\n%s", got)
	}
	// They sit above the mechanical sections but below the agent's prompt.
	agentAt := strings.Index(got, "AGENT-INSTRUCTIONS")
	rulesAt := strings.Index(got, "Never commit secrets.")
	toolsAt := strings.Index(got, "<tools>")
	if !(agentAt < rulesAt && rulesAt < toolsAt) {
		t.Fatalf("project rules must sit between the agent prompt and the tool list, got:\n%s", got)
	}
}

// TestLoadProjectRules_Truncates pins the size cap.
//
// The cut must be announced: a silently truncated rules file reads to the model
// as a complete one, so it would follow a half-written rule and never know the
// rest existed.
func TestLoadProjectRules_Truncates(t *testing.T) {
	dir := t.TempDir()

	// One distinctive line at the very top (must survive), then enough filler to
	// cross the cap with a marker that must NOT appear.
	const head = "RULE-AT-THE-TOP\n"
	body := head + strings.Repeat("x", MaxProjectRulesBytes) + "\nRULE-BEYOND-THE-CAP\n"
	if err := os.WriteFile(filepath.Join(dir, ProjectRulesFile), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	rules := loadProjectRules(dir)

	if !strings.Contains(rules, "RULE-AT-THE-TOP") {
		t.Fatalf("the head of the file must survive the cap, got %d bytes", len(rules))
	}
	if strings.Contains(rules, "RULE-BEYOND-THE-CAP") {
		t.Fatal("content past the cap must not be injected")
	}
	if !strings.Contains(rules, "was not included") {
		t.Fatalf("the cut must be announced to the model, got:\n%s", rules[len(rules)-300:])
	}
	// The injected text stays near the cap — the announcement is small.
	if len(rules) > MaxProjectRulesBytes+512 {
		t.Fatalf("injected rules overshoot the cap badly: %d bytes", len(rules))
	}
}

// TestJoinWithOr pins the denial phrasing helper.
func TestJoinWithOr(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"a"}, "a"},
		{[]string{"a", "b"}, "a or b"},
		{[]string{"a", "b", "c"}, "a, b or c"},
	}
	for _, c := range cases {
		if got := joinWithOr(c.in); got != c.want {
			t.Errorf("joinWithOr(%v) = %q, want %q", c.in, got, c.want)
		}
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

// panickingClient fails the way a broken tool or a malformed provider response
// would: a panic inside the run loop.
type panickingClient struct{}

func (panickingClient) Model() string        { return "stub" }
func (panickingClient) SupportsVision() bool { return false }
func (panickingClient) Stream(context.Context, llm.Request) (<-chan llm.Event, error) {
	panic("boom")
}

// TestRun_RecoversPanicAsTerminalError pins the blast-radius guard: the built-in
// agent runs inside the ClawBench process, so an unrecovered panic here would
// take the whole server (and every other session) down. It must instead surface
// as this run's terminal error, exactly once.
func TestRun_RecoversPanicAsTerminalError(t *testing.T) {
	terminal, kinds := collect(t, Run(context.Background(), Spec{Client: panickingClient{}}))

	if terminal.Kind != EventError {
		t.Fatalf("a panic must end the run with an error, got %v", terminal.Kind)
	}
	if terminal.Err == nil || !strings.Contains(terminal.Err.Error(), "internal error") {
		t.Fatalf("the panic must be reported as an internal error, got %v", terminal.Err)
	}
	terminals := 0
	for _, k := range kinds {
		if k == EventDone || k == EventError {
			terminals++
		}
	}
	if terminals != 1 {
		t.Fatalf("expected exactly one terminal event, got %d (%v)", terminals, kinds)
	}
}

// TestRun_ReplaysThinkingWithToolCalls pins the round-trip that extended
// thinking requires: the reasoning block (and its signature) must travel back
// with the tool call it justified. Dropping it makes Anthropic reject every
// round after the first, so a thinking level would break the agent loop.
func TestRun_ReplaysThinkingWithToolCalls(t *testing.T) {
	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{
		func(llm.Request) []llm.Event {
			return []llm.Event{
				{Kind: llm.KindReasoning, Reasoning: "need the file"},
				{Kind: llm.KindReasoningSignature, Signature: "sig-1"},
				{Kind: llm.KindToolCallStart, ToolCall: &llm.ToolCall{ID: "c1", Name: "read"}},
				{Kind: llm.KindToolCallDelta, ToolCall: &llm.ToolCall{Arguments: `{"path":"a.txt"}`}},
				{Kind: llm.KindDone},
			}
		},
		textRound("done"),
	}}

	spec := Spec{
		Client:  client,
		Prompt:  "hi",
		WorkDir: t.TempDir(),
		Policy:  model.ToolPolicy{Allow: []string{model.CapFSRead}}.Normalized(),
	}
	collect(t, Run(context.Background(), spec))

	if client.roundCount() < 2 {
		t.Fatalf("expected a second round, got %d", client.roundCount())
	}
	req := client.request(1)
	var found *llm.Thinking
	for _, m := range req.Messages {
		if m.Role != llm.RoleAssistant {
			continue
		}
		for _, p := range m.Parts {
			if p.Thinking != nil {
				found = p.Thinking
			}
		}
	}
	if found == nil {
		t.Fatal("the reasoning block must be replayed in the assistant turn")
	}
	if found.Text != "need the file" || found.Signature != "sig-1" {
		t.Fatalf("thinking = %+v, want text and signature preserved", *found)
	}
}
