package service_test

import (
	"database/sql"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
)

// ---------- ContinueFromExecution: dedup check ----------

func TestContinueFromExecution_CheckNotContinued(t *testing.T) {
	setupDB(t)

	taskID := helperCreateScheduledTask(t, "/project", "Daily Review", "claude")
	sessID := helperCreateScheduledSession(t, "/project", "claude", "Daily Review")
	execID := helperCreateTaskExecution(t, taskID, sessID, "completed")

	exists, sessionID, err := service.CheckContinueSession(execID)
	assert.NoError(t, err)
	assert.False(t, exists)
	assert.Empty(t, sessionID)
}

func TestContinueFromExecution_CheckAlreadyContinued(t *testing.T) {
	setupDB(t)

	taskID := helperCreateScheduledTask(t, "/project", "Daily Review", "claude")
	sessID := helperCreateScheduledSession(t, "/project", "claude", "Daily Review")
	execID := helperCreateTaskExecution(t, taskID, sessID, "completed")

	// Continue the execution
	newSessID, _, err := service.ContinueFromExecution(execID, "/project")
	assert.NoError(t, err)
	assert.NotEmpty(t, newSessID)

	// Check should now find it
	exists, foundSessID, err := service.CheckContinueSession(execID)
	assert.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, newSessID, foundSessID)
}

// ---------- ContinueFromExecution: normal flow ----------

func TestContinueFromExecution_NormalFlow(t *testing.T) {
	setupDB(t)

	taskID := helperCreateScheduledTask(t, "/project", "Daily Code Review", "claude")
	sessID := helperCreateScheduledSessionWithDetails(t, "/project", "claude", "Daily Code Review", "claude-agent", "claude-sonnet-4-6", "high")
	// Set external session ID on the source session (simulates CLI having assigned one)
	err := service.UpdateExternalSessionID(sessID, "ext-session-123")
	assert.NoError(t, err)
	execID := helperCreateTaskExecution(t, taskID, sessID, "completed")

	// Add messages to the scheduled session
	_, err = service.AddChatMessage("/project", "claude", sessID, "user", "Review the code", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "assistant", "Code looks good", nil, false, "")
	assert.NoError(t, err)

	newSessID, _, err := service.ContinueFromExecution(execID, "/project")
	assert.NoError(t, err)
	assert.NotEmpty(t, newSessID)

	// New session should be a chat session
	var sessionType string
	err = service.DB.QueryRow("SELECT session_type FROM chat_sessions WHERE id = ?", newSessID).Scan(&sessionType)
	assert.NoError(t, err)
	assert.Equal(t, "chat", sessionType)

	// New session should have the task name with [MM-DD HH:MM] prefix as title
	title, err := service.GetSessionTitle(newSessID)
	assert.NoError(t, err)
	assert.Contains(t, title, "Daily Code Review")
	assert.Regexp(t, `^\[\d{2}-\d{2} \d{2}:\d{2}\] Daily Code Review$`, title)

	// New session should inherit agent/model/thinking
	info, err := service.GetSessionInfo(newSessID)
	assert.NoError(t, err)
	assert.Equal(t, "claude", info.Backend)
	assert.Equal(t, "claude-agent", info.AgentID)
	assert.Equal(t, "claude-sonnet-4-6", info.Model)
	// ThinkingEffort is no longer persisted to DB

	// New session should have source_session_id
	var sourceSessID *string
	err = service.DB.QueryRow("SELECT source_session_id FROM chat_sessions WHERE id = ?", newSessID).Scan(&sourceSessID)
	assert.NoError(t, err)
	assert.NotNil(t, sourceSessID)
	assert.Equal(t, sessID, *sourceSessID)

	// External session ID should be inherited from source session
	assert.Equal(t, "ext-session-123", service.GetExternalSessionID(newSessID))

	// Messages should be copied
	msgs, err := service.GetChatHistory("/project", "claude", newSessID)
	assert.NoError(t, err)
	assert.Len(t, msgs, 2)
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "Review the code", msgs[0].Content)
	assert.Equal(t, "assistant", msgs[1].Role)
	assert.Equal(t, "Code looks good", msgs[1].Content)
}

// ---------- ContinueFromExecution: dedup (already continued) ----------

