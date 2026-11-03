package service

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Glossary ────────────────────────────────────────────────────────────────

func TestLiveNoteGlossary_LiftsTheMeetingsOwnVocabulary(t *testing.T) {
	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, "README.md"), []byte(
		"# ClawBench\n\nWe use `useLiveNotes` and the ACP adapter. The meeting is about 发布流程.\n",
	), 0o644))

	got := LiveNoteGlossaryPromptSection(
		"发布流程复盘",
		[]LiveNoteFile{{Path: "internal/service/live_notes.go"}},
		[]LiveNoteSegment{
			seg(1, "mic", "泷泽", 1_000, "好"),
			seg(2, "viewer:v2", "张三", 2_000, "嗯"),
		},
		project,
	)
	require.NotEmpty(t, got)
	assert.Contains(t, got, "发布流程复盘", "the title is a domain term")
	assert.Contains(t, got, "泷泽")
	assert.Contains(t, got, "张三")
	assert.Contains(t, got, "live_notes.go", "an opened file is the closest vocabulary there is")
	assert.Contains(t, got, "ClawBench")
	assert.Contains(t, got, "useLiveNotes", "a code span is a name, not prose")
	assert.Contains(t, got, "ACP", "an acronym is exactly what ASR mangles")
}

// A placeholder title is a timestamp, a pronoun label is not a name, and a
// capitalised English word at the start of a sentence is prose. None of them may
// reach the glossary: it is a spelling reference, and noise dilutes it.
func TestLiveNoteGlossary_SkipsPlaceholdersAndProse(t *testing.T) {
	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, "README.md"), []byte(
		"The project is small. We meet weekly.\n",
	), 0o644))

	got := LiveNoteGlossaryPromptSection(
		"11-01 17:35",
		nil,
		[]LiveNoteSegment{
			seg(1, "mic", "我", 1_000, "好"),
			seg(2, "viewer:v2", "观众 2", 2_000, "嗯"),
		},
		project,
	)
	assert.Empty(t, got, "nothing in this meeting carries a spelling worth fixing")
}

func TestLiveNoteGlossary_EmptyWhenNothingToSay(t *testing.T) {
	assert.Empty(t, LiveNoteGlossaryPromptSection("", nil, nil, ""))
}

func TestLiveNoteGlossary_BoundsTheBlock(t *testing.T) {
	terms := make([]LiveNoteFile, 0, 200)
	for i := range 200 {
		terms = append(terms, LiveNoteFile{Path: "pkg/module_" + strings.Repeat("x", 3) + "_" + strconv.Itoa(i) + ".go"})
	}
	got := LiveNoteGlossaryPromptSection("", terms, nil, "")
	require.NotEmpty(t, got)
	assert.LessOrEqual(t, len(strings.Split(strings.TrimSpace(got), "\n")), liveNoteGlossaryMaxTerms+3,
		"the list is capped so it cannot crowd out the transcript")
}

func TestLiveNoteDocTerms_IgnoresCommandsAndURLs(t *testing.T) {
	terms := liveNoteDocTerms("run `go test ./...` then read https://example.com/ClawBench and `ACP`")
	assert.Contains(t, terms, "ACP")
	assert.NotContains(t, terms, "go test ./...")
	assert.NotContains(t, terms, "https://example.com/ClawBench")
}

// ── Meeting info ────────────────────────────────────────────────────────────

func TestLiveNoteMeetingInfo_RendersTimeRosterAndInitiator(t *testing.T) {
	start := time.Date(2026, 11, 3, 14, 0, 0, 0, time.Local)
	info := LiveNoteMeetingInfo{
		ProjectName: "clawbench",
		StartedAt:   start.UnixMilli(),
		EndedAt:     start.Add(47 * time.Minute).UnixMilli(),
		Segments: []LiveNoteSegment{
			seg(1, "mic", "泷泽", start.UnixMilli(), "开场"),
			seg(2, "viewer:v2", "张三", start.Add(time.Minute).UnixMilli(), "好"),
			seg(3, "viewer:v2", "张三", start.Add(2*time.Minute).UnixMilli(), "还有"),
		},
		Screens: []LiveNoteScreenShare{{Sharer: "host", SharerName: "泷泽", StartedAt: start.UnixMilli()}},
		Self:    LiveNotesSpeaker{Label: "泷泽", Name: "泷泽"},
	}

	got := info.PromptSection()
	require.NotEmpty(t, got)
	assert.Contains(t, got, "2026-11-03 14:00–14:47", "the span, not just the start")
	assert.Contains(t, got, "47 分钟", "the duration is what lets the model say how long it took")
	assert.Contains(t, got, "项目：clawbench")
	assert.Contains(t, got, "泷泽（发起人", "the initiator is marked")
	assert.Contains(t, got, "张三（发言 2 段）")
	// Busiest first: 张三 spoke twice, 泷泽 once.
	assert.Less(t, strings.Index(got, "张三"), strings.Index(got, "泷泽（"))
}

