package service_test

import (
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
)

// ---------- ForkSession: normal flow ----------

func TestForkSession_NormalFlow(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original Session")

	// Add messages to the source session (AddChatMessage auto-updates title to first user message)
	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "Hello AI", nil, false, "")
	assert.NoError(t, err)
	asstID, err := service.AddChatMessage("/project", "claude", sessID, "assistant", "Hi there!", nil, false, "")
	assert.NoError(t, err)

	// Add a summary to the assistant message
	err = service.SaveSummary("chat_message", asstID, "Greeting exchange")
	assert.NoError(t, err)

	// Fork with title prefix from handler (i18n would be "[Fork] " in English)
	newSessID, err := service.ForkSession(sessID, "/project", "[Fork] Hello AI", 0)
	assert.NoError(t, err)
	assert.NotEmpty(t, newSessID)
	assert.NotEqual(t, sessID, newSessID)

	// New session title should match the title passed from handler
	title, err := service.GetSessionTitle(newSessID)
	assert.NoError(t, err)
	assert.Equal(t, "[Fork] Hello AI", title)

	// New session should have source_session_id set
	var sourceID *string
	err = service.UnsafeDBForTest().QueryRow("SELECT source_session_id FROM chat_sessions WHERE id = ?", newSessID).Scan(&sourceID)
	assert.NoError(t, err)
	assert.NotNil(t, sourceID)
	assert.Equal(t, sessID, *sourceID)

	// Messages should be copied
	msgs, err := service.GetChatHistory("/project", "claude", newSessID)
	assert.NoError(t, err)
	assert.Len(t, msgs, 2)
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "Hello AI", msgs[0].Content)
	assert.Equal(t, "assistant", msgs[1].Role)
	assert.Equal(t, "Hi there!", msgs[1].Content)

	// Summary should be copied
	newAsstID := msgs[1].ID
	summary, found := service.GetSummary("chat_message", newAsstID)
	assert.True(t, found)
	assert.Equal(t, "Greeting exchange", summary)
}

// ---------- ForkSession: does NOT copy external_session_id ----------

func TestForkSession_NoExternalSessionID(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")
	err := service.UpdateExternalSessionID(sessID, "ext-cli-session-123")
	assert.NoError(t, err)

	newSessID, err := service.ForkSession(sessID, "/project", "[Fork] Original", 0)
	assert.NoError(t, err)

	// Forked session should NOT inherit external_session_id
	extID := service.GetExternalSessionID(newSessID)
	assert.NotEqual(t, "ext-cli-session-123", extID)
}

// ---------- ForkSession: session not found ----------