func TestContinueFromExecution_AlreadyContinued(t *testing.T) {
	setupDB(t)

	taskID := helperCreateScheduledTask(t, "/project", "Task", "claude")
	sessID := helperCreateScheduledSession(t, "/project", "claude", "Task")
	execID := helperCreateTaskExecution(t, taskID, sessID, "completed")

	newSessID1, alreadyExists1, err := service.ContinueFromExecution(execID, "/project")
	assert.NoError(t, err)
	assert.False(t, alreadyExists1)

	// Second call should return the same session with alreadyExists=true
	newSessID2, alreadyExists2, err := service.ContinueFromExecution(execID, "/project")
	assert.NoError(t, err)
	assert.Equal(t, newSessID1, newSessID2)
	assert.True(t, alreadyExists2)
}

// ---------- ContinueFromExecution: delete then re-continue (restores) ----------

func TestContinueFromExecution_DeletedThenRecontinue(t *testing.T) {
	setupDB(t)

	taskID := helperCreateScheduledTask(t, "/project", "Task", "claude")
	sessID := helperCreateScheduledSession(t, "/project", "claude", "Task")
	execID := helperCreateTaskExecution(t, taskID, sessID, "completed")

	newSessID1, alreadyExists1, err := service.ContinueFromExecution(execID, "/project")
	assert.NoError(t, err)
	assert.False(t, alreadyExists1)

	// Delete the continued session
	err = service.DeleteSession("/project", "claude", newSessID1)
	assert.NoError(t, err)

	// Should restore the deleted session, not create a new one
	newSessID2, alreadyExists2, err := service.ContinueFromExecution(execID, "/project")
	assert.NoError(t, err)
	assert.Equal(t, newSessID1, newSessID2) // Same session ID (restored)
	assert.True(t, alreadyExists2)

	// Session should no longer be deleted
	var deleted int
	err = service.DB.QueryRow("SELECT deleted FROM chat_sessions WHERE id = ?", newSessID2).Scan(&deleted)
	assert.NoError(t, err)
	assert.Equal(t, 0, deleted)
}

// ---------- ContinueFromExecution: session count limit ----------

func TestContinueFromExecution_SessionCountLimit(t *testing.T) {
	setupDB(t)

	// Set max session count
	origMax := model.SessionMaxCount
	model.SessionMaxCount = 1
	t.Cleanup(func() { model.SessionMaxCount = origMax })

	// Create a chat session to fill the limit
	helperCreateSession(t, "/project", "claude", "Existing")

	taskID := helperCreateScheduledTask(t, "/project", "Task", "claude")
	sessID := helperCreateScheduledSession(t, "/project", "claude", "Task")
	execID := helperCreateTaskExecution(t, taskID, sessID, "completed")

	_, _, err := service.ContinueFromExecution(execID, "/project")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "session limit")
}

// ---------- ContinueFromExecution: running status rejection ----------

func TestContinueFromExecution_RunningExecution(t *testing.T) {
	setupDB(t)

	taskID := helperCreateScheduledTask(t, "/project", "Task", "claude")
	sessID := helperCreateScheduledSession(t, "/project", "claude", "Task")
	execID := helperCreateTaskExecution(t, taskID, sessID, "running")

	_, _, err := service.ContinueFromExecution(execID, "/project")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "running")
}

// ---------- ContinueFromExecution: copy scope ----------

func TestContinueFromExecution_SkipsStreamingMessages(t *testing.T) {
	setupDB(t)

	taskID := helperCreateScheduledTask(t, "/project", "Task", "claude")
	sessID := helperCreateScheduledSession(t, "/project", "claude", "Task")
	execID := helperCreateTaskExecution(t, taskID, sessID, "completed")

	// Add finalized + streaming messages
	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "prompt", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "assistant", "final", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "assistant", "streaming...", nil, true, "")
	assert.NoError(t, err)

	newSessID, _, err := service.ContinueFromExecution(execID, "/project")
	assert.NoError(t, err)

	msgs, err := service.GetChatHistory("/project", "claude", newSessID)
	assert.NoError(t, err)
	assert.Len(t, msgs, 2) // user + finalized assistant only
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "assistant", msgs[1].Role)
	assert.Equal(t, "final", msgs[1].Content)
}

// ---------- ContinueFromExecution: summaries copy ----------

