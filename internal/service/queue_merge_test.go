package service_test

import (
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// queuedTexts returns the queued messages of a session in queue order, for
// assertions about what the merge left behind.
func queuedTexts(t *testing.T, sessionID string) []string {
	t.Helper()
	msgs, err := service.GetQueuedMessages(sessionID)
	require.NoError(t, err)
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.Text)
	}
	return out
}

// TestMergeQueuedMessages_JoinsTextInQueueOrderWithBlankLines pins the agreed
// text shape: the parts keep their original order (DB id order == queue order)
// and are separated by a blank line, with no extra markers added.
func TestMergeQueuedMessages_JoinsTextInQueueOrderWithBlankLines(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Merge Order")

	_, err := service.AddQueuedMessage("/project", "claude", sid, "first", nil, "q-1", "")
	require.NoError(t, err)
	_, err = service.AddQueuedMessage("/project", "claude", sid, "second", nil, "q-2", "")
	require.NoError(t, err)
	_, err = service.AddQueuedMessage("/project", "claude", sid, "third", nil, "q-3", "")
	require.NoError(t, err)

	merged, oldIDs, ok, err := service.MergeQueuedMessages(sid, "q-merged")
	require.NoError(t, err)
	require.True(t, ok, "three queued messages are mergeable")
	assert.Equal(t, "first\n\nsecond\n\nthird", merged.Content)
	assert.Equal(t, "q-merged", merged.QueueID)
	assert.ElementsMatch(t, []string{"q-1", "q-2", "q-3"}, oldIDs)

	// The old rows are gone and exactly one row remains, holding the merge.
	assert.Equal(t, []string{"first\n\nsecond\n\nthird"}, queuedTexts(t, sid))
}

// TestMergeQueuedMessages_SkipsEmptyParts verifies an attachment-only entry
// (empty text) does not inject stray blank lines into the merged prompt.
func TestMergeQueuedMessages_SkipsEmptyParts(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Merge Empty Parts")

	_, err := service.AddQueuedMessage("/project", "claude", sid, "before", nil, "q-1", "")
	require.NoError(t, err)
	// Attachment-only: no text at all.
	_, err = service.AddQueuedMessage("/project", "claude", sid, "", []model.FileEntry{{Path: "/project/a.png"}}, "q-2", "")
	require.NoError(t, err)
	_, err = service.AddQueuedMessage("/project", "claude", sid, "after", nil, "q-3", "")
	require.NoError(t, err)

	merged, _, ok, err := service.MergeQueuedMessages(sid, "q-merged")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "before\n\nafter", merged.Content,
		"the empty part must not contribute a blank line")
	// Its attachment still travels.
	require.Len(t, merged.Files, 1)
	assert.Equal(t, "/project/a.png", merged.Files[0].Path)
}

// TestMergeQueuedMessages_ConcatenatesAndDedupesFiles verifies attachments from
// every part are carried over in order, with exact duplicates collapsed — the
// same dedupe identity the frontend uses.
func TestMergeQueuedMessages_ConcatenatesAndDedupesFiles(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Merge Files")

	_, err := service.AddQueuedMessage("/project", "claude", sid, "one", []model.FileEntry{
		{Path: "/project/a.go"},
		{Path: "/project/b.go"},
	}, "q-1", "")
	require.NoError(t, err)
	_, err = service.AddQueuedMessage("/project", "claude", sid, "two", []model.FileEntry{
		{Path: "/project/b.go"},               // exact duplicate of the above
		{Path: "/project/c.go", StartLine: 3}, // same path, different range: kept
		{Path: "/project/a.go", StartLine: 3}, // same path, different range: kept
	}, "q-2", "")
	require.NoError(t, err)

	merged, _, ok, err := service.MergeQueuedMessages(sid, "q-merged")
	require.NoError(t, err)
	require.True(t, ok)

	paths := make([]string, 0, len(merged.Files))
	for _, f := range merged.Files {
		paths = append(paths, f.Path)
	}
	assert.Equal(t, []string{
		"/project/a.go",
		"/project/b.go",
		"/project/c.go",
		"/project/a.go",
	}, paths, "dedupe keeps the first of each identity, in queue order")

	// The stored row must carry the same attachments, not just the return value.
	msgs, err := service.GetQueuedMessages(sid)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Len(t, msgs[0].Files, 4)
}

