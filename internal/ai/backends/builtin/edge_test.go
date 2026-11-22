package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawbench/internal/agentcore"
	"clawbench/internal/agentcore/llm"
	"clawbench/internal/agentcore/tools"
	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// These tests cover the wiring around the translator: the request a turn is
// built from, the summarizer the compaction path calls, and the error branches
// of ExecuteStream. The translation table itself is pinned in builtin_test.go.

// withProvider installs one OpenAI-compatible provider for the duration of a
// test and registers an agent bound to it.
func withProvider(t *testing.T, baseURL string) string {
	t.Helper()
	prev := model.GetProvider(70)
	model.ReplaceProviders([]*model.Provider{{
		ID: 70, Name: "edge-test", Protocol: model.ProviderProtocolOpenAI,
		BaseURL: baseURL, APIKey: "k",
		Models: []string{"edge-model"}, DefaultModel: "edge-model",
	}})
	t.Cleanup(func() {
		if prev != nil {
			model.ReplaceProviders([]*model.Provider{prev})
			return
		}
		model.ReplaceProviders(nil)
	})

	if model.Agents == nil {
		model.Agents = map[string]*model.Agent{}
	}
	id := "builtin-edge-" + t.Name()
	pid := int64(70)
	model.Agents[id] = &model.Agent{
		ID: id, Backend: BackendID, BuiltinBackend: true,
		ProviderID: &pid, PreferredModel: "edge-model",
		ToolPolicy: model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}
	t.Cleanup(func() { delete(model.Agents, id) })
	return id
}

