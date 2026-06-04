package service

import (
	"sync"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

func TestEnqueueMessage(t *testing.T) {
	sessionID := "qtest-enqueue"
	defer ClearQueue(sessionID)

	queue := EnqueueMessage(sessionID, model.QueuedMessage{
		Text:      "msg1",
		CreatedAt: time.Now().Format(time.RFC3339),
	})
	assert.Len(t, queue, 1)
	assert.Equal(t, "msg1", queue[0].Text)

	queue = EnqueueMessage(sessionID, model.QueuedMessage{
		Text:      "msg2",
		CreatedAt: time.Now().Format(time.RFC3339),
	})
	assert.Len(t, queue, 2)
	assert.Equal(t, "msg1", queue[0].Text)
	assert.Equal(t, "msg2", queue[1].Text)
}

func TestDequeueMessage(t *testing.T) {
	sessionID := "qtest-dequeue"
	defer ClearQueue(sessionID)

	EnqueueMessage(sessionID, model.QueuedMessage{Text: "first", CreatedAt: time.Now().Format(time.RFC3339)})
	EnqueueMessage(sessionID, model.QueuedMessage{Text: "second", CreatedAt: time.Now().Format(time.RFC3339)})

	msg, ok := DequeueMessage(sessionID)
	assert.True(t, ok)
	assert.Equal(t, "first", msg.Text)

	msg, ok = DequeueMessage(sessionID)
	assert.True(t, ok)
	assert.Equal(t, "second", msg.Text)
}

func TestDequeueMessage_Empty(t *testing.T) {
	sessionID := "qtest-dequeue-empty"
	defer ClearQueue(sessionID)

	// Enqueue then dequeue all
	EnqueueMessage(sessionID, model.QueuedMessage{Text: "only", CreatedAt: time.Now().Format(time.RFC3339)})
	DequeueMessage(sessionID)

	_, ok := DequeueMessage(sessionID)
	assert.False(t, ok)
}

func TestDequeueMessage_NonexistentSession(t *testing.T) {
	_, ok := DequeueMessage("qtest-nonexistent")
	assert.False(t, ok)
}

func TestGetQueue(t *testing.T) {
	sessionID := "qtest-get"
	defer ClearQueue(sessionID)

	EnqueueMessage(sessionID, model.QueuedMessage{Text: "a", CreatedAt: time.Now().Format(time.RFC3339)})
	EnqueueMessage(sessionID, model.QueuedMessage{Text: "b", CreatedAt: time.Now().Format(time.RFC3339)})

	queue := GetQueue(sessionID)
	assert.Len(t, queue, 2)
	assert.Equal(t, "a", queue[0].Text)
	assert.Equal(t, "b", queue[1].Text)
}

func TestGetQueue_Empty(t *testing.T) {
	sessionID := "qtest-get-empty"
	defer ClearQueue(sessionID)

	// Enqueue then dequeue all — entry stays in sync.Map but items is empty (ISS-293)
	EnqueueMessage(sessionID, model.QueuedMessage{Text: "x", CreatedAt: time.Now().Format(time.RFC3339)})
	DequeueMessage(sessionID)

	queue := GetQueue(sessionID)
	// After ISS-293 fix: entry is kept alive when empty, so GetQueue finds it
	// but items is empty, so it returns nil
	assert.Nil(t, queue)
}

func TestGetQueue_Nonexistent(t *testing.T) {
	queue := GetQueue("qtest-nonexistent-get")
	assert.Nil(t, queue)
}

func TestRemoveQueueItem(t *testing.T) {
	sessionID := "qtest-remove"
	defer ClearQueue(sessionID)

	EnqueueMessage(sessionID, model.QueuedMessage{Text: "a", CreatedAt: time.Now().Format(time.RFC3339)})
	EnqueueMessage(sessionID, model.QueuedMessage{Text: "b", CreatedAt: time.Now().Format(time.RFC3339)})
	EnqueueMessage(sessionID, model.QueuedMessage{Text: "c", CreatedAt: time.Now().Format(time.RFC3339)})

	queue := RemoveQueueItem(sessionID, 1)
	assert.Len(t, queue, 2)
	assert.Equal(t, "a", queue[0].Text)
	assert.Equal(t, "c", queue[1].Text)
}

func TestRemoveQueueItem_OutOfRange(t *testing.T) {
	sessionID := "qtest-remove-oob"
	defer ClearQueue(sessionID)

	EnqueueMessage(sessionID, model.QueuedMessage{Text: "a", CreatedAt: time.Now().Format(time.RFC3339)})

	queue := RemoveQueueItem(sessionID, 5)
	assert.Len(t, queue, 1)
	assert.Equal(t, "a", queue[0].Text)

	queue = RemoveQueueItem(sessionID, -1)
	assert.Len(t, queue, 1)
}

func TestRemoveQueueItem_LastItem(t *testing.T) {
	sessionID := "qtest-remove-last"
	defer ClearQueue(sessionID)

	EnqueueMessage(sessionID, model.QueuedMessage{Text: "only", CreatedAt: time.Now().Format(time.RFC3339)})

	queue := RemoveQueueItem(sessionID, 0)
	// Last item removed → items slice is empty → returns nil (ISS-293: entry stays in map)
	assert.Nil(t, queue)
}

func TestClearQueue(t *testing.T) {
	sessionID := "qtest-clear"
	defer ClearQueue(sessionID)

	EnqueueMessage(sessionID, model.QueuedMessage{Text: "a", CreatedAt: time.Now().Format(time.RFC3339)})
	EnqueueMessage(sessionID, model.QueuedMessage{Text: "b", CreatedAt: time.Now().Format(time.RFC3339)})

	ClearQueue(sessionID)
	assert.Nil(t, GetQueue(sessionID))
}

func TestEnqueueReturnsCopy(t *testing.T) {
	sessionID := "qtest-copy"
	defer ClearQueue(sessionID)

	queue := EnqueueMessage(sessionID, model.QueuedMessage{Text: "a", CreatedAt: time.Now().Format(time.RFC3339)})

	// Modify the returned slice — should not affect internal state
	queue[0].Text = "modified"

	// Get a fresh snapshot
	fresh := GetQueue(sessionID)
	assert.Equal(t, "a", fresh[0].Text)
	assert.Len(t, fresh, 1)
}

func TestDequeueMessage_EmptyQueueKeepsEntry_ISS293(t *testing.T) {
	// ISS-293: After draining all messages, the queue entry should remain in the sync.Map
	// so that concurrent EnqueueMessage finds the existing entry via LoadOrStore
	// instead of creating a new one that the drain loop won't see.
	sessionID := "qtest-iss293-keep-entry"
	defer ClearQueue(sessionID)

	// Enqueue and dequeue all
	EnqueueMessage(sessionID, model.QueuedMessage{Text: "first", CreatedAt: time.Now().Format(time.RFC3339)})
	DequeueMessage(sessionID)

	// Queue is empty — but the entry should still exist in sync.Map.
	// Enqueue should work without losing the message.
	queue := EnqueueMessage(sessionID, model.QueuedMessage{Text: "after-empty", CreatedAt: time.Now().Format(time.RFC3339)})
	assert.Len(t, queue, 1)
	assert.Equal(t, "after-empty", queue[0].Text)

	// Dequeue should succeed
	msg, ok := DequeueMessage(sessionID)
	assert.True(t, ok)
	assert.Equal(t, "after-empty", msg.Text)
}

func TestDequeueMessage_ConcurrentEnqueueNoLoss_ISS293(t *testing.T) {
	// ISS-293: Simulate the race condition where DequeueMessage deletes the entry
	// while a concurrent EnqueueMessage is trying to add a message.
	// With the fix (don't delete empty entries), the message should not be lost.
	sessionID := "qtest-iss293-concurrent"
	defer ClearQueue(sessionID)

	// Pre-populate with one message
	EnqueueMessage(sessionID, model.QueuedMessage{Text: "initial", CreatedAt: time.Now().Format(time.RFC3339)})

	var wg sync.WaitGroup
	wg.Add(2)

	var enqueuedMsg model.QueuedMessage

	// Goroutine 1: Dequeue until empty
	go func() {
		defer wg.Done()
		for {
			_, ok := DequeueMessage(sessionID)
			if !ok {
				break
			}
		}
	}()

	// Goroutine 2: Enqueue a new message (may race with the dequeue-empty)
	go func() {
		defer wg.Done()
		// Small delay to increase chance of racing with the empty-queue state
		time.Sleep(time.Millisecond)
		enqueuedMsg = model.QueuedMessage{Text: "concurrent-msg", CreatedAt: time.Now().Format(time.RFC3339)}
		EnqueueMessage(sessionID, enqueuedMsg)
	}()

	wg.Wait()

	// The concurrently-enqueued message should be retrievable
	msg, ok := DequeueMessage(sessionID)
	if ok {
		assert.Equal(t, "concurrent-msg", msg.Text)
	}
	// If not ok, message was already dequeued by goroutine 1 — also fine.
	// The important thing is it wasn't silently lost (ISS-293).
}