func TestLiveNoteMeetingInfo_SaysWhenTheMeetingIsStillRunning(t *testing.T) {
	start := time.Date(2026, 11, 3, 14, 0, 0, 0, time.Local)
	got := LiveNoteMeetingInfo{StartedAt: start.UnixMilli()}.PromptSection()
	assert.Contains(t, got, "仍在进行", "a running meeting has no duration to report")
	assert.NotContains(t, got, "共 ")
}

func TestLiveNoteMeetingInfo_SilentSharerIsStillPresent(t *testing.T) {
	start := time.Date(2026, 11, 3, 14, 0, 0, 0, time.Local)
	got := LiveNoteMeetingInfo{
		StartedAt: start.UnixMilli(),
		EndedAt:   start.Add(10 * time.Minute).UnixMilli(),
		Segments:  []LiveNoteSegment{seg(1, "mic", "泷泽", start.UnixMilli(), "看这个")},
		Screens:   []LiveNoteScreenShare{{Sharer: "v9", SharerName: "李四", StartedAt: start.UnixMilli()}},
	}.PromptSection()
	assert.Contains(t, got, "李四（共享过屏幕）", "a meeting where someone presented silently still had them in it")
}

func TestLiveNoteMeetingInfo_EmptyWhenNothingIsKnown(t *testing.T) {
	assert.Empty(t, LiveNoteMeetingInfo{}.PromptSection())
}

func TestLiveNoteMeetingInfo_RosterIsCapped(t *testing.T) {
	segments := make([]LiveNoteSegment, 0, 40)
	for i := range 40 {
		segments = append(segments, seg(int64(i+1), "viewer:v", "成员"+strconv.Itoa(i), 1_000, "好"))
	}
	got := LiveNoteMeetingInfo{StartedAt: 1_000, Segments: segments}.PromptSection()
	assert.LessOrEqual(t, strings.Count(got, "、")+1, liveNoteMaxParticipants)
}

// ── Key frames ──────────────────────────────────────────────────────────────

func TestLiveNoteShotsPromptSection_RendersTimeline(t *testing.T) {
	start := time.Date(2026, 11, 3, 14, 0, 0, 0, time.Local)
	shots := []LiveNoteShot{
		{At: start.Add(12 * time.Second).UnixMilli(), Reason: LiveNoteShotReasonChange},
		{At: start.Add(65 * time.Second).UnixMilli(), Reason: LiveNoteShotReasonFile, Ref: "internal/service/live_notes.go"},
		{At: start.Add(90 * time.Second).UnixMilli(), Reason: LiveNoteShotReasonSharer},
	}
	got := LiveNoteShotsPromptSection(shots)
	require.NotEmpty(t, got)
	assert.Contains(t, got, "- 14:00 画面变化")
	assert.Contains(t, got, "- 14:01 打开文件 · live_notes.go")
	assert.Contains(t, got, "成员共享")
	assert.Contains(t, got, "不是发言内容", "a frame is not a statement")
}

func TestLiveNoteShotsPromptSection_SamplesLongMeetings(t *testing.T) {
	start := time.Date(2026, 11, 3, 14, 0, 0, 0, time.Local)
	shots := make([]LiveNoteShot, 0, 100)
	for i := range 100 {
		shots = append(shots, LiveNoteShot{
			At:     start.Add(time.Duration(i) * 10 * time.Second).UnixMilli(),
			Reason: LiveNoteShotReasonChange,
		})
	}
	got := LiveNoteShotsPromptSection(shots)
	assert.Equal(t, liveNoteShotPromptMax, strings.Count(got, "\n- "), "the timeline is sampled, not dumped")
	assert.Contains(t, got, "共 100 个画面")
	assert.Contains(t, got, "- 14:00", "the first frame survives the sample")
	assert.Contains(t, got, "- 14:16", "and so does the last")
}

