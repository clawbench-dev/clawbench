package service

import (
	"sync"
	"time"

	"clawbench/internal/model"
)

// sessionDrainChans stores per-session channels used to signal the drain loop
// when a new message is enqueued, replacing the old 50ms sleep+retry hack.
var sessionDrainChans sync.Map // map[string]chan struct{}

func getDrainChan(sessionID string) chan struct{} {
	val, _ := sessionDrainChans.LoadOrStore(sessionID, make(chan struct{}, 1))
	return val.(chan struct{}) //nolint:errcheck // LoadOrStore always returns chan struct{}
}

// SignalDrain wakes up a waiting drain loop for a session, if any. Non-blocking:
// if no drain loop is waiting (buffer full), the signal is dropped — the next
// drain-loop check of the DB queue will pick the message up anyway.
func SignalDrain(sessionID string) {
	select {
	case getDrainChan(sessionID) <- struct{}{}:
	default:
	}
}

// WaitForEnqueue blocks until a message is enqueued or the timeout expires.
// The drain loop calls this instead of time.Sleep to get immediate wake-up.
func WaitForEnqueue(sessionID string, timeout time.Duration) bool {
	select {
	case <-getDrainChan(sessionID):
		return true
	case <-time.After(timeout):
		return false
	}
}

// NOTE: The legacy in-memory queued-message store (EnqueueMessage/GetQueue/
// ClearQueue) has been removed in favour of queued-message persistence in
// chat_history (queued-message-persistence plan). These remaining functions
// are migration shims used by the push SessionMessenger implementations until
// Task 6 migrates them; they will be deleted once that lands.

// EnqueueMessage adds a message to the session's in-memory queue (legacy shim).
// Deprecated: use AddQueuedMessage + EnqueueAndMaybeStart instead.
func EnqueueMessage(sessionID string, msg model.QueuedMessage) []model.QueuedMessage {
	info := GetSessionFullInfo(sessionID)
	if info == nil {
		return nil
	}
	if _, err := AddQueuedMessage(info.ProjectPath, info.Backend, sessionID, msg.Text, msg.Files, msg.QueueID, info.Title); err != nil {
		return nil
	}
	SignalDrain(sessionID)
	return GetQueue(sessionID)
}

// GetQueue returns the current in-memory queue for a session (legacy shim).
// Deprecated: use GetQueuedMessages instead.
func GetQueue(sessionID string) []model.QueuedMessage {
	msgs, err := GetQueuedMessages(sessionID)
	if err != nil || msgs == nil {
		return nil
	}
	queue := make([]model.QueuedMessage, 0, len(msgs))
	for _, m := range msgs {
		queue = append(queue, model.QueuedMessage{
			QueueID:   m.QueueID,
			Text:      m.Content,
			FilePaths: filePathsFromFiles(m.Files),
			Files:     m.Files,
			CreatedAt: m.CreatedAt.Format(time.RFC3339),
		})
	}
	return queue
}

// ClearQueue removes all queued messages for a session (legacy shim).
// Deprecated: use ClearQueuedMessages instead.
func ClearQueue(sessionID string) {
	_ = ClearQueuedMessages(sessionID)
}
