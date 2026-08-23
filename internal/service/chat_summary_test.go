package service

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestDBForChatSummary creates an in-memory DB with chat_history and summaries tables.
func setupTestDBForChatSummary(t *testing.T) (*sql.DB, func()) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	db.SetMaxOpenConns(1)
	_, _ = db.Exec("PRAGMA journal_mode=WAL")
	_, _ = db.Exec("PRAGMA busy_timeout=5000")

	// Create minimal tables needed for enrichMessagesWithSummaries
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			project_path TEXT NOT NULL,
			backend TEXT NOT NULL,
			title TEXT NOT NULL,
			archived INTEGER NOT NULL DEFAULT 0
		);
	`)
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_path TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			files TEXT,
			session_id TEXT,
			backend TEXT NOT NULL DEFAULT 'claude',
			streaming INTEGER NOT NULL DEFAULT 0,
			indexed INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS summaries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			target_type TEXT NOT NULL,
			target_id   INTEGER NOT NULL,
			summary     TEXT NOT NULL,
			summary_cards TEXT NOT NULL DEFAULT '',
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(target_type, target_id)
		);
	`)
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS chat_recommendations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			project_path TEXT NOT NULL DEFAULT '',
			message_id INTEGER NOT NULL DEFAULT 0,
			recommendation TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)

	cleanup := SetDBForTest(db, db)
	teardown := func() {
		cleanup()
		db.Close()
	}
	return db, teardown
}

func TestEnrichMessagesWithSummaries_NoAssistantMessages(t *testing.T) {
	_, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	// Only user messages — no enrichment needed
	messages := []model.ChatMessage{
		{ID: 1, Role: "user", Content: "hello"},
		{ID: 2, Role: "user", Content: "world"},
	}
	enrichMessagesWithSummaries(messages)
	assert.Nil(t, messages[0].Summary)
	assert.Nil(t, messages[1].Summary)
}

func TestEnrichMessagesWithSummaries_WithSummary(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	// Save a summary for assistant message ID 10
	_, err := db.Exec("INSERT INTO summaries (target_type, target_id, summary) VALUES ('chat_message', 10, '这是摘要')")
	assert.NoError(t, err)

	messages := []model.ChatMessage{
		{ID: 5, Role: "user", Content: "question"},
		{ID: 10, Role: "assistant", Content: "long answer"},
	}
	enrichMessagesWithSummaries(messages)
	assert.Nil(t, messages[0].Summary)
	assert.NotNil(t, messages[1].Summary)
	assert.Equal(t, "这是摘要", *messages[1].Summary)
}

func TestEnrichMessagesWithSummaries_NoSummarySaved(t *testing.T) {
	_, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	// No summary in DB for message ID 20
	messages := []model.ChatMessage{
		{ID: 20, Role: "assistant", Content: "answer without summary"},
	}
	enrichMessagesWithSummaries(messages)
	assert.Nil(t, messages[0].Summary)
}

func TestEnrichMessagesWithSummaries_EmptySummary(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	// Empty summary means text was too short
	_, err := db.Exec("INSERT INTO summaries (target_type, target_id, summary) VALUES ('chat_message', 30, '')")
	assert.NoError(t, err)

	messages := []model.ChatMessage{
		{ID: 30, Role: "assistant", Content: "short"},
	}
	enrichMessagesWithSummaries(messages)
	assert.NotNil(t, messages[0].Summary)
	assert.Equal(t, "", *messages[0].Summary)
}

func TestEnrichMessagesWithSummaries_MultipleAssistantMessages(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	// Save summaries for two assistant messages
	_, err := db.Exec("INSERT INTO summaries (target_type, target_id, summary) VALUES ('chat_message', 40, '摘要一')")
	assert.NoError(t, err)
	_, err = db.Exec("INSERT INTO summaries (target_type, target_id, summary) VALUES ('chat_message', 42, '摘要二')")
	assert.NoError(t, err)

	messages := []model.ChatMessage{
		{ID: 39, Role: "user", Content: "q1"},
		{ID: 40, Role: "assistant", Content: "a1"},
		{ID: 41, Role: "user", Content: "q2"},
		{ID: 42, Role: "assistant", Content: "a2"},
	}
	enrichMessagesWithSummaries(messages)
	assert.Nil(t, messages[0].Summary)
	assert.NotNil(t, messages[1].Summary)
	assert.Equal(t, "摘要一", *messages[1].Summary)
	assert.Nil(t, messages[2].Summary)
	assert.NotNil(t, messages[3].Summary)
	assert.Equal(t, "摘要二", *messages[3].Summary)
}

