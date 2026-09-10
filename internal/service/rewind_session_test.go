package service_test

import (
	"errors"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
)

// helperCreateSession + AddChatMessage create a plain-text session; for child-row
// tests we build JSON block content with tool_use/thinking like fork_session_test does.

// ---------- TruncateSessionAfterMessage: basic truncation ----------

func TestRewindSession_TruncatesAfterAnchor(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")

	// conversation: u1 → a1 → u2 → a2 → u3 → a3
	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "Q1", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "assistant", "A1", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "user", "Q2", nil, false, "")
	assert.NoError(t, err)
	asst2ID, err := service.AddChatMessage("/project", "claude", sessID, "assistant", "A2", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "user", "Q3", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "assistant", "A3", nil, false, "")
	assert.NoError(t, err)

	// Rewind at asst2 — keep Q1/A1/Q2/A2, delete Q3/A3.
	res, err := service.TruncateSessionAfterMessage(sessID, asst2ID)
	assert.NoError(t, err)
	assert.Equal(t, int64(2), res.DeletedCount)
	assert.Equal(t, "Q3", res.RestoredText)

	msgs, err := service.GetChatHistory("/project", "claude", sessID)
	assert.NoError(t, err)
	assert.Len(t, msgs, 4)
	assert.Equal(t, "Q1", msgs[0].Content)
	assert.Equal(t, "A1", msgs[1].Content)
	assert.Equal(t, "Q2", msgs[2].Content)
	assert.Equal(t, "A2", msgs[3].Content)
}

// ---------- TruncateSessionAfterMessage: session/message not found ----------

func TestRewindSession_AnchorNotFound(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")

	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "Q1", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "assistant", "A1", nil, false, "")
	assert.NoError(t, err)

	_, err = service.TruncateSessionAfterMessage(sessID, 99999)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, service.ErrRewindAnchorNotFound), "error should wrap ErrRewindAnchorNotFound, got: %v", err)
}

// ---------- TruncateSessionAfterMessage: anchor is a user message ----------

func TestRewindSession_AnchorIsUser(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")

	user2ID, err := service.AddChatMessage("/project", "claude", sessID, "user", "Q2", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "assistant", "A2", nil, false, "")
	assert.NoError(t, err)

	_, err = service.TruncateSessionAfterMessage(sessID, user2ID)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, service.ErrRewindAnchorNotAssistant), "error should wrap ErrRewindAnchorNotAssistant, got: %v", err)
}

// ---------- TruncateSessionAfterMessage: anchor is streaming ----------

func TestRewindSession_AnchorStreaming(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")

	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "Q1", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "assistant", "Streaming...", nil, true, "")
	assert.NoError(t, err)

	var streamingID int64
	err = service.UnsafeDBForTest().QueryRow("SELECT id FROM chat_history WHERE session_id = ? AND streaming = 1", sessID).Scan(&streamingID)
	assert.NoError(t, err)

	_, err = service.TruncateSessionAfterMessage(sessID, streamingID)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, service.ErrRewindAnchorStreaming), "error should wrap ErrRewindAnchorStreaming, got: %v", err)
}

// ---------- TruncateSessionAfterMessage: nothing after the anchor ----------

func TestRewindSession_NothingAfterAnchor(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")

	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "Q1", nil, false, "")
	assert.NoError(t, err)
	asstID, err := service.AddChatMessage("/project", "claude", sessID, "assistant", "A1", nil, false, "")
	assert.NoError(t, err)

	res, err := service.TruncateSessionAfterMessage(sessID, asstID)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), res.DeletedCount)
	assert.Equal(t, "", res.RestoredText)

	// History unchanged.
	msgs, err := service.GetChatHistory("/project", "claude", sessID)
	assert.NoError(t, err)
	assert.Len(t, msgs, 2)
}

// ---------- TruncateSessionAfterMessage: restored text is plain text of removed user message ----------

