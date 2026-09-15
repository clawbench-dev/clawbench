package service

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// BoundForkContext — tail window over the character budget
// ============================================================================

func TestBoundForkContext_UnderBudgetKeepsAll(t *testing.T) {
	entries := []ForkContextMessage{
		{Role: "user", Body: "first question"},
		{Role: "assistant", Body: "first answer"},
		{Role: "user", Body: "second question"},
	}
	out := BoundForkContext(entries, ForkContextOptions{BudgetChars: 1000})

	assert.Contains(t, out, "user: first question")
	assert.Contains(t, out, "assistant: first answer")
	assert.Contains(t, out, "user: second question")
	assert.NotContains(t, out, "omitted", "nothing was dropped, so no notice should appear")
}

func TestBoundForkContext_DropsOldestKeepsTail(t *testing.T) {
	entries := []ForkContextMessage{
		{Role: "user", Body: strings.Repeat("a", 300)},
		{Role: "assistant", Body: strings.Repeat("b", 300)},
		{Role: "user", Body: "the newest question"},
	}
	// Room for roughly one 300-char body plus the notice/overhead.
	out := BoundForkContext(entries, ForkContextOptions{BudgetChars: 420})

	assert.Contains(t, out, "the newest question", "newest entry must survive")
	assert.NotContains(t, out, strings.Repeat("a", 300), "oldest entry must be dropped")
	assert.Contains(t, out, "omitted", "dropping entries must be disclosed to the model")
}

func TestBoundForkContext_EmitsOmissionNoticeWithCount(t *testing.T) {
	entries := []ForkContextMessage{
		{Role: "user", Body: strings.Repeat("x", 200)},
		{Role: "assistant", Body: strings.Repeat("y", 200)},
		{Role: "user", Body: "newest"},
	}
	out := BoundForkContext(entries, ForkContextOptions{BudgetChars: 260})

	// The assistant entry is dropped; the older user entry is truncated rather
	// than dropped, so exactly one message is omitted.
	assert.Contains(t, out, "1 earlier messages were omitted")
	assert.Contains(t, out, forkTruncatedSuffix, "the older user message is truncated, not dropped")
}

// TestBoundForkContext_PreservesAllUserMessages is the core guarantee of the
// role-aware drop order: user messages are short (~51 chars on average in a real
// installation) while assistant entries carry the tool payloads (~12 KB), so
// sacrificing assistant entries first keeps every user instruction at almost no
// budget cost.
func TestBoundForkContext_PreservesAllUserMessages(t *testing.T) {
	entries := []ForkContextMessage{
		{Role: "user", Body: "first instruction"},
		{Role: "assistant", Body: strings.Repeat("a", 4000)},
		{Role: "user", Body: "second instruction"},
		{Role: "assistant", Body: strings.Repeat("b", 4000)},
		{Role: "user", Body: "third instruction"},
		{Role: "assistant", Body: strings.Repeat("c", 4000)},
	}

	// Enough for the user messages plus a little assistant context, far short of
	// all three assistant bodies.
	out := BoundForkContext(entries, ForkContextOptions{BudgetChars: 300})

	assert.Contains(t, out, "first instruction", "oldest user message must survive")
	assert.Contains(t, out, "second instruction")
	assert.Contains(t, out, "third instruction")
}

// TestBoundForkContext_UserMessagesSurviveWithoutAssistantBodies checks the
// extreme: when the budget only fits user messages, all of them are still kept
// and the assistant entries are the ones dropped.
func TestBoundForkContext_UserMessagesSurviveWithoutAssistantBodies(t *testing.T) {
	entries := []ForkContextMessage{
		{Role: "user", Body: "keep me one"},
		{Role: "assistant", Body: strings.Repeat("z", 5000)},
		{Role: "user", Body: "keep me two"},
		{Role: "assistant", Body: strings.Repeat("z", 5000)},
	}

	out := BoundForkContext(entries, ForkContextOptions{BudgetChars: 200})

	assert.Contains(t, out, "keep me one")
	assert.Contains(t, out, "keep me two")
	assert.NotContains(t, out, strings.Repeat("z", 100), "assistant bodies are the sacrifice")
	assert.Contains(t, out, "omitted")
}