func TestLiveNoteShotsPromptSection_Empty(t *testing.T) {
	assert.Empty(t, LiveNoteShotsPromptSection(nil))
}

// ── Chat ────────────────────────────────────────────────────────────────────

func TestLiveNoteChatPromptSection_RendersLines(t *testing.T) {
	start := time.Date(2026, 11, 3, 14, 0, 0, 0, time.Local)
	got := LiveNoteChatPromptSection([]LiveNoteChatMessage{
		{Speaker: "张三", Text: "这条先合了吧", At: start.Add(12 * time.Second).UnixMilli()},
		{Speaker: "", Text: "多行\n文本", At: start.Add(70 * time.Second).UnixMilli()},
	})
	require.NotEmpty(t, got)
	assert.Contains(t, got, "14:00 张三：这条先合了吧")
	assert.Contains(t, got, "14:01 ", "an anonymous line still gets a timestamp")
	assert.Contains(t, got, "多行 文本", "a newline inside a message must not break the line format")
	assert.Contains(t, got, "同属会议内容")
}

func TestLiveNoteChatPromptSection_CapsAndSaysSo(t *testing.T) {
	start := time.Date(2026, 11, 3, 14, 0, 0, 0, time.Local)
	msgs := make([]LiveNoteChatMessage, 0, liveNoteChatPromptMaxMessages+10)
	for i := range liveNoteChatPromptMaxMessages + 10 {
		msgs = append(msgs, LiveNoteChatMessage{
			Speaker: "张三",
			Text:    "收到",
			At:      start.Add(time.Duration(i) * time.Second).UnixMilli(),
		})
	}
	got := LiveNoteChatPromptSection(msgs)
	assert.Contains(t, got, "以上为前 "+strconv.Itoa(liveNoteChatPromptMaxMessages)+" 条")
}

func TestLiveNoteChatPromptSection_Empty(t *testing.T) {
	assert.Empty(t, LiveNoteChatPromptSection(nil))
}

// ── Previous meeting ────────────────────────────────────────────────────────

func TestLiveNotePreviousTodos_ExtractsOnlyTheActionItems(t *testing.T) {
	prev := LiveNote{
		Title:     "发布复盘",
		StartedAt: time.Date(2026, 10, 30, 10, 0, 0, 0, time.Local).UnixMilli(),
		Minutes:   "## 概要\n上次聊了发版。\n\n## 待办事项\n- 补齐 npm 载荷包\n- 修 Windows 通知\n\n## 结论与决定\n- 先发 beta\n",
	}
	got := LiveNotePreviousTodosPromptSection(prev)
	require.NotEmpty(t, got)
	assert.Contains(t, got, "2026-10-30")
	assert.Contains(t, got, "《发布复盘》")
	assert.Contains(t, got, "补齐 npm 载荷包")
	assert.NotContains(t, got, "上次聊了发版", "only the action items travel, not the whole summary")
	assert.NotContains(t, got, "先发 beta", "the next section must not leak in")
	assert.Contains(t, got, "不是本次会议的发言", "the block must be framed as background")
}

func TestLiveNotePreviousTodos_NoSectionMeansNoBlock(t *testing.T) {
	prev := LiveNote{Minutes: "## 概要\n只有一段概要。\n"}
	assert.Empty(t, LiveNotePreviousTodosPromptSection(prev))
}

func TestLiveNotePreviousTodos_AcceptsHeadingVariants(t *testing.T) {
	for _, heading := range []string{"## 行动项", "## Action Items", "## TODO", "## 遗留事项"} {
		prev := LiveNote{Minutes: heading + "\n- 一件事\n"}
		assert.NotEmpty(t, LiveNotePreviousTodosPromptSection(prev), heading)
	}
}

