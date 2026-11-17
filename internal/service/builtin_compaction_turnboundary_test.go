package service

import (
	"context"
	"testing"

	"clawbench/internal/ai"
)

// TestCompaction_NextTurnReplaysTheKeptTurns pins the receiver: the boundary an
// event names becomes exactly "the first N replayed turns are covered", so the
// turns the engine kept verbatim come back on the next turn.
//
// It asserts the stored boundary DIRECTLY, not the resulting history: the id
// fallback alone can produce a correct history while the position is wrong (and
// then the next rewind or delete makes it wrong again — see
// TestBuildBuiltinHistory_RewindKeepsTheSummaryBoundary).
func TestCompaction_NextTurnReplaysTheKeptTurns(t *testing.T) {
	setupTestDBForBuiltinHistory(t)
	const session = "cmp-turn-1"

	ids := seedHistoryWithIDs(t, session, [][2]string{
		{"user", "OLD-QUESTION-1"},
		{"assistant", "OLD-ANSWER-1"},
		{"user", "OLD-QUESTION-2"},
		{"assistant", "OLD-ANSWER-2"},
		{"user", "KEPT-QUESTION"},
		{"assistant", "KEPT-ANSWER"},
	})
	if len(ids) != 6 {
		t.Fatalf("expected 6 stored messages, got %d", len(ids))
	}
	prior := buildBuiltinHistory(session, 0, builtinSummary{})
	if len(prior) != 6 {
		t.Fatalf("the first turn must replay all six turns, got %d", len(prior))
	}

	// The engine folded history[0..3] (two old exchanges) and kept the third.
	event := ai.StreamEvent{
		Type:                      "compacted",
		Summary:                   "the user asked two old questions",
		CompactedThroughHistory:   3,
		CompactedThroughMessageID: ids[3],
	}
	boundary := builtinSummaryFromCompaction(event, prior)
	if boundary.CoversThroughTurn != 4 {
		t.Fatalf("the boundary must cover exactly the 4 FOLDED turns, got %d", boundary.CoversThroughTurn)
	}
	if boundary.UpToMessageID != ids[3] {
		t.Fatalf("the id boundary must be the last folded message (%d), got %d", ids[3], boundary.UpToMessageID)
	}

	// Persist and read back, as the executor does: the next turn sees stored state.
	SetBuiltinSummary(session, boundary)
	stored := GetBuiltinSummary(session)
	if stored.Text != event.Summary || stored.CoversThroughTurn != 4 {
		t.Fatalf("the boundary must survive the store, got %+v", stored)
	}
	next := buildBuiltinHistory(session, 0, stored)
	if got, want := texts(next), []string{"KEPT-QUESTION", "KEPT-ANSWER"}; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("the next turn must replay exactly the kept turns, got %v", got)
	}
}

// TestCompaction_NoHistoryFoldedWritesNoBoundary pins the deliberate trade for a
// compaction that only folded THIS turn's tool results.
//
// The engine reports -1 in that case: nothing of the stored history left the
// context, so there is nothing the caller may stop replaying. The summary is
// still stored (it is on the done event and describes what the tool output said),
// but with NO boundary — which costs one redundant summarizer call on the next
// turn and can never lose content.
//
// The alternative — writing the newest id, which is what the old code did — is
// cheap and wrong: it drops turns that are still in the model's context and that
// the summary never mentioned. Between an extra model call and silent data loss,
// the extra call wins.
func TestCompaction_NoHistoryFoldedWritesNoBoundary(t *testing.T) {
	setupTestDBForBuiltinHistory(t)
	const session = "cmp-turn-2"

	ids := seedHistoryWithIDs(t, session, [][2]string{
		{"user", "Q1"},
		{"assistant", "A1"},
	})
	prior := buildBuiltinHistory(session, 0, builtinSummary{})

	// A mid-turn fold: -1 means "only this turn's tool results were folded".
	event := ai.StreamEvent{
		Type:                    "compacted",
		Summary:                 "a large file was read and folded",
		CompactedThroughHistory: -1,
	}
	boundary := builtinSummaryFromCompaction(event, prior)
	if boundary.CoversThroughTurn != 0 || boundary.UpToMessageID != 0 {
		t.Fatalf("no folded history must write no boundary, got %+v", boundary)
	}

	SetBuiltinSummary(session, boundary)
	stored := GetBuiltinSummary(session)
	if stored.CoversThroughTurn != 0 || stored.UpToMessageID != 0 {
		t.Fatalf("the store must not invent a boundary, got %+v", stored)
	}
	// The whole history is replayed again — the price of not guessing.
	next := buildBuiltinHistory(session, 0, stored)
	if len(next) != 2 || next[0].ID != ids[0] || next[1].ID != ids[1] {
		t.Fatalf("the full history must be replayed, got %+v", next)
	}
}

