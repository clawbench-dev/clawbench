package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

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
	segments := []LiveNoteSegment{
		seg(3, "viewer:v2", "观众 2", 20_000, "我同意"),
		seg(1, "system", "系统声音", 0, "今天讨论发布计划"),
		seg(2, "mic", "我", 10_000, "先确认一下时间"),
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
	assert.Equal(t,
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

	require.NoError(t, SetLiveNoteMinutes("tok", pid, "标题", LiveNotesStatusGenerating, "", ""))
	note, ok, err := GetLiveNote("tok")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, LiveNotesStatusGenerating, note.Status)
	assert.Equal(t, "标题", note.Title)

	// Ready overwrites the status and stores the markdown.
	require.NoError(t, SetLiveNoteMinutes("tok", pid, "", LiveNotesStatusReady, "## 概要", ""))
	note, _, err = GetLiveNote("tok")
	require.NoError(t, err)
	assert.Equal(t, LiveNotesStatusReady, note.Status)
	assert.Equal(t, "## 概要", note.Minutes)
	assert.Equal(t, "标题", note.Title, "an empty title must not clear the stored one")
}

func TestCleanupLiveNotes_RemovesSegmentsWithTheShare(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()

	pid, err := store.ProjectIDForPath("/proj/a")
	require.NoError(t, err)
	_, err = AppendLiveNoteSegment(context.Background(), "old", pid, seg(0, "mic", "我", 1_000, "旧"))
	require.NoError(t, err)
	_, err = AppendLiveNoteSegment(context.Background(), "new", pid, seg(0, "mic", "我", 2_000, "新"))
	require.NoError(t, err)

	removed, err := CleanupLiveNotes(context.Background(), time.Now().Add(time.Minute))
	require.NoError(t, err)
	assert.Equal(t, 2, removed)

	rows, err := ListLiveNoteSegments("old", 0)
	require.NoError(t, err)
	assert.Empty(t, rows)
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
