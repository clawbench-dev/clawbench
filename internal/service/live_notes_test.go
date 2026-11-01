package service

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupTestDBForLiveNotes creates an in-memory database with the two live-note
// tables and the project registry (segments are attributed to a project id).
func setupTestDBForLiveNotes(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)

	_, err = db.Exec(store.ProjectsDDL)
	require.NoError(t, err)
	_, err = db.Exec(LiveNotesDDL)
	require.NoError(t, err)

	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(cleanup)
	return db
}

func seg(id int64, channel, speaker string, at int64, text string) LiveNoteSegment {
	return LiveNoteSegment{ID: id, Channel: channel, Speaker: speaker, StartedAt: at, DurationMS: 8000, Text: text}
}

// The whole point of per-channel recording: the channels finish transcribing in
// a different order than they were spoken, and reading must put them back in
// speaking order with each line attributed to its own channel.
func TestBuildLiveNotesTranscript_ReordersChannelsByTime(t *testing.T) {
	// Real timestamps (epoch millis), not 0: 0 means "unknown" and would be
	// rendered as [--:--] instead of an offset.
	base := int64(1_700_000_000_000)
	segments := []LiveNoteSegment{
		seg(3, "viewer:v2", "观众 2", base+20_000, "我同意"),
		seg(1, "system", "系统声音", base, "今天讨论发布计划"),
		seg(2, "mic", "我", base+10_000, "先确认一下时间"),
	}

	got := BuildLiveNotesTranscript(segments)

	assert.Equal(t,
		"[00:00] 系统声音：今天讨论发布计划\n"+
			"[00:10] 我：先确认一下时间\n"+
			"[00:20] 观众 2：我同意",
		got)
}

// A speaker's voice reaches the publisher's speakers and comes back through the
// system-audio loopback, so the same sentence is transcribed twice. The later
// copy must be dropped or every remote participant appears twice.
func TestDedupeLiveNoteSegments_DropsEchoedCopy(t *testing.T) {
	sorted := SortLiveNoteSegments([]LiveNoteSegment{
		seg(1, "mic", "我", 10_000, "这个方案可行。"),
		seg(2, "system", "系统声音", 12_500, "这个方案可行"),
	})

	got := DedupeLiveNoteSegments(sorted)

	require.Len(t, got, 1)
	assert.Equal(t, "mic", got[0].Channel, "the earliest capture wins")
}

// Two people saying different things at the same moment is normal conversation
// and must survive.
func TestDedupeLiveNoteSegments_KeepsSimultaneousDifferentSpeech(t *testing.T) {
	sorted := SortLiveNoteSegments([]LiveNoteSegment{
		seg(1, "mic", "我", 10_000, "先看后端"),
		seg(2, "system", "系统声音", 10_500, "前端也需要改"),
	})

	assert.Len(t, DedupeLiveNoteSegments(sorted), 2)
}

// Identical words far apart are two separate utterances, not an echo.
func TestDedupeLiveNoteSegments_KeepsRepeatedPhraseLater(t *testing.T) {
	sorted := SortLiveNoteSegments([]LiveNoteSegment{
		seg(1, "mic", "我", 10_000, "好"),
		seg(2, "system", "系统声音", 40_000, "好"),
	})

	assert.Len(t, DedupeLiveNoteSegments(sorted), 2)
}

func TestSortLiveNoteSegments_DropsEmptyTextAndBreaksTiesByID(t *testing.T) {
	got := SortLiveNoteSegments([]LiveNoteSegment{
		seg(2, "mic", "我", 5_000, "b"),
		seg(3, "mic", "我", 5_000, "c"),
		seg(1, "mic", "我", 5_000, "a"),
		seg(4, "mic", "我", 6_000, "   "),
	})

	require.Len(t, got, 3)
	assert.Equal(t, []int64{1, 2, 3}, []int64{got[0].ID, got[1].ID, got[2].ID})
}

func TestLiveNoteSegments_IncrementalCursorIsById(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()

	pid, err := store.ProjectIDForPath("/proj/a")
	require.NoError(t, err)

	id1, err := AppendLiveNoteSegment(context.Background(), "tok", pid, seg(0, "system", "系统声音", 30_000, "第一句"))
	require.NoError(t, err)
	// A late transcription: larger id, earlier speaking time. The cursor must
	// follow the id, otherwise this row would be returned twice (or missed).
	id2, err := AppendLiveNoteSegment(context.Background(), "tok", pid, seg(0, "mic", "我", 10_000, "第二句"))
	require.NoError(t, err)
	require.Greater(t, id2, id1)

	first, err := ListLiveNoteSegments("tok", 0)
	require.NoError(t, err)
	require.Len(t, first, 2)
	assert.Equal(t, "第一句", first[0].Text)

	rest, err := ListLiveNoteSegments("tok", id1)
	require.NoError(t, err)
	require.Len(t, rest, 1)
	assert.Equal(t, "第二句", rest[0].Text)

	none, err := ListLiveNoteSegments("tok", id2)
	require.NoError(t, err)
	assert.Empty(t, none)

	// The transcript is rebuilt in speaking order regardless of insertion order.
	assert.Equal(
		t,
		"[00:00] 我：第二句\n[00:20] 系统声音：第一句",
		BuildLiveNotesTranscript(first),
	)
}

