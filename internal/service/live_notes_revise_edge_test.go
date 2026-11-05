package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Empty and unusable answers ─────────────────────────────────────────────

// An empty answer carries no edit at all. It must be a format error rather
// than a silent "nothing to fix", otherwise the stage would report success
// while the transcript was never checked.
func TestParseLiveNoteAnswer_EmptyAnswerIsAFormatError(t *testing.T) {
	for _, raw := range []string{"", "   ", "\n\t"} {
		pairs, splits, err := parseLiveNoteAnswer(raw)
		require.Error(t, err, raw)
		assert.ErrorIs(t, err, ErrLiveNoteReviseFormat, raw)
		assert.Empty(t, pairs, raw)
		assert.Empty(t, splits, raw)
	}
}

// A pair whose TARGET side is longer than the limit is unusable even though
// its source side is fine. The pair must be dropped, and the usable pair in the
// same answer must survive — one bad row must not cost the whole chunk.
func TestParseLiveNoteAnswer_DropsPairWhoseTargetIsTooLong(t *testing.T) {
	raw := "科劳本奇 => " + strings.Repeat("很", liveNoteRevisePairMaxRunes+1) + "\n王小明 => 王总"
	pairs, splits, err := parseLiveNoteAnswer(raw)
	require.NoError(t, err)
	assert.Empty(t, splits)
	require.Len(t, pairs, 1, "the usable pair must survive the unusable one")
	assert.Equal(t, liveNotePair{From: "王小明", To: "王总"}, pairs[0])
}

// ── Split lines ────────────────────────────────────────────────────────────

// A "==>" line that carries no clock is not a split: the clock is what keeps a
// split from being applied to an unrelated line.
func TestParseLiveNoteSplitLine_WithoutClockIsNotASplit(t *testing.T) {
	split, ok := parseLiveNoteSplitLine("我：你好 ==> 张三")
	assert.False(t, ok)
	assert.Equal(t, liveNoteSplit{}, split)
}

// A clocked line that has no "==>" arrow is not a split either.
func TestParseLiveNoteSplitLine_WithoutArrowIsNotASplit(t *testing.T) {
	split, ok := parseLiveNoteSplitLine("[00:03] 我：你好")
	assert.False(t, ok)
	assert.Equal(t, liveNoteSplit{}, split)
}

// ── Clock parsing ──────────────────────────────────────────────────────────

// liveNoteClockBody is the only thing that separates a transcript line from a
// free-text line, so every malformed clock shape has to be rejected rather
// than half-parsed.
func TestLiveNoteClockBody_RejectsMalformedClocks(t *testing.T) {
	cases := []string{
		"no bracket at all", // no "[" prefix
		"[]body",            // empty bracket
		"[",                 // no closing bracket
		"[   ]body",         // whitespace-only clock
	}
	for _, line := range cases {
		clock, body, ok := liveNoteClockBody(line)
		assert.False(t, ok, line)
		assert.Empty(t, clock, line)
		assert.Empty(t, body, line)
	}
}

func TestLiveNoteClockBody_AcceptsAWellFormedClock(t *testing.T) {
	clock, body, ok := liveNoteClockBody("[00:07] 我：你好")
	require.True(t, ok)
	assert.Equal(t, "00:07", clock)
	assert.Equal(t, "我：你好", body)
}

// ── Applying splits ────────────────────────────────────────────────────────

// No splits means no change: applying them must not touch the transcript, not
// even its line endings.
func TestApplyLiveNoteSplits_NoSplitsIsANoOp(t *testing.T) {
	transcript := "[00:00] 我：你好\n[00:05] 张三：好的"
	assert.Equal(t, transcript, applyLiveNoteSplits(transcript, nil))
	assert.Equal(t, transcript, applyLiveNoteSplits(transcript, []liveNoteSplit{}))
}

// A line without a clock cannot be cut: it is not a transcript line, so it must
// come back untouched.
func TestSplitLiveNoteLineAt_LineWithoutClockIsUnchanged(t *testing.T) {
	got := splitLiveNoteLineAt("just some text", []liveNoteSplit{{Clock: "00:00", From: "text", Speaker: "李四"}}, "|")
	assert.Equal(t, []string{"just some text"}, got)
}

// Two cuts in the same line must be applied left to right, and a cut that
// starts at or before the previous one must be skipped — otherwise the same
// words would be emitted twice.
func TestSplitLiveNoteLineAt_OrdersCutsAndSkipsOverlappingOnes(t *testing.T) {
	line := "[00:00] 我：甲乙丙丁"
	splits := []liveNoteSplit{
		{Clock: "00:00", From: "丙", Speaker: "乙 speaker"},
		{Clock: "00:00", From: "甲", Speaker: "甲 speaker"},
		{Clock: "00:00", From: "丙", Speaker: "丙 speaker"},
	}
	got := splitLiveNoteLineAt(line, splits, "|")
	require.Len(t, got, 3, "two real cuts produce three lines: %v", got)
	assert.Equal(t, "[00:00] 我：", got[0], "the cut happens BEFORE the word, so it stays with the first speaker")
	assert.Equal(t, "[00:00] 甲 speaker|甲乙", got[1])
	assert.Equal(t, "[00:00] 乙 speaker|丙丁", got[2], "the third cut starts at the same offset as the second and must be skipped")
}
