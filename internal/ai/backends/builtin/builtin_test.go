package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"clawbench/internal/agentcore"
	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// TestTranslate_TextIsContent pins the most common event.
func TestTranslate_TextIsContent(t *testing.T) {
	tr := &translator{}
	got := tr.translate(agentcore.Event{Kind: agentcore.EventText, Text: "hi"})
	if len(got) != 1 || got[0].Type != "content" || got[0].Content != "hi" {
		t.Fatalf("expected a content event, got %+v", got)
	}
}

// TestTranslate_ClosesReasoningBeforeText pins the defect this state machine
// exists to prevent.
//
// A text delta that follows reasoning without a thinking_done leaves the
// thinking block "in progress" forever: the UI renders a spinner with no body,
// and the periodic flush persists in_progress so a reload adopts it too.
func TestTranslate_ClosesReasoningBeforeText(t *testing.T) {
	tr := &translator{}
	first := tr.translate(agentcore.Event{Kind: agentcore.EventReasoning, Reasoning: "thinking..."})
	if len(first) != 1 || first[0].Type != "thinking" {
		t.Fatalf("expected a thinking event, got %+v", first)
	}

	second := tr.translate(agentcore.Event{Kind: agentcore.EventText, Text: "answer"})
	if len(second) != 2 {
		t.Fatalf("expected thinking_done + content, got %+v", second)
	}
	if second[0].Type != "thinking_done" {
		t.Fatalf("the reasoning block must be closed first, got %q", second[0].Type)
	}
	if second[1].Type != "content" {
		t.Fatalf("the content must follow the close, got %q", second[1].Type)
	}
}

// TestTranslate_ClosesReasoningBeforeDone pins the same rule at the end of a
// run: a turn that ends on reasoning would otherwise leave a spinner.
func TestTranslate_ClosesReasoningBeforeDone(t *testing.T) {
	tr := &translator{}
	tr.translate(agentcore.Event{Kind: agentcore.EventReasoning, Reasoning: "..."})
	got := tr.translate(agentcore.Event{Kind: agentcore.EventDone, StopReason: "complete"})
	if len(got) != 2 || got[0].Type != "thinking_done" || got[1].Type != "done" {
		t.Fatalf("expected thinking_done then done, got %+v", got)
	}
}

// TestTranslate_ToolEndCarriesInputOutputAndStatus pins the tool card's data.
func TestTranslate_ToolEndCarriesInputOutputAndStatus(t *testing.T) {
	tr := &translator{}
	got := tr.translate(agentcore.Event{
		Kind:        agentcore.EventToolEnd,
		ToolCallID:  "c1",
		ToolName:    "read",
		ToolInput:   `{"path":"a.txt"}`,
		ToolOutput:  "file body",
		ToolIsError: false,
	})
	if len(got) != 1 {
		t.Fatalf("expected one event, got %d", len(got))
	}
	tc := got[0].Tool
	if tc == nil {
		t.Fatal("a tool_use event must carry the tool call")
	}
	if tc.ID != "c1" || tc.Name != "read" {
		t.Fatalf("identity must be carried, got %+v", tc)
	}
	if tc.Input != `{"path":"a.txt"}` {
		t.Fatalf("the input must be forwarded verbatim, got %q", tc.Input)
	}
	if tc.Output != "file body" {
		t.Fatalf("the output must be forwarded, got %q", tc.Output)
	}
	if !tc.Done || tc.Status != "success" {
		t.Fatalf("a successful call must be marked done/success, got done=%v status=%q", tc.Done, tc.Status)
	}

	got = tr.translate(agentcore.Event{
		Kind: agentcore.EventToolEnd, ToolCallID: "c2", ToolName: "bash", ToolIsError: true,
	})
	if got[0].Tool.Status != "error" {
		t.Fatalf("a failed call must be marked error, got %q", got[0].Tool.Status)
	}
}

// TestTranslate_ToolStartIsNotDone pins that the opening frame must not claim
// the call finished, or the card renders as complete while it is still running.
func TestTranslate_ToolStartIsNotDone(t *testing.T) {
	tr := &translator{}
	got := tr.translate(agentcore.Event{Kind: agentcore.EventToolStart, ToolCallID: "c1", ToolName: "read"})
	if len(got) != 1 || got[0].Tool == nil {
		t.Fatalf("expected a tool_use event, got %+v", got)
	}
	if got[0].Tool.Done {
		t.Fatal("the start frame must not be marked done")
	}
}

