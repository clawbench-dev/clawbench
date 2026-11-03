package service

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The interval is the point: a meeting record has to answer "when did they look
// at this", and a bare open timestamp cannot.
func TestRecordLiveNoteFiles_StoresIntervals(t *testing.T) {
	setupTestDBForLiveNotes(t)
	ctx := context.Background()
	base := int64(1_700_000_000_000)

	require.NoError(t, RecordLiveNoteFiles(ctx, "tok", 7, []LiveNoteFile{
		{Owner: "host", Path: "docs/a.md", OpenedAt: base, ClosedAt: base + 60_000, Source: "preview"},
		{Owner: "host", Path: "docs/b.go", OpenedAt: base + 90_000, Source: "editor"},
	}))

	files, err := ListLiveNoteFiles("tok", 0)
	require.NoError(t, err)
	require.Len(t, files, 2)
	assert.Equal(t, "docs/a.md", files[0].Path)
	assert.Equal(t, "a.md", files[0].Name, "basename is always computed")
	assert.Equal(t, base+60_000, files[0].ClosedAt)
	assert.Equal(t, int64(0), files[1].ClosedAt, "still open")
}

// A retry must not duplicate a row, and must not reopen an interval the server
// already closed at meeting end.
func TestRecordLiveNoteFiles_RetryIsIdempotentAndNeverReopens(t *testing.T) {
	setupTestDBForLiveNotes(t)
	ctx := context.Background()
	base := int64(1_700_000_000_000)
	file := LiveNoteFile{Owner: "host", Path: "docs/a.md", OpenedAt: base}

	require.NoError(t, RecordLiveNoteFiles(ctx, "tok", 7, []LiveNoteFile{file}))
	require.NoError(t, RecordLiveNoteFiles(ctx, "tok", 7, []LiveNoteFile{file}))
	files, err := ListLiveNoteFiles("tok", 0)
	require.NoError(t, err)
	require.Len(t, files, 1, "same (owner, path, openedAt) is one row")

	require.NoError(t, CloseOpenLiveNoteFiles("tok", base+30_000))
	// The publisher retries its still-"open" batch after the meeting closed.
	require.NoError(t, RecordLiveNoteFiles(ctx, "tok", 7, []LiveNoteFile{file}))
	files, err = ListLiveNoteFiles("tok", 0)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, base+30_000, files[0].ClosedAt, "closed_at only moves forward")
}

func TestCloseOpenLiveNoteFiles_LeavesClosedRowsAlone(t *testing.T) {
	setupTestDBForLiveNotes(t)
	ctx := context.Background()
	base := int64(1_700_000_000_000)

	require.NoError(t, RecordLiveNoteFiles(ctx, "tok", 7, []LiveNoteFile{
		{Owner: "host", Path: "a.md", OpenedAt: base, ClosedAt: base + 1000},
		{Owner: "host", Path: "b.md", OpenedAt: base + 2000},
	}))
	require.NoError(t, CloseOpenLiveNoteFiles("tok", base+50_000))

	files, err := ListLiveNoteFiles("tok", 0)
	require.NoError(t, err)
	require.Len(t, files, 2)
	assert.Equal(t, base+1000, files[0].ClosedAt, "an observed close is the truth")
	assert.Equal(t, base+50_000, files[1].ClosedAt)
}

func TestRecordLiveNoteFiles_SkipsUnusableEntries(t *testing.T) {
	setupTestDBForLiveNotes(t)
	ctx := context.Background()
	base := int64(1_700_000_000_000)

	require.NoError(t, RecordLiveNoteFiles(ctx, "tok", 7, []LiveNoteFile{
		{Path: "", OpenedAt: base},
		{Path: "a.md"},
		{Path: "b.md", OpenedAt: base},
	}))
	files, err := ListLiveNoteFiles("tok", 0)
	require.NoError(t, err)
	require.Len(t, files, 1, "one bad entry must not discard the batch")
	assert.Equal(t, "b.md", files[0].Path)
}

// File events are not speech, but they belong on the same track: the transcript
// is the one place a reader goes to ask what happened when.
func TestBuildLiveNotesVTT_CarriesFileEvents(t *testing.T) {
	base := time.Date(2026, 10, 30, 14, 30, 0, 0, time.Local).UnixMilli()
	got := BuildLiveNotesVTT([]LiveNoteSegment{
		{Channel: "mic", Speaker: "我", StartedAt: base, DurationMS: 4000, Text: "看下这个文档"},
	}, []LiveNoteFile{
		{Path: "docs/a.md", Name: "a.md", OpenedAt: base + 5000, ClosedAt: base + 65_000},
	}, nil)

	assert.Contains(t, got, "file\n00:00:05.000 --> 00:00:07.000\n<v 文件>打开 docs/a.md\n")
	assert.Contains(t, got, "file\n00:01:05.000 --> 00:01:07.000\n<v 文件>关闭 docs/a.md（1 分钟）\n")
	// Timeline order: the utterance came first.
	assert.Less(t, strings.Index(got, "看下这个文档"), strings.Index(got, "打开 docs/a.md"))
}

