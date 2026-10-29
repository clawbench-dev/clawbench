package ai

import (
	"context"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// pinChunkClock replaces the dedup clock with a controllable one, returning an
// advance function. Tests must not sleep: the window is a semantic boundary.
func pinChunkClock(t *testing.T) func(time.Duration) {
	t.Helper()
	base := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	now := base
	prev := chunkDedupNow
	chunkDedupNow = func() time.Time { return now }
	t.Cleanup(func() { chunkDedupNow = prev })
	return func(d time.Duration) { now = now.Add(d) }
}

// codeBuddyChunk builds an agent_message_chunk with the _meta a real CodeBuddy
// frame carries (verified on the wire 2026-10-29). `ts` is the message-level
// publication timestamp; upstream's key uses it verbatim, so two frames of the
// same message differ here even when their text is identical.
func codeBuddyChunk(text, ts string) acp.SessionUpdate {
	mid := "01a185c9-eefe-7d43-b6e4-5aa20799273e"
	return acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Meta: map[string]any{
				metaKeyCodeBuddyMessageID:             mid,
				metaKeyCodeBuddyTimestamp:             ts,
				metaKeyCodeBuddyConversationRequestID: "01a185c9ecd1782f8643f986a69fbb0b",
				metaKeyCodeBuddyRequestID:             "01a185c9ecd1782f8643f986a69fbb0b",
			},
			MessageId: &mid,
			Content:   acp.TextBlock(text),
		},
	}
}

// ---------------------------------------------------------------------------
// chunkIdentity — the ported key
// ---------------------------------------------------------------------------

// Both halves are required, exactly like upstream's `if (ts && msgId)`.
func TestChunkIdentity_RequiresTimestampAndMessageID(t *testing.T) {
	base := map[string]any{
		metaKeyCodeBuddyMessageID: "m1",
		metaKeyCodeBuddyTimestamp: "t1",
	}

	assert.NotEmpty(t, chunkIdentity("sid", "agent_message_chunk", "", base, "hi"))

	t.Run("missing timestamp", func(t *testing.T) {
		assert.Empty(t, chunkIdentity("sid", "agent_message_chunk", "", map[string]any{
			metaKeyCodeBuddyMessageID: "m1",
		}, "hi"))
	})

	t.Run("missing messageId everywhere", func(t *testing.T) {
		assert.Empty(t, chunkIdentity("sid", "agent_message_chunk", "", map[string]any{
			metaKeyCodeBuddyTimestamp: "t1",
		}, "hi"))
	})

	t.Run("empty meta", func(t *testing.T) {
		assert.Empty(t, chunkIdentity("sid", "agent_message_chunk", "", nil, "hi"))
	})
}

// Upstream prefers the TOP-LEVEL messageId over the _meta copy.
func TestChunkIdentity_PrefersTopLevelMessageID(t *testing.T) {
	meta := map[string]any{
		metaKeyCodeBuddyMessageID: "from-meta",
		metaKeyCodeBuddyTimestamp: "t1",
	}
	withTop := chunkIdentity("sid", "agent_message_chunk", "from-top", meta, "hi")
	withoutTop := chunkIdentity("sid", "agent_message_chunk", "", meta, "hi")

	require.NotEmpty(t, withTop)
	require.NotEmpty(t, withoutTop)
	assert.NotEqual(t, withTop, withoutTop, "the top-level messageId must win")
}

// The key prefers conversationRequestId and falls back to requestId, matching
// upstream's `conversationRequestId ?? requestId`.
func TestChunkIdentity_RequestIDPrecedence(t *testing.T) {
	withConv := chunkIdentity("sid", "agent_message_chunk", "", map[string]any{
		metaKeyCodeBuddyMessageID:             "m1",
		metaKeyCodeBuddyTimestamp:             "t1",
		metaKeyCodeBuddyConversationRequestID: "conv",
		metaKeyCodeBuddyRequestID:             "req",
	}, "hi")
	reqOnly := chunkIdentity("sid", "agent_message_chunk", "", map[string]any{
		metaKeyCodeBuddyMessageID: "m1",
		metaKeyCodeBuddyTimestamp: "t1",
		metaKeyCodeBuddyRequestID: "req",
	}, "hi")

	require.NotEmpty(t, withConv)
	require.NotEmpty(t, reqOnly)
	assert.NotEqual(t, withConv, reqOnly, "conversationRequestId must take precedence over requestId")
}