func TestLiveNote_ProjectScopedLookupAndMinutesUpsert(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()

	pid, err := store.ProjectIDForPath("/proj/a")
	require.NoError(t, err)
	_, err = AppendLiveNoteSegment(context.Background(), "tok", pid, seg(0, "mic", "我", 1_000, "内容"))
	require.NoError(t, err)

	gotPID, ok, err := LiveNoteProjectID("tok")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, pid, gotPID)

	require.NoError(t, SetLiveNoteMinutes("tok", pid, "标题", LiveNotesStatusGenerating, "", "", ""))
	note, ok, err := GetLiveNote("tok")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, LiveNotesStatusGenerating, note.Status)
	assert.Equal(t, "标题", note.Title)

	// Ready overwrites the status and stores the markdown.
	require.NoError(t, SetLiveNoteMinutes("tok", pid, "", LiveNotesStatusReady, "## 概要", "", "/p/notes.md"))
	note, _, err = GetLiveNote("tok")
	require.NoError(t, err)
	assert.Equal(t, LiveNotesStatusReady, note.Status)
	assert.Equal(t, "## 概要", note.Minutes)
	assert.Equal(t, "标题", note.Title, "an empty title must not clear the stored one")
	assert.Equal(t, "/p/notes.md", note.FilePath)
}

func TestSplitRunes_PrefersLineBoundary(t *testing.T) {
	text := "aaa\nbbb\nccc"
	chunks := splitRunes(text, 8)
	require.Len(t, chunks, 2)
	assert.Equal(t, "aaa\nbbb\n", chunks[0])
	assert.Equal(t, "ccc", chunks[1])
	// Nothing may be lost.
	assert.Equal(t, text, chunks[0]+chunks[1])
}

func TestSplitRunes_ShortTextIsOneChunk(t *testing.T) {
	assert.Equal(t, []string{"abc"}, splitRunes("abc", 100))
}

func TestWriteLiveNotesMinutesFile_LinksSiblingsInsteadOfInlining(t *testing.T) {
	paths := LiveNotesPathsIn(filepath.Join(t.TempDir(), "2026-10-30-1430-季度规划-abc123"))
	started := time.Date(2026, 10, 30, 14, 30, 0, 0, time.Local)

	written, err := WriteLiveNotesMinutesFile(paths, "季度规划", started, "## 概要\n\n发布计划确认。", 7)
	require.NoError(t, err)
	assert.Equal(t, paths.Minutes, written)

	body, err := os.ReadFile(written)
	require.NoError(t, err)
	text := string(body)
	assert.Contains(t, text, "# 季度规划 · 2026-10-30 14:30")
	assert.Contains(t, text, "## 概要")
	// The transcript has its own file now. A copy here would go stale the moment
	// somebody edits it, and it is the transcript that must never be lost.
	assert.NotContains(t, text, "## 附：完整转写")
	assert.Contains(t, text, "[transcript.srt](transcript.srt)")
	assert.Contains(t, text, "[transcript-revised.md](transcript-revised.md)")
	assert.Contains(t, text, "`audio/`（7 段）")
}

func TestWriteLiveNotesMinutesFile_RequiresDirAndMinutes(t *testing.T) {
	_, err := WriteLiveNotesMinutesFile(LiveNotesPathsIn(""), "t", time.Now(), "m", 0)
	assert.Error(t, err)

	_, err = WriteLiveNotesMinutesFile(LiveNotesPathsIn(t.TempDir()), "t", time.Now(), "   ", 0)
	assert.ErrorIs(t, err, ErrLiveNotesEmpty)
}

func TestWriteLiveNotesTranscriptFile_KeepsHeadingNoteAndLines(t *testing.T) {
	paths := LiveNotesPathsIn(t.TempDir())
	started := time.Date(2026, 10, 30, 14, 30, 0, 0, time.Local)

	path, err := WriteLiveNotesRevisedTranscript(
		paths, "季度规划", started, "[00:00] 我：确认 ClawBench 的发布计划",
	)
	require.NoError(t, err)
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(body), "# 修订版转写 · 季度规划 · 2026-10-30 14:30")
	assert.Contains(t, string(body), "> 已结合项目上下文修正听写错误与专有名词")
	assert.Contains(t, string(body), "[00:00] 我：确认 ClawBench 的发布计划")

	base := started.UnixMilli()
	raw, err := WriteLiveNotesRawTranscript(paths, []LiveNoteSegment{
		{Speaker: "我", StartedAt: base, DurationMS: 1500, Text: "原样"},
	})
	require.NoError(t, err)
	rawBody, err := os.ReadFile(raw)
	require.NoError(t, err)
	assert.Equal(t, paths.Transcript, raw)
	assert.True(t, strings.HasSuffix(raw, "transcript.srt"), "raw transcript is SRT, got %q", raw)
	// A subtitle track starts with a cue index, not a Markdown heading.
	assert.True(t, strings.HasPrefix(string(rawBody), "1\n"), "want an SRT cue, got %q", string(rawBody))
	assert.NotContains(t, string(rawBody), "# 转写")
	assert.Contains(t, string(rawBody), "00:00:00,000 --> 00:00:01,500")
	assert.Contains(t, string(rawBody), "我：原样")

	_, err = WriteLiveNotesRawTranscript(paths, nil)
	assert.ErrorIs(t, err, ErrLiveNotesEmpty)

	_, err = WriteLiveNotesRawTranscript(LiveNotesPathsIn(""), []LiveNoteSegment{
		{Speaker: "我", StartedAt: base, DurationMS: 1000, Text: "原样"},
	})
	assert.Error(t, err)
}

