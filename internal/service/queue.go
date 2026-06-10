package service

import (
	"sync"

	"clawbench/internal/model"
)

type queueEntry struct {
	mu    sync.Mutex
	items []model.QueuedMessage
}

var sessionQueues sync.Map // map[string]*queueEntry

func getOrCreateEntry(sessionID string) *queueEntry {
	val, _ := sessionQueues.LoadOrStore(sessionID, &queueEntry{})
	return val.(*queueEntry) //nolint:errcheck // LoadOrStore always returns *queueEntry
}

// EnqueueMessage adds a message to the session's queue and returns the full queue.
func EnqueueMessage(sessionID string, msg model.QueuedMessage) []model.QueuedMessage {
	entry := getOrCreateEntry(sessionID)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	entry.items = append(entry.items, msg)
	result := make([]model.QueuedMessage, len(entry.items))
	copy(result, entry.items)
	return result
}

// DequeueMessage removes and returns the first message from the queue.
// Returns false if the queue is empty.
func DequeueMessage(sessionID string) (model.QueuedMessage, bool) {
	val, ok := sessionQueues.Load(sessionID)
	if !ok {
		return model.QueuedMessage{}, false
	}
	entry := val.(*queueEntry) //nolint:errcheck // Load always returns *queueEntry //nolint:errcheck // Load always returns *queueEntry
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

// GetQueue returns a snapshot of the current queue for a session.
func GetQueue(sessionID string) []model.QueuedMessage {
	val, ok := sessionQueues.Load(sessionID)
	if !ok {
		return nil
	}
	entry := val.(*queueEntry) //nolint:errcheck // Load always returns *queueEntry //nolint:errcheck // Load always returns *queueEntry
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

// ClearQueue removes all items from the session's queue.
func ClearQueue(sessionID string) {
	sessionQueues.Delete(sessionID)
}