// Every component participates, so two genuinely different chunks cannot
// collide and have the second dropped.
func TestChunkIdentity_DistinguishesEveryComponent(t *testing.T) {
	full := func(mut func(map[string]any)) map[string]any {
		m := map[string]any{
			metaKeyCodeBuddyMessageID:             "m1",
			metaKeyCodeBuddyTimestamp:             "t1",
			metaKeyCodeBuddyConversationRequestID: "c1",
			metaKeyCodeBuddyParentToolCallID:      "p1",
		}
		mut(m)
		return m
	}
	want := chunkIdentity("sid", "agent_message_chunk", "", full(func(map[string]any) {}), "hi")
	require.NotEmpty(t, want)

	cases := map[string]string{
		"session":  chunkIdentity("sid-2", "agent_message_chunk", "", full(func(map[string]any) {}), "hi"),
		"variant":  chunkIdentity("sid", "agent_thought_chunk", "", full(func(map[string]any) {}), "hi"),
		"text":     chunkIdentity("sid", "agent_message_chunk", "", full(func(map[string]any) {}), "ho"),
		"msgId":    chunkIdentity("sid", "agent_message_chunk", "", full(func(m map[string]any) { m[metaKeyCodeBuddyMessageID] = "m2" }), "hi"),
		"time":     chunkIdentity("sid", "agent_message_chunk", "", full(func(m map[string]any) { m[metaKeyCodeBuddyTimestamp] = "t2" }), "hi"),
		"request":  chunkIdentity("sid", "agent_message_chunk", "", full(func(m map[string]any) { m[metaKeyCodeBuddyConversationRequestID] = "c2" }), "hi"),
		"parentID": chunkIdentity("sid", "agent_message_chunk", "", full(func(m map[string]any) { m[metaKeyCodeBuddyParentToolCallID] = "p2" }), "hi"),
	}
	for name, got := range cases {
		assert.NotEqual(t, want, got, "%s must be part of the identity", name)
	}
}

// Non-volatile variants get no key at all — upstream's isVolatileSessionUpdate.
func TestChunkIdentity_NonVolatileVariantHasNoKey(t *testing.T) {
	meta := map[string]any{
		metaKeyCodeBuddyMessageID: "m1",
		metaKeyCodeBuddyTimestamp: "t1",
	}
	for _, variant := range []string{"tool_call", "tool_call_update", "plan", "session_info_update", ""} {
		assert.Empty(t, chunkIdentity("sid", variant, "", meta, "hi"),
			"%q is not volatile and must not be de-duplicated", variant)
	}
}

// ---------------------------------------------------------------------------
// isDuplicateChunk — the ported window
// ---------------------------------------------------------------------------

func TestIsDuplicateChunk_DropsRepeatInsideWindow(t *testing.T) {
	pinChunkClock(t)
	conn := &ACPConn{}
	id := "key"

	assert.False(t, conn.isDuplicateChunk(id), "first delivery must pass")
	assert.True(t, conn.isDuplicateChunk(id), "repeat inside the window must be dropped")
	assert.True(t, conn.isDuplicateChunk(id), "a third copy is still a duplicate")

	// A different key is unaffected — the guard must not latch.
	assert.False(t, conn.isDuplicateChunk("other"))
}

// Upstream compares `now - seenAt < eQ` against a timestamp it only writes on
// acceptance, so a suppressed duplicate does not slide the window.
func TestIsDuplicateChunk_WindowMeasuredFromAcceptedDelivery(t *testing.T) {
	advance := pinChunkClock(t)
	conn := &ACPConn{}
	id := "key"

	require.False(t, conn.isDuplicateChunk(id))

	advance(chunkDedupWindow - time.Millisecond)
	assert.True(t, conn.isDuplicateChunk(id), "inside the window: duplicate")

	advance(2 * time.Millisecond)
	assert.False(t, conn.isDuplicateChunk(id),
		"past the window measured from acceptance, the same key is content again")
}

func TestIsDuplicateChunk_EmptyIdentityNeverDrops(t *testing.T) {
	pinChunkClock(t)
	conn := &ACPConn{}
	assert.False(t, conn.isDuplicateChunk(""))
	assert.False(t, conn.isDuplicateChunk(""))
}

