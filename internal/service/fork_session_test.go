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
	newSessID, err := service.ForkSession(sessID, "/project", "[Fork] Hello AI")
	assert.NoError(t, err)
	assert.NotEmpty(t, newSessID)
	assert.NotEqual(t, sessID, newSessID)

	// New session title should match the title passed from handler
	title, err := service.GetSessionTitle(newSessID)
	assert.NoError(t, err)
	assert.Equal(t, "[Fork] Hello AI", title)

	// New session should have source_session_id set
	var sourceID *string
	err = service.DB.QueryRow("SELECT source_session_id FROM chat_sessions WHERE id = ?", newSessID).Scan(&sourceID)
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

	newSessID, err := service.ForkSession(sessID, "/project", "[Fork] Original")
	assert.NoError(t, err)

	// Forked session should NOT inherit external_session_id
	extID := service.GetExternalSessionID(newSessID)
	assert.NotEqual(t, "ext-cli-session-123", extID)
}

// ---------- ForkSession: session not found ----------

func TestForkSession_SessionNotFound(t *testing.T) {
	setupDB(t)

	_, err := service.ForkSession("nonexistent-session-id", "/project", "[Fork] Session")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ---------- ForkSession: project mismatch ----------

func TestForkSession_ProjectMismatch(t *testing.T) {
	setupDB(t)

	sessID := helperCreateSession(t, "/project", "claude", "Original")

	_, err := service.ForkSession(sessID, "/other-project", "[Fork] Original")
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

	_, err := service.ForkSession(sessID, "/project", "[Fork] Original")
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

	newSessID, err := service.ForkSession(sessID, "/project", "[Fork] prompt")
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
	_, err = service.ForkSession(sessID, "/project", "[Fork] Original")
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

	newSessID, err := service.ForkSession(sessID, "/project", "[Fork] prompt")
	assert.NoError(t, err)

	info, err := service.GetSessionInfo(newSessID)
	assert.NoError(t, err)
	assert.Equal(t, "claude", info.Backend)
	assert.Equal(t, "claude-agent", info.AgentID)
	assert.Equal(t, "claude-sonnet-4-6", info.Model)
}
