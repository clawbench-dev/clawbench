package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUserMessageStats(t *testing.T) {
	setupStoreTestDB(t)

	insertHistory(t, "s1", "hello", false, false)
	insertHistory(t, "s1", "hello", false, false)
	insertHistory(t, "s1", "hello", false, false)
	insertHistory(t, "s1", "fix bug", false, false)
	insertHistory(t, "s1", "fix bug", false, false)

	// Filtered out: streaming, empty, too long, slash-command, file-attached.
	insertHistory(t, "s1", "streaming", false, true)
	insertHistory(t, "s1", "", false, false)
	insertHistory(t, "s1", "/help", false, false)
	insertHistory(t, "s1", "@agent", false, false)
	long := make([]byte, 201)
	for i := range long {
		long[i] = 'x'
	}
	insertHistory(t, "s1", string(long), false, false)

	stats, err := GetUserMessageStats(0)
	require.NoError(t, err)
	require.Len(t, stats, 2)
	byText := map[string]int{}
	for _, s := range stats {
		byText[s.Text] = s.Count
	}
	assert.Equal(t, 3, byText["hello"])
	assert.Equal(t, 2, byText["fix bug"])
}

// Messages already in quick-send (by label or command) are excluded so the
// cluster list does not re-propose the user's own canned replies.
func TestGetUserMessageStats_ExcludesQuickSend(t *testing.T) {
	setupStoreTestDB(t)

	insertHistory(t, "s1", "canned", false, false)
	insertHistory(t, "s1", "/quick", false, false)
	insertHistory(t, "s1", "real question", false, false)

	_, err := UnsafeDBForTest().Exec("INSERT INTO chat_quick_send (label, command) VALUES ('canned', '/canned-cmd'), ('qlabel', '/quick')")
	require.NoError(t, err)

	stats, err := GetUserMessageStats(0)
	require.NoError(t, err)
	require.Len(t, stats, 1)
	assert.Equal(t, "real question", stats[0].Text)
}

// limit <= 0 defaults to 500; a small explicit limit caps the result.
func TestGetUserMessageStats_Limit(t *testing.T) {
	setupStoreTestDB(t)

	for _, c := range []string{"a", "b", "c"} {
		insertHistory(t, "s1", c, false, false)
	}

	stats, err := GetUserMessageStats(2)
	require.NoError(t, err)
	assert.Len(t, stats, 2)

	// A non-positive limit takes the default rather than returning nothing.
	stats, err = GetUserMessageStats(-1)
	require.NoError(t, err)
	assert.Len(t, stats, 3)
}

// File-attached messages carry a non-empty files column and must be skipped.
func TestGetUserMessageStats_ExcludesFileAttached(t *testing.T) {
	setupStoreTestDB(t)

	_, err := UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_id, role, content, files, session_id, backend, streaming) VALUES (1, 'user', 'with file', '[{\"path\":\"x\"}]', 's1', 'claude', 0)")
	require.NoError(t, err)
	insertHistory(t, "s1", "no file", false, false)

	stats, err := GetUserMessageStats(0)
	require.NoError(t, err)
	require.Len(t, stats, 1)
	assert.Equal(t, "no file", stats[0].Text)
}

func TestSaveAndGetClusterCache(t *testing.T) {
	setupStoreTestDB(t)

	// Empty cache + no meta row → empty entries, empty mode, zero time.
	entries, mode, _, err := GetClusterCache()
	require.NoError(t, err)
	assert.Empty(t, entries)
	assert.Equal(t, "", mode)

	require.NoError(t, SaveClusterCache([]ClusterCacheEntry{
		{Representative: "hello", Variants: `["hello"]`, TotalCount: 3, RepresentativeCount: 3, SortOrder: 0},
		{Representative: "fix bug", Variants: `["fix bug"]`, TotalCount: 2, RepresentativeCount: 2, SortOrder: 1},
	}, "fts"))

	entries, mode, _, err = GetClusterCache()
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, "fts", mode)
	assert.Equal(t, "hello", entries[0].Representative)
	assert.Equal(t, 3, entries[0].TotalCount)
	// Ordered by sort_order.
	assert.Equal(t, "fix bug", entries[1].Representative)
}

// Saving again replaces the previous cache rather than appending to it.
func TestSaveClusterCache_ReplacesOldRows(t *testing.T) {
	setupStoreTestDB(t)

	require.NoError(t, SaveClusterCache([]ClusterCacheEntry{
		{Representative: "old", Variants: `[]`, SortOrder: 0},
	}, "fts"))
	require.NoError(t, SaveClusterCache([]ClusterCacheEntry{
		{Representative: "new", Variants: `[]`, SortOrder: 0},
	}, "vector"))

	entries, mode, _, err := GetClusterCache()
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "new", entries[0].Representative)
	assert.Equal(t, "vector", mode)
}

func TestSaveClusterMeta_PreservesModeAndPhaseWhenEmpty(t *testing.T) {
	setupStoreTestDB(t)

	// First write establishes mode=fts, phase=extracting.
	require.NoError(t, SaveClusterMeta("computing", "fts", 10, 0, 5, "extracting"))

	// A later write with empty mode/phase must keep the stored values.
	require.NoError(t, SaveClusterMeta("computing", "", 10, 3, 20))

	m := GetClusterMeta()
	assert.Equal(t, "fts", m.Mode)
	assert.Equal(t, "computing", m.Progress)
	assert.Equal(t, "extracting", m.Phase)
	assert.Equal(t, 10, m.MsgCount)
	assert.Equal(t, 3, m.ClusterCount)
	assert.Equal(t, 20, m.ElapsedMs)
}

func TestGetClusterMeta_DefaultsToIdle(t *testing.T) {
	setupStoreTestDB(t)

	m := GetClusterMeta()
	assert.Equal(t, "idle", m.Progress)
	assert.Equal(t, "", m.Mode)
}

func TestSaveClusterMetaError_PreservesMode(t *testing.T) {
	setupStoreTestDB(t)

	require.NoError(t, SaveClusterMeta("computing", "vector", 10, 0, 5, "clustering"))
	require.NoError(t, SaveClusterMetaError("error", "saving", "boom"))

	m := GetClusterMeta()
	assert.Equal(t, "error", m.Progress)
	assert.Equal(t, "saving", m.Phase)
	assert.Equal(t, "boom", m.ErrorMsg)
	// The mode from the previous row survives an error write.
	assert.Equal(t, "vector", m.Mode)
}
