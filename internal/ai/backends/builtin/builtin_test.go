package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

// TestTranslate_ToolArgsCarriesInputBeforeRun pins that the arguments reach the
// card while the tool is still executing: the start frame has none (they stream
// in afterwards) and the end frame arrives when the work is already done, so
// without this event a long-running bash call had no summary to show.
func TestTranslate_ToolArgsCarriesInputBeforeRun(t *testing.T) {
	tr := &translator{}
	got := tr.translate(agentcore.Event{
		Kind:       agentcore.EventToolArgs,
		ToolCallID: "c1",
		ToolName:   "bash",
		ToolInput:  `{"command":"npm test","description":"Run the tests"}`,
	})
	if len(got) != 1 || got[0].Tool == nil {
		t.Fatalf("expected one tool_use event, got %+v", got)
	}
	if got[0].Type != "tool_use" {
		t.Fatalf("event type = %q, want tool_use", got[0].Type)
	}
	if got[0].Tool.Done {
		t.Fatal("the args frame must not be marked done — the tool has not run yet")
	}
	if got[0].Tool.Input != `{"command":"npm test","description":"Run the tests"}` {
		t.Fatalf("input must be forwarded verbatim, got %q", got[0].Tool.Input)
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

// TestBuildSpec_RequestsAnOutputBudget pins the chat path's max_tokens.
//
// Without it the endpoint applies its own default, which is often smaller than
// one reasoning round — the reply then stops at the cap and looks finished.
// The background-task path has its own default (service.RunBuiltinAgentTask),
// so this covers the turn a user actually types into.
func TestBuildSpec_RequestsAnOutputBudget(t *testing.T) {
	if model.Agents == nil {
		model.Agents = map[string]*model.Agent{}
	}
	prevProvider := model.GetProvider(1)
	model.ReplaceProviders([]*model.Provider{{
		ID: 1, Name: "test", Protocol: model.ProviderProtocolOpenAI,
		BaseURL: "http://127.0.0.1:1", APIKey: "k", Models: []string{"test-model"}, DefaultModel: "test-model",
	}})
	t.Cleanup(func() {
		if prevProvider != nil {
			model.ReplaceProviders([]*model.Provider{prevProvider})
			return
		}
		model.ReplaceProviders(nil)
	})

	const id = "builtin-budget-test"
	pid := int64(1)
	model.Agents[id] = &model.Agent{
		ID: id, Backend: BackendID, BuiltinBackend: true,
		ProviderID: &pid, PreferredModel: "test-model",
		ToolPolicy: model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}
	defer delete(model.Agents, id)

	spec, err := buildSpec(context.Background(), ai.ChatRequest{AgentID: id, Prompt: "hi"})
	if err != nil {
		t.Fatalf("buildSpec: %v", err)
	}
	if spec.MaxOutputTokens <= 0 {
		t.Fatal("the built-in agent must request an explicit output budget, not the provider default")
	}
}

// TestOutputBudget pins the max_tokens the built-in agent requests.
//
// Leaving it unset (the old behaviour) hands the choice to the provider, whose
// default is often smaller than one reasoning round — the endpoint then cuts the
// reply off and the turn looks finished. Asking for more than the model supports
// is a hard API error, so a published limit always wins.
func TestOutputBudget(t *testing.T) {
	model.ReplaceCatalog(&model.Catalog{
		Providers: map[string]model.CatalogProvider{
			"p": {ID: "p", Models: []model.CatalogModel{
				{ID: "small-output", Output: 8192},
				{ID: "huge-output", Output: 384000},
			}},
		},
	})
	t.Cleanup(func() { model.ReplaceCatalog(nil) })

	if got := model.OutputTokenBudget("small-output"); got != 8192 {
		t.Fatalf("a published limit below the ceiling must be used as-is, got %d", got)
	}
	if got := model.OutputTokenBudget("huge-output"); got != model.MaxOutputTokenBudget {
		t.Fatalf("a huge published limit must be capped at the ceiling, got %d", got)
	}
	if got := model.OutputTokenBudget("never-heard-of-it"); got != model.MaxOutputTokenBudget {
		t.Fatalf("an unknown model must get the ceiling, got %d", got)
	}
	if got := model.OutputTokenBudget(""); got != model.MaxOutputTokenBudget {
		t.Fatalf("an empty model id must get the ceiling, got %d", got)
	}
}

// TestTranslate_BudgetStopAddsANotice pins the user-visible half of a budget
// stop: without a note, a turn that ran out of steps is indistinguishable from
// one the model finished, so the user reads it as the agent dying mid-task.
func TestTranslate_BudgetStopAddsANotice(t *testing.T) {
	tr := &translator{locale: "zh"}
	got := tr.translate(agentcore.Event{
		Kind: agentcore.EventDone, StopReason: agentcore.StopMaxRounds,
		// Rounds is one past the cap (the closing summary call); the notice
		// must name the configured limit, not that total.
		Rounds: 101, Limit: 100,
	})
	if len(got) != 2 {
		t.Fatalf("expected notice + done, got %+v", got)
	}
	if got[0].Type != "system_message" || got[0].SystemMessage == nil {
		t.Fatalf("expected a system message first, got %+v", got[0])
	}
	if !strings.Contains(got[0].SystemMessage.Content, "100") {
		t.Fatalf("the notice must name the limit that was hit, got %q", got[0].SystemMessage.Content)
	}
	if strings.Contains(got[0].SystemMessage.Content, "101") {
		t.Fatalf("the notice must not report the closing call as the limit, got %q", got[0].SystemMessage.Content)
	}
	if got[1].Type != "done" || got[1].Meta == nil || got[1].Meta.StopReason != agentcore.StopMaxRounds {
		t.Fatalf("the stop reason must still reach the ledger, got %+v", got[1])
	}
}

// TestTranslate_ToolCallBudgetNoticeNamesToolCalls pins that the two budgets
// are not reported as one generic "limit".
func TestTranslate_ToolCallBudgetNoticeNamesToolCalls(t *testing.T) {
	tr := &translator{locale: "en"}
	got := tr.translate(agentcore.Event{
		Kind: agentcore.EventDone, StopReason: agentcore.StopMaxToolCalls, ToolCalls: 300, Limit: 300,
	})
	if len(got) != 2 || got[0].SystemMessage == nil {
		t.Fatalf("expected notice + done, got %+v", got)
	}
	if !strings.Contains(got[0].SystemMessage.Content, "300") ||
		!strings.Contains(got[0].SystemMessage.Content, "tool calls") {
		t.Fatalf("the notice must name the tool-call budget, got %q", got[0].SystemMessage.Content)
	}
}

// TestTranslate_TruncatedAnswerAddsANotice pins that a reply cut off at the
// output cap is called out. The text the user sees just stops; without the note
// there is nothing to distinguish "the model finished" from "the model was cut
// off".
func TestTranslate_TruncatedAnswerAddsANotice(t *testing.T) {
	tr := &translator{locale: "zh"}
	got := tr.translate(agentcore.Event{
		Kind: agentcore.EventDone, StopReason: agentcore.StopComplete, Truncated: true,
	})
	if len(got) != 2 || got[0].Type != "system_message" || got[0].SystemMessage == nil {
		t.Fatalf("expected a notice + done, got %+v", got)
	}
	if !strings.Contains(got[0].SystemMessage.Content, "输出上限") {
		t.Fatalf("the notice must say why, got %q", got[0].SystemMessage.Content)
	}
	if got[1].Type != "done" {
		t.Fatalf("the done event must follow the notice, got %q", got[1].Type)
	}
}

// TestTranslate_TruncatedAndBudgetAddTwoNotices pins that the two facts are not
// collapsed into one: "you ran out of steps" and "the reply was cut off" are
// different, and a user acts on them differently.
func TestTranslate_TruncatedAndBudgetAddTwoNotices(t *testing.T) {
	tr := &translator{locale: "en"}
	got := tr.translate(agentcore.Event{
		Kind: agentcore.EventDone, StopReason: agentcore.StopMaxRounds, Limit: 100, Truncated: true,
	})
	if len(got) != 3 {
		t.Fatalf("expected two notices + done, got %d events", len(got))
	}
	for _, ev := range got[:2] {
		if ev.Type != "system_message" || ev.SystemMessage == nil {
			t.Fatalf("expected system messages first, got %+v", ev)
		}
	}
}

// TestTranslate_CompleteStopHasNoNotice pins that the normal ending stays quiet.
func TestTranslate_CompleteStopHasNoNotice(t *testing.T) {
	tr := &translator{locale: "zh"}
	got := tr.translate(agentcore.Event{Kind: agentcore.EventDone, StopReason: agentcore.StopComplete})
	if len(got) != 1 || got[0].Type != "done" {
		t.Fatalf("a normal completion must produce only the done event, got %+v", got)
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

// TestSpecNameIsTheProductName pins the display name.
//
// It is the product's name, not a description of the mechanism: this agent IS
// ClawBench running its own tool loop. "Builtin" described how it is
// implemented, which is not what a user is choosing between in a picker.
func TestSpecNameIsTheProductName(t *testing.T) {
	spec := model.FindSpecByBackend(BackendID)
	if spec == nil {
		t.Fatal("the backend must be registered")
	}
	if spec.Name != "ClawBench Agent" {
		t.Fatalf("expected the product name, got %q", spec.Name)
	}
}

// TestStaticCapability_OffersModesAndEfforts pins the option set the chat
// drawer and settings page read.
func TestStaticCapability_OffersModesAndEfforts(t *testing.T) {
	cap := staticCapability()

	var modeIDs []string
	for _, m := range cap.AvailableModes {
		modeIDs = append(modeIDs, m.ID)
	}
	if len(modeIDs) != 3 || modeIDs[0] != model.ModeRead || modeIDs[1] != model.ModeWrite || modeIDs[2] != model.ModeFull {
		t.Fatalf("modes = %v, want [read write full]", modeIDs)
	}

	var effortIDs []string
	for _, l := range cap.AvailableThinkingEfforts {
		effortIDs = append(effortIDs, l.ID)
	}
	if len(effortIDs) != 4 || effortIDs[0] != "auto" || effortIDs[3] != "high" {
		t.Fatalf("efforts = %v, want [auto low medium high]", effortIDs)
	}
}

// TestStaticCapability_ReachesRegistryForCopiedAgents pins the backend-keyed
// lookup: a copied agent has a new id but the same backend, and must still see
// the options without registering them again.
func TestStaticCapability_ReachesRegistryForCopiedAgents(t *testing.T) {
	const id = "builtin-copy-test"
	if model.Agents == nil {
		model.Agents = map[string]*model.Agent{}
	}
	model.Agents[id] = &model.Agent{ID: id, Backend: BackendID, BuiltinBackend: true}
	defer delete(model.Agents, id)

	reg := ai.GetAgentCapabilityRegistry()

	ms := reg.GetModeState(id, "")
	if ms == nil || len(ms.AvailableModes) != 3 {
		t.Fatalf("GetModeState = %+v, want three modes", ms)
	}
	// The empty current id is derived from the grant so a fresh session has a
	// selection instead of an unhighlighted list.
	if ms.CurrentModeID == "" {
		t.Fatal("a fresh session must report a default mode")
	}
	if !reg.IsModeAvailable(id, model.ModeRead) {
		t.Fatal("mode validation must accept the static list")
	}

	es := reg.GetThinkingEffortStateForModel(id, "any-model", "")
	if es == nil || len(es.AvailableLevels) != 4 {
		t.Fatalf("GetThinkingEffortStateForModel = %+v, want four levels", es)
	}
}
