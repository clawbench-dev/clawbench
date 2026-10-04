package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertSession inserts a chat_sessions row with an explicit creation time,
// session_type and archived flag. projectPath is resolved (and registered)
// through the same normalization the queries use.
func insertSession(t *testing.T, projectPath, id, title, sessionType, createdAt string, archived bool) {
	t.Helper()
	archivedInt := 0
	if archived {
		archivedInt = 1
	}
	_, err := UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_id, backend, title, session_type, archived, created_at, updated_at) VALUES (?, ?, 'claude', ?, ?, ?, ?, ?)",
		id, ProjectIDForTest(t, projectPath), title, sessionType, archivedInt, createdAt, createdAt,
	)
	require.NoError(t, err)
}

func TestNormalizeSessionArchiveFilter(t *testing.T) {
	assert.Equal(t, SessionArchiveFilterAll, NormalizeSessionArchiveFilter(""))
	assert.Equal(t, SessionArchiveFilterAll, NormalizeSessionArchiveFilter("bogus"))
	assert.Equal(t, SessionArchiveFilterActive, NormalizeSessionArchiveFilter(" Active "))
	assert.Equal(t, SessionArchiveFilterArchived, NormalizeSessionArchiveFilter("ARCHIVED"))
}

func TestNormalizeSessionSortOrder(t *testing.T) {
	assert.Equal(t, SessionSortRelevance, NormalizeSessionSortOrder(""))
	assert.Equal(t, SessionSortRelevance, NormalizeSessionSortOrder("bogus"))
	assert.Equal(t, SessionSortNewest, NormalizeSessionSortOrder("Newest"))
	assert.Equal(t, SessionSortOldest, NormalizeSessionSortOrder(" OLDEST "))
}

func TestNormalizeSessionTypeFilter(t *testing.T) {
	assert.Equal(t, SessionTypeFilterAll, NormalizeSessionTypeFilter(""))
	assert.Equal(t, SessionTypeFilterAll, NormalizeSessionTypeFilter("bogus"))
	assert.Equal(t, SessionTypeFilterChat, NormalizeSessionTypeFilter(" CHAT "))
	assert.Equal(t, SessionTypeFilterTask, NormalizeSessionTypeFilter("Task"))
}

func TestSessionTypeDBValue(t *testing.T) {
	// "all" must yield the empty string so callers can use it as "no predicate".
	assert.Equal(t, "", SessionTypeDBValue(""))
	assert.Equal(t, "", SessionTypeDBValue("all"))
	assert.Equal(t, "", SessionTypeDBValue("bogus"))
	assert.Equal(t, "chat", SessionTypeDBValue("chat"))
	// The user-facing "task" maps onto the DB's 'scheduled'.
	assert.Equal(t, "scheduled", SessionTypeDBValue("task"))
}

func TestEscapeLikePattern(t *testing.T) {
	// Every LIKE metacharacter must be backslash-escaped so it matches
	// literally once paired with ESCAPE '\'.
	assert.Equal(t, `100\%`, escapeLikePattern("100%"))
	assert.Equal(t, `a\_b`, escapeLikePattern("a_b"))
	assert.Equal(t, `c:\\temp`, escapeLikePattern(`c:\temp`))
	assert.Equal(t, "plain", escapeLikePattern("plain"))
	// The exported forwarder must agree with the internal one.
	assert.Equal(t, escapeLikePattern("x%y_z"), EscapeLikePatternForTest("x%y_z"))
}

// ---------- GetRecentSessions ----------

func TestGetRecentSessions_NewestFirstIncludesArchived(t *testing.T) {
	setupStoreTestDB(t)

	insertSession(t, "/project", "old", "Old", "chat", "2024-01-01 10:00:00", false)
	insertSession(t, "/project", "new", "New", "chat", "2024-03-01 10:00:00", false)
	insertSession(t, "/project", "arch", "Archived", "chat", "2024-02-01 10:00:00", true)

	sessions, hasMore, err := GetRecentSessions("/project", 0, "", "", "", "", "", "", "")
	require.NoError(t, err)
	require.Len(t, sessions, 3)
	assert.False(t, hasMore)
	// Reverse chronological order (newest first).
	assert.Equal(t, "new", sessions[0].ID)
	assert.Equal(t, "arch", sessions[1].ID)
	assert.Equal(t, "old", sessions[2].ID)
	assert.True(t, sessions[1].Archived)
	assert.False(t, sessions[2].Archived)
}

