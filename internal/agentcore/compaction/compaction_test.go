package compaction

import (
	"strings"
	"testing"

	"clawbench/internal/agentcore/llm"
)

// userMsg and assistantMsg build single-text-part messages.
func userMsg(text string) llm.Message      { return llm.TextMessage(llm.RoleUser, text) }
func assistantMsg(text string) llm.Message { return llm.TextMessage(llm.RoleAssistant, text) }

// TestEstimateTokens_BiasesUp pins the direction of the estimate's error.
//
// Over-estimating compacts slightly early, which is harmless. Under-estimating
// lets a conversation run past the window, which is a hard API failure mid-turn.
// The bias must therefore always point up.
func TestEstimateTokens_BiasesUp(t *testing.T) {
	if got := EstimateTokens(""); got != 0 {
		t.Fatalf("empty text must be 0 tokens, got %d", got)
	}
	// Latin: roughly one token per four characters. 400 chars ≈ 100 tokens; a
	// real tokenizer gives 80-110, so 100 must not be below the low end.
	got := EstimateTokens(strings.Repeat("abcd", 100))
	if got < 90 {
		t.Fatalf("latin estimate must not be optimistic, got %d for 400 chars", got)
	}
	// CJK: roughly one token per character.
	got = EstimateTokens(strings.Repeat("中文", 100))
	if got < 190 {
		t.Fatalf("CJK estimate must count per character, got %d for 200 chars", got)
	}
}

// TestMessageTokens_CountsToolPayloads pins that tool arguments and results are
// counted. They are where the bulk of a long turn's tokens actually live, so a
// counter that skipped them would never trigger compaction during a tool loop.
func TestMessageTokens_CountsToolPayloads(t *testing.T) {
	big := strings.Repeat("x", 4000)
	msg := llm.Message{Role: llm.RoleTool, Parts: []llm.Part{
		{ToolResult: &llm.ToolResult{CallID: "c1", Parts: []llm.Part{{Text: big}}}},
	}}
	if got := MessageTokens(msg); got < 900 {
		t.Fatalf("a 4000-char tool result must count, got %d tokens", got)
	}

	callMsg := llm.Message{Role: llm.RoleAssistant, Parts: []llm.Part{
		{ToolCall: &llm.ToolCall{ID: "c1", Name: "write", Arguments: big}},
	}}
	if got := MessageTokens(callMsg); got < 900 {
		t.Fatalf("a 4000-char tool argument must count, got %d tokens", got)
	}
}

// TestMessageTokens_CountsImages pins that an image is not treated as free.
func TestMessageTokens_CountsImages(t *testing.T) {
	msg := llm.Message{Role: llm.RoleUser, Parts: []llm.Part{
		{Image: &llm.Image{MediaType: "image/png", Data: []byte{1}}},
	}}
	if got := MessageTokens(msg); got < 500 {
		t.Fatalf("an image must carry a token estimate, got %d", got)
	}
}

// TestShouldCompact_UnknownWindowNeverCompacts pins the fail-safe direction for
// an unknown model.
//
// A model absent from the catalog is usually a self-hosted endpoint with a
// generous window. Guessing a small one would compact conversations that fit
// perfectly, losing information for no reason.
func TestShouldCompact_UnknownWindowNeverCompacts(t *testing.T) {
	huge := []llm.Message{userMsg(strings.Repeat("x", 100000))}
	if ShouldCompact("", huge, 0) {
		t.Fatal("an unknown window must never trigger compaction")
	}
	if ShouldCompact("", huge, -1) {
		t.Fatal("a negative window must never trigger compaction")
	}
}

// TestShouldCompact_TriggersBeforeTheLimit pins the ratio: compaction must fire
// while there is still room for the turn to run.
func TestShouldCompact_TriggersBeforeTheLimit(t *testing.T) {
	window := 10000
	// 74% full: must NOT compact yet.
	near := []llm.Message{userMsg(strings.Repeat("x", int(float64(window)*0.74)*4))}
	if ShouldCompact("", near, window) {
		t.Fatalf("74%% full must not compact (trigger is %.0f%%)", TriggerRatio*100)
	}
	// 80% full: must compact.
	over := []llm.Message{userMsg(strings.Repeat("x", int(float64(window)*0.80)*4))}
	if !ShouldCompact("", over, window) {
		t.Fatalf("80%% full must compact (trigger is %.0f%%)", TriggerRatio*100)
	}
}