func TestForkSession_SessionNotFound(t *testing.T) {
	setupDB(t)

	_, err := service.ForkSession("nonexistent-session-id", "/project", "[Fork] Session", 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ---------- ForkSession: project mismatch ----------

func TestForkSession_ProjectMismatch(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")

	_, err := service.ForkSession(sessID, "/other-project", "[Fork] Original", 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong")
}

// ---------- ForkSession: session count limit ----------

func TestForkSession_SessionCountLimit(t *testing.T) {
	setupDB(t)

	origMax := model.SessionMaxCount
	model.SessionMaxCount = 1
	t.Cleanup(func() { model.SessionMaxCount = origMax })

	sessID := helperCreateSession(t, "/project", "claude", "Original")

	_, err := service.ForkSession(sessID, "/project", "[Fork] Original", 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "session limit")
}

// ---------- ForkSession: skips streaming messages ----------

func TestForkSession_SkipsStreamingMessages(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")

	// Add finalized + streaming messages
	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "prompt", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "assistant", "final", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "assistant", "streaming...", nil, true, "")
	assert.NoError(t, err)

	newSessID, err := service.ForkSession(sessID, "/project", "[Fork] prompt", 0)
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", newSessID)
	assert.NoError(t, err)
	assert.Len(t, msgs, 2) // user + finalized assistant only
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "assistant", msgs[1].Role)
	assert.Equal(t, "final", msgs[1].Content)
}

// ---------- ForkSession: soft-deleted source ----------

func TestForkSession_SoftDeletedSource(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")

	// Soft-delete the source session
	err := service.DeleteSession("/project", "claude", sessID)
	assert.NoError(t, err)

	// Should fail because deleted=0 filter
	_, err = service.ForkSession(sessID, "/project", "[Fork] Original", 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ---------- ForkSession: inherits agent/model from source ----------

func TestForkSession_InheritsAgentAndModel(t *testing.T) {
	setupDB(t)

	// Create session with specific agent and model
	sessID, err := service.CreateSession("/project", "claude", "Original", "claude-agent", "claude-sonnet-4-6", "user", "chat")
	assert.NoError(t, err)

	_, err = service.AddChatMessage("/project", "claude", sessID, "user", "prompt", nil, false, "")
	assert.NoError(t, err)

	newSessID, err := service.ForkSession(sessID, "/project", "[Fork] prompt", 0)
	assert.NoError(t, err)

	info, err := service.GetSessionInfo(newSessID)
	assert.NoError(t, err)
	assert.Equal(t, "claude", info.Backend)
	assert.Equal(t, "claude-agent", info.AgentID)
	assert.Equal(t, "claude-sonnet-4-6", info.Model)
}

// ---------- ForkSession: empty session (no messages) ----------

func TestForkSession_EmptySession(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Empty Session")

	// Fork without any messages
	newSessID, err := service.ForkSession(sessID, "/project", "[Fork] Empty Session", 0)
	assert.NoError(t, err)
	assert.NotEmpty(t, newSessID)

	// Forked session should have no messages
	msgs, err := service.GetChatHistory("/project", "claude", newSessID)
	assert.NoError(t, err)
	assert.Empty(t, msgs)
}

// ---------- ForkSession: messages with files ----------

func TestForkSession_MessagesWithFiles(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "With Files")

	// Add a message with files
	files := []string{"/src/main.go", "/src/util.ts"}
	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "Review these files", files, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "assistant", "Looks good", nil, false, "")
	assert.NoError(t, err)

	newSessID, err := service.ForkSession(sessID, "/project", "[Fork] Review these files", 0)
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", newSessID)
	assert.NoError(t, err)
	assert.Len(t, msgs, 2)
	assert.Equal(t, []string{"/src/main.go", "/src/util.ts"}, msgs[0].Files)
}

// ---------- ForkSession: fork of a forked session ----------

func TestForkSession_ForkOfFork(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")
	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "Hello", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "assistant", "World", nil, false, "")
	assert.NoError(t, err)

	// First fork
	fork1ID, err := service.ForkSession(sessID, "/project", "[Fork] Hello", 0)
	assert.NoError(t, err)

	// Fork the forked session
	fork2ID, err := service.ForkSession(fork1ID, "/project", "[Fork] [Fork] Hello", 0)
	assert.NoError(t, err)

	// Both forks should be independent
	assert.NotEqual(t, sessID, fork1ID)
	assert.NotEqual(t, fork1ID, fork2ID)
	assert.NotEqual(t, sessID, fork2ID)

	// fork2's source_session_id should point to fork1, not original
	var sourceID *string
	err = service.UnsafeDBForTest().QueryRow("SELECT source_session_id FROM chat_sessions WHERE id = ?", fork2ID).Scan(&sourceID)
	assert.NoError(t, err)
	assert.NotNil(t, sourceID)
	assert.Equal(t, fork1ID, *sourceID)

	// All messages should be copied through (user + assistant = 2)
	msgs, err := service.GetChatHistory("/project", "claude", fork2ID)
	assert.NoError(t, err)
	assert.Len(t, msgs, 2)
}

// ---------- ForkSession: session type is chat ----------

