package service_test

import (
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// queueEntryByID returns the queued row with the given queue id, or nil.
func queueEntryByID(t *testing.T, sessionID, queueID string) *model.QueuedMessage {
	t.Helper()
	msgs, err := service.GetQueuedMessages(sessionID)
	require.NoError(t, err)
	for i := range msgs {
		if msgs[i].QueueID == queueID {
			return &msgs[i]
		}
	}
	return nil
}

// TestUpdateQueuedMessage_RewritesTextOnly pins the core contract: the text
// changes, everything that identifies the entry (queue id, position,
// attachments, creation stamp) survives untouched when no files are supplied.
//
// Position is the load-bearing part. A queued message can wait many minutes, so
// an edit that re-queued it would cost the user the wait — exactly what the
// recall-and-resend design could not avoid.
func TestUpdateQueuedMessage_RewritesTextOnly(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Edit")

	files := []model.FileEntry{{Path: "/project/shot.png"}}
	_, err := service.AddQueuedMessage("/project", "claude", sid, "first", nil, "q-1", "")
	require.NoError(t, err)
	_, err = service.AddQueuedMessage("/project", "claude", sid, "second", files, "q-2", "")
	require.NoError(t, err)
	_, err = service.AddQueuedMessage("/project", "claude", sid, "third", nil, "q-3", "")
	require.NoError(t, err)

	before := queueEntryByID(t, sid, "q-2")
	require.NotNil(t, before)

	updated, ok, err := service.UpdateQueuedMessage(sid, "q-2", "second, revised", nil)
	require.NoError(t, err)
	require.True(t, ok, "an existing queue row must report a real update")
	assert.Equal(t, "second, revised", updated.Text)
	assert.Equal(t, "q-2", updated.QueueID, "the queue id is the client's identity — it must not change")
	require.Len(t, updated.Files, 1, "a nil file list means 'keep the attachments' — the text-only path must not drop them")
	assert.Equal(t, "/project/shot.png", updated.Files[0].Path)
	assert.Equal(t, before.CreatedAt, updated.CreatedAt)
	assert.Equal(t, before.ID, updated.ID)

	// Queue order is untouched: the edited entry is still second.
	assert.Equal(t, []string{"first", "second, revised", "third"}, queuedTexts(t, sid))
}

// TestUpdateQueuedMessage_MissingRowReportsFalse: the drain loop may claim
// the row between the client rendering the queue and the user saving. A no-op
// UPDATE must not be reported as success — the message already left with the OLD
// text, so "saved" would be a lie the user acts on.
func TestUpdateQueuedMessage_MissingRowReportsFalse(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Edit Missing")

	_, err := service.AddQueuedMessage("/project", "claude", sid, "still here", nil, "q-1", "")
	require.NoError(t, err)

	_, ok, err := service.UpdateQueuedMessage(sid, "q-never-existed", "ghost edit", nil)
	require.NoError(t, err, "an absent row is not an error — it is a decline")
	assert.False(t, ok)

	// The surviving row is untouched.
	assert.Equal(t, []string{"still here"}, queuedTexts(t, sid))
}

// TestUpdateQueuedMessage_WrongSessionDoesNotLeak: queue ids are only
// unique per session. Addressing another session's id must decline AND leave
// that session's row alone.
func TestUpdateQueuedMessage_WrongSessionDoesNotLeak(t *testing.T) {
	setupDB(t)
	mine := helperCreateSession(t, "/project", "claude", "Mine")
	theirs := helperCreateSession(t, "/project", "claude", "Theirs")

	_, err := service.AddQueuedMessage("/project", "claude", theirs, "their message", nil, "q-shared", "")
	require.NoError(t, err)

	_, ok, err := service.UpdateQueuedMessage(mine, "q-shared", "hijacked", nil)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, []string{"their message"}, queuedTexts(t, theirs),
		"a queue id from another session must never be rewritten")
}

