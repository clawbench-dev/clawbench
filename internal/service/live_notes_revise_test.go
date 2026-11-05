package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Pair parsing ────────────────────────────────────────────────────────────

func TestParseLiveNoteAnswer_AcceptsListAndFenceNoise(t *testing.T) {
	raw := "```\n- 科劳本奇 => ClawBench\n* `a c p` => ACP\n1. 王小明 => 王总\n```"
	pairs, splits, err := parseLiveNoteAnswer(raw)
	require.NoError(t, err)
	require.Len(t, pairs, 3)
	assert.Empty(t, splits)
	assert.Equal(t, liveNotePair{From: "科劳本奇", To: "ClawBench"}, pairs[0])
	assert.Equal(t, liveNotePair{From: "a c p", To: "ACP"}, pairs[1], "backticks around a side are quoting, not content")
	assert.Equal(t, liveNotePair{From: "王小明", To: "王总"}, pairs[2])
}

func TestParseLiveNoteAnswer_NoChangesSentinel(t *testing.T) {
	for _, raw := range []string{"NO_CHANGES", " no_changes ", "NO_CHANGES。"} {
		pairs, splits, err := parseLiveNoteAnswer(raw)
		require.NoError(t, err, raw)
		assert.Empty(t, pairs, raw)
		assert.Empty(t, splits, raw)
	}
}

// A model that answers with the transcript it was supposed to correct has not
// followed the contract, and the whole stage's guarantee (every offset and label
// survives) rests on that contract. It must fail, not pass the raw text through
// as if nothing needed fixing.
func TestParseLiveNoteAnswer_RejectsARewrite(t *testing.T) {
	raw := "[00:00] 我：确认 ClawBench 的 ACP 计划\n[00:08] 张三：好"
	_, _, err := parseLiveNoteAnswer(raw)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLiveNoteReviseFormat)
	assert.Contains(t, err.Error(), "[00:00] 我：确认", "the error must quote the offending line")
}

func TestParseLiveNoteAnswer_DropsUnusableRows(t *testing.T) {
	// Same word on both sides, a one-rune original, and an over-long side are
	// all unapplicable; the usable row still lands.
	raw := "好 => 好\n云 => 云\n长 => " + strings.Repeat("很", 80) + "\n科劳本奇 => ClawBench"
	pairs, _, err := parseLiveNoteAnswer(raw)
	require.NoError(t, err)
	require.Len(t, pairs, 1)
	assert.Equal(t, "科劳本奇", pairs[0].From)
}

// A split is the second output shape: `[mm:ss] 原词 ==> 新说话人`. The clock is
// what anchors it to one line, so it is mandatory.
func TestParseLiveNoteAnswer_ReadsSplits(t *testing.T) {
	raw := "[00:12] 那我来补充一下 ==> 李四\n科劳本奇 => ClawBench"
	pairs, splits, err := parseLiveNoteAnswer(raw)
	require.NoError(t, err)
	require.Len(t, pairs, 1)
	require.Len(t, splits, 1)
	assert.Equal(t, liveNoteSplit{Clock: "00:12", From: "那我来补充一下", Speaker: "李四"}, splits[0])
}

// `==>` must never be read as a pair with a stray `=`: the two shapes coexist in
// one answer, and `strings.Cut(line, "=>")` would otherwise match the split.
func TestParseLiveNoteAnswer_SplitIsNotAPair(t *testing.T) {
	pairs, splits, err := parseLiveNoteAnswer("[00:12] 那我来补充一下 ==> 李四")
	require.NoError(t, err)
	assert.Empty(t, pairs, "the `=>` inside `==>` must not produce a replacement pair")
	require.Len(t, splits, 1)
}

// A malformed split is dropped, not fatal: the format error aborts the whole
// meeting, and a badly phrased speaker change says nothing about the rest of the
// answer.
func TestParseLiveNoteAnswer_DropsUnusableSplits(t *testing.T) {
	cases := []string{
		"[00:12] 好 ==> 李四",                               // too short to locate
		"那我来补充一下 ==> 李四",                                 // no clock anchor
		"[00:12] 那我来补充一下 ==> ",                           // no label
		"[00:12] 那我来补充一下 ==> 李四：",                        // a colon would move the boundary
		"[00:12] 那我来补充一下 ==> " + strings.Repeat("李", 30), // not a label
	}
	for _, raw := range cases {
		pairs, splits, err := parseLiveNoteAnswer(raw + "\n科劳本奇 => ClawBench")
		require.NoError(t, err, raw)
		assert.Empty(t, splits, raw)
		assert.Len(t, pairs, 1, raw)
	}
}