func TestRewindSession_ReturnsRestoredText(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")

	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "Q1", nil, false, "")
	assert.NoError(t, err)
	asstID, err := service.AddChatMessage("/project", "claude", sessID, "assistant", "A1", nil, false, "")
	assert.NoError(t, err)
	// Deleted user message stored as JSON blocks content (real message format).
	_, err = service.AddChatMessage("/project", "claude", sessID, "user", `{"blocks":[{"type":"text","text":"editable question"}]}`, nil, false, "")
	assert.NoError(t, err)

	res, err := service.TruncateSessionAfterMessage(sessID, asstID)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), res.DeletedCount)
	assert.Equal(t, "editable question", res.RestoredText)
}

// ---------- TruncateSessionAfterMessage: deletes child rows, keeps earlier rows ----------

func TestRewindSession_DeletesChildRows(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")

	asst1ID, err := service.AddChatMessage("/project", "claude", sessID, "assistant",
		`{"blocks":[{"type":"tool_use","id":"toolu_keep","name":"Read","done":true},{"type":"thinking","think_id":"th_keep","done":true}]}`,
		nil, false, "")
	assert.NoError(t, err)
	user2ID, err := service.AddChatMessage("/project", "claude", sessID, "user", "Q2", nil, false, "")
	assert.NoError(t, err)
	asst2ID, err := service.AddChatMessage("/project", "claude", sessID, "assistant",
		`{"blocks":[{"type":"tool_use","id":"toolu_cut","name":"Bash","done":true},{"type":"thinking","think_id":"th_cut","done":true}]}`,
		nil, false, "")
	assert.NoError(t, err)

	assert.NoError(t, service.UpsertToolCall(asst1ID, sessID, "toolu_keep", "Read", []byte(`{"file_path":"/a.go"}`), "keep-out", "success", "a.go", true, 0))
	assert.NoError(t, service.UpsertThinking(asst1ID, sessID, "th_keep", "keep text"))
	assert.NoError(t, service.SaveSummary("chat_message", asst1ID, "keep summary"))
	assert.NoError(t, service.SaveRawResponse(sessID, "claude", asst1ID, "keep raw"))
	meta := minimalMetadata()
	assert.NoError(t, service.SaveMetadata(asst1ID, meta))

	assert.NoError(t, service.UpsertToolCall(asst2ID, sessID, "toolu_cut", "Bash", []byte(`{"command":"ls"}`), "cut-out", "success", "ls", true, 0))
	assert.NoError(t, service.UpsertThinking(asst2ID, sessID, "th_cut", "cut text"))
	assert.NoError(t, service.SaveSummary("chat_message", asst2ID, "cut summary"))
	assert.NoError(t, service.SaveRawResponse(sessID, "claude", asst2ID, "cut raw"))
	assert.NoError(t, service.SaveMetadata(asst2ID, minimalMetadata()))

	// A metadata row with a dead/missing message_id and one for user2 metadata.
	assert.NoError(t, service.SaveMetadata(user2ID, minimalMetadata()))

	// chat_recommendations is not part of the shared test schema — create it and
	// seed one row pointing at the to-be-deleted asst2 message plus one pointing
	// at the preserved asst1 message.
	db0 := service.UnsafeDBForTest()
	_, err = db0.Exec(`CREATE TABLE IF NOT EXISTS chat_recommendations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		project_path TEXT NOT NULL DEFAULT '',
		message_id INTEGER NOT NULL DEFAULT 0,
		recommendation TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	assert.NoError(t, err)
	_, err = db0.Exec("INSERT INTO chat_recommendations (session_id, project_path, message_id, recommendation) VALUES (?, '/project', ?, 'follow up on cut')", sessID, asst2ID)
	assert.NoError(t, err)
	_, err = db0.Exec("INSERT INTO chat_recommendations (session_id, project_path, message_id, recommendation) VALUES (?, '/project', ?, 'follow up on keep')", sessID, asst1ID)
	assert.NoError(t, err)

	res, err := service.TruncateSessionAfterMessage(sessID, asst1ID)
	assert.NoError(t, err)
	assert.Equal(t, int64(2), res.DeletedCount) // Q2 + asst2 removed

	// Child rows for the deleted asst2 message must be gone.
	var n int
	db := service.UnsafeDBForTest()
	err = db.QueryRow("SELECT COUNT(*) FROM chat_tool_calls WHERE message_id = ?", asst2ID).Scan(&n)
	assert.NoError(t, err)
	assert.Equal(t, 0, n)
	err = db.QueryRow("SELECT COUNT(*) FROM chat_thinking WHERE message_id = ?", asst2ID).Scan(&n)
	assert.NoError(t, err)
	assert.Equal(t, 0, n)
	err = db.QueryRow("SELECT COUNT(*) FROM summaries WHERE target_type = 'chat_message' AND target_id = ?", asst2ID).Scan(&n)
	assert.NoError(t, err)
	assert.Equal(t, 0, n)
	err = db.QueryRow("SELECT COUNT(*) FROM ai_raw_responses WHERE message_id = ?", asst2ID).Scan(&n)
	assert.NoError(t, err)
	assert.Equal(t, 0, n)
	// chat_metadata (the usage ledger) must SURVIVE a rewind: the removed turns
	// really did consume tokens/cost, and re-sending later produces fresh
	// AUTOINCREMENT message ids so the retained row never double-counts.
	err = db.QueryRow("SELECT COUNT(*) FROM chat_metadata WHERE message_id = ?", asst2ID).Scan(&n)
	assert.NoError(t, err)
	assert.Equal(t, 1, n)

	// Orphan recommendation for the deleted asst2 message is cleaned; the one for
	// the preserved asst1 message survives.
	err = db.QueryRow("SELECT COUNT(*) FROM chat_recommendations WHERE session_id = ? AND message_id = ?", sessID, asst2ID).Scan(&n)
	assert.NoError(t, err)
	assert.Equal(t, 0, n)
	err = db.QueryRow("SELECT COUNT(*) FROM chat_recommendations WHERE session_id = ? AND message_id = ?", sessID, asst1ID).Scan(&n)
	assert.NoError(t, err)
	assert.Equal(t, 1, n)

	// Kept rows must survive.
	rec, err := service.GetToolCall("toolu_keep", asst1ID)
	assert.NoError(t, err)
	assert.NotNil(t, rec)
	if rec != nil {
		assert.Equal(t, "keep-out", rec.Output)
	}
	th, err := service.GetThinking("th_keep", asst1ID)
	assert.NoError(t, err)
	assert.NotNil(t, th)
	if th != nil {
		assert.Equal(t, "keep text", th.Text)
	}
	summary, found := service.GetSummary("chat_message", asst1ID)
	assert.True(t, found)
	assert.Equal(t, "keep summary", summary)

	// Remaining chat_history rows: asst1 + user2 (user2 kept because id < anchor? No — user2 is between
	// asst1 and asst2, so it was deleted). Remaining = just the first assistant message.
	msgs, err := service.GetChatHistory("/project", "claude", sessID)
	assert.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "assistant", msgs[0].Role)
}

// minimalMetadata builds a Metadata with just required scalar fields.
func minimalMetadata() *ai.Metadata {
	return &ai.Metadata{Model: "test-model", TotalTokens: 5}
}

// ---------- TruncateSessionAfterMessage: queued rows beyond the anchor are deleted ----------

func TestRewindSession_DeletesQueuedRowsAfterAnchor(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")

	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "Q1", nil, false, "")
	assert.NoError(t, err)
	asstID, err := service.AddChatMessage("/project", "claude", sessID, "assistant", "A1", nil, false, "")
	assert.NoError(t, err)

	// Simulate a queued user message typed after A1 (assigned an id > asstID).
	_, err = service.AddQueuedMessage("/project", "claude", sessID, "queued followup", nil, "q-1", "queued followup")
	assert.NoError(t, err)

	res, err := service.TruncateSessionAfterMessage(sessID, asstID)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), res.DeletedCount)
	assert.Equal(t, "queued followup", res.RestoredText)

	// No queued rows remain.
	var queuedCount int
	err = service.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND queued = 1", sessID).Scan(&queuedCount)
	assert.NoError(t, err)
	assert.Equal(t, 0, queuedCount)

	msgs, err := service.GetChatHistory("/project", "claude", sessID)
	assert.NoError(t, err)
	assert.Len(t, msgs, 2)
}

// ---------- TruncateSessionAfterMessage: keeps the session row & title untouched ----------

func TestRewindSession_KeepsSessionRecord(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")
	err := service.UpdateExternalSessionID(sessID, "ext-old")
	assert.NoError(t, err)

	_, err = service.AddChatMessage("/project", "claude", sessID, "user", "Q1", nil, false, "")
	assert.NoError(t, err)
	asstID, err := service.AddChatMessage("/project", "claude", sessID, "assistant", "A1", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "user", "Q2", nil, false, "")
	assert.NoError(t, err)

	// Truncation is service-level only — it must NOT clear external_session_id
	// (the handler does that). Verify the session row is otherwise untouched.
	res, err := service.TruncateSessionAfterMessage(sessID, asstID)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), res.DeletedCount)

	assert.Equal(t, "ext-old", service.GetExternalSessionID(sessID))
	// AddChatMessage auto-titles the session from the first user message (Q1),
	// and truncation must not change the stored title.
	title, err := service.GetSessionTitle(sessID)
	assert.NoError(t, err)
	assert.Equal(t, "Q1", title)

	// Session still present (not a new row).
	var archived int
	err = service.UnsafeDBForTest().QueryRow("SELECT archived FROM chat_sessions WHERE id = ?", sessID).Scan(&archived)
	assert.NoError(t, err)
	assert.Equal(t, 0, archived)
}

// ---------- Deadlock regression: RAG purge must run AFTER writeMu release ----------

// TestRewindSession_RAGPurgeDoesNotDeadlock reproduces the self-deadlock where
// TruncateSessionAfterMessage called the RAG purge callback while still holding
// the global writeMu. The real rag.Store serializes on the SAME lock via
// serviceWriteLocker (store_sqlite.go), so re-acquiring writeMu inside the
// callback deadlocks on the non-reentrant mutex (froze the whole server for 37
// minutes in production). The callback below mimics that: it re-takes the
// service writeMu exactly like rag's serviceWriteLocker.Lock does.
func TestRewindSession_RAGPurgeDoesNotDeadlock(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")
	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "Q1", nil, false, "")
	assert.NoError(t, err)
	asstID, err := service.AddChatMessage("/project", "claude", sessID, "assistant", "A1", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "user", "Q2", nil, false, "")
	assert.NoError(t, err)

	// Restore the previous callback after the test (package-global).
	service.SetPurgeRAGChunksAfterMessageFn(func(sessionID string, anchorID int64) (int64, error) {
		// Mimic rag.Store.DeleteChunksBySessionAfterMessage running under
		// serviceWriteLocker: it takes the service-global writeMu.
		service.WriteLock()
		defer service.WriteUnlock()
		return 0, nil
	})
	t.Cleanup(func() { service.SetPurgeRAGChunksAfterMessageFn(nil) })

	done := make(chan struct{})
	go func() {
		defer close(done)
		res, err := service.TruncateSessionAfterMessage(sessID, asstID)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), res.DeletedCount)
	}()

	select {
	case <-done:
		// Success: truncation finished without deadlocking.
	case <-time.After(2 * time.Second):
		t.Fatal("TruncateSessionAfterMessage deadlocked: RAG purge callback ran while writeMu was still held")
	}
}
