package ai

import (
	"context"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

func TestIsCompactCommand(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"bare command", "/compact", true},
		{"with surrounding whitespace", "  /compact  ", true},
		{"with arguments", "/compact keep the API notes", true},
		{"with newline argument", "/compact\nkeep it short", true},
		{"not a slash command", "compact", false},
		{"different command sharing a prefix", "/compaction", false},
		{"a command merely containing compact", "/reload-compact", false},
		{"mentioned mid-sentence", "please run /compact now", false},
		{"empty", "", false},
		{"slash only", "/", false},
		{"other command", "/reload-plugins", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsCompactCommand(tt.text))
		})
	}
}

func TestIsCodeBuddyCompactionMeta(t *testing.T) {
	tests := []struct {
		name string
		meta map[string]any
		want bool
	}{
		{"nil meta", nil, false},
		{"empty meta", map[string]any{}, false},
		{
			"boolean flag",
			map[string]any{"codebuddy.ai/isCompactInternal": true},
			true,
		},
		{
			"flag serialized as string",
			map[string]any{"codebuddy.ai/isCompactInternal": "true"},
			true,
		},
		{
			"flag serialized as number",
			map[string]any{"codebuddy.ai/isCompactInternal": float64(1)},
			true,
		},
		{
			"explicit false flag",
			map[string]any{"codebuddy.ai/isCompactInternal": false},
			false,
		},
		{
			"compactType alone (manual)",
			map[string]any{"codebuddy.ai/compactType": "user-command"},
			true,
		},
		{
			"compactType alone (auto)",
			map[string]any{"codebuddy.ai/compactType": "pre-message-auto"},
			true,
		},
		{
			"unrelated meta",
			map[string]any{"codebuddy.ai/requestId": "abc"},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsCodeBuddyCompactionMeta(tt.meta))
		})
	}
}

func TestIsCodeBuddyCompactionCancelledMeta(t *testing.T) {
	tests := []struct {
		name string
		meta map[string]any
		want bool
	}{
		{"nil meta", nil, false},
		{
			"cancelled",
			map[string]any{"codebuddy.ai/compact-cancelled": map[string]any{"cancelled": true}},
			true,
		},
		{
			"limit reached",
			map[string]any{"codebuddy.ai/compact-limit-reached": map[string]any{"limitReached": true}},
			true,
		},
		{
			"successful compaction is not a cancellation",
			map[string]any{"codebuddy.ai/isCompactInternal": true},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsCodeBuddyCompactionCancelledMeta(tt.meta))
		})
	}
}

func TestIsClaudeCompactionText(t *testing.T) {
	assert.True(t, IsClaudeCompactionText("Compacting completed."))
	assert.True(t, IsClaudeCompactionText("\n\nCompacting completed."))
	assert.False(t, IsClaudeCompactionText("Compacting..."))
	assert.False(t, IsClaudeCompactionText("compacting completed")) // case-sensitive
	assert.False(t, IsClaudeCompactionText(""))
}

func TestIsGrokCompactionCompleted(t *testing.T) {
	assert.True(t, isGrokCompactionCompleted("compact_completed"))
	assert.True(t, isGrokCompactionCompleted("auto_compact_completed"))
	// Started/failed/cancelled events do not mean the context was rewritten.
	assert.False(t, isGrokCompactionCompleted("auto_compact_started"))
	assert.False(t, isGrokCompactionCompleted("auto_compact_failed"))
	assert.False(t, isGrokCompactionCompleted("compact_cancelled"))
	assert.False(t, isGrokCompactionCompleted("text"))
}