func TestContinueFromExecution_CopiesChatMessageSummaries(t *testing.T) {
	setupDB(t)

	taskID := helperCreateScheduledTask(t, "/project", "Task", "claude")
	sessID := helperCreateScheduledSession(t, "/project", "claude", "Task")
	execID := helperCreateTaskExecution(t, taskID, sessID, "completed")

	// Add messages
	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "prompt", nil, false, "")
	assert.NoError(t, err)
	asstID, err := service.AddChatMessage("/project", "claude", sessID, "assistant", "reply", nil, false, "")
	assert.NoError(t, err)

	// Add a chat_message summary
	err = service.SaveSummary("chat_message", asstID, "AI replied with details")
	assert.NoError(t, err)

	// Add a task_execution summary (should NOT be copied)
	err = service.SaveSummary("task_execution", execID, "Task completed successfully")
	assert.NoError(t, err)

	newSessID, _, err := service.ContinueFromExecution(execID, "/project")
	assert.NoError(t, err)

	// The new assistant message should have a summary
	newMsgs, err := service.GetChatHistory("/project", "claude", newSessID)
	assert.NoError(t, err)
	assert.Len(t, newMsgs, 2)

	// Look up summary for the new assistant message
	newAsstID := newMsgs[1].ID
	summary, found := service.GetSummary("chat_message", newAsstID)
	assert.True(t, found)
	assert.Equal(t, "AI replied with details", summary)
}

// ---------- ContinueFromExecution: soft-deleted source session ----------

func TestContinueFromExecution_SoftDeletedSource(t *testing.T) {
	setupDB(t)

	taskID := helperCreateScheduledTask(t, "/project", "Task", "claude")
	sessID := helperCreateScheduledSessionWithDetails(t, "/project", "claude", "Task", "agent-1", "model-1", "low")
	execID := helperCreateTaskExecution(t, taskID, sessID, "completed")

	// Add messages before deleting
	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "prompt", nil, false, "")
	assert.NoError(t, err)

	// Soft-delete the source session
	err = service.DeleteSession("/project", "claude", sessID)
	assert.NoError(t, err)

	// Should still be able to continue (source metadata still readable)
	newSessID, _, err := service.ContinueFromExecution(execID, "/project")
	assert.NoError(t, err)
	assert.NotEmpty(t, newSessID)

	// Inherited fields should still be correct
	info, err := service.GetSessionInfo(newSessID)
	assert.NoError(t, err)
	assert.Equal(t, "claude", info.Backend)
	assert.Equal(t, "agent-1", info.AgentID)
	assert.Equal(t, "model-1", info.Model)
	// ThinkingEffort is no longer persisted to DB
}

// ---------- ContinueFromExecution: execution not found ----------

func TestContinueFromExecution_ExecutionNotFound(t *testing.T) {
	setupDB(t)

	_, _, err := service.ContinueFromExecution(99999, "/project")
	assert.Error(t, err)
}

// ---------- ContinueFromExecution: project mismatch ----------

func TestContinueFromExecution_ProjectMismatch(t *testing.T) {
	setupDB(t)

	taskID := helperCreateScheduledTask(t, "/project", "Task", "claude")
	sessID := helperCreateScheduledSession(t, "/project", "claude", "Task")
	execID := helperCreateTaskExecution(t, taskID, sessID, "completed")

	// Wrong project path
	_, _, err := service.ContinueFromExecution(execID, "/other-project")
	assert.Error(t, err)
}

// ---------- ContinueFromExecution: field inheritance ----------

func TestContinueFromExecution_FieldInheritance(t *testing.T) {
	setupDB(t)

	taskID := helperCreateScheduledTask(t, "/project", "Task", "codebuddy")
	sessID := helperCreateScheduledSessionWithDetails(t, "/project", "codebuddy", "Task", "cb-agent", "gpt-4o", "medium")
	// Set external session ID (codebuddy uses ClawBench UUID directly, so external_session_id stays empty)
	execID := helperCreateTaskExecution(t, taskID, sessID, "completed")

	newSessID, _, err := service.ContinueFromExecution(execID, "/project")
	assert.NoError(t, err)

	info, err := service.GetSessionInfo(newSessID)
	assert.NoError(t, err)
	assert.Equal(t, "codebuddy", info.Backend)
	assert.Equal(t, "cb-agent", info.AgentID)
	assert.Equal(t, "gpt-4o", info.Model)
	// ThinkingEffort is no longer persisted to DB

	// Project path should be inherited
	var projPath string
	err = service.DB.QueryRow("SELECT project_path FROM chat_sessions WHERE id = ?", newSessID).Scan(&projPath)
	assert.NoError(t, err)
	assert.Equal(t, "/project", projPath)

	// External session ID should be inherited from source session
	// (codebuddy now stores its ClawBench UUID as external_session_id)
	assert.Equal(t, sessID, service.GetExternalSessionID(newSessID))
}

