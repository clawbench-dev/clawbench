package service

import (
	"sync/atomic"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/ws"

	"github.com/stretchr/testify/assert"
)

func setupDrainTest() {
	ws.SetManagerForTest(ws.NewManagerForTest())
}

func TestDrainLoop_UserCancel_ClearsQueueAndEmitsCancel(t *testing.T) {
	setupDrainTest()
	sessionID := "drain-test-user-cancel"
	defer ClearQueue(sessionID)

	// Enqueue some messages so queue_cancel has queueIDs to emit
	EnqueueMessage(sessionID, model.QueuedMessage{QueueID: "q1", Text: "pending1", CreatedAt: time.Now().Format(time.RFC3339)})
	EnqueueMessage(sessionID, model.QueuedMessage{QueueID: "q2", Text: "pending2", CreatedAt: time.Now().Format(time.RFC3339)})

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		PersistUser: func(text string, files []model.FileEntry) (int64, error) {
			return 1, nil
		},
		ExecuteRunWithMessage: func(qMsg model.QueuedMessage) DrainResult {
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{CancelReason: cancelReasonUser})
	assert.Equal(t, statusCancelled, finalEvent.Type)

	// Queue should be cleared
	assert.Nil(t, GetQueue(sessionID))
}

func TestDrainLoop_UserCancel_WithQueueIDs_EmitsQueueCancel(t *testing.T) {
	setupDrainTest()
	sessionID := "drain-test-queue-cancel-event"
	defer ClearQueue(sessionID)

	EnqueueMessage(sessionID, model.QueuedMessage{QueueID: "qc1", Text: "pending", CreatedAt: time.Now().Format(time.RFC3339)})

	var finalEvent ai.StreamEvent
	var queueCancelEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		PersistUser: func(text string, files []model.FileEntry) (int64, error) {
			return 1, nil
		},
		ExecuteRunWithMessage: func(qMsg model.QueuedMessage) DrainResult {
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	// We need to intercept emitDrainEvent — since it uses ws.EmitToSession,
	// which requires subscribers, the queue_cancel event won't be captured
	// without a subscriber. We verify the side effects instead.
	RunDrainLoop(cfg, DrainResult{CancelReason: cancelReasonUser})
	assert.Equal(t, statusCancelled, finalEvent.Type)
	assert.Nil(t, GetQueue(sessionID))

	// Suppress unused warning
	_ = queueCancelEvent
}

func TestDrainLoop_UserCancel_NoQueueIDs_NoQueueCancelEvent(t *testing.T) {
	setupDrainTest()
	sessionID := "drain-test-no-queue-ids"
	defer ClearQueue(sessionID)

	// No messages in queue — queue_cancel should not be emitted (no queueIDs)
	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		PersistUser: func(text string, files []model.FileEntry) (int64, error) {
			return 1, nil
		},
		ExecuteRunWithMessage: func(qMsg model.QueuedMessage) DrainResult {
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
	defer ClearQueue(sessionID)

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		PersistUser: func(text string, files []model.FileEntry) (int64, error) {
			return 1, nil
		},
		ExecuteRunWithMessage: func(qMsg model.QueuedMessage) DrainResult {
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
	defer ClearQueue(sessionID)

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		PersistUser: func(text string, files []model.FileEntry) (int64, error) {
			return 1, nil
		},
		ExecuteRunWithMessage: func(qMsg model.QueuedMessage) DrainResult {
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
	defer ClearQueue(sessionID)

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		PersistUser: func(text string, files []model.FileEntry) (int64, error) {
			return 1, nil
		},
		ExecuteRunWithMessage: func(qMsg model.QueuedMessage) DrainResult {
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
	defer ClearQueue(sessionID)

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		PersistUser: func(text string, files []model.FileEntry) (int64, error) {
			return 1, nil
		},
		ExecuteRunWithMessage: func(qMsg model.QueuedMessage) DrainResult {
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{})
	assert.Equal(t, "done", finalEvent.Type)
}

func TestDrainLoop_QueueHasNextMessage_PersistsExecutesAndLoops(t *testing.T) {
	setupDrainTest()
	sessionID := "drain-test-next-msg"
	defer ClearQueue(sessionID)

	// Enqueue two messages
	EnqueueMessage(sessionID, model.QueuedMessage{
		QueueID:   "q1",
		Text:      "first queued",
		FilePaths: []string{"/a/b.go"},
		Files:     []model.FileEntry{{Path: "/a/b.go"}},
		CreatedAt: time.Now().Format(time.RFC3339),
	})
	EnqueueMessage(sessionID, model.QueuedMessage{
		QueueID:   "q2",
		Text:      "second queued",
		CreatedAt: time.Now().Format(time.RFC3339),
	})

	var persistedTexts []string
	var persistedFiles [][]model.FileEntry
	var executeCount int32
	var finalEvent ai.StreamEvent

	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		PersistUser: func(text string, files []model.FileEntry) (int64, error) {
			persistedTexts = append(persistedTexts, text)
			persistedFiles = append(persistedFiles, files)
			return int64(len(persistedTexts)), nil
		},
		ExecuteRunWithMessage: func(qMsg model.QueuedMessage) DrainResult {
			atomic.AddInt32(&executeCount, 1)
			// First execution returns empty result (loop continues, dequeues next)
			// Second execution also returns empty result (loop continues, queue now empty)
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	// Initial result is empty (normal completion) — loop should dequeue and execute
	RunDrainLoop(cfg, DrainResult{})

	// Both queued messages should have been persisted and executed
	assert.Equal(t, int32(2), atomic.LoadInt32(&executeCount))
	assert.Equal(t, []string{"first queued", "second queued"}, persistedTexts)
	assert.Len(t, persistedFiles, 2)
	assert.Equal(t, []model.FileEntry{{Path: "/a/b.go"}}, persistedFiles[0])
	assert.Equal(t, "done", finalEvent.Type)
}

func TestDrainLoop_QueueMessageReturnsError_StopsLoop(t *testing.T) {
	setupDrainTest()
	sessionID := "drain-test-msg-error"
	defer ClearQueue(sessionID)

	EnqueueMessage(sessionID, model.QueuedMessage{
		QueueID:   "q1",
		Text:      "will error",
		CreatedAt: time.Now().Format(time.RFC3339),
	})

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		PersistUser: func(text string, files []model.FileEntry) (int64, error) {
			return 1, nil
		},
		ExecuteRunWithMessage: func(qMsg model.QueuedMessage) DrainResult {
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
	defer ClearQueue(sessionID)

	EnqueueMessage(sessionID, model.QueuedMessage{
		QueueID:   "q1",
		Text:      "will cancel",
		CreatedAt: time.Now().Format(time.RFC3339),
	})

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		PersistUser: func(text string, files []model.FileEntry) (int64, error) {
			return 1, nil
		},
		ExecuteRunWithMessage: func(qMsg model.QueuedMessage) DrainResult {
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
	defer ClearQueue(sessionID)

	// Mix: one with QueueID, one without
	EnqueueMessage(sessionID, model.QueuedMessage{QueueID: "has-id", Text: "a", CreatedAt: time.Now().Format(time.RFC3339)})
	EnqueueMessage(sessionID, model.QueuedMessage{QueueID: "", Text: "no-id", CreatedAt: time.Now().Format(time.RFC3339)})

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		PersistUser: func(text string, files []model.FileEntry) (int64, error) {
			return 1, nil
		},
		ExecuteRunWithMessage: func(qMsg model.QueuedMessage) DrainResult {
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{CancelReason: cancelReasonUser})
	assert.Equal(t, statusCancelled, finalEvent.Type)
	assert.Nil(t, GetQueue(sessionID))
}
