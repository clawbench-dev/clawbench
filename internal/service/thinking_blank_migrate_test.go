package service

import (
	"encoding/json"
	"testing"

	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testSessionID is the only session these cases need.
const testSessionID = "sess-1"

// insertAssistantMsg stores an assistant row and returns its id.
func insertAssistantMsg(t *testing.T, content string, streaming int) int64 {
	t.Helper()
	res, err := store.UnsafeDBForTest().Exec(
		"INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming) VALUES (?, 'assistant', ?, ?, 'claude', ?)",
		"/proj", content, testSessionID, streaming,
	)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	return id
}

// insertThinkingRow stores one chat_thinking chunk.
func insertThinkingRow(t *testing.T, msgID int64, thinkID, text string) {
	t.Helper()
	_, err := store.UnsafeDBForTest().Exec(
		"INSERT INTO chat_thinking (message_id, session_id, think_id, seq, text) VALUES (?, ?, ?, 0, ?)",
		msgID, testSessionID, thinkID, text,
	)
	require.NoError(t, err)
}

// blockTypes reads back the message's block types, in order.
func blockTypes(t *testing.T, msgID int64) []string {
	t.Helper()
	var content string
	require.NoError(t, store.UnsafeDBForTest().QueryRow(
		"SELECT content FROM chat_history WHERE id = ?", msgID).Scan(&content))
	var parsed struct {
		Blocks []map[string]any `json:"blocks"`
	}
	require.NoError(t, json.Unmarshal([]byte(content), &parsed))
	types := make([]string, 0, len(parsed.Blocks))
	for _, b := range parsed.Blocks {
		typ, _ := b["type"].(string)
		types = append(types, typ)
	}
	return types
}

func thinkingRowCount(t *testing.T, msgID int64) int {
	t.Helper()
	var n int
	require.NoError(t, store.UnsafeDBForTest().QueryRow(
		"SELECT COUNT(*) FROM chat_thinking WHERE message_id = ?", msgID).Scan(&n))
	return n
}

// The reported defect: a thinking block whose whole content is a single space
// renders as an expandable 「深度思考」chip with nothing inside. The writer is
// fixed, but finalized messages are never re-saved, so the existing ones must be
// cleaned up here.
func TestMigrateBlankThinkingBlocks_DropsWhitespaceOnlyBlock(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()
	_, err := store.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('" + testSessionID + "', 1, 'claude', 'Test')")
	require.NoError(t, err)

	msgID := insertAssistantMsg(t, `{"blocks":[
		{"type":"thinking","think_id":"th_real","done":true},
		{"type":"text","text":"answer"},
		{"type":"thinking","think_id":"th_blank","done":true}
	]}`, 0)
	insertThinkingRow(t, msgID, "th_real", "actual reasoning")
	insertThinkingRow(t, msgID, "th_blank", " ")

	require.True(t, MigrateBlankThinkingBlocks())

	assert.Equal(t, []string{"thinking", "text"}, blockTypes(t, msgID),
		"the whitespace-only block must be gone, the real one must stay")
	assert.Equal(t, 1, thinkingRowCount(t, msgID), "its chat_thinking row must go too")

	var thinkID string
	require.NoError(t, store.UnsafeDBForTest().QueryRow(
		"SELECT think_id FROM chat_thinking WHERE message_id = ?", msgID).Scan(&thinkID))
	assert.Equal(t, "th_real", thinkID, "the surviving row must be the real one")
}

// Every whitespace shape counts, not just a single space.
func TestMigrateBlankThinkingBlocks_AllWhitespaceShapes(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()
	_, err := store.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('" + testSessionID + "', 1, 'claude', 'Test')")
	require.NoError(t, err)

	msgID := insertAssistantMsg(t, `{"blocks":[
		{"type":"thinking","think_id":"th_a","done":true},
		{"type":"thinking","think_id":"th_b","done":true},
		{"type":"thinking","think_id":"th_c","done":true},
		{"type":"text","text":"answer"}
	]}`, 0)
	insertThinkingRow(t, msgID, "th_a", "\n")
	insertThinkingRow(t, msgID, "th_b", "\t ")
	insertThinkingRow(t, msgID, "th_c", "   ")

	require.True(t, MigrateBlankThinkingBlocks())

	assert.Equal(t, []string{"text"}, blockTypes(t, msgID))
	assert.Equal(t, 0, thinkingRowCount(t, msgID))
}

// The pre-split shape: whitespace carried INLINE in the content row, with no
// chat_thinking row anywhere. It must be dropped by the same pass.
func TestMigrateBlankThinkingBlocks_DropsInlineWhitespaceWithNoRow(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()
	_, err := store.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('" + testSessionID + "', 1, 'claude', 'Test')")
	require.NoError(t, err)

	msgID := insertAssistantMsg(t, `{"blocks":[
		{"type":"thinking","text":" ","think_id":"th_blank","done":true},
		{"type":"text","text":"answer"}
	]}`, 0)

	require.True(t, MigrateBlankThinkingBlocks())

	assert.Equal(t, []string{"text"}, blockTypes(t, msgID))
}

// A slim marker carries no text of its own, so "blank" can only be decided by
// its chat_thinking row. A marker whose row holds real reasoning must survive —
// otherwise a real thought would vanish on every upgrade.
func TestMigrateBlankThinkingBlocks_KeepsMarkerWithRealRow(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()
	_, err := store.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('" + testSessionID + "', 1, 'claude', 'Test')")
	require.NoError(t, err)

	msgID := insertAssistantMsg(t,
		`{"blocks":[{"type":"thinking","think_id":"th_keep","done":true},{"type":"text","text":"answer"}]}`, 0)
	insertThinkingRow(t, msgID, "th_keep", "real reasoning")

	require.True(t, MigrateBlankThinkingBlocks())

	assert.Equal(t, []string{"thinking", "text"}, blockTypes(t, msgID))
	assert.Equal(t, 1, thinkingRowCount(t, msgID))
}

// A marker with NO row at all is a different defect (a lost chat_thinking row).
// Its reasoning may be real, so this migration — whose subject is blank blocks —
// must leave it alone rather than silently delete a recoverable thought.
func TestMigrateBlankThinkingBlocks_KeepsMarkerWithMissingRow(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()
	_, err := store.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('" + testSessionID + "', 1, 'claude', 'Test')")
	require.NoError(t, err)

	msgID := insertAssistantMsg(t,
		`{"blocks":[{"type":"thinking","think_id":"th_lost","done":true},{"type":"text","text":"answer"}]}`, 0)

	require.True(t, MigrateBlankThinkingBlocks())

	assert.Equal(t, []string{"thinking", "text"}, blockTypes(t, msgID))
}

// An ACP multi-message session stores a marker in one row while the text lives
// under another message; the detail endpoint resolves that via session_id. The
// migration must apply the same rule, or it would delete loadable blocks.
func TestMigrateBlankThinkingBlocks_KeepsMarkerResolvedBySession(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()
	_, err := store.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('" + testSessionID + "', 1, 'claude', 'Test')")
	require.NoError(t, err)

	// The text row hangs off a DIFFERENT message of the same session.
	otherMsg := insertAssistantMsg(t, `{"blocks":[{"type":"text","text":"x"}]}`, 0)
	insertThinkingRow(t, otherMsg, "th_shared", "real reasoning")

	msgID := insertAssistantMsg(t,
		`{"blocks":[{"type":"thinking","think_id":"th_shared","done":true},{"type":"text","text":"answer"}]}`, 0)

	require.True(t, MigrateBlankThinkingBlocks())

	assert.Equal(t, []string{"thinking", "text"}, blockTypes(t, msgID),
		"a marker resolvable through session_id is loadable and must stay")
}

// Dropping every block would leave an empty bubble, which is worse than the
// empty chip it replaces.
func TestMigrateBlankThinkingBlocks_KeepsMessageWhenNothingWouldRemain(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()
	_, err := store.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('" + testSessionID + "', 1, 'claude', 'Test')")
	require.NoError(t, err)

	msgID := insertAssistantMsg(t,
		`{"blocks":[{"type":"thinking","think_id":"th_only","done":true}]}`, 0)
	insertThinkingRow(t, msgID, "th_only", " ")

	require.True(t, MigrateBlankThinkingBlocks())

	assert.Equal(t, []string{"thinking"}, blockTypes(t, msgID))
	assert.Equal(t, 1, thinkingRowCount(t, msgID))
}

// A streaming row is still being written; its whitespace block may yet grow real
// text. Finalize owns that row, so the migration must not touch it.
func TestMigrateBlankThinkingBlocks_SkipsStreamingRows(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()
	_, err := store.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('" + testSessionID + "', 1, 'claude', 'Test')")
	require.NoError(t, err)

	msgID := insertAssistantMsg(t,
		`{"blocks":[{"type":"thinking","think_id":"th_live","in_progress":true},{"type":"text","text":"a"}]}`, 1)
	insertThinkingRow(t, msgID, "th_live", " ")

	require.True(t, MigrateBlankThinkingBlocks())

	assert.Equal(t, []string{"thinking", "text"}, blockTypes(t, msgID))
	assert.Equal(t, 1, thinkingRowCount(t, msgID))
}

// Content that merely mentions `think_id` in prose must not be rewritten, and
// must not stop the migration from completing (a false "failed" would leave the
// migration unrecorded and re-running forever).
func TestMigrateBlankThinkingBlocks_IgnoresNonBlocksContent(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()
	_, err := store.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('" + testSessionID + "', 1, 'claude', 'Test')")
	require.NoError(t, err)

	prose := `{"blocks":[{"type":"text","text":"the think_id column, and a \"text\":\" \" quote"}]}`
	msgID := insertAssistantMsg(t, prose, 0)

	require.True(t, MigrateBlankThinkingBlocks())

	var content string
	require.NoError(t, store.UnsafeDBForTest().QueryRow(
		"SELECT content FROM chat_history WHERE id = ?", msgID).Scan(&content))
	assert.Equal(t, prose, content, "a text block is never touched")
}

// A whitespace row with no marker left in the content (an orphan) is still this
// migration's business: it can never be loaded, and leaving it means the row
// count never converges. The content must not be rewritten for it.
func TestMigrateBlankThinkingBlocks_DeletesOrphanWhitespaceRow(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()
	_, err := store.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('" + testSessionID + "', 1, 'claude', 'Test')")
	require.NoError(t, err)

	content := `{"blocks":[{"type":"text","text":"answer"}]}`
	msgID := insertAssistantMsg(t, content, 0)
	insertThinkingRow(t, msgID, "th_orphan", " ")

	require.True(t, MigrateBlankThinkingBlocks())

	assert.Equal(t, 0, thinkingRowCount(t, msgID), "the unreachable row must be deleted")

	var got string
	require.NoError(t, store.UnsafeDBForTest().QueryRow(
		"SELECT content FROM chat_history WHERE id = ?", msgID).Scan(&got))
	assert.Equal(t, content, got, "content with no marker must not be rewritten")
}

// A block is blank only when ALL of its chunks are blank. A first chunk that is
// a lone space followed by real reasoning is a normal thought.
func TestMigrateBlankThinkingBlocks_KeepsBlockWithMixedChunks(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()
	_, err := store.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_id, backend, title) VALUES (?, 1, 'claude', 'Test')", testSessionID)
	require.NoError(t, err)

	msgID := insertAssistantMsg(t,
		`{"blocks":[{"type":"thinking","think_id":"th_mixed","done":true},{"type":"text","text":"answer"}]}`, 0)
	insertThinkingRow(t, msgID, "th_mixed", " ")
	_, err = store.UnsafeDBForTest().Exec(
		"INSERT INTO chat_thinking (message_id, session_id, think_id, seq, text) VALUES (?, ?, ?, 1, ?)",
		msgID, testSessionID, "th_mixed", "real reasoning")
	require.NoError(t, err)

	require.True(t, MigrateBlankThinkingBlocks())

	assert.Equal(t, []string{"thinking", "text"}, blockTypes(t, msgID))
	var n int
	require.NoError(t, store.UnsafeDBForTest().QueryRow(
		"SELECT COUNT(*) FROM chat_thinking WHERE message_id = ?", msgID).Scan(&n))
	assert.Equal(t, 2, n, "both chunks must survive")
}

