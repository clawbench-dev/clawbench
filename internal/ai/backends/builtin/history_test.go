package builtin

import (
	"testing"

	"clawbench/internal/agentcore"
	"clawbench/internal/agentcore/llm"
	"clawbench/internal/ai"
)

// TestHistoryIDsAreParallelToHistory pins the pairing compaction depends on: the
// boundary the engine reports is an INDEX into the replayed list, and the caller
// records the id at that index. A list that skips an entry (an empty text, a
// tool row) would shift every id and make the summary cover the wrong turns.
func TestHistoryIDsAreParallelToHistory(t *testing.T) {
	req := ai.ChatRequest{
		ForkContext: "earlier context",
		History: []ai.HistoryMessage{
			{Role: "user", Text: "q1", ID: 11},
			{Role: "assistant", Text: "", ID: 12}, // not replayable: no text, no tools
			{Role: "assistant", Text: "a1", ID: 13},
			{Role: "tool", Text: "tool output", ID: 14}, // not replayable: role
			{Role: "user", Text: "q2", ID: 15},
		},
	}
	replay := buildHistoryReplay(req)
	if len(replay.Messages) != len(replay.IDs) {
		t.Fatalf("ids must be parallel to the replayed messages: %d messages, %d ids", len(replay.Messages), len(replay.IDs))
	}
	// The fork context is synthetic: it has no stored id, and a boundary that
	// landed on it must therefore write none.
	if replay.IDs[0] != 0 {
		t.Fatalf("the fork context must carry no id, got %d", replay.IDs[0])
	}
	want := []int64{0, 11, 13, 15}
	if len(replay.IDs) != len(want) {
		t.Fatalf("expected %d entries, got %d (%v)", len(want), len(replay.IDs), replay.IDs)
	}
	for i := range want {
		if replay.IDs[i] != want[i] {
			t.Fatalf("id %d = %d, want %d (all: %v)", i, replay.IDs[i], want[i], replay.IDs)
		}
	}
}

// TestHistoryReplay_ExpandsToolTurns pins the shape the model needs: a turn
// that called tools becomes an assistant message carrying tool_use parts and a
// following tool message carrying the results. Prose alone was what let the
// model "remember" work it never did.
func TestHistoryReplay_ExpandsToolTurns(t *testing.T) {
	req := ai.ChatRequest{History: []ai.HistoryMessage{{
		Role: "assistant",
		Text: "let me look",
		ID:   21,
		Turn: 3,
		Tools: []ai.HistoryToolCall{
			{ID: "c1", Name: "read", Input: `{"file_path":"a.go"}`, Output: "package a"},
			{ID: "c2", Name: "bash", Input: `{"command":"go test"}`, Output: "FAIL", IsError: true},
		},
	}}}
	replay := buildHistoryReplay(req)
	if len(replay.Messages) != 2 {
		t.Fatalf("expected assistant + tool messages, got %d", len(replay.Messages))
	}
	if replay.Messages[0].Role != llm.RoleAssistant || len(replay.Messages[0].Parts) != 3 {
		t.Fatalf("the assistant turn must carry text + 2 calls, got %+v", replay.Messages[0])
	}
	if replay.Messages[0].Parts[1].ToolCall.Name != "read" {
		t.Fatalf("tool order must be preserved, got %+v", replay.Messages[0].Parts[1].ToolCall)
	}
	if replay.Messages[1].Role != llm.RoleTool || len(replay.Messages[1].Parts) != 2 {
		t.Fatalf("results must be one tool message, got %+v", replay.Messages[1])
	}
	if !replay.Messages[1].Parts[1].ToolResult.IsError {
		t.Fatal("a failed call must stay marked as an error")
	}
	// Both messages belong to the same stored turn, so the boundary id is the
	// same on both — that is what keeps the index/id pairing intact after the
	// expansion.
	if replay.IDs[0] != 21 || replay.IDs[1] != 21 {
		t.Fatalf("the expanded pair must share the stored id, got %v", replay.IDs)
	}
}

// TestHistoryReplay_ToolOnlyTurnIsReplayed pins that a turn with no text is
// still replayed when it called tools: skipping it was how the model lost the
// record of its own work. A trailing one is dropped separately (it never
// answered), so this fixture puts a user turn after it.
func TestHistoryReplay_ToolOnlyTurnIsReplayed(t *testing.T) {
	req := ai.ChatRequest{History: []ai.HistoryMessage{
		{Role: "assistant", ID: 31, Turn: 0, Tools: []ai.HistoryToolCall{{ID: "c1", Name: "bash", Input: `{}`, Output: "ok"}}},
		{Role: "user", Text: "next", ID: 32, Turn: 1},
	}}
	got := buildHistoryReplay(req).Messages
	if len(got) != 3 {
		t.Fatalf("a mid-history tool-only turn must replay as assistant + tool, got %d messages", len(got))
	}
	if got[0].Role != llm.RoleAssistant || got[1].Role != llm.RoleTool {
		t.Fatalf("unexpected roles: %v / %v", got[0].Role, got[1].Role)
	}
}