func TestSaveLiveNoteAudio_NamesByStartTimeAndChannel(t *testing.T) {
	paths := LiveNotesPathsIn(t.TempDir())

	first, err := SaveLiveNoteAudio(paths, "viewer:2", 1_700_000_000_123, []byte("a"))
	require.NoError(t, err)
	assert.Equal(t, "1700000000123-viewer-2.webm", filepath.Base(first))

	second, err := SaveLiveNoteAudio(paths, "mic", 1_700_000_001_000, []byte("b"))
	require.NoError(t, err)
	assert.Equal(t, "1700000001000-mic.webm", filepath.Base(second))
	// Sorting the folder by name must be sorting by time: that is what makes the
	// recording usable without an index file.
	assert.Less(t, filepath.Base(first), filepath.Base(second))

	_, err = SaveLiveNoteAudio(paths, "mic", 1, nil)
	assert.Error(t, err, "an empty upload must not create a zero-byte file")
}

// ── Edge cases: persistence ─────────────────────────────────────────────────

func TestAppendLiveNoteSegment_RejectsEmptyToken(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()

	_, err := AppendLiveNoteSegment(context.Background(), "", 0, seg(0, "mic", "我", 1, "x"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty token")
}

func TestGetLiveNote_MissingShareIsNotAnError(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()

	note, ok, err := GetLiveNote("nobody")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, note.Status)

	pid, exists, err := LiveNoteProjectID("nobody")
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Zero(t, pid)
}

// ── Edge cases: timeline assembly ───────────────────────────────────────────

func TestBuildLiveNotesTranscript_EmptyInput(t *testing.T) {
	assert.Empty(t, BuildLiveNotesTranscript(nil))
	assert.Empty(t, BuildLiveNotesTranscript([]LiveNoteSegment{seg(1, "mic", "我", 1, "   ")}))
}

// A segment with no speaker label must fall back to its channel so the
// summariser still knows who was talking.
func TestBuildLiveNotesTranscript_FallsBackToChannel(t *testing.T) {
	got := BuildLiveNotesTranscript([]LiveNoteSegment{
		seg(1, "viewer:v1", "  ", 5_000, "  多个   空格  "),
	})
	assert.Equal(t, "[00:00] viewer:v1：多个 空格", got)
}

func TestNormalizeNoteText_FoldsCaseWhitespaceAndPunctuation(t *testing.T) {
	assert.Equal(t, "helloworld", normalizeNoteText("  Hello, World!  "))
	assert.Equal(t, "第一句", normalizeNoteText("第一句。"))
	assert.Equal(t, "abc123", normalizeNoteText("ABC\t123"))
	// Everything that is not a letter/digit/CJK ideograph disappears.
	assert.Empty(t, normalizeNoteText("。，！？ —— "))
}

func TestDedupeLiveNoteSegments_SkipsPunctuationOnlyLines(t *testing.T) {
	// normalizeNoteText folds these to "", so they cannot be compared and are
	// dropped instead of being emitted as blank timeline lines.
	got := DedupeLiveNoteSegments([]LiveNoteSegment{
		seg(1, "mic", "我", 1_000, "……"),
		seg(2, "mic", "我", 2_000, "有内容"),
	})
	require.Len(t, got, 1)
	assert.Equal(t, "有内容", got[0].Text)
}

// The recent-window cache is bounded: older entries are forgotten so a long
// meeting does not grow it without limit, but the newest eight still dedupe.
func TestDedupeLiveNoteSegments_WindowIsBounded(t *testing.T) {
	segments := make([]LiveNoteSegment, 0, 10)
	for i := range 10 {
		segments = append(segments, seg(int64(i+1), "mic", "我", int64(i)*10_000, "第"+string(rune('a'+i))+"句"))
	}
	got := DedupeLiveNoteSegments(segments)
	require.Len(t, got, 10, "ten distinct utterances must all survive")

	// A repeat of the very first utterance is outside the window by now and is
	// therefore treated as a new utterance, not an echo.
	segments = append(segments, seg(11, "system", "系统声音", 95_000, "第a句"))
	assert.Len(t, DedupeLiveNoteSegments(segments), 11)
}

func TestAbsInt64(t *testing.T) {
	assert.Equal(t, int64(5), absInt64(-5))
	assert.Equal(t, int64(5), absInt64(5))
	assert.Zero(t, absInt64(0))
}

// ── Minutes generation ─────────────────────────────────────────────────────

// aiSummaryServer stands in for an OpenAI-compatible endpoint and returns one
// canned reply per call (the last one repeats).
func aiSummaryServer(t *testing.T, replies ...string) *httptest.Server {
	t.Helper()
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		idx := calls
		if idx >= len(replies) {
			idx = len(replies) - 1
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + strconvQuote(replies[idx]) + `}}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// strconvQuote JSON-quotes a string without pulling encoding/json into the test.
func strconvQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func withAISummary(t *testing.T, baseURL string) {
	t.Helper()
	prev := model.ConfigInstance
	t.Cleanup(func() { model.ConfigInstance = prev })
	model.ConfigInstance = model.Config{}
	model.ConfigInstance.AISummary.API.BaseURL = baseURL
	model.ConfigInstance.AISummary.Format = "openai"
}

func TestGenerateLiveNotesMinutes_EmptyTranscript(t *testing.T) {
	_, err := GenerateLiveNotesMinutes(context.Background(), "标题", "")
	assert.ErrorIs(t, err, ErrLiveNotesEmpty)
}

func TestGenerateLiveNotesMinutes_NoSummaryModel(t *testing.T) {
	withAISummary(t, "")

	_, err := GenerateLiveNotesMinutes(context.Background(), "标题", "[00:00] 我：内容")
	assert.ErrorIs(t, err, ErrSummaryModelNotConfigured)
}

func TestGenerateLiveNotesMinutes_SingleChunk(t *testing.T) {
	srv := aiSummaryServer(t, "## 概要\n\n发布计划已确认。")
	withAISummary(t, srv.URL)

	got, err := GenerateLiveNotesMinutes(context.Background(), "季度会", "[00:00] 我：先确认时间")
	require.NoError(t, err)
	assert.Equal(t, "## 概要\n\n发布计划已确认。", got)
}

// A meeting longer than the chunk budget is map-reduced: every chunk is
// condensed first, and only the condensed text reaches the final pass.
func TestGenerateLiveNotesMinutes_LongMeetingIsMapReduced(t *testing.T) {
	segments := make([]LiveNoteSegment, 0, 3)
	long := strings.Repeat("很长的发言内容。", 400) // ~3200 runes per line
	for i := range 3 {
		segments = append(segments, seg(int64(i+1), "mic", "我", int64(i)*60_000, long))
	}
	require.Greater(t, len([]rune(BuildLiveNotesTranscript(segments))), liveNotesChunkRunes,
		"fixture must exceed one chunk or the map-reduce path is not exercised")

	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		n := len(bodies)
		mu.Unlock()
		reply := "要点" + strconv.Itoa(n)
		if n >= 3 {
			reply = "## 概要\n\n合并后的纪要"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + strconvQuote(reply) + `}}]}`))
	}))
	defer srv.Close()
	withAISummary(t, srv.URL)

	got, err := GenerateLiveNotesMinutes(context.Background(), "", BuildLiveNotesTranscript(segments))
	require.NoError(t, err)
	assert.Equal(t, "## 概要\n\n合并后的纪要", got)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 3, "two chunk passes plus one final pass")
	assert.Contains(t, bodies[0], "第 1/2 段", "chunk prompt carries its position")
	assert.Contains(t, bodies[1], "第 2/2 段")
	assert.Contains(t, bodies[2], "要点1", "the final pass sees the condensed chunks")
	assert.Contains(t, bodies[2], "要点2")
}

func TestGenerateLiveNotesMinutes_ProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	withAISummary(t, srv.URL)

	_, err := GenerateLiveNotesMinutes(context.Background(), "", "[00:00] 我：内容")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "live notes: minutes")
}

// ── Context-aware proofreading ─────────────────────────────────────────────

// The proofreader is fed the project's own documents: that is where the correct
// spelling of product names lives, and it is the only reason this stage exists.
func TestReviseLiveNotesTranscript_UsesProjectContextAndKeepsTheTimeline(t *testing.T) {
	project := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(project, "AGENTS.md"), []byte("ClawBench 是产品名；ACP 是协议名。"), 0o644,
	))

	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"[00:00] 我：确认 ClawBench 的 ACP 计划"}}]}`))
	}))
	defer srv.Close()
	withAISummary(t, srv.URL)

	got, err := ReviseLiveNotesTranscript(context.Background(), project, "周会", []LiveNoteSegment{
		seg(1, "mic", "我", 1_000, "确认科劳本奇的 a c p 计划"),
	})
	require.NoError(t, err)
	assert.Equal(t, "[00:00] 我：确认 ClawBench 的 ACP 计划", got)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 1)
	assert.Contains(t, bodies[0], "ClawBench 是产品名", "project context must reach the model")
	assert.Contains(t, bodies[0], "确认科劳本奇的 a c p 计划", "and so must the raw line")
}