// ── Pair application ────────────────────────────────────────────────────────

func TestApplyLiveNotePairs_KeepsTimelineAndSpeakerLabels(t *testing.T) {
	transcript := "[00:00] 张三：科劳本奇上线了\n[01:05] 我：张三说得对"
	pairs := []liveNotePair{
		{From: "科劳本奇", To: "ClawBench"},
		// A pair that happens to match a speaker label must not rewrite it:
		// rule 3 makes the label the authority for a person's name.
		{From: "张三", To: "李四"},
	}
	got := applyLiveNotePairs(transcript, pairs)
	assert.Equal(t, "[00:00] 张三：ClawBench上线了\n[01:05] 我：李四说得对", got,
		"the label keeps its spelling, the body does not")
}

// The whole reason pairs are applied once over the meeting instead of per chunk:
// two chunks can fix the same word differently, and the transcript must end up
// with one spelling.
func TestResolveLiveNotePairs_MajorityWinsAndTiesKeepTheFirst(t *testing.T) {
	resolved := resolveLiveNotePairs([]liveNotePair{
		{From: "科劳本奇", To: "ClawBench"},
		{From: "科劳本奇", To: "ClawBench"},
		{From: "科劳本奇", To: "Claw Bench"},
		{From: "a c p", To: "ACP"},
		{From: "a c p", To: "APC"},
	})
	require.Len(t, resolved, 2)
	byFrom := map[string]string{}
	for _, p := range resolved {
		byFrom[p.From] = p.To
	}
	assert.Equal(t, "ClawBench", byFrom["科劳本奇"], "two votes beat one")
	assert.Equal(t, "ACP", byFrom["a c p"], "a tie keeps the first-seen correction")
}

func TestResolveLiveNotePairs_LongestFirst(t *testing.T) {
	resolved := resolveLiveNotePairs([]liveNotePair{
		{From: "a c", To: "AC"},
		{From: "a c p", To: "ACP"},
	})
	require.Len(t, resolved, 2)
	assert.Equal(t, "a c p", resolved[0].From,
		"the longer phrase must be applied first, or the short one eats it")
}

func TestApplyLiveNotePairs_NoPairsIsANoOp(t *testing.T) {
	transcript := "[00:00] 我：原文"
	assert.Equal(t, transcript, applyLiveNotePairs(transcript, nil))
}

// ── Split application ───────────────────────────────────────────────────────

// The whole point of a split: one channel, two people, two lines — with the
// clock they actually spoke at, not an invented one.
func TestApplyLiveNoteSplits_OpensANewLineAtTheSameClock(t *testing.T) {
	transcript := "[00:12] 张三：这个方案我看行。那我来补充一下，接口还没联调完。"
	got := applyLiveNoteSplits(transcript, []liveNoteSplit{
		{Clock: "00:12", From: "那我来补充一下", Speaker: "李四"},
	})
	assert.Equal(t,
		"[00:12] 张三：这个方案我看行。\n[00:12] 李四：那我来补充一下，接口还没联调完。", got)
}

// A split may only cut the line its clock points at. Otherwise a common phrase
// would re-diarize every line that happens to contain it.
func TestApplyLiveNoteSplits_OnlyTouchesTheAnchoredLine(t *testing.T) {
	transcript := "[00:12] 张三：那我来补充一下\n[00:30] 张三：前半句。那我来补充一下"
	got := applyLiveNoteSplits(transcript, []liveNoteSplit{
		{Clock: "00:30", From: "那我来补充一下", Speaker: "李四"},
	})
	assert.Equal(t, "[00:12] 张三：那我来补充一下\n[00:30] 张三：前半句。\n[00:30] 李四：那我来补充一下", got,
		"the first line keeps its label: the split names one line, not one phrase")
}

