package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/store"
	"clawbench/internal/summarize"

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

// Two utterances on the SAME channel are never an echo, however close they are.
// 「好」「对」「嗯」 are the most common short answers in a meeting; dropping the
// second one silently removes a real reply from the transcript and minutes.
func TestDedupeLiveNoteSegments_KeepsRepeatedPhraseOnSameChannel(t *testing.T) {
	sorted := SortLiveNoteSegments([]LiveNoteSegment{
		seg(1, "mic", "我", 10_000, "好"),
		seg(2, "mic", "我", 13_000, "好"),
	})

	assert.Len(t, DedupeLiveNoteSegments(sorted), 2)
}

// Two different participants answering the same thing are two voices, not an
// echo of each other — neither channel is the loopback.
func TestDedupeLiveNoteSegments_KeepsSameWordsFromTwoViewers(t *testing.T) {
	sorted := SortLiveNoteSegments([]LiveNoteSegment{
		seg(1, "viewer:v1", "小明", 10_000, "对"),
		seg(2, "viewer:v2", "小红", 12_000, "对"),
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

	written, err := WriteLiveNotesMinutesFile(paths, "季度规划", started, "## 概要\n\n发布计划确认。", 7, true)
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
	assert.Contains(t, text, "[transcript.vtt](transcript.vtt)")
	assert.Contains(t, text, "[transcript-revised.md](transcript-revised.md)")
	// The merged recording is the one to listen to; the per-channel files are
	// the source it was built from.
	assert.Contains(t, text, "[meeting.webm](audio/meeting.webm)")
	assert.Contains(t, text, "`audio/`（7 个文件）")
}

func TestWriteLiveNotesMinutesFile_RequiresDirAndMinutes(t *testing.T) {
	_, err := WriteLiveNotesMinutesFile(LiveNotesPathsIn(""), "t", time.Now(), "m", 0, false)
	assert.Error(t, err)

	_, err = WriteLiveNotesMinutesFile(LiveNotesPathsIn(t.TempDir()), "t", time.Now(), "   ", 0, false)
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
	assert.Contains(t, string(body), "# 校对版转写 · 季度规划 · 2026-10-30 14:30")
	assert.Contains(t, string(body), "> 已结合项目上下文修正听写错误与专有名词")
	assert.Contains(t, string(body), "[00:00] 我：确认 ClawBench 的发布计划")

	base := started.UnixMilli()
	raw, err := WriteLiveNotesRawTranscript(paths, []LiveNoteSegment{
		{Speaker: "我", StartedAt: base, DurationMS: 1500, Text: "原样"},
	}, nil, nil)
	require.NoError(t, err)
	rawBody, err := os.ReadFile(raw)
	require.NoError(t, err)
	assert.Equal(t, paths.Transcript, raw)
	assert.True(t, strings.HasSuffix(raw, "transcript.vtt"), "raw transcript is WebVTT, got %q", raw)
	// A subtitle track starts with the format header, not a Markdown heading.
	assert.True(t, strings.HasPrefix(string(rawBody), "WEBVTT\n\n"), "want a WebVTT header, got %q", string(rawBody))
	assert.NotContains(t, string(rawBody), "# 转写")
	assert.Contains(t, string(rawBody), "00:00:00.000 --> 00:00:01.500")
	assert.Contains(t, string(rawBody), "<v 我>原样")

	_, err = WriteLiveNotesRawTranscript(paths, nil, nil, nil)
	assert.ErrorIs(t, err, ErrLiveNotesEmpty)

	_, err = WriteLiveNotesRawTranscript(LiveNotesPathsIn(""), []LiveNoteSegment{
		{Speaker: "我", StartedAt: base, DurationMS: 1000, Text: "原样"},
	}, nil, nil)
	assert.Error(t, err)
}

// artifactCJK matches any CJK character (including the fullwidth punctuation
// this file used to hardcode) — an English artifact must contain none.
var artifactCJK = regexp.MustCompile(`[\x{3000}-\x{303f}\x{4e00}-\x{9fff}\x{ff00}-\x{ffef}]`)

// The meeting folder is a user artifact that gets committed to the repository,
// and files.md has always followed the UI language. The chat archive, the
// minutes and the proofread transcript used to be Chinese-only regardless.
func TestMeetingArtifacts_FollowTheConfiguredLanguage(t *testing.T) {
	withLanguage(t, "en")
	paths := LiveNotesPathsIn(filepath.Join(t.TempDir(), "2026-10-30-1430-weekly-abc123"))
	started := time.Date(2026, 10, 30, 14, 30, 0, 0, time.Local)
	require.NoError(t, os.MkdirAll(paths.Dir, 0o755))

	chatPath, err := WriteLiveNoteChatFile(paths, []LiveNoteChatMessage{
		{Speaker: "Alice", Text: "first line", At: started.UnixMilli()},
		{Speaker: "", Text: "second line"},
	})
	require.NoError(t, err)
	chat, err := os.ReadFile(chatPath)
	require.NoError(t, err)
	assert.NotContains(t, string(chat), "会议聊天记录")
	assert.NotContains(t, string(chat), "匿名")
	assert.Contains(t, string(chat), "# Meeting chat log")
	assert.Contains(t, string(chat), "[14:30] Alice: first line")
	assert.Contains(t, string(chat), "Anonymous: second line")
	assert.NotRegexp(t, artifactCJK, string(chat))

	minutesPath, err := WriteLiveNotesMinutesFile(paths, "", started, "## Summary\n\nAll good.", 3, true)
	require.NoError(t, err)
	minutes, err := os.ReadFile(minutesPath)
	require.NoError(t, err)
	assert.Contains(t, string(minutes), "# Meeting minutes · 2026-10-30 14:30")
	assert.Contains(t, string(minutes), "## Artifacts")
	assert.Contains(t, string(minutes), "- Transcript: [transcript.vtt](transcript.vtt)")
	assert.Contains(t, string(minutes), "- Proofread transcript: [transcript-revised.md]")
	assert.Contains(t, string(minutes), "- Recording: [meeting.webm]")
	assert.Contains(t, string(minutes), "Per-channel recordings: `audio/` (3 files)")
	assert.NotRegexp(t, artifactCJK, string(minutes))

	revisedPath, err := WriteLiveNotesRevisedTranscript(paths, "Weekly sync", started, "[00:00] Alice: hello")
	require.NoError(t, err)
	revised, err := os.ReadFile(revisedPath)
	require.NoError(t, err)
	assert.Contains(t, string(revised), "# Proofread transcript · Weekly sync")
	assert.Contains(t, string(revised), "> Dictation errors and proper nouns were corrected")
	assert.NotRegexp(t, artifactCJK, string(revised))
}

// The transcript handed to the model, the lines merged into the proofread copy
// and the parser that puts file events back in order all have to agree on the
// separator — otherwise an English meeting gets a transcript the merge cannot
// place its events into.
func TestMeetingTranscript_SeparatorFollowsTheLanguageAndStaysParseable(t *testing.T) {
	withLanguage(t, "en")
	base := int64(1_700_000_000_000)

	transcript := BuildLiveNotesTranscript([]LiveNoteSegment{
		seg(1, "mic", "Alice", base, "hello"),
		seg(2, "system", "System", base+65_000, "bye"),
	})
	assert.Equal(t, "[00:00] Alice: hello\n[01:05] System: bye", transcript)
	assert.NotRegexp(t, artifactCJK, transcript)

	merged := MergeLiveNoteFileEvents(transcript, []LiveNoteFile{
		{Owner: "host", Path: "/proj/main.go", OpenedAt: base + 30_000, ClosedAt: base + 90_000},
	}, nil, base)
	assert.Contains(t, merged, "[00:30] File: Opened /proj/main.go")
	assert.Contains(t, merged, "[01:30] File: Closed /proj/main.go")
	assert.NotRegexp(t, artifactCJK, merged)
	// Every event line keeps the `[mm:ss]` prefix the merge itself keys on, so a
	// second merge pass would place it again instead of treating it as prose.
	for _, line := range strings.Split(merged, "\n") {
		if strings.HasPrefix(line, "[") {
			assert.NotNil(t, liveNoteTranscriptLine.FindStringSubmatch(line), "unparseable line %q", line)
		}
	}

	// The fallback section (used when the model rewrote the whole format) is
	// localized too.
	unparseable := MergeLiveNoteFileEvents("the model ignored the format", []LiveNoteFile{
		{Owner: "host", Path: "/proj/main.go", OpenedAt: base + 30_000},
	}, nil, base)
	assert.Contains(t, unparseable, "## Files opened during the meeting")
	assert.NotRegexp(t, artifactCJK, unparseable)
}

// A nickname is participant-typed text. A newline in it would start a new
// Markdown paragraph and let the sender forge a second chat line.
func TestWriteLiveNoteChatFile_FlattensSpeakerNewlines(t *testing.T) {
	paths := LiveNotesPathsIn(t.TempDir())
	require.NoError(t, os.MkdirAll(paths.Dir, 0o755))

	path, err := WriteLiveNoteChatFile(paths, []LiveNoteChatMessage{
		{Speaker: "小明\n[10:00] 小红\r\n", Text: "hi\nthere"},
	})
	require.NoError(t, err)
	body, err := os.ReadFile(path)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	require.Len(t, lines, 3, "heading, blank, one message line: %q", string(body))
	assert.Equal(t, "小明 [10:00] 小红：hi there", lines[2])
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
	_, err := GenerateLiveNotesMinutes(context.Background(), "标题", "", "", LiveNotesSpeaker{})
	assert.ErrorIs(t, err, ErrLiveNotesEmpty)
}

func TestGenerateLiveNotesMinutes_NoSummaryModel(t *testing.T) {
	withAISummary(t, "")

	_, err := GenerateLiveNotesMinutes(context.Background(), "标题", "[00:00] 我：内容", "", LiveNotesSpeaker{})
	assert.ErrorIs(t, err, ErrSummaryModelNotConfigured)
}

func TestGenerateLiveNotesMinutes_SingleChunk(t *testing.T) {
	srv := aiSummaryServer(t, "## 概要\n\n发布计划已确认。")
	withAISummary(t, srv.URL)

	got, err := GenerateLiveNotesMinutes(context.Background(), "季度会", "[00:00] 我：先确认时间", "", LiveNotesSpeaker{})
	require.NoError(t, err)
	assert.Equal(t, "## 概要\n\n发布计划已确认。", got)
}

// captureSummaryBodies serves a canned reply per call and records every request
// body, so a test can assert on what the summarizer was actually told.
func captureSummaryBodies(t *testing.T, replies ...string) func() []string {
	t.Helper()
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		idx := len(bodies) - 1
		mu.Unlock()
		if idx >= len(replies) {
			idx = len(replies) - 1
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + strconvQuote(replies[idx]) + `}}]}`))
	}))
	t.Cleanup(srv.Close)
	withAISummary(t, srv.URL)
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), bodies...)
	}
}

