package agentcore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"clawbench/internal/agentcore/llm"
	"clawbench/internal/model"
)

func boundaryIDs(events []Event) []string {
	var ids []string
	for _, ev := range events {
		if ev.Kind == EventInjected {
			ids = append(ids, ev.ClientUserMessageID)
		}
	}
	return ids
}

// lastMessageText returns the text of the conversation's final message, which
// is what the next model round will see immediately before the prompt.
func lastMessageText(req llm.Request) string {
	if len(req.Messages) == 0 {
		return ""
	}
	var b strings.Builder
	for _, p := range req.Messages[len(req.Messages)-1].Parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

// TestMailbox_TakeOrSealIsAtomic pins the property the whole design rests on:
// a message is either taken (so it gets a round) or the mailbox is sealed (so
// the producer falls back to the queue). It can never be both accepted and
// dropped.
func TestMailbox_TakeOrSealIsAtomic(t *testing.T) {
	box := NewInjectionMailbox()
	if !box.Push(PendingInjection{ClientUserMessageID: "q1", Text: "hello"}) {
		t.Fatal("an open mailbox must accept a message")
	}
	pending, sealed := box.TakeOrSeal()
	if sealed || len(pending) != 1 || pending[0].ClientUserMessageID != "q1" {
		t.Fatalf("expected the pending message and an open mailbox, got %+v sealed=%v", pending, sealed)
	}
	if _, sealed := box.TakeOrSeal(); !sealed {
		t.Fatal("an empty mailbox must seal when the turn tries to end")
	}
	if box.Push(PendingInjection{ClientUserMessageID: "q2", Text: "late"}) {
		t.Fatal("a sealed mailbox must refuse new messages")
	}
}

// TestRun_InjectsAtToolBoundary pins the normal path: a message that arrives
// during a tool round is appended after the tool results and answered by the
// next model round, with a boundary event at that exact point.
func TestRun_InjectsAtToolBoundary(t *testing.T) {
	box := NewInjectionMailbox()
	root := t.TempDir()
	writeFile(t, root, "file body")

	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{
		func(req llm.Request) []llm.Event {
			if !box.Push(PendingInjection{ClientUserMessageID: "q-1", Text: "also look at b.txt"}) {
				t.Error("the mailbox should be open during the first round")
			}
			return toolRound("c1", "read", `{"path":"a.txt"}`)(req)
		},
		textRound("done"),
	}}

	events, _ := collectAll(t, Run(context.Background(), Spec{
		Client: client, Prompt: "hi", WorkDir: root,
		Policy:     model.ToolPolicy{Allow: []string{model.CapFSRead}},
		Injections: box,
	}))

	if ids := boundaryIDs(events); len(ids) != 1 || ids[0] != "q-1" {
		t.Fatalf("expected exactly one boundary for q-1, got %v", ids)
	}
	if client.roundCount() != 2 {
		t.Fatalf("expected 2 rounds, got %d", client.roundCount())
	}
	if got := lastMessageText(client.request(1)); got != "also look at b.txt" {
		t.Fatalf("the second round must see the injected message last, got %q", got)
	}
	// The boundary must precede the second round's text in the event stream.
	boundaryAt, textAt := -1, -1
	for i, ev := range events {
		if ev.Kind == EventInjected {
			boundaryAt = i
		}
		if ev.Kind == EventText && ev.Text == "done" {
			textAt = i
		}
	}
	if boundaryAt < 0 || textAt < 0 || boundaryAt > textAt {
		t.Fatalf("the boundary must come before the answer it splits: boundary=%d text=%d", boundaryAt, textAt)
	}
}

// TestRun_InjectionAfterFinalAnswerKeepsTurnAlive is the case that makes
// "insert" honest: the model already produced a complete answer when the
// message arrived, so the turn runs one more round for it instead of ending
// with the message unanswered.
func TestRun_InjectionAfterFinalAnswerKeepsTurnAlive(t *testing.T) {
	box := NewInjectionMailbox()
	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{
		func(req llm.Request) []llm.Event {
			// Arrives while the final answer is streaming.
			if !box.Push(PendingInjection{ClientUserMessageID: "q-late", Text: "one more thing"}) {
				t.Error("the mailbox should still be open during the last round")
			}
			return textRound("first half")(req)
		},
		textRound("second half"),
	}}

	events, terminal := collectAll(t, Run(context.Background(), Spec{
		Client: client, Prompt: "hi", WorkDir: t.TempDir(),
		Injections: box,
	}))

	if ids := boundaryIDs(events); len(ids) != 1 || ids[0] != "q-late" {
		t.Fatalf("expected the late message to produce a boundary, got %v", ids)
	}
	if client.roundCount() != 2 {
		t.Fatalf("the injected message must earn a round, got %d rounds", client.roundCount())
	}
	if got := lastMessageText(client.request(1)); got != "one more thing" {
		t.Fatalf("the second round must answer the injected message, got %q", got)
	}
	if terminal.Kind != EventDone || terminal.StopReason != StopComplete {
		t.Fatalf("the turn must still end normally, got %+v", terminal)
	}
}

// TestRun_SealsMailboxWhenTurnEnds verifies the cleanup path: after a normal
// completion the producer is told to keep the message queued.
func TestRun_SealsMailboxWhenTurnEnds(t *testing.T) {
	box := NewInjectionMailbox()
	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{textRound("hello")}}
	collectAll(t, Run(context.Background(), Spec{
		Client: client, Prompt: "hi", WorkDir: t.TempDir(), Injections: box,
	}))
	if box.Push(PendingInjection{ClientUserMessageID: "q-after", Text: "too late"}) {
		t.Fatal("a finished turn must refuse a message instead of accepting it silently")
	}
}

// TestRun_CancelSealsMailbox keeps the cancellation path from accepting a
// message it will never read.
func TestRun_CancelSealsMailbox(t *testing.T) {
	box := NewInjectionMailbox()
	started := make(chan struct{})
	release := make(chan struct{})
	client := &scriptedClient{rounds: []func(llm.Request) []llm.Event{
		func(req llm.Request) []llm.Event {
			box.Push(PendingInjection{ClientUserMessageID: "q-cancel", Text: "never seen"})
			close(started)
			<-release
			return textRound("unreachable")(req)
		},
	}}

	ctx, cancel := context.WithCancel(context.Background())
	ch := Run(ctx, Spec{Client: client, Prompt: "hi", WorkDir: t.TempDir(), Injections: box})
	<-started
	cancel()
	close(release)
	_, terminal := collectAll(t, ch)
	if terminal.Kind != EventError || !errors.Is(terminal.Err, context.Canceled) {
		t.Fatalf("expected a cancellation, got %+v", terminal)
	}
	if box.Push(PendingInjection{ClientUserMessageID: "q-after", Text: "too late"}) {
		t.Fatal("a cancelled turn must refuse a message instead of accepting it silently")
	}
}

// TestChildSpec_DropsParentInjections guards the copy in childSpec: a sub-agent
// runs its own loop, so the parent's mailbox must not travel with it.
func TestChildSpec_DropsParentInjections(t *testing.T) {
	box := NewInjectionMailbox()
	tool := &taskTool{r: &runner{spec: Spec{Injections: box, Policy: model.ToolPolicy{}}}}
	child := tool.childSpec("do something", 1)
	if child.Injections != nil {
		t.Fatal("a sub-agent must not inherit the parent's injection mailbox")
	}
}