func TestIsDuplicateChunk_NilConnNeverDrops(t *testing.T) {
	pinChunkClock(t)
	var conn *ACPConn
	assert.False(t, conn.isDuplicateChunk("anything"))
}

func TestResetChunkDedup_AllowsTheSameChunkNextTurn(t *testing.T) {
	pinChunkClock(t)
	conn := &ACPConn{}
	require.False(t, conn.isDuplicateChunk("key"))
	require.True(t, conn.isDuplicateChunk("key"))

	conn.resetChunkDedup()
	assert.False(t, conn.isDuplicateChunk("key"), "a new turn must accept the same identity")
}

// Overflow prunes expired entries rather than discarding live ones.
func TestIsDuplicateChunk_OverflowPrunesExpired(t *testing.T) {
	advance := pinChunkClock(t)
	conn := &ACPConn{}

	for i := range maxChunkDedupEntries {
		require.False(t, conn.isDuplicateChunk("k"+string(rune(i%1000))+string(rune(i/1000))))
	}
	advance(chunkDedupWindow)

	fresh := "fresh"
	require.False(t, conn.isDuplicateChunk(fresh))
	assert.True(t, conn.isDuplicateChunk(fresh),
		"pruning must keep the entry recorded for the fresh chunk")
}

// ---------------------------------------------------------------------------
// end-to-end: the real mapping path
// ---------------------------------------------------------------------------

// drainContentText concatenates every content/thinking payload the mapper
// emitted, so a test can assert on the assembled text exactly as the executor's
// accumulator would.
func drainContentText(t *testing.T, ch chan StreamEvent) string {
	t.Helper()
	var out string
	for {
		select {
		case ev := <-ch:
			if ev.Type == "content" || ev.Type == "thinking" {
				out += ev.Content
			}
		default:
			return out
		}
	}
}

// THE regression. CodeBuddy streams model output character by character, so a
// single table's delimiter row arrives as frames whose text repeats constantly:
//
//	'|'  ' ---'  ' |'  ' ---'  ' |'  '\n'
//
// Each frame carries its own `timestamp`, so upstream's key differs for every
// one of them and none is dropped. A key that ignored `timestamp` (or that used
// a coarser identity) deleted the repeats — measured 2026-10-29: 16 assistant
// messages each lost exactly one `|---|---|`, which left a delimiter row two
// cells short of its header, and marked then rejected the whole table.
//
// Verified against real wire data: 82 frames of a two-table reply produce zero
// duplicate keys under this scheme, while ' |' repeats 21 times with 21
// distinct timestamps.
func TestMapACPSessionUpdate_TableDelimiterRowsSurvive(t *testing.T) {
	pinChunkClock(t)
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "sid")
	ch := make(chan StreamEvent, 512)

	// Real frame sequence for one delimiter row, each with a distinct timestamp.
	frames := []struct{ text, ts string }{
		{"|", "2026-10-29T03:52:47.940Z"},
		{" ---", "2026-10-29T03:52:47.950Z"},
		{" |", "2026-10-29T03:52:47.951Z"},
		{" ---", "2026-10-29T03:52:47.952Z"},
		{" |", "2026-10-29T03:52:47.953Z"},
		{"\n", "2026-10-29T03:52:47.954Z"},
	}
	for _, f := range frames {
		mapACPSessionUpdate(codeBuddyChunk(f.text, f.ts), ch, context.Background(), conn, nil)
	}

	var want strings.Builder
	for _, f := range frames {
		want.WriteString(f.text)
	}
	assert.Equal(t, want.String(), drainContentText(t, ch),
		"every frame of the delimiter row must reach the client")
}