// TestBoundForkContext_DropsOldestAssistantFirst pins the drop ORDER: an
// assistant entry is evicted before an older user entry.
func TestBoundForkContext_DropsOldestAssistantFirst(t *testing.T) {
	entries := []ForkContextMessage{
		{Role: "assistant", Body: strings.Repeat("OLD_ASSISTANT", 200)},
		{Role: "user", Body: "ancient instruction"},
		{Role: "assistant", Body: strings.Repeat("NEW_ASSISTANT", 200)},
		{Role: "user", Body: "latest instruction"},
	}

	out := BoundForkContext(entries, ForkContextOptions{BudgetChars: 400})

	assert.Contains(t, out, "latest instruction")
	assert.Contains(t, out, "ancient instruction", "user messages outrank assistant bodies")
}

// TestBoundForkContext_ChronologicalOrderAfterSkipping guards rendering order:
// entries are collected newest-first with assistant entries skipped, so the
// output must still read oldest-to-newest.
func TestBoundForkContext_ChronologicalOrderAfterSkipping(t *testing.T) {
	entries := []ForkContextMessage{
		{Role: "user", Body: "ALPHA"},
		{Role: "assistant", Body: strings.Repeat("x", 5000)},
		{Role: "user", Body: "BETA"},
		{Role: "assistant", Body: strings.Repeat("y", 5000)},
		{Role: "user", Body: "GAMMA"},
	}

	out := BoundForkContext(entries, ForkContextOptions{BudgetChars: 200})

	iAlpha := strings.Index(out, "ALPHA")
	iBeta := strings.Index(out, "BETA")
	iGamma := strings.Index(out, "GAMMA")
	require.NotEqual(t, -1, iAlpha)
	require.NotEqual(t, -1, iBeta)
	require.NotEqual(t, -1, iGamma)
	assert.Less(t, iAlpha, iBeta, "history must read oldest-first")
	assert.Less(t, iBeta, iGamma)
}

// TestBoundForkContext_OmissionCountCountsEveryDroppedEntry pins the omission
// count to "total entries minus kept entries". A manual tally during selection
// under-counts, because entries skipped in one phase are re-examined in the next
// and the running counter can be overwritten.
func TestBoundForkContext_OmissionCountCountsEveryDroppedEntry(t *testing.T) {
	entries := []ForkContextMessage{
		{Role: "user", Body: "old"},
		{Role: "assistant", Body: strings.Repeat("a", 300)},
		{Role: "user", Body: strings.Repeat("b", 300)},
		{Role: "assistant", Body: strings.Repeat("c", 300)},
		{Role: "user", Body: strings.Repeat("d", 300)},
	}

	// All three user entries are kept (the older two truncated in); the two
	// assistant entries are dropped, so exactly two messages are omitted.
	out := BoundForkContext(entries, ForkContextOptions{BudgetChars: 800})

	assert.Contains(t, out, "2 earlier messages were omitted",
		"both dropped assistant entries must be counted")
	assert.Contains(t, out, "old", "the oldest user entry survives")
}

// TestBoundForkContext_NewestUserSurvivesTinyBudget guards the priority this
// whole file exists to enforce: a user message larger than the budget is
// truncated, never dropped. (The budget here must clear the wrapper floor of 118
// runes, below which only the wrapper can be emitted.)
func TestBoundForkContext_NewestUserSurvivesTinyBudget(t *testing.T) {
	entries := []ForkContextMessage{
		{Role: "user", Body: strings.Repeat("z", 5000)},
	}
	out := BoundForkContext(entries, ForkContextOptions{BudgetChars: 200})

	assert.Contains(t, out, "user: ")
	assert.Contains(t, out, forkTruncatedSuffix)
	assert.LessOrEqual(t, utf8.RuneCountInString(out), 200,
		"the budget must bound the output even on the truncate path")
}

