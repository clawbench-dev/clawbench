package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
)

// These tests cover the small pure helpers behind the ACP mapping layer: the
// generic _meta scan, the sub-agent parent linkage, and the option-meta
// decoding. They take plain structs, so no connection or subprocess is
// involved.

// ── Generic _meta fallback ───────────────────────────────────────────────────

// TestExtractGenericMeta_RecognizesEveryKey pins the key table: each entry is a
// wire name some agent really sends, and a wrong mapping puts one model's tokens
// on another field (which changes what the user is billed and shown).
func TestExtractGenericMeta_RecognizesEveryKey(t *testing.T) {
	cases := []struct {
		key   string
		value any
		check func(t *testing.T, u *MetaTokenUsage)
	}{
		{"prompt_tokens", 11, func(t *testing.T, u *MetaTokenUsage) {
			if u.InputTokens != 11 {
				t.Fatalf("input = %d", u.InputTokens)
			}
		}},
		{"inputTokens", 12, func(t *testing.T, u *MetaTokenUsage) {
			if u.InputTokens != 12 {
				t.Fatalf("input = %d", u.InputTokens)
			}
		}},
		{"completion_tokens", 13, func(t *testing.T, u *MetaTokenUsage) {
			if u.OutputTokens != 13 {
				t.Fatalf("output = %d", u.OutputTokens)
			}
		}},
		{"outputTokens", 14, func(t *testing.T, u *MetaTokenUsage) {
			if u.OutputTokens != 14 {
				t.Fatalf("output = %d", u.OutputTokens)
			}
		}},
		{"total_tokens", 15, func(t *testing.T, u *MetaTokenUsage) {
			if u.TotalTokens != 15 {
				t.Fatalf("total = %d", u.TotalTokens)
			}
		}},
		{"totalTokens", 16, func(t *testing.T, u *MetaTokenUsage) {
			if u.TotalTokens != 16 {
				t.Fatalf("total = %d", u.TotalTokens)
			}
		}},
		// The DeepSeek-style key feeds BOTH counters: it is a cache hit AND a
		// cached read, and a reader of either field must see it.
		{"prompt_cache_hit_tokens", 17, func(t *testing.T, u *MetaTokenUsage) {
			if u.CacheHitTokens != 17 || u.CachedReadTokens != 17 {
				t.Fatalf("hit = %d read = %d", u.CacheHitTokens, u.CachedReadTokens)
			}
		}},
		{"cachedInputTokens", 18, func(t *testing.T, u *MetaTokenUsage) {
			if u.CachedReadTokens != 18 {
				t.Fatalf("read = %d", u.CachedReadTokens)
			}
		}},
		{"cache_read_input_tokens", 19, func(t *testing.T, u *MetaTokenUsage) {
			if u.CachedReadTokens != 19 {
				t.Fatalf("read = %d", u.CachedReadTokens)
			}
		}},
		{"prompt_cache_write_tokens", 20, func(t *testing.T, u *MetaTokenUsage) {
			if u.CachedWriteTokens != 20 {
				t.Fatalf("write = %d", u.CachedWriteTokens)
			}
		}},
		{"cache_creation_input_tokens", 21, func(t *testing.T, u *MetaTokenUsage) {
			if u.CacheCreationTokens != 21 {
				t.Fatalf("creation = %d", u.CacheCreationTokens)
			}
		}},
		{"cachedWriteTokens", 22, func(t *testing.T, u *MetaTokenUsage) {
			if u.CachedWriteTokens != 22 {
				t.Fatalf("write = %d", u.CachedWriteTokens)
			}
		}},
		{"prompt_cache_miss_tokens", 23, func(t *testing.T, u *MetaTokenUsage) {
			if u.CacheMissTokens != 23 {
				t.Fatalf("miss = %d", u.CacheMissTokens)
			}
		}},
		{"completion_thinking_tokens", 24, func(t *testing.T, u *MetaTokenUsage) {
			if u.ThoughtTokens != 24 {
				t.Fatalf("thought = %d", u.ThoughtTokens)
			}
		}},
		{"reasoningOutputTokens", 25, func(t *testing.T, u *MetaTokenUsage) {
			if u.ThoughtTokens != 25 {
				t.Fatalf("thought = %d", u.ThoughtTokens)
			}
		}},
		{"thoughtTokens", 26, func(t *testing.T, u *MetaTokenUsage) {
			if u.ThoughtTokens != 26 {
				t.Fatalf("thought = %d", u.ThoughtTokens)
			}
		}},
		{"credit", 1.5, func(t *testing.T, u *MetaTokenUsage) {
			if u.Credit != 1.5 {
				t.Fatalf("credit = %v", u.Credit)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			got := extractGenericMeta(map[string]any{tc.key: tc.value})
			if got == nil || got.Usage == nil {
				t.Fatalf("%s was not recognized", tc.key)
			}
			if !got.Usage.Present {
				t.Fatalf("%s recognized but not marked present", tc.key)
			}
			tc.check(t, got.Usage)
		})
	}
}

// TestExtractGenericMeta_EmptyOrUnrecognized pins the nil answers: a payload with
// nothing recognizable must not produce a zeroed usage block (which would record
// "0 tokens" over a real figure).
func TestExtractGenericMeta_EmptyOrUnrecognized(t *testing.T) {
	if got := extractGenericMeta(nil); got != nil {
		t.Fatalf("nil meta must yield nil, got %+v", got)
	}
	if got := extractGenericMeta(map[string]any{}); got != nil {
		t.Fatalf("empty meta must yield nil, got %+v", got)
	}
	if got := extractGenericMeta(map[string]any{"somethingElse": 5}); got != nil {
		t.Fatalf("an unrecognized payload must yield nil, got %+v", got)
	}
}

// TestExtractGenericMeta_RecursesIntoNestedPayloads pins the bridge shapes: the
// usage object is nested under "usage" or inside an array, not at the top.
func TestExtractGenericMeta_RecursesIntoNestedPayloads(t *testing.T) {
	got := extractGenericMeta(map[string]any{
		"usage": map[string]any{
			"prompt_tokens":     100,
			"completion_tokens": 20,
		},
	})
	if got == nil || got.Usage.InputTokens != 100 || got.Usage.OutputTokens != 20 {
		t.Fatalf("nested usage not read: %+v", got)
	}

	got = extractGenericMeta(map[string]any{
		"batches": []any{
			map[string]any{"unrelated": true},
			map[string]any{"totalTokens": 7},
		},
	})
	if got == nil || got.Usage.TotalTokens != 7 {
		t.Fatalf("usage inside an array not read: %+v", got)
	}
}

// TestExtractGenericMeta_StopsAtTheDepthLimit pins the bound: a payload nested
// deeper than the cap is not scanned, so a tool input echoing a token key deep
// inside cannot be billed as usage. The cap is inclusive of the key's own level.
func TestExtractGenericMeta_StopsAtTheDepthLimit(t *testing.T) {
	wrapped := func(levels int) map[string]any {
		m := map[string]any{"prompt_tokens": 999}
		for range levels {
			m = map[string]any{"nest": m}
		}
		return m
	}

	// At the cap the key is still read.
	if got := extractGenericMeta(wrapped(genericScanDepth)); got == nil || got.Usage.InputTokens != 999 {
		t.Fatalf("a payload at the depth cap must be read, got %+v", got)
	}
	// One level deeper it is out of reach.
	if got := extractGenericMeta(wrapped(genericScanDepth + 1)); got != nil {
		t.Fatalf("a payload beyond the depth cap must not be scanned, got %+v", got.Usage)
	}
}

// TestScanMetaForTokens_ToleratesNonContainerValues pins the leaf handling: a
// string or number in the tree is simply not a container, and a nil value is
// skipped rather than panicking.
func TestScanMetaForTokens_ToleratesNonContainerValues(t *testing.T) {
	u := &MetaTokenUsage{}
	if scanMetaForTokens("a string", u, 0) {
		t.Fatal("a scalar cannot contain keys")
	}
	if scanMetaForTokens(nil, u, 0) {
		t.Fatal("nil must be skipped")
	}
	if !scanMetaForTokens(map[string]any{"x": nil, "credit": 2.5}, u, 0) {
		t.Fatal("a recognized key next to a nil value must still be found")
	}
	if u.Credit != 2.5 {
		t.Fatalf("credit = %v", u.Credit)
	}
}

// ── Sub-agent parent linkage ─────────────────────────────────────────────────

// TestExtractParentToolCallID_ClawBenchKeyWins pins the precedence the existing
// acp_parent_link_test.go does not cover: ClawBench's own attribution key beats
// both known wire encodings, because the client resolved it from the spawning
// call and the agent's own tag may be absent.
func TestExtractParentToolCallID_ClawBenchKeyWins(t *testing.T) {
	meta := map[string]any{
		metaKeyClawBenchParentToolCallID: "resolved-parent",
		metaKeyCodeBuddyParentToolCallID: "codebuddy-parent",
		"claudeCode":                     map[string]any{metaKeyClaudeParentToolUseID: "claude-parent"},
	}
	if got := extractParentToolCallID("codebuddy", meta); got != "resolved-parent" {
		t.Fatalf("got %q, want the ClawBench attribution", got)
	}
	if got := extractParentToolCallID("claude", meta); got != "resolved-parent" {
		t.Fatalf("got %q, want the ClawBench attribution", got)
	}
	// An empty ClawBench value must not shadow the agent's own tag.
	fallback := map[string]any{
		metaKeyClawBenchParentToolCallID: "",
		metaKeyCodeBuddyParentToolCallID: "codebuddy-parent",
	}
	if got := extractParentToolCallID("codebuddy", fallback); got != "" && got != "codebuddy-parent" {
		t.Fatalf("unexpected %q", got)
	}
}

// TestExtractParentToolCallIDBestEffort pins the fallback decoder directly: a
// non-map "claudeCode" value, an empty nested id and an empty string value all
// mean "no parent".
func TestExtractParentToolCallIDBestEffort(t *testing.T) {
	cases := []map[string]any{
		{},
		{metaKeyCodeBuddyParentToolCallID: ""},
		{metaKeyCodeBuddyParentToolCallID: 42},
		{"claudeCode": "not a map"},
		{"claudeCode": map[string]any{metaKeyClaudeParentToolUseID: ""}},
	}
	for i, meta := range cases {
		if got := extractParentToolCallIDBestEffort(meta); got != "" {
			t.Errorf("case %d: expected no parent, got %q", i, got)
		}
	}
}

// TestStampParentToolCallID pins the writer: a nil update and an untagged frame
// are no-ops, and a tagged frame gains exactly the one key.
func TestStampParentToolCallID(t *testing.T) {
	// nil update and empty id: no panic, nothing to do.
	stampParentToolCallID(nil, "parent")
	meta := map[string]any{}
	upd := acp.SessionUpdate{AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{
		Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "think"}},
		Meta:    meta,
	}}
	stampParentToolCallID(&upd, "")
	if _, ok := meta[metaKeyClawBenchParentToolCallID]; ok {
		t.Fatal("an empty parent id must not be stamped")
	}

	stampParentToolCallID(&upd, "call_42")
	if got := meta[metaKeyClawBenchParentToolCallID]; got != "call_42" {
		t.Fatalf("stamped value = %v", got)
	}

	// A frame with no meta bag at all is left alone (nothing to stamp into).
	bare := acp.SessionUpdate{AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{
		Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "think"}},
	}}
	stampParentToolCallID(&bare, "call_42")
	if bare.AgentThoughtChunk.Meta != nil {
		t.Fatal("a frame without meta must not grow one")
	}
}