// TestSplitPoint_KeepsRecentExchanges pins the "summary + recent verbatim" rule.
func TestSplitPoint_KeepsRecentExchangesWithinBudget(t *testing.T) {
	msgs := make([]llm.Message, 0, 10)
	for range 5 {
		msgs = append(msgs, userMsg("q"), assistantMsg("a"))
	}
	// Each exchange is exactly 10 tokens. A 25-token budget fits 2 exchanges,
	// so the split lands at the 4th exchange's start (index 6).
	split := SplitPoint(msgs, 25)
	if split != 6 {
		t.Fatalf("expected split at 6 (keeping the newest exchanges that fit), got %d", split)
	}
	if kept := len(msgs[split:]); kept != 4 {
		t.Fatalf("the kept tail must be 2 exchanges (4 messages), got %d messages", kept)
	}

	// A budget large enough for everything keeps the whole conversation.
	if got := SplitPoint(msgs, 1_000_000); got != 0 {
		t.Fatalf("a budget that fits everything must yield 0, got %d", got)
	}
}

// TestSplitPoint_NeverSeparatesToolCallFromResult pins the invariant that makes
// a compacted conversation still valid.
//
// Cutting between an assistant's tool call and the tool result that answers it
// produces a conversation both wire protocols reject outright — the result
// references a call id that is no longer there.
func TestSplitPoint_NeverSeparatesToolCallFromResult(t *testing.T) {
	msgs := []llm.Message{
		userMsg("first question"),
		assistantMsg("first answer"),
		// This exchange contains a full tool round: ask, result, answer.
		userMsg("read the file"),
		{Role: llm.RoleAssistant, Parts: []llm.Part{
			{ToolCall: &llm.ToolCall{ID: "c1", Name: "read", Arguments: `{"path":"a"}`}},
		}},
		{Role: llm.RoleTool, Parts: []llm.Part{
			{ToolResult: &llm.ToolResult{CallID: "c1", Parts: []llm.Part{{Text: "file body"}}}},
		}},
		assistantMsg("the file says X"),
	}

	// A budget that fits only the newest exchange must split at the second
	// exchange's start, never inside it.
	split := SplitPoint(msgs, 15)
	if split != 2 {
		t.Fatalf("expected the split at the second exchange's start (2), got %d", split)
	}
	if msgs[split].Role != llm.RoleUser || isToolResultOnly(msgs[split]) {
		t.Fatalf("the split must land on a user message, got role %q", msgs[split].Role)
	}
	// The kept tail must contain the tool call AND its result — never one
	// without the other.
	var sawCall, sawResult bool
	for _, m := range msgs[split:] {
		for _, p := range m.Parts {
			if p.ToolCall != nil {
				sawCall = true
			}
			if p.ToolResult != nil {
				sawResult = true
			}
		}
	}
	if !sawCall || !sawResult {
		t.Fatalf("the kept tail must contain both the tool call and its result (call=%v result=%v)", sawCall, sawResult)
	}
}

// TestSplitPoint_TooFewExchangesCompactsNothing pins that a short conversation
// is left alone.
func TestSplitPoint_TooFewExchangesCompactsNothing(t *testing.T) {
	msgs := []llm.Message{userMsg("q"), assistantMsg("a")}
	if got := SplitPoint(msgs, 10); got != 0 {
		t.Fatalf("a single exchange must not be compacted, got split %d", got)
	}
	if got := SplitPoint(nil, 100); got != 0 {
		t.Fatalf("an empty conversation must yield split 0, got %d", got)
	}
	if got := SplitPoint(msgs, 0); got != 0 {
		t.Fatalf("a zero budget must yield split 0, got %d", got)
	}
}