func TestForkSession_SessionTypeIsChat(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")
	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "Hi", nil, false, "")
	assert.NoError(t, err)

	newSessID, err := service.ForkSession(sessID, "/project", "[Fork] Hi", 0)
	assert.NoError(t, err)

	var sessionType string
	err = service.UnsafeDBForTest().QueryRow("SELECT session_type FROM chat_sessions WHERE id = ?", newSessID).Scan(&sessionType)
	assert.NoError(t, err)
	assert.Equal(t, "chat", sessionType)
}

// ---------- ForkSession: copied messages get new IDs ----------

func TestForkSession_CopiedMessagesGetNewIDs(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")
	origMsgID, err := service.AddChatMessage("/project", "claude", sessID, "user", "Hello", nil, false, "")
	assert.NoError(t, err)

	newSessID, err := service.ForkSession(sessID, "/project", "[Fork] Hello", 0)
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", newSessID)
	assert.NoError(t, err)
	assert.Len(t, msgs, 1)

	// The copied message should have a different ID from the original
	assert.NotEqual(t, origMsgID, msgs[0].ID)
}

// ---------- ForkSession: does NOT copy task_execution summaries ----------

func TestForkSession_DoesNotCopyTaskExecutionSummaries(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")
	asstID, err := service.AddChatMessage("/project", "claude", sessID, "user", "prompt", nil, false, "")
	assert.NoError(t, err)

	// Add chat_message summary
	err = service.SaveSummary("chat_message", asstID, "Chat summary")
	assert.NoError(t, err)

	// Add task_execution summary (should NOT be copied since it's keyed by different target_type)
	err = service.SaveSummary("task_execution", asstID, "Task summary")
	assert.NoError(t, err)

	newSessID, err := service.ForkSession(sessID, "/project", "[Fork] prompt", 0)
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", newSessID)
	assert.NoError(t, err)
	assert.Len(t, msgs, 1)

	// chat_message summary should be copied
	newMsgID := msgs[0].ID
	summary, found := service.GetSummary("chat_message", newMsgID)
	assert.True(t, found)
	assert.Equal(t, "Chat summary", summary)
}

// ---------- ForkSession: with beforeMessageID truncation ----------

func TestForkSession_BeforeMessageID(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")

	// Create a conversation: user1 → asst1 → user2 → asst2 → user3 → asst3
	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "First question", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "assistant", "First answer", nil, false, "")
	assert.NoError(t, err)
	user2ID, err := service.AddChatMessage("/project", "claude", sessID, "user", "Second question", nil, false, "")
	assert.NoError(t, err)
	asst2ID, err := service.AddChatMessage("/project", "claude", sessID, "assistant", "Second answer", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "user", "Third question", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "assistant", "Third answer", nil, false, "")
	assert.NoError(t, err)

	// Add a summary to the second assistant message
	err = service.SaveSummary("chat_message", asst2ID, "Second answer summary")
	assert.NoError(t, err)

	// Fork from user2 (second user message) — should include user1, asst1, user2, asst2
	newSessID, err := service.ForkSession(sessID, "/project", "[Fork] Second question", user2ID)
	assert.NoError(t, err)
	assert.NotEmpty(t, newSessID)

	msgs, err := service.GetChatHistory("/project", "claude", newSessID)
	assert.NoError(t, err)
	assert.Len(t, msgs, 4) // user1 + asst1 + user2 + asst2
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "First question", msgs[0].Content)
	assert.Equal(t, "assistant", msgs[1].Role)
	assert.Equal(t, "First answer", msgs[1].Content)
	assert.Equal(t, "user", msgs[2].Role)
	assert.Equal(t, "Second question", msgs[2].Content)
	assert.Equal(t, "assistant", msgs[3].Role)
	assert.Equal(t, "Second answer", msgs[3].Content)

	// Summary of asst2 should be copied (asst2 is included in the fork)
	// asst2's summary should be copied
	asst2NewID := msgs[3].ID
	summary, found := service.GetSummary("chat_message", asst2NewID)
	assert.True(t, found)
	assert.Equal(t, "Second answer summary", summary)
}

