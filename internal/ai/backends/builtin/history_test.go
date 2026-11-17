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
