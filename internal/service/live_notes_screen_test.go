package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "Who is sharing" is a chain of intervals: the next sharer closes the previous
// one in the same write, so the timeline can never show two people at once.
func TestSetLiveNoteScreenSharer_ClosesPreviousAndOpensNext(t *testing.T) {
	setupTestDBForLiveNotes(t)
	ctx := context.Background()
	base := int64(1_700_000_000_000)

	require.NoError(t, SetLiveNoteScreenSharer(ctx, "tok", 7, "host", "", base))
	require.NoError(t, SetLiveNoteScreenSharer(ctx, "tok", 7, "viewer:v3", "张三", base+60_000))
	require.NoError(t, SetLiveNoteScreenSharer(ctx, "tok", 7, "", "", base+120_000))

	screens, err := ListLiveNoteScreenShares("tok")
	require.NoError(t, err)
	require.Len(t, screens, 2)
	assert.Equal(t, "host", screens[0].Sharer)
	assert.Equal(t, base+60_000, screens[0].EndedAt)
	assert.Equal(t, "viewer:v3", screens[1].Sharer)
	assert.Equal(t, "张三", screens[1].SharerName)
	assert.Equal(t, base+60_000, screens[1].StartedAt)
	assert.Equal(t, base+120_000, screens[1].EndedAt)
}

// A reconnect or a duplicate watch reports the same sharer again; that must not
// split the interval into two.
func TestSetLiveNoteScreenSharer_RepeatedSharerDoesNotSplit(t *testing.T) {
	setupTestDBForLiveNotes(t)
	ctx := context.Background()
	base := int64(1_700_000_000_000)

	require.NoError(t, SetLiveNoteScreenSharer(ctx, "tok", 7, "host", "", base))
	require.NoError(t, SetLiveNoteScreenSharer(ctx, "tok", 7, "host", "", base+5000))

	screens, err := ListLiveNoteScreenShares("tok")
	require.NoError(t, err)
	require.Len(t, screens, 1)
	assert.Equal(t, int64(0), screens[0].EndedAt)
}

func TestSetLiveNoteScreenSharer_EmptyWithNothingOpenIsNoop(t *testing.T) {
	setupTestDBForLiveNotes(t)
	ctx := context.Background()

	require.NoError(t, SetLiveNoteScreenSharer(ctx, "tok", 7, "", "", 1000))
	screens, err := ListLiveNoteScreenShares("tok")
	require.NoError(t, err)
	assert.Empty(t, screens)
}

func TestCloseOpenLiveNoteScreenShare_StampsOnlyTheRunningOne(t *testing.T) {
	setupTestDBForLiveNotes(t)
	ctx := context.Background()
	base := int64(1_700_000_000_000)

	require.NoError(t, SetLiveNoteScreenSharer(ctx, "tok", 7, "host", "", base))
	require.NoError(t, SetLiveNoteScreenSharer(ctx, "tok", 7, "viewer:v1", "小明", base+1000))
	require.NoError(t, CloseOpenLiveNoteScreenShare("tok", base+9000))

	screens, err := ListLiveNoteScreenShares("tok")
	require.NoError(t, err)
	require.Len(t, screens, 2)
	assert.Equal(t, base+1000, screens[0].EndedAt, "an observed switch keeps its own end")
	assert.Equal(t, base+9000, screens[1].EndedAt)
}

func TestBuildLiveNotesVTT_CarriesScreenEvents(t *testing.T) {
	base := time.Date(2026, 10, 30, 14, 30, 0, 0, time.Local).UnixMilli()
	got := BuildLiveNotesVTT([]LiveNoteSegment{
		{Channel: "mic", Speaker: "我", StartedAt: base, DurationMS: 4000, Text: "我先共享一下"},
	}, nil, []LiveNoteScreenShare{
		{Sharer: "host", StartedAt: base + 5000, EndedAt: base + 65_000},
	})

	assert.Contains(t, got, "screen\n00:00:05.000 --> 00:00:07.000\n<v 屏幕>主持人 开始共享屏幕\n")
	assert.Contains(t, got, "screen\n00:01:05.000 --> 00:01:07.000\n<v 屏幕>主持人 停止共享屏幕（1 分钟）\n")
}

func TestBuildLiveNotesVTT_ScreenUsesTheParticipantName(t *testing.T) {
	base := int64(1_700_000_000_000)
	got := BuildLiveNotesVTT([]LiveNoteSegment{
		{Channel: "mic", Speaker: "我", StartedAt: base, DurationMS: 1000, Text: "开始"},
	}, nil, []LiveNoteScreenShare{
		{Sharer: "viewer:v2", SharerName: "张三", StartedAt: base + 2000},
	})
	assert.Contains(t, got, "<v 屏幕>张三 开始共享屏幕")
	assert.NotContains(t, got, "停止")
}

