package builtin

import (
	"testing"

	"clawbench/internal/agentcore"
)

// TestTranslate_SubagentContentCarriesParentID pins the attribution the
// frontend groups by: every block a child produces must name the task call that
// spawned it, on the event AND on the tool call.
func TestTranslate_SubagentContentCarriesParentID(t *testing.T) {
	const parent = "call_task"

	t.Run("content", func(t *testing.T) {
		got := (&translator{}).translate(agentcore.Event{
			Kind: agentcore.EventText, Text: "child report", ParentToolCallID: parent,
		})
		if len(got) != 1 || got[0].ParentToolCallID != parent {
			t.Fatalf("content must carry the parent id, got %+v", got)
		}
	})

	t.Run("thinking", func(t *testing.T) {
		got := (&translator{}).translate(agentcore.Event{
			Kind: agentcore.EventReasoning, Reasoning: "child thinking", ParentToolCallID: parent,
		})
		if len(got) != 1 || got[0].ParentToolCallID != parent {
			t.Fatalf("thinking must carry the parent id, got %+v", got)
		}
	})

	t.Run("tool", func(t *testing.T) {
		got := (&translator{}).translate(agentcore.Event{
			Kind: agentcore.EventToolEnd, ToolCallID: "c1", ToolName: "read", ParentToolCallID: parent,
		})
		if len(got) != 1 {
			t.Fatalf("expected one event, got %+v", got)
		}
		if got[0].ParentToolCallID != parent {
			t.Fatalf("the event must carry the parent id, got %+v", got[0])
		}
		if got[0].Tool == nil || got[0].Tool.ParentToolCallID != parent {
			t.Fatalf("the tool call must carry the parent id, got %+v", got[0].Tool)
		}
	})
}

// TestTranslate_ReasoningDoneClosesOnlyItsParent pins the rule that keeps one
// sub-agent's completion from marking another's block done.
func TestTranslate_ReasoningDoneClosesOnlyItsParent(t *testing.T) {
	tr := &translator{}

	tr.translate(agentcore.Event{Kind: agentcore.EventReasoning, Reasoning: "p1 thought", ParentToolCallID: "p1"})
	closed := tr.translate(agentcore.Event{Kind: agentcore.EventReasoningDone, ParentToolCallID: "p1"})
	if len(closed) != 1 || closed[0].Type != "thinking_done" || closed[0].ParentToolCallID != "p1" {
		t.Fatalf("expected a thinking_done for p1, got %+v", closed)
	}

	tr.translate(agentcore.Event{Kind: agentcore.EventReasoning, Reasoning: "p2 thought", ParentToolCallID: "p2"})
	// A late close for p1 must not touch p2's open block.
	if got := tr.translate(agentcore.Event{Kind: agentcore.EventReasoningDone, ParentToolCallID: "p1"}); len(got) != 0 {
		t.Fatalf("a close for another parent must be a no-op, got %+v", got)
	}
	got := tr.translate(agentcore.Event{Kind: agentcore.EventText, Text: "p2 answer", ParentToolCallID: "p2"})
	if len(got) != 2 || got[0].Type != "thinking_done" || got[0].ParentToolCallID != "p2" || got[1].Type != "content" {
		t.Fatalf("p2's own text must close p2 first, got %+v", got)
	}
}

// TestTranslate_NewParentClosesTheOldOne pins the interleaving case: a backend
// that starts another parent's reasoning without closing the previous block
// must not leave the first block spinning forever.
func TestTranslate_NewParentClosesTheOldOne(t *testing.T) {
	tr := &translator{}
	tr.translate(agentcore.Event{Kind: agentcore.EventReasoning, Reasoning: "p1", ParentToolCallID: "p1"})
	got := tr.translate(agentcore.Event{Kind: agentcore.EventReasoning, Reasoning: "p2", ParentToolCallID: "p2"})
	if len(got) != 2 || got[0].Type != "thinking_done" || got[0].ParentToolCallID != "p1" || got[1].Type != "thinking" {
		t.Fatalf("expected p1's close then p2's thinking, got %+v", got)
	}
}
