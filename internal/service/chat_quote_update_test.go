package service_test

import (
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertUserMessageWithFiles persists a user message carrying the given file
// entries and returns its id.
func insertUserMessageWithFiles(t *testing.T, projectPath, sessionID string, files []model.FileEntry) int64 {
	t.Helper()
	id, err := service.AddChatMessage(projectPath, "claude", sessionID, "user", "解释一下", files, false, "fallback")
	require.NoError(t, err)
	require.NotZero(t, id)
	return id
}

// TestUpdateChatQuoteNote_UpdatesOnlyTheAddressedEntry verifies the note is
// written to the entry identified by quoteId, leaving the others untouched.
func TestUpdateChatQuoteNote_UpdatesOnlyTheAddressedEntry(t *testing.T) {
	setupDB(t)
	const project = "/proj"
	sid := helperCreateSession(t, project, "claude", "quote-edit")

	msgID := insertUserMessageWithFiles(t, project, sid, []model.FileEntry{
		{Path: "src/a.go", Kind: "quote", ID: "q1", Text: "first", Note: "old note"},
		{Path: "src/b.go", Kind: "quote", ID: "q2", Text: "second", Note: "untouched"},
		{Path: "src/c.go"}, // a plain file must not be disturbed either
	})

	entries, err := service.UpdateChatQuoteNote(sid, msgID, "q1", "new note")
	require.NoError(t, err)
	require.Len(t, entries, 3)
	assert.Equal(t, "new note", entries[0].Note)
	assert.Equal(t, "untouched", entries[1].Note, "a different quote must not be rewritten")
	assert.Equal(t, "src/c.go", entries[2].Path)

	// Persisted, not just returned.
	msgs, err := service.GetChatHistory(project, "claude", sid)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "new note", msgs[0].Files[0].Note)
	assert.Equal(t, "untouched", msgs[0].Files[1].Note)
}

// TestUpdateChatQuoteNote_UnknownQuoteID verifies a bad id is a not-found
// rather than a silent no-op or a wrong-entry write.
func TestUpdateChatQuoteNote_UnknownQuoteID(t *testing.T) {
	setupDB(t)
	const project = "/proj"
	sid := helperCreateSession(t, project, "claude", "quote-edit")

	msgID := insertUserMessageWithFiles(t, project, sid, []model.FileEntry{
		{Path: "src/a.go", Kind: "quote", ID: "q1", Text: "first"},
	})

	_, err := service.UpdateChatQuoteNote(sid, msgID, "does-not-exist", "x")
	assert.ErrorIs(t, err, service.ErrChatQuoteNotFound)

	// The original note is untouched.
	msgs, err := service.GetChatHistory(project, "claude", sid)
	require.NoError(t, err)
	assert.Equal(t, "", msgs[0].Files[0].Note)
}

// TestUpdateChatQuoteNote_RejectsCrossSession verifies the session scoping: a
// valid message id from another session must not be editable.
func TestUpdateChatQuoteNote_RejectsCrossSession(t *testing.T) {
	setupDB(t)
	const project = "/proj"
	sidA := helperCreateSession(t, project, "claude", "session-a")
	sidB := helperCreateSession(t, project, "claude", "session-b")

	msgID := insertUserMessageWithFiles(t, project, sidA, []model.FileEntry{
		{Path: "src/a.go", Kind: "quote", ID: "q1", Text: "first", Note: "original"},
	})

	_, err := service.UpdateChatQuoteNote(sidB, msgID, "q1", "hijacked")
	assert.ErrorIs(t, err, service.ErrChatQuoteNotFound)

	msgs, err := service.GetChatHistory(project, "claude", sidA)
	require.NoError(t, err)
	assert.Equal(t, "original", msgs[0].Files[0].Note, "the owning session's note must be untouched")
}

// TestUpdateChatQuoteNote_RejectsAssistantMessage verifies assistant rows are
// not eligible: they never carry user-authored quotes.
func TestUpdateChatQuoteNote_RejectsAssistantMessage(t *testing.T) {
	setupDB(t)
	const project = "/proj"
	sid := helperCreateSession(t, project, "claude", "assistant-quote")

	msgID, err := service.AddChatMessage(project, "claude", sid, "assistant", "reply", []model.FileEntry{
		{Path: "src/a.go", Kind: "quote", ID: "q1", Text: "first"},
	}, false, "")
	require.NoError(t, err)

	_, err = service.UpdateChatQuoteNote(sid, msgID, "q1", "x")
	assert.ErrorIs(t, err, service.ErrChatQuoteNotFound)
}