// ---------- ContinueFromExecution: original session unaffected ----------

func TestContinueFromExecution_OriginalSessionUnaffected(t *testing.T) {
	setupDB(t)

	taskID := helperCreateScheduledTask(t, "/project", "Task", "claude")
	sessID := helperCreateScheduledSession(t, "/project", "claude", "Task")
	execID := helperCreateTaskExecution(t, taskID, sessID, "completed")

	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "prompt", nil, false, "")
	assert.NoError(t, err)

	newSessID, _, err := service.ContinueFromExecution(execID, "/project")
	assert.NoError(t, err)

	// Original session should still be scheduled type
	var origType string
	err = service.DB.QueryRow("SELECT session_type FROM chat_sessions WHERE id = ?", sessID).Scan(&origType)
	assert.NoError(t, err)
	assert.Equal(t, "scheduled", origType)

	// Original session's source_session_id should be NULL
	var origSource *string
	err = service.DB.QueryRow("SELECT source_session_id FROM chat_sessions WHERE id = ?", sessID).Scan(&origSource)
	assert.NoError(t, err)
	assert.Nil(t, origSource)

	// New session should NOT affect original messages
	origMsgs, err := service.GetChatHistory("/project", "claude", sessID)
	assert.NoError(t, err)
	assert.Len(t, origMsgs, 1)

	// New session should be completely separate
	newMsgs, err := service.GetChatHistory("/project", "claude", newSessID)
	assert.NoError(t, err)
	assert.Len(t, newMsgs, 1)
}

// ---------- ContinueFromExecution: no messages to copy ----------

func TestContinueFromExecution_NoMessages(t *testing.T) {
	setupDB(t)

	taskID := helperCreateScheduledTask(t, "/project", "Task", "claude")
	sessID := helperCreateScheduledSession(t, "/project", "claude", "Task")
	execID := helperCreateTaskExecution(t, taskID, sessID, "completed")

	// No messages added — should still create the session
	newSessID, _, err := service.ContinueFromExecution(execID, "/project")
	assert.NoError(t, err)
	assert.NotEmpty(t, newSessID)

	msgs, err := service.GetChatHistory("/project", "claude", newSessID)
	assert.NoError(t, err)
	assert.Empty(t, msgs)
}

// ========== Test Helpers ==========

// helperCreateScheduledTask creates a scheduled task and returns its ID.
func helperCreateScheduledTask(t *testing.T, projectPath, name, agentID string) int64 {
	t.Helper()
	result, err := service.DB.Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, status) VALUES (?, ?, '0 8 * * *', ?, 'Do task', 'active')",
		projectPath, name, agentID,
	)
	assert.NoError(t, err)
	id, err := result.LastInsertId()
	assert.NoError(t, err)
	return id
}

// helperCreateTaskExecution creates a task execution row and returns its ID.
func helperCreateTaskExecution(t *testing.T, taskID int64, sessionID, status string) int64 {
	t.Helper()
	result, err := service.DB.Exec(
		"INSERT INTO task_executions (task_id, session_id, status) VALUES (?, ?, ?)",
		taskID, sessionID, status,
	)
	assert.NoError(t, err)
	id, err := result.LastInsertId()
	assert.NoError(t, err)
	return id
}

// helperCreateScheduledSessionWithDetails creates a scheduled session with full metadata.
func helperCreateScheduledSessionWithDetails(t *testing.T, projectPath, backend, title, agentID, modelName, _ string) string {
	t.Helper()
	id, err := service.CreateSession(projectPath, backend, title, agentID, modelName, "default", "scheduled")
	assert.NoError(t, err)
	assert.NotEmpty(t, id)
	// ThinkingEffort is no longer persisted to DB; parameter kept for API compatibility
	return id
}

