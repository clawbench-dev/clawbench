package ai

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// pinChunkClock replaces the dedup clock with a controllable one for the
// duration of the test, returning an advance function. Tests must not sleep:
// the window is a semantic boundary, and a real-clock test would either take
// 5s per case or assert on a value the implementation happens to use.
func pinChunkClock(t *testing.T) func(time.Duration) {
	t.Helper()
	base := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	now := base
	prev := chunkDedupNow
	chunkDedupNow = func() time.Time { return now }
	t.Cleanup(func() { chunkDedupNow = prev })
	return func(d time.Duration) { now = now.Add(d) }
}

// ---------------------------------------------------------------------------
// chunkIdentity
// ---------------------------------------------------------------------------

func TestChunkIdentity_RequiresMessageIDAndTimestamp(t *testing.T) {
	sid := "sid-1"
	base := map[string]any{
		metaKeyCodeBuddyMessageID: "m1",
		metaKeyCodeBuddyTimestamp: "1792590363737",
	}

	t.Run("both present", func(t *testing.T) {
		assert.NotEmpty(t, chunkIdentity(sid, base, "agent_message_chunk", "hi"))
	})

	// Upstream's own rule: no key without BOTH halves. A text-only key would
	// also match a model legitimately repeating a token, so it must not exist.
	t.Run("missing timestamp yields no key", func(t *testing.T) {
		assert.Empty(t, chunkIdentity(sid, map[string]any{
			metaKeyCodeBuddyMessageID: "m1",
		}, "agent_message_chunk", "hi"))
	})

	t.Run("missing messageId yields no key", func(t *testing.T) {
		assert.Empty(t, chunkIdentity(sid, map[string]any{
			metaKeyCodeBuddyTimestamp: "1",
		}, "agent_message_chunk", "hi"))
	})

	t.Run("empty meta yields no key", func(t *testing.T) {
		assert.Empty(t, chunkIdentity(sid, nil, "agent_message_chunk", "hi"))
	})
}

// Every component must participate, otherwise two genuinely different chunks
// could collide and the second would be dropped as a "duplicate".
func TestChunkIdentity_DistinguishesEveryComponent(t *testing.T) {
	base := map[string]any{
		metaKeyCodeBuddyMessageID:   "m1",
		metaKeyCodeBuddyTimestamp:   "t1",
		metaKeyCodeBuddyMemberEvent: "alpha",
	}
	want := chunkIdentity("sid", base, "agent_message_chunk", "hi")
	require.NotEmpty(t, want)

	otherSession := chunkIdentity("sid-2", base, "agent_message_chunk", "hi")
	otherVariant := chunkIdentity("sid", base, "agent_thought_chunk", "hi")
	otherText := chunkIdentity("sid", base, "agent_message_chunk", "ho")
	otherMember := chunkIdentity("sid", map[string]any{
		metaKeyCodeBuddyMessageID:   "m1",
		metaKeyCodeBuddyTimestamp:   "t1",
		metaKeyCodeBuddyMemberEvent: "beta",
	}, "agent_message_chunk", "hi")
	otherMsg := chunkIdentity("sid", map[string]any{
		metaKeyCodeBuddyMessageID:   "m2",
		metaKeyCodeBuddyTimestamp:   "t1",
		metaKeyCodeBuddyMemberEvent: "alpha",
	}, "agent_message_chunk", "hi")
	otherTS := chunkIdentity("sid", map[string]any{
		metaKeyCodeBuddyMessageID:   "m1",
		metaKeyCodeBuddyTimestamp:   "t2",
		metaKeyCodeBuddyMemberEvent: "alpha",
	}, "agent_message_chunk", "hi")

	for name, got := range map[string]string{
		"session": otherSession,
		"variant": otherVariant,
		"text":    otherText,
		"member":  otherMember,
		"msgId":   otherMsg,
		"time":    otherTS,
	} {
		assert.NotEqual(t, want, got, "%s must be part of the identity", name)
	}
}