func TestGetRecentSessions_ProjectScopedAndLimited(t *testing.T) {
	setupStoreTestDB(t)

	insertSession(t, "/project", "a", "A", "chat", "2024-01-01 10:00:00", false)
	insertSession(t, "/project", "b", "B", "chat", "2024-01-02 10:00:00", false)
	insertSession(t, "/other", "c", "C", "chat", "2024-01-03 10:00:00", false)

	sessions, _, err := GetRecentSessions("/project", 0, "", "", "", "", "", "", "")
	require.NoError(t, err)
	require.Len(t, sessions, 2)

	// limit=1 must fetch limit+1 rows to detect the further page.
	sessions, hasMore, err := GetRecentSessions("/project", 1, "", "", "", "", "", "", "")
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.True(t, hasMore)
	assert.Equal(t, "b", sessions[0].ID)
}

func TestGetRecentSessions_EmptyProjectBrowsesAll(t *testing.T) {
	setupStoreTestDB(t)

	insertSession(t, "/project", "a", "A", "chat", "2024-01-01 10:00:00", false)
	insertSession(t, "/other", "b", "B", "chat", "2024-01-02 10:00:00", false)

	sessions, _, err := GetRecentSessions("", 0, "", "", "", "", "", "", "")
	require.NoError(t, err)
	require.Len(t, sessions, 2)
	assert.Equal(t, "b", sessions[0].ID)
}

func TestGetRecentSessions_ArchiveFilter(t *testing.T) {
	setupStoreTestDB(t)

	insertSession(t, "/project", "active-1", "A1", "chat", "2024-01-01 10:00:00", false)
	insertSession(t, "/project", "arch-1", "R1", "chat", "2024-02-01 10:00:00", true)
	insertSession(t, "/project", "active-2", "A2", "chat", "2024-03-01 10:00:00", false)
	insertSession(t, "/project", "arch-2", "R2", "chat", "2024-04-01 10:00:00", true)

	active, _, err := GetRecentSessions("/project", 0, SessionArchiveFilterActive, "", "", "", "", "", "")
	require.NoError(t, err)
	require.Len(t, active, 2)
	for _, s := range active {
		assert.False(t, s.Archived)
	}

	archived, _, err := GetRecentSessions("/project", 0, SessionArchiveFilterArchived, "", "", "", "", "", "")
	require.NoError(t, err)
	require.Len(t, archived, 2)
	for _, s := range archived {
		assert.True(t, s.Archived)
	}
	assert.Equal(t, "arch-2", archived[0].ID)

	all, _, err := GetRecentSessions("/project", 0, SessionArchiveFilterAll, "", "", "", "", "", "")
	require.NoError(t, err)
	assert.Len(t, all, 4)
}

func TestGetRecentSessions_SortOrderOldest(t *testing.T) {
	setupStoreTestDB(t)

	insertSession(t, "/project", "new", "New", "chat", "2024-03-01 10:00:00", false)
	insertSession(t, "/project", "old", "Old", "chat", "2024-01-01 10:00:00", false)
	insertSession(t, "/project", "mid", "Mid", "chat", "2024-02-01 10:00:00", false)

	oldest, _, err := GetRecentSessions("/project", 0, "", "", SessionSortOldest, "", "", "", "")
	require.NoError(t, err)
	require.Len(t, oldest, 3)
	assert.Equal(t, "old", oldest[0].ID)
	assert.Equal(t, "mid", oldest[1].ID)
	assert.Equal(t, "new", oldest[2].ID)
}