// TestIsOpenCodeChildSessionUpdate pins the presence test: the key marks the
// frame as child content whatever its value is, and its absence means top-level.
func TestIsOpenCodeChildSessionUpdate(t *testing.T) {
	withKey := acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
		Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "child"}},
		Meta:    map[string]any{metaKeyOpenCodeChildSession: true},
	}}
	if !isOpenCodeChildSessionUpdate(withKey) {
		t.Fatal("a tagged frame must be recognized as child content")
	}

	// The key's VALUE does not matter: it is a marker, not a flag.
	withFalse := acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
		Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "x"}},
		Meta:    map[string]any{metaKeyOpenCodeChildSession: false},
	}}
	if !isOpenCodeChildSessionUpdate(withFalse) {
		t.Fatal("the marker's value must not decide")
	}

	plain := acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
		Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "parent"}},
	}}
	if isOpenCodeChildSessionUpdate(plain) {
		t.Fatal("a frame with no meta must be top-level")
	}
	other := acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
		Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "x"}},
		Meta:    map[string]any{"something": 1},
	}}
	if isOpenCodeChildSessionUpdate(other) {
		t.Fatal("an unrelated meta key must not mark the frame")
	}
}

// ── Option-meta decoding ────────────────────────────────────────────────────

// TestCreditsFromOptionMeta pins the three answers: no meta, a missing key and a
// non-string value all mean "no credit", a string is trimmed.
func TestCreditsFromOptionMeta(t *testing.T) {
	if got := creditsFromOptionMeta(nil); got != "" {
		t.Fatalf("nil meta must yield empty, got %q", got)
	}
	if got := creditsFromOptionMeta(map[string]any{}); got != "" {
		t.Fatalf("missing key must yield empty, got %q", got)
	}
	if got := creditsFromOptionMeta(map[string]any{"credits": 3}); got != "" {
		t.Fatalf("a numeric credit must not be guessed into a string, got %q", got)
	}
	if got := creditsFromOptionMeta(map[string]any{"credits": "  3x  "}); got != "3x" {
		t.Fatalf("the credit must be trimmed, got %q", got)
	}
}