// TestUpdateQueuedMessage_AllowsEmptyTextForAttachmentOnly: clearing the
// text of an attachment-only message is legitimate (the user keeps the file and
// drops the words). The "nothing at all" case is rejected at the handler, which
// can see the request body; the service only enforces row existence.
func TestUpdateQueuedMessage_AllowsEmptyTextForAttachmentOnly(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Edit Empty")

	files := []model.FileEntry{{Path: "/project/shot.png"}}
	_, err := service.AddQueuedMessage("/project", "claude", sid, "describe this", files, "q-1", "")
	require.NoError(t, err)

	updated, ok, err := service.UpdateQueuedMessage(sid, "q-1", "", nil)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "", updated.Text)
	require.Len(t, updated.Files, 1, "the attachment is the reason the empty text is legal")
}

// TestGetQueuedMessage_ReturnsRowByQueueID pins the read used by the update
// endpoint to decide whether an edit would leave the entry unsendable (empty
// text with no attachments). It must return the row's attachments so that
// decision can be made BEFORE writing.
func TestGetQueuedMessage_ReturnsRowByQueueID(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Get One")

	files := []model.FileEntry{{Path: "/project/shot.png"}}
	_, err := service.AddQueuedMessage("/project", "claude", sid, "with a file", files, "q-1", "")
	require.NoError(t, err)
	_, err = service.AddQueuedMessage("/project", "claude", sid, "no file", nil, "q-2", "")
	require.NoError(t, err)

	got, found, err := service.GetQueuedMessage(sid, "q-2")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "no file", got.Text)
	assert.Empty(t, got.Files, "the empty-text check keys off this: no attachments means the edit is invalid")

	got, found, err = service.GetQueuedMessage(sid, "q-1")
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, got.Files, 1, "attachments must come back so the check can see them")
	assert.Equal(t, "/project/shot.png", got.Files[0].Path)
}

// TestGetQueuedMessage_MissingOrUnaddressableReportsNotFound: a drained row and a
// malformed address are both "nothing to read", never an error.
func TestGetQueuedMessage_MissingOrUnaddressableReportsNotFound(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Get Missing")

	for _, tc := range []struct{ name, session, queue string }{
		{"unknown queue id", sid, "q-nope"},
		{"empty queue id", sid, ""},
		{"empty session", "", "q-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, found, err := service.GetQueuedMessage(tc.session, tc.queue)
			require.NoError(t, err)
			assert.False(t, found)
		})
	}
}

// TestUpdateQueuedMessage_UnaddressableIsDeclined: an empty session or
// queue id must not open a transaction that would match every row.
func TestUpdateQueuedMessage_UnaddressableIsDeclined(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Update Unaddressable")

	_, err := service.AddQueuedMessage("/project", "claude", sid, "keep me", nil, "q-1", "")
	require.NoError(t, err)

	_, ok, err := service.UpdateQueuedMessage(sid, "", "ignored", nil)
	require.NoError(t, err)
	assert.False(t, ok)

	_, ok, err = service.UpdateQueuedMessage("", "q-1", "ignored", nil)
	require.NoError(t, err)
	assert.False(t, ok)

	assert.Equal(t, []string{"keep me"}, queuedTexts(t, sid))
}

// TestUpdateQueuedMessage_ReadFailureIsError: a broken read is a real
// error, not a decline. The two must stay distinguishable — a decline means the
// message is gone (the caller tells the user "too late"), an error means the
// server is broken (the caller retries or reports a failure).
func TestUpdateQueuedMessage_ReadFailureIsError(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Update Broken Read")

	// Drop the table so the SELECT fails with a real error (not ErrNoRows).
	_, err := store.UnsafeDBForTest().Exec("DROP TABLE queued_messages")
	require.NoError(t, err)

	_, ok, err := service.UpdateQueuedMessage(sid, "q-1", "text", nil)
	require.Error(t, err, "a missing table must surface as an error")
	assert.False(t, ok)
}