// A line with unknown timing still splits — the second half inherits `[--:--]`
// rather than acquiring a timestamp nobody measured.
func TestApplyLiveNoteSplits_KeepsTheUnknownClock(t *testing.T) {
	got := applyLiveNoteSplits("[--:--] 张三：前半句。那我来补充一下后半句", []liveNoteSplit{
		{Clock: "--:--", From: "那我来补充一下", Speaker: "李四"},
	})
	assert.Equal(t, "[--:--] 张三：前半句。\n[--:--] 李四：那我来补充一下后半句", got)
}

// A split whose anchor is missing is dropped: the transcript is the authority,
// and a cut in the wrong place is worse than no cut.
func TestApplyLiveNoteSplits_IgnoresAnUnmatchedAnchor(t *testing.T) {
	transcript := "[00:12] 张三：完全不同的内容"
	got := applyLiveNoteSplits(transcript, []liveNoteSplit{
		{Clock: "00:12", From: "那我来补充一下", Speaker: "李四"},
	})
	assert.Equal(t, transcript, got)
}

// A cut at the very start would emit an empty line ahead of the new speaker.
func TestApplyLiveNoteSplits_RefusesAnEmptyFirstHalf(t *testing.T) {
	transcript := "[00:12] 张三：那我来补充一下"
	got := applyLiveNoteSplits(transcript, []liveNoteSplit{
		{Clock: "00:12", From: "那我来补充一下", Speaker: "李四"},
	})
	assert.Equal(t, transcript, got)
}

// Two identical splits must not open the same turn twice.
func TestApplyLiveNoteSplits_DedupesTheSameCut(t *testing.T) {
	split := liveNoteSplit{Clock: "00:12", From: "那我来补充一下", Speaker: "李四"}
	got := applyLiveNoteSplits("[00:12] 张三：前半句。那我来补充一下后半句",
		[]liveNoteSplit{split, split})
	assert.Equal(t, "[00:12] 张三：前半句。\n[00:12] 李四：那我来补充一下后半句", got)
}

// Splitting a line to the label it already carries would only produce two
// consecutive turns for the same person.
func TestApplyLiveNoteSplits_IgnoresACutToTheSameSpeaker(t *testing.T) {
	transcript := "[00:12] 张三：前半句。那我来补充一下后半句"
	got := applyLiveNoteSplits(transcript, []liveNoteSplit{
		{Clock: "00:12", From: "那我来补充一下", Speaker: "张三"},
	})
	assert.Equal(t, transcript, got)
}

// Splits run before pairs, so a correction still lands in the half that was just
// separated off — and never in the new speaker's label.
func TestApplyLiveNoteEdits_SplitsThenCorrects(t *testing.T) {
	transcript := "[00:12] 张三：前半句。那我来补充一下科劳本奇的方案"
	got := applyLiveNoteEdits(transcript,
		[]liveNotePair{{From: "科劳本奇", To: "ClawBench"}, {From: "李四", To: "王五"}},
		[]liveNoteSplit{{Clock: "00:12", From: "那我来补充一下", Speaker: "李四"}})
	assert.Equal(t,
		"[00:12] 张三：前半句。\n[00:12] 李四：那我来补充一下ClawBench的方案", got,
		"the pair reaches the new line, and the label stays as the split wrote it")
}

func TestSplitLiveNoteLine_KeepsThePrefix(t *testing.T) {
	head, body := splitLiveNoteLine("[00:12] 张三：内容", "：")
	assert.Equal(t, "[00:12] 张三：", head)
	assert.Equal(t, "内容", body)

	// A line without a separator is all body: there is no prefix to protect.
	head, body = splitLiveNoteLine("没有分隔符的一行", "：")
	assert.Equal(t, "", head)
	assert.Equal(t, "没有分隔符的一行", body)
}

// ── End to end ──────────────────────────────────────────────────────────────