// TestUpdateChatQuoteNote_UnknownMessage verifies a bogus message id 404s
// instead of creating a row or panicking.
func TestUpdateChatQuoteNote_UnknownMessage(t *testing.T) {
	setupDB(t)
	const project = "/proj"
	sid := helperCreateSession(t, project, "claude", "no-message")

	_, err := service.UpdateChatQuoteNote(sid, 999999, "q1", "x")
	assert.ErrorIs(t, err, service.ErrChatQuoteNotFound)
}

// TestUpdateChatQuoteNote_RoundTripsSpecialCharacters guards against the note
// being mangled by the JSON re-marshal (quotes, newlines, CJK, emoji).
func TestUpdateChatQuoteNote_RoundTripsSpecialCharacters(t *testing.T) {
	setupDB(t)
	const project = "/proj"
	sid := helperCreateSession(t, project, "claude", "special-chars")

	msgID := insertUserMessageWithFiles(t, project, sid, []model.FileEntry{
		{Path: "src/a.go", Kind: "quote", ID: "q1", Text: `he said "hi"`},
	})

	note := "多行\n批注 \"quoted\" \\ backslash 🎯"
	_, err := service.UpdateChatQuoteNote(sid, msgID, "q1", note)
	require.NoError(t, err)

	msgs, err := service.GetChatHistory(project, "claude", sid)
	require.NoError(t, err)
	assert.Equal(t, note, msgs[0].Files[0].Note)
	// The quoted text must survive the re-marshal too.
	assert.Equal(t, `he said "hi"`, msgs[0].Files[0].Text)
}

// TestUpdateChatQuoteNote_CanClearTheNote verifies an empty note is a real
// value (clearing the annotation), not a "leave unchanged" signal.
func TestUpdateChatQuoteNote_CanClearTheNote(t *testing.T) {
	setupDB(t)
	const project = "/proj"
	sid := helperCreateSession(t, project, "claude", "clear-note")

	msgID := insertUserMessageWithFiles(t, project, sid, []model.FileEntry{
		{Path: "src/a.go", Kind: "quote", ID: "q1", Text: "body", Note: "to be cleared"},
	})

	_, err := service.UpdateChatQuoteNote(sid, msgID, "q1", "")
	require.NoError(t, err)

	msgs, err := service.GetChatHistory(project, "claude", sid)
	require.NoError(t, err)
	assert.Equal(t, "", msgs[0].Files[0].Note)
}

// TestUpdateChatQuoteNote_RejectsInvalidArguments verifies the guard clauses
// rather than relying on a confusing DB error.
func TestUpdateChatQuoteNote_RejectsInvalidArguments(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/proj", "claude", "invalid-args")

	for _, tc := range []struct {
		name      string
		sessionID string
		messageID int64
		quoteID   string
	}{
		{"empty session", "", 1, "q1"},
		{"zero message id", sid, 0, "q1"},
		{"negative message id", sid, -5, "q1"},
		{"empty quote id", sid, 1, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.UpdateChatQuoteNote(tc.sessionID, tc.messageID, tc.quoteID, "x")
			assert.ErrorIs(t, err, service.ErrChatQuoteNotFound)
		})
	}
}

// TestUpdateChatQuoteNote_LeavesNonQuoteEntriesAlone ensures the edit does not
// accidentally rewrite URL attachments or plain files sharing the message.
func TestUpdateChatQuoteNote_LeavesNonQuoteEntriesAlone(t *testing.T) {
	setupDB(t)
	const project = "/proj"
	sid := helperCreateSession(t, project, "claude", "mixed-entries")

	msgID := insertUserMessageWithFiles(t, project, sid, []model.FileEntry{
		{Path: "acme/widgets#7", Kind: "url", URL: "https://github.com/acme/widgets/issues/7"},
		{Path: "src/a.go", Kind: "quote", ID: "q1", Text: "body"},
		{Path: "/src/plain.go"},
	})

	_, err := service.UpdateChatQuoteNote(sid, msgID, "q1", "note")
	require.NoError(t, err)

	msgs, err := service.GetChatHistory(project, "claude", sid)
	require.NoError(t, err)
	require.Len(t, msgs[0].Files, 3)
	assert.Equal(t, "url", msgs[0].Files[0].Kind)
	assert.Equal(t, "https://github.com/acme/widgets/issues/7", msgs[0].Files[0].URL)
	assert.Equal(t, "note", msgs[0].Files[1].Note)
	assert.Equal(t, "/src/plain.go", msgs[0].Files[2].Path)
}