// TestMergeQueuedMessages_FewerThanTwoIsDecline verifies a single (or empty)
// queue has nothing to merge and is reported as a decline, leaving the row
// untouched.
func TestMergeQueuedMessages_FewerThanTwoIsDecline(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Merge Decline")

	// Empty queue.
	_, oldIDs, ok, err := service.MergeQueuedMessages(sid, "q-merged")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, oldIDs)

	// Exactly one message: still nothing to merge.
	_, err = service.AddQueuedMessage("/project", "claude", sid, "only", nil, "q-1", "")
	require.NoError(t, err)
	_, _, ok, err = service.MergeQueuedMessages(sid, "q-merged")
	require.NoError(t, err)
	assert.False(t, ok, "a single message must not be merged into itself")
	assert.Equal(t, []string{"only"}, queuedTexts(t, sid), "the row must be untouched")
}

// TestMergeQueuedMessages_DoesNotTouchOtherSessions verifies the merge is
// scoped to one session: another session's queue is untouched even when it has
// mergeable messages of its own.
func TestMergeQueuedMessages_DoesNotTouchOtherSessions(t *testing.T) {
	setupDB(t)
	sidA := helperCreateSession(t, "/project", "claude", "Merge A")
	sidB := helperCreateSession(t, "/project", "claude", "Merge B")

	for i, sid := range []string{sidA, sidB} {
		_, err := service.AddQueuedMessage("/project", "claude", sid, "one", nil, "q-1", "")
		require.NoError(t, err)
		_, err = service.AddQueuedMessage("/project", "claude", sid, "two", nil, "q-2", "")
		require.NoError(t, err)
		_ = i
	}

	_, _, ok, err := service.MergeQueuedMessages(sidA, "q-merged-a")
	require.NoError(t, err)
	require.True(t, ok)

	assert.Len(t, queuedTexts(t, sidA), 1)
	assert.Equal(t, []string{"one", "two"}, queuedTexts(t, sidB),
		"merging one session must not touch another session's queue")
}

// TestMergeQueuedMessages_EmptySessionIDIsError verifies the guard: an empty
// session id is a programming error, not an empty queue.
func TestMergeQueuedMessages_EmptySessionIDIsError(t *testing.T) {
	setupDB(t)
	_, _, ok, err := service.MergeQueuedMessages("", "q-merged")
	assert.Error(t, err)
	assert.False(t, ok)
}

// TestMergeQueuedMessages_DedupesURLAndQuoteAttachments pins the identity rules
// for the two non-file attachment kinds: URLs key on their address, quotes on
// their id + content. Two identical URLs must collapse; two quotes of the same
// range with different text must both survive.
func TestMergeQueuedMessages_DedupesURLAndQuoteAttachments(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Merge Attach Kinds")

	_, err := service.AddQueuedMessage("/project", "claude", sid, "one", []model.FileEntry{
		{Kind: "url", URL: "https://example.com/a"},
		{Kind: "quote", ID: "x", Path: "/f.go", StartLine: 1, EndLine: 2, Text: "alpha"},
	}, "q-1", "")
	require.NoError(t, err)
	_, err = service.AddQueuedMessage("/project", "claude", sid, "two", []model.FileEntry{
		{Kind: "url", URL: "https://example.com/a"},                                     // same URL as above
		{Kind: "quote", ID: "x", Path: "/f.go", StartLine: 1, EndLine: 2, Text: "beta"}, // same range, different text
	}, "q-2", "")
	require.NoError(t, err)

	merged, _, ok, err := service.MergeQueuedMessages(sid, "q-merged")
	require.NoError(t, err)
	require.True(t, ok)

	require.Len(t, merged.Files, 3, "duplicate URL collapses, both distinct quotes survive")
	urls, quotes := 0, 0
	for _, f := range merged.Files {
		switch {
		case f.IsURL():
			urls++
		case f.IsQuote():
			quotes++
		}
	}
	assert.Equal(t, 1, urls)
	assert.Equal(t, 2, quotes)
}

