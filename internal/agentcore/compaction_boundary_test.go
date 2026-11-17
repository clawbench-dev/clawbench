package agentcore

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"clawbench/internal/agentcore/llm"
	"clawbench/internal/model"
)

// historyIDs makes a parallel id list: 100, 101, ... so a boundary index and a
// boundary id are never accidentally equal.
func historyIDs(n int) []int64 {
	out := make([]int64, n)
	for i := range out {
		out[i] = int64(100 + i)
	}
	return out
}

// collectCompacted runs a spec and returns every EventCompacted it emitted.
func collectCompacted(t *testing.T, spec Spec) []Event {
	t.Helper()
	var out []Event
	_, _ = RunSync(context.Background(), spec, func(ev Event) {
		if ev.Kind == EventCompacted {
			out = append(out, ev)
		}
	})
	return out
}

// TestCompacted_ReportsTheLastFoldedHistoryMessage pins the boundary the caller
// needs to stop replaying.
//
// Dropped alone cannot express it: the engine keeps the most recent turns
// verbatim, so "I folded the older part" is not "everything so far is now the
// summary". A caller that wrote its own newest id dropped those kept turns on
// the next turn.
func TestCompacted_ReportsTheLastFoldedHistoryMessage(t *testing.T) {
	client := &summaryClient{
		rounds:         []func(llm.Request) []llm.Event{textRound("done")},
		summaryReplies: []string{"older stuff"},
	}
	// Six large turns (6084 estimated tokens), then a recent turn of a tenth of
	// that: compaction folds the large stretch and keeps the recent turn
	// verbatim, which is exactly the shape the next turn must not lose.
	history := manyTurns(6, 2000)
	history = append(history,
		llm.TextMessage(llm.RoleUser, strings.Repeat("r", 300)),
		llm.TextMessage(llm.RoleAssistant, strings.Repeat("s", 300)),
	)
	ids := historyIDs(len(history))

	events := collectCompacted(t, Spec{
		Client:            client,
		Prompt:            "and now?",
		History:           history,
		HistoryMessageIDs: ids,
		WorkDir:           t.TempDir(),
		Policy:            model.ToolPolicy{Allow: []string{model.CapFSRead}},
		ContextWindow:     8000,
		Summarize:         withSummary(client),
	})
	if len(events) == 0 {
		t.Fatal("the overflowing history must be compacted")
	}
	ev := events[0]
	if ev.CompactedThroughHistory < 0 {
		t.Fatalf("a compaction that folded history must name the boundary, got %d", ev.CompactedThroughHistory)
	}
	if ev.CompactedThroughHistory >= len(history) {
		t.Fatalf("the boundary index %d is outside the %d-message history", ev.CompactedThroughHistory, len(history))
	}
	// The boundary must stop BEFORE the turn that is still in the model's
	// context verbatim: the summary does not describe it. (The new prompt always
	// follows the history, so with every exchange inside the keep budget the
	// split can never fall inside the history — this asserts the invariant that
	// matters, not which exchange SplittingPoint happened to choose.)
	if ev.CompactedThroughHistory >= len(history) {
		t.Fatalf("the boundary index %d overlaps the kept turns of a %d-message history",
			ev.CompactedThroughHistory, len(history))
	}
	if want := ids[ev.CompactedThroughHistory]; ev.CompactedThroughMessageID != want {
		t.Fatalf("boundary id = %d, want the id of history message %d (%d)",
			ev.CompactedThroughMessageID, ev.CompactedThroughHistory, want)
	}
}