func TestReviseLiveNotesTranscript_EmptyTranscript(t *testing.T) {
	_, err := ReviseLiveNotesTranscript(context.Background(), t.TempDir(), "", nil)
	assert.ErrorIs(t, err, ErrLiveNotesEmpty)
}

func TestReviseLiveNotesTranscript_NoSummaryModel(t *testing.T) {
	withAISummary(t, "")
	_, err := ReviseLiveNotesTranscript(context.Background(), t.TempDir(), "", []LiveNoteSegment{
		seg(1, "mic", "我", 1_000, "内容"),
	})
	assert.ErrorIs(t, err, ErrSummaryModelNotConfigured)
}

func TestSetLiveNoteRevise_RoundTrips(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()

	require.NoError(t, SetLiveNoteRevise("tok", 3, LiveNotesStatusReady, "修订后的文字", ""))
	note, ok, err := GetLiveNote("tok")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, LiveNotesStatusReady, note.ReviseStatus)
	assert.Equal(t, "修订后的文字", note.Revised)
	assert.Equal(t, "", note.ReviseError)

	require.NoError(t, SetLiveNoteRevise("tok", 3, LiveNotesStatusFailed, "", "model down"))
	note, _, err = GetLiveNote("tok")
	require.NoError(t, err)
	assert.Equal(t, LiveNotesStatusFailed, note.ReviseStatus)
	assert.Equal(t, "model down", note.ReviseError)
}