func TestPreviousLiveNoteMinutes_PicksTheLatestEarlierMeeting(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	defer func() { _ = db.Close() }()

	insert := func(token string, projectID, startedAt int64, minutes string) {
		_, err := db.Exec(
			`INSERT INTO live_notes (token, project_id, title, minutes, started_at) VALUES (?, ?, ?, ?, ?)`,
			token, projectID, "会 "+token, minutes, startedAt,
		)
		require.NoError(t, err)
	}
	insert("old", 7, 1_000, "## 待办事项\n- 旧事")
	insert("newer", 7, 3_000, "## 待办事项\n- 新事")
	insert("current", 7, 5_000, "## 待办事项\n- 本次")
	insert("other-project", 8, 4_000, "## 待办事项\n- 别的项目")
	insert("empty", 7, 4_500, "")

	prev, ok, err := PreviousLiveNoteMinutes(7, 5_000, "current")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "newer", prev.Token, "the nearest earlier meeting with minutes, in this project only")

	// Nothing earlier: the first meeting of a project has no history.
	_, ok, err = PreviousLiveNoteMinutes(7, 1_000, "old")
	require.NoError(t, err)
	assert.False(t, ok)

	// A project id of 0 is the global sentinel, not a project.
	_, ok, err = PreviousLiveNoteMinutes(0, 9_999, "x")
	require.NoError(t, err)
	assert.False(t, ok)
}

// ── Assembly ────────────────────────────────────────────────────────────────

func TestLiveNoteMinutesContext_OrdersFramingContentAndHistory(t *testing.T) {
	base := int64(1_700_000_000_000)
	got := LiveNoteMinutesContext{
		Info: LiveNoteMeetingInfo{
			StartedAt:   base,
			EndedAt:     base + 600_000,
			ProjectPath: t.TempDir(),
			Segments:    []LiveNoteSegment{seg(1, "mic", "泷泽", base, "开场")},
		},
		Chat:    []LiveNoteChatMessage{{Speaker: "张三", Text: "收到", At: base + 60_000}},
		Files:   []LiveNoteFile{{Path: "/proj/a/main.go", OpenedAt: base + 30_000}},
		Shots:   []LiveNoteShot{{At: base + 20_000, Reason: LiveNoteShotReasonChange}},
		Prev:    LiveNote{Title: "上次", Minutes: "## 待办事项\n- 一件事"},
		HasPrev: true,
	}.PromptSection()

	require.NotEmpty(t, got)
	order := []string{"## 会议信息", "## 会议聊天记录", "## 会议期间打开的文件", "## 共享画面", "## 上一次会议的待办事项"}
	last := -1
	for _, heading := range order {
		idx := strings.Index(got, heading)
		require.GreaterOrEqual(t, idx, 0, heading)
		assert.Greater(t, idx, last, "%s is out of order", heading)
		last = idx
	}
}

func TestLiveNoteMinutesContext_EmptyWhenTheMeetingHasNothingElse(t *testing.T) {
	assert.Empty(t, LiveNoteMinutesContext{}.PromptSection())
}

func TestLiveNoteShotReasonText_CoversEveryTrigger(t *testing.T) {
	want := map[string]string{
		LiveNoteShotReasonManual: "手动截图",
		LiveNoteShotReasonFile:   "打开文件",
		LiveNoteShotReasonSharer: "成员共享",
		LiveNoteShotReasonSwitch: "切换共享",
		LiveNoteShotReasonSpeech: "有人发言",
		LiveNoteShotReasonChange: "画面变化",
	}
	seen := map[string]bool{}
	for reason, label := range want {
		assert.Equal(t, label, liveNoteShotReasonText(reason), reason)
		assert.False(t, seen[label], "two triggers must not read the same: %s", label)
		seen[label] = true
	}
	// A frame stored by a newer client still has to render as something.
	assert.Equal(t, "画面变化", liveNoteShotReasonText("from-the-future"))
}

func TestLiveNoteLooksLikeTerm_RejectsThingsThatAreNotNames(t *testing.T) {
	cases := []struct {
		term string
		want bool
	}{
		{"ACP", true},
		{"useLiveNotes", true},
		{"internal/service/live_notes.go", true},
		{"https://example.com/x", false},
		{"./relative/path", false},
		{"/absolute/path", false},
		{"go test ./...", false},
		{"中文短语", false},
		{"", false},
		{strings.Repeat("a", 41), false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, liveNoteLooksLikeTerm(tc.term), tc.term)
	}
}