// The cursor pagination must not skip or duplicate rows that share a timestamp:
// the tie-break on id is what makes the two pages partition the result set.
func TestGetRecentSessions_CursorPagination(t *testing.T) {
	setupStoreTestDB(t)

	// Three rows share one timestamp; two more are strictly newer.
	insertSession(t, "/project", "tie-a", "Tie A", "chat", "2024-02-01 10:00:00", false)
	insertSession(t, "/project", "tie-b", "Tie B", "chat", "2024-02-01 10:00:00", false)
	insertSession(t, "/project", "tie-c", "Tie C", "chat", "2024-02-01 10:00:00", false)
	insertSession(t, "/project", "new1", "New 1", "chat", "2024-03-01 10:00:00", false)
	insertSession(t, "/project", "new2", "New 2", "chat", "2024-04-01 10:00:00", false)

	page1, hasMore, err := GetRecentSessions("/project", 2, "", "", "", "", "", "", "")
	require.NoError(t, err)
	require.Len(t, page1, 2)
	assert.True(t, hasMore)
	assert.Equal(t, "new2", page1[0].ID)
	assert.Equal(t, "new1", page1[1].ID)

	// Next page starts strictly after the last row's (created_at, id).
	last := page1[len(page1)-1]
	page2, _, err := GetRecentSessions("/project", 2, "", "", "", "", "", last.CreatedAt.Format("2006-01-02 15:04:05"), last.ID)
	require.NoError(t, err)
	require.Len(t, page2, 2)
	assert.Equal(t, "tie-c", page2[0].ID)
	assert.Equal(t, "tie-b", page2[1].ID)

	// No overlap between pages.
	seen := map[string]bool{}
	for _, s := range append(page1, page2...) {
		assert.False(t, seen[s.ID], "row %s appeared on both pages", s.ID)
		seen[s.ID] = true
	}
}

// Oldest-first cursor pagination walks forward in time with a `>` tie-break.
func TestGetRecentSessions_CursorPaginationOldest(t *testing.T) {
	setupStoreTestDB(t)

	insertSession(t, "/project", "tie-a", "Tie A", "chat", "2024-02-01 10:00:00", false)
	insertSession(t, "/project", "tie-b", "Tie B", "chat", "2024-02-01 10:00:00", false)
	insertSession(t, "/project", "new1", "New 1", "chat", "2024-03-01 10:00:00", false)

	page1, hasMore, err := GetRecentSessions("/project", 2, "", "", SessionSortOldest, "", "", "", "")
	require.NoError(t, err)
	require.Len(t, page1, 2)
	assert.True(t, hasMore)
	assert.Equal(t, "tie-a", page1[0].ID)
	assert.Equal(t, "tie-b", page1[1].ID)

	last := page1[len(page1)-1]
	page2, _, err := GetRecentSessions("/project", 2, "", "", SessionSortOldest, "", "", last.CreatedAt.Format("2006-01-02 15:04:05"), last.ID)
	require.NoError(t, err)
	require.Len(t, page2, 1)
	assert.Equal(t, "new1", page2[0].ID)
}

func TestGetRecentSessions_TypeFilterSeparation(t *testing.T) {
	setupStoreTestDB(t)

	insertSession(t, "/project", "conv", "Conversation", "chat", "2024-01-01 10:00:00", false)
	insertSession(t, "/project", "job", "Task run", "scheduled", "2024-02-01 10:00:00", false)

	// Default / "all" → conversations only, never tasks.
	all, _, err := GetRecentSessions("/project", 0, "", "", "", "", "", "", "")
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "conv", all[0].ID)
	assert.Equal(t, "chat", all[0].SessionType)

	task, _, err := GetRecentSessions("/project", 0, "", SessionTypeFilterTask, "", "", "", "", "")
	require.NoError(t, err)
	require.Len(t, task, 1)
	assert.Equal(t, "job", task[0].ID)
	assert.Equal(t, "scheduled", task[0].SessionType)
}

// Time-range bounds apply to the session's created_at.
func TestGetRecentSessions_TimeRange(t *testing.T) {
	setupStoreTestDB(t)

	insertSession(t, "/project", "early", "Early", "chat", "2024-01-01 10:00:00", false)
	insertSession(t, "/project", "mid", "Mid", "chat", "2024-02-01 10:00:00", false)
	insertSession(t, "/project", "late", "Late", "chat", "2024-03-01 10:00:00", false)

	got, _, err := GetRecentSessions("/project", 0, "", "", "", "2024-01-15 00:00:00", "2024-02-15 00:00:00", "", "")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "mid", got[0].ID)
}

// ---------- SearchSessionsByTitle ----------