// TestTranslate_InvalidToolInputIsStillReadable pins that a malformed argument
// blob does not silently disappear from the card.
func TestTranslate_InvalidToolInputIsStillReadable(t *testing.T) {
	tr := &translator{}
	got := tr.translate(agentcore.Event{
		Kind: agentcore.EventToolEnd, ToolCallID: "c1", ToolName: "read",
		ToolInput: "not json at all",
	})
	if got[0].Tool.Input == "" {
		t.Fatal("a malformed input must still be shown, not dropped")
	}
	if !json.Valid([]byte(got[0].Tool.Input)) {
		t.Fatal("the forwarded input must be valid JSON")
	}
}

// TestTranslate_CancellationIsNotAGenericFailure pins that pressing stop does
// not surface as "something went wrong".
func TestTranslate_CancellationIsNotAGenericFailure(t *testing.T) {
	tr := &translator{}
	got := tr.translate(agentcore.Event{Kind: agentcore.EventError, Err: context.Canceled})
	if len(got) != 1 || got[0].Type != "error" {
		t.Fatalf("expected an error event, got %+v", got)
	}
	if got[0].Reason != ai.ReasonUserCancel {
		t.Fatalf("a cancellation must be reported as user_cancel, got %q", got[0].Reason)
	}
}

func TestTranslate_RealErrorIsRequestFailed(t *testing.T) {
	tr := &translator{}
	got := tr.translate(agentcore.Event{Kind: agentcore.EventError, Err: errors.New("boom")})
	if got[0].Reason != ai.ReasonRequestFailed {
		t.Fatalf("a real error must be request_failed, got %q", got[0].Reason)
	}
	if got[0].Error != "boom" {
		t.Fatalf("the message must be carried, got %q", got[0].Error)
	}
}

// TestHistoryFor_ReplaysUserAndAssistantOnly pins what is replayed: text turns
// only, in order, with no tool transcripts.
func TestHistoryFor_ReplaysUserAndAssistantOnly(t *testing.T) {
	req := ai.ChatRequest{
		ForkContext: "[earlier]",
		History: []ai.HistoryMessage{
			{Role: "user", Text: "first"},
			{Role: "assistant", Text: "reply"},
			{Role: "user", Text: ""}, // empty turns are skipped
			{Role: "system", Text: "ignored"},
		},
	}
	got := historyFor(req)
	if len(got) != 3 {
		t.Fatalf("expected fork context + 2 turns, got %d", len(got))
	}
	if got[0].Parts[0].Text != "[earlier]" {
		t.Fatalf("fork context must lead, got %q", got[0].Parts[0].Text)
	}
	if got[1].Role != "user" || got[2].Role != "assistant" {
		t.Fatalf("roles must be preserved in order, got %v / %v", got[1].Role, got[2].Role)
	}
}

// TestHistoryFor_EmptyIsNil pins that a brand-new session replays nothing.
func TestHistoryFor_EmptyIsNil(t *testing.T) {
	if got := historyFor(ai.ChatRequest{}); got != nil {
		t.Fatalf("expected no history, got %+v", got)
	}
}

// TestBackendName pins the backend id, which is also the agent id a fresh
// install auto-inserts.
func TestBackendName(t *testing.T) {
	if got := (&Backend{}).Name(); got != BackendID {
		t.Fatalf("expected %q, got %q", BackendID, got)
	}
	if BackendID != "builtin" {
		t.Fatal("the backend id is a persisted value (agent ids, backend column); changing it orphans existing rows")
	}
}

// TestSpecIsBuiltinAndNoCLI pins the registration flags the UI depends on.
func TestSpecIsBuiltinAndNoCLI(t *testing.T) {
	spec := model.FindSpecByBackend(BackendID)
	if spec == nil {
		t.Fatal("the backend must be registered")
	}
	if !spec.Builtin {
		t.Fatal("Builtin must be set: the UI hides the CLI command field, MCP section and install command based on it")
	}
	if !spec.NoCLI {
		t.Fatal("NoCLI must be set: there is no binary to probe, so detection must not require one")
	}
	if spec.AcpCommand != "" {
		t.Fatal("this backend must never spawn a process, so it must not advertise ACP")
	}
	if spec.AutoLoadsNativeSkills {
		t.Fatal("the backend has no skill directory of its own; claiming it self-loads would leave it with no skills at all")
	}
}