// TestBoundForkContext_HugeUserEntrySurvivesDespiteOlderAssistant is the
// regression test for the capitalized-role bug: the handler capitalizes roles for
// display, so comparing e.Role against the lowercase role constant classified
// every entry as an assistant and inverted the priority. Here the newest entry is
// a huge USER message and an older assistant entry would fit — the user must win.
func TestBoundForkContext_HugeUserEntrySurvivesDespiteOlderAssistant(t *testing.T) {
	entries := []ForkContextMessage{
		{Role: "Assistant", Body: "OLD ASSISTANT CHATTER"},
		{Role: "User", Body: strings.Repeat("L", 5000)},
	}
	out := BoundForkContext(entries, ForkContextOptions{
		CapitalizeRoles: true,
		Header:          "[Below is the conversation history from before this session. Continue based on this context.]\n\n",
		Footer:          "[End of conversation history. Now answer the user's new question.]\n\n",
		BudgetChars:     400,
	})

	assert.Contains(t, out, "User: ", "the newest user message must be kept")
	assert.NotContains(t, out, "OLD ASSISTANT CHATTER",
		"an older assistant entry must not displace the newest user message")
}

// TestBoundForkContext_OversizedUserDoesNotEvictOlderUser covers the fair-share
// rule: one oversized user message must not consume the entire budget and evict
// older instructions. The older (small) user entry is kept in full; the oversized
// newer one is truncated into what remains.
func TestBoundForkContext_OversizedUserDoesNotEvictOlderUser(t *testing.T) {
	entries := []ForkContextMessage{
		{Role: "user", Body: "CONSTRAINT: never touch prod"},
		{Role: "user", Body: strings.Repeat("L", 5000)},
		{Role: "assistant", Body: strings.Repeat("A", 5000)},
	}
	out := BoundForkContext(entries, ForkContextOptions{BudgetChars: 1000})

	assert.Contains(t, out, "CONSTRAINT: never touch prod",
		"an older user instruction must survive a newer oversized user message")
	assert.LessOrEqual(t, utf8.RuneCountInString(out), 1000)
}

// TestBoundForkContext_UserEntriesWinOverAssistantBudget verifies user entries are
// selected before assistant entries: an older user message is kept even though a
// newer assistant entry could have used that budget.
func TestBoundForkContext_UserEntriesWinOverAssistantBudget(t *testing.T) {
	entries := []ForkContextMessage{
		{Role: "user", Body: "keep this instruction"},
		{Role: "assistant", Body: strings.Repeat("A", 250)},
	}
	// Room for roughly one entry only.
	out := BoundForkContext(entries, ForkContextOptions{BudgetChars: 200})

	assert.Contains(t, out, "keep this instruction")
}

// TestBoundForkContext_TruncatePathStaysWithinBudget pins the budget invariant
// on the truncate branch with an older entry present, which makes the omission
// notice appear. The truncate branch must reserve the entry separator as well as
// the truncation suffix; omitting the separator overshoots by exactly 2 runes.
func TestBoundForkContext_TruncatePathStaysWithinBudget(t *testing.T) {
	entries := []ForkContextMessage{
		{Role: "user", Body: "older question"},
		{Role: "assistant", Body: strings.Repeat("y", 5000)},
	}

	// Budgets comfortably above the wrapper floor (the notice alone is ~110
	// runes here), so the invariant must hold exactly.
	for _, budget := range []int{200, 400, 1000} {
		out := BoundForkContext(entries, ForkContextOptions{BudgetChars: budget})
		assert.LessOrEqual(t, utf8.RuneCountInString(out), budget,
			"budget %d must bound the output", budget)
	}
}

