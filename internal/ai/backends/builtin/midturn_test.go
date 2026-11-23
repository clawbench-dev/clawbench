package builtin

import (
	"context"
	"testing"

	"clawbench/internal/agentcore"
	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// TestTranslate_InjectedIsSteerBoundary pins the event that lets the service
// layer split the reply at the exact insertion point.
func TestTranslate_InjectedIsSteerBoundary(t *testing.T) {
	tr := &translator{}
	got := tr.translate(agentcore.Event{Kind: agentcore.EventInjected, ClientUserMessageID: "q-1"})
	if len(got) != 1 || got[0].Type != "steer_boundary" {
		t.Fatalf("expected one steer_boundary event, got %+v", got)
	}
	if got[0].SteerBoundary == nil || got[0].SteerBoundary.ClientUserMessageID != "q-1" {
		t.Fatalf("the boundary must carry the queue id, got %+v", got[0].SteerBoundary)
	}
}

// TestTranslate_InjectedClosesReasoning keeps a thinking block that belongs to
// the "before" half from spinning on into the "after" half.
func TestTranslate_InjectedClosesReasoning(t *testing.T) {
	tr := &translator{}
	tr.translate(agentcore.Event{Kind: agentcore.EventReasoning, Reasoning: "thinking"})
	got := tr.translate(agentcore.Event{Kind: agentcore.EventInjected, ClientUserMessageID: "q-1"})
	if len(got) != 2 || got[0].Type != "thinking_done" || got[1].Type != "steer_boundary" {
		t.Fatalf("expected thinking_done then steer_boundary, got %+v", got)
	}
}

// TestInjectInProcess_RoutesToRunningTurn is the core routing check: the policy
// finds the session's mailbox and leaves the message there for the engine.
func TestInjectInProcess_RoutesToRunningTurn(t *testing.T) {
	box := agentcore.NewInjectionMailbox()
	release := registerInjectionBox("sid-running", box)
	defer release()

	res, err := midTurn{}.InjectInProcess(context.Background(), ai.MidTurnInjectRequest{
		SessionID:           "sid-running",
		Content:             "also check b.txt",
		ClientUserMessageID: "q-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Injected {
		t.Fatalf("a running turn must accept the message, got %+v", res)
	}
	pending, ok := box.Pop()
	if !ok || pending.ClientUserMessageID != "q-1" || pending.Text != "also check b.txt" {
		t.Fatalf("the message did not reach the mailbox: %+v ok=%v", pending, ok)
	}
}

// TestInjectInProcess_NoRunningTurn reports a benign decline so the caller
// keeps the message queued.
func TestInjectInProcess_NoRunningTurn(t *testing.T) {
	res, err := midTurn{}.InjectInProcess(context.Background(), ai.MidTurnInjectRequest{
		SessionID: "sid-idle", Content: "hello", ClientUserMessageID: "q-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Injected || res.Reason != "idle" {
		t.Fatalf("expected an idle decline, got %+v", res)
	}
}

// TestInjectInProcess_SealedMailboxDeclines covers the race the mailbox exists
// for: the turn is finishing, so the message must stay queued.
func TestInjectInProcess_SealedMailboxDeclines(t *testing.T) {
	box := agentcore.NewInjectionMailbox()
	release := registerInjectionBox("sid-closing", box)
	defer release()
	box.Seal()

	res, err := midTurn{}.InjectInProcess(context.Background(), ai.MidTurnInjectRequest{
		SessionID: "sid-closing", Content: "hello", ClientUserMessageID: "q-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Injected {
		t.Fatal("a sealed mailbox must not accept a message")
	}
}

// TestRegisterInjectionBox_OldTurnCannotRemoveNewTurn pins the compare-and-
// delete: a retry that registered its own mailbox first must survive the older
// turn's cleanup.
func TestRegisterInjectionBox_OldTurnCannotRemoveNewTurn(t *testing.T) {
	oldBox := agentcore.NewInjectionMailbox()
	releaseOld := registerInjectionBox("sid-retry", oldBox)
	newBox := agentcore.NewInjectionMailbox()
	releaseNew := registerInjectionBox("sid-retry", newBox)
	defer releaseNew()

	releaseOld()
	if got := injectionBoxFor("sid-retry"); got != newBox {
		t.Fatal("the older turn's cleanup removed the newer turn's mailbox")
	}
}

// TestCapabilities_BuiltinSupportsMidTurn pins the capability the chat UI reads
// to decide between "insert into the reply" and "interrupt".
func TestCapabilities_BuiltinSupportsMidTurn(t *testing.T) {
	if !model.BackendSupportsMidTurn(BackendID) {
		t.Fatal("the built-in agent must report mid-turn support")
	}
	if !model.BackendMidTurnSplitsReply(BackendID) {
		t.Fatal("the built-in agent splits the reply at the insertion point")
	}
}

// TestInjectMidTurn_ReachesTheInProcessPolicy goes through the REAL wiring:
// ai.InjectMidTurn resolves the backend's policy through the lookup that
// backends.init installs, then the policy finds the session's mailbox. Without
// this, the two halves could each pass their unit tests while the production
// path silently declines every insertion.
func TestInjectMidTurn_ReachesTheInProcessPolicy(t *testing.T) {
	if ai.LookupInProcessMidTurnInjectorFn == nil {
		t.Fatal("the in-process mid-turn lookup must be wired by backends.init")
	}
	if ai.LookupInProcessMidTurnInjectorFn(BackendID) == nil {
		t.Fatal("the built-in backend must expose its in-process policy")
	}

	box := agentcore.NewInjectionMailbox()
	release := registerInjectionBox("sid-wired", box)
	defer release()

	res := ai.InjectMidTurn(context.Background(), BackendID, "sid-wired", "agent-1", "hello", "q-wired")
	if !res.Injected {
		t.Fatalf("the wired lookup must reach the running turn, got %+v", res)
	}
	pending, ok := box.Pop()
	if !ok || pending.ClientUserMessageID != "q-wired" {
		t.Fatalf("the message did not reach the mailbox: %+v ok=%v", pending, ok)
	}
}