func TestContinueFromExecution_CopiesChatMessageSummary(t *testing.T) {
	// Since the scheduler now stores summaries as target_type='chat_message'
	// (keyed by the assistant message ID), ContinueFromExecution simply copies
	// these chat_message summaries from the source session to the new session,
	// just like interactive chat sessions.
	setupDB(t)
	projectPath := "/test/project"

	// Create task + session + assistant message + execution
	taskID := helperCreateScheduledTask(t, projectPath, "Summary Test", "agent1")
	sessionID := helperCreateScheduledSessionWithDetails(t, projectPath, "claude", "Summary Test", "agent1", "", "")
	_, err := service.DB.Exec("INSERT INTO chat_history (session_id, project_path, role, content, backend) VALUES (?, ?, 'user', 'hello', 'claude')", sessionID, projectPath)
	assert.NoError(t, err)
	_, err = service.DB.Exec("INSERT INTO chat_history (session_id, project_path, role, content, backend) VALUES (?, ?, 'assistant', '{\"blocks\":[{\"type\":\"text\",\"text\":\"response\"}]}', 'claude')", sessionID, projectPath)
	assert.NoError(t, err)
	execID := helperCreateTaskExecution(t, taskID, sessionID, "completed")

	// Add chat_message type summary on the source assistant message (this is
	// what the scheduler now creates — same as interactive sessions)
	var sourceAssistantID int64
	err = service.DB.QueryRow("SELECT id FROM chat_history WHERE session_id = ? AND role = 'assistant' ORDER BY id DESC LIMIT 1", sessionID).Scan(&sourceAssistantID)
	assert.NoError(t, err)
	_, err = service.DB.Exec("INSERT INTO summaries (target_type, target_id, summary, created_at) VALUES ('chat_message', ?, 'Scheduled task summary', CURRENT_TIMESTAMP)", sourceAssistantID)
	assert.NoError(t, err)

	// Continue
	newSessionID, alreadyExists, err := service.ContinueFromExecution(execID, projectPath)
	assert.NoError(t, err)
	assert.False(t, alreadyExists)
	assert.NotEmpty(t, newSessionID)

	// Verify: chat_message summary is copied to the corresponding new message
	var lastAssistantID int64
	err = service.DB.QueryRow("SELECT id FROM chat_history WHERE session_id = ? AND role = 'assistant' ORDER BY id DESC LIMIT 1", newSessionID).Scan(&lastAssistantID)
	assert.NoError(t, err)

	var copiedSummary string
	err = service.DB.QueryRow("SELECT summary FROM summaries WHERE target_type = 'chat_message' AND target_id = ?", lastAssistantID).Scan(&copiedSummary)
	assert.NoError(t, err)
	assert.Equal(t, "Scheduled task summary", copiedSummary)
}

// ========== restoreDeletedSession (tested via DB) ==========

// TestRestoreDeletedSession_NonExistent verifies that restoring a non-existent
// session does not error (UPDATE on non-existent row is a no-op).
func TestRestoreDeletedSession_NonExistent(t *testing.T) {
	setupDB(t)

	// Directly call the equivalent of restoreDeletedSession via DB
	_, err := service.DB.Exec(
		"UPDATE chat_sessions SET deleted = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		"non-existent-session-id",
	)
	assert.NoError(t, err)
}

// TestRestoreDeletedSession_AlreadyRestored verifies that restoring an
// already-active session (deleted=0) is idempotent — no error, no side effect.
func TestRestoreDeletedSession_AlreadyRestored(t *testing.T) {
	setupDB(t)

	sid := helperCreateSession(t, "/project", "claude", "Active")
	// Session is already active (deleted=0)
	_, err := service.DB.Exec(
		"UPDATE chat_sessions SET deleted = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		sid,
	)
	assert.NoError(t, err)

	var deleted int
	err = service.DB.QueryRow("SELECT deleted FROM chat_sessions WHERE id = ?", sid).Scan(&deleted)
	assert.NoError(t, err)
	assert.Equal(t, 0, deleted, "session should still be active")
}

// ========== CheckContinueSession: soft-deleted session auto-restore ==========

// TestCheckContinueSession_AutoRestoresDeletedSession verifies that
// CheckContinueSession finds a soft-deleted continued session and
// auto-restores it (sets deleted=0), returning exists=true.
func TestCheckContinueSession_AutoRestoresDeletedSession(t *testing.T) {
	setupDB(t)

	taskID := helperCreateScheduledTask(t, "/project", "Task", "claude")
	sessID := helperCreateScheduledSession(t, "/project", "claude", "Task")
	execID := helperCreateTaskExecution(t, taskID, sessID, "completed")

	// Continue the execution to create a continued session
	newSessID, _, err := service.ContinueFromExecution(execID, "/project")
	assert.NoError(t, err)

	// Soft-delete the continued session
	err = service.DeleteSession("/project", "claude", newSessID)
	assert.NoError(t, err)

	// CheckContinueSession should find and auto-restore the deleted session
	exists, foundID, err := service.CheckContinueSession(execID)
	assert.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, newSessID, foundID)

	// Verify the session is restored (deleted=0)
	var deleted int
	err = service.DB.QueryRow("SELECT deleted FROM chat_sessions WHERE id = ?", newSessID).Scan(&deleted)
	assert.NoError(t, err)
	assert.Equal(t, 0, deleted, "session should be restored (deleted=0)")
}