// ---------------------------------------------------------------------------
// isDuplicateChunk
// ---------------------------------------------------------------------------

func TestIsDuplicateChunk_DropsExactRepeatOnly(t *testing.T) {
	pinChunkClock(t)
	conn := &ACPConn{}
	id := "sid\x00agent_message_chunk\x00m1\x00t1\x00alpha\x00hi"

	assert.False(t, conn.isDuplicateChunk(id), "first delivery must pass")
	assert.True(t, conn.isDuplicateChunk(id), "second identical delivery must be dropped")
	assert.True(t, conn.isDuplicateChunk(id), "third identical delivery must be dropped too")

	// A different chunk after a duplicate is still accepted — the guard must not
	// latch into "drop everything".
	assert.False(t, conn.isDuplicateChunk("sid\x00agent_message_chunk\x00m1\x00t1\x00alpha\x00ho"))
}

// The retry that produces duplicates re-sends the same update milliseconds
// later, so a repeat inside the window is still a duplicate. This is the case
// the guard exists for and it must keep working.
func TestIsDuplicateChunk_RepeatInsideWindowIsDropped(t *testing.T) {
	advance := pinChunkClock(t)
	conn := &ACPConn{}
	id := "sid\x00agent_message_chunk\x00m1\x00t1\x00alpha\x00hi"

	require.False(t, conn.isDuplicateChunk(id))

	advance(chunkDedupWindow - time.Millisecond)
	assert.True(t, conn.isDuplicateChunk(id),
		"a retry just inside the window is still the same delivery")

	// The window is measured from the ACCEPTED delivery and does not slide on a
	// dropped one — upstream's `em - eA < eQ` compares against the recorded
	// time, which a suppressed duplicate never updates. So once the window has
	// elapsed since acceptance, the same identity is content again.
	advance(2 * time.Millisecond)
	assert.False(t, conn.isDuplicateChunk(id),
		"past the window measured from acceptance, the identity is no longer a duplicate")
}

// The identity is only unique within one message and one publication instant:
// `timestamp` is message-level, so the text is the ONLY varying component.
// Identical text is normal model output — every GFM table's delimiter row is
// the same string — so an unbounded set deletes real content.
//
// Production evidence (2026-10-25): 16 assistant messages each lost exactly one
// `|---|---` chunk, and every one of them was emitted well past the window.
func TestIsDuplicateChunk_SameTextAfterWindowIsNotDuplicate(t *testing.T) {
	advance := pinChunkClock(t)
	conn := &ACPConn{}

	// One assistant message: same messageId, same message-level timestamp, so
	// only the text distinguishes chunks.
	const delim = "|---|---|"
	first := "sid\x00agent_message_chunk\x00m1\x00t1\x00\x00" + delim
	second := first // same message, same text, later in the stream

	require.False(t, conn.isDuplicateChunk(first), "first table's delimiter row must be delivered")

	// A second table's delimiter row, emitted seconds later in the same message.
	advance(chunkDedupWindow)
	assert.False(t, conn.isDuplicateChunk(second),
		"an identical delimiter row past the window is real content, not a retry")
}

// End-to-end shape of the regression: a message with N tables produces N-1
// deletions when the window is missing. Drive the actual delimiter sequence and
// assert every row survives.
func TestIsDuplicateChunk_EveryTableDelimiterRowSurvives(t *testing.T) {
	advance := pinChunkClock(t)
	conn := &ACPConn{}

	// A turn emitting several tables, each introduced by the same delimiter row.
	// The gaps are far larger than a retry but well within one turn.
	rows := []string{"|---|---|", "|---|---|", "|---|---|", "|---|---|", "|---|---|"}
	for i, row := range rows {
		id := "sid\x00agent_message_chunk\x00m1\x00t1\x00\x00" + row
		assert.False(t, conn.isDuplicateChunk(id),
			"table %d's delimiter row must be delivered", i+1)
		advance(12 * time.Second) // ~one table of prose apart
	}
}