// A tool that produced nothing must still replay as a result the model can
// read: an empty text part is dropped by the accumulator, which would leave the
// call/result pairing looking broken.
func TestHistoryReplay_EmptyToolOutputIsPlaceholder(t *testing.T) {
	req := ai.ChatRequest{History: []ai.HistoryMessage{
		{Role: "assistant", Text: "ran it", ID: 51, Tools: []ai.HistoryToolCall{
			{ID: "c1", Name: "bash", Input: `{}`, Output: ""},
		}},
		{Role: "user", Text: "next", ID: 52},
	}}
	got := buildHistoryReplay(req).Messages
	if len(got) != 3 || got[1].Role != llm.RoleTool {
		t.Fatalf("expected assistant + tool + user, got %+v", got)
	}
	if text := got[1].Parts[0].ToolResult.Parts[0].Text; text != "(no output)" {
		t.Fatalf("an empty tool output must become a readable placeholder, got %q", text)
	}
}

// The trimmer only removes a trailing tool round that answers an ASSISTANT
// turn. A trailing tool message with anything else in front of it is not the
// shape it exists for, so it must be left alone.
func TestTrimIncompleteTail_KeepsWhenPreviousIsNotAssistant(t *testing.T) {
	r := historyReplay{
		Messages: []llm.Message{
			llm.TextMessage(llm.RoleUser, "q"),
			{Role: llm.RoleTool, Parts: []llm.Part{{ToolResult: &llm.ToolResult{CallID: "c1"}}}},
		},
		IDs: []int64{61, 62},
	}
	got := trimIncompleteTail(r)
	if len(got.Messages) != 2 || len(got.IDs) != 2 {
		t.Fatalf("a tool message not preceded by an assistant turn must be kept, got %+v", got)
	}
}