// TestRenderForSummary_OmitsFullToolOutput pins that the summarizer input does
// not include whole file contents.
//
// A summary that receives the entire conversation it is meant to replace has
// not saved anything: the summarizer's own context would overflow instead.
func TestRenderForSummary_OmitsFullToolOutput(t *testing.T) {
	big := strings.Repeat("FILE-CONTENT-", 500)
	msgs := []llm.Message{
		userMsg("read it"),
		{Role: llm.RoleAssistant, Parts: []llm.Part{
			{ToolCall: &llm.ToolCall{ID: "c1", Name: "read", Arguments: `{"path":"big.txt"}`}},
		}},
		{Role: llm.RoleTool, Parts: []llm.Part{
			{ToolResult: &llm.ToolResult{CallID: "c1", Parts: []llm.Part{{Text: big}}}},
		}},
	}

	rendered := RenderForSummary(msgs)
	if len(rendered) > 2000 {
		t.Fatalf("rendered summary input must be bounded, got %d chars", len(rendered))
	}
	if !strings.Contains(rendered, "read") {
		t.Fatal("the tool NAME must survive so the summary records what was done")
	}
	if strings.Count(rendered, "FILE-CONTENT-") > 40 {
		t.Fatal("the tool output must be truncated, not sent in full")
	}
}

// TestDigestSummary_KeepsFactsAndDropsFileBodies pins the no-summarizer
// fallback.
//
// It cannot produce good prose, but it must preserve what a later turn needs:
// what was asked and which tools ran. Losing those makes the fallback useless.
func TestDigestSummary_KeepsFactsAndDropsFileBodies(t *testing.T) {
	rendered := RenderForSummary([]llm.Message{
		userMsg("find the config"),
		{Role: llm.RoleAssistant, Parts: []llm.Part{
			{ToolCall: &llm.ToolCall{ID: "c1", Name: "grep", Arguments: `{"pattern":"config"}`}},
		}},
		{Role: llm.RoleTool, Parts: []llm.Part{
			{ToolResult: &llm.ToolResult{CallID: "c1", Parts: []llm.Part{{Text: "config.go:12: config = load()"}}}},
		}},
	})

	got := DigestSummary(rendered)
	if !strings.Contains(got, "find the config") {
		t.Fatalf("the user's question must survive, got:\n%s", got)
	}
	if !strings.Contains(got, "grep") {
		t.Fatalf("the tool that ran must survive, got:\n%s", got)
	}
	if strings.Contains(got, "load()") {
		t.Fatalf("tool output must be dropped (it is what makes a summary large), got:\n%s", got)
	}
}

// TestDigestSummary_EmptyForNothing pins that an empty input yields an empty
// summary, which the runner treats as "compaction did not help".
func TestDigestSummary_EmptyForNothing(t *testing.T) {
	if got := DigestSummary("   \n  \n"); got != "" {
		t.Fatalf("empty input must yield an empty summary, got %q", got)
	}
}

// TestToolResultSplitPoint_FoldsInsideOneExchange pins the single-turn path.
//
// A turn that reads several large files exceeds any window without the
// conversation ever ending, so a turn-boundary-only design waits forever.
func TestToolResultSplitPoint_FoldsInsideOneExchange(t *testing.T) {
	old := KeepRecentToolRounds
	KeepRecentToolRounds = 1
	defer func() { KeepRecentToolRounds = old }()

	msgs := make([]llm.Message, 0, 7) // one prompt + 3 × (call, result)
	msgs = append(msgs, userMsg("read three files"))
	for i := range 3 {
		id := string(rune('a' + i))
		msgs = append(
			msgs,
			llm.Message{Role: llm.RoleAssistant, Parts: []llm.Part{
				{ToolCall: &llm.ToolCall{ID: id, Name: "read", Arguments: `{"path":"f"}`}},
			}},
			llm.Message{Role: llm.RoleTool, Parts: []llm.Part{
				{ToolResult: &llm.ToolResult{CallID: id, Parts: []llm.Part{{Text: strings.Repeat("x", 5000)}}}},
			}},
		)
	}

	cut := ToolResultSplitPoint(msgs)
	if cut <= 0 {
		t.Fatal("three tool rounds with only one kept must produce a cut")
	}
	// The cut must land on a tool-result message.
	if !isToolResultOnly(msgs[cut]) {
		t.Fatalf("the cut must land on a tool result, got role %q", msgs[cut].Role)
	}

	folded := FoldToolResults(msgs, cut)
	if len(folded) != len(msgs) {
		t.Fatalf("folding must preserve message count (the calls stay), got %d vs %d", len(folded), len(msgs))
	}
	// Folded results shrink; kept ones do not.
	before := ConversationTokens("", msgs)
	after := ConversationTokens("", folded)
	if after >= before {
		t.Fatalf("folding must reduce tokens, got %d -> %d", before, after)
	}
	// Every call must still have a result referencing it — the API rejects a
	// call whose result is missing.
	calls := map[string]bool{}
	results := map[string]bool{}
	for _, m := range folded {
		for _, p := range m.Parts {
			if p.ToolCall != nil {
				calls[p.ToolCall.ID] = true
			}
			if p.ToolResult != nil {
				results[p.ToolResult.CallID] = true
			}
		}
	}
	for id := range calls {
		if !results[id] {
			t.Fatalf("call %q lost its result, which the API rejects", id)
		}
	}
}