func TestSearchSessionsByTitle_MatchesTitleAndOrdersNewestFirst(t *testing.T) {
	setupStoreTestDB(t)

	insertSession(t, "/project", "a", "数据库优化方案", "chat", "2024-01-01 10:00:00", false)
	insertSession(t, "/project", "b", "数据库迁移记录", "chat", "2024-03-01 10:00:00", false)
	insertSession(t, "/project", "c", "前端重构", "chat", "2024-02-01 10:00:00", false)

	got, err := SearchSessionsByTitle("/project", []string{"数据库"}, 0, "", "", "", "", "", "")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "b", got[0].ID)
	assert.Equal(t, "a", got[1].ID)
}

func TestSearchSessionsByTitle_AllTermsMustMatch(t *testing.T) {
	setupStoreTestDB(t)

	insertSession(t, "/project", "both", "数据库优化方案", "chat", "2024-01-01 10:00:00", false)
	insertSession(t, "/project", "one", "数据库迁移记录", "chat", "2024-01-02 10:00:00", false)

	got, err := SearchSessionsByTitle("/project", []string{"数据库", "优化"}, 0, "", "", "", "", "", "")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "both", got[0].ID)

	got, err = SearchSessionsByTitle("/project", []string{"数据库", "不存在"}, 0, "", "", "", "", "", "")
	require.NoError(t, err)
	assert.Empty(t, got)
}

// A literal % / _ / \ must not act as a LIKE wildcard.
func TestSearchSessionsByTitle_EscapesLikeWildcards(t *testing.T) {
	setupStoreTestDB(t)

	insertSession(t, "/project", "pct", "覆盖率 100% 达成", "chat", "2024-01-01 10:00:00", false)
	insertSession(t, "/project", "other", "覆盖率统计", "chat", "2024-01-02 10:00:00", false)

	got, err := SearchSessionsByTitle("/project", []string{"100%"}, 0, "", "", "", "", "", "")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "pct", got[0].ID)

	got, err = SearchSessionsByTitle("/project", []string{"%"}, 0, "", "", "", "", "", "")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "pct", got[0].ID)

	insertSession(t, "/project", "bs", `路径 C:\temp 记录`, "chat", "2024-01-03 10:00:00", false)
	got, err = SearchSessionsByTitle("/project", []string{`C:\temp`}, 0, "", "", "", "", "", "")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "bs", got[0].ID)
}

func TestSearchSessionsByTitle_Filters(t *testing.T) {
	setupStoreTestDB(t)

	insertSession(t, "/project", "active", "数据库优化", "chat", "2024-01-01 10:00:00", false)
	insertSession(t, "/project", "archived", "数据库归档", "chat", "2024-02-01 10:00:00", true)
	insertSession(t, "/other", "otherproj", "数据库优化", "chat", "2024-03-01 10:00:00", false)
	insertSession(t, "/project", "task", "数据库任务", "scheduled", "2024-04-01 10:00:00", false)

	got, err := SearchSessionsByTitle("/project", []string{"数据库"}, 0, "", "", "", "", "", "")
	require.NoError(t, err)
	require.Len(t, got, 2)
	for _, s := range got {
		assert.NotEqual(t, "task", s.ID)
		assert.NotEqual(t, "otherproj", s.ID)
	}

	active, err := SearchSessionsByTitle("/project", []string{"数据库"}, 0, SessionArchiveFilterActive, "", "", "", "", "")
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, "active", active[0].ID)

	archived, err := SearchSessionsByTitle("/project", []string{"数据库"}, 0, SessionArchiveFilterArchived, "", "", "", "", "")
	require.NoError(t, err)
	require.Len(t, archived, 1)
	assert.Equal(t, "archived", archived[0].ID)

	tasks, err := SearchSessionsByTitle("/project", []string{"数据库"}, 0, "", SessionTypeFilterTask, "", "", "", "")
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, "task", tasks[0].ID)

	windowed, err := SearchSessionsByTitle("/project", []string{"数据库"}, 0, "", "", "2024-01-15 00:00:00", "2024-03-15 00:00:00", "", "")
	require.NoError(t, err)
	require.Len(t, windowed, 1)
	assert.Equal(t, "archived", windowed[0].ID)

	limited, err := SearchSessionsByTitle("/project", []string{"数据库"}, 1, "", "", "", "", "", "")
	require.NoError(t, err)
	require.Len(t, limited, 1)
	assert.Equal(t, "archived", limited[0].ID)
}