// TestBuildSpec_UnknownAgentIsAnError pins that a request for an agent that does
// not exist fails before any network work.
func TestBuildSpec_UnknownAgentIsAnError(t *testing.T) {
	_, err := buildSpec(ai.ChatRequest{AgentID: "no-such-agent"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected a not-found error, got %v", err)
	}
}

// TestBuildSpec_AgentWithoutProviderIsAnError pins the deliberate hard failure:
// silently running on some other endpoint produces answers the user cannot
// account for.
//
// It also pins that the text is LOCALIZED. The message used to be a frozen
// English string while the locale entries sat unread in the yaml files, so a
// Chinese UI showed English with no indication anything was wrong.
func TestBuildSpec_AgentWithoutProviderIsAnError(t *testing.T) {
	if model.Agents == nil {
		model.Agents = map[string]*model.Agent{}
	}
	const id = "builtin-edge-no-provider"
	model.Agents[id] = &model.Agent{ID: id, Backend: BackendID, BuiltinBackend: true}
	defer delete(model.Agents, id)

	prevLang := model.Language
	defer func() { model.Language = prevLang }()

	// Chinese UI.
	model.Language = "zh"
	_, err := buildSpec(ai.ChatRequest{AgentID: id})
	if !errors.Is(err, errNoProvider) {
		t.Fatalf("expected a no-provider error, got %v", err)
	}
	if !strings.Contains(err.Error(), "模型提供商") {
		t.Fatalf("a Chinese UI must get the Chinese text, got %q", err.Error())
	}
	if strings.Contains(err.Error(), "has no model provider") {
		t.Fatalf("the English sentinel leaked into the Chinese text, got %q", err.Error())
	}

	// English UI.
	model.Language = "en"
	_, err = buildSpec(ai.ChatRequest{AgentID: id})
	if !strings.Contains(err.Error(), "Model Provider") {
		t.Fatalf("an English UI must get the English text, got %q", err.Error())
	}
}

// TestBuildSpec_ProviderWithUnknownProtocolIsAnError pins that the protocol from
// the provider row is validated (a typo is a configuration error, not a silent
// fallback to OpenAI).
func TestBuildSpec_ProviderWithUnknownProtocolIsAnError(t *testing.T) {
	prev := model.GetProvider(71)
	model.ReplaceProviders([]*model.Provider{{
		ID: 71, Name: "typo", Protocol: "open-ai", BaseURL: "http://127.0.0.1:1",
		Models: []string{"m"}, DefaultModel: "m",
	}})
	t.Cleanup(func() {
		if prev != nil {
			model.ReplaceProviders([]*model.Provider{prev})
			return
		}
		model.ReplaceProviders(nil)
	})

	if model.Agents == nil {
		model.Agents = map[string]*model.Agent{}
	}
	const id = "builtin-edge-bad-protocol"
	pid := int64(71)
	model.Agents[id] = &model.Agent{ID: id, Backend: BackendID, ProviderID: &pid, PreferredModel: "m"}
	defer delete(model.Agents, id)

	_, err := buildSpec(ai.ChatRequest{AgentID: id})
	if !errors.Is(err, llm.ErrUnsupportedProtocol) {
		t.Fatalf("expected ErrUnsupportedProtocol, got %v", err)
	}
}

// TestBuildSpec_CarriesTheRequestIntoTheRun pins that per-turn inputs reach the
// engine: the prompt, the working directory, the prior summary and the message
// ids the compaction boundary refers to.
func TestBuildSpec_CarriesTheRequestIntoTheRun(t *testing.T) {
	agentID := withProvider(t, "http://127.0.0.1:1")
	req := ai.ChatRequest{
		AgentID:      agentID,
		Prompt:       "summarize the repo",
		SystemPrompt: "you are terse",
		WorkDir:      "/tmp/project",
		History: []ai.HistoryMessage{
			{Role: "user", Text: "earlier question", ID: 11},
			{Role: "assistant", Text: "earlier answer", ID: 12},
		},
		PriorSummary:   "- fact one",
		ThinkingEffort: llm.EffortHigh,
		Mode:           model.ModeRead,
	}
	spec, err := buildSpec(req)
	if err != nil {
		t.Fatalf("buildSpec: %v", err)
	}
	if spec.Prompt != req.Prompt || spec.SystemPrompt != req.SystemPrompt {
		t.Fatalf("the turn's text must be carried, got %+v", spec)
	}
	if spec.WorkDir != "/tmp/project" {
		t.Fatalf("the working directory must confine the tools, got %q", spec.WorkDir)
	}
	if spec.PriorSummary != "- fact one" {
		t.Fatalf("the stored summary must be carried, got %q", spec.PriorSummary)
	}
	if spec.ThinkingEffort != llm.EffortHigh {
		t.Fatalf("the level must be carried verbatim, got %q", spec.ThinkingEffort)
	}
	if len(spec.History) != 2 || len(spec.HistoryMessageIDs) != 2 {
		t.Fatalf("history and its ids must be parallel, got %d / %d", len(spec.History), len(spec.HistoryMessageIDs))
	}
	if spec.HistoryMessageIDs[0] != 11 || spec.HistoryMessageIDs[1] != 12 {
		t.Fatalf("the ids must be the caller's, got %v", spec.HistoryMessageIDs)
	}
	if spec.Summarize == nil {
		t.Fatal("a client was resolved, so a summarizer must be supplied")
	}
	if spec.Client == nil || spec.Client.Model() != "edge-model" {
		t.Fatalf("the client must target the agent's model, got %+v", spec.Client)
	}
}

// TestBuildSpec_ModelOverrideWins pins that a per-request model selection beats
// the agent's preferred model — the chat drawer's model picker depends on it.
func TestBuildSpec_ModelOverrideWins(t *testing.T) {
	agentID := withProvider(t, "http://127.0.0.1:1")
	spec, err := buildSpec(ai.ChatRequest{AgentID: agentID, Model: "picked-at-runtime"})
	if err != nil {
		t.Fatalf("buildSpec: %v", err)
	}
	if got := spec.Client.Model(); got != "picked-at-runtime" {
		t.Fatalf("the override must win, got %q", got)
	}
}

// TestBuildSpec_ModeNeverWidensTheGrant pins that the session mode only narrows:
// an agent granted read+write+exec, in read mode, must not be able to write or
// run a command.
func TestBuildSpec_ModeNeverWidensTheGrant(t *testing.T) {
	agentID := withProvider(t, "http://127.0.0.1:1")
	model.GetAgent(agentID).ToolPolicy = model.ToolPolicy{
		Allow: []string{model.CapFSRead, model.CapFSWrite, model.CapExec},
	}

	spec, err := buildSpec(ai.ChatRequest{AgentID: agentID, Mode: model.ModeRead})
	if err != nil {
		t.Fatalf("buildSpec: %v", err)
	}
	if spec.Policy.Has(model.CapFSWrite) || spec.Policy.Has(model.CapExec) {
		t.Fatalf("read mode must drop write and exec, got %v", spec.Policy.Allow)
	}
	if !spec.Policy.Has(model.CapFSRead) {
		t.Fatalf("read mode must keep reads, got %v", spec.Policy.Allow)
	}

	// An empty mode (background task, older client) leaves the grant alone.
	spec, err = buildSpec(ai.ChatRequest{AgentID: agentID})
	if err != nil {
		t.Fatalf("buildSpec: %v", err)
	}
	if !spec.Policy.Has(model.CapFSWrite) || !spec.Policy.Has(model.CapExec) {
		t.Fatalf("an empty mode must not narrow the grant, got %v", spec.Policy.Allow)
	}
}

// TestExecuteStream_UnknownAgentClosesWithOneError pins the AIBackend contract on
// the failure path: an error BEFORE the channel exists is returned directly, and
// a failure DURING the run is exactly one terminal event.
func TestExecuteStream_UnknownAgentClosesWithOneError(t *testing.T) {
	b := NewForTest()
	_, err := b.ExecuteStream(context.Background(), ai.ChatRequest{AgentID: "nope"})
	if err == nil {
		t.Fatal("an unknown agent must fail before streaming starts")
	}
}

// TestExecuteStream_EmitsAWholeTurn pins the adapter end to end against a fake
// endpoint, with a real tool call in the middle: the tool card gets its start,
// its arguments and its end frame with the file's content, and the turn ends
// with exactly one done carrying the usage.
func TestExecuteStream_EmitsAWholeTurn(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hi there\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The endpoint plays the model: the first round asks to read the file, and
	// once it sees the tool result it answers. One script, so a loop that never
	// converges shows up as a bounded budget rather than a hang.
	const askForTheFile = `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c0","function":{"name":"read","arguments":"{\"path\":\"hello.txt\"}"}}]}}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		answer := `{"choices":[{"delta":{"content":"It says hi there."}}]}`
		if strings.Contains(string(raw), "hi there") {
			_, _ = io.WriteString(w, "data: "+answer+"\n\ndata: [DONE]\n\n")
			return
		}
		for _, f := range []string{
			`{"choices":[{"delta":{"content":"Looking."}}]}`,
			askForTheFile,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":4}}`,
			`[DONE]`,
		} {
			_, _ = io.WriteString(w, "data: "+f+"\n\n")
		}
	}))
	defer srv.Close()

	agentID := withProvider(t, srv.URL)
	b := NewForTest()
	ch, err := b.ExecuteStream(context.Background(), ai.ChatRequest{
		AgentID: agentID, Prompt: "read hello.txt", WorkDir: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	events := collectStream(t, ch)

	var done *ai.StreamEvent
	types := make([]string, 0, len(events))
	var sawContent, sawToolStart, sawToolArgs, sawToolEnd bool
	for i := range events {
		ev := events[i]
		types = append(types, ev.Type)
		switch ev.Type {
		case "content":
			if ev.Content == "Looking." {
				sawContent = true
			}
		case "tool_use":
			if ev.Tool == nil {
				t.Fatalf("a tool_use frame must carry the call, got %+v", ev)
			}
			switch {
			case !ev.Tool.Done && ev.Tool.Input == "" && ev.Tool.Name == "read":
				sawToolStart = true
			case !ev.Tool.Done && ev.Tool.Input != "":
				sawToolArgs = true
			case ev.Tool.Done:
				if sawToolEnd {
					// Only the first call is the read; later rounds have none.
					break
				}
				sawToolEnd = true
				if ev.Tool.Status != "success" {
					t.Fatalf("the read must succeed, got status %q (output %q)", ev.Tool.Status, ev.Tool.Output)
				}
				if !strings.Contains(ev.Tool.Output, "hi there") {
					t.Fatalf("the tool output must be the file body, got %q", ev.Tool.Output)
				}
			}
		case "done":
			done = &events[i]
		}
	}
	if !sawContent || !sawToolStart || !sawToolArgs || !sawToolEnd {
		t.Fatalf("missing frames: content=%v start=%v args=%v end=%v", sawContent, sawToolStart, sawToolArgs, sawToolEnd)
	}
	if done == nil {
		t.Fatal("the stream must end with a done event")
	}
	if events[len(events)-1].Type != "done" {
		t.Fatalf("done must be last, got %q", events[len(events)-1].Type)
	}
	if done.Meta == nil || done.Meta.Transport != "builtin" {
		t.Fatalf("the done frame must identify the transport, got %+v", done.Meta)
	}
	if got := countOf(types, "tool_use"); got != 3 {
		t.Fatalf("expected start + args + end frames for one call, got %d in %v", got, types)
	}
	if done.Meta.StopReason != agentcore.StopComplete {
		t.Fatalf("a turn that answered must stop as complete, got %q", done.Meta.StopReason)
	}
	var answered bool
	for _, ev := range events {
		if ev.Type == "content" && ev.Content == "It says hi there." {
			answered = true
		}
	}
	if !answered {
		t.Fatalf("the second round's answer must reach the transcript, got %v", types)
	}
}

func countOf(list []string, want string) int {
	n := 0
	for _, s := range list {
		if s == want {
			n++
		}
	}
	return n
}

// TestExecuteStream_ReportsAModelFailure pins the failure path after the channel
// exists: exactly one terminal error frame, no done, and a reason the frontend
// localizes.
func TestExecuteStream_ReportsAModelFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "kaput")
	}))
	defer srv.Close()

	agentID := withProvider(t, srv.URL)
	ch, err := NewForTest().ExecuteStream(context.Background(), ai.ChatRequest{
		AgentID: agentID, Prompt: "hi", WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	events := collectStream(t, ch)
	if len(events) == 0 {
		t.Fatal("a failed run must still emit a terminal event")
	}
	last := events[len(events)-1]
	if last.Type != "error" {
		t.Fatalf("expected a terminal error, got %q", last.Type)
	}
	if !strings.Contains(last.Error, "kaput") {
		t.Fatalf("the endpoint's message must reach the user, got %q", last.Error)
	}
	if last.Reason != ai.ReasonRequestFailed {
		t.Fatalf("reason = %q", last.Reason)
	}
}

// collectStream drains an ai.StreamEvent channel.
func collectStream(t *testing.T, ch <-chan ai.StreamEvent) []ai.StreamEvent {
	t.Helper()
	var out []ai.StreamEvent
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

func jsonUnmarshal(raw []byte, dst any) error { return json.Unmarshal(raw, dst) }

// TestToolsPackageIsReachable pins the capability wiring the adapter relies on:
// a read grant produces the read tool, so the tool call above is possible.
func TestToolsPackageIsReachable(t *testing.T) {
	names := map[string]bool{}
	for _, tl := range tools.All(true, false, false) {
		names[tl.Name()] = true
	}
	if !names["read"] || names["write"] {
		t.Fatalf("read-only tools = %v", names)
	}
}

// TestSummarizer_ReturnsTheStreamedText pins the compaction path's model call:
// the summary is the concatenation of the text events, and the request is the
// fixed transcription prompt rather than the agent's persona.
func TestSummarizer_ReturnsTheStreamedText(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = jsonUnmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range []string{
			`{"choices":[{"delta":{"content":"- user asked "}}]}`,
			`{"choices":[{"delta":{"content":"about X"}}]}`,
			`[DONE]`,
		} {
			_, _ = io.WriteString(w, "data: "+f+"\n\n")
		}
	}))
	defer srv.Close()

	client, err := llm.New(llm.Config{Protocol: llm.ProtocolOpenAI, BaseURL: srv.URL, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	sum, err := summarizer(client)(context.Background(), "USER: tell me about X")
	if err != nil {
		t.Fatalf("summarizer: %v", err)
	}
	if sum != "- user asked about X" {
		t.Fatalf("the summary must join every text event, got %q", sum)
	}
	if gotBody["model"] != "m" {
		t.Fatalf("the summary must use the turn's model, got %v", gotBody["model"])
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) == 0 {
		t.Fatal("the summary request must carry the conversation text")
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "system" {
		t.Fatalf("the transcription prompt must be the system message, got %v", first["role"])
	}
}

// TestSummarizer_RequestFailureIsReturned pins that a dead endpoint surfaces the
// transport error instead of an empty summary (which would silently erase the
// conversation).
func TestSummarizer_RequestFailureIsReturned(t *testing.T) {
	client, _ := llm.New(llm.Config{Protocol: llm.ProtocolOpenAI, BaseURL: "http://127.0.0.1:1", Model: "m"})
	sum, err := summarizer(client)(context.Background(), "text")
	if err == nil {
		t.Fatalf("a failed request must be an error, got summary %q", sum)
	}
}

// TestSummarizer_StreamErrorIsReturned pins the in-stream failure: a provider
// that accepts the request and then errors must not produce a partial summary as
// if it were complete.
func TestSummarizer_StreamErrorIsReturned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"error\":{\"message\":\"overloaded\"}}\n\n")
	}))
	defer srv.Close()

	client, _ := llm.New(llm.Config{Protocol: llm.ProtocolOpenAI, BaseURL: srv.URL, Model: "m"})
	_, err := summarizer(client)(context.Background(), "text")
	if err == nil || !strings.Contains(err.Error(), "overloaded") {
		t.Fatalf("the provider's error must reach the caller, got %v", err)
	}
}