// The transcript labels the publisher's own channel with a localized "我" (or
// "Me"), which means nothing to a reader of the minutes. The nickname has to
// reach the prompt, otherwise the summary says "我负责发版" to somebody who
// was not in the meeting.
func TestGenerateLiveNotesMinutes_NamesTheUserBehindTheSelfLabel(t *testing.T) {
	bodies := captureSummaryBodies(t, "## 概要\n\n发布计划已确认。")

	_, err := GenerateLiveNotesMinutes(context.Background(), "季度会", "[00:00] 我：先确认时间", "",
		LiveNotesSpeaker{Label: "我", Name: "泷泽"})
	require.NoError(t, err)

	got := bodies()
	require.Len(t, got, 1)
	assert.Contains(t, got[0], "泷泽", "the nickname must reach the model")
	assert.Contains(t, got[0], "「我」", "the label must be named, not assumed")
	assert.Contains(t, got[0], "补充说明")
}

// A long meeting is map-reduced, and the chunk summaries are what the final
// pass reads — so the identity has to be in BOTH prompts, or the condensed
// text still says "我".
func TestGenerateLiveNotesMinutes_ChunkPassAlsoKnowsTheUser(t *testing.T) {
	long := strings.Repeat("很长的发言内容。", 400)
	segments := make([]LiveNoteSegment, 0, 8)
	for i := range 8 {
		segments = append(segments, seg(int64(i+1), "mic", "我", int64(i)*60_000, long))
	}
	transcript := BuildLiveNotesTranscript(segments)
	require.Greater(t, len([]rune(transcript)), liveNotesMinutesChunkRunes,
		"fixture must exceed one chunk or the chunk pass is not exercised")

	bodies := captureSummaryBodies(t, "要点", "要点", "## 概要\n\n合并后的纪要")
	_, err := GenerateLiveNotesMinutes(context.Background(), "", transcript, "",
		LiveNotesSpeaker{Label: "我", Name: "泷泽"})
	require.NoError(t, err)

	got := bodies()
	require.Len(t, got, 3, "two chunk passes plus one final pass")
	for i, body := range got {
		assert.Containsf(t, body, "泷泽", "pass %d must know who 「我」 is", i)
	}
}

// Nothing is injected when there is nothing to resolve: no nickname configured,
// only the placeholder, the user never spoke, or — the normal case now — the
// transcript already labels them with their 称谓.
func TestGenerateLiveNotesMinutes_NoSpeakerNoteWhenUnresolvable(t *testing.T) {
	cases := []struct {
		name string
		self LiveNotesSpeaker
	}{
		{"no label", LiveNotesSpeaker{Name: "泷泽"}},
		{"no name", LiveNotesSpeaker{Label: "我"}},
		{"placeholder name", LiveNotesSpeaker{Label: "我", Name: model.DefaultUserNickname}},
		{"whitespace", LiveNotesSpeaker{Label: "  ", Name: "  "}},
		{"label already is the 称谓", LiveNotesSpeaker{Label: "泷泽", Name: "泷泽"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bodies := captureSummaryBodies(t, "## 概要")
			_, err := GenerateLiveNotesMinutes(context.Background(), "", "[00:00] 我：内容", "", tc.self)
			require.NoError(t, err)

			got := bodies()
			require.Len(t, got, 1)
			assert.NotContains(t, got[0], "补充说明")
			assert.NotContains(t, got[0], "称谓是「"+model.DefaultUserNickname+"」",
				"the placeholder nickname must never be presented as a real name")
		})
	}
}

// The self label is data, not a constant: the client localizes it ("我" / "Me"),
// and the `mic` channel is the only one that is the publisher.
func TestLiveNotesSelfLabel(t *testing.T) {
	cases := []struct {
		name     string
		segments []LiveNoteSegment
		want     string
	}{
		{
			"mic label wins over viewers",
			[]LiveNoteSegment{
				{Channel: "viewer:v1", Speaker: "观众 1"},
				{Channel: "mic", Speaker: "Me"},
			},
			"Me",
		},
		{
			"no mic channel means the user never spoke",
			[]LiveNoteSegment{{Channel: "viewer:v1", Speaker: "观众 1"}},
			"",
		},
		{
			"unlabelled mic falls back to the channel name, as the transcript does",
			[]LiveNoteSegment{{Channel: "mic", Speaker: "  "}},
			"mic",
		},
		{"no segments", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, LiveNotesSelfLabel(tc.segments))
		})
	}
}

// A meeting longer than the chunk budget is map-reduced: every chunk is
// condensed first, and only the condensed text reaches the final pass.
func TestGenerateLiveNotesMinutes_LongMeetingIsMapReduced(t *testing.T) {
	segments := make([]LiveNoteSegment, 0, 8)
	long := strings.Repeat("很长的发言内容。", 400) // ~3200 runes per line
	for i := range 8 {
		segments = append(segments, seg(int64(i+1), "mic", "我", int64(i)*60_000, long))
	}
	require.Greater(t, len([]rune(BuildLiveNotesTranscript(segments))), liveNotesMinutesChunkRunes,
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

	got, err := GenerateLiveNotesMinutes(context.Background(), "", BuildLiveNotesTranscript(segments), "", LiveNotesSpeaker{})
	require.NoError(t, err)
	assert.Equal(t, "## 概要\n\n合并后的纪要", got)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 3, "two chunk passes plus one final pass")
	assert.Contains(t, bodies[0], "第 1/2 段", "chunk prompt carries its position")
	assert.Contains(t, bodies[1], "第 2/2 段")
	assert.Contains(t, bodies[0], `"max_tokens":4800`,
		"the extraction budget must scale with the chunk size")
	assert.Contains(t, bodies[2], "要点1", "the final pass sees the condensed chunks")
	assert.Contains(t, bodies[2], "要点2")
}

// A meeting under the minutes budget goes to the model in one pass. This is
// the point of the larger minutes chunk: re-chunking a 10k-rune transcript
// would only cost an extra pass and drop cross-chunk context.
func TestGenerateLiveNotesMinutes_ShortMeetingIsNotMapReduced(t *testing.T) {
	long := strings.Repeat("很长的发言内容。", 400) // ~3200 runes per line
	segments := make([]LiveNoteSegment, 0, 3)
	for i := range 3 {
		segments = append(segments, seg(int64(i+1), "mic", "我", int64(i)*60_000, long))
	}
	transcript := BuildLiveNotesTranscript(segments)
	require.Greater(t, len([]rune(transcript)), liveNotesReviseChunkRunes,
		"fixture must exceed the old budget or the test proves nothing")
	require.Less(t, len([]rune(transcript)), liveNotesMinutesChunkRunes)

	bodies := captureSummaryBodies(t, "## 概要\n\n一次生成。")
	_, err := GenerateLiveNotesMinutes(context.Background(), "", transcript, "", LiveNotesSpeaker{})
	require.NoError(t, err)
	require.Len(t, bodies(), 1, "a meeting this size must not be chunked")
}

func TestGenerateLiveNotesMinutes_ProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	withAISummary(t, srv.URL)

	_, err := GenerateLiveNotesMinutes(context.Background(), "", "[00:00] 我：内容", "", LiveNotesSpeaker{})
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
	}, LiveNotesSpeaker{})
	require.NoError(t, err)
	assert.Equal(t, "[00:00] 我：确认 ClawBench 的 ACP 计划", got)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 1)
	assert.Contains(t, bodies[0], "ClawBench 是产品名", "project context must reach the model")
	assert.Contains(t, bodies[0], "确认科劳本奇的 a c p 计划", "and so must the raw line")
}

// People are the only vocabulary the transcript carries itself: the line prefix
// is the speaker's 称谓, so a mangled name in the body has something to be
// checked against. A prefix that is only a pronoun or a placeholder carries no
// name, and a form of address must keep its honorific — all three are
// prompt-level contracts, and the prompt is the whole implementation of this
// stage.
func TestReviseLiveNotesTranscript_PromptCoversNamesAndAddress(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"[00:00] 我：好"}}]}`))
	}))
	defer srv.Close()
	withAISummary(t, srv.URL)

	_, err := ReviseLiveNotesTranscript(context.Background(), t.TempDir(), "", []LiveNoteSegment{
		seg(1, "mic", "我", 1_000, "好"),
	}, LiveNotesSpeaker{})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 1)
	prompt := bodies[0]

	assert.Contains(t, prompt, "行首的说话人标签就是本人的称谓",
		"the speaker label is the only name vocabulary in a transcript")
	assert.Contains(t, prompt, "称呼",
		"forms of address need their own rule, not just 「人名」")
	assert.Contains(t, prompt, "不要把称呼整体换成姓名",
		"「王总」 must not become 「王小明」: the honorific is not the model's to rewrite")
	assert.Contains(t, prompt, "「我」",
		"a pronoun label must be named explicitly, or the model will read it as a name")
	assert.Contains(t, prompt, "不要为任何说话人补上转写里没有的名字",
		"the pronoun rule applies to every speaker, not to one privileged role")
	assert.NotContains(t, prompt, "主持人",
		"no role may be singled out: the publisher is only a label like any other")
	assert.Contains(t, prompt, "标签本身不要动",
		"rule 3 corrects the body, never the label")
	assert.Contains(t, prompt, "不要改变时间戳、说话人",
		"the timeline lock must survive the new rules")
}

// LEGACY transcripts label the publisher with a pronoun, so nothing in the text
// says who they are; the name is passed in and goes into the SYSTEM prompt so
// that every chunk sees it (a chunk can mention them without containing a
// single 「我」 line). New transcripts label them with their 称谓, which rule 3
// already covers — hence the "label already is the 称谓" case below, where
// nothing may be injected.
func TestReviseLiveNotesTranscript_InjectsTheInitiator(t *testing.T) {
	cases := []struct {
		name string
		self LiveNotesSpeaker
		want string
	}{
		{
			name: "legacy 我 label",
			self: LiveNotesSpeaker{Label: "我", Name: "泷泽"},
			want: "转写里说话人标注为「我」的那一路，是本次会议的发起人本人，称谓是「泷泽」",
		},
		{
			name: "称谓 only, the initiator never spoke",
			self: LiveNotesSpeaker{Name: "泷泽"},
			want: "本次会议的发起人，称谓是「泷泽」",
		},
		{
			name: "the default placeholder is not a name",
			self: LiveNotesSpeaker{Label: "我", Name: model.DefaultUserNickname},
			want: "",
		},
		{
			name: "label already is the 称谓",
			self: LiveNotesSpeaker{Label: "泷泽", Name: "泷泽"},
			want: "",
		},
		{
			name: "nothing configured",
			self: LiveNotesSpeaker{Label: "我"},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var bodies []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				mu.Lock()
				bodies = append(bodies, string(body))
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"[00:00] 我：好"}}]}`))
			}))
			defer srv.Close()
			withAISummary(t, srv.URL)

			_, err := ReviseLiveNotesTranscript(context.Background(), t.TempDir(), "", []LiveNoteSegment{
				seg(1, "mic", "我", 1_000, "好"),
			}, tc.self)
			require.NoError(t, err)

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

			var system string
			for _, m := range req.Messages {
				if m.Role == "system" {
					system = m.Content
					continue
				}
				// The note must NOT ride along with the chunk: it belongs to the
				// instructions, which is what makes it apply to every chunk.
				assert.NotContains(t, m.Content, "发起人", "the initiator belongs in the system prompt")
			}

			if tc.want == "" {
				assert.NotContains(t, system, "发起人", "nothing to say when the 称谓 is a placeholder")
				return
			}
			assert.Contains(t, system, tc.want)
			if tc.self.Label != "" {
				assert.Contains(t, system, "不要动", "the label itself must survive the substitution")
			}
		})
	}
}