// Both protocols require a JSON object as the tool input. An empty, malformed
// or non-object blob must become {} so the call/result pairing survives instead
// of producing an invalid request.
func TestToolArguments_FallsBackToEmptyObject(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"empty", "", "{}"},
		{"malformed", "{not json", "{}"},
		{"valid but not an object", `["a"]`, "{}"},
		{"object is kept", `{"file_path":"a.go"}`, `{"file_path":"a.go"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := toolArguments(tc.in); got != tc.want {
				t.Fatalf("toolArguments(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestTranslate_UsageDoesNotBecomeAMetadataFrame pins the accounting rule.
//
// A per-round metadata frame is a REPLACEMENT in the executor, and the terminal
// done frame only fills still-empty fields — so a multi-round turn recorded the
// LAST round's tokens and the total was ignored, understating usage and cost.
// Only the terminal frame may carry usage.
func TestTranslate_UsageDoesNotBecomeAMetadataFrame(t *testing.T) {
	tr := &translator{}
	out := tr.translate(agentcore.Event{
		Kind:  agentcore.EventUsage,
		Usage: &llm.Usage{InputTokens: 1000, OutputTokens: 200},
	})
	if len(out) != 0 {
		t.Fatalf("a per-round usage event must not produce a metadata frame, got %+v", out)
	}
}

// TestTranslate_DoneReportsContextUsage pins the terminal usage frame the
// bottom bar's context panel reads.
//
// A built-in agent has no agent-side usage reporting, so without this frame the
// frontend's usage chip never appeared (it keys on size > 0). It must arrive
// BEFORE done: the terminal frame is what closes the turn on the client.
func TestTranslate_DoneReportsContextUsage(t *testing.T) {
	tr := &translator{}
	out := tr.translate(agentcore.Event{
		Kind:          agentcore.EventDone,
		StopReason:    agentcore.StopComplete,
		TotalUsage:    llm.Usage{InputTokens: 1200, OutputTokens: 300},
		ContextTokens: 4200,
		WindowTokens:  128000,
	})
	if len(out) != 2 {
		t.Fatalf("expected usage_update + done, got %+v", out)
	}
	if out[0].Type != "usage_update" || out[1].Type != "done" {
		t.Fatalf("usage must precede the terminal frame, got %q then %q", out[0].Type, out[1].Type)
	}
	u := out[0].Usage
	if u == nil {
		t.Fatal("the usage frame must carry a usage block")
	}
	if u.Used != 4200 || u.Size != 128000 {
		t.Fatalf("used/size must be the engine's report, got %d/%d", u.Used, u.Size)
	}
	if u.InputTokens != 1200 || u.OutputTokens != 300 || u.TotalTokens != 1500 {
		t.Fatalf("the token detail must carry the turn totals, got %+v", u)
	}
}

// TestTranslate_DoneWithoutWindowOmitsUsage pins the other side: no known
// window means no percentage, and a 0/0 frame would render as an empty bar at
// 0% rather than "no data".
func TestTranslate_DoneWithoutWindowOmitsUsage(t *testing.T) {
	tr := &translator{}
	out := tr.translate(agentcore.Event{
		Kind:       agentcore.EventDone,
		StopReason: agentcore.StopComplete,
		TotalUsage: llm.Usage{InputTokens: 100, OutputTokens: 20},
	})
	for _, ev := range out {
		if ev.Type == "usage_update" {
			t.Fatalf("an unknown window must not produce a usage frame, got %+v", ev)
		}
	}
}

// TestTranslate_DoneCarriesTheTurnTotal pins the other half: the terminal frame
// is where the accounting comes from, and it carries the SUM across rounds.
func TestTranslate_DoneCarriesTheTurnTotal(t *testing.T) {
	tr := &translator{}
	out := tr.translate(agentcore.Event{
		Kind:       agentcore.EventDone,
		StopReason: agentcore.StopComplete,
		TotalUsage: llm.Usage{InputTokens: 3000, OutputTokens: 600},
	})
	if len(out) == 0 {
		t.Fatal("done must produce a terminal frame")
	}
	last := out[len(out)-1]
	if last.Type != "done" || last.Meta == nil {
		t.Fatalf("expected a done frame with metadata, got %+v", last)
	}
	if last.Meta.InputTokens != 3000 || last.Meta.OutputTokens != 600 {
		t.Fatalf("done must carry the turn's totals, got %+v", last.Meta)
	}
}

// TestTranslate_DoneCarriesTheCacheSplit pins that the provider's cache numbers
// reach the message metadata. They were parsed and then dropped on the floor,
// so every built-in turn looked like a total miss.
func TestTranslate_DoneCarriesTheCacheSplit(t *testing.T) {
	tr := &translator{}
	out := tr.translate(agentcore.Event{
		Kind:       agentcore.EventDone,
		StopReason: agentcore.StopComplete,
		TotalUsage: llm.Usage{InputTokens: 900, OutputTokens: 100, CacheReadTokens: 700, CacheWriteTokens: 20},
	})
	last := out[len(out)-1]
	if last.Type != "done" || last.Meta == nil {
		t.Fatalf("expected a done frame, got %+v", last)
	}
	if last.Meta.CachedReadTokens != 700 || last.Meta.CachedWriteTokens != 20 {
		t.Fatalf("cache split lost in translation: %+v", last.Meta)
	}
	if last.Meta.CacheHitTokens != 700 {
		t.Fatalf("the hit column must mirror the read side, got %d", last.Meta.CacheHitTokens)
	}
}

// TestHistoryReplay_DropsIncompleteTrailingToolRound pins the one shape we do
// not replay: a cancelled turn that only managed to call a tool. It has no
// answer, and leaving it as the last assistant message breaks Anthropic's
// extended-thinking rule (we cannot replay a signature we never stored).
func TestHistoryReplay_DropsIncompleteTrailingToolRound(t *testing.T) {
	req := ai.ChatRequest{History: []ai.HistoryMessage{
		{Role: "user", Text: "do it", ID: 41, Turn: 0},
		{Role: "assistant", ID: 42, Turn: 1, Tools: []ai.HistoryToolCall{
			{ID: "c1", Name: "bash", Input: `{}`, Output: "started"},
		}},
	}}
	replay := buildHistoryReplay(req)
	if len(replay.Messages) != 1 || replay.Messages[0].Role != llm.RoleUser {
		t.Fatalf("the incomplete tail must be dropped, got %+v", replay.Messages)
	}
	if len(replay.IDs) != 1 || replay.IDs[0] != 41 {
		t.Fatalf("ids must follow the trimmed messages, got %v", replay.IDs)
	}

	// A completed turn (it has text) is kept even though it called tools.
	req.History[1].Text = "done"
	if got := buildHistoryReplay(req).Messages; len(got) != 3 {
		t.Fatalf("a completed tool turn must be kept, got %d messages", len(got))
	}
}
