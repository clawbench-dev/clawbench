package service

import (
	"sync"
	"time"

	"clawbench/internal/model"
)

type queueEntry struct {
	mu    sync.Mutex
	items []model.QueuedMessage
}

var sessionQueues sync.Map // map[string]*queueEntry

// sessionDrainChans stores per-session channels used to signal the drain loop
// when a new message is enqueued, replacing the old 50ms sleep+retry hack.
var sessionDrainChans sync.Map // map[string]chan struct{}

func getOrCreateEntry(sessionID string) *queueEntry {
	val, _ := sessionQueues.LoadOrStore(sessionID, &queueEntry{})
	return val.(*queueEntry) //nolint:errcheck // LoadOrStore always returns *queueEntry
}

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

// EnqueueMessage adds a message to the session's queue and returns the full queue.
// It also signals the drain channel so a waiting drain loop can wake up immediately.
func EnqueueMessage(sessionID string, msg model.QueuedMessage) []model.QueuedMessage {
	entry := getOrCreateEntry(sessionID)
	entry.mu.Lock()
	entry.items = append(entry.items, msg)
	result := make([]model.QueuedMessage, len(entry.items))
	copy(result, entry.items)
	entry.mu.Unlock()

	// Signal drain loop — non-blocking, drop if already signaled
	SignalDrain(sessionID)

	return result
}

// DequeueMessage removes and returns the first message from the queue.
// Returns false if the queue is empty.
func DequeueMessage(sessionID string) (model.QueuedMessage, bool) {
	val, ok := sessionQueues.Load(sessionID)
	if !ok {
		return model.QueuedMessage{}, false
	}
	entry := val.(*queueEntry) //nolint:errcheck // Load always returns *queueEntry
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if len(entry.items) == 0 {
		return model.QueuedMessage{}, false
	}
	msg := entry.items[0]
	entry.items = entry.items[1:]
	// Keep the queue entry alive when empty instead of deleting it.
	// Deleting causes a TOCTOU race: concurrent EnqueueMessage creates a new entry
	// via LoadOrStore while drain loop already saw empty — new message is lost (ISS-293).
	// Only ClearQueue removes the sync.Map entry entirely.
	return msg, true
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

// GetQueue returns a snapshot of the current queue for a session.
func GetQueue(sessionID string) []model.QueuedMessage {
	val, ok := sessionQueues.Load(sessionID)
	if !ok {
		return nil
	}
	entry := val.(*queueEntry) //nolint:errcheck // Load always returns *queueEntry
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if len(entry.items) == 0 {
		return nil
	}
	result := make([]model.QueuedMessage, len(entry.items))
	copy(result, entry.items)
	return result
}

// RemoveQueueItem removes the item at the given index and returns the updated queue.
// Returns nil if the index is out of range or the session has no queue.
func RemoveQueueItem(sessionID string, index int) []model.QueuedMessage {
	val, ok := sessionQueues.Load(sessionID)
	if !ok {
		return nil
	}
	entry := val.(*queueEntry) //nolint:errcheck // Load always returns *queueEntry
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if index < 0 || index >= len(entry.items) {
		result := make([]model.QueuedMessage, len(entry.items))
		copy(result, entry.items)
		return result
	}
	entry.items = append(entry.items[:index], entry.items[index+1:]...)
	if len(entry.items) == 0 {
		return nil
	}
	result := make([]model.QueuedMessage, len(entry.items))
	copy(result, entry.items)
	return result
}

// RemoveQueueItemByQueueID removes the item with the given queueID and returns the updated queue.
// Returns nil if the item is not found or the session has no queue.
func RemoveQueueItemByQueueID(sessionID string, queueID string) []model.QueuedMessage {
	if queueID == "" {
		return GetQueue(sessionID)
	}
	val, ok := sessionQueues.Load(sessionID)
	if !ok {
		return nil
	}
	entry := val.(*queueEntry) //nolint:errcheck // Load always returns *queueEntry
	entry.mu.Lock()
	defer entry.mu.Unlock()
	idx := -1
	for i, item := range entry.items {
		if item.QueueID == queueID {
			idx = i
			break
		}
	}
	if idx < 0 {
		result := make([]model.QueuedMessage, len(entry.items))
		copy(result, entry.items)
		return result
	}
	entry.items = append(entry.items[:idx], entry.items[idx+1:]...)
	if len(entry.items) == 0 {
		return nil
	}
	result := make([]model.QueuedMessage, len(entry.items))
	copy(result, entry.items)
	return result
}

// ClearQueue removes all items from the session's queue.
func ClearQueue(sessionID string) {
	sessionQueues.Delete(sessionID)
}