func TestIsCLICompactionMessage(t *testing.T) {
	t.Run("claude compact_boundary", func(t *testing.T) {
		assert.True(t, isCLICompactionMessage(&ClaudeStreamMessage{Type: "system", Subtype: "compact_boundary"}))
	})
	t.Run("codebuddy compacting status", func(t *testing.T) {
		assert.True(t, isCLICompactionMessage(&ClaudeStreamMessage{Type: "system", Subtype: "status", Status: "compacting"}))
	})
	t.Run("codebuddy status null is not compaction", func(t *testing.T) {
		assert.False(t, isCLICompactionMessage(&ClaudeStreamMessage{Type: "system", Subtype: "status"}))
	})
	t.Run("codebuddy providerData flag", func(t *testing.T) {
		msg := &ClaudeStreamMessage{Type: "message"}
		msg.ProviderData = &struct {
			Model string `json:"model,omitempty"`
			Usage *struct {
				InputTokens  int `json:"inputTokens"`
				OutputTokens int `json:"outputTokens"`
			} `json:"usage,omitempty"`
			IsCompactInternal bool   `json:"isCompactInternal,omitempty"`
			CompactType       string `json:"compactType,omitempty"`
		}{IsCompactInternal: true}
		assert.True(t, isCLICompactionMessage(msg))
	})
	t.Run("codebuddy _meta flag", func(t *testing.T) {
		assert.True(t, isCLICompactionMessage(&ClaudeStreamMessage{
			Type: "message",
			Meta: map[string]any{"codebuddy.ai/isCompactInternal": true},
		}))
	})
	t.Run("plain assistant text is not compaction", func(t *testing.T) {
		assert.False(t, isCLICompactionMessage(&ClaudeStreamMessage{Type: "assistant", Subtype: "text", Text: "hello"}))
	})
	t.Run("nil message", func(t *testing.T) {
		assert.False(t, isCLICompactionMessage(nil))
	})
}

// TestStreamParser_ReportsCompactionOnce pins the latch: the CLI repeats the
// compaction flag on every item emitted while compacting, but the service layer
// must be told about the compaction a single time.
func TestStreamParser_ReportsCompactionOnce(t *testing.T) {
	p := &StreamParser{}
	ch := make(chan StreamEvent, 32)

	p.ParseLine(`{"type":"system","subtype":"compact_boundary"}`, ch)
	p.ParseLine(`{"type":"system","subtype":"compact_boundary"}`, ch)
	p.ParseLine(`{"type":"message","_meta":{"codebuddy.ai/isCompactInternal":true}}`, ch)

	detected := 0
	for {
		select {
		case ev := <-ch:
			if ev.Type == "compact_detected" {
				detected++
			}
		default:
			assert.Equal(t, 1, detected, "a compaction must be reported exactly once per parser")
			return
		}
	}
}

// TestStreamParser_NoCompactionWithoutSignal guards the negative direction: a
// normal turn must not flag the session as compacted.
func TestStreamParser_NoCompactionWithoutSignal(t *testing.T) {
	p := &StreamParser{}
	ch := make(chan StreamEvent, 32)
	p.ParseLine(`{"type":"assistant","subtype":"text","text":"hello"}`, ch)
	p.ParseLine(`{"type":"system","subtype":"init"}`, ch)
	p.ParseLine(`{"type":"system","subtype":"status"}`, ch)

	for {
		select {
		case ev := <-ch:
			assert.NotEqual(t, "compact_detected", ev.Type)
		default:
			return
		}
	}
}

func TestGrokStreamParser_ReportsCompaction(t *testing.T) {
	p := &GrokStreamParser{}
	ch := make(chan StreamEvent, 16)
	p.ParseLine(`{"type":"auto_compact_completed"}`, ch)
	p.ParseLine(`{"type":"auto_compact_completed"}`, ch)

	detected := 0
	for {
		select {
		case ev := <-ch:
			if ev.Type == "compact_detected" {
				detected++
			}
		default:
			assert.Equal(t, 1, detected, "grok compaction must be reported exactly once")
			return
		}
	}
}

func TestGrokStreamParser_IgnoresFailedCompaction(t *testing.T) {
	p := &GrokStreamParser{}
	ch := make(chan StreamEvent, 16)
	p.ParseLine(`{"type":"auto_compact_started"}`, ch)
	p.ParseLine(`{"type":"auto_compact_failed"}`, ch)

	for {
		select {
		case ev := <-ch:
			assert.NotEqual(t, "compact_detected", ev.Type)
		default:
			return
		}
	}
}