// ========== Dedup query: ORDER BY with both active and deleted sessions ==========

// TestContinueFromExecution_DedupPrefersActiveOverDeleted verifies that
// when both an active and a soft-deleted continued session exist for the same
// source_session_id, the dedup query (ORDER BY deleted ASC, updated_at DESC LIMIT 1)
// returns the active one, so no unnecessary restore happens.
func TestContinueFromExecution_DedupPrefersActiveOverDeleted(t *testing.T) {
	setupDB(t)

	taskID := helperCreateScheduledTask(t, "/project", "Task", "claude")
	sessID := helperCreateScheduledSession(t, "/project", "claude", "Task")
	execID := helperCreateTaskExecution(t, taskID, sessID, "completed")

	// First continuation creates session A
	sessA, _, err := service.ContinueFromExecution(execID, "/project")
	assert.NoError(t, err)

	// Soft-delete session A
	err = service.DeleteSession("/project", "claude", sessA)
	assert.NoError(t, err)

	// Manually create session B (simulating a second continued session)
	// by directly inserting into the DB with a different ID
	sessB := "manual-continued-session-b"
	_ = service.DB.QueryRow("SELECT id FROM chat_sessions WHERE id = ?", sessB).Scan(new(string))
	// sessB shouldn't exist yet
	_, err = service.DB.Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type, source_session_id, external_session_id) VALUES (?, ?, ?, ?, ?, ?, ?, 'chat', ?, ?)",
		sessB, "/project", "claude", "Manual B", "", "default", "", sessID, sessID,
	)
	assert.NoError(t, err)

	// Now dedup: ORDER BY deleted ASC should prefer sessB (deleted=0) over sessA (deleted=1)
	exists, foundID, err := service.CheckContinueSession(execID)
	assert.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, sessB, foundID, "active session B should be preferred over deleted session A")
}

// ========== ContinueFromExecution with empty external_session_id ==========

// TestContinueFromExecution_EmptyExternalSessionID verifies that when the source
// session has an empty external_session_id (e.g., session_capture was missed),
// the continued session still gets created with the empty value.
// buildChatRequest handles this by clearing effectiveSessionID (no --resume).
func TestContinueFromExecution_EmptyExternalSessionID(t *testing.T) {
	setupDB(t)

	taskID := helperCreateScheduledTask(t, "/project", "Task", "pi")
	sessID := helperCreateScheduledSession(t, "/project", "pi", "Task")
	execID := helperCreateTaskExecution(t, taskID, sessID, "completed")

	// Clear external_session_id to simulate missed session_capture
	err := service.UpdateExternalSessionID(sessID, "")
	assert.NoError(t, err)

	newSessID, _, err := service.ContinueFromExecution(execID, "/project")
	assert.NoError(t, err)
	assert.NotEmpty(t, newSessID)

	// Continued session should have empty external_session_id (inherited from source)
	extID := service.GetExternalSessionID(newSessID)
	assert.Equal(t, "", extID, "continued session should inherit empty external_session_id")
}

// ========== Restored session preserves all chat history ==========