func TestMergeLiveNoteFileEvents_InsertsScreenEvents(t *testing.T) {
	base := int64(1_700_000_000_000)
	revised := "[00:00] 我：先共享屏幕\n[01:00] 张三：看这里"

	got := MergeLiveNoteFileEvents(revised, nil, []LiveNoteScreenShare{
		{Sharer: "host", StartedAt: base + 30_000, EndedAt: base + 90_000},
	}, base)

	lines := strings.Split(got, "\n")
	require.Len(t, lines, 4)
	assert.Equal(t, "[00:00] 我：先共享屏幕", lines[0])
	assert.Equal(t, "[00:30] 屏幕：主持人 开始共享屏幕", lines[1])
	assert.Equal(t, "[01:00] 张三：看这里", lines[2])
	assert.Equal(t, "[01:30] 屏幕：主持人 停止共享屏幕（1 分钟）", lines[3])
}

func TestLiveNoteScreenPromptSection_ListsWhoSharedWhen(t *testing.T) {
	started := time.Date(2026, 10, 30, 14, 30, 0, 0, time.Local)
	base := started.UnixMilli()

	got := LiveNoteScreenPromptSection([]LiveNoteScreenShare{
		{Sharer: "host", StartedAt: base, EndedAt: base + 8*60_000},
		{Sharer: "viewer:v1", SharerName: "张三", StartedAt: base + 10*60_000},
	}, started)

	assert.Contains(t, got, "## 屏幕共享")
	assert.Contains(t, got, "- 14:30–14:38 主持人 共享屏幕")
	assert.Contains(t, got, "- 14:40 起 张三 共享屏幕")
	assert.Empty(t, LiveNoteScreenPromptSection(nil, started))
}

func TestSetLiveNoteScreenSharer_EmptyTokenAndZeroTime(t *testing.T) {
	setupTestDBForLiveNotes(t)
	ctx := context.Background()

	assert.EqualError(t, SetLiveNoteScreenSharer(ctx, "", 7, "host", "", 1000), "live notes: empty token")

	// A zero timestamp means "now": the client may omit the clock, and a 1970
	// interval would sort below every real one.
	before := time.Now().UnixMilli()
	require.NoError(t, SetLiveNoteScreenSharer(ctx, "tok", 7, "host", "", 0))

	screens, err := ListLiveNoteScreenShares("tok")
	require.NoError(t, err)
	require.Len(t, screens, 1)
	assert.GreaterOrEqual(t, screens[0].StartedAt, before)
}

// A transition stamped before the interval it closes (a skewed client clock)
// must not produce an interval that ends before it starts.
func TestSetLiveNoteScreenSharer_ClampsTheEndToTheStart(t *testing.T) {
	setupTestDBForLiveNotes(t)
	ctx := context.Background()
	base := int64(1_700_000_000_000)

	require.NoError(t, SetLiveNoteScreenSharer(ctx, "tok", 7, "host", "", base))
	require.NoError(t, SetLiveNoteScreenSharer(ctx, "tok", 7, "viewer:v1", "小明", base-5_000))

	screens, err := ListLiveNoteScreenShares("tok")
	require.NoError(t, err)
	require.Len(t, screens, 2)

	bySharer := map[string]LiveNoteScreenShare{}
	for _, s := range screens {
		bySharer[s.Sharer] = s
	}
	assert.Equal(t, base, bySharer["host"].EndedAt, "the closed interval cannot end before it began")
	assert.GreaterOrEqual(t, bySharer["host"].EndedAt, bySharer["host"].StartedAt)
	assert.Equal(t, base-5_000, bySharer["viewer:v1"].StartedAt, "the new interval keeps the reported clock")
}

func TestCloseOpenLiveNoteScreenShare_EmptyTokenAndZeroTime(t *testing.T) {
	setupTestDBForLiveNotes(t)
	ctx := context.Background()

	require.NoError(t, CloseOpenLiveNoteScreenShare("", 1000), "no token means nothing to close")
	require.NoError(t, SetLiveNoteScreenSharer(ctx, "tok", 7, "host", "", 1000))

	before := time.Now().UnixMilli()
	require.NoError(t, CloseOpenLiveNoteScreenShare("tok", 0))

	screens, err := ListLiveNoteScreenShares("tok")
	require.NoError(t, err)
	require.Len(t, screens, 1)
	assert.GreaterOrEqual(t, screens[0].EndedAt, before)
}

// An interval with no start cannot be placed on a timeline, so it contributes
// no cue at all rather than one at minute zero.
func TestBuildLiveNotesVTT_ScreenWithoutStartIsSkipped(t *testing.T) {
	got := BuildLiveNotesVTT([]LiveNoteSegment{
		{Channel: "mic", Speaker: "我", StartedAt: 1_700_000_000_000, DurationMS: 1000, Text: "开始"},
	}, nil, []LiveNoteScreenShare{{Sharer: "host"}})

	assert.Contains(t, got, "开始")
	assert.NotContains(t, got, "共享屏幕")
}

// A viewer who never typed a name is still identifiable by id; the host gets a
// role label instead of the literal "host".
func TestLiveNoteScreenName_FallsBackToTheRoleOrTheID(t *testing.T) {
	assert.Equal(t, "主持人", liveNoteScreenName(LiveNoteScreenShare{Sharer: "host"}))
	assert.Equal(t, "viewer:v2", liveNoteScreenName(LiveNoteScreenShare{Sharer: "viewer:v2"}))
	assert.Equal(t, "张三", liveNoteScreenName(LiveNoteScreenShare{Sharer: "viewer:v2", SharerName: " 张三 "}))
}