// ---------------------------------------------------------------------------
// Compaction reported as a TOOL CALL (claude-agent-acp / codex-acp)
// ---------------------------------------------------------------------------
//
// Both agents report a compaction as a pseudo-tool. Claude stamps the name in
// _meta.claudeCode.toolName; codex-acp sends only the title. Measured shapes
// (2026-10-18):
//
//	claude: title "Compact conversation", kind think, _meta toolName "compact"
//	codex : title "Compact conversation", kind think, no _meta
func TestIsCompactionToolCall(t *testing.T) {
	tests := []struct {
		name  string
		tool  string
		title string
		want  bool
	}{
		{"claude meta name", "compact", "Compact conversation", true},
		{"codex title only", "", "Compact conversation", true},
		{"title casing differs", "", "compact conversation", true},
		{"title with padding", "", "  Compact conversation  ", true},
		{"name wins over a non-matching title", "compact", "whatever", true},
		// False-positive guards: an agent that merely TALKS about compaction,
		// or a real tool whose name happens to contain the word.
		{"ordinary read tool", "Read", "Read file", false},
		{"file literally named compact", "", "Write compact.md", false},
		{"title mentioning compaction mid-sentence", "", "Explain context compaction", false},
		{"empty everything", "", "", false},
		{"real tool named compact_md", "compact_md", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsCompactionToolCall(tt.tool, tt.title))
		})
	}
}

// TestMapACPSessionUpdate_CompactionToolCall covers the production path: the
// opening tool_call frame must produce exactly one compact_detected, and the
// terminal tool_call_update must not produce a second one.
func TestMapACPSessionUpdate_CompactionToolCall(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "claude", Backend: "claude"}, "s1")
	ch := make(chan StreamEvent, 64)

	title := "Compact conversation"
	mapACPSessionUpdate(acp.SessionUpdate{
		ToolCall: &acp.SessionUpdateToolCall{
			ToolCallId: "tc-1",
			Title:      title,
			Meta:       map[string]any{"claudeCode": map[string]any{"toolName": "compact"}},
		},
	}, ch, context.Background(), conn, nil)

	assert.Equal(t, 1, drainCompactionEvents(t, ch),
		"the opening tool_call must report the compaction once")

	// A SECOND opening frame for the same compaction must be latched out.
	// Deliberately another ToolCall (not a ToolCallUpdate): ToolCallUpdate is a
	// retry point that re-checks the same signal, so asserting on it would pass
	// even with the latch removed. A second ToolCall is only suppressed BY the
	// latch.
	mapACPSessionUpdate(acp.SessionUpdate{
		ToolCall: &acp.SessionUpdateToolCall{
			ToolCallId: "tc-1-dup",
			Title:      title,
			Meta:       map[string]any{"claudeCode": map[string]any{"toolName": "compact"}},
		},
	}, ch, context.Background(), conn, nil)

	assert.Equal(t, 0, drainCompactionEvents(t, ch),
		"a second frame of the same compaction must not re-report it")
}

// TestMapACPSessionUpdate_OrdinaryToolCallIsNotCompaction guards the reverse:
// a normal tool call must not be mistaken for a compaction.
func TestMapACPSessionUpdate_OrdinaryToolCallIsNotCompaction(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "claude", Backend: "claude"}, "s1")
	ch := make(chan StreamEvent, 64)

	title := "Read file"
	mapACPSessionUpdate(acp.SessionUpdate{
		ToolCall: &acp.SessionUpdateToolCall{
			ToolCallId: "tc-2",
			Title:      title,
			Meta:       map[string]any{"claudeCode": map[string]any{"toolName": "Read"}},
		},
	}, ch, context.Background(), conn, nil)

	assert.Equal(t, 0, drainCompactionEvents(t, ch))
}

// ---------------------------------------------------------------------------
// Compaction detected from a usage drop (OpenCode and any silent agent)
// ---------------------------------------------------------------------------