// TestUpdateQueuedMessage_TextOnlyEditDoesNotRewriteFilesColumn: a text-only
// edit must not touch the `files` column at all.
//
// The old implementation rewrote it from the parsed value on every save, so a
// blob this build cannot parse (written by another version, or corrupted) would
// be replaced with an empty list — a text edit silently dropping attachments.
func TestUpdateQueuedMessage_TextOnlyEditDoesNotRewriteFilesColumn(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Edit Raw Files")

	_, err := service.AddQueuedMessage("/project", "claude", sid, "text", nil, "q-1", "")
	require.NoError(t, err)
	_, err = store.UnsafeDBForTest().Exec(
		"UPDATE queued_messages SET files = ? WHERE queue_id = ?", "{not valid json", "q-1")
	require.NoError(t, err)

	updated, ok, err := service.UpdateQueuedMessage(sid, "q-1", "new text", nil)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "new text", updated.Text)

	var raw string
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT files FROM queued_messages WHERE queue_id = ?", "q-1").Scan(&raw))
	assert.Equal(t, "{not valid json", raw, "a text-only edit must leave the stored blob untouched")
}

// TestGetQueuedMessage_ReadFailureIsError mirrors the above for the pre-read.
func TestGetQueuedMessage_ReadFailureIsError(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Get Broken Read")

	_, err := store.UnsafeDBForTest().Exec("DROP TABLE queued_messages")
	require.NoError(t, err)

	_, found, err := service.GetQueuedMessage(sid, "q-1")
	require.Error(t, err)
	assert.False(t, found)
}

// TestUpdateQueuedMessage_ReplacesAttachments: a non-nil file list is a FULL
// replacement, not an append. The client sends the list the message should end
// up with, so a removed attachment must be gone and the surviving order kept.
func TestUpdateQueuedMessage_ReplacesAttachments(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Edit Attachments")

	old := []model.FileEntry{{Path: "/project/old.png"}, {Path: "/project/keep.md"}}
	_, err := service.AddQueuedMessage("/project", "claude", sid, "look at these", old, "q-1", "")
	require.NoError(t, err)

	next := []model.FileEntry{{Path: "/project/keep.md"}, {Path: "/project/new.pdf", StartLine: 3, EndLine: 9}}
	updated, ok, err := service.UpdateQueuedMessage(sid, "q-1", "look at these", next)
	require.NoError(t, err)
	require.True(t, ok)

	require.Len(t, updated.Files, 2, "the replaced list is exactly what the client sent")
	assert.Equal(t, "/project/keep.md", updated.Files[0].Path)
	assert.Equal(t, "/project/new.pdf", updated.Files[1].Path)
	assert.Equal(t, 3, updated.Files[1].StartLine, "line ranges are part of an attachment's identity and must survive")

	// Persisted, not just echoed back: the drain loop re-reads this row.
	stored := queueEntryByID(t, sid, "q-1")
	require.NotNil(t, stored)
	require.Len(t, stored.Files, 2)
	assert.Equal(t, "/project/new.pdf", stored.Files[1].Path)

	// Queue position is untouched by an attachment edit.
	assert.Equal(t, []string{"look at these"}, queuedTexts(t, sid))
}

// TestUpdateQueuedMessage_EmptyListClearsAttachments: an EMPTY (but non-nil)
// list means "remove every attachment", which is a different intent from nil
// ("leave them alone"). Collapsing the two would either make removal impossible
// or silently wipe attachments on every text edit.
func TestUpdateQueuedMessage_EmptyListClearsAttachments(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Edit Clear Files")

	files := []model.FileEntry{{Path: "/project/shot.png"}}
	_, err := service.AddQueuedMessage("/project", "claude", sid, "with a file", files, "q-1", "")
	require.NoError(t, err)

	updated, ok, err := service.UpdateQueuedMessage(sid, "q-1", "text only now", []model.FileEntry{})
	require.NoError(t, err)
	require.True(t, ok)
	assert.Empty(t, updated.Files)

	stored := queueEntryByID(t, sid, "q-1")
	require.NotNil(t, stored)
	assert.Empty(t, stored.Files, "the cleared list must be persisted, not just returned")
	assert.Equal(t, "text only now", stored.Text)
}