// TestToolResultSplitPoint_NothingToFold pins the guards.
func TestToolResultSplitPoint_NothingToFold(t *testing.T) {
	if got := ToolResultSplitPoint(nil); got != 0 {
		t.Fatalf("an empty conversation must yield 0, got %d", got)
	}
	// A single tool round is not worth folding.
	msgs := []llm.Message{
		userMsg("read"),
		{Role: llm.RoleTool, Parts: []llm.Part{
			{ToolResult: &llm.ToolResult{CallID: "c1", Parts: []llm.Part{{Text: "body"}}}},
		}},
	}
	if got := ToolResultSplitPoint(msgs); got != 0 {
		t.Fatalf("one tool round must not be folded, got %d", got)
	}
}

// TestFoldToolResults_NoOpForInvalidCut pins that a bad cut changes nothing
// rather than silently emptying the conversation.
func TestFoldToolResults_NoOpForInvalidCut(t *testing.T) {
	msgs := []llm.Message{userMsg("q")}
	if got := FoldToolResults(msgs, 0); len(got) != 1 {
		t.Fatal("cut 0 must be a no-op")
	}
	if got := FoldToolResults(msgs, 99); len(got) != 1 {
		t.Fatal("an out-of-range cut must be a no-op")
	}
}

// TestSummaryMessage_IsFramedUnmistakably pins that the model can tell a
// summary from something the user actually said.
func TestSummaryMessage_IsFramedUnmistakably(t *testing.T) {
	msg := SummaryMessage("the user wants X")
	if msg.Role != llm.RoleUser {
		t.Fatalf("the summary must be a user message (a mid-conversation system message is not expressible everywhere), got %q", msg.Role)
	}
	text := msg.Parts[0].Text
	if !strings.Contains(text, "compacted") || !strings.Contains(text, "the user wants X") {
		t.Fatalf("the summary must be framed and included, got %q", text)
	}
	if !strings.HasPrefix(text, "[") || !strings.Contains(text, "[End of summary") {
		t.Fatalf("the summary must be bracketed so the model can tell where it ends, got %q", text)
	}
}

// TestToolResultSplitPoint_KeepsExactlyKResults pins the arithmetic a reviewer
// suspected was off by one: the cut is the index of the K-th NEWEST result, and
// folding replaces only results BEFORE that index, so exactly K stay verbatim.
//
// It matters because the kept tail is what the model can still read; folding one
// extra result silently removes evidence from the turn being answered.
func TestToolResultSplitPoint_KeepsExactlyKResults(t *testing.T) {
	old := KeepRecentToolRounds
	KeepRecentToolRounds = 2
	defer func() { KeepRecentToolRounds = old }()

	msgs := []llm.Message{userMsg("read four files")}
	for i := range 4 {
		id := string(rune('a' + i))
		msgs = append(msgs,
			llm.Message{Role: llm.RoleAssistant, Parts: []llm.Part{
				{ToolCall: &llm.ToolCall{ID: id, Name: "read", Arguments: `{}`}},
			}},
			llm.Message{Role: llm.RoleTool, Parts: []llm.Part{
				{ToolResult: &llm.ToolResult{CallID: id, Parts: []llm.Part{{Text: strings.Repeat("x", 5000)}}}},
			}},
		)
	}

	cut := ToolResultSplitPoint(msgs)
	folded := FoldToolResults(msgs, cut)

	// Walk the results newest-first and count the ones that kept their body.
	kept := 0
	for i := len(folded) - 1; i >= 0; i-- {
		m := folded[i]
		if len(m.Parts) != 1 || m.Parts[0].ToolResult == nil {
			continue
		}
		body := m.Parts[0].ToolResult.Parts[0].Text
		if len(body) >= 5000 {
			kept++
		}
	}
	if kept != KeepRecentToolRounds {
		t.Fatalf("expected the last %d results verbatim, got %d (cut=%d)", KeepRecentToolRounds, kept, cut)
	}
}