// ── Markdown output edge cases ─────────────────────────────────────────────

func TestWriteLiveNotesMinutesFile_UntitledMeetingUsesDefaultHeading(t *testing.T) {
	paths := LiveNotesPathsIn(t.TempDir())
	started := time.Date(2026, 10, 30, 8, 0, 0, 0, time.Local)

	written, err := WriteLiveNotesMinutesFile(paths, "   ", started, "## 概要", 0)
	require.NoError(t, err)

	body, err := os.ReadFile(written)
	require.NoError(t, err)
	assert.Contains(t, string(body), "# 会议纪要 · 2026-10-30 08:00")
	assert.NotContains(t, string(body), "`audio/`", "no segments means no audio line")
}

// Two meetings can start in the same minute under the same title; sharing a
// folder would interleave their audio and overwrite each other's minutes.
func TestLiveNotesMeetingDirName_SanitizesAndDisambiguates(t *testing.T) {
	started := time.Date(2026, 10, 30, 9, 5, 0, 0, time.Local)

	name := liveNotesMeetingDirName(started, "a/b:c*d?e", "abcdef123456")
	assert.Equal(t, "2026-10-30-0905-abcde-abcdef", name)
	assert.NotContains(t, name, "/")
	assert.NotContains(t, name, ":")

	assert.NotEqual(t, name, liveNotesMeetingDirName(started, "a/b:c*d?e", "999999"))
	assert.Equal(t, "2026-10-30-0905", liveNotesMeetingDirName(started, "", ""))
}

func TestLiveNotesFileSlug_DropsUnsafeRunesAndCapsLength(t *testing.T) {
	assert.Equal(t, "abcdefghij", liveNotesFileSlug("a/b\\c:d*e?f\"g<h>i|j"))
	assert.Equal(t, "a b", liveNotesFileSlug("a\nb"), "line breaks become spaces")
	assert.Equal(t, "x", liveNotesFileSlug("  x...  "), "trailing dots and spaces are trimmed")
	assert.Empty(t, liveNotesFileSlug("   "))
	assert.Empty(t, liveNotesFileSlug("///"))

	long := strings.Repeat("会", liveNotesNameMaxRunes+20)
	assert.Equal(t, liveNotesNameMaxRunes, len([]rune(liveNotesFileSlug(long))))
}

// A meeting held in Japanese/Korean/Russian must survive dedupe: those scripts
// are neither Latin nor CJK ideographs, and dropping them would silently delete
// every line and leave the minutes empty.
func TestDedupeLiveNoteSegments_KeepsNonLatinScripts(t *testing.T) {
	sorted := SortLiveNoteSegments([]LiveNoteSegment{
		seg(1, "mic", "我", 10_000, "そうですね"),
		seg(2, "viewer:v1", "A", 12_000, "네 알겠습니다"),
		seg(3, "viewer:v2", "B", 14_000, "да, согласен"),
		seg(4, "system", "C", 16_000, "sí, de acuerdo"),
	})

	got := DedupeLiveNoteSegments(sorted)
	require.Len(t, got, 4, "every script must survive: %v", got)
	assert.NotEmpty(t, BuildLiveNotesTranscript(sorted))
}

// An echo in a non-Latin script is still an echo.
func TestDedupeLiveNoteSegments_DropsNonLatinEcho(t *testing.T) {
	sorted := SortLiveNoteSegments([]LiveNoteSegment{
		seg(1, "mic", "我", 10_000, "そうですね"),
		seg(2, "system", "系统声音", 12_000, "そうですね！"),
	})
	assert.Len(t, DedupeLiveNoteSegments(sorted), 1)
}

func TestNormalizeNoteText_KeepsEveryScript(t *testing.T) {
	assert.Equal(t, "そうですね", normalizeNoteText("そうですね。"))
	assert.Equal(t, "네알겠습니다", normalizeNoteText("네, 알겠습니다"))
	assert.Equal(t, "дасогласен", normalizeNoteText("да, согласен"))
	assert.Equal(t, "café", normalizeNoteText("Café"))
}

// A segment with no timestamp must not become the timeline origin: it is
// rendered as [--:--] and the offsets still start from the earliest real one.
func TestBuildLiveNotesTranscript_UnknownTimestampDoesNotShiftTheTimeline(t *testing.T) {
	base := int64(1_700_000_000_000)
	got := BuildLiveNotesTranscript([]LiveNoteSegment{
		seg(1, "mic", "我", 0, "时间未知的一句"),
		seg(2, "mic", "我", base, "第一句"),
		seg(3, "system", "系统", base+65_000, "第二句"),
	})

	assert.Equal(t,
		"[--:--] 我：时间未知的一句\n"+
			"[00:00] 我：第一句\n"+
			"[01:05] 系统：第二句",
		got)
}