// TestBoundForkContext_BudgetBelowWrapperFloor documents the one case where the
// budget cannot be honored: when header+notice+footer alone exceed it, the
// wrapper is still emitted (so the model learns history was compressed) and the
// output necessarily exceeds the budget. The PATCH validator and the frontend
// min keep real configurations far above this floor.
func TestBoundForkContext_BudgetBelowWrapperFloor(t *testing.T) {
	entries := []ForkContextMessage{
		{Role: "user", Body: "older"},
		{Role: "assistant", Body: strings.Repeat("y", 5000)},
	}
	opts := ForkContextOptions{Header: "[H]\n\n", Footer: "[F]\n\n", BudgetChars: 5}

	out := BoundForkContext(entries, opts)

	assert.Contains(t, out, "[H]", "the wrapper is emitted even below the floor")
	assert.Contains(t, out, "[F]")
	assert.Contains(t, out, "omitted")
}

// TestBoundForkContext_HandlerOptionsStaysWithinBudget covers the handler's
// option set (header + footer + capitalized roles), which reserves the most
// space and so has the least room for the separator slip.
func TestBoundForkContext_HandlerOptionsStaysWithinBudget(t *testing.T) {
	entries := []ForkContextMessage{
		{Role: "User", Body: "older question"},
		{Role: "User", Body: "another older one"},
		{Role: "Assistant", Body: strings.Repeat("z", 9000)},
	}
	opts := ForkContextOptions{
		Header:          "[Below is the conversation history from before this session. Continue based on this context.]\n\n",
		Footer:          "[End of conversation history. Now answer the user's new question.]\n\n",
		CapitalizeRoles: true,
		BudgetChars:     400,
	}

	out := BoundForkContext(entries, opts)
	assert.LessOrEqual(t, utf8.RuneCountInString(out), opts.BudgetChars)
}

// TestBoundForkContext_NoticeDoesNotPromiseCompleteness guards the wording: the
// newest entry is truncated when it alone exceeds the budget, so a notice
// claiming the remaining messages are complete would be a lie.
func TestBoundForkContext_NoticeDoesNotPromiseCompleteness(t *testing.T) {
	entries := []ForkContextMessage{
		{Role: "user", Body: "older"},
		{Role: "assistant", Body: strings.Repeat("q", 5000)},
	}
	out := BoundForkContext(entries, ForkContextOptions{BudgetChars: 200})

	assert.Contains(t, out, "omitted")
	assert.NotContains(t, out, "reproduced in full")
}

func TestBoundForkContext_UnicodeCountsRunesNotBytes(t *testing.T) {
	// Two entries of 100 CJK runes each. Rune-measured they both fit, so the
	// output must be complete and notice-free. Byte-measured each entry costs
	// 3x (300 bytes) and would force the older one out, adding an omission
	// notice — that difference is what this test pins down.
	older := strings.Repeat("中", 100)
	newest := strings.Repeat("文", 100)
	entries := []ForkContextMessage{
		{Role: "user", Body: older},
		{Role: "assistant", Body: newest},
	}

	out := BoundForkContext(entries, ForkContextOptions{BudgetChars: 350})

	assert.Contains(t, out, older, "older entry fits when measured in characters")
	assert.Contains(t, out, newest)
	assert.NotContains(t, out, "omitted", "byte-based measuring would have dropped an entry")
	assert.NotContains(t, out, forkTruncatedSuffix)
}

func TestBoundForkContext_TinyBudgetStillEmitsHeaderFooter(t *testing.T) {
	entries := []ForkContextMessage{{Role: "user", Body: "hello"}}
	out := BoundForkContext(entries, ForkContextOptions{
		Header:      "[H]",
		Footer:      "[F]",
		BudgetChars: 1, // smaller than header+footer+notice
	})

	assert.Contains(t, out, "[H]")
	assert.Contains(t, out, "[F]")
}