func TestEnrichMessagesWithSummaries_DifferentTargetType(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	// Save summary with different target type — should NOT match
	_, err := db.Exec("INSERT INTO summaries (target_type, target_id, summary) VALUES ('task_execution', 50, 'task summary')")
	assert.NoError(t, err)

	messages := []model.ChatMessage{
		{ID: 50, Role: "assistant", Content: "answer"},
	}
	enrichMessagesWithSummaries(messages)
	assert.Nil(t, messages[0].Summary) // Different target_type, should not match
}

// --- triggerChatSummarization ---

func TestTriggerChatSummarization_ExtractsLastAnswer(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	// Insert session + messages
	sessionID := "test-simple-session"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, '/test', 'claude', 'test')", sessionID)
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (100, '/test', 'user', 'hello', ?, 0)", sessionID)
	assistantContent := `{"blocks":[{"type":"text","text":"Let me check."},{"type":"tool_use","name":"Bash","id":"t1"},{"type":"text","text":"The answer is 42."}]}`
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (101, '/test', 'assistant', ?, ?, 0)", assistantContent, sessionID)

	triggerChatSummarization(context.Background(), sessionID)

	// Always-extract: should have saved the last text block (conclusion) as summary
	summary, found := GetSummary("chat_message", 101)
	assert.True(t, found)
	assert.Equal(t, "The answer is 42.", summary)
}

func TestTriggerChatSummarization_NoTextAfterToolUse(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	sessionID := "test-simple-notext"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, '/test', 'claude', 'test')", sessionID)
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (110, '/test', 'user', 'hello', ?, 0)", sessionID)
	assistantContent := `{"blocks":[{"type":"tool_use","name":"Bash","id":"t1"}]}`
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (111, '/test', 'assistant', ?, ?, 0)", assistantContent, sessionID)

	triggerChatSummarization(context.Background(), sessionID)

	// No text block at all → no summary saved
	_, found := GetSummary("chat_message", 111)
	assert.False(t, found)
}

func TestBackfillMissingSummaries_GeneratesMissingSummaries(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	sessionID := "test-backfill"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, '/test', 'claude', 'test')", sessionID)

	// Message with existing summary — should be skipped
	contentWithSummary := `{"blocks":[{"type":"text","text":"already summarized"}]}`
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (200, '/test', 'assistant', ?, ?, 0)", contentWithSummary, sessionID)
	_ = SaveSummary("chat_message", 200, "existing summary")

	// Message without summary — should be backfilled
	contentNoSummary := `{"blocks":[{"type":"text","text":"needs summary"}]}`
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (201, '/test', 'assistant', ?, ?, 0)", contentNoSummary, sessionID)

	// Streaming message — should be skipped
	contentStreaming := `{"blocks":[{"type":"text","text":"still running"}]}`
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (202, '/test', 'assistant', ?, ?, 1)", contentStreaming, sessionID)

	// Simulate what enrichMessagesWithSummaries does: build messages, query summaries, call backfill
	messages, _ := GetMessagesBySessionID(sessionID)
	assistantIDs := []int64{}
	summaryMap := map[int64]string{}
	for _, m := range messages {
		if m.Role == "assistant" {
			assistantIDs = append(assistantIDs, m.ID)
		}
	}
	// Mark 200 as already having a summary
	summaryMap[200] = "existing summary"

	// Call backfill synchronously (not via goroutine) for test determinism
	backfillMissingSummaries(assistantIDs, summaryMap)

	// Message 200: already had a summary — unchanged
	s200, found200 := GetSummary("chat_message", 200)
	assert.True(t, found200)
	assert.Equal(t, "existing summary", s200)

	// Message 201: was missing — now has a summary
	s201, found201 := GetSummary("chat_message", 201)
	assert.True(t, found201)
	assert.Equal(t, "needs summary", s201)

	// Message 202: streaming — should NOT be summarized
	_, found202 := GetSummary("chat_message", 202)
	assert.False(t, found202)
}