// TestForward_GivesUpOnAConsumerThatStopsReading pins the bounded wait: a
// consumer that stops reading must not park the producer goroutine forever. The
// timeout is shortened here so the test does not wait the production 30 s.
func TestForward_GivesUpOnAConsumerThatStopsReading(t *testing.T) {
	prev := emitTimeout
	emitTimeout = 20 * time.Millisecond
	t.Cleanup(func() { emitTimeout = prev })

	// One slot, pre-filled: the first send blocks until the timeout fires.
	stuck := make(chan ai.StreamEvent, 1)
	stuck <- ai.StreamEvent{Type: "filler"}

	done := make(chan struct{})
	start := time.Now()
	go func() {
		defer close(done)
		forward(stuck, &translator{}, agentcore.Event{Kind: agentcore.EventText, Text: "x"})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("forward did not return: the wait is not bounded")
	}
	if elapsed := time.Since(start); elapsed < emitTimeout {
		t.Fatalf("forward returned after %v, before the timeout could fire", elapsed)
	}
	if len(stuck) != 1 {
		t.Fatalf("the event must have been dropped, buffer holds %d", len(stuck))
	}
	if ev := <-stuck; ev.Type != "filler" {
		t.Fatalf("the queued event must be untouched, got %+v", ev)
	}
}