// A duplicate that arrives inside the window is still suppressed even when the
// same text was legitimately delivered earlier in the turn.
func TestIsDuplicateChunk_RetryStillDroppedAfterLegitimateRepeat(t *testing.T) {
	advance := pinChunkClock(t)
	conn := &ACPConn{}
	id := "sid\x00agent_message_chunk\x00m1\x00t1\x00\x00|---|---|"

	require.False(t, conn.isDuplicateChunk(id), "first delivery accepted")
	advance(chunkDedupWindow)
	require.False(t, conn.isDuplicateChunk(id), "second delivery past the window accepted")

	// Now a retry of that second send, milliseconds later.
	advance(50 * time.Millisecond)
	assert.True(t, conn.isDuplicateChunk(id),
		"a retry of the latest delivery must still be dropped")
}

func TestIsDuplicateChunk_EmptyIdentityNeverDrops(t *testing.T) {
	pinChunkClock(t)
	conn := &ACPConn{}
	// Frames without the identity pair cannot be de-duplicated; dropping them
	// would lose content.
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
	id := "sid\x00agent_message_chunk\x00m1\x00t1\x00alpha\x00hi"
	require.False(t, conn.isDuplicateChunk(id))
	require.True(t, conn.isDuplicateChunk(id))

	conn.resetChunkDedup()
	assert.False(t, conn.isDuplicateChunk(id), "a new turn must accept the same identity again")
}

// A very long turn must not grow the set without bound. Pruning drops expired
// identities first; only a still-full set is cleared wholesale, which can only
// let an OLD duplicate through — never drop a fresh chunk.
func TestIsDuplicateChunk_BoundedSetStillAcceptsNewChunks(t *testing.T) {
	pinChunkClock(t)
	conn := &ACPConn{}
	for i := range maxDupChunkKeys + 10 {
		id := "sid\x00agent_message_chunk\x00m" + strconv.Itoa(i) + "\x00t\x00alpha\x00hi"
		assert.False(t, conn.isDuplicateChunk(id), "unique chunk %d must be accepted", i)
	}
	// Still functional after the reset that the cap triggers.
	assert.False(t, conn.isDuplicateChunk("sid\x00agent_message_chunk\x00fresh\x00t\x00alpha\x00hi"))
	assert.True(t, conn.isDuplicateChunk("sid\x00agent_message_chunk\x00fresh\x00t\x00alpha\x00hi"))
}

// Overflow must prune expired identities rather than discarding the live ones:
// after the window passes, the cap should be reclaimed by pruning, so a chunk
// delivered moments ago still counts as a duplicate.
func TestIsDuplicateChunk_OverflowPrunesExpiredBeforeClearing(t *testing.T) {
	advance := pinChunkClock(t)
	conn := &ACPConn{}

	// Fill the set, then let every entry expire.
	for i := range maxDupChunkKeys {
		id := "sid\x00agent_message_chunk\x00m" + strconv.Itoa(i) + "\x00t\x00alpha\x00hi"
		require.False(t, conn.isDuplicateChunk(id))
	}
	advance(chunkDedupWindow)

	// A fresh delivery triggers the cap check; expired entries are pruned, so
	// the map must not be cleared wholesale.
	fresh := "sid\x00agent_message_chunk\x00fresh\x00t\x00alpha\x00hi"
	require.False(t, conn.isDuplicateChunk(fresh))
	assert.True(t, conn.isDuplicateChunk(fresh),
		"pruning must keep the entry recorded for the fresh chunk")
}

// ---------------------------------------------------------------------------
// backend scoping
// ---------------------------------------------------------------------------

func TestChunkDedupEnabled_OnlyCodeBuddy(t *testing.T) {
	assert.True(t, chunkDedupEnabled("codebuddy"))
	for _, backend := range []string{"claude", "codex", "kimi", "opencode", "hermes", "cursor", ""} {
		assert.False(t, chunkDedupEnabled(backend), "%s must not be de-duplicated", backend)
	}
}