func TestBoundForkContext_EmptyEntriesReturnsEmpty(t *testing.T) {
	out := BoundForkContext(nil, ForkContextOptions{
		Header:      "[H]",
		Footer:      "[F]",
		BudgetChars: 1000,
	})
	assert.Equal(t, "", out, "no history means no injection at all, not an empty wrapper")
}

func TestBoundForkContext_ZeroBudgetFallsBackToDefault(t *testing.T) {
	entries := []ForkContextMessage{{Role: "user", Body: "small"}}
	out := BoundForkContext(entries, ForkContextOptions{BudgetChars: 0})

	assert.Contains(t, out, "user: small", "a zero/negative budget must not mean 'inject nothing'")
}

// ============================================================================
// TrimToolInput — L0 stripping of content-valued keys
// ============================================================================

func TestTrimToolInput_StripsLargeContentKeys(t *testing.T) {
	big := strings.Repeat("c", 500)
	input := json.RawMessage(`{"file_path":"/a/b.go","content":"` + big + `","old_string":"x"}`)

	out := TrimToolInput(input, 200)

	require.True(t, json.Valid(out), "trimmed input must remain valid JSON")
	var obj map[string]any
	require.NoError(t, json.Unmarshal(out, &obj))
	assert.Equal(t, "/a/b.go", obj["file_path"], "locator keys must be preserved")
	assert.Equal(t, "[omitted 500 chars]", obj["content"])
	assert.Equal(t, "x", obj["old_string"], "small values are left alone")
}

func TestTrimToolInput_KeepsSmallValuesVerbatim(t *testing.T) {
	input := json.RawMessage(`{"command":"ls -la","file_path":"/x"}`)
	out := TrimToolInput(input, 200)
	assert.Equal(t, string(input), string(out), "inputs under the threshold must be byte-identical")
}

func TestTrimToolInput_NonObjectReturnedUnchanged(t *testing.T) {
	for _, raw := range []string{`"just a string"`, `[1,2,3]`, `not json`, ``} {
		out := TrimToolInput(json.RawMessage(raw), 200)
		assert.Equal(t, raw, string(out))
	}
}

func TestTrimToolInput_ZeroThresholdDisablesTrimming(t *testing.T) {
	input := json.RawMessage(`{"content":"` + strings.Repeat("d", 900) + `"}`)
	out := TrimToolInput(input, 0)
	assert.Equal(t, string(input), string(out))
}

// TestTrimToolInput_StripsNestedEditsArray covers backends that normalize
// multi-edit payloads into a nested shape (Pi remaps to
// {"edits":[{"old_string":...,"new_string":...}]}). A top-level-only walk would
// leave the entire patch text in the injected history.
func TestTrimToolInput_StripsNestedEditsArray(t *testing.T) {
	big := strings.Repeat("p", 700)
	input := json.RawMessage(`{"file_path":"/a/b.go","edits":[{"old_string":"` + big + `","new_string":"` + big + `"}]}`)

	out := TrimToolInput(input, 200)

	require.True(t, json.Valid(out))
	assert.Contains(t, string(out), `"/a/b.go"`, "locator key at the top level survives")
	assert.Contains(t, string(out), "[omitted 700 chars]")
	assert.NotContains(t, string(out), big, "nested patch text must not be injected")
}

func TestTrimToolInput_NestedLocatorKeysPreserved(t *testing.T) {
	// Only content keys are stripped; a nested file_path must survive.
	input := json.RawMessage(`{"edits":[{"file_path":"/keep/me.go","old_string":"` + strings.Repeat("k", 400) + `"}]}`)

	out := TrimToolInput(input, 200)

	assert.Contains(t, string(out), "/keep/me.go")
}

func TestTrimToolInput_HandlesDeepNestingWithoutRecursingAway(t *testing.T) {
	// A pathologically nested payload must terminate (depth cap) and still be
	// returned as valid JSON.
	deep := `{"a":{"b":{"c":{"d":{"e":{"f":{"g":{"h":{"i":{"j":{"content":"` + strings.Repeat("n", 900) + `"}}}}}}}}}}}`
	out := TrimToolInput(json.RawMessage(deep), 200)
	require.True(t, json.Valid(out))
}