// TestCompactionNotice pins the two message shapes: with a window it names the
// scale, without one it must not print a meaningless percentage.
func TestCompactionNotice(t *testing.T) {
	withWindow := compactionNotice(12, 40_000, 100_000)
	if !strings.Contains(withWindow, "12 messages folded") || !strings.Contains(withWindow, "40% full") {
		t.Fatalf("got %q", withWindow)
	}
	withoutWindow := compactionNotice(12, 40_000, 0)
	if !strings.Contains(withoutWindow, "12 messages folded") {
		t.Fatalf("got %q", withoutWindow)
	}
	if strings.Contains(withoutWindow, "%") {
		t.Fatalf("an unknown window must not produce a percentage, got %q", withoutWindow)
	}
}

// TestTranslate_CompactedCarriesTheBoundary pins that both boundaries travel:
// the executor records exactly which stored turns the summary stands for.
func TestTranslate_CompactedCarriesTheBoundary(t *testing.T) {
	tr := &translator{locale: "en"}
	got := tr.translate(agentcore.Event{
		Kind:                      agentcore.EventCompacted,
		Summary:                   "the dense summary",
		CompactedThroughHistory:   4,
		CompactedThroughMessageID: 987,
		Dropped:                   4,
		SummaryTokens:             1000,
		WindowTokens:              4000,
	})
	if len(got) != 1 || got[0].Type != "compacted" {
		t.Fatalf("expected one compacted event, got %+v", got)
	}
	if got[0].Summary != "the dense summary" {
		t.Fatalf("the summary must travel, got %q", got[0].Summary)
	}
	if got[0].CompactedThroughHistory != 4 || got[0].CompactedThroughMessageID != 987 {
		t.Fatalf("both boundaries must travel, got %+v", got[0])
	}
	if got[0].SystemMessage == nil || !strings.Contains(got[0].SystemMessage.Content, "4 messages folded") {
		t.Fatalf("the notice must state the scale, got %+v", got[0].SystemMessage)
	}
	if strings.Contains(got[0].SystemMessage.Content, "the dense summary") {
		t.Fatal("the summary itself must not be shown as the notice")
	}
}

