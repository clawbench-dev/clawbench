package ai

import (
	"context"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// codeBuddyChunk builds an agent_message_chunk with the _meta a real CodeBuddy
// frame carries (verified on the wire 2026-10-29). `ts` is stamped per frame at
// send time, so two frames of one message normally differ here; a retry reuses
// the whole object and therefore repeats it.
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

// ---------------------------------------------------------------------------
// chunkIdentity
// ---------------------------------------------------------------------------

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

// The key prefers conversationRequestId and falls back to requestId.
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

// The key must be unambiguous: the old implementation joined fields with \x00,
// which a text containing a NUL byte could forge. JSON escaping cannot.
func TestChunkIdentity_IsInjective(t *testing.T) {
	meta := map[string]any{
		metaKeyCodeBuddyMessageID: "m1",
		metaKeyCodeBuddyTimestamp: "t1",
	}
	// Old scheme: "a\x00b" and "a" + "\x00b" collide. JSON must not.
	a := chunkIdentity("sid", "agent_message_chunk", "", meta, "a\x00b")
	b := chunkIdentity("sid", "agent_message_chunk", "", meta, "a\"b")
	assert.NotEqual(t, a, b)
}

// ---------------------------------------------------------------------------
// isDuplicateChunk — adjacency, not elapsed time
// ---------------------------------------------------------------------------

func TestIsDuplicateChunk_DropsOnlyTheImmediatelyRepeatedChunk(t *testing.T) {
	conn := &ACPConn{}

	assert.False(t, conn.isDuplicateChunk("k1"), "first delivery passes")
	assert.True(t, conn.isDuplicateChunk("k1"), "an immediate repeat is a retry")
	assert.True(t, conn.isDuplicateChunk("k1"), "and so is a third copy")
}

// THE regression. A retry is adjacent; a legitimate repeat is not.
//
// CodeBuddy streams character by character, so a delimiter row arrives as
// `'|' ' ---' ' |' ' ---' ' |'`. The two `' ---'` frames are identical but are
// separated by `' |'`, so they are both content. A time-window guard (5 s, as
// upstream uses on its publish bus) deleted the second one; that is exactly the
// production defect — 6436 chunks dropped over one week, median gap between
// drops 11.7 s, leaving 16 assistant messages with a `|---|---|` missing and the
// whole table rendering as a paragraph.
func TestIsDuplicateChunk_NonAdjacentRepeatIsContent(t *testing.T) {
	conn := &ACPConn{}

	assert.False(t, conn.isDuplicateChunk("delim"), "first ' ---'")
	assert.False(t, conn.isDuplicateChunk("pipe"), "intervening ' |'")
	assert.False(t, conn.isDuplicateChunk("delim"),
		"the second ' ---' is separated by output, so it is content — a 5 s window would delete it here")
}

// A repeat that happens long after its twin is still content. This is the case
// a time window gets wrong at any size, and the reason the window was removed.
func TestIsDuplicateChunk_RepeatMuchLaterIsContent(t *testing.T) {
	conn := &ACPConn{}

	assert.False(t, conn.isDuplicateChunk("row"))
	// Hundreds of frames later, the same delimiter row appears again.
	for i := range 200 {
		assert.False(t, conn.isDuplicateChunk("filler"+string(rune('a'+i%26))+string(rune('a'+i/26))))
	}
	assert.False(t, conn.isDuplicateChunk("row"), "a distant repeat is content")
}

func TestIsDuplicateChunk_EmptyIdentityNeverDrops(t *testing.T) {
	conn := &ACPConn{}
	// Frames without the identity pair cannot be de-duplicated; dropping them
	// would lose content.
	assert.False(t, conn.isDuplicateChunk(""))
	assert.False(t, conn.isDuplicateChunk(""))
}

func TestIsDuplicateChunk_NilConnNeverDrops(t *testing.T) {
	var conn *ACPConn
	assert.False(t, conn.isDuplicateChunk("anything"))
}

func TestResetChunkDedup_ForgetsThePreviousFrame(t *testing.T) {
	conn := &ACPConn{}
	require.False(t, conn.isDuplicateChunk("k1"))

	conn.resetChunkDedup()
	// Without the reset, the first chunk of the next turn could be mistaken for
	// a retry of the last chunk of the previous turn.
	assert.False(t, conn.isDuplicateChunk("k1"), "a new turn starts with no previous frame")
}

// A dropped retry must not become the new "previous": three copies of one frame
// collapse to one, and the frame after them is still compared against the
// accepted one.
func TestIsDuplicateChunk_RetryDoesNotReplacePrevious(t *testing.T) {
	conn := &ACPConn{}

	require.False(t, conn.isDuplicateChunk("a"))
	require.True(t, conn.isDuplicateChunk("a"))
	require.True(t, conn.isDuplicateChunk("a"))
	assert.False(t, conn.isDuplicateChunk("b"), "a new frame after the retries is content")
	assert.True(t, conn.isDuplicateChunk("b"), "and its own retry is still dropped")
}

// ---------------------------------------------------------------------------
// end-to-end: the real mapping path
// ---------------------------------------------------------------------------

// A delimiter row is six frames; none may be lost.
func TestMapACPSessionUpdate_TableDelimiterRowSurvives(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "sid")
	ch := make(chan StreamEvent, 512)

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

// Three tables in one message; two share the same two-column delimiter row.
func TestMapACPSessionUpdate_MultipleTablesKeepTheirDelimiterRows(t *testing.T) {
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
		mapACPSessionUpdate(codeBuddyChunk(text, tsFor(i)), ch, context.Background(), conn, nil)
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

// tsFor produces a distinct timestamp per index. It must not wrap: a repeated
// timestamp combined with repeated text would be indistinguishable from a
// retry, i.e. the helper would reproduce the bug under test.
func tsFor(i int) string {
	return "2026-10-29T03:52:47." + strings.Repeat("0", 3-len(itoa(i))) + itoa(i) + "Z"
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// The guard still has to do its original job: a retry re-sends the SAME frame.
func TestMapACPSessionUpdate_ImmediateRetryIsDropped(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "sid")
	ch := make(chan StreamEvent, 64)

	for range 3 {
		mapACPSessionUpdate(codeBuddyChunk("已经完成。\n", "2026-10-29T03:52:47.908Z"),
			ch, context.Background(), conn, nil)
	}

	assert.Equal(t, "已经完成。\n", drainContentText(t, ch),
		"a retry of the same frame must not duplicate the text")
}

// A frame identical to an earlier one, but with output in between, must survive
// end to end. This is the production shape of the content loss.
func TestMapACPSessionUpdate_SameTextLaterInMessageIsKept(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "sid")
	ch := make(chan StreamEvent, 512)

	// Two tables separated by a paragraph, both using the same delimiter row.
	chunks := []string{
		"| A | B |\n", "|---|---|\n", "| 1 | 2 |\n\n",
		"中间一段说明文字。\n\n",
		"| C | D |\n", "|---|---|\n", "| 3 | 4 |\n",
	}
	for i, text := range chunks {
		mapACPSessionUpdate(codeBuddyChunk(text, tsFor(i)), ch, context.Background(), conn, nil)
	}

	got := drainContentText(t, ch)
	assert.Equal(t, strings.Join(chunks, ""), got)
	assert.Equal(t, 2, strings.Count(got, "|---|---|"), "both delimiter rows must survive")
}

func TestMapACPSessionUpdate_ThoughtChunkRetryIsDropped(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "sid")
	ch := make(chan StreamEvent, 64)

	mid := "m1"
	frame := acp.SessionUpdate{
		AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{
			Meta: map[string]any{
				metaKeyCodeBuddyMessageID: mid,
				metaKeyCodeBuddyTimestamp: "t1",
			},
			MessageId: &mid,
			Content:   acp.TextBlock("嗯"),
		},
	}
	for range 2 {
		mapACPSessionUpdate(frame, ch, context.Background(), conn, nil)
	}

	assert.Equal(t, "嗯", drainContentText(t, ch))
}

