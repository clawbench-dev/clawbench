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

// TestUpdateQueuedMessageContent_RewritesTextOnly pins the core contract: the
// text changes, everything that identifies the entry (queue id, position,
// attachments, creation stamp) survives untouched.
//
// Position is the load-bearing part. A queued message can wait many minutes, so
// an edit that re-queued it would cost the user the wait — exactly what the
// recall-and-resend design could not avoid.
func TestUpdateQueuedMessageContent_RewritesTextOnly(t *testing.T) {
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

	updated, ok, err := service.UpdateQueuedMessageContent(sid, "q-2", "second, revised")
	require.NoError(t, err)
	require.True(t, ok, "an existing queue row must report a real update")
	assert.Equal(t, "second, revised", updated.Text)
	assert.Equal(t, "q-2", updated.QueueID, "the queue id is the client's identity — it must not change")
	require.Len(t, updated.Files, 1, "attachments are not editable here and must survive verbatim")
	assert.Equal(t, "/project/shot.png", updated.Files[0].Path)
	assert.Equal(t, before.CreatedAt, updated.CreatedAt)
	assert.Equal(t, before.ID, updated.ID)

	// Queue order is untouched: the edited entry is still second.
	assert.Equal(t, []string{"first", "second, revised", "third"}, queuedTexts(t, sid))
}

// TestUpdateQueuedMessageContent_MissingRowReportsFalse: the drain loop may claim
// the row between the client rendering the queue and the user saving. A no-op
// UPDATE must not be reported as success — the message already left with the OLD
// text, so "saved" would be a lie the user acts on.
func TestUpdateQueuedMessageContent_MissingRowReportsFalse(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Edit Missing")

	_, err := service.AddQueuedMessage("/project", "claude", sid, "still here", nil, "q-1", "")
	require.NoError(t, err)

	_, ok, err := service.UpdateQueuedMessageContent(sid, "q-never-existed", "ghost edit")
	require.NoError(t, err, "an absent row is not an error — it is a decline")
	assert.False(t, ok)

	// The surviving row is untouched.
	assert.Equal(t, []string{"still here"}, queuedTexts(t, sid))
}

// TestUpdateQueuedMessageContent_WrongSessionDoesNotLeak: queue ids are only
// unique per session. Addressing another session's id must decline AND leave
// that session's row alone.
func TestUpdateQueuedMessageContent_WrongSessionDoesNotLeak(t *testing.T) {
	setupDB(t)
	mine := helperCreateSession(t, "/project", "claude", "Mine")
	theirs := helperCreateSession(t, "/project", "claude", "Theirs")

	_, err := service.AddQueuedMessage("/project", "claude", theirs, "their message", nil, "q-shared", "")
	require.NoError(t, err)

	_, ok, err := service.UpdateQueuedMessageContent(mine, "q-shared", "hijacked")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, []string{"their message"}, queuedTexts(t, theirs),
		"a queue id from another session must never be rewritten")
}

// TestUpdateQueuedMessageContent_AllowsEmptyTextForAttachmentOnly: clearing the
// text of an attachment-only message is legitimate (the user keeps the file and
// drops the words). The "nothing at all" case is rejected at the handler, which
// can see the request body; the service only enforces row existence.
func TestUpdateQueuedMessageContent_AllowsEmptyTextForAttachmentOnly(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Edit Empty")

	files := []model.FileEntry{{Path: "/project/shot.png"}}
	_, err := service.AddQueuedMessage("/project", "claude", sid, "describe this", files, "q-1", "")
	require.NoError(t, err)

	updated, ok, err := service.UpdateQueuedMessageContent(sid, "q-1", "")
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

// TestUpdateQueuedMessageContent_UnaddressableIsDeclined: an empty session or
// queue id must not open a transaction that would match every row.
func TestUpdateQueuedMessageContent_UnaddressableIsDeclined(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Update Unaddressable")

	_, err := service.AddQueuedMessage("/project", "claude", sid, "keep me", nil, "q-1", "")
	require.NoError(t, err)

	_, ok, err := service.UpdateQueuedMessageContent(sid, "", "ignored")
	require.NoError(t, err)
	assert.False(t, ok)

	_, ok, err = service.UpdateQueuedMessageContent("", "q-1", "ignored")
	require.NoError(t, err)
	assert.False(t, ok)

	assert.Equal(t, []string{"keep me"}, queuedTexts(t, sid))
}

// TestUpdateQueuedMessageContent_ReadFailureIsError: a broken read is a real
// error, not a decline. The two must stay distinguishable — a decline means the
// message is gone (the caller tells the user "too late"), an error means the
// server is broken (the caller retries or reports a failure).
func TestUpdateQueuedMessageContent_ReadFailureIsError(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "Queue Update Broken Read")

	// Drop the table so the SELECT fails with a real error (not ErrNoRows).
	_, err := store.UnsafeDBForTest().Exec("DROP TABLE queued_messages")
	require.NoError(t, err)

	_, ok, err := service.UpdateQueuedMessageContent(sid, "q-1", "text")
	require.Error(t, err, "a missing table must surface as an error")
	assert.False(t, ok)
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