// TestCompacted_ToolResultsOnlyBoundaryDoesNotCoverHistory pins the mid-turn
// path: folding tool results replaces them with notes IN PLACE, so nothing of the
// stored history may be declared covered — the "cover the whole history" variant
// is exactly what made a single overflowing turn swallow the recent turns for
// the next one.
//
// The path needs a window too small to hold even one result AND a stored turn
// large enough to be inside the keep budget on its own, so the exchange split
// stops right before it and the fold has to land on this turn's tool output.
func TestCompacted_ToolResultsOnlyBoundaryDoesNotCoverHistory(t *testing.T) {
	big := strings.Repeat("x", 240000) // ~60k tokens in ONE result
	client := &summaryClient{
		rounds: []func(llm.Request) []llm.Event{
			toolRound("c1", "read", `{"path":"a.txt"}`),
			toolRound("c2", "read", `{"path":"a.txt"}`),
			toolRound("c3", "read", `{"path":"a.txt"}`),
			toolRound("c4", "read", `{"path":"a.txt"}`),
			toolRound("c5", "read", `{"path":"a.txt"}`),
			toolRound("c6", "read", `{"path":"a.txt"}`),
			textRound("finally"),
		},
		summaryReplies: []string{"read a large file repeatedly"},
	}
	root := t.TempDir()
	writeFile(t, root, "a.txt", big)

	// No stored history at all: every message in flight belongs to this turn, so
	// the tool-results fold demonstrably removes nothing a caller could replay.
	var events []Event
	_, _ = RunSync(context.Background(), Spec{
		Client:        client,
		Prompt:        "read it a few times",
		WorkDir:       root,
		Policy:        model.ToolPolicy{Allow: []string{model.CapFSRead}},
		ContextWindow: 40000,
		Summarize:     withSummary(client),
	}, func(ev Event) {
		if ev.Kind == EventCompacted {
			events = append(events, ev)
		}
	})
	if len(events) == 0 {
		t.Fatal("the turn must have been compacted mid-way")
	}
	for _, ev := range events {
		if ev.CompactedThroughHistory != -1 || ev.CompactedThroughMessageID != 0 {
			t.Fatalf("a turn with no stored history must report no boundary, got index=%d id=%d",
				ev.CompactedThroughHistory, ev.CompactedThroughMessageID)
		}
	}
}

// TestCompacted_KeptHistoryIsNotClaimedAsCovered pins the shape that broke the
// next turn: the older stored turns are folded, the most recent one is kept
// verbatim. The boundary must point at the last FOLDED message — claiming the
// newest one told the next turn to skip a turn that is still in the model's
// context and that the summary does not describe.
func TestCompacted_KeptHistoryIsNotClaimedAsCovered(t *testing.T) {
	client := &summaryClient{
		rounds:         []func(llm.Request) []llm.Event{textRound("done")},
		summaryReplies: []string{"older stuff"},
	}
	// Two large turns to fold, then two small recent turns and the prompt, which
	// fit the keep budget together: the split lands after the large turns, so the
	// small ones stay verbatim and the boundary must stop before them.
	large := strings.Repeat("L", 4000)
	_ = large
	history := []llm.Message{
		llm.TextMessage(llm.RoleUser, large), llm.TextMessage(llm.RoleAssistant, large),
		llm.TextMessage(llm.RoleUser, large), llm.TextMessage(llm.RoleAssistant, large),
		llm.TextMessage(llm.RoleUser, "recent q"), llm.TextMessage(llm.RoleAssistant, "recent a"),
	}
	ids := historyIDs(len(history))

	var events []Event
	_, _ = RunSync(context.Background(), Spec{
		Client:            client,
		Prompt:            "and now?",
		History:           history,
		HistoryMessageIDs: ids,
		WorkDir:           t.TempDir(),
		Policy:            model.ToolPolicy{Allow: []string{model.CapFSRead}},
		ContextWindow:     5000,
		Summarize:         withSummary(client),
	}, func(ev Event) {
		if ev.Kind == EventCompacted {
			events = append(events, ev)
		}
	})
	if len(events) == 0 {
		t.Fatal("the overflowing history must be compacted")
	}
	ev := events[0]
	// The boundary must point at a message the summary actually folded (the four
	// large turns), and its id must be that message's id — not the newest one,
	// which is still in the model's context.
	if ev.CompactedThroughHistory < 0 || ev.CompactedThroughHistory >= len(history) {
		t.Fatalf("boundary index %d is outside the history", ev.CompactedThroughHistory)
	}
	if want := ids[ev.CompactedThroughHistory]; ev.CompactedThroughMessageID != want {
		t.Fatalf("boundary id = %d, want %d", ev.CompactedThroughMessageID, want)
	}
}