func TestTrimToolInput_NoChangeReturnsOriginalBytes(t *testing.T) {
	// Unchanged inputs must be returned untouched so callers can compare bytes.
	input := json.RawMessage(`{"edits":[{"old_string":"small"}]}`)
	out := TrimToolInput(input, 200)
	assert.Equal(t, string(input), string(out))
}

// ============================================================================
// RenderForkContextMessages — option-driven rendering
// ============================================================================

func TestRenderForkContextMessages_CapitalizesRolesWhenAsked(t *testing.T) {
	msgs := []model.ChatMessage{
		{Role: "user", Content: `{"blocks":[{"type":"text","text":"hi"}]}`},
		{Role: "assistant", Content: `{"blocks":[{"type":"text","text":"yo"}]}`},
	}

	plain := RenderForkContextMessages(msgs, nil, ForkContextOptions{})
	require.Len(t, plain, 2)
	assert.Equal(t, "user", plain[0].Role)
	assert.Equal(t, "assistant", plain[1].Role)

	caps := RenderForkContextMessages(msgs, nil, ForkContextOptions{CapitalizeRoles: true})
	require.Len(t, caps, 2)
	assert.Equal(t, "User", caps[0].Role)
	assert.Equal(t, "Assistant", caps[1].Role)
}

func TestRenderForkContextMessages_PlainTextFallbackGated(t *testing.T) {
	// Non-block content is only recovered when the option is on; the service
	// path (option off) must skip it, as it always has.
	msgs := []model.ChatMessage{{Role: "user", Content: "plain text, not blocks"}}

	assert.Empty(t, RenderForkContextMessages(msgs, nil, ForkContextOptions{}))

	withFallback := RenderForkContextMessages(msgs, nil, ForkContextOptions{PlainTextFallback: true})
	require.Len(t, withFallback, 1)
	assert.Contains(t, withFallback[0].Body, "plain text")
}

func TestRenderForkContextMessages_TrimsToolInputInPlace(t *testing.T) {
	big := strings.Repeat("e", 800)
	msgs := []model.ChatMessage{
		{Role: "assistant", Content: `{"blocks":[{"type":"tool_use","name":"Write","id":"t1","status":"success","done":true}]}`},
	}
	toolCallMap := map[string]*ToolCallRecord{
		"t1": {ToolID: "t1", Name: "Write", Input: json.RawMessage(`{"file_path":"/f.go","content":"` + big + `"}`)},
	}

	entries := RenderForkContextMessages(msgs, toolCallMap, ForkContextOptions{})
	require.Len(t, entries, 1)
	// The tool input is embedded as a JSON string inside the <tool_use> wrapper,
	// so its quotes arrive escaped.
	assert.Contains(t, entries[0].Body, `\"file_path\":\"/f.go\"`, "locator key survives")
	assert.Contains(t, entries[0].Body, "[omitted 800 chars]")
	assert.NotContains(t, entries[0].Body, big, "the whole file body must not be injected")

	// The caller's record must not be mutated — the map is shared with other readers.
	assert.Contains(t, string(toolCallMap["t1"].Input), big, "input records must not be mutated in place")
}

func TestRenderForkContextMessages_SkipsThinkingAndEmpty(t *testing.T) {
	msgs := []model.ChatMessage{
		{Role: "user", Content: `{"blocks":[{"type":"thinking","text":"hmm"}]}`},
		{Role: "assistant", Content: `{"blocks":[]}`},
		{Role: "system", Content: `{"blocks":[{"type":"text","text":"sys"}]}`},
	}
	assert.Empty(t, RenderForkContextMessages(msgs, nil, ForkContextOptions{CapitalizeRoles: true}))
}