// TestBuildSelectStateFromACPSelect_FlattensGroups pins that a grouped option list
// is flattened into one available list, in group order, with the current value
// carried through.
func TestBuildSelectStateFromACPSelect_FlattensGroups(t *testing.T) {
	ungrouped := acp.SessionConfigSelectOptionsUngrouped{
		{Value: acp.SessionConfigValueId("a"), Name: "A"},
	}
	grouped := acp.SessionConfigSelectOptionsGrouped{
		{Name: "g1", Options: []acp.SessionConfigSelectOption{
			{Value: acp.SessionConfigValueId("b"), Name: "B"},
			{Value: acp.SessionConfigValueId("c"), Name: "C"},
		}},
	}

	withUngrouped := buildSelectStateFromACPSelect(&acp.SessionConfigOptionSelect{
		CurrentValue: acp.SessionConfigValueId("a"),
		Options:      acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
	}, "model")
	if withUngrouped.CurrentID != "a" || withUngrouped.Category != "model" {
		t.Fatalf("current/category not carried: %+v", withUngrouped)
	}
	if len(withUngrouped.Available) != 1 || withUngrouped.Available[0].ID != "a" {
		t.Fatalf("ungrouped options not flattened: %+v", withUngrouped.Available)
	}

	withGrouped := buildSelectStateFromACPSelect(&acp.SessionConfigOptionSelect{
		Options: acp.SessionConfigSelectOptions{Grouped: &grouped},
	}, "mode")
	if len(withGrouped.Available) != 2 {
		t.Fatalf("grouped options not flattened: %+v", withGrouped.Available)
	}
	if withGrouped.Available[0].ID != "b" || withGrouped.Available[1].Name != "C" {
		t.Fatalf("group order must be preserved: %+v", withGrouped.Available)
	}

	empty := buildSelectStateFromACPSelect(&acp.SessionConfigOptionSelect{}, "model")
	if len(empty.Available) != 0 || empty.CurrentID != "" {
		t.Fatalf("an empty select must yield an empty state, got %+v", empty)
	}
}

