package handler

// 符号链接：浏览一直跟随，搜索和读接口以前不跟随——同一个文件在文件管理器里
// 点得开、搜不到，链接目录里有什么也一概看不见。这一组钉住修好后的口径：
//
//   - 链接进来的文件/目录，文件名搜索和内容搜索都能找到；
//   - 结果路径是**逻辑路径**（项目内那条），前端拿它就能打开；
//   - 跟随不越过配置的根目录（浏览也进不去的地方，搜索同样不给结果）；
//   - 指回自己祖先的链接不会让遍历转圈；
//   - 条目预算耗光时上报"结果被截断"，而不是假装搜完了。

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// symlinkFixture 在项目外（但仍在 RootPaths 内）造一份真实数据，再把它以符号
// 链接的形式放进项目——就是用户把外部目录链进项目的样子。
type symlinkFixture struct {
	externalDir string // 真实位置：<watch>/external/data
	linkPath    string // 项目内的链接：<project>/data
}

func newSymlinkFixture(t *testing.T, env *testEnv) symlinkFixture {
	t.Helper()
	externalDir := filepath.Join(env.WatchDir, "external", "data")
	require.NoError(t, os.MkdirAll(externalDir, 0o755))
	linkPath := filepath.Join(env.ProjectDir, "data")
	require.NoError(t, os.Symlink(externalDir, linkPath))
	return symlinkFixture{externalDir: externalDir, linkPath: linkPath}
}

// dirSearchHits 发一次文件名搜索，返回命中的路径集合。
func dirSearchHits(t *testing.T, projectDir, query string) map[string]DirSearchResult {
	t.Helper()
	req := newRequest(t, http.MethodGet, "/api/dir/search?path=&q="+query+"&recursive=true", nil)
	withProjectCookie(req, projectDir)
	w := callHandler(DirSearch, req)
	assertOK(t, w)

	hits := make(map[string]DirSearchResult)
	for _, raw := range parseSearchSSEEvents(w.Body.String())["result"] {
		var r DirSearchResult
		require.NoError(t, json.Unmarshal(raw, &r))
		hits[r.Path] = r
	}
	return hits
}

func TestDirSearch_FindsFilesInsideSymlinkedDirectory(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	fx := newSymlinkFixture(t, env)
	createTestFile(t, fx.externalDir, "nested.txt", "hello")

	hits := dirSearchHits(t, env.ProjectDir, "nested")

	require.Len(t, hits, 1, "链接目录里的文件必须能搜到")
	hit := hits["data/nested.txt"]
	assert.Equal(t, "nested.txt", hit.Name)
	assert.Equal(t, "file", hit.Type)
	assert.Equal(t, int64(len("hello")), hit.Size, "大小取目标文件的，不是链接本身的")
}

func TestDirSearch_SymlinkedDirectoryIsATypeDirHit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	newSymlinkFixture(t, env)

	hits := dirSearchHits(t, env.ProjectDir, "data")

	require.Contains(t, hits, "data")
	assert.Equal(t, "dir", hits["data"].Type, "指向目录的链接要按目录报，否则点开会被当文件打开")
}

func TestDirSearch_FindsFilesBehindSymlinkedFile(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	target := filepath.Join(env.WatchDir, "external", "notes.md")
	createTestFile(t, filepath.Dir(target), "notes.md", "0123456789")
	require.NoError(t, os.Symlink(target, filepath.Join(env.ProjectDir, "readme.md")))

	hits := dirSearchHits(t, env.ProjectDir, "readme")

	require.Contains(t, hits, "readme.md")
	hit := hits["readme.md"]
	assert.Equal(t, "file", hit.Type)
	assert.Equal(t, int64(10), hit.Size, "指向文件的链接要报目标的类型与大小")
	assert.NotEmpty(t, hit.Modified, "修改时间也取目标的")
}

func TestDirSearch_DoesNotFollowSymlinkOutsideEveryRoot(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// RootPaths 是 [WatchDir]，所以这个临时目录在"任何根之外"——真实场景对应
	// Windows 上另一个盘符。浏览把这种链接显示成不可进入的文件，搜索也不进。
	outsideEveryRoot := canonPath(t.TempDir())
	createTestFile(t, outsideEveryRoot, "secret.txt", "hidden")
	require.NoError(t, os.Symlink(outsideEveryRoot, filepath.Join(env.ProjectDir, "out")))

	hits := dirSearchHits(t, env.ProjectDir, "secret")
	assert.Empty(t, hits, "根目录之外的链接目标不能被搜到")

	hits = dirSearchHits(t, env.ProjectDir, "out")
	require.Contains(t, hits, "out")
	assert.Equal(t, "file", hits["out"].Type, "不可进入的链接目录，类型与浏览一致：按文件报")
}

