package ai

import (
	"context"
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

// schema 1.32 replaced the legacy `plan` with typed plan operations. The
// checklist form must render exactly like the legacy shape.
func TestMapACPSessionUpdate_PlanUpdateVariant(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	update := acp.SessionUpdate{
		PlanUpdate: &acp.SessionPlanUpdate{
			Plan: acp.PlanUpdateContent{
				Items: &acp.PlanUpdateContentItems{
					Entries: []acp.PlanEntry{
						{Content: "Read files", Priority: acp.PlanEntryPriorityHigh, Status: acp.PlanEntryStatusCompleted},
						{Content: "Write code", Priority: acp.PlanEntryPriorityMedium, Status: acp.PlanEntryStatusInProgress},
					},
				},
			},
		},
	}

	mapACPSessionUpdate(update, ch, context.Background(), nil, nil)

	events := drainACPEvents(ch, 1)
	if len(events) != 1 || events[0].Type != "plan_update" || events[0].Plan == nil {
		t.Fatalf("events = %+v, want one plan_update with a plan", events)
	}
	if len(events[0].Plan.Entries) != 2 {
		t.Fatalf("entries = %+v, want 2", events[0].Plan.Entries)
	}
	if events[0].Plan.Entries[0].Content != "Read files" ||
		events[0].Plan.Entries[0].Status != "completed" {
		t.Fatalf("first entry = %+v", events[0].Plan.Entries[0])
	}
	assertNoMoreACPEvents(ch, t)
}

// plan_removed clears the panel; the frontend's replace semantics need an
// explicit empty snapshot, not silence.
func TestMapACPSessionUpdate_PlanRemoved(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	update := acp.SessionUpdate{
		PlanRemoved: &acp.SessionUpdatePlanRemoved{PlanId: "plan"},
	}

	mapACPSessionUpdate(update, ch, context.Background(), nil, nil)

	events := drainACPEvents(ch, 1)
	if len(events) != 1 || events[0].Type != "plan_update" || events[0].Plan == nil {
		t.Fatalf("events = %+v, want one empty plan_update", events)
	}
	if len(events[0].Plan.Entries) != 0 {
		t.Fatalf("entries = %+v, want none", events[0].Plan.Entries)
	}
	assertNoMoreACPEvents(ch, t)
}

// A markdown plan has no checklist entries. The panel only renders entries, so
// it must not fabricate a one-item list from the markdown body.
func TestMapACPSessionUpdate_MarkdownPlanIsIgnored(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	update := acp.SessionUpdate{
		PlanUpdate: &acp.SessionPlanUpdate{
			Plan: acp.PlanUpdateContent{
				Markdown: &acp.PlanUpdateContentMarkdown{
					Content: "# Approved plan\n\nDo the thing.",
				},
			},
		},
	}

	mapACPSessionUpdate(update, ch, context.Background(), nil, nil)
	assertNoMoreACPEvents(ch, t)
}

// A subagent's checklist is a separate plan ID in schema 1.32. The single-panel
// UI must not let it replace or clear the conversation's main plan.
func TestMapACPSessionUpdate_AuxiliaryPlanIgnored(t *testing.T) {
	ch := make(chan StreamEvent, 10)

	update := acp.SessionUpdate{
		PlanUpdate: &acp.SessionPlanUpdate{
			Plan: acp.PlanUpdateContent{
				Items: &acp.PlanUpdateContentItems{
					PlanId:  "plan:tool-1",
					Entries: []acp.PlanEntry{{Content: "subagent step", Status: acp.PlanEntryStatusInProgress}},
				},
			},
		},
	}
	mapACPSessionUpdate(update, ch, context.Background(), nil, nil)
	assertNoMoreACPEvents(ch, t)

	removed := acp.SessionUpdate{
		PlanRemoved: &acp.SessionUpdatePlanRemoved{PlanId: "plan:tool-1"},
	}
	mapACPSessionUpdate(removed, ch, context.Background(), nil, nil)
	assertNoMoreACPEvents(ch, t)
}
