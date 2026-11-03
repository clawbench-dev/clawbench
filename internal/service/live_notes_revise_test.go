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

func TestParseLiveNotePairs_AcceptsListAndFenceNoise(t *testing.T) {
	raw := "```\n- 科劳本奇 => ClawBench\n* `a c p` => ACP\n1. 王小明 => 王总\n```"
	pairs, err := parseLiveNotePairs(raw)
	require.NoError(t, err)
	require.Len(t, pairs, 3)
	assert.Equal(t, liveNotePair{From: "科劳本奇", To: "ClawBench"}, pairs[0])
	assert.Equal(t, liveNotePair{From: "a c p", To: "ACP"}, pairs[1], "backticks around a side are quoting, not content")
	assert.Equal(t, liveNotePair{From: "王小明", To: "王总"}, pairs[2])
}

func TestParseLiveNotePairs_NoChangesSentinel(t *testing.T) {
	for _, raw := range []string{"NO_CHANGES", " no_changes ", "NO_CHANGES。"} {
		pairs, err := parseLiveNotePairs(raw)
		require.NoError(t, err, raw)
		assert.Empty(t, pairs, raw)
	}
}

// A model that answers with the transcript it was supposed to correct has not
// followed the contract, and the whole stage's guarantee (every offset and label
// survives) rests on that contract. It must fail, not pass the raw text through
// as if nothing needed fixing.
func TestParseLiveNotePairs_RejectsARewrite(t *testing.T) {
	raw := "[00:00] 我：确认 ClawBench 的 ACP 计划\n[00:08] 张三：好"
	_, err := parseLiveNotePairs(raw)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLiveNoteReviseFormat)
	assert.Contains(t, err.Error(), "[00:00] 我：确认", "the error must quote the offending line")
}

func TestParseLiveNotePairs_DropsUnusableRows(t *testing.T) {
	// Same word on both sides, a one-rune original, and an over-long side are
	// all unapplicable; the usable row still lands.
	raw := "好 => 好\n云 => 云\n长 => " + strings.Repeat("很", 80) + "\n科劳本奇 => ClawBench"
	pairs, err := parseLiveNotePairs(raw)
	require.NoError(t, err)
	require.Len(t, pairs, 1)
	assert.Equal(t, "科劳本奇", pairs[0].From)
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