func TestDirSearch_SymlinkLoopTerminates(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "a/main.go", "package main")
	// 指回自己的祖先：跟随的遍历必须发现它绕回来了。
	require.NoError(t, os.Symlink(filepath.Join(env.ProjectDir, "a"), filepath.Join(env.ProjectDir, "a", "loop")))

	hits := dirSearchHits(t, env.ProjectDir, "main")

	require.Len(t, hits, 1, "绕回祖先的链接不能把同一个文件重复报出或让遍历转圈")
	assert.Contains(t, hits, "a/main.go")
}

func TestContentSearch_FollowsSymlinkedFile(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	target := filepath.Join(env.WatchDir, "external", "notes.txt")
	createTestFile(t, filepath.Dir(target), "notes.txt", "line one\nTODO: fix this\n")
	require.NoError(t, os.Symlink(target, filepath.Join(env.ProjectDir, "notes.txt")))

	req := contentSearchRequest(t, env.ProjectDir, "path=&q=TODO")
	w := callHandler(ContentSearch, req)
	assertOK(t, w)

	results, done := decodeContentSearchResults(t, w.Body.String())
	require.Contains(t, results, "notes.txt", "链接指向的文件，内容也要能搜到")
	require.Len(t, results["notes.txt"].Matches, 1)
	assert.Equal(t, 1, done.Files)
}

func TestContentSearch_FollowsSymlinkedDirectory(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	fx := newSymlinkFixture(t, env)
	createTestFile(t, fx.externalDir, "inner.go", "package inner\n// TODO: linked in\n")

	req := contentSearchRequest(t, env.ProjectDir, "path=&q=TODO")
	w := callHandler(ContentSearch, req)
	assertOK(t, w)

	results, _ := decodeContentSearchResults(t, w.Body.String())
	require.Contains(t, results, "data/inner.go", "链接目录里的文件内容必须能搜到")
}

func TestDirSearch_EntryBudgetReportsTruncated(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	origBudget := maxWalkedEntries
	maxWalkedEntries = 2
	t.Cleanup(func() { maxWalkedEntries = origBudget })

	for _, name := range []string{"hit1.txt", "hit2.txt", "hit3.txt", "hit4.txt"} {
		createTestFile(t, env.ProjectDir, name, "x")
	}

	req := newRequest(t, http.MethodGet, "/api/dir/search?path=&q=hit&recursive=true", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(DirSearch, req)
	assertOK(t, w)

	events := parseSearchSSEEvents(w.Body.String())
	require.Len(t, events["done"], 1)
	var done DirSearchDone
	require.NoError(t, json.Unmarshal(events["done"][0], &done))

	assert.True(t, done.Truncated, "预算耗光必须报「还有更多」，不能假装搜完了")
	assert.Less(t, done.Total, 4, "预算耗光时不可能把四个都数出来")
}

// ── 读接口：链接进来就能打开 ─────────────────────────────────────────────────
//
// 搜索给出的是项目内相对路径（data/nested.txt）。这些接口以前一律 403，于是
// 用户看到的是"搜到了但点开说文件不存在"。

func TestBatchExists_ResolvesPathThroughSymlink(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	fx := newSymlinkFixture(t, env)
	createTestFile(t, fx.externalDir, "nested.txt", "hello")
	createTestFile(t, env.WatchDir, "escape.txt", "x") // 目标真实存在，仍然要拒绝

	req := newRequest(t, http.MethodPost, "/api/file/batch-exists", map[string]interface{}{
		"paths": []string{"data/nested.txt", "data", "../escape.txt"},
	})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeFileBatchExists, req)
	assertOK(t, w)

	var resp struct {
		Results map[string]string `json:"results"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, "file", resp.Results["data/nested.txt"], "链接进来的文件必须算存在，否则路径标注被判定为丢失")
	assert.Equal(t, "dir", resp.Results["data"])
	assert.Equal(t, "none", resp.Results["../escape.txt"], "词法上的 .. 越界仍然拒绝")
}

func TestGetFile_ReadsThroughSymlink(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	fx := newSymlinkFixture(t, env)
	createTestFile(t, fx.externalDir, "nested.txt", "linked content")

	req := newRequest(t, http.MethodGet, "/api/fs/file/data/nested.txt", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(GetFile, req)
	assertOK(t, w)

	var resp FileContent
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "linked content", resp.Content)
}

func TestListDir_ListsSymlinkedDirectory(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	fx := newSymlinkFixture(t, env)
	createTestFile(t, fx.externalDir, "nested.txt", "hello")

	req := newRequest(t, http.MethodGet, "/api/dir?path=data", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ListDir, req)
	assertOK(t, w)

	var resp struct {
		Items []DirEntry `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Items, 1)
	assert.Equal(t, "nested.txt", resp.Items[0].Name)
}

func TestListDir_RejectsLexicalEscape(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// 放宽的是"链接跳出去"，不是"路径写出项目"。
	req := newRequest(t, http.MethodGet, "/api/dir?path=../", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ListDir, req)

	assertStatus(t, w, http.StatusForbidden)
}