// Three tables in one message: two share the same two-column delimiter row and
// the six-column one contains it as a prefix. All must survive.
func TestMapACPSessionUpdate_MultipleTablesKeepTheirDelimiterRows(t *testing.T) {
	pinChunkClock(t)
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "sid")
	ch := make(chan StreamEvent, 512)

	chunks := []string{
		"| 他的判断 | 核实结果 |\n", "|---|---|\n",
		"| 06 下没有 tools | ✅ |\n\n",
		"| 段 | 分镜稿 | 实际段长 | 配音 | 差额 | 来源 |\n", "|---|---|---|---|---|---|\n",
		"| 钩子 | 3s | 4.0s | 1.512s | +1.0 | 常量 |\n\n",
		"| 已发布 | 讲了什么 |\n", "|---|---|\n",
		"| EP04 | 同一份素材 |\n",
	}
	for i, text := range chunks {
		// Distinct timestamp per frame, as on the wire.
		mapACPSessionUpdate(codeBuddyChunk(text, "2026-10-29T03:52:47."+padTS(i)+"Z"),
			ch, context.Background(), conn, nil)
	}

	got := drainContentText(t, ch)
	assert.Equal(t, strings.Join(chunks, ""), got, "no delimiter row may be dropped")

	var twoCol, sixCol int
	for _, line := range strings.Split(got, "\n") {
		switch line {
		case "|---|---|":
			twoCol++
		case "|---|---|---|---|---|---|":
			sixCol++
		}
	}
	assert.Equal(t, 2, twoCol, "both two-column delimiter rows must survive")
	assert.Equal(t, 1, sixCol, "the six-column delimiter row must survive")
}

func padTS(i int) string {
	s := "000" + string(rune('0'+i%10))
	return s[len(s)-3:]
}

// The guard still has to do its original job: a retry re-sends the SAME frame
// (identical timestamp), so it is dropped.
func TestMapACPSessionUpdate_ImmediateRetryIsDropped(t *testing.T) {
	pinChunkClock(t)
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "sid")
	ch := make(chan StreamEvent, 64)

	// The team bridge retries up to 3 times with the same update object.
	for range 3 {
		mapACPSessionUpdate(codeBuddyChunk("已经完成。\n", "2026-10-29T03:52:47.908Z"),
			ch, context.Background(), conn, nil)
	}

	assert.Equal(t, "已经完成。\n", drainContentText(t, ch),
		"a retry of the same frame must not duplicate the text")
}

// Thinking chunks take the same path.
func TestMapACPSessionUpdate_ThoughtChunkRetryIsDropped(t *testing.T) {
	pinChunkClock(t)
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "sid")
	ch := make(chan StreamEvent, 64)

	mid := "m1"
	for range 2 {
		mapACPSessionUpdate(acp.SessionUpdate{
			AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{
				Meta: map[string]any{
					metaKeyCodeBuddyMessageID: mid,
					metaKeyCodeBuddyTimestamp: "t1",
				},
				MessageId: &mid,
				Content:   acp.TextBlock("嗯"),
			},
		}, ch, context.Background(), conn, nil)
	}

	assert.Equal(t, "嗯", drainContentText(t, ch))
}

// The guard is CodeBuddy-only: another agent repeating identical text must reach
// the client intact.
func TestMapACPSessionUpdate_DedupDoesNotApplyToOtherBackends(t *testing.T) {
	pinChunkClock(t)
	conn := newACPConn(&model.Agent{ID: "claude", Backend: "claude"}, "sid")
	ch := make(chan StreamEvent, 64)

	for range 3 {
		mapACPSessionUpdate(codeBuddyChunk("哈", "2026-10-29T03:52:47.908Z"),
			ch, context.Background(), conn, nil)
	}

	assert.Equal(t, "哈哈哈", drainContentText(t, ch))
}

// A frame without the identity pair is never dropped: dropping content we
// cannot identify is worse than the duplication.
func TestMapACPSessionUpdate_KeepsChunksWithoutIdentity(t *testing.T) {
	pinChunkClock(t)
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "sid")
	ch := make(chan StreamEvent, 64)

	for range 3 {
		mapACPSessionUpdate(acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
				Content: acp.TextBlock("x"),
			},
		}, ch, context.Background(), conn, nil)
	}

	assert.Equal(t, "xxx", drainContentText(t, ch))
}

func TestChunkDedupEnabled_OnlyCodeBuddy(t *testing.T) {
	assert.True(t, chunkDedupEnabled("codebuddy"))
	for _, backend := range []string{"claude", "codex", "kimi", "opencode", "hermes", "cursor", "pi", ""} {
		assert.False(t, chunkDedupEnabled(backend), "%s must not be de-duplicated", backend)
	}
}
