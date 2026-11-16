package agentcore

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"clawbench/internal/agentcore/llm"
	"clawbench/internal/model"
)

// summaryClient is a stub that answers text rounds AND records summary calls.
//
// A summary request is distinguishable by its system prompt (the compaction
// instruction), which lets one stub serve both roles the way the real client
// does.
type summaryClient struct {
	mu sync.Mutex

	rounds []func(llm.Request) []llm.Event
	// summaryReplies are handed out in order to compaction calls; the last one
	// repeats.
	summaryReplies []string
	summaryInputs  []string
	roundRequests  []llm.Request
}

func (c *summaryClient) Model() string        { return "stub" }
func (c *summaryClient) SupportsVision() bool { return false }

func (c *summaryClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.Event, error) {
	c.mu.Lock()
	isSummary := req.System != "" && strings.Contains(req.System, "compacting a conversation")
	if isSummary {
		input := ""
		if len(req.Messages) > 0 && len(req.Messages[0].Parts) > 0 {
			input = req.Messages[0].Parts[0].Text
		}
		idx := len(c.summaryInputs)
		c.summaryInputs = append(c.summaryInputs, input)
		reply := "SUMMARY"
		if len(c.summaryReplies) > 0 {
			if idx < len(c.summaryReplies) {
				reply = c.summaryReplies[idx]
			} else {
				reply = c.summaryReplies[len(c.summaryReplies)-1]
			}
		}
		c.mu.Unlock()
		return closedStream(reply), nil
	}

	idx := len(c.roundRequests)
	c.roundRequests = append(c.roundRequests, req)
	var fn func(llm.Request) []llm.Event
	if idx < len(c.rounds) {
		fn = c.rounds[idx]
	}
	c.mu.Unlock()

	ch := make(chan llm.Event, 16)
	go func() {
		defer close(ch)
		if fn == nil {
			ch <- llm.Event{Kind: llm.KindDone}
			return
		}
		for _, ev := range fn(req) {
			ch <- ev
		}
	}()
	return ch, nil
}

// closedStream returns a channel carrying one text reply and a done event.
func closedStream(text string) <-chan llm.Event {
	ch := make(chan llm.Event, 4)
	ch <- llm.Event{Kind: llm.KindText, Text: text}
	ch <- llm.Event{Kind: llm.KindDone}
	close(ch)
	return ch
}

func (c *summaryClient) summaryCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.summaryInputs)
}

// withSummary wires a client's own summary path as the run's summarizer.
//
// Without it the engine falls back to DigestSummary (a deterministic digest
// used when no summarizer is configured), which is correct behaviour but does
// not exercise the model-summary path these tests are about.
func withSummary(c llm.Client) func(context.Context, string) (string, error) {
	return func(ctx context.Context, text string) (string, error) {
		stream, err := c.Stream(ctx, llm.Request{
			System:   "You are compacting a conversation so it can continue.",
			Messages: []llm.Message{llm.TextMessage(llm.RoleUser, text)},
		})
		if err != nil {
			return "", err
		}
		var b strings.Builder
		for ev := range stream {
			if ev.Kind == llm.KindText {
				b.WriteString(ev.Text)
			}
		}
		return b.String(), nil
	}
}

// manyTurns builds a history of n exchanges, each large enough to matter.
func manyTurns(n int, size int) []llm.Message {
	body := strings.Repeat("word ", size/5)
	var out []llm.Message
	for i := 0; i < n; i++ {
		out = append(out, llm.TextMessage(llm.RoleUser, "question "+body))
		out = append(out, llm.TextMessage(llm.RoleAssistant, "answer "+body))
	}
	return out
}