func TestCompacted_SecondPassKeepsItsOwnBoundary(t *testing.T) {
	root := t.TempDir()
	big := strings.Repeat("y", 40000)
	writeFile(t, root, "a.txt", big)

	// Two stored turns, then several large tool rounds so the run keeps
	// compacting past its first pass.
	history := []llm.Message{
		llm.TextMessage(llm.RoleUser, "first question"),
		llm.TextMessage(llm.RoleAssistant, "first answer"),
	}
	client := &summaryClient{
		rounds: []func(llm.Request) []llm.Event{
			toolRound("c1", "read", `{"path":"a.txt"}`),
			toolRound("c2", "read", `{"path":"a.txt"}`),
			toolRound("c3", "read", `{"path":"a.txt"}`),
			toolRound("c4", "read", `{"path":"a.txt"}`),
			toolRound("c5", "read", `{"path":"a.txt"}`),
			toolRound("c6", "read", `{"path":"a.txt"}`),
			textRound("done"),
		},
		summaryReplies: []string{"summary-" + strings.Repeat("z", 200), "summary-2"},
	}
	events := collectCompacted(t, Spec{
		Client:            client,
		Prompt:            "go",
		History:           history,
		HistoryMessageIDs: []int64{41, 42},
		WorkDir:           root,
		Policy:            model.ToolPolicy{Allow: []string{model.CapFSRead}},
		ContextWindow:     20000,
		Summarize:         withSummary(client),
	})
	if len(events) < 2 {
		t.Fatalf("expected several compaction passes, got %d", len(events))
	}
	for i, ev := range events {
		if ev.CompactedThroughHistory >= len(history) {
			t.Fatalf("pass %d reported boundary %d outside a %d-message history", i, ev.CompactedThroughHistory, len(history))
		}
		if ev.CompactedThroughMessageID > 0 && ev.CompactedThroughHistory >= 0 {
			if want := []int64{41, 42}[ev.CompactedThroughHistory]; ev.CompactedThroughMessageID != want {
				t.Fatalf("pass %d: id %d does not belong to history index %d (%d)",
					i, ev.CompactedThroughMessageID, ev.CompactedThroughHistory, want)
			}
		}
	}
}

// TestCompacted_NoHistoryReportsNoBoundary pins the simplest shape: a first turn
// that overflows on its own tool results has nothing stored to cover.
func TestCompacted_NoHistoryReportsNoBoundary(t *testing.T) {
	big := strings.Repeat("x", 40000)
	client := &summaryClient{
		rounds: []func(llm.Request) []llm.Event{
			toolRound("c1", "read", `{"path":"a.txt"}`),
			toolRound("c2", "read", `{"path":"a.txt"}`),
			toolRound("c3", "read", `{"path":"a.txt"}`),
			textRound("finally"),
		},
		summaryReplies: []string{"read a file"},
	}
	root := t.TempDir()
	writeFile(t, root, "a.txt", big)

	events := collectCompacted(t, Spec{
		Client:        client,
		Prompt:        "read",
		WorkDir:       root,
		Policy:        model.ToolPolicy{Allow: []string{model.CapFSRead}},
		ContextWindow: 20000,
		Summarize:     withSummary(client),
	})
	if len(events) == 0 {
		t.Fatal("expected a compaction")
	}
	if got := events[len(events)-1].CompactedThroughHistory; got != -1 {
		t.Fatalf("a run with no stored history must report no boundary (-1), got %d", got)
	}
}