func TestIsCompactionUsageDrop(t *testing.T) {
	tests := []struct {
		name string
		prev *UsageState
		next *UsageState
		want bool
	}{
		{
			name: "opencode observed drop",
			prev: &UsageState{Used: 20663, Size: 204800},
			next: &UsageState{Used: 1893, Size: 204800},
			want: true,
		},
		{
			name: "claude observed drop",
			prev: &UsageState{Used: 49744, Size: 200000},
			next: &UsageState{Used: 2258, Size: 200000},
			want: true,
		},
		{
			name: "a normal turn grows",
			prev: &UsageState{Used: 20000, Size: 200000},
			next: &UsageState{Used: 26000, Size: 200000},
			want: false,
		},
		{
			name: "a modest drop is not a compaction",
			prev: &UsageState{Used: 20000, Size: 200000},
			next: &UsageState{Used: 12000, Size: 200000},
			want: false, // 40% — below the 60% threshold
		},
		// A window change no longer VETOES detection. It used to, and that
		// silently disabled the only signal OpenCode ever produces: a compaction
		// can itself move the window, and a missed compaction is permanent (the
		// system prompt is never re-injected for the rest of the session). The
		// cost of a false positive is one extra prompt injection.
		//
		// The stricter regime is an ABSOLUTE floor on top of the ratio, so a
		// re-advertised window over a small context cannot fake it.
		{
			name: "window changed but the absolute drop is large -> compaction",
			prev: &UsageState{Used: 20000, Size: 200000},
			next: &UsageState{Used: 1000, Size: 1000000},
			want: true,
		},
		{
			name: "window changed with a SMALL absolute drop -> not evidence",
			prev: &UsageState{Used: 1100, Size: 200000},
			next: &UsageState{Used: 200, Size: 2000000},
			want: false, // 82% ratio but only 900 tokens gone, across a size change
		},
		{
			name: "tiny contexts are noise",
			prev: &UsageState{Used: 10, Size: 200000},
			next: &UsageState{Used: 1, Size: 200000},
			want: false,
		},
		{
			name: "no window size",
			prev: &UsageState{Used: 20000, Size: 0},
			next: &UsageState{Used: 1000, Size: 0},
			want: false,
		},
		{
			name: "nil previous",
			prev: nil,
			next: &UsageState{Used: 1000, Size: 200000},
			want: false,
		},
		{
			name: "equal values",
			prev: &UsageState{Used: 20000, Size: 200000},
			next: &UsageState{Used: 20000, Size: 200000},
			want: false,
		},
		{
			// A used=0 snapshot with a real window is a LEGAL "empty context"
			// notification, not a compaction. Without the explicit next.Used<=0
			// guard the ratio is 1.0 and every such frame would be reported.
			name: "an emptied context is not a compaction",
			prev: &UsageState{Used: 20000, Size: 200000},
			next: &UsageState{Used: 0, Size: 200000},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsCompactionUsageDrop(tt.prev, tt.next))
		})
	}
}

// TestMapACPSessionUpdate_UsageDropDetectsCompaction covers the OpenCode case
// end to end: two usage_updates with no other signal, the second a collapse.
func TestMapACPSessionUpdate_UsageDropDetectsCompaction(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "opencode", Backend: "opencode"}, "s1")
	ch := make(chan StreamEvent, 64)

	send := func(used, size int) {
		mapACPSessionUpdate(acp.SessionUpdate{
			UsageUpdate: &acp.SessionUsageUpdate{Used: used, Size: size},
		}, ch, context.Background(), conn, nil)
	}

	send(20663, 204800)
	assert.Equal(t, 0, drainCompactionEvents(t, ch), "the first snapshot has nothing to compare against")

	send(1893, 204800)
	assert.Equal(t, 1, drainCompactionEvents(t, ch), "the collapse must be reported as a compaction")

	// A LATER collapse must not re-report: the latch, not the "no change"
	// branch, is what suppresses it. Sending the identical value would pass
	// even with the latch removed (next.Used >= prev.Used short-circuits).
	send(700, 204800)
	assert.Equal(t, 0, drainCompactionEvents(t, ch), "one compaction is reported once")
}

// TestMapACPSessionUpdate_UsageGrowthIsNotCompaction guards the reverse.
func TestMapACPSessionUpdate_UsageGrowthIsNotCompaction(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "opencode", Backend: "opencode"}, "s1")
	ch := make(chan StreamEvent, 64)

	for _, used := range []int{20000, 26000, 31000} {
		mapACPSessionUpdate(acp.SessionUpdate{
			UsageUpdate: &acp.SessionUsageUpdate{Used: used, Size: 204800},
		}, ch, context.Background(), conn, nil)
	}
	assert.Equal(t, 0, drainCompactionEvents(t, ch))
}