// The guard is CodeBuddy-only: another agent repeating identical text must reach
// the client intact.
func TestMapACPSessionUpdate_DedupDoesNotApplyToOtherBackends(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "claude", Backend: "claude"}, "sid")
	ch := make(chan StreamEvent, 64)

	for range 3 {
		mapACPSessionUpdate(codeBuddyChunk("哈", "2026-10-29T03:52:47.908Z"),
			ch, context.Background(), conn, nil)
	}

	assert.Equal(t, "哈哈哈", drainContentText(t, ch))
}

// A frame without the identity pair is never dropped.
func TestMapACPSessionUpdate_KeepsChunksWithoutIdentity(t *testing.T) {
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

// ---------------------------------------------------------------------------
// real wire replay
// ---------------------------------------------------------------------------

// TestMapACPSessionUpdate_RealWireSequenceIsLossless replays the EXACT frame
// sequence captured from a real `codebuddy --acp` session (2026-10-29, 82
// agent_message_chunk frames of a two-table reply) and asserts every byte
// arrives.
//
// This is the test that can catch the actual production defect. The reply is
// character-streamed: it contains frames with identical text separated by a
// single frame (two ' ---' with one ' |' between them) and frames stamped in the
// SAME millisecond. Hand-written fixtures with strictly increasing timestamps
// cannot reproduce that shape, which is why they passed against the buggy build.
func TestMapACPSessionUpdate_RealWireSequenceIsLossless(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "sid")
	ch := make(chan StreamEvent, 1024)

	frames := []struct{ text, ts string }{
		{"|", "2026-10-29T03:52:47.908Z"},
		{" ", "2026-10-29T03:52:47.909Z"},
		{"项目", "2026-10-29T03:52:47.910Z"},
		{" |", "2026-10-29T03:52:47.920Z"},
		{" ", "2026-10-29T03:52:47.921Z"},
		{"说明", "2026-10-29T03:52:47.922Z"},
		{" |\n", "2026-10-29T03:52:47.939Z"},
		{"|", "2026-10-29T03:52:47.940Z"},
		{" ---", "2026-10-29T03:52:47.950Z"},
		{" |", "2026-10-29T03:52:47.951Z"},
		{" ---", "2026-10-29T03:52:47.952Z"},
		{" |\n", "2026-10-29T03:52:47.952Z"},
		{"|", "2026-10-29T03:52:47.953Z"},
		{" ", "2026-10-29T03:52:47.953Z"},
		{"列", "2026-10-29T03:52:47.965Z"},
		{"一", "2026-10-29T03:52:47.980Z"},
		{" |", "2026-10-29T03:52:47.981Z"},
		{" ", "2026-10-29T03:52:47.982Z"},
		{"内容", "2026-10-29T03:52:47.982Z"},
		{" |\n", "2026-10-29T03:52:47.995Z"},
		{"|", "2026-10-29T03:52:47.996Z"},
		{" ", "2026-10-29T03:52:47.997Z"},
		{"列", "2026-10-29T03:52:47.997Z"},
		{"二", "2026-10-29T03:52:47.998Z"},
		{" |", "2026-10-29T03:52:48.017Z"},
		{" ", "2026-10-29T03:52:48.021Z"},
		{"内容", "2026-10-29T03:52:48.023Z"},
		{" |\n\n", "2026-10-29T03:52:48.024Z"},
		{"|", "2026-10-29T03:52:48.025Z"},
		{" A", "2026-10-29T03:52:48.026Z"},
		{" |", "2026-10-29T03:52:48.042Z"},
		{" B", "2026-10-29T03:52:48.045Z"},
		{" |", "2026-10-29T03:52:48.046Z"},
		{" C", "2026-10-29T03:52:48.048Z"},
		{" |", "2026-10-29T03:52:48.050Z"},
		{" D", "2026-10-29T03:52:48.051Z"},
		{" |", "2026-10-29T03:52:48.058Z"},
		{" E", "2026-10-29T03:52:48.061Z"},
		{" |\n", "2026-10-29T03:52:48.063Z"},
		{"|", "2026-10-29T03:52:48.065Z"},
		{" ---", "2026-10-29T03:52:48.071Z"},
		{" |", "2026-10-29T03:52:48.072Z"},
		{" ---", "2026-10-29T03:52:48.074Z"},
		{" |", "2026-10-29T03:52:48.074Z"},
		{" ---", "2026-10-29T03:52:48.075Z"},
		{" |", "2026-10-29T03:52:48.076Z"},
		{" ---", "2026-10-29T03:52:48.116Z"},
		{" |", "2026-10-29T03:52:48.117Z"},
		{" ---", "2026-10-29T03:52:48.118Z"},
		{" |\n", "2026-10-29T03:52:48.119Z"},
		{"|", "2026-10-29T03:52:48.121Z"},
		{" ", "2026-10-29T03:52:48.123Z"},
		{"1", "2026-10-29T03:52:48.124Z"},
		{" |", "2026-10-29T03:52:48.125Z"},
		{" ", "2026-10-29T03:52:48.127Z"},
		{"2", "2026-10-29T03:52:48.128Z"},
		{" |", "2026-10-29T03:52:48.129Z"},
		{" ", "2026-10-29T03:52:48.131Z"},
		{"3", "2026-10-29T03:52:48.133Z"},
		{" |", "2026-10-29T03:52:48.135Z"},
		{" ", "2026-10-29T03:52:48.137Z"},
		{"4", "2026-10-29T03:52:48.138Z"},
		{" |", "2026-10-29T03:52:48.140Z"},
		{" ", "2026-10-29T03:52:48.142Z"},
		{"5", "2026-10-29T03:52:48.144Z"},
		{" |\n", "2026-10-29T03:52:48.146Z"},
		{"|", "2026-10-29T03:52:48.149Z"},
		{" ", "2026-10-29T03:52:48.150Z"},
		{"6", "2026-10-29T03:52:48.152Z"},
		{" |", "2026-10-29T03:52:48.154Z"},
		{" ", "2026-10-29T03:52:48.155Z"},
		{"7", "2026-10-29T03:52:48.156Z"},
		{" |", "2026-10-29T03:52:48.157Z"},
		{" ", "2026-10-29T03:52:48.159Z"},
		{"8", "2026-10-29T03:52:48.160Z"},
		{" |", "2026-10-29T03:52:48.162Z"},
		{" ", "2026-10-29T03:52:48.164Z"},
		{"9", "2026-10-29T03:52:48.166Z"},
		{" |", "2026-10-29T03:52:48.167Z"},
		{" ", "2026-10-29T03:52:48.170Z"},
		{"10", "2026-10-29T03:52:48.171Z"},
		{" |", "2026-10-29T03:52:48.182Z"},
	}
	for _, f := range frames {
		mapACPSessionUpdate(codeBuddyChunk(f.text, f.ts), ch, context.Background(), conn, nil)
	}

	var want string
	for _, f := range frames {
		want += f.text
	}
	assert.Equal(t, want, drainContentText(t, ch),
		"every captured frame must reach the client byte for byte")
}