// TestContinueFromExecution_RestoredSessionPreservesHistory verifies that
// when a continued session is deleted and then restored, all chat history
// is still present. This was a key bug: the old code soft-deleted
// chat_history rows, but now only the session record is soft-deleted.
func TestContinueFromExecution_RestoredSessionPreservesHistory(t *testing.T) {
	setupDB(t)

	taskID := helperCreateScheduledTask(t, "/project", "Task", "claude")
	sessID := helperCreateScheduledSession(t, "/project", "claude", "Task")
	execID := helperCreateTaskExecution(t, taskID, sessID, "completed")

	// Add messages to source session
	_, err := service.AddChatMessage("/project", "claude", sessID, "user", "Review the code", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/project", "claude", sessID, "assistant", "Code looks good", nil, false, "")
	assert.NoError(t, err)

	// Continue the execution
	newSessID, _, err := service.ContinueFromExecution(execID, "/project")
	assert.NoError(t, err)

	// Verify the continued session has messages
	msgs, err := service.GetChatHistory("/project", "claude", newSessID)
	assert.NoError(t, err)
	assert.Len(t, msgs, 2)

	// Add an extra message to the continued session
	_, err = service.AddChatMessage("/project", "claude", newSessID, "user", "Tell me more", nil, false, "")
	assert.NoError(t, err)

	// Soft-delete the continued session
	err = service.DeleteSession("/project", "claude", newSessID)
	assert.NoError(t, err)

	// Restore via ContinueFromExecution
	restoredID, alreadyExists, err := service.ContinueFromExecution(execID, "/project")
	assert.NoError(t, err)
	assert.Equal(t, newSessID, restoredID)
	assert.True(t, alreadyExists)

	// Verify chat history is intact after restore
	// GetChatHistory works even for deleted sessions since chat_history has no deleted column
	msgs, err = service.GetChatHistory("/project", "claude", restoredID)
	assert.NoError(t, err)
	assert.Len(t, msgs, 3, "restored session should have all 3 messages (2 copied + 1 added)")
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "Review the code", msgs[0].Content)
	assert.Equal(t, "assistant", msgs[1].Role)
	assert.Equal(t, "Code looks good", msgs[1].Content)
	assert.Equal(t, "user", msgs[2].Role)
	assert.Equal(t, "Tell me more", msgs[2].Content)
}

// ========== Soft-deleted source session external_session_id read ==========

// TestContinueFromExecution_SoftDeletedSourceReadsExternalSessionID verifies
// that ContinueFromExecution can read external_session_id from a soft-deleted
// source session (the query does not filter by deleted=0).
func TestContinueFromExecution_SoftDeletedSourceReadsExternalSessionID(t *testing.T) {
	setupDB(t)

	taskID := helperCreateScheduledTask(t, "/project", "Task", "opencode")
	sessID := helperCreateScheduledSession(t, "/project", "opencode", "Task")
	execID := helperCreateTaskExecution(t, taskID, sessID, "completed")

	// Set external_session_id on the source session
	err := service.UpdateExternalSessionID(sessID, "opencode-sess-xyz")
	assert.NoError(t, err)

	// Soft-delete the source session
	err = service.DeleteSession("/project", "opencode", sessID)
	assert.NoError(t, err)

	// Continue should still be able to read external_session_id from the soft-deleted source
	newSessID, _, err := service.ContinueFromExecution(execID, "/project")
	assert.NoError(t, err)
	assert.Equal(t, "opencode-sess-xyz", service.GetExternalSessionID(newSessID),
		"continued session should inherit external_session_id from soft-deleted source")
}

// ========== Title format edge cases ==========

// TestContinueFromExecution_TitleFormatWithExplicitTimestamp verifies the
// [MM-DD HH:MM] title prefix with a known execution timestamp.
func TestContinueFromExecution_TitleFormatWithExplicitTimestamp(t *testing.T) {
	setupDB(t)

	taskID := helperCreateScheduledTask(t, "/project", "Daily Review", "claude")
	sessID := helperCreateScheduledSession(t, "/project", "claude", "Daily Review")

	// Insert execution with a known created_at
	result, err := service.DB.Exec(
		"INSERT INTO task_executions (task_id, session_id, status, created_at) VALUES (?, ?, 'completed', '2026-03-15 08:30:00')",
		taskID, sessID,
	)
	assert.NoError(t, err)
	execID, _ := result.LastInsertId()

	newSessID, _, err := service.ContinueFromExecution(execID, "/project")
	assert.NoError(t, err)

	title, err := service.GetSessionTitle(newSessID)
	assert.NoError(t, err)
	assert.Equal(t, "[03-15 08:30] Daily Review", title)
}