// TestIsCompactionUsageDropFromWire covers the guard that the plain function
// cannot express: the incoming NOTIFICATION must carry its own window size.
//
// MergeUsageState keeps `size` sticky, so a naked update (used only) merged
// against a known snapshot yields merged.Size == prev.Size — the size guard
// would then pass while comparing a REAL snapshot against a PARTIAL one.
func TestIsCompactionUsageDropFromWire(t *testing.T) {
	prev := &UsageState{Used: 20000, Size: 200000}

	t.Run("real snapshot with matching window", func(t *testing.T) {
		assert.True(t, IsCompactionUsageDropFromWire(prev, &UsageState{Used: 3000, Size: 200000}))
	})
	t.Run("naked update without a window is not evidence", func(t *testing.T) {
		// Would be a 85% "drop" if merged, but it carries no window of its own.
		assert.False(t, IsCompactionUsageDropFromWire(prev, &UsageState{Used: 3000, Size: 0}))
	})
	t.Run("empty context still excluded", func(t *testing.T) {
		assert.False(t, IsCompactionUsageDropFromWire(prev, &UsageState{Used: 0, Size: 200000}))
	})
	t.Run("nil incoming", func(t *testing.T) {
		assert.False(t, IsCompactionUsageDropFromWire(prev, nil))
	})
}

// TestMapACPSessionUpdate_NakedUsageUpdateIsNotCompaction is the end-to-end
// version: a partial notification must not be able to fake a compaction even
// though the merge makes its window size "match".
func TestMapACPSessionUpdate_NakedUsageUpdateIsNotCompaction(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "opencode", Backend: "opencode"}, "s1")
	ch := make(chan StreamEvent, 64)

	send := func(used, size int) {
		mapACPSessionUpdate(acp.SessionUpdate{
			UsageUpdate: &acp.SessionUsageUpdate{Used: used, Size: size},
		}, ch, context.Background(), conn, nil)
	}

	send(20000, 204800)
	assert.Equal(t, 0, drainCompactionEvents(t, ch))

	// Naked: no size of its own.
	send(3000, 0)
	assert.Equal(t, 0, drainCompactionEvents(t, ch),
		"a partial notification must not be treated as a compaction")
}

// TestMapACPSessionUpdate_UsageDropRetriedWhenChannelFull verifies the retry
// contract: a detection whose event could not be delivered is reported on the
// NEXT usage_update instead of being lost.
//
// This matters because the drop comparison is against the previously cached
// snapshot, which has already been overwritten — the evidence is consumed by
// the detection itself, so there is no way to re-derive it later.
func TestMapACPSessionUpdate_UsageDropRetriedWhenChannelFull(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "opencode", Backend: "opencode"}, "s1")

	send := func(ch chan StreamEvent, used, size int) {
		mapACPSessionUpdate(acp.SessionUpdate{
			UsageUpdate: &acp.SessionUsageUpdate{Used: used, Size: size},
		}, ch, context.Background(), conn, nil)
	}

	// Fill the channel so the detection cannot be delivered.
	full := make(chan StreamEvent, 1)
	full <- StreamEvent{Type: "filler"}

	send(full, 20000, 204800) // baseline (also dropped, but it carries no signal)
	send(full, 1000, 204800)  // collapse — cannot be delivered
	assert.True(t, conn.compactDropPending.Load(),
		"an undeliverable detection must be remembered")

	// Drain the filler, then a later usage_update retries the pending report.
	drained := make(chan StreamEvent, 64)
	send(drained, 1000, 204800)
	assert.Equal(t, 1, drainCompactionEvents(t, drained),
		"the pending detection must be reported on the next usage_update")
	assert.False(t, conn.compactDropPending.Load())
}