func TestReviseLiveNotesTranscript_EmptyTranscript(t *testing.T) {
	_, err := ReviseLiveNotesTranscript(context.Background(), t.TempDir(), "", nil, LiveNotesSpeaker{})
	assert.ErrorIs(t, err, ErrLiveNotesEmpty)
}

func TestReviseLiveNotesTranscript_NoSummaryModel(t *testing.T) {
	withAISummary(t, "")
	_, err := ReviseLiveNotesTranscript(context.Background(), t.TempDir(), "", []LiveNoteSegment{
		seg(1, "mic", "我", 1_000, "内容"),
	}, LiveNotesSpeaker{})
	assert.ErrorIs(t, err, ErrSummaryModelNotConfigured)
}

func TestSetLiveNoteRevise_RoundTrips(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()

	require.NoError(t, SetLiveNoteRevise("tok", 3, LiveNotesStatusReady, "校对后的文字", ""))
	note, ok, err := GetLiveNote("tok")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, LiveNotesStatusReady, note.ReviseStatus)
	assert.Equal(t, "校对后的文字", note.Revised)
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

	written, err := WriteLiveNotesMinutesFile(paths, "   ", started, "## 概要", 0, false)
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

// testMeetingDirName is the folder name the writer produces for the meetings
// these tests lay out; the reader is always addressed by this same name.
const testMeetingDirName = "2026-10-31-1732-周会-a1b2c3"

// writeMeetingFolder lays out one meeting folder the way the writer does, so
// the history reader is tested against the real layout rather than a guess.
func writeMeetingFolder(t *testing.T, project string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(project, filepath.FromSlash(LiveNotesDir), testMeetingDirName)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "audio"), 0o755))
	for file, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644))
	}
	return dir
}

func TestReadLiveNoteArtifact_ReturnsEachKind(t *testing.T) {
	project := t.TempDir()
	writeMeetingFolder(t, project, map[string]string{
		"minutes.md":            "MINUTES",
		"transcript.vtt":        "RAW",
		"transcript-revised.md": "REVISED",
	})

	for kind, want := range map[string]string{"minutes": "MINUTES", "transcript": "RAW", "revised": "REVISED"} {
		got, err := ReadLiveNoteArtifact(project, testMeetingDirName, kind)
		require.NoError(t, err, kind)
		assert.Equal(t, want, got)
	}
}

func TestReadLiveNoteArtifact_RejectsTraversalAndUnknownKinds(t *testing.T) {
	project := t.TempDir()
	writeMeetingFolder(t, project, map[string]string{"minutes.md": "MINUTES"})

	for _, dir := range []string{"", ".", "..", "../etc", "2026-10-31-1732-周会-a1b2c3/../..", "/etc"} {
		_, err := ReadLiveNoteArtifact(project, dir, "minutes")
		assert.ErrorIs(t, err, ErrLiveNoteSessionNotFound, "dir=%q must not resolve", dir)
	}
	_, err := ReadLiveNoteArtifact(project, testMeetingDirName, "audio")
	assert.ErrorIs(t, err, ErrLiveNoteArtifactKind)
	_, err = ReadLiveNoteArtifact(project, testMeetingDirName, "transcript")
	assert.ErrorIs(t, err, ErrLiveNoteSessionNotFound, "the file was never written")
}

func TestReadLiveNoteArtifact_RejectsOversizedFile(t *testing.T) {
	project := t.TempDir()
	dir := writeMeetingFolder(t, project, nil)
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "minutes.md"),
		bytes.Repeat([]byte("x"), liveNoteArtifactMaxBytes+1),
		0o644,
	))

	_, err := ReadLiveNoteArtifact(project, testMeetingDirName, "minutes")
	assert.ErrorIs(t, err, ErrLiveNoteArtifactTooLarge)
}

// ── WebVTT transcript ───────────────────────────────────────────────────────

// The cue identifier is the CHANNEL — the only stable speaker identity here —
// and the voice span is the name a player shows.
func TestBuildLiveNotesVTT_CarriesChannelAndSpeaker(t *testing.T) {
	base := time.Date(2026, 10, 30, 14, 30, 0, 0, time.Local).UnixMilli()
	got := BuildLiveNotesVTT([]LiveNoteSegment{
		{Channel: "mic", Speaker: "我", StartedAt: base, DurationMS: 1500, Text: "第一句"},
		// No duration: a cue still needs an end, so a floor applies.
		{Channel: "viewer:v1", Speaker: "张三", StartedAt: base + 2000, DurationMS: 0, Text: "第二句  有空格"},
		// No speaker: fall back to the channel label.
		{Channel: "system", Speaker: "", StartedAt: base + 5000, DurationMS: 1000, Text: "第三句"},
		// Blank text must not produce a cue.
		{Channel: "mic", Speaker: "我", StartedAt: base + 6000, DurationMS: 1000, Text: "   "},
	}, nil, nil)

	assert.True(t, strings.HasPrefix(got, "WEBVTT\n\n"))
	assert.Contains(t, got, "mic\n00:00:00.000 --> 00:00:01.500\n<v 我>第一句\n")
	assert.Contains(t, got, "viewer:v1\n00:00:02.000 --> 00:00:04.000\n<v 张三>第二句 有空格\n")
	assert.Contains(t, got, "system\n00:00:05.000 --> 00:00:06.000\n<v system>第三句\n")
	assert.Equal(t, 3, strings.Count(got, "-->"), "a blank utterance produces no cue")
}

func TestBuildLiveNotesVTT_UntimedSegmentsChainForward(t *testing.T) {
	got := BuildLiveNotesVTT([]LiveNoteSegment{
		{Channel: "mic", Speaker: "我", Text: "没有时间戳"},
		{Channel: "mic", Speaker: "我", Text: "也没有"},
	}, nil, nil)
	// Unknown timing chains from the previous cue instead of inventing an
	// epoch offset (which would show up as thousands of hours).
	assert.Contains(t, got, "mic\n00:00:00.000 --> 00:00:02.000\n<v 我>没有时间戳\n")
	assert.Contains(t, got, "mic\n00:00:02.000 --> 00:00:04.000\n<v 我>也没有\n")
}

// Cue text is text, not markup: "&" and "<" must survive as characters instead
// of being parsed as an entity or a tag. The speaker label is client input, so
// a ">" in it must not close the voice span early.
func TestBuildLiveNotesVTT_EscapesMarkup(t *testing.T) {
	got := BuildLiveNotesVTT([]LiveNoteSegment{
		{Channel: "viewer:v1", Speaker: "A<B>C", Text: "a < b & c > d"},
	}, nil, nil)
	assert.Contains(t, got, "<v ABC>a &lt; b &amp; c &gt; d\n")
}

func TestBuildLiveNotesVTT_Empty(t *testing.T) {
	assert.Empty(t, BuildLiveNotesVTT(nil, nil, nil))
	assert.Empty(t, BuildLiveNotesVTT([]LiveNoteSegment{{Speaker: "我", Text: "  "}}, nil, nil))
}

// The transcript artifact is WebVTT and nothing else: no fallback to the
// SubRip or Markdown files older builds wrote.
func TestReadLiveNoteArtifactWithName_TranscriptIsVTT(t *testing.T) {
	project := t.TempDir()
	writeMeetingFolder(t, project, map[string]string{
		"transcript.vtt": "WEBVTT\n\nmic\n00:00:00.000 --> 00:00:01.000\n<v 我>新\n",
		"transcript.srt": "1\n00:00:00,000 --> 00:00:01,000\n我：旧\n",
	})

	body, file, err := ReadLiveNoteArtifactWithName(project, testMeetingDirName, "transcript")
	require.NoError(t, err)
	assert.Equal(t, "transcript.vtt", file)
	assert.Contains(t, body, "<v 我>新")
}