// Regression test: copied messages must NOT have ISO 8601 UTC timestamps
// (e.g. "2026-05-29T01:59:53Z") because the Go SQLite driver converts DATETIME
// to that format when reading. When written back, the format breaks string-based
// time comparisons with CURRENT_TIMESTAMP format ("YYYY-MM-DD HH:MM:SS"),
// causing phantom unread badges. The fix: let the database assign CURRENT_TIMESTAMP
// as created_at instead of copying the ISO-format value.
func TestContinueFromExecution_CreatedAtFormatConsistent(t *testing.T) {
	setupDB(t)
	projectPath := "/project"

	taskID := helperCreateScheduledTask(t, projectPath, "Format Test", "claude")
	sessID := helperCreateScheduledSession(t, projectPath, "claude", "Format Test")
	execID := helperCreateTaskExecution(t, taskID, sessID, "completed")

	// Add messages (AddChatMessage uses DEFAULT CURRENT_TIMESTAMP → "YYYY-MM-DD HH:MM:SS")
	_, err := service.AddChatMessage(projectPath, "claude", sessID, "user", "prompt", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage(projectPath, "claude", sessID, "assistant", "response", nil, false, "")
	assert.NoError(t, err)

	newSessID, _, err := service.ContinueFromExecution(execID, projectPath)
	assert.NoError(t, err)

	// Verify: copied messages' created_at should NOT contain 'T' or 'Z' (ISO format markers)
	var hasBadFormat int
	err = service.DB.QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND (created_at LIKE '%T%' OR created_at LIKE '%Z%')",
		newSessID,
	).Scan(&hasBadFormat)
	assert.NoError(t, err)
	assert.Equal(t, 0, hasBadFormat, "copied messages should use CURRENT_TIMESTAMP format, not ISO 8601")

	// Verify: unread count query should return 0 for the continued session
	// (last_read_at is set at creation time, and created_at uses the same format)
	var unreadCount int
	err = service.DB.QueryRow(
		`
		SELECT COALESCE(unread.cnt, 0) FROM chat_sessions s
		LEFT JOIN (
			SELECT h.session_id, COUNT(*) AS cnt
			FROM chat_history h
			JOIN chat_sessions s2 ON s2.id = h.session_id
			WHERE h.project_path = ?
			  AND h.role = 'assistant' AND h.streaming = 0
			  AND (s2.last_read_at IS NULL OR h.created_at > s2.last_read_at)
			GROUP BY h.session_id
		) unread ON unread.session_id = s.id
		WHERE s.id = ?`,
		projectPath, newSessID,
	).Scan(&unreadCount)
	assert.NoError(t, err)
	assert.Equal(t, 0, unreadCount, "continued session should have 0 unread messages after creation")
}

// ========== CheckContinueSession: execution not found ==========

func TestCheckContinueSession_ExecutionNotFound(t *testing.T) {
	setupDB(t)

	exists, sessionID, err := service.CheckContinueSession(99999)
	assert.Error(t, err)
	assert.False(t, exists)
	assert.Empty(t, sessionID)
}

// ========== ContinueFromExecution: task not found ==========

func TestContinueFromExecution_TaskNotFound(t *testing.T) {
	setupDB(t)

	// Create an execution referencing a non-existent task
	sessID := helperCreateScheduledSession(t, "/project", "claude", "Task")
	result, err := service.DB.Exec(
		"INSERT INTO task_executions (task_id, session_id, status) VALUES (99999, ?, 'completed')",
		sessID,
	)
	assert.NoError(t, err)
	execID, _ := result.LastInsertId()

	_, _, err = service.ContinueFromExecution(execID, "/project")
	assert.Error(t, err)
}

// ========== CheckContinueSession: closed DB connection ==========

func TestCheckContinueSession_ClosedDB(t *testing.T) {
	setupDB(t)

	origDBRead := service.DBRead
	// Use a closed DB connection — this triggers a real error from the SQL driver
	closedDB, err := sql.Open("sqlite", ":memory:")
	assert.NoError(t, err)
	closedDB.Close()
	service.DBRead = closedDB
	t.Cleanup(func() { service.DBRead = origDBRead })

	exists, sessionID, err := service.CheckContinueSession(1)
	assert.Error(t, err)
	assert.False(t, exists)
	assert.Empty(t, sessionID)
}

// ========== restoreDeletedSession: DB error path ==========

func TestRestoreDeletedSession_DBError(t *testing.T) {
	setupDB(t)

	origDB := service.DB
	// Set DB to a closed connection to trigger an error
	closedDB, err := sql.Open("sqlite", ":memory:")
	assert.NoError(t, err)
	closedDB.Close()
	service.DB = closedDB
	t.Cleanup(func() { service.DB = origDB })

	// restoreDeletedSession is private, but we can test the equivalent DB call
	_, err = service.DB.Exec(
		"UPDATE chat_sessions SET deleted = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		"some-session-id",
	)
	assert.Error(t, err, "should fail with closed DB")
}