// TestUpdateQueuedMessage_DedupesReplacedAttachments: the replace path applies
// the same identity dedupe as enqueue. A client that sends the same attachment
// twice (e.g. a file re-picked from "recent uploads") must not persist it twice
// — the user bubble would render the same image two times.
func TestUpdateQueuedMessage_DedupesReplacedAttachments(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Edit Dedupe")

	_, err := service.AddQueuedMessage("/project", "claude", sid, "msg", nil, "q-1", "")
	require.NoError(t, err)

	dup := []model.FileEntry{{Path: "/project/a.png"}, {Path: "/project/a.png"}}
	updated, ok, err := service.UpdateQueuedMessage(sid, "q-1", "msg", dup)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, updated.Files, 1, "identical attachments collapse to one entry")
	assert.Equal(t, "/project/a.png", updated.Files[0].Path)
}

// TestUpdateQueuedMessage_KeepsQuoteAttachmentsWhenReplacing: quote entries are
// attachments too. A replacement list that still contains them must persist
// their payload (text + note), since a quote's value is that payload and not its
// label path.
func TestUpdateQueuedMessage_KeepsQuoteAttachmentsWhenReplacing(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Edit Quote")

	quote := model.FileEntry{Kind: "quote", ID: "q-quote-1", Path: "main.go:12", Text: "func main() {}", Note: "why"}
	_, err := service.AddQueuedMessage("/project", "claude", sid, "about this", []model.FileEntry{quote}, "q-1", "")
	require.NoError(t, err)

	updated, ok, err := service.UpdateQueuedMessage(sid, "q-1", "about this", []model.FileEntry{quote, {Path: "/project/b.png"}})
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, updated.Files, 2)
	assert.Equal(t, "quote", updated.Files[0].Kind)
	assert.Equal(t, "func main() {}", updated.Files[0].Text, "the quoted text is the payload and must survive")
	assert.Equal(t, "why", updated.Files[0].Note)
}

// TestUpdateQueuedMessage_WriteFailureIsError: the row is read and written in
// one transaction, and a failing write must surface as an error with nothing
// changed — both for a text-only edit and for an attachment replacement (they
// use different statements).
//
// A trigger supplies the failure: SQLite has no way to make one UPDATE fail
// from the outside, and the branch is otherwise unreachable in a unit test.
func TestUpdateQueuedMessage_WriteFailureIsError(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Update Broken Write")

	_, err := service.AddQueuedMessage("/project", "claude", sid, "original", nil, "q-1", "")
	require.NoError(t, err)

	_, err = store.UnsafeDBForTest().Exec(`
		CREATE TRIGGER block_queue_update BEFORE UPDATE ON queued_messages
		BEGIN SELECT RAISE(ABORT, 'queue update blocked'); END`)
	require.NoError(t, err)

	// Text-only edit: the `files` column is left alone.
	_, ok, err := service.UpdateQueuedMessage(sid, "q-1", "edited", nil)
	require.Error(t, err, "a failing UPDATE must not be reported as a decline")
	assert.False(t, ok)
	assert.Contains(t, err.Error(), "queue update blocked")

	// Attachment replacement: a different UPDATE statement, same contract.
	_, ok, err = service.UpdateQueuedMessage(sid, "q-1", "edited", []model.FileEntry{{Path: "/project/a.png"}})
	require.Error(t, err)
	assert.False(t, ok)

	// Nothing was written.
	entry := queueEntryByID(t, sid, "q-1")
	require.NotNil(t, entry)
	assert.Equal(t, "original", entry.Text)
	assert.Empty(t, entry.Files)
}