func TestReadLiveNoteArtifactWithName_IgnoresLegacyTranscripts(t *testing.T) {
	project := t.TempDir()
	writeMeetingFolder(t, project, map[string]string{
		"transcript.srt": "1\n00:00:00,000 --> 00:00:01,000\n我：旧\n",
		"transcript.md":  "# 转写 · 旧会议\n",
	})

	_, _, err := ReadLiveNoteArtifactWithName(project, testMeetingDirName, "transcript")
	assert.ErrorIs(t, err, ErrLiveNoteSessionNotFound, "no compatibility layer for pre-VTT meetings")
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

// backdateLiveNote moves a row's last-write stamp. updated_at is the ONLY
// evidence the generation reset has that a job died with its process.
func backdateLiveNote(t *testing.T, token string, ago time.Duration) {
	t.Helper()
	_, err := store.WriteExec(`UPDATE live_notes SET updated_at = ? WHERE token = ?`,
		time.Now().Add(-ago).UTC().Format(liveNoteSQLTimeLayout), token)
	require.NoError(t, err)
}

// A generating row is written by a detached goroutine with a ten-minute budget.
// If the process dies inside it, nothing ever writes a terminal status — the row
// stays generating forever and the meeting can never be deleted. Age is the only
// signal left, so it must flip to failed; a job that wrote a minute ago is still
// plausibly alive.
func TestResetStaleLiveNoteGeneration_OnlyFlipsAbandonedJobs(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()
	pid, err := store.ProjectIDForPath("/proj/a")
	require.NoError(t, err)

	require.NoError(t, SetLiveNoteMinutes("stale", pid, "", LiveNotesStatusGenerating, "", "", ""))
	backdateLiveNote(t, "stale", 11*time.Minute)
	// The proofread stage has its own status column and its own detached job.
	require.NoError(t, SetLiveNoteRevise("stale-rev", pid, LiveNotesStatusGenerating, "", ""))
	backdateLiveNote(t, "stale-rev", 11*time.Minute)
	require.NoError(t, SetLiveNoteMinutes("fresh", pid, "", LiveNotesStatusGenerating, "", "", ""))
	backdateLiveNote(t, "fresh", time.Minute)
	require.NoError(t, SetLiveNoteMinutes("ready", pid, "", LiveNotesStatusReady, "m", "", ""))
	backdateLiveNote(t, "ready", 11*time.Minute)

	now := time.Now().UnixMilli()
	n, err := ResetStaleLiveNoteGeneration(now)
	require.NoError(t, err)
	assert.Equal(t, 2, n, "the two abandoned jobs, and nothing else")

	stale, _, err := GetLiveNote("stale")
	require.NoError(t, err)
	assert.Equal(t, LiveNotesStatusFailed, stale.Status)
	assert.NotEmpty(t, stale.Error, "the row must say why it is failed")

	rev, _, err := GetLiveNote("stale-rev")
	require.NoError(t, err)
	assert.Equal(t, LiveNotesStatusFailed, rev.ReviseStatus)
	assert.NotEmpty(t, rev.ReviseError)

	fresh, _, err := GetLiveNote("fresh")
	require.NoError(t, err)
	assert.Equal(t, LiveNotesStatusGenerating, fresh.Status, "a job that wrote a minute ago may still be running")
	running, err := LiveNoteGenerationRunning("fresh", now)
	require.NoError(t, err)
	assert.True(t, running)
	running, err = LiveNoteGenerationRunning("stale", now)
	require.NoError(t, err)
	assert.False(t, running, "an abandoned job must not block deleting its meeting")

	ready, _, err := GetLiveNote("ready")
	require.NoError(t, err)
	assert.Equal(t, LiveNotesStatusReady, ready.Status, "a finished job must not be rewritten")
	assert.Empty(t, ready.Error)
}

// The startup sweep is the path that actually runs after a crash: the reset has
// to happen there, not only in a helper nobody calls.
func TestCloseOrphanedLiveNotes_ResetsAbandonedGeneration(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()
	pid, err := store.ProjectIDForPath("/proj/a")
	require.NoError(t, err)
	require.NoError(t, StartLiveNote("crashed", pid, "崩溃的会", time.Now().Add(-30*time.Minute).UnixMilli()))
	require.NoError(t, SetLiveNoteMinutes("crashed", pid, "崩溃的会", LiveNotesStatusGenerating, "", "", ""))
	backdateLiveNote(t, "crashed", 11*time.Minute)

	_, err = CloseOrphanedLiveNotes(time.Now().UnixMilli())
	require.NoError(t, err)

	note, _, err := GetLiveNote("crashed")
	require.NoError(t, err)
	assert.Equal(t, LiveNotesStatusFailed, note.Status,
		"a restart must not leave the meeting stuck in generating")
}

// Same wiring on the hourly backstop: a row whose writer vanished while the
// process stayed up never reaches CloseOrphanedLiveNotes.
func TestCloseStaleLiveNotes_ResetsAbandonedGeneration(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()
	pid, err := store.ProjectIDForPath("/proj/a")
	require.NoError(t, err)
	require.NoError(t, StartLiveNote("stuck", pid, "卡住的会", time.Now().Add(-30*time.Minute).UnixMilli()))
	require.NoError(t, SetLiveNoteMinutes("stuck", pid, "卡住的会", LiveNotesStatusGenerating, "", "", ""))
	backdateLiveNote(t, "stuck", 11*time.Minute)

	// The 12h close window does not reach this row — only the reset should.
	closed, err := CloseStaleLiveNotes(time.Now().Add(-12 * time.Hour).UnixMilli())
	require.NoError(t, err)
	assert.Equal(t, 0, closed)

	note, _, err := GetLiveNote("stuck")
	require.NoError(t, err)
	assert.Equal(t, LiveNotesStatusFailed, note.Status)
}

// A restart is the only caller: rooms live in memory, so every record still
// marked open belongs to a process that is gone. Closing them is what stops the
// list from showing 进行中 for the next 12 hours.
func TestCloseOrphanedLiveNotes_ClosesWhatTheCrashedProcessLeftOpen(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()
	pid, err := store.ProjectIDForPath("/proj/a")
	require.NoError(t, err)
	require.NoError(t, StartLiveNote("crashed", pid, "崩溃的会", 1_700_000_000_000))
	// Last write ten minutes in: the closest thing to "when it really stopped".
	_, err = store.WriteExec(
		`UPDATE live_notes SET updated_at = '2023-11-14 22:23:20' WHERE token = 'crashed'`,
	)
	require.NoError(t, err)
	require.NoError(t, StartLiveNote("done", pid, "已结束", 1_700_000_000_000))
	require.NoError(t, EndLiveNote("done", 1_700_000_030_000))

	closed, err := CloseOrphanedLiveNotes(1_700_009_999_000)
	require.NoError(t, err)
	assert.Equal(t, 1, closed)

	crashed, _, err := GetLiveNote("crashed")
	require.NoError(t, err)
	assert.Equal(t, int64(1_700_000_600_000), crashed.EndedAt,
		"the finish line is the last write, not the restart time")

	done, _, err := GetLiveNote("done")
	require.NoError(t, err)
	assert.Equal(t, int64(1_700_000_030_000), done.EndedAt, "a closed record must not move")
}

// The finish line is clamped to the meeting's own window: a row written before
// it started (or one whose clock ran ahead) must not produce a negative
// duration or an end time in the future.
func TestCloseOrphanedLiveNotes_ClampsTheFinishLineToTheMeetingWindow(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()
	pid, err := store.ProjectIDForPath("/proj/a")
	require.NoError(t, err)
	require.NoError(t, StartLiveNote("backdated", pid, "倒挂", 1_700_000_500_000))
	_, err = store.WriteExec(
		`UPDATE live_notes SET updated_at = '2023-11-14 22:13:20' WHERE token = 'backdated'`,
	)
	require.NoError(t, err)
	require.NoError(t, StartLiveNote("ahead", pid, "时钟超前", 1_700_000_000_000))
	_, err = store.WriteExec(
		`UPDATE live_notes SET updated_at = '2030-01-01 00:00:00' WHERE token = 'ahead'`,
	)
	require.NoError(t, err)

	_, err = CloseOrphanedLiveNotes(1_700_009_999_000)
	require.NoError(t, err)

	backdated, _, err := GetLiveNote("backdated")
	require.NoError(t, err)
	assert.Equal(t, int64(1_700_000_500_000), backdated.EndedAt, "never before the start")
	ahead, _, err := GetLiveNote("ahead")
	require.NoError(t, err)
	assert.Equal(t, int64(1_700_009_999_000), ahead.EndedAt, "never after the restart")
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

// Pre-record rows (started_at = 0, from before the meeting-record model) are
// hidden rather than deleted by the upgrade, so their transcript survives. The
// list is the user's view: a hidden row must not appear there, and opening it by
// token must behave like a meeting that does not exist.
func TestMeetingRecords_HideRowsMarkedHidden(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()
	pid, err := store.ProjectIDForPath("/proj/a")
	require.NoError(t, err)
	project := t.TempDir()

	require.NoError(t, StartLiveNote("visible", pid, "周会", 1_700_000_000_000))
	require.NoError(t, StartLiveNote("legacy", pid, "旧会", 1_700_000_000_000))
	_, err = AppendLiveNoteSegment(context.Background(), "legacy", pid, seg(0, "mic", "我", 1, "旧转写"))
	require.NoError(t, err)
	_, err = store.WriteExec(`UPDATE live_notes SET hidden = 1 WHERE token = 'legacy'`)
	require.NoError(t, err)

	records, err := ListMeetingRecords(project, pid, 200)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "visible", records[0].Token)

	_, ok, err := GetMeetingRecord(project, pid, "legacy")
	require.NoError(t, err)
	assert.False(t, ok, "a hidden meeting must not open by token either")

	// Hiding is not deleting: the transcript is still there for recovery.
	segs, err := ListLiveNoteSegments("legacy", 0)
	require.NoError(t, err)
	assert.Len(t, segs, 1)
}

// A record carries what the room WAS (voice vs video) and how many people the
// transcript heard. It is what the record list needs to say something about a
// meeting it cannot otherwise describe.
func TestMeetingRecords_CarrySpeakerCount(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()
	pid, err := store.ProjectIDForPath("/proj/a")
	require.NoError(t, err)
	project := t.TempDir()

	require.NoError(t, StartLiveNote("voice", pid, "语音", 1_700_000_000_000))
	require.NoError(t, StartLiveNote("video", pid, "视频", 1_700_000_500_000))
	// Three segments, two people: the count must be DISTINCT speakers.
	for _, sg := range []LiveNoteSegment{
		seg(0, "mic", "我", 1_700_000_000_000, "开始"),
		seg(1, "system", "小明", 1_700_000_001_000, "收到"),
		seg(2, "mic", "我", 1_700_000_002_000, "继续"),
	} {
		_, appendErr := AppendLiveNoteSegment(context.Background(), "voice", pid, sg)
		require.NoError(t, appendErr)
	}

	records, err := ListMeetingRecords(project, pid, 200)
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, 2, records[1].SpeakerCount)
	assert.Equal(t, 0, records[0].SpeakerCount, "no transcript, no speakers")

	one, ok, err := GetMeetingRecord(project, pid, "voice")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 2, one.SpeakerCount)
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
	// The file timeline and the screen-share log are written by the publisher
	// during a meeting; a delete that only clears the other three tables leaves
	// them behind for good (no other statement removes them).
	require.NoError(t, RecordLiveNoteFiles(context.Background(), "tok", pid, []LiveNoteFile{
		{Owner: "host", Path: "/proj/a/main.go", OpenedAt: 1_700_000_001_000},
	}))
	require.NoError(t, SetLiveNoteScreenSharer(context.Background(), "tok", pid, "host", "主持人", 1_700_000_002_000))

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
	for _, table := range []string{"live_notes_files", "live_notes_screen"} {
		var n int
		require.NoError(t, store.ReadDB().QueryRow(
			"SELECT COUNT(*) FROM "+table+" WHERE token = 'tok'",
		).Scan(&n))
		assert.Zero(t, n, "%s rows must go with the meeting", table)
	}
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
	// Another message lands in between: this is what makes a stale
	// last_insert_rowid() differ from the right answer. A retry immediately
	// after the original would pass even with the buggy implementation.
	second, err := AppendLiveNoteChat("tok", pid, LiveNoteChatMessage{ClientID: "c2", Speaker: "小红", Text: "二"})
	require.NoError(t, err)
	require.NotEqual(t, first, second)

	again, err := AppendLiveNoteChat("tok", pid, LiveNoteChatMessage{ClientID: "c1", Speaker: "小明", Text: "一"})
	require.NoError(t, err)
	assert.Equal(t, first, again, "a retried message keeps ITS row, not the last one this connection inserted")

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

// ── Timeline origin, durations and rendering edges ──────────────────────────

// Every artifact (transcript, WebVTT, file events) computes its offsets from
// the same origin, and that origin must skip a segment whose timestamp is
// missing: 0 sorts first, and using it would turn every real offset into
// "milliseconds since 1970" — putting the last thing said at the top.
func TestLiveNotesTimelineBase_IgnoresUntimedSegments(t *testing.T) {
	base := int64(1_700_000_000_000)

	assert.Zero(t, LiveNotesTimelineBase(nil), "no segments means no origin")
	assert.Equal(t, base+5_000, LiveNotesTimelineBase([]LiveNoteSegment{
		{ID: 1, Channel: "mic", StartedAt: 0, Text: "没有时间戳"},
		{ID: 2, Channel: "mic", StartedAt: base + 20_000, Text: "后一句"},
		{ID: 3, Channel: "mic", StartedAt: base + 5_000, Text: "前一句"},
	}))
}