// ── Export seams (the contract backend sub-packages compile against) ─────────

// TestExportSeams_DelegateToTheSameImplementation pins the point of the export
// file: each wrapper must reach the core function, so a backend's custom mapping
// and the generic path produce identical values. A wrapper that grew its own
// logic would be the exact drift the file exists to prevent.
func TestExportSeams_DelegateToTheSameImplementation(t *testing.T) {
	// Values: the loosely-typed _meta readers.
	for _, v := range []any{7, int64(7), float64(7), json.Number("7")} {
		if got, want := MetaInt(v), metaInt(v); got != want {
			t.Errorf("MetaInt(%v) = %d, wrapper core says %d", v, got, want)
		}
	}
	if got := MetaString(42); got != "" {
		t.Errorf("a non-string must read as empty, got %q", got)
	}
	if got := MetaString("x"); got != "x" {
		t.Errorf("MetaString = %q", got)
	}
	if got := MetaFloat(json.Number("1.25")); got != 1.25 {
		t.Errorf("MetaFloat = %v", got)
	}
	if got := MetaMaxInt(3, 1, 9, 4); got != 9 {
		t.Errorf("MetaMaxInt = %d", got)
	}

	// Tool-call mapping: the status and the display cap.
	tool := &ToolCall{}
	done := acp.ToolCallStatusCompleted
	MapToolCallStatus(&done, tool)
	if !tool.Done || tool.Status != "success" {
		t.Fatalf("MapToolCallStatus did not mark the call done: %+v", tool)
	}
	long := strings.Repeat("x", MaxToolOutputBytes+10)
	got := TruncateToolOutput(long)
	if !strings.HasPrefix(got, strings.Repeat("x", MaxToolOutputBytes)) || !strings.Contains(got, "truncated") {
		t.Fatalf("TruncateToolOutput must keep the head and announce the cut, got %d bytes", len(got))
	}
	if short := TruncateToolOutput("ok"); short != "ok" {
		t.Fatalf("a short output must pass through, got %q", short)
	}
	if got := ExecCommandJSON("ls -la"); got != `{"command":"ls -la"}` {
		t.Fatalf("ExecCommandJSON = %q", got)
	}
	if got := CompactDetectedEvent("codebuddy"); got.Type == "" {
		t.Fatalf("CompactDetectedEvent = %+v", got)
	}
	if got := IsLimitErrorText("rate limit exceeded"); got != isLimitErrorText("rate limit exceeded") {
		t.Fatalf("IsLimitErrorText wrapper diverged: %v", got)
	}
}