// Appending a segment is activity: retention must count from the LAST segment,
// not from the meeting's start.
func TestAppendLiveNoteSegment_RefreshesUpdatedAt(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()

	pid, err := store.ProjectIDForPath("/proj/a")
	require.NoError(t, err)
	_, err = AppendLiveNoteSegment(context.Background(), "tok", pid, seg(0, "mic", "我", 1, "第一句"))
	require.NoError(t, err)

	// Backdate the row as if the meeting had started an hour ago.
	_, err = store.WriteExec(`UPDATE live_notes SET updated_at = '2000-01-01 00:00:00' WHERE token = ?`, "tok")
	require.NoError(t, err)

	_, err = AppendLiveNoteSegment(context.Background(), "tok", pid, seg(0, "mic", "我", 2, "第二句"))
	require.NoError(t, err)

	var updated string
	require.NoError(t, db.QueryRow(`SELECT updated_at FROM live_notes WHERE token = ?`, "tok").Scan(&updated))
	assert.NotEqual(t, "2000-01-01 00:00:00", updated, "a new segment must refresh updated_at")
}

// ── History ─────────────────────────────────────────────────────────────────

// writeMeetingFolder lays out one meeting folder the way the writer does, so
// the history reader is tested against the real layout rather than a guess.
func writeMeetingFolder(t *testing.T, project, name string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(project, filepath.FromSlash(LiveNotesDir), name)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "audio"), 0o755))
	for file, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644))
	}
	return dir
}

func TestReadLiveNoteArtifact_ReturnsEachKind(t *testing.T) {
	project := t.TempDir()
	writeMeetingFolder(t, project, "2026-10-31-1732-周会-a1b2c3", map[string]string{
		"minutes.md":            "MINUTES",
		"transcript.md":         "RAW",
		"transcript-revised.md": "REVISED",
	})

	for kind, want := range map[string]string{"minutes": "MINUTES", "transcript": "RAW", "revised": "REVISED"} {
		got, err := ReadLiveNoteArtifact(project, "2026-10-31-1732-周会-a1b2c3", kind)
		require.NoError(t, err, kind)
		assert.Equal(t, want, got)
	}
}

func TestReadLiveNoteArtifact_RejectsTraversalAndUnknownKinds(t *testing.T) {
	project := t.TempDir()
	writeMeetingFolder(t, project, "2026-10-31-1732-周会-a1b2c3", map[string]string{"minutes.md": "MINUTES"})

	for _, dir := range []string{"", ".", "..", "../etc", "2026-10-31-1732-周会-a1b2c3/../..", "/etc"} {
		_, err := ReadLiveNoteArtifact(project, dir, "minutes")
		assert.ErrorIs(t, err, ErrLiveNoteSessionNotFound, "dir=%q must not resolve", dir)
	}
	_, err := ReadLiveNoteArtifact(project, "2026-10-31-1732-周会-a1b2c3", "audio")
	assert.ErrorIs(t, err, ErrLiveNoteArtifactKind)
	_, err = ReadLiveNoteArtifact(project, "2026-10-31-1732-周会-a1b2c3", "transcript")
	assert.ErrorIs(t, err, ErrLiveNoteSessionNotFound, "the file was never written")
}

func TestReadLiveNoteArtifact_RejectsOversizedFile(t *testing.T) {
	project := t.TempDir()
	dir := writeMeetingFolder(t, project, "2026-10-31-1732-周会-a1b2c3", nil)
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "minutes.md"),
		bytes.Repeat([]byte("x"), liveNoteArtifactMaxBytes+1),
		0o644,
	))

	_, err := ReadLiveNoteArtifact(project, "2026-10-31-1732-周会-a1b2c3", "minutes")
	assert.ErrorIs(t, err, ErrLiveNoteArtifactTooLarge)
}

// ── SRT transcript ──────────────────────────────────────────────────────────

func TestBuildLiveNotesSRT(t *testing.T) {
	base := time.Date(2026, 10, 30, 14, 30, 0, 0, time.Local).UnixMilli()
	got := BuildLiveNotesSRT([]LiveNoteSegment{
		{Speaker: "张三", StartedAt: base, DurationMS: 1500, Text: "第一句"},
		// No duration: SRT still needs an end, so a floor applies.
		{Speaker: "李四", StartedAt: base + 2000, DurationMS: 0, Text: "第二句  有空格"},
		// No speaker: fall back to the channel label.
		{Speaker: "", Channel: "mic", StartedAt: base + 5000, DurationMS: 1000, Text: "第三句"},
		// Blank text must not produce a cue.
		{Speaker: "张三", StartedAt: base + 6000, DurationMS: 1000, Text: "   "},
	})

	assert.Contains(t, got, "1\n00:00:00,000 --> 00:00:01,500\n张三：第一句\n")
	assert.Contains(t, got, "2\n00:00:02,000 --> 00:00:04,000\n李四：第二句 有空格\n")
	assert.Contains(t, got, "3\n00:00:05,000 --> 00:00:06,000\nmic：第三句\n")
	assert.NotContains(t, got, "4\n", "a blank utterance produces no cue")
}

func TestBuildLiveNotesSRT_UntimedSegmentsChainForward(t *testing.T) {
	got := BuildLiveNotesSRT([]LiveNoteSegment{
		{Speaker: "我", Text: "没有时间戳"},
		{Speaker: "我", Text: "也没有"},
	})
	// Unknown timing chains from the previous cue instead of inventing an
	// epoch offset (which would show up as thousands of hours).
	assert.Contains(t, got, "1\n00:00:00,000 --> 00:00:02,000\n我：没有时间戳\n")
	assert.Contains(t, got, "2\n00:00:02,000 --> 00:00:04,000\n我：也没有\n")
}