func TestLiveNoteDurationValue_Buckets(t *testing.T) {
	withLanguage(t, "zh")

	cases := []struct {
		name string
		ms   int64
		want string
	}{
		{"under a second says nothing", 999, ""},
		{"seconds", 8_400, "8 秒"},
		{"whole minutes keep the minute form", 8 * 60_000, "8 分钟"},
		{"minutes and seconds", 8*60_000 + 25_000, "8 分 25 秒"},
		{"hours drop the seconds", 2*3600_000 + 5*60_000 + 30_000, "2 小时 5 分"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, liveNoteDurationValue(tc.ms))
		})
	}
}

// The stay suffix is only added when there is something to say: a two-second
// visit must not render as an empty pair of brackets in the manifest.
func TestLiveNoteDurationSuffix_WrapsOnlyRealDurations(t *testing.T) {
	withLanguage(t, "zh")

	assert.Empty(t, liveNoteDurationSuffix(500))
	assert.Equal(t, "（8 分 25 秒）", liveNoteDurationSuffix(8*60_000+25_000))
}

// A cue offset cannot be negative; a segment stamped before the origin can only
// come from a clock that moved backwards, and the file must still be valid.
func TestLiveNoteEventLines_ClampNegativeOffsets(t *testing.T) {
	withLanguage(t, "zh")

	assert.True(t, strings.HasPrefix(liveNoteFileLine(-500, "打开 a.md"), "[00:00] 文件："))
	assert.True(t, strings.HasPrefix(liveNoteScreenLine(-1, "开始共享屏幕"), "[00:00] 屏幕："))
}

func TestVTTTimestamp_ClampsNegativeToZero(t *testing.T) {
	assert.Equal(t, "00:00:00.000", vttTimestamp(-1))
	assert.Equal(t, "01:02:03.004", vttTimestamp(3600_000+2*60_000+3000+4))
}

// An unset UI language must mean the product default, not "English by
// accident": the minutes are written by a background job with no request in
// scope, so model.Language is the only signal there is.
func TestLiveNoteT_UnsetLanguageUsesTheProductDefault(t *testing.T) {
	prev := model.Language
	t.Cleanup(func() { model.Language = prev })

	model.Language = ""
	unset := liveNoteT("MeetingUntitled")
	model.Language = model.DefaultLanguage
	assert.Equal(t, liveNoteT("MeetingUntitled"), unset)
	assert.NotEmpty(t, unset)
}

// The publisher has the full path; a viewer's copy of the row carries only the
// base name. Both must render, and an event with neither cannot be shown.
func TestLiveNoteFileLabel_PathWinsAndNameIsTheFallback(t *testing.T) {
	assert.Equal(t, "docs/a.md", liveNoteFileLabel(LiveNoteFile{Path: " docs/a.md ", Name: "a.md"}))
	assert.Equal(t, "a.md", liveNoteFileLabel(LiveNoteFile{Name: "a.md"}))
	assert.Empty(t, liveNoteFileLabel(LiveNoteFile{}))
}

func TestMeetingDirPath_NeedsBothHalves(t *testing.T) {
	assert.Empty(t, MeetingDirPath("", "2026-10-30-1430"))
	assert.Empty(t, MeetingDirPath("/proj", "  "))
	assert.Equal(t,
		filepath.Join("/proj", ".clawbench/notes", "2026-10-30-1430"),
		MeetingDirPath("/proj", "2026-10-30-1430"))
}

func TestMeetingDirName_UnknownMeetingIsEmpty(t *testing.T) {
	setupTestDBForLiveNotes(t)

	name, err := MeetingDirName("nope")
	require.NoError(t, err)
	assert.Empty(t, name)
}

// A channel with no printable ASCII left still has to name a file; an empty
// element would make SaveLiveNoteAudio write ".webm" for every such channel.
func TestLiveNotesChannelSlug_UnprintableChannelGetsAPlaceholder(t *testing.T) {
	assert.Equal(t, "viewer-2", liveNotesChannelSlug("viewer:2"))
	assert.Equal(t, "channel", liveNotesChannelSlug("观众"), "a slug with no ASCII left still names a file")
	assert.Equal(t, "channel", liveNotesChannelSlug(""))
}

// Binary content must never reach the prompt. A NUL in the head is the cheap
// test; invalid UTF-8 is refused because it cannot be rendered faithfully.
func TestLiveNoteExcerptText_RejectsBinaryAndCapsRunes(t *testing.T) {
	assert.Empty(t, liveNoteExcerptText([]byte{0x89, 'P', 'N', 'G', 0x00}, 100))
	assert.Empty(t, liveNoteExcerptText([]byte{0xff, 0xfe, 'a'}, 100))
	assert.Equal(t, "abc", liveNoteExcerptText([]byte("  abc \n"), 100))
	assert.Equal(t, "abc", liveNoteExcerptText([]byte("abcdef"), 3))
}

// Reading is bounded: a client that reports a thousand files must not turn the
// prompt into a file dump.
func TestLiveNoteFileExcerpts_ReadsAtMostFiveFiles(t *testing.T) {
	project := t.TempDir()
	files := make([]LiveNoteFile, 0, 6)
	for i := range 6 {
		name := "f" + strconv.Itoa(i) + ".md"
		require.NoError(t, os.WriteFile(filepath.Join(project, name), []byte(name+" 的内容"), 0o644))
		files = append(files, LiveNoteFile{Path: name, OpenedAt: int64(i + 1)})
	}

	got := LiveNoteFileExcerpts(project, files)
	assert.Contains(t, got, "### f4.md")
	assert.NotContains(t, got, "### f5.md", "only the first five files are read")
}

// Every reason a file cannot contribute an excerpt must be a skip, not an
// error: one unreadable file must not cost the meeting its summary.
func TestLiveNoteFileExcerpts_SkipsUnusableFiles(t *testing.T) {
	project := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(project, "folder.md"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(project, "bin.md"), []byte("a\x00b"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(project, "big.md"), make([]byte, LiveNoteFileCopyMaxBytes+1), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(project, "ok.md"), []byte("可用内容"), 0o644))

	got := LiveNoteFileExcerpts(project, []LiveNoteFile{
		{Path: "folder.md", OpenedAt: 1},
		{Path: "bin.md", OpenedAt: 2},
		{Path: "big.md", OpenedAt: 3},
		{Path: "gone.md", OpenedAt: 4},
		{Path: "ok.md", OpenedAt: 5},
	})
	assert.Contains(t, got, "### ok.md")
	for _, skipped := range []string{"folder.md", "bin.md", "big.md", "gone.md"} {
		assert.NotContains(t, got, "### "+skipped, "%s must be skipped, not excerpted", skipped)
	}
}

func TestResolveLiveNoteFilePath_AcceptsProjectRelativeAndAbsolute(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "a.md")
	assert.Equal(t, abs, resolveLiveNoteFilePath("/proj", abs))
	assert.Equal(t, filepath.Join("/proj", "docs/a.md"), resolveLiveNoteFilePath("/proj", "docs/a.md"))
	assert.Empty(t, resolveLiveNoteFilePath("", "docs/a.md"), "a relative path with no project cannot be resolved")
	assert.Empty(t, resolveLiveNoteFilePath("/proj", "  "))
}

// An entry with no open time, or with nothing to call it, cannot be placed on
// the timeline — and an empty list of rows must not produce a bare heading.
func TestLiveNoteFilesPromptSection_AllEntriesUnusable(t *testing.T) {
	started := time.UnixMilli(1_700_000_000_000)

	assert.Empty(t, LiveNoteFilesPromptSection([]LiveNoteFile{{Path: "a.md"}}, started, ""))
	assert.Empty(t, LiveNoteFilesPromptSection([]LiveNoteFile{{OpenedAt: started.UnixMilli()}}, started, ""))
}

// A screen share that never ended says so; an interval with no start cannot be
// placed at all.
func TestLiveNoteScreenPromptSection_OpenIntervalAndUnusableRows(t *testing.T) {
	started := time.UnixMilli(1_700_000_000_000)

	got := LiveNoteScreenPromptSection([]LiveNoteScreenShare{
		{Sharer: "host", SharerName: "泷泽", StartedAt: started.UnixMilli()},
	}, started)
	assert.Contains(t, got, "泷泽 共享屏幕")
	assert.Contains(t, got, "起", "an interval still running says so instead of inventing an end")

	assert.Empty(t, LiveNoteScreenPromptSection([]LiveNoteScreenShare{{Sharer: "host"}}, started))
	assert.Empty(t, LiveNoteScreenPromptSection(nil, started))
}

// File events without a timeline origin are dropped, so a share that has only
// file events produces no track at all rather than one starting at 1970.
func TestBuildLiveNotesVTT_NoUsableCues(t *testing.T) {
	assert.Empty(t, BuildLiveNotesVTT(nil, []LiveNoteFile{
		{Path: "a.md", OpenedAt: 1_700_000_000_000},
	}, nil))
}

// A cue whose channel and speaker are both blank has no voice span to open; it
// still has to be written as a plain cue instead of an empty <v> tag.
func TestBuildLiveNotesVTT_UnlabelledCueHasNoVoiceSpan(t *testing.T) {
	got := BuildLiveNotesVTT([]LiveNoteSegment{
		{Channel: "", Speaker: "", StartedAt: 1_700_000_000_000, DurationMS: 1000, Text: "没有名字"},
	}, nil, nil)

	assert.Contains(t, got, "没有名字")
	assert.NotContains(t, got, "<v", "a blank label must not produce an empty voice span")
}

// File and screen events with no usable timestamp contribute nothing, which
// must leave the proofread transcript untouched rather than reflowing it.
func TestMergeLiveNoteFileEvents_UnusableEventsAreIdentity(t *testing.T) {
	revised := "[00:00] 我：内容"
	assert.Equal(t, revised, MergeLiveNoteFileEvents(revised,
		[]LiveNoteFile{{Path: "a.md"}}, []LiveNoteScreenShare{{Sharer: "host"}}, 1_700_000_000_000))
}

func TestLiveNoteArtifactFlags_EmptyPathProbesNothing(t *testing.T) {
	st := liveNoteArtifactFlags("")
	assert.False(t, st.hasMinutes)
	assert.False(t, st.hasTranscript)
	assert.Zero(t, st.audioFiles)
}

// ── Minutes title ───────────────────────────────────────────────────────────

func TestGenerateLiveNotesTitle_NamesTheMeetingFromTheMinutes(t *testing.T) {
	srv := aiSummaryServer(t, "季度发布计划评审")
	withAISummary(t, srv.URL)

	got, err := GenerateLiveNotesTitle(context.Background(), "## 概要\n\n确认了下周发布。")
	require.NoError(t, err)
	assert.Equal(t, "季度发布计划评审", got)
}

func TestGenerateLiveNotesTitle_NoSummaryModel(t *testing.T) {
	withAISummary(t, "")

	_, err := GenerateLiveNotesTitle(context.Background(), "## 概要")
	assert.ErrorIs(t, err, ErrSummaryModelNotConfigured)
}