// TestRun_CompactsWhenHistoryOverflows pins the core behaviour: a conversation
// that does not fit is summarized rather than truncated.
//
// Before this, an overflowing history was silently cut from the front — the
// agent simply forgot what was said. The summary is what makes a long session
// continue instead of restart.
func TestRun_CompactsWhenHistoryOverflows(t *testing.T) {
	client := &summaryClient{
		rounds:         []func(llm.Request) []llm.Event{textRound("done")},
		summaryReplies: []string{"the user asked about the config file"},
	}

	var compacted []Event
	_, _ = RunSync(context.Background(), Spec{
		Client:        client,
		Prompt:        "and now?",
		History:       manyTurns(20, 2000),
		WorkDir:       t.TempDir(),
		Policy:        model.ToolPolicy{Allow: []string{model.CapFSRead}},
		ContextWindow: 8000,
		Summarize:     withSummary(client),
	}, func(ev Event) {
		if ev.Kind == EventCompacted {
			compacted = append(compacted, ev)
		}
	})

	if client.summaryCount() == 0 {
		t.Fatal("an overflowing history must be summarized")
	}
	if len(compacted) == 0 {
		t.Fatal("compaction must be reported so the user can see it")
	}
	ev := compacted[0]
	if ev.Summary != "the user asked about the config file" {
		t.Fatalf("the summary must be carried, got %q", ev.Summary)
	}
	if ev.Dropped == 0 {
		t.Fatal("the report must say how many messages were folded")
	}
	if ev.WindowTokens != 8000 {
		t.Fatalf("the report must carry the window, got %d", ev.WindowTokens)
	}
}