func TestBuildLiveNotesSRT_Empty(t *testing.T) {
	assert.Empty(t, BuildLiveNotesSRT(nil))
	assert.Empty(t, BuildLiveNotesSRT([]LiveNoteSegment{{Speaker: "我", Text: "  "}}))
}

func TestReadLiveNoteArtifactWithName_PrefersSRT(t *testing.T) {
	project := t.TempDir()
	writeMeetingFolder(t, project, "2026-10-31-1732-周会-a1b2c3", map[string]string{
		"transcript.srt": "1\n00:00:00,000 --> 00:00:01,000\n我：新\n",
		"transcript.md":  "OLD MARKDOWN",
	})

	body, file, err := ReadLiveNoteArtifactWithName(project, "2026-10-31-1732-周会-a1b2c3", "transcript")
	require.NoError(t, err)
	assert.Equal(t, "transcript.srt", file)
	assert.Contains(t, body, "00:00:00,000 --> 00:00:01,000")
}

func TestReadLiveNoteArtifactWithName_FallsBackToMarkdown(t *testing.T) {
	project := t.TempDir()
	writeMeetingFolder(t, project, "2026-10-31-1732-周会-a1b2c3", map[string]string{
		"transcript.md": "# 转写 · 旧会议\n",
	})

	body, file, err := ReadLiveNoteArtifactWithName(project, "2026-10-31-1732-周会-a1b2c3", "transcript")
	require.NoError(t, err)
	assert.Equal(t, "transcript.md", file, "meetings recorded before SRT keep their file")
	assert.Contains(t, body, "旧会议")
}

// ── Meeting records ─────────────────────────────────────────────────────────

func TestStartLiveNote_IsIdempotentAndKeepsTheOriginalStart(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()
	pid, err := store.ProjectIDForPath("/proj/a")
	require.NoError(t, err)

	require.NoError(t, StartLiveNote("tok", pid, "周会", 1_700_000_000_000))
	require.NoError(t, StartLiveNote("tok", pid, "改名了", 1_700_000_999_000))

	note, exists, err := GetLiveNote("tok")
	require.NoError(t, err)
	require.True(t, exists)
	assert.Equal(t, int64(1_700_000_000_000), note.StartedAt, "a reconnect must not move the start")
	assert.Equal(t, "周会", note.Title, "the original title wins over the retry")
	assert.Zero(t, note.EndedAt)
}

func TestEndLiveNote_StampsOnce(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()
	pid, err := store.ProjectIDForPath("/proj/a")
	require.NoError(t, err)
	require.NoError(t, StartLiveNote("tok", pid, "周会", 1_700_000_000_000))

	require.NoError(t, EndLiveNote("tok", 1_700_000_060_000))
	require.NoError(t, EndLiveNote("tok", 1_700_000_999_000))

	note, _, err := GetLiveNote("tok")
	require.NoError(t, err)
	assert.Equal(t, int64(1_700_000_060_000), note.EndedAt, "a second close must not move the finish line")
}

func TestCloseStaleLiveNotes_OnlyClosesOldOpenRecords(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()
	pid, err := store.ProjectIDForPath("/proj/a")
	require.NoError(t, err)
	require.NoError(t, StartLiveNote("old", pid, "旧会", 1_700_000_000_000))
	require.NoError(t, StartLiveNote("fresh", pid, "新会", 1_700_000_000_000))
	require.NoError(t, StartLiveNote("closed", pid, "已结束", 1_700_000_000_000))
	require.NoError(t, EndLiveNote("closed", 1_700_000_001_000))
	_, err = store.WriteExec(`UPDATE live_notes SET updated_at = '2000-01-01 00:00:00' WHERE token IN ('old','closed')`)
	require.NoError(t, err)

	closed, err := CloseStaleLiveNotes(time.Now().Add(-12 * time.Hour).UnixMilli())
	require.NoError(t, err)
	assert.Equal(t, 1, closed)

	old, _, err := GetLiveNote("old")
	require.NoError(t, err)
	assert.NotZero(t, old.EndedAt)
	fresh, _, err := GetLiveNote("fresh")
	require.NoError(t, err)
	assert.Zero(t, fresh.EndedAt)
}

func TestEnsureMeetingDir_IsStableAndKeepsAudioOutOfGit(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()
	pid, err := store.ProjectIDForPath("/proj/a")
	require.NoError(t, err)

	project := t.TempDir()
	started := time.Date(2026, 10, 30, 14, 30, 0, 0, time.Local).UnixMilli()
	require.NoError(t, StartLiveNote("abcdef123456", pid, "季度/规划", started))

	paths, err := EnsureMeetingDir("abcdef123456", pid, project, "季度/规划", started)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(project, ".clawbench", "notes", "2026-10-30-1430-季度规划-abcdef"), paths.Dir)

	info, err := os.Stat(paths.Audio)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	ignore, err := os.ReadFile(filepath.Join(paths.Audio, ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, "*\n", string(ignore), "binary audio must not be committed by default")

	again, err := EnsureMeetingDir("abcdef123456", pid, project, "改名了", started)
	require.NoError(t, err)
	assert.Equal(t, paths.Dir, again.Dir, "the recorded folder wins over the new title")
}