// ---------- ForkSession: beforeMessageID = first user message ----------

func TestForkSession_BeforeMessageID_FirstMessage(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")

	user1ID, err := service.AddChatMessage("/project", "claude", sessID, "user", "Only user message", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "assistant", "Response", nil, false, "")
	assert.NoError(t, err)

	// Fork from the first (and only) user message — includes user + assistant reply
	newSessID, err := service.ForkSession(sessID, "/project", "[Fork] Only user message", user1ID)
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", newSessID)
	assert.NoError(t, err)
	assert.Len(t, msgs, 2) // user message + assistant reply
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "Only user message", msgs[0].Content)
	assert.Equal(t, "assistant", msgs[1].Role)
	assert.Equal(t, "Response", msgs[1].Content)
}

// ---------- ForkSession: beforeMessageID = last user message (equivalent to full fork) ----------

func TestForkSession_BeforeMessageID_LastMessage(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")

	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "Q1", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "assistant", "A1", nil, false, "")
	assert.NoError(t, err)
	user2ID, err := service.AddChatMessage("/project", "claude", sessID, "user", "Q2", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "assistant", "A2", nil, false, "")
	assert.NoError(t, err)

	// Fork from the last user message — includes all messages (Q1 + A1 + Q2 + A2)
	newSessID, err := service.ForkSession(sessID, "/project", "[Fork] Q2", user2ID)
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", newSessID)
	assert.NoError(t, err)
	assert.Len(t, msgs, 4) // Q1 + A1 + Q2 + A2 (all messages including the last assistant reply)
}

// ---------- ForkSession: beforeMessageID = non-existent message ----------

func TestForkSession_BeforeMessageID_NotFound(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")
	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "Hello", nil, false, "")
	assert.NoError(t, err)

	_, err = service.ForkSession(sessID, "/project", "[Fork] Hello", 99999)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found in session")
}

// ---------- ForkSession: beforeMessageID = assistant message (not user) ----------

func TestForkSession_BeforeMessageID_NotUserMessage(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")
	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "Hello", nil, false, "")
	assert.NoError(t, err)
	asstID, err := service.AddChatMessage("/project", "claude", sessID, "assistant", "World", nil, false, "")
	assert.NoError(t, err)

	// Fork from an assistant message should fail
	_, err = service.ForkSession(sessID, "/project", "[Fork] Hello", asstID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be a user message")
}

// ---------- ForkSession: beforeMessageID = 0 (backward compatible, copies all) ----------

func TestForkSession_BeforeMessageID_Zero(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")
	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "Q1", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "assistant", "A1", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "user", "Q2", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "assistant", "A2", nil, false, "")
	assert.NoError(t, err)

	// beforeMessageID=0 should copy all messages (backward compatible)
	newSessID, err := service.ForkSession(sessID, "/project", "[Fork] Q1", 0)
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", newSessID)
	assert.NoError(t, err)
	assert.Len(t, msgs, 4)
}

// ---------- ForkSession: user message with no assistant reply (last message) ----------

func TestForkSession_BeforeMessageID_NoAssistantReply(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")
	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "Q1", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "assistant", "A1", nil, false, "")
	assert.NoError(t, err)
	user2ID, err := service.AddChatMessage("/project", "claude", sessID, "user", "Q2 (no reply yet)", nil, false, "")
	assert.NoError(t, err)
	// No assistant reply after user2 — this is the last message

	// Fork from user2 — no assistant reply, so cut at user2
	newSessID, err := service.ForkSession(sessID, "/project", "[Fork] Q2", user2ID)
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", newSessID)
	assert.NoError(t, err)
	assert.Len(t, msgs, 3) // Q1 + A1 + Q2 (no assistant reply for Q2)
}