// ---------------------------------------------------------------------------
// end-to-end: the real ACP mapping path
// ---------------------------------------------------------------------------

// codeBuddyChunk builds the _meta a real CodeBuddy agent_message_chunk carries:
// message-level messageId and timestamp, so only the text varies between chunks
// of one assistant message.
func codeBuddyChunk(text string) acp.SessionUpdate {
	return acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Meta: map[string]any{
				metaKeyCodeBuddyMessageID: "01a16e4c-9b6a-7848-b24b-853b623d8739",
				metaKeyCodeBuddyTimestamp: "1792851899974",
			},
			Content: acp.TextBlock(text),
		},
	}
}

// TestMapACPSessionUpdate_TableDelimiterRowsSurvive is the regression for the
// 2026-10-25 content loss: one assistant message containing several GFM tables.
// Every table's delimiter row is the same 8-byte string, so an unbounded
// de-duplication key treated the second and later ones as retries and deleted
// them, leaving a delimiter row shorter than its header. marked then refuses the
// table entirely and the whole block renders as a paragraph.
//
// Real shape (chat_history id=65192, session 12188d47): three tables, the first
// and third both `|---|---|`, emitted minutes apart inside one turn.
func TestMapACPSessionUpdate_TableDelimiterRowsSurvive(t *testing.T) {
	advance := pinChunkClock(t)
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "sid")
	ch := make(chan StreamEvent, 256)

	// Two-column table, prose, then a six-column table, then another two-column
	// table — the same layout that lost a chunk in production.
	chunks := []string{
		"| 他的判断 | 核实结果 |\n",
		"|---|---|\n",
		"| 06 下没有 tools | ✅ |\n\n",
		"| 段 | 分镜稿 | 实际段长 | 配音 | 差额 | 来源 |\n",
		"|---|---|---|---|---|---|\n",
		"| 钩子 | 3s | 4.0s | 1.512s | +1.0 | 常量 |\n\n",
		"| 已发布 | 讲了什么 |\n",
		"|---|---|\n",
		"| EP04 | 同一份素材 |\n",
	}
	for i, text := range chunks {
		mapACPSessionUpdate(codeBuddyChunk(text), ch, context.Background(), conn, nil)
		if i%2 == 1 {
			advance(20 * time.Second) // a table of prose between delimiters
		}
	}

	got := drainContentText(t, ch)
	want := strings.Join(chunks, "")
	assert.Equal(t, want, got,
		"every table delimiter row must reach the client; a dropped one breaks the table")

	// Count whole lines, not substrings: "|---|---|" is also a prefix of
	// "|---|---|---|---|---|---|", so a substring count would over-count.
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

// The guard still has to do its original job: a retry that re-sends the same
// update milliseconds later must not duplicate the text.
func TestMapACPSessionUpdate_ImmediateRetryStillDropped(t *testing.T) {
	pinChunkClock(t)
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "sid")
	ch := make(chan StreamEvent, 256)

	// The team bridge retries up to 3 times within the send timeout.
	for range 3 {
		mapACPSessionUpdate(codeBuddyChunk("已经完成。\n"), ch, context.Background(), conn, nil)
	}

	assert.Equal(t, "已经完成。\n", drainContentText(t, ch),
		"a retry inside the window must not duplicate the text")
}

// A retry arriving after the window is treated as new content. That is the
// deliberate trade-off: the guard can no longer tell it from a legitimate
// repeat, and duplicating is recoverable while deleting is not.
func TestMapACPSessionUpdate_RetryAfterWindowIsKept(t *testing.T) {
	advance := pinChunkClock(t)
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "sid")
	ch := make(chan StreamEvent, 256)

	mapACPSessionUpdate(codeBuddyChunk("abc"), ch, context.Background(), conn, nil)
	advance(chunkDedupWindow)
	mapACPSessionUpdate(codeBuddyChunk("abc"), ch, context.Background(), conn, nil)

	assert.Equal(t, "abcabc", drainContentText(t, ch),
		"past the window the guard must not delete content it cannot prove is a retry")
}