// A long meeting is proofread chunk by chunk. The corrections from every chunk
// must reach the transcript, and a word fixed differently in two chunks must come
// out spelled one way.
func TestReviseLiveNotesTranscript_AppliesPairsFromEveryChunk(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		// First chunk says 写法一, second says 写法二: a 1-1 tie that the
		// first-seen rule has to break.
		answer := fmt.Sprintf("科劳本奇 => 写法%d", n)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"` + answer + `"}}]}`))
	}))
	defer srv.Close()
	withAISummary(t, srv.URL)

	// Comfortably more than liveNotesReviseChunkRunes, and the word appears on
	// both sides of the split.
	filler := strings.Repeat("这是一段会议内容。", 400)
	segments := []LiveNoteSegment{
		seg(1, "mic", "我", 1_000, "科劳本奇"+filler),
		seg(2, "viewer:v2", "张三", 200_000, "科劳本奇"+filler),
	}

	got, err := ReviseLiveNotesTranscript(context.Background(), LiveNotesReviseInput{Segments: segments})
	require.NoError(t, err)

	mu.Lock()
	require.Greater(t, calls, 1, "the transcript must have been chunked")
	mu.Unlock()

	assert.Contains(t, got, "写法1", "the first chunk's correction must be applied")
	assert.NotContains(t, got, "写法2", "one word gets one spelling")
	assert.NotContains(t, got, "科劳本奇")
	assert.Contains(t, got, "[00:00] 我：", "the timeline survives untouched")
	assert.Contains(t, got, "[03:19] 张三：", "and so does the second speaker")
}

// A label is a microphone, and a microphone can have a room in front of it. The
// proofreader may open a second turn inside one line; the second half keeps the
// clock it actually spoke at.
func TestReviseLiveNotesTranscript_AppliesASpeakerSplit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"[00:00] 那我来补充一下 ==> 李四"}}]}`))
	}))
	defer srv.Close()
	withAISummary(t, srv.URL)

	segments := []LiveNoteSegment{
		seg(1, "mic", "张三", 1_000, "这个方案我看行。那我来补充一下接口还没联调完"),
		seg(2, "viewer:v2", "李四", 9_000, "好"),
	}
	got, err := ReviseLiveNotesTranscript(context.Background(), LiveNotesReviseInput{Segments: segments})
	require.NoError(t, err)
	assert.Equal(t,
		"[00:00] 张三：这个方案我看行。\n[00:00] 李四：那我来补充一下接口还没联调完\n[00:08] 李四：好", got)
}

// The split is the one place this stage may change the line structure, so the
// prompt has to say that a label is a channel rather than a person, and gate the
// exception instead of inviting a free re-diarization.
func TestReviseLiveNotesTranscript_PromptCoversSharedMicrophones(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"NO_CHANGES"}}]}`))
	}))
	defer srv.Close()
	withAISummary(t, srv.URL)

	_, err := ReviseLiveNotesTranscript(context.Background(), LiveNotesReviseInput{
		Segments: []LiveNoteSegment{seg(1, "mic", "我", 1_000, "好")},
	})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 1)
	prompt := bodies[0]

	assert.Contains(t, prompt, "不保证这一行只有一个人在说话",
		"the label must not be presented as a person")
	// The JSON body escapes ">" as \u003e, so the arrow is asserted in halves.
	assert.Contains(t, prompt, "原词 ==")
	assert.Contains(t, prompt, "新说话人",
		"the second output shape has to be in the prompt or the model cannot emit it")
	assert.Contains(t, prompt, "转写里没有的名字不要编",
		"a split may only use a name the transcript already carries")
	assert.Contains(t, prompt, "共用同一个 [mm:ss]",
		"the two halves must not acquire separate timestamps")
	assert.Contains(t, prompt, "不要拆",
		"the exception needs a stop condition, not just an invitation")
}

// The prompt carries the meeting's own vocabulary, but a project with no
// documents and no files still has to work: the block is simply absent.
func TestReviseLiveNotesTranscript_WorksWithoutAnyContext(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"NO_CHANGES"}}]}`))
	}))
	defer srv.Close()
	withAISummary(t, srv.URL)

	got, err := ReviseLiveNotesTranscript(context.Background(), LiveNotesReviseInput{
		Segments: []LiveNoteSegment{seg(1, "mic", "我", 1_000, "内容")},
	})
	require.NoError(t, err)
	assert.Equal(t, "[00:00] 我：内容", got)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 1)
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	require.NoError(t, json.Unmarshal([]byte(bodies[0]), &req))
	var user string
	for _, m := range req.Messages {
		if m.Role == "user" {
			user += m.Content
		}
	}
	assert.Contains(t, user, "## 待校对转写", "the transcript needs its own heading even with no context")
}