func TestEnsureMeetingDir_RequiresProjectPath(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()
	_, err := EnsureMeetingDir("tok", 0, "  ", "t", 1)
	assert.Error(t, err)
}

func TestListMeetingRecords_NewestFirstWithArtifacts(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()
	pid, err := store.ProjectIDForPath("/proj/a")
	require.NoError(t, err)
	project := t.TempDir()

	require.NoError(t, StartLiveNote("older", pid, "晨会", 1_700_000_000_000))
	require.NoError(t, StartLiveNote("newer", pid, "周会", 1_700_000_500_000))
	paths, err := EnsureMeetingDir("newer", pid, project, "周会", 1_700_000_500_000)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(paths.Minutes, []byte("MINUTES"), 0o644))
	require.NoError(t, os.WriteFile(paths.Revised, []byte("REVISED"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(paths.Audio, "001-mic.webm"), []byte("x"), 0o644))
	_, err = AppendLiveNoteChat("newer", pid, LiveNoteChatMessage{ClientID: "c1", Speaker: "小明", Text: "你好"})
	require.NoError(t, err)

	records, err := ListMeetingRecords(project, pid, 200)
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, "newer", records[0].Token)
	assert.Equal(t, "周会", records[0].Title)
	assert.True(t, records[0].HasMinutes)
	assert.True(t, records[0].HasRevised)
	assert.False(t, records[0].HasTranscript)
	assert.Equal(t, 1, records[0].AudioFiles)
	assert.Equal(t, 1, records[0].ChatCount)
	assert.NotEmpty(t, records[0].DirPath)

	assert.Equal(t, "older", records[1].Token)
	assert.Empty(t, records[1].Dir, "a meeting with no artifacts has no folder")
	assert.False(t, records[1].HasMinutes)
}

func TestDeleteMeetingRecord_RemovesFolderRowsAndChat(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()
	pid, err := store.ProjectIDForPath("/proj/a")
	require.NoError(t, err)
	project := t.TempDir()

	require.NoError(t, StartLiveNote("tok", pid, "周会", 1_700_000_000_000))
	paths, err := EnsureMeetingDir("tok", pid, project, "周会", 1_700_000_000_000)
	require.NoError(t, err)
	_, err = AppendLiveNoteSegment(context.Background(), "tok", pid, seg(0, "mic", "我", 1, "内容"))
	require.NoError(t, err)
	_, err = AppendLiveNoteChat("tok", pid, LiveNoteChatMessage{ClientID: "c1", Speaker: "我", Text: "你好"})
	require.NoError(t, err)

	require.NoError(t, DeleteMeetingRecord(project, pid, "tok"))

	_, err = os.Stat(paths.Dir)
	assert.True(t, os.IsNotExist(err), "the folder goes with the record")
	_, exists, err := GetLiveNote("tok")
	require.NoError(t, err)
	assert.False(t, exists)
	segs, err := ListLiveNoteSegments("tok", 0)
	require.NoError(t, err)
	assert.Empty(t, segs)
	msgs, err := ListLiveNoteChat("tok", 0)
	require.NoError(t, err)
	assert.Empty(t, msgs)
}

func TestDeleteMeetingRecord_UnknownToken(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()
	err := DeleteMeetingRecord(t.TempDir(), 0, "nope")
	assert.ErrorIs(t, err, ErrMeetingNotFound)
}

func TestAppendLiveNoteChat_IsIdempotentAndOrdered(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()
	pid, err := store.ProjectIDForPath("/proj/a")
	require.NoError(t, err)
	require.NoError(t, StartLiveNote("tok", pid, "周会", 1_700_000_000_000))

	first, err := AppendLiveNoteChat("tok", pid, LiveNoteChatMessage{ClientID: "c1", Speaker: "小明", Text: "一"})
	require.NoError(t, err)
	again, err := AppendLiveNoteChat("tok", pid, LiveNoteChatMessage{ClientID: "c1", Speaker: "小明", Text: "一"})
	require.NoError(t, err)
	assert.Equal(t, first, again, "a retried message keeps its row")
	_, err = AppendLiveNoteChat("tok", pid, LiveNoteChatMessage{ClientID: "c2", Speaker: "小红", Text: "二"})
	require.NoError(t, err)

	msgs, err := ListLiveNoteChat("tok", 0)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, "一", msgs[0].Text)
	assert.Equal(t, "二", msgs[1].Text)
	assert.Equal(t, 2, mustChatCount(t, "tok"))
}

func TestWriteLiveNoteChatFile_WritesReadableArchive(t *testing.T) {
	paths := LiveNotesPathsIn(t.TempDir())
	require.NoError(t, os.MkdirAll(paths.Dir, 0o755))
	_, err := WriteLiveNoteChatFile(paths, []LiveNoteChatMessage{
		{Speaker: "小明", Text: "第一句", At: 1_700_000_000_000},
		{Speaker: "", Text: "第二句"},
	})
	require.NoError(t, err)
	body, err := os.ReadFile(filepath.Join(paths.Dir, "chat.md"))
	require.NoError(t, err)
	assert.Contains(t, string(body), "小明：第一句")
	assert.Contains(t, string(body), "匿名：第二句")
}

func mustChatCount(t *testing.T, token string) int {
	t.Helper()
	n, err := CountLiveNoteChat(token)
	require.NoError(t, err)
	return n
}