// TestRun_KeepsRecentTurnsVerbatim pins the "summary + recent" shape.
//
// A summary alone loses the detail of what is being discussed RIGHT NOW, which
// is exactly what the next reply depends on.
func TestRun_KeepsRecentTurnsVerbatim(t *testing.T) {
	client := &summaryClient{
		rounds:         []func(llm.Request) []llm.Event{textRound("done")},
		summaryReplies: []string{"older stuff"},
	}

	history := manyTurns(20, 2000)
	history = append(history, llm.TextMessage(llm.RoleUser, "UNIQUE-RECENT-MARKER"))

	_, _ = RunSync(context.Background(), Spec{
		Client:        client,
		Prompt:        "go on",
		History:       history,
		WorkDir:       t.TempDir(),
		Policy:        model.ToolPolicy{Allow: []string{model.CapFSRead}},
		ContextWindow: 8000,
		Summarize:     withSummary(client),
	}, nil)

	// The model's request must still contain the recent marker verbatim.
	req := client.roundRequests[len(client.roundRequests)-1]
	found := false
	for _, m := range req.Messages {
		for _, p := range m.Parts {
			if strings.Contains(p.Text, "UNIQUE-RECENT-MARKER") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("the most recent turns must be kept verbatim, not summarized away")
	}
}

// TestRun_PriorSummaryIsReused pins that a session compacts ONCE.
//
// Without this, every turn would summarize the same history again: one extra
// model call per turn, and each summary summarizes the previous one, so the
// facts degrade with every turn.
func TestRun_PriorSummaryIsReused(t *testing.T) {
	client := &summaryClient{
		rounds: []func(llm.Request) []llm.Event{textRound("ok")},
	}

	_, _ = RunSync(context.Background(), Spec{
		Client:        client,
		Prompt:        "next question",
		History:       manyTurns(2, 100),
		WorkDir:       t.TempDir(),
		Policy:        model.ToolPolicy{Allow: []string{model.CapFSRead}},
		ContextWindow: 100000, // plenty of room
		PriorSummary:  "PRIOR-SUMMARY-TEXT",
	}, nil)

	if client.summaryCount() != 0 {
		t.Fatal("a conversation that already fits must not be re-summarized")
	}
	// The prior summary must lead the conversation.
	req := client.roundRequests[0]
	if len(req.Messages) == 0 || !strings.Contains(req.Messages[0].Parts[0].Text, "PRIOR-SUMMARY-TEXT") {
		t.Fatal("the stored summary must be replayed first")
	}
}

// TestRun_NoWindowMeansNoCompaction pins the unknown-model behaviour.
func TestRun_NoWindowMeansNoCompaction(t *testing.T) {
	client := &summaryClient{rounds: []func(llm.Request) []llm.Event{textRound("ok")}}

	_, _ = RunSync(context.Background(), Spec{
		Client:  client,
		Prompt:  "hi",
		History: manyTurns(20, 2000),
		WorkDir: t.TempDir(),
		Policy:  model.ToolPolicy{Allow: []string{model.CapFSRead}},
		// ContextWindow deliberately 0 (unknown).
	}, nil)

	if client.summaryCount() != 0 {
		t.Fatal("an unknown window must not trigger compaction")
	}
}

// TestRun_CompactsWithinOneTurn pins the second trigger: a single turn whose
// tool results overflow the window on their own.
//
// This is the case a turn-boundary-only design misses entirely: one read of a
// large file, repeated a few rounds, exceeds any window without the
// conversation ever ending.
func TestRun_CompactsWithinOneTurn(t *testing.T) {
	big := strings.Repeat("x", 40000) // ~10k tokens per result
	client := &summaryClient{
		rounds: []func(llm.Request) []llm.Event{
			toolRound("c1", "read", `{"path":"a.txt"}`),
			toolRound("c2", "read", `{"path":"a.txt"}`),
			toolRound("c3", "read", `{"path":"a.txt"}`),
			textRound("finally"),
		},
		summaryReplies: []string{"read a large file three times"},
	}

	root := t.TempDir()
	writeFile(t, root, "a.txt", big)

	terminal, kinds := collect(t, Run(context.Background(), Spec{
		Client:        client,
		Prompt:        "read it a few times",
		WorkDir:       root,
		Policy:        model.ToolPolicy{Allow: []string{model.CapFSRead}},
		ContextWindow: 20000,
		Summarize:     withSummary(client),
	}))

	if terminal.Kind != EventDone {
		t.Fatalf("the run must survive compaction, got %v (%v)", terminal.Kind, terminal.Err)
	}
	if client.summaryCount() == 0 {
		t.Fatal("a turn whose tool results overflow the window must be compacted mid-turn")
	}
	if !containsKind(kinds, EventCompacted) {
		t.Fatal("the mid-turn compaction must be reported")
	}
	// The run must still finish normally rather than being cut short.
	if terminal.StopReason != StopComplete {
		t.Fatalf("expected a normal completion, got %q", terminal.StopReason)
	}
}

// TestRun_CompactionFailureIsNotFatal pins that a summarizer error degrades the
// turn instead of ending it.
//
// Refusing to continue because a SUMMARY failed turns a degraded answer into no
// answer at all.
func TestRun_CompactionFailureIsNotFatal(t *testing.T) {
	client := &summaryClient{rounds: []func(llm.Request) []llm.Event{textRound("still answered")}}

	_, err := RunSync(context.Background(), Spec{
		Client:        client,
		Prompt:        "hi",
		History:       manyTurns(20, 2000),
		WorkDir:       t.TempDir(),
		Policy:        model.ToolPolicy{Allow: []string{model.CapFSRead}},
		ContextWindow: 8000,
		Summarize: func(context.Context, string) (string, error) {
			return "", errors.New("summarizer exploded")
		},
	}, nil)

	if err != nil {
		t.Fatalf("a summarizer failure must not fail the turn: %v", err)
	}
	if len(client.roundRequests) == 0 {
		t.Fatal("the model must still be called")
	}
}

// TestRun_CompactionReturnsSummaryOnDone pins that the final summary rides the
// done event, which is what the caller persists across the turn boundary.
func TestRun_CompactionReturnsSummaryOnDone(t *testing.T) {
	client := &summaryClient{
		rounds:         []func(llm.Request) []llm.Event{textRound("ok")},
		summaryReplies: []string{"THE-SUMMARY"},
	}

	var terminal Event
	for ev := range Run(context.Background(), Spec{
		Client:        client,
		Prompt:        "hi",
		History:       manyTurns(20, 2000),
		WorkDir:       t.TempDir(),
		Policy:        model.ToolPolicy{Allow: []string{model.CapFSRead}},
		ContextWindow: 8000,
		Summarize:     withSummary(client),
	}) {
		if ev.Kind == EventDone {
			terminal = ev
		}
	}

	if terminal.Summary != "THE-SUMMARY" {
		t.Fatalf("the done event must carry the summary for persistence, got %q", terminal.Summary)
	}
}

// TestRun_EmptySummaryDoesNotDropHistory pins the guard against a summarizer
// that answers with nothing.
//
// Replacing the conversation with an empty summary would delete the context
// entirely — far worse than not compacting at all.
func TestRun_EmptySummaryDoesNotDropHistory(t *testing.T) {
	client := &summaryClient{
		rounds:         []func(llm.Request) []llm.Event{textRound("ok")},
		summaryReplies: []string{"   "},
	}

	_, _ = RunSync(context.Background(), Spec{
		Client:        client,
		Prompt:        "hi",
		History:       manyTurns(20, 2000),
		WorkDir:       t.TempDir(),
		Policy:        model.ToolPolicy{Allow: []string{model.CapFSRead}},
		ContextWindow: 8000,
		Summarize:     withSummary(client),
	}, nil)

	req := client.roundRequests[len(client.roundRequests)-1]
	if len(req.Messages) < 10 {
		t.Fatalf("an empty summary must not discard the conversation, kept only %d messages", len(req.Messages))
	}
}