// TestTranslate_UsageIsNotForwarded pins the deliberate omission: a per-round
// metadata frame REPLACES the previous one in the executor, which understated
// usage on every multi-round turn.
func TestTranslate_UsageIsNotForwarded(t *testing.T) {
	tr := &translator{}
	if got := tr.translate(agentcore.Event{Kind: agentcore.EventUsage}); len(got) != 0 {
		t.Fatalf("a usage event must not become a frame, got %+v", got)
	}
}

// TestTranslate_ClosesReasoningBeforeEveryContentEvent pins each of the five
// opening kinds: a still-open thinking block renders as a spinner forever.
func TestTranslate_ClosesReasoningBeforeEveryContentEvent(t *testing.T) {
	closers := []agentcore.Event{
		{Kind: agentcore.EventText, Text: "x"},
		{Kind: agentcore.EventToolStart, ToolCallID: "c", ToolName: "read"},
		{Kind: agentcore.EventToolEnd, ToolCallID: "c", ToolName: "read"},
		{Kind: agentcore.EventDone, StopReason: agentcore.StopComplete},
		{Kind: agentcore.EventError, Err: errors.New("boom")},
	}
	for _, ev := range closers {
		tr := &translator{}
		tr.translate(agentcore.Event{Kind: agentcore.EventReasoning, Reasoning: "hmm"})
		got := tr.translate(ev)
		if len(got) < 2 || got[0].Type != "thinking_done" {
			t.Errorf("%v must close the thinking block first, got %+v", ev.Kind, got)
		}
		// And exactly once: a second content event must not repeat the close.
		again := tr.translate(ev)
		for _, e := range again {
			if e.Type == "thinking_done" {
				t.Errorf("%v closed the block twice", ev.Kind)
			}
		}
	}
}

