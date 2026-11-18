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
			{Role: "assistant", Text: "", ID: 12}, // not replayable: no text
			{Role: "assistant", Text: "a1", ID: 13},
			{Role: "tool", Text: "tool output", ID: 14}, // not replayable: role
			{Role: "user", Text: "q2", ID: 15},
		},
	}
	msgs := historyFor(req)
	ids := historyIDs(req)
	if len(msgs) != len(ids) {
		t.Fatalf("ids must be parallel to the replayed messages: %d messages, %d ids", len(msgs), len(ids))
	}
	// The fork context is synthetic: it has no stored id, and a boundary that
	// landed on it must therefore write none.
	if ids[0] != 0 {
		t.Fatalf("the fork context must carry no id, got %d", ids[0])
	}
	want := []int64{0, 11, 13, 15}
	if len(ids) != len(want) {
		t.Fatalf("expected %d entries, got %d (%v)", len(want), len(ids), ids)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("id %d = %d, want %d (all: %v)", i, ids[i], want[i], ids)
		}
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