// TestMergeQueuedMessages_MintsQueueIDWhenEmpty verifies the "" queueID is not
// stored verbatim: the merge mints one so the caller always has a usable id to
// broadcast.
func TestMergeQueuedMessages_MintsQueueIDWhenEmpty(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Merge Mint ID")

	_, err := service.AddQueuedMessage("/project", "claude", sid, "a", nil, "q-1", "")
	require.NoError(t, err)
	_, err = service.AddQueuedMessage("/project", "claude", sid, "b", nil, "q-2", "")
	require.NoError(t, err)

	merged, _, ok, err := service.MergeQueuedMessages(sid, "")
	require.NoError(t, err)
	require.True(t, ok)
	assert.NotEmpty(t, merged.QueueID, "an empty queueID must be minted, not stored blank")
}

// TestMergeQueuedMessages_EmptyResultIsDecline pins the guard against producing
// a queue row with nothing in it.
//
// The reachable trigger is whitespace-only text. The enqueue handler rejects an
// empty message with `req.Message == ""`, which a whitespace-only string
// passes; the merge then drops it via `strings.TrimSpace(r.Content) != ""`. So
// two whitespace-only, attachment-less messages collapse to an empty body AND
// an empty file list — a row the user cannot send, and one the UI renders as
// "attachment" while opening it shows nothing. Declining keeps the originals.
func TestMergeQueuedMessages_EmptyResultIsDecline(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Merge Empty Result")

	// Whitespace-only, no attachments: passes the handler's `== ""` check.
	_, err := service.AddQueuedMessage("/project", "claude", sid, "   ", nil, "q-1", "")
	require.NoError(t, err)
	_, err = service.AddQueuedMessage("/project", "claude", sid, "\n\t", nil, "q-2", "")
	require.NoError(t, err)

	merged, oldIDs, ok, err := service.MergeQueuedMessages(sid, "q-merged")
	require.NoError(t, err)
	assert.False(t, ok, "a merge with no text and no attachment must decline")
	assert.Empty(t, oldIDs)
	assert.Equal(t, "", merged.Content)

	// The originals survive — declining must not delete them.
	msgs, err := service.GetQueuedMessages(sid)
	require.NoError(t, err)
	assert.Len(t, msgs, 2, "a declined merge must leave both rows queued")
}

// TestMergeQueuedMessages_AttachmentOnlyWithDistinctFilesIsAllowed is the
// counterweight: an empty body is fine as long as the merge carries something.
// Without this, a blanket "empty content declines" would silently break
// attachment-only merging, which is a legitimate flow.
func TestMergeQueuedMessages_AttachmentOnlyWithDistinctFilesIsAllowed(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Merge Attach Only")

	_, err := service.AddQueuedMessage("/project", "claude", sid, "", []model.FileEntry{{Path: "/project/a.png"}}, "q-1", "")
	require.NoError(t, err)
	_, err = service.AddQueuedMessage("/project", "claude", sid, "", []model.FileEntry{{Path: "/project/b.png"}}, "q-2", "")
	require.NoError(t, err)

	merged, _, ok, err := service.MergeQueuedMessages(sid, "q-merged")
	require.NoError(t, err)
	require.True(t, ok, "distinct attachments are a real merge")
	assert.Equal(t, "", merged.Content)
	require.Len(t, merged.Files, 2)
}