// The per-block verdict is pure; pin each branch directly so a future rewrite
// cannot silently start dropping real reasoning.
func TestIsBlankThinkingBlock_Verdicts(t *testing.T) {
	blank := map[string]struct{}{"th_blank": {}}
	rowExists := func(id string) (bool, error) { return id == "th_has_row", nil }

	cases := []struct {
		name  string
		raw   any
		blank bool
	}{
		{"not a map", "just a string", false},
		{"non-thinking type", map[string]any{"type": "text", "text": " "}, false},
		{"real inline text", map[string]any{"type": "thinking", "text": "real"}, false},
		{"no id, no text", map[string]any{"type": "thinking"}, true},
		{"no id, whitespace text", map[string]any{"type": "thinking", "text": " "}, true},
		{"id with blank rows", map[string]any{"type": "thinking", "think_id": "th_blank"}, true},
		{"inline whitespace, no row", map[string]any{"type": "thinking", "think_id": "th_gone", "text": " "}, true},
		{"inline whitespace, row exists", map[string]any{"type": "thinking", "think_id": "th_has_row", "text": " "}, false},
		{"marker with no text, row exists", map[string]any{"type": "thinking", "think_id": "th_has_row"}, false},
		{"marker with no text, no row (other defect)", map[string]any{"type": "thinking", "think_id": "th_lost"}, false},
	}
	for _, tc := range cases {
		got, err := isBlankThinkingBlock(tc.raw, blank, rowExists)
		require.NoError(t, err, tc.name)
		assert.Equal(t, tc.blank, got, tc.name)
	}
}

