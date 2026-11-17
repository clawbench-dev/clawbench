package service

import (
	"strings"
	"testing"

	"clawbench/internal/ai"
)

// TestCompactionThenNextTurnReplaysTheKeptTurns is the end-to-end version of the
// P0 defect, at the service layer: compact, store the boundary, then rebuild the
// next turn's history and check that the turns the engine KEPT are still there.
//
// The old boundary was "the newest finalized message", which marked those very
// turns as already-summarized — so the next turn skipped them while the summary
// said nothing about them.
func TestCompactionThenNextTurnReplaysTheKeptTurns(t *testing.T) {
	setupTestDBForBuiltinHistory(t)
	seedHistory(t, "cmp-1", [][2]string{
		{"user", "old question one"},
		{"assistant", "old answer one"},
		{"user", "old question two"},
		{"assistant", "old answer two"},
		// The two most recent turns: the engine keeps these verbatim.
		{"user", "recent question"},
		{"assistant", "recent answer"},
	})

	prior := buildBuiltinHistory("cmp-1", 0, builtinSummary{})
	if len(prior) != 6 {
		t.Fatalf("expected the full history, got %d", len(prior))
	}

	// The engine folded only the first two exchanges and reports that boundary
	// (index 3, id of "old answer two"), which is what the translator forwards.
	lastFolded := 3
	event := ai.StreamEvent{
		Type:                      "compacted",
		Summary:                   "the user asked two old questions",
		CompactedThroughHistory:   lastFolded,
		CompactedThroughMessageID: prior[lastFolded].ID,
	}
	boundary := builtinSummaryFromCompaction(event, prior)
	SetBuiltinSummary("cmp-1", boundary)

	stored := GetBuiltinSummary("cmp-1")
	if stored.CoversThroughTurn != 4 {
		t.Fatalf("stored boundary = %d turns, want 4", stored.CoversThroughTurn)
	}
	next := buildBuiltinHistory("cmp-1", 0, stored)
	if len(next) != 2 {
		t.Fatalf("the two kept turns must be replayed, got %d: %+v", len(next), next)
	}
	if next[0].Text != "recent question" || next[1].Text != "recent answer" {
		t.Fatalf("the kept turns must survive compaction, got %+v", next)
	}
	for _, m := range next {
		if strings.Contains(m.Text, "old ") {
			t.Fatalf("a folded turn must not be replayed, got %q", m.Text)
		}
	}
}

// TestBuildBuiltinHistory_RewindKeepsTheSummaryBoundary pins the SECOND reason
// the boundary is stored by position: a rewind deletes the tail of the session,
// which shifts nothing forward but can drop messages the summary covered. The
// position still names the turns the summary was built from, so the turns after
// it are replayed exactly as before.
func TestBuildBuiltinHistory_RewindKeepsTheSummaryBoundary(t *testing.T) {
	setupTestDBForBuiltinHistory(t)
	seedHistory(t, "cmp-2", [][2]string{
		{"user", "q1"},
		{"assistant", "a1"},
		{"user", "q2"},
		{"assistant", "a2"},
	})

	all := buildBuiltinHistory("cmp-2", 0, builtinSummary{})
	if len(all) != 4 {
		t.Fatalf("expected 4 turns, got %d", len(all))
	}
	// The summary covers the first exchange.
	stored := builtinSummaryHeuristic(all, 1)
	got := buildBuiltinHistory("cmp-2", 0, stored)
	if len(got) != 2 {
		t.Fatalf("expected the two turns after the summary, got %d: %+v", len(got), got)
	}
	if got[0].Text != "q2" || got[1].Text != "a2" {
		t.Fatalf("expected the turns after the boundary, got %+v", got)
	}
}

// builtinSummaryHeuristic builds the boundary a compaction that folded history
// up to index `last` would store.
func builtinSummaryHeuristic(history []ai.HistoryMessage, last int) builtinSummary {
	return builtinSummaryFromCompaction(ai.StreamEvent{
		Type:                      "compacted",
		Summary:                   "sum",
		CompactedThroughHistory:   last,
		CompactedThroughMessageID: history[last].ID,
	}, history)
}