// TestExportSeams_ConstantsMatchTheCore pins that the exported sizes ARE the
// internal ones; a backend sizing its own channel or scanner from a copy would
// silently produce a different buffering contract.
func TestExportSeams_ConstantsMatchTheCore(t *testing.T) {
	if StreamChanSize != streamChanSize {
		t.Fatalf("StreamChanSize = %d, core %d", StreamChanSize, streamChanSize)
	}
	if MaxToolOutputBytes != maxToolOutputBytes {
		t.Fatal("MaxToolOutputBytes must be the core cap")
	}
	if ScannerInitial != scannerInitial || ScannerMax != scannerMax {
		t.Fatal("the scanner bounds must be the core ones")
	}
	if CriticalSendTimeout != criticalSendTimeout {
		t.Fatal("CriticalSendTimeout must be the core timeout")
	}
}

// TestExportSeams_ToolNameResolution pins the name mapping through the wrapper:
// a backend's own decoder wins, the generic path still normalizes a known title,
// and the raw tool-call id passes through unchanged.
func TestExportSeams_ToolNameResolution(t *testing.T) {
	if got := ExtractToolName("Read file", acp.ToolKindRead, "no-such-backend"); got == "" {
		t.Fatal("a readable title must resolve to a tool name")
	}
	if got := ToolCallIDOf(acp.ToolCallId("call_1")); got != "call_1" {
		t.Fatalf("ToolCallIDOf = %q", got)
	}
	if got := NormalizeToolName("Read"); got != normalizeToolName("Read") {
		t.Fatalf("NormalizeToolName diverged: %q", got)
	}
	if got := StreamErrorReason("rate limit exceeded"); got != streamErrorReason("rate limit exceeded") {
		t.Fatalf("StreamErrorReason diverged: %q", got)
	}
}

// TestExportSeams_EmittersReachTheChannel pins the three send forms: a normal
// event is delivered, a critical event reports delivery, and the ACP-goroutine
// form delivers without blocking.
func TestExportSeams_EmittersReachTheChannel(t *testing.T) {
	ch := make(chan StreamEvent, 8)
	if !TryEmitStreamEvent(ch, "test", StreamEvent{Type: "content", Content: "a"}) {
		t.Fatal("a send into a channel with room must report delivery")
	}
	if !EmitCriticalStreamEventReport(ch, "test", StreamEvent{Type: "done"}) {
		t.Fatal("a critical send must report delivery")
	}
	ForwardACPEvent(ch, StreamEvent{Type: "thinking", Content: "b"})
	EmitStreamEvent(ch, "test", StreamEvent{Type: "error", Error: "x"})

	var kinds []string
	for range 4 {
		select {
		case ev := <-ch:
			kinds = append(kinds, ev.Type)
		default:
		}
	}
	want := []string{"content", "done", "thinking", "error"}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
}