// Content that is not JSON at all (the LIKE pre-filter can match prose) must be
// skipped without an error, or the migration would never be recorded.
func TestCleanBlankThinkingForRow_NonJSONContent(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()

	require.NoError(t, cleanBlankThinkingForRow(1, testSessionID, "not json at all"))
	// Valid JSON without a blocks array behaves the same way.
	require.NoError(t, cleanBlankThinkingForRow(2, testSessionID, `{"metadata":{"a":1}}`))
	require.NoError(t, cleanBlankThinkingForRow(3, testSessionID, `{"blocks":[]}`))
}

// Nothing to clean must still report success so the ledger records the pass.
func TestMigrateBlankThinkingBlocks_NoCandidates(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()

	require.True(t, MigrateBlankThinkingBlocks())
}

// A broken read (here: the thinking table is gone) must report failure so the
// migration is retried next start instead of being recorded as done.
func TestMigrateBlankThinkingBlocks_CountErrorReturnsFalse(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()

	_, err := store.UnsafeDBForTest().Exec("DROP TABLE chat_thinking")
	require.NoError(t, err)

	assert.False(t, MigrateBlankThinkingBlocks(),
		"a failing count must not be recorded as a completed migration")
}

// The row lookup is part of the verdict; its error must abort the row rather
// than silently keep or drop a block.
func TestPruneBlankThinkingBlocks_PropagatesRowLookupError(t *testing.T) {
	blocks := []any{
		map[string]any{"type": "thinking", "text": " ", "think_id": "th_x"},
	}
	_, err := pruneBlankThinkingBlocks(blocks, map[string]struct{}{}, func(string) (bool, error) {
		return false, assert.AnError
	})
	assert.ErrorIs(t, err, assert.AnError)
}