func TestBuildLiveNotesVTT_OpenFileHasNoCloseCue(t *testing.T) {
	base := int64(1_700_000_000_000)
	got := BuildLiveNotesVTT([]LiveNoteSegment{
		{Channel: "mic", Speaker: "我", StartedAt: base, DurationMS: 1000, Text: "开始"},
	}, []LiveNoteFile{{Path: "a.md", OpenedAt: base + 2000}}, nil)

	assert.Contains(t, got, "打开 a.md")
	assert.NotContains(t, got, "关闭")
}

// Without a timeline origin an absolute event time would be rendered as
// thousands of hours since 1970. Dropping the events is honest; inventing a
// position is not.
func TestBuildLiveNotesVTT_NoBaseDropsFileEvents(t *testing.T) {
	got := BuildLiveNotesVTT([]LiveNoteSegment{
		{Channel: "mic", Speaker: "我", Text: "没有时间戳"},
	}, []LiveNoteFile{{Path: "a.md", OpenedAt: 1_700_000_000_000}}, nil)

	assert.Contains(t, got, "没有时间戳")
	assert.NotContains(t, got, "打开 a.md")
}

// The proofread pass is a model call; file lines must be merged afterwards so
// they keep their exact text and position.
func TestMergeLiveNoteFileEvents_InsertsByTimestamp(t *testing.T) {
	base := int64(1_700_000_000_000)
	revised := "[00:00] 我：先看接口文档" + "\n" + "[01:00] 张三：我觉得可以"

	got := MergeLiveNoteFileEvents(revised, []LiveNoteFile{
		{Path: "docs/a.md", OpenedAt: base + 30_000, ClosedAt: base + 90_000},
	}, nil, base)

	lines := strings.Split(got, "\n")
	require.Len(t, lines, 4)
	assert.Equal(t, "[00:00] 我：先看接口文档", lines[0])
	assert.Equal(t, "[00:30] 文件：打开 docs/a.md", lines[1])
	assert.Equal(t, "[01:00] 张三：我觉得可以", lines[2])
	assert.Equal(t, "[01:30] 文件：关闭 docs/a.md（1 分钟）", lines[3])
}

// When the model rewrote the timestamps anyway, appending a section is worse
// than nothing but far better than dropping the events on the floor.
func TestMergeLiveNoteFileEvents_FallsBackToTrailingSection(t *testing.T) {
	base := int64(1_700_000_000_000)
	got := MergeLiveNoteFileEvents("我：先看接口文档\n张三：我觉得可以", []LiveNoteFile{
		{Path: "docs/a.md", OpenedAt: base + 30_000},
	}, nil, base)

	assert.Contains(t, got, "我：先看接口文档")
	assert.Contains(t, got, "## 会议期间打开的文件")
	assert.Contains(t, got, "[00:30] 文件：打开 docs/a.md")
}

func TestMergeLiveNoteFileEvents_NoFilesIsIdentity(t *testing.T) {
	revised := "[00:00] 我：内容"
	assert.Equal(t, revised, MergeLiveNoteFileEvents(revised, nil, nil, 1))
	assert.Equal(t, revised, MergeLiveNoteFileEvents(revised, []LiveNoteFile{{Path: "a.md", OpenedAt: 5}}, nil, 0))
}

// The summariser must be told these are things people LOOKED AT, not things
// people SAID — otherwise "a.go was open" becomes "the team decided to refactor
// a.go".
func TestLiveNoteFilesPromptSection_WarnsThatFilesAreNotSpeech(t *testing.T) {
	started := time.Date(2026, 10, 30, 14, 30, 0, 0, time.Local)
	base := started.UnixMilli()

	got := LiveNoteFilesPromptSection([]LiveNoteFile{
		{Owner: "host", Path: "docs/a.md", OpenedAt: base, ClosedAt: base + 8*60_000},
		{Owner: "viewer:v1", OwnerName: "张三", Path: "b.go", OpenedAt: base + 10*60_000},
	}, started, "")

	assert.Contains(t, got, "不是发言内容")
	assert.Contains(t, got, "- 14:30–14:38 docs/a.md")
	assert.Contains(t, got, "- 14:40 起（至会议结束） b.go（张三）")
	assert.Empty(t, LiveNoteFilesPromptSection(nil, started, ""))
}

// ── Archive ─────────────────────────────────────────────────────────────────