func TestSearchSessionsByTitle_EmptyTermsMatchNothing(t *testing.T) {
	setupStoreTestDB(t)

	insertSession(t, "/project", "a", "任意标题", "chat", "2024-01-01 10:00:00", false)

	got, err := SearchSessionsByTitle("/project", nil, 0, "", "", "", "", "", "")
	require.NoError(t, err)
	assert.Empty(t, got)

	got, err = SearchSessionsByTitle("/project", []string{}, 0, "", "", "", "", "", "")
	require.NoError(t, err)
	assert.Empty(t, got)
}

// sessionID narrows to one session; excludeSessionID drops one — the latter is
// how /cb-chatsearch keeps the current conversation out of its own results.
func TestSearchSessionsByTitle_SessionIDScopes(t *testing.T) {
	setupStoreTestDB(t)

	insertSession(t, "/project", "a", "数据库优化", "chat", "2024-01-01 10:00:00", false)
	insertSession(t, "/project", "b", "数据库迁移", "chat", "2024-01-02 10:00:00", false)

	got, err := SearchSessionsByTitle("/project", []string{"数据库"}, 0, "", "", "", "", "a", "")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "a", got[0].ID)

	got, err = SearchSessionsByTitle("/project", []string{"数据库"}, 0, "", "", "", "", "", "a")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "b", got[0].ID)
}

// With no database handle installed, the title channel returns empty rather
// than dereferencing a nil handle (RAG standalone / early startup).
func TestSearchSessionsByTitle_NoDBReturnsEmpty(t *testing.T) {
	restore := SetDBForTest(nil, nil)
	defer restore()
	ResetProjectIDCacheForTest()

	got, err := SearchSessionsByTitle("/project", []string{"x"}, 0, "", "", "", "", "", "")
	require.NoError(t, err)
	assert.Empty(t, got)
}

// ---------- GetSessionTitlesBatchIncludeArchived ----------

func TestGetSessionTitlesBatchIncludeArchived(t *testing.T) {
	setupStoreTestDB(t)

	insertSession(t, "/project", "a", "Alpha", "chat", "2024-01-01 10:00:00", false)
	insertSession(t, "/project", "b", "Beta", "chat", "2024-01-02 10:00:00", true)

	// Empty input short-circuits to an empty map.
	titles, err := GetSessionTitlesBatchIncludeArchived(nil)
	require.NoError(t, err)
	assert.Empty(t, titles)

	// Archived sessions are included.
	titles, err = GetSessionTitlesBatchIncludeArchived([]string{"a", "b", "missing"})
	require.NoError(t, err)
	assert.Equal(t, "Alpha", titles["a"])
	assert.Equal(t, "Beta", titles["b"])
	_, ok := titles["missing"]
	assert.False(t, ok)
}

// A session with an empty title is omitted from the map so callers do not
// render a blank label.
func TestGetSessionTitlesBatchIncludeArchived_SkipsEmptyTitles(t *testing.T) {
	setupStoreTestDB(t)

	insertSession(t, "/project", "a", "", "chat", "2024-01-01 10:00:00", false)

	titles, err := GetSessionTitlesBatchIncludeArchived([]string{"a"})
	require.NoError(t, err)
	assert.Empty(t, titles)
}

// ---------- Index bookkeeping ----------

func insertHistory(t *testing.T, sessionID, content string, indexed, streaming bool) int64 {
	t.Helper()
	idx, str := 0, 0
	if indexed {
		idx = 1
	}
	if streaming {
		str = 1
	}
	res, err := UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming, indexed) VALUES (1, 'user', ?, ?, 'claude', ?, ?)",
		content, sessionID, str, idx,
	)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	return id
}

func TestGetUnindexedMessages(t *testing.T) {
	setupStoreTestDB(t)

	insertHistory(t, "s1", "unindexed", false, false)
	insertHistory(t, "s1", "indexed", true, false)
	insertHistory(t, "s1", "streaming", false, true)

	msgs, err := GetUnindexedMessages(10)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "unindexed", msgs[0].Content)
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "s1", msgs[0].SessionID)
}

func TestMarkMessageIndexed(t *testing.T) {
	setupStoreTestDB(t)

	id := insertHistory(t, "s1", "msg", false, false)
	require.NoError(t, MarkMessageIndexed(id))

	var indexed int
	require.NoError(t, UnsafeDBForTest().QueryRow("SELECT indexed FROM chat_history WHERE id = ?", id).Scan(&indexed))
	assert.Equal(t, 1, indexed)
}