// TestCompaction_UnmappableBoundaryWritesNoBoundary pins the other failure mode:
// the event names an index this caller's history cannot explain (an id list
// shorter than the history, or a stale index from a different list).
//
// Mapping such an index to some id is a guess, and a wrong guess silently drops
// stored turns. Writing nothing replays everything.
func TestCompaction_UnmappableBoundaryWritesNoBoundary(t *testing.T) {
	history := []ai.HistoryMessage{
		{Role: roleUser, Text: "q1", ID: 11},
		{Role: roleAssistant, Text: "a1", ID: 12},
	}
	cases := []struct {
		name string
		ev   ai.StreamEvent
	}{
		{"index past the end of the history", ai.StreamEvent{CompactedThroughHistory: 2, CompactedThroughMessageID: 12}},
		// The id list is not consulted by the receiver (it maps the index onto
		// the history it was given), so the way to express "the ids do not line
		// up" is an index with no matching message.
		{"negative index other than -1", ai.StreamEvent{CompactedThroughHistory: -7}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.ev.Type = "compacted"
			tc.ev.Summary = "s"
			s := builtinSummaryFromCompaction(tc.ev, history)
			if s.CoversThroughTurn != 0 || s.UpToMessageID != 0 {
				t.Fatalf("an unmappable boundary must write nothing, got %+v", s)
			}
			if s.Text != "s" {
				t.Fatalf("the summary text itself must still be kept, got %q", s.Text)
			}
		})
	}

	// An entry with no stored id (the synthetic fork-context header) maps by
	// position only: the boundary is still written, and the id is simply absent.
	s := builtinSummaryFromCompaction(ai.StreamEvent{
		Type: "compacted", Summary: "s", CompactedThroughHistory: 0,
	}, []ai.HistoryMessage{{Role: roleUser, Text: "context", ID: 0}})
	if s.CoversThroughTurn != 1 {
		t.Fatalf("a positional boundary must still be written, got %+v", s)
	}
	if s.UpToMessageID != 0 {
		t.Fatalf("there is no id to write for a synthetic entry, got %d", s.UpToMessageID)
	}
}

// TestCompaction_BoundarySurvivesTheExecutor pins the wiring between the event
// and the store: the executor is the only production caller of
// builtinSummaryFromCompaction, and the value it writes is what the next turn
// reads. A receiver that computes the right boundary and an event carrying the
// right index still lose the kept turns if the two are not connected.
func TestCompaction_BoundarySurvivesTheExecutor(t *testing.T) {
	setupTestDBForBuiltinHistory(t)
	const session = "cmp-turn-3"
	ids := seedHistoryWithIDs(t, session, [][2]string{
		{"user", "OLD-Q1"},
		{"assistant", "OLD-A1"},
		{"user", "OLD-Q2"},
		{"assistant", "OLD-A2"},
		{"user", "KEPT-Q"},
		{"assistant", "KEPT-A"},
	})
	prior := buildBuiltinHistory(session, 0, builtinSummary{})
	if len(prior) != 6 {
		t.Fatalf("expected all six turns to be replayed first, got %d", len(prior))
	}

	// The event exactly as the translator forwards it off the engine's
	// EventCompacted (see internal/ai/backends/builtin/translate.go).
	e := NewSessionExecutor(context.Background(), RunConfig{
		SessionID:   session,
		ChatRequest: ai.ChatRequest{History: prior},
	})
	e.handleNonTerminalEvent(ai.StreamEvent{
		Type:                      "compacted",
		Summary:                   "two old questions were discussed",
		CompactedThroughHistory:   3,
		CompactedThroughMessageID: ids[3],
	})

	stored := GetBuiltinSummary(session)
	if stored.Text != "two old questions were discussed" {
		t.Fatalf("the executor must store the summary, got %q", stored.Text)
	}
	if stored.CoversThroughTurn != 4 {
		t.Fatalf("the executor must store the engine's boundary (4 turns), got %d", stored.CoversThroughTurn)
	}
	if stored.UpToMessageID != ids[3] {
		t.Fatalf("the executor must store the last FOLDED id %d, got %d", ids[3], stored.UpToMessageID)
	}

	next := buildBuiltinHistory(session, 0, stored)
	if got, want := texts(next), []string{"KEPT-Q", "KEPT-A"}; len(got) != len(want) ||
		got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("the next turn must replay the kept turns, got %v", got)
	}
}

// TestCompaction_ExecutorIgnoresAMidTurnFold pins the same wiring for the
// no-history case: a mid-turn tool-result fold stores the summary and no
// boundary, so the next turn replays everything.
func TestCompaction_ExecutorIgnoresAMidTurnFold(t *testing.T) {
	setupTestDBForBuiltinHistory(t)
	const session = "cmp-turn-4"
	ids := seedHistoryWithIDs(t, session, [][2]string{
		{"user", "Q1"}, {"assistant", "A1"},
	})
	prior := buildBuiltinHistory(session, 0, builtinSummary{})

	e := NewSessionExecutor(context.Background(), RunConfig{
		SessionID:   session,
		ChatRequest: ai.ChatRequest{History: prior},
	})
	e.handleNonTerminalEvent(ai.StreamEvent{
		Type: "compacted", Summary: "a big tool result was folded",
		CompactedThroughHistory: -1,
	})

	stored := GetBuiltinSummary(session)
	if stored.CoversThroughTurn != 0 || stored.UpToMessageID != 0 {
		t.Fatalf("the executor must not invent a boundary, got %+v", stored)
	}
	next := buildBuiltinHistory(session, 0, stored)
	if len(next) != 2 || next[0].ID != ids[0] || next[1].ID != ids[1] {
		t.Fatalf("the full history must be replayed, got %+v", next)
	}
}

// texts renders a replayed history as its message texts.
func texts(history []ai.HistoryMessage) []string {
	out := make([]string, 0, len(history))
	for _, m := range history {
		out = append(out, m.Text)
	}
	return out
}