func TestArchiveLiveNoteFiles_CopiesAndDedupes(t *testing.T) {
	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, "a.md"), []byte("hello"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(project, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(project, "sub", "a.md"), []byte("hello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(project, "b.go"), []byte("package b"), 0o644))

	paths := LiveNotesPathsIn(t.TempDir())
	started := time.Date(2026, 10, 30, 14, 30, 0, 0, time.Local)
	base := started.UnixMilli()
	manifest, err := ArchiveLiveNoteFiles(paths, project, []LiveNoteFile{
		{Path: "a.md", OpenedAt: base, ClosedAt: base + 60_000},
		{Path: "sub/a.md", OpenedAt: base + 1000},
		{Path: "b.go", OpenedAt: base + 2000},
	}, started)
	require.NoError(t, err)

	entries, err := os.ReadDir(filepath.Join(paths.Dir, LiveNoteFileArchiveDir))
	require.NoError(t, err)
	require.Len(t, entries, 2, "identical content is archived once")

	body, err := os.ReadFile(manifest)
	require.NoError(t, err)
	assert.Contains(t, string(body), "files/a.md")
	assert.Contains(t, string(body), "files/b.go")
	assert.Contains(t, string(body), "内容与已有文件相同")
	assert.Contains(t, string(body), "| 14:30–14:31 | a.md |")
}

func TestArchiveLiveNoteFiles_SkipsOversizedAndMissing(t *testing.T) {
	project := t.TempDir()
	big := filepath.Join(project, "big.bin")
	require.NoError(t, os.WriteFile(big, make([]byte, LiveNoteFileCopyMaxBytes+1), 0o644))

	paths := LiveNotesPathsIn(t.TempDir())
	manifest, err := ArchiveLiveNoteFiles(paths, project, []LiveNoteFile{
		{Path: "big.bin", OpenedAt: 1000},
		{Path: "gone.md", OpenedAt: 2000},
	}, time.UnixMilli(1000))
	require.NoError(t, err)

	_, err = os.ReadDir(filepath.Join(paths.Dir, LiveNoteFileArchiveDir))
	assert.ErrorIs(t, err, os.ErrNotExist, "an empty archive folder must not be left behind")

	body, err := os.ReadFile(manifest)
	require.NoError(t, err)
	assert.Contains(t, string(body), "超过 5 MB")
	assert.Contains(t, string(body), "文件已不存在")
}

// The excerpt is what makes the summary able to talk about the documents, so it
// must read text and refuse binary — not paste a PNG into the prompt.
func TestLiveNoteFileExcerpts_ReadsTextSkipsBinary(t *testing.T) {
	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, "a.md"), []byte("# 发布计划\n\n下周一上线"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(project, "pic.png"), []byte{0x89, 0x50, 0x4e, 0x47, 0x00, 0x01}, 0o644))

	got := LiveNoteFileExcerpts(project, []LiveNoteFile{
		{Path: "a.md", OpenedAt: 1000},
		{Path: "pic.png", OpenedAt: 2000},
	})
	assert.Contains(t, got, "### a.md")
	assert.Contains(t, got, "下周一上线")
	assert.NotContains(t, got, "pic.png")
}

func TestLiveNoteFileExcerpts_TruncatesToBudget(t *testing.T) {
	project := t.TempDir()
	long := strings.Repeat("长", liveNoteFileExcerptMaxRunes*2)
	require.NoError(t, os.WriteFile(filepath.Join(project, "a.md"), []byte(long), 0o644))

	got := LiveNoteFileExcerpts(project, []LiveNoteFile{{Path: "a.md", OpenedAt: 1000}})
	assert.LessOrEqual(t, len([]rune(got)), liveNoteFileExcerptBudget+64)
}

func TestLiveNoteFileExcerpts_NothingReadable(t *testing.T) {
	assert.Empty(t, LiveNoteFileExcerpts("", nil))
	assert.Empty(t, LiveNoteFileExcerpts(t.TempDir(), []LiveNoteFile{{Path: "missing.md", OpenedAt: 1}}))
}

// Archiving is best-effort per file, but the request itself still needs a
// folder to write into and something to write.
func TestArchiveLiveNoteFiles_RequiresAFolderAndSomeFiles(t *testing.T) {
	_, err := ArchiveLiveNoteFiles(LiveNotesPathsIn(""), t.TempDir(),
		[]LiveNoteFile{{Path: "a.md", OpenedAt: 1}}, time.UnixMilli(1))
	assert.EqualError(t, err, "live notes: no project path")

	path, err := ArchiveLiveNoteFiles(LiveNotesPathsIn(t.TempDir()), t.TempDir(), nil, time.UnixMilli(1))
	require.NoError(t, err)
	assert.Empty(t, path, "no files means no manifest")
}