// Same for the per-message row query.
func TestCleanBlankThinkingForRow_BlankIDLookupError(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()

	content := `{"blocks":[{"type":"thinking","think_id":"th_x"},{"type":"text","text":"a"}]}`
	msgID := insertAssistantMsg(t, content, 0)
	_, err := store.UnsafeDBForTest().Exec("DROP TABLE chat_thinking")
	require.NoError(t, err)

	assert.Error(t, cleanBlankThinkingForRow(msgID, testSessionID, content))
}

// Re-running must be a no-op: the ledger gates it in production, but the function
// itself has to be safe to call twice.
func TestMigrateBlankThinkingBlocks_Idempotent(t *testing.T) {
	teardown := setupTestDBForThinkingMigration(t)
	defer teardown()
	_, err := store.UnsafeDBForTest().Exec(
		"INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('" + testSessionID + "', 1, 'claude', 'Test')")
	require.NoError(t, err)

	msgID := insertAssistantMsg(t,
		`{"blocks":[{"type":"thinking","think_id":"th_blank","done":true},{"type":"text","text":"a"}]}`, 0)
	insertThinkingRow(t, msgID, "th_blank", " ")

	require.True(t, MigrateBlankThinkingBlocks())
	require.True(t, MigrateBlankThinkingBlocks())

	assert.Equal(t, []string{"text"}, blockTypes(t, msgID))
	assert.Equal(t, 0, thinkingRowCount(t, msgID))
}
