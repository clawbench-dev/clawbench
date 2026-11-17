package agentcore

import (
	"encoding/json"
	"testing"

	"clawbench/internal/agentcore/llm"
)

// intPtr is the provider's call index on a streamed event.
func intPtr(i int) *int { return &i }

// callStart builds a tool-call start event for provider slot index.
func callStart(index int, id, name string) llm.Event {
	return llm.Event{Kind: llm.KindToolCallStart, ToolCall: &llm.ToolCall{ID: id, Name: name, Index: intPtr(index)}}
}

// callDelta builds an argument fragment for provider slot index.
func callDelta(index int, args string) llm.Event {
	return llm.Event{Kind: llm.KindToolCallDelta, ToolCall: &llm.ToolCall{Arguments: args, Index: intPtr(index)}}
}

// TestRoundAccumulator_InterleavedParallelCalls pins the bug that made two
// tools unparseable: OpenAI streams parallel calls INTERLEAVED (fragment for
// call 0, fragment for call 1, more for call 0), and appending every fragment
// to "the call started last" spliced the two argument objects together into
// JSON that is not valid — so the tool received mangled arguments or none.
func TestRoundAccumulator_InterleavedParallelCalls(t *testing.T) {
	a := newRoundAccumulator(true)
	r := &runner{ch: make(chan Event, 64)}
	events := []llm.Event{
		callStart(0, "c0", "read"),
		callStart(1, "c1", "bash"),
		callDelta(0, `{"path":`),
		callDelta(1, `{"command":`),
		callDelta(0, `"a.txt"}`),
		callDelta(1, `"ls"}`),
	}
	for _, ev := range events {
		if err := a.apply(ev, r); err != nil {
			t.Fatalf("apply: %v", err)
		}
	}
	out := a.result(1)
	if len(out.calls) != 2 {
		t.Fatalf("expected both calls, got %d", len(out.calls))
	}
	want := []string{`{"path":"a.txt"}`, `{"command":"ls"}`}
	for i, want := range want {
		if out.calls[i].Arguments != want {
			t.Fatalf("call %d (id=%q) arguments = %q, want %q", i, out.calls[i].ID, out.calls[i].Arguments, want)
		}
		// The point of the fix: each blob must be valid JSON on its own.
		var v map[string]any
		if err := json.Unmarshal([]byte(out.calls[i].Arguments), &v); err != nil {
			t.Fatalf("call %d arguments are not valid JSON (%q): %v", i, out.calls[i].Arguments, err)
		}
	}
}

// TestRoundAccumulator_ProviderIndexIsNotTheCallOrder pins that the provider's
// slot number is not treated as "the Nth call": numbering that does not start
// at 0 (or that skips) must still route fragments to the right call.
func TestRoundAccumulator_ProviderIndexIsNotTheCallOrder(t *testing.T) {
	a := newRoundAccumulator(true)
	r := &runner{ch: make(chan Event, 64)}
	for _, ev := range []llm.Event{
		callStart(7, "c7", "grep"),
		callDelta(7, `{"pattern":"x"}`),
	} {
		if err := a.apply(ev, r); err != nil {
			t.Fatalf("apply: %v", err)
		}
	}
	out := a.result(1)
	if len(out.calls) != 1 || out.calls[0].Arguments != `{"pattern":"x"}` {
		t.Fatalf("a non-zero provider index must still address its call, got %+v", out.calls)
	}
}

// TestRoundAccumulator_SingletonUnchanged pins the single-call path, which is
// the common case and must not depend on the index being present at all.
func TestRoundAccumulator_SingletonUnchanged(t *testing.T) {
	a := newRoundAccumulator(true)
	r := &runner{ch: make(chan Event, 64)}
	for _, ev := range []llm.Event{
		{Kind: llm.KindToolCallStart, ToolCall: &llm.ToolCall{ID: "c1", Name: "read"}},
		{Kind: llm.KindToolCallDelta, ToolCall: &llm.ToolCall{Arguments: `{"path":`}},
		{Kind: llm.KindToolCallDelta, ToolCall: &llm.ToolCall{Arguments: `"a.txt"}`}},
	} {
		if err := a.apply(ev, r); err != nil {
			t.Fatalf("apply: %v", err)
		}
	}
	out := a.result(1)
	if len(out.calls) != 1 || out.calls[0].Arguments != `{"path":"a.txt"}` {
		t.Fatalf("an indexed-less single call must still assemble, got %+v", out.calls)
	}
}

// TestRoundAccumulator_UnknownIndexIsIgnored pins that a fragment for a call
// whose start was never seen cannot attach itself to an unrelated call.
func TestRoundAccumulator_UnknownIndexIsIgnored(t *testing.T) {
	a := newRoundAccumulator(true)
	r := &runner{ch: make(chan Event, 64)}
	for _, ev := range []llm.Event{
		callStart(0, "c0", "read"),
		callDelta(0, `{"path":"a.txt"}`),
		callDelta(3, `{"stray":true}`),
	} {
		if err := a.apply(ev, r); err != nil {
			t.Fatalf("apply: %v", err)
		}
	}
	out := a.result(1)
	if len(out.calls) != 1 {
		t.Fatalf("an unknown index must not create a call, got %+v", out.calls)
	}
	if out.calls[0].Arguments != `{"path":"a.txt"}` {
		t.Fatalf("a stray fragment must not be appended to another call, got %q", out.calls[0].Arguments)
	}
}