func TestGenerateLiveNotesTitle_ProviderErrorIsWrapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	withAISummary(t, srv.URL)

	_, err := GenerateLiveNotesTitle(context.Background(), "## 概要")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "live notes: title")
}

// ── Storage failures ────────────────────────────────────────────────────────

// Every store-backed operation must surface a storage failure with its own
// operation name. A silent zero would look like "this meeting has nothing",
// which is the one answer the caller cannot tell apart from the truth.
func TestLiveNotes_StoreUnavailableReportsEveryOperation(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	require.NoError(t, db.Close())

	ctx := context.Background()
	base := int64(1_700_000_000_000)
	cases := []struct {
		name string
		want string
		call func() error
	}{
		{"append segment", "live notes: begin", func() error {
			_, err := AppendLiveNoteSegment(ctx, "tok", 1, seg(0, "mic", "我", base, "内容"))
			return err
		}},
		{"record files", "live notes: begin", func() error {
			return RecordLiveNoteFiles(ctx, "tok", 1, []LiveNoteFile{{Path: "a.md", OpenedAt: base}})
		}},
		{"set screen sharer", "live notes: begin", func() error {
			return SetLiveNoteScreenSharer(ctx, "tok", 1, "host", "泷泽", base)
		}},
		{"delete meeting", "live notes: delete lookup", func() error {
			return DeleteMeetingRecord(t.TempDir(), 1, "tok")
		}},
		{"close open files", "live notes: close open files", func() error {
			return CloseOpenLiveNoteFiles("tok", base)
		}},
		{"close open screen", "live notes: close open screen share", func() error {
			return CloseOpenLiveNoteScreenShare("tok", base)
		}},
		{"start meeting", "live notes: start meeting", func() error {
			return StartLiveNote("tok", 1, "标题", base)
		}},
		{"end meeting", "live notes: end meeting", func() error {
			return EndLiveNote("tok", base)
		}},
		{"set minutes", "live notes: set minutes", func() error {
			return SetLiveNoteMinutes("tok", 1, "标题", LiveNotesStatusReady, "纪要", "", "")
		}},
		{"set revise", "live notes: set revise", func() error {
			return SetLiveNoteRevise("tok", 1, LiveNotesStatusReady, "校对稿", "")
		}},
		{"reset stale generation", "live notes: reset stale generation", func() error {
			_, err := ResetStaleLiveNoteGeneration(base)
			return err
		}},
		{"close stale meetings", "live notes: reset stale generation", func() error {
			_, err := CloseStaleLiveNotes(base)
			return err
		}},
		{"close orphaned meetings", "live notes: reset stale generation", func() error {
			_, err := CloseOrphanedLiveNotes(base)
			return err
		}},
		{"append chat", "live notes: append chat", func() error {
			_, err := AppendLiveNoteChat("tok", 1, LiveNoteChatMessage{ClientID: "c1", Text: "内容"})
			return err
		}},
		{"list segments", "live notes: list segments", func() error {
			_, err := ListLiveNoteSegments("tok", 0)
			return err
		}},
		{"list files", "live notes: list files", func() error {
			_, err := ListLiveNoteFiles("tok", 0)
			return err
		}},
		{"list screen shares", "live notes: list screen shares", func() error {
			_, err := ListLiveNoteScreenShares("tok")
			return err
		}},
		{"list chat", "live notes: list chat", func() error {
			_, err := ListLiveNoteChat("tok", 0)
			return err
		}},
		{"count chat", "live notes: count chat", func() error {
			_, err := CountLiveNoteChat("tok")
			return err
		}},
		{"get note", "live notes: get", func() error {
			_, _, err := GetLiveNote("tok")
			return err
		}},
		{"project id", "live notes: project lookup", func() error {
			_, _, err := LiveNoteProjectID("tok")
			return err
		}},
		{"dir name", "live notes: dir lookup", func() error {
			_, err := MeetingDirName("tok")
			return err
		}},
		{"generation running", "live notes: generation lookup", func() error {
			_, err := LiveNoteGenerationRunning("tok", base)
			return err
		}},
		{"list meetings", "live notes: list meetings", func() error {
			_, err := ListMeetingRecords("/proj", 1, 0)
			return err
		}},
		{"get meeting", "live notes: get meeting", func() error {
			_, _, err := GetMeetingRecord("/proj", 1, "tok")
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// dropLiveNoteTable removes one table so the next statement against it fails
// while the rest of the schema — and the connection — stays usable.
func dropLiveNoteTable(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	_, err := db.Exec("DROP TABLE " + table)
	require.NoError(t, err)
}

// A statement that fails part-way must abort the whole batch instead of
// committing a partial one: the publisher retries, and half a meeting is worse
// than a failed upload.
func TestLiveNotes_PartialSchemaFailuresAbortTheOperation(t *testing.T) {
	ctx := context.Background()
	base := int64(1_700_000_000_000)

	t.Run("missing owner row aborts every publisher write", func(t *testing.T) {
		db := setupTestDBForLiveNotes(t)
		dropLiveNoteTable(t, db, "live_notes")

		_, err := AppendLiveNoteSegment(ctx, "tok", 1, seg(0, "mic", "我", base, "内容"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "live notes: ensure row")

		err = RecordLiveNoteFiles(ctx, "tok", 1, []LiveNoteFile{{Path: "a.md", OpenedAt: base}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "live notes: ensure row")

		err = SetLiveNoteScreenSharer(ctx, "tok", 1, "host", "", base)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "live notes: ensure row")
	})

	t.Run("segment insert failure", func(t *testing.T) {
		db := setupTestDBForLiveNotes(t)
		dropLiveNoteTable(t, db, "live_notes_segments")

		_, err := AppendLiveNoteSegment(ctx, "tok", 1, seg(0, "mic", "我", base, "内容"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "live notes: insert segment")
	})

	t.Run("file insert failure", func(t *testing.T) {
		db := setupTestDBForLiveNotes(t)
		dropLiveNoteTable(t, db, "live_notes_files")

		err := RecordLiveNoteFiles(ctx, "tok", 1, []LiveNoteFile{{Path: "a.md", OpenedAt: base}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "live notes: insert file")
	})

	t.Run("unreadable screen table", func(t *testing.T) {
		db := setupTestDBForLiveNotes(t)
		dropLiveNoteTable(t, db, "live_notes_screen")

		err := SetLiveNoteScreenSharer(ctx, "tok", 1, "host", "泷泽", base)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "live notes: read open screen share")
	})

	t.Run("chat insert failure", func(t *testing.T) {
		db := setupTestDBForLiveNotes(t)
		dropLiveNoteTable(t, db, "live_notes_chat")

		_, err := AppendLiveNoteChat("tok", 1, LiveNoteChatMessage{ClientID: "c1", Text: "内容"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "live notes: append chat")
	})
}

// A row that cannot be decoded is an error, not a silently skipped line: a
// transcript missing one utterance reads as if nobody said it.
func TestLiveNotes_UnscannableRowsAreReported(t *testing.T) {
	cases := []struct {
		name   string
		table  string
		ddl    string
		insert string
		want   string
		call   func() error
	}{
		{
			name:  "segments",
			table: "live_notes_segments",
			ddl: `CREATE TABLE live_notes_segments (
				id TEXT, token TEXT, project_id INTEGER, channel TEXT, speaker TEXT,
				started_at TEXT, duration_ms TEXT, text TEXT)`,
			insert: `INSERT INTO live_notes_segments VALUES ('not-a-number','tok',1,'mic','我','0','0','内容')`,
			want:   "live notes: scan segment",
			call:   func() error { _, err := ListLiveNoteSegments("tok", 0); return err },
		},
		{
			name:  "files",
			table: "live_notes_files",
			ddl: `CREATE TABLE live_notes_files (
				id INTEGER, token TEXT, project_id INTEGER, owner TEXT, owner_name TEXT, path TEXT,
				opened_at TEXT, closed_at TEXT, source TEXT)`,
			insert: `INSERT INTO live_notes_files VALUES (1,'tok',1,'host','','a.md','soon','0','preview')`,
			want:   "live notes: scan file",
			call:   func() error { _, err := ListLiveNoteFiles("tok", 0); return err },
		},
		{
			name:  "screen shares",
			table: "live_notes_screen",
			ddl: `CREATE TABLE live_notes_screen (
				id INTEGER, token TEXT, project_id INTEGER, sharer TEXT, sharer_name TEXT,
				started_at TEXT, ended_at TEXT)`,
			insert: `INSERT INTO live_notes_screen VALUES (1,'tok',1,'host','泷泽','soon','0')`,
			want:   "live notes: scan screen share",
			call:   func() error { _, err := ListLiveNoteScreenShares("tok"); return err },
		},
		{
			name:  "chat",
			table: "live_notes_chat",
			ddl: `CREATE TABLE live_notes_chat (
				id INTEGER, token TEXT, project_id INTEGER, client_msg_id TEXT, speaker TEXT,
				viewer_id TEXT, text TEXT, created_at TEXT)`,
			insert: `INSERT INTO live_notes_chat VALUES (1,'tok',1,'c1','我','','内容','not-a-date')`,
			want:   "live notes: scan chat",
			call:   func() error { _, err := ListLiveNoteChat("tok", 0); return err },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDBForLiveNotes(t)
			dropLiveNoteTable(t, db, tc.table)
			_, err := db.Exec(tc.ddl)
			require.NoError(t, err)
			_, err = db.Exec(tc.insert)
			require.NoError(t, err)

			err = tc.call()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// The meeting list is assembled from three queries; a failure in either count
// query must fail the list rather than render zeros for every meeting.
func TestMeetingRecords_CountQueryFailuresAreReported(t *testing.T) {
	base := int64(1_700_000_000_000)

	t.Run("chat counts", func(t *testing.T) {
		db := setupTestDBForLiveNotes(t)
		require.NoError(t, StartLiveNote("tok", 1, "标题", base))
		dropLiveNoteTable(t, db, "live_notes_chat")

		_, err := ListMeetingRecords(t.TempDir(), 1, 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "live notes: chat counts")

		_, _, err = GetMeetingRecord(t.TempDir(), 1, "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "live notes: chat counts")
	})

	t.Run("speaker counts", func(t *testing.T) {
		db := setupTestDBForLiveNotes(t)
		require.NoError(t, StartLiveNote("tok", 1, "标题", base))
		dropLiveNoteTable(t, db, "live_notes_segments")

		_, err := ListMeetingRecords(t.TempDir(), 1, 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "live notes: speaker counts")

		_, _, err = GetMeetingRecord(t.TempDir(), 1, "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "live notes: speaker counts")
	})
}

func TestListMeetingRecords_ClampsTheLimit(t *testing.T) {
	setupTestDBForLiveNotes(t)
	base := int64(1_700_000_000_000)
	for i := range 205 {
		require.NoError(t, StartLiveNote("m"+strconv.Itoa(i), 1, "会议", base+int64(i)))
	}

	// Both an absent limit and an absurd one fall back to the same cap, so the
	// list cannot be turned into a full-table read by a crafted query string.
	for _, limit := range []int{0, -1, 501} {
		rows, err := ListMeetingRecords(t.TempDir(), 1, limit)
		require.NoError(t, err)
		assert.Len(t, rows, 200, "limit=%d", limit)
	}

	rows, err := ListMeetingRecords(t.TempDir(), 1, 3)
	require.NoError(t, err)
	assert.Len(t, rows, 3)
}

// The detail view is the list row plus its artifacts: the flags are probed from
// the folder, and the counts come from the tables.
func TestGetMeetingRecord_CarriesArtifactsAndCounts(t *testing.T) {
	setupTestDBForLiveNotes(t)
	project := t.TempDir()
	base := int64(1_700_000_000_000)

	paths, err := EnsureMeetingDir("tok", 1, project, "季度会", base)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(paths.Minutes, []byte("# 纪要"), 0o644))
	require.NoError(t, os.WriteFile(paths.Transcript, []byte("WEBVTT\n"), 0o644))
	_, err = AppendLiveNoteChat("tok", 1, LiveNoteChatMessage{ClientID: "c1", Speaker: "我", Text: "内容"})
	require.NoError(t, err)
	_, err = AppendLiveNoteSegment(context.Background(), "tok", 1, seg(0, "mic", "我", base, "内容"))
	require.NoError(t, err)

	rec, ok, err := GetMeetingRecord(project, 1, "tok")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "tok", rec.Token)
	assert.True(t, rec.HasMinutes)
	assert.True(t, rec.HasTranscript)
	assert.False(t, rec.HasRevised)
	assert.Equal(t, 1, rec.ChatCount)
	assert.Equal(t, 1, rec.SpeakerCount)
	assert.Equal(t, MeetingDirPath(project, rec.Dir), rec.DirPath)
}

// ── Filesystem failures ─────────────────────────────────────────────────────

// A file where a directory belongs: the write cannot succeed, and saying so is
// the only way the caller can retry instead of believing the artifact exists.
func TestLiveNotesArtifacts_ReportUnwritableFolders(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "meeting")
	require.NoError(t, os.WriteFile(blocked, []byte("x"), 0o644))
	blockedPaths := LiveNotesPathsIn(blocked)
	started := time.UnixMilli(1_700_000_000_000)

	_, err := WriteLiveNotesMinutesFile(blockedPaths, "标题", started, "## 概要", 0, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create notes dir")

	_, err = WriteLiveNotesRawTranscript(blockedPaths, []LiveNoteSegment{{Channel: "mic", StartedAt: 1, Text: "内容"}}, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create notes dir")

	_, err = WriteLiveNotesRevisedTranscript(blockedPaths, "标题", started, "[00:00] 我：内容")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create notes dir")

	_, err = ensureLiveNotesDirs(filepath.Join(blocked, "meeting"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create notes dir")
}

// The meeting folder exists but the artifact name is taken by a directory: the
// write must fail loudly rather than leave a half-written file behind.
func TestLiveNotesArtifacts_ReportUnwritableTargets(t *testing.T) {
	started := time.UnixMilli(1_700_000_000_000)

	minutesPaths := LiveNotesPathsIn(t.TempDir())
	require.NoError(t, os.MkdirAll(minutesPaths.Minutes, 0o755))
	_, err := WriteLiveNotesMinutesFile(minutesPaths, "标题", started, "## 概要", 0, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write minutes file")

	rawPaths := LiveNotesPathsIn(t.TempDir())
	require.NoError(t, os.MkdirAll(rawPaths.Transcript, 0o755))
	_, err = WriteLiveNotesRawTranscript(rawPaths, []LiveNoteSegment{{Channel: "mic", StartedAt: 1, Text: "内容"}}, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write transcript file")

	revisedPaths := LiveNotesPathsIn(t.TempDir())
	require.NoError(t, os.MkdirAll(revisedPaths.Revised, 0o755))
	_, err = WriteLiveNotesRevisedTranscript(revisedPaths, "标题", started, "[00:00] 我：内容")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write transcript file")
}

// Nothing to write is reported as empty, not as an empty artifact: an empty
// subtitle track would open as a broken file in the viewer.
func TestWriteLiveNotesRawTranscript_EmptyTranscript(t *testing.T) {
	_, err := WriteLiveNotesRawTranscript(LiveNotesPathsIn(t.TempDir()), nil, nil, nil)
	assert.ErrorIs(t, err, ErrLiveNotesEmpty)

	_, err = WriteLiveNotesRevisedTranscript(LiveNotesPathsIn(t.TempDir()), "标题", time.Now(), "   ")
	assert.ErrorIs(t, err, ErrLiveNotesEmpty)
}

// An unnamed meeting keeps a single separator in its heading; an empty title
// segment ("# 转写 ·  · 2026-10-30") reads as a bug.
func TestWriteLiveNotesRevisedTranscript_UntitledHeadingHasOneSeparator(t *testing.T) {
	paths := LiveNotesPathsIn(t.TempDir())
	path, err := WriteLiveNotesRevisedTranscript(paths, "  ", time.UnixMilli(1_700_000_000_000), "[00:00] 我：内容")
	require.NoError(t, err)

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	heading, _, _ := strings.Cut(string(body), "\n")
	assert.Equal(t, 1, strings.Count(heading, "·"), "heading: %s", heading)
}

func TestSaveLiveNoteAudio_RejectsEmptyAndUnwritableTargets(t *testing.T) {
	paths := LiveNotesPathsIn(t.TempDir())
	started := int64(1_700_000_000_000)

	_, err := SaveLiveNoteAudio(paths, "mic", started, nil)
	assert.EqualError(t, err, "live notes: empty audio")

	blocked := filepath.Join(t.TempDir(), "meeting")
	require.NoError(t, os.WriteFile(blocked, []byte("x"), 0o644))
	_, err = SaveLiveNoteAudio(LiveNotesPathsIn(blocked), "mic", started, []byte("audio"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create audio dir")

	require.NoError(t, os.MkdirAll(filepath.Join(paths.Audio, strconv.FormatInt(started, 10)+"-mic.webm"), 0o755))
	_, err = SaveLiveNoteAudio(paths, "mic", started, []byte("audio"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write audio")
}

func TestWriteLiveNoteChatFile_EmptyAndUnwritable(t *testing.T) {
	path, err := WriteLiveNoteChatFile(LiveNotesPathsIn(t.TempDir()), nil)
	require.NoError(t, err)
	assert.Empty(t, path, "no messages means no file")

	missing := filepath.Join(t.TempDir(), "gone", "meeting")
	_, err = WriteLiveNoteChatFile(LiveNotesPathsIn(missing), []LiveNoteChatMessage{{Text: "内容"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write chat archive")
}

func TestReadLiveNoteArtifact_ReportsUnreadableArtifacts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod does not restrict reads on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}

	project := t.TempDir()
	dir := "2026-10-30-1430-abc123"
	folder := filepath.Join(project, ".clawbench", "notes", dir)
	require.NoError(t, os.MkdirAll(folder, 0o755))

	t.Run("directory instead of artifact", func(t *testing.T) {
		require.NoError(t, os.MkdirAll(filepath.Join(folder, "minutes.md"), 0o755))
		_, _, err := ReadLiveNoteArtifactWithName(project, dir, "minutes")
		assert.ErrorIs(t, err, ErrLiveNoteSessionNotFound)
	})

	t.Run("unreadable file", func(t *testing.T) {
		file := filepath.Join(folder, "transcript.vtt")
		require.NoError(t, os.WriteFile(file, []byte("WEBVTT\n"), 0o644))
		require.NoError(t, os.Chmod(file, 0o000))
		t.Cleanup(func() { _ = os.Chmod(file, 0o644) })

		_, _, err := ReadLiveNoteArtifactWithName(project, dir, "transcript")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read artifact")
	})

	t.Run("unreadable folder", func(t *testing.T) {
		unreadable := filepath.Join(project, ".clawbench", "notes", "2026-10-30-1500-def456")
		require.NoError(t, os.MkdirAll(unreadable, 0o755))
		require.NoError(t, os.Chmod(unreadable, 0o000))
		t.Cleanup(func() { _ = os.Chmod(unreadable, 0o755) })

		_, _, err := ReadLiveNoteArtifactWithName(project, "2026-10-30-1500-def456", "revised")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stat artifact")
	})
}

func TestDeleteMeetingRecord_RefusesEmptyTokenAndEscapingFolder(t *testing.T) {
	setupTestDBForLiveNotes(t)
	project := t.TempDir()

	assert.ErrorIs(t, DeleteMeetingRecord(project, 1, ""), ErrMeetingNotFound)

	_, err := store.WriteExec(
		`INSERT INTO live_notes (token, project_id, dir_name, started_at) VALUES ('tok', 1, '../escape', 1)`,
	)
	require.NoError(t, err)

	err = DeleteMeetingRecord(project, 1, "tok")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to delete outside notes root")

	// A refused delete must not half-remove anything: the row is still there.
	note, ok, getErr := GetLiveNote("tok")
	require.NoError(t, getErr)
	require.True(t, ok)
	assert.Equal(t, "../escape", note.DirName)
}

func TestDeleteMeetingRecord_ReportsFolderThatCannotBeRemoved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod does not restrict deletion on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	setupTestDBForLiveNotes(t)
	project := t.TempDir()
	dirName := "2026-10-30-1430-abc123"
	root := filepath.Join(project, ".clawbench", "notes")
	require.NoError(t, os.MkdirAll(filepath.Join(root, dirName), 0o755))
	_, err := store.WriteExec(
		`INSERT INTO live_notes (token, project_id, dir_name, started_at) VALUES ('tok', 1, ?, 1)`, dirName,
	)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(root, 0o500))
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	err = DeleteMeetingRecord(project, 1, "tok")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remove meeting folder")

	// Rows survive so the user can retry once the permission is fixed.
	_, ok, getErr := GetLiveNote("tok")
	require.NoError(t, getErr)
	assert.True(t, ok)
}

func TestDeleteMeetingRecord_ReportsRowDeleteFailure(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	require.NoError(t, StartLiveNote("tok", 1, "标题", 1_700_000_000_000))
	dropLiveNoteTable(t, db, "live_notes_segments")

	err := DeleteMeetingRecord(t.TempDir(), 1, "tok")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete meeting rows")

	// The transaction rolled back: the record is still deletable later.
	_, ok, getErr := GetLiveNote("tok")
	require.NoError(t, getErr)
	assert.True(t, ok)
}

func TestEnsureMeetingDir_ReportsStorageFailure(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	require.NoError(t, db.Close())

	_, err := EnsureMeetingDir("tok", 1, t.TempDir(), "季度会", 1_700_000_000_000)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "live notes: dir lookup")
}

// ── Zero-value tolerance ────────────────────────────────────────────────────

// A zero timestamp means "now" everywhere, never 1970: the publisher may omit
// the clock, and a 1970 record would sort below every real meeting.
func TestLiveNotes_ZeroTimestampsMeanNow(t *testing.T) {
	setupTestDBForLiveNotes(t)

	require.NoError(t, EndLiveNote("", 1_000), "no token means nothing to end")
	require.NoError(t, CloseOpenLiveNoteFiles("", 1_000), "no token means nothing to close")
	require.NoError(t, CloseOpenLiveNoteScreenShare("", 1_000))

	assert.EqualError(t, StartLiveNote("", 1, "标题", 1), "live notes: empty token")

	before := time.Now().UnixMilli()
	require.NoError(t, StartLiveNote("tok", 1, "标题", 0))
	require.NoError(t, EndLiveNote("tok", 0))

	note, ok, err := GetLiveNote("tok")
	require.NoError(t, err)
	require.True(t, ok)
	assert.GreaterOrEqual(t, note.StartedAt, before)
	assert.GreaterOrEqual(t, note.EndedAt, note.StartedAt)
}

func TestCloseOpenLiveNoteFiles_ZeroTimeUsesNow(t *testing.T) {
	setupTestDBForLiveNotes(t)
	require.NoError(t, RecordLiveNoteFiles(context.Background(), "tok", 1,
		[]LiveNoteFile{{Path: "a.md", OpenedAt: 1_000}}))

	before := time.Now().UnixMilli()
	require.NoError(t, CloseOpenLiveNoteFiles("tok", 0))

	files, err := ListLiveNoteFiles("tok", 0)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.GreaterOrEqual(t, files[0].ClosedAt, before)
}

func TestLiveNoteGeneration_ZeroTimeMeansNow(t *testing.T) {
	setupTestDBForLiveNotes(t)
	require.NoError(t, SetLiveNoteMinutes("tok", 1, "", LiveNotesStatusGenerating, "", "", ""))

	running, err := LiveNoteGenerationRunning("tok", 0)
	require.NoError(t, err)
	assert.True(t, running, "a job written just now is still running")

	n, err := ResetStaleLiveNoteGeneration(0)
	require.NoError(t, err)
	assert.Zero(t, n, "a fresh job is not abandoned")

	running, err = LiveNoteGenerationRunning("", 0)
	require.NoError(t, err)
	assert.False(t, running, "no token means no job to look for")

	closed, err := CloseStaleLiveNotes(0)
	require.NoError(t, err)
	assert.Zero(t, closed, "no cutoff means nothing is stale")
}

// ── Input validation ────────────────────────────────────────────────────────

func TestRecordLiveNoteFiles_EmptyInputIsANoop(t *testing.T) {
	setupTestDBForLiveNotes(t)

	assert.EqualError(t,
		RecordLiveNoteFiles(context.Background(), "", 1, []LiveNoteFile{{Path: "a.md", OpenedAt: 1}}),
		"live notes: empty token")
	require.NoError(t, RecordLiveNoteFiles(context.Background(), "tok", 1, nil))

	// An empty batch must not register a meeting: nothing happened.
	_, ok, err := GetLiveNote("tok")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestAppendLiveNoteChat_RejectsIncompleteMessages(t *testing.T) {
	setupTestDBForLiveNotes(t)

	for _, msg := range []LiveNoteChatMessage{
		{ClientID: "c1", Text: "   "},
		{Text: "内容"},
	} {
		_, err := AppendLiveNoteChat("tok", 1, msg)
		assert.EqualError(t, err, "live notes: invalid chat message")
	}
	_, err := AppendLiveNoteChat("", 1, LiveNoteChatMessage{ClientID: "c1", Text: "内容"})
	assert.EqualError(t, err, "live notes: invalid chat message")
}

// ── Remaining zero-value and chunking paths ─────────────────────────────────

func TestSplitRunes_CutsOnANewlineNearTheBoundary(t *testing.T) {
	text := strings.Repeat(strings.Repeat("a", 10)+"\n", 100)
	chunks := splitRunes(text, 100)

	require.Greater(t, len(chunks), 1)
	for i, c := range chunks {
		assert.LessOrEqual(t, len([]rune(c)), 100, "chunk %d must fit the budget", i)
	}
	assert.Equal(t, text, strings.Join(chunks, ""), "nothing may be lost")
}

// A long transcript does not fit in one request: every chunk is proofread on
// its own, and each request says which part it is.
func TestReviseLiveNotesTranscript_ChunksALongTranscript(t *testing.T) {
	long := strings.Repeat("很长的发言内容。", 400)
	segments := []LiveNoteSegment{
		seg(1, "mic", "我", 1_700_000_000_000, long),
		seg(2, "mic", "我", 1_700_000_060_000, long),
	}
	require.Greater(t, len([]rune(BuildLiveNotesTranscript(segments))), liveNotesReviseChunkRunes,
		"fixture must exceed one chunk or the chunk path is not exercised")

	bodies := captureSummaryBodies(t, "[00:00] 我：第一段", "[01:00] 我：第二段")
	got, err := ReviseLiveNotesTranscript(context.Background(), "", "", segments, LiveNotesSpeaker{})
	require.NoError(t, err)
	assert.Equal(t, "[00:00] 我：第一段\n[01:00] 我：第二段", got)

	requests := bodies()
	require.Len(t, requests, 2)
	assert.Contains(t, requests[0], "这是第 1/2 段")
	assert.Contains(t, requests[1], "这是第 2/2 段")
}

func TestReviseLiveNotesTranscript_ChunkErrorIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	withAISummary(t, srv.URL)

	long := strings.Repeat("很长的发言内容。", 400)
	_, err := ReviseLiveNotesTranscript(context.Background(), "", "", []LiveNoteSegment{
		seg(1, "mic", "我", 1, long),
		seg(2, "mic", "我", 60_000, long),
	}, LiveNotesSpeaker{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "live notes: revise chunk")
}

// A reply that hit the output cap is a prefix, not a shorter proofread: the
// tail lines are gone. It must fail the stage so the minutes fall back to the
// raw transcript instead of summarizing a truncated copy.
func TestReviseLiveNotesTranscript_TruncatedChunkIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"[00:00] 我：内容"},"finish_reason":"length"}]}`))
	}))
	defer srv.Close()
	withAISummary(t, srv.URL)

	_, err := ReviseLiveNotesTranscript(context.Background(), "", "", []LiveNoteSegment{
		seg(1, "mic", "我", 1, "内容"),
	}, LiveNotesSpeaker{})
	require.Error(t, err)
	assert.ErrorIs(t, err, summarize.ErrOutputTruncated)
}

func TestGenerateLiveNotesMinutes_ChunkErrorIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	withAISummary(t, srv.URL)

	long := strings.Repeat("很长的发言内容。", 400)
	segments := make([]LiveNoteSegment, 0, 8)
	for i := range 8 {
		segments = append(segments, seg(int64(i+1), "mic", "我", int64(i)*60_000, long))
	}
	_, err := GenerateLiveNotesMinutes(context.Background(), "", BuildLiveNotesTranscript(segments), "", LiveNotesSpeaker{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "live notes: chunk")
}

// The file timeline is appended after the map-reduce, never inside it: in a
// long meeting a section in one chunk would be summarized away.
func TestGenerateLiveNotesMinutes_AppendsTheFileSection(t *testing.T) {
	bodies := captureSummaryBodies(t, "## 概要\n\n确认了发布计划。")

	_, err := GenerateLiveNotesMinutes(context.Background(), "季度会", "[00:00] 我：内容",
		"## 会议期间打开的文件\n\n- 14:30 docs/a.md", LiveNotesSpeaker{})
	require.NoError(t, err)

	requests := bodies()
	require.Len(t, requests, 1)
	assert.Contains(t, requests[0], "docs/a.md")
}

func TestLiveNoteFilesPromptSection_IncludesExcerpts(t *testing.T) {
	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, "a.md"), []byte("# 发布计划"), 0o644))
	started := time.UnixMilli(1_700_000_000_000)

	got := LiveNoteFilesPromptSection([]LiveNoteFile{
		{Path: "a.md", OpenedAt: started.UnixMilli()},
	}, started, project)
	assert.Contains(t, got, "文件内容摘录")
	assert.Contains(t, got, "发布计划")
}

// A file event with no timestamp and no label has nothing to render; it must be
// dropped from the track rather than produce an empty cue.
func TestBuildLiveNotesVTT_FileEventWithoutOpenTimeIsSkipped(t *testing.T) {
	got := BuildLiveNotesVTT([]LiveNoteSegment{
		{Channel: "mic", Speaker: "我", StartedAt: 1_700_000_000_000, DurationMS: 1000, Text: "开始"},
	}, []LiveNoteFile{{Path: "a.md"}}, nil)

	assert.Contains(t, got, "开始")
	assert.NotContains(t, got, "打开")
}

func TestSaveLiveNoteAudio_ZeroStartUsesNow(t *testing.T) {
	paths := LiveNotesPathsIn(t.TempDir())
	before := time.Now().UnixMilli()

	path, err := SaveLiveNoteAudio(paths, "mic", 0, []byte("audio"))
	require.NoError(t, err)

	ms, err := strconv.ParseInt(strings.SplitN(filepath.Base(path), "-", 2)[0], 10, 64)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, ms, before)
}

func TestWriteLiveNotesRevisedTranscript_RequiresADir(t *testing.T) {
	_, err := WriteLiveNotesRevisedTranscript(LiveNotesPathsIn(""), "标题", time.Now(), "[00:00] 我：内容")
	assert.EqualError(t, err, "live notes: no project path")
}

func TestEnsureMeetingDir_ZeroStartIsDatedToday(t *testing.T) {
	setupTestDBForLiveNotes(t)

	paths, err := EnsureMeetingDir("tok", 1, t.TempDir(), "季度会", 0)
	require.NoError(t, err)
	assert.DirExists(t, paths.Dir)
	assert.NotContains(t, filepath.Base(paths.Dir), "1970-01-01",
		"a missing clock means now, not the epoch")
}

func TestCloseOrphanedLiveNotes_ZeroTimeMeansNow(t *testing.T) {
	setupTestDBForLiveNotes(t)

	n, err := CloseOrphanedLiveNotes(0)
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestReadLiveNoteArtifact_ChatArchive(t *testing.T) {
	project := t.TempDir()
	dir := "2026-10-30-1430-abc123"
	folder := filepath.Join(project, ".clawbench", "notes", dir)
	require.NoError(t, os.MkdirAll(folder, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(folder, "chat.md"), []byte("# 会议聊天记录\n"), 0o644))

	body, name, err := ReadLiveNoteArtifactWithName(project, dir, "chat")
	require.NoError(t, err)
	assert.Equal(t, "chat.md", name)
	assert.Contains(t, body, "会议聊天记录")
}

func TestLiveNotesFileSlug_DropsControlCharacters(t *testing.T) {
	assert.Equal(t, "季度 规划", liveNotesFileSlug("季度\x01 规划"))
	assert.Equal(t, "a b", liveNotesFileSlug("a\tb"))
}