// TestTranslate_NilErrorStillProducesReadableText pins the defensive branch: a
// terminal error with no cause must not render an empty transcript row.
func TestTranslate_NilErrorStillProducesReadableText(t *testing.T) {
	tr := &translator{}
	got := tr.translate(agentcore.Event{Kind: agentcore.EventError})
	if len(got) != 1 || got[0].Error == "" {
		t.Fatalf("expected readable text, got %+v", got[0])
	}
	if got[0].Reason != ai.ReasonRequestFailed {
		t.Fatalf("a causeless failure is still request_failed, got %q", got[0].Reason)
	}
}

// TestToolInputJSON pins the three shapes: empty passes through, valid JSON
// passes through, invalid JSON is quoted so the card is not blank.
func TestToolInputJSON(t *testing.T) {
	if got := toolInputJSON(""); got != "" {
		t.Fatalf("empty must stay empty, got %q", got)
	}
	if got := toolInputJSON(`{"a":1}`); got != `{"a":1}` {
		t.Fatalf("valid JSON must pass through, got %q", got)
	}
	if got := toolInputJSON("oops"); got != `"oops"` {
		t.Fatalf("invalid JSON must be quoted, got %q", got)
	}
}

// TestTranslate_EmptyLocaleFallsBack pins that an unset locale does not surface a
// raw message id: the notice is written in the product default language.
func TestTranslate_EmptyLocaleFallsBack(t *testing.T) {
	tr := &translator{}
	got := tr.translate(agentcore.Event{Kind: agentcore.EventDone, Truncated: true})
	if len(got) < 1 || got[0].SystemMessage == nil {
		t.Fatalf("expected a notice, got %+v", got)
	}
	if strings.HasPrefix(got[0].SystemMessage.Content, "Builtin") {
		t.Fatalf("a message id leaked into the transcript: %q", got[0].SystemMessage.Content)
	}
}
