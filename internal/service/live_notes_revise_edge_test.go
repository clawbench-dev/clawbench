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

// An answer where every line HAS an arrow but none of them can be applied is the
// case the old error reported as `(first line: "")` — it collected only lines
// without an arrow, which is the one thing this answer does not contain. The
// message has to name the line and the reason, because the person reading it is
// the one deciding whether to retry or switch models.
func TestParseLiveNoteAnswer_UnusableAnswerNamesTheLineAndTheReason(t *testing.T) {
	raw := "好 => 好\n云 => 云\n在 => 再"
	_, _, err := parseLiveNoteAnswer(raw)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLiveNoteReviseFormat)
	assert.NotContains(t, err.Error(), `first: ""`, "an answer full of arrows must not report an empty sample")
	assert.Contains(t, err.Error(), "好 => 好", "the offending line itself must be quoted")
	assert.Contains(t, err.Error(), "same", "the reason must be stated")
	assert.Contains(t, err.Error(), "3 unusable line(s)")
}

// The clock-less `==>` answer is a distinct failure: the model used the
// speaker-split arrow for plain replacements. Naming that pattern is what tells
// the reader the prompt needs sharpening rather than the model being hopeless.
func TestParseLiveNoteAnswer_HintsAtTheSplitArrowWhenEveryLineUsesIt(t *testing.T) {
	raw := "科劳本奇 ==> ClawBench\n王小明 ==> 王总"
	_, _, err := parseLiveNoteAnswer(raw)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLiveNoteReviseFormat)
	assert.Contains(t, err.Error(), "科劳本奇 ==> ClawBench")
	assert.Contains(t, err.Error(), "speaker-split arrow", "the whole-answer pattern must be named")
}

// The hint is for the whole-answer pattern only: one stray `==>` line among
// prose lines says nothing about what the model was doing, so it must not be
// claimed.
func TestParseLiveNoteAnswer_NoSplitHintWhenOnlyOneLineUsesTheArrow(t *testing.T) {
	raw := "科劳本奇 ==> ClawBench\n这段没有需要修改的地方"
	_, _, err := parseLiveNoteAnswer(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2 unusable line(s)")
	assert.NotContains(t, err.Error(), "speaker-split arrow")
}

// ── Split lines ────────────────────────────────────────────────────────────

// A "==>" line that carries no clock is not a split: the clock is what keeps a
// split from being applied to an unrelated line.
func TestParseLiveNoteSplitLine_WithoutClockIsNotASplit(t *testing.T) {
	split, reason := parseLiveNoteSplitLine("我：你好 ==> 张三")
	assert.NotEmpty(t, reason, "the rejection must say why")
	assert.Contains(t, reason, "[mm:ss]")
	assert.Equal(t, liveNoteSplit{}, split)
}

// A clocked line that has no "==>" arrow is not a split either.
func TestParseLiveNoteSplitLine_WithoutArrowIsNotASplit(t *testing.T) {
	split, reason := parseLiveNoteSplitLine("[00:03] 我：你好")
	assert.NotEmpty(t, reason)
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