// TestSummarizeMessageOnce_Dedup verifies the M3 fix: while a summary for a
// message is already in flight, a concurrent call for the same message is
// skipped (returns false) instead of generating a duplicate summary + broadcast.
func TestSummarizeMessageOnce_Dedup(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	sessionID := "test-summary-dedup"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, '/test', 'claude', 'test')", sessionID)
	content := `{"blocks":[{"type":"text","text":"dedup me"}]}`
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (300, '/test', 'assistant', ?, ?, 0)", content, sessionID)

	blocks, err := parseMessageBlocks(content)
	require.NoError(t, err)

	// Simulate another goroutine already summarizing message 300.
	summaryInFlight.Store(int64(300), struct{}{})
	t.Cleanup(func() { summaryInFlight.Delete(int64(300)) })

	// Concurrent call must be skipped and must NOT persist a summary.
	ran := summarizeMessageOnce(300, blocks, "/test", sessionID)
	assert.False(t, ran, "in-flight message must not be summarized twice")
	_, found := GetSummary("chat_message", 300)
	assert.False(t, found, "skipped call must not persist a summary")

	// Once the in-flight claim is released, a later call proceeds normally.
	summaryInFlight.Delete(int64(300))
	ran = summarizeMessageOnce(300, blocks, "/test", sessionID)
	assert.True(t, ran, "call after in-flight release must run")
	s, found := GetSummary("chat_message", 300)
	assert.True(t, found)
	assert.Equal(t, "dedup me", s)
}

// The chat recommendation's blocking LLM call must NOT delay stream finalization.
// Previously triggerChatSummarization ran it inline, stalling Finalize() and the
// terminal 'done' WS event for seconds (post-reply meta bar / completion sound lag,
// plus a phantom "loading" message when switching to the session meanwhile).
func TestTriggerChatSummarization_DoesNotBlockOnRecommendation(t *testing.T) {
	// AISummary server that sleeps 800ms before responding — any inline call would
	// block triggerChatSummarization (and thus the 'done' event) by that much.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(800 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Continue the work."}}]}`))
	}))
	defer srv.Close()

	sub, cleanup := setupRecommendTest(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}
	model.ConfigInstance.Chat.RecommendEnabled = true
	model.ConfigInstance.AISummary.API.BaseURL = srv.URL
	model.ConfigInstance.AISummary.Format = "openai"

	sessionID := "sess-summary-async-rec"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, '/test', 'claude', 't')", sessionID)
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (500, '/test', 'user', 'hello', ?, 0)", sessionID)
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (501, '/test', 'assistant', '{\"blocks\":[{\"type\":\"text\",\"text\":\"The answer is 42.\"}]}', ?, 0)", sessionID)

	start := time.Now()
	triggerChatSummarization(context.Background(), sessionID)
	elapsed := time.Since(start)

	// Must return well before the 800ms server sleep — the recommendation runs async.
	assert.Less(t, elapsed, 300*time.Millisecond,
		"triggerChatSummarization must not block on the recommendation LLM call")

	// The async recommendation goroutine must still complete. Wait until its final
	// DB write (SaveChatRecommendation) is visible so we never tear down the DB
	// while the goroutine is still in flight.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		if rec := LatestChatRecommendation(ctx, sessionID, 501); rec != "" {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("expected the async recommendation goroutine to persist its result")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// And it must have broadcast its WS event too.
	var eventSeen bool
	for _, evt := range sub.GetBufferedEvents() {
		if evt.Event == "chat_recommendation" {
			eventSeen = true
			data, ok := evt.Data.(ws.ChatRecommendationData)
			if !ok {
				t.Fatalf("unexpected data type: %T", evt.Data)
			}
			assert.Equal(t, int64(501), data.MessageID)
			assert.Equal(t, "Continue the work.", data.Recommendation)
		}
	}
	assert.True(t, eventSeen, "expected a chat_recommendation WS event from the async goroutine")
}