func TestArchiveLiveNoteFiles_ReportsUnwritableFolder(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "meeting")
	require.NoError(t, os.WriteFile(blocked, []byte("x"), 0o644))
	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, "a.md"), []byte("hello"), 0o644))

	_, err := ArchiveLiveNoteFiles(LiveNotesPathsIn(blocked), project,
		[]LiveNoteFile{{Path: "a.md", OpenedAt: 1000}}, time.UnixMilli(1000))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create files dir")
}

// The copies are a convenience; the manifest is the record. A manifest that
// cannot be written must fail the archive rather than report success.
func TestArchiveLiveNoteFiles_ReportsUnwritableManifest(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permissions are not enforced for this user")
	}
	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, "a.md"), []byte("hello"), 0o644))

	paths := LiveNotesPathsIn(t.TempDir())
	require.NoError(t, os.MkdirAll(filepath.Join(paths.Dir, LiveNoteFileArchiveDir), 0o755))
	require.NoError(t, os.Chmod(paths.Dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(paths.Dir, 0o755) })

	_, err := ArchiveLiveNoteFiles(paths, project,
		[]LiveNoteFile{{Path: "a.md", OpenedAt: 1000}}, time.UnixMilli(1000))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write files manifest")
}

// Every skip is recorded with its reason: a file silently missing from the
// manifest would look like nobody opened it.
func TestArchiveLiveNoteFiles_RecordsEverySkipReason(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permissions are not enforced for this user")
	}
	project := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(project, "folder"), 0o755))
	secret := filepath.Join(project, "secret.md")
	require.NoError(t, os.WriteFile(secret, []byte("x"), 0o644))
	require.NoError(t, os.Chmod(secret, 0o000))
	t.Cleanup(func() { _ = os.Chmod(secret, 0o644) })

	paths := LiveNotesPathsIn(t.TempDir())
	manifest, err := ArchiveLiveNoteFiles(paths, project, []LiveNoteFile{
		{OpenedAt: 1000},                 // a viewer's copy: no path, no name
		{Path: "folder", OpenedAt: 2000}, // a directory, not a document
		{Path: "secret.md", OpenedAt: 3000},
		{Path: "later.md"}, // no open time: not on the timeline
	}, time.UnixMilli(1000))
	require.NoError(t, err)

	body, err := os.ReadFile(manifest)
	require.NoError(t, err)
	assert.Contains(t, string(body), "路径未知")
	assert.Contains(t, string(body), "读取失败", "a directory is not a regular file")
	assert.Contains(t, string(body), "没有读取权限")
	assert.NotContains(t, string(body), "later.md", "an entry with no open time is not part of the timeline")
}

// A client bug that reports hundreds of files must not copy them all.
func TestArchiveLiveNoteFiles_CapsTheNumberOfCopies(t *testing.T) {
	project := t.TempDir()
	files := make([]LiveNoteFile, 0, LiveNoteFileCopyMaxCount+1)
	for i := 0; i <= LiveNoteFileCopyMaxCount; i++ {
		name := "f" + strconv.Itoa(i) + ".md"
		require.NoError(t, os.WriteFile(filepath.Join(project, name), []byte(name), 0o644))
		files = append(files, LiveNoteFile{Path: name, OpenedAt: int64(i + 1)})
	}

	paths := LiveNotesPathsIn(t.TempDir())
	manifest, err := ArchiveLiveNoteFiles(paths, project, files, time.UnixMilli(1000))
	require.NoError(t, err)

	entries, err := os.ReadDir(filepath.Join(paths.Dir, LiveNoteFileArchiveDir))
	require.NoError(t, err)
	assert.Len(t, entries, LiveNoteFileCopyMaxCount)

	body, err := os.ReadFile(manifest)
	require.NoError(t, err)
	assert.Contains(t, string(body), "超过单场会议归档数量上限")
}

// Two files called a.md from different folders is a normal meeting, not an
// error: the second copy is suffixed instead of overwriting the first.
func TestArchiveLiveNoteFiles_SuffixesCollidingNames(t *testing.T) {
	project := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(project, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(project, "a.md"), []byte("one"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(project, "sub", "a.md"), []byte("two"), 0o644))

	paths := LiveNotesPathsIn(t.TempDir())
	manifest, err := ArchiveLiveNoteFiles(paths, project, []LiveNoteFile{
		{Path: "a.md", OpenedAt: 1000},
		{Path: "sub/a.md", OpenedAt: 2000},
	}, time.UnixMilli(1000))
	require.NoError(t, err)

	body, err := os.ReadFile(manifest)
	require.NoError(t, err)
	assert.Contains(t, string(body), "files/a.md")
	assert.Contains(t, string(body), "files/a-2.md")

	first, err := os.ReadFile(filepath.Join(paths.Dir, LiveNoteFileArchiveDir, "a.md"))
	require.NoError(t, err)
	assert.Equal(t, "one", string(first), "the first copy is never overwritten")
}