// TestMapACPSessionUpdate_PendingDropDoesNotLeakIntoNextTurn is the regression
// test for a state-machine defect: a usage-drop detection that could not be
// delivered stayed pending across the turn boundary, so the NEXT turn's first
// usage_update reported a compaction that turn never had.
//
// The invariant: `compactDropPending` describes the CURRENT turn's compaction
// and must never outlive it.
func TestMapACPSessionUpdate_PendingDropDoesNotLeakIntoNextTurn(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "opencode", Backend: "opencode"}, "s1")

	send := func(ch chan StreamEvent, used, size int) {
		mapACPSessionUpdate(acp.SessionUpdate{
			UsageUpdate: &acp.SessionUsageUpdate{Used: used, Size: size},
		}, ch, context.Background(), conn, nil)
	}

	// A full channel makes the detection undeliverable.
	full := make(chan StreamEvent, 1)
	full <- StreamEvent{Type: "filler"}
	send(full, 20000, 204800)
	send(full, 1000, 204800)
	require.True(t, conn.compactDropPending.Load(), "precondition: a pending retry exists")

	// Turn boundary — exactly what acp_conn_prompt.go does before each Prompt.
	conn.ClearCompactionState()
	require.False(t, conn.compactDropPending.Load(),
		"a new turn must not inherit the previous turn's pending retry")

	// The new turn's first usage_update must not report anything.
	next := make(chan StreamEvent, 64)
	send(next, 1200, 204800)
	assert.Equal(t, 0, drainCompactionEvents(t, next),
		"the new turn must not report the previous turn's compaction")
}

// TestMapACPSessionUpdate_ToolReportClearsPendingDrop covers the cross-path
// case: a usage drop was detected but not delivered, then the SAME compaction
// is reported through the tool-call path. The pending retry must be dropped,
// otherwise the next usage_update reports the compaction a second time.
func TestMapACPSessionUpdate_ToolReportClearsPendingDrop(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "claude", Backend: "claude"}, "s1")

	sendUsage := func(ch chan StreamEvent, used, size int) {
		mapACPSessionUpdate(acp.SessionUpdate{
			UsageUpdate: &acp.SessionUsageUpdate{Used: used, Size: size},
		}, ch, context.Background(), conn, nil)
	}

	full := make(chan StreamEvent, 1)
	full <- StreamEvent{Type: "filler"}
	sendUsage(full, 20000, 200000)
	sendUsage(full, 1000, 200000)
	require.True(t, conn.compactDropPending.Load(), "precondition: a pending retry exists")

	// The tool-call path delivers successfully.
	deliver := make(chan StreamEvent, 64)
	mapACPSessionUpdate(acp.SessionUpdate{
		ToolCall: &acp.SessionUpdateToolCall{
			ToolCallId: "tc-1",
			Title:      "Compact conversation",
			Meta:       map[string]any{"claudeCode": map[string]any{"toolName": "compact"}},
		},
	}, deliver, context.Background(), conn, nil)
	assert.Equal(t, 1, drainCompactionEvents(t, deliver))
	assert.False(t, conn.compactDropPending.Load(),
		"a successful report on another path must drop the pending retry")

	// A later usage_update must not report the same compaction again.
	sendUsage(deliver, 1100, 200000)
	assert.Equal(t, 0, drainCompactionEvents(t, deliver),
		"the compaction was already reported; no duplicate")
}

// TestMapACPSessionUpdate_CancelledCompactionClearsPendingDrop covers the
// cancelled/limit-reached branch: the agent restored the original history, so
// a pending usage-drop detection must be discarded rather than reported.
func TestMapACPSessionUpdate_CancelledCompactionClearsPendingDrop(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")

	full := make(chan StreamEvent, 1)
	full <- StreamEvent{Type: "filler"}
	mapACPSessionUpdate(acp.SessionUpdate{
		UsageUpdate: &acp.SessionUsageUpdate{Used: 20000, Size: 200000},
	}, full, context.Background(), conn, nil)
	mapACPSessionUpdate(acp.SessionUpdate{
		UsageUpdate: &acp.SessionUsageUpdate{Used: 1000, Size: 200000},
	}, full, context.Background(), conn, nil)
	require.True(t, conn.compactDropPending.Load(), "precondition: a pending retry exists")

	mapACPSessionUpdate(acp.SessionUpdate{
		SessionInfoUpdate: &acp.SessionSessionInfoUpdate{
			Meta: map[string]any{"codebuddy.ai/compact-cancelled": true},
		},
	}, full, context.Background(), conn, nil)

	assert.False(t, conn.compactDropPending.Load(),
		"a cancelled compaction must discard the pending retry")

	ch := make(chan StreamEvent, 64)
	mapACPSessionUpdate(acp.SessionUpdate{
		UsageUpdate: &acp.SessionUsageUpdate{Used: 1000, Size: 200000},
	}, ch, context.Background(), conn, nil)
	assert.Equal(t, 0, drainCompactionEvents(t, ch),
		"nothing was compacted, so nothing may be reported")
}