func TestMarkMessagesIndexed(t *testing.T) {
	setupStoreTestDB(t)

	// Empty slice is a no-op, not an error.
	require.NoError(t, MarkMessagesIndexed(nil))

	id1 := insertHistory(t, "s1", "m1", false, false)
	id2 := insertHistory(t, "s1", "m2", false, false)
	id3 := insertHistory(t, "s1", "m3", false, false)
	require.NoError(t, MarkMessagesIndexed([]int64{id1, id2}))

	var indexed int
	require.NoError(t, UnsafeDBForTest().QueryRow("SELECT indexed FROM chat_history WHERE id = ?", id1).Scan(&indexed))
	assert.Equal(t, 1, indexed)
	require.NoError(t, UnsafeDBForTest().QueryRow("SELECT indexed FROM chat_history WHERE id = ?", id3).Scan(&indexed))
	assert.Equal(t, 0, indexed, "unlisted id must stay unindexed")
}

func TestResetAllIndexed(t *testing.T) {
	setupStoreTestDB(t)

	insertHistory(t, "s1", "a", true, false)
	insertHistory(t, "s1", "b", true, false)
	// Streaming rows are intentionally excluded.
	insertHistory(t, "s1", "streaming", true, true)

	n, err := ResetAllIndexed()
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)

	var indexed int
	require.NoError(t, UnsafeDBForTest().QueryRow("SELECT indexed FROM chat_history WHERE content = 'streaming'").Scan(&indexed))
	assert.Equal(t, 1, indexed, "streaming rows must not be reset")
}

func TestMessageCounts(t *testing.T) {
	setupStoreTestDB(t)

	insertHistory(t, "s1", "i1", true, false)
	insertHistory(t, "s1", "i2", true, false)
	insertHistory(t, "s1", "u1", false, false)
	insertHistory(t, "s1", "streaming", true, true)

	unindexed, err := UnindexedCount()
	require.NoError(t, err)
	assert.Equal(t, 1, unindexed)

	total, err := TotalMessageCount()
	require.NoError(t, err)
	assert.Equal(t, 3, total, "streaming rows are excluded from the total")

	indexed, err := IndexedMessageCount()
	require.NoError(t, err)
	assert.Equal(t, 2, indexed)

	total2, indexed2, err := MessageIndexCounts()
	require.NoError(t, err)
	assert.Equal(t, 3, total2)
	assert.Equal(t, 2, indexed2)
}

func TestMessageIndexCounts_Empty(t *testing.T) {
	setupStoreTestDB(t)

	total, indexed, err := MessageIndexCounts()
	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.Equal(t, 0, indexed)
}

// ---------- GetExpiredArchivedSessions ----------

func TestGetExpiredArchivedSessions(t *testing.T) {
	setupStoreTestDB(t)

	// Archived and old → expired.
	insertSession(t, "/project", "expired", "Expired", "chat", "2024-01-01 10:00:00", true)
	_, err := UnsafeDBForTest().Exec("UPDATE chat_sessions SET updated_at = datetime('now', '-100 days') WHERE id = 'expired'")
	require.NoError(t, err)

	// Archived but recent → kept. updated_at must be "now" for this to be
	// recent; the helper writes created_at into both columns, so set it here.
	insertSession(t, "/project", "recent", "Recent", "chat", "2024-01-01 10:00:00", true)
	_, err = UnsafeDBForTest().Exec("UPDATE chat_sessions SET updated_at = CURRENT_TIMESTAMP WHERE id = 'recent'")
	require.NoError(t, err)

	// Active and old → not archived, kept.
	insertSession(t, "/project", "active", "Active", "chat", "2024-01-01 10:00:00", false)
	_, err = UnsafeDBForTest().Exec("UPDATE chat_sessions SET updated_at = datetime('now', '-100 days') WHERE id = 'active'")
	require.NoError(t, err)

	ids, err := GetExpiredArchivedSessions(time.Now().AddDate(0, 0, -90))
	require.NoError(t, err)
	assert.Equal(t, []string{"expired"}, ids)
}

// ---------- PurgeArchivedData ----------