// TestCompaction_StopsWhenOnlyTheSummaryIsLeftToFold pins the loop's progress
// guard at the place the defect lives.
//
// Once a conversation is down to [summary, one exchange, prompt], SplitPoint
// measures the keep budget from the newest exchange — which always fits on its
// own — so it hands back the index of the PREVIOUS summary. The pass then
// summarizes that single message and puts a summary of the same size back:
// tokens_after == tokens_before, ShouldCompact still holds, and the old loop did
// it four times in a row. Each pass is a real summarizer call that spends the
// user's quota, and because every turn starts in this shape the user paid for all
// four after EVERY message.
//
// The fixture makes the split land on the summary on the very first pass (a
// window too small for even one exchange), so the guard is exercised immediately.
// Two passes are therefore expected: the one that folds the history, and the one
// degenerate pass it cannot know in advance is degenerate (only its own outcome
// says so). What must NOT happen is a third: after a pass removes nothing the
// loop has the fact it needs, and before the guard it ignored that fact and ran
// to its cap.
func TestCompaction_StopsWhenOnlyTheSummaryIsLeftToFold(t *testing.T) {
	client := &summaryClient{
		rounds:         []func(llm.Request) []llm.Event{textRound("ok")},
		summaryReplies: []string{strings.Repeat("S", 6000)},
	}
	// A window too small for even one exchange (keep budget 800 tokens, each
	// exchange ~4k): every pass folds exactly one exchange, so the loop keeps
	// making progress until one exchange is left — and then the split points at
	// the summary, which is where the old loop spun.
	history := manyTurns(6, 16000)
	compacted := collectCompacted(t, Spec{
		Client:            client,
		Prompt:            "go",
		History:           history,
		HistoryMessageIDs: historyIDs(len(history)),
		WorkDir:           t.TempDir(),
		Policy:            model.ToolPolicy{Allow: []string{model.CapFSRead}},
		ContextWindow:     2000,
		Summarize:         withSummary(client),
	})
	if len(compacted) != 2 {
		t.Fatalf("expected the folding pass plus the degenerate pass that ends the loop, got %d passes (%v)",
			len(compacted), compactedSizes(compacted))
	}
	// The first pass must have folded the stored turns rather than spun in
	// place: a guard that also blocked it would leave the conversation
	// unsummarised, which is its own regression (the model then sees a
	// conversation that does not fit).
	if compacted[0].Dropped == 0 {
		t.Fatalf("the first pass must fold the stored turns, got %d dropped", compacted[0].Dropped)
	}
	// The second pass is the one the guard is about: it removed nothing, and it
	// is the LAST one.
	if compacted[1].SummaryTokens != compacted[0].SummaryTokens {
		t.Fatalf("the stopping pass must be the one that removed nothing, got %v", compactedSizes(compacted))
	}
}

// compactedSizes renders the per-pass sizes for a failure message.
func compactedSizes(events []Event) []int {
	out := make([]int, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.SummaryTokens)
	}
	return out
}

// TestCompaction_DoesNotStopWhileItIsGainingRoom covers the other side of the
// guard: it must only stop a pass that removed nothing, never one that removed
// something. A tool loop is the shape where this matters — every round appends a
// large result, the first pass folds the stored history, and each later round
// needs its own pass. Each of those shrinks the conversation, so none may be cut
// short, however many rounds the turn takes.
//
// The failure this forbids: a guard written as "one pass per compactIfNeeded
// call" (or "one per turn") would leave the conversation growing without bound
// in exactly this loop.
func TestCompaction_DoesNotStopWhileItIsGainingRoom(t *testing.T) {
	big := strings.Repeat("x", 40000) // ~10k tokens per result
	root := t.TempDir()
	writeFile(t, root, "a.txt", big)

	const rounds = 12
	script := make([]func(llm.Request) []llm.Event, 0, rounds+1)
	for i := 0; i < rounds; i++ {
		script = append(script, toolRound(fmt.Sprintf("c%d", i), "read", `{"path":"a.txt"}`))
	}
	script = append(script, textRound("finally"))
	client := &summaryClient{
		rounds:         script,
		summaryReplies: []string{strings.Repeat("M", 6000)},
	}

	compacted := collectCompacted(t, Spec{
		Client:        client,
		Prompt:        "go",
		WorkDir:       root,
		Policy:        model.ToolPolicy{Allow: []string{model.CapFSRead}},
		ContextWindow: 4000,
		Summarize:     withSummary(client),
	})
	// The first pass has the stored tool output to fold; the rounds after it each
	// add another large result behind an over-trigger conversation, so each needs
	// its own pass. If the loop stopped early the later ones would never happen.
	if len(compacted) < 2 {
		t.Fatalf("every round adds work for the next pass; got %d compaction(s)", len(compacted))
	}
	for i, ev := range compacted {
		if ev.Dropped == 0 {
			t.Fatalf("pass %d folded nothing; the loop must stop there instead of running it", i)
		}
	}
	// The guard allows one pass that removes the previous summary (equal size) —
	// it cannot know before running it. What it does not allow is a SECOND one:
	// the sizes after that pass must all describe new, larger rounds.
	if len(compacted) > 3 && compacted[1].SummaryTokens == compacted[0].SummaryTokens &&
		compacted[2].SummaryTokens == compacted[1].SummaryTokens {
		t.Fatalf("two passes in a row removed nothing (%v)", compactedSizes(compacted))
	}
}
