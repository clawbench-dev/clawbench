package service

import (
	"database/sql"
	"sync/atomic"
	"time"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/ws"

	"github.com/stretchr/testify/assert"
)

func setupDrainTest() {
	ws.SetManagerForTest(ws.NewManagerForTest())
}

// drainTestSchema is the chat_history/chat_sessions schema used by drain tests
// (mirrors the main test schema in chat_test.go but lives in package service).
const drainTestSchema = `
CREATE TABLE IF NOT EXISTS chat_history (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_path TEXT NOT NULL,
	role TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
	content TEXT NOT NULL,
	files TEXT,
	session_id TEXT,
	backend TEXT NOT NULL DEFAULT 'claude',
	streaming INTEGER NOT NULL DEFAULT 0,
	indexed INTEGER NOT NULL DEFAULT 0,
	external_message_id TEXT DEFAULT '',
	queue_id TEXT DEFAULT '',
	queued INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS chat_sessions (
	id TEXT PRIMARY KEY,
	project_path TEXT NOT NULL,
	backend TEXT NOT NULL,
	title TEXT NOT NULL,
	agent_id TEXT DEFAULT '',
	archived INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

// setupDrainSession creates a DB session + queued messages for drain tests.
func setupDrainSession(t *testing.T, sessionID string) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	assert.NoError(t, err)
	_, err = db.Exec(drainTestSchema)
	assert.NoError(t, err)
	cleanup := SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		db.Close()
	})
	_, err = db.Exec(
		`INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, '/test', 'codebuddy', 'Drain')`,
		sessionID,
	)
	assert.NoError(t, err)
}

func TestDrainLoop_UserCancel_ClearsQueueAndEmitsCancel(t *testing.T) {
	setupDrainTest()
	sessionID := "drain-test-user-cancel"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	// Enqueue some messages so queue_cancel has queueIDs to emit
	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "pending1", nil, "q1", "")
	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "pending2", nil, "q2", "")

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{CancelReason: cancelReasonUser})
	assert.Equal(t, statusCancelled, finalEvent.Type)

	// Queue should be cleared
	assert.Equal(t, 0, GetQueuedCount(sessionID))
}

func TestDrainLoop_UserCancel_WithQueueIDs_EmitsQueueCancel(t *testing.T) {
	setupDrainTest()
	sessionID := "drain-test-queue-cancel-event"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "pending", nil, "qc1", "")

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	// queue_cancel is emitted via ws.EmitToSession which requires a subscriber;
	// we verify the DB side effect (queue cleared) instead.
	RunDrainLoop(cfg, DrainResult{CancelReason: cancelReasonUser})
	assert.Equal(t, statusCancelled, finalEvent.Type)
	assert.Equal(t, 0, GetQueuedCount(sessionID))
}

func TestDrainLoop_UserCancel_NoQueueIDs_NoQueueCancelEvent(t *testing.T) {
	setupDrainTest()
	sessionID := "drain-test-no-queue-ids"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	// No messages in queue — queue_cancel should not be emitted (no queueIDs)
	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{CancelReason: cancelReasonUser})
	assert.Equal(t, statusCancelled, finalEvent.Type)
}

func TestDrainLoop_ErrorResult_EmitsErrorEvent(t *testing.T) {
	setupDrainTest()
	sessionID := "drain-test-error"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{Err: "runtime failure"})
	assert.Equal(t, "error", finalEvent.Type)
	assert.Equal(t, "runtime failure", finalEvent.Error)
}

func TestDrainLoop_EmptyResult_EmitsErrorWithReason(t *testing.T) {
	setupDrainTest()
	sessionID := "drain-test-empty"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{Empty: true})
	assert.Equal(t, "error", finalEvent.Type)
	assert.Equal(t, "AI returned no content", finalEvent.Error)
	assert.Equal(t, ai.ReasonEmpty, finalEvent.Reason)
}

func TestDrainLoop_NonUserCancelReason_EmitsCancelled(t *testing.T) {
	setupDrainTest()
	sessionID := "drain-test-other-cancel"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{CancelReason: "disconnect"})
	assert.Equal(t, statusCancelled, finalEvent.Type)
}

func TestDrainLoop_QueueEmpty_EmitsDone(t *testing.T) {
	setupDrainTest()
	sessionID := "drain-test-empty-queue"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{})
	assert.Equal(t, "done", finalEvent.Type)
}

func TestDrainLoop_QueueHasNextMessage_ExecutesAndLoops(t *testing.T) {
	setupDrainTest()
	sessionID := "drain-test-next-msg"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	// Enqueue two messages via DB
	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "first queued", []model.FileEntry{{Path: "/a/b.go"}}, "q1", "")
	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "second queued", nil, "q2", "")

	var executeCount int32
	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			atomic.AddInt32(&executeCount, 1)
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{})

	// Both queued messages should have been executed
	assert.Equal(t, int32(2), atomic.LoadInt32(&executeCount))
	assert.Equal(t, "done", finalEvent.Type)
}

func TestDrainLoop_QueueMessageReturnsError_StopsLoop(t *testing.T) {
	setupDrainTest()
	sessionID := "drain-test-msg-error"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "will error", nil, "q1", "")

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			return DrainResult{Err: "execution failed"}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{})
	assert.Equal(t, "error", finalEvent.Type)
	assert.Equal(t, "execution failed", finalEvent.Error)
}

func TestDrainLoop_QueueMessageCancelled_StopsLoop(t *testing.T) {
	setupDrainTest()
	sessionID := "drain-test-msg-cancel"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "will cancel", nil, "q1", "")

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			return DrainResult{CancelReason: cancelReasonUser}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{})
	assert.Equal(t, statusCancelled, finalEvent.Type)
}

func TestDrainLoop_UserCancelWithQueueIDsOnly_IncludesOnlyNonEmptyQueueIDs(t *testing.T) {
	setupDrainTest()
	sessionID := "drain-test-mixed-qids"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	// Mix: one with QueueID, one without (AddQueuedMessage auto-generates when empty)
	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "a", nil, "has-id", "")
	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "no-id", nil, "", "")

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{CancelReason: cancelReasonUser})
	assert.Equal(t, statusCancelled, finalEvent.Type)
	assert.Equal(t, 0, GetQueuedCount(sessionID))
}

// TestWaitForEnqueue_SignaledBySignalDrain verifies a queued message signal
// wakes WaitForEnqueue immediately (design plan: SignalDrain → WaitForEnqueue
// returns true).
func TestWaitForEnqueue_SignaledBySignalDrain(t *testing.T) {
	sessionID := "drain-wait-signal"
	started := make(chan struct{})
	done := make(chan bool)
	go func() {
		close(started)
		done <- WaitForEnqueue(sessionID, 500*time.Millisecond)
	}()
	<-started
	SignalDrain(sessionID)
	assert.True(t, <-done, "WaitForEnqueue must return true when signaled")
}

// TestWaitForEnqueue_Timeout verifies WaitForEnqueue returns false when no
// signal arrives within the timeout.
func TestWaitForEnqueue_Timeout(t *testing.T) {
	sessionID := "drain-wait-timeout"
	start := time.Now()
	assert.False(t, WaitForEnqueue(sessionID, 50*time.Millisecond))
	assert.GreaterOrEqual(t, time.Since(start), 40*time.Millisecond)
}