func TestPurgeArchivedData_EmptyList(t *testing.T) {
	setupStoreTestDB(t)

	sessions, messages, err := PurgeArchivedData(nil)
	require.NoError(t, err)
	assert.Equal(t, int64(0), sessions)
	assert.Equal(t, int64(0), messages)
}

func TestPurgeArchivedData_HardDeletesSessions(t *testing.T) {
	setupStoreTestDB(t)

	insertSession(t, "/project", "sid", "To Purge", "chat", "2024-01-01 10:00:00", true)
	insertHistory(t, "sid", "msg1", false, false)
	insertHistory(t, "sid", "reply1", false, false)

	sessionsPurged, messagesPurged, err := PurgeArchivedData([]string{"sid"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), sessionsPurged)
	assert.Equal(t, int64(2), messagesPurged)

	var count int
	require.NoError(t, UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_sessions WHERE id = 'sid'").Scan(&count))
	assert.Equal(t, 0, count)
	require.NoError(t, UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = 'sid'").Scan(&count))
	assert.Equal(t, 0, count)
}

// The link table has no FK to chat_sessions, so the purge must delete its rows
// explicitly or they survive forever.
func TestPurgeArchivedData_CleansSessionTagLinks(t *testing.T) {
	setupStoreTestDB(t)

	insertSession(t, "/project", "sid", "Tagged", "chat", "2024-01-01 10:00:00", true)
	insertSession(t, "/project", "keep", "Keep", "chat", "2024-01-01 10:00:00", true)

	_, err := UnsafeDBForTest().Exec("INSERT INTO session_tags (name, scope, project_id) VALUES ('bug', 'project', 0)")
	require.NoError(t, err)
	var tagID int64
	require.NoError(t, UnsafeDBForTest().QueryRow("SELECT id FROM session_tags WHERE name = 'bug'").Scan(&tagID))
	_, err = UnsafeDBForTest().Exec("INSERT INTO session_tag_links (session_id, tag_id) VALUES ('sid', ?), ('keep', ?)", tagID, tagID)
	require.NoError(t, err)

	_, _, err = PurgeArchivedData([]string{"sid"})
	require.NoError(t, err)

	var count int
	require.NoError(t, UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM session_tag_links WHERE session_id = 'sid'").Scan(&count))
	assert.Equal(t, 0, count, "purge must not leave orphan tag links")
	require.NoError(t, UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM session_tag_links WHERE session_id = 'keep'").Scan(&count))
	assert.Equal(t, 1, count, "surviving session keeps its link")
}

// A non-archived session is not deleted (the WHERE archived = 1 guard), but its
// messages are still removed — the messages deletion has no such guard.
func TestPurgeArchivedData_DoesNotPurgeActiveSession(t *testing.T) {
	setupStoreTestDB(t)

	insertSession(t, "/project", "active", "Active", "chat", "2024-01-01 10:00:00", false)
	insertHistory(t, "active", "msg", false, false)

	sessionsPurged, messagesPurged, err := PurgeArchivedData([]string{"active"})
	require.NoError(t, err)
	assert.Equal(t, int64(0), sessionsPurged)
	assert.Equal(t, int64(1), messagesPurged)

	var count int
	require.NoError(t, UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_sessions WHERE id = 'active'").Scan(&count))
	assert.Equal(t, 1, count)
}

func TestPurgeArchivedData_MultipleSessions(t *testing.T) {
	setupStoreTestDB(t)

	insertSession(t, "/project", "sid1", "Purge 1", "chat", "2024-01-01 10:00:00", true)
	insertSession(t, "/project", "sid2", "Purge 2", "chat", "2024-01-01 10:00:00", true)
	insertHistory(t, "sid1", "m1", false, false)
	insertHistory(t, "sid2", "m2", false, false)

	sessionsPurged, messagesPurged, err := PurgeArchivedData([]string{"sid1", "sid2"})
	require.NoError(t, err)
	assert.Equal(t, int64(2), sessionsPurged)
	assert.Equal(t, int64(2), messagesPurged)
}

func TestPurgeArchivedData_NonExistentSessionID(t *testing.T) {
	setupStoreTestDB(t)

	sessionsPurged, messagesPurged, err := PurgeArchivedData([]string{"non-existent-id"})
	require.NoError(t, err)
	assert.Equal(t, int64(0), sessionsPurged)
	assert.Equal(t, int64(0), messagesPurged)
}