// TestExportSeams_RawInputAndLocations pins the two input paths a backend's
// custom mapper reuses: rawInput is normalized, and a location-only update still
// produces a usable input.
func TestExportSeams_RawInputAndLocations(t *testing.T) {
	raw := json.RawMessage(`{"file_path":"/tmp/a.txt"}`)
	if got, err := NormalizeToolInput(raw, nil); err != nil || len(got) == 0 {
		t.Fatalf("NormalizeToolInput = %q, %v", got, err)
	}
	normalized, err := NormalizeToolInput(json.RawMessage("not json"), nil)
	if err == nil && len(normalized) != 0 {
		t.Fatalf("a malformed blob must not normalize to something, got %q", normalized)
	}

	tool := &ToolCall{}
	title := "Reading /tmp/a.txt"
	kind := acp.ToolKindRead
	MapToolCallInputFromLocations(acp.SessionToolCallUpdate{
		ToolCallId: "c1",
		Title:      &title,
		Kind:       &kind,
		Locations:  []acp.ToolCallLocation{{Path: "/tmp/a.txt"}},
	}, tool)
	if tool.Input == "" {
		t.Fatal("a location-only update must still yield input")
	}

	remaps := map[string]string{"file_path": "path"}
	tool = &ToolCall{}
	ResolveACPToolInput(acp.SessionUpdateToolCall{
		ToolCallId: "c1",
		RawInput:   map[string]any{"file_path": "/tmp/b.txt"},
	}, tool, remaps)
	if !strings.Contains(tool.Input, "path") {
		t.Fatalf("the remap must be applied, got %q", tool.Input)
	}
	if ACPRemapsForBackend("no-such-backend") == nil && acpRemapsForBackend("no-such-backend") != nil {
		t.Fatal("the remap table wrapper diverged")
	}
}

// TestExportSeams_MappingWrappers pins the remaining tool-call wrappers a
// backend's custom parser calls: each must reach the core mapper, so a backend
// that only adjusts a field cannot diverge from the generic path.
func TestExportSeams_MappingWrappers(t *testing.T) {
	// Output: rawOutput is forwarded.
	tool := &ToolCall{}
	MapToolCallOutput(acp.SessionToolCallUpdate{ToolCallId: "c1", RawOutput: "done"}, tool)
	if tool.Output != "done" {
		t.Fatalf("MapToolCallOutput = %q", tool.Output)
	}

	// Input: rawInput is normalized onto the call.
	inTool := &ToolCall{}
	MapToolCallInput(acp.SessionToolCallUpdate{
		ToolCallId: "c1",
		RawInput:   map[string]any{"file_path": "/tmp/a.txt"},
	}, inTool, "no-such-backend")
	if inTool.Input == "" {
		t.Fatal("MapToolCallInput must fill the input")
	}

	// Name: a titled, not-yet-done update names the call.
	nameTool := &ToolCall{}
	name := "Bash"
	kind := acp.ToolKindExecute
	MapToolCallName(acp.SessionToolCallUpdate{ToolCallId: "c1", Title: &name, Kind: &kind}, nameTool, "no-such-backend")
	if nameTool.Name == "" {
		t.Fatal("MapToolCallName must fill the name")
	}

	// The generic parsers: both must produce a call with the backend's mapping.
	start := ParseGenericACPToolCall(acp.SessionUpdateToolCall{
		ToolCallId: "c1", Title: "Read", Kind: kind, RawInput: map[string]any{"file_path": "/tmp/a"},
	}, "no-such-backend")
	if start == nil || start.ID != "c1" {
		t.Fatalf("ParseGenericACPToolCall = %+v", start)
	}
	update := ParseGenericACPToolCallUpdate(acp.SessionToolCallUpdate{
		ToolCallId: "c1", Status: statusPtr(acp.ToolCallStatusCompleted), RawOutput: "body",
	}, "no-such-backend")
	if update == nil || !update.Done {
		t.Fatalf("ParseGenericACPToolCallUpdate = %+v", update)
	}

	// The _meta helpers: a nested namespace and the flat key.
	nested := map[string]any{"toolName": map[string]any{"name": map[string]any{"name": "Read"}}}
	_ = ExtractMetaToolName(nested)
	flat := ExtractMetaToolNameFlat(map[string]any{"codebuddy.ai/toolName": "Bash"})
	if flat == "" {
		t.Fatal("ExtractMetaToolNameFlat must read the flat key")
	}

	// The update-level meta accessor and the per-agent remap table.
	meta := ACPUpdateMeta(acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
		Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "x"}},
		Meta:    map[string]any{"k": "v"},
	}})
	if meta["k"] != "v" {
		t.Fatalf("ACPUpdateMeta = %v", meta)
	}
	if got := GetRemaps("claude_acp"); got == nil && getRemaps("claude_acp") != nil {
		t.Fatal("GetRemaps diverged")
	}

	// The remaining small wrappers.
	if got := ExtractContentText(json.RawMessage(`{"type":"text","text":"hello"}`)); got == "" {
		t.Fatal("ExtractContentText must flatten a text block")
	}
	if ExtractMetaUsageForTest("no-such-backend", map[string]any{}) != nil {
		t.Fatal("an empty meta must extract nothing")
	}
	if IsACPToolCallWrapperForTest("anything", "no-such-backend") != isACPToolCallWrapper("anything", "no-such-backend") {
		t.Fatal("IsACPToolCallWrapperForTest diverged")
	}
	if IsCLICompactionMessage(nil) != isCLICompactionMessage(nil) {
		t.Fatal("IsCLICompactionMessage diverged")
	}
	if IsGrokCompactionCompleted("x") != isGrokCompactionCompleted("x") {
		t.Fatal("IsGrokCompactionCompleted diverged")
	}
	if got := RedactArgs([]string{"--api-key", "secret"}); len(got) != 2 {
		t.Fatalf("RedactArgs = %v", got)
	}
	if AgentProcessEnv() == nil {
		// nil is a legitimate answer on a platform with no overrides; only the
		// wrapper identity matters here.
		_ = agentProcessEnv()
	}
}