// TestRenderForkContextMessages_TolerantOfNonCanonicalBlockJSON guards against
// gating on a literal `{"blocks":` prefix: a wrapper with leading whitespace or
// differently ordered keys is still a valid block message and must not be
// silently dropped from the injected history.
func TestRenderForkContextMessages_TolerantOfNonCanonicalBlockJSON(t *testing.T) {
	msgs := []model.ChatMessage{
		{Role: "user", Content: `  {"blocks":[{"type":"text","text":"leading space"}]}`},
		{Role: "user", Content: `{"extra":1,"blocks":[{"type":"text","text":"reordered"}]}`},
	}

	entries := RenderForkContextMessages(msgs, nil, ForkContextOptions{})

	require.Len(t, entries, 2, "both are block messages and must be rendered")
	assert.Contains(t, entries[0].Body, "leading space")
	assert.Contains(t, entries[1].Body, "reordered")
}

// TestRenderForkContextMessages_PlainTextUserMessages covers the handler path's
// critical dependency: web chat persists user messages as PLAIN TEXT, not as a
// blocks wrapper. Without PlainTextFallback the user's own questions would be
// missing from the fork context entirely.
func TestRenderForkContextMessages_PlainTextUserMessages(t *testing.T) {
	msgs := []model.ChatMessage{
		{Role: "user", Content: "what does this function do?"},
		{Role: "assistant", Content: `{"blocks":[{"type":"text","text":"it parses X"}]}`},
	}

	entries := RenderForkContextMessages(msgs, nil, ForkContextOptions{PlainTextFallback: true, CapitalizeRoles: true})

	require.Len(t, entries, 2)
	assert.Equal(t, "User", entries[0].Role)
	assert.Contains(t, entries[0].Body, "what does this function do?")
	assert.Equal(t, "Assistant", entries[1].Role)
}

// ============================================================================
// DB-level: configured budget is honored end to end
// ============================================================================

func TestBuildForkContext_RespectsConfiguredBudget(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "fork-budget"
	// Old message big enough that it cannot fit alongside the newest one.
	_, err := WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'user', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"text","text":"`+strings.Repeat("o", 900)+`"}]}`, sessionID,
	)
	require.NoError(t, err)
	_, err = WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'user', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"text","text":"newest question"}]}`, sessionID,
	)
	require.NoError(t, err)

	prev := model.ChatForkContextBudget
	model.ChatForkContextBudget = 400
	defer func() { model.ChatForkContextBudget = prev }()

	out := BuildForkContext(sessionID)
	// Both are user messages, so both survive: the newest verbatim and the older
	// one truncated. Nothing is omitted.
	assert.Contains(t, out, "newest question")
	assert.NotContains(t, out, strings.Repeat("o", 900), "the oversized body is truncated")
	assert.LessOrEqual(t, utf8.RuneCountInString(out), 400)
}

// TestBuildForkContext_DropsAssistantWhenBudgetTight is the DB-level counterpart:
// with an assistant entry competing for the same budget, the assistant is the one
// dropped while the user message survives.
func TestBuildForkContext_DropsAssistantWhenBudgetTight(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	sessionID := "fork-budget-asst"
	_, err := WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'user', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"text","text":"the user instruction"}]}`, sessionID,
	)
	require.NoError(t, err)
	_, err = WriteExec(
		"INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', 0)",
		"/proj", `{"blocks":[{"type":"text","text":"`+strings.Repeat("a", 900)+`"}]}`, sessionID,
	)
	require.NoError(t, err)

	prev := model.ChatForkContextBudget
	model.ChatForkContextBudget = 400
	defer func() { model.ChatForkContextBudget = prev }()

	out := BuildForkContext(sessionID)
	assert.Contains(t, out, "the user instruction", "the user message must win the budget")
	assert.NotContains(t, out, strings.Repeat("a", 900))
	assert.Contains(t, out, "omitted", "the dropped assistant entry is disclosed")
	assert.LessOrEqual(t, utf8.RuneCountInString(out), 400)
}