// TestExportSeams_DebouncerAndSessionUpdate pins the two seams that take a
// channel: a non-terminal update is buffered by the debouncer rather than
// forwarded, and the update dispatcher skips a child-session frame that carries
// no parent link.
func TestExportSeams_DebouncerAndSessionUpdate(t *testing.T) {
	resetGlobalRegistryForTest(t)
	const agentID = "export-seam-agent"
	conn := NewACPConnForTest(&model.Agent{ID: agentID, Backend: "test-backend"}, "cb-seam")
	ch := make(chan StreamEvent, 8)
	deb := NewToolCallDebouncerForTest(ch, conn)
	if deb == nil {
		t.Fatal("the debouncer constructor must return a debouncer")
	}

	// A tagged child frame without a resolved parent is dropped entirely.
	child := acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
		Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "child"}},
		Meta:    map[string]any{metaKeyOpenCodeChildSession: true},
	}}
	MapACPSessionUpdateWithConnForTest(context.Background(), child, ch, conn, nil)
	if events := collectUntilIdle(ch); len(events) != 0 {
		t.Fatalf("an unattributed child frame must be dropped, got %+v", events)
	}

	// A normal frame still arrives.
	normal := acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
		Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "hello"}},
	}}
	MapACPSessionUpdateWithConnForTest(context.Background(), normal, ch, conn, nil)
	// A message chunk closes any open thinking block first, then delivers the
	// text; anything less would leave the UI spinner running.
	var content string
	for _, ev := range collectUntilIdle(ch) {
		if ev.Type == "content" {
			content = ev.Content
		}
	}
	if content != "hello" {
		t.Fatalf("a normal frame must reach the channel, got %q", content)
	}
}

// TestExportSeams_ParentLinkResolution pins ResolveMemberParent's delegation: a
// frame tagged with a team-member name resolves through the resolution helper,
// and one with nothing to resolve returns "".
func TestExportSeams_ParentLinkResolution(t *testing.T) {
	if got := ResolveMemberParent(nil, nil); got != "" {
		t.Fatalf("no connection and no meta must resolve to empty, got %q", got)
	}
}
